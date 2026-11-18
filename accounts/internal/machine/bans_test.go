package machine_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestMemoryBanLifecycle(t *testing.T) {
	// R-ZTND-DZE7 R-W8S5-OZKJ R-ZW36-5IVL R-ZUV9-RR4W R-BFDK-ZTUM
	if duration, ok := any(machine.MemoryBanTime).(time.Duration); !ok || duration != 10*time.Minute {
		t.Fatalf("MemoryBanTime = %s", machine.MemoryBanTime)
	}
	base := time.Date(2026, 11, 18, 12, 0, 0, 123, time.FixedZone("test zone", 3600))
	now := base
	m := machine.NewMemory(machine.MemoryConfig{
		Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) {},
	})
	check := func(allowlist []string, bans ...protocol.Ban) {
		t.Helper()
		state := m.State()
		if !slices.Equal(state.Allowlist, allowlist) || !slices.Equal(state.Bans, bans) {
			t.Fatalf("allowlist and bans = %v, %v, want %v, %v", state.Allowlist, state.Bans, allowlist, bans)
		}
	}
	ban := func(address string, at time.Time) protocol.Ban {
		at = at.UTC().Truncate(time.Second)
		return protocol.Ban{Address: address, Banned: at, Expires: at.Add(10 * time.Minute)}
	}
	check(nil)
	for _, invalid := range []string{"::ffff:192.0.2.77", "192.0.2.077", "2001:DB8::1"} {
		m.Ban(invalid)
	}
	check(nil)

	const covered, lifted, expires = "192.0.2.77", "198.51.100.7", "2001:db8::2"
	m.Ban(lifted)
	m.Ban(covered)
	m.Ban(expires)
	check(nil, ban(covered, base), ban(lifted, base), ban(expires, base))
	now = now.Add(time.Second)
	m.Ban(lifted)
	check(nil, ban(lifted, now), ban(covered, base), ban(expires, base))

	failure := errors.New("simulated machine failure")
	m.SetFailure(failure)
	state := protocol.DesiredState{Allowlist: []string{"2001:db8::1", "192.0.2.0/24"}, Unban: []string{lifted}}
	if err := m.Apply(t.Context(), state); !errors.Is(err, failure) {
		t.Fatalf("Apply failure = %v", err)
	}
	check(nil, ban(lifted, now), ban(covered, base), ban(expires, base))
	m.SetFailure(nil)

	state.Unban = nil
	apply(t, m, state)
	wantAllowed := []string{"192.0.2.0/24", "2001:db8::1"}
	m.Ban(covered)
	m.Ban("2001:db8::1")
	check(wantAllowed, ban(lifted, now), ban(expires, base))
	snapshot := m.State()
	snapshot.Allowlist[0], snapshot.Bans[0].Address = "192.0.3.0/24", "192.0.3.7"
	state.Allowlist[0] = "2001:db8::2"
	check(wantAllowed, ban(lifted, now), ban(expires, base))

	apply(t, m, protocol.DesiredState{Unban: []string{lifted}})
	check(nil, ban(expires, base))
	now = base.Truncate(time.Second).Add(9*time.Minute + 59*time.Second)
	check(nil, ban(expires, base))
	now = now.Add(time.Second)
	check(nil)
}

func TestMemoryBanCap(t *testing.T) {
	// R-ZUV9-RR4W
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	now := base
	m := machine.NewMemory(machine.MemoryConfig{Now: func() time.Time { return now }})
	for i := 0; i < protocol.MaxBans; i++ {
		m.Ban(fmt.Sprintf("2001:db8::%x", i+1))
	}
	const newAddress = "198.51.100.7"
	m.Ban(newAddress)
	before := m.State().Bans
	if len(before) != protocol.MaxBans || slices.ContainsFunc(before, func(b protocol.Ban) bool { return b.Address == newAddress }) {
		t.Fatalf("bans at capacity = %v", before)
	}
	now = now.Add(time.Second)
	m.Ban("2001:db8::1")
	after := m.State().Bans
	if len(after) != protocol.MaxBans || after[0].Address != "2001:db8::1" || after[0].Banned != now || !slices.Equal(after[1:], before[1:]) {
		t.Fatal("renewing a ban at capacity must replace the ban")
	}
	now = base.Add(machine.MemoryBanTime)
	m.Ban(newAddress)
	after = m.State().Bans
	if len(after) != 2 || after[0].Address != newAddress || after[1].Address != "2001:db8::1" {
		t.Fatalf("bans after expiry made room = %v", after)
	}
}

func TestMemoryBanDuringApplyDelay(t *testing.T) {
	// R-RSO6-D0Y7
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	sleeping, release := make(chan struct{}), make(chan struct{})
	m := machine.NewMemory(machine.MemoryConfig{
		Now: func() time.Time { return time.Unix(48, 0) },
		Sleep: func(ctx context.Context, _ time.Duration) {
			close(sleeping)
			select {
			case <-release:
			case <-ctx.Done():
			}
		},
	})
	applied := make(chan error, 1)
	go func() {
		err := m.Apply(ctx, protocol.DesiredState{Allowlist: []string{"192.0.2.0/24"}})
		if err == nil {
			_, err = m.Inspect(ctx, protocol.DesiredState{})
		}
		applied <- err
	}()
	select {
	case <-sleeping:
	case <-ctx.Done():
		t.Fatal("Apply did not call Sleep")
	}

	observed, watched := make(chan machine.Snapshot, 1), make(chan struct{})
	go func() {
		m.Ban("192.0.2.77")
		observed <- m.State()
		for i := 0; i < protocol.MaxBans; i++ {
			m.Ban("192.0.2.77")
			m.State()
		}
		close(watched)
	}()
	select {
	case state := <-observed:
		if len(state.Bans) != 1 || state.Bans[0].Address != "192.0.2.77" {
			t.Fatalf("ban was not visible during Apply's delay: %+v", state)
		}
	case <-ctx.Done():
		t.Fatal("Ban or State waited for Apply's Sleep")
	}
	close(release)
	select {
	case err := <-applied:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Apply and Inspect did not finish")
	}
	select {
	case <-watched:
	case <-ctx.Done():
		t.Fatal("Ban and State did not finish")
	}
	if state := m.State(); len(state.Bans) != 0 {
		t.Fatalf("Apply did not lift the ban created during its delay: %+v", state)
	}
}
