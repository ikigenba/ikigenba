package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-QF57-E042 R-CO8E-1KCH R-QHL0-5JLG R-QISW-JBC5
func TestTokenCandidateValidation(t *testing.T) {
	valid := []string{"ikp_a", "ikp_a\n", "ikp_a\r\n", "ikp_!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"}
	for _, in := range valid {
		t.Run(in, func(t *testing.T) {
			f := newReportFixture(t, "wip", nil, 7400)
			f.expect(f.run(context.Background(), []string{"token", "set"}, strings.NewReader(in)), 0, "", "")
			got, err := os.ReadFile(filepath.Join(f.data, "token"))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.TrimSuffix(strings.TrimSuffix(in, "\n"), "\r")
			if string(got) != want {
				t.Fatalf("stored %q want %q", got, want)
			}
		})
	}
	for _, in := range []string{"", "\n", "\r\n"} {
		t.Run("empty"+in, func(t *testing.T) {
			f := newReportFixture(t, "wip", nil, 7400)
			f.expect(f.run(context.Background(), []string{"token", "set"}, strings.NewReader(in)), 2, "", "sandbox: no token on stdin\n")
		})
	}
	invalid := []string{"ikp_a\n\n", "ikp_a\r\n\r\n", "ikp_a\r", "hunter2\n", "ikp_", "IKP_a", " ikp_a", "ikp_a b", "ikp_a\tb", "ikp_a\nikp_b", "ikp_a\xc3\xa9", "\n\n", "ikp_\x00", "ikp_\x7f"}
	for _, in := range invalid {
		t.Run(in, func(t *testing.T) {
			f := newReportFixture(t, "wip", nil, 7400)
			f.expect(f.run(context.Background(), []string{"token", "set"}, strings.NewReader(in)), 2, "", "sandbox: stdin does not hold a bearer token\n\na bearer token is one line beginning 'ikp_'\n")
		})
	}
}

// R-QK0S-X32U R-CWS9-P170 R-QMGL-OMK8 R-QNOI-2EAX R-CRW3-6VKK R-CY06-2SXP
func TestTokenAtomicReplacement(t *testing.T) {
	for _, mode := range []os.FileMode{0, 0644, 0400} {
		t.Run(mode.String(), func(t *testing.T) {
			f := newReportFixture(t, "wip", []string{"dummy"}, 7400)
			root, openErr := os.OpenRoot(f.root)
			if openErr != nil {
				t.Fatal(openErr)
			}
			t.Cleanup(func() {
				if closeErr := root.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			})
			registry := filepath.Join(f.root, "registry.json")
			beforeInfo, err := os.Stat(registry)
			if err != nil {
				t.Fatal(err)
			}
			before, err := root.ReadFile("registry.json")
			if err != nil {
				t.Fatal(err)
			}
			if mode != 0 {
				f.writeToken("ikp_old-long-token", mode)
				if err = os.Link(filepath.Join(f.data, "token"), filepath.Join(f.root, "earlier-token")); err != nil {
					t.Fatal(err)
				}
			}
			f.expect(f.run(context.Background(), []string{"token", "set"}, strings.NewReader("ikp_new")), 0, "", "")
			path := filepath.Join(f.data, "token")
			got, err := root.ReadFile(filepath.Join(f.name, "token"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "ikp_new" {
				t.Fatalf("stored %q", got)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("mode %o", info.Mode().Perm())
			}
			if mode != 0 {
				old, err := os.ReadFile(filepath.Join(f.root, "earlier-token"))
				if err != nil {
					t.Fatal(err)
				}
				if string(old) != "ikp_old-long-token" {
					t.Fatalf("hardlink changed %q", old)
				}
			}
			afterInfo, err := os.Stat(registry)
			if err != nil {
				t.Fatal(err)
			}
			after, err := root.ReadFile("registry.json")
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(beforeInfo, afterInfo) || !bytes.Equal(before, after) {
				t.Fatal("registry changed")
			}
			if len(f.execs) != 1 || f.execs[0].Path != "git" || len(f.streams) != 0 {
				t.Fatalf("unexpected runs %v %v", f.execs, f.streams)
			}
		})
	}
}

// R-CPGA-FC36 R-CQO6-T3TV
func TestTokenRefusalsPreserveFiles(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, in := range []string{"", "hunter2"} {
			suffix := "absent"
			if existing {
				suffix = "stored"
			}
			t.Run(in+suffix, func(t *testing.T) {
				f := newReportFixture(t, "wip", nil, 7400)
				root, openErr := os.OpenRoot(f.root)
				if openErr != nil {
					t.Fatal(openErr)
				}
				t.Cleanup(func() {
					if closeErr := root.Close(); closeErr != nil {
						t.Error(closeErr)
					}
				})
				if existing {
					f.writeToken("ikp_original", 0600)
				}
				code := f.run(context.Background(), []string{"token", "set"}, strings.NewReader(in))
				if code != 2 {
					t.Fatalf("code %d", code)
				}
				value, err := root.ReadFile(filepath.Join(f.name, "token"))
				if existing {
					if err != nil || string(value) != "ikp_original" {
						t.Fatalf("prior token %q %v", value, err)
					}
				} else {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("new token %q %v", value, err)
					}
					if _, err = os.Stat(f.data); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("data created %v", err)
					}
				}
				entries, err := os.ReadDir(f.root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					allowed := entry.Name() == "registry.json" || entry.Name() == "wip.lock"
					if existing && entry.Name() == "wip" {
						allowed = true
					}
					if !allowed {
						t.Fatalf("unexpected created entry %s", entry.Name())
					}
				}
			})
		}
	}
}

