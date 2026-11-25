package box_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type snapshot struct {
	Host     store.Host
	Accounts []store.Account
	Users    []store.User
	Invites  map[string][]store.Invite
	Desired  protocol.DesiredState
}

func (f *fixture) snapshot() snapshot {
	f.t.Helper()
	ctx := f.t.Context()
	h, err := f.st.Host(ctx, f.host)
	must(f.t, err)
	accounts, err := f.st.ListAccounts(ctx, "", valid.MaxAccountsPerHost)
	must(f.t, err)
	users, err := f.st.ListUsers(ctx, "", "", "", valid.MaxUsersPerHost)
	must(f.t, err)
	desired, err := f.st.Desired(ctx, f.host)
	must(f.t, err)
	s := snapshot{Host: h, Accounts: accounts, Users: users, Invites: make(map[string][]store.Invite), Desired: desired}
	for _, a := range accounts {
		s.Invites[a.Slug], err = f.st.Invites(ctx, a.Slug)
		must(f.t, err)
	}
	return s
}

func (f *fixture) unchanged(before snapshot) {
	f.t.Helper()
	if after := f.snapshot(); !reflect.DeepEqual(after, before) {
		f.t.Fatalf("record changed\nbefore %+v\nafter  %+v", before, after)
	}
}

// R-EIPE-DPEF R-EHHH-ZXNQ R-EMD3-J0MI R-EOSW-AK3W R-0YKM-GXC1
func TestAccountSelfService(t *testing.T) {
	f := setup(t)
	f.seed()
	c, _ := f.box(true)
	central := f.central()
	do := func(tool string, args any) mcp.Result {
		t.Helper()
		return as(t, c, daffy, tool, args)
	}
	refusal(t, as(t, c, bugs, tools.ToolUsers, box.NoArgs{}), box.OnlyAdmin)
	if got := answer[box.KeyList](t, do(box.ToolKeys, box.NoArgs{})); got.User != daffy || !slices.Equal(got.Keys, []string{key(1)}) {
		t.Fatal(got)
	}
	wantUsers := []box.User{
		{User: bugs, State: store.UserActive, Pending: true, Created: f.now.UTC().Format(time.RFC3339), CreatedBy: "central-creator"},
		{User: daffy, Admin: true, State: store.UserActive, Pending: true, Created: f.now.UTC().Format(time.RFC3339), CreatedBy: "central-creator"},
	}
	if got := answer[box.UserList](t, do(tools.ToolUsers, box.NoArgs{})); !slices.Equal(got.Users, wantUsers) {
		t.Fatal(got, wantUsers)
	}
	i := answer[tools.InviteMade](t, do(tools.ToolCreateInvite, box.NoArgs{}))
	if list := answer[tools.InviteList](t, do(tools.ToolListInvites, box.NoArgs{})); len(list.Invites) != 1 || list.Invites[0].Invite != i.Invite {
		t.Fatal(i, list)
	}
	for _, value := range []bool{true, false} {
		if got := answer[tools.UserChange](t, do(tools.ToolSetAdmin, box.SetAdminArgs{User: bugs, Admin: value})); got.User != bugs || got.Admin != value {
			t.Fatal(got)
		}
	}
	if got := answer[tools.UserChange](t, do(tools.ToolLockUser, box.UserArgs{User: bugs})); got.State != store.UserLocked {
		t.Fatal(got)
	}
	if got := answer[box.UserList](t, do(tools.ToolUsers, box.NoArgs{})); len(got.Users) != 2 || got.Users[0].State != store.UserLocked {
		t.Fatal(got)
	}
	if got := answer[tools.UserChange](t, do(tools.ToolDeleteUser, box.UserArgs{User: bugs})); got.State != store.UserRemoving {
		t.Fatal(got)
	}
	if got := answer[box.UserList](t, do(tools.ToolUsers, box.NoArgs{})); len(got.Users) != 2 || got.Users[0].State != store.UserRemoving {
		t.Fatal(got)
	}
	refusal(t, do(tools.ToolRemoveKey, box.KeyArgs{Key: key(1)}), box.LastKey)
	refusal(t, do(tools.ToolLockUser, box.UserArgs{User: daffy}), box.NotSelf)
	refusal(t, do(tools.ToolSetAdmin, box.SetAdminArgs{User: daffy, Admin: false}), box.NotSelf)
	refusal(t, do(tools.ToolLockUser, box.UserArgs{User: elmer}), tools.NoUser)
	slug := acme
	users := answer[tools.UserList](t, as(t, central, daffy, tools.ToolUsers, tools.UsersArgs{Account: &slug})).Users
	if len(users) != 2 || users[0].User != bugs || users[0].State != store.UserRemoving || users[1].User != daffy ||
		users[1].State != store.UserActive || !users[1].Admin || !slices.Equal(users[1].Keys, []string{key(1)}) {
		t.Fatal(users)
	}
	slug = beta
	if users := answer[tools.UserList](t, as(t, central, daffy, tools.ToolUsers, tools.UsersArgs{Account: &slug})).Users; len(users) != 1 || users[0].User != elmer || users[0].State != store.UserActive {
		t.Fatal(users)
	}
}

