package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const expectedBuildUsage = `Usage: devctl build <app>

Build <app> for linux/amd64 and write <app>/dist/<app>-<version>.tar.xz, the
file deploy copies to a host and opsctl installs. HEAD must be a commit that
the app's version tag (<app>/v<semver>) points at, with no uncommitted
changes.
`

func TestBuildUsageDiagnosticsThroughCLI(t *testing.T) {
	// R-6G2S-18HB
	// R-6HAO-F080
	// R-6IIK-SRYP
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", args: []string{"build"}, want: "devctl: build needs <app>\n\nsee 'devctl build --help' for usage\n"},
		{name: "extra", args: []string{"build", "crm", "api"}, want: "devctl: build takes one <app>\n\nsee 'devctl build --help' for usage\n"},
		{name: "unknown before app", args: []string{"build", "--force", "crm"}, want: "devctl: unknown option '--force'\n\nsee 'devctl build --help' for usage\n"},
		{name: "unknown after app", args: []string{"build", "crm", "-v"}, want: "devctl: unknown option '-v'\n\nsee 'devctl build --help' for usage\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertResult(t, invoke(test.args...), 2, "", test.want)
		})
	}
}

func TestBuildHelpThroughCLIHasNoExternalOperation(t *testing.T) {
	// R-6DMZ-9OZX
	// R-GSOJ-R4YD
	for _, args := range [][]string{
		{"build", "--help"},
		{"build", "-h"},
		{"build", "crm", "--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			execCalls := 0
			cloudCalls := 0
			result := invokeWithDeps(seam.Deps{
				EUID: 1,
				Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
					execCalls++
					return seam.Result{}, errors.New("unexpected Exec call")
				},
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					cloudCalls++
					return cloud.Clients{}, errors.New("unexpected Cloud call")
				},
			}, args...)
			assertResult(t, result, 0, expectedBuildUsage, "")
			if execCalls != 0 || cloudCalls != 0 {
				t.Fatalf("external calls = Exec %d, Cloud %d; want zero", execCalls, cloudCalls)
			}
		})
	}
}

func TestBuildDispatchesArgumentsStdoutAndDeps(t *testing.T) {
	// R-6JQH-6JPE R-TI90-6NW4
	for _, prefix := range [][]string{nil, {"--account", "unused"}} {
		t.Run(strings.Join(prefix, "_"), func(t *testing.T) {
			fixture := newCLIBuildFixture(t)
			args := append(append([]string(nil), prefix...), "build", "crm")
			result := invokeWithDeps(fixture.deps(), args...)
			assertResult(t, result, 0, "crm/dist/crm-v1.2.3.tar.xz\n", "")
			if fixture.cloudCalls != 0 {
				t.Fatalf("Cloud calls = %d, want zero", fixture.cloudCalls)
			}
			if !reflect.DeepEqual(fixture.binaryArgs, [][]string{{"--version"}, {"manifest"}}) {
				t.Fatalf("binary arguments = %#v, want forwarded build of crm", fixture.binaryArgs)
			}
		})
	}
}

func TestBuildStartFailuresThroughCLI(t *testing.T) {
	// R-70T2-JC34
	for _, failPath := range []string{"go", "binary", "tar"} {
		t.Run(failPath, func(t *testing.T) {
			fixture := newCLIBuildFixture(t)
			fixture.failPath = failPath
			result := invokeWithDeps(fixture.deps(), "build", "crm")
			wantPath := failPath
			if failPath == "binary" {
				wantPath = filepath.Join(fixture.root, "crm", "dist", ".devctl-build-")
			}
			if result.code != 1 || result.stdout != "" {
				t.Fatalf("Run = (%d, %q, %q), want exit 1 and empty stdout", result.code, result.stdout, result.stderr)
			}
			if strings.Count(result.stderr, "\n") != 1 || !strings.HasPrefix(result.stderr, "devctl: ") || !strings.Contains(result.stderr, wantPath) {
				t.Fatalf("stderr = %q, want one diagnostic line containing %q", result.stderr, wantPath)
			}
		})
	}
}

type cliBuildFixture struct {
	t          *testing.T
	root       string
	workDir    string
	manifest   []byte
	failPath   string
	cloudCalls int
	binaryArgs [][]string
}

func newCLIBuildFixture(t *testing.T) *cliBuildFixture {
	t.Helper()
	root := t.TempDir()
	workDir := filepath.Join(root, "some", "subdirectory")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("app = \"crm\"\n")
	appDir := filepath.Join(root, "crm")
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(appDir, "cmd", "crm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "cmd", "crm", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "etc", "manifest.toml"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	return &cliBuildFixture{t: t, root: root, workDir: workDir, manifest: manifest}
}

func (fixture *cliBuildFixture) deps() seam.Deps {
	return seam.Deps{
		Dir:  fixture.workDir,
		EUID: 1,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			fixture.cloudCalls++
			return cloud.Clients{}, errors.New("unexpected Cloud call")
		},
		Exec: fixture.exec,
	}
}

func (fixture *cliBuildFixture) exec(ctx context.Context, command seam.Cmd) (seam.Result, error) {
	switch command.Path {
	case "git":
		return fixture.git(command)
	case "go":
		if fixture.failPath == "go" {
			return seam.Result{}, errors.New("could not start go")
		}
		if err := os.WriteFile(command.Args[2], []byte("compiled"), 0o600); err != nil {
			fixture.t.Fatal(err)
		}
		return seam.Result{}, nil
	case "tar":
		if fixture.failPath == "tar" {
			return seam.Result{}, errors.New("could not start tar")
		}
		return seam.Exec(ctx, command)
	default:
		fixture.binaryArgs = append(fixture.binaryArgs, append([]string(nil), command.Args...))
		if fixture.failPath == "binary" {
			return seam.Result{}, errors.New("could not start binary")
		}
		if reflect.DeepEqual(command.Args, []string{"--version"}) {
			return seam.Result{Stdout: []byte("v1.2.3\n")}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"manifest"}) {
			return seam.Result{Stdout: fixture.manifest}, nil
		}
		return seam.Result{}, errors.New("unexpected executable: " + command.Path)
	}
}

func (fixture *cliBuildFixture) git(command seam.Cmd) (seam.Result, error) {
	switch {
	case reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}):
		if command.Dir != fixture.workDir {
			fixture.t.Fatalf("checkout discovery Dir = %q, want %q", command.Dir, fixture.workDir)
		}
		return seam.Result{Stdout: []byte(fixture.root + "\n")}, nil
	case reflect.DeepEqual(command.Args, []string{"status", "--porcelain"}):
		if command.Dir != fixture.root {
			fixture.t.Fatalf("git status Dir = %q, want checkout root %q", command.Dir, fixture.root)
		}
		return seam.Result{}, nil
	case reflect.DeepEqual(command.Args, []string{"rev-parse", "HEAD"}):
		if command.Dir != fixture.root {
			fixture.t.Fatalf("git rev-parse HEAD Dir = %q, want checkout root %q", command.Dir, fixture.root)
		}
		return seam.Result{Stdout: []byte("0123456789abcdef\n")}, nil
	case reflect.DeepEqual(command.Args, []string{"tag", "--points-at", "HEAD"}):
		if command.Dir != fixture.root {
			fixture.t.Fatalf("git tag Dir = %q, want checkout root %q", command.Dir, fixture.root)
		}
		return seam.Result{Stdout: []byte("crm/v1.2.3\n")}, nil
	default:
		return seam.Result{}, errors.New("unexpected git arguments")
	}
}
