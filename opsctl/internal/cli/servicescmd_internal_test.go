package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestRestoreServicesPublicationIsSilentAndRunsOnlyAfterNginx(t *testing.T) {
	// R-XTC1-NPBI
	for _, failure := range []string{"", "source", "nginx", "services"} {
		t.Run(failure, func(t *testing.T) {
			root := configuredBackupRoot(t)
			if err := os.MkdirAll(filepath.Join(root, "var/opt/ikigenba/notes"), 0o750); err != nil {
				t.Fatal(err)
			}
			for name, contents := range map[string]string{
				"opt/notes/bin/notes": "binary", "opt/notes/etc/manifest.toml": "app='notes'\n", "etc/opt/ikigenba/notes/env": "DRAIN_SECONDS=5\n",
			} {
				filename := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := config.Store{Root: root}
			if err := store.Set("host.name", "HOST.Example.Test."); err != nil {
				t.Fatal(err)
			}
			if failure != "nginx" {
				if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			client := newHostCLICloud()
			if failure != "source" {
				client.objects["s3://backups.example/host/notes/2026-09-16T10:00:00Z.tar.zst"] = append([]byte{0x28, 0xb5, 0x2f, 0xfd}, makeRestoreCLITar(t, "restored")...)
			}
			if err := os.MkdirAll(filepath.Join(root, "var/lib/ikigenba"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "var/lib/ikigenba/services.json"), []byte("previous\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			calls, accountCalls := 0, 0
			accountPrepared := false
			zstd := roundTripZstdExecute(nil)
			execute := func(ctx context.Context, c host.Command) (host.Result, error) {
				if c.Name == "zstd" {
					return zstd(ctx, c)
				}
				if c.Name == "systemctl" && len(c.Args) == 4 && c.Args[0] == "show" {
					return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=disabled\n")}, nil
				}
				if c.Name == "getent" && strings.Join(c.Args, " ") == "passwd ikigenba" {
					accountPrepared = true
					return host.Result{Stdout: []byte("ikigenba:x:998:998:service:/nonexistent:/sbin/nologin\n")}, nil
				}
				if c.Name == "systemctl" && len(c.Args) > 0 && c.Args[0] == "stop" {
					return host.Result{}, nil
				}
				if c.Name == "id" && strings.Join(c.Args, " ") == "--user ikigenba" {
					if !accountPrepared {
						accountCalls++
						return host.Result{Stdout: []byte("998\n")}, nil
					}
					calls++
					if _, err := os.Stat(filepath.Join(root, "etc/nginx/conf.d/ikigenba.conf")); err != nil {
						t.Fatalf("services before nginx publication: %v", err)
					}
					if failure == "services" {
						return host.Result{}, errors.New("account unavailable")
					}
				}
				if c.Name == "id" {
					if c.Args[0] == "--user" {
						return host.Result{Stdout: []byte("998\n")}, nil
					}
					return host.Result{Stdout: []byte("ikigenba\n")}, nil
				}
				if c.Name == "chown" {
					return host.Result{}, nil
				}
				return host.Result{}, fmt.Errorf("unexpected command %s %v", c.Name, c.Args)
			}
			stdout, stderr, code := invokeBackupCLI([]string{"restore", "notes"}, hostCLIDeps(root, client, execute))
			wantCalls := 1
			if failure == "source" || failure == "nginx" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("calls %d want %d code %d stdout %q stderr %q", calls, wantCalls, code, stdout, stderr)
			}
			wantAccountCalls := 1
			if failure == "source" {
				wantAccountCalls = 0
			}
			if accountCalls != wantAccountCalls {
				t.Fatalf("initial account preparations %d want %d", accountCalls, wantAccountCalls)
			}
			if strings.Contains(stdout, "services:") {
				t.Fatalf("services report leaked: %q", stdout)
			}
			data := readRestoreServicesFile(t, root)
			if failure == "" {
				var document struct {
					Services []struct {
						Name string `json:"name"`
					} `json:"services"`
				}
				if err := json.Unmarshal([]byte(data), &document); err != nil {
					t.Fatal(err)
				}
				if code != 0 || stderr != "" || len(document.Services) != 1 || document.Services[0].Name != "notes" {
					t.Fatalf("success %d %q %q %q", code, stdout, stderr, data)
				}
			} else if code != 1 || data != "previous\n" || (failure != "source" && !strings.Contains(stderr, "nginx regeneration")) {
				t.Fatalf("failure %d %q %q %q", code, stdout, stderr, data)
			}
		})
	}
}
