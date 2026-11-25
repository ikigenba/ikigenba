package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-CX8V-T5SG R-CYGS-6XJ5 R-CZOO-KP9U
func TestInviteEncoding(t *testing.T) {
	_ = struct {
		Invite, Account             string
		Admin                       bool
		Created, Expires, CreatedBy string
	}(tools.Invite{})
	_ = struct {
		Invite, Account                   string
		Admin                             bool
		Created, Expires, CreatedBy, Link string
	}(tools.InviteMade{})
	_ = struct{ Invites []tools.Invite }(tools.InviteList{})

	members := []string{"invite", "account", "admin", "created", "expires", "created_by"}
	invite := tools.Invite{Invite: "iv_example", Account: "acme", Admin: true, Created: "start", Expires: "end", CreatedBy: "someone"}
	inviteWire := testEncoding(t, invite)
	wireMembers(t, inviteWire, members...)
	madeWire := testEncoding(t, tools.InviteMade{Invite: invite.Invite, Admin: true})
	wireMembers(t, madeWire, append(members, "link")...)
	for _, wire := range []json.RawMessage{inviteWire, madeWire} {
		if string(field(t, wire, "admin")) != "true" {
			t.Fatal(string(wire))
		}
	}
	list := testEncoding(t, tools.InviteList{Invites: []tools.Invite{invite}})
	wireMembers(t, list, "invites")
	wireArray(t, list, "invites", members...)
	empty := testEncoding(t, tools.InviteList{})
	wireMembers(t, empty, "invites")
	wireArray(t, empty, "invites")
}

