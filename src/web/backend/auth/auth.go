package auth

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

/* type Server struct {
	mux *http.ServeMux
} */

type AuthStore struct {
	mu       sync.RWMutex
	Username string
	Hash     string
	Disabled bool // true when no credentials are configured; all routes become public
	file     string // credentials changed in the web UI are stored here and override UI_USERNAME/UI_PASSWORD
	sessionManager *SessionManager
}

type storedCreds struct {
	Username string `json:"username"`
	Hash     string `json:"hash"`
}

// LoadFile enables UI-managed credentials: if the file exists it replaces the values from the
// environment (so a login changed in the UI survives restarts); later changes are written to it.
func (a *AuthStore) LoadFile(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.file = path
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c storedCreds
	if json.Unmarshal(raw, &c) == nil && c.Username != "" && c.Hash != "" {
		a.Username, a.Hash, a.Disabled = c.Username, c.Hash, false
	}
}

func (a *AuthStore) isDisabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Disabled
}

func NewAuthStore(user, password string, sessionManager *SessionManager) *AuthStore{
	// No credentials configured at all: run the UI unauthenticated instead of
	// showing a login screen that accepts empty input anyway.
	if user == "" && password == "" {
		return &AuthStore{
			Disabled: true,
			sessionManager: sessionManager,
		}
	}

	hashPass, err := hashPassword(password)
	if err != nil {
		panic("failed to hash password")
	}

	return &AuthStore{
		Username: user,
		Hash: hashPass,
		sessionManager: sessionManager,
	}
}


func (a *AuthStore) CompareCreds(formUser, formPass string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.Disabled {
		return false
	}
	if formUser != a.Username || bcrypt.CompareHashAndPassword([]byte(a.Hash), []byte(formPass)) != nil {
		return false
	}
	return true
}

func (a *AuthStore) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.isDisabled() { // login can be switched on at runtime from the settings
			next.ServeHTTP(w, r)
			return
		}
		sess := a.sessionManager.GetSession(r)

		auth, _ := sess.Get("authenticated").(bool)
		if !auth {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	return string(bytes), err
	
}