// R-08YQ-FQRG R-0CMF-L1ZJ R-EJXA-RH54 R-0IPX-HWP0 R-0MDM-N7X3 R-0NLJ-0ZNS R-0OTF-EREH R-0Q1B-SJ56 R-0R98-6AVV
func TestBoxWritesAndInviteReadsMatchCentralTools(t *testing.T) {
	local, twin := setup(t), setup(t)
	local.seed()
	twin.seed()
	b, _ := local.box(true)
	c := twin.central()
	run := func(name string, boxArgs, centralArgs any) mcp.Result {
		t.Helper()
		left, right := as(t, b, daffy, name, boxArgs), as(t, c, daffy, name, centralArgs)
		if !reflect.DeepEqual(wire(t, left), wire(t, right)) || left.IsError() != right.IsError() {
			t.Fatalf("%s box %+v, central %+v", name, wire(t, left), wire(t, right))
		}
		twin.unchanged(local.snapshot())
		return left
	}
	for _, action := range []struct {
		name string
		key  byte
	}{
		{tools.ToolAddKey, 4},
		{tools.ToolAddKey, 4},
		{tools.ToolRemoveKey, 4},
		{tools.ToolRemoveKey, 5},
	} {
		k := key(action.key)
		out := run(action.name, box.KeyArgs{Key: k}, tools.UserKeyArgs{Account: acme, User: daffy, Key: k})
		switch {
		case action.key == 5:
			refusal(t, out, tools.KeyNotHeld)
		case action.name == tools.ToolRemoveKey:
			if got := answer[tools.KeyRemoved](t, out); got.KeysLeft != 1 || !got.Admin {
				t.Fatal(got)
			}
		default:
			if got := answer[tools.UserChange](t, out); got.User != daffy || !got.Admin {
				t.Fatal(got)
			}
		}
	}
	user := box.UserArgs{User: bugs}
	centralUser := tools.UserArgs{Account: acme, User: bugs}
	refusal(t, run(tools.ToolDeleteUser, user, centralUser), tools.UserNotLocked)
	for _, op := range []string{tools.ToolLockUser, tools.ToolUnlockUser} {
		answer[tools.UserChange](t, run(op, user, centralUser))
	}
	for _, value := range []bool{true, true, false} {
		answer[tools.UserChange](t, run(tools.ToolSetAdmin, box.SetAdminArgs{User: bugs, Admin: value}, tools.SetAdminArgs{Account: acme, User: bugs, Admin: value}))
	}
	answer[tools.UserChange](t, run(tools.ToolLockUser, user, centralUser))
	answer[tools.UserChange](t, run(tools.ToolDeleteUser, user, centralUser))
	refusal(t, run(tools.ToolSetAdmin, box.SetAdminArgs{User: bugs, Admin: true}, tools.SetAdminArgs{Account: acme, User: bugs, Admin: true}), tools.UserRemoving)
	made := answer[tools.InviteMade](t, run(tools.ToolCreateInvite, box.NoArgs{}, tools.CreateInviteArgs{Account: acme}))
	if made.Admin || made.Account != acme || made.CreatedBy != box.ActorPrefix+daffy {
		t.Fatal(made)
	}
	nextInvite := made.Invite
	invites := answer[tools.InviteList](t, run(tools.ToolListInvites, box.NoArgs{}, tools.AccountArgs{Account: acme}))
	if len(invites.Invites) != 1 || invites.Invites[0].Invite != nextInvite {
		t.Fatal(invites)
	}
	id := tools.InviteArgs{Invite: nextInvite}
	if i := answer[tools.Invite](t, run(tools.ToolRevokeInvite, id, id)); i != invites.Invites[0] {
		t.Fatal(i)
	}
	refusal(t, run(tools.ToolRevokeInvite, id, id), tools.NoInvite)
}

