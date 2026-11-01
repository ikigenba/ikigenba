package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/turns"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func installPaths(t testing.TB, deps seam.Deps) paths.Paths {
	t.Helper()
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func installExpected(t testing.TB, exe string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, name := range []string{turns.ServiceUnit, turns.TimerUnit} {
		data, err := fs.ReadFile(turns.Units(), name)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		if name == turns.ServiceUnit {
			for i, line := range lines {
				if strings.HasPrefix(line, "ExecStart=") {
					lines[i] = "ExecStart=" + exe + " push"
				}
			}
		}
		result[name] = strings.Join(lines, "\n")
	}
	return result
}
func installAssertUnits(t testing.TB, p paths.Paths, exe string) {
	t.Helper()
	root, err := os.OpenRoot(p.UserUnits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for name, want := range installExpected(t, exe) {
		path := filepath.Join(p.UserUnits, name)
		data, err := root.ReadFile(name)
		if err != nil || string(data) != want {
			t.Fatalf("unit %s = %q, %v; want %q", name, data, err, want)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
			t.Fatalf("unit mode %s: %v %v", path, info, err)
		}
	}
}
func installSuccess(t testing.TB, deps seam.Deps) {
	t.Helper()
	code, out, errText := runCLI(t, deps, []string{"install"}, nil)
	if code != ExitOK || out != "" || errText != "" {
		t.Fatalf("install: %d %q %q", code, out, errText)
	}
}
func installRecorder(calls *[]seam.Cmd) seam.Runner {
	return func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		*calls = append(*calls, cmd)
		return seam.Result{}, nil
	}
}

// R-7NA6-G3RR R-7OI2-TVIG
func TestInstallGrammar(t *testing.T) {
	const help = `Usage: turns install

Write turns.service and turns.timer under ~/.config/systemd/user/, reload the
user manager, and enable and start the timer, which runs 'turns push' every
five minutes. On a host whose /etc/systemd/user/ already holds the units, only
enable and start the timer. Safe to run again.

Options:
  -h, --help   print this help
`
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--force", "--help"}, {"now", "-h"}, {"--force"}, {"now"}} {
		deps, root := syntheticDeps(t)
		before := snapshotTree(t, root)
		input := &recordingReader{}
		code, out, errText := runCLI(t, deps, append([]string{"install"}, args...), input)
		if args[len(args)-1] == "--help" || args[len(args)-1] == "-h" {
			if code != ExitOK || out != help || errText != "" {
				t.Fatalf("%d %q %q", code, out, errText)
			}
		} else {
			summary := "install takes no arguments"
			if args[0] == "--force" {
				summary = "unknown option '--force'"
			}
			if code != ExitUsage || out != "" || errText != "turns: "+summary+"\n\nsee 'turns install --help' for usage\n" {
				t.Fatalf("%d %q %q", code, out, errText)
			}
		}
		if input.Reads != 0 {
			t.Fatal("read stdin")
		}
		assertTreeEqual(t, before, snapshotTree(t, root))
	}
}

