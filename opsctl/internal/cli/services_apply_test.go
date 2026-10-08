package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantServicesUsage = `Usage: opsctl services <subcommand>

Generate /run/ikigenba/services.json, the services file every app reads
through IKIGENBA_SERVICES, from the release /opt/ikigenba/current names,
host.name, and which apps systemd reports disabled. The file is generated,
never edited.

Subcommands:
  apply  write the file; with no current release, write an empty list

Configuration keys:
  host.name  the fully-qualified name this host answers at

ikigenba-services.service runs 'opsctl services apply' at boot, before any
app starts, so the file is there again after the host restarts.
`

// R-9L1Q-PZKQ R-9NHJ-HJ24
func TestServicesHelpAndGrammarBeforeHostAccess(t *testing.T) {
	for _, uid := range []int{0, 1000} {
		for _, test := range []struct {
			args     []string
			code     exitCode
			out, err string
		}{
			{[]string{"--help"}, exitOK, wantServicesUsage, ""}, {[]string{"-h"}, exitOK, wantServicesUsage, ""},
			{nil, exitUsage, "", "opsctl: no services subcommand given\n\nsee 'opsctl services --help' for usage\n"},
			{[]string{"unknown"}, exitUsage, "", "opsctl: unknown services subcommand 'unknown'\n\nsee 'opsctl services --help' for usage\n"},
			{[]string{"apply", "extra"}, exitUsage, "", "opsctl: services apply takes no arguments\n\nsee 'opsctl services --help' for usage\n"},
		} {
			var out, err bytes.Buffer
			root := filepath.Join(t.TempDir(), "missing")
			code := exitCode(Run(append([]string{"services"}, test.args...), nil, &out, &err, Deps{Root: root, EUID: uid, Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("inert invocation executed process")
				return host.Result{}, nil
			}}))
			if code != test.code || out.String() != test.out || err.String() != test.err {
				t.Fatalf("%v: %d %q %q", test.args, code, out.String(), err.String())
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("inert invocation touched root %v", err)
			}
		}
	}
}

// R-9M9N-3RBF
func TestServicesApplySilentAndQuotedFailure(t *testing.T) {
	for _, failure := range []string{"", "unset", "empty", "command", "corrupt"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			store := config.Store{Root: root}
			if failure != "unset" {
				name := "Host.Example."
				if failure == "empty" {
					name = ""
				}
				if err := store.Set("host.name", name); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "corrupt" {
				if err := os.WriteFile(filepath.Join(root, "etc/ikigenba/config.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			deps := Deps{Root: root, EUID: 0, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
				calls++
				if failure == "command" {
					return host.Result{Stdout: []byte("out\n"), Stderr: []byte("err\n")}, errors.New("account unavailable")
				}
				switch c.Name {
				case "id":
					if c.Args[0] == "--user" {
						return host.Result{Stdout: []byte("998\n")}, nil
					}
					return host.Result{Stdout: []byte("ikigenba\n")}, nil
				case "chown":
					return host.Result{}, nil
				default:
					t.Fatalf("unexpected %+v", c)
					return host.Result{}, nil
				}
			}}
			var out, diagnostic bytes.Buffer
			code := exitCode(Run([]string{"services", "apply"}, nil, &out, &diagnostic, deps))
			if out.Len() != 0 {
				t.Fatalf("stdout %q", out.String())
			}
			if failure == "" {
				if code != exitOK || diagnostic.Len() != 0 {
					t.Fatalf("success %d %q", code, diagnostic.String())
				}
				data, err := readReleaseFile(root, "run/ikigenba/services.json")
				if err != nil || string(data) != "{\n  \"services\": []\n}\n" {
					t.Fatalf("file %s %v", data, err)
				}
			} else {
				if code != exitFail || !strings.HasPrefix(diagnostic.String(), "opsctl: ") {
					t.Fatalf("failure %d %q", code, diagnostic.String())
				}
				if failure == "unset" || failure == "empty" {
					if diagnostic.String() != "opsctl: host.name not set\n" || calls != 0 {
						t.Fatalf("unset %q %d", diagnostic.String(), calls)
					}
				}
				if failure == "command" && !strings.HasSuffix(diagnostic.String(), "\n\n> out\n> err\n") {
					t.Fatalf("command detail %q", diagnostic.String())
				}
				if _, err := os.Stat(filepath.Join(root, "run/ikigenba/services.json")); !os.IsNotExist(err) {
					t.Fatalf("failed apply wrote file %v", err)
				}
			}
		})
	}
}

// R-9M9N-3RBF
func TestServicesApplyUsesNormalizedHostAndCurrentRelease(t *testing.T) {
	root := t.TempDir()
	const sha = "c604e32a4b1f9d07e5c38a26b1d4f0e97a3c5b18"
	base := filepath.Join(root, "opt/ikigenba/releases", sha, "crm")
	for _, directory := range []string{"bin", "etc"} {
		if err := os.MkdirAll(filepath.Join(base, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "bin/crm"), []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "etc/manifest.toml"), []byte("app = \"crm\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(root, "opt/ikigenba/current")); err != nil {
		t.Fatal(err)
	}
	if err := (config.Store{Root: root}).Set("host.name", "Host.Example."); err != nil {
		t.Fatal(err)
	}
	accountCalls := 0
	ownershipCalls := 0
	deps := Deps{Root: root, EUID: 0, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		switch c.Name {
		case "systemctl":
			if c.Args[0] != "show" {
				t.Fatalf("unit changed %+v", c)
			}
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
		case "id":
			if c.Args[0] == "--user" {
				accountCalls++
				return host.Result{Stdout: []byte("998\n")}, nil
			}
			return host.Result{Stdout: []byte("ikigenba\n")}, nil
		case "chown":
			ownershipCalls++
			return host.Result{}, nil
		default:
			t.Fatalf("unexpected command %+v", c)
			return host.Result{}, nil
		}
	}}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"services", "apply"}, nil, &out, &diagnostic, deps); code != 0 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatalf("apply %d %q %q", code, out.String(), diagnostic.String())
	}
	if accountCalls != 1 || ownershipCalls != 1 {
		t.Fatalf("apply count account=%d ownership=%d", accountCalls, ownershipCalls)
	}
	data, err := readReleaseFile(root, "run/ikigenba/services.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"url": "https://crm.host.example"`) || !strings.Contains(string(data), `"enabled": false`) {
		t.Fatalf("file %s", data)
	}
}
