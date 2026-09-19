package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestCheckoutErrorsThroughCLI(t *testing.T) {
	// R-QEXY-821P
	t.Run("no app", func(t *testing.T) {
		root := writeD04CLIApp(t)
		deps := d04CheckoutDeps(t, root)
		want := "devctl: no app 'bogus' in the checkout\n"

		for _, args := range [][]string{
			{"build", "bogus"},
			{"secrets", "push", "sbx1", "bogus"},
		} {
			t.Run(strings.Join(args, "_"), func(t *testing.T) {
				assertResult(t, invokeWithDeps(deps, args...), 2, "", want)
			})
		}
	})

	t.Run("manifest", func(t *testing.T) {
		root := writeD04CLIApp(t)
		manifest := filepath.Join(root, "crm", checkout.ManifestFile)
		if err := os.WriteFile(manifest, []byte("app = \"other\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertResult(t, invokeWithDeps(d04CheckoutDeps(t, root), "build", "crm"), 2, "",
			"devctl: crm: etc/manifest.toml: app is 'other', not 'crm'\n")
	})

	t.Run("not in checkout", func(t *testing.T) {
		const dir = "/work"
		deps := d04CheckoutDeps(t, dir)
		deps.Exec = func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" || !reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}) {
				t.Fatalf("checkout command = %#v", command)
			}
			return seam.Result{ExitCode: 128}, nil
		}
		assertResult(t, invokeWithDeps(deps, "space", "list"), 2, "",
			"devctl: '/work' is not inside a git checkout\n")
	})

	t.Run("no root file", func(t *testing.T) {
		root := t.TempDir()
		assertResult(t, invokeWithDeps(d04CheckoutDeps(t, root), "space", "list"), 2, "",
			"devctl: no infra/terraform.tfvars.json in the checkout\n")
	})

	t.Run("invalid root file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, checkout.RootFilePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"domain": "ikigenba.dev"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		assertResult(t, invokeWithDeps(d04CheckoutDeps(t, root), "space", "list"), 2, "",
			"devctl: infra/terraform.tfvars.json: missing 'region'\n")
	})
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
	if err := os.MkdirAll(filepath.Join(appDir, "cmd", "crm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "cmd", "crm", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, checkout.ManifestFile), []byte("app = \"crm\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func d04CheckoutDeps(t *testing.T, root string) seam.Deps {
	t.Helper()
	return seam.Deps{
		Dir:  root,
		EUID: 1,
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" || !reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}) {
				t.Fatalf("checkout command = %#v", command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("unexpected cloud call")
			return cloud.Clients{}, nil
		},
	}
}