// R-D24H-C8R8 R-1913-9A87 R-1A8Z-N1YW R-1MFZ-GRDU R-1RBK-ZUCM R-1NNV-UJ4J R-1Q3O-M2LX
func TestInviteLifecycle(t *testing.T) {
	ctx := context.Background()
	sourceCalls := 0
	base := accountsURL
	f := setupWithURL(t, true, func() (string, error) { sourceCalls++; return base, nil })
	f.serveLoopback()
	f.now = time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	h, _ := f.seedHost(true)
	var responses []mcp.Result
	recordCall := func(name string, args any) mcp.Result {
		t.Helper()
		res := f.call(name, args)
		responses = append(responses, res)
		return res
	}
	createdAccount := answer[tools.Account](t, recordCall(tools.ToolCreateAccount, tools.CreateAccountArgs{Account: "acme", Host: h.Name}))
	if createdAccount.Invites != 0 {
		t.Fatal(createdAccount)
	}
	f.seedAccount("beta", h.Name)
	member := tools.UserArgs{Account: "acme", User: "bugs"}
	answer[tools.UserChange](t, recordCall(tools.ToolAddUser, tools.UserKeyArgs{Account: member.Account, User: member.User, Key: key(2)}))
	checkCounts := func(n int) {
		t.Helper()
		a := answer[tools.AccountRead](t, recordCall(tools.ToolAccount, tools.AccountArgs{Account: "acme"}))
		as := answer[tools.AccountList](t, recordCall(tools.ToolAccounts, tools.AccountsArgs{}))
		if a.Invites != n || len(as.Accounts) != 2 || as.Accounts[0].Invites != n || as.Accounts[1].Invites != 0 {
			t.Fatalf("invite count = %d, %+v, want %d for acme and 0 for beta", a.Invites, as, n)
		}
	}

	before := f.host(h.Name).Version
	const joinPrefix = accountsURL + "/join?token="
	first := answer[tools.InviteMade](t, f.call(tools.ToolCreateInvite, json.RawMessage(`{"account":"acme","admin":null}`)))
	token, ok := strings.CutPrefix(first.Link, joinPrefix)
	if !ok || !store.ValidToken(token) {
		t.Fatal(first)
	}
	rec, err := f.st.InviteByToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	want := tools.Invite{Invite: rec.ID, Account: "acme", Created: f.now.UTC().Format(time.RFC3339),
		Expires: f.now.Add(store.InviteLife).UTC().Format(time.RFC3339), CreatedBy: f.caller.UserID}
	if first != (tools.InviteMade{Invite: want.Invite, Account: want.Account, Created: want.Created,
		Expires: want.Expires, CreatedBy: want.CreatedBy, Link: first.Link}) ||
		sourceCalls != 1 || f.host(h.Name).Version != before {
		t.Fatalf("invite = %+v, record = %+v, URL calls = %d", first, rec, sourceCalls)
	}
	f.now = f.now.Add(5 * time.Second)
	admin := true
	base = accountsURL + "/"
	second := answer[tools.InviteMade](t, recordCall(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "acme", Admin: &admin}))
	secondToken, ok := strings.CutPrefix(second.Link, base+"/join?token=")
	if !ok || !store.ValidToken(secondToken) || !second.Admin || second.Created != f.now.UTC().Format(time.RFC3339) || sourceCalls != 2 {
		t.Fatal(second, sourceCalls)
	}
	base = accountsURL
	checkCounts(2)

	acme := tools.AccountArgs{Account: "acme"}
	revoke := tools.InviteArgs{Invite: first.Invite}
	list := answer[tools.InviteList](t, recordCall(tools.ToolListInvites, acme))
	if len(list.Invites) != 2 || list.Invites[0] != want || list.Invites[1].Invite != second.Invite || !list.Invites[1].Admin {
		t.Fatal(list)
	}
	users := answer[tools.UserList](t, recordCall(tools.ToolUsers, tools.UsersArgs{Account: &acme.Account}))
	if len(users.Users) != 1 || users.Users[0].User != member.User {
		t.Fatal(users)
	}
	if got := answer[tools.Invite](t, recordCall(tools.ToolRevokeInvite, revoke)); got != want {
		t.Fatal(got, want)
	}
	refusal(t, recordCall(tools.ToolRevokeInvite, revoke), tools.NoInvite)
	if _, err := f.st.InviteByToken(ctx, token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked token: %v", err)
	}
	if got := answer[tools.InviteList](t, recordCall(tools.ToolListInvites, acme)); !slices.Equal(got.Invites, list.Invites[1:]) {
		t.Fatal(got)
	}
	checkCounts(1)
	f.now = f.now.Add(store.InviteLife)
	if got := answer[tools.InviteList](t, recordCall(tools.ToolListInvites, acme)); got.Invites == nil || len(got.Invites) != 0 {
		t.Fatal(got)
	}
	checkCounts(0)
	refusal(t, recordCall(tools.ToolRevokeInvite, tools.InviteArgs{Invite: second.Invite}), tools.NoInvite)
	if f.host(h.Name).Version != before {
		t.Fatal("invite lifecycle raised desired version")
	}

	makeBeta := answer[tools.InviteMade](t, recordCall(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "beta"}))
	if makeBeta.Admin {
		t.Fatal(makeBeta)
	}
	keyValue, err := valid.ParseKey(key(1))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = f.st.Enroll(ctx, strings.TrimPrefix(makeBeta.Link, joinPrefix), "daffy",
		store.Key{Type: keyValue.Type, Body: keyValue.Body, Line: keyValue.Line()})
	if err != nil {
		t.Fatal(err)
	}
	refusal(t, recordCall(tools.ToolRevokeInvite, tools.InviteArgs{Invite: makeBeta.Invite}), tools.NoInvite)

	answer[tools.InviteMade](t, recordCall(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "acme"}))
	answer[tools.UserChange](t, recordCall(tools.ToolLockUser, member))
	answer[tools.UserChange](t, recordCall(tools.ToolDeleteUser, member))
	f.syncHost(h.Name)
	answer[tools.Account](t, recordCall(tools.ToolRemoveAccount, acme))
	if got := answer[tools.InviteList](t, recordCall(tools.ToolListInvites, acme)); got.Invites == nil || len(got.Invites) != 0 {
		t.Fatal(got)
	}
	refusal(t, recordCall(tools.ToolListInvites, tools.AccountArgs{Account: "absent"}), tools.NoAccount)

	for _, res := range responses {
		r := unpack(t, res)
		if strings.Contains(string(r.Structured), token) {
			t.Fatalf("token leaked in %s", r.Structured)
		}
		for _, block := range r.Content {
			if strings.Contains(block.Text, token) {
				t.Fatalf("token leaked in %q", block.Text)
			}
		}
	}
	for _, event := range allEvents(t, f) {
		raw, err := json.Marshal(event.Attrs)
		if err != nil || strings.Contains(string(raw), token) {
			t.Fatalf("token leaked in %s (%v)", raw, err)
		}
	}
}

