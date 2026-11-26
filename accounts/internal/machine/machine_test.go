package machine_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

var _ machine.Machine = (*machine.Memory)(nil)

func TestMachineContracts(t *testing.T) {
	// R-B9A3-2Z55 R-7QOL-B9O2 R-BBPV-UIMJ
	const delay = 33 * time.Second
	ctx, cancel := context.WithCancel(t.Context())
	var sleeps int
	now := time.Date(2026, 2, 3, 4, 5, 6, 7, time.FixedZone("test", -3600))
	var m machine.Machine = machine.NewMemory(machine.MemoryConfig{
		ApplyDelay: delay,
		Sleep: func(received context.Context, d time.Duration) {
			sleeps++
			if received != ctx || d != delay {
				t.Fatalf("Sleep = %v, %v", received, d)
			}
			cancel()
		},
		Now: func() time.Time { return now },
	})
	if err := m.Apply(ctx, protocol.DesiredState{}); !errors.Is(err, context.Canceled) || sleeps != 1 {
		t.Fatalf("cancelled Apply = %v, sleeps = %d", err, sleeps)
	}
	if report, err := m.Inspect(ctx, protocol.DesiredState{}); err != nil || report.Booted != now.UTC().Truncate(time.Second) {
		t.Fatalf("Inspect = %+v, %v", report, err)
	}
	if err := m.InstallDaemon(t.Context(), strings.NewReader(""), ""); !errors.Is(err, machine.ErrNotExecutable) {
		t.Fatalf("InstallDaemon with empty body = %v", err)
	}
	if got := m.(*machine.Memory).State(); !reflect.DeepEqual(got, machine.Snapshot{
		Accounts: nil, Users: nil, Folders: nil, Daemon: "", Applies: 0, Allowlist: nil, Bans: nil,
	}) {
		t.Fatalf("initial snapshot = %+v", got)
	}
	if state := machine.NewMemory(machine.MemoryConfig{}).State(); state.Applies != 0 {
		t.Fatalf("default MemoryConfig state = %+v", state)
	}
	errs := []error{
		machine.ErrNoDataVolume, machine.ErrDigestMismatch, machine.ErrNotExecutable,
		machine.ErrTooLarge, machine.ErrIdentity, machine.ErrInvalidState,
	}
	for i, err := range errs {
		if err == nil {
			t.Fatalf("error at index %d is nil", i)
		}
		for _, prior := range errs[:i] {
			if errors.Is(err, prior) || errors.Is(prior, err) {
				t.Fatalf("error at index %d matches an earlier error", i)
			}
		}
	}
}

func fixedMachine() *machine.Memory {
	return machine.NewMemory(machine.MemoryConfig{
		Sleep: func(context.Context, time.Duration) {},
		Now:   func() time.Time { return time.Unix(48, 1234) },
	})
}

