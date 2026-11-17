package playlist

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"explo/src/config"
	"explo/src/discovery"
	"explo/src/util"
)

// CustomPlaylist holds the metadata for a user-imported playlist.
type CustomPlaylist struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Source          string    `json:"source"`                     // "listenbrainz" | "apple_music" | "spotify"
	SourceURL       string    `json:"source_url,omitempty"`       // original URL for dedup + refresh
	LBMBID          string    `json:"lb_mbid,omitempty"`          // ListenBrainz MBID (backward compat)
	ArtworkURL      string    `json:"artwork_url,omitempty"`      // playlist cover image (Apple Music)
	ArtworkUploaded bool      `json:"artwork_uploaded,omitempty"` // true after artwork has been pushed to the music app
	RefreshDays     int       `json:"refresh_days"`
	ColorIndex      int       `json:"color_index"`
	LastFetched     time.Time `json:"last_fetched"`
}

// CustomPlaylistArtworkPath returns the local file path where a playlist's
// artwork is cached (regardless of whether the file exists).
func CustomPlaylistArtworkPath(cfgDir, id string) string {
	return filepath.Join(cfgDir, "cache", "playlist_artwork", id+".jpg")
}

// GetCustomPlaylist looks up a custom playlist by ID. Returns nil if not found.
func GetCustomPlaylist(cfgDir, id string) *CustomPlaylist {
	for _, p := range loadCustomPlaylists(cfgDir) {
		if p.ID == id {
			cp := p
			return &cp
		}
	}
	return nil
}

// MarkCustomPlaylistArtworkUploaded sets ArtworkUploaded=true and persists.
func MarkCustomPlaylistArtworkUploaded(cfgDir, id string) error {
	playlists := loadCustomPlaylists(cfgDir)
	for i := range playlists {
		if playlists[i].ID == id {
			if playlists[i].ArtworkUploaded {
				return nil
			}
			playlists[i].ArtworkUploaded = true
			return saveCustomPlaylists(cfgDir, playlists)
		}
	}
	return nil
}

var lbMBIDRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

var appleMusicURLRe = regexp.MustCompile(
	`^https?://music\.apple\.com/[a-z]{2}/playlist/[^/]+/(pl\.[a-zA-Z0-9-]+)`,
)

// extractAppleMusicID pulls the playlist ID (pl.xxx) from an Apple Music URL.
func extractAppleMusicID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	m := appleMusicURLRe.FindStringSubmatch(raw)
	if len(m) < 2 {
		return "", fmt.Errorf("not a valid Apple Music playlist URL")
	}
	return m[1], nil
}

// extractLBMBID pulls the playlist UUID out of a ListenBrainz playlist URL or bare MBID string.
func extractLBMBID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	m := lbMBIDRe.FindString(raw)
	if m == "" {
		return "", fmt.Errorf("no ListenBrainz playlist UUID found in %q", raw)
	}
	return m, nil
}

func customPlaylistsPath(cfgDir string) string {
	return filepath.Join(cfgDir, "custom-playlists.json")
}

func loadCustomPlaylists(cfgDir string) []CustomPlaylist {
	data, err := os.ReadFile(customPlaylistsPath(cfgDir))
	if err != nil {
		return nil
	}
	var out []CustomPlaylist
	if err := json.Unmarshal(data, &out); err != nil {
		slog.Warn("custom-playlists: failed to parse metadata", "err", err)
		return nil
	}
	return out
}

