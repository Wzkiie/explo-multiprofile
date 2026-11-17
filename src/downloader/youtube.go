package downloader

import (
	"context"
	"sync/atomic"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cfg "explo/src/config"
	"explo/src/logging"
	"explo/src/models"
	"explo/src/util"

	ffmpeg "github.com/u2takey/ffmpeg-go"
	"github.com/wader/goutubedl"
)

type Videos struct {
	Items []Item `json:"items"`
}

type ID struct {
	VideoID string `json:"videoId"`
}

type Snippet struct {
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
}

type Item struct {
	ID      ID      `json:"id"`
	Snippet Snippet `json:"snippet"`
}

type YTMusicSearchResult struct {
	VideoID         string `json:"videoId"`
	Title           string `json:"title"`
	DurationSeconds int    `json:"duration_seconds"`
}

// A downloaded song counts as wrong when it is shorter than 60% or longer than 200% of the real length.
const (
	minLengthRatio = 0.6
	maxLengthRatio = 2.0
	maxExtraTries  = 2 // further search results to try after a wrong-length download
)

// counters for the summary line of one run
var (
	durChecked  atomic.Int64
	durRejected atomic.Int64
)

type Youtube struct {
	DownloadDir string
	HttpClient  *util.HttpClient
	Cfg         cfg.Youtube
	gouTubeOpts goutubedl.Options
	Sleep 		int
}

func NewYoutube(cfg cfg.Youtube, discovery, downloadDir string, httpClient *util.HttpClient) *Youtube { // init downloader cfg for youtube
	// check for custom ytdlp options
	if cfg.YtdlpPath != "" {
		goutubedl.Path = cfg.YtdlpPath
	}

	var opts goutubedl.Options
	if _, err := os.Stat(cfg.CookiesPath); err == nil {
		opts.Cookies = cfg.CookiesPath
	}

	return &Youtube{
		DownloadDir: downloadDir,
		Cfg:         cfg,
		HttpClient:  httpClient,
		gouTubeOpts: opts}
}

func (c *Youtube) GetConf() (MonitorConfig, error) {
	return MonitorConfig{}, fmt.Errorf("[youtube] no monitoring required")
}

func (c *Youtube) QueryTrack(track *models.Track) error { // Queries youtube for the song

	query := fmt.Sprintf("%s - %s", track.Title, track.Artist)
	if c.Cfg.APIKey == "" { // if no API key set, use Python YT Music module
		err := c.queryYTMusic(track, query)
		return err
	}

	escQuery := url.PathEscape(query)
	queryURL := fmt.Sprintf("https://youtube.googleapis.com/youtube/v3/search?part=snippet&q=%s&type=video&videoCategoryId=10&key=%s", escQuery, c.Cfg.APIKey)

	body, err := c.HttpClient.MakeRequest("GET", queryURL, nil, nil)
	if err != nil {
		return err
	}
	var videos Videos
	if err = util.ParseResp(body, &videos); err != nil {
		return fmt.Errorf("failed to unmarshal queryYT body: %w", err)
	}

	id := c.gatherVideo(c.Cfg, videos, *track)
	if id == "" {
		return fmt.Errorf("no YouTube video found for track: %s - %s", track.Title, track.Artist)
	}
	track.ID = id

	return nil
}

func (c *Youtube) queryYTMusic(track *models.Track, query string) error {

	slog.Debug(fmt.Sprintf("Querying YTMusic for track %s", query))

	cmd := exec.Command("python3", "search_ytmusic.py", query, "10")

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ytmusicapi subprocess failed: %w", err)
	}

	var results []YTMusicSearchResult
	if err := json.Unmarshal(out, &results); err != nil {
		return fmt.Errorf("failed to parse ytmusicapi JSON: %w", err)
	}

	if len(results) == 0 {
		return fmt.Errorf("no YouTube Music track found for: %s", query)
	}

	id, alts, err := c.filterYTMusic(*track, results)
	if err != nil {
		return err
	}

	track.ID = id
	track.AltIDs = alts
	//log.Printf("Matched track %s => videoId %s", query, track.ID) keeping this until I improve logging (good trace)

	return nil
}