// R-JSJT-BI22 R-7QXV-LEZU R-SBE7-A6J7 R-SCM3-NY9W R-SDU0-1Q0L R-JW7I-GTA5 R-JXFE-UL0U R-84CR-SW5H
func TestInstallLaptopAndRepair(t *testing.T) {
	for _, kind := range []string{"new", "private-config", "repair", "umask"} {
		t.Run(kind, func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			getenv := deps.Getenv
			deps.Getenv = func(key string) string {
				if key == "XDG_CONFIG_HOME" {
					return ""
				}
				return getenv(key)
			}
			p := installPaths(t, deps)
			if kind == "private-config" {
				if err := os.Mkdir(filepath.Join(p.Home, ".config"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "repair" {
				writeFixture(t, filepath.Join(p.UserUnits, turns.ServiceUnit), "ExecStart="+turns.HostBinary+" push\n", 0600)
				writeFixture(t, filepath.Join(p.UserUnits, turns.TimerUnit), "old arbitrary timer", 0600)
			}
			if kind == "umask" {
				old := syscall.Umask(0077)
				defer syscall.Umask(old)
			}
			var calls []seam.Cmd
			deps.Exec = func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
				installAssertUnits(t, p, deps.Arg0)
				calls = append(calls, cmd)
				return seam.Result{}, nil
			}
			installSuccess(t, deps)
			installAssertUnits(t, p, deps.Arg0)
			want := []seam.Cmd{{Path: "systemctl", Args: []string{"--user", "daemon-reload"}, Dir: deps.Dir}, {Path: "systemctl", Args: []string{"--user", "enable", "--now", turns.TimerUnit}, Dir: deps.Dir}}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("commands %#v", calls)
			}
			for _, path := range []string{filepath.Join(p.Home, ".config"), filepath.Join(p.Home, ".config/systemd"), p.UserUnits} {
				info, err := os.Stat(path)
				mode := fs.FileMode(0755)
				if kind == "private-config" && path == filepath.Join(p.Home, ".config") {
					mode = 0700
				}
				if kind == "repair" {
					mode = 0700
				}
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("directory %s: %v %v; want %o", path, info, err, mode)
				}
			}
		})
	}
}

// R-JMGB-ENCL R-JNO7-SF3A
func TestInstallExecutablePaths(t *testing.T) {
	for _, kind := range []string{"relative", "clean", "missing", "absolute-link", "relative-link", "search", "relative-search", "absolute-search-link", "relative-search-link", "empty-search-entry", "utf8"} {
		t.Run(kind, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			p := installPaths(t, deps)
			bin := filepath.Join(root, "bin")
			target := filepath.Join(root, "target", "program")
			writeFixture(t, target, "fixture", 0755)
			deps.Dir = bin
			deps.Arg0 = "./turns"
			want := filepath.Join(bin, "turns")
			switch kind {
			case "clean":
				deps.Arg0 = root + "/x/../bin/turns"
			case "missing":
				deps.Arg0 = filepath.Join(root, "absent", "turns")
				want = deps.Arg0
			case "absolute-link", "relative-link":
				writeFixture(t, filepath.Join(bin, "placeholder"), "", 0600)
				if err := os.Symlink(target, want); err != nil {
					t.Fatal(err)
				}
				if kind == "absolute-link" {
					deps.Arg0 = want
				}
			case "utf8":
				deps.Arg0 = filepath.Join(root, "文字", "turns")
				want = deps.Arg0
			case "search", "relative-search", "absolute-search-link", "relative-search-link", "empty-search-entry":
				deps.Arg0 = "turns"
				deps.Dir = root
				pathValue := "bin"
				switch {
				case kind == "search":
					missing := filepath.Join(root, "missing")
					dir := filepath.Join(root, "directory")
					inert := filepath.Join(root, "inert")
					if err := os.MkdirAll(filepath.Join(dir, "turns"), 0700); err != nil {
						t.Fatal(err)
					}
					writeFixture(t, filepath.Join(inert, "turns"), "no", 0644)
					pathValue = strings.Join([]string{missing, dir, inert, bin, filepath.Join(root, "later")}, ":")
					writeFixture(t, want, "executable", 0100)
					writeFixture(t, filepath.Join(root, "later", "turns"), "later", 0755)
				case strings.HasSuffix(kind, "link"):
					writeFixture(t, filepath.Join(bin, "placeholder"), "", 0600)
					if err := os.Symlink(target, want); err != nil {
						t.Fatal(err)
					}
					if kind == "absolute-search-link" {
						pathValue = bin
					}
				case kind == "empty-search-entry":
					pathValue = ":" + filepath.Join(root, "later")
					want = filepath.Join(root, "turns")
					writeFixture(t, want, "executable", 0755)
				default:
					writeFixture(t, want, "executable", 0755)
				}
				getenv := deps.Getenv
				deps.Getenv = func(key string) string {
					if key == "PATH" {
						return pathValue
					}
					return getenv(key)
				}
			}
			var calls []seam.Cmd
			deps.Exec = installRecorder(&calls)
			installSuccess(t, deps)
			installAssertUnits(t, p, want)
		})
	}
}

