package tools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type fixedReader struct{}

func (fixedReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type fixture struct {
	t       *testing.T
	d       *db.DB
	path    string
	st      *store.Store
	h       *spy
	w       *telemetry.Writer
	capture *telemetry.Capture
	srv     *mcp.Server
	client  *mcp.Client
	caller  identity.Caller
	now     time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "accounts.db"), now: time.Date(2026, 10, 29, 9, 3, 7, 900000000, time.FixedZone("offset", -5*3600)), caller: identity.Caller{UserID: "first-caller", Email: "private@example.test", RequestID: "tool-request"}}
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: f.path, Migrations: accounts.Migrations(), Now: func() time.Time { return f.now }, Service: "accounts", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }})
	f.h = &spy{Host: host.NewMemory()}
	f.capture = &telemetry.Capture{}
	f.w = telemetry.New(telemetry.Config{Service: "accounts", Sink: f.capture, Now: func() time.Time { return f.now }, Rand: fixedReader{}, Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	t.Cleanup(func() { f.w.Shutdown(context.Background(), "done") })
	f.srv = mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	tools.Register(f.srv, tools.Config{Store: f.st, Host: f.h, Telemetry: f.w})
	server := httptest.NewServer(identity.Require(f.srv))
	t.Cleanup(server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client()})
	return f
}
func (f *fixture) call(name string, args any) mcp.Result {
	f.t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	r, err := f.client.CallTool(context.Background(), f.caller, name, b)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

type envelope struct {
	Content    []struct{ Type, Text string }
	Structured json.RawMessage `json:"structuredContent"`
}

func unpack(t *testing.T, r mcp.Result) envelope {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var e envelope
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}
func answer[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	e := unpack(t, r)
	if r.IsError() || len(e.Content) != 1 || e.Content[0].Type != "text" || !bytes.Equal([]byte(e.Content[0].Text), e.Structured) {
		t.Fatalf("bad success %+v", e)
	}
	var v T
	if err := json.Unmarshal(e.Structured, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func refusal(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	e := unpack(t, r)
	if !r.IsError() || len(e.Content) != 1 || e.Content[0].Type != "text" || e.Content[0].Text != want || len(e.Structured) != 0 {
		t.Fatalf("refusal %+v want %q", e, want)
	}
}
func (f *fixture) changes() []telemetry.Event {
	f.t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range f.capture.Events() {
		if e.Name == trail.EventChangeFinished {
			out = append(out, e)
		}
	}
	return out
}
func key(n byte) string {
	b := binary.BigEndian.AppendUint32(nil, 11)
	b = append(b, []byte("ssh-ed25519")...)
	b = binary.BigEndian.AppendUint32(b, 32)
	b = append(b, bytes.Repeat([]byte{n}, 32)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(b) + " comment with spaces"
}

type invocation struct {
	name   string
	args   []string
	caller identity.Caller
}
type spy struct {
	host.Host
	calls       []invocation
	err         error
	readErr     error
	groups      []host.Group
	users       []host.User
	members     *host.Members
	passthrough bool
}

func (s *spy) record(ctx context.Context, n string, a ...string) error {
	c, _ := identity.FromContext(ctx)
	s.calls = append(s.calls, invocation{n, a, c})
	return s.err
}
func (s *spy) GroupAdd(c context.Context, g string) error {
	if e := s.record(c, "GroupAdd", g); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.GroupAdd(c, g)
}
func (s *spy) GroupRemove(c context.Context, g string) error {
	if e := s.record(c, "GroupRemove", g); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.GroupRemove(c, g)
}
func (s *spy) UserAdd(c context.Context, u, g, k string) error {
	if e := s.record(c, "UserAdd", u, g, k); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserAdd(c, u, g, k)
}
func (s *spy) UserKeyAdd(c context.Context, u, k string) error {
	if e := s.record(c, "UserKeyAdd", u, k); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserKeyAdd(c, u, k)
}
func (s *spy) UserKeyRemove(c context.Context, u, k string) error {
	if e := s.record(c, "UserKeyRemove", u, k); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserKeyRemove(c, u, k)
}
func (s *spy) UserJoin(c context.Context, u, g string) error {
	if e := s.record(c, "UserJoin", u, g); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserJoin(c, u, g)
}
func (s *spy) UserLeave(c context.Context, u, g string) error {
	if e := s.record(c, "UserLeave", u, g); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserLeave(c, u, g)
}
func (s *spy) UserLock(c context.Context, u string) error {
	if e := s.record(c, "UserLock", u); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserLock(c, u)
}
func (s *spy) UserUnlock(c context.Context, u string) error {
	if e := s.record(c, "UserUnlock", u); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserUnlock(c, u)
}
func (s *spy) UserDelete(c context.Context, u string) error {
	if e := s.record(c, "UserDelete", u); e != nil {
		return e
	}
	if s.passthrough {
		return nil
	}
	return s.Host.UserDelete(c, u)
}
func (s *spy) GroupList(c context.Context) ([]host.Group, error) {
	if e := s.record(c, "GroupList"); e != nil {
		return nil, e
	}
	if s.readErr != nil {
		return nil, s.readErr
	}
	if s.groups != nil {
		return s.groups, nil
	}
	return s.Host.GroupList(c)
}
func (s *spy) GroupMembers(c context.Context, g string) (host.Members, error) {
	if e := s.record(c, "GroupMembers", g); e != nil {
		return host.Members{}, e
	}
	if s.readErr != nil {
		return host.Members{}, s.readErr
	}
	if s.members != nil {
		return *s.members, nil
	}
	return s.Host.GroupMembers(c, g)
}
func (s *spy) UserList(c context.Context) ([]host.User, error) {
	if e := s.record(c, "UserList"); e != nil {
		return nil, e
	}
	if s.readErr != nil {
		return nil, s.readErr
	}
	if s.users != nil {
		return s.users, nil
	}
	return s.Host.UserList(c)
}

type schema struct {
	Type, Description string
	Properties        map[string]schema
	Required          []string
	Items             *schema
}

func checkRequired(t *testing.T, s schema) {
	t.Helper()
	if s.Type == "object" {
		want := make([]string, 0)
		for n, v := range s.Properties {
			if n != "next" {
				want = append(want, n)
			}
			checkRequired(t, v)
		}
		got := append([]string(nil), s.Required...)
		if len(want) != len(got) {
			t.Fatalf("required %v properties %v", got, want)
		}
		for _, n := range want {
			found := false
			for _, r := range got {
				if r == n {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing required %s", n)
			}
		}
	}
	if s.Items != nil {
		checkRequired(t, *s.Items)
	}
}

// R-505N-G1QB R-51DJ-TTH0 R-52LG-7L7P R-5518-Z4P3 R-59WU-I7NV R-5B4Q-VZEK R-5CCN-9R59 R-5DKJ-NIVY R-5ESG-1AMN R-5G0C-F2DC R-5H88-SU41 R-5IG5-6LUQ R-5VV1-E30D R-636F-OPGJ R-64EC-2H78
func TestCatalogAndSchemas(t *testing.T) {
	f := setup(t)
	names := []string{tools.ToolGroups, tools.ToolGroup, tools.ToolCreateGroup, tools.ToolRemoveGroup, tools.ToolUsers, tools.ToolAddUser, tools.ToolAddKey, tools.ToolRemoveKey, tools.ToolAddMember, tools.ToolRemoveMember, tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser}
	if !reflect.DeepEqual(names, []string{"groups", "group", "create_group", "remove_group", "users", "add_user", "add_key", "remove_key", "add_member", "remove_member", "lock_user", "unlock_user", "delete_user"}) {
		t.Fatal(names)
	}
	descriptions := []string{tools.DescribeGroups, tools.DescribeGroup, tools.DescribeCreateGroup, tools.DescribeRemoveGroup, tools.DescribeUsers, tools.DescribeAddUser, tools.DescribeAddKey, tools.DescribeRemoveKey, tools.DescribeAddMember, tools.DescribeRemoveMember, tools.DescribeLockUser, tools.DescribeUnlockUser, tools.DescribeDeleteUser}
	effects := []mcp.Effect{mcp.Read, mcp.Read, mcp.Additive, mcp.Destructive, mcp.Read, mcp.Additive, mcp.Additive, mcp.Destructive, mcp.Additive, mcp.Destructive, mcp.Additive, mcp.Additive, mcp.Destructive}
	fields := [][]string{{"page", "limit"}, {"group"}, {"group"}, {"group"}, {"page", "limit"}, {"user", "group", "key"}, {"user", "key"}, {"user", "key"}, {"user", "group"}, {"user", "group"}, {"user"}, {"user"}, {"user"}}
	copyByField := map[string]string{"page": paging.PageDescription, "limit": paging.LimitDescription, "group": tools.ArgGroup, "user": tools.ArgUser, "key": tools.ArgKey}
	for _, s := range []string{tools.ArgGroup, tools.ArgUser, tools.ArgKey, tools.Failed, tools.KeysUnread} {
		if s == "" || strings.Contains(s, "\n") {
			t.Fatal(s)
		}
	}
	if tools.Failed == tools.KeysUnread || tools.Failed == store.Unreachable || tools.KeysUnread == store.Unreachable {
		t.Fatal("failure copy collision")
	}
	list, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	registered := f.srv.Tools()
	if len(list) != 13 || len(registered) != 13 {
		t.Fatal(len(list), len(registered))
	}
	for i, info := range list {
		if info.Name != names[i] || info.Description != descriptions[i] || info.Effect() != effects[i] || registered[i].Name != names[i] || registered[i].Description != descriptions[i] || tools.FirstLine(descriptions[i]) == "" {
			t.Fatalf("catalog entry %+v", info)
		}
		var in, out schema
		if err = json.Unmarshal(info.InputSchema, &in); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(info.OutputSchema, &out); err != nil {
			t.Fatal(err)
		}
		if len(in.Properties) != len(fields[i]) {
			t.Fatal(string(info.InputSchema))
		}
		last := -1
		for _, n := range fields[i] {
			v, ok := in.Properties[n]
			pos := bytes.Index(info.InputSchema, []byte(`"`+n+`":`))
			if !ok || v.Description != copyByField[n] || pos <= last {
				t.Fatalf("input schema %s", info.InputSchema)
			}
			last = pos
			if n == "limit" {
				if v.Type != "integer" {
					t.Fatal(v)
				}
			} else if v.Type != "string" {
				t.Fatal(v)
			}
		}
		wantRequired := fields[i]
		if i == 0 || i == 4 {
			wantRequired = nil
		}
		if len(in.Required) != len(wantRequired) {
			t.Fatal(string(info.InputSchema))
		}
		for _, n := range wantRequired {
			found := false
			for _, r := range in.Required {
				if n == r {
					found = true
				}
			}
			if !found {
				t.Fatal(string(info.InputSchema))
			}
		}
		checkRequired(t, out)
		if v, ok := out.Properties["next"]; ok && v.Description != paging.NextDescription {
			t.Fatal(v.Description)
		}
		valid := map[string]any{}
		for _, n := range fields[i] {
			switch n {
			case "page":
				valid[n] = ""
			case "limit":
				valid[n] = 1
			default:
				valid[n] = "value"
			}
		}
		invalid := map[string]any{}
		for n, v := range valid {
			invalid[n] = v
		}
		invalid["unknown"] = true
		before := len(f.h.calls)
		r := f.call(names[i], invalid)
		if !r.IsError() || len(f.h.calls) != before {
			t.Fatal("unknown member accepted", names[i])
		}
		for _, n := range fields[i] {
			wrong := map[string]any{}
			for k, v := range valid {
				wrong[k] = v
			}
			wrong[n] = []string{"wrong"}
			if !f.call(names[i], wrong).IsError() || len(f.h.calls) != before {
				t.Fatal("type accepted", names[i], n)
			}
		}
		for _, n := range wantRequired {
			missing := map[string]any{}
			for k, v := range valid {
				if k != n {
					missing[k] = v
				}
			}
			if !f.call(names[i], missing).IsError() || len(f.h.calls) != before {
				t.Fatal("missing accepted", names[i], n)
			}
		}
	}
	if len(f.changes()) != 0 {
		t.Fatal("invalid args emitted change")
	}
	for _, tc := range []struct{ in, want string }{{"first\nsecond", "first"}, {"", ""}, {"all", "all"}, {"\nremaining", ""}} {
		if tools.FirstLine(tc.in) != tc.want {
			t.Fatal(tc)
		}
	}
	// Use each declared argument type, including optional pointer fields.
	page := ""
	limit := 1
	answer[tools.GroupList](t, f.call(tools.ToolGroups, tools.GroupsArgs{Page: &page, Limit: &limit}))
	answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{Page: &page, Limit: &limit}))
}

// R-5JO1-KDLF R-5KVX-Y5C4 R-5NBQ-POTI R-5OJN-3GK7 R-5PRJ-H8AW R-5QZF-V01L R-5S7C-8RSA R-5TF8-MJIZ R-5UN5-0B9O R-5X2X-RUR2 R-5YAU-5MHR R-5ZIQ-JE8G R-60QM-X5Z5 R-6GLB-W6M6 R-6HT8-9YCV R-6QCI-YCJQ
func TestMemoryLifecycleAndPersistence(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	k := key(1)
	k2 := key(2)
	if got := answer[tools.GroupDone](t, f.call(tools.ToolCreateGroup, tools.GroupArgs{Group: "acme"})); got.Group != "acme" {
		t.Fatal(got)
	}
	first := f.now.UTC().Truncate(time.Second).Format(time.RFC3339)
	f.now = f.now.Add(time.Hour)
	f.caller.UserID = "second-caller"
	answer[tools.GroupDone](t, f.call(tools.ToolCreateGroup, tools.GroupArgs{Group: "acme"}))
	if got := answer[tools.MemberDone](t, f.call(tools.ToolAddUser, tools.AddUserArgs{User: "alice", Group: "acme", Key: k})); got != (tools.MemberDone{User: "alice", Group: "acme"}) {
		t.Fatal(got)
	}
	users := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{}))
	want := tools.User{Name: "alice", Groups: []string{"acme"}, Keys: []string{k}, State: host.StateActive, Created: f.now.UTC().Truncate(time.Second).Format(time.RFC3339), CreatedBy: "second-caller"}
	if !reflect.DeepEqual(users, tools.UserList{Users: []tools.User{want}}) {
		t.Fatalf("users %+v want %+v", users, want)
	}
	groups := answer[tools.GroupList](t, f.call(tools.ToolGroups, tools.GroupsArgs{}))
	if !reflect.DeepEqual(groups, tools.GroupList{Groups: []tools.Group{{Slug: "acme", MemberCount: 1, Created: first, CreatedBy: "first-caller"}}}) {
		t.Fatal(groups)
	}
	g := answer[tools.GroupRead](t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "acme"}))
	if !reflect.DeepEqual(g, tools.GroupRead{Slug: "acme", Members: []string{"alice"}, Created: first, CreatedBy: "first-caller"}) {
		t.Fatal(g)
	}
	// Reopen only through the public store; no table reads.
	d, err := db.Open(ctx, db.Config{Path: f.path, Migrations: accounts.Migrations(), Now: func() time.Time { return f.now }, Service: "accounts", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d, store.Config{Now: func() time.Time { return f.now }})
	savedGroups, err := st.Groups(ctx, []string{"acme"})
	if err != nil {
		t.Fatal(err)
	}
	savedUsers, err := st.Users(ctx, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if savedGroups["acme"].CreatedUserID != "first-caller" || savedGroups["acme"].Created.Format(time.RFC3339) != first || savedUsers["alice"].CreatedUserID != "second-caller" {
		t.Fatal(savedGroups, savedUsers)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{tools.ToolLockUser, tools.ToolUnlockUser} {
		got := answer[tools.UserDone](t, f.call(tool, tools.UserArgs{User: "alice"}))
		if got.User != "alice" {
			t.Fatal(got)
		}
		u := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{}))
		state := host.StateLocked
		if tool == tools.ToolUnlockUser {
			state = host.StateActive
		}
		if u.Users[0].State != state || !reflect.DeepEqual(u.Users[0].Groups, want.Groups) || !reflect.DeepEqual(u.Users[0].Keys, want.Keys) {
			t.Fatal(u)
		}
	}
	refusal(t, f.call(tools.ToolRemoveGroup, tools.GroupArgs{Group: "acme"}), host.RefuseGroupNotEmpty)
	refusal(t, f.call(tools.ToolDeleteUser, tools.UserArgs{User: "alice"}), host.RefuseUserNotLocked)
	answer[tools.UserDone](t, f.call(tools.ToolAddKey, tools.KeyArgs{User: "alice", Key: k2}))
	if got := answer[tools.KeyRemoved](t, f.call(tools.ToolRemoveKey, tools.KeyArgs{User: "alice", Key: k})); got.User != "alice" || got.NoKeyLeft {
		t.Fatal(got)
	}
	if got := answer[tools.KeyRemoved](t, f.call(tools.ToolRemoveKey, tools.KeyArgs{User: "alice", Key: k2})); !got.NoKeyLeft {
		t.Fatal(got)
	}
	answer[tools.GroupDone](t, f.call(tools.ToolCreateGroup, tools.GroupArgs{Group: "other"}))
	for _, tool := range []string{tools.ToolAddMember, tools.ToolRemoveMember} {
		got := answer[tools.MemberDone](t, f.call(tool, tools.MemberArgs{User: "alice", Group: "other"}))
		if got != (tools.MemberDone{User: "alice", Group: "other"}) {
			t.Fatal(got)
		}
	}
	answer[tools.UserDone](t, f.call(tools.ToolLockUser, tools.UserArgs{User: "alice"}))
	answer[tools.UserDone](t, f.call(tools.ToolDeleteUser, tools.UserArgs{User: "alice"}))
	for _, slug := range []string{"acme", "other"} {
		answer[tools.GroupDone](t, f.call(tools.ToolRemoveGroup, tools.GroupArgs{Group: slug}))
	}
	if got := answer[tools.GroupList](t, f.call(tools.ToolGroups, tools.GroupsArgs{})); len(got.Groups) != 0 {
		t.Fatal(got)
	}
	if got := answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{})); len(got.Users) != 0 {
		t.Fatal(got)
	}
	rg, err := f.st.Groups(ctx, []string{"acme", "other"})
	if err != nil || len(rg) != 0 {
		t.Fatal(rg, err)
	}
	ru, err := f.st.Users(ctx, []string{"alice"})
	if err != nil || len(ru) != 0 {
		t.Fatal(ru, err)
	}
}