// filterYTMusic returns the best result and up to maxExtraTries alternatives. When the real length of the
// song is known, results whose length is far off are skipped before anything is downloaded.
func (c *Youtube) filterYTMusic(track models.Track, results []YTMusicSearchResult) (string, []string, error) {
	var ids []string
	for _, video := range results {
		if video.VideoID == "" {
			continue
		}
		if ContainsKeyword(track, video.Title, c.Cfg.Filters.FilterList) {
			continue
		}
		if c.Cfg.DurationCheck && track.Duration > 0 && video.DurationSeconds > 0 &&
			!util.DurationRatioOK(track.Duration, video.DurationSeconds*1000, minLengthRatio, maxLengthRatio) {
			slog.Debug("skipping search result with a wrong length", "title", video.Title, "seconds", video.DurationSeconds, "expected_seconds", track.Duration/1000)
			continue
		}
		ids = append(ids, video.VideoID)
		if len(ids) > maxExtraTries {
			break
		}
	}
	if len(ids) == 0 {
		return "", nil, fmt.Errorf("no suitable YouTube Music result found")
	}
	return ids[0], ids[1:], nil
}

func (c *Youtube) GetTrack(track *models.Track) error {
	ctx := context.Background() // ctx for yt-dlp

	track.File = fmt.Sprintf("%s.%s", getFilename(track.Title, track.Artist), c.Cfg.FileExtension)
	check := c.Cfg.DurationCheck && track.Duration > 0

	for {
		track.Present = fetchAndSaveVideo(ctx, *c, track)

		wrong := false
		if track.Present && check {
			durChecked.Add(1)
			wrong = c.wrongLength(track)
		}
		if track.Present && !wrong {
			slog.Info("download finished", "service", "youtube", "track", track.File)
			return nil
		}

		// failed download or wrong length: try the next search result
		if len(track.AltIDs) > 0 {
			track.ID, track.AltIDs = track.AltIDs[0], track.AltIDs[1:]
			continue
		}
		if wrong {
			return fmt.Errorf("skipped %s - %s: no download with the right length found", track.Title, track.Artist)
		}
		return fmt.Errorf("failed to download track: %s - %s", track.Title, track.Artist)
	}
}

// wrongLength measures the file that was just saved. A file that is far too short or too long
// (preview, short, album upload) is deleted and track.Present is reset; true is returned.
func (c *Youtube) wrongLength(track *models.Track) bool {
	path, err := outputPathFor(*c, track)
	if err != nil {
		return false
	}
	got, err := util.ProbeDurationMs(c.Cfg.FfmpegPath, path)
	if err != nil {
		slog.Debug("could not measure the length, keeping the file", "track", track.File, "err", err.Error())
		return false
	}
	if util.DurationRatioOK(track.Duration, got, minLengthRatio, maxLengthRatio) {
		return false
	}
	slog.Warn(fmt.Sprintf("wrong length: %ds instead of %ds for %s - %s", got/1000, track.Duration/1000, track.Title, track.Artist), "service", "youtube")
	if err := os.Remove(path); err != nil {
		slog.Warn("could not delete the wrong file", "path", path, "err", err.Error())
	}
	durRejected.Add(1)
	track.Present = false
	return true
}

// DurationSummary is logged once per run; it reports how many downloads were checked and rejected.
func DurationSummary() {
	if n := durChecked.Swap(0); n > 0 {
		slog.Info(fmt.Sprintf("%d downloads checked, %d with a wrong length thrown away", n, durRejected.Swap(0)), "service", "youtube")
	} else {
		durRejected.Store(0)
	}
}

func (c *Youtube) MonitorDownloads(track []*models.Track) error { // No need to monitor yt-dlp downloads, there is no queue for them
	slog.Info("no further monitoring required", "service", "youtube")
	return nil
}

// gets song under artist topic or personal channel
func getTopic(cfg cfg.Youtube, videos Videos, track models.Track) string {

	for _, v := range videos.Items {
		if (strings.Contains(v.Snippet.ChannelTitle, "- Topic") || v.Snippet.ChannelTitle == track.MainArtist) && !ContainsKeyword(track, v.Snippet.Title, cfg.Filters.FilterList) {
			return v.ID.VideoID
		}
	}
	return ""
}

