package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

type sequence struct {
	mu sync.Mutex
	n  byte
}

func (s *sequence) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range b {
		s.n++
		b[i] = s.n
	}
	return len(b), nil
}

type fixture struct {
	d       *db.DB
	st      *store.Store
	core    *runs.Core
	w       *telemetry.Writer
	capture *telemetry.Capture
	client  *mcp.Client
	srv     *mcp.Server
	dir     string
	now     time.Time
	clockMu sync.Mutex
	caller  identity.Caller
	n       int
}

// R-8YYK-4LW3 R-906G-IDMS R-9S85-B3OR R-9TG1-OVFG
func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	f := &fixture{dir: t.TempDir(), now: time.Date(2026, 2, 3, 4, 5, 6, 123, time.FixedZone("test", 3600)), caller: identity.Caller{UserID: "owner", Email: "owner@example.test"}}
	clock := func() time.Time { f.clockMu.Lock(); defer f.clockMu.Unlock(); return f.now }
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(f.dir, "catalog.db"), Migrations: prompts.Migrations(), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(f.d, store.Config{Now: clock, Rand: &sequence{}})
	f.capture = &telemetry.Capture{}
	f.w = telemetry.New(telemetry.Config{Service: "prompts", Sink: f.capture, Now: clock, Rand: &sequence{}})
	t.Cleanup(func() { f.w.Shutdown(context.Background(), "test") })
	f.core = runs.New(runs.Config{Store: f.st, Writer: f.w, Runs: filepath.Join(f.dir, "runs"), Unavailable: "test unavailable", PromptSeconds: 60, OutputMaxBytes: 1024, MaxToolCalls: 2, KeepDays: 1, KeepCount: 3, RunMemoryMaxBytes: 1024, RunPidsMax: 8, MaxActive: 1, MaxQueued: 2, Now: clock, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: &sequence{}})
	f.srv = mcp.NewServer(mcp.ServerConfig{Name: "prompts", Version: "fixture", Telemetry: f.w})
	tools.Register(f.srv, tools.Config{Store: f.st, Runs: f.core, Telemetry: f.w})
	server := httptest.NewServer(telemetry.Middleware(f.w, identity.Require(f.srv)))
	t.Cleanup(server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL})
	return f
}
func (f *fixture) advance(d time.Duration) {
	f.clockMu.Lock()
	f.now = f.now.Add(d)
	f.clockMu.Unlock()
}
func (f *fixture) call(t *testing.T, name, args string) mcp.Result {
	t.Helper()
	f.n++
	c := f.caller
	c.RequestID = fmt.Sprintf("call-%d", f.n)
	r, err := f.client.CallTool(context.Background(), c, name, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func content(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Structured json.RawMessage               `json:"structuredContent"`
		Content    []struct{ Type, Text string } `json:"content"`
	}
	if err = json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if r.IsError() {
		t.Fatalf("refusal %s", b)
	}
	if len(wire.Content) != 1 || wire.Content[0].Type != "text" || !bytes.Equal([]byte(wire.Content[0].Text), wire.Structured) {
		t.Fatalf("content disagrees %s", b)
	}
	return wire.Structured
}
func refused(t *testing.T, r mcp.Result, text string) {
	t.Helper()
	b, _ := r.MarshalJSON()
	want, _ := mcp.ErrorResult(text).MarshalJSON()
	var gotMembers, wantMembers map[string]json.RawMessage
	if err := json.Unmarshal(b, &gotMembers); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantMembers); err != nil {
		t.Fatal(err)
	}
	delete(gotMembers, "_meta")
	if !reflect.DeepEqual(gotMembers, wantMembers) {
		t.Fatalf("got %s want %s", b, want)
	}
}
func decode[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(content(t, r), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func assertNoRunFiles(t *testing.T, f *fixture) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.dir, "runs"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected run entries %v", entries)
	}
}

func model() string { return agentkit.Catalog()[0].Model }
func createArgs(name string) string {
	b, _ := json.Marshal(map[string]any{"name": name, "model": model(), "prompt": "test prompt"})
	return string(b)
}
func (f *fixture) seed(t *testing.T, name string) tools.Prompt {
	t.Helper()
	return decode[tools.Prompt](t, f.call(t, "create", createArgs(name)))
}

// R-91EC-W5DH
func TestCopyFormats(t *testing.T) {
	for _, row := range []struct {
		s string
		n int
	}{{tools.MissingPrompt, 1}, {tools.MissingRun, 1}, {tools.InvalidName, 1}, {tools.InvalidEvent, 1}, {tools.NameTaken, 1}, {tools.NotSubscribed, 2}, {tools.Ended, 1}, {tools.UnknownModel, 1}, {tools.InvalidTools, 1}, {tools.InvalidSchema, 1}, {tools.EmptyPrompt, 0}} {
		if strings.Count(row.s, "%s") != row.n || strings.Contains(strings.ReplaceAll(row.s, "%s", ""), "%") {
			t.Fatalf("bad format %q", row.s)
		}
	}
}