// R-61YJ-AXPU R-AI7O-JCMY R-ADC3-09O6 R-AEJZ-E1EV R-AFRV-RT5K R-6FDF-IEVH R-AGZS-5KW9 R-AC46-MHXH
func TestPagingAndReadFailures(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	// The record includes hidden entries, while host lists contain 60 entries.
	if _, _, err := f.st.AddGroup(ctx, "hidden", "creator"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.AddUser(ctx, "hidden", "creator"); err != nil {
		t.Fatal(err)
	}
	for i := range 60 {
		name := fmt.Sprintf("entry-%02d", i)
		f.h.groups = append(f.h.groups, host.Group{Slug: name, Members: i})
		f.h.users = append(f.h.users, host.User{Name: name, Groups: []string{"z", "a"}, Keys: []string{"  opaque key comment ", "another"}, State: host.StateLocked, LastSignIn: time.Date(2026, 1, 2, 3, 4, 5, 123, time.FixedZone("offset", 3600))})
	}
	if _, _, err := f.st.AddGroup(ctx, "entry-01", "group-creator"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.AddUser(ctx, "entry-01", "user-creator"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{tools.ToolGroups, tools.ToolUsers} {
		var page *string
		seen := 0
		for {
			args := map[string]any{"page": page}
			if name == tools.ToolGroups {
				out := answer[tools.GroupList](t, f.call(name, args))
				for _, g := range out.Groups {
					want := f.h.groups[seen]
					if g.Slug != want.Slug || g.MemberCount != want.Members {
						t.Fatal(g)
					}
					if seen == 1 {
						if g.CreatedBy != "group-creator" || g.Created != f.now.UTC().Truncate(time.Second).Format(time.RFC3339) {
							t.Fatal(g)
						}
					} else if g.Created != "" || g.CreatedBy != "" {
						t.Fatal(g)
					}
					seen++
				}
				page = out.Next
			} else {
				out := answer[tools.UserList](t, f.call(name, args))
				for _, u := range out.Users {
					want := f.h.users[seen]
					if u.Name != want.Name || !reflect.DeepEqual(u.Groups, want.Groups) || !reflect.DeepEqual(u.Keys, want.Keys) || u.State != want.State || u.LastSignIn != want.LastSignIn.UTC().Format(time.RFC3339) {
						t.Fatal(u)
					}
					if seen == 1 {
						if u.CreatedBy != "user-creator" {
							t.Fatal(u)
						}
					} else if u.Created != "" || u.CreatedBy != "" {
						t.Fatal(u)
					}
					seen++
				}
				page = out.Next
			}
			if page == nil {
				break
			}
			var position string
			if err := (paging.Token{Tool: name}).Decode(*page, &position); err != nil || position != fmt.Sprintf("entry-%02d", seen-1) {
				t.Fatal(position, err)
			}
		}
		if seen != 60 {
			t.Fatal(seen)
		}
	}
	f.h.members = &host.Members{Group: "entry-01", Members: []string{"z", "a"}}
	got := answer[tools.GroupRead](t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "entry-01"}))
	if !reflect.DeepEqual(got.Members, []string{"z", "a"}) || got.CreatedBy != "group-creator" {
		t.Fatal(got)
	}
	f.h.members = &host.Members{Group: "unrecorded"}
	got = answer[tools.GroupRead](t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "unrecorded"}))
	if got.Created != "" || got.CreatedBy != "" || got.Members == nil || len(got.Members) != 0 {
		t.Fatal(got)
	}
	f.d.SetFailing(true)
	before := len(f.h.calls)
	for _, name := range []string{tools.ToolGroups, tools.ToolUsers} {
		for _, limit := range []int{0, -1, paging.MaxLimit + 1} {
			_, e := paging.Window(&limit)
			refusal(t, f.call(name, map[string]any{"page": "bogus", "limit": limit}), e.Error())
		}
		refusal(t, f.call(name, map[string]any{"page": "bogus"}), paging.BadPage)
		other := tools.ToolGroups
		if name == other {
			other = tools.ToolUsers
		}
		refusal(t, f.call(name, map[string]any{"page": (paging.Token{Tool: other}).Encode("entry-00")}), paging.BadPage)
	}
	if len(f.h.calls) != before {
		t.Fatal("bad paging reached host")
	}
	for _, name := range []string{tools.ToolGroups, tools.ToolUsers} {
		refusal(t, f.call(name, map[string]any{"page": nil, "limit": nil}), store.Unreachable)
		refusal(t, f.call(name, map[string]any{"page": ""}), store.Unreachable)
	}
	refusal(t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "unrecorded"}), store.Unreachable)
	f.h.groups = []host.Group{}
	f.h.users = []host.User{}
	answer[tools.GroupList](t, f.call(tools.ToolGroups, tools.GroupsArgs{}))
	answer[tools.UserList](t, f.call(tools.ToolUsers, tools.UsersArgs{}))
	// A position past every host entry also avoids the record entirely.
	f.h.groups = []host.Group{{Slug: "a"}}
	f.h.users = []host.User{{Name: "a"}}
	answer[tools.GroupList](t, f.call(tools.ToolGroups, map[string]any{"page": (paging.Token{Tool: tools.ToolGroups}).Encode("z")}))
	answer[tools.UserList](t, f.call(tools.ToolUsers, map[string]any{"page": (paging.Token{Tool: tools.ToolUsers}).Encode("z")}))
	f.d.SetFailing(false)
	if len(f.changes()) != 0 {
		t.Fatal("reads emitted changes")
	}
	for _, c := range f.h.calls {
		if c.name != "GroupList" && c.name != "GroupMembers" && c.name != "UserList" {
			t.Fatal("read mutated", c)
		}
	}
	saved, err := f.st.Groups(ctx, []string{"hidden", "entry-01"})
	if err != nil || len(saved) != 2 {
		t.Fatal(saved, err)
	}
}

