package run

import (
	"fmt"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"context"
	"sync"

	"explo/src/web/backend/app"
)

// RunStatus is returned by GET /api/run/status.
type RunStatus struct {
	Running  bool `json:"running"`
	ExitCode *int `json:"exit_code,omitempty"`
}

type manualRunState struct {
	mu          sync.Mutex
	running     bool
	closed      bool // profile is being removed/renamed: no new runs
	cancel      context.CancelFunc
	exitCode    *int
	logs        []string
	subscribers map[chan runEvent]struct{}
}

// runEvent is an SSE event sent to connected browser clients.
type runEvent struct {
	typ  string
	data string
}

type ManualRun struct {
	cfg app.Config
	state manualRunState
}

var errRunAlreadyStarted = errors.New("run already in progress")
// ErrProfileClosed is returned when a run is requested for a profile that is being removed or renamed.
var ErrProfileClosed = errors.New("profile is being changed, try again")
var errOtherRunActive = errors.New("a run for another profile is already in progress")

// runSlot is a process-wide semaphore: at most one manual run at a time, whatever the profile.
var runSlot = make(chan struct{}, 1)

func NewManualRun(cfg app.Config) *ManualRun {
	return &ManualRun{
		cfg: cfg,
		state: newManualRunState(),
	}
}

func newManualRunState() manualRunState {
	return manualRunState{subscribers: make(map[chan runEvent]struct{})}
}

func (mr *ManualRun) startRun(args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, mr.cfg.ExploPath, args...)
	cmd.Env = mr.cfg.ChildEnv()

	pr, pw, err := os.Pipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create pipe: %w", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	lf, err := mr.openRunLog()
	if err != nil {
		slog.Warn("failed to open run log", "err", err.Error())
	}

	abort := func(reason error) error {
		mr.state.mu.Unlock()
		cancel()
		if err := pr.Close(); err != nil {
			slog.Warn("failed to close file reader", "err", err.Error())
		}
		if err := pw.Close(); err != nil {
			slog.Warn("failed to close file writer", "err", err.Error())
		}
		if lf != nil {
			if err := lf.Close(); err != nil {
				slog.Warn("failed to close run log", "err", err.Error())
			}
		}
		return reason
	}

	mr.state.mu.Lock()
	if mr.state.closed {
		return abort(ErrProfileClosed)
	}
	if mr.state.running {
		return abort(errRunAlreadyStarted)
	}
	// Only one run at a time across all profiles: parallel runs would fight over
	// the shared download directory, yt-dlp/slskd queues and the music server.
	select {
	case runSlot <- struct{}{}:
	default:
		return abort(errOtherRunActive)
	}
	mr.state.running = true
	mr.state.cancel = cancel
	mr.state.exitCode = nil
	mr.state.logs = nil
	mr.state.mu.Unlock()

	if err := cmd.Start(); err != nil {
		mr.finishRun(1)
		cancel()
		if err := pr.Close(); err != nil {
			slog.Warn("failed to close file reader", "err", err.Error())
		}

		if err := pw.Close(); err != nil {
			slog.Warn("failed to close file writer", "err", err.Error())
		}
		if lf != nil {
			if err := lf.Close(); err != nil {
				slog.Warn("failed to close run log", "err", err.Error())
			}
		}
		return fmt.Errorf("failed to start explo: %w", err)
	}

	// Close write end in parent so reader gets EOF when child exits.
	if err := pw.Close(); err != nil {
		slog.Warn("failed to close file writer", "err", err.Error())
	}

	go mr.collectRunOutput(cmd, pr, lf)
	return nil
}

func (mr *ManualRun) currentRunStatus() RunStatus {
	mr.state.mu.Lock()
	defer mr.state.mu.Unlock()

	var exitCode *int
	if mr.state.exitCode != nil {
		code := *mr.state.exitCode
		exitCode = &code
	}
	return RunStatus{Running: mr.state.running, ExitCode: exitCode}
}

func (mr *ManualRun) finishRun(code int) {
	select {
	case <-runSlot:
	default:
	}
	done := runEvent{typ: "done", data: fmt.Sprintf("%d", code)}

	mr.state.mu.Lock()
	mr.state.running = false
	mr.state.cancel = nil
	mr.state.exitCode = &code
	subscribers := make([]chan runEvent, 0, len(mr.state.subscribers))
	for ch := range mr.state.subscribers {
		subscribers = append(subscribers, ch)
		delete(mr.state.subscribers, ch)
	}
	mr.state.mu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- done:
		default:
		}
		close(ch)
	}
}

// helper to build flag arguments
func buildArgs(playlist, downloadMode, WebEnvPath string, replacePlaylist bool) []string {
	args := []string{"--config", WebEnvPath}
	if playlist != "" {
		args = append(args, "--playlist", playlist)
	}
	if downloadMode != "" {
		args = append(args, "--download-mode", downloadMode)
	}
	if !replacePlaylist {
		args = append(args, "--replace-playlist=false")
	}
	return args
}
// IsRunning reports whether a manual run of this profile is in progress.
func (mr *ManualRun) IsRunning() bool {
	return mr.currentRunStatus().Running
}

// Close stops new runs (used before a profile folder is moved). It fails while a run is active.
func (mr *ManualRun) Close() bool {
	mr.state.mu.Lock()
	defer mr.state.mu.Unlock()
	if mr.state.running {
		return false
	}
	mr.state.closed = true
	return true
}

// Reopen allows runs again after a failed Close-protected change.
func (mr *ManualRun) Reopen() {
	mr.state.mu.Lock()
	mr.state.closed = false
	mr.state.mu.Unlock()
}
