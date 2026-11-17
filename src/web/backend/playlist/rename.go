package playlist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"explo/src/config"
	"explo/src/util"
	"explo/src/web"
	"explo/src/web/backend/defs"
)

var (
	ErrRenameInvalid  = errors.New("invalid name")
	ErrRenameConflict = errors.New("name already used")
	ErrRenameNotFound = errors.New("playlist not found")
)

// RenameResult is returned to the UI.
type RenameResult struct {
	Name        string `json:"name"`
	MusicServer string `json:"music_server"` // what happened in the music server ("" = nothing to do)
	Warning     bool   `json:"warning,omitempty"`
}

// renameInServerFn renames the playlist (and its dated copies) in the music server; replaced in tests.
var renameInServerFn = func(p *Playlist, oldName, newName string) (string, bool) {
	if p.cfg.ExploPath == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.cfg.ExploPath, "--config", p.cfg.WebEnvPath, "--rename-playlist", oldName, "--to", newName)
	cmd.Env = p.cfg.ChildEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return "Renamed in the music server too.", false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 3 {
		return "This music server cannot be renamed from Explo: rename the playlist there by hand, otherwise the next run creates a new one.", true
	}
	last := strings.TrimSpace(string(out))
	if i := strings.LastIndex(last, "\n"); i >= 0 {
		last = last[i+1:]
	}
	return "Could not rename it in the music server (" + last + "). Rename it there by hand, otherwise the next run creates a new playlist.", true
}

func validName(s string) bool {
	if s == "" || len([]rune(s)) > 64 {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlnum = true
		}
	}
	return hasAlnum
}

// Rename gives a playlist a new name: custom playlists (id "custom-…") and built-in ones ("daily-jams", …).
// Everything that hangs on the name moves along: the entry in custom-playlists.json, the schedule keys in
// the .env, and the playlist (with its dated copies) in the music server. Covers hang on the id and stay.
// An empty name on a built-in playlist resets it to the default name.
func (p *Playlist) Rename(id, newName string) (RenameResult, error) {
	newName = strings.TrimSpace(newName)
	if _, ok := defs.PlaylistDefs[id]; ok {
		return p.renameBuiltin(id, newName)
	}
	if !defs.CustomIDRe.MatchString(id) {
		return RenameResult{}, ErrRenameNotFound
	}
	if !validName(newName) {
		return RenameResult{}, fmt.Errorf("%w: 1-64 characters with at least one letter or digit, no slashes", ErrRenameInvalid)
	}
	all := loadCustomPlaylists(p.cfg.WebDataDir)
	idx := -1
	for i, cp := range all {
		if cp.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return RenameResult{}, ErrRenameNotFound
	}
	oldName := all[idx].Name
	if newName == oldName {
		return RenameResult{Name: newName}, nil
	}
	newPrefix, oldPrefix := util.CustomEnvPrefix(newName), util.CustomEnvPrefix(oldName)
	for i, cp := range all {
		if i != idx && (strings.EqualFold(cp.Name, newName) || util.CustomEnvPrefix(cp.Name) == newPrefix) {
			return RenameResult{}, fmt.Errorf("%w: another playlist is already called %q", ErrRenameConflict, cp.Name)
		}
	}

	msg, warn := renameInServerFn(p, oldName, newName)

	// schedule + flags live under a key prefix derived from the name
	var env map[string]string
	if data, err := os.ReadFile(p.cfg.WebEnvPath); err == nil {
		env = p.settings.ParseEnvText(string(data))
	} else {
		env = map[string]string{}
	}
	if newPrefix != oldPrefix {
		updates := map[string]string{
			oldPrefix + "_SCHEDULE": "", oldPrefix + "_FLAGS": "",
			newPrefix + "_SCHEDULE": env[oldPrefix+"_SCHEDULE"], newPrefix + "_FLAGS": env[oldPrefix+"_FLAGS"],
		}
		if err := p.settings.UpdateEnvKeys(updates, web.SampleEnv); err != nil {
			return RenameResult{}, err
		}
	}
	all[idx].Name = newName
	if err := saveCustomPlaylists(p.cfg.WebDataDir, all); err != nil {
		return RenameResult{}, err
	}
	return RenameResult{Name: newName, MusicServer: msg, Warning: warn}, nil
}

func (p *Playlist) renameBuiltin(id string, newName string) (RenameResult, error) {
	defaultName := config.PlaylistFolderName(id)
	if newName != "" && !validName(newName) {
		return RenameResult{}, fmt.Errorf("%w: 1-64 characters with at least one letter or digit, no slashes", ErrRenameInvalid)
	}
	names := config.ReadPlaylistNames(p.cfg.WebDataDir)
	oldName := names[id]
	if oldName == "" {
		oldName = defaultName
	}
	target := newName
	if target == "" || target == defaultName {
		target, newName = defaultName, ""
	}
	if target == oldName {
		return RenameResult{Name: target}, nil
	}
	for otherID, other := range names {
		if otherID != id && strings.EqualFold(other, target) {
			return RenameResult{}, fmt.Errorf("%w: another playlist is already called %q", ErrRenameConflict, other)
		}
	}
	for _, cp := range loadCustomPlaylists(p.cfg.WebDataDir) {
		if strings.EqualFold(cp.Name, target) {
			return RenameResult{}, fmt.Errorf("%w: another playlist is already called %q", ErrRenameConflict, cp.Name)
		}
	}
	msg, warn := renameInServerFn(p, oldName, target)
	if err := config.WritePlaylistName(p.cfg.WebDataDir, id, newName); err != nil {
		return RenameResult{}, err
	}
	return RenameResult{Name: target, MusicServer: msg, Warning: warn}, nil
}

// PlaylistNames returns the custom names of built-in playlists (for display in the UI).
func (p *Playlist) PlaylistNames() map[string]string {
	return config.ReadPlaylistNames(p.cfg.WebDataDir)
}
