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
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/tools"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
)

type fixture struct {
	t      *testing.T
	ctx    context.Context
	d      *db.DB
	st     *store.Store
	sch    *scheduler.Scheduler
	w      *telemetry.Writer
	em     *events.Emitter
	tc     *telemetry.Capture
	ec     *events.Capture
	client *mcp.Client
	server *httptest.Server
	caller identity.Caller
	mu     sync.Mutex
	now    time.Time
	serial int
}

func parse(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}

// R-K27Z-SDCA R-K3FW-652Z R-PMN7-4MNK R-9B7P-NNZ2
func setup(t *testing.T, seed bool) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	f := &fixture{t: t, ctx: ctx, now: parse("2026-10-05T09:32:00Z"), tc: &telemetry.Capture{}, ec: &events.Capture{}, caller: identity.Caller{UserID: "owner", Email: "mg@example.com"}}
	now := func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
	d, e := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "state", "cron.db"), Migrations: cron.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	f.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	data := make([]byte, 8000)
	for i := range data {
		data[i] = byte(i/8 + 1)
	}
	f.st = store.New(d, store.Config{Now: now, Rand: bytes.NewReader(data)})
	if seed {
		for _, a := range []struct{ slug, when, owner, email, created, last, status string }{
			{"hourly", "@hourly", "owner", "mg@example.com", "2026-09-20T08:00:00Z", "2026-10-05T09:00:00Z", store.Active},
			{"month_end", "@monthly", "other", "ann@example.com", "2026-10-04T10:00:00Z", "", store.Active},
			{"nightly_backup", "30 2 * * *", "other", "ann@example.com", "2026-09-28T17:15:00Z", "2026-10-05T02:30:00Z", store.Active},
			{"weekly_digest", "0 8 * * 1", "owner", "mg@example.com", "2026-09-01T12:00:00Z", "2026-09-28T08:00:00Z", store.Paused},
		} {
			f.now = parse(a.created)
			x, err := f.st.Create(ctx, store.Draft{Slug: a.slug, When: a.when, OwnerID: a.owner, OwnerEmail: a.email})
			if err != nil {
				t.Fatal(err)
			}
			if a.last != "" {
				if _, err = f.st.SetLastFired(ctx, x.ID, parse(a.last)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.st.SetStatus(ctx, x.ID, a.status); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.now = parse("2026-10-05T09:32:00Z")
	f.w = telemetry.New(telemetry.Config{Service: "cron", Sink: f.tc, Now: now})
	f.em = events.New(events.Config{Service: "cron", Sink: f.ec, Now: now, Rand: bytes.NewReader(data), Telemetry: f.w, Emits: trail.Emits()})
	sch, e := scheduler.Start(ctx, scheduler.Config{Store: f.st, Events: f.em, Telemetry: f.w, Now: now, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: bytes.NewReader(data)})
	if e != nil {
		t.Fatal(e)
	}
	f.sch = sch
	t.Cleanup(sch.Stop)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "cron", Version: "test", Telemetry: f.w})
	tools.Register(srv, tools.Config{Store: f.st, Scheduler: sch})
	f.server = httptest.NewServer(telemetry.Middleware(f.w, events.Middleware(identity.Require(srv))))
	t.Cleanup(f.server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.server.URL + "/mcp"})
	return f
}
func (f *fixture) move(s string) { f.mu.Lock(); defer f.mu.Unlock(); f.now = parse(s) }
func (f *fixture) flush() {
	f.t.Helper()
	if e := f.em.Flush(f.ctx); e != nil {
		f.t.Fatal(e)
	}
	if e := f.w.Flush(f.ctx); e != nil {
		f.t.Fatal(e)
	}
}
func (f *fixture) content() []store.Trigger {
	f.t.Helper()
	xs, e := f.st.List(f.ctx)
	if e != nil {
		f.t.Fatal(e)
	}
	return xs
}
func (f *fixture) nexts() map[string]time.Time {
	r := map[string]time.Time{}
	for _, x := range f.content() {
		if n, ok := f.sch.Next(x.ID); ok {
			r[x.ID] = n
		}
	}
	return r
}
func (f *fixture) call(name string, args any) map[string]json.RawMessage {
	f.t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		f.t.Fatal(e)
	}
	return f.callRaw(name, b)
}
func (f *fixture) callRaw(name string, b json.RawMessage) map[string]json.RawMessage {
	f.t.Helper()
	f.serial++
	f.caller.RequestID = fmt.Sprintf("%032x", f.serial)
	r, e := f.client.CallTool(f.ctx, f.caller, name, b)
	if e != nil {
		f.t.Fatal(e)
	}
	raw, e := r.MarshalJSON()
	if e != nil {
		f.t.Fatal(e)
	}
	var obj map[string]json.RawMessage
	if e = json.Unmarshal(raw, &obj); e != nil {
		f.t.Fatal(e)
	}
	return obj
}
func success(t *testing.T, r map[string]json.RawMessage, out any) {
	t.Helper()
	if _, ok := r["isError"]; ok {
		t.Fatalf("refused: %s", r["content"])
	}
	if e := json.Unmarshal(r["structuredContent"], out); e != nil {
		t.Fatal(e)
	}
	var c []struct{ Type, Text string }
	if e := json.Unmarshal(r["content"], &c); e != nil {
		t.Fatal(e)
	}
	if len(c) != 1 || c[0].Type != "text" || c[0].Text != string(r["structuredContent"]) {
		t.Fatalf("content differs: %s", r["content"])
	}
	var compact bytes.Buffer
	if e := json.Compact(&compact, r["structuredContent"]); e != nil {
		t.Fatal(e)
	}
	if compact.String() != c[0].Text {
		t.Fatal("text not compact")
	}
	if strings.Contains(c[0].Text, ":null") {
		t.Fatal("null result member")
	}
	switch v := out.(type) {
	case *tools.Trigger:
		keys := []string{"id", "slug", "when", "owner", "status", "created"}
		if v.LastFired != nil {
			keys = append(keys, "last_fired")
		}
		if v.Next != nil {
			keys = append(keys, "next")
		}
		assertKeys(t, r["structuredContent"], keys)
	case *tools.TriggerList:
		assertKeys(t, r["structuredContent"], []string{"triggers"})
		var objects struct{ Triggers []json.RawMessage }
		if e := json.Unmarshal(r["structuredContent"], &objects); e != nil {
			t.Fatal(e)
		}
		for i, obj := range objects.Triggers {
			keys := []string{"id", "slug", "when", "owner", "status"}
			if v.Triggers[i].LastFired != nil {
				keys = append(keys, "last_fired")
			}
			if v.Triggers[i].Next != nil {
				keys = append(keys, "next")
			}
			assertKeys(t, obj, keys)
		}
	case *tools.Deleted:
		assertKeys(t, r["structuredContent"], []string{"deleted", "id"})
	}
}
func assertKeys(t *testing.T, raw json.RawMessage, want []string) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, e := d.Token(); e != nil || token != json.Delim('{') {
		t.Fatal("expected object", e)
	}
	var keys []string
	for d.More() {
		token, e := d.Token()
		if e != nil {
			t.Fatal(e)
		}
		key, ok := token.(string)
		if !ok {
			t.Fatal("expected key")
		}
		keys = append(keys, key)
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatal("result member order", keys, want)
	}
}
func refusal(t *testing.T, r map[string]json.RawMessage, want string) {
	t.Helper()
	delete(r, "_meta")
	got, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := mcp.ErrorResult(want).MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var a, c any
	if e = json.Unmarshal(got, &a); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &c); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("refusal got %s want %s", got, b)
	}
}
func (f *fixture) unchanged(name string, args any, want string) {
	f.t.Helper()
	before := f.content()
	next := f.nexts()
	f.flush()
	n := len(f.ec.Events())
	m := len(f.tc.Events())
	r := f.call(name, args)
	refusal(f.t, r, want)
	f.flush()
	if !reflect.DeepEqual(before, f.content()) || !reflect.DeepEqual(next, f.nexts()) {
		f.t.Fatal("refusal changed state")
	}
	if len(f.ec.Events()) != n {
		f.t.Fatal("refusal emitted")
	}
	for _, e := range f.tc.Events()[m:] {
		if strings.HasPrefix(e.Name, "cron.") {
			f.t.Fatal("refusal lifecycle")
		}
	}
}

