// Package paths resolves the directories turns uses without accessing them.
package paths

import (
	"errors"
	"fmt"
	"path/filepath"
)

const (
	// ConfigFile holds the configured space.
	ConfigFile = "config"
	// TokenFile holds the personal token.
	TokenFile = "token"
	// LastPushFile records the latest push.
	LastPushFile = "last-push"
	// LockFile serializes pushes.
	LockFile = "lock"
)

// Paths names the user's and host's configuration, state, and session paths.
type Paths struct {
	Home       string
	Config     string
	State      string
	HostConfig string
	UserUnits  string
	HostUnits  string
	Claude     string
	Codex      string
	Grok       string
	Jobs       string
}

// Resolve derives paths from the supplied environment and system root.
func Resolve(getenv func(key string) string, etc string) (Paths, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	home := getenv("HOME")
	if home == "" {
		return Paths{}, errors.New("HOME is not set")
	}
	if !filepath.IsAbs(home) {
		return Paths{}, fmt.Errorf("HOME '%s' is not an absolute path", home)
	}
	home = filepath.Clean(home)
	resolve := func(key, fallback string) string {
		value := getenv(key)
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		return filepath.Join(home, fallback)
	}
	config := resolve("XDG_CONFIG_HOME", ".config")
	p := Paths{
		Home:      home,
		Config:    filepath.Join(config, "turns"),
		State:     filepath.Join(resolve("XDG_STATE_HOME", ".local/state"), "turns"),
		UserUnits: filepath.Join(config, "systemd/user"),
		Claude:    resolve("CLAUDE_CONFIG_DIR", ".claude"),
		Codex:     resolve("CODEX_HOME", ".codex"),
		Grok:      resolve("GROK_HOME", ".grok"),
		Jobs:      filepath.Join(home, ".local/state/dispatch/jobs"),
	}
	if etc != "" {
		p.HostConfig = filepath.Join(etc, "xdg/turns/config")
		p.HostUnits = filepath.Join(etc, "systemd/user")
	}
	return p, nil
}
