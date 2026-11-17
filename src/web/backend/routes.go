package backend

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) registerRoutes() {
	distFS, indexHTML := spaFS()
	fileServer := http.FileServer(http.FS(distFS))

	// SPA fallback: serve static assets when they exist, otherwise serve index.html.
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(distFS, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(indexHTML); err != nil {
			slog.Error("failed writing to http", "msg", err.Error())
		}
	})

	s.registerAuthRoutes()
	s.registerSettingRoutes()
	s.registerPlaylistRoutes()
	s.registerRunRoutes()
	s.registerMiscRoutes()

}

func (s *Server) registerAuthRoutes() {
	s.mux.Handle("POST /api/ui/logout", s.auth(s.authStore.HandleLogout))

	// Public routes
	s.mux.HandleFunc("GET /api/ui/csrf", s.authStore.HandleCSRF)
	s.mux.HandleFunc("POST /api/ui/login", s.authStore.HandleLogin)
	s.mux.HandleFunc("GET /api/ui/auth/status", s.authStore.HandleAuthStatus)
}

func (s *Server) registerSettingRoutes() {
	s.mux.Handle("GET /api/ui/config", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleGetConfig(w, r) })))
	s.mux.Handle("POST /api/ui/config", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveConfig(w, r) })))

	s.mux.Handle("POST /api/ui/config/keys", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveKeys(w, r) })))
	s.mux.Handle("GET /api/ui/cookies", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleCookiesStatus(w, r) })))
	s.mux.Handle("POST /api/ui/cookies", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleUploadCookies(w, r) })))
	s.mux.Handle("DELETE /api/ui/cookies", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleDeleteCookies(w, r) })))
	s.mux.Handle("GET /api/ui/spotify-cookie", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSpotifyCookieStatus(w, r) })))
	s.mux.Handle("POST /api/ui/spotify-cookie", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveSpotifyCookie(w, r) })))
	s.mux.Handle("DELETE /api/ui/spotify-cookie", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleDeleteSpotifyCookie(w, r) })))
	s.mux.Handle("GET /api/ui/config/raw", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleGetConfigRaw(w, r) })))
	s.mux.Handle("POST /api/ui/config/reset", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleResetConfig(w, r) })))
	s.mux.Handle("POST /api/ui/config/schedules", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveSchedule(w, r) })))
	s.mux.Handle("POST /api/ui/config/path-template", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSavePathTemplate(w, r) })))
	s.mux.Handle("POST /api/ui/config/enrich-metadata", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveEnrichMetadata(w, r) })))
	s.mux.Handle("POST /api/ui/config/replace-playlist", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveReplacePlaylist(w, r) })))
	s.mux.Handle("POST /api/ui/config/clean-downloads", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveCleanDownloads(w, r) })))
	s.mux.Handle("POST /api/ui/config/suffix-removal", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSaveSuffixRemoval(w, r) })))

	// Path template presets: GET list, POST add; DELETE per name under prefix
	s.mux.Handle("api/ui/path-templates", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandlePathTemplates(w, r) })))
	s.mux.Handle("DELETE /api/ui/path-templates/", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleDeletePathTemplate(w, r) })))

	// Wizard steps (POST) — require auth
	s.mux.Handle("POST /api/ui/wizard/step1", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleWizardStep1(w, r) })))
	s.mux.Handle("POST /api/ui/wizard/step2", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleWizardStep2(w, r) })))
	s.mux.Handle("POST /api/ui/wizard/step3", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleWizardStep3(w, r) })))

	// Public
	s.mux.HandleFunc("GET /api/ui/setup-status", s.withProfileOrDefault(func(p *profile, w http.ResponseWriter, r *http.Request) { p.settings.HandleSetupStatus(w, r) }))

}

func (s *Server) registerPlaylistRoutes() {
	s.mux.Handle("GET /api/ui/playlists", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleGetPlaylist(w, r) })))
	s.mux.Handle("POST /api/ui/playlists/{id}/cover", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleUploadCover(w, r) })))
	s.mux.Handle("DELETE /api/ui/playlists/{id}/cover", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleDeleteCover(w, r) })))
	s.mux.Handle("GET /api/ui/playlists/{id}/cover", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleGetCover(w, r) })))
	s.mux.Handle("POST /api/ui/playlists/prefetch", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandlePrefetchCovers(w, r) })))

	// custom playlists: GET list, POST import (same path); per-ID actions under prefix
	s.mux.Handle("GET /api/ui/custom-playlists", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleGetCustomPlaylists(w, r) })))
	s.mux.Handle("POST /api/ui/custom-playlists", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleImportCustomPlaylist(w, r) })))

	// ID-specific routes: DELETE /api/ui/custom-playlists/{id} and POST .../{id}/refresh
	s.mux.Handle("POST /api/ui/custom-playlists/{id}/refresh", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleRefreshCustomPlaylist(w, r) })))
	s.mux.Handle("DELETE /api/ui/custom-playlists/{id}", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleDeleteCustomPlaylist(w, r) })))

	s.mux.HandleFunc("GET /api/ui/background-art", s.withProfileOrDefault(func(p *profile, w http.ResponseWriter, r *http.Request) { p.playlist.HandleBackgroundArt(w, r) }))
}

func (s *Server) registerRunRoutes() {
	s.mux.Handle("POST /api/ui/run", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.run.HandleRun(w, r) })))
	s.mux.Handle("GET /api/ui/run/events", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.run.HandleRunEvents(w, r) })))
	s.mux.Handle("POST /api/ui/run/stop", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.run.HandleStopRun(w, r) })))
	s.mux.Handle("GET /api/ui/run/status", s.auth(s.withProfile(func(p *profile, w http.ResponseWriter, r *http.Request) { p.run.HandleRunStatus(w, r) })))
}

func (s *Server) registerMiscRoutes() {
	s.mux.Handle("GET /api/ui/profiles", s.auth(s.handleListProfiles))
	s.mux.Handle("POST /api/ui/profiles", s.auth(s.handleCreateProfile))
	s.mux.Handle("DELETE /api/ui/profiles/{name}", s.auth(s.handleDeleteProfile))
	s.mux.Handle("POST /api/ui/profiles/{name}/rename", s.auth(s.handleRenameProfile))
	s.mux.Handle("GET /api/ui/account", s.auth(s.authStore.HandleAccountStatus))
	s.mux.Handle("POST /api/ui/account", s.auth(s.authStore.HandleChangeAccount))
	s.mux.Handle("GET /api/ui/profile-labels", s.auth(s.handleProfileLabels))
	s.mux.Handle("POST /api/ui/profiles/{name}/label", s.auth(s.handleSetProfileLabel))
	s.mux.Handle("GET /api/ui/shared", s.auth(s.handleGetShared))
	s.mux.Handle("POST /api/ui/shared", s.auth(s.handleSaveShared))
	s.mux.Handle("GET /api/ui/restart", s.auth(s.handleRestartStatus))
	s.mux.Handle("POST /api/ui/restart", s.auth(s.handleRestart))
	s.mux.Handle("GET /api/ui/logs", s.auth(s.withProfile(s.handleGetLog)))
	s.mux.Handle("GET /api/ui/browse", s.auth(s.handleBrowse))

	coversDir := filepath.Join(s.cfg.WebDataDir, "cache", "covers")
	s.mux.Handle("GET /api/covers/", http.StripPrefix("/api/covers/", http.FileServer(http.Dir(coversDir))))
}

// small helper func for auth routing
func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return s.authStore.RequireAuth(h)
}