// R-92M9-9X46 R-93U5-NOUV R-9522-1GLK R-969Y-F8C9 R-97HU-T02Y R-99XN-KJKC R-9B5J-YBB1 R-9CDG-C31Q R-9DLC-PUSF R-9ET9-3MJ4 R-9G15-HE9T
func TestArgumentTypes(t *testing.T) {
	text := "system"
	groups := []string{"files"}
	raw := json.RawMessage(`{}`)
	rows := []struct {
		v    any
		want string
	}{
		{tools.ListArgs{}, `{}`}, {tools.ShowArgs{Name: "daily"}, `{"name":"daily"}`},
		{tools.CreateArgs{Name: "daily", Model: "chosen", Prompt: "body", System: &text, Tools: &groups, Schema: raw}, `{"name":"daily","model":"chosen","prompt":"body","system":"system","tools":["files"],"schema":{}}`},
		{tools.UpdateArgs{Name: "daily", Model: &text, Prompt: &text, System: &text, Tools: &groups, Schema: raw}, `{"name":"daily","model":"system","prompt":"system","system":"system","tools":["files"],"schema":{}}`},
		{tools.DeleteArgs{Name: "daily"}, `{"name":"daily"}`}, {tools.SubscribeArgs{Name: "daily", Event: "cron.*.fired"}, `{"name":"daily","event":"cron.*.fired"}`}, {tools.UnsubscribeArgs{Name: "daily", Event: "repo.pushed"}, `{"name":"daily","event":"repo.pushed"}`},
		{tools.RunArgs{Name: "daily", Input: raw}, `{"name":"daily","input":{}}`}, {tools.RunsArgs{Name: "daily"}, `{"name":"daily"}`}, {tools.ResultArgs{Run: "run-value"}, `{"run":"run-value"}`}, {tools.CancelArgs{Run: "run-value"}, `{"run":"run-value"}`},
	}
	for _, r := range rows {
		b, err := json.Marshal(r.v)
		if err != nil || string(b) != r.want {
			t.Fatalf("%T: %s %v", r.v, b, err)
		}
	}
}

