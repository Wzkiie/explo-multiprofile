package main

import (
	"encoding/json"
	"explo/src/logging"
	"explo/src/models"
	"explo/src/web/backend"
	"explo/src/web/backend/playlist"
	"fmt"
	"time"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"explo/src/client"
	"explo/src/config"
	"explo/src/discovery"
	"explo/src/downloader"
	"explo/src/util"
)

type Song struct {
	Title  string
	Artist string
	Album  string
}

// loadCustomTracks reads a custom playlist's track cache and returns them as
// models.Track slices, bypassing the LB discovery step entirely.
func loadCustomTracks(dataDir, playlistID string) ([]*models.Track, string, error) {
	type cachedTrack struct {
		Title      string `json:"title"`
		Artist     string `json:"artist"`
		MainArtist string `json:"mainArtist"`
		Release    string `json:"release"`
		CoverURL   string `json:"coverUrl"`
		CoverPath          string `json:"coverPath"`
		MusicBrainzTrackID string `json:"mbTrackId,omitempty"`
		DurationMs         int    `json:"durationMs,omitempty"`
		DurationMsAlt      int    `json:"duration_ms,omitempty"` // older caches written by other tools
	}
	type cacheFile struct {
		Tracks []cachedTrack `json:"tracks"`
	}
	type customPlaylist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	data, err := os.ReadFile(filepath.Join(dataDir, "cache", playlistID+".json"))
	if err != nil {
		return nil, "", fmt.Errorf("custom playlist %q not found in cache: %w", playlistID, err)
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, "", fmt.Errorf("failed to parse custom playlist cache: %w", err)
	}

	// Look up the human-readable name from metadata
	name := playlistID
	if meta, err := os.ReadFile(filepath.Join(dataDir, "custom-playlists.json")); err == nil {
		var all []customPlaylist
		if json.Unmarshal(meta, &all) == nil {
			for _, p := range all {
				if p.ID == playlistID {
					name = p.Name
					break
				}
			}
		}
	}

	tracks := make([]*models.Track, len(c.Tracks))
	for i, t := range c.Tracks {
		mainArtist := t.MainArtist
		if mainArtist == "" {
			mainArtist = t.Artist
		}
		tracks[i] = &models.Track{
			CleanTitle: t.Title,
			Title:      t.Title,
			Artist:     t.Artist,
			MainArtist: mainArtist,
			Album:      t.Release,
			CoverURL:   t.CoverURL,
			CoverPath:          t.CoverPath,
			MusicBrainzTrackID: t.MusicBrainzTrackID,
			Duration:           max(t.DurationMs, t.DurationMsAlt),
		}
	}
	return tracks, name, nil
}

func initHttpClient() *util.HttpClient {
	return util.NewHttp(util.HttpClientConfig{
		Timeout: 10,
	})
}