// R-0A6M-TII5 R-0SH4-K2MK
func TestBoxUserToolsAgreeAcrossTiers(t *testing.T) {
	a, b := setup(t), setup(t)
	a.seed()
	b.seed()
	user, _ := a.box(false)
	admin, _ := b.box(true)
	for _, req := range []struct {
		tool string
		args any
	}{
		{box.ToolKeys, box.NoArgs{}},
		{tools.ToolAddKey, box.KeyArgs{Key: key(4)}},
		{tools.ToolRemoveKey, box.KeyArgs{Key: key(4)}},
		{tools.ToolRemoveKey, box.KeyArgs{Key: key(1)}},
		{tools.ToolAddKey, box.KeyArgs{Key: "bad"}},
	} {
		x, y := as(t, user, daffy, req.tool, req.args), as(t, admin, daffy, req.tool, req.args)
		if x.IsError() != y.IsError() || !reflect.DeepEqual(wire(t, x), wire(t, y)) {
			t.Fatal(wire(t, x), wire(t, y))
		}
		b.unchanged(a.snapshot())
	}
	eventsA, eventsB := a.events(), b.events()
	if len(eventsA) != len(eventsB) {
		t.Fatal(eventsA, eventsB)
	}
	for i, x := range eventsA {
		y := eventsB[i]
		if x.Name != y.Name || x.User != y.User || !reflect.DeepEqual(x.Attrs, y.Attrs) {
			t.Fatal(x, y)
		}
	}
	before := a.snapshot()
	for _, tool := range catalog()[3:] {
		args := tool.args
		if args == nil {
			args = map[string]any{}
		}
		raw, err := json.Marshal(args)
		must(t, err)
		_, err = user.CallTool(t.Context(), identity.Caller{UserID: box.ActorPrefix + daffy}, tool.name, raw)
		var rpc *mcp.RPCError
		if !errors.As(err, &rpc) || rpc.Code != mcp.CodeInvalidParams {
			t.Fatalf("user tier accepted %s: %v", tool.name, err)
		}
	}
	a.unchanged(before)
}