// R-A4F5-4T3P R-A5N1-IKUE R-A6UX-WCL3
func TestDiscovery(t *testing.T) {
	f := setup(t)
	infos, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"list", "show", "create", "update", "delete", "subscribe", "unsubscribe", "run", "runs", "result", "cancel"}
	effects := []mcp.Effect{mcp.Read, mcp.Read, mcp.Additive, mcp.Additive, mcp.Destructive, mcp.Additive, mcp.Destructive, mcp.Additive, mcp.Read, mcp.Read, mcp.Destructive}
	if len(infos) != len(names) {
		t.Fatal(len(infos))
	}
	byName := map[string]mcp.ToolInfo{}
	for i, info := range infos {
		if info.Name != names[i] || info.Description == "" || info.Effect() != effects[i] || len(info.OutputSchema) == 0 {
			t.Fatalf("bad tool %#v", info)
		}
		hints := info.Annotations
		if hints.ReadOnlyHint == nil || *hints.ReadOnlyHint != (effects[i] == mcp.Read) || hints.DestructiveHint == nil || *hints.DestructiveHint != (effects[i] == mcp.Destructive) || hints.OpenWorldHint == nil || *hints.OpenWorldHint || hints.IdempotentHint != nil {
			t.Fatalf("incorrect annotations for %s: %#v", info.Name, hints)
		}
		byName[info.Name] = info
	}
	rows := []struct{ name, kind, want string }{
		// R-A82U-A4BS
		{"list", "inputSchema", `{"type":"object","additionalProperties":false}`},
		// R-A9AQ-NW2H
		{"show", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"}},"required":["name"],"additionalProperties":false}`},
		// R-A9AQ-NW2H
		{"delete", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"}},"required":["name"],"additionalProperties":false}`},
		// R-A9AQ-NW2H
		{"runs", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"}},"required":["name"],"additionalProperties":false}`},
		// R-AAIN-1NT6
		{"create", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"},"model":{"type":"string","description":"description"},"prompt":{"type":"string","description":"description"},"system":{"type":"string","description":"description"},"tools":{"type":"array","items":{"type":"string"},"description":"description"},"schema":{"type":"object","description":"description"}},"required":["name","model","prompt"],"additionalProperties":false}`},
		// R-ABQJ-FFJV
		{"update", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"},"model":{"type":"string","description":"description"},"prompt":{"type":"string","description":"description"},"system":{"type":"string","description":"description"},"tools":{"type":"array","items":{"type":"string"},"description":"description"},"schema":{"type":"object","description":"description"}},"required":["name"],"additionalProperties":false}`},
		// R-AE6C-6Z19
		{"subscribe", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"},"event":{"type":"string","description":"description"}},"required":["name","event"],"additionalProperties":false}`},
		// R-AFE8-KQRY
		{"unsubscribe", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"},"event":{"type":"string","description":"description"}},"required":["name","event"],"additionalProperties":false}`},
		// R-AGM4-YIIN
		{"run", "inputSchema", `{"type":"object","properties":{"name":{"type":"string","description":"description"},"input":{"type":"object","description":"description"}},"required":["name"],"additionalProperties":false}`},
		// R-AHU1-CA9C
		{"result", "inputSchema", `{"type":"object","properties":{"run":{"type":"string","description":"description"}},"required":["run"],"additionalProperties":false}`},
		// R-AHU1-CA9C
		{"cancel", "inputSchema", `{"type":"object","properties":{"run":{"type":"string","description":"description"}},"required":["run"],"additionalProperties":false}`},
		// R-AJ1X-Q201
		{"show", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"prompt":{"type":"string"},"system":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"schema":{"type":"object"},"created":{"type":"string"},"subscriptions":{"type":"array","items":{"type":"object","properties":{"event":{"type":"string"},"created":{"type":"string"}},"required":["event","created"],"additionalProperties":false}},"last_run":{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"}},"required":["id","status","started"],"additionalProperties":false}},"required":["id","name","model","prompt","system","tools","created","subscriptions"],"additionalProperties":false}`},
		// R-AJ1X-Q201
		{"create", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"prompt":{"type":"string"},"system":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"schema":{"type":"object"},"created":{"type":"string"},"subscriptions":{"type":"array","items":{"type":"object","properties":{"event":{"type":"string"},"created":{"type":"string"}},"required":["event","created"],"additionalProperties":false}},"last_run":{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"}},"required":["id","status","started"],"additionalProperties":false}},"required":["id","name","model","prompt","system","tools","created","subscriptions"],"additionalProperties":false}`},
		// R-AJ1X-Q201
		{"update", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"prompt":{"type":"string"},"system":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"schema":{"type":"object"},"created":{"type":"string"},"subscriptions":{"type":"array","items":{"type":"object","properties":{"event":{"type":"string"},"created":{"type":"string"}},"required":["event","created"],"additionalProperties":false}},"last_run":{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"}},"required":["id","status","started"],"additionalProperties":false}},"required":["id","name","model","prompt","system","tools","created","subscriptions"],"additionalProperties":false}`},
		// R-AJ1X-Q201
		{"subscribe", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"prompt":{"type":"string"},"system":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"schema":{"type":"object"},"created":{"type":"string"},"subscriptions":{"type":"array","items":{"type":"object","properties":{"event":{"type":"string"},"created":{"type":"string"}},"required":["event","created"],"additionalProperties":false}},"last_run":{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"}},"required":["id","status","started"],"additionalProperties":false}},"required":["id","name","model","prompt","system","tools","created","subscriptions"],"additionalProperties":false}`},
		// R-AJ1X-Q201
		{"unsubscribe", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"prompt":{"type":"string"},"system":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"schema":{"type":"object"},"created":{"type":"string"},"subscriptions":{"type":"array","items":{"type":"object","properties":{"event":{"type":"string"},"created":{"type":"string"}},"required":["event","created"],"additionalProperties":false}},"last_run":{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"}},"required":["id","status","started"],"additionalProperties":false}},"required":["id","name","model","prompt","system","tools","created","subscriptions"],"additionalProperties":false}`},
		// R-AK9U-3TQQ
		{"list", "outputSchema", `{"type":"object","properties":{"prompts":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"model":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"subscriptions":{"type":"integer"},"last_run":{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"}},"required":["id","status","started"],"additionalProperties":false}},"required":["id","name","model","tools","subscriptions"],"additionalProperties":false}}},"required":["prompts"],"additionalProperties":false}`},
		// R-ALHQ-HLHF
		{"delete", "outputSchema", `{"type":"object","properties":{"deleted":{"type":"boolean"},"id":{"type":"string"}},"required":["deleted","id"],"additionalProperties":false}`},
		// R-AMPM-VD84
		{"run", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"status":{"type":"string"},"reason":{"type":"string"}},"required":["id","status"],"additionalProperties":false}`},
		// R-ANXJ-94YT
		{"cancel", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"model":{"type":"string"},"trigger":{"type":"string"},"event":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"},"finished":{"type":"string"},"truncated":{"type":"boolean"},"reason":{"type":"string"},"calls":{"type":"integer"},"tool_calls":{"type":"integer"},"input_tokens":{"type":"integer"},"cached_tokens":{"type":"integer"},"output_tokens":{"type":"integer"},"reasoning_tokens":{"type":"integer"},"cost_nanos":{"type":"integer"}},"required":["id","model","trigger","status","started","truncated"],"additionalProperties":false}`},
		// R-AP5F-MWPI
		{"runs", "outputSchema", `{"type":"object","properties":{"runs":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"model":{"type":"string"},"trigger":{"type":"string"},"event":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"},"finished":{"type":"string"},"truncated":{"type":"boolean"},"reason":{"type":"string"},"calls":{"type":"integer"},"tool_calls":{"type":"integer"},"input_tokens":{"type":"integer"},"cached_tokens":{"type":"integer"},"output_tokens":{"type":"integer"},"reasoning_tokens":{"type":"integer"},"cost_nanos":{"type":"integer"}},"required":["id","model","trigger","status","started","truncated"],"additionalProperties":false}}},"required":["runs"],"additionalProperties":false}`},
		// R-AQDC-0OG7
		{"result", "outputSchema", `{"type":"object","properties":{"id":{"type":"string"},"prompt":{"type":"string"},"model":{"type":"string"},"user":{"type":"string"},"request_id":{"type":"string"},"trigger":{"type":"string"},"event":{"type":"string"},"status":{"type":"string"},"exit_code":{"type":"integer"},"started":{"type":"string"},"finished":{"type":"string"},"stdout_bytes":{"type":"integer"},"stderr_bytes":{"type":"integer"},"truncated":{"type":"boolean"},"reason":{"type":"string"},"calls":{"type":"integer"},"tool_calls":{"type":"integer"},"input_tokens":{"type":"integer"},"cached_tokens":{"type":"integer"},"output_tokens":{"type":"integer"},"reasoning_tokens":{"type":"integer"},"cost_nanos":{"type":"integer"},"stdout":{"type":"string"},"stderr":{"type":"string"},"transcript_bytes":{"type":"integer"},"files":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"size":{"type":"integer"}},"required":["path","size"],"additionalProperties":false}},"files_gone":{"type":"boolean"}},"required":["id","prompt","model","user","request_id","trigger","status","started","stdout_bytes","stderr_bytes","truncated"],"additionalProperties":false}`},
	}
	description := regexp.MustCompile(`"description":"(?:[^"\\]|\\.)*"`)
	for _, row := range rows {
		info := byName[row.name]
		got := info.InputSchema
		if row.kind == "outputSchema" {
			got = info.OutputSchema
		}
		for _, d := range description.FindAll(got, -1) {
			if bytes.Equal(d, []byte(`"description":""`)) {
				t.Fatal("empty argument description")
			}
		}
		got = description.ReplaceAll(got, []byte(`"description":"description"`))
		if string(got) != row.want {
			t.Fatalf("%s %s got %s want %s", row.name, row.kind, got, row.want)
		}
	}
}

// R-B680-ZP38 R-B7FX-DGTX R-BFZ8-1V0S R-BIF0-TEI6 R-AV8X-JREZ R-B504-LXCJ
// R-9JOU-MPHW R-9KWR-0H8L R-9UNY-2N65
func TestCatalogSuccess(t *testing.T) {
	f := setup(t)
	defer assertNoRunFiles(t, f)
	other, err := f.st.Create(context.Background(), store.Draft{Owner: "another", Name: "other", Model: model(), Prompt: "other body"})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode[tools.PromptList](t, f.call(t, "list", `{}`)); len(got.Prompts) != 0 {
		t.Fatal(got)
	}
	p := f.seed(t, "zeta")
	q := f.seed(t, "alpha")
	stored, err := f.st.Find(context.Background(), f.caller.UserID, p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Owner != f.caller.UserID || stored.OwnerEmail != f.caller.Email || stored.Model != model() || stored.Prompt != "test prompt" || stored.System != "" || len(stored.Tools) != 0 || stored.Schema != nil || stored.Last != nil || len(stored.Subscriptions) != 0 || p.Created != f.now.UTC().Format("2006-01-02T15:04:05Z") {
		t.Fatalf("stored %#v answered %#v", stored, p)
	}
	got := decode[tools.PromptList](t, f.call(t, "list", `{}`))
	if len(got.Prompts) != 2 || got.Prompts[0].ID != q.ID || got.Prompts[1].ID != p.ID || got.Prompts[0].Tools == nil || got.Prompts[0].Subscriptions != 0 || got.Prompts[0].LastRun != nil {
		t.Fatal(got)
	}
	shown := decode[tools.Prompt](t, f.call(t, "show", `{"name":"zeta"}`))
	if !reflect.DeepEqual(shown, p) {
		t.Fatalf("got %#v want %#v", shown, p)
	}
	for _, name := range []string{other.Name, p.ID, "Daily Digest", "absent"} {
		b, _ := json.Marshal(tools.ShowArgs{Name: name})
		refused(t, f.call(t, "show", string(b)), fmt.Sprintf(tools.MissingPrompt, name))
	}
	changed := decode[tools.Prompt](t, f.call(t, "update", `{"name":"zeta","prompt":" ","system":"detail","tools":["suite","files"],"schema":{"type":"object"}}`))
	if changed.ID != p.ID || changed.Name != p.Name || changed.Created != p.Created || changed.Prompt != " " || changed.System != "detail" || !reflect.DeepEqual(changed.Tools, []string{"suite", "files"}) || string(changed.Schema) != `{"type":"object"}` {
		t.Fatal(changed)
	}
	unchanged := decode[tools.Prompt](t, f.call(t, "update", `{"name":"zeta","model":null,"prompt":null,"system":null,"tools":null}`))
	if !reflect.DeepEqual(unchanged, changed) {
		t.Fatal(unchanged)
	}
	unchanged = decode[tools.Prompt](t, f.call(t, "update", `{"name":"zeta"}`))
	if !reflect.DeepEqual(unchanged, changed) {
		t.Fatal(unchanged)
	}
	cleared := decode[tools.Prompt](t, f.call(t, "update", `{"name":"zeta","system":"","tools":[]}`))
	if cleared.System != "" || cleared.Tools == nil || len(cleared.Tools) != 0 || string(cleared.Schema) != string(changed.Schema) {
		t.Fatal(cleared)
	}
	args := createArgs("nullable")
	args = strings.TrimSuffix(args, "}") + `,"system":null,"tools":null}`
	nullable := decode[tools.Prompt](t, f.call(t, "create", args))
	if nullable.System != "" || len(nullable.Tools) != 0 {
		t.Fatal(nullable)
	}
	for _, id := range []string{p.ID, q.ID, other.ID, nullable.ID} {
		rs, err := f.st.Runs(context.Background(), id)
		if err != nil || len(rs) != 0 {
			t.Fatalf("unexpected runs %v %v", rs, err)
		}
	}
}

// R-B8NT-R8KM R-B9VQ-50BB R-BB3M-IS20 R-BCBI-WJSP R-BDJF-ABJE R-BERB-O3A3 R-BH74-FMRH
// R-A1ZC-D9MB R-A378-R1D0 R-B2KB-UDV5 R-0KCT-G67E
func TestMutationRefusals(t *testing.T) {
	f := setup(t)
	defer assertNoRunFiles(t, f)
	p := f.seed(t, "taken")
	rows := []struct{ tool, args, want string }{
		{"create", `{"name":"events","model":"bad","prompt":""}`, fmt.Sprintf(tools.InvalidName, "events")},
		{"create", `{"name":"taken","model":"bad","prompt":"","tools":["web"]}`, fmt.Sprintf(tools.NameTaken, "taken")},
		{"create", `{"name":"new","model":"bad","prompt":"","tools":["web"]}`, fmt.Sprintf(tools.UnknownModel, "bad")},
		{"update", `{"name":"missing","model":"bad","prompt":""}`, fmt.Sprintf(tools.MissingPrompt, "missing")},
		{"update", `{"name":"taken","model":"bad","tools":["web"]}`, fmt.Sprintf(tools.UnknownModel, "bad")},
		{"update", `{"name":"taken","prompt":"","tools":["web"]}`, tools.EmptyPrompt},
	}
	for _, r := range rows {
		refused(t, f.call(t, r.tool, r.args), r.want)
	}
	for _, unknown := range []string{"", "no-such-model", strings.ToUpper(model())} {
		b, _ := json.Marshal(map[string]any{"name": "taken", "model": unknown})
		refused(t, f.call(t, "update", string(b)), fmt.Sprintf(tools.UnknownModel, unknown))
	}
	goodModel, _ := json.Marshal(model())
	refused(t, f.call(t, "create", `{"name":"new","model":`+string(goodModel)+`,"prompt":""}`), tools.EmptyPrompt)
	for _, groups := range [][]string{{"web"}, {"files", "files"}, {"Files"}, {""}} {
		b, _ := json.Marshal(groups)
		refused(t, f.call(t, "update", `{"name":"taken","tools":`+string(b)+`}`), fmt.Sprintf(tools.InvalidTools, string(b)))
		refused(t, f.call(t, "create", `{"name":"new","model":`+string(goodModel)+`,"prompt":"body","tools":`+string(b)+`}`), fmt.Sprintf(tools.InvalidTools, string(b)))
	}
	for _, schema := range []string{`{"type":"array","items":{"type":"string"}}`, `{"type":"object","properties":{"answer":{"type":"string"}}}`} {
		err := agentkit.ValidateOutputSchema(json.RawMessage(schema))
		if err == nil {
			t.Fatal("bad fixture")
		}
		refused(t, f.call(t, "update", `{"name":"taken","schema":`+schema+`}`), fmt.Sprintf(tools.InvalidSchema, err.Error()))
		refused(t, f.call(t, "create", `{"name":"new","model":`+string(goodModel)+`,"prompt":"body","schema":`+schema+`}`), fmt.Sprintf(tools.InvalidSchema, err.Error()))
	}
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.capture.Events() {
		if e.RequestID != "call-1" && (strings.HasPrefix(e.Name, "prompt.") || strings.HasPrefix(e.Name, "run.")) {
			t.Fatalf("refusal emitted mutation event %v", e)
		}
	}
	all, err := f.st.List(context.Background(), f.caller.UserID)
	if err != nil || len(all) != 1 {
		t.Fatalf("refusal changed catalog %v %v", all, err)
	}
	after := decode[tools.Prompt](t, f.call(t, "show", `{"name":"taken"}`))
	if !reflect.DeepEqual(after, p) {
		t.Fatalf("refusals changed prompt: %#v", after)
	}
	for _, groups := range []string{`[]`, `["suite"]`, `["bash","files","suite"]`} {
		decode[tools.Prompt](t, f.call(t, "update", `{"name":"taken","tools":`+groups+`}`))
	}
	decode[tools.Prompt](t, f.call(t, "update", `{"name":"taken","schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}`))
}

// R-9IGY-8XR7 R-BOII-Q97N R-BPQF-40YC R-BQYB-HSP1 R-BS67-VKFQ R-BTE4-9C6F
func TestSubscriptions(t *testing.T) {
	f := setup(t)
	defer assertNoRunFiles(t, f)
	p := f.seed(t, "daily")
	for _, event := range []string{"Repo.Pushed", "pushed", "*", "repo..pushed", "repo.pushed.", "re*po.pushed", "repo.push*", "repo.**", "repo-x.pushed", " repo.pushed", ""} {
		b, _ := json.Marshal(tools.SubscribeArgs{Name: p.Name, Event: event})
		refused(t, f.call(t, "subscribe", string(b)), fmt.Sprintf(tools.InvalidEvent, event))
	}
	refused(t, f.call(t, "subscribe", `{"name":"missing","event":"Repo.Pushed"}`), fmt.Sprintf(tools.MissingPrompt, "missing"))
	refused(t, f.call(t, "unsubscribe", `{"name":"daily","event":"repo.pushed"}`), fmt.Sprintf(tools.NotSubscribed, p.Name, "repo.pushed"))
	sub := decode[tools.Prompt](t, f.call(t, "subscribe", `{"name":"daily","event":"repo.pushed"}`))
	if len(sub.Subscriptions) != 1 || sub.Subscriptions[0].Event != "repo.pushed" || sub.Subscriptions[0].Created != p.Created {
		t.Fatal(sub)
	}
	f.advance(time.Hour) // A duplicate must retain its earlier subscription timestamp.
	again := decode[tools.Prompt](t, f.call(t, "subscribe", `{"name":"daily","event":"repo.pushed"}`))
	if !reflect.DeepEqual(again, sub) {
		t.Fatal(again)
	}
	sub = decode[tools.Prompt](t, f.call(t, "subscribe", `{"name":"daily","event":"crm.contact_added"}`))
	if sub.Subscriptions[0].Event != "crm.contact_added" {
		t.Fatal(sub)
	}
	for _, e := range []string{"cron.*.fired", "cron.hourly.fired", "repo.git.pushed", "repo.*", "*.*"} {
		b, _ := json.Marshal(tools.SubscribeArgs{Name: p.Name, Event: e})
		decode[tools.Prompt](t, f.call(t, "subscribe", string(b)))
	}
	refused(t, f.call(t, "unsubscribe", `{"name":"daily","event":"Repo.Pushed"}`), fmt.Sprintf(tools.InvalidEvent, "Repo.Pushed"))
	refused(t, f.call(t, "unsubscribe", `{"name":"daily","event":"cron.other.fired"}`), fmt.Sprintf(tools.NotSubscribed, p.Name, "cron.other.fired"))
	sub = decode[tools.Prompt](t, f.call(t, "unsubscribe", `{"name":"daily","event":"cron.*.fired"}`))
	for _, s := range sub.Subscriptions {
		if s.Event == "cron.*.fired" {
			t.Fatal(sub)
		}
	}
	for _, s := range sub.Subscriptions {
		b, _ := json.Marshal(tools.UnsubscribeArgs{Name: p.Name, Event: s.Event})
		sub = decode[tools.Prompt](t, f.call(t, "unsubscribe", string(b)))
	}
	if sub.Subscriptions == nil || len(sub.Subscriptions) != 0 {
		t.Fatal(sub)
	}
	f.advance(time.Hour)
	resubscribed := decode[tools.Prompt](t, f.call(t, "subscribe", `{"name":"daily","event":"repo.pushed"}`))
	expectedCreated := f.now.UTC().Format("2006-01-02T15:04:05Z")
	if len(resubscribed.Subscriptions) != 1 || resubscribed.Subscriptions[0].Created != expectedCreated || resubscribed.Subscriptions[0].Created == again.Subscriptions[0].Created {
		t.Fatal(resubscribed)
	}
	rs, err := f.st.Runs(context.Background(), p.ID)
	if err != nil || len(rs) != 0 {
		t.Fatalf("subscription made run %v %v", rs, err)
	}
}

// R-B04J-2UDR R-A378-R1D0 R-0KCT-G67E
func TestCatalogFailure(t *testing.T) {
	f := setup(t)
	p := f.seed(t, "daily")
	f.d.SetFailing(true)
	rows := []struct{ tool, args, want string }{
		{"list", `{}`, store.Unreachable},
		{"create", `{"name":"Daily Digest","model":"bad","prompt":""}`, fmt.Sprintf(tools.InvalidName, "Daily Digest")},
		{"create", `{"name":"new","model":"bad","prompt":""}`, store.Unreachable},
		{"show", `{"name":"daily"}`, store.Unreachable},
		{"update", `{"name":"daily","tools":["web"]}`, store.Unreachable},
		{"delete", `{"name":"daily"}`, store.Unreachable},
		{"subscribe", `{"name":"daily","event":"Repo.Pushed"}`, store.Unreachable},
		{"unsubscribe", `{"name":"daily","event":"Repo.Pushed"}`, store.Unreachable},
		{"run", `{"name":"daily"}`, store.Unreachable},
		{"runs", `{"name":"daily"}`, store.Unreachable},
		{"result", `{"run":"prr_0011223344556677"}`, store.Unreachable},
		{"cancel", `{"run":"prr_0011223344556677"}`, store.Unreachable},
		{"result", `{"run":"daily"}`, fmt.Sprintf(tools.MissingRun, "daily")},
		{"cancel", `{"run":"daily"}`, fmt.Sprintf(tools.MissingRun, "daily")},
	}
	for _, r := range rows {
		refused(t, f.call(t, r.tool, r.args), r.want)
	}
	f.d.SetFailing(false)
	after := decode[tools.Prompt](t, f.call(t, "show", `{"name":"daily"}`))
	if !reflect.DeepEqual(after, p) {
		t.Fatal(after)
	}
}

// R-ARL8-EG6W R-AYWM-P2N2
func TestDiscoveryStable(t *testing.T) {
	f := setup(t)
	before, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	for phase := range 3 {
		if phase == 0 {
			f.d.SetFailing(true)
		}
		if phase == 1 {
			f.d.SetFailing(false)
			f.core.Drain(context.Background())
		}
		if phase == 2 {
			f.call(t, "list", `{}`)
		}
		after, err := f.client.ListTools(context.Background(), f.caller)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("discovery changed %v", err)
		}
	}
	_, err = f.client.CallTool(context.Background(), identity.Caller{UserID: "owner", RequestID: "unknown-call"}, "rename", json.RawMessage(`{}`))
	var rpc *mcp.RPCError
	if !errors.As(err, &rpc) || rpc.Code != -32602 {
		t.Fatalf("unknown tool %v", err)
	}
	if err = f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.capture.Events() {
		if e.Name == "tool.called" && e.RequestID != "call-1" {
			t.Fatalf("discovery/unknown emitted %v", e)
		}
	}
	ps, err := f.st.List(context.Background(), "owner")
	if err != nil || len(ps) != 0 {
		t.Fatalf("discovery changed catalog %v %v", ps, err)
	}
}

// R-AU11-5ZOA
func TestInvalidArguments(t *testing.T) {
	f := setup(t)
	defer assertNoRunFiles(t, f)
	quoted, _ := json.Marshal(model())
	m := string(quoted)
	rows := []struct{ tool, args string }{
		{"list", `{"name":"daily"}`}, {"show", `{"prompt":"daily"}`},
		{"create", `{"name":"new","model":` + m + `}`}, {"create", `{"name":"new","model":null,"prompt":"body"}`},
		{"create", `{"name":"new","model":` + m + `,"prompt":"body","tools":"files"}`}, {"create", `{"name":"new","model":` + m + `,"prompt":"body","tools":["files",1]}`},
		{"create", `{"name":"new","model":` + m + `,"prompt":"body","schema":null}`}, {"create", `{"name":"new","model":` + m + `,"prompt":"body","schema":"{}"}`}, {"create", `{"name":"new","model":` + m + `,"prompt":"body","owner":"another"}`},
		{"update", `{"model":` + m + `}`}, {"update", `{"name":"daily","prompt":7}`}, {"update", `{"name":"daily","schema":[]}`}, {"update", `{"name":"daily","new_name":"weekly"}`},
		{"delete", `{"id":"prm_0011223344556677"}`}, {"subscribe", `{"name":"daily"}`}, {"subscribe", `{"name":"daily","event":["repo.pushed"]}`}, {"unsubscribe", `{"name":"daily"}`}, {"unsubscribe", `{"name":"daily","event":["repo.pushed"]}`},
		{"run", `{"input":{}}`}, {"run", `{"name":"daily","input":"date"}`}, {"run", `{"name":"daily","input":["sales"]}`}, {"run", `{"name":"daily","input":null}`}, {"runs", `{"prompt":"daily"}`}, {"result", `{"id":"prr_0011223344556677"}`}, {"result", `{"run":3}`}, {"cancel", `{"name":"daily"}`},
	}
	for _, r := range rows {
		got := f.call(t, r.tool, r.args)
		if !got.IsError() {
			t.Fatalf("accepted %s %s", r.tool, r.args)
		}
	}
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := f.capture.Events()
	for i, row := range rows {
		rid := fmt.Sprintf("call-%d", i+1)
		count := 0
		for _, e := range events {
			if e.RequestID != rid {
				continue
			}
			if strings.HasPrefix(e.Name, "prompt.") || strings.HasPrefix(e.Name, "run.") {
				t.Fatal(e)
			}
			if e.Name == "tool.called" {
				count++
				kind := "additive"
				switch row.tool {
				case "list", "show", "runs", "result":
					kind = "read"
				case "delete", "unsubscribe", "cancel":
					kind = "destructive"
				}
				if e.Attrs["kind"] != kind || e.Attrs["tool"] != row.tool || e.Attrs["outcome"] != "invalid_arguments" || e.Attrs["duration_us"] != int64(0) {
					t.Fatal(e)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s: %d tool events", rid, count)
		}
	}
	ps, err := f.st.List(context.Background(), "owner")
	if err != nil || len(ps) != 0 {
		t.Fatalf("invalid arguments changed catalog %v %v", ps, err)
	}
}

// R-AWGT-XJ5O
func TestAbsentArguments(t *testing.T) {
	f := setup(t)
	f.seed(t, "daily")
	server := httptest.NewServer(identity.Require(f.srv))
	defer server.Close()
	for _, name := range []string{"list", "show"} {
		var results []json.RawMessage
		for _, args := range []string{"", `,"arguments":{}`} {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `"` + args + `}}`
			req, err := http.NewRequestWithContext(context.Background(), "POST", server.URL, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			identity.Forward(f.caller, req)
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(res.Body)
			closeErr := res.Body.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("read %v close %v", err, closeErr)
			}
			var wire struct{ Result json.RawMessage }
			if err = json.Unmarshal(b, &wire); err != nil {
				t.Fatal(err)
			}
			results = append(results, wire.Result)
		}
		if !bytes.Equal(results[0], results[1]) {
			t.Fatalf("%s mismatch %s %s", name, results[0], results[1])
		}
	}
}

// R-9YBN-7YE8 R-9ZJJ-LQ4X R-A0RF-ZHVM R-9X3Q-U6NJ R-9VVU-GEWU
// R-9H91-V60I R-9OKG-5SGO R-9PSC-JK7D R-9NCJ-S0PZ R-B3S8-85LU
func TestRunResultFiles(t *testing.T) {
	f := setup(t)
	p := f.seed(t, "daily")
	r, err := f.st.AddRun(context.Background(), store.Run{ID: "prr_0011223344556677", Prompt: p.ID, Model: model(), User: f.caller.UserID, RequestID: "origin", Trigger: store.TriggerEvent, Event: "evt-test", Status: store.StatusRunning, Started: f.now})
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.st.FinishRun(context.Background(), r.ID, store.Ending{Status: store.StatusExited, ExitCode: 2, Finished: f.now.Add(time.Second), StdoutBytes: 10, StderrBytes: 3, StdoutTruncated: true, Usage: store.Usage{Calls: 1, ToolCalls: 2, InputTokens: 3, CachedTokens: 4, OutputTokens: 5, ReasoningTokens: 6, CostNanos: 7}})
	if err != nil {
		t.Fatal(err)
	}
	folder := f.core.Folder(r)
	work := filepath.Join(folder, runs.WorkDir)
	if err = os.MkdirAll(filepath.Join(work, "charts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(work, ".cache"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string][]byte{filepath.Join(runs.WorkDir, ".cache", "state"): []byte("ok"), runs.StdoutFile: []byte("Zürich — 東京\n"), runs.StderrFile: {0xff, 'x'}, runs.TranscriptFile: bytes.Repeat([]byte("a"), 4096), filepath.Join(runs.WorkDir, "report.csv"): bytes.Repeat([]byte("b"), 13), filepath.Join(runs.WorkDir, "charts", "sales.svg"): bytes.Repeat([]byte("c"), 42)} {
		if err = os.WriteFile(filepath.Join(folder, path), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Symlink("report.csv", filepath.Join(work, "latest.csv")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("charts", filepath.Join(work, "graphs")); err != nil {
		t.Fatal(err)
	}
	out := decode[tools.RunResult](t, f.call(t, "result", `{"run":"`+r.ID+`"}`))
	if out.ID != r.ID || out.Prompt != p.ID || out.Model != r.Model || out.User != r.User || out.RequestID != r.RequestID || out.Trigger != r.Trigger || out.Event == nil || *out.Event != r.Event || out.ExitCode == nil || *out.ExitCode != 2 || out.Finished == nil || out.Reason != nil || !out.Truncated || out.Calls == nil || *out.Calls != 1 || *out.ToolCalls != 2 || *out.InputTokens != 3 || *out.CachedTokens != 4 || *out.OutputTokens != 5 || *out.ReasoningTokens != 6 || *out.CostNanos != 7 || out.Stdout == nil || *out.Stdout != "Zürich — 東京\n" || out.Stderr == nil || *out.Stderr != "\ufffdx" || out.TranscriptBytes == nil || *out.TranscriptBytes != 4096 || out.FilesGone != nil || !reflect.DeepEqual(*out.Files, []tools.File{{Path: ".cache/state", Size: 2}, {Path: "charts/sales.svg", Size: 42}, {Path: "report.csv", Size: 13}}) {
		t.Fatalf("result %#v", out)
	}
	// The MCP result's entry and prompt last-run must retain their independent shapes.
	listed := decode[tools.RunList](t, f.call(t, "runs", `{"name":"daily"}`))
	if len(listed.Runs) != 1 || listed.Runs[0].ID != r.ID || *listed.Runs[0].ExitCode != r.ExitCode || *listed.Runs[0].CostNanos != 7 {
		t.Fatal(listed)
	}
	shown := decode[tools.Prompt](t, f.call(t, "show", `{"name":"daily"}`))
	if shown.LastRun == nil || shown.LastRun.ID != r.ID || shown.LastRun.Status != r.Status || *shown.LastRun.ExitCode != r.ExitCode || shown.LastRun.Started != r.Started.UTC().Format("2006-01-02T15:04:05Z") {
		t.Fatal(shown)
	}
	stdout := filepath.Join(folder, runs.StdoutFile)
	if err = os.Remove(stdout); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(work, "report.csv"), stdout); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(folder, runs.TranscriptFile)
	if err = os.Remove(transcript); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(work, "report.csv"), transcript); err != nil {
		t.Fatal(err)
	}
	out = decode[tools.RunResult](t, f.call(t, "result", `{"run":"`+r.ID+`"}`))
	if *out.Stdout != "" || *out.TranscriptBytes != 0 {
		t.Fatal(out)
	}
	if err = os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	out = decode[tools.RunResult](t, f.call(t, "result", `{"run":"`+r.ID+`"}`))
	if out.FilesGone == nil || !*out.FilesGone || out.Stdout != nil || out.Stderr != nil || out.TranscriptBytes != nil || out.Files != nil {
		t.Fatal(out)
	}
	again, err := f.st.RunByID(context.Background(), r.ID)
	if err != nil || !reflect.DeepEqual(again, r) {
		t.Fatalf("read changed run %#v %v", again, err)
	}
	// Public answer types remain usable as independent values.
	started := tools.Started{ID: r.ID, Status: store.StatusFailed, Reason: &r.Reason}
	b, err := json.Marshal(started)
	if err != nil || string(b) != `{"id":"`+r.ID+`","status":"failed","reason":""}` {
		t.Fatalf("start %s %v", b, err)
	}
}

// R-B9VQ-50BB
func TestTakenAcrossOwnersAndConcurrentCreates(t *testing.T) {
	f := setup(t)
	defer assertNoRunFiles(t, f)
	_, err := f.st.Create(context.Background(), store.Draft{Owner: "another", OwnerEmail: "another@example.test", Name: "owned", Model: model(), Prompt: "other"})
	if err != nil {
		t.Fatal(err)
	}
	refused(t, f.call(t, "create", createArgs("owned")), fmt.Sprintf(tools.NameTaken, "owned"))
	const count = 8
	type answer struct {
		result mcp.Result
		err    error
	}
	start := make(chan struct{})
	answers := make(chan answer, count)
	for i := range count {
		go func() {
			<-start
			c := f.caller
			c.RequestID = fmt.Sprintf("concurrent-%d", i)
			r, err := f.client.CallTool(context.Background(), c, "create", json.RawMessage(createArgs("shared")))
			answers <- answer{r, err}
		}()
	}
	close(start)
	success := 0
	for range count {
		a := <-answers
		if a.err != nil {
			t.Fatal(a.err)
		}
		if a.result.IsError() {
			refused(t, a.result, fmt.Sprintf(tools.NameTaken, "shared"))
		} else {
			success++
			p := decode[tools.Prompt](t, a.result)
			if p.Name != "shared" || p.Model != model() {
				t.Fatal(p)
			}
		}
	}
	if success != 1 {
		t.Fatalf("got %d successful concurrent creates", success)
	}
	ps, err := f.st.List(context.Background(), f.caller.UserID)
	if err != nil || len(ps) != 1 || ps[0].Name != "shared" {
		t.Fatalf("catalog %v %v", ps, err)
	}
	other, err := f.st.Find(context.Background(), "another", "owned")
	if err != nil || other.Owner != "another" || other.Prompt != "other" {
		t.Fatalf("other owner's prompt changed %v %v", other, err)
	}
}

func rawPromptCall(t *testing.T, f *fixture, tool, args string) (tools.Prompt, string) {
	t.Helper()
	server := httptest.NewServer(identity.Require(f.srv))
	defer server.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
	req, err := http.NewRequestWithContext(context.Background(), "POST", server.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	c := f.caller
	c.RequestID = "raw-schema"
	identity.Forward(c, req)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(res.Body)
	closeErr := res.Body.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read %v close %v", err, closeErr)
	}
	var wire struct {
		Result struct {
			Structured json.RawMessage               `json:"structuredContent"`
			Content    []struct{ Type, Text string } `json:"content"`
			IsError    bool                          `json:"isError"`
		}
	}
	if err = json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Result.IsError || len(wire.Result.Content) != 1 || wire.Result.Content[0].Type != "text" {
		t.Fatalf("raw refusal %s", b)
	}
	var p tools.Prompt
	if err = json.Unmarshal(wire.Result.Structured, &p); err != nil {
		t.Fatal(err)
	}
	return p, wire.Result.Content[0].Text
}

// R-BFZ8-1V0S R-BIF0-TEI6 R-BCBI-WJSP R-BDJF-ABJE R-BERB-O3A3
func TestCreateOptionalFieldsAndModelSchemaUpdate(t *testing.T) {
	f := setup(t)
	defer assertNoRunFiles(t, f)
	m, _ := json.Marshal(model())
	schema := `{ "type" : "object" }`
	made, text := rawPromptCall(t, f, "create", `{"name":"configured","model":`+string(m)+`,"prompt":" ","system":"author system","tools":["suite","files","bash"],"schema":`+schema+`}`)
	if made.System != "author system" || made.Prompt != " " || !reflect.DeepEqual(made.Tools, []string{"suite", "files", "bash"}) || string(made.Schema) != schema || !strings.Contains(text, `"schema":`+schema) {
		t.Fatalf("create answer %#v text %s", made, text)
	}
	before, err := f.st.Find(context.Background(), f.caller.UserID, made.Name)
	if err != nil {
		t.Fatal(err)
	}
	if before.Owner != f.caller.UserID || before.OwnerEmail != f.caller.Email || before.System != made.System || before.Prompt != made.Prompt || !reflect.DeepEqual(before.Tools, made.Tools) || string(before.Schema) != schema || before.Last != nil || len(before.Subscriptions) != 0 {
		t.Fatal(before)
	}
	entries := agentkit.Catalog()
	if len(entries) < 2 {
		t.Fatal("test needs two catalog models")
	}
	newModel := entries[1].Model
	newModelJSON, _ := json.Marshal(newModel)
	updatedSchema := `{ "type" : "object", "properties" : { "value" : { "type" : "string" } }, "required" : [ "value" ], "additionalProperties" : false }`
	updated, text := rawPromptCall(t, f, "update", `{"name":"configured","model":`+string(newModelJSON)+`,"schema":`+updatedSchema+`}`)
	if updated.Model != newModel || string(updated.Schema) != updatedSchema || !strings.Contains(text, `"schema":`+updatedSchema) {
		t.Fatalf("update answer %#v text %s", updated, text)
	}
	after, err := f.st.Find(context.Background(), f.caller.UserID, made.Name)
	if err != nil {
		t.Fatal(err)
	}
	expected := before
	expected.Model = newModel
	expected.Schema = json.RawMessage(updatedSchema)
	if !reflect.DeepEqual(after, expected) {
		t.Fatalf("update stored %#v want %#v", after, expected)
	}
}
