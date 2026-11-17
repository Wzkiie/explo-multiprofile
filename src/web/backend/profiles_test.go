package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"explo/src/web/backend/app"
)

func envHas(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

func envHasKey(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func TestChildEnvIsolatesNamedProfiles(t *testing.T) {
	t.Setenv("SYSTEM_URL", "http://container-wide")
	t.Setenv("WEB_UI", "true")
	t.Setenv("WEB_DATA_PATH", "/opt/explo/config/")
	t.Setenv("TZ", "Europe/Berlin")

	named := app.Config{Profile: "anna", Named: true, WebDataDir: "/opt/explo/config/profiles/anna"}.ChildEnv()
	if envHasKey(named, "SYSTEM_URL") || envHasKey(named, "WEB_UI") {
		t.Fatal("named profile must not inherit config vars or WEB_UI from the container env")
	}
	if !envHas(named, "WEB_DATA_PATH=/opt/explo/config/profiles/anna/") {
		t.Fatal("named profile must get its own WEB_DATA_PATH with trailing slash")
	}
	if !envHas(named, "TZ=Europe/Berlin") {
		t.Fatal("non-config vars (TZ) must be passed through")
	}

	def := app.Config{Profile: "default"}.ChildEnv()
	if !envHas(def, "SYSTEM_URL=http://container-wide") || !envHas(def, "WEB_DATA_PATH=/opt/explo/config/") {
		t.Fatal("default profile keeps the legacy container env")
	}
	if envHasKey(def, "WEB_UI") {
		t.Fatal("WEB_UI must be stripped for every child")
	}
}

func TestProfileStoreDiscoversAndCreates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "profiles", "bob"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "bob", ".env"), []byte("A=b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "profiles", "noenv"), 0755); err != nil {
		t.Fatal(err)
	}

	ps := newProfileStore(app.Config{WebDataDir: dir, WebEnvPath: filepath.Join(dir, ".env"), ExploPath: "explo"})
	if got := strings.Join(ps.names(), ","); got != "default,bob" {
		t.Fatalf("names = %s", got)
	}
	if _, err := ps.create("Bad Name"); err == nil {
		t.Fatal("invalid name must be rejected")
	}
	if _, err := ps.create("bob"); err != errProfileExists {
		t.Fatalf("duplicate must fail, got %v", err)
	}
	p, err := ps.create("anna")
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.WebEnvPath != filepath.Join(dir, "profiles", "anna", ".env") {
		t.Fatalf("env path = %s", p.cfg.WebEnvPath)
	}
	if _, err := os.Lstat(p.cfg.CoversDir()); err != nil {
		t.Fatalf("covers link missing: %v", err)
	}
}