// gets video stream using yt-dlp
func getVideo(ctx context.Context, c Youtube, videoID string) (*goutubedl.DownloadResult, error) {

	result, err := goutubedl.New(ctx, videoID, c.gouTubeOpts)
	if err != nil {
		return nil, fmt.Errorf("could not create URL for video download (ID: %s): %w", videoID, err)
	}

	downloadResult, err := result.Download(ctx, "bestaudio")
	if err != nil {
		return nil, fmt.Errorf("could not download video: %w", err)
	}

	return downloadResult, nil

}

func saveVideo(c Youtube, track *models.Track, stream *goutubedl.DownloadResult) bool {

	defer func() {
		if err := stream.Close(); err != nil {
			slog.Warn("closing stream failed", "context", err)
		}
	}()

	input := filepath.Join(c.DownloadDir, track.File+".tmp")
	file, err := os.Create(input)
	if err != nil {
		slog.Error("failed to create song file", "err", err)
		return false
	}

	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("file close failed", "context", err)
		}
	}()

	defer func() {
    	if err := os.Remove(input); err != nil {
			slog.Debug(
				fmt.Sprintf("failed to remove %s", input),
				logging.RuntimeAttr(err.Error()),
			)
    	}
}()

	if _, err = io.Copy(file, stream); err != nil {
		slog.Error("failed to copy stream to file", "err", err)
		return false
	}

	metadata := util.BuildffmpegMetadata(*track)

	outputPath, err := outputPathFor(c, track)
	if err != nil {
		slog.Error("invalid path template", "err", err)
		return false
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			slog.Error("failed to create output directory", "err", err)
			return false
	}

	var opts ffmpeg.KwArgs
	var streams []*ffmpeg.Stream
	streams = append(streams, ffmpeg.Input(input))
	if c.Cfg.EmbedCoverArt && track.CoverURL != "" {
		if track.CoverPath == "" {
			_, track.CoverPath = util.DownloadCover(track.CoverURL, c.Cfg.CoversDir)
		}
		streams = append(streams, ffmpeg.Input(track.CoverPath))
		opts = ffmpeg.KwArgs{
			"c:v":            "mjpeg",
			"disposition:v:0": "attached_pic",
			"metadata:s:v":   []string{"title=Album cover", "comment=Cover (front)"},
			"metadata":       metadata,
			"loglevel":       "error",
		}
	} else {
		opts = ffmpeg.KwArgs{
			"map": "0:a",
			"metadata": metadata,
			"loglevel": "error",
		}
	}

	if err := util.WriteMetadata(streams, c.Cfg.FfmpegPath, outputPath, opts); err != nil {
		slog.Error("failed saving file", "err", err)
		return false
	}

	return true
}

// filter out video ID
func (c *Youtube) gatherVideo(cfg cfg.Youtube, videos Videos, track models.Track) string {

	// Try to get the video from the official or topic channel
	if id := getTopic(cfg, videos, track); id != "" {
		return id

	}
	// If official video isn't found, try the first suitable channel
	for _, video := range videos.Items {
		if !ContainsKeyword(track, video.Snippet.Title, c.Cfg.Filters.FilterList) {
			return video.ID.VideoID
		}
	}

	return ""
}

func fetchAndSaveVideo(ctx context.Context, cfg Youtube, track *models.Track) bool {
	stream, err := getVideo(ctx, cfg, track.ID)
	if err != nil {
		slog.Error("failed getting stream for video", "trackID", track.ID, "err", err)
		return false
	}

	if stream != nil {
		return saveVideo(cfg, track, stream)
	}

	slog.Error("stream was empty for video", "trackID", track.ID)
	return false
}

func (c *Youtube) GetDownloadStatus(tracks []*models.Track) (map[string]FileStatus, error) {
	return nil, fmt.Errorf("no monitoring required")
}

func (c *Youtube) Cleanup(track models.Track, ID string) error {
	return nil
}

func (c *Youtube) MoveDownload(srcDir, destDir, trackPath string, track *models.Track) error {

	return nil
}

// outputPathFor is where a finished download of the track is stored (respects PATH_TEMPLATE).
func outputPathFor(c Youtube, track *models.Track) (string, error) {
	if c.Cfg.PathTemplate == "" {
		return filepath.Join(c.DownloadDir, track.File), nil
	}
	relativePath, err := buildTrackPath(c.Cfg.PathTemplate, track)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.DownloadDir, relativePath), nil
}
