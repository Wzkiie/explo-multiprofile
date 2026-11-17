package config

import (
	"path/filepath"
	"testing"
)

func TestCustomNameForBuiltinPlaylistKeepsDownloadFolder(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlaylistName(dir, "daily-jams", "Mein Daily Mix"); err != nil {
		t.Fatal(err)
	}
	gen := func(replace bool) *Config {
		cfg := &Config{ReplacePlaylist: replace}
		cfg.Flags.Playlist = "daily-jams"
		cfg.ServerCfg.WebDataDir = dir + "/"
		cfg.ClientCfg.PlaylistNFormat = "date"
		cfg.DownloadCfg.DownloadDir = "/music/"
		cfg.DownloadCfg.UseSubDir = true
		cfg.GenPlaylistDetails()
		return cfg
	}
	c := gen(true)
	if c.ClientCfg.PlaylistName != "Mein Daily Mix" {
		t.Fatalf("name in the music server: %q", c.ClientCfg.PlaylistName)
	}
	if c.DownloadCfg.DownloadDir != filepath.Join("/music", "Daily-Jams") {
		t.Fatalf("download folder must keep its name: %q", c.DownloadCfg.DownloadDir)
	}
	k := gen(false) // keep mode: dated copies of the custom name
	if len(k.ClientCfg.PlaylistName) != len("Mein Daily Mix-2026-11-17") || k.ClientCfg.PlaylistName[:len("Mein Daily Mix-")] != "Mein Daily Mix-" {
		t.Fatalf("keep mode name: %q", k.ClientCfg.PlaylistName)
	}
	// reset removes the override
	if err := WritePlaylistName(dir, "daily-jams", ""); err != nil {
		t.Fatal(err)
	}
	if gen(true).ClientCfg.PlaylistName != "Daily-Jams" {
		t.Fatal("reset must bring the default name back")
	}
}