func allEvents(t *testing.T, f *fixture) []telemetry.Event {
	t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f.capture.Events()
}

// R-FHMR-DAV0 R-FGEU-ZJ4B R-1Q3O-M2LX
func TestInviteRefusals(t *testing.T) {
	const maxBytes int = tools.MaxLinkBase
	const maxUnsigned uint = tools.MaxLinkBase
	if got, want := [2]uint{uint(maxBytes), maxUnsigned}, [2]uint{512, 512}; got != want {
		t.Fatal(got, want)
	}
	boundary := accountsURL + strings.Repeat("x", maxBytes-len(accountsURL))
	for _, base := range []func() (string, error){
		nil,
		func() (string, error) { return "", settings.ErrNoAccountsURL },
		func() (string, error) { return boundary[:maxBytes-1] + "é", nil },
	} {
		f := setupWithURL(t, true, base)
		h, _ := f.seedHost(true)
		f.seedAccount("acme", h.Name)
		refusal(t, f.call(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "acme"}), tools.NoLink)
		list := answer[tools.InviteList](t, f.call(tools.ToolListInvites, tools.AccountArgs{Account: "acme"}))
		if len(list.Invites) != 0 {
			t.Fatal(list)
		}
		f.d.SetFailing(true)
		refusal(t, f.call(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "missing"}), tools.NoLink)
		refusal(t, f.call(tools.ToolRevokeInvite, tools.InviteArgs{Invite: "iv_0"}), tools.NoInvite)
	}
	f := setupWithURL(t, true, func() (string, error) { return boundary, nil })
	h, _ := f.seedHost(true)
	f.seedAccount("acme", h.Name)
	f.seedAccount("leaving", h.Name)
	answer[tools.Account](t, f.call(tools.ToolRemoveAccount, tools.AccountArgs{Account: "leaving"}))
	refusal(t, f.call(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "leaving"}), tools.AccountRemoving)
	refusal(t, f.call(tools.ToolCreateInvite, tools.CreateInviteArgs{Account: "missing"}), tools.NoAccount)
	refusal(t, f.call(tools.ToolRevokeInvite, tools.InviteArgs{Invite: store.InvitePrefix + "0000000000000000"}), tools.NoInvite)
	args := tools.CreateInviteArgs{Account: "acme"}
	var first tools.InviteMade
	for i := range store.MaxInvites {
		made := answer[tools.InviteMade](t, f.call(tools.ToolCreateInvite, args))
		if i == 0 {
			first = made
			f.now = f.now.Add(time.Second)
		}
	}
	refusal(t, f.call(tools.ToolCreateInvite, args), tools.TooManyInvites)
	answer[tools.Invite](t, f.call(tools.ToolRevokeInvite, tools.InviteArgs{Invite: first.Invite}))
	answer[tools.InviteMade](t, f.call(tools.ToolCreateInvite, args))
	refusal(t, f.call(tools.ToolCreateInvite, args), tools.TooManyInvites)
	f.now = f.now.Add(store.InviteLife - time.Second)
	refusal(t, f.call(tools.ToolCreateInvite, args), tools.TooManyInvites)
	f.now = f.now.Add(time.Second)
	answer[tools.InviteMade](t, f.call(tools.ToolCreateInvite, args))
}

