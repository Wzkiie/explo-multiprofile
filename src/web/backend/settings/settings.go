package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"fmt"
	"os"
	"strings"

	"explo/src/web/backend/app"
)

// ConfigResponse is returned by GET /api/config.
type ConfigResponse struct {
	Values  map[string]string `json:"values"`
	Sources map[string]string `json:"sources"` // "env" | "file"
}

type Settings struct {
	cfg app.Config
}

func NewSettings(Config app.Config) *Settings {
	return &Settings{cfg: Config}
}

// parseEnvText parses key=value lines, ignoring comments, blanks and unquotes variables
func (s *Settings) ParseEnvText(text string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		if k = strings.TrimSpace(k); k != "" {
			v = strings.TrimSpace(v)

			// unquote if quoted
			if len(v) >= 2 {
				if (v[0] == '\'' && v[len(v)-1] == '\'') ||
					(v[0] == '"' && v[len(v)-1] == '"') {
					v = v[1 : len(v)-1]
				}
			}
			out[k] = v
		}
	}
	return out
}

// validateEnvText rejects content that isn't valid .env syntax before it's written to
// disk. cleanenv.ReadConfig fails hard on any non-blank, non-comment line without a "=",
// which causes a crash loop for the container with no way tofix since the UI becomes
// inaccessible.Mirrors ParseEnvText's line rules,but treats a missing "=" as an error
// instead of silently skipping the line.
func validateEnvText(text string) error {
	for i, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if !strings.Contains(t, "=") {
			return fmt.Errorf("line %d is not valid .env syntax (expected KEY=value): %q", i+1, t)
		}
	}
	return nil
}

// updateEnvKeys reads the env file (falling back to fallback if missing), updates the
// given key=value pairs in-place preserving comments, and writes the result back.
// envWriteMu serializes every read-modify-write of an .env file. The UI fires several saves at once
// (e.g. "Replace" for all playlists); without the lock they overwrite each other and can leave a half-written file.
var envWriteMu sync.Mutex

func (s *Settings) UpdateEnvKeys(updates map[string]string, fallback []byte) error {
	envWriteMu.Lock()
	defer envWriteMu.Unlock()
	path := s.cfg.WebEnvPath
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = fallback
	} else if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	touched := make(map[string]bool)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if val, ok := updates[key]; ok {
			if val == "" {
				lines[i] = "" // remove by blanking
			} else {
				lines[i] = key + "=" + formatEnvValue(val)
			}
			touched[key] = true
		}
	}

	// Append any keys that weren't already in the file
	for k, v := range updates {
		if !touched[k] && v != "" {
			lines = append(lines, k+"="+formatEnvValue(v))
		}
	}

	// Filter out consecutive blank lines left by removals
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, l := range lines {
		blank := strings.TrimSpace(l) == ""
		if blank && prevBlank {
			continue
		}
		out = append(out, l)
		prevBlank = blank
	}

	return writeEnvFile(path, []byte(strings.Join(out, "\n")+"\n"))
}

// writeEnvFile replaces the file atomically (temp file + rename) so a run or a restart never reads a
// half-written .env. A single file mounted into a container cannot be replaced by rename, so in that
// case it is overwritten in place (still under envWriteMu).
func writeEnvFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err == nil {
		if err := os.Rename(tmp, path); err == nil {
			return nil
		}
		_ = os.Remove(tmp)
	}
	return os.WriteFile(path, data, 0600)
}

// Check for special chars in env vars that might need quoting
func formatEnvValue(v string) string {
	// preserve already quoted values
	if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return v
	}

	if strings.ContainsAny(v, `"$#?'`) {
		// escape single quotes inside value
		v = strings.ReplaceAll(v, `'`, `'\''`)
		return fmt.Sprintf(`'%s'`, v)
	}

	return v
}

// envVersion identifies the content of an .env file (used to detect a stale editor snapshot).
func envVersion(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
