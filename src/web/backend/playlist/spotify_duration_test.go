package playlist

import (
	"encoding/json"
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
