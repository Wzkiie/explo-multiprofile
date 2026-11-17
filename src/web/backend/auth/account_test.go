package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func change(a *AuthStore, body string) int {
	rec := httptest.NewRecorder()
	a.HandleChangeAccount(rec, httptest.NewRequest("POST", "/api/ui/account", strings.NewReader(body)))
	return rec.Code
}

func TestChangeAccount(t *testing.T) {
	sm := NewSessionManager(NewInMemorySessionStore(), time.Hour, time.Hour, "s")
	file := filepath.Join(t.TempDir(), "ui-auth.json")
	a := NewAuthStore("admin", "oldpassword", sm)
	a.LoadFile(file)

	if c := change(a, `{"current_password":"wrong","new_password":"newpassword1"}`); c != http.StatusForbidden {
		t.Fatalf("wrong current password must be rejected, got %d", c)
	}
	if c := change(a, `{"current_password":"oldpassword","new_password":"short"}`); c != http.StatusBadRequest {
		t.Fatalf("short password must be rejected, got %d", c)
	}
	if c := change(a, `{"current_password":"oldpassword","username":"bad name"}`); c != http.StatusBadRequest {
		t.Fatalf("username with space must be rejected, got %d", c)
	}
	if c := change(a, `{"current_password":"oldpassword","username":"jacob","new_password":"newpassword1"}`); c != http.StatusOK {
		t.Fatalf("change failed: %d", c)
	}
	if a.CompareCreds("admin", "oldpassword") || !a.CompareCreds("jacob", "newpassword1") {
		t.Fatal("new login not in effect")
	}
	// username only (keep password)
	if c := change(a, `{"current_password":"newpassword1","username":"jacob2"}`); c != http.StatusOK || !a.CompareCreds("jacob2", "newpassword1") {
		t.Fatalf("username-only change failed: %d", c)
	}
	// restart: file wins over the environment values
	b := NewAuthStore("admin", "oldpassword", sm)
	b.LoadFile(file)
	if b.CompareCreds("admin", "oldpassword") || !b.CompareCreds("jacob2", "newpassword1") {
		t.Fatal("stored login must override the environment after a restart")
	}
}

func TestEnableLoginFromOpenInstance(t *testing.T) {
	sm := NewSessionManager(NewInMemorySessionStore(), time.Hour, time.Hour, "s")
	a := NewAuthStore("", "", sm)
	a.LoadFile(filepath.Join(t.TempDir(), "ui-auth.json"))
	if !a.isDisabled() {
		t.Fatal("expected open instance")
	}
	if c := change(a, `{"username":"jacob"}`); c != http.StatusBadRequest {
		t.Fatalf("turning login on needs a password, got %d", c)
	}
	if c := change(a, `{"username":"jacob","new_password":"password123"}`); c != http.StatusOK || a.isDisabled() {
		t.Fatalf("enable failed: %d", c)
	}
	h := sm.Handle(a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("routes must be protected right after enabling login, got %d", rec.Code)
	}
}