// R-17T6-VIHI R-1L83-2ZN5
func TestSetAdminKeepsDesiredState(t *testing.T) {
	f := setup(t)
	h, _ := f.seedHost(true)
	f.seedAccount("acme", h.Name)
	f.seedAccount("beta", h.Name)
	u := tools.UserArgs{Account: "acme", User: "daffy"}
	args := tools.SetAdminArgs{Account: u.Account, User: u.User, Admin: true}
	if got := answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: u.Account, User: u.User, Key: key(1)})); got.Admin {
		t.Fatal(got)
	}
	for _, tc := range []struct{ account, user, text string }{
		{"missing", u.User, tools.NoAccount}, {u.Account, "absent", tools.NoUser}, {"beta", u.User, tools.NotInAccount},
	} {
		refusal(t, f.call(tools.ToolSetAdmin, tools.SetAdminArgs{Account: tc.account, User: tc.user, Admin: true}), tc.text)
	}
	f.syncHost(h.Name)
	before := f.host(h.Name).Version
	record := f.user(u.Account, u.User)
	for range 2 {
		got := answer[tools.UserChange](t, f.call(tools.ToolSetAdmin, args))
		if !got.Admin || got.Pending || got.State != store.UserActive || f.host(h.Name).Version != before {
			t.Fatal(got)
		}
	}
	record.Admin = true
	if after := f.user(u.Account, u.User); !reflect.DeepEqual(after, record) {
		t.Fatalf("user = %+v, want %+v", after, record)
	}
	list := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{Account: &u.Account}))
	if len(list.Users) != 1 || !list.Users[0].Admin {
		t.Fatal(list)
	}
	if got := answer[tools.UserChange](t, f.call(tools.ToolLockUser, u)); !got.Admin {
		t.Fatal(got)
	}
	removed := tools.UserKeyArgs{Account: u.Account, User: u.User, Key: key(1)}
	if got := answer[tools.KeyRemoved](t, f.call(tools.ToolRemoveKey, removed)); !got.Admin || got.KeysLeft != 0 {
		t.Fatal(got)
	}
	before = f.host(h.Name).Version
	if got := answer[tools.UserChange](t, f.call(tools.ToolSetAdmin, args)); !got.Admin || got.State != store.UserLocked || f.host(h.Name).Version != before {
		t.Fatal(got)
	}
	args.Admin = false
	if got := answer[tools.UserChange](t, f.call(tools.ToolSetAdmin, args)); got.Admin || got.State != store.UserLocked || f.host(h.Name).Version != before {
		t.Fatal(got)
	}
	answer[tools.UserChange](t, f.call(tools.ToolDeleteUser, u))
	refusal(t, f.call(tools.ToolSetAdmin, args), tools.UserRemoving)
}

