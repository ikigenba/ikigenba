package store_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

func runningHost(t *testing.T, s *store.Store, instance string) store.Host {
	t.Helper()
	h, op := create(t, s)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{InstanceID: instance}), nil)
	return value(t, result(s.Host(ctx, h.Name)))
}

// R-1YS8-SMQF R-23NU-BPP7 R-24VQ-PHFW
func TestAllowlistEditsAndHostReads(t *testing.T) {
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	path := filepath.Join(t.TempDir(), "record.db")
	d, s := open(t, path, accounts.Migrations(), cfg)
	h, op := create(t, s)
	eq(t, h.Allowlist, []string{})
	const subnet, address = "198.51.100.0/24", "192.0.2.7"
	for _, edit := range []func(string, string) observation{
		func(host, entry string) observation { return observe(s.Allow(ctx, host, entry)) },
		func(host, entry string) observation { return observe(s.Disallow(ctx, host, entry)) },
	} {
		edit("missing", "198.51.100.1/24").assertZero(t, store.ErrUnreachable)
		edit("missing", subnet).assertZero(t, store.ErrNotFound)
	}
	eq(t, value(t, result(s.Host(ctx, h.Name))), h)
	want := h
	want.Version++
	want.Allowlist = []string{subnet}
	eq(t, value(t, result(s.Allow(ctx, h.Name, subnet))), want)
	eq(t, value(t, result(s.Allow(ctx, h.Name, subnet))), want)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{InstanceID: "instance"}), nil)
	_, stop := start(t, s, h.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	want = value(t, result(s.Host(ctx, h.Name)))
	eq(t, want.State, store.HostStopped)
	want.Version++
	want.Allowlist = []string{address, subnet}
	eq(t, value(t, result(s.Allow(ctx, h.Name, address))), want)
	checkErr(t, d.Close(), nil)
	_, s = open(t, path, accounts.Migrations(), cfg)
	eq(t, value(t, result(s.Host(ctx, h.Name))), want)
	eq(t, value(t, result(s.HostByInstance(ctx, "instance"))), want)
	eq(t, value(t, result(s.ListHosts(ctx, 0, 1))), []store.Host{want})
	eq(t, value(t, result(s.ListHostsBefore(ctx, 2, 1))), []store.Host{want})
	eq(t, value(t, result(s.LastHosts(ctx, 1))), []store.Host{want})
	_, resume := start(t, s, h.Name, store.OpStart, store.Target{})
	checkErr(t, s.Fail(ctx, resume.ID, "failed"), nil)
	want = value(t, result(s.Host(ctx, h.Name)))
	eq(t, want.State, store.HostFailed)
	watch, release := s.Watch(h.Name)
	defer release()
	want.Version++
	want.Allowlist = []string{subnet}
	eq(t, value(t, result(s.Disallow(ctx, h.Name, address))), want)
	closed(t, watch, true)
	watch, releaseAgain := s.Watch(h.Name)
	defer releaseAgain()
	eq(t, value(t, result(s.Disallow(ctx, h.Name, address))), want)
	closed(t, watch, false)
	want.Version++
	want.Allowlist = []string{address, subnet}
	eq(t, value(t, result(s.Allow(ctx, h.Name, address))), want)
	closed(t, watch, true)
	retrying, _, err := s.RetryHost(ctx, h.Name, caller)
	checkErr(t, err, nil)
	eq(t, retrying.Allowlist, want.Allowlist)
	checkErr(t, s.Finish(ctx, resume.ID, store.Update{}), nil)
	silent := value(t, result(s.MarkSilent(ctx, baseTime.Add(store.SilentAfter+time.Second), "", 1)))
	eq(t, len(silent), 1)
	eq(t, silent[0].Allowlist, want.Allowlist)
	deleting, deletion := start(t, s, h.Name, store.OpDelete, store.Target{})
	eq(t, deleting.Allowlist, want.Allowlist)
	observe(s.Allow(ctx, h.Name, address)).assertZero(t, store.ErrHostGone)
	observe(s.Disallow(ctx, h.Name, address)).assertZero(t, store.ErrHostGone)
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	observe(s.Allow(ctx, h.Name, address)).assertZero(t, store.ErrHostGone)
	observe(s.Disallow(ctx, h.Name, address)).assertZero(t, store.ErrHostGone)
	observe(s.Desired(ctx, h.Name)).assertZero(t, store.ErrNotFound)
}