// R-K4NS-JWTO R-4JNG-2DHD R-4KVC-G582 R-4M38-TWYR R-4NB5-7OPG R-4OJ1-LGG5 R-4PQX-Z86U
// R-KMYA-AGY3 R-4TEN-4JEX R-4UMJ-IB5M R-4VUF-W2WB R-4YA8-NMDP R-4ZI5-1E4E R-50Q1-F5V3 R-51XX-SXLS
// R-KWPH-CMVN R-KZ5A-46D1 R-L0D6-HY3Q R-L1L2-VPUF
func TestInventory(t *testing.T) {
	f := setup(t, true)
	expected := []struct {
		name, input, output string
		effect              mcp.Effect
		args                any
	}{
		{"list", `{"type":"object","additionalProperties":false}`, `{"type":"object","properties":{"triggers":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"slug":{"type":"string"},"when":{"type":"string"},"owner":{"type":"string"},"status":{"type":"string"},"last_fired":{"type":"string"},"next":{"type":"string"}},"additionalProperties":false}}},"additionalProperties":false}`, mcp.Read, tools.ListArgs{}},
		{"show", `{"type":"object","properties":{"slug":{"type":"string"}},"required":["slug"],"additionalProperties":false}`, `{"type":"object","properties":{"id":{"type":"string"},"slug":{"type":"string"},"when":{"type":"string"},"owner":{"type":"string"},"status":{"type":"string"},"created":{"type":"string"},"last_fired":{"type":"string"},"next":{"type":"string"}},"additionalProperties":false}`, mcp.Read, tools.ShowArgs{Slug: "hourly"}},
		{"create", `{"type":"object","properties":{"slug":{"type":"string"},"when":{"type":"string"}},"required":["slug","when"],"additionalProperties":false}`, `{"type":"object","properties":{"id":{"type":"string"},"slug":{"type":"string"},"when":{"type":"string"},"owner":{"type":"string"},"status":{"type":"string"},"created":{"type":"string"},"last_fired":{"type":"string"},"next":{"type":"string"}},"additionalProperties":false}`, mcp.Additive, tools.CreateArgs{Slug: "crm_sync", When: "*/15 * * * *"}},
		{"update", `{"type":"object","properties":{"slug":{"type":"string"},"when":{"type":"string"}},"required":["slug","when"],"additionalProperties":false}`, `{"type":"object","properties":{"id":{"type":"string"},"slug":{"type":"string"},"when":{"type":"string"},"owner":{"type":"string"},"status":{"type":"string"},"created":{"type":"string"},"last_fired":{"type":"string"},"next":{"type":"string"}},"additionalProperties":false}`, mcp.Additive, tools.UpdateArgs{Slug: "hourly", When: "@daily"}},
		{"pause", `{"type":"object","properties":{"slug":{"type":"string"}},"required":["slug"],"additionalProperties":false}`, `{"type":"object","properties":{"id":{"type":"string"},"slug":{"type":"string"},"when":{"type":"string"},"owner":{"type":"string"},"status":{"type":"string"},"created":{"type":"string"},"last_fired":{"type":"string"},"next":{"type":"string"}},"additionalProperties":false}`, mcp.Destructive, tools.PauseArgs{Slug: "hourly"}},
		{"resume", `{"type":"object","properties":{"slug":{"type":"string"}},"required":["slug"],"additionalProperties":false}`, `{"type":"object","properties":{"id":{"type":"string"},"slug":{"type":"string"},"when":{"type":"string"},"owner":{"type":"string"},"status":{"type":"string"},"created":{"type":"string"},"last_fired":{"type":"string"},"next":{"type":"string"}},"additionalProperties":false}`, mcp.Additive, tools.ResumeArgs{Slug: "hourly"}},
		{"delete", `{"type":"object","properties":{"slug":{"type":"string"}},"required":["slug"],"additionalProperties":false}`, `{"type":"object","properties":{"deleted":{"type":"boolean"},"id":{"type":"string"}},"additionalProperties":false}`, mcp.Destructive, tools.DeleteArgs{Slug: "hourly"}},
	}
	checkInventory := func(infos []mcp.ToolInfo) {
		if len(infos) != len(expected) {
			t.Fatal("inventory length")
		}
		for i, info := range infos {
			x := expected[i]
			if info.Name != x.name || info.Description == "" || schemaWithoutCopy(t, info.InputSchema) != x.input || string(info.OutputSchema) != x.output || info.Effect() != x.effect {
				t.Fatalf("wrong tool metadata %s: %+v", x.name, info)
			}
			a := info.Annotations
			if a.ReadOnlyHint == nil || *a.ReadOnlyHint != (x.effect == mcp.Read) || a.DestructiveHint == nil || *a.DestructiveHint != (x.effect == mcp.Destructive) || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint != nil {
				t.Fatalf("annotations %s", x.name)
			}
			b, e := json.Marshal(x.args)
			if e != nil {
				t.Fatal(e)
			}
			wantArgs := `{"slug":"hourly"}`
			switch x.name {
			case "list":
				wantArgs = `{}`
			case "create":
				wantArgs = `{"slug":"crm_sync","when":"*/15 * * * *"}`
			case "update":
				wantArgs = `{"slug":"hourly","when":"@daily"}`
			}
			if string(b) != wantArgs {
				t.Fatalf("args encoding: %s, want %s", b, wantArgs)
			}
		}
	}
	before := f.content()
	f.flush()
	tn := len(f.tc.Events())
	en := len(f.ec.Events())
	for _, failing := range []bool{false, true, false} {
		f.d.SetFailing(failing)
		infos, e := f.client.ListTools(f.ctx, f.caller)
		if e != nil {
			t.Fatal(e)
		}
		checkInventory(infos)
	}
	f.flush()
	if !reflect.DeepEqual(before, f.content()) || len(f.ec.Events()) != en {
		t.Fatal("inventory changed state")
	}
	for _, e := range f.tc.Events()[tn:] {
		if e.Name == "tool.called" {
			t.Fatal("list tools called tool")
		}
	}
	var empty tools.TriggerList
	success(t, f.call("list", tools.ListArgs{}), &empty)
	f.flush()
	before = f.content()
	tn, en = len(f.tc.Events()), len(f.ec.Events())
	for _, failing := range []bool{false, true, false} {
		f.d.SetFailing(failing)
		infos, e := f.client.ListTools(f.ctx, f.caller)
		if e != nil {
			t.Fatal(e)
		}
		checkInventory(infos)
	}
	f.flush()
	if !reflect.DeepEqual(before, f.content()) || len(f.ec.Events()) != en {
		t.Fatal("inventory after call changed state")
	}
	for _, e := range f.tc.Events()[tn:] {
		if e.Name == "tool.called" {
			t.Fatal("inventory after call recorded tool.called")
		}
	}
}

