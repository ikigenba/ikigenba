package host_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
)

var background = context.Background()

// exactHost supplies the reverse assignment that rejects extra Host methods.
type exactHost interface {
	GroupAdd(context.Context, string) error
	GroupRemove(context.Context, string) error
	GroupList(context.Context) ([]host.Group, error)
	GroupMembers(context.Context, string) (host.Members, error)
	UserAdd(context.Context, string, string, string) error
	UserList(context.Context) ([]host.User, error)
	UserKeyAdd(context.Context, string, string) error
	UserKeyRemove(context.Context, string, string) error
	UserJoin(context.Context, string, string) error
	UserLeave(context.Context, string, string) error
	UserLock(context.Context, string) error
	UserUnlock(context.Context, string) error
	UserDelete(context.Context, string) error
}

func publicKey(seed byte) string {
	b := make([]byte, 51)
	binary.BigEndian.PutUint32(b[:4], 11)
	copy(b[4:15], "ssh-ed25519")
	binary.BigEndian.PutUint32(b[15:19], 32)
	for i := 19; i < len(b); i++ {
		b[i] = seed
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(b) + " chosen comment"
}
func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func requireRefused(t *testing.T, err error, status int, diagnostic string) {
	t.Helper()
	var r *host.RefusedError
	var f *host.FailedError
	if !errors.As(err, &r) || errors.As(err, &f) || r.Status != status || r.Diagnostic != diagnostic {
		t.Fatalf("refusal got %#v, want %d %q", err, status, diagnostic)
	}
}
func requireFailed(t *testing.T, err error, status int) {
	t.Helper()
	var r *host.RefusedError
	var f *host.FailedError
	if !errors.As(err, &f) || errors.As(err, &r) || f.Status != status {
		t.Fatalf("failure got %#v, want %d", err, status)
	}
}

type invocation struct {
	name string
	run  func(host.Host) (any, error)
	zero any
}

func invocations(ctx context.Context, name, group, key string) []invocation {
	return []invocation{
		{"group add", func(h host.Host) (any, error) { return nil, h.GroupAdd(ctx, group) }, nil},
		{"group remove", func(h host.Host) (any, error) { return nil, h.GroupRemove(ctx, group) }, nil},
		{"group list", func(h host.Host) (any, error) { v, e := h.GroupList(ctx); return v, e }, []host.Group(nil)},
		{"group members", func(h host.Host) (any, error) { v, e := h.GroupMembers(ctx, group); return v, e }, host.Members{}},
		{"user add", func(h host.Host) (any, error) { return nil, h.UserAdd(ctx, name, group, key) }, nil},
		{"user list", func(h host.Host) (any, error) { v, e := h.UserList(ctx); return v, e }, []host.User(nil)},
		{"user key add", func(h host.Host) (any, error) { return nil, h.UserKeyAdd(ctx, name, key) }, nil},
		{"user key remove", func(h host.Host) (any, error) { return nil, h.UserKeyRemove(ctx, name, key) }, nil},
		{"user join", func(h host.Host) (any, error) { return nil, h.UserJoin(ctx, name, group) }, nil},
		{"user leave", func(h host.Host) (any, error) { return nil, h.UserLeave(ctx, name, group) }, nil},
		{"user lock", func(h host.Host) (any, error) { return nil, h.UserLock(ctx, name) }, nil},
		{"user unlock", func(h host.Host) (any, error) { return nil, h.UserUnlock(ctx, name) }, nil},
		{"user delete", func(h host.Host) (any, error) { return nil, h.UserDelete(ctx, name) }, nil},
	}
}

type snapshot struct {
	groups []host.Group
	users  []host.User
}

func snap(t *testing.T, h host.Host) snapshot {
	t.Helper()
	gs, e := h.GroupList(background)
	requireOK(t, e)
	us, e := h.UserList(background)
	requireOK(t, e)
	return snapshot{gs, us}
}
func unchanged(t *testing.T, h host.Host, want snapshot) {
	t.Helper()
	if got := snap(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed: %+v want %+v", got, want)
	}
}

// R-HLSK-9TXW R-HN0G-NLOL R-HO8D-1DFA R-HPG9-F55Z R-HQO5-SWWO
// R-HRW2-6OND R-HUBU-Y84R R-HVJR-BZVG R-HXZK-3JCU
// R-WH5O-XBBH R-WIDL-B326 R-IOTC-IHO4 R-I2V5-MMBM R-I432-0E2B
func TestPublicContracts(t *testing.T) {
	h := constructHosts(t, host.NewMemory, host.NewCommand)
	gs, e := h.GroupList(background)
	requireOK(t, e)
	us, e := h.UserList(background)
	requireOK(t, e)
	if gs == nil || us == nil || len(gs) != 0 || len(us) != 0 {
		t.Fatal(gs, us)
	}
	_ = host.Group{"chosen", 2}
	_ = host.Members{"chosen", []string{"person"}}
	_ = host.User{"person", []string{"chosen"}, []string{publicKey(1)}, host.StateActive, time.Time{}}
	if host.StateActive != "active" || host.StateLocked != "locked" || host.DefaultCommand != "sudo -n /usr/local/bin/opsctl" || host.DiagnosticPrefix != "opsctl: " {
		t.Fatal("protocol constants")
	}
	type state string
	protocol := []state{host.StateActive, host.StateLocked, host.DefaultCommand, host.DiagnosticPrefix}
	if len(protocol) != 4 {
		t.Fatal(protocol)
	}
	const (
		invalidName   = host.RefuseInvalidName
		invalidKey    = host.RefuseInvalidKey
		noGroup       = host.RefuseNoGroup
		noUser        = host.RefuseNoUser
		nameTaken     = host.RefuseNameTaken
		groupNotEmpty = host.RefuseGroupNotEmpty
		userNotLocked = host.RefuseUserNotLocked
		hostFailed    = host.HostFailed
	)
	seen := map[string]bool{}
	for _, s := range []string{invalidName, invalidKey, noGroup, noUser, nameTaken, groupNotEmpty, userNotLocked} {
		if s == "" || strings.ContainsAny(s, "\r\n") || seen[s] {
			t.Fatal("diagnostic contract", s)
		}
		seen[s] = true
	}
	if hostFailed == "" || strings.ContainsAny(hostFailed, "\r\n") {
		t.Fatal("failure copy")
	}
	for _, status := range []int{-1, 0, 1, 2, 4, 71} {
		r := &host.RefusedError{status, "  chosen\r\nline"}
		if r.Error() != r.Diagnostic {
			t.Fatal(r)
		}
		f := &host.FailedError{status}
		if f.Error() != host.HostFailed+" ("+strconv.Itoa(status)+")" {
			t.Fatal(f)
		}
	}
	for _, s := range []string{"", " \t\n", " /tmp/stub  -x 'a b' ", "a\\b\t\"word\"\nlast"} {
		want := strings.Fields(s)
		if len(want) == 0 {
			want = []string{"sudo", "-n", "/usr/local/bin/opsctl"}
		}
		if got := host.Fields(s); !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
}

func constructHosts(t *testing.T, newMemory func() *host.Memory, newCommand func([]string) *host.Command) host.Host {
	t.Helper()
	var exact exactHost = newMemory()
	var h host.Host = exact
	for _, typ := range []reflect.Type{reflect.TypeOf(*newMemory()), reflect.TypeOf(*newCommand(nil))} {
		for field := range typ.NumField() {
			if typ.Field(field).IsExported() {
				t.Fatalf("%v has exported field %s", typ, typ.Field(field).Name)
			}
		}
	}
	return h
}

// R-D9PT-VML3 R-IR95-A15I R-I1N9-8UKX R-J73U-91SJ
func TestMemoryValidationAndCancellation(t *testing.T) {
	m := host.NewMemory()
	key := publicKey(1)
	requireOK(t, m.GroupAdd(background, "business"))
	requireOK(t, m.UserAdd(background, "person", "business", key))
	before := snap(t, m)
	for _, invalid := range []string{"", "X", "1x", "a.b", "a/b", "é", strings.Repeat("a", 33), "a\x00b", "a\nb", "-x"} {
		for _, op := range invocations(background, invalid, invalid, "broken") {
			if op.name == "group list" || op.name == "user list" {
				continue
			}
			got, err := op.run(m)
			requireRefused(t, err, 2, host.RefuseInvalidName)
			if !reflect.DeepEqual(got, op.zero) {
				t.Fatal(op.name, got)
			}
			unchanged(t, m, before)
		}
		for _, op := range []func() error{func() error { return m.UserAdd(background, "person", invalid, key) }, func() error { return m.UserJoin(background, "person", invalid) }, func() error { return m.UserLeave(background, "person", invalid) }} {
			requireRefused(t, op(), 2, host.RefuseInvalidName)
			unchanged(t, m, before)
		}
	}
	for _, valid := range []string{"_", "_x", "a0_b-c", strings.Repeat("a", 32)} {
		requireOK(t, m.GroupAdd(background, valid))
	}
	before = snap(t, m)
	blob, _ := base64.StdEncoding.DecodeString(strings.Fields(key)[1])
	wrongType := append([]byte(nil), blob...)
	wrongType[4] = 'x'
	wrongLen := append([]byte(nil), blob...)
	wrongLen[18] = 31
	invalidKeys := []string{"", "ssh-rsa " + strings.Fields(key)[1], "ssh-ed25519", "ssh-ed25519 !!!!", key + "\n", key + "\r", "ssh-ed25519 " + base64.RawStdEncoding.EncodeToString(blob[:50]), "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob[:50]), "ssh-ed25519 " + base64.StdEncoding.EncodeToString(append(blob, 0)), "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wrongType), "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wrongLen)}
	for _, bad := range invalidKeys {
		for _, op := range []func() error{func() error { return m.UserAdd(background, "new", "absent", bad) }, func() error { return m.UserKeyAdd(background, "absent", bad) }, func() error { return m.UserKeyRemove(background, "absent", bad) }} {
			requireRefused(t, op(), 2, host.RefuseInvalidKey)
			unchanged(t, m, before)
		}
	}
	cancelled, cancel := context.WithCancel(background)
	cancel()
	for _, op := range invocations(cancelled, "INVALID", "INVALID", "invalid") {
		got, err := op.run(m)
		requireFailed(t, err, -1)
		if !reflect.DeepEqual(got, op.zero) {
			t.Fatal(op.name, got)
		}
		unchanged(t, m, before)
	}
}

// R-9VCK-2CEE
func TestMemoryGroupAdd(t *testing.T) {
	m := host.NewMemory()
	requireOK(t, m.GroupAdd(background, "business"))
	if got := snap(t, m); !reflect.DeepEqual(got.groups, []host.Group{{"business", 0}}) {
		t.Fatal(got)
	}
	before := snap(t, m)
	requireRefused(t, m.GroupAdd(background, "business"), 4, host.RefuseNameTaken)
	unchanged(t, m, before)

	requireOK(t, m.UserAdd(background, "person", "business", publicKey(1)))
	before = snap(t, m)
	for _, name := range []string{"business", "person"} {
		requireRefused(t, m.GroupAdd(background, name), 4, host.RefuseNameTaken)
		unchanged(t, m, before)
	}
	requireOK(t, m.UserLock(background, "person"))
	before = snap(t, m)
	requireRefused(t, m.GroupAdd(background, "person"), 4, host.RefuseNameTaken)
	unchanged(t, m, before)

	requireOK(t, m.GroupAdd(background, "another"))
	members, err := m.GroupMembers(background, "another")
	requireOK(t, err)
	if members.Group != "another" || len(members.Members) != 0 {
		t.Fatal(members)
	}
}

// R-SXVL-IZ6F R-IUWU-FCDL R-IXCN-6VUZ R-IYKJ-KNLO R-IZSF-YFCD
// R-J10C-C732 R-J288-PYTR R-J3G5-3QKG R-J4O1-HIB5 R-SZ3H-WQX4
func TestMemoryAccountLifecycle(t *testing.T) {
	m := host.NewMemory()
	key, second := publicKey(1), publicKey(2)
	for _, g := range []string{"zeta", "alpha", "empty"} {
		requireOK(t, m.GroupAdd(background, g))
	}
	before := snap(t, m)
	members, e := m.GroupMembers(background, "empty")
	requireOK(t, e)
	if members.Group != "empty" || members.Members == nil || len(members.Members) != 0 {
		t.Fatal(members)
	}
	requireRefused(t, m.GroupRemove(background, "absent"), 4, host.RefuseNoGroup)
	members, e = m.GroupMembers(background, "absent")
	requireRefused(t, e, 4, host.RefuseNoGroup)
	if !reflect.DeepEqual(members, host.Members{}) {
		t.Fatal(members)
	}
	unchanged(t, m, before)
	requireRefused(t, m.UserAdd(background, "alpha", "absent", key), 4, host.RefuseNameTaken)
	requireRefused(t, m.UserAdd(background, "new", "absent", key), 4, host.RefuseNoGroup)
	unchanged(t, m, before)
	requireOK(t, m.UserAdd(background, "zoe", "zeta", key))
	requireOK(t, m.UserAdd(background, "amy", "zeta", key))
	before = snap(t, m)
	requireRefused(t, m.UserAdd(background, "amy", "absent", second), 4, host.RefuseNameTaken)
	requireRefused(t, m.GroupAdd(background, "amy"), 4, host.RefuseNameTaken)
	requireRefused(t, m.GroupRemove(background, "zeta"), 4, host.RefuseGroupNotEmpty)
	requireRefused(t, m.UserDelete(background, "amy"), 4, host.RefuseUserNotLocked)
	unchanged(t, m, before)
	for _, op := range invocations(background, "absent", "absent", key) {
		if !strings.HasPrefix(op.name, "user ") || op.name == "user add" || op.name == "user list" {
			continue
		}
		_, err := op.run(m)
		requireRefused(t, err, 4, host.RefuseNoUser)
		unchanged(t, m, before)
	}
	for _, op := range []func() error{func() error { return m.UserJoin(background, "amy", "absent") }, func() error { return m.UserLeave(background, "amy", "absent") }} {
		requireRefused(t, op(), 4, host.RefuseNoGroup)
		unchanged(t, m, before)
	}
	for range 2 {
		requireOK(t, m.UserJoin(background, "amy", "alpha"))
	}
	requireOK(t, m.UserKeyAdd(background, "amy", second))
	requireOK(t, m.UserKeyAdd(background, "amy", key+" another comment"))
	requireOK(t, m.UserKeyAdd(background, "amy", " \t"+second+" comment"))
	us, e := m.UserList(background)
	requireOK(t, e)
	want := []host.User{{"amy", []string{"alpha", "zeta"}, []string{key, second}, host.StateActive, time.Time{}}, {"zoe", []string{"zeta"}, []string{key}, host.StateActive, time.Time{}}}
	if !reflect.DeepEqual(us, want) {
		t.Fatal(us, want)
	}
	gs, e := m.GroupList(background)
	requireOK(t, e)
	if !reflect.DeepEqual(gs, []host.Group{{"alpha", 1}, {"empty", 0}, {"zeta", 2}}) {
		t.Fatal(gs)
	}
	members, e = m.GroupMembers(background, "zeta")
	requireOK(t, e)
	if !reflect.DeepEqual(members, host.Members{"zeta", []string{"amy", "zoe"}}) {
		t.Fatal(members)
	}
	for range 2 {
		requireOK(t, m.UserLock(background, "amy"))
	}
	us, e = m.UserList(background)
	requireOK(t, e)
	want[0].State = host.StateLocked
	if !reflect.DeepEqual(us, want) {
		t.Fatal(us, want)
	}
	for range 2 {
		requireOK(t, m.UserUnlock(background, "amy"))
	}
	us, e = m.UserList(background)
	requireOK(t, e)
	want[0].State = host.StateActive
	if !reflect.DeepEqual(us, want) {
		t.Fatal(us, want)
	}
	for range 2 {
		requireOK(t, m.UserKeyRemove(background, "amy", key+" ignored"))
		requireOK(t, m.UserLeave(background, "amy", "alpha"))
	}
	us, e = m.UserList(background)
	requireOK(t, e)
	want[0].Keys = []string{second}
	want[0].Groups = []string{"zeta"}
	if !reflect.DeepEqual(us, want) {
		t.Fatal(us, want)
	}
	requireOK(t, m.UserKeyRemove(background, "amy", second))
	requireOK(t, m.UserLeave(background, "amy", "zeta"))
	us, e = m.UserList(background)
	requireOK(t, e)
	if us[0].Groups == nil || us[0].Keys == nil || len(us[0].Groups) != 0 || len(us[0].Keys) != 0 {
		t.Fatal(us)
	}
	requireOK(t, m.GroupRemove(background, "alpha"))
	requireOK(t, m.UserAdd(background, "alpha", "zeta", key))
	requireOK(t, m.UserLock(background, "alpha"))
	requireOK(t, m.UserDelete(background, "alpha"))
	requireOK(t, m.GroupAdd(background, "alpha"))
	requireOK(t, m.UserLock(background, "zoe"))
	requireOK(t, m.UserDelete(background, "zoe"))
	gs, e = m.GroupList(background)
	requireOK(t, e)
	if gs[2].Members != 0 {
		t.Fatal(gs)
	}
	requireOK(t, m.GroupAdd(background, "zoe"))
	requireOK(t, m.UserLock(background, "amy"))
	requireOK(t, m.UserDelete(background, "amy"))
	requireOK(t, m.UserAdd(background, "amy", "zeta", key))
}

// R-J8BQ-MTJ8 R-J9JN-0L9X
func TestMemorySnapshotsAndConcurrency(t *testing.T) {
	m := host.NewMemory()
	requireOK(t, m.GroupAdd(background, "business"))
	requireOK(t, m.UserAdd(background, "person", "business", publicKey(1)))
	want := snap(t, m)
	a, _ := m.UserList(background)
	b, _ := m.UserList(background)
	a[0].Name = "changed"
	a[0].Groups[0] = "changed"
	a[0].Keys[0] = "changed"
	if !reflect.DeepEqual(b, want.users) {
		t.Fatal(b)
	}
	gs, _ := m.GroupList(background)
	gs[0].Slug = "changed"
	gs[0].Members = 9
	x, _ := m.GroupMembers(background, "business")
	y, _ := m.GroupMembers(background, "business")
	x.Members[0] = "changed"
	if !reflect.DeepEqual(y.Members, []string{"person"}) {
		t.Fatal(y)
	}
	unchanged(t, m, want)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			name := "person" + strconv.Itoa(i)
			for range 10 {
				if e := m.UserAdd(background, name, "business", publicKey(1)); e != nil {
					var r *host.RefusedError
					if !errors.As(e, &r) || r.Diagnostic != host.RefuseNameTaken {
						t.Error(e)
					}
				}
				requireOK(t, m.UserLock(background, name))
				requireOK(t, m.UserUnlock(background, name))
				requireOK(t, m.UserKeyAdd(background, name, publicKey(99)))
				requireOK(t, m.UserJoin(background, name, "business"))
				_, e := m.GroupMembers(background, "business")
				requireOK(t, e)
				_, e = m.UserList(background)
				requireOK(t, e)
			}
		})
	}
	wg.Wait()
	us, e := m.UserList(background)
	requireOK(t, e)
	gs, e = m.GroupList(background)
	requireOK(t, e)
	if len(us) != 21 || len(gs) != 1 || gs[0].Members != 21 {
		t.Fatal(len(us), gs)
	}
}
