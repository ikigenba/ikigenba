package cert_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestDomainCertificateAPI(t *testing.T) {
	// R-GWRV-RWU9 R-GXZS-5OKY R-GZ7O-JGBN
	const hostWebroot string = cert.Webroot
	if hostWebroot != "/var/www/acme" {
		t.Fatal(hostWebroot)
	}
	acceptObtainDomainSignature(cert.ObtainDomain)
	acceptDeleteDomainSignature(cert.DeleteDomain)
}

func acceptObtainDomainSignature(func(context.Context, host.Env, string, string) error) {}
func acceptDeleteDomainSignature(func(context.Context, host.Env, string) error)         {}

func TestDomainCertificateRejectsMissingInputsWithoutEffects(t *testing.T) {
	// R-H0FK-X82C R-S7VF-DU3K
	for _, tc := range []struct {
		name, email string
		deleting    bool
	}{
		{name: "custom.example"},
		{},
		{email: "admin@example.com"},
		{deleting: true},
	} {
		t.Run(tc.name+tc.email+map[bool]string{true: "delete", false: "obtain"}[tc.deleting], func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "kept"), []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotTree(t, root)
			calls := 0
			env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				calls++
				return host.Result{}, nil
			}}
			var err error
			if tc.deleting {
				err = cert.DeleteDomain(context.Background(), env, tc.name)
			} else {
				err = cert.ObtainDomain(context.Background(), env, tc.name, tc.email)
			}
			if err == nil || !tc.deleting && tc.email == "" && err.Error() != "acme.email not set" {
				t.Fatalf("error = %v", err)
			}
			if calls != 0 || !reflect.DeepEqual(snapshotTree(t, root), before) {
				t.Fatal("rejected call changed host state or executed a command")
			}
		})
	}
}

func TestDomainCertificateExactCommands(t *testing.T) {
	// R-H1NH-AZT1 R-H5B6-GB14
	for _, deleting := range []bool{false, true} {
		t.Run(map[bool]string{true: "delete", false: "obtain"}[deleting], func(t *testing.T) {
			root := t.TempDir()
			ctx := context.WithValue(context.Background(), contextKey{}, "custom-domain")
			before := snapshotTree(t, root)
			calls := 0
			var command host.Command
			env := host.Env{Root: root, Execute: func(gotCtx context.Context, got host.Command) (host.Result, error) {
				if gotCtx != ctx || !reflect.DeepEqual(snapshotTree(t, root), before) {
					t.Fatal("context not passed or host state changed before execution")
				}
				calls++
				command = got
				return host.Result{}, nil
			}}
			args := []string{"certonly", "--non-interactive", "--agree-tos", "--email", "admin@example.com", "--webroot", "--webroot-path", filepath.Join(root, "var/www/acme"), "--deploy-hook", wantDeployHook, "--cert-name", "custom.example", "-d", "custom.example", "--keep-until-expiring"}
			var err error
			if deleting {
				args = []string{"delete", "--non-interactive", "--cert-name", "custom.example"}
				err = cert.DeleteDomain(ctx, env, "custom.example")
			} else {
				err = cert.ObtainDomain(ctx, env, "custom.example", "admin@example.com")
			}
			args = append(args, "--config-dir", filepath.Join(root, "etc/letsencrypt"), "--work-dir", filepath.Join(root, "var/lib/letsencrypt"), "--logs-dir", filepath.Join(root, "var/log/letsencrypt"))
			if err != nil || calls != 1 || !reflect.DeepEqual(command, host.Command{Name: "certbot", Args: args}) {
				t.Fatalf("error %v, calls %d, command %#v; want args %v", err, calls, command, args)
			}
			if !reflect.DeepEqual(snapshotTree(t, root), before) {
				t.Fatal("operation directly wrote state")
			}
		})
	}
}

func TestDomainCertificatePreservesFailuresWithoutPrinting(t *testing.T) {
	// R-H43A-2JAF R-H7QZ-7UII
	cause := errors.New("cannot start certbot")
	for _, deleting := range []bool{false, true} {
		for _, executionError := range []error{nil, cause, &host.CommandError{Label: "underlying", Err: cause}} {
			result := host.Result{ExitCode: 7, Stdout: []byte("captured"), Stderr: []byte("failure detail")}
			env := host.Env{Root: t.TempDir(), Execute: func(context.Context, host.Command) (host.Result, error) {
				return result, executionError
			}}
			var err error
			stdout, stderr := captureOutput(t, func() {
				if deleting {
					err = cert.DeleteDomain(context.Background(), env, "custom.example")
				} else {
					err = cert.ObtainDomain(context.Background(), env, "custom.example", "admin@example.com")
				}
			})
			label := "certbot certonly"
			if deleting {
				label = "certbot delete"
			}
			var commandError *host.CommandError
			if !errors.As(err, &commandError) || commandError.Label != label || !reflect.DeepEqual(commandError.Result, result) || !errors.Is(commandError.Err, executionError) || len(stdout)+len(stderr) != 0 {
				t.Fatalf("error %#v, stdout %q, stderr %q", commandError, stdout, stderr)
			}
		}
	}
}

func TestDomainDeployHookExecutesViaFixturePATH(t *testing.T) {
	// R-H2VD-ORJQ
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rootFS.Close(); err != nil {
			t.Error(err)
		}
	})
	logPath := filepath.Join(root, "systemctl.log")
	if err := rootFS.WriteFile("bin/systemctl", []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$HOOK_LOG\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rootFS.Chmod("bin/systemctl", 0o750); err != nil {
		t.Fatal(err)
	}
	var hook string
	env := host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		hook = argumentValue(t, c.Args, "--deploy-hook")
		return host.Result{}, nil
	}}
	if err := cert.ObtainDomain(context.Background(), env, "custom.example", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	// The shell splits the hook, then resolves the first word through a PATH
	// containing only the fixture executable. No host service can be invoked.
	validation := "set -- " + hook + "; case \"$1\" in */*) exit 9;; esac; command -v \"$1\""
	result, err := host.Exec(context.Background(), host.Command{Name: "/bin/sh", Args: []string{"-c", validation}, Env: []string{"PATH=" + bin}})
	if err != nil || result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != filepath.Join(bin, "systemctl") {
		t.Fatalf("hook resolution = %#v, %v", result, err)
	}
	result, err = host.Exec(context.Background(), host.Command{Name: "/bin/sh", Args: []string{"-c", hook}, Env: []string{"PATH=" + bin, "HOOK_LOG=" + logPath}})
	if err != nil || result.ExitCode != 0 {
		t.Fatal(result, err)
	}
	got, err := rootFS.ReadFile("systemctl.log")
	if err != nil || string(got) != "try-reload-or-restart nginx\n" {
		t.Fatal(string(got), err)
	}
}
