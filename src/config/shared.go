package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SharedKeys are the settings that live once in shared.env and apply to every profile
// that has no value of its own: one admin account for the library scan.
var SharedKeys = []string{"ADMIN_SYSTEM_USERNAME", "ADMIN_SYSTEM_PASSWORD", "ADMIN_API_KEY"}

// SharedEnvPath returns the shared.env path for a profile data dir. Named profiles live in
// <root>/profiles/<name>/, the default profile directly in <root>/, and both use <root>/shared.env.
func SharedEnvPath(webDataDir string) string {
	dir := filepath.Clean(webDataDir)
	if parent := filepath.Dir(dir); filepath.Base(parent) == "profiles" {
		dir = filepath.Dir(parent)
	}
	return filepath.Join(dir, "shared.env")
}

// ReadSharedEnv returns the values stored in shared.env (empty map if the file does not exist).
func ReadSharedEnv(webDataDir string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(SharedEnvPath(webDataDir))
	if err != nil {
		return out
	}
	allowed := map[string]bool{}
	for _, k := range SharedKeys {
		allowed[k] = true
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !allowed[k] {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// WriteSharedEnv replaces shared.env with the given values (only SharedKeys, empty values are dropped).
func WriteSharedEnv(webDataDir string, values map[string]string) error {
	var keys []string
	for _, k := range SharedKeys {
		v := strings.TrimSpace(values[k])
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("line breaks are not allowed in %s", k)
		}
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Shared by all profiles (admin account for the library scan). Edit in the web UI.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, strings.TrimSpace(values[k]))
	}
	p := SharedEnvPath(webDataDir)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// applySharedAdmin fills the admin account from shared.env where the profile has none of its own.
func (cfg *Config) applySharedAdmin() {
	sh := ReadSharedEnv(cfg.ServerCfg.WebDataDir)
	if len(sh) == 0 {
		return
	}
	a := &cfg.ClientCfg.AdminCreds
	if a.User == "" && a.Password == "" && a.APIKey == "" {
		a.User, a.Password, a.APIKey = sh["ADMIN_SYSTEM_USERNAME"], sh["ADMIN_SYSTEM_PASSWORD"], sh["ADMIN_API_KEY"]
	}
}