// R-E28N-SMHC R-JTRP-P9SR R-BYZ4-9W2S R-C070-NNTH R-QZOJ-JKFX
func TestInstallEarlyRefusals(t *testing.T) {
	for _, kind := range []string{"empty", "missing", "empty-path", "relative-path", "relative-search", "home", "relative-home", "control-home", "space", "percent", "dollar", "double-quote", "single-quote", "backslash", "control", "delete", "invalid-utf8"} {
		t.Run(kind, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			deps.Arg0 = ""
			want := "turns: cannot find the turns that is running\n\nrun 'turns install' by its full path\n"
			getenv := deps.Getenv
			pathValue := ""
			switch kind {
			case "missing":
				deps.Arg0 = "turns"
				pathValue = filepath.Join(root, "missing")
			case "empty-path":
				deps.Arg0 = "turns"
			case "relative-path":
				deps.Arg0 = "./turns"
				deps.Dir = ""
			case "relative-search":
				deps.Arg0 = "turns"
				deps.Dir = ""
				pathValue = "bin"
			case "home", "relative-home", "control-home":
				deps.Getenv = func(key string) string {
					if key == "HOME" {
						if kind == "control-home" {
							return "relative\nvalue"
						}
						if kind == "relative-home" {
							return "relative"
						}
						return ""
					}
					return getenv(key)
				}
				_, err := paths.Resolve(deps.Getenv, deps.Etc)
				want = "turns: " + strings.ReplaceAll(err.Error(), "\n", `\n`) + "\n"
			default:
				if kind != "empty" {
					suffix := map[string]string{"space": "a b", "percent": "a%b", "dollar": "a$b", "double-quote": "a\"b", "single-quote": "a'b", "backslash": "a\\b", "control": "a\nb", "delete": "a\x7fb", "invalid-utf8": "a\xffb"}[kind]
					deps.Arg0 = filepath.Join(root, suffix, "turns")
					want = "turns: systemd cannot run turns from " + strconv.Quote(deps.Arg0) + "\n\nmove turns to a path that holds no whitespace, quote, backslash, '%', '$', control character or invalid UTF-8, and run install again\n"
				}
			}
			if kind != "home" && kind != "relative-home" && kind != "control-home" {
				deps.Getenv = func(key string) string {
					if key == "PATH" {
						return pathValue
					}
					return getenv(key)
				}
			}
			before := snapshotTree(t, root)
			code, out, errText := runCLI(t, deps, []string{"install"}, nil)
			if code != ExitFailed || out != "" || errText != want {
				t.Fatalf("%d %q %q; want %q", code, out, errText, want)
			}
			assertTreeEqual(t, before, snapshotTree(t, root))
		})
	}
}

