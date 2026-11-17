package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedAdminAppliesToNamedAndDefault(t *testing.T) {
	root := t.TempDir()
	named := filepath.Join(root, "profiles", "anna")
	if err := os.MkdirAll(named, 0755); err != nil {
		t.Fatal(err)
	}
	if SharedEnvPath(root) != SharedEnvPath(named) {
		t.Fatalf("default and named profile must share one file: %s vs %s", SharedEnvPath(root), SharedEnvPath(named))
	}
	if err := WriteSharedEnv(root, map[string]string{"ADMIN_SYSTEM_USERNAME": "root", "ADMIN_SYSTEM_PASSWORD": "pw", "BOGUS": "x"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(SharedEnvPath(root)); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("shared.env must be 0600: %v %v", fi, err)
	}
	got := ReadSharedEnv(named)
	if got["ADMIN_SYSTEM_USERNAME"] != "root" || got["ADMIN_SYSTEM_PASSWORD"] != "pw" || len(got) != 2 {
		t.Fatalf("unexpected shared values: %v", got)
	}

	// profile without admin gets the shared one
	var cfg Config
	cfg.ServerCfg.WebDataDir = named + "/"
	cfg.applySharedAdmin()
	if cfg.ClientCfg.AdminCreds.User != "root" || cfg.ClientCfg.AdminCreds.Password != "pw" {
		t.Fatalf("shared admin not applied: %+v", cfg.ClientCfg.AdminCreds)
	}
	// a profile with its own admin keeps it
	var own Config
	own.ServerCfg.WebDataDir = root + "/"
	own.ClientCfg.AdminCreds.User = "mine"
	own.applySharedAdmin()
	if own.ClientCfg.AdminCreds.User != "mine" || own.ClientCfg.AdminCreds.Password != "" {
		t.Fatalf("own admin must win: %+v", own.ClientCfg.AdminCreds)
	}
	// clearing removes it
	if err := WriteSharedEnv(root, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if len(ReadSharedEnv(root)) != 0 {
		t.Fatal("shared admin not cleared")
	}
}