// R-EQ0S-OBUL R-EIPE-DPEF R-EL57-58VT
func TestBoxGuardPrecedence(t *testing.T) {
	for _, closeDB := range []bool{false, true} {
		t.Run(fmt.Sprint(closeDB), func(t *testing.T) {
			f := setup(t)
			f.seed()
			c, _ := f.box(true)
			if closeDB {
				must(t, f.d.Close())
			} else {
				f.d.SetFailing(true)
			}
			for i, tool := range catalog() {
				args := tool.args
				if args == nil {
					args = map[string]any{}
				}
				want := tools.NoUser
				if i >= 3 {
					want = box.OnlyAdmin
				}
				for _, who := range []string{"no-prefix", box.ActorPrefix} {
					refusal(t, call(t, c, who, tool.name, args), want)
				}
				refusal(t, as(t, c, daffy, tool.name, args), store.Unreachable)
			}
			for _, tool := range []string{tools.ToolAddKey, tools.ToolRemoveKey} {
				refusal(t, as(t, c, daffy, tool, box.KeyArgs{Key: "invalid"}), tools.InvalidKey)
			}
		})
	}
	t.Run("removing account", func(t *testing.T) {

		// Use the test database to put the actor in a removing account, since the
		// tool that removes an account cannot do so until its user rows are gone.
		f := setup(t)
		f.seed()
		must(t, f.d.Write(t.Context(), func(tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), "UPDATE accounts SET state = ? WHERE slug = ?", store.AccountRemoving, acme)
			return err
		}))
		c, _ := f.box(true)
		before := f.snapshot()
		for _, tool := range []string{tools.ToolAddKey, tools.ToolRemoveKey} {
			refusal(t, as(t, c, daffy, tool, box.KeyArgs{Key: key(4)}), tools.NoUser)
		}
		for _, tool := range catalog()[3:] {
			args := tool.args
			if args == nil {
				args = map[string]any{}
			}
			refusal(t, as(t, c, daffy, tool.name, args), box.OnlyAdmin)
		}
		if got := answer[box.KeyList](t, as(t, c, daffy, box.ToolKeys, box.NoArgs{})); !slices.Equal(got.Keys, []string{key(1)}) {
			t.Fatal(got)
		}
		f.unchanged(before)
	})
	f := setup(t)
	f.seed()
	f.base = nil
	c, _ := f.box(true)
	central := f.central()
	before := f.snapshot()
	for _, name := range []string{bugs, "missing"} {
		for _, tool := range catalog()[3:] {
			args := tool.args
			if args == nil {
				args = map[string]any{}
			}
			refusal(t, as(t, c, name, tool.name, args), box.OnlyAdmin)
		}
	}
	f.unchanged(before)
	refusal(t, as(t, c, daffy, tools.ToolCreateInvite, box.NoArgs{}), tools.NoLink)
	_, err := f.st.Lock(t.Context(), acme, daffy)
	must(t, err)
	refusal(t, as(t, c, daffy, tools.ToolUsers, box.NoArgs{}), box.OnlyAdmin)
	if got := answer[box.KeyList](t, as(t, c, daffy, box.ToolKeys, box.NoArgs{})); !slices.Equal(got.Keys, []string{key(1)}) {
		t.Fatal(got)
	}
	for _, tool := range []string{tools.ToolAddKey, tools.ToolRemoveKey} {
		refusal(t, as(t, c, daffy, tool, box.KeyArgs{Key: key(1)}), tools.NoUser)
	}
	refusal(t, as(t, c, "missing", box.ToolKeys, box.NoArgs{}), tools.NoUser)
	_, err = f.st.Unlock(t.Context(), acme, daffy)
	must(t, err)
	answer[tools.UserChange](t, as(t, central, daffy, tools.ToolSetAdmin, tools.SetAdminArgs{Account: acme, User: daffy, Admin: false}))
	refusal(t, as(t, c, daffy, tools.ToolUsers, box.NoArgs{}), box.OnlyAdmin)
}

// R-0MDM-N7X3 R-0NLJ-0ZNS R-0R98-6AVV R-0SH4-K2MK
func TestBoxScopesAdminTargets(t *testing.T) {
	f := setup(t)
	f.seed()
	c, _ := f.box(true)
	foreign, token, err := f.st.CreateInvite(t.Context(), beta, false, identity.Caller{UserID: "central-creator"})
	must(t, err)
	before := f.snapshot()
	for _, target := range []string{"", "missing", elmer} {
		for _, tool := range []string{tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser} {
			refusal(t, as(t, c, daffy, tool, box.UserArgs{User: target}), tools.NoUser)
		}
		refusal(t, as(t, c, daffy, tools.ToolSetAdmin, box.SetAdminArgs{User: target, Admin: true}), tools.NoUser)
	}
	for _, tool := range []string{tools.ToolLockUser, tools.ToolDeleteUser} {
		refusal(t, as(t, c, daffy, tool, box.UserArgs{User: daffy}), box.NotSelf)
	}
	answer[tools.UserChange](t, as(t, c, daffy, tools.ToolUnlockUser, box.UserArgs{User: daffy}))
	answer[tools.UserChange](t, as(t, c, daffy, tools.ToolSetAdmin, box.SetAdminArgs{User: daffy, Admin: true}))
	refusal(t, as(t, c, daffy, tools.ToolSetAdmin, box.SetAdminArgs{User: daffy, Admin: false}), box.NotSelf)
	for _, id := range []string{"iv_0", store.InvitePrefix + "0000000000000000", foreign.ID} {
		refusal(t, as(t, c, daffy, tools.ToolRevokeInvite, tools.InviteArgs{Invite: id}), tools.NoInvite)
	}
	f.unchanged(before)
	if row, err := f.st.InviteByToken(t.Context(), token); err != nil || row != foreign {
		t.Fatal(row, err)
	}
}