// Inits debug, gets playlist name, if needed, handles deprecation
func setup(cfg *config.Config) {
	cfg.HandleDeprecation()
	logging.Init(cfg)
	cfg.GenPlaylistDetails()
}
func runSearchTest(cfg *config.Config, httpClient *util.HttpClient) {
	lb := discovery.NewListenBrainz(cfg.DiscoveryCfg, httpClient)
	track, err := lb.LookupRecording(cfg.Flags.SearchMBID)
	if err != nil {
		log.Fatalf("failed to resolve MBID %s from ListenBrainz: %s", cfg.Flags.SearchMBID, err)
	}
	slog.Info("resolved recording", "title", track.CleanTitle, "artist", track.MainArtist, "album", track.Album, "duration_ms", track.Duration)

	c, err := client.NewClient(cfg)
	if err != nil {
		log.Fatalf("failed to init client: %s", err)
	}
	tracks := []*models.Track{track}
	if err := c.CheckTracks(tracks); err != nil {
		slog.Warn("CheckTracks error", "err", err)
	}

	if track.Present {
		slog.Info("FOUND in library", "system", cfg.System, "key", track.ID)
	} else {
		slog.Info("NOT FOUND in library", "system", cfg.System)
	}
}
func main() {
	// Cron runs of non-default profiles set EXPLO_ISOLATED=1: values from the container environment
	// (docker "environment:") must not override the profile's own .env, like for runs from the web UI.
	if os.Getenv("EXPLO_ISOLATED") == "1" {
		for key := range config.EnvKeys() {
			if key != "WEB_DATA_PATH" {
				_ = os.Unsetenv(key)
			}
		}
	}
	var cfg config.Config
	if err := cfg.GetFlags(); err != nil {
		log.Fatal(err)
	}
	cfg.ReadEnv()
	cfg.MergeFlags()
	setup(&cfg)

	httpClient := initHttpClient()

	if cfg.Flags.SearchMBID != "" {
		runSearchTest(&cfg, httpClient)
		return
	}

	if cfg.Flags.RefreshOnly {
		if err := client.TriggerRefresh(&cfg); err != nil {
			slog.Error("refresh-only failed", "err", err.Error())
			os.Exit(1)
		}
		slog.Info("library refresh triggered")
		return
	}

	if cfg.Flags.RenameFrom != "" {
		os.Exit(renamePlaylists(&cfg))
	}

	if cfg.Flags.CheckShort {
		checkShortFiles(&cfg)
		return
	}

	slog.Info("Starting Explo...")

	if err := os.MkdirAll(cfg.DownloadCfg.Youtube.CoversDir, 0755); err != nil {
		slog.Error("failed making directory", "msg", err.Error())
	}

	if cfg.ServerCfg.Enabled && !cfg.Flags.PlaylistSet {

		exploPath, err := os.Executable()
		if err != nil {
			log.Fatal("could not determine executable path: ", err)
		}

		cfg.ServerCfg.ExploPath = exploPath
		srv := backend.NewServer(cfg.ServerCfg)
		log.Fatal(srv.Start())
	}

	slog.Info("Pulling playlist", "playlist", cfg.Flags.Playlist)

	var tracks []*models.Track
	var err error
	if strings.HasPrefix(cfg.Flags.Playlist, "custom-") {
		var playlistName string
		// pull the current tracks from the source first (Spotify lists change daily)
		if refreshed, err := playlist.RefreshCustomPlaylist(cfg.ServerCfg.WebDataDir, cfg.Flags.Playlist, false); err != nil {
			slog.Warn("could not refresh playlist from its source, using cached tracks", "playlist", cfg.Flags.Playlist, "err", err.Error())
		} else if !refreshed {
			slog.Debug("playlist has no source, using its cached tracks", "playlist", cfg.Flags.Playlist)
		}
		tracks, playlistName, err = loadCustomTracks(cfg.ServerCfg.WebDataDir, cfg.Flags.Playlist)
		if err == nil {
			if strings.TrimSpace(playlistName) == "" {
				playlistName = cfg.Flags.Playlist
			}
			cfg.ClientCfg.PlaylistName = playlistName
			if cp := playlist.GetCustomPlaylist(cfg.ServerCfg.WebDataDir, cfg.Flags.Playlist); cp != nil && cp.Public {
				cfg.ClientCfg.PublicPlaylist = true // visible to everyone, whatever the global setting says
			}
			if !cfg.ReplacePlaylist {
				// keep-every-version mode: dated name, otherwise identical names pile up
				cfg.ClientCfg.PlaylistName = fmt.Sprintf("%s-%s", playlistName, time.Now().Format("2006-01-02"))
			}
			hasMBIDs := func() bool {
				for _, t := range tracks {
					if t.MusicBrainzTrackID != "" {
						return true
					}
				}
				return false
			}()
			if cfg.DiscoveryCfg.Listenbrainz.EnrichTrackMetadata && hasMBIDs {
				enriched, enrichErr := discovery.EnrichTracks(httpClient, cfg.DiscoveryCfg.Listenbrainz, tracks)
				if enrichErr != nil {
					slog.Warn("failed to enrich custom playlist metadata", "error", enrichErr)
				} else {
					tracks = enriched
				}
			}
		}
	} else {
		disc := discovery.NewDiscoverer(cfg.DiscoveryCfg, httpClient)
		tracks, err = disc.Discover()
	}

	if err != nil {
		slog.Error(err.Error(), "notify", true)
		os.Exit(1)
	}
	allTracks := append([]*models.Track(nil), tracks...)
	if cfg.ServerCfg.WebDataDir != "" {
		playlist.WritePlaylistCache(cfg.ServerCfg.WebDataDir, cfg.Flags.Playlist, allTracks, nil)
		slog.Info("Saved playlist", "playlist", cfg.Flags.Playlist, "tracks", len(allTracks))
	}

	client, err := client.NewClient(&cfg)
	if err != nil {
		slog.Error(err.Error(), "notify", true)
		os.Exit(1)
	}
	downloader, err := downloader.NewDownloader(&cfg.DownloadCfg, httpClient, cfg.Flags.ExcludeLocal)
	if err != nil {
		slog.Error(err.Error(), "notify", true)
		os.Exit(1)
	}
	// the old playlist is removed inside client.CreatePlaylist, right before the new one is created
	client.ReplaceExisting = cfg.ReplacePlaylist
	if cfg.Flags.CleanDownloads && cfg.DownloadCfg.UseSubDir {
		downloader.DeleteSongs()
	}
	if cfg.Flags.DownloadMode != "force" {
		if err := client.CheckTracks(tracks); err != nil { // Check if tracks exist on system before downloading
			slog.Warn(err.Error(), "notify", true)
		}
	}

	if cfg.Flags.DownloadMode != "skip" {
		if err := downloader.StartDownload(&tracks); err != nil {
			slog.Error(err.Error(), "notify", true)
			os.Exit(1)
		}
		if len(tracks) == 0 {
			slog.Error("couldn't download any tracks", "notify", true)
			os.Exit(1)
		}
	}

	if err := client.CreatePlaylist(tracks); err != nil {
		slog.Warn(err.Error())
	} else {
		slog.Info("playlist created successfully", "system", cfg.System, "playlistName", cfg.ClientCfg.PlaylistName, "notify", true)
		keep := cfg.Flags.KeepVersions
		if keep == 0 {
			keep = cfg.KeepVersions
		}
		if keep > 0 && !cfg.ReplacePlaylist {
			if pr, ok := client.API.(interface{ PruneVersions(int, string) error }); ok {
				if err := pr.PruneVersions(keep, cfg.ClientCfg.PlaylistID); err != nil {
					slog.Warn(err.Error())
				}
			} else {
				slog.Warn("--keep-versions is only supported for Subsonic/Navidrome, old copies were kept")
			}
		}
		uploadCustomPlaylistArtwork(&cfg, client)
	}
}

