package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

const fixtureCommit = "c604e32a9f1b7d58e03c6b2f4a19d8e7b5c30f61"

func fixtureGitResult(c seam.Cmd, worktree string) seam.Result {
	switch {
	case reflect.DeepEqual(c.Args, []string{"rev-parse", "HEAD"}):
		return seam.Result{Stdout: []byte(fixtureCommit + "\n")}
	case reflect.DeepEqual(c.Args, []string{"--no-optional-locks", "status", "--porcelain", "--untracked-files=normal"}):
		return seam.Result{}
	default:
		return seam.Result{Stdout: []byte(worktree + "\n")}
	}
}

func TestUpCommitRunsAndEnvironment(t *testing.T) {
	// R-9KQ3-5KKB R-9N5V-X41P R-9PLO-ONJ3 R-R0VY-536R R-R23U-IUXG
	for _, head := range []string{fixtureCommit, fixtureCommit + "\n", strings.Repeat("abcdef01", 8) + "\n"} {
		for _, status := range []string{"", "?? notes.txt\n", " M go.mod\n"} {
			t.Run(fmt.Sprintf("%d/%q", len(head), status), func(t *testing.T) {
				f := newUpFixture(t, "auth", "dummy")
				f.commit, f.treeStatus = head, status
				for _, active := range []bool{false, true} {
					f.active = active
					f.calls = nil
					if active {
						f.commit = "0d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6a7b8c9d0\n"
						f.treeStatus = ""
					}
					code, _, diagnostic := f.run("up")
					if code != 0 || diagnostic != "" {
						t.Fatalf("up: %d %q", code, diagnostic)
					}
					wantGit := []seam.Cmd{
						{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: f.worktree},
						{Path: "git", Args: []string{"rev-parse", "HEAD"}, Dir: f.worktree},
						{Path: "git", Args: []string{"--no-optional-locks", "status", "--porcelain", "--untracked-files=normal"}, Dir: f.worktree},
					}
					if len(f.calls) < 4 || !reflect.DeepEqual(f.calls[:3], wantGit) || f.calls[3].Path != "go" {
						t.Fatalf("commit run order: %+v", f.calls)
					}
					for _, c := range f.calls[3:] {
						if c.Path == "git" {
							t.Fatalf("extra git: %+v", c)
						}
					}
					commit := strings.TrimSuffix(f.commit, "\n")
					if f.treeStatus != "" {
						commit += "-dirty"
					}
					for _, app := range []string{"auth", "dummy"} {
						want := "DRAIN_SECONDS=\"5\"\nIKIGENBA_CALLBACK_URL=\"http://localhost:7400\"\nIKIGENBA_COMMIT=\"" + commit + "\"\nIKIGENBA_PUBLIC_URL=\"http://" + app + ".wip.localhost:7400\"\nIKIGENBA_SANDBOX=\"wip\"\nIKIGENBA_SERVICES=\"" + strings.ReplaceAll(filepath.Join(f.data, "services.json"), "$", "\\$") + "\"\n"
						if got := upRead(t, filepath.Join(f.data, "env", app+".env")); got != want {
							t.Fatalf("%s env: %q want %q", app, got, want)
						}
					}
				}
			})
		}
	}
}

func TestUpCommitFailuresPreserveDeployment(t *testing.T) {
	// R-QZO1-RBG2 R-9S1H-G70H R-9T9D-TYR6 R-9UHA-7QHV
	fatal := "fatal: ambiguous argument 'HEAD': unknown revision or path not in the working tree.\nHEAD\n"
	cases := []struct {
		name, target string
		result       seam.Result
		err          error
		diagnostic   string
	}{
		{name: "head exit", target: "head", result: seam.Result{ExitCode: 128, Stdout: []byte("HEAD\n"), Output: []byte(fatal)}, diagnostic: "sandbox: git rev-parse HEAD: exit status 128\n\n> fatal: ambiguous argument 'HEAD': unknown revision or path not in the working tree.\n> HEAD\n"},
		{name: "status exit", target: "status", result: seam.Result{ExitCode: 128}, diagnostic: "sandbox: git status --porcelain: exit status 128\n"},
		{name: "head runner", target: "head", err: errors.New("runner failed"), diagnostic: "sandbox: git rev-parse HEAD: runner failed\n"},
		{name: "status runner", target: "status", err: errors.New("runner failed"), diagnostic: "sandbox: git status --porcelain: runner failed\n"},
	}
	for _, stdout := range []string{"HEAD\n", "", strings.ToUpper(fixtureCommit), fixtureCommit[:39], fixtureCommit + "a", strings.Repeat("a", 63), fixtureCommit + "\r\n", fixtureCommit + "\n\n"} {
		diagnostic := "sandbox: git rev-parse HEAD: printed no commit name\n"
		output := ""
		if stdout == "HEAD\n" {
			output = stdout
			diagnostic += "\n> HEAD\n"
		}
		cases = append(cases, struct {
			name, target string
			result       seam.Result
			err          error
			diagnostic   string
		}{"invalid " + fmt.Sprintf("%q", stdout), "head", seam.Result{Stdout: []byte(stdout), Output: []byte(output)}, nil, diagnostic})
	}
	for _, known := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("known%t/%s", known, tc.name), func(t *testing.T) {
				f := newUpFixture(t, "auth", "dummy")
				if known {
					code, _, diagnostic := f.run("up")
					if code != 0 {
						t.Fatal(diagnostic)
					}
					f.active = true
					f.put(filepath.Join(f.data, "apps", "auth", "state", "db"), "kept", 0600)
				}
				f.calls = nil
				var before map[string]string
				if known {
					before = upCommitStable(t, f)
				}
				f.exec = func(c seam.Cmd) (seam.Result, error) {
					if c.Path == "git" && reflect.DeepEqual(c.Args, []string{"rev-parse", "HEAD"}) {
						if !known {
							before = upCommitStable(t, f)
						}
						if tc.target == "head" {
							return tc.result, tc.err
						}
					}
					if c.Path == "git" && len(c.Args) > 0 && c.Args[0] == "--no-optional-locks" && tc.target == "status" {
						return tc.result, tc.err
					}
					return f.answer(c)
				}
				code, out, diagnostic := f.run("up")
				if code != 1 || out != "" || diagnostic != tc.diagnostic {
					t.Fatalf("%d %q %q want %q", code, out, diagnostic, tc.diagnostic)
				}
				wantCalls := 2
				if tc.target == "status" {
					wantCalls = 3
				}
				if len(f.calls) != wantCalls {
					t.Fatalf("runs after failure: %+v", f.calls)
				}
				if got := upCommitStable(t, f); !reflect.DeepEqual(got, before) {
					t.Fatalf("files changed: %+v want %+v", got, before)
				}
				upMissing(t, filepath.Join(f.data, "stage"))
				if !known {
					entries := f.records()
					if len(entries) != 1 || entries[0].Port != 7400 || entries[0].Worktree != f.worktree || len(entries[0].Apps) != 0 {
						t.Fatalf("new registry: %+v", entries)
					}
					upMissing(t, f.data)
					upMissing(t, f.units)
				}
			})
		}
	}
}

func upCommitStable(t *testing.T, f *upFixture) map[string]string {
	t.Helper()
	snapshot := upStable(t, f, false)
	for key := range snapshot {
		if key == "apps" || strings.HasPrefix(key, "apps/") {
			delete(snapshot, key)
		}
	}
	return snapshot
}