// R-2005-6EH4 R-2181-K67T R-GRI0-S00X
func TestPendingUnbanGuardsAndDesiredCaps(t *testing.T) {
	_, s, _ := fixture(t)
	h := runningHost(t, s, "first")
	const ip = "192.0.2.1"
	observe(s.Unban(ctx, "missing", "2001:0DB8::1")).assertZero(t, store.ErrUnreachable)
	observe(s.Unban(ctx, "missing", ip)).assertZero(t, store.ErrNotFound)
	want := h
	for i := 1; i <= valid.MaxUnbansPerHost; i++ {
		want.Version++
		eq(t, value(t, result(s.Unban(ctx, h.Name, fmt.Sprintf("192.0.2.%d", i)))), want)
	}
	eq(t, value(t, result(s.Unban(ctx, h.Name, ip))), want)
	observe(s.Unban(ctx, h.Name, "198.51.100.7")).assertZero(t, store.ErrTooManyUnbans)
	for i := 1; i <= valid.MaxAllowlistPerHost; i++ {
		value(t, result(s.Allow(ctx, h.Name, fmt.Sprintf("198.51.100.%d", i))))
	}
	before := value(t, result(s.Host(ctx, h.Name)))
	eq(t, value(t, result(s.Allow(ctx, h.Name, "198.51.100.1"))), before)
	observe(s.Allow(ctx, h.Name, "192.0.2.1")).assertZero(t, store.ErrAllowlistFull)
	eq(t, value(t, result(s.Host(ctx, h.Name))), before)
	eq(t, len(before.Allowlist), valid.MaxAllowlistPerHost)
	desired := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, desired.Version, before.Version)
	eq(t, len(desired.Unban), valid.MaxUnbansPerHost)
	eq(t, desired.Allowlist, before.Allowlist)
	_, stop := start(t, s, h.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	stopped := value(t, result(s.Host(ctx, h.Name)))
	observe(s.Unban(ctx, h.Name, ip)).assertZero(t, store.ErrNotRunning)
	eq(t, value(t, result(s.Host(ctx, h.Name))), stopped)
	_, deletion := start(t, s, h.Name, store.OpDelete, store.Target{})
	checkErr(t, s.Finish(ctx, deletion.ID, store.Update{}), nil)
	observe(s.Unban(ctx, h.Name, ip)).assertZero(t, store.ErrHostGone)
}