type tokenErrorReader struct{ done bool }

func (r *tokenErrorReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, "ikp_a"), nil
	}
	return 0, errors.New("boom")
}

type tokenForbiddenReader struct{ t *testing.T }

func (r tokenForbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("stdin was read")
	return 0, io.EOF
}

// R-QRC7-7PJ0
func TestTokenReadError(t *testing.T) {
	f := newReportFixture(t, "wip", nil, 7400)
	f.writeToken("ikp_original", 0600)
	f.expect(f.run(context.Background(), []string{"token", "set"}, &tokenErrorReader{}), 1, "", "sandbox: stdin: boom\n")
	value, err := os.ReadFile(filepath.Join(f.data, "token"))
	if err != nil || string(value) != "ikp_original" {
		t.Fatalf("token %q %v", value, err)
	}
}

// R-LQO9-TAPA
func TestTokenPreconditionRefusalsDoNotRead(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*reportFixture)
		want   string
	}{
		{"directory", func(f *reportFixture) { f.deps.Dir = "" }, "current directory"},
		{"checkout", func(f *reportFixture) {
			f.deps.Exec = func(context.Context, seam.Cmd) (seam.Result, error) { return seam.Result{ExitCode: 128}, nil }
		}, "git checkout"},
		{"control", func(f *reportFixture) { f.worktree = "/tmp/a\nb" }, "control character"},
		{"utf8", func(f *reportFixture) { f.worktree = "/tmp/a\xffb" }, "valid UTF-8"},
		{"name", func(f *reportFixture) { f.worktree = "/tmp/---" }, "usable sandbox name"},
		{"home", func(f *reportFixture) { f.deps.Getenv = func(string) string { return "relative" } }, "HOME is not an absolute path"},
		{"root", func(f *reportFixture) {
			f.deps.Getenv = func(key string) string {
				if key == "XDG_STATE_HOME" {
					return "/tmp/a\nb"
				}
				return "/tmp"
			}
		}, "state directory"},
		{"unknown", func(f *reportFixture) {
			if err := os.Remove(filepath.Join(f.root, "registry.json")); err != nil {
				f.t.Fatal(err)
			}
		}, "no sandbox"},
		{"ownership", func(f *reportFixture) { f.worktree = filepath.Join(f.worktree, "else", "wip") }, "another worktree"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReportFixture(t, "wip", nil, 7400)
			tc.modify(f)
			code := f.run(context.Background(), []string{"token", "set"}, tokenForbiddenReader{t})
			if code != 2 || f.out.Len() != 0 || !strings.Contains(f.err.String(), tc.want) {
				t.Fatalf("code %d stdout %q stderr %q", code, f.out.String(), f.err.String())
			}
		})
	}
}

