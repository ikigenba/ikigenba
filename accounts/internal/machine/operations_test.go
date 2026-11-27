package machine_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestMemoryCloseFinals(t *testing.T) {
	// R-YLH8-UVKG
	s := backupState()
	now := fixtureTime
	var accountPuts int
	m := backupMachine(&now, nil, func(c protocol.Credentials, _ string) error {
		if c.AccessKeyID == "account-key" {
			accountPuts++
		}
		return nil
	})
	apply(t, m, s)
	s.Backup.Close = "close-one"
	host := s.Backup.Host
	s.Backup.Host = protocol.Credentials{}
	m.FailBackups("acme", true)
	pending := protocol.Step{ID: "close-one", State: protocol.StepPending}
	for range 2 {
		runPass(t, m.Backup, s)
		r := inspect(t, m, s)
		if r.HostBackup.Final != pending || r.Backups[0].Final != pending || accountPuts != 0 {
			t.Fatalf("unlocked close: %+v, %+v, %d puts", r.HostBackup, r.Backups, accountPuts)
		}
		now = now.Add(backupDelay)
	}
	apply(t, m, s)
	runPass(t, m.Backup, s)
	now = now.Add(backupDelay - time.Second)
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.HostBackup.Final != pending || r.Backups[0].Final != pending {
		t.Fatalf("close completed early: %+v, %+v", r.HostBackup, r.Backups)
	}
	now = now.Add(time.Second)
	runPass(t, m.Backup, s)
	failed := protocol.Step{ID: "close-one", State: protocol.StepFailed, At: now, Code: protocol.CodeFailed}
	if r := inspect(t, m, s); r.HostBackup.Final != failed || r.HostBackup.Error != machine.NoCredentials ||
		r.Backups[0].Final != failed || r.Backups[0].Error == "" || accountPuts != 0 {
		t.Fatalf("failed final: %+v, %+v, %d puts", r.HostBackup, r.Backups, accountPuts)
	}

	m.FailBackups("acme", false)
	s.Backup.Host = host
	now = now.Add(time.Second)
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.HostBackup.Final != failed || r.Backups[0].Final != failed || accountPuts != 1 {
		t.Fatalf("failure retried without greater applied version: %+v, %+v, %d puts", r.HostBackup, r.Backups, accountPuts)
	}
	s.Version++
	m.SetFailure(machine.ErrInvalidState)
	if err := m.Apply(t.Context(), s); !errors.Is(err, machine.ErrInvalidState) {
		t.Fatalf("failed Apply = %v", err)
	}
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.HostBackup.Final != failed || r.Backups[0].Final != failed || accountPuts != 1 {
		t.Fatalf("failed apply authorized a retry: %+v, %+v, %d puts", r.HostBackup, r.Backups, accountPuts)
	}
	m.SetFailure(nil)
	apply(t, m, s)
	now = now.Add(time.Second)
	runPass(t, m.Backup, s)
	done := protocol.Step{ID: "close-one", State: protocol.StepDone, At: now}
	if r := inspect(t, m, s); r.HostBackup.Final != done || r.Backups[0].Final != done ||
		r.Backups[0].Snapshot != now || r.Backups[0].Error != "" || accountPuts != 2 {
		t.Fatalf("retry did not force a snapshot: %+v, %+v, %d puts", r.HostBackup, r.Backups, accountPuts)
	}
	now = now.Add(time.Second)
	s.Accounts = append(s.Accounts, protocol.Account{Slug: "beta", State: protocol.AccountActive})
	s.Backup.Accounts = append(s.Backup.Accounts, protocol.AccountCredentials{Account: "beta", Credentials: protocol.Credentials{AccessKeyID: "beta-key"}})
	apply(t, m, s)
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.Backups[0].Final != done || r.Backups[1].Final.At != now ||
		r.Backups[1].Final.State != protocol.StepDone || accountPuts != 2 {
		t.Fatalf("close repeated or missed new account: %+v, %d puts", r.Backups, accountPuts)
	}
	s.Backup.Close = ""
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.HostBackup.Final != (protocol.Step{}) ||
		r.Backups[0].Final != (protocol.Step{}) || r.Backups[1].Final != (protocol.Step{}) {
		t.Fatalf("empty close: %+v, %+v", r.HostBackup, r.Backups)
	}
}