type changeCase struct {
	tool, method, group, user string
	args                      any
	hostArgs                  []string
}

func changeCases() []changeCase {
	return []changeCase{
		{tools.ToolCreateGroup, "GroupAdd", " raw group ", "", tools.GroupArgs{Group: " raw group "}, []string{" raw group "}},
		{tools.ToolRemoveGroup, "GroupRemove", " raw group ", "", tools.GroupArgs{Group: " raw group "}, []string{" raw group "}},
		{tools.ToolAddUser, "UserAdd", " raw group ", " raw user ", tools.AddUserArgs{User: " raw user ", Group: " raw group ", Key: "  key comment spaces 'quoted' "}, []string{" raw user ", " raw group ", "  key comment spaces 'quoted' "}},
		{tools.ToolAddKey, "UserKeyAdd", "", " raw user ", tools.KeyArgs{User: " raw user ", Key: " key 'bytes' "}, []string{" raw user ", " key 'bytes' "}},
		{tools.ToolRemoveKey, "UserKeyRemove", "", " raw user ", tools.KeyArgs{User: " raw user ", Key: " key 'bytes' "}, []string{" raw user ", " key 'bytes' "}},
		{tools.ToolAddMember, "UserJoin", " raw group ", " raw user ", tools.MemberArgs{User: " raw user ", Group: " raw group "}, []string{" raw user ", " raw group "}},
		{tools.ToolRemoveMember, "UserLeave", " raw group ", " raw user ", tools.MemberArgs{User: " raw user ", Group: " raw group "}, []string{" raw user ", " raw group "}},
		{tools.ToolLockUser, "UserLock", "", " raw user ", tools.UserArgs{User: " raw user "}, []string{" raw user "}},
		{tools.ToolUnlockUser, "UserUnlock", "", " raw user ", tools.UserArgs{User: " raw user "}, []string{" raw user "}},
		{tools.ToolDeleteUser, "UserDelete", "", " raw user ", tools.UserArgs{User: " raw user "}, []string{" raw user "}},
	}
}

