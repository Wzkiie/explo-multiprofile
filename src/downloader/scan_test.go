package downloader

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cfg "explo/src/config"
	"explo/src/models"
)

func TestScanShortFilesMovesWrongOnesOnly(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	mk := func(name string, secs string) {
		if err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration="+secs, filepath.Join(dir, name)).Run(); err != nil {
			t.Skip("could not create test audio")
		}
	}
	dc := &cfg.DownloadConfig{DownloadDir: dir, Youtube: cfg.Youtube{FileExtension: "mp3"}}
	good := &models.Track{Title: "Good", Artist: "A", Duration: 3000}   // real file: 3 s
	clip := &models.Track{Title: "Clip", Artist: "A", Duration: 200000} // real length 3:20, file is a 3 s clip
	gone := &models.Track{Title: "Gone", Artist: "A", Duration: 100000}
	unknown := &models.Track{Title: "Unknown", Artist: "A"} // no length: ignored
	mk(getFilename("Good", "A")+".mp3", "3")
	mk(getFilename("Clip", "A")+".mp3", "3")
	mk(getFilename("Unknown", "A")+".mp3", "3")

	res := ScanShortFiles(dc, []*models.Track{good, clip, gone, unknown}, false)
	if res.Checked != 2 || res.Wrong != 1 || res.Missing != 1 || res.Moved != 0 {
		t.Fatalf("report only: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, getFilename("Clip", "A")+".mp3")); err != nil {
		t.Fatal("without move the file must stay")
	}

	res = ScanShortFiles(dc, []*models.Track{good, clip}, true)
	if res.Moved != 1 {
		t.Fatalf("move: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, getFilename("Clip", "A")+".mp3")); !os.IsNotExist(err) {
		t.Fatal("wrong file must be moved away")
	}
	if _, err := os.Stat(filepath.Join(dir, ".explo-rejected", getFilename("Clip", "A")+".mp3")); err != nil {
		t.Fatal("wrong file must land in .explo-rejected (moved, not deleted)")
	}
	if _, err := os.Stat(filepath.Join(dir, getFilename("Good", "A")+".mp3")); err != nil {
		t.Fatal("a good file must not be touched")
	}
}
