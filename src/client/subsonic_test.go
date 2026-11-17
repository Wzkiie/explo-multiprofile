package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"explo/src/config"
	"explo/src/util"
)

// fake Navidrome: 3 playlists named "Weekly-Jams" (duplicates) + 1 other, native cover upload.
func TestSubsonicDeleteAllDuplicatesAndCoverUpload(t *testing.T) {
	var mu sync.Mutex
	deleted := []string{}
	var gotAuth, gotField, gotLoginUser string
	var gotBytes int

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getPlaylists", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","playlists":{"playlist":[
			{"id":"a","name":"Weekly-Jams","owner":"anna"},{"id":"b","name":"Other","owner":"anna"},{"id":"c","name":"Weekly-Jams","owner":"bob"},{"id":"d","name":"Weekly-Jams","owner":"Anna"}]}}}`))
	})
	mux.HandleFunc("/rest/deletePlaylist", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = append(deleted, r.URL.Query().Get("id"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
	})
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotLoginUser = body["username"]
		_, _ = w.Write([]byte(`{"token":"jwt123"}`))
	})
	mux.HandleFunc("/api/playlist/p1/image", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-ND-Authorization")
		f, _, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "missing", 400)
			return
		}
		gotField = "image"
		b := make([]byte, 100)
		n, _ := f.Read(b)
		gotBytes = n
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &config.ClientConfig{URL: srv.URL, PlaylistName: "Weekly-Jams", PlaylistID: "p1"}
	cfg.Creds.User = "anna"
	cfg.Creds.Password = "secret"
	c := &Subsonic{Cfg: *cfg, HttpClient: util.NewHttp(util.HttpClientConfig{Timeout: 5})}

	if err := c.DeletePlaylist(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(deleted, ",") != "a,d" {
		t.Fatalf("deleted = %v, want a,d (bob's public playlist must stay)", deleted)
	}

	img := filepath.Join(t.TempDir(), "c.jpg")
	if err := os.WriteFile(img, []byte("\xff\xd8\xff fake jpeg bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPlaylistArtwork(img); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer jwt123" || gotField != "image" || gotLoginUser != "anna" || gotBytes == 0 {
		t.Fatalf("auth=%q field=%q user=%q bytes=%d", gotAuth, gotField, gotLoginUser, gotBytes)
	}
}

func TestMaskSubsonicSecrets(t *testing.T) {
	in := `got 500 from http://h/rest/getPlaylists?f=json&u=anna&t=abc123&s=salt99&v=1.16.1&c=explo`
	out := maskSubsonicSecrets(in)
	if strings.Contains(out, "abc123") || strings.Contains(out, "salt99") || strings.Contains(out, "u=anna") {
		t.Fatalf("secrets leaked: %s", out)
	}
	if !strings.Contains(out, "f=json") || !strings.Contains(out, "v=1.16.1") {
		t.Fatalf("harmless params must stay: %s", out)
	}
}

func TestSubsonicPruneVersions(t *testing.T) {
	var mu sync.Mutex
	deleted := []string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/getPlaylists", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","playlists":{"playlist":[
			{"id":"1","name":"Daily-Jams-2026-11-10","owner":"anna"},
			{"id":"2","name":"Daily-Jams-2026-11-17","owner":"anna"},
			{"id":"3","name":"Daily-Jams-2026-11-12","owner":"anna"},
			{"id":"4","name":"Daily-Jams-2026-11-15","owner":"anna"},
			{"id":"5","name":"Daily-Jams-2026-11-01","owner":"bob"},
			{"id":"6","name":"Daily-Jams","owner":"anna"},
			{"id":"7","name":"Weekly-Jams-2026-11-01","owner":"anna"}]}}}`))
	})
	mux.HandleFunc("/rest/deletePlaylist", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = append(deleted, r.URL.Query().Get("id"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &config.ClientConfig{URL: srv.URL, PlaylistName: "Daily-Jams-2026-11-17"}
	cfg.Creds.User = "anna"
	c := &Subsonic{Cfg: *cfg, HttpClient: util.NewHttp(util.HttpClientConfig{Timeout: 5})}
	if err := c.PruneVersions(3, "2"); err != nil {
		t.Fatal(err)
	}
	// anna's dated copies newest first: 17(2), 15(4), 12(3), 10(1) -> only 10 (id 1) goes
	if len(deleted) != 1 || deleted[0] != "1" {
		t.Fatalf("deleted = %v, want [1]", deleted)
	}
}