func TestMemoryCloseFinalRetryAfterConcurrentApply(t *testing.T) {
	// R-YLH8-UVKG
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	s := backupState()
	s.Backup.Close = "close-concurrent"
	now := fixtureTime
	putStarted, release := make(chan struct{}), make(chan struct{})
	releasePut := sync.OnceFunc(func() { close(release) })
	defer releasePut()
	var accountPuts int
	putErr := errors.New("forced snapshot failed")
	m := backupMachine(&now, nil, func(c protocol.Credentials, _ string) error {
		if c == s.Backup.Accounts[0].Credentials {
			accountPuts++
			if accountPuts == 2 {
				close(putStarted)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return putErr
			}
		}
		return nil
	})
	apply(t, m, s)
	runPass(t, m.Backup, s)
	now = now.Add(backupDelay)
	result := make(chan error, 1)
	go func() { result <- m.Backup(ctx, s) }()
	select {
	case <-putStarted:
	case err := <-result:
		t.Fatalf("forced snapshot did not start: %v", err)
	case <-ctx.Done():
		t.Fatalf("timed out waiting for forced snapshot: %v", ctx.Err())
	}
	newVersion := s
	newVersion.Version++
	applied := make(chan error, 1)
	go func() { applied <- m.Apply(ctx, newVersion) }()
	applyErr := awaitMachineTest(ctx, t, applied, "Apply during forced snapshot Put")
	releasePut()
	if backupErr := awaitMachineTest(ctx, t, result, "Backup after forced snapshot Put"); applyErr != nil || backupErr != nil {
		t.Fatalf("concurrent Apply = %v, Backup = %v", applyErr, backupErr)
	}
	if got := inspect(t, m, newVersion).Backups[0]; got.Final.State != protocol.StepFailed || got.Error != putErr.Error() {
		t.Fatalf("forced snapshot did not record its failure: %+v", got)
	}
	now = now.Add(time.Second)
	runPass(t, m.Backup, newVersion)
	if got := inspect(t, m, newVersion).Backups[0]; got.Final.State != protocol.StepDone || got.Final.At != now ||
		got.Snapshot != now || accountPuts != 3 {
		t.Fatalf("newer applied version did not retry final: %+v, %d puts", got, accountPuts)
	}
}