// R-QXFP-4K8H R-QZVH-W3PV R-R3J7-1EXY R-CRW3-6VKK
func TestTokenReadAndAbsent(t *testing.T) {
	for _, tc := range []struct {
		name string
		port int
		apps []string
	}{{"wip", 7400, []string{"auth"}}, {"feature-x", 7412, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReportFixture(t, tc.name, tc.apps, tc.port)
			want := "sandbox: no token stored for sandbox '" + tc.name + "'\n\nAsk the human to sign in at http://auth." + tc.name + ".localhost:"
			if tc.port == 7400 {
				want += "7400"
			} else {
				want += "7412"
			}
			want += ", create a bearer\ntoken, and store it with: printf '%s' '<token>' | sandbox token set\n"
			f.expect(f.run(context.Background(), []string{"token"}, tokenForbiddenReader{t}), 2, "", want)
			if len(f.execs) != 1 || f.execs[0].Path != "git" || len(f.streams) != 0 {
				t.Fatalf("calls %v %v", f.execs, f.streams)
			}
			f.writeToken("ikp_!abc~", 0600)
			f.expect(f.run(context.Background(), []string{"token"}, tokenForbiddenReader{t}), 0, "ikp_!abc~\n", "")
			if len(f.execs) != 1 || f.execs[0].Path != "git" || len(f.streams) != 0 {
				t.Fatalf("calls %v %v", f.execs, f.streams)
			}
		})
	}
}

// R-AJ23-9HD8
func TestTokenInvalidStoredContent(t *testing.T) {
	for _, value := range []string{"", "ikp_a\n", "hunter2"} {
		t.Run(value, func(t *testing.T) {
			f := newReportFixture(t, "wip", nil, 7400)
			f.writeToken(value, 0600)
			f.expect(f.run(context.Background(), []string{"token"}, nil), 1, "", "sandbox: "+filepath.Join(f.data, "token")+": does not hold a bearer token\n")
		})
	}
}

// R-AK9Z-N93X
func TestTokenUnreadableFile(t *testing.T) {
	f := newReportFixture(t, "wip", nil, 7400)
	path := filepath.Join(f.data, "token")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	f.expect(f.run(context.Background(), []string{"token"}, nil), 1, "", "sandbox: "+path+": is a directory\n")
}

// R-AFEE-4655
func TestTokenDataCannotBeCreated(t *testing.T) {
	f := newReportFixture(t, "wip", nil, 7400)
	if err := os.WriteFile(f.data, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	f.expect(f.run(context.Background(), []string{"token", "set"}, strings.NewReader("ikp_a")), 1, "", "sandbox: "+f.data+": not a directory\n")
}

// R-AGMA-HXVU
func TestTokenCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("permission proof requires non-root test process")
	}
	f := newReportFixture(t, "wip", nil, 7400)
	f.writeToken("ikp_previous", 0600)
	dir, err := os.OpenRoot(f.data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dir.Chmod(".", 0700); err != nil {
			t.Error(err)
		}
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := dir.Chmod(".", 0500); err != nil {
		t.Fatal(err)
	}
	f.expect(f.run(context.Background(), []string{"token", "set"}, strings.NewReader("ikp_a")), 1, "", "sandbox: "+filepath.Join(f.data, "token")+": permission denied\n")
	got, err := dir.ReadFile("token")
	if err != nil || string(got) != "ikp_previous" {
		t.Fatalf("earlier token %q %v", got, err)
	}
}
