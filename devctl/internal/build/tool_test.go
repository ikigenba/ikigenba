package build_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

var _ func(context.Context, *checkout.Checkout, string, string, string, string) error = build.Tool // R-2TH5-I5NM

func TestToolBuildAndIsolation(t *testing.T) {
	// R-2TH5-I5NM R-2UP1-VXEB
	for _, arch := range []string{"amd64", "arm64"} {
		for _, existing := range []bool{false, true} {
			t.Run(arch+" existing="+strconv.FormatBool(existing), func(t *testing.T) {
				f := newSuite(t)
				if existing {
					f.put(f.root, build.ReleaseFile(suiteSHA), "earlier release", 0600)
					f.put(f.root, "dist/keep", "keep", 0600)
				}
				// A malformed root file must have no bearing on this operation.
				f.put(f.root, "infra/terraform.tfvars.json", "invalid", 0600)
				out := filepath.Join(t.TempDir(), "opsctl")
				c := &checkout.Checkout{Root: f.root, Deps: f.deps}
				if err := build.Tool(t.Context(), c, suiteSHA, "opsctl", arch, out); err != nil {
					t.Fatal(err)
				}
				if len(f.commands) != 3 || len(f.worktrees) != 1 {
					t.Fatal(f.commands)
				}
				dir := f.worktrees[0]
				want := []seam.Cmd{
					{Path: "git", Dir: f.root, Args: []string{"worktree", "add", "--detach", dir, suiteSHA}},
					{Path: "go", Dir: filepath.Join(dir, "opsctl"), Args: []string{"build", "-buildvcs=false", "-o", out, "./cmd/opsctl"}, Env: []string{"GOOS=linux", "GOARCH=" + arch, "CGO_ENABLED=0", "GOWORK=off"}},
					{Path: "git", Dir: f.root, Args: []string{"worktree", "remove", "--force", dir}},
				}
				actual := append([]seam.Cmd(nil), f.commands...)
				actual[1].Env = slices.Clone(actual[1].Env)
				slices.Sort(actual[1].Env)
				slices.Sort(want[1].Env)
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("commands %#v want %#v", f.commands, want)
				}
				outputRoot, err := os.OpenRoot(filepath.Dir(out))
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := outputRoot.ReadFile("opsctl")
				closeErr := outputRoot.Close()
				if readErr != nil || closeErr != nil || string(data) != "binary opsctl" {
					t.Fatalf("output %q read %v close %v", data, readErr, closeErr)
				}
				entries, err := os.ReadDir(filepath.Join(f.root, "dist"))
				if existing {
					if err != nil || len(entries) != 2 || entries[0].Name() != suiteSHA+".tar.xz" || entries[1].Name() != "keep" {
						t.Fatal(entries, err)
					}
					data, err := os.ReadFile(c.Path(build.ReleaseFile(suiteSHA)))
					if err != nil || string(data) != "earlier release" {
						t.Fatal(string(data), err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatal("dist retained", entries, err)
				}
			})
		}
	}
}

func TestToolFailuresAndUncancelledCleanup(t *testing.T) {
	// R-2VWY-9P50 R-2UP1-VXEB
	for _, failure := range []string{"add", "compile", "exec", "typed exec", "remove", "compile and remove"} {
		t.Run(failure, func(t *testing.T) {
			f := newSuite(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("runner sentinel")
			if failure == "typed exec" {
				cause = &build.ProcessError{Label: "runner", Status: 9}
			}
			f.put(f.root, build.ReleaseFile(suiteSHA), "existing release", 0600)
			var returned error
			original := f.deps.Exec
			f.deps.Exec = func(callCtx context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path == "git" && c.Args[1] == "add" && failure == "add" {
					f.commands = append(f.commands, c)
					return seam.Result{ExitCode: 1, Stderr: []byte("add failed")}, nil
				}
				if c.Path == "go" && failure != "remove" {
					f.commands = append(f.commands, c)
					cancel()
					if failure == "exec" || failure == "typed exec" {
						return seam.Result{}, cause
					}
					return seam.Result{ExitCode: 1, Stderr: []byte("cmd/opsctl/main.go:9:2: undefined: backup")}, nil
				}
				if c.Path == "git" && c.Args[1] == "remove" && strings.Contains(failure, "remove") {
					if callCtx.Err() != nil {
						t.Fatal("cancelled removal")
					}
					f.commands = append(f.commands, c)
					return seam.Result{ExitCode: 1}, nil
				}
				return original(callCtx, c)
			}
			c := &checkout.Checkout{Root: f.root, Deps: f.deps}
			returned = build.Tool(ctx, c, suiteSHA, "opsctl", "amd64", filepath.Join(t.TempDir(), "opsctl"))
			if returned == nil {
				t.Fatal("missing failure")
			}
			data, readErr := os.ReadFile(c.Path(build.ReleaseFile(suiteSHA)))
			if readErr != nil || string(data) != "existing release" {
				t.Fatal("release changed", string(data), readErr)
			}
			var process *build.ProcessError
			var gitError *checkout.GitError
			switch failure {
			case "add", "remove":
				if !errors.As(returned, &gitError) {
					t.Fatal(returned)
				}
			case "exec", "typed exec":
				if errors.As(returned, &process) || returned.Error() != "go: "+cause.Error() {
					t.Fatal(returned)
				}
			default:
				if !errors.As(returned, &process) || process.Label != "build opsctl" || process.Status != 1 || process.Error() != "build opsctl: exit status 1" || process.Detail() != "> cmd/opsctl/main.go:9:2: undefined: backup" {
					t.Fatal(returned)
				}
			}
			if failure == "add" {
				if len(f.commands) != 1 {
					t.Fatal(f.commands)
				}
			} else {
				f.assertCleanup()
				if len(f.commands) != 3 {
					t.Fatal(f.commands)
				}
			}
		})
	}
}
