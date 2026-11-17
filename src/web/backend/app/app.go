package app

import (
	"os"
	"strings"

	"explo/src/config"
)

// Config describes one profile (= one user): its own .env and its own data dir.
type Config struct {
	Profile    string // profile name ("default" for the legacy single-user setup)
	Named      bool   // true for profiles under profiles/<name>/, false for the legacy default
	WebEnvPath string
	WebDataDir string
	ExploPath  string
}

// ChildEnv builds the environment for an explo child process of this profile.
// The default profile keeps the container env untouched (legacy behaviour: env wins over .env).
// Named profiles must not inherit config values from the container env, otherwise
// every profile would end up with the same credentials/paths, so every variable that
// Config reads is dropped and WEB_DATA_PATH is pinned to the profile directory.
func (c Config) ChildEnv() []string {
	var skip map[string]bool
	if c.Named {
		skip = config.EnvKeys()
	}
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if key == "WEB_UI" || skip[key] {
			continue
		}
		env = append(env, e)
	}
	if c.Named {
		// trailing slash matters: CoversDir is derived via filepath.Dir(WebDataDir)
		env = append(env, "WEB_DATA_PATH="+strings.TrimRight(c.WebDataDir, "/")+"/")
	}
	return env
}