// R-6BPQ-D3NE R-6CXM-QVE3 R-6E5J-4N4S R-6J14-NQ3K R-AAWA-8Q6S
func TestChangingOutcomesAndTrail(t *testing.T) {
	for _, tc := range changeCases() {
		t.Run(tc.tool, func(t *testing.T) {
			f := setup(t)
			f.h.passthrough = true
			f.h.users = []host.User{}
			for _, outcome := range []struct {
				err          error
				want, result string
				failing      bool
			}{
				{nil, "", trail.ResultOK, false},
				{fmt.Errorf("wrapped: %w", &host.RefusedError{Status: 4, Diagnostic: " bytes of refusal  "}), " bytes of refusal  ", trail.ResultRefused, false},
				{&host.FailedError{Status: 1}, tools.Failed, trail.ResultFailed, false},
				{errors.New("private host failure"), tools.Failed, trail.ResultFailed, false},
				{&host.RefusedError{Status: 2, Diagnostic: "refusal over failed record"}, "refusal over failed record", trail.ResultRefused, true},
				{errors.New("host failure over failed record"), tools.Failed, trail.ResultFailed, true},
				{nil, "", trail.ResultOK, true},
			} {
				// Seed entries so a refusal must preserve both records.
				f.d.SetFailing(false)
				if _, _, err := f.st.AddGroup(context.Background(), " raw group ", "original"); err != nil {
					t.Fatal(err)
				}
				if _, _, err := f.st.AddUser(context.Background(), " raw user ", "original"); err != nil {
					t.Fatal(err)
				}
				f.h.err = outcome.err
				f.d.SetFailing(outcome.failing)
				before := len(f.h.calls)
				events := len(f.changes())
				r := f.call(tc.tool, tc.args)
				want, result := outcome.want, outcome.result
				records := tc.tool == tools.ToolCreateGroup || tc.tool == tools.ToolRemoveGroup || tc.tool == tools.ToolAddUser || tc.tool == tools.ToolDeleteUser
				if outcome.err == nil && outcome.failing && records {
					want = store.Unreachable
					result = trail.ResultFailed
				}
				if want != "" {
					refusal(t, r, want)
				} else {
					switch tc.tool {
					case tools.ToolCreateGroup, tools.ToolRemoveGroup:
						answer[tools.GroupDone](t, r)
					case tools.ToolAddUser, tools.ToolAddMember, tools.ToolRemoveMember:
						answer[tools.MemberDone](t, r)
					case tools.ToolRemoveKey:
						answer[tools.KeyRemoved](t, r)
					default:
						answer[tools.UserDone](t, r)
					}
				}
				calls := f.h.calls[before:]
				n := 1
				if tc.tool == tools.ToolRemoveKey && outcome.err == nil {
					n = 2
				}
				if len(calls) != n || calls[0].name != tc.method || !reflect.DeepEqual(calls[0].args, tc.hostArgs) || calls[0].caller != f.caller {
					t.Fatalf("host calls %+v", calls)
				}
				if n == 2 && calls[1].name != "UserList" {
					t.Fatal(calls)
				}
				changes := f.changes()
				if len(changes) != events+1 {
					t.Fatalf("changes %d want %d", len(changes), events+1)
				}
				e := changes[len(changes)-1]
				if e.User != f.caller.UserID || e.RequestID != f.caller.RequestID || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"tool": tc.tool, "group": tc.group, "user": tc.user, "result": result}) {
					t.Fatalf("change %+v", e)
				}
				f.d.SetFailing(false)
				if outcome.err != nil || !records {
					g, err := f.st.Groups(context.Background(), []string{" raw group "})
					if err != nil || g[" raw group "].CreatedUserID != "original" {
						t.Fatal(g, err)
					}
					u, err := f.st.Users(context.Background(), []string{" raw user "})
					if err != nil || u[" raw user "].CreatedUserID != "original" {
						t.Fatal(u, err)
					}
				}
			}
		})
	}
}

