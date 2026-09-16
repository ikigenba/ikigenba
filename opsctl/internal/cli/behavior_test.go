package cli_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var allCommands = []string{"backup", "cert", "config", "dns", "host", "init", "install", "nginx", "restart", "restore", "retire", "status", "uninstall", "version"}

func inertDeps(t *testing.T, euid int) (cli.Deps, func()) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "ikigenba")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoAccess := observeFilesystemAccess(t, root)
	unexpected := func(name string) { t.Helper(); t.Errorf("unexpected environment call: %s", name) }
	return cli.Deps{
		Root: root, EUID: euid,
		Getenv:   func(string) string { unexpected("Getenv"); return "" },
		LookPath: func(string) (string, error) { unexpected("LookPath"); return "", errors.New("unexpected lookup") },
		LookupHost: func(context.Context, string) ([]string, error) {
			unexpected("LookupHost")
			return nil, errors.New("unexpected lookup")
		},
		Execute: func(context.Context, host.Command) (host.Result, error) {
			unexpected("Execute")
			return host.Result{}, errors.New("unexpected execution")
		},
		DNS: dns.Env{
			Open: func(context.Context, string) (dns.Provider, error) {
				unexpected("DNS.Open")
				return nil, errors.New("unexpected DNS")
			},
			LookupNS: func(context.Context, string) ([]string, error) {
				unexpected("DNS.LookupNS")
				return nil, errors.New("unexpected DNS")
			},
		},
		Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) {
			unexpected("Cloud.Open")
			return nil, errors.New("unexpected cloud")
		}},
	}, assertNoAccess
}

func TestNonVersionCommandsRefuseWithoutHostAccess(t *testing.T) {
	// R-ENLY-EGT2
	for _, uid := range []int{1, -1, 1000} {
		for _, name := range allCommands {
			if name == "version" {
				continue
			}
			t.Run(fmt.Sprintf("%s/%d", name, uid), func(t *testing.T) {
				deps, assertNoAccess := inertDeps(t, uid)
				args := []string{name}
				switch name {
				case "install":
					args = append(args, "s3://bucket/key")
				case "restart", "uninstall":
					args = append(args, "app")
				}
				stdout, stderr, code := invoke(args, deps)
				assertNoAccess()
				if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
					t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
				}
			})
		}
	}
}

func TestInformationalAndGrammarCallsAreInert(t *testing.T) {
	// R-ESHJ-XJRU
	cases := [][]string{nil, {"--help"}, {"-h"}, {"version"}, {"--version"}, {"-V"}, {"unknown"}, {"--unknown"}}
	for _, name := range allCommands {
		cases = append(cases, []string{name, "--help"}, []string{name, "-h"})
	}
	for _, uid := range []int{0, 1000} {
		for _, args := range cases {
			t.Run(fmt.Sprintf("%v/%d", args, uid), func(t *testing.T) {
				deps, assertNoAccess := inertDeps(t, uid)
				_, _, code := invoke(args, deps)
				assertNoAccess()
				wantCode := 0
				if len(args) == 0 || args[0] == "unknown" || args[0] == "--unknown" {
					wantCode = 2
				}
				if code != wantCode {
					t.Fatalf("exit %d, want %d", code, wantCode)
				}
			})
		}
	}
}

func TestReportFindingsAndOperationFailuresUseTheirStreams(t *testing.T) {
	// R-ER9N-JS15
	deps := configuredDNSDeps(t, &fakeDNSProvider{recordsErr: map[string]error{"ZONE": errors.New("provider failed")}}, "example.com")
	stdout, stderr, code := invoke([]string{"dns", "check"}, deps)
	if code != 1 || stdout != "example.com: failed: provider failed\n" || stderr != "" {
		t.Fatalf("report: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	stdout, stderr, code = invoke([]string{"dns", "list", "example.com"}, deps)
	if code != 1 || stdout != "" || stderr != "opsctl: provider failed\n" {
		t.Fatalf("operation: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestCapturedCommandFailureDiagnostics(t *testing.T) {
	// R-VW8X-AM1W
	// R-EQ1R-60AG
	cases := []struct{ name, out, err, want string }{
		{"both", "output\n\nlast", "error\r\nlast error", "\n> output\n> \n> last\n> error\r\n> last error\n"},
		{"stdout", "only out\n", "", "\n> only out\n"},
		{"stderr", "", "only err", "\n> only err\n"},
		{"empty", "", "", ""},
		{"blank", "\n", "\n", "\n> \n> \n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failure := fmt.Errorf("action failed: %w", &host.CommandError{Label: "worker", Result: host.Result{ExitCode: 7, Stdout: []byte(tc.out), Stderr: []byte(tc.err)}})
			deps := configuredDNSDeps(t, &fakeDNSProvider{recordsErr: map[string]error{"ZONE": failure}}, "example.com")
			stdout, stderr, code := invoke([]string{"dns", "list", "example.com"}, deps)
			want := "opsctl: action failed: worker: exit status 7\n" + tc.want
			if code != 1 || stdout != "" || stderr != want {
				t.Fatalf("exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
			}
		})
	}
}

func TestAuthoredDiagnosticDetail(t *testing.T) {
	// R-EQ1R-60AG
	deps := configuredDNSDeps(t, &fakeDNSProvider{recordsErr: map[string]error{"ZONE": errors.New("provider failed\n\nretry with updated credentials\nthen check the zone")}}, "example.com")
	stdout, stderr, code := invoke([]string{"dns", "list", "example.com"}, deps)
	want := "opsctl: provider failed\n\nretry with updated credentials\nthen check the zone\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
	}
}