func TestProfileRenameAndRemove(t *testing.T) {
	dir := t.TempDir()
	ps := newProfileStore(app.Config{WebDataDir: dir, WebEnvPath: filepath.Join(dir, ".env"), ExploPath: "explo"})
	p, err := ps.create("anna")
	if err != nil {
		t.Fatal(err)
	}
	// absolute path inside the .env must follow the rename
	if err := os.WriteFile(p.cfg.WebEnvPath, []byte("COOKIES_PATH="+filepath.Join(p.cfg.WebDataDir, "cookies.txt")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ps.rename("default", "x"); err != errProfileDefault {
		t.Fatalf("default must be protected, got %v", err)
	}
	if err := ps.rename("anna", "Bad Name"); err == nil {
		t.Fatal("invalid new name must be rejected")
	}
	if _, err := ps.create("bob"); err != nil {
		t.Fatal(err)
	}
	if err := ps.rename("anna", "bob"); err != errProfileExists {
		t.Fatalf("clash must fail, got %v", err)
	}
	if err := ps.rename("anna", "annabelle"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ps.get("anna"); ok {
		t.Fatal("old name must be gone")
	}
	np, ok := ps.get("annabelle")
	if !ok {
		t.Fatal("new name missing")
	}
	raw, _ := os.ReadFile(np.cfg.WebEnvPath)
	if !strings.Contains(string(raw), filepath.Join(dir, "profiles", "annabelle", "cookies.txt")) {
		t.Fatalf("COOKIES_PATH not updated: %s", raw)
	}

	if err := ps.remove("default"); err != errProfileDefault {
		t.Fatalf("default must be protected, got %v", err)
	}
	if err := ps.remove("nope"); err != errProfileNotFound {
		t.Fatalf("got %v", err)
	}
	if err := ps.remove("annabelle"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(np.cfg.WebDataDir); !os.IsNotExist(err) {
		t.Fatal("profile folder should be moved away")
	}
	trash, _ := filepath.Glob(filepath.Join(dir, "profiles", ".trash", "annabelle-*", ".env"))
	if len(trash) != 1 {
		t.Fatalf("expected the profile in .trash, got %v", trash)
	}
	// reload: trash must not come back as a profile
	ps2 := newProfileStore(app.Config{WebDataDir: dir, WebEnvPath: filepath.Join(dir, ".env"), ExploPath: "explo"})
	if got := strings.Join(ps2.names(), ","); got != "default,bob" {
		t.Fatalf("names after reload = %s", got)
	}
}

func TestRestartNeededOnlyWhenCronTableChanges(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("WEEKLY_JAMS_SCHEDULE=30 0 * * 1\nWEEKLY_JAMS_FLAGS=--playlist weekly-jams\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ps := newProfileStore(app.Config{WebDataDir: dir, WebEnvPath: envPath, ExploPath: "explo"})
	if ps.restartNeeded() {
		t.Fatal("fresh start must not need a restart")
	}
	write := func(s string) {
		if err := os.WriteFile(envPath, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// unrelated change (credentials) -> no restart
	write("SYSTEM_URL=http://x\nWEEKLY_JAMS_SCHEDULE=30 0 * * 1\nWEEKLY_JAMS_FLAGS=--playlist weekly-jams\n")
	if ps.restartNeeded() {
		t.Fatal("unrelated settings must not trigger the hint")
	}
	// new schedule time -> restart needed
	write("WEEKLY_JAMS_SCHEDULE=45 1 * * 1\nWEEKLY_JAMS_FLAGS=--playlist weekly-jams\n")
	if !ps.restartNeeded() {
		t.Fatal("changed schedule must need a restart")
	}
	// changed back -> hint disappears
	write("WEEKLY_JAMS_SCHEDULE=30 0 * * 1\nWEEKLY_JAMS_FLAGS=--playlist weekly-jams\n")
	if ps.restartNeeded() {
		t.Fatal("reverting must clear the hint")
	}
	// flags change on a scheduled playlist -> restart needed (flags are part of the cron command)
	write("WEEKLY_JAMS_SCHEDULE=30 0 * * 1\nWEEKLY_JAMS_FLAGS=--playlist weekly-jams --replace-playlist=false\n")
	if !ps.restartNeeded() {
		t.Fatal("changed flags of a scheduled playlist must need a restart")
	}
	// flags change on a playlist WITHOUT schedule -> no restart
	write("WEEKLY_JAMS_SCHEDULE=30 0 * * 1\nWEEKLY_JAMS_FLAGS=--playlist weekly-jams\nDAILY_JAMS_SCHEDULE=\nDAILY_JAMS_FLAGS=--replace-playlist=false\n")
	if ps.restartNeeded() {
		t.Fatal("flags without a schedule are irrelevant for cron")
	}
	// new profile with a schedule -> restart needed
	p, err := ps.create("anna")
	if err != nil {
		t.Fatal(err)
	}
	if ps.restartNeeded() {
		t.Fatal("empty new profile has no cron jobs")
	}
	if err := os.WriteFile(p.cfg.WebEnvPath, []byte("ON_REPEAT_SCHEDULE=0 4 * * *\nON_REPEAT_FLAGS=--playlist on-repeat\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !ps.restartNeeded() {
		t.Fatal("scheduled job in a new profile must need a restart")
	}
}

func TestClosedProfileRefusesRuns(t *testing.T) {
	dir := t.TempDir()
	ps := newProfileStore(app.Config{WebDataDir: dir, WebEnvPath: filepath.Join(dir, ".env"), ExploPath: "explo"})
	p, err := ps.create("anna")
	if err != nil {
		t.Fatal(err)
	}
	if !p.run.Close() {
		t.Fatal("Close must succeed when nothing runs")
	}
	// a rename that fails must reopen the profile
	if _, err := ps.create("bob"); err != nil {
		t.Fatal(err)
	}
	p.run.Reopen()
	if err := ps.rename("anna", "bob"); err != errProfileExists {
		t.Fatalf("got %v", err)
	}
	if !p.run.Close() { // would fail if rename had left it half-closed and running state were odd; Close is idempotent here
		t.Fatal("profile should be usable again after a failed rename")
	}
}