// R-6LGX-F9KY
func TestRemainingKeysAndFailedFollowup(t *testing.T) {
	f := setup(t)
	f.h.passthrough = true
	for _, users := range [][]host.User{{{Name: "alice", Keys: []string{"remaining"}}}, {{Name: "alice", Keys: []string{}}}, {{Name: "other", Keys: []string{"other"}}}} {
		f.h.users = users
		got := answer[tools.KeyRemoved](t, f.call(tools.ToolRemoveKey, tools.KeyArgs{User: "alice", Key: "opaque"}))
		want := len(users) == 0 || users[0].Name != "alice" || len(users[0].Keys) == 0
		if got != (tools.KeyRemoved{User: "alice", NoKeyLeft: want}) {
			t.Fatal(got)
		}
	}
	for _, err := range []error{&host.RefusedError{Status: 4, Diagnostic: "refused list"}, &host.FailedError{Status: 1}, errors.New("list failed")} {
		f.h.readErr = err
		before := len(f.h.calls)
		refusal(t, f.call(tools.ToolRemoveKey, tools.KeyArgs{User: "alice", Key: "opaque"}), tools.KeysUnread)
		calls := f.h.calls[before:]
		if len(calls) != 2 || calls[0].name != "UserKeyRemove" || calls[1].name != "UserList" {
			t.Fatal(calls)
		}
		events := f.changes()
		if events[len(events)-1].Attrs["result"] != trail.ResultFailed {
			t.Fatal(events)
		}
	}
}

