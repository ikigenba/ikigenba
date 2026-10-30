package cli_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInitHardeningRunsSilentlyAfterSlicesOnEveryLayout(t *testing.T) {
	// R-DJGH-HHQX
	for _, layout := range []string{"fresh", "per-app", "released"} {
		for _, changed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/changed=%t", layout, changed), func(t *testing.T) {
				deps := readyStateGuardDeps(t)
				switch layout {
				case "per-app":
					makeStateGuardApp(t, deps.Root, "notes", true)
					writeUninstallFile(t, deps.Root, "opt/notes/etc/manifest.toml", "app='notes'\n")
					writeUninstallFile(t, deps.Root, "etc/opt/ikigenba/notes/env", "TOKEN=kept\n")
				case "released":
					sha := strings.Repeat("a", 40)
					writeUninstallFile(t, deps.Root, "opt/ikigenba/releases/"+sha+"/release.json", `{"sha":"`+sha+`"}`)
					writeUninstallFile(t, deps.Root, "opt/ikigenba/releases/"+sha+"/notes/bin/notes", "binary")
					writeUninstallFile(t, deps.Root, "opt/ikigenba/releases/"+sha+"/notes/etc/manifest.toml", "app='notes'\n")
					if err := os.Symlink("releases/"+sha, filepath.Join(deps.Root, "opt/ikigenba/current")); err != nil {
						t.Fatal(err)
					}
				}
				seedHardeningRules(t, deps.Root)
				keys := filepath.Join(deps.Root, hardening.KeysDir)
				if changed {
					if err := initFixtureRootChmod(t, deps.Root, hardening.KeysDir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				certificate, slices, hardened := false, false, false
				queries := 0
				deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
					if command.Name == "certbot" {
						certificate = true
					}
					if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"restart", "nginx"}) {
						if !certificate {
							t.Fatal("slices before certificate")
						}
						slices = true
					}
					if command.Name == "getent" && reflect.DeepEqual(command.Args, []string{"group", "shell"}) {
						if !slices {
							t.Fatal("hardening before slices")
						}
						hardened = true
						queries++
					}
					if command.Name == "nginx" {
						if !hardened {
							t.Fatal("nginx before hardening")
						}
						info, err := os.Lstat(keys)
						if err != nil || info.Mode().Perm() != 0755 {
							t.Fatalf("hardening was not applied: %v %v", info, err)
						}
						return host.Result{}, errors.New("stop after hardening")
					}
					if result, ok := servicesFixtureCommand(command); ok {
						return result, nil
					}
					if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
						return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
					}
					return host.Result{}, nil
				}
				stdout, stderr, code := invoke([]string{"init"}, deps)
				if code != 1 || stderr != "opsctl: nginx -t: stop after hardening\n" || strings.Contains(stdout, "hardening") || queries != 1 {
					t.Fatalf("exit %d out %q err %q queries %d", code, stdout, stderr, queries)
				}
			})
		}
	}
}

func TestInitHardeningFailureKeepsCertificateAndSlices(t *testing.T) {
	// R-DJGH-HHQX R-LXGW-8J1R
	deps := readyStateGuardDeps(t)
	keys := filepath.Join(deps.Root, hardening.KeysDir)
	if err := initFixtureRootChmod(t, deps.Root, hardening.KeysDir, 0700); err != nil {
		t.Fatal(err)
	}
	certificate := filepath.Join(deps.Root, "etc/letsencrypt/live/api.example.com/fullchain.pem")
	var calls []host.Command
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		calls = append(calls, command)
		if command.Name == "certbot" {
			writeCLIInstallFile(t, certificate, "new certificate\n")
		}
		if command.Name == "rpm" && reflect.DeepEqual(command.Args, []string{"-q", "dnf-automatic"}) {
			return host.Result{ExitCode: 1}, nil
		}
		if command.Name == "dnf" {
			return host.Result{ExitCode: 1, Stdout: []byte("installer output\n"), Stderr: []byte("installer failure")}, nil
		}
		if command.Name == "nginx" {
			t.Fatal("nginx ran after failed hardening")
		}
		if result, ok := servicesFixtureCommand(command); ok {
			return result, nil
		}
		return host.Result{}, nil
	}
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 || stderr != "opsctl: dnf install: exit status 1\n\n> installer output\n> installer failure\n" || strings.Contains(stdout, "hardening") {
		t.Fatalf("%d %q %q", code, stdout, stderr)
	}
	last := calls[len(calls)-1]
	if last.Name != "dnf" || !reflect.DeepEqual(last.Args, []string{"install", "-y", "dnf-automatic"}) {
		t.Fatalf("last command %+v", last)
	}
	rootFS, err := os.OpenRoot(deps.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	if data, err := rootFS.ReadFile("etc/letsencrypt/live/api.example.com/fullchain.pem"); err != nil || string(data) != "new certificate\n" {
		t.Fatalf("certificate lost %q %v", data, err)
	}
	for _, name := range []string{"ikigenba.slice", "ikigenba-core.slice", "ikigenba-apps.slice", "nginx.service.d/ikigenba.conf"} {
		if _, err := os.Stat(filepath.Join(deps.Root, "etc/systemd/system", name)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Lstat(keys)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("earlier rule lost %v %v", info, err)
	}
	for _, name := range []string{"etc/nginx/conf.d/ikigenba.conf", "run/ikigenba/services.json", "etc/litestream.yml", "etc/systemd/system/ikigenba-renew-certificate.timer"} {
		if _, err := os.Lstat(filepath.Join(deps.Root, name)); !os.IsNotExist(err) {
			t.Fatalf("later step wrote %s: %v", name, err)
		}
	}
}

func initFixtureRootChmod(t *testing.T, root, name string, mode os.FileMode) error {
	t.Helper()
	fs, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = fs.Close() }()
	return fs.Chmod(strings.TrimPrefix(name, "/"), mode)
}