func saveCustomPlaylists(cfgDir string, playlists []CustomPlaylist) error {
	raw, err := json.MarshalIndent(playlists, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(customPlaylistsPath(cfgDir), raw, 0644)
}

// FetchResult is the uniform return type for fetching playlist data from any source.
type FetchResult struct {
	Name       string
	ArtworkURL string
	Tracks     []PlaylistTrack
}

// fetchCustomPlaylistTracks dispatches to the appropriate source fetcher.
// This is the single point where source-specific logic lives for fetching.
func fetchCustomPlaylistTracks(p CustomPlaylist, enabled bool) (FetchResult, error) {
	switch p.Source {
	case "apple_music":
		name, art, tracks, err := fetchAppleMusicPlaylist(p.SourceURL, enabled)
		return FetchResult{name, art, tracks}, err
	case "spotify":
		name, art, tracks, err := fetchSpotifyPlaylist(p.SourceURL)
		return FetchResult{name, art, tracks}, err
	default: // "listenbrainz" or legacy empty
		mbid := p.LBMBID
		if mbid == "" && p.SourceURL != "" {
			var err error
			mbid, err = extractLBMBID(p.SourceURL)
			if err != nil {
				return FetchResult{}, err
			}
		}
		if mbid == "" {
			return FetchResult{}, fmt.Errorf("no source data for playlist %s", p.ID)
		}
		httpClient := util.NewHttp(util.HttpClientConfig{Timeout: 30})
		name, modelTracks, err := discovery.FetchPlaylistByMBID(httpClient, mbid)
		if err != nil {
			return FetchResult{}, err
		}
		tracks := modelTracksToPlaylistTracks(modelTracks)
		return FetchResult{Name: name, Tracks: tracks}, nil
	}
}

// extractSourceID validates a URL and returns the canonical ID for the given source.
func extractSourceID(source, url string) (string, error) {
	switch source {
	case "apple_music":
		return extractAppleMusicID(url)
	case "spotify":
		return extractSpotifyID(url)
	default:
		return extractLBMBID(url)
	}
}

// isDuplicate checks whether a playlist with the same source and source ID already exists.
func isDuplicate(source, sourceID string, existing []CustomPlaylist) (string, bool) {
	for _, p := range existing {
		if p.Source != source && p.Source != "" {
			continue
		}
		existID, _ := extractSourceID(p.Source, p.SourceURL)
		if existID == "" && p.LBMBID != "" {
			existID = p.LBMBID
		}
		if existID == "" {
			continue
		}
		if existID == sourceID {
			return p.ID, true
		}
	}
	return "", false
}

func (p *Playlist) PrefetchCovers() {

	coversDir := p.cfg.CoversDir()

	url := randomLocalCoverHiRes(coversDir)
	if url == "" {
		fetchSitewideCovers(coversDir)
	}
}

// customPlaylistTrackCount reads the cached track count for a custom playlist without
// fully parsing the JSON.
func customPlaylistTrackCount(cfgDir, id string) int {
	type mini struct {
		Tracks []json.RawMessage `json:"tracks"`
	}
	data, err := os.ReadFile(filepath.Join(cfgDir, "cache", id+".json"))
	if err != nil {
		return 0
	}
	var m mini
	if err := json.Unmarshal(data, &m); err != nil {
		return 0
	}
	return len(m.Tracks)
}

// Hooks for tests.
var (
	fetchTracksFn = fetchCustomPlaylistTracks
	sleepFn       = time.Sleep
	nowFn         = time.Now
)

// refreshMinInterval is the minimum gap between two refreshes from an external source (Spotify,
// Apple Music), shared by all profiles and processes (CUSTOM_REFRESH_MIN_INTERVAL_SECONDS, default 30).
func refreshMinInterval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("CUSTOM_REFRESH_MIN_INTERVAL_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 30 * time.Second
}

func lastRefreshFile(cfgDir string) string {
	return filepath.Join(filepath.Dir(config.SharedEnvPath(cfgDir)), ".last-refresh")
}

// waitForRefreshSlot sleeps until the minimum interval since the previous external refresh has passed.
// Every run is its own process, so the time of the last refresh is kept in a small file in the config root.
func waitForRefreshSlot(cfgDir string) {
	min := refreshMinInterval()
	if min <= 0 {
		return
	}
	if raw, err := os.ReadFile(lastRefreshFile(cfgDir)); err == nil {
		if last, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw))); err == nil {
			if wait := min - nowFn().Sub(last); wait > 0 {
				if wait > min {
					wait = min // clock jumped backwards: never wait longer than the interval
				}
				slog.Info("waiting before the next refresh from the source", "seconds", int(wait.Seconds()+0.5))
				sleepFn(wait)
			}
		}
	}
}

func markRefreshDone(cfgDir string) {
	if err := os.WriteFile(lastRefreshFile(cfgDir), []byte(nowFn().UTC().Format(time.RFC3339Nano)), 0644); err != nil {
		slog.Debug("could not store the refresh time", "err", err.Error())
	}
}

// cachedTrackKeys returns an identity key per track of the cached list: MusicBrainz id, else title+artist.
func cachedTrackKeys(cfgDir, id string) map[string]bool {
	keys := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(cfgDir, "cache", id+".json"))
	if err != nil {
		return keys
	}
	var c struct {
		Tracks []struct {
			Title  string `json:"title"`
			Artist string `json:"artist"`
			MBID   string `json:"mbTrackId"`
		} `json:"tracks"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return keys
	}
	for _, t := range c.Tracks {
		keys[trackKey(t.MBID, t.Title, t.Artist)] = true
	}
	return keys
}

func trackKey(mbid, title, artist string) string {
	if mbid != "" {
		return "mb:" + mbid
	}
	return "ta:" + strings.ToLower(strings.TrimSpace(title)) + "|" + strings.ToLower(strings.TrimSpace(artist))
}

// RefreshCustomPlaylist re-reads a custom playlist from its source (Spotify, Apple Music,
// ListenBrainz) and rewrites the track cache, so a run always works with the current tracks.
// It is called right before a run; on failure the existing cache and last_fetched stay untouched.
// The bool is true when the list was refreshed.
func RefreshCustomPlaylist(cfgDir, id string, suffixRemoval bool) (bool, error) {
	cp := GetCustomPlaylist(cfgDir, id)
	if cp == nil || (cp.SourceURL == "" && cp.LBMBID == "") {
		return false, nil // manual playlist without a source: nothing to refresh
	}
	external := cp.Source == "spotify" || cp.Source == "apple_music"
	if external {
		waitForRefreshSlot(cfgDir)
	}
	result, err := fetchTracksFn(*cp, suffixRemoval)
	if external {
		markRefreshDone(cfgDir) // also after a failure: do not hammer a source that is struggling
	}
	if err != nil {
		return false, err
	}
	if len(result.Tracks) == 0 {
		return false, fmt.Errorf("source returned no tracks, keeping the cached list")
	}
	old := cachedTrackKeys(cfgDir, id)
	added, current := 0, map[string]bool{}
	for _, t := range result.Tracks {
		k := trackKey(t.MusicBrainzTrackID, t.Title, t.Artist)
		current[k] = true
		if !old[k] {
			added++
		}
	}
	removed := 0
	for k := range old {
		if !current[k] {
			removed++
		}
	}
	writePrefetchCache(cfgDir, id, result.Tracks)
	all := loadCustomPlaylists(cfgDir)
	for i := range all {
		if all[i].ID == id {
			all[i].LastFetched = nowFn().UTC()
		}
	}
	src := cp.Source
	if src == "" {
		src = "listenbrainz"
	}
	slog.Info(src+" tracklist refreshed", "playlist", id, "name", cp.Name, "tracks", len(result.Tracks), "added", added, "removed", removed)
	return true, saveCustomPlaylists(cfgDir, all)
}
