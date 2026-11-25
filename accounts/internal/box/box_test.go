package box_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const (
	acme    = "acme"
	beta    = "beta"
	daffy   = "daffy"
	bugs    = "bugs"
	elmer   = "elmer"
	baseURL = "https://accounts.test"
)

type idleCloud struct{ cloud.Cloud }

type fixture struct {
	t       *testing.T
	d       *db.DB
	st      *store.Store
	w       *telemetry.Writer
	capture *telemetry.Capture
	now     time.Time
	host    string
	base    func() (string, error)
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{t: t, now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.FixedZone("fixture", -5*60*60)),
		capture: &telemetry.Capture{}, base: func() (string, error) { return baseURL, nil }}
	var err error
	f.d, err = db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "accounts.db"), Migrations: accounts.Migrations(), Service: "accounts", Now: func() time.Time { return f.now }, Stderr: io.Discard})
	must(t, err)
	t.Cleanup(func() { must(t, f.d.Close()) })
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rand.NewChaCha8([32]byte{1})})
	f.w = telemetry.New(telemetry.Config{Service: "accounts", Sink: f.capture, Now: func() time.Time { return f.now }, Rand: rand.NewChaCha8([32]byte{2}), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	t.Cleanup(func() { f.w.Shutdown(context.Background(), "done") })
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func key(i byte) string {
	wire := make([]byte, 4+len("ssh-ed25519")+4+32)
	binary.BigEndian.PutUint32(wire, uint32(len("ssh-ed25519")))
	copy(wire[4:], "ssh-ed25519")
	binary.BigEndian.PutUint32(wire[4+len("ssh-ed25519"):], 32)
	wire[len(wire)-1] = i
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire) + " fixture"
}

func (f *fixture) seed() {
	f.t.Helper()
	ctx := f.t.Context()
	creator := identity.Caller{UserID: "central-creator"}
	h, _, err := f.st.CreateHost(ctx, store.HostSpec{Type: valid.DefaultType, RootGB: valid.DefaultRootGB, DataGB: valid.DefaultDataGB}, creator)
	must(f.t, err)
	f.host = h.Name
	for _, slug := range []string{acme, beta} {
		_, _, err := f.st.PlaceAccount(ctx, slug, h.Name, creator)
		must(f.t, err)
	}
	for i, u := range []struct{ account, name string }{{acme, daffy}, {acme, bugs}, {beta, elmer}} {
		k, err := valid.ParseKey(key(byte(i + 1)))
		must(f.t, err)
		_, err = f.st.AddUser(ctx, u.account, u.name, store.Key{Type: k.Type, Body: k.Body, Line: k.Line()}, creator)
		must(f.t, err)
	}
	_, _, err = f.st.SetAdmin(ctx, acme, daffy, true)
	must(f.t, err)
}

func (f *fixture) connect(srv *mcp.Server) *mcp.Client {
	f.t.Helper()
	http := httptest.NewServer(identity.Require(srv))
	f.t.Cleanup(http.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: http.URL + "/mcp", HTTPClient: http.Client()})
}

func (f *fixture) box(admin bool) (*mcp.Client, *mcp.Server) {
	f.t.Helper()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	cfg := box.Config{Store: f.st, Telemetry: f.w, AccountsURL: f.base}
	if admin {
		box.RegisterAdmin(srv, cfg)
	} else {
		box.RegisterUser(srv, cfg)
	}
	return f.connect(srv), srv
}

func (f *fixture) central() *mcp.Client {
	f.t.Helper()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	tools.Register(srv, tools.Config{Store: f.st, Configured: true, Cloud: idleCloud{}, Wake: func() { f.t.Error("unexpected engine wake") }, Telemetry: f.w, AccountsURL: f.base})
	return f.connect(srv)
}

func call(t *testing.T, client *mcp.Client, userID, name string, args any) mcp.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	must(t, err)
	r, err := client.CallTool(t.Context(), identity.Caller{UserID: userID, RequestID: "box-test"}, name, raw)
	must(t, err)
	return r
}

func as(t *testing.T, client *mcp.Client, name, tool string, args any) mcp.Result {
	t.Helper()
	return call(t, client, box.ActorPrefix+name, tool, args)
}

type envelope struct {
	Content    []struct{ Type, Text string }
	Structured json.RawMessage `json:"structuredContent"`
}

func wire(t *testing.T, r mcp.Result) envelope {
	t.Helper()
	raw, err := json.Marshal(r)
	must(t, err)
	var out envelope
	must(t, json.Unmarshal(raw, &out))
	return out
}
func answer[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	e := wire(t, r)
	if r.IsError() || len(e.Content) != 1 || e.Content[0].Type != "text" || !bytes.Equal([]byte(e.Content[0].Text), e.Structured) {
		t.Fatalf("expected a structured success, got %+v", e)
	}
	var out T
	must(t, json.Unmarshal(e.Structured, &out))
	return out
}
func refusal(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	e := wire(t, r)
	if !r.IsError() || len(e.Content) != 1 || e.Content[0].Type != "text" || e.Content[0].Text != want || len(e.Structured) != 0 {
		t.Fatalf("refusal %+v, want %q", e, want)
	}
}

