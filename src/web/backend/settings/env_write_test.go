package settings

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"explo/src/web/backend/app"
)

func newTestSettings(t *testing.T) (*Settings, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("EXPLO_SYSTEM=subsonic\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return NewSettings(app.Config{WebEnvPath: path, WebDataDir: filepath.Dir(path) + "/"}), path
}

// the UI fires one save per playlist at the same time: none of them may be lost or tear the file
func TestParallelEnvUpdatesKeepEveryKey(t *testing.T) {
	s, path := newTestSettings(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("CUSTOM_P%d_FLAGS", i)
			if err := s.UpdateEnvKeys(map[string]string{key: fmt.Sprintf("--playlist custom-%08d --replace-playlist=true", i)}, nil); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	raw, _ := os.ReadFile(path)
	text := string(raw)
	if err := validateEnvText(text); err != nil {
		t.Fatalf("file is not valid after parallel writes: %v", err)
	}
	for i := 0; i < 40; i++ {
		if !strings.Contains(text, fmt.Sprintf("CUSTOM_P%d_FLAGS=--playlist custom-%08d --replace-playlist=true\n", i, i)) {
			t.Fatalf("update %d lost or cut:\n%s", i, text)
		}
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("temp file left behind")
	}
}

func TestRawSaveRefusesStaleSnapshot(t *testing.T) {
	s, path := newTestSettings(t)
	rec := httptest.NewRecorder()
	s.HandleGetConfigRaw(rec, httptest.NewRequest("GET", "/", nil))
	version := rec.Header().Get("X-Env-Version")
	if version == "" {
		t.Fatal("raw config must carry a version")
	}
	// somebody else changes the file after the editor was opened
	if err := s.UpdateEnvKeys(map[string]string{"DAILY_JAMS_FLAGS": "--playlist daily-jams"}, nil); err != nil {
		t.Fatal(err)
	}
	post := func(body, ver string) int {
		req := httptest.NewRequest("POST", "/api/ui/config", strings.NewReader(body))
		req.Header.Set("X-Env-Version", ver)
		w := httptest.NewRecorder()
		s.HandleSaveConfig(w, req)
		return w.Code
	}
	if c := post("EXPLO_SYSTEM=plex\n", version); c != http.StatusConflict {
		t.Fatalf("stale snapshot must be refused, got %d", c)
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "DAILY_JAMS_FLAGS") {
		t.Fatal("newer settings were overwritten")
	}
	cur, _ := os.ReadFile(path)
	if c := post("EXPLO_SYSTEM=plex\nDAILY_JAMS_FLAGS=--playlist daily-jams\n", envVersion(cur)); c != http.StatusOK {
		t.Fatalf("current snapshot must be accepted, got %d", c)
	}
	if c := post("this is not valid", ""); c != http.StatusBadRequest {
		t.Fatalf("invalid content must be refused, got %d", c)
	}
}
