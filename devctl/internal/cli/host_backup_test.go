package cli

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/spacemove"
)

func TestCreateAccountsOnlyListingSkipsAllRestores(t *testing.T) {
	// R-1EZN-XUSH R-1IND-360K
	f := newCompleteCreateFake(t)
	f.onlyAccounts = true
	got := invokeWithDeps(f.deps(), "space", "create", "staging", "--acme-email", "ops@ikigenba.dev")
	if got.code != 0 || got.stderr != "" || !strings.Contains(got.stdout, "restore: ok (no host backup)\ninit: ok\n") || strings.Count(got.stdout, "restore:") != 1 {
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

func TestMoveRejectsListingsWithoutHostTarball(t *testing.T) {
	// R-LXP7-5B5C R-LU1H-ZZX9
	for _, key := range []string{"", "sbx1/host/2026-11-08T18:29:41.222933531Z.accounts.tar.zst", "sbx1/host/nested/backup.tar.zst", "other/host/backup.tar.zst", "sbx1/host/backup.zip", "sbx1/host/backup.1.tar.zst", "sbx1/host/latest.tar.zst"} {
		t.Run(key, func(t *testing.T) {
			f := newMoveFake(t)
			f.instances[0].Space = "sbx1.ikigenba.dev"
			f.addresses[0].Space = "sbx1.ikigenba.dev"
			f.hostObjects = []cloud.Object{}
			if key != "" {
				f.hostObjects = append(f.hostObjects, cloud.Object{Key: key})
			}
			got := f.run("sbx1")
			if got.code != 1 || !strings.HasSuffix(got.stdout, "opsctl: ok (5 keys set)\n") || got.stderr != "devctl: restore: no host backup under s3://ikigenba.dev/sbx1/host/\n" || len(f.changes) != 0 || f.events[len(f.events)-1] != "objects sbx1/host/" {
				t.Fatal(got, f.events)
			}
			last := f.commands[len(f.commands)-1]
			if !strings.HasSuffix(last.Args[len(last.Args)-1], "'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/sbx1/'") {
				t.Fatal("process after failed restore", last)
			}
			f.clean()
		})
	}
	// Check the returned error at the package boundary as well as its CLI form.
	f := newMoveFake(t)
	f.onlyAccounts = true
	var out bytes.Buffer
	err := spacemove.Run(f.ctx, []string{"staging"}, "test-version", &out, f.deps())
	var missing *spacemove.NoHostBackupError
	if !errors.As(err, &missing) || reflect.ValueOf(err) != reflect.ValueOf(missing) || missing.Bucket != "ikigenba.dev" || missing.Prefix != "staging/host/" {
		t.Fatal(err)
	}
	f.clean()
}

func TestMoveHostBackupSelectionBreaksTieByKey(t *testing.T) {
	// R-LU1H-ZZX9
	f := newMoveFake(t)
	when := time.Unix(1, 0)
	f.hostObjects = []cloud.Object{
		{Key: "staging/host/2026-11-08T18:29:41.5Z.tar.zst", Modified: when},
		{Key: "staging/host/2026-11-08T18:29:41.50Z.tar.zst", Modified: when.Add(time.Hour)},
		{Key: "staging/host/2026-11-08T18:29:41.5Z.accounts.tar.zst", Modified: when.Add(time.Hour)},
		{Key: "staging/host/nested/2026-11-08T18:29:41.5Z.tar.zst", Modified: when.Add(time.Hour)},
	}
	got := f.run("staging")
	want := strings.ReplaceAll(expectedMove(false, false, false, ""), "host/2026-11-08T18:29:41.222933531Z.tar.zst", "host/2026-11-08T18:29:41.5Z.tar.zst")
	assertResult(t, got, 0, want, "")
	f.clean()
}