// R-KEEZ-M2R8 R-KFMV-ZUHX R-KGUS-DM8M R-KI2O-RDZB R-KLQD-WP7E
// R-D2F3-OMM4 R-581F-PSB9 R-599C-3K1Y
func TestReadResults(t *testing.T) {
	f := setup(t, true)
	before := f.content()
	ns := f.nexts()
	f.flush()
	tn, en := len(f.tc.Events()), len(f.ec.Events())
	for _, u := range []string{"owner", "other"} {
		f.caller.UserID = u
		var list tools.TriggerList
		r := f.call("list", tools.ListArgs{})
		success(t, r, &list)
		if len(list.Triggers) != 4 {
			t.Fatal(list)
		}
		var raw struct{ Triggers []map[string]json.RawMessage }
		if e := json.Unmarshal(r["structuredContent"], &raw); e != nil {
			t.Fatal(e)
		}
		for i, x := range before {
			v := list.Triggers[i]
			if v.ID != x.ID || v.Slug != x.Slug || v.When != x.When || v.Owner != x.OwnerEmail || v.Status != x.Status {
				t.Fatal(v, x)
			}
			if _, ok := raw.Triggers[i]["created"]; ok {
				t.Fatal("listed created")
			}
			var one tools.Trigger
			r = f.call("show", tools.ShowArgs{Slug: x.Slug})
			success(t, r, &one)
			assertTrigger(t, f, one, x)
			if v.LastFired == nil != x.LastFired.IsZero() {
				t.Fatal("last fired presence")
			}
			if v.LastFired != nil && *v.LastFired != x.LastFired.Format(time.RFC3339) {
				t.Fatal("last fired value")
			}
			next, ok := ns[x.ID]
			if (v.Next != nil) != ok || ok && *v.Next != next.Format(time.RFC3339) {
				t.Fatal("list next")
			}
		}
	}
	f.flush()
	if !reflect.DeepEqual(before, f.content()) || !reflect.DeepEqual(ns, f.nexts()) || len(f.ec.Events()) != en {
		t.Fatal("reads changed state")
	}
	for _, e := range f.tc.Events()[tn:] {
		if strings.HasPrefix(e.Name, "cron.") {
			t.Fatal("read lifecycle")
		}
	}
	empty := setup(t, false)
	var list tools.TriggerList
	r := empty.call("list", tools.ListArgs{})
	success(t, r, &list)
	if string(r["structuredContent"]) != "{\"triggers\":[]}" || list.Triggers == nil {
		t.Fatal("empty list")
	}
}
func assertTrigger(t *testing.T, f *fixture, v tools.Trigger, x store.Trigger) {
	t.Helper()
	if v.ID != x.ID || v.Slug != x.Slug || v.When != x.When || v.Owner != x.OwnerEmail || v.Status != x.Status || v.Created != x.Created.Format(time.RFC3339) {
		t.Fatalf("trigger got %+v want %+v", v, x)
	}
	if (v.LastFired != nil) != (!x.LastFired.IsZero()) || v.LastFired != nil && *v.LastFired != x.LastFired.Format(time.RFC3339) {
		t.Fatal("last fired")
	}
	n, ok := f.sch.Next(x.ID)
	if (v.Next != nil) != ok || ok && *v.Next != n.Format(time.RFC3339) {
		t.Fatal("next")
	}
}

