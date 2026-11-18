package playlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTracksReadsDuration(t *testing.T) {
	var item partnerItem
	raw := `{"itemV2":{"data":{"__typename":"Track","name":"Song","trackDuration":{"totalMilliseconds":201000},"artists":{"items":[{"profile":{"name":"Artist"}}]},"albumOfTrack":{"name":"Album"}}}}`
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	tr := extractTracks([]partnerItem{item})
	if len(tr) != 1 || tr[0].DurationMs != 201000 {
		t.Fatalf("duration not read: %+v", tr)
	}
}

// regression: the second cache write (after the cover download) used to drop the track lengths,
// so the wrong-length guard never saw a duration
func TestCacheKeepsDurationAfterCoverStep(t *testing.T) {
	dir := t.TempDir()
	tracks := []PlaylistTrack{{Title: "A", Artist: "X", DurationMs: 201000}, {Title: "B", Artist: "Y", DurationMs: 99000}}
	writePrefetchCache(dir, "custom-d1", tracks) // preliminary write + cover step
	keys := cachedTrackDurations(t, dir, "custom-d1")
	if keys["A"] != 201000 || keys["B"] != 99000 {
		t.Fatalf("durations lost in the cache: %v", keys)
	}
}

func cachedTrackDurations(t *testing.T, dir, id string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "cache", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Tracks []struct {
			Title string `json:"title"`
			Dur   int    `json:"durationMs"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, tr := range c.Tracks {
		out[tr.Title] = tr.Dur
	}
	return out
}
