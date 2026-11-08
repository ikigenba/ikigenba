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
	// R-ILO6-ET2A R-IMW2-SKSZ
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
	// R-CMP5-YDIQ R-CK9D-6U1C
	for _, key := range []string{"", "staging/host/2026-11-08T18:29:41Z.accounts.tar.zst", "staging/host/nested/backup.tar.zst", "other/host/backup.tar.zst", "staging/host/backup.zip", "staging/host/backup.1.tar.zst"} {
		t.Run(key, func(t *testing.T) {
			f := newMoveFake(t)
			f.hostObjects = []cloud.Object{}
			if key != "" {
				f.hostObjects = append(f.hostObjects, cloud.Object{Key: key})
			}
			got := f.run("staging")
			if got.code != 1 || !strings.HasSuffix(got.stdout, "opsctl: ok (5 keys set)\n") || got.stderr != "devctl: restore: no host backup under s3://ikigenba.dev/staging/host/\n" || len(f.changes) != 0 || f.events[len(f.events)-1] != "objects staging/host/" {
				t.Fatal(got, f.events)
			}
			last := f.commands[len(f.commands)-1]
			if !strings.HasSuffix(last.Args[len(last.Args)-1], "'config' 'set' 'backup.s3_uri=s3://ikigenba.dev/staging/'") {
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
	// R-CK9D-6U1C
	f := newMoveFake(t)
	when := time.Unix(1, 0)
	f.hostObjects = []cloud.Object{
		{Key: "staging/host/z.tar.zst", Modified: when},
		{Key: "staging/host/a.tar.zst", Modified: when},
		{Key: "staging/host/zz.accounts.tar.zst", Modified: when.Add(time.Hour)},
		{Key: "staging/host/nested/zz.tar.zst", Modified: when.Add(time.Hour)},
	}
	got := f.run("staging")
	want := strings.ReplaceAll(expectedMove(false, false, false, ""), "host/2026-11-08T18:29:41Z.tar.zst", "host/z.tar.zst")
	assertResult(t, got, 0, want, "")
	f.clean()
}