func wire(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	e := unpack(t, r)
	if r.IsError() || len(e.Content) != 1 || e.Content[0].Text != want || string(e.Structured) != want {
		t.Fatalf("wire %s want %s", e.Structured, want)
	}
}

// R-5JO1-KDLF R-5KVX-Y5C4 R-5NBQ-POTI R-5OJN-3GK7 R-5PRJ-H8AW R-5QZF-V01L R-5S7C-8RSA R-5TF8-MJIZ R-5UN5-0B9O R-5X2X-RUR2
func TestOutputWireMembersAndOrder(t *testing.T) {
	f := setup(t)
	f.h.passthrough = true
	f.h.groups = []host.Group{{Slug: "group", Members: 2}, {Slug: "later", Members: 0}}
	f.h.users = []host.User{{Name: "user", State: host.StateActive}, {Name: "z", State: host.StateLocked}}
	f.h.members = &host.Members{Group: "group"}
	token := (paging.Token{Tool: tools.ToolGroups}).Encode("group")
	wire(t, f.call(tools.ToolGroups, map[string]any{"limit": 1}), `{"groups":[{"slug":"group","member_count":2,"created":"","created_by":""}],"next":"`+token+`"}`)
	token = (paging.Token{Tool: tools.ToolUsers}).Encode("user")
	wire(t, f.call(tools.ToolUsers, map[string]any{"limit": 1}), `{"users":[{"name":"user","groups":[],"keys":[],"state":"active","last_sign_in":"","created":"","created_by":""}],"next":"`+token+`"}`)
	wire(t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "group"}), `{"slug":"group","members":[],"created":"","created_by":""}`)
	for _, tool := range []string{tools.ToolCreateGroup, tools.ToolRemoveGroup} {
		wire(t, f.call(tool, tools.GroupArgs{Group: "group"}), `{"group":"group"}`)
	}
	wire(t, f.call(tools.ToolAddUser, tools.AddUserArgs{User: "user", Group: "group", Key: "opaque"}), `{"user":"user","group":"group"}`)
	wire(t, f.call(tools.ToolAddKey, tools.KeyArgs{User: "user", Key: "opaque"}), `{"user":"user"}`)
	wire(t, f.call(tools.ToolRemoveKey, tools.KeyArgs{User: "user", Key: "opaque"}), `{"user":"user","no_key_left":true}`)
	for _, tool := range []string{tools.ToolAddMember, tools.ToolRemoveMember} {
		wire(t, f.call(tool, tools.MemberArgs{User: "user", Group: "group"}), `{"user":"user","group":"group"}`)
	}
	for _, tool := range []string{tools.ToolLockUser, tools.ToolUnlockUser, tools.ToolDeleteUser} {
		wire(t, f.call(tool, tools.UserArgs{User: "user"}), `{"user":"user"}`)
	}
	f.h.groups = []host.Group{}
	f.h.users = []host.User{}
	wire(t, f.call(tools.ToolGroups, tools.GroupsArgs{}), `{"groups":[]}`)
	wire(t, f.call(tools.ToolUsers, tools.UsersArgs{}), `{"users":[]}`)
	// A group's members are always supplied, including an empty list.
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var previous string
	for _, e := range f.capture.Events() {
		if e.Name == "tool.called" {
			if e.Attrs["tool"] == tools.ToolCreateGroup && previous != trail.EventChangeFinished {
				t.Fatalf("event order %+v", e)
			}
		}
		previous = e.Name
	}
}

