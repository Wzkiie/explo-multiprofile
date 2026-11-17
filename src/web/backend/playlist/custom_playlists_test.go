package playlist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixture: a profile dir with one Spotify custom playlist and a 3-track cache.
func refreshFixture(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("CUSTOM_REFRESH_MIN_INTERVAL_SECONDS", "30")
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cp := []CustomPlaylist{{ID: "custom-a1", Name: "Daily Mix 1", Source: "spotify", SourceURL: "https://open.spotify.com/playlist/x", LastFetched: old}}
	raw, _ := json.Marshal(cp)
	if err := os.WriteFile(filepath.Join(dir, "custom-playlists.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	cache := `{"tracks":[{"title":"A","artist":"X"},{"title":"B","artist":"X"},{"title":"C","artist":"Y"}]}`
	if err := os.WriteFile(filepath.Join(dir, "cache", "custom-a1.json"), []byte(cache), 0644); err != nil {
		t.Fatal(err)
	}
	// no network: stub the cover download by pre-creating nothing; writePrefetchCache only needs the dir
	return dir
}

func stubTime(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	oldNow, oldSleep := nowFn, sleepFn
	nowFn = func() time.Time { return now }
	sleepFn = func(d time.Duration) { now = now.Add(d) }
	t.Cleanup(func() { nowFn, sleepFn = oldNow, oldSleep })
	return &now
}

func stubFetch(t *testing.T, tracks []PlaylistTrack, err error, calls *int) {
	t.Helper()
	old := fetchTracksFn
	fetchTracksFn = func(_ string, p CustomPlaylist, _ bool) (FetchResult, error) {
		*calls++
		return FetchResult{Name: p.Name, Tracks: tracks}, err
	}
	t.Cleanup(func() { fetchTracksFn = old })
}

func lastFetched(t *testing.T, dir string) time.Time {
	t.Helper()
	return loadCustomPlaylists(dir)[0].LastFetched
}

func TestRefreshWritesCacheAndLastFetched(t *testing.T) {
	dir := refreshFixture(t)
	now := stubTime(t)
	calls := 0
	stubFetch(t, []PlaylistTrack{{Title: "A", Artist: "X"}, {Title: "D", Artist: "Z"}}, nil, &calls)

	ok, err := RefreshCustomPlaylist(dir, "custom-a1", false)
	if err != nil || !ok || calls != 1 {
		t.Fatalf("refresh = %v %v calls=%d", ok, err, calls)
	}
	if !lastFetched(t, dir).Equal(*now) {
		t.Fatalf("last_fetched not updated: %v", lastFetched(t, dir))
	}
	keys := cachedTrackKeys(dir, "custom-a1")
	if len(keys) != 2 || !keys["ta:a|x"] || !keys["ta:d|z"] {
		t.Fatalf("cache not rewritten with the new list: %v", keys)
	}
}

func TestRefreshFailureKeepsCacheAndLastFetched(t *testing.T) {
	dir := refreshFixture(t)
	stubTime(t)
	before := lastFetched(t, dir)
	calls := 0
	stubFetch(t, nil, errors.New("spotify 429"), &calls)

	ok, err := RefreshCustomPlaylist(dir, "custom-a1", false)
	if err == nil || ok {
		t.Fatalf("expected an error and no refresh, got %v %v", ok, err)
	}
	if !lastFetched(t, dir).Equal(before) {
		t.Fatal("last_fetched must only change on success")
	}
	if len(cachedTrackKeys(dir, "custom-a1")) != 3 {
		t.Fatal("old cache must stay readable and unchanged")
	}
	// empty result is also treated as a failure
	stubFetch(t, []PlaylistTrack{}, nil, &calls)
	if ok, err := RefreshCustomPlaylist(dir, "custom-a1", false); ok || err == nil {
		t.Fatalf("empty list must not replace the cache: %v %v", ok, err)
	}
	if len(cachedTrackKeys(dir, "custom-a1")) != 3 {
		t.Fatal("cache replaced by an empty list")
	}
}

func TestRefreshWaitsForMinInterval(t *testing.T) {
	dir := refreshFixture(t)
	now := stubTime(t)
	slept := time.Duration(0)
	sleepFn = func(d time.Duration) { slept += d; *now = now.Add(d) }
	calls := 0
	stubFetch(t, []PlaylistTrack{{Title: "A", Artist: "X"}}, nil, &calls)

	if _, err := RefreshCustomPlaylist(dir, "custom-a1", false); err != nil {
		t.Fatal(err)
	}
	if slept != 0 {
		t.Fatalf("first refresh must not wait, slept %v", slept)
	}
	*now = now.Add(10 * time.Second) // 10 s later: 20 s missing
	if _, err := RefreshCustomPlaylist(dir, "custom-a1", false); err != nil {
		t.Fatal(err)
	}
	if slept != 20*time.Second || calls != 2 {
		t.Fatalf("expected a 20s wait and a second fetch, slept=%v calls=%d", slept, calls)
	}
	// interval 0 turns the wait off
	t.Setenv("CUSTOM_REFRESH_MIN_INTERVAL_SECONDS", "0")
	slept = 0
	if _, err := RefreshCustomPlaylist(dir, "custom-a1", false); err != nil || slept != 0 {
		t.Fatalf("interval 0 must not wait: %v %v", err, slept)
	}
}

func TestRefreshSkipsPlaylistWithoutSource(t *testing.T) {
	dir := refreshFixture(t)
	raw, _ := json.Marshal([]CustomPlaylist{{ID: "custom-a1", Name: "Manual"}})
	_ = os.WriteFile(filepath.Join(dir, "custom-playlists.json"), raw, 0644)
	calls := 0
	stubFetch(t, nil, nil, &calls)
	if ok, err := RefreshCustomPlaylist(dir, "custom-a1", false); ok || err != nil || calls != 0 {
		t.Fatalf("source-less playlist must be left alone: %v %v calls=%d", ok, err, calls)
	}
}

func TestTrackKeyPrefersMBID(t *testing.T) {
	if trackKey("abc", "T", "A") != "mb:abc" {
		t.Fatal("MusicBrainz id must win")
	}
	if trackKey("", " Song ", "ARTIST") != trackKey("", "song", "artist") {
		t.Fatal("title/artist key must ignore case and spaces")
	}
}