// uploadCustomPlaylistArtwork pushes a custom playlist's cached artwork to the music app
// after first successful creation. No-op for non-custom playlists, playlists without
// artwork, or clients that don't support artwork upload (MPD).
func uploadCustomPlaylistArtwork(cfg *config.Config, c *client.Client) {
	// A cover uploaded in the web UI wins and is re-applied on every run,
	// because replacing a playlist creates a new one without cover.
	if uploader, ok := c.API.(client.ArtworkUploader); ok && cfg.ServerCfg.WebDataDir != "" {
		userCover := playlist.UserCoverPath(cfg.ServerCfg.WebDataDir, cfg.Flags.Playlist)
		if _, err := os.Stat(userCover); err == nil {
			if err := uploader.SetPlaylistArtwork(userCover); err != nil {
				slog.Warn("could not set custom playlist cover", "playlist", cfg.Flags.Playlist, "err", err.Error())
			} else {
				slog.Info("custom playlist cover applied", "playlist", cfg.Flags.Playlist, "system", cfg.System)
				return
			}
		}
	}
	if !strings.HasPrefix(cfg.Flags.Playlist, "custom-") {
		return
	}
	cp := playlist.GetCustomPlaylist(cfg.ServerCfg.WebDataDir, cfg.Flags.Playlist)
	// With "replace playlist" the playlist is re-created on every run, so its cover is gone
	// and has to be pushed again; otherwise once after the first creation is enough.
	if cp == nil || cp.ArtworkURL == "" || (cp.ArtworkUploaded && !cfg.ReplacePlaylist) {
		return
	}
	uploader, ok := c.API.(client.ArtworkUploader)
	if !ok {
		return
	}
	path := playlist.CustomPlaylistArtworkPath(cfg.ServerCfg.WebDataDir, cp.ID)
	if _, err := os.Stat(path); err != nil {
		slog.Warn("custom-playlists: artwork not cached locally, skipping upload", "id", cp.ID, "path", path)
		return
	}
	if err := uploader.SetPlaylistArtwork(path); err != nil {
		slog.Warn("custom-playlists: failed to upload playlist artwork", "id", cp.ID, "err", err.Error())
		return
	}
	if err := playlist.MarkCustomPlaylistArtworkUploaded(cfg.ServerCfg.WebDataDir, cp.ID); err != nil {
		slog.Warn("custom-playlists: artwork upload succeeded but flag not persisted", "id", cp.ID, "err", err.Error())
		return
	}
	slog.Info("custom-playlists: playlist artwork uploaded", "id", cp.ID, "system", cfg.System)
}

