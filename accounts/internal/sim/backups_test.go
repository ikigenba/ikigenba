package sim

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

func checkBackupDrops(t *testing.T, path string, clock *simClock, c *memory.Cloud, streams, backups chan chan time.Time, look func(), report func() protocol.Report) {
	t.Helper()
	// R-D9O3-RR2D R-DAW0-5IT2 R-D60E-MFUA
	for range 2 {
		look()
		expectAbsent(t, path)
	}
	must(t, os.Mkdir(path, 0o700))
	look()
	info, err := os.Lstat(path)
	must(t, err)
	if !info.IsDir() {
		t.Fatalf("backup drop was changed: %v", info.Mode())
	}
	must(t, os.Remove(path))
	const database = "data/app.db"
	const plain = "data/old.db"
	lines := "host01 team\nhost01 team database\nhost01 team database /etc/x.db\n" +
		"host01 team store data/a.db\nhost01 team fail now\nnohost team database data/missing.db\n" +
		"host01\tteam database " + database + "\nhost01 team plain " + plain + "\n"
	const clipped = "host01 team database data/cut.db"
	padding := strings.Repeat("x", MaxBackupFileBytes-len(lines)-len(clipped)-1)
	drop := func(body string) {
		t.Helper()
		must(t, os.WriteFile(path, []byte(body), 0o600))
		look()
		expectAbsent(t, path)
	}
	drop(lines + padding + "\n" + clipped + "x\nhost01 team database data/late.db\n")
	fireBackupPass(t, streams, clock)
	entry := teamBackup(t, report())
	dbs := entry.Databases
	if len(dbs) != 2 || dbs[0].Path != database || dbs[0].Error != "" || dbs[0].Synced.IsZero() ||
		dbs[1].Path != plain || dbs[1].Error != machine.NotWAL || !dbs[1].Synced.IsZero() {
		t.Fatalf("backup drop reported databases %+v", dbs)
	}
	bucket := settings.SandboxHosts.BackupBucket()
	keys := backupKeys(t, c, bucket, protocol.DataPrefix("team")+"/")
	if !slices.Equal(keys, []string{protocol.AccountPrefix("team") + database}) {
		t.Fatalf("stream wrote keys %v", keys)
	}
	drop("host01 team fail\n")
	fireBackupPass(t, backups, clock)
	if entry := teamBackup(t, report()); entry.Error == "" || !entry.Snapshot.IsZero() {
		t.Fatalf("fail instruction reported %+v", entry)
	}
	drop("host01 team heal")
	fireBackupPass(t, backups, clock)
	if entry := teamBackup(t, report()); entry.Error != "" || entry.Snapshot.IsZero() {
		t.Fatalf("heal instruction reported %+v", entry)
	}
	keys = backupKeys(t, c, bucket, protocol.FilesPrefix("team")+"/")
	if len(keys) == 0 {
		t.Fatal("healed backup did not write a repository key")
	}
}

func fireBackupPass(t *testing.T, timers chan chan time.Time, clock *simClock) {
	t.Helper()
	// Each worker publishes its next timer before the pass. Wait through that
	// timer as well to observe that the pass triggered here finished on both hosts.
	for range 2 {
		first, second := await(t, timers), await(t, timers)
		first <- clock.now()
		second <- clock.now()
	}
	first, second := await(t, timers), await(t, timers)
	timers <- first
	timers <- second
}

func backupKeys(t *testing.T, c *memory.Cloud, bucket, prefix string) []string {
	t.Helper()
	var keys []string
	must(t, c.ListKeys(t.Context(), bucket, prefix, func(page []string) bool {
		keys = append(keys, page...)
		return true
	}))
	return keys
}

func teamBackup(t *testing.T, r protocol.Report) protocol.AccountBackup {
	t.Helper()
	for _, entry := range r.Backups {
		if entry.Account == "team" {
			return entry
		}
	}
	t.Fatalf("report for %s has no backup for team: %+v", r.InstanceID, r.Backups)
	return protocol.AccountBackup{}
}