// R-0TP0-XUD9 R-0UWX-BM3Y R-J4ZX-ENRZ
func TestBoxEventsAndSecrets(t *testing.T) {
	f := setup(t)
	f.seed()
	const bodyMarker = "GHIJKLMNGHIJKLMNGHIJKLMN"
	const commentMarker = "WXYZ1234WXYZ1234WXYZ1234"
	markerKey := func(i byte) string {
		fields := strings.Split(key(i), " ")
		return fields[0] + " " + fields[1][:28] + bodyMarker + fields[1][52:] + " " + commentMarker
	}
	held := markerKey(1)
	marker := markerKey(8)
	initial, err := valid.ParseKey(key(1))
	must(t, err)
	last, err := valid.ParseKey(held)
	must(t, err)
	_, err = f.st.AddKey(t.Context(), acme, daffy, store.Key{Type: last.Type, Body: last.Body, Line: last.Line()})
	must(t, err)
	_, _, err = f.st.RemoveKey(t.Context(), acme, daffy, store.Key{Type: initial.Type, Body: initial.Body, Line: initial.Line()})
	must(t, err)

	boxClient, _ := f.box(true)
	central := f.central()
	actor := box.ActorPrefix + daffy
	var later []mcp.Result
	do := func(tool string, args any) mcp.Result {
		t.Helper()
		r := as(t, boxClient, daffy, tool, args)
		later = append(later, r)
		return r
	}
	i := answer[tools.InviteMade](t, as(t, boxClient, daffy, tools.ToolCreateInvite, box.NoArgs{}))
	token, ok := strings.CutPrefix(i.Link, baseURL+"/join?token=")
	if !ok || !store.ValidToken(token) {
		t.Fatal(i)
	}
	answer[tools.UserChange](t, do(tools.ToolAddKey, box.KeyArgs{Key: marker}))
	answer[tools.KeyRemoved](t, do(tools.ToolRemoveKey, box.KeyArgs{Key: marker}))
	refusal(t, do(tools.ToolRemoveKey, box.KeyArgs{Key: held}), box.LastKey)
	answer[tools.InviteList](t, do(tools.ToolListInvites, box.NoArgs{}))
	later = append(later, as(t, central, daffy, tools.ToolListInvites, tools.AccountArgs{Account: acme}))
	refused := as(t, boxClient, bugs, tools.ToolRevokeInvite, tools.InviteArgs{Invite: i.Invite})
	refusal(t, refused, box.OnlyAdmin)
	later = append(later, refused)
	answer[tools.Invite](t, do(tools.ToolRevokeInvite, tools.InviteArgs{Invite: i.Invite}))
	later = append(later, as(t, central, daffy, tools.ToolAccount, tools.AccountArgs{Account: acme}))
	for range 2 {
		answer[tools.UserChange](t, do(tools.ToolSetAdmin, box.SetAdminArgs{User: bugs, Admin: true}))
	}
	answer[tools.UserChange](t, do(tools.ToolLockUser, box.UserArgs{User: bugs}))
	f.d.SetFailing(true)
	refusal(t, do(tools.ToolLockUser, box.UserArgs{User: bugs}), store.Unreachable)
	for _, r := range later {
		out := wire(t, r)
		if strings.Contains(string(out.Structured), token) {
			t.Fatalf("token leaked in %s", out.Structured)
		}
		for _, block := range out.Content {
			if strings.Contains(block.Text, token) {
				t.Fatalf("token leaked in %q", block.Text)
			}
		}
	}
	change := func(tool, account, host, user, result, by string) telemetry.Event {
		return telemetry.Event{Name: trail.EventChangeFinished, User: by, Attrs: telemetry.Attrs{"tool": tool, "account": account, "host": host, "user": user, "ip": "", "result": result}}
	}
	want := []telemetry.Event{
		{Name: trail.EventInviteCreated, User: actor, Attrs: telemetry.Attrs{"invite": i.Invite, "account": acme, "host": f.host, "admin": false}},
		change(tools.ToolCreateInvite, acme, f.host, "", trail.ResultOK, actor),
		change(tools.ToolAddKey, acme, f.host, daffy, trail.ResultOK, actor),
		change(tools.ToolRemoveKey, acme, f.host, daffy, trail.ResultOK, actor),
		change(tools.ToolRemoveKey, acme, f.host, daffy, trail.ResultRefused, actor),
		change(tools.ToolRevokeInvite, acme, f.host, "", trail.ResultRefused, box.ActorPrefix+bugs),
		{Name: trail.EventInviteRevoked, User: actor, Attrs: telemetry.Attrs{"invite": i.Invite, "account": acme, "host": f.host}},
		change(tools.ToolRevokeInvite, acme, f.host, "", trail.ResultOK, actor),
		{Name: trail.EventAdminChanged, User: actor, Attrs: telemetry.Attrs{"account": acme, "host": f.host, "user": bugs, "admin": true}},
		change(tools.ToolSetAdmin, acme, f.host, bugs, trail.ResultOK, actor),
		change(tools.ToolSetAdmin, acme, f.host, bugs, trail.ResultOK, actor),
		change(tools.ToolLockUser, acme, f.host, bugs, trail.ResultOK, actor),
		change(tools.ToolLockUser, "", "", bugs, trail.ResultFailed, actor),
	}
	var got []telemetry.Event
	for _, e := range f.events() {
		data, err := json.Marshal(e.Attrs)
		must(t, err)
		if strings.Contains(string(data), token) {
			t.Fatalf("token leaked in %s", data)
		}
		for _, secret := range []string{bodyMarker, commentMarker} {
			if strings.Contains(strings.ToUpper(string(data)), secret) {
				t.Fatalf("key marker leaked in %s", data)
			}
		}
		if e.Name != "tool.called" {
			got = append(got, e)
		}
	}
	if len(got) != len(want) {
		t.Fatal(got, want)
	}
	for i, a := range got {
		b := want[i]
		if a.Name != b.Name || a.User != b.User || !reflect.DeepEqual(a.Attrs, b.Attrs) {
			t.Fatalf("event %d %+v, want %+v", i, a, b)
		}
	}
}

