package trail_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	sessions "github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
)

type trailToolRig struct {
	d       *db.DB
	s       *store.Store
	w       *telemetry.Writer
	capture *telemetry.Capture
	client  *mcp.Client
	serial  int
}

func trailDatabase(t *testing.T, path string) (*db.DB, *store.Store) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: sessions.Migrations(), Now: func() time.Time { return trailNow }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, store.New(d, store.Config{Now: func() time.Time { return trailNow }})
}
func newTrailToolRig(t *testing.T) *trailToolRig {
	t.Helper()
	d, s := trailDatabase(t, filepath.Join(t.TempDir(), "sessions.db"))
	capture := &telemetry.Capture{}
	w := trailWriter(t, capture)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "sessions", Version: "test", Telemetry: w})
	tools.Register(srv, tools.Config{Store: s, Telemetry: w})
	h := httptest.NewServer(identity.Require(srv))
	t.Cleanup(h.Close)
	return &trailToolRig{d: d, s: s, w: w, capture: capture, client: mcp.NewClient(mcp.ClientConfig{Endpoint: h.URL, HTTPClient: h.Client(), Name: "test", Version: "test"})}
}
func trailJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (r *trailToolRig) call(t *testing.T, name string, args any, want string) telemetry.Event {
	t.Helper()
	r.serial++
	q := fmt.Sprintf("trail-call-%d", r.serial)
	result, err := r.client.CallTool(context.Background(), identity.Caller{UserID: "owner", Email: "private@invalid.test", RequestID: q}, name, trailJSON(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError() != (want != trail.ResultOK) {
		t.Fatalf("%s error=%v want result %s: %+v", name, result.IsError(), want, result)
	}
	trailFlush(t, r.w)
	var got []telemetry.Event
	changeAt, toolAt := -1, -1
	for i, e := range r.capture.Events() {
		if e.RequestID == q {
			if e.Name == trail.EventChangeFinished {
				got = append(got, e)
				changeAt = i
			}
			if e.Name == "tool.called" {
				toolAt = i
			}
			if e.Name == trail.EventRefreshFinished {
				t.Fatalf("tool emitted refresh: %+v", e)
			}
		}
	}
	if len(got) != 1 || changeAt >= toolAt {
		t.Fatalf("%s changes=%+v indexes %d/%d", name, got, changeAt, toolAt)
	}
	e := got[0]
	if e.User != "owner" || e.Attrs["tool"] != name || e.Attrs["result"] != want {
		t.Fatalf("%s event=%+v", name, e)
	}
	if want != trail.ResultOK {
		for _, key := range []string{"sessions", "turns_new", "turns_changed", "roots"} {
			if v, ok := e.Attrs[key]; ok && v != int64(0) {
				t.Fatalf("%s failure count %s=%v", name, key, v)
			}
		}
	}
	return e
}
func trailEntry(id string, n int) tools.PushSession {
	turns := make([]tools.PushTurn, n)
	for i := range n {
		turns[i] = tools.PushTurn{Moment: trailNow.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano), Whose: "agent", Model: "unknown-model", Input: 1, Output: 10}
	}
	return tools.PushSession{Tool: "codex", ID: id, Host: "workstation", Directory: "/work/trail", Turns: turns}
}
func trailPush(entries ...tools.PushSession) tools.PushTurnsArgs {
	return tools.PushTurnsArgs{Sessions: entries}
}
func trailCounts(t *testing.T, e telemetry.Event, s, n, c int) {
	t.Helper()
	if e.Attrs["sessions"] != int64(s) || e.Attrs["turns_new"] != int64(n) || e.Attrs["turns_changed"] != int64(c) {
		t.Fatalf("push attrs=%v want %d/%d/%d", e.Attrs, s, n, c)
	}
}

// R-RS2J-UE2S R-QP8I-3XLL R-QPMX-HQQS
func TestToolChangeOutcomes(t *testing.T) {
	r := newTrailToolRig(t)
	for _, tc := range []struct {
		name      string
		good, bad any
		attrs     telemetry.Attrs
	}{
		{tools.ToolPushTurns, trailPush(trailEntry("accepted", 1)), trailPush(func() tools.PushSession { s := trailEntry("bad", 1); s.Directory = "relative"; return s }()), telemetry.Attrs{}},
		{tools.ToolAddRule, tools.AddRuleArgs{Host: "other-host", Directory: "/other/rule", Kind: "build"}, tools.AddRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz", Kind: "operate"}, telemetry.Attrs{"host": "other-host", "directory": "/other/rule", "command": "", "kind": "build"}},
		{tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz"}, tools.RemoveRuleArgs{Host: "missing", Directory: "/missing"}, telemetry.Attrs{"host": "ld01", "directory": "/mnt/projects/biz", "command": "", "kind": "plan"}},
		{tools.ToolSetPrice, tools.SetPriceArgs{Model: "configured", Effective: "2026-01-01", Input: "1", Cached: "2", Output: "3", Reasoning: "4"}, tools.SetPriceArgs{Model: "configured", Effective: "bad", Input: "1", Cached: "2", Output: "3", Reasoning: "4"}, telemetry.Attrs{"model": "configured", "effective": "2026-01-01"}},
		{tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "claude-opus-5-5", Effective: "2026-01-01"}, tools.RemovePriceArgs{Model: "absent", Effective: "2026-01-01"}, telemetry.Attrs{"model": "claude-opus-5-5", "effective": "2026-01-01"}},
		{tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}, tools.SetAwayLimitArgs{Minutes: 0}, telemetry.Attrs{"minutes": int64(30)}},
	} {
		e := r.call(t, tc.name, tc.good, trail.ResultOK)
		for k, v := range tc.attrs {
			if !reflect.DeepEqual(e.Attrs[k], v) {
				t.Fatalf("%s %s=%v want %v", tc.name, k, e.Attrs[k], v)
			}
		}
		refused := r.call(t, tc.name, tc.bad, trail.ResultRefused)
		// Values describe the actual request even when the store refuses it.
		raw := map[string]any{}
		if err := json.Unmarshal(trailJSON(t, tc.bad), &raw); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"host", "directory", "command", "kind", "model", "effective", "minutes"} {
			if want, ok := raw[k]; ok {
				if want == nil {
					want = ""
				}
				if k == "minutes" {
					want = int64(want.(float64))
				}
				if refused.Attrs[k] != want {
					t.Fatalf("%s refused %s=%v want %v", tc.name, k, refused.Attrs[k], want)
				}
			}
		}
		if tc.name == tools.ToolRemoveRule && refused.Attrs["kind"] != "" {
			t.Fatal(refused)
		}
		r.d.SetFailing(true)
		failed := r.call(t, tc.name, tc.good, trail.ResultFailed)
		for k, v := range tc.attrs {
			if tc.name == tools.ToolRemoveRule && k == "kind" {
				v = ""
			}
			if !reflect.DeepEqual(failed.Attrs[k], v) {
				t.Fatalf("%s failed %s=%v want %v", tc.name, k, failed.Attrs[k], v)
			}
		}
		r.d.SetFailing(false)
	}
	command := "inspect"
	key := tools.AddRuleArgs{Host: "command-host", Directory: "/command-rule", Command: &command, Kind: "operate"}
	for _, want := range []string{trail.ResultOK, trail.ResultRefused} {
		e := r.call(t, tools.ToolAddRule, key, want)
		for k, v := range (telemetry.Attrs{"host": key.Host, "directory": key.Directory, "command": command, "kind": key.Kind}) {
			if e.Attrs[k] != v {
				t.Fatalf("commandful add %s=%v want %v", k, e.Attrs[k], v)
			}
		}
	}
	removal := tools.RemoveRuleArgs{Host: key.Host, Directory: key.Directory, Command: &command}
	for _, want := range []string{trail.ResultOK, trail.ResultRefused} {
		e := r.call(t, tools.ToolRemoveRule, removal, want)
		kind := ""
		if want == trail.ResultOK {
			kind = key.Kind
		}
		for k, v := range (telemetry.Attrs{"host": key.Host, "directory": key.Directory, "command": command, "kind": kind}) {
			if e.Attrs[k] != v {
				t.Fatalf("commandful remove %s=%v want %v", k, e.Attrs[k], v)
			}
		}
	}

	// An input rejected by appkit never invokes a change recorder.
	q := "invalid-arguments"
	_, err := r.client.CallTool(context.Background(), identity.Caller{UserID: "owner", RequestID: q}, tools.ToolAddRule, json.RawMessage(`{"directory":"/rule","kind":"build"}`))
	if err != nil {
		t.Fatal(err)
	}
	trailFlush(t, r.w)
	for _, e := range r.capture.Events() {
		if e.RequestID == q && e.Name == trail.EventChangeFinished {
			t.Fatal(e)
		}
	}
}

