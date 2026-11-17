package settings

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The Spotify "sp_dc" login cookie lets Explo read personal playlists (Blend, Daily Mix, ...).
// It is stored per profile in <data dir>/spotify-cookie (mode 0600), never in the .env.

var spDCValueRe = regexp.MustCompile(`^[A-Za-z0-9_.~%-]{20,1000}$`)

func (s *Settings) spotifyCookiePath() string {
	return filepath.Join(s.cfg.WebDataDir, "spotify-cookie")
}

// extractSpDC takes either a cookies.txt (Netscape format) or the bare cookie value and returns the sp_dc value.
func extractSpDC(data []byte) (string, bool) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", false
	}
	if !strings.ContainsAny(text, "\t\n") && strings.HasPrefix(text, "sp_dc=") {
		text = strings.TrimPrefix(text, "sp_dc=")
	}
	if spDCValueRe.MatchString(text) {
		return text, true
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#HttpOnly_"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) >= 7 && f[5] == "sp_dc" && strings.HasSuffix(strings.TrimPrefix(f[0], "."), "spotify.com") && spDCValueRe.MatchString(strings.TrimSpace(f[6])) {
			return strings.TrimSpace(f[6]), true
		}
	}
	return "", false
}

// HandleSpotifyCookieStatus: GET /api/ui/spotify-cookie -> {"present": bool}
func (s *Settings) HandleSpotifyCookieStatus(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(s.spotifyCookiePath())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"present": err == nil})
}

// HandleSaveSpotifyCookie: POST /api/ui/spotify-cookie, multipart "file" (cookies.txt) or form field "value" (bare sp_dc).
func (s *Settings) HandleSaveSpotifyCookie(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	var data []byte
	if file, _, err := r.FormFile("file"); err == nil {
		defer func() { _ = file.Close() }()
		data, _ = io.ReadAll(io.LimitReader(file, 2<<20))
	} else {
		data = []byte(r.FormValue("value"))
	}
	value, ok := extractSpDC(data)
	if !ok {
		http.Error(w, "no sp_dc cookie found. Upload the cookies.txt exported while logged in to open.spotify.com, or paste the value of the sp_dc cookie", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(s.cfg.WebDataDir, 0755); err != nil {
		http.Error(w, "failed to store the cookie", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(s.spotifyCookiePath(), []byte(value), 0600); err != nil {
		http.Error(w, "failed to store the cookie", http.StatusInternalServerError)
		return
	}
	slog.Info("Spotify cookie saved", "profile", s.cfg.Profile)
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteSpotifyCookie: DELETE /api/ui/spotify-cookie
func (s *Settings) HandleDeleteSpotifyCookie(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(s.spotifyCookiePath()); err != nil && !os.IsNotExist(err) {
		http.Error(w, "failed to remove the cookie", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
