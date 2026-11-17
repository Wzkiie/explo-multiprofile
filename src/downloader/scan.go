package downloader

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	cfg "explo/src/config"
	"explo/src/models"
	"explo/src/util"
)

// ScanResult summarizes ScanShortFiles.
type ScanResult struct {
	Checked int // files found and measured
	Missing int // tracks whose file was not found at the expected place
	Wrong   int // files with a wrong length
	Moved   int
}

// ScanShortFiles measures already downloaded songs against their real length (tracks with Duration > 0)
// and reports the ones that are far too short or too long. With move set they are not deleted but moved into
// <download dir>/.explo-rejected/, so the next run downloads them again and nothing is lost.
func ScanShortFiles(dc *cfg.DownloadConfig, tracks []*models.Track, move bool) ScanResult {
	var res ScanResult
	y := Youtube{DownloadDir: dc.DownloadDir, Cfg: dc.Youtube}
	for _, t := range tracks {
		if t.Duration <= 0 {
			continue
		}
		t.File = fmt.Sprintf("%s.%s", getFilename(t.Title, t.Artist), dc.Youtube.FileExtension)
		path, err := outputPathFor(y, t)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			res.Missing++
			continue
		}
		got, err := util.ProbeDurationMs(dc.Youtube.FfmpegPath, path)
		if err != nil {
			continue
		}
		res.Checked++
		if util.DurationRatioOK(t.Duration, got, minLengthRatio, maxLengthRatio) {
			continue
		}
		res.Wrong++
		slog.Warn(fmt.Sprintf("wrong length: %ds instead of %ds for %s - %s", got/1000, t.Duration/1000, t.Title, t.Artist), "file", path)
		if move {
			dir := filepath.Join(dc.DownloadDir, ".explo-rejected")
			if err := os.MkdirAll(dir, 0755); err != nil {
				slog.Error("could not create the folder for rejected files", "err", err.Error())
				continue
			}
			if err := os.Rename(path, filepath.Join(dir, filepath.Base(path))); err != nil {
				slog.Error("could not move the file", "file", path, "err", err.Error())
				continue
			}
			res.Moved++
		}
	}
	return res
}