// R-A0PJ-BC24
func TestPushChangeCounts(t *testing.T) {
	r := newTrailToolRig(t)
	a := trailEntry("first", 3)
	empty := trailEntry("no-turns", 0)
	trailCounts(t, r.call(t, tools.ToolPushTurns, trailPush(a, empty), trail.ResultOK), 2, 3, 0)
	a.Turns[0].Output = 19
	trailCounts(t, r.call(t, tools.ToolPushTurns, trailPush(a), trail.ResultOK), 1, 0, 1)
	trailCounts(t, r.call(t, tools.ToolPushTurns, trailPush(a), trail.ResultOK), 1, 0, 0)
	duplicate := trailEntry("duplicate", 1)
	duplicate.Turns = append(duplicate.Turns, duplicate.Turns[0])
	duplicate.Turns[1].Output = 20
	trailCounts(t, r.call(t, tools.ToolPushTurns, trailPush(duplicate), trail.ResultOK), 1, 1, 0)
	duplicate.Turns = duplicate.Turns[1:]
	trailCounts(t, r.call(t, tools.ToolPushTurns, trailPush(duplicate), trail.ResultOK), 1, 0, 0)
	bad := trailEntry("relative", 1)
	bad.Directory = "relative"
	trailCounts(t, r.call(t, tools.ToolPushTurns, trailPush(trailEntry("rollback", 2), bad), trail.ResultRefused), 0, 0, 0)
}