// R-EJXA-RH54 R-0IPX-HWP0 R-EHHH-ZXNQ
func TestBoxKeyLimitAndLastKey(t *testing.T) {
	f := setup(t)
	f.seed()
	c, _ := f.box(true)
	central := f.central()
	for i := 2; i <= valid.MaxKeysPerUser; i++ {
		answer[tools.UserChange](t, as(t, c, daffy, tools.ToolAddKey, box.KeyArgs{Key: key(byte(i))}))
	}
	before := f.snapshot()
	refusal(t, as(t, c, daffy, tools.ToolAddKey, box.KeyArgs{Key: key(100)}), tools.TooManyKeys)
	refusal(t, as(t, c, daffy, tools.ToolRemoveKey, box.KeyArgs{Key: key(100)}), tools.KeyNotHeld)
	got := answer[box.KeyList](t, as(t, c, daffy, box.ToolKeys, box.NoArgs{}))
	if len(got.Keys) != valid.MaxKeysPerUser || !slices.IsSorted(got.Keys) {
		t.Fatal(got)
	}
	f.unchanged(before)
	for i := 2; i <= valid.MaxKeysPerUser; i++ {
		answer[tools.KeyRemoved](t, as(t, c, daffy, tools.ToolRemoveKey, box.KeyArgs{Key: key(byte(i))}))
	}
	refusal(t, as(t, c, daffy, tools.ToolRemoveKey, box.KeyArgs{Key: key(1)}), box.LastKey)
	args := tools.UserKeyArgs{Account: acme, User: daffy, Key: key(1)}
	if got := answer[tools.KeyRemoved](t, as(t, central, daffy, tools.ToolRemoveKey, args)); got.KeysLeft != 0 || !got.Admin {
		t.Fatal(got)
	}
	if got := answer[box.KeyList](t, as(t, c, daffy, box.ToolKeys, box.NoArgs{})); got.User != daffy || len(got.Keys) != 0 || got.Keys == nil {
		t.Fatal(got)
	}
}

