package downloader

import (
	"testing"

	cfg "explo/src/config"
	"explo/src/models"
)

func TestFilterYTMusicSkipsWrongLengthAndKeepsAlternatives(t *testing.T) {
	c := &Youtube{Cfg: cfg.Youtube{DurationCheck: true}}
	track := models.Track{Title: "Song", Artist: "Artist", MainArtist: "Artist", Duration: 200000} // 3:20
	results := []YTMusicSearchResult{
		{VideoID: "short", Title: "Song (Short)", DurationSeconds: 30},
		{VideoID: "a", Title: "Song", DurationSeconds: 201},
		{VideoID: "album", Title: "Full album", DurationSeconds: 3600},
		{VideoID: "b", Title: "Song live", DurationSeconds: 215},
		{VideoID: "unknown", Title: "Song 2", DurationSeconds: 0}, // length unknown: not excluded
		{VideoID: "c", Title: "Song again", DurationSeconds: 199},
	}
	id, alts, err := c.filterYTMusic(track, results)
	if err != nil {
		t.Fatal(err)
	}
	if id != "a" || len(alts) != maxExtraTries || alts[0] != "b" || alts[1] != "unknown" {
		t.Fatalf("id=%s alts=%v", id, alts)
	}

	// no usable result at all
	if _, _, err := c.filterYTMusic(track, results[:1]); err == nil {
		t.Fatal("only a 30 s clip: must not be accepted")
	}

	// check off, or length unknown: first result wins like before
	off := &Youtube{Cfg: cfg.Youtube{DurationCheck: false}}
	if id, _, _ := off.filterYTMusic(track, results); id != "short" {
		t.Fatalf("with the check off the first result is used, got %s", id)
	}
	noLen := models.Track{Title: "Song", Artist: "Artist", MainArtist: "Artist"}
	if id, _, _ := c.filterYTMusic(noLen, results); id != "short" {
		t.Fatalf("without a known length nothing is filtered, got %s", id)
	}
}