// R-DKEZ-2SVN
func TestCentralUserAndKeyChangesTreatAdminsLikeUsers(t *testing.T) {
	operator, ordinary := setup(t), setup(t)
	k, err := valid.ParseKey(key(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*fixture{operator, ordinary} {
		h, _ := f.seedHost(true)
		f.seedAccount("acme", h.Name)
		_, err := f.st.AddUser(context.Background(), "acme", "daffy", store.Key{Type: k.Type, Body: k.Body, Line: k.Line()}, f.caller)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = operator.st.SetAdmin(context.Background(), "acme", "daffy", true)
	if err != nil {
		t.Fatal(err)
	}
	u := tools.UserArgs{Account: "acme", User: "daffy"}
	keyArgs := tools.UserKeyArgs{Account: u.Account, User: u.User, Key: key(1)}
	for _, c := range []callCase{
		{tools.ToolAddKey, tools.UserKeyArgs{Account: u.Account, User: u.User, Key: key(2)}, ""},
		{tools.ToolRemoveKey, tools.UserKeyArgs{Account: u.Account, User: u.User, Key: key(2)}, ""},
		{tools.ToolRemoveKey, keyArgs, ""},
		{tools.ToolDeleteUser, u, tools.UserNotLocked},
		{tools.ToolLockUser, u, ""},
		{tools.ToolUnlockUser, u, ""},
		{tools.ToolLockUser, u, ""},
		{tools.ToolDeleteUser, u, ""},
	} {
		left, right := operator.call(c.name, c.args), ordinary.call(c.name, c.args)
		if c.want != "" {
			refusal(t, left, c.want)
			refusal(t, right, c.want)
			continue
		}
		if c.name == tools.ToolRemoveKey {
			a, b := answer[tools.KeyRemoved](t, left), answer[tools.KeyRemoved](t, right)
			if !a.Admin || (c.args == keyArgs && a.KeysLeft != 0) {
				t.Fatal(a)
			}
			a.Admin = false
			if a != b {
				t.Fatal(a, b)
			}
		} else {
			a, b := answer[tools.UserChange](t, left), answer[tools.UserChange](t, right)
			if !a.Admin {
				t.Fatal(a)
			}
			a.Admin = false
			if a != b {
				t.Fatal(a, b)
			}
		}
	}
	a, b := allEvents(t, operator), allEvents(t, ordinary)
	if len(a) != len(b) {
		t.Fatal(a, b)
	}
	for i := range a {
		if a[i].Name != b[i].Name || !reflect.DeepEqual(a[i].Attrs, b[i].Attrs) {
			t.Fatal(a[i], b[i])
		}
	}
}

// R-EURW-22PN R-FWKR-WYP6 R-FXSO-AQFV R-FZ0K-OI6K
func TestAdminInviteTrail(t *testing.T) {
	base := accountsURL
	f := setupWithURL(t, true, func() (string, error) {
		if base == "" {
			return "", settings.ErrNoAccountsURL
		}
		return base, nil
	})
	f.serveLoopback()
	h, _ := f.seedHost(true)
	f.seedAccount("acme", h.Name)
	user := tools.UserArgs{Account: "acme", User: "daffy"}
	answer[tools.UserChange](t, f.call(tools.ToolAddUser, tools.UserKeyArgs{Account: user.Account, User: user.User, Key: key(1)}))
	start := len(allEvents(t, f))
	wants := []telemetry.Event{}
	change := func(tool, host, account, name, result string) telemetry.Event {
		return telemetry.Event{Name: trail.EventChangeFinished, Attrs: telemetry.Attrs{
			"tool": tool, "host": host, "account": account, "user": name, "ip": "", "result": result,
		}}
	}

	args := tools.SetAdminArgs{Account: user.Account, User: user.User, Admin: true}
	for range 2 {
		answer[tools.UserChange](t, f.call(tools.ToolSetAdmin, args))
	}
	wants = append(wants,
		telemetry.Event{Name: trail.EventAdminChanged, Attrs: telemetry.Attrs{"account": user.Account, "host": h.Name, "user": user.User, "admin": true}},
		change(tools.ToolSetAdmin, h.Name, user.Account, user.User, trail.ResultOK),
		change(tools.ToolSetAdmin, h.Name, user.Account, user.User, trail.ResultOK))

	admin := true
	makeArgs := tools.CreateInviteArgs{Account: user.Account, Admin: &admin}
	made := answer[tools.InviteMade](t, f.call(tools.ToolCreateInvite, makeArgs))
	answer[tools.InviteList](t, f.call(tools.ToolListInvites, tools.AccountArgs{Account: user.Account}))
	revoke := tools.InviteArgs{Invite: made.Invite}
	answer[tools.Invite](t, f.call(tools.ToolRevokeInvite, revoke))
	refusal(t, f.call(tools.ToolRevokeInvite, revoke), tools.NoInvite)
	base = ""
	refusal(t, f.call(tools.ToolCreateInvite, makeArgs), tools.NoLink)
	wants = append(wants,
		telemetry.Event{Name: trail.EventInviteCreated, Attrs: telemetry.Attrs{"invite": made.Invite, "account": user.Account, "host": h.Name, "admin": true}},
		change(tools.ToolCreateInvite, h.Name, user.Account, "", trail.ResultOK),
		telemetry.Event{Name: trail.EventInviteRevoked, Attrs: telemetry.Attrs{"invite": made.Invite, "account": user.Account, "host": h.Name}},
		change(tools.ToolRevokeInvite, h.Name, user.Account, "", trail.ResultOK),
		change(tools.ToolRevokeInvite, "", "", "", trail.ResultRefused),
		change(tools.ToolCreateInvite, "", user.Account, "", trail.ResultRefused))

	f.d.SetFailing(true)
	refusal(t, f.call(tools.ToolSetAdmin, args), store.Unreachable)
	f.d.SetFailing(false)
	wants = append(wants, change(tools.ToolSetAdmin, "", user.Account, user.User, trail.ResultFailed))
	if f.wakes != 0 || len(f.cloud.lookups) != 0 {
		t.Fatal(f.wakes, f.cloud.lookups)
	}
	events := allEvents(t, f)[start:]
	business := slices.DeleteFunc(events, func(e telemetry.Event) bool { return e.Name == "tool.called" })
	if len(business) != len(wants) {
		t.Fatal(business, wants)
	}
	for i, got := range business {
		if got.Name != wants[i].Name || got.User != f.caller.UserID || !reflect.DeepEqual(got.Attrs, wants[i].Attrs) {
			t.Fatalf("event %d = %+v, want %+v", i, got, wants[i])
		}
	}
}
