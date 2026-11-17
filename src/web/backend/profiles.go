package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"explo/src/web"
	"explo/src/web/backend/app"
	"explo/src/web/backend/playlist"
	"explo/src/web/backend/run"
	"explo/src/web/backend/settings"
)

// DefaultProfile is the legacy single-user setup (WEB_ENV_PATH + WEB_DATA_PATH).
// Additional users live in <WEB_DATA_PATH>/profiles/<name>/{.env,cache,logs,...}.
const DefaultProfile = "default"

var profileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type profile struct {
	cfg      app.Config
	settings *settings.Settings
	playlist *playlist.Playlist
	run      *run.ManualRun
}

type profileStore struct {
	mu       sync.RWMutex
	root     app.Config
	profiles map[string]*profile
	baseline string // cron table as it was built at container start (see cronSnapshot)
}

func newProfileStore(root app.Config) *profileStore {
	ps := &profileStore{root: root, profiles: map[string]*profile{}}
	root.Profile = DefaultProfile
	ps.root = root
	ps.profiles[DefaultProfile] = newProfile(root)

	defer func() { ps.baseline = ps.cronSnapshot() }()
	entries, err := os.ReadDir(ps.profilesDir())
	if err != nil {
		return ps
	}
	for _, e := range entries {
		if !e.IsDir() || !profileNameRe.MatchString(e.Name()) || e.Name() == DefaultProfile {
			continue
		}
		cfg := ps.namedConfig(e.Name())
		if _, err := os.Stat(cfg.WebEnvPath); err != nil {
			continue // folder without .env is not a profile
		}
		ps.prepareDirs(cfg)
		ps.profiles[e.Name()] = newProfile(cfg)
	}
	return ps
}

func newProfile(cfg app.Config) *profile {
	st := settings.NewSettings(cfg)
	return &profile{cfg: cfg, settings: st, playlist: playlist.NewPlaylist(cfg, st), run: run.NewManualRun(cfg)}
}

func (ps *profileStore) profilesDir() string {
	return filepath.Join(ps.root.WebDataDir, "profiles")
}

func (ps *profileStore) namedConfig(name string) app.Config {
	dir := filepath.Join(ps.profilesDir(), name)
	return app.Config{
		Profile:    name,
		Named:      true,
		WebEnvPath: filepath.Join(dir, ".env"),
		WebDataDir: dir,
		ExploPath:  ps.root.ExploPath,
	}
}

// prepareDirs creates the profile's cache dir and links its covers dir to the shared one,
// because cover URLs (/api/covers/...) are served from the default data dir.
func (ps *profileStore) prepareDirs(cfg app.Config) {
	if err := os.MkdirAll(filepath.Join(cfg.WebDataDir, "cache"), 0755); err != nil {
		slog.Warn("profiles: failed to create cache dir", "profile", cfg.Profile, "err", err.Error())
		return
	}
	shared := ps.root.CoversDir()
	if err := os.MkdirAll(shared, 0755); err != nil {
		slog.Warn("profiles: failed to create shared covers dir", "err", err.Error())
		return
	}
	link := cfg.CoversDir()
	if _, err := os.Lstat(link); err == nil {
		return
	}
	if err := os.Symlink(shared, link); err != nil {
		slog.Warn("profiles: failed to link covers dir", "profile", cfg.Profile, "err", err.Error())
	}
}

func (ps *profileStore) get(name string) (*profile, bool) {
	if name == "" {
		name = DefaultProfile
	}
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.profiles[name]
	return p, ok
}

func (ps *profileStore) names() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	out := make([]string, 0, len(ps.profiles))
	for n := range ps.profiles {
		if n != DefaultProfile {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return append([]string{DefaultProfile}, out...)
}

func (ps *profileStore) all() []*profile {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	out := make([]*profile, 0, len(ps.profiles))
	for _, p := range ps.profiles {
		out = append(out, p)
	}
	return out
}

var errProfileExists = errors.New("profile already exists")

func (ps *profileStore) create(name string) (*profile, error) {
	if !profileNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid profile name (use a-z, 0-9, - and _; max 32 chars)")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, ok := ps.profiles[name]; ok {
		return nil, errProfileExists
	}
	cfg := ps.namedConfig(name)
	if err := os.MkdirAll(cfg.WebDataDir, 0755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(cfg.WebEnvPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfg.WebEnvPath, web.SampleEnv, 0600); err != nil {
			return nil, err
		}
	}
	ps.prepareDirs(cfg)
	p := newProfile(cfg)
	ps.profiles[name] = p
	return p, nil
}

// profileFrom resolves the profile for a request via ?profile=<name> (default: "default").
func (s *Server) profileFrom(r *http.Request) (*profile, bool) {
	return s.profiles.get(r.URL.Query().Get("profile"))
}

// withProfile adapts a profile-bound handler to a plain handler.
func (s *Server) withProfile(h func(p *profile, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.profileFrom(r)
		if !ok {
			http.Error(w, "unknown profile", http.StatusNotFound)
			return
		}
		h(p, w, r)
	}
}

