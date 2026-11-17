package util

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProbeDurationMs returns the length of an audio file in milliseconds using ffprobe
// (taken from the directory of ffmpegPath when set, otherwise from PATH).
func ProbeDurationMs(ffmpegPath, file string) (int, error) {
	bin := "ffprobe"
	if ffmpegPath != "" {
		if dir := filepath.Dir(ffmpegPath); dir != "." {
			bin = filepath.Join(dir, "ffprobe")
		}
	}
	out, err := exec.Command(bin, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", file).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("could not read the length of %s", file)
	}
	return int(secs * 1000), nil
}

// DurationRatioOK reports whether a downloaded length is plausible for the expected one:
// at least minRatio and at most maxRatio of it. Unknown expected length (<= 0) always passes.
func DurationRatioOK(expectedMs, gotMs int, minRatio, maxRatio float64) bool {
	if expectedMs <= 0 {
		return true
	}
	r := float64(gotMs) / float64(expectedMs)
	return r >= minRatio && r <= maxRatio
}
