package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestCheckoutUsageErrorsAreSingleLine(t *testing.T) {
	// R-WIEQ-9TUN
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "not in checkout",
			err:  fmt.Errorf("outer context: %w", &checkout.NotInCheckoutError{Dir: "/work"}),
			want: "devctl: '/work' is not inside a git checkout\n",
		},
		{
			name: "no app",
			err:  fmt.Errorf("outer context: %w", &checkout.NoAppError{Name: "bogus"}),
			want: "devctl: no app 'bogus' in the checkout\n",
		},
		{
			name: "manifest",
			err: fmt.Errorf("outer context: %w", &checkout.ManifestError{
				App: "crm", Detail: "invalid manifest", Err: errors.New("decode failure"),
			}),
			want: "devctl: crm: etc/manifest.toml: invalid manifest\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := operationError(&stderr, test.err); got != 2 {
				t.Fatalf("operationError exit code = %d, want 2", got)
			}
			if got := stderr.String(); got != test.want {
				t.Fatalf("stderr = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBogusCheckoutAppsThroughCLI(t *testing.T) {
	// R-WIEQ-9TUN
	root := writeD04CLIApp(t)
	cloudCalls := 0
	deps := seam.Deps{
		Dir:  root,
		EUID: 1,
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" || !reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}) {
				t.Fatalf("checkout command = %#v", command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, errors.New("unexpected cloud call")
		},
	}
	want := "devctl: no app 'bogus' in the checkout\n"

	for _, args := range [][]string{
		{"build", "bogus"},
		{"--account", "work", "secrets", "push", "foo.sbx.ikigenba.dev", "bogus"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			assertResult(t, invokeWithDeps(deps, args...), 2, "", want)
		})
	}
	if cloudCalls != 0 {
		t.Fatalf("Deps.Cloud calls = %d, want none", cloudCalls)
	}
}

func TestCheckoutGitErrorsThroughCLI(t *testing.T) {
	// R-CPT9-XFBP
	root := writeD04CLIApp(t)
	for _, test := range []struct {
		name   string
		stderr string
		want   string
	}{
		{
			name:   "nonempty stderr",
			stderr: "fatal: first line\nsecond line\n",
			want:   "devctl: git status --porcelain: exit status 23\n\n> fatal: first line\n> second line\n",
		},
		{
			name: "empty stderr",
			want: "devctl: git status --porcelain: exit status 23\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps := seam.Deps{
				Dir:  root,
				EUID: 1,
				Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
					switch {
					case command.Path == "git" && reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}):
						return seam.Result{Stdout: []byte(root + "\n")}, nil
					case command.Path == "git" && reflect.DeepEqual(command.Args, []string{"status", "--porcelain"}):
						return seam.Result{ExitCode: 23, Stderr: []byte(test.stderr)}, nil
					default:
						t.Fatalf("unexpected command = %#v", command)
						return seam.Result{}, nil
					}
				},
			}
			var stdout, stderr bytes.Buffer
			stdout.WriteString("preceding output\n")
			code := Run(context.Background(), []string{"build", "crm"}, strings.NewReader(""), &stdout, &stderr, deps)
			if code != 1 {
				t.Errorf("Run exit code = %d, want 1", code)
			}
			if got, want := stdout.String(), "preceding output\n"; got != want {
				t.Errorf("stdout = %q, want preserved %q", got, want)
			}
			if got := stderr.String(); got != test.want {
				t.Errorf("stderr = %q, want %q", got, test.want)
			}
		})
	}
}

func writeD04CLIApp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	appDir := filepath.Join(root, "crm")
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, checkout.ManifestFile), []byte("app = \"crm\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