func TestMemoryWholeHostRestore(t *testing.T) {
	// R-Z3RQ-LFOV R-YFDQ-Y0UZ R-YP4Y-06SJ
	s := backupState()
	s.Accounts = nil
	now := fixtureTime
	disks := machine.NewDisks()
	old := backupMachine(&now, disks, nil)
	apply(t, old, s)
	m := backupMachine(&now, disks, func(protocol.Credentials, string) error {
		t.Error("a whole-host restore attempted a scheduled backup")
		return nil
	})
	s.Backup.Close = "ignored-while-restoring"
	s.Backup.Restore = &protocol.HostRestore{
		ID: "restore-one",
		Accounts: []protocol.RestoreAccount{
			{Slug: "zebra"},
			{Slug: "beta"},
			{Slug: "acme", Databases: []string{"data/current.db"}},
		},
	}
	s.Backup.Accounts = append(s.Backup.Accounts, protocol.AccountCredentials{Account: "beta", Credentials: protocol.Credentials{AccessKeyID: "beta-key"}})
	m.FailBackups("beta", true)
	pending := protocol.Step{ID: "restore-one", State: protocol.StepPending}
	checkPending := func() {
		t.Helper()
		runPass(t, m.Backup, s)
		r := inspect(t, m, s)
		if r.HostBackup.Restored != pending || len(r.Backups) != 3 {
			t.Fatalf("unexpected restore state: %+v, %+v", r.HostBackup, r.Backups)
		}
		for _, b := range r.Backups {
			if b.Restored != pending {
				t.Fatalf("unexpected account restore state: %+v", b)
			}
		}
	}
	checkPending()
	apply(t, m, s)
	now = now.Add(2 * backupDelay)
	checkPending()

	existing := protocol.DesiredState{DataVolume: "owned-existing"}
	apply(t, m, existing)
	s.DataVolume = existing.DataVolume
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.HostBackup.Restored != (protocol.Step{
		ID: "restore-one", State: protocol.StepFailed, At: now, Code: protocol.CodeNotEmpty,
	}) || len(m.State().Folders) != 0 {
		t.Fatalf("restore over existing volume: %+v, %+v", r.HostBackup, m.State())
	}

	s.DataVolume = "restore-destination"
	apply(t, m, s)
	checkPending()
	now = now.Add(backupDelay - time.Second)
	checkPending()
	now = now.Add(time.Second)
	runPass(t, m.Backup, s)
	done := protocol.Step{ID: "restore-one", State: protocol.StepDone, At: now}
	failed := protocol.Step{ID: "restore-one", State: protocol.StepFailed, At: now, Code: protocol.CodeFailed}
	r := inspect(t, m, s)
	if r.HostBackup.Restored != done || r.Backups[0].Account != "acme" || r.Backups[0].Restored != done ||
		r.Backups[1].Account != "beta" || r.Backups[1].Restored != failed || r.Backups[1].Error == "" ||
		r.Backups[2].Account != "zebra" || r.Backups[2].Restored != pending {
		t.Fatalf("completed restore: %+v, %+v", r.HostBackup, r.Backups)
	}
	state := m.State()
	if !slices.Equal(state.Restored, []string{"acme"}) || !slices.Equal(state.Folders, []string{"acme"}) ||
		len(state.Seeds) != 0 || !reflect.DeepEqual(r.Backups[0].Databases, []protocol.Database{{Path: "data/current.db"}}) {
		t.Fatalf("restored volume: %+v", state)
	}
	now = now.Add(time.Second)
	s.Backup.Accounts = append(s.Backup.Accounts, protocol.AccountCredentials{Account: "zebra", Credentials: protocol.Credentials{AccessKeyID: "zebra-key"}})
	runPass(t, m.Backup, s)
	if r := inspect(t, m, s); r.Backups[2].Restored.State != protocol.StepDone || r.Backups[2].Restored.At != now {
		t.Fatalf("late credentials: %+v", r.Backups)
	}
	s.Backup.Restore = nil
	s.Accounts = []protocol.Account{{Slug: "acme", State: protocol.AccountActive}}
	apply(t, m, s)
	if got := m.State(); len(got.Seeds) != 0 || !slices.Equal(got.Restored, []string{"acme", "zebra"}) ||
		!slices.Equal(got.Folders, got.Restored) {
		t.Fatalf("reactivation reseeded or lost folders: %+v", got)
	}
	replica := backupMachine(&now, disks, nil)
	apply(t, replica, s)
	runPass(t, replica.Stream, s)
	if got := replica.State().Backups[0].Databases[0]; got.Path != "data/current.db" || got.Synced != now {
		t.Fatalf("restored replica did not stream in WAL mode: %+v", got)
	}
}

