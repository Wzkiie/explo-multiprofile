package playlist

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

const maxUserCoverBytes = 10 << 20 // 10 MB, same limit as Navidrome

// UserCoverPath is where a user-uploaded cover for a playlist (built-in or custom) is stored.
// Covers are always normalized to JPEG so every media server accepts them.
func UserCoverPath(dataDir, id string) string {
	return filepath.Join(dataDir, "user_covers", id+".jpg")
}

// normalizeCover decodes a JPEG/PNG/GIF and re-encodes it as JPEG (alpha flattened onto white).
func normalizeCover(r io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// refuse huge pixel dimensions before decoding (a tiny PNG can claim 60000x60000 pixels)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("not a valid JPEG, PNG or GIF image")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8000 || cfg.Height > 8000 || cfg.Width*cfg.Height > 40_000_000 {
		return nil, fmt.Errorf("image too large (%dx%d), please use at most 8000x8000 pixels", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("not a valid JPEG, PNG or GIF image")
	}
	b := img.Bounds()
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, b, img, b.Min, draw.Over)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, flat, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// HandleUploadCover stores a user cover: POST /api/ui/playlists/{id}/cover (multipart field "image").
func (p *Playlist) HandleUploadCover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidPlaylistID(id) {
		http.Error(w, "unknown playlist", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUserCoverBytes+(1<<20))
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image file (max 10 MB)", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()
	data, err := normalizeCover(io.LimitReader(file, maxUserCoverBytes))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := UserCoverPath(p.cfg.WebDataDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		http.Error(w, "failed to store cover", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		http.Error(w, "failed to store cover", http.StatusInternalServerError)
		return
	}
	slog.Info("user cover saved", "profile", p.cfg.Profile, "playlist", id)
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteCover removes the user cover: DELETE /api/ui/playlists/{id}/cover.
func (p *Playlist) HandleDeleteCover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidPlaylistID(id) {
		http.Error(w, "unknown playlist", http.StatusBadRequest)
		return
	}
	if err := os.Remove(UserCoverPath(p.cfg.WebDataDir, id)); err != nil && !os.IsNotExist(err) {
		http.Error(w, "failed to remove cover", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetCover serves the user cover (404 if none): GET /api/ui/playlists/{id}/cover.
func (p *Playlist) HandleGetCover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isValidPlaylistID(id) {
		http.Error(w, "unknown playlist", http.StatusBadRequest)
		return
	}
	path := UserCoverPath(p.cfg.WebDataDir, id)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}