// R-D177-AUVF R-535U-6PCH R-54DQ-KH36 R-55LM-Y8TV R-5AH8-HBSN R-5BP4-V3JC R-5CX1-8VA1 R-5GKQ-E6I4
func TestRefusals(t *testing.T) {
	f := setup(t, true)
	for _, s := range []string{"cleanup", "crn_0101010101010101", "CRM_Sync", ""} {
		f.unchanged("show", tools.ShowArgs{Slug: s}, tools.NoTrigger(s))
	}
	for _, s := range []string{"nightly_backup", "cleanup", "CRM_Sync", ""} {
		for _, name := range []string{"update", "pause", "resume", "delete"} {
			a := map[string]string{"slug": s}
			if name == "update" {
				a["when"] = "bogus"
			}
			f.unchanged(name, a, tools.NoTrigger(s))
		}
	}
	for _, slug := range []string{"crm-sync", "CRM_Sync", "9am_report", "_crm", "crm__sync", "crm_", "crm.sync", "crm sync", " crm_sync", "", strings.Repeat("a", 65)} {
		f.unchanged("create", tools.CreateArgs{Slug: slug, When: "bogus"}, tools.InvalidSlug(slug))
	}
	for _, slug := range []string{"hourly", "month_end"} {
		for _, when := range []string{"@daily", "bogus"} {
			f.unchanged("create", tools.CreateArgs{Slug: slug, When: when}, tools.SlugTaken(slug))
		}
	}
	for _, w := range []string{"0 */15 * * * *", "@every 5m", "@reboot", "@annually", "@midnight", "@DAILY", "bogus", "", " */15 * * * *", "*/15 * * * * ", "*/15  * * * *", " @hourly", "@hourly "} {
		f.unchanged("create", tools.CreateArgs{Slug: "crm_sync", When: w}, tools.InvalidWhen(w))
		f.unchanged("update", tools.UpdateArgs{Slug: "hourly", When: w}, tools.InvalidWhen(w))
	}
}

