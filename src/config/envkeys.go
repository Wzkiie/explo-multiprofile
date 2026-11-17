package config

import "reflect"

// EnvKeys returns every environment variable name that Config reads (the `env` struct tags).
// Used to keep container-level env vars from leaking into per-profile child processes.
func EnvKeys() map[string]bool {
	keys := make(map[string]bool)
	collectEnvKeys(reflect.TypeOf(Config{}), keys)
	return keys
}

func collectEnvKeys(t reflect.Type, keys map[string]bool) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if k := f.Tag.Get("env"); k != "" {
			keys[k] = true
		}
		collectEnvKeys(f.Type, keys)
	}
}
