package settings

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"explo/src/web"
)

const maxCookiesBytes = 2 << 20

func (s *Settings) cookiesPath() string {
	return filepath.Join(s.cfg.WebDataDir, "cookies.txt")
}

// looksLikeNetscapeCookies accepts yt-dlp's cookie format: a "Netscape" header or tab-separated 7-field lines.
func looksLikeNetscapeCookies(data []byte) bool {
	text := string(data)
	if strings.Contains(strings.SplitN(text, "\n", 2)[0], "Netscape") {
		return true
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return len(strings.Split(line, "\t")) >= 7
	}
	return false
}

// HandleCookiesStatus: GET /api/ui/cookies -> {"present": bool}
func (s *Settings) HandleCookiesStatus(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(s.cookiesPath())
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"present": err == nil}); err != nil {
		slog.Warn("failed to encode cookies status", "err", err.Error())
	}
}

// HandleUploadCookies: POST /api/ui/cookies (multipart field "file"). Stores the file in the
// profile's data dir (mode 0600) and points COOKIES_PATH at it.
func (s *Settings) HandleUploadCookies(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCookiesBytes+(1<<20))
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file (max 2 MB)", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxCookiesBytes))
	if err != nil || len(data) == 0 {
		http.Error(w, "could not read file", http.StatusBadRequest)
		return
	}
	if !looksLikeNetscapeCookies(data) {
		http.Error(w, "not a cookies.txt in Netscape format (export it with a browser extension like “Get cookies.txt LOCALLY”)", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(s.cfg.WebDataDir, 0755); err != nil {
		http.Error(w, "failed to store cookies", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(s.cookiesPath(), data, 0600); err != nil {
		http.Error(w, "failed to store cookies", http.StatusInternalServerError)
		return
	}
	if err := s.UpdateEnvKeys(map[string]string{"COOKIES_PATH": s.cookiesPath()}, web.SampleEnv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("YouTube cookies saved", "profile", s.cfg.Profile)
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteCookies: DELETE /api/ui/cookies
func (s *Settings) HandleDeleteCookies(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(s.cookiesPath()); err != nil && !os.IsNotExist(err) {
		http.Error(w, "failed to remove cookies", http.StatusInternalServerError)
		return
	}
	if err := s.UpdateEnvKeys(map[string]string{"COOKIES_PATH": ""}, web.SampleEnv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