// R-0OTF-EREH
func TestBoxInviteURLFailure(t *testing.T) {
	f := setup(t)
	f.seed()
	urlErr := errors.New("accounts URL unavailable")
	calls := 0
	base, baseErr := "", urlErr
	f.base = func() (string, error) {
		calls++
		return base, baseErr
	}
	c, _ := f.box(true)
	central := f.central()
	before := f.snapshot()

	refusal(t, as(t, c, bugs, tools.ToolCreateInvite, box.NoArgs{}), box.OnlyAdmin)
	if calls > 1 {
		t.Fatalf("non-admin call read the URL %d times", calls)
	}
	f.unchanged(before)

	calls = 0
	refusal(t, as(t, c, daffy, tools.ToolCreateInvite, box.NoArgs{}), tools.NoLink)
	if calls != 1 {
		t.Fatalf("admin call read the URL %d times, want 1", calls)
	}
	f.unchanged(before)

	base, baseErr = baseURL+strings.Repeat("x", tools.MaxLinkBase), nil
	refusal(t, as(t, c, bugs, tools.ToolCreateInvite, box.NoArgs{}), box.OnlyAdmin)
	calls = 0
	refusal(t, as(t, c, daffy, tools.ToolCreateInvite, box.NoArgs{}), tools.NoLink)
	if calls != 1 {
		t.Fatalf("admin call read the oversized URL %d times, want 1", calls)
	}
	refusal(t, as(t, central, daffy, tools.ToolCreateInvite, tools.CreateInviteArgs{Account: acme}), tools.NoLink)
	f.unchanged(before)
}

// R-0OTF-EREH R-0Q1B-SJ56
func TestBoxInviteCapacityAndLists(t *testing.T) {
	f := setup(t)
	f.seed()
	calls := 0
	f.base = func() (string, error) { calls++; return baseURL, nil }
	c, _ := f.box(true)
	central := f.central()
	outsider := answer[tools.InviteMade](t, as(t, central, daffy, tools.ToolCreateInvite, tools.CreateInviteArgs{Account: beta}))
	first := answer[tools.InviteMade](t, as(t, central, daffy, tools.ToolCreateInvite, tools.CreateInviteArgs{Account: acme}))
	f.now = f.now.Add(time.Second)
	second := answer[tools.InviteMade](t, as(t, c, daffy, tools.ToolCreateInvite, box.NoArgs{}))
	list := answer[tools.InviteList](t, as(t, c, daffy, tools.ToolListInvites, box.NoArgs{}))
	if len(list.Invites) != 2 || list.Invites[0].Invite != first.Invite || list.Invites[1].Invite != second.Invite || calls != 3 {
		t.Fatal(outsider, first, second, list, calls)
	}
	for range store.MaxInvites - 2 {
		answer[tools.InviteMade](t, as(t, c, daffy, tools.ToolCreateInvite, box.NoArgs{}))
	}
	before := f.snapshot()
	refusal(t, as(t, c, daffy, tools.ToolCreateInvite, box.NoArgs{}), tools.TooManyInvites)
	got := answer[tools.InviteList](t, as(t, c, daffy, tools.ToolListInvites, box.NoArgs{}))
	if len(got.Invites) != store.MaxInvites || calls != store.MaxInvites+2 {
		t.Fatal(got, calls)
	}
	for _, i := range got.Invites {
		if i.Account != acme {
			t.Fatal(i)
		}
	}
	f.unchanged(before)
}