// R-6BPQ-D3NE R-6CXM-QVE3 R-6J14-NQ3K R-AGZS-5KW9 R-6E5J-4N4S R-AC46-MHXH
func TestRefusalsAndHostChangeSurvivesRecordFailure(t *testing.T) {
	f := setup(t)
	f.d.SetFailing(true)
	refusal(t, f.call(tools.ToolCreateGroup, tools.GroupArgs{Group: "acme"}), store.Unreachable)
	f.d.SetFailing(false)
	g := answer[tools.GroupList](t, f.call(tools.ToolGroups, tools.GroupsArgs{}))
	if len(g.Groups) != 1 || g.Groups[0].Slug != "acme" || g.Groups[0].Created != "" || g.Groups[0].CreatedBy != "" {
		t.Fatal(g)
	}
	f.h.passthrough = true
	for _, tc := range []struct {
		tool   string
		args   any
		method string
	}{{tools.ToolCreateGroup, tools.GroupArgs{Group: ""}, "GroupAdd"}, {tools.ToolRemoveGroup, tools.GroupArgs{Group: ""}, "GroupRemove"}, {tools.ToolAddUser, tools.AddUserArgs{User: "", Group: "acme", Key: "key"}, "UserAdd"}, {tools.ToolDeleteUser, tools.UserArgs{User: ""}, "UserDelete"}} {
		before := len(f.h.calls)
		refusal(t, f.call(tc.tool, tc.args), store.Unreachable)
		if len(f.h.calls) != before+1 || f.h.calls[before].name != tc.method || f.h.calls[before].args[0] != "" {
			t.Fatal(f.h.calls[before:])
		}
	}
	// Read host failures take precedence over a failing record, relay refusals exactly,
	// and must never leave a change event.
	changes := len(f.changes())
	f.d.SetFailing(true)
	for _, e := range []error{&host.RefusedError{Status: 4, Diagnostic: " exact diagnostic "}, &host.FailedError{Status: 0}, errors.New("plain failure")} {
		f.h.err = e
		want := tools.Failed
		var refused *host.RefusedError
		if errors.As(e, &refused) {
			want = refused.Diagnostic
		}
		refusal(t, f.call(tools.ToolGroups, tools.GroupsArgs{}), want)
		refusal(t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "acme"}), want)
		refusal(t, f.call(tools.ToolUsers, tools.UsersArgs{}), want)
	}
	if len(f.changes()) != changes {
		t.Fatal("read failure emitted change")
	}
	f.h.err = nil
	f.d.SetFailing(false)
	// Missing executable is a host failure; no group record is created.
	f.h.Host = host.NewCommand([]string{filepath.Join(t.TempDir(), "missing")})
	f.h.passthrough = false
	refusal(t, f.call(tools.ToolCreateGroup, tools.GroupArgs{Group: "missing"}), tools.Failed)
	records, err := f.st.Groups(context.Background(), []string{"missing"})
	if err != nil || len(records) != 0 {
		t.Fatal(records, err)
	}
}