// R-I6JO-SH4Z
func TestSettingChangeCountsRoots(t *testing.T) {
	r := newTrailToolRig(t)
	first, second, child := trailEntry("root-one", 1), trailEntry("root-two", 1), trailEntry("child", 1)
	child.Parent = &tools.SessionRef{Tool: first.Tool, ID: first.ID}
	r.call(t, tools.ToolPushTurns, trailPush(first, second, child), trail.ResultOK)
	for _, tc := range []struct {
		name string
		args any
	}{
		{tools.ToolAddRule, tools.AddRuleArgs{Host: "config", Directory: "/config", Kind: "build"}},
		{tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "config", Directory: "/config"}},
		{tools.ToolSetPrice, tools.SetPriceArgs{Model: "configured", Effective: "2026-01-01", Input: "1", Cached: "1", Output: "1", Reasoning: "1"}},
		{tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "configured", Effective: "2026-01-01"}},
		{tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 30}},
	} {
		if e := r.call(t, tc.name, tc.args, trail.ResultOK); e.Attrs["roots"] != int64(2) {
			t.Fatal(e)
		}
	}
	r.call(t, tools.ToolAddRule, tools.AddRuleArgs{Host: "ld01", Directory: "/mnt/projects/biz", Kind: "build"}, trail.ResultRefused)
	// Add enough roots to cross a refresh page boundary.
	for start := 2; start < store.BatchSize+1; {
		n := min(store.MaxPushSessions, store.BatchSize+1-start)
		entries := make([]tools.PushSession, n)
		for i := range n {
			entries[i] = trailEntry(fmt.Sprintf("extra-%d", start+i), 1)
		}
		r.call(t, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: entries}, trail.ResultOK)
		start += n
	}
	if e := r.call(t, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 20}, trail.ResultOK); e.Attrs["roots"] != int64(store.BatchSize+1) {
		t.Fatal(e)
	}
}

// R-QTAM-N1YV
func TestReadsHaveNoChangeEvents(t *testing.T) {
	r := newTrailToolRig(t)
	for _, failed := range []bool{false, true} {
		r.d.SetFailing(failed)
		for i, tc := range []struct {
			name string
			args any
		}{
			{tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{{Tool: "codex", ID: "absent"}}}},
			{tools.ToolListSessions, tools.ListSessionsArgs{}},
			{tools.ToolTotalSessions, tools.TotalSessionsArgs{Period: "week", From: "2026-10-26", Group: "kind"}},
			{tools.ToolListRules, tools.NoArgs{}}, {tools.ToolListPrices, tools.NoArgs{}}, {tools.ToolGetAwayLimit, tools.NoArgs{}},
		} {
			q := fmt.Sprintf("read-%t-%d", failed, i)
			_, err := r.client.CallTool(context.Background(), identity.Caller{UserID: "reader", RequestID: q}, tc.name, trailJSON(t, tc.args))
			if err != nil {
				t.Fatal(err)
			}
			trailFlush(t, r.w)
			for _, e := range r.capture.Events() {
				if e.RequestID == q && (e.Name == trail.EventChangeFinished || e.Name == trail.EventRefreshFinished) {
					t.Fatal(e)
				}
			}
		}
	}
}
