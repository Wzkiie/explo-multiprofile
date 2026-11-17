package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// playlist-names.json (per profile data dir) maps a built-in playlist type ("daily-jams") to the name
// the playlist gets in the music server. The type itself, and with it the source, never changes.

func playlistNamesPath(dataDir string) string {
	return filepath.Join(dataDir, "playlist-names.json")
}

// ReadPlaylistNames returns the custom names of built-in playlists (empty map if none are set).
func ReadPlaylistNames(dataDir string) map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(playlistNamesPath(dataDir)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// WritePlaylistName sets (or, with an empty name, removes) the custom name of a built-in playlist.
func WritePlaylistName(dataDir, playlistType, name string) error {
	names := ReadPlaylistNames(dataDir)
	name = strings.TrimSpace(name)
	if name == "" {
		delete(names, playlistType)
	} else {
		names[playlistType] = name
	}
	raw, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	tmp := playlistNamesPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, playlistNamesPath(dataDir))
}
