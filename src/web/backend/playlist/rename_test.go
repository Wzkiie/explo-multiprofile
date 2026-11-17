package playlist

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"explo/src/config"
	"explo/src/web/backend/app"
	"explo/src/web/backend/settings"
)

func renameFixture(t *testing.T) (*Playlist, string, *[][2]string) {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	env := "CUSTOM_DAILY_MIX_1_FLAGS=--playlist custom-a1 --replace-playlist=true\nCUSTOM_DAILY_MIX_1_SCHEDULE=0 3 * * *\nCUSTOM_OTHER_FLAGS=--playlist custom-b2\n"
	if err := os.WriteFile(envPath, []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{WebEnvPath: envPath, WebDataDir: dir + "/"}
	st := settings.NewSettings(cfg)
	p := NewPlaylist(cfg, st)
	if err := saveCustomPlaylists(dir, []CustomPlaylist{{ID: "custom-a1", Name: "Daily Mix 1"}, {ID: "custom-b2", Name: "Other"}}); err != nil {
		t.Fatal(err)
	}
	var calls [][2]string
	old := renameInServerFn
	renameInServerFn = func(_ *Playlist, o, n string) (string, bool) {
		calls = append(calls, [2]string{o, n})
		return "Renamed in the music server too.", false
	}
	t.Cleanup(func() { renameInServerFn = old })
	return p, dir, &calls
}

func TestRenameCustomMovesEverythingThatHangsOnTheName(t *testing.T) {
	p, dir, calls := renameFixture(t)
	res, err := p.Rename("custom-a1", "  Mein Mix  ")
	if err != nil || res.Name != "Mein Mix" {
		t.Fatalf("rename failed: %v %+v", err, res)
	}
	if len(*calls) != 1 || (*calls)[0] != [2]string{"Daily Mix 1", "Mein Mix"} {
		t.Fatalf("music server rename call: %v", *calls)
	}
	if got := GetCustomPlaylist(dir, "custom-a1"); got == nil || got.Name != "Mein Mix" {
		t.Fatalf("custom-playlists.json not updated: %+v", got)
	}
	raw, _ := os.ReadFile(p.cfg.WebEnvPath)
	env := string(raw)
	if strings.Contains(env, "CUSTOM_DAILY_MIX_1") {
		t.Fatalf("old schedule keys must be gone:\n%s", env)
	}
	if !strings.Contains(env, "CUSTOM_MEIN_MIX_SCHEDULE=0 3 * * *") || !strings.Contains(env, "CUSTOM_MEIN_MIX_FLAGS=--playlist custom-a1 --replace-playlist=true") {
		t.Fatalf("schedule and flags must move to the new prefix:\n%s", env)
	}
	if !strings.Contains(env, "CUSTOM_OTHER_FLAGS=--playlist custom-b2") {
		t.Fatal("other playlists must not be touched")
	}
}

func TestRenameCustomValidation(t *testing.T) {
	p, _, calls := renameFixture(t)
	for name, want := range map[string]error{
		"":        ErrRenameInvalid,
		"///":     ErrRenameInvalid,
		"a/b":     ErrRenameInvalid,
		"!!!":     ErrRenameInvalid, // no letter or digit: no usable schedule key
		"other":   ErrRenameConflict,
		"OTHER":   ErrRenameConflict,
		"Other!!": ErrRenameConflict, // same schedule key prefix as "Other"
	} {
		if _, err := p.Rename("custom-a1", name); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", name, err, want)
		}
	}
	if _, err := p.Rename("custom-zz", "Fine"); !errors.Is(err, ErrRenameNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may be renamed in the music server after a rejected name: %v", *calls)
	}
	if res, err := p.Rename("custom-a1", "Daily Mix 1"); err != nil || res.Name != "Daily Mix 1" || len(*calls) != 0 {
		t.Fatalf("same name is a no-op: %v %+v %v", err, res, *calls)
	}
}

func TestRenameBuiltinKeepsTheSourceAndCanBeReset(t *testing.T) {
	p, dir, calls := renameFixture(t)
	res, err := p.Rename("daily-jams", "Mein Daily Mix")
	if err != nil || res.Name != "Mein Daily Mix" {
		t.Fatalf("%v %+v", err, res)
	}
	if (*calls)[0] != [2]string{"Daily-Jams", "Mein Daily Mix"} {
		t.Fatalf("first rename starts from the default name: %v", *calls)
	}
	if config.ReadPlaylistNames(dir)["daily-jams"] != "Mein Daily Mix" {
		t.Fatal("name not stored")
	}
	// a second rename starts from the custom name
	if _, err := p.Rename("daily-jams", "Noch ein Name"); err != nil || (*calls)[1] != [2]string{"Mein Daily Mix", "Noch ein Name"} {
		t.Fatalf("second rename: %v %v", err, *calls)
	}
	// another built-in or custom playlist already uses the name
	if _, err := p.Rename("weekly-jams", "noch ein name"); !errors.Is(err, ErrRenameConflict) {
		t.Fatalf("duplicate built-in name: %v", err)
	}
	if _, err := p.Rename("weekly-jams", "other"); !errors.Is(err, ErrRenameConflict) {
		t.Fatalf("name of a custom playlist: %v", err)
	}
	// empty name resets to the default
	res, err = p.Rename("daily-jams", "")
	if err != nil || res.Name != "Daily-Jams" {
		t.Fatalf("reset: %v %+v", err, res)
	}
	if _, ok := config.ReadPlaylistNames(dir)["daily-jams"]; ok {
		t.Fatal("override must be removed on reset")
	}
}