// R-22FX-XXYI R-J6DY-Y834
func TestUnbanRunningAppliesToTheWholeRunningSet(t *testing.T) {
	_, s, _ := fixture(t)
	const ip = "192.0.2.7"
	eq(t, value(t, result(s.UnbanRunning(ctx, ip))), 0)
	observe(s.UnbanRunning(ctx, "2001:0DB8::1")).assertZero(t, store.ErrUnreachable)
	a := runningHost(t, s, "first")
	b := runningHost(t, s, "second")
	c := runningHost(t, s, "third")
	_, stop := start(t, s, c.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	c = value(t, result(s.Host(ctx, c.Name)))
	for i := 1; i <= valid.MaxUnbansPerHost; i++ {
		b = value(t, result(s.Unban(ctx, b.Name, fmt.Sprintf("198.51.100.%d", i))))
	}
	chA, releaseA := s.Watch(a.Name)
	defer releaseA()
	chC, releaseC := s.Watch(c.Name)
	defer releaseC()
	observe(s.UnbanRunning(ctx, ip)).assertZero(t, store.ErrTooManyUnbans)
	eq(t, value(t, result(s.Host(ctx, a.Name))), a)
	eq(t, value(t, result(s.Host(ctx, b.Name))), b)
	eq(t, value(t, result(s.Host(ctx, c.Name))), c)
	eq(t, value(t, result(s.Desired(ctx, a.Name))).Unban, []string{})
	closed(t, chA, false)
	value(t, result(s.RecordReport(ctx, b.Name, protocol.Report{Applied: b.Version}, baseTime)))
	firstWatchB, releaseFirstB := s.Watch(b.Name)
	defer releaseFirstB()
	b = value(t, result(s.Unban(ctx, b.Name, ip)))
	closed(t, firstWatchB, true)
	closed(t, chA, false)
	chB, releaseB := s.Watch(b.Name)
	defer releaseB()
	eq(t, value(t, result(s.UnbanRunning(ctx, ip))), 2)
	a.Version++
	eq(t, value(t, result(s.Host(ctx, a.Name))), a)
	eq(t, value(t, result(s.Host(ctx, b.Name))), b)
	eq(t, value(t, result(s.Host(ctx, c.Name))), c)
	eq(t, value(t, result(s.Desired(ctx, a.Name))).Unban, []string{ip})
	eq(t, value(t, result(s.Desired(ctx, b.Name))).Unban, []string{ip})
	eq(t, value(t, result(s.Desired(ctx, c.Name))).Unban, []string{})
	closed(t, chA, true)
	closed(t, chB, false)
	closed(t, chC, false)
	chA2, releaseA2 := s.Watch(a.Name)
	defer releaseA2()
	eq(t, value(t, result(s.UnbanRunning(ctx, "192.0.2.1"))), 2)
	closed(t, chA2, true)
	closed(t, chB, true)
	closed(t, chC, false)
}

// R-27BJ-H0XA R-263N-396L R-28JF-USNZ
func TestDesiredBanEditsAndReportPurge(t *testing.T) {
	_, s, now := fixture(t)
	h := runningHost(t, s, "host")
	before := value(t, result(s.Desired(ctx, h.Name)))
	wire := func(d protocol.DesiredState) map[string]json.RawMessage {
		t.Helper()
		encoded := value(t, result(json.Marshal(d)))
		var raw map[string]json.RawMessage
		checkErr(t, json.Unmarshal(encoded, &raw), nil)
		return raw
	}
	empty := wire(before)
	eq(t, string(empty["allowlist"]), "[]")
	eq(t, string(empty["unban"]), "[]")
	const lower, higher, subnet = "192.0.2.7", "198.51.100.7", "198.51.100.0/24"
	h = value(t, result(s.Allow(ctx, h.Name, subnet)))
	h = value(t, result(s.Allow(ctx, h.Name, lower)))
	first := value(t, result(s.Unban(ctx, h.Name, higher)))
	h = value(t, result(s.Unban(ctx, h.Name, lower)))
	want := before
	want.Version = before.Version + 4
	want.Allowlist = []string{lower, subnet}
	want.Unban = []string{lower, higher}
	eq(t, h.Version, want.Version)
	got := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, got, want)
	document := wire(got)
	eq(t, string(document["allowlist"]), `["192.0.2.7","198.51.100.0/24"]`)
	eq(t, string(document["unban"]), `["192.0.2.7","198.51.100.7"]`)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: first.Version - 1}, *now)))
	eq(t, value(t, result(s.Desired(ctx, h.Name))), want)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: h.Version, Error: "apply failed"}, *now)))
	eq(t, value(t, result(s.Desired(ctx, h.Name))), want)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: first.Version}, *now)))
	want.Unban = []string{lower}
	eq(t, value(t, result(s.Desired(ctx, h.Name))), want)
	value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{Applied: h.Version}, *now)))
	want.Unban = []string{}
	eq(t, value(t, result(s.Desired(ctx, h.Name))), want)
	h = value(t, result(s.Unban(ctx, h.Name, higher)))
	want.Version++
	want.Unban = []string{higher}
	eq(t, h.Version, want.Version)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), want)
}

// R-263N-396L
func TestDesiredOrdersAccountsUsersAndKeys(t *testing.T) {
	_, s, _ := fixture(t)
	h := runningHost(t, s, "host")
	pending, _ := place(t, s, "zebra", h.Name)
	active, activate := place(t, s, "active", h.Name)
	checkErr(t, s.Finish(ctx, activate.ID, store.Update{}), nil)
	failed, fail := place(t, s, "failed", h.Name)
	checkErr(t, s.Fail(ctx, fail.ID, "failed"), nil)
	removing, _ := place(t, s, "removing", h.Name)
	remove(t, s, removing.Slug)
	locked := value(t, result(s.AddUser(ctx, active.Slug, "zed", firstKey, caller)))
	key := store.Key{Type: firstKey.Type, Body: "a-key", Line: "ssh-ed25519 a-key second"}
	value(t, result(s.AddKey(ctx, active.Slug, locked.Name, key)))
	value(t, result(s.Lock(ctx, active.Slug, locked.Name)))
	user := value(t, result(s.AddUser(ctx, pending.Slug, "amy", firstKey, caller)))

	got := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, got.Accounts, []protocol.Account{
		{Slug: active.Slug, GID: active.GID, State: protocol.AccountActive},
		{Slug: failed.Slug, GID: failed.GID, State: protocol.AccountActive},
		{Slug: removing.Slug, GID: removing.GID, State: protocol.AccountRemoved},
		{Slug: pending.Slug, GID: pending.GID, State: protocol.AccountActive},
	})
	eq(t, got.Users, []protocol.User{
		{Name: user.Name, Account: pending.Slug, UID: user.UID, State: protocol.UserActive, Keys: []string{firstKey.Line}},
		{Name: locked.Name, Account: active.Slug, UID: locked.UID, State: protocol.UserLocked, Keys: []string{key.Line, firstKey.Line}},
	})
}