// checkShortFiles measures the downloaded songs of every playlist cache of this profile that has a known
// real length (see downloader.ScanShortFiles). Songs live in <download dir>/<playlist name>/ (also dated variants).
func checkShortFiles(cfg *config.Config) {
	root := cfg.DownloadCfg.DownloadDir
	if cfg.DownloadCfg.UseSubDir {
		root = filepath.Dir(filepath.Clean(root)) // GenPlaylistDetails appended one sub folder
	}
	files, _ := filepath.Glob(filepath.Join(cfg.ServerCfg.WebDataDir, "cache", "*.json"))
	var total downloader.ScanResult
	withLength := 0
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		tracks, _, err := loadCustomTracks(cfg.ServerCfg.WebDataDir, id)
		if err != nil {
			continue
		}
		known := 0
		for _, t := range tracks {
			if t.Duration > 0 {
				known++
			}
		}
		if known == 0 {
			continue
		}
		withLength += known
		dirs := []string{root}
		if cfg.DownloadCfg.UseSubDir {
			dirs, _ = filepath.Glob(filepath.Join(root, config.PlaylistFolderName(id)+"*"))
		}
		for _, dir := range dirs {
			dc := cfg.DownloadCfg
			dc.DownloadDir = dir
			r := downloader.ScanShortFiles(&dc, tracks, cfg.Flags.MoveShort)
			total.Checked += r.Checked
			total.Wrong += r.Wrong
			total.Moved += r.Moved
			total.Missing += r.Missing
		}
	}
	slog.Info(fmt.Sprintf("%d files checked, %d with a wrong length, %d moved to .explo-rejected, %d not found at the expected place", total.Checked, total.Wrong, total.Moved, total.Missing), "songs_with_known_length", withLength)
	if total.Wrong > 0 && !cfg.Flags.MoveShort {
		slog.Info("run again with --move-short to move them out of the way (nothing is deleted); the next playlist run downloads them again")
	}
	if withLength == 0 {
		slog.Warn("no song has a known length yet: run each playlist once (or refresh it) so the lengths get stored, then check again")
	}
}

// renamePlaylists renames the playlists in the music server (exit codes: 0 ok, 3 not supported by this
// music server, 1 failed). It is started by the web UI when a playlist is renamed there.
func renamePlaylists(cfg *config.Config) int {
	if strings.TrimSpace(cfg.Flags.RenameTo) == "" {
		slog.Error("--rename-playlist needs --to <new name>")
		return 1
	}
	c, err := client.NewClient(cfg)
	if err != nil {
		slog.Error(err.Error())
		return 1
	}
	r, ok := c.API.(interface {
		RenamePlaylists(oldName, newName string) (int, error)
	})
	if !ok {
		slog.Warn("renaming playlists is not supported for this music server, rename it there by hand", "system", cfg.System)
		return 3
	}
	n, err := r.RenamePlaylists(cfg.Flags.RenameFrom, cfg.Flags.RenameTo)
	if err != nil {
		slog.Error("rename failed", "err", err.Error())
		return 1
	}
	slog.Info(fmt.Sprintf("renamed %d playlist(s) in the music server", n), "from", cfg.Flags.RenameFrom, "to", cfg.Flags.RenameTo)
	return 0
}
