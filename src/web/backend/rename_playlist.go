package backend

import (
	"encoding/json"
	"errors"
	"net/http"

	"explo/src/web/backend/playlist"
)

// POST /api/ui/playlists/{id}/rename {"name": "..."} — see playlist.Rename.
func (s *Server) handleRenamePlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.profileFrom(r)
	if !ok {
		http.Error(w, "unknown profile", http.StatusNotFound)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if p.run.IsRunning() {
		http.Error(w, "a run is in progress for this profile — wait until it is done", http.StatusConflict)
		return
	}
	res, err := p.playlist.Rename(r.PathValue("id"), body.Name)
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	case errors.Is(err, playlist.ErrRenameNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, playlist.ErrRenameConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, playlist.ErrRenameInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// GET /api/ui/playlist-names — custom names of built-in playlists {"daily-jams": "My Daily Mix"}.
func (s *Server) handlePlaylistNames(w http.ResponseWriter, r *http.Request) {
	p, ok := s.profileFrom(r)
	if !ok {
		http.Error(w, "unknown profile", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p.playlist.PlaylistNames())
}
