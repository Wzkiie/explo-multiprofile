package util

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDurationRatioOK(t *testing.T) {
	cases := []struct {
		expected, got int
		ok            bool
	}{
		{200000, 200000, true},
		{200000, 130000, true},   // 65%
		{200000, 30000, false},   // 30 s preview of a 3:20 song
		{200000, 119000, false},  // just under 60%
		{200000, 390000, true},   // under 200%
		{200000, 3600000, false}, // one hour upload
		{0, 5000, true},          // length unknown: nothing to compare
	}
	for _, c := range cases {
		if got := DurationRatioOK(c.expected, c.got, 0.6, 2.0); got != c.ok {
			t.Errorf("expected %d got %d: want %v, got %v", c.expected, c.got, c.ok, got)
		}
	}
}

func TestProbeDurationMs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	file := filepath.Join(t.TempDir(), "tone.mp3")
	if err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", file).Run(); err != nil {
		t.Skip("could not create a test file")
	}
	ms, err := ProbeDurationMs("", file)
	if err != nil || ms < 2800 || ms > 3300 {
		t.Fatalf("length = %d ms, err = %v (want about 3000)", ms, err)
	}
	if _, err := ProbeDurationMs("", file+".missing"); err == nil {
		t.Fatal("a missing file must be an error")
	}
}