// R-56TJ-C0KK R-D177-AUVF
func TestFailingDatabaseAndRandom(t *testing.T) {
	f := setup(t, true)
	before, ns := f.content(), f.nexts()
	f.flush()
	tn, en := len(f.tc.Events()), len(f.ec.Events())
	f.d.SetFailing(true)
	for _, name := range []string{"list", "show", "create", "update", "pause", "resume", "delete"} {
		a := map[string]string{}
		if name != "list" {
			a["slug"] = "nightly_backup"
		}
		if name == "create" {
			a["slug"] = "crm_sync"
		}
		if name == "create" || name == "update" {
			a["when"] = "bogus"
		}
		refusal(t, f.call(name, a), store.Unreachable)
	}
	for _, slug := range []string{"cleanup", "hourly", "weekly_digest"} {
		for _, name := range []string{"show", "update", "pause", "resume", "delete"} {
			a := map[string]string{"slug": slug}
			if name == "update" {
				a["when"] = "@hourly"
			}
			refusal(t, f.call(name, a), store.Unreachable)
		}
	}
	for _, slug := range []string{"hourly", "crm_sync"} {
		refusal(t, f.call("create", tools.CreateArgs{Slug: slug, When: "@daily"}), store.Unreachable)
	}
	for _, name := range []string{"show", "update", "pause", "resume", "delete"} {
		a := map[string]string{"slug": "CRM_Sync"}
		if name == "update" {
			a["when"] = "bogus"
		}
		refusal(t, f.call(name, a), tools.NoTrigger("CRM_Sync"))
	}
	refusal(t, f.call("create", tools.CreateArgs{Slug: "crm-sync", When: "bogus"}), tools.InvalidSlug("crm-sync"))
	f.d.SetFailing(false)
	f.flush()
	if !reflect.DeepEqual(before, f.content()) || !reflect.DeepEqual(ns, f.nexts()) || len(f.ec.Events()) != en {
		t.Fatal("failing calls changed state")
	}
	for _, e := range f.tc.Events()[tn:] {
		if strings.HasPrefix(e.Name, "cron.") {
			t.Fatal("failing call recorded lifecycle", e)
		}
	}
	// A fresh scheduler and server over the same handle with an exhausted source.
	st := store.New(f.d, store.Config{Now: func() time.Time { return parse("2026-10-05T09:32:00Z") }, Rand: bytes.NewReader(nil)})
	sch, e := scheduler.Start(f.ctx, scheduler.Config{Store: st, Events: f.em, Telemetry: f.w, Rand: bytes.NewReader(make([]byte, 64)), Now: func() time.Time { return parse("2026-10-05T09:32:00Z") }, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	if e != nil {
		t.Fatal(e)
	}
	defer sch.Stop()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "cron", Telemetry: f.w})
	tools.Register(srv, tools.Config{Store: st, Scheduler: sch})
	server := httptest.NewServer(telemetry.Middleware(f.w, events.Middleware(identity.Require(srv))))
	defer server.Close()
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	f.st, f.sch = st, sch
	f.unchanged("create", tools.CreateArgs{Slug: "crm_sync", When: "*/15 * * * *"}, store.Unreachable)
}

// R-L58S-112I R-D177-AUVF
func TestStrictArguments(t *testing.T) {
	f := setup(t, true)
	cases := []struct{ name, args, want string }{
		{"list", `{"slug":"hourly"}`, "slug: unknown field"},
		{"show", `{"name":"hourly"}`, "slug: missing required field\nname: unknown field"},
		{"create", `{"slug":"crm_sync"}`, "when: missing required field"},
		{"create", `{"name":"crm_sync","when":"*/15 * * * *"}`, "slug: missing required field\nname: unknown field"},
		{"create", `{"slug":"crm_sync","when":15,"owner":"ann@example.com"}`, "when: expected string, got number\nowner: unknown field"},
		{"update", `{"slug":"hourly"}`, "when: missing required field"},
		{"update", `{"slug":"hourly","when":60,"status":"paused","new_slug":"every_hour"}`, "when: expected string, got number\nstatus: unknown field\nnew_slug: unknown field"},
		{"pause", `{"id":"crn_0101010101010101"}`, "slug: missing required field\nid: unknown field"},
		{"resume", `{"id":"crn_0404040404040404"}`, "slug: missing required field\nid: unknown field"},
		{"delete", `{"id":"crn_0101010101010101"}`, "slug: missing required field\nid: unknown field"},
	}
	for _, failing := range []bool{false, true} {
		for _, c := range cases {
			before, ns := f.content(), f.nexts()
			f.flush()
			tn, en := len(f.tc.Events()), len(f.ec.Events())
			f.d.SetFailing(failing)
			refusal(t, f.callRaw(c.name, json.RawMessage(c.args)), "invalid arguments:\n"+c.want)
			f.d.SetFailing(false)
			f.flush()
			if !reflect.DeepEqual(before, f.content()) || !reflect.DeepEqual(ns, f.nexts()) || len(f.ec.Events()) != en {
				t.Fatal("strict argument refusal changed state", c.name)
			}
			for _, e := range f.tc.Events()[tn:] {
				if strings.HasPrefix(e.Name, "cron.") {
					t.Fatal("strict argument refusal lifecycle", e)
				}
			}
		}
	}
}

