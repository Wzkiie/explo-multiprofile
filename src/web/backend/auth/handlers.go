package auth

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"log/slog"
	"encoding/json"
)

func (a *AuthStore) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if a.isDisabled() {
		writeAuthStatus(w, true, true)
		return
	}

	sess := a.sessionManager.GetSession(r)
	auth, _ := sess.Get("authenticated").(bool)
	if !auth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeAuthStatus(w, true, false)
}

func writeAuthStatus(w http.ResponseWriter, authenticated, disabled bool) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{
		"authenticated": authenticated,
		"auth_disabled": disabled,
	}); err != nil {
		slog.Error("failed encoding auth status to http", "msg", err.Error())
	}
}

func (a *AuthStore) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		err := http.StatusMethodNotAllowed
		http.Error(w, "Invalid request method", err)
		return
	}

	// Nothing to authenticate against. Accept, so a cached frontend that still
	// renders the login form does not get a confusing 401 from the empty hash.
	if a.isDisabled() {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if !a.CompareCreds(username, password) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sess := a.sessionManager.GetSession(r)
	sess.Put("authenticated", true)
	sess.Put("username", username)
	
	if err := a.sessionManager.Migrate(sess); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	slog.Info("successful login", "user", username)
}

func (a *AuthStore) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if a.isDisabled() {
		w.WriteHeader(http.StatusOK)
		return
	}

	sess := a.sessionManager.GetSession(r)
	sess.Delete("authenticated")
	sess.Delete("username")
	w.WriteHeader(http.StatusOK)
}

func (a *AuthStore) HandleCSRF(w http.ResponseWriter, r *http.Request) {
	session := a.sessionManager.GetSession(r)

	token, _ := session.Get("csrf_token").(string)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]string{
		"csrf_token": token,
	}); err != nil {
		slog.Error("failed encoding token to http", "msg", err.Error())
	}
}

// GET /api/ui/account — current login name (no secrets).
func (a *AuthStore) HandleAccountStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	user, disabled := a.Username, a.Disabled
	a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"username": user, "auth_disabled": disabled})
}

// POST /api/ui/account {"current_password", "username", "new_password"}
// Changes the web UI login. With login switched on the current password is required; with login
// off, this turns it on.
func (a *AuthStore) HandleChangeAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current  string `json:"current_password"`
		Username string `json:"username"`
		Password string `json:"new_password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.Disabled && bcrypt.CompareHashAndPassword([]byte(a.Hash), []byte(body.Current)) != nil {
		http.Error(w, "current password is wrong", http.StatusForbidden)
		return
	}
	user := strings.TrimSpace(body.Username)
	if user == "" {
		user = a.Username
	}
	if user == "" || len([]rune(user)) > 64 || strings.IndexFunc(user, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		http.Error(w, "username: 1-64 characters, no spaces", http.StatusBadRequest)
		return
	}
	hash := a.Hash
	if body.Password != "" || a.Disabled {
		if len(body.Password) < 8 || len(body.Password) > 72 {
			http.Error(w, "new password must be 8-72 characters", http.StatusBadRequest)
			return
		}
		h, err := hashPassword(body.Password)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		hash = h
	}
	if a.file == "" {
		http.Error(w, "changing the login is not available", http.StatusInternalServerError)
		return
	}
	raw, _ := json.Marshal(storedCreds{Username: user, Hash: hash})
	if err := os.MkdirAll(filepath.Dir(a.file), 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmp := a.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, a.file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.Username, a.Hash, a.Disabled = user, hash, false
	slog.Info("web UI login changed", "user", user)
	w.WriteHeader(http.StatusOK)
}
