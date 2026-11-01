package paths_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/paths"
)

func TestPublicShapes(t *testing.T) {
	// R-I8XM-BZ81
	type shape struct {
		Home, Config, State, HostConfig, UserUnits, HostUnits, Claude, Codex, Grok, Jobs string
	}
	p := paths.Paths(shape{"home", "config", "state", "host", "user-units", "host-units", "claude", "codex", "grok", "jobs"})
	if p.Home != "home" || p.Jobs != "jobs" {
		t.Fatal(p)
	}
	// R-IBDF-3IPF
	requireType[func(func(string) string, string) (paths.Paths, error)](paths.Resolve)
}

func requireType[T any](_ T) {}

func TestFileConstants(t *testing.T) {
	type filename string
	const (
		// R-ICLB-HAG4
		config filename = paths.ConfigFile
		// R-IDT7-V26T
		token filename = paths.TokenFile
		// R-IF14-8TXI
		lastPush filename = paths.LastPushFile
		// R-IG90-MLO7
		lock filename = paths.LockFile
	)
	if config != "config" || token != "token" || lastPush != "last-push" || lock != "lock" {
		t.Fatalf("unexpected file constants: %q, %q, %q, %q", config, token, lastPush, lock)
	}
}

func TestHomeErrors(t *testing.T) {
	// R-OPF4-LBRE R-OQN0-Z3I3 R-IIOT-E55L
	for _, tt := range []struct {
		name   string
		getenv func(string) string
		want   string
	}{
		{"unset", env(nil), "HOME is not set"},
		{"empty", env(map[string]string{"HOME": ""}), "HOME is not set"},
		{"nil", nil, "HOME is not set"},
		{"relative", env(map[string]string{"HOME": "home/me"}), "HOME 'home/me' is not an absolute path"},
		{"dot", env(map[string]string{"HOME": "."}), "HOME '.' is not an absolute path"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := paths.Resolve(tt.getenv, "")
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestHomeCleanAndNoCreation(t *testing.T) {
	// R-IJWP-RWWA
	for _, home := range []string{"/home/me/", t.TempDir() + "/missing/../person/"} {
		for _, etc := range []string{"", "relative/../etc", "/abs/../etc/"} {
			p, err := paths.Resolve(env(map[string]string{
				"HOME": home, "XDG_CONFIG_HOME": "relative", "XDG_STATE_HOME": "/uncreated/state/",
				"CLAUDE_CONFIG_DIR": "../claude", "CODEX_HOME": "/uncreated/codex", "GROK_HOME": "",
			}), etc)
			if err != nil || p.Home != filepath.Clean(home) {
				t.Fatalf("Resolve(%q, %q) = %v, %v", home, etc, p, err)
			}
		}
	}
	root := t.TempDir()
	for _, home := range []string{root, filepath.Join(root, "absent")} {
		if _, err := paths.Resolve(env(map[string]string{"HOME": home}), filepath.Join(root, "etc")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("Resolve created files: %v, %v", entries, err)
	}
}

func TestXDGConfig(t *testing.T) {
	// R-IL4M-5OMZ
	for _, tt := range []struct{ value, base string }{
		{"", "/home/me/.config"}, {"/x/cfg/", "/x/cfg"}, {"rel/cfg", "/home/me/.config"},
		{"/x/../cfg/./", "/cfg"},
	} {
		p, err := paths.Resolve(env(map[string]string{"HOME": "/home/me", "XDG_CONFIG_HOME": tt.value}), "")
		if err != nil || p.Config != filepath.Join(tt.base, "turns") || p.UserUnits != filepath.Join(tt.base, "systemd/user") {
			t.Fatalf("XDG_CONFIG_HOME=%q: %v, %v", tt.value, p, err)
		}
	}
}

func TestStateAndJobs(t *testing.T) {
	// R-IMCI-JGDO R-IQ07-ORLR
	for _, tt := range []struct{ value, state string }{
		{"", "/home/me/.local/state/turns"}, {"/x/state", "/x/state/turns"},
		{"rel/state", "/home/me/.local/state/turns"}, {"/x/../state/", "/state/turns"},
	} {
		p, err := paths.Resolve(env(map[string]string{"HOME": "/home/me", "XDG_STATE_HOME": tt.value}), "")
		if err != nil || p.State != tt.state || p.Jobs != "/home/me/.local/state/dispatch/jobs" {
			t.Fatalf("XDG_STATE_HOME=%q: %v, %v", tt.value, p, err)
		}
	}
}

func TestHostPaths(t *testing.T) {
	// R-INKE-X84D
	for _, tt := range []struct{ etc, config, units string }{
		{"", "", ""}, {"/etc", "/etc/xdg/turns/config", "/etc/systemd/user"},
		{"/root/../etc/", "/etc/xdg/turns/config", "/etc/systemd/user"},
		{"root/../etc/", "etc/xdg/turns/config", "etc/systemd/user"},
	} {
		p, err := paths.Resolve(env(map[string]string{"HOME": "/home/me"}), tt.etc)
		if err != nil || p.HostConfig != tt.config || p.HostUnits != tt.units {
			t.Fatalf("etc=%q: %v, %v", tt.etc, p, err)
		}
	}
}

func TestSessionRoots(t *testing.T) {
	// R-IOSB-AZV2
	for _, tt := range []struct{ value, claude, codex, grok string }{
		{"", "/home/me/.claude", "/home/me/.codex", "/home/me/.grok"},
		{"/sessions/", "/sessions", "/sessions", "/sessions"},
		{"rel/sessions", "/home/me/.claude", "/home/me/.codex", "/home/me/.grok"},
		{"/sessions/../clean/", "/clean", "/clean", "/clean"},
	} {
		values := map[string]string{"HOME": "/home/me"}
		if tt.value != "" {
			values["CLAUDE_CONFIG_DIR"], values["CODEX_HOME"], values["GROK_HOME"] = tt.value, tt.value, tt.value
		}
		p, err := paths.Resolve(env(values), "")
		if err != nil || p.Claude != tt.claude || p.Codex != tt.codex || p.Grok != tt.grok {
			t.Fatalf("roots=%q: %v, %v", tt.value, p, err)
		}
	}
	for _, key := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GROK_HOME"} {
		p, err := paths.Resolve(env(map[string]string{"HOME": "/home/me", key: ""}), "")
		if err != nil || p.Claude != "/home/me/.claude" || p.Codex != "/home/me/.codex" || p.Grok != "/home/me/.grok" {
			t.Fatalf("explicit empty %s: %v, %v", key, p, err)
		}
	}
}

func TestOnlyAllowedEnvironmentKeys(t *testing.T) {
	// R-IR84-2JCG
	var keys []string
	_, err := paths.Resolve(func(key string) string {
		keys = append(keys, key)
		if key == "HOME" {
			return "/home/me"
		}
		return ""
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		switch key {
		case "HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "GROK_HOME":
		default:
			t.Errorf("unexpected environment key %q", key)
		}
	}
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