func TestMemoryPartialRestores(t *testing.T) {
	// R-YNX1-MF1U R-YQCU-DYJ8 R-YP4Y-06SJ
	s := backupState()
	now := fixtureTime
	m := backupMachine(&now, nil, nil)
	apply(t, m, s)
	m.AddDatabase("acme", "data/app.db", true)
	m.AddDatabase("acme", "data/app.restored-20261118T120000Z.db", true)
	database := protocol.RestoreRequest{ID: "database-one", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: fixtureTime}
	databaseAgain := database
	databaseAgain.ID = "database-two"
	files := protocol.RestoreRequest{ID: "files-one", Account: "acme", Kind: protocol.RestoreFiles, Path: "docs/a.txt", AsOf: fixtureTime}
	filesAgain := files
	filesAgain.ID = "files-two"
	home := files
	home.ID, home.User = "home-one", "alice"
	missing := files
	missing.ID, missing.AsOf = "missing-one", fixtureTime.Add(-time.Hour)
	s.Backup.Restores = []protocol.RestoreRequest{files, database, home, missing}
	runPass(t, m.Backup, s)
	now = now.Add(backupDelay - time.Second)
	runPass(t, m.Backup, s)
	if got := m.State(); len(got.Restores) != 0 || len(got.Backups[0].Snapshots) != 1 {
		t.Fatalf("restore finished before delay: %+v", got)
	}
	now = now.Add(time.Second)
	runPass(t, m.Backup, s)
	s.Backup.Restores = append(s.Backup.Restores, filesAgain, databaseAgain)
	runPass(t, m.Backup, s)
	now = now.Add(backupDelay)
	runPass(t, m.Backup, s)
	want := []protocol.RestoreResult{
		{ID: "database-one", State: protocol.StepDone, Copy: "data/app.restored-20261118T120000Z-2.db"},
		{ID: "database-two", State: protocol.StepDone, Copy: "data/app.restored-20261118T120000Z-3.db"},
		{ID: "files-one", State: protocol.StepDone, Copy: "restored/20261118T120000Z/docs/a.txt"},
		{ID: "files-two", State: protocol.StepDone, Copy: "restored/20261118T120000Z-2/docs/a.txt"},
		{ID: "home-one", State: protocol.StepDone, Copy: "restored/20261118T120000Z/docs/a.txt"},
		{ID: "missing-one", State: protocol.StepFailed, Error: machine.ErrNoSnapshot.Error()},
	}
	if got := m.State().Restores; !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got, inspect(t, m, s).Restores) {
		t.Fatalf("restore results: %+v, want %+v", got, want)
	}
	now = now.Add(backupDelay)
	m.FailBackups("acme", true)
	runPass(t, m.Backup, s)
	if got := m.State().Restores; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed requests ran again: %+v", got)
	}
	s.Backup.Restores = nil
	runPass(t, m.Backup, s)
	oldScope := s
	backup := *s.Backup
	oldScope.Backup = &backup
	oldScope.Backup.Restores = []protocol.RestoreRequest{database}
	if got := m.State().Restores; len(got) != 0 || len(inspect(t, m, oldScope).Restores) != 0 {
		t.Fatalf("removed result was retained: %+v", got)
	}
	s = oldScope
	runPass(t, m.Backup, s)
	now = now.Add(backupDelay)
	runPass(t, m.Backup, s)
	if got := m.State().Restores; len(got) != 1 || got[0].ID != database.ID || got[0].State != protocol.StepFailed ||
		got[0].Copy != "" || got[0].Error == "" {
		t.Fatalf("request for failing account: %+v", got)
	}
}

func TestMemoryPartialRestoreWaitsForEligibleAccount(t *testing.T) {
	// R-YNX1-MF1U
	s := backupState()
	now := fixtureTime
	m := backupMachine(&now, nil, nil)
	apply(t, m, s)
	m.AddDatabase("acme", "data/app.db", true)
	available := protocol.RestoreRequest{ID: "available", Account: "acme", Kind: protocol.RestoreDatabase, Path: "data/app.db", AsOf: fixtureTime}
	missing := available
	missing.ID, missing.Path = "missing", "data/missing.db"
	s.Backup.Restores = []protocol.RestoreRequest{available, missing}
	active, credentials := s.Accounts, s.Backup.Accounts
	s.Accounts = nil
	runPass(t, m.Backup, s)
	now = now.Add(backupDelay)
	runPass(t, m.Backup, s)
	if got := m.State().Restores; len(got) != 0 {
		t.Fatalf("inactive account produced a result: %+v", got)
	}
	s.Accounts, s.Backup.Accounts = active, nil
	runPass(t, m.Backup, s)
	if got := m.State().Restores; len(got) != 0 {
		t.Fatalf("account without credentials produced a result: %+v", got)
	}
	s.Backup.Accounts = credentials
	runPass(t, m.Backup, s)
	got := m.State().Restores
	want := protocol.RestoreResult{ID: available.ID, State: protocol.StepDone, Copy: "data/app.restored-20261118T120000Z.db"}
	if len(got) != 2 || got[0] != want || got[1].ID != missing.ID || got[1].State != protocol.StepFailed ||
		got[1].Copy != "" || got[1].Error == "" {
		t.Fatalf("eligible requests: %+v", got)
	}
}