// R-0XCQ-35LC
func TestBoxAnswersAtCapacity(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	maker := identity.Caller{UserID: strings.Repeat("<", valid.MaxUserIDBytes)}
	host, _, err := f.st.CreateHost(ctx, store.HostSpec{Type: valid.DefaultType, RootGB: valid.DefaultRootGB, DataGB: valid.DefaultDataGB}, maker)
	must(t, err)
	_, _, err = f.st.PlaceAccount(ctx, acme, host.Name, maker)
	must(t, err)
	largeKey := func(i byte) store.Key {
		t.Helper()
		prefix := strings.TrimSuffix(key(i), "fixture")
		k, err := valid.ParseKey(prefix + strings.Repeat("<", valid.MaxKeyBytes-len(prefix)))
		must(t, err)
		return store.Key{Type: k.Type, Body: k.Body, Line: k.Line()}
	}
	admin := fmt.Sprintf("user%028d", 0)
	firstKey := largeKey(1)
	for i := range valid.MaxUsersPerHost {
		name := fmt.Sprintf("user%028d", i)
		_, err := f.st.AddUser(ctx, acme, name, firstKey, maker)
		must(t, err)
	}
	_, _, err = f.st.SetAdmin(ctx, acme, admin, true)
	must(t, err)
	for i := 2; i <= valid.MaxKeysPerUser; i++ {
		_, err = f.st.AddKey(ctx, acme, admin, largeKey(byte(i)))
		must(t, err)
	}
	for range store.MaxInvites - 1 {
		_, _, err = f.st.CreateInvite(ctx, acme, false, maker)
		must(t, err)
	}

	base := baseURL + "/" + strings.Repeat("<", tools.MaxLinkBase-len(baseURL)-1)
	f.base = func() (string, error) { return base, nil }
	srv := mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	box.RegisterAdmin(srv, box.Config{Store: f.st, Telemetry: f.w, AccountsURL: f.base})
	var replyBytes atomic.Int64
	httpServer := httptest.NewServer(identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, r)
		replyBytes.Store(int64(rec.Body.Len()))
		for name, values := range rec.Header() {
			w.Header()[name] = values
		}
		w.WriteHeader(rec.Code)
		if _, err := rec.Body.WriteTo(w); err != nil {
			t.Error(err)
		}
	})))
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: httpServer.URL + "/mcp", HTTPClient: httpServer.Client()})
	callAtCapacity := func(tool string) mcp.Result {
		t.Helper()
		replyBytes.Store(0)
		result := as(t, client, admin, tool, box.NoArgs{})
		if n := replyBytes.Load(); n == 0 || n > protocol.MaxBoxAnswer {
			t.Fatalf("%s answered %d bytes", tool, n)
		}
		var members map[string]json.RawMessage
		must(t, json.Unmarshal(wire(t, result).Structured, &members))
		if _, paged := members["next"]; paged {
			t.Fatalf("%s returned a next member", tool)
		}
		return result
	}
	made := answer[tools.InviteMade](t, callAtCapacity(tools.ToolCreateInvite))
	token, ok := strings.CutPrefix(made.Link, base+"/join?token=")
	if !ok || !store.ValidToken(token) {
		t.Fatalf("invite link at base limit = %q", made.Link)
	}
	users := answer[box.UserList](t, callAtCapacity(tools.ToolUsers)).Users
	if len(users) != valid.MaxUsersPerHost {
		t.Fatalf("users = %d, want %d", len(users), valid.MaxUsersPerHost)
	}
	keys := answer[box.KeyList](t, callAtCapacity(box.ToolKeys)).Keys
	if len(keys) != valid.MaxKeysPerUser {
		t.Fatalf("keys = %d, want %d", len(keys), valid.MaxKeysPerUser)
	}
	invites := answer[tools.InviteList](t, callAtCapacity(tools.ToolListInvites)).Invites
	if len(invites) != store.MaxInvites {
		t.Fatalf("invites = %d, want %d", len(invites), store.MaxInvites)
	}
}
