package backend

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"explo/src/config"
	"explo/src/web"
)

// profileLabelsPath stores display names; this is how the default profile gets a real name
// ("Jacob") without moving its folder.
func (ps *profileStore) profileLabelsPath() string {
	return filepath.Join(ps.root.WebDataDir, "profile-labels.json")
}

func (ps *profileStore) labels() map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(ps.profileLabelsPath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (ps *profileStore) setLabel(name, label string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, ok := ps.profiles[name]; !ok {
		return errProfileNotFound
	}
	labels := ps.labels()
	if label == "" || label == name {
		delete(labels, name)
	} else {
		labels[name] = label
	}
	raw, err := json.MarshalIndent(labels, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ps.profileLabelsPath(), raw, 0644)
}

func validLabel(s string) bool {
	if s == "" || len([]rune(s)) > 32 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (s *Server) handleProfileLabels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.profiles.labels())
}

// POST /api/ui/profiles/{name}/label {"label": "Jacob"} — empty label resets to the profile name.
func (s *Server) handleSetProfileLabel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(body.Label)
	if label != "" && !validLabel(label) {
		http.Error(w, "name must be 1-32 characters", http.StatusBadRequest)
		return
	}
	s.respondProfileChangeLabels(w, s.profiles.setLabel(r.PathValue("name"), label))
}

func (s *Server) respondProfileChangeLabels(w http.ResponseWriter, err error) {
	if err != nil {
		code := http.StatusBadRequest
		if err == errProfileNotFound {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.profiles.labels())
}

// GET /api/ui/shared — the admin account that every profile without its own admin uses.
func (s *Server) handleGetShared(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"values": config.ReadSharedEnv(s.profiles.root.WebDataDir)})
}

// POST /api/ui/shared {"values": {"ADMIN_SYSTEM_USERNAME": "...", ...}}
func (s *Server) handleSaveShared(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Values map[string]string `json:"values"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := config.WriteSharedEnv(s.profiles.root.WebDataDir, body.Values); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	slog.Info("shared admin account updated")
	w.WriteHeader(http.StatusOK)
}

// seedFromDefault copies the non-secret server settings of the default profile into a new profile,
// so only the user name and password have to be typed in.
func (s *Server) seedFromDefault(p *profile) {
	def, ok := s.profiles.get(DefaultProfile)
	if !ok {
		return
	}
	raw, err := os.ReadFile(def.cfg.WebEnvPath)
	if err != nil {
		return
	}
	src := def.settings.ParseEnvText(string(raw))
	seed := map[string]string{}
	for _, k := range []string{"EXPLO_SYSTEM", "SYSTEM_URL", "LIBRARY_NAME", "DOWNLOAD_DIR"} {
		if src[k] != "" {
			seed[k] = src[k]
		}
	}
	if len(seed) == 0 {
		return
	}
	if err := p.settings.UpdateEnvKeys(seed, web.SampleEnv); err != nil {
		slog.Warn("profiles: could not pre-fill the new profile", "err", err.Error())
	}
}
