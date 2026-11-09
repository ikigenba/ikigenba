package cli

import (
	"strings"
	"testing"
)

func TestCreateAccountsOnlyListingSkipsAllRestores(t *testing.T) {
	// R-O101-XO9A R-O73J-UIYR
	f := newCompleteCreateFake(t)
	f.onlyAccounts = true
	got := invokeWithDeps(f.deps(), "space", "create", "staging", "--acme-email", "ops@ikigenba.dev")
	if got.code != 0 || got.stderr != "" || !strings.Contains(got.stdout, "restore: ok (no host backup)\nknown_hosts: ok (removed staging.ikigenba.dev)\ninit: ok\n") || strings.Count(got.stdout, "restore:") != 1 {
		t.Fatal(got)
	}
	if len(f.prefixes) != 1 || f.prefixes[0] != "staging/host/" {
		t.Fatal("listed app backups without host backup", f.prefixes)
	}
	for _, remote := range f.remotes {
		if strings.Contains(remote, "'restore'") || strings.Contains(remote, "'host.apex'") {
			t.Fatal(remote)
		}
	}
}
