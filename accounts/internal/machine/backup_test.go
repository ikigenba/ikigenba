package machine_test

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

var fixtureTime = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

const backupDelay = 10 * time.Second

func backupMachine(now *time.Time, disks *machine.Disks, put func(protocol.Credentials, string) error) *machine.Memory {
	return machine.NewMemory(machine.MemoryConfig{
		ApplyDelay: backupDelay,
		Sleep:      func(context.Context, time.Duration) {},
		Now:        func() time.Time { return *now },
		Disks:      disks, Put: put,
	})
}

func backupState() protocol.DesiredState {
	return protocol.DesiredState{
		DataVolume: "volume-one", Version: 1,
		Accounts: []protocol.Account{{Slug: "acme", State: protocol.AccountActive}},
		Backup: &protocol.Backup{
			Bucket: "test-bucket", Settings: protocol.Settings{EveryMinutes: 60, CheckDays: 2},
			Accounts: []protocol.AccountCredentials{{Account: "acme", Credentials: protocol.Credentials{
				AccessKeyID: "account-key", SecretAccessKey: "test-account-secret", SessionToken: "test-account-token", Expires: fixtureTime.Add(time.Hour),
			}}},
			Host: protocol.Credentials{
				AccessKeyID: "host-key", SecretAccessKey: "test-host-secret", SessionToken: "test-host-token", Expires: fixtureTime.Add(2 * time.Hour),
			},
			HostPrefix: protocol.HostPrefix("host01", 7),
		},
	}
}

func runPass(t *testing.T, pass func(context.Context, protocol.DesiredState) error, s protocol.DesiredState) {
	t.Helper()
	if err := pass(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

func awaitMachineTest[T any](ctx context.Context, t *testing.T, result <-chan T, operation string) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s: %v", operation, ctx.Err())
		var zero T
		return zero
	}
}