func apply(t *testing.T, m *machine.Memory, s protocol.DesiredState) {
	t.Helper()
	if err := m.Apply(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryApplyAndSnapshot(t *testing.T) {
	// R-BFDK-ZTUM R-BGLH-DLLB R-ZKBA-QWDO R-ZLJ7-4O4D
	m := fixedMachine()
	if got := m.State().Hostname; got != "" {
		t.Fatalf("initial hostname = %q", got)
	}
	first := protocol.DesiredState{
		Host:       "host01",
		Domain:     "hosts.example.com",
		DataVolume: "vol-00000001",
		Accounts: []protocol.Account{
			{Slug: "zebra", GID: 20000, State: protocol.AccountActive},
			{Slug: "alpaca", GID: 20001, State: protocol.AccountActive},
		},
		Users: []protocol.User{
			{Name: "zoe", Account: "zebra", UID: 20002, State: protocol.UserLocked, Keys: []string{"z", "a"}},
			{Name: "alice", Account: "alpaca", UID: 20003, State: protocol.UserActive, Keys: []string{"b"}},
			{Name: "eric", Account: "alpaca", UID: 20004, State: protocol.UserActive},
		},
	}
	apply(t, m, first)
	want := machine.Snapshot{
		Accounts: []machine.Account{{Slug: "alpaca", GID: 20001}, {Slug: "zebra", GID: 20000}},
		Users: []machine.User{
			{Name: "alice", Account: "alpaca", UID: 20003, Keys: []string{"b"}},
			{Name: "eric", Account: "alpaca", UID: 20004},
			{Name: "zoe", Account: "zebra", UID: 20002, Locked: true, Keys: []string{"z", "a"}},
		},
		Folders: []string{"alpaca", "zebra"}, Applies: 1,
		Hostname: "host01.hosts.example.com",
		Seeds:    m.State().Seeds, // Seeding is covered by TestMemorySeeds.
	}
	got := m.State()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("State = %+v, want %+v", got, want)
	}
	first.Users[0].Keys[0] = "unrecorded"
	got.Users[0].Keys[0] = "modified"
	got.Users[1].Name = ""
	got.Folders[0], got.Accounts[0].Slug = "", ""
	if again := m.State(); !reflect.DeepEqual(again, want) {
		t.Fatalf("State aliases mutable slices: %+v", again)
	}
	failure := errors.New("injected")
	m.SetFailure(failure)
	if err := m.Apply(t.Context(), protocol.DesiredState{Host: "failed", Domain: first.Domain}); !errors.Is(err, failure) {
		t.Fatalf("faulted Apply = %v", err)
	}
	m.SetFailure(nil)
	if err := m.Apply(t.Context(), protocol.DesiredState{Host: "failed", Domain: first.Domain, Users: first.Users}); !errors.Is(err, machine.ErrNoDataVolume) {
		t.Fatalf("Apply without volume = %v", err)
	}
	if after := m.State(); !reflect.DeepEqual(after, want) {
		t.Fatalf("failed Apply changed machine: %+v", after)
	}
	apply(t, m, protocol.DesiredState{
		DataVolume: first.DataVolume,
		Accounts:   []protocol.Account{{Slug: "zebra", GID: 20000, State: protocol.AccountRemoved}},
		Users: []protocol.User{
			{Name: "zoe", State: protocol.UserDeleted},
			{Name: "eric", Account: "alpaca", UID: 20004, State: protocol.UserLocked, Keys: []string{"new"}},
		},
	})
	want.Accounts = want.Accounts[:1]
	want.Users = want.Users[:2]
	want.Users[0].Locked, want.Users[1].Locked = true, true
	want.Users[1].Keys = []string{"new"}
	want.Applies++
	if got := m.State(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reconciled state = %+v, want %+v", got, want)
	}
	apply(t, m, protocol.DesiredState{Host: "host02", Domain: "hosts2.example.com"})
	if got := m.State().Hostname; got != "host02.hosts2.example.com" {
		t.Fatalf("updated hostname = %q", got)
	}
}

// R-QZUK-JFN7
func TestSnapshotShape(_ *testing.T) {
	var _ struct {
		Accounts  []machine.Account
		Users     []machine.User
		Folders   []string
		Daemon    string
		Applies   int
		Allowlist []string
		Bans      []protocol.Ban
		Hostname  string
		Seeds     []machine.Seed
	} = fixedMachine().State()
	var _ struct {
		Slug string
		GID  int64
	} = machine.Account{}
	var _ struct {
		Name, Account string
		UID           int64
		Locked        bool
		Keys          []string
	} = machine.User{}
	var _ struct {
		Account                string
		Files                  []string
		Commits                int
		Author, Email, Message string
	} = machine.Seed{}
}

// R-R12G-X7DW R-H6HF-3CTF
func TestMemorySeeds(t *testing.T) {
	const message string = machine.SeedMessage
	if message == "" || strings.ContainsAny(message, "\r\n") {
		t.Fatalf("SeedMessage = %q, want a nonempty line", message)
	}
	var paths []string
	if err := fs.WalkDir(accounts.Seed(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(paths)
	m := fixedMachine()
	if got := m.State().Seeds; len(got) != 0 {
		t.Fatalf("initial seeds = %+v", got)
	}
	state := protocol.DesiredState{
		Host: "host01", Domain: "hosts.example.com", DataVolume: "vol-00000001",
		Accounts: []protocol.Account{
			{Slug: "zebra", GID: 20000, State: protocol.AccountActive},
			{Slug: "alpaca", GID: 20001, State: protocol.AccountRemoved},
		},
	}
	apply(t, m, state)
	zebra := machine.Seed{Account: "zebra", Files: paths, Commits: 1, Author: "zebra", Email: "zebra@host01.hosts.example.com", Message: message}
	if got := m.State().Seeds; !reflect.DeepEqual(got, []machine.Seed{zebra}) {
		t.Fatalf("seeds after first apply = %+v, want %+v", got, zebra)
	}
	state.Accounts[0].State = protocol.AccountRemoved
	apply(t, m, state)
	if got := m.State().Seeds; !reflect.DeepEqual(got, []machine.Seed{zebra}) {
		t.Fatalf("seeds after removal = %+v, want %+v", got, zebra)
	}
	state.Host, state.Domain = "host02", ""
	state.Accounts[0].State = protocol.AccountActive
	state.Accounts[1].State = protocol.AccountActive
	state.DataVolume = ""
	if err := m.Apply(t.Context(), state); !errors.Is(err, machine.ErrNoDataVolume) {
		t.Fatalf("apply without data volume = %v", err)
	}
	if got := m.State().Seeds; !reflect.DeepEqual(got, []machine.Seed{zebra}) {
		t.Fatalf("seeds after failed apply = %+v, want %+v", got, zebra)
	}
	state.DataVolume = "vol-00000001"
	apply(t, m, state)
	want := []machine.Seed{
		{Account: "alpaca", Files: paths, Commits: 1, Author: "alpaca", Email: "alpaca@host02", Message: message},
		zebra,
	}
	snapshot := m.State()
	if !reflect.DeepEqual(snapshot.Seeds, want) {
		t.Fatalf("seeds after reactivation and new account = %+v, want %+v", snapshot.Seeds, want)
	}
	snapshot.Seeds[0].Files[0] = "changed"
	snapshot.Seeds[1] = machine.Seed{}
	if got := m.State().Seeds; !reflect.DeepEqual(got, want) {
		t.Fatalf("seeds after modifying snapshot = %+v, want %+v", got, want)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	failure := errors.New("injected")
	for _, tc := range []struct {
		slug    string
		gid     int64
		ctx     context.Context
		failure error
		wantErr error
	}{
		{slug: "bison", gid: 20002, ctx: canceled, wantErr: context.Canceled},
		{slug: "camel", gid: 20003, ctx: t.Context(), failure: failure, wantErr: failure},
	} {
		state.Accounts = []protocol.Account{{Slug: tc.slug, GID: tc.gid, State: protocol.AccountActive}}
		m.SetFailure(tc.failure)
		if err := m.Apply(tc.ctx, state); !errors.Is(err, tc.wantErr) {
			t.Fatalf("apply for %s = %v, want %v", tc.slug, err, tc.wantErr)
		}
		if got := m.State().Seeds; !reflect.DeepEqual(got, want) {
			t.Fatalf("seeds after failed apply for %s = %+v, want %+v", tc.slug, got, want)
		}
		m.SetFailure(nil)
		apply(t, m, state)
		want = slices.Insert(want, len(want)-1, machine.Seed{
			Account: tc.slug, Files: paths, Commits: 1, Author: tc.slug, Email: tc.slug + "@host02", Message: message,
		})
		if got := m.State().Seeds; !reflect.DeepEqual(got, want) {
			t.Fatalf("seeds after successful retry for %s = %+v, want %+v", tc.slug, got, want)
		}
	}
}

func TestMemoryInspect(t *testing.T) {
	// R-BE5O-M23X R-ZMR3-IFV2
	m := fixedMachine()
	s := protocol.DesiredState{Host: "host01", Domain: "hosts.example.com", RootGB: 20, DataGB: 30, DataVolume: "vol-00000001", Accounts: []protocol.Account{
		{Slug: "zebra", GID: 20000, State: protocol.AccountActive},
		{Slug: "alpaca", GID: 20001, State: protocol.AccountActive},
	}}
	apply(t, m, s)
	m.Ban("192.0.2.77")
	m.Ban("192.0.2.7")
	s.Allowlist = []string{"2001:db8::1", "198.51.100.0/24"}
	apply(t, m, s)
	if machine.MemoryUsed != 512<<20 || machine.MemoryAvailable != 3<<30 ||
		machine.MemoryDiskUsed != 1<<30 || machine.MemoryInodesUsed != 50000 || machine.MemoryInodesTotal != 10000000 {
		t.Fatal("memory machine fixture constants differ from their specified values")
	}
	want := protocol.Report{
		OS: machine.MemoryOS, Kernel: machine.MemoryKernel, Booted: time.Unix(48, 0).UTC(),
		MemoryUsed: machine.MemoryUsed, MemoryAvailable: machine.MemoryAvailable,
		Root:   protocol.Disk{UsedBytes: machine.MemoryDiskUsed, SizeBytes: 20 << 30, InodesUsed: machine.MemoryInodesUsed, InodesTotal: machine.MemoryInodesTotal},
		Data:   protocol.Disk{UsedBytes: machine.MemoryDiskUsed, SizeBytes: 30 << 30, InodesUsed: machine.MemoryInodesUsed, InodesTotal: machine.MemoryInodesTotal},
		Slices: []protocol.Slice{{Account: "alpaca"}, {Account: "zebra"}},
		Bans: []protocol.Ban{
			{Address: "192.0.2.7", Banned: time.Unix(48, 0).UTC(), Expires: time.Unix(648, 0).UTC()},
			{Address: "192.0.2.77", Banned: time.Unix(48, 0).UTC(), Expires: time.Unix(648, 0).UTC()},
		},
		BanTotal:  2,
		Allowlist: []string{"198.51.100.0/24", "2001:db8::1"},
		Hostname:  "host01.hosts.example.com",
	}
	s.Domain = ""
	for _, volume := range []string{s.DataVolume, ""} {
		s.DataVolume = volume
		if volume == "" {
			want.Data = protocol.Disk{}
		}
		if got, err := m.Inspect(t.Context(), s); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Inspect(%q) = %+v, %v, want %+v", volume, got, err, want)
		}
	}
}

type finiteReader struct {
	left    int
	largest int
}

func (r *finiteReader) Read(p []byte) (int, error) {
	r.largest = max(r.largest, len(p))
	if r.left == 0 {
		return 0, io.EOF
	}
	n := min(r.left, len(p))
	clear(p[:n])
	r.left -= n
	return n, nil
}

func TestMemoryInstallDaemon(t *testing.T) {
	// R-BLH2-WOK3
	m := fixedMachine()
	body := "\x7fELFpayload"
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))
	if err := m.InstallDaemon(t.Context(), strings.NewReader(body), digest); err != nil || m.State().Daemon != digest {
		t.Fatalf("valid install = %v, digest = %s", err, m.State().Daemon)
	}
	over := &finiteReader{left: protocol.MaxBinaryBytes + 2*protocol.DigestBuffer}
	for _, test := range []struct {
		body io.Reader
		err  error
	}{
		{over, machine.ErrTooLarge},
		{strings.NewReader("wrong format"), machine.ErrNotExecutable},
		{strings.NewReader(body + "!"), machine.ErrDigestMismatch},
	} {
		if err := m.InstallDaemon(t.Context(), test.body, digest); !errors.Is(err, test.err) || m.State().Daemon != digest {
			t.Fatalf("invalid install = %v, digest = %s, want %v", err, m.State().Daemon, test.err)
		}
	}
	if over.left != 0 || over.largest > protocol.DigestBuffer {
		t.Fatalf("oversize stream left = %d, read maximum = %d", over.left, over.largest)
	}
}
