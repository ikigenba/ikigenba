package opsctl

import (
	"fmt"
	"os"
	"strings"
)

// currentEnv returns the process environment as a KEY=VALUE slice. Wrapped so the
// RealRunner can layer per-verb overrides on top of the inherited env.
func currentEnv() []string { return os.Environ() }

// LoadEnvFile layers a systemd-style KEY=VALUE environment file under the
// existing process environment.
func LoadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: malformed environment line %q", path, i+1, line)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
	}
	return nil
}