func inspect(t *testing.T, m *machine.Memory, s protocol.DesiredState) protocol.Report {
	t.Helper()
	r, err := m.Inspect(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBackupCopy(t *testing.T) {
	// R-XTCV-M2X5 R-YBQ1-SPMW
	const notWAL, noCreds, tooMany, tooLarge = machine.NotWAL, machine.NoCredentials, machine.TooManyDatabases, machine.DataTooLarge
	texts := [...]string{notWAL, noCreds, tooMany, tooLarge}
	seen := make(map[string]bool)
	for _, text := range texts {
		if text == "" || strings.ContainsAny(text, "\r\n") || seen[text] {
			t.Fatalf("invalid or repeated diagnostic %q", text)
		}
		seen[text] = true
	}
	for _, err := range []error{machine.ErrNotSynced, machine.ErrNoSnapshot} {
		if err == nil || err.Error() == "" {
			t.Fatalf("invalid sentinel %v", err)
		}
		for _, other := range []error{machine.ErrNoDataVolume, machine.ErrDigestMismatch, machine.ErrNotExecutable,
			machine.ErrTooLarge, machine.ErrIdentity, machine.ErrInvalidState, linux.ErrClaudeKey} {
			if errors.Is(err, other) || errors.Is(other, err) {
				t.Fatalf("%v and %v match", err, other)
			}
		}
	}
	if errors.Is(machine.ErrNotSynced, machine.ErrNoSnapshot) || errors.Is(machine.ErrNoSnapshot, machine.ErrNotSynced) {
		t.Fatal("the restore errors match")
	}
}

func TestSharedDisks(t *testing.T) {
	// R-YAI5-EXW7 R-YCXY-6HDL R-YGLN-BSLO R-YFDQ-Y0UZ R-YE5U-K94A R-Y9A9-165I
	disks := machine.NewDisks()
	now := fixtureTime
	first := backupMachine(&now, disks, nil)
	second := backupMachine(&now, disks, nil)
	own := backupMachine(&now, nil, nil)
	s := backupState()
	first.AddDatabase("acme", "data/ignored.db", true)
	if got := first.State(); len(got.Folders) != 0 || len(got.Seeds) != 0 || len(got.Backups) != 0 {
		t.Fatalf("before apply: %+v", got)
	}
	apply(t, first, s)
	first.AddDatabase("acme", "data/shared.db", false)
	volumeOnly := protocol.DesiredState{DataVolume: s.DataVolume}
	apply(t, second, volumeOnly)
	apply(t, own, volumeOnly)
	got, want := second.State(), first.State()
	if !slices.Equal(got.Folders, []string{"acme"}) || !reflect.DeepEqual(got.Seeds, want.Seeds) || len(own.State().Folders) != 0 {
		t.Fatalf("shared: %+v, original: %+v, independent: %+v", got, want, own.State())
	}
	runPass(t, second.Stream, s)
	r := second.State().Backups[0]
	if !reflect.DeepEqual(r.Databases, []protocol.Database{{Path: "data/shared.db", Error: machine.NotWAL}}) {
		t.Fatalf("first replica facts: %+v", r)
	}
	first.AddDatabase("acme", "data/shared.db", true)
	runPass(t, second.Stream, s)
	if got := second.State().Backups[0].Databases; !reflect.DeepEqual(got, []protocol.Database{{
		Path: "data/shared.db", Oldest: now, Synced: now, Written: now,
	}}) {
		t.Fatalf("replacement replica facts: %+v", got)
	}
	for _, volume := range []string{"volume-new", ""} {
		apply(t, second, protocol.DesiredState{DataVolume: volume})
		if got := second.State(); len(got.Folders) != 0 || len(got.Seeds) != 0 || len(got.Backups[0].Databases) != 0 {
			t.Fatalf("volume %q: %+v", volume, got)
		}
	}
	second.SetFailure(errors.New("injected"))
	if err := second.Apply(t.Context(), volumeOnly); err == nil || len(second.State().Folders) != 0 {
		t.Fatalf("failed apply switched volume, error %v: %+v", err, second.State())
	}
}

type putCall struct {
	credentials protocol.Credentials
	key         string
}

func putCounts(puts []putCall) map[putCall]int {
	counts := make(map[putCall]int)
	for _, call := range puts {
		counts[call]++
	}
	return counts
}

func TestMemoryStream(t *testing.T) {
	// R-YJ1G-3C32 R-YGLN-BSLO R-YP4Y-06SJ R-YQCU-DYJ8
	s := backupState()
	now := fixtureTime.Add(250 * time.Millisecond).In(time.FixedZone("test", -3600))
	var puts []putCall
	putError := errors.New("injected replica error")
	m := backupMachine(&now, nil, func(c protocol.Credentials, key string) error {
		puts = append(puts, putCall{c, key})
		if key == protocol.AccountPrefix("acme")+"data/retry.db" {
			return putError
		}
		return nil
	})
	apply(t, m, s)
	m.AddDatabase("acme", "data/retry.db", true)
	m.AddDatabase("acme", "data/legacy.db", false)
	m.AddDatabase("acme", "data/current.db", true)
	good := s.Backup
	badBucket, badSettings, badCredentials := *good, *good, *good
	badBucket.Bucket = ""
	badSettings.Settings.EveryMinutes = 0
	badCredentials.Accounts = nil
	for _, backup := range []*protocol.Backup{nil, &badBucket, &badSettings, &badCredentials} {
		s.Backup = backup
		runPass(t, m.Stream, s)
		if m.State().Backups[0].Error != machine.NoCredentials || len(puts) != 0 {
			t.Fatalf("invalid backup streamed: %+v, puts %v", m.State().Backups, puts)
		}
	}
	s.Backup = good
	runPass(t, m.Stream, s)
	prefix := protocol.AccountPrefix("acme")
	wantPuts := []putCall{{good.Accounts[0].Credentials, prefix + "data/current.db"}, {good.Accounts[0].Credentials, prefix + "data/retry.db"}}
	if !maps.Equal(putCounts(puts), putCounts(wantPuts)) {
		t.Fatalf("puts = %v, want %v", puts, wantPuts)
	}
	if got := m.State().Backups[0]; got.Error != "" || got.DatabaseTotal != 3 || got.DatabaseErrors != 2 ||
		got.Databases[1].Error != machine.NotWAL || got.Databases[2].Error != putError.Error() {
		t.Fatalf("failed stream facts: %+v", got)
	}
	putError = nil
	now = now.Add(time.Second)
	m.FailBackups("acme", true)
	runPass(t, m.Stream, s)
	first, second := fixtureTime, fixtureTime.Add(time.Second)
	if got := m.State().Backups[0].Databases; got[0].Synced != first || got[0].Written != first ||
		got[2].Oldest != second || !got[2].Synced.IsZero() || !got[2].Written.IsZero() {
		t.Fatalf("failing account replica facts: %+v", got)
	}
	m.FailBackups("acme", false)
	now = now.Add(time.Second)
	runPass(t, m.Stream, s)
	third := fixtureTime.Add(2 * time.Second)
	want := protocol.AccountBackup{
		Account: "acme", DatabaseTotal: 3, DatabaseErrors: 1, Synced: third, Written: third,
		Databases: []protocol.Database{
			{Path: "data/current.db", Oldest: first, Synced: third, Written: third},
			{Path: "data/legacy.db", Error: machine.NotWAL},
			{Path: "data/retry.db", Oldest: second, Synced: third, Written: third},
		},
	}
	report := inspect(t, m, s)
	if !reflect.DeepEqual(report.Backups, []protocol.AccountBackup{want}) || !reflect.DeepEqual(m.State().Backups, report.Backups) ||
		!maps.Equal(putCounts(puts), putCounts(append(wantPuts, wantPuts[1]))) {
		t.Fatalf("recovered report: %+v, state: %+v, puts %v", report.Backups, m.State().Backups, puts)
	}
	s.Accounts = nil
	if report := inspect(t, m, s); len(report.Backups) != 0 || len(m.State().Backups) != 1 {
		t.Fatalf("report and last-pass scopes differ incorrectly: %+v, %+v", report.Backups, m.State().Backups)
	}
	runPass(t, m.Stream, s)
	if got := m.State().Backups; len(got) != 0 {
		t.Fatalf("last-pass scope: %+v", got)
	}
}

func TestMemorySnapshotSchedule(t *testing.T) {
	// R-YK9C-H3TR R-YE5U-K94A
	s := backupState()
	now := fixtureTime.Add(time.Millisecond)
	var puts []putCall
	putError := errors.New("injected config put error")
	m := backupMachine(&now, nil, func(c protocol.Credentials, key string) error {
		puts = append(puts, putCall{c, key})
		return putError
	})
	apply(t, m, s)
	good := s.Backup
	badBucket, badSettings, badCredentials := *good, *good, *good
	badBucket.Bucket = ""
	badSettings.Settings.EveryMinutes = 0
	badCredentials.Accounts, badCredentials.Host = nil, protocol.Credentials{}
	for _, b := range []*protocol.Backup{nil, &badBucket, &badSettings, &badCredentials} {
		s.Backup = b
		runPass(t, m.Backup, s)
		if got := m.State().Backups[0]; got.Error != machine.NoCredentials || len(puts) != 0 || !got.Snapshot.IsZero() {
			t.Fatalf("missing account credentials backed up: %+v, %v", got, puts)
		}
	}
	s.Backup = good
	runPass(t, m.Backup, s)
	wantPuts := []putCall{{good.Accounts[0].Credentials, protocol.FilesPrefix("acme") + "/config"}, {good.Host, good.HostPrefix + "config"}}
	r := inspect(t, m, s)
	if !maps.Equal(putCounts(puts), putCounts(wantPuts)) || r.HostBackup.Error != putError.Error() || !r.HostBackup.Snapshot.IsZero() ||
		r.Backups[0].Error != putError.Error() || r.Backups[0].Checked != fixtureTime || !r.Backups[0].Snapshot.IsZero() {
		t.Fatalf("failed backup: %+v, host %+v, puts %v", r.Backups, r.HostBackup, puts)
	}
	putError = nil
	runPass(t, m.Backup, s)
	now = now.Add(59 * time.Minute)
	runPass(t, m.Backup, s)
	if !maps.Equal(putCounts(puts), putCounts(append(wantPuts, wantPuts...))) {
		t.Fatalf("puts before next snapshot due: %v", puts)
	}
	now = now.Add(time.Minute)
	m.FailBackups("acme", true)
	runPass(t, m.Backup, s)
	if got := inspect(t, m, s); got.Backups[0].Snapshot != fixtureTime || got.Backups[0].Error == "" ||
		got.HostBackup.Snapshot != fixtureTime.Add(time.Hour) || got.HostBackup.Error != "" || len(puts) != 5 {
		t.Fatalf("backup with failing account: %+v, host %+v, puts %v", got.Backups, got.HostBackup, puts)
	}
	m.FailBackups("acme", false)
	for range protocol.MaxSnapshots + 1 {
		runPass(t, m.Backup, s)
		now = now.Add(time.Hour)
	}
	wantCounts := map[putCall]int{wantPuts[0]: protocol.MaxSnapshots + 3, wantPuts[1]: protocol.MaxSnapshots + 3}
	if got := putCounts(puts); !maps.Equal(got, wantCounts) {
		t.Fatalf("scheduled put counts = %v, want %v", got, wantCounts)
	}
	snap := m.State()
	got := snap.Backups[0]
	at := now.Add(-time.Hour).UTC().Truncate(time.Second)
	if got.Snapshot != at || len(got.Snapshots) != protocol.MaxSnapshots || got.Checked != fixtureTime || got.Error != "" {
		t.Fatalf("scheduled backup: %+v", got)
	}
	for i, ts := range got.Snapshots {
		if want := at.Add(-time.Duration(i) * time.Hour).Unix(); ts != want {
			t.Fatalf("snapshot %d = %d, want %d", i, ts, want)
		}
	}
	snap.Backups[0].Snapshots[0] = 0
	if got := m.State().Backups[0].Snapshots[0]; got != at.Unix() {
		t.Fatalf("snapshot slice aliases machine state: %d", got)
	}
	now = fixtureTime.Add(48 * time.Hour)
	runPass(t, m.Backup, s)
	if got := m.State().Backups[0].Checked; got != now {
		t.Fatalf("next check = %v, want %v", got, now)
	}
}

func TestMemoryInspectDatabaseAggregate(t *testing.T) {
	// R-YP4Y-06SJ
	s := backupState()
	disks := machine.NewDisks()
	early, late := fixtureTime, fixtureTime.Add(time.Minute)
	const firstPath, laterPath = "data/older.db", "data/newer.db"
	other := backupMachine(&late, disks, func(_ protocol.Credentials, key string) error {
		if key == protocol.AccountPrefix("acme")+firstPath {
			return errors.New("the earlier write remains in flight")
		}
		return nil
	})
	var m *machine.Memory
	m = backupMachine(&early, disks, func(_ protocol.Credentials, key string) error {
		if key == protocol.AccountPrefix("acme")+firstPath {
			other.AddDatabase("acme", laterPath, true)
			runPass(t, other.Stream, s)
			inFlight := inspect(t, m, s).Backups[0]
			if inFlight.Synced != late || inFlight.Written != late {
				t.Errorf("aggregate with a single nonzero time: %+v", inFlight)
			}
		}
		return nil
	})
	apply(t, m, s)
	apply(t, other, s)
	m.AddDatabase("acme", "data/0-non-wal.db", false)
	m.AddDatabase("acme", firstPath, true)
	runPass(t, m.Stream, s)
	got := inspect(t, m, s).Backups[0]
	if got.Synced != early || got.Written != late || len(got.Databases) != 3 ||
		!got.Databases[0].Synced.IsZero() || !got.Databases[0].Written.IsZero() ||
		got.Databases[1].Synced != late || got.Databases[1].Written != late ||
		got.Databases[2].Synced != early || got.Databases[2].Written != early {
		t.Fatalf("aggregate with differing times: %+v", got)
	}
}

func TestMemoryConcurrentPasses(t *testing.T) {
	// R-YHTJ-PKCD R-YCXY-6HDL
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	disks := machine.NewDisks()
	s := backupState()
	started, release := make(chan struct{}), make(chan struct{})
	releaseSleep := sync.OnceFunc(func() { close(release) })
	defer releaseSleep()
	var firstSleep sync.Once
	m := machine.NewMemory(machine.MemoryConfig{
		Disks: disks,
		Now:   func() time.Time { return fixtureTime },
		Sleep: func(ctx context.Context, _ time.Duration) {
			firstSleep.Do(func() {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
			})
		},
	})
	result := make(chan error, 1)
	go func() { result <- m.Apply(ctx, s) }()
	awaitMachineTest(ctx, t, started, "Apply to enter Sleep")
	observed := make(chan struct{})
	go func() {
		m.AddDatabase("acme", "data/ready.db", true)
		m.FailBackups("acme", true)
		_ = m.State()
		close(observed)
	}()
	awaitMachineTest(ctx, t, observed, "AddDatabase, FailBackups and State during Apply's Sleep")
	releaseSleep()
	if err := awaitMachineTest(ctx, t, result, "Apply to return after Sleep"); err != nil {
		t.Fatal(err)
	}
	now := fixtureTime
	other := backupMachine(&now, disks, nil)
	apply(t, other, protocol.DesiredState{DataVolume: s.DataVolume})
	const workers = 6
	finished := make(chan error, workers)
	launch := func(work func() error) {
		go func() { finished <- work() }()
	}
	launch(func() error {
		for i := 0; i < 20 && ctx.Err() == nil; i++ {
			m.AddDatabase("acme", "data/ready.db", true)
			m.FailBackups("acme", false)
			_ = m.State()
		}
		return ctx.Err()
	})
	for _, pass := range []func(context.Context, protocol.DesiredState) error{m.Apply, m.Stream, other.Stream, m.Backup} {
		launch(func() error {
			for i := 0; i < 20 && ctx.Err() == nil; i++ {
				if err := pass(ctx, s); err != nil {
					return err
				}
			}
			return ctx.Err()
		})
	}
	launch(func() error {
		for i := 0; i < 20 && ctx.Err() == nil; i++ {
			if err := other.Apply(ctx, s); err != nil {
				return err
			}
			if _, err := m.Inspect(ctx, s); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	for range workers {
		if err := awaitMachineTest(ctx, t, finished, "a concurrent machine worker"); err != nil {
			t.Error(err)
		}
	}
}