// R-LG7V-GYQR R-LNJ9-RL6X
func TestCreate(t *testing.T) {
	for _, when := range []string{"*/15 * * * *", "0 0 30 2 *"} {
		t.Run(when, func(t *testing.T) {
			f := setup(t, true)
			before := f.content()
			var v tools.Trigger
			success(t, f.call("create", tools.CreateArgs{Slug: "crm_sync", When: when}), &v)
			x, e := f.st.Get(f.ctx, "crm_sync")
			if e != nil {
				t.Fatal(e)
			}
			assertTrigger(t, f, v, x)
			if x.OwnerID != f.caller.UserID || x.OwnerEmail != f.caller.Email || x.Status != store.Active || !x.LastFired.IsZero() || x.Created != parse("2026-10-05T09:32:00Z") {
				t.Fatal(x)
			}
			if when == "*/15 * * * *" && (v.Next == nil || *v.Next != "2026-10-05T09:45:00Z") || when == "0 0 30 2 *" && v.Next != nil {
				t.Fatal("create next")
			}
			after := f.content()
			if len(after) != 5 || !reflect.DeepEqual(before, after[1:]) {
				t.Fatal("create changed siblings")
			}
			f.flush()
			checkCallEvents(t, f, "create", "additive", "ok", "cron.crm_sync.created", x)
		})
	}
}

// R-5E4X-MN0Q R-D3N0-2ECT
func TestUpdate(t *testing.T) {
	cases := []struct{ slug, when, next string }{{"hourly", "@hourly", "2026-10-05T10:00:00Z"}, {"hourly", "45 * * * *", "2026-10-05T09:45:00Z"}, {"hourly", "0 * * * *", "2026-10-05T10:00:00Z"}, {"hourly", "30 * * * *", "2026-10-05T10:30:00Z"}, {"hourly", "0 0 30 2 *", ""}, {"weekly_digest", "0 9 * * 1", ""}}
	for _, c := range cases {
		t.Run(c.slug+c.when, func(t *testing.T) {
			f := setup(t, true)
			before := f.content()
			x, e := f.st.Get(f.ctx, c.slug)
			if e != nil {
				t.Fatal(e)
			}
			var v tools.Trigger
			success(t, f.call("update", tools.UpdateArgs{Slug: c.slug, When: c.when}), &v)
			x.When = c.when
			assertTrigger(t, f, v, x)
			if c.next == "" && v.Next != nil || c.next != "" && (v.Next == nil || *v.Next != c.next) {
				t.Fatal("update next")
			}
			for i := range before {
				if before[i].Slug == c.slug {
					before[i].When = c.when
				}
			}
			if !reflect.DeepEqual(before, f.content()) {
				t.Fatal("update other fields")
			}
			f.flush()
			checkCallEvents(t, f, "update", "additive", "ok", "", x)
			if c.slug == "weekly_digest" {
				success(t, f.call("resume", tools.ResumeArgs{Slug: c.slug}), &v)
				if v.Next == nil || *v.Next != "2026-10-12T09:00:00Z" {
					t.Fatal("paused update then resume")
				}
			}
		})
	}
	f := setup(t, true)
	f.move("2026-10-05T10:05:00Z")
	var v tools.Trigger
	success(t, f.call("update", tools.UpdateArgs{Slug: "hourly", When: "@hourly"}), &v)
	if v.Next == nil || *v.Next != "2026-10-05T10:00:00Z" {
		t.Fatal("no-op update moved due next")
	}
}

