//go:build linux

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/session"
)

// R-Z3AT-GWJQ
func TestStatusPathRefusalDoesNotReadSessions(t *testing.T) {
	for _, home := range []string{"", "relative\nvalue"} {
		t.Run(strconvStatusName(home), func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			p := statusPaths(t, deps)
			statusSession(t, p, session.ToolClaude, "readable", 1)
			path := filepath.Join(p.Claude, "projects", "work", "readable.jsonl")
			sessionRoot, err := os.OpenRoot(p.Claude)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if closeErr := sessionRoot.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			})
			getenv := deps.Getenv
			deps.Getenv = func(key string) string {
				if key == "HOME" {
					return home
				}
				if key == "CLAUDE_CONFIG_DIR" {
					return p.Claude
				}
				return getenv(key)
			}
			watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if closeErr := syscall.Close(watch); closeErr != nil {
					t.Error(closeErr)
				}
			})
			if _, err = syscall.InotifyAddWatch(watch, path, syscall.IN_OPEN|syscall.IN_ACCESS); err != nil {
				t.Fatal(err)
			}
			// Prove the observer sees this file being read, then drain its events.
			if _, err = sessionRoot.ReadFile("projects/work/readable.jsonl"); err != nil {
				t.Fatal(err)
			}
			var events [4096]byte
			if n, readErr := syscall.Read(watch, events[:]); readErr != nil || n <= 0 {
				t.Fatalf("read observation failed: bytes=%d error=%v", n, readErr)
			}
			if n, readErr := syscall.Read(watch, events[:]); n > 0 || !errors.Is(readErr, syscall.EAGAIN) {
				t.Fatalf("observer did not drain: bytes=%d error=%v", n, readErr)
			}
			code, out, errText := statusRun(t, deps)
			// Check before any other operation can read the fixture again.
			if n, readErr := syscall.Read(watch, events[:]); n > 0 || !errors.Is(readErr, syscall.EAGAIN) {
				t.Fatalf("status read a session before refusing paths: bytes=%d error=%v", n, readErr)
			}
			wantErr := "turns: HOME is not set\n"
			if home != "" {
				wantErr = "turns: HOME 'relative\\nvalue' is not an absolute path\n"
			}
			if code != ExitFailed || out != "" || errText != wantErr {
				t.Fatalf("%d %q %q; want stderr %q", code, out, errText, wantErr)
			}
		})
	}
}