func (f *fixture) events() []telemetry.Event {
	f.t.Helper()
	must(f.t, f.w.Flush(f.t.Context()))
	return f.capture.Events()
}

type toolSpec struct {
	name, description string
	effect            mcp.Effect
	args              map[string]any
	required          []string
}

func catalog() []toolSpec {
	user := map[string]any{"user": bugs}
	userKey := map[string]any{"key": key(1)}
	return []toolSpec{
		{box.ToolKeys, box.DescribeKeys, mcp.Read, nil, nil},
		{tools.ToolAddKey, box.DescribeAddKey, mcp.Additive, userKey, []string{"key"}},
		{tools.ToolRemoveKey, box.DescribeRemoveKey, mcp.Destructive, userKey, []string{"key"}},
		{tools.ToolUsers, box.DescribeUsers, mcp.Read, nil, nil},
		{tools.ToolLockUser, box.DescribeLockUser, mcp.Additive, user, []string{"user"}},
		{tools.ToolUnlockUser, box.DescribeUnlockUser, mcp.Additive, user, []string{"user"}},
		{tools.ToolDeleteUser, box.DescribeDeleteUser, mcp.Destructive, user, []string{"user"}},
		{tools.ToolSetAdmin, box.DescribeSetAdmin, mcp.Additive, map[string]any{"user": bugs, "admin": true}, []string{"user", "admin"}},
		{tools.ToolCreateInvite, box.DescribeCreateInvite, mcp.Additive, nil, nil},
		{tools.ToolListInvites, box.DescribeListInvites, mcp.Read, nil, nil},
		{tools.ToolRevokeInvite, box.DescribeRevokeInvite, mcp.Destructive, map[string]any{"invite": "iv_0"}, []string{"invite"}},
	}
}

// R-ZRW5-2YDQ R-ZT41-GQ4F R-ZUBX-UHV4 R-ZVJU-89LT R-ZXZM-ZT37 R-00FF-RCKL R-01NC-54BA R-0434-WNSO R-06IX-O7A2 R-07QU-1Z0R R-0XCQ-35LC
func TestCatalog(t *testing.T) {
	_ = struct {
		Store       *store.Store
		Telemetry   *telemetry.Writer
		AccountsURL func() (string, error)
	}(box.Config{})
	_ = struct{}(box.NoArgs{})
	_ = struct{ Key string }(box.KeyArgs{})
	_ = struct{ User string }(box.UserArgs{})
	_ = struct {
		User  string
		Admin bool
	}(box.SetAdminArgs{})
	const actorPrefix = box.ActorPrefix
	const keys = box.ToolKeys
	const userCopy = box.ArgUser
	if actorPrefix != "ssh:" || keys != "keys" || userCopy == "" || bytes.ContainsRune([]byte(userCopy), '\n') {
		t.Fatal(actorPrefix, keys, userCopy)
	}
	f := setup(t)
	cfg := catalog()
	for _, tier := range []struct {
		admin bool
		count int
	}{{false, 3}, {true, 11}} {
		client, srv := f.box(tier.admin)
		list, err := client.ListTools(t.Context(), identity.Caller{UserID: box.ActorPrefix + daffy})
		must(t, err)
		registered := srv.Tools()
		if len(list) != tier.count || len(registered) != tier.count {
			t.Fatal(list, registered)
		}
		for i, info := range list {
			want := cfg[i]
			if info.Name != want.name || registered[i].Name != want.name || registered[i].Description != want.description ||
				info.Description != want.description || tools.FirstLine(want.description) == "" || info.Effect() != want.effect {
				t.Fatal(info, registered[i], want)
			}
			checkInput(t, info, want)
			checkOutput(t, info.OutputSchema)
			testArgumentRefusals(t, client, want)
		}
	}
	for _, e := range f.events() {
		if e.Name != "tool.called" {
			t.Fatal(e)
		}
	}
}

type schema struct {
	Type, Description string
	Properties        map[string]schema
	Required          []string
	Items             *schema
}

func checkInput(t *testing.T, info mcp.ToolInfo, want toolSpec) {
	t.Helper()
	var in schema
	must(t, json.Unmarshal(info.InputSchema, &in))
	if in.Type != "object" || len(in.Properties) != len(want.required) || !slices.Equal(in.Required, want.required) {
		t.Fatalf("input for %s = %s", want.name, info.InputSchema)
	}
	descriptions := map[string]string{"key": tools.ArgKey, "user": box.ArgUser, "admin": tools.ArgAdmin, "invite": tools.ArgInvite}
	for _, field := range want.required {
		kind := "string"
		if field == "admin" {
			kind = "boolean"
		}
		if p := in.Properties[field]; p.Type != kind || p.Description != descriptions[field] {
			t.Fatalf("input for %s.%s = %s", want.name, field, info.InputSchema)
		}
	}
}

