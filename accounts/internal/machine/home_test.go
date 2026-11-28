package machine_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// R-GHQA-005H
func TestHomeTokens(t *testing.T) {
	type fillHome func(io.Reader, string, string) io.Reader
	var fill fillHome = machine.FillHome
	type token string
	const user, account token = machine.UserToken, machine.AccountToken
	if user != "<user>" || account != "<account>" {
		t.Fatalf("tokens = %q and %q", user, account)
	}
	got, err := io.ReadAll(fill(strings.NewReader(string(user+account)), "ada", "acme"))
	if string(got) != "adaacme" || err != nil {
		t.Fatalf("token substitution = %q, %v", got, err)
	}
}

// R-4EHC-R80B
func TestFillHome(t *testing.T) {
	const sample = "<user> (x y) <users> <User> <account <account> <us<user> <acc"
	const sampleWant = "ada (x y) <users> <User> <account acme <usada <acc"
	for _, tc := range []struct {
		name, input, user, account, want string
	}{
		{"example", sample, "ada", "acme", sampleWant},
		{"adjacent and binary", "\x00<account><user><user>\xff<account><", "longer-than-the-token", "", "\x00longer-than-the-tokenlonger-than-the-token\xff<"},
		{"invalid overlaps", "<<account><a<user>> <accounts> <user <accoun", "ada", "acme", "<acme<aada> <accounts> <user <accoun"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, wrap := range []func(io.Reader) io.Reader{iotest.OneByteReader, iotest.HalfReader, iotest.DataErrReader} {
				for _, size := range []int{1, 5, 32 * 1024} {
					var output strings.Builder
					filled := machine.FillHome(wrap(strings.NewReader(tc.input)), tc.user, tc.account)
					_, err := io.CopyBuffer(&output, filled, make([]byte, size))
					if err != nil || output.String() != tc.want {
						t.Fatalf("read with %d-byte buffer = %q, %v, want %q", size, output.String(), err, tc.want)
					}
				}
			}
		})
	}
	t.Run("read error", func(t *testing.T) {
		failure := errors.New("injected read failure")
		source := iotest.DataErrReader(io.MultiReader(strings.NewReader("<account> and <us"), iotest.ErrReader(failure)))
		got, err := io.ReadAll(machine.FillHome(source, "ada", "acme"))
		if string(got) != "acme and <us" || !errors.Is(err, failure) || errors.Is(err, io.EOF) {
			t.Fatalf("read to error = %q, %v", got, err)
		}
	})
}

// repeatedHomeInput generates a fixed-length prefix of a repeated pattern.
type repeatedHomeInput struct {
	pattern   string
	offset    int
	remaining int
}

func (r *repeatedHomeInput) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	for written := 0; written < n; {
		copied := copy(p[written:n], r.pattern[r.offset:])
		written += copied
		r.offset = (r.offset + copied) % len(r.pattern)
	}
	r.remaining -= n
	return n, nil
}

// R-4GX5-IRHP
func TestFillHomeBoundedAllocation(t *testing.T) {
	const inputSize = 32 * 1024 * 1024
	const user = "abcdefghijklmnopqrstuvwxyz012345"
	source := io.MultiReader(
		&repeatedHomeInput{pattern: "<accoun", remaining: inputSize},
		&repeatedHomeInput{pattern: machine.UserToken, remaining: inputSize},
	)
	buffer := make([]byte, 32*1024)
	discard := struct{ io.Writer }{io.Discard}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	n, err := io.CopyBuffer(discard, machine.FillHome(source, user, "acme"), buffer)
	runtime.ReadMemStats(&after)
	const want = int64(inputSize + inputSize/len(machine.UserToken)*len(user) + inputSize%len(machine.UserToken))
	if err != nil || n != want {
		t.Fatalf("copied %d bytes, %v, want %d bytes", n, err, want)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 16*1024*1024 {
		t.Fatalf("substitution allocated %d bytes, want less than 16 MiB", allocated)
	}
}

// R-XJ31-1JYY
func TestMemoryHomes(t *testing.T) {
	homeFor := func(user protocol.User) machine.Home {
		return machine.Home{
			User: user.Name, Account: user.Account, Files: machine.MemoryHomeFiles(), Commit: machine.MemoryTemplateCommit,
		}
	}
	check := func(m *machine.Memory, want []machine.Home) {
		t.Helper()
		if got := m.State().Homes; !slices.EqualFunc(got, want, func(a, b machine.Home) bool {
			return a.User == b.User && a.Account == b.Account && slices.Equal(a.Files, b.Files) && a.Commit == b.Commit
		}) {
			t.Fatalf("homes = %+v, want %+v", got, want)
		}
	}
	const volume = "vol-first"
	disks := machine.NewDisks()
	newMachine := func() *machine.Memory {
		return machine.NewMemory(machine.MemoryConfig{
			Sleep: func(context.Context, time.Duration) {},
			Now:   func() time.Time { return time.Unix(48, 1234) },
			Disks: disks,
		})
	}
	m := newMachine()
	check(m, nil)
	apply(t, m, protocol.DesiredState{DataVolume: volume})
	check(m, nil)
	zoe := protocol.User{Name: "zoe", Account: "zebra", UID: 20002, State: protocol.UserLocked}
	ada := protocol.User{Name: "ada", Account: "acme", UID: 20003, State: protocol.UserActive}
	state := protocol.DesiredState{DataVolume: volume, Users: []protocol.User{zoe, ada}}
	want := []machine.Home{homeFor(ada), homeFor(zoe)}
	apply(t, m, state)
	check(m, want)
	apply(t, m, state)
	check(m, want)
	snapshot := m.State()
	snapshot.Homes[0].Files[0] = "changed"
	snapshot.Homes[1] = machine.Home{}
	check(m, want)

	replacement := newMachine()
	deleted := ada
	deleted.State = protocol.UserDeleted
	apply(t, replacement, protocol.DesiredState{DataVolume: volume, Users: []protocol.User{deleted}})
	check(replacement, want)
	changed := ada
	changed.Account = "other"
	apply(t, replacement, protocol.DesiredState{DataVolume: volume, Users: []protocol.User{changed}})
	check(replacement, want)
	apply(t, replacement, protocol.DesiredState{DataVolume: "vol-next", Users: []protocol.User{changed}})
	check(replacement, nil)
	check(m, want)

	failure := errors.New("injected apply failure")
	m.SetFailure(failure)
	newUser := protocol.User{Name: "bess", Account: "acme", UID: 20004, State: protocol.UserActive}
	update := protocol.DesiredState{DataVolume: volume, Users: []protocol.User{deleted, newUser}}
	if err := m.Apply(t.Context(), update); !errors.Is(err, failure) {
		t.Fatalf("failed Apply = %v, want %v", err, failure)
	}
	check(m, want)
	m.SetFailure(nil)
	apply(t, m, update)
	check(m, []machine.Home{homeFor(newUser), want[1]})
	apply(t, m, protocol.DesiredState{})
	check(m, nil)
}
