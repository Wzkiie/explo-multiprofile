package app

import (
	"log/slog"
	"os"
	"time"
)

func restartFlagPath() string {
	if p := os.Getenv("EXPLO_RESTART_FLAG"); p != "" {
		return p
	}
	return "/tmp/explo-restart"
}

// RestartFunc performs the restart; replaceable in tests.
var RestartFunc = func() {
	// The web UI may run as a non-root user (PUID/PGID) and cannot signal PID 1, so it only creates
	// a flag file. docker/start.sh watches for it and ends the container; the Docker restart policy
	// (restart: unless-stopped) starts it again and the cron table is rebuilt.
	if err := os.WriteFile(restartFlagPath(), []byte("restart\n"), 0644); err != nil {
		slog.Warn("failed to request restart", "msg", err.Error())
	}
}

// TriggerRestart restarts shortly after the HTTP response has been sent.
func TriggerRestart() {
	go func() {
		time.Sleep(400 * time.Millisecond)
		RestartFunc()
	}()
}