// R-80P2-NKXE R-SG9S-T9HZ R-84CR-SW5H
func TestInstallHostDetection(t *testing.T) {
	for _, kind := range []string{"host-file", "host-directory", "empty-etc", "timer-absent", "no-search"} {
		t.Run(kind, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			if kind == "empty-etc" {
				deps.Etc = ""
			}
			p := installPaths(t, deps)
			host := strings.HasPrefix(kind, "host-")
			switch kind {
			case "host-file":
				writeFixture(t, filepath.Join(p.HostUnits, turns.TimerUnit), "host unit", 0644)
			case "host-directory":
				if err := os.MkdirAll(filepath.Join(p.HostUnits, turns.TimerUnit), 0700); err != nil {
					t.Fatal(err)
				}
			case "timer-absent":
				writeFixture(t, filepath.Join(p.HostUnits, turns.ServiceUnit), "only service", 0644)
			case "no-search":
				writeFixture(t, filepath.Join(p.HostUnits, turns.TimerUnit), "inaccessible", 0644)
				parent, err := os.OpenRoot(filepath.Dir(p.HostUnits))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = parent.Close() }()
				if err = parent.Chmod(filepath.Base(p.HostUnits), 0600); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = parent.Chmod(filepath.Base(p.HostUnits), 0700) }()
			}
			var before map[string]treeEntry
			if host {
				before = snapshotTree(t, root)
			}
			if host {
				deps.Arg0 = ""
			}
			var calls []seam.Cmd
			deps.Exec = installRecorder(&calls)
			installSuccess(t, deps)
			enable := seam.Cmd{Path: "systemctl", Args: []string{"--user", "enable", "--now", turns.TimerUnit}, Dir: deps.Dir}
			if host {
				if !reflect.DeepEqual(calls, []seam.Cmd{enable}) {
					t.Fatalf("host calls %#v", calls)
				}
				assertTreeEqual(t, before, snapshotTree(t, root))
				if _, err := os.Stat(p.UserUnits); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("created user units: %v", err)
				}
			} else {
				installAssertUnits(t, p, deps.Arg0)
				if len(calls) != 2 || !reflect.DeepEqual(calls[1], enable) {
					t.Fatalf("laptop calls %#v", calls)
				}
			}
		})
	}
}

// R-E3GK-6E81 R-E4OG-K5YQ R-E5WC-XXPF R-BVBF-4KUP R-SL5E-CCGR R-JYNB-8CRJ
func TestInstallSystemctlFailures(t *testing.T) {
	for _, host := range []bool{false, true} {
		for _, failureAt := range []int{1, 2} {
			if host && failureAt == 2 {
				continue
			}
			for _, kind := range []string{"stderr", "stdout", "newline-stderr", "empty", "newlines", "exec"} {
				t.Run(fmt.Sprintf("%t-%d-%s", host, failureAt, kind), func(t *testing.T) {
					deps, _ := syntheticDeps(t)
					p := installPaths(t, deps)
					if host {
						writeFixture(t, filepath.Join(p.HostUnits, turns.TimerUnit), "host", 0644)
					}
					calls := 0
					want := "turns: could not enable turns.timer\n"
					result := seam.Result{ExitCode: 7}
					var execErr error
					switch kind {
					case "stderr":
						result.Stderr = []byte("Failed to connect to bus: No medium found\n")
						result.Stdout = []byte("ignored-stdout")
						want += "\n> Failed to connect to bus: No medium found\n"
					case "stdout", "newline-stderr":
						result.Stdout = []byte("fallback\nsecond\n")
						if kind == "newline-stderr" {
							result.Stderr = []byte("\n\n")
						}
						want += "\n> fallback\n> second\n"
					case "newlines":
						result.Stdout = []byte("\n")
						result.Stderr = []byte("\n\n")
					case "exec":
						execErr = errors.New("a\nb")
						want += "\na\\nb\n"
					}
					deps.Exec = func(_ context.Context, _ seam.Cmd) (seam.Result, error) {
						calls++
						if calls == failureAt {
							return result, execErr
						}
						return seam.Result{}, nil
					}
					code, out, errText := runCLI(t, deps, []string{"install"}, nil)
					if code != ExitFailed || out != "" || errText != want || calls != failureAt {
						t.Fatalf("%d %q %q calls=%d; want %q", code, out, errText, calls, want)
					}
					if !host {
						installAssertUnits(t, p, deps.Arg0)
					}
				})
			}
		}
	}
}

