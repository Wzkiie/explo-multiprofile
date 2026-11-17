package client

import (
	"bytes"
	"regexp"
	"sort"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"log/slog"
	"strings"
	"time"

	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/url"

	"explo/src/config"
	"explo/src/models"
	"explo/src/util"
)

type FailedResp struct {
	SubsonicResponse struct {
		Status        string `json:"status"`
		Error         struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"subsonic-response"`
}

type SubResponse struct {
	SubsonicResponse struct {
		Status        string        `json:"status"`
		Version       string        `json:"version"`
		Type          string        `json:"type"`
		ServerVersion string        `json:"serverVersion"`
		SearchResult3 struct {
			Song []struct {
				ID            string    `json:"id"`
				Title         string    `json:"title"`
				Artist        string    `json:"artist"`
				Album         string    `json:"album"`
				Duration      int       `json:"duration"`
				MusicBrainzID string    `json:"musicBrainzId"`
				Path          string    `json:"path"`
			} `json:"song"`
		} `json:"searchResult3"`
		Playlists     struct {
			Playlist []Playlist `json:"playlist"`
		} `json:"playlists"`
		Playlist      Playlist `json:"playlist"`
	} `json:"subsonic-response"`
}

type Playlist struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Comment   string    `json:"comment,omitempty"`
	SongCount int       `json:"songCount"`
	Duration  int       `json:"duration"`
	Public    bool      `json:"public"`
	Owner     string    `json:"owner"`
	Created   time.Time `json:"created"`
	Changed   time.Time `json:"changed"`
	CoverArt  string    `json:"coverArt"`
}

type ScanState struct {
	SubsonicResponse struct {
		Status        string `json:"status"`
		Version       string `json:"version"`
		Type          string `json:"type"`
		ServerVersion string `json:"serverVersion"`
		OpenSubsonic  bool   `json:"openSubsonic"`
		ScanStatus    struct {
			Scanning    bool      `json:"scanning"`
			Count       int       `json:"count"`
			FolderCount int       `json:"folderCount"`
			LastScan    time.Time `json:"lastScan"`
			ScanType    string    `json:"scanType"`
			ElapsedTime int       `json:"elapsedTime"`
		} `json:"scanStatus"`
	} `json:"subsonic-response"`
}

type Subsonic struct {
	Token string
	Salt string
	HttpClient *util.HttpClient
	Cfg config.ClientConfig
}

func NewSubsonic(cfg config.ClientConfig, httpClient *util.HttpClient) *Subsonic {
	return &Subsonic{Cfg: cfg,
		HttpClient: httpClient}
}

func (c *Subsonic) AddHeader() error {
	return nil
}

func (c *Subsonic) GetAuth() error { // Generate salt and token
	var salt = make([]byte, 6)


	_, err := rand.Read(salt)
	if err != nil {
		return fmt.Errorf("failed to read salt: %s", err.Error())
	}

	saltStr := base64.RawURLEncoding.EncodeToString(salt)
	passSalt := fmt.Sprintf("%s%s", c.Cfg.Creds.Password, saltStr)

	token := fmt.Sprintf("%x", md5.Sum([]byte(passSalt)))

	c.Token = url.PathEscape(token)
	c.Salt = url.PathEscape(saltStr)
	return nil
}

func (c *Subsonic) GetLibrary() error {
	return nil
}

func (c *Subsonic) AddLibrary() error {
	return nil
}

func (c *Subsonic) SearchSongs(tracks []*models.Track) error {
	for _, track := range tracks {
		searchQuery := util.CleanSearchTitle(track.CleanTitle)
		reqParam := fmt.Sprintf("search3?query=%s&f=json", url.QueryEscape(searchQuery))

		body, err := c.subsonicRequest(reqParam)
		if err != nil {
			return err
		}

		var resp SubResponse
		if err := util.ParseResp(body, &resp); err != nil {
			return err
		}

		songs := resp.SubsonicResponse.SearchResult3.Song

		if len(songs) == 0 {
			if track.MusicBrainzTrackID != "" {
				slog.Debug("[subsonic] using fallback MB TrackID search", "mbid", track.MusicBrainzTrackID)
				reqParam = fmt.Sprintf("search3?query=%s&f=json", url.QueryEscape(track.MusicBrainzTrackID))
				body, err = c.subsonicRequest(reqParam)
				if err != nil {
					return err
				}

				if err := util.ParseResp(body, &resp); err != nil {
					return err
				}

				songs = resp.SubsonicResponse.SearchResult3.Song
			}

			if len(songs) == 0 {
				slog.Debug("no results returned for query", "query", searchQuery, "trackArtist", track.MainArtist, "trackAlbum", track.Album)
				continue
			}
		}
		
		searchData := make([]SearchResult, 0, len(songs))
		for _, song := range songs {
			searchData = append(searchData, SearchResult{
				ID: song.ID,
				Title: song.Title,
				Album: song.Album,
				Artist: song.Artist,
				Path: song.Path,
				Duration: song.Duration,
				MBID: song.MusicBrainzID,
			})
		}
		trackMatch, ok := BestMatch(track, searchData, c.Cfg.MatchScore)
		if !ok {
			slog.Debug("failed to find match, no results returned above threshold score", "threshold", c.Cfg.MatchScore, "searchQuery", searchQuery)
			continue
		}
		track.ID = trackMatch.ID
		track.Present = true
		slog.Debug("matched track", "searchQuery", searchQuery, "matchScore", trackMatch.Score, "trackArtist", trackMatch.Artist, "trackAlbum", trackMatch.Album)
	}
	return nil
}

func (c *Subsonic) RefreshLibrary() error {
	if c.Cfg.AdminCreds.User != "" && c.Cfg.AdminCreds.Password != "" {
		adminCfg := c.Cfg
		adminCfg.Creds = config.Credentials{User: c.Cfg.AdminCreds.User, Password: c.Cfg.AdminCreds.Password}
		adminClient := NewSubsonic(adminCfg, c.HttpClient)

		if err := adminClient.GetAuth(); err != nil {
			return err
		}
		return adminClient.startScan()
	}

	return c.startScan()
}

func (c *Subsonic) startScan() error {
	reqParam := "startScan?f=json"
	if _, err := c.subsonicRequest(reqParam); err != nil {
		return err
	}
	return nil
}

func (c *Subsonic) CheckRefreshState() bool {
	time.Sleep(500 * time.Millisecond) // small sleep to avoid calling scan status too fast
	var state ScanState
	reqParam := "getScanStatus?f=json"

	for {
		body, err := c.subsonicRequest(reqParam)
		if err != nil {
			slog.Warn("could not check scan status", "err", err)
			return false
		}
		if err = json.Unmarshal(body, &state); err != nil {
			slog.Warn("failed to unmarshal scan status response", "err", err)
			return false
		}
		if !state.SubsonicResponse.ScanStatus.Scanning {
			return true
		}
		slog.Debug("Library scan still ongoing")
		time.Sleep(15 * time.Second)
	}
}

func (c *Subsonic) CreatePlaylist(tracks []*models.Track) error {
	var trackIDs strings.Builder
	for _, track := range tracks { // build songID parameters
		fmt.Fprintf(&trackIDs, "&songId=%s", track.ID)
	}

	reqParam := fmt.Sprintf("createPlaylist?name=%s%s&f=json", url.QueryEscape(c.Cfg.PlaylistName), trackIDs.String())

	body, err := c.subsonicRequest(reqParam)
	if err != nil {
		return err
	}

	var resp SubResponse
	if err := util.ParseResp(body, &resp); err != nil {
        return err
    }
	
	c.Cfg.PlaylistID = resp.SubsonicResponse.Playlist.ID
	return nil
}

func (c *Subsonic) SearchPlaylist() error {
	reqParam := "getPlaylists?f=json"

	body, err := c.subsonicRequest(reqParam)
	if err != nil {
		return err
	}

	var resp SubResponse
	if err := util.ParseResp(body, &resp); err != nil {
        return err
    }

	for _, playlist := range resp.SubsonicResponse.Playlists.Playlist {
		if playlist.Name == c.Cfg.PlaylistName {
			c.Cfg.PlaylistID = playlist.ID
			return nil

		}
	}
	return nil
}

func (c *Subsonic) UpdatePlaylist() error {
	reqParam := fmt.Sprintf("updatePlaylist?playlistId=%s&comment=%s&f=json&public=%t",c.Cfg.PlaylistID, url.QueryEscape(c.Cfg.PlaylistDescr), c.Cfg.PublicPlaylist)

	if _, err := c.subsonicRequest(reqParam); err != nil {
		return err
	}
	return nil
}

// DeletePlaylist removes every playlist with the configured name (not just the first one),
// so duplicates left over from earlier runs get cleaned up as well.
func (c *Subsonic) DeletePlaylist() error {
	ids := []string{}
	body, err := c.subsonicRequest("getPlaylists?f=json")
	if err != nil {
		return fmt.Errorf("could not list playlists, old playlist not removed: %w", err)
	}
	var resp SubResponse
	if err := util.ParseResp(body, &resp); err != nil {
		return fmt.Errorf("could not read playlists, old playlist not removed: %w", err)
	}
	for _, pl := range resp.SubsonicResponse.Playlists.Playlist {
		if pl.Name != c.Cfg.PlaylistName {
			continue
		}
		// getPlaylists also returns public playlists of other users (and, for admins, all of them):
		// only touch the ones that belong to the configured user
		if pl.Owner != "" && !strings.EqualFold(pl.Owner, c.Cfg.Creds.User) {
			continue
		}
		ids = append(ids, pl.ID)
	}
	if len(ids) == 0 && c.Cfg.PlaylistID != "" {
		ids = append(ids, c.Cfg.PlaylistID)
	}
	if len(ids) == 0 {
		return nil // nothing to delete
	}

	var firstErr error
	for _, id := range ids {
		if _, err := c.subsonicRequest(fmt.Sprintf("deletePlaylist?id=%s&f=json", url.QueryEscape(id))); err != nil {
			slog.Warn("failed to delete playlist", "id", id, "err", err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if len(ids) > 1 {
		slog.Info("removed duplicate playlists", "name", c.Cfg.PlaylistName, "count", len(ids))
	}
	return firstErr
}

var datedNameRe = regexp.MustCompile(`^(.*)-(\d{4}-\d{2}-\d{2})$`)

// PruneVersions keeps only the newest `keep` dated copies ("<base>-YYYY-MM-DD") of the playlist
// that was just created and deletes older ones. Only playlists of the configured user are touched,
// the date in the name decides the order (not the change time), and the playlist with keepID is never removed.
func (c *Subsonic) PruneVersions(keep int, keepID string) error {
	if keep < 1 {
		return nil
	}
	m := datedNameRe.FindStringSubmatch(c.Cfg.PlaylistName)
	if m == nil {
		return nil // not a dated name, nothing to prune
	}
	base := m[1]
	body, err := c.subsonicRequest("getPlaylists?f=json")
	if err != nil {
		return fmt.Errorf("could not list playlists for pruning: %w", err)
	}
	var resp SubResponse
	if err := util.ParseResp(body, &resp); err != nil {
		return fmt.Errorf("could not read playlists for pruning: %w", err)
	}
	type dated struct{ id, name, date string }
	var found []dated
	for _, pl := range resp.SubsonicResponse.Playlists.Playlist {
		if pl.Owner != "" && !strings.EqualFold(pl.Owner, c.Cfg.Creds.User) {
			continue
		}
		pm := datedNameRe.FindStringSubmatch(pl.Name)
		if pm == nil || pm[1] != base {
			continue
		}
		found = append(found, dated{pl.ID, pl.Name, pm[2]})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].date != found[j].date {
			return found[i].date > found[j].date
		}
		return found[i].id == keepID && found[j].id != keepID // the new one wins a same-day tie
	})
	var firstErr error
	removed := 0
	for i, d := range found {
		if i < keep || d.id == keepID {
			continue
		}
		if _, err := c.subsonicRequest(fmt.Sprintf("deletePlaylist?id=%s&f=json", url.QueryEscape(d.id))); err != nil {
			slog.Warn("failed to prune old playlist", "name", d.name, "err", err.Error())
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("pruned old playlist versions", "base", base, "kept", keep, "removed", removed)
	}
	return firstErr
}

// SetPlaylistArtwork uploads a cover through Navidrome's native API (Navidrome >= 0.61).
// The Subsonic API itself has no way to set playlist covers, so this logs in at /auth/login
// with the configured user/password and posts the image to /api/playlist/{id}/image.
func (c *Subsonic) SetPlaylistArtwork(localPath string) error {
	if c.Cfg.PlaylistID == "" {
		return fmt.Errorf("subsonic: no PlaylistID set")
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read artwork: %w", err)
	}

	loginBody, err := json.Marshal(map[string]string{"username": c.Cfg.Creds.User, "password": c.Cfg.Creds.Password})
	if err != nil {
		return err
	}
	resp, err := c.HttpClient.MakeRequest("POST", c.Cfg.URL+"/auth/login", bytes.NewReader(loginBody), nil)
	if err != nil {
		return fmt.Errorf("navidrome login failed (cover upload needs Navidrome >= 0.61): %w", err)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp, &login); err != nil || login.Token == "" {
		return fmt.Errorf("navidrome login returned no token")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("image", filepath.Base(localPath))
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.Cfg.URL+"/api/playlist/"+url.PathEscape(c.Cfg.PlaylistID)+"/image", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-ND-Authorization", "Bearer "+login.Token)
	res, err := c.HttpClient.Client.Do(req)
	if err != nil {
		return fmt.Errorf("upload cover: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("upload cover: status %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func (c *Subsonic) subsonicRequest(reqParams string) ([]byte, error) {

	reqURL := fmt.Sprintf("%s/rest/%s&u=%s&t=%s&s=%s&v=%s&c=%s",c.Cfg.URL, reqParams, c.Cfg.Creds.User, c.Token, c.Salt, c.Cfg.Subsonic.Version, c.Cfg.ClientID)
	body, err := c.HttpClient.MakeRequest("GET", reqURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request %s", maskSubsonicSecrets(err.Error()))
	}

	var checkResp FailedResp
	if err = util.ParseResp(body, &checkResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request %s", err.Error())
	} else if checkResp.SubsonicResponse.Status == "failed" {
		return nil, fmt.Errorf("%s", checkResp.SubsonicResponse.Error.Message)
	}
	return body, nil
}

// subsonicSecretRe matches the user, token and salt parameters of a Subsonic request URL.
var subsonicSecretRe = regexp.MustCompile(`([?&](?:u|t|s|p)=)[^&\s"']+`)

// maskSubsonicSecrets hides credentials that HTTP errors may carry inside the request URL.
func maskSubsonicSecrets(msg string) string {
	return subsonicSecretRe.ReplaceAllString(msg, "${1}***")
}