// withProfileOrDefault is for the two public routes: an unknown profile name behaves like the default,
// so they cannot be used to find out which profiles exist without logging in.
func (s *Server) withProfileOrDefault(h func(p *profile, w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.profileFrom(r)
		if !ok {
			p, _ = s.profiles.get(DefaultProfile)
		}
		h(p, w, r)
	}
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.profiles.names()); err != nil {
		slog.Warn("failed to encode profiles", "err", err.Error())
	}
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	p, err := s.profiles.create(body.Name)
	if errors.Is(err, errProfileExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.seedFromDefault(p)
	s.registerProfileJobs(p)
	slog.Info("profile created", "profile", p.cfg.Profile)
	s.handleListProfiles(w, r)
}

var (
	errProfileNotFound = errors.New("profile not found")
	errProfileDefault  = errors.New("the default profile cannot be renamed or deleted")
	errProfileBusy     = errors.New("a run is in progress for this profile")
)

// remove moves the profile's folder to profiles/.trash/<name>-<timestamp> (nothing is deleted)
// and forgets the profile. Takes effect for the cron table after a restart.
func (ps *profileStore) remove(name string) error {
	if name == DefaultProfile {
		return errProfileDefault
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, ok := ps.profiles[name]
	if !ok {
		return errProfileNotFound
	}
	if !p.run.Close() {
		return errProfileBusy
	}
	trash := filepath.Join(ps.profilesDir(), ".trash")
	if err := os.MkdirAll(trash, 0755); err != nil {
		p.run.Reopen()
		return err
	}
	dest := filepath.Join(trash, fmt.Sprintf("%s-%s", name, time.Now().Format("20060102-150405")))
	if err := os.Rename(p.cfg.WebDataDir, dest); err != nil {
		p.run.Reopen()
		return err
	}
	delete(ps.profiles, name)
	return nil
}

// rename moves the profile folder and rebuilds the profile; paths stored inside its .env are updated.
func (ps *profileStore) rename(oldName, newName string) error {
	if oldName == DefaultProfile || newName == DefaultProfile {
		return errProfileDefault
	}
	if !profileNameRe.MatchString(newName) {
		return fmt.Errorf("invalid profile name (use a-z, 0-9, - and _; max 32 chars)")
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	old, ok := ps.profiles[oldName]
	if !ok {
		return errProfileNotFound
	}
	if _, exists := ps.profiles[newName]; exists {
		return errProfileExists
	}
	if !old.run.Close() {
		return errProfileBusy
	}
	newCfg := ps.namedConfig(newName)
	if _, err := os.Stat(newCfg.WebDataDir); err == nil {
		old.run.Reopen()
		return errProfileExists
	}
	if err := os.Rename(old.cfg.WebDataDir, newCfg.WebDataDir); err != nil {
		old.run.Reopen()
		return err
	}
	// keep absolute paths in the .env valid (e.g. COOKIES_PATH)
	if raw, err := os.ReadFile(newCfg.WebEnvPath); err == nil {
		fixed := strings.ReplaceAll(string(raw), old.cfg.WebDataDir, newCfg.WebDataDir)
		if fixed != string(raw) {
			if err := os.WriteFile(newCfg.WebEnvPath, []byte(fixed), 0600); err != nil {
				slog.Warn("profiles: failed to update paths after rename", "err", err.Error())
			}
		}
	}
	delete(ps.profiles, oldName)
	ps.prepareDirs(newCfg)
	ps.profiles[newName] = newProfile(newCfg)
	return nil
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := s.profiles.remove(name)
	if err == nil {
		s.unregisterProfileJobs(name)
	}
	s.respondProfileChange(w, r, err)
}

func (s *Server) handleRenameProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	oldName := r.PathValue("name")
	newName := strings.TrimSpace(strings.ToLower(body.Name))
	err := s.profiles.rename(oldName, newName)
	if err == nil {
		s.unregisterProfileJobs(oldName)
		if p, ok := s.profiles.get(newName); ok {
			s.registerProfileJobs(p)
		}
	}
	s.respondProfileChange(w, r, err)
}

func (s *Server) respondProfileChange(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == nil:
		s.handleListProfiles(w, r)
	case errors.Is(err, errProfileNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, errProfileExists), errors.Is(err, errProfileBusy):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (s *Server) handleRestartStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"needed": s.profiles.restartNeeded()}); err != nil {
		slog.Warn("failed to encode restart status", "err", err.Error())
	}
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	for _, p := range s.profiles.all() {
		if p.run.IsRunning() {
			http.Error(w, "a run is in progress — stop it or wait until it is done", http.StatusConflict)
			return
		}
	}
	slog.Info("restart requested from the web UI")
	w.WriteHeader(http.StatusAccepted)
	app.TriggerRestart()
}

// cronSnapshot describes everything start.sh turns into cron entries: for every profile and every
// *_SCHEDULE that is set, the schedule plus its *_FLAGS. It is compared with the snapshot taken at
// startup to tell whether a restart is really needed (changing something back clears the hint).
func (ps *profileStore) cronSnapshot() string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	var lines []string
	for name, p := range ps.profiles {
		raw, err := os.ReadFile(p.cfg.WebEnvPath)
		if err != nil {
			continue
		}
		vals := map[string]string{}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `'"`)
		}
		for k, sched := range vals {
			job, ok := strings.CutSuffix(k, "_SCHEDULE")
			if !ok || sched == "" {
				continue
			}
			lines = append(lines, name+"|"+job+"|"+sched+"|"+vals[job+"_FLAGS"])
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func (ps *profileStore) restartNeeded() bool {
	return ps.cronSnapshot() != ps.baseline
}