// R-BWJB-ICLE R-BXR7-W4C3: independently exercise failures before any process starts.
func TestInstallWriteFailures(t *testing.T) {
	for _, kind := range []string{"parent", turns.ServiceUnit, turns.TimerUnit} {
		t.Run(kind, func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			p := installPaths(t, deps)
			path := p.UserUnits
			action := "create"
			if kind == "parent" {
				writeFixture(t, filepath.Dir(p.UserUnits), "blocking parent", 0600)
			} else {
				path = filepath.Join(p.UserUnits, kind)
				writeFixture(t, filepath.Join(path, "child"), "keep", 0600)
				action = "write"
			}
			code, out, errText := runCLI(t, deps, []string{"install"}, nil)
			prefix := fmt.Sprintf("turns: could not %s '%s'\n\n", action, path)
			if code != ExitFailed || out != "" || !strings.HasPrefix(errText, prefix) {
				t.Fatalf("%d %q %q", code, out, errText)
			}
			detail := strings.TrimPrefix(errText, prefix)
			lines := strings.Split(strings.TrimSuffix(detail, "\n"), "\n")
			if len(lines) != 1 || lines[0] == "" || strings.HasPrefix(lines[0], "> ") {
				t.Fatalf("bad detail %q", detail)
			}
		})
	}
}

// R-RSFO-4OGI R-RTNK-IG77
func TestInstallOnlyChangesUnits(t *testing.T) {
	for _, kind := range []string{"laptop", "laptop-fail", "laptop-write-fail", "host", "host-fail", "absent", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			p := installPaths(t, deps)
			if kind != "absent" {
				mode := fs.FileMode(0600)
				if kind == "unreadable" {
					mode = 0
				}
				writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=test.example\n", mode)
				writeFixture(t, filepath.Join(p.Config, paths.TokenFile), "untouched-token", mode)
				writeFixture(t, filepath.Join(p.State, paths.LastPushFile), "prior moment", 0600)
				writeFixture(t, filepath.Join(p.Claude, "session"), "never read", 0600)
			}
			host := strings.HasPrefix(kind, "host")
			if host {
				writeFixture(t, filepath.Join(p.HostUnits, turns.TimerUnit), "host", 0644)
			}
			if kind == "laptop-write-fail" {
				writeFixture(t, filepath.Join(p.UserUnits, turns.TimerUnit, "child"), "keep", 0600)
			}
			// Exclude only the exact permitted files and directory ancestors.
			filtered := func(tree map[string]treeEntry) map[string]treeEntry {
				if host {
					return tree
				}
				for _, path := range []string{filepath.Join(p.UserUnits, turns.ServiceUnit), filepath.Join(p.UserUnits, turns.TimerUnit)} {
					delete(tree, strings.TrimPrefix(path, root))
				}
				for path := p.UserUnits; path != root && strings.HasPrefix(path, root+string(filepath.Separator)); path = filepath.Dir(path) {
					delete(tree, strings.TrimPrefix(path, root))
				}
				return tree
			}
			before := filtered(snapshotTree(t, root))
			calls := 0
			deps.Exec = func(_ context.Context, _ seam.Cmd) (seam.Result, error) {
				calls++
				if strings.HasSuffix(kind, "-fail") && (host || calls == 2) {
					return seam.Result{ExitCode: 1}, nil
				}
				return seam.Result{}, nil
			}
			code, out, errText := runCLI(t, deps, []string{"install"}, nil)
			expected := ExitOK
			if strings.HasSuffix(kind, "-fail") {
				expected = ExitFailed
			}
			if code != expected || out != "" || (expected == ExitOK && errText != "") {
				t.Fatalf("%d %q %q", code, out, errText)
			}
			if kind == "laptop-write-fail" && calls != 0 {
				t.Fatal("process after write failure")
			}
			if (kind == "absent" || kind == "unreadable") && calls != 2 {
				t.Fatal("configuration affected install")
			}
			assertTreeEqual(t, before, filtered(snapshotTree(t, root)))
			if kind == "absent" {
				if _, err := os.Stat(p.State); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("state created")
				}
			}
		})
	}
}
