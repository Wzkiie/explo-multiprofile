package backend

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// handleImportCustomPlaylist imports a playlist for the current profile and, if "profiles" lists
// others, for each of them as well. Every profile builds the playlist in its own music-server
// account and, for Spotify, reads it with its own login cookie.
// Response: the normal import result of the current profile plus "also": {"<profile>": "ok" | error text}.
func (s *Server) handleImportCustomPlaylist(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var extra struct {
		Profiles []string `json:"profiles"`
	}
	_ = json.Unmarshal(raw, &extra)

	cur, ok := s.profileFrom(r)
	if !ok {
		http.Error(w, "unknown profile", http.StatusNotFound)
		return
	}
	call := func(p *profile) (int, []byte) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/ui/custom-playlists", bytes.NewReader(raw))
		p.playlist.HandleImportCustomPlaylist(rec, req)
		return rec.Code, rec.Body.Bytes()
	}

	code, body := call(cur)
	if code != http.StatusOK || len(extra.Profiles) == 0 {
		w.Header().Set("Content-Type", rec(code, body))
		w.WriteHeader(code)
		_, _ = w.Write(body)
		return
	}

	also := map[string]string{}
	seen := map[string]bool{cur.cfg.Profile: true}
	for _, name := range extra.Profiles {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		p, ok := s.profiles.get(name)
		if !ok {
			also[name] = "unknown profile"
			continue
		}
		c, b := call(p)
		if c == http.StatusOK {
			also[name] = "ok"
		} else {
			also[name] = strings.TrimSpace(string(b))
		}
	}
	var result map[string]any
	if json.Unmarshal(body, &result) != nil {
		result = map[string]any{}
	}
	result["also"] = also
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func rec(code int, body []byte) string {
	if code == http.StatusOK {
		return "application/json"
	}
	return "text/plain; charset=utf-8"
}