// R-AGZS-5KW9 R-ADC3-09O6 R-AEJZ-E1EV
func TestClosedRecordReads(t *testing.T) {
	f := setup(t)
	f.h.groups = []host.Group{{Slug: "a"}}
	f.h.users = []host.User{{Name: "a"}}
	f.h.members = &host.Members{Group: "a"}
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call(tools.ToolGroups, tools.GroupsArgs{}), store.Unreachable)
	refusal(t, f.call(tools.ToolUsers, tools.UsersArgs{}), store.Unreachable)
	refusal(t, f.call(tools.ToolGroup, tools.GroupArgs{Group: "a"}), store.Unreachable)
	f.h.groups = []host.Group{}
	f.h.users = []host.User{}
	wire(t, f.call(tools.ToolGroups, tools.GroupsArgs{}), `{"groups":[]}`)
	wire(t, f.call(tools.ToolUsers, tools.UsersArgs{}), `{"users":[]}`)
}

// R-505N-G1QB R-59WU-I7NV R-5B4Q-VZEK R-5CCN-9R59 R-5DKJ-NIVY R-5ESG-1AMN R-5G0C-F2DC R-5H88-SU41 R-5JO1-KDLF R-5KVX-Y5C4 R-5NBQ-POTI R-5OJN-3GK7 R-5PRJ-H8AW R-5QZF-V01L R-5S7C-8RSA R-5TF8-MJIZ R-5UN5-0B9O
func TestExactDeclaredShapes(t *testing.T) {
	t.Helper()
	// A struct conversion requires identical names, types and field order,
	// ignoring tags. An extra field therefore makes these uses fail to compile.
	_ = struct {
		Store     *store.Store
		Host      host.Host
		Telemetry *telemetry.Writer
	}(tools.Config{})
	_ = struct {
		Page  *string
		Limit *int
	}(tools.GroupsArgs{})
	_ = struct {
		Page  *string
		Limit *int
	}(tools.UsersArgs{})
	_ = struct{ Group string }(tools.GroupArgs{})
	_ = struct{ User string }(tools.UserArgs{})
	_ = struct{ User, Group, Key string }(tools.AddUserArgs{})
	_ = struct{ User, Key string }(tools.KeyArgs{})
	_ = struct{ User, Group string }(tools.MemberArgs{})
	_ = struct {
		Slug               string
		MemberCount        int
		Created, CreatedBy string
	}(tools.Group{})
	_ = struct {
		Groups []tools.Group
		Next   *string
	}(tools.GroupList{})
	_ = struct {
		Slug               string
		Members            []string
		Created, CreatedBy string
	}(tools.GroupRead{})
	_ = struct {
		Name                                  string
		Groups, Keys                          []string
		State, LastSignIn, Created, CreatedBy string
	}(tools.User{})
	_ = struct {
		Users []tools.User
		Next  *string
	}(tools.UserList{})
	_ = struct{ Group string }(tools.GroupDone{})
	_ = struct{ User string }(tools.UserDone{})
	_ = struct{ User, Group string }(tools.MemberDone{})
	_ = struct {
		User      string
		NoKeyLeft bool
	}(tools.KeyRemoved{})
}

type toolName string

// R-5518-Z4P3
func TestToolConstantsAreUntyped(t *testing.T) {
	t.Helper()
	// Each initializer must be a constant, and a typed string constant cannot
	// be assigned to this distinct named type.
	const (
		_ toolName = tools.ToolGroups
		_ toolName = tools.ToolGroup
		_ toolName = tools.ToolCreateGroup
		_ toolName = tools.ToolRemoveGroup
		_ toolName = tools.ToolUsers
		_ toolName = tools.ToolAddUser
		_ toolName = tools.ToolAddKey
		_ toolName = tools.ToolRemoveKey
		_ toolName = tools.ToolAddMember
		_ toolName = tools.ToolRemoveMember
		_ toolName = tools.ToolLockUser
		_ toolName = tools.ToolUnlockUser
		_ toolName = tools.ToolDeleteUser
	)
	_ = []toolName{
		tools.ToolGroups,
		tools.ToolGroup,
		tools.ToolCreateGroup,
		tools.ToolRemoveGroup,
		tools.ToolUsers,
		tools.ToolAddUser,
		tools.ToolAddKey,
		tools.ToolRemoveKey,
		tools.ToolAddMember,
		tools.ToolRemoveMember,
		tools.ToolLockUser,
		tools.ToolUnlockUser,
		tools.ToolDeleteUser,
	}
}