func checkOutput(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var out schema
	must(t, json.Unmarshal(raw, &out))
	if _, paged := out.Properties["next"]; paged {
		t.Fatalf("output schema declares next: %s", raw)
	}
	checkRequired(t, out)
}
func checkRequired(t *testing.T, s schema) {
	t.Helper()
	if len(s.Properties) != len(s.Required) {
		t.Fatal(s)
	}
	for name, child := range s.Properties {
		if !slices.Contains(s.Required, name) {
			t.Fatal(name, s)
		}
		checkRequired(t, child)
	}
	if s.Items != nil {
		checkRequired(t, *s.Items)
	}
}

func testArgumentRefusals(t *testing.T, c *mcp.Client, want toolSpec) {
	t.Helper()
	test := func(field string, value any, omit bool) {
		t.Helper()
		args := make(map[string]any, len(want.args)+1)
		for n, v := range want.args {
			args[n] = v
		}
		if omit {
			delete(args, field)
		} else {
			args[field] = value
		}
		if r := as(t, c, daffy, want.name, args); !r.IsError() {
			t.Fatal(want.name, args)
		}
	}
	test("account", acme, false)
	test("page", "next-page", false)
	test("limit", 1, false)
	for _, field := range want.required {
		test(field, []any{}, false)
		test(field, nil, true)
	}
	if want.name == tools.ToolCreateInvite {
		test("admin", true, false)
	}
}

// R-ZZ7J-DKTW
func TestBoxRefusalConstants(t *testing.T) {
	const (
		_ = box.LastKey
		_ = box.NotSelf
		_ = box.OnlyAdmin
	)
	seen := map[string]bool{}
	for _, text := range []string{
		store.Unreachable, tools.NotConfigured, tools.InvalidName, tools.ReservedName, tools.NameTaken,
		tools.NoHost, tools.NoAccount, tools.NoUser, tools.NotInAccount, tools.InvalidKey, tools.KeyNotHeld,
		tools.TypeNotAllowed, tools.SizeOutOfRange, tools.SizeShrinks, tools.NothingToChange,
		tools.HostBusy, tools.HostFailed, tools.HostNotFailed, tools.HostNotRunning, tools.HostNotStopped,
		tools.HostHasAccounts, tools.HostFull, tools.HostGone, tools.AccountHasUsers, tools.AccountRemoving,
		tools.AccountFull, tools.TooManyKeys, tools.GrowLimit, tools.UserNotLocked, tools.UserRemoving,
		tools.NameHasRecord, tools.CloudFailed, tools.InvalidAddress, tools.PrefixTooWide, tools.TooManyUnbans,
		tools.AllowlistFull, tools.TooManyInvites, tools.NoInvite, tools.NoLink,
	} {
		seen[text] = true
	}
	for _, refusal := range []string{box.LastKey, box.NotSelf, box.OnlyAdmin} {
		if refusal == "" || bytes.ContainsRune([]byte(refusal), '\n') || seen[refusal] {
			t.Fatal(refusal)
		}
		seen[refusal] = true
	}
}

// R-02V8-IW1Z
func TestBoxResultTypesEncodeThroughMCP(t *testing.T) {
	_ = struct {
		User string
		Keys []string
	}(box.KeyList{})
	_ = struct {
		User                           string
		Admin                          bool
		State                          string
		Pending                        bool
		LastSignIn, Created, CreatedBy string
	}(box.User{})
	_ = struct{ Users []box.User }(box.UserList{})
	f := setup(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	addProbe(srv, "keys_empty", box.KeyList{})
	addProbe(srv, "users_empty", box.UserList{})
	addProbe(srv, "users_data", box.UserList{Users: []box.User{{User: daffy, Admin: true, State: store.UserActive, Pending: true}}})
	c := f.connect(srv)
	for _, want := range []struct{ tool, encoded string }{
		{"keys_empty", `{"user":"","keys":[]}`},
		{"users_empty", `{"users":[]}`},
		{"users_data", `{"users":[{"user":"daffy","admin":true,"state":"active","pending":true,"last_sign_in":"","created":"","created_by":""}]}`},
	} {
		if got := wire(t, as(t, c, daffy, want.tool, box.NoArgs{})); string(got.Structured) != want.encoded {
			t.Fatalf("%s result = %s, want %s", want.tool, got.Structured, want.encoded)
		}
	}
}

func addProbe[R any](srv *mcp.Server, name string, out R) {
	mcp.AddTool(srv, mcp.Tool[box.NoArgs, R]{Name: name, Description: "Encode a test result.", Effect: mcp.Read,
		Handler: func(context.Context, identity.Caller, box.NoArgs) (R, error) { return out, nil }})
}