// R-5HSM-RY8T R-5J0J-5PZI R-5K8F-JHQ7 R-5LGB-X9GW
func TestPauseResumeDelete(t *testing.T) {
	for _, c := range []struct{ name, slug, status, next, event string }{
		{"pause", "hourly", store.Paused, "", "cron.hourly.paused"},
		{"pause", "weekly_digest", store.Paused, "", ""},
		{"resume", "hourly", store.Active, "2026-10-05T10:00:00Z", ""},
		{"resume", "weekly_digest", store.Active, "2026-10-12T08:00:00Z", "cron.weekly_digest.resumed"},
		{"delete", "hourly", "", "", "cron.hourly.deleted"},
		{"delete", "weekly_digest", "", "", "cron.weekly_digest.deleted"},
	} {
		t.Run(c.name+c.slug, func(t *testing.T) {
			f := setup(t, true)
			before := f.content()
			x, e := f.st.Get(f.ctx, c.slug)
			if e != nil {
				t.Fatal(e)
			}
			args := map[string]string{"slug": c.slug}
			r := f.call(c.name, args)
			if c.name == "delete" {
				var v tools.Deleted
				success(t, r, &v)
				if !v.Deleted || v.ID != x.ID || string(r["structuredContent"]) != "{\"deleted\":true,\"id\":\""+x.ID+"\"}" {
					t.Fatal(v)
				}
				if _, e = f.st.Get(f.ctx, c.slug); !errors.Is(e, store.ErrNotFound) {
					t.Fatal("delete still found")
				}
				if _, ok := f.sch.Next(x.ID); ok {
					t.Fatal("deleted next")
				}
				want := make([]store.Trigger, 0)
				for _, a := range before {
					if a.ID != x.ID {
						want = append(want, a)
					}
				}
				if !reflect.DeepEqual(want, f.content()) {
					t.Fatal("delete siblings")
				}
			} else {
				var v tools.Trigger
				success(t, r, &v)
				x.Status = c.status
				assertTrigger(t, f, v, x)
				if c.next == "" && v.Next != nil || c.next != "" && (v.Next == nil || *v.Next != c.next) {
					t.Fatal("status next")
				}
				for i := range before {
					if before[i].ID == x.ID {
						before[i].Status = c.status
					}
				}
				if !reflect.DeepEqual(before, f.content()) {
					t.Fatal("status siblings")
				}
				var shown tools.Trigger
				success(t, f.call("show", tools.ShowArgs{Slug: c.slug}), &shown)
				if !reflect.DeepEqual(v, shown) {
					t.Fatal("show after status")
				}
				f.serial--
				f.caller.RequestID = fmt.Sprintf("%032x", f.serial)
			}
			f.flush()
			kind := "destructive"
			if c.name == "resume" {
				kind = "additive"
			}
			checkCallEvents(t, f, c.name, kind, "ok", c.event, x)
			if c.name == "delete" {
				for _, name := range []string{"show", "update", "pause", "resume", "delete"} {
					a := map[string]string{"slug": c.slug}
					if name == "update" {
						a["when"] = "@daily"
					}
					f.unchanged(name, a, tools.NoTrigger(c.slug))
				}
				f.caller.UserID = "other"
				var created tools.Trigger
				success(t, f.call("create", tools.CreateArgs{Slug: c.slug, When: "@hourly"}), &created)
				if created.LastFired != nil || created.Owner != f.caller.Email {
					t.Fatal("recreated old fire")
				}
			}
		})
	}
	f := setup(t, true)
	f.move("2026-10-05T10:05:00Z")
	var v tools.Trigger
	success(t, f.call("resume", tools.ResumeArgs{Slug: "hourly"}), &v)
	if v.Next == nil || *v.Next != "2026-10-05T10:00:00Z" {
		t.Fatal("resume moved due next")
	}
}

// R-L2SZ-9HL4
func checkCallEvents(t *testing.T, f *fixture, name, kind, outcome, lifecycle string, x store.Trigger) {
	t.Helper()
	var trace []telemetry.Event
	for _, e := range f.tc.Events() {
		if e.RequestID == f.caller.RequestID {
			trace = append(trace, e)
		}
	}
	names := []string{"request.started", "tool.called", "request.finished"}
	if lifecycle != "" {
		names = []string{"request.started", lifecycle, "tool.called", "request.finished"}
	}
	if len(trace) != len(names) {
		t.Fatalf("trace count %+v", trace)
	}
	for i, e := range trace {
		if e.Name != names[i] || e.User != f.caller.UserID {
			t.Fatalf("trace %+v", trace)
		}
		if e.Name == "tool.called" {
			if len(e.Attrs) != 4 || e.Attrs["tool"] != name || e.Attrs["kind"] != kind || e.Attrs["outcome"] != outcome || e.Attrs["duration_us"] != int64(0) {
				t.Fatal("tool attrs", e.Attrs)
			}
		}
		if e.Name == lifecycle {
			if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"trigger": x.ID, "when": x.When}) {
				t.Fatal("lifecycle attrs", e)
			}
		}
	}
	var bus []events.Event
	for _, e := range f.ec.Events() {
		if e.RequestID == f.caller.RequestID {
			bus = append(bus, e)
		}
	}
	if lifecycle == "" {
		if len(bus) != 0 {
			t.Fatal("unexpected emission")
		}
	} else {
		if len(bus) != 1 || bus[0].Name != lifecycle || bus[0].User != f.caller.UserID || bus[0].Cause != "" || bus[0].Depth != 0 || !reflect.DeepEqual(bus[0].Attrs, events.Attrs{"trigger": x.ID, "when": x.When}) {
			t.Fatal("bus lifecycle", bus)
		}
	}
}

