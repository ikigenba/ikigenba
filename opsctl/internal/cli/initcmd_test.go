package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantInitUsage = `Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  none yet; each setup command adds itself here when it is designed

Configuration keys:
  host.name  the fully-qualified name this host answers at, at or under a configured zone
`

func TestInitHelp(t *testing.T) {
	// R-ED1L-QHES
	for _, args := range [][]string{{"init", "--help"}, {"init", "-h"}} {
		stdout, stderr, code := invoke(args, depsAt(t, 1))
		if code != 0 || stdout != wantInitUsage || stderr != "" {
			t.Errorf("%q: exit %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestInitRejectsArguments(t *testing.T) {
	// R-EE9I-495H
	want := "opsctl: init takes no arguments\n\nsee 'opsctl init --help' for usage\n"
	for _, args := range [][]string{{"init", "extra"}, {"init", "--help", "extra"}, {"init", "-h", "extra"}} {
		stdout, stderr, code := invoke(args, depsAt(t, 0))
		if code != 2 || stdout != "" || stderr != want {
			t.Errorf("%q: exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", args, code, stdout, stderr, want)
		}
	}
}

func TestInitRequiresRootBeforeWork(t *testing.T) {
	// R-EFHE-I0W6
	deps := depsAt(t, 1)
	configDir := filepath.Join(deps.Root, "etc", "ikigenba")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps.LookPath = func(string) (string, error) {
		t.Fatal("LookPath called before root check")
		return "", nil
	}
	deps.LookupHost = func(_ context.Context, _ string) ([]string, error) {
		t.Fatal("LookupHost called before root check")
		return nil, nil
	}

	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("exit %d stdout %q stderr %q, want root refusal", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"init", "extra"}, deps)
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("argument case: exit %d stdout %q stderr %q, want root refusal", code, stdout, stderr)
	}
}

func TestInitCorruptConfig(t *testing.T) {
	// R-ESWA-PI1T
	deps := depsAt(t, 0)
	writeCorrupt(t, deps.Root)

	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "opsctl: ") || !strings.Contains(stderr, "config.json") {
		t.Errorf("stderr = %q, want one prefixed line naming config.json", stderr)
	}
}