// R-L7OK-SKJW
func TestUnknownTool(t *testing.T) {
	f := setup(t, true)
	before, ns := f.content(), f.nexts()
	f.flush()
	tn, en := len(f.tc.Events()), len(f.ec.Events())
	_, e := f.client.CallTool(f.ctx, f.caller, "rename", json.RawMessage(`{"slug":"hourly","new_slug":"every_hour"}`))
	var rpc *mcp.RPCError
	if !errors.As(e, &rpc) || rpc.Code != -32602 || rpc.Message != "Unknown tool: rename" {
		t.Fatal(e)
	}
	f.flush()
	if !reflect.DeepEqual(before, f.content()) || !reflect.DeepEqual(ns, f.nexts()) || len(f.ec.Events()) != en {
		t.Fatal("rename changed state")
	}
	for _, e := range f.tc.Events()[tn:] {
		if e.Name == "tool.called" {
			t.Fatal("unknown tool recorded call")
		}
	}
}

// R-L6GO-EST7
func TestAbsentArguments(t *testing.T) {
	f := setup(t, true)
	for _, name := range []string{"list", "show", "create", "update", "pause", "resume", "delete"} {
		with := f.call(name, tools.ListArgs{})
		without := f.rawMissing(name)
		delete(with, "_meta")
		delete(without, "_meta")
		delete(without, "resultType")
		if !reflect.DeepEqual(with, without) {
			t.Fatalf("missing arguments differ %s\n%v\n%v", name, with, without)
		}
	}
}
func (f *fixture) rawMissing(name string) map[string]json.RawMessage {
	f.t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	req, e := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/mcp", strings.NewReader(body))
	if e != nil {
		f.t.Fatal(e)
	}
	identity.Forward(f.caller, req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", name)
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		f.t.Fatal(e)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			f.t.Error(err)
		}
	}()
	data, e := io.ReadAll(response.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	var rpc struct{ Result map[string]json.RawMessage }
	if e = json.Unmarshal(data, &rpc); e != nil {
		f.t.Fatal(e)
	}
	if response.StatusCode != 200 || rpc.Result == nil {
		f.t.Fatalf("raw response %s", data)
	}
	return rpc.Result
}

type causeTransport struct{}

func (causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Event-Cause", "evt_8c3f1a6e2d9b4075")
	r.Header.Set("X-Event-Depth", "1")
	return http.DefaultTransport.RoundTrip(r)
}

// R-LNJ9-RL6X R-5LGB-X9GW
func TestLifecycleCause(t *testing.T) {
	for _, name := range []string{"create", "pause", "resume", "delete"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t, true)
			f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: causeTransport{}}})
			slug := "hourly"
			args := map[string]string{"slug": slug}
			if name == "create" {
				slug = "crm_sync"
				args["slug"] = slug
				args["when"] = "*/15 * * * *"
			}
			if name == "resume" {
				slug = "weekly_digest"
				args["slug"] = slug
			}
			r := f.call(name, args)
			if _, ok := r["isError"]; ok {
				t.Fatal("lifecycle refused")
			}
			f.flush()
			bus := f.ec.Events()
			if len(bus) != 1 || bus[0].Name != "cron."+slug+"."+map[string]string{"create": "created", "pause": "paused", "resume": "resumed", "delete": "deleted"}[name] || bus[0].Cause != "evt_8c3f1a6e2d9b4075" || bus[0].Depth != 2 || bus[0].User != f.caller.UserID || bus[0].RequestID != f.caller.RequestID {
				t.Fatal("causal emission", bus)
			}
			for _, e := range f.tc.Events() {
				if strings.HasPrefix(e.Name, "cron.") {
					if len(e.Attrs) != 2 || e.Attrs["when"] == nil || e.Attrs["trigger"] == nil {
						t.Fatal("cause contaminated trail", e)
					}
				}
			}
		})
	}
}

// R-L2SZ-9HL4
func TestRefusalTrace(t *testing.T) {
	f := setup(t, true)
	refusal(t, f.call("show", tools.ShowArgs{Slug: "cleanup"}), tools.NoTrigger("cleanup"))
	f.flush()
	checkCallEvents(t, f, "show", "read", "error", "", store.Trigger{})
	refusal(t, f.callRaw("create", json.RawMessage(`{"slug":"crm_sync"}`)), "invalid arguments:\nwhen: missing required field")
	f.flush()
	checkCallEvents(t, f, "create", "additive", "invalid_arguments", "", store.Trigger{})
}

// R-4QYU-CZXJ R-4S6Q-QRO8
func TestRefusalCopy(t *testing.T) {
	for _, fn := range []func(string) string{tools.NoTrigger, tools.InvalidSlug, tools.SlugTaken, tools.InvalidWhen} {
		for _, arg := range []string{"", "a'\n<&value", "雪", " spaced "} {
			got := fn(arg)
			if got == "" || !strings.Contains(got, arg) {
				t.Fatalf("refusal %q does not carry %q", got, arg)
			}
		}
	}
}

func schemaWithoutCopy(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var schema struct{ Properties map[string]json.RawMessage }
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for _, property := range schema.Properties {
		assertKeys(t, property, []string{"type", "description"})
		var fields map[string]string
		if err := json.Unmarshal(property, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["description"] == "" {
			t.Fatal("empty field description")
		}
	}
	// Remove only the descriptions, retaining object member order for comparison.
	return string(regexp.MustCompile(`,"description":"(?:[^"\\]|\\.)*"`).ReplaceAll(raw, nil))
}
