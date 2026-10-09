package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

var ctx = context.Background()
var instant = time.Date(2025, 2, 3, 4, 5, 6, 987654321, time.FixedZone("test", 3600))

func clock() time.Time                 { return instant }
func normalized(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func open(t *testing.T, path string, r io.Reader) (*store.Store, *db.DB) {
	t.Helper()
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: prompts.Migrations(), Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	return store.New(d, store.Config{Now: clock, Rand: r}), d
}
func setup(t *testing.T) (*store.Store, *db.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.db")
	r := []byte{}
	for i := 0; i < 128; i++ {
		r = append(r, 0, 0, 0, 0, 0, 0, byte(i>>8), byte(i))
	}
	s, d := open(t, path, bytes.NewReader(r))
	return s, d, path
}
func draft(name string) store.Draft {
	return store.Draft{Owner: "alice", OwnerEmail: "alice@example.test", Name: name, Model: "model-a", Prompt: "first message", System: "system message", Tools: []string{"suite", "files"}, Schema: json.RawMessage(" { \"type\": \"object\" } ")}
}
func create(t *testing.T, s *store.Store, name string) store.Prompt {
	t.Helper()
	p, e := s.Create(ctx, draft(name))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func record(p store.Prompt, n int, status string) store.Run {
	return store.Run{ID: fmt.Sprintf("prr_%016x", n), Prompt: p.ID, Model: p.Model, User: p.Owner, RequestID: "request", Trigger: store.TriggerManual, Status: status, Started: instant}
}
func add(t *testing.T, s *store.Store, r store.Run) store.Run {
	t.Helper()
	v, e := s.AddRun(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func catalogError(t *testing.T, e error) {
	t.Helper()
	if e == nil {
		t.Fatal("expected catalog error")
	}
	for _, sentinel := range []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded, store.ErrNotSubscribed, store.ErrDelivered} {
		if errors.Is(e, sentinel) {
			t.Fatalf("content error: %v", e)
		}
	}
}
func equal[T any](t *testing.T, a, b T) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v want %#v", a, b)
	}
}
func content(t *testing.T, s *store.Store) []store.Prompt {
	t.Helper()
	a, e := s.List(ctx, "alice")
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.List(ctx, "bob")
	if e != nil {
		t.Fatal(e)
	}
	return append(a, b...)
}

// R-LWFC-8AWE R-LXN8-M2N3 R-M031-DM4H R-M1AX-RDV6
func TestIDs(t *testing.T) {
	for _, c := range []struct {
		prefix string
		new    func(io.Reader) (string, error)
		valid  func(string) bool
		other  func(string) bool
	}{{store.PromptPrefix, store.NewPromptID, store.ValidPromptID, store.ValidRunID}, {store.RunPrefix, store.NewRunID, store.ValidRunID, store.ValidPromptID}} {
		r := bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8, 0xab})
		id, e := c.new(r)
		if e != nil || id != c.prefix+"0102030405060708" || r.Len() != 1 || !c.valid(id) || c.other(id) {
			t.Fatal(id, e, r.Len())
		}
		for _, bad := range []string{"", c.prefix + "010203040506070A", c.prefix + "01020304050607", id + " ", "run_0102030405060708"} {
			if c.valid(bad) {
				t.Fatal(bad)
			}
		}
		for n := 0; n < 8; n++ {
			v, e := c.new(bytes.NewReader(make([]byte, n)))
			if v != "" || e == nil {
				t.Fatal(v, e)
			}
		}
	}
}

// R-YAWY-LH7K R-VTET-VUR1 R-VCC8-J2DB R-VDK4-WU40 R-VB4C-5AMM
func TestValidation(t *testing.T) {
	for _, s := range []string{"a", "0", "a-", "about-us", "toolset", "mcp-audit", "events-digest", "declarations2", strings.Repeat("a", 64)} {
		if !store.ValidName(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"about", "tools", "mcp", "events", "declarations", "Daily Digest", " digest", "-digest", "a_b", "", strings.Repeat("a", 65), "é"} {
		if store.ValidName(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"repo.pushed", "crm.contact_added", "a1.b_2c", "repo.git.pushed", "repo.*", "*.*", "*.pushed", "cron.*.fired"} {
		if !store.ValidEvent(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"*", "pushed", "Repo.Pushed", ".pushed", "repo.", "repo..pushed", "repo.pushed.", "repo.2fa", "repo._pushed", "repo.pushed_", "repo.push__ed", "re*po.pushed", "repo.push*", "repo.**", "repo-x.pushed", " repo.pushed", "repo.pushed ", "repo.pushed\n", ""} {
		if store.ValidEvent(s) {
			t.Fatal(s)
		}
	}
	sentinels := []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded, store.ErrNotSubscribed, store.ErrDelivered}
	for i, a := range sentinels {
		if a == nil {
			t.Fatal(i)
		}
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatal(i, j)
			}
		}
	}
	if store.Unreachable == "" || strings.ContainsAny(store.Unreachable, "\r\n") {
		t.Fatal(store.Unreachable)
	}
	equal(t, store.TriggerManual, "manual")
	equal(t, store.TriggerEvent, "event")
}

// R-V3SX-UO6G R-V8OJ-DR58 R-V9WF-RIVX
func TestConstantsAndTruncation(t *testing.T) {
	equal(t, []string{store.StatusQueued, store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed}, []string{"queued", "running", "exited", "killed", "timed_out", "failed"})
	equal(t, []string{store.ReasonStartFailed, store.ReasonQueueAbandoned}, []string{"start_failed", "queue_abandoned"})
	for _, a := range []bool{false, true} {
		for _, b := range []bool{false, true} {
			if (store.Run{StdoutTruncated: a, StderrTruncated: b}).Truncated() != (a || b) {
				t.Fatal(a, b)
			}
		}
	}
}

// R-UWHJ-K1QA R-VES1-ALUP R-W0Q8-6H77 R-WCX8-06M5 R-WE54-DYCU R-WBPB-MEVG
func TestEmptyAndOwnership(t *testing.T) {
	s, _, _ := setup(t)
	for _, owner := range []string{"", "alice", "bob"} {
		ps, e := s.List(ctx, owner)
		if e != nil || ps == nil || len(ps) != 0 {
			t.Fatal(ps, e)
		}
	}
	for _, f := range []func(context.Context) ([]store.Run, error){s.Running, s.Queued} {
		rs, e := f(ctx)
		if e != nil || rs == nil || len(rs) != 0 {
			t.Fatal(rs, e)
		}
	}
	yes, e := s.Taken(ctx, "a")
	if e != nil || yes {
		t.Fatal(yes, e)
	}
	_, e = s.RunByID(ctx, "absent")
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	b := create(t, s, "b")
	a := create(t, s, "a")
	other := draft("c")
	other.Owner = "bob"
	if _, e = s.Create(ctx, other); e != nil {
		t.Fatal(e)
	}
	ps, e := s.List(ctx, "alice")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, ps, []store.Prompt{a, b})
	for _, pair := range [][2]string{{"bob", "a"}, {"alice", a.ID}, {"", "a"}, {"alice", ""}, {"alice", "absent"}} {
		p, e := s.Find(ctx, pair[0], pair[1])
		equal(t, p, store.Prompt{})
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"a", "b", "c"} {
		yes, e = s.Taken(ctx, name)
		if !yes || e != nil {
			t.Fatal(name, yes, e)
		}
	}
}

// R-UXPF-XTGZ R-V50U-8FX5 R-VQZ1-4B9N R-W360-Y0OL R-W81M-H3ND R-W99I-UVE2 R-MJ98-VPMC R-W6TQ-3BWO R-W5LT-PK5Z
func TestCreateAndFailures(t *testing.T) {
	s, d, path := setup(t)
	want := draft("example")
	p, e := s.Create(ctx, want)
	if e != nil {
		t.Fatal(e)
	}
	if !store.ValidPromptID(p.ID) {
		t.Fatal(p.ID)
	}
	equal(t, p, store.Prompt{ID: p.ID, Name: want.Name, Owner: want.Owner, OwnerEmail: want.OwnerEmail, Model: want.Model, Prompt: want.Prompt, System: want.System, Tools: want.Tools, Schema: want.Schema, Created: normalized(instant), Subscriptions: []store.Subscription{}})
	read, e := s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, read, p)
	r := bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	s = store.New(d, store.Config{Now: clock, Rand: r})
	q, e := s.Create(ctx, draft("second"))
	if e != nil || q.ID != "prm_0102030405060708" || r.Len() != 0 {
		t.Fatal(q, e, r.Len())
	}
	before := content(t, s)
	r = bytes.NewReader(make([]byte, 64))
	s = store.New(d, store.Config{Now: clock, Rand: r})
	_, e = s.Create(ctx, draft("collision"))
	catalogError(t, e)
	if r.Len() != 0 {
		t.Fatal(r.Len())
	}
	equal(t, content(t, s), before)
	for _, bad := range []store.Draft{{}, {Owner: "alice", Name: "ok", Prompt: "text"}, {Owner: "alice", Name: "ok", Model: "m"}, {Owner: "alice", Name: "about", Model: "m", Prompt: "text"}} {
		r = bytes.NewReader(make([]byte, 8))
		s = store.New(d, store.Config{Now: clock, Rand: r})
		v, e := s.Create(ctx, bad)
		catalogError(t, e)
		equal(t, v, store.Prompt{})
		equal(t, r.Len(), 8)
		equal(t, content(t, s), before)
	}
	r = bytes.NewReader(make([]byte, 8))
	s = store.New(d, store.Config{Now: clock, Rand: r})
	taken := draft("example")
	taken.Owner = "bob"
	_, e = s.Create(ctx, taken)
	if !errors.Is(e, store.ErrNameTaken) || r.Len() != 8 {
		t.Fatal(e, r.Len())
	}
	s = store.New(d, store.Config{Now: clock, Rand: bytes.NewReader([]byte{1})})
	v, e := s.Create(ctx, draft("short"))
	catalogError(t, e)
	equal(t, v, store.Prompt{})
	equal(t, content(t, s), before)
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, bytes.NewReader([]byte{9, 8, 7, 6, 5, 4, 3, 2}))
	equal(t, content(t, s), before)
	nilDraft := store.Draft{Owner: "alice", Name: "minimal", Model: "m", Prompt: "p"}
	v, e = s.Create(ctx, nilDraft)
	if e != nil || v.Tools == nil || v.Schema != nil || v.Subscriptions == nil || v.Created.Location() != time.UTC || v.Created.Nanosecond() != 0 {
		t.Fatal(v, e)
	}
}

// R-V68Q-M7NU R-MKH5-9HD1 R-0P8E-Z966 R-MLP1-N93Q
func TestUpdate(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "example")
	other := create(t, s, "other")
	model, text, system := "new-model", "new-text", ""
	groups := []string{"files", "suite"}
	schema := json.RawMessage(" {\"x\":3} ")
	ch := store.Change{Model: &model, Prompt: &text, System: &system, Tools: &groups, Schema: &schema}
	v, changed, e := s.Update(ctx, p.ID, ch)
	if e != nil || !changed {
		t.Fatal(v, changed, e)
	}
	want := p
	want.Model = model
	want.Prompt = text
	want.System = system
	want.Tools = groups
	want.Schema = schema
	equal(t, v, want)
	read, e := s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, read, want)
	read, e = s.Find(ctx, other.Owner, other.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, read, other)
	for _, c := range []store.Change{ch, {}} {
		v, changed, e = s.Update(ctx, p.ID, c)
		if e != nil || changed {
			t.Fatal(v, changed, e)
		}
		equal(t, v, want)
	}
	empty := ""
	for _, c := range []store.Change{{Model: &empty}, {Prompt: &empty}} {
		for _, id := range []string{p.ID, "unknown"} {
			v, changed, e = s.Update(ctx, id, c)
			catalogError(t, e)
			equal(t, v, store.Prompt{})
			equal(t, changed, false)
		}
	}
	_, _, e = s.Update(ctx, "unknown", store.Change{})
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	var nilGroups []string
	v, changed, e = s.Update(ctx, p.ID, store.Change{Tools: &nilGroups})
	if e != nil || !changed || v.Tools == nil || len(v.Tools) != 0 {
		t.Fatal(v, changed, e)
	}
}

// R-V058-PCYD R-VH7U-25C3 R-WGKX-5HU8 R-WLGI-OKT0 R-WMOF-2CJP R-WNWB-G4AE R-WP47-TW13 R-WSRW-Z796 R-WTZT-CYZV
func TestSubscriptions(t *testing.T) {
	s, d, _ := setup(t)
	p := create(t, s, "example")
	q := create(t, s, "other")
	now := instant
	s = store.New(d, store.Config{Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 8))})
	a, e := s.Subscribe(ctx, p.ID, "cron.hourly.fired")
	if e != nil {
		t.Fatal(e)
	}
	first := a.Subscriptions[0]
	now = now.Add(time.Hour)
	a, e = s.Subscribe(ctx, p.ID, "cron.hourly.fired")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a.Subscriptions, []store.Subscription{first})
	a, e = s.Subscribe(ctx, p.ID, "cron.*.fired")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a.Subscriptions, []store.Subscription{{Event: "cron.*.fired", Created: normalized(now)}, first})
	if _, e = s.Subscribe(ctx, q.ID, "*.*"); e != nil {
		t.Fatal(e)
	}
	for _, event := range []string{"cron.hourly.fired", "cron.tick.fired"} {
		ps, e := s.Subscribers(ctx, event)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, ps, []store.Prompt{a})
	}
	for _, event := range []string{"cron.fired", "cron.a.b.fired", "cron.Hourly.fired", "repo.*"} {
		ps, e := s.Subscribers(ctx, event)
		if e != nil || ps == nil || len(ps) != 0 && (event != "cron.fired") {
			t.Fatal(event, ps, e)
		}
		if event == "cron.fired" && len(ps) != 1 {
			t.Fatal(ps)
		}
	}
	ps, e := s.Subscribers(ctx, "repo.pushed")
	if e != nil || len(ps) != 1 || ps[0].ID != q.ID {
		t.Fatal(ps, e)
	}
	before := a
	_, _, e = s.Update(ctx, p.ID, store.Change{})
	if e != nil {
		t.Fatal(e)
	}
	r := add(t, s, record(p, 1, store.StatusQueued))
	if _, e = s.StartRun(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: now}); e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteRun(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	a, e = s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a, before)
	for _, f := range []func(context.Context, string, string) (store.Prompt, error){s.Subscribe, s.Unsubscribe} {
		v, e := f(ctx, "missing", "bad")
		catalogError(t, e)
		equal(t, v, store.Prompt{})
		_, e = f(ctx, "missing", "repo.pushed")
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	_, e = s.Unsubscribe(ctx, p.ID, "cron.tick.fired")
	if !errors.Is(e, store.ErrNotSubscribed) {
		t.Fatal(e)
	}
	a, e = s.Unsubscribe(ctx, p.ID, "cron.hourly.fired")
	if e != nil || len(a.Subscriptions) != 1 {
		t.Fatal(a, e)
	}
	now = now.Add(time.Hour)
	a, e = s.Subscribe(ctx, p.ID, "cron.hourly.fired")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, a.Subscriptions[1].Created, normalized(now))
	if _, e = s.Unsubscribe(ctx, p.ID, "cron.hourly.fired"); e != nil {
		t.Fatal(e)
	}
	_, e = s.Unsubscribe(ctx, p.ID, "cron.hourly.fired")
	if !errors.Is(e, store.ErrNotSubscribed) {
		t.Fatal(e)
	}
}

// R-V1D5-34P2 R-V2L1-GWFR R-V7GM-ZZEJ R-VFZX-ODLE R-0O0I-LHFH R-VPR4-QJIY R-WXNI-IA7Y R-X03B-9TPC R-X3R0-F4XF R-X7EP-KG5I R-X8ML-Y7W7 R-X9UI-BZMW R-XB2E-PRDL R-XDI7-HAUZ R-WFD0-RQ3J
func TestRunTransitionsAndReads(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "example")
	a := add(t, s, record(p, 1, store.StatusQueued))
	b := add(t, s, record(p, 2, store.StatusRunning))
	equal(t, a.Started, normalized(instant))
	equal(t, a.Finished, time.Time{})
	rs, e := s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, rs, []store.Run{b, a})
	queued, e := s.Queued(ctx)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, queued, []store.Run{a})
	running, e := s.Running(ctx)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, running, []store.Run{b})
	updated, e := s.StartRun(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.Status = store.StatusRunning
	equal(t, updated, a)
	ending := store.Ending{Status: store.StatusKilled, Finished: instant.Add(time.Minute), StdoutBytes: 17, StderrBytes: 23, StderrTruncated: true, Usage: store.Usage{Calls: 1, ToolCalls: 2, InputTokens: 3, CachedTokens: 4, OutputTokens: 5, ReasoningTokens: 6, CostNanos: 1234567}}
	out, e := s.FinishRun(ctx, a.ID, ending)
	if e != nil {
		t.Fatal(e)
	}
	a.Status = ending.Status
	a.Finished = normalized(ending.Finished)
	a.StdoutBytes = 17
	a.StderrBytes = 23
	a.StderrTruncated = true
	a.Usage = ending.Usage
	equal(t, out, a)
	for _, f := range []func(context.Context, string) (store.Run, error){s.RunByID, func(c context.Context, id string) (store.Run, error) { return s.FindRun(c, p.Owner, id) }} {
		r, e := f(ctx, a.ID)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, r, a)
	}
	for _, owner := range []string{"bob", ""} {
		r, e := s.FindRun(ctx, owner, a.ID)
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
		equal(t, r, store.Run{})
	}
	rs, e = s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, rs, []store.Run{b, a})
	for _, f := range []func(context.Context, string) (store.Run, error){s.RunByID, func(c context.Context, id string) (store.Run, error) { return s.FindRun(c, p.Owner, id) }} {
		_, e := f(ctx, "missing")
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	for _, f := range []func(context.Context, string) ([]store.Run, error){s.Runs} {
		rs, e := f(ctx, "missing")
		if e != nil || rs == nil || len(rs) != 0 {
			t.Fatal(rs, e)
		}
	}
	p, e = s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, *p.Last, b)
	model := "changed-model"
	p, _, e = s.Update(ctx, p.ID, store.Change{Model: &model})
	if e != nil {
		t.Fatal(e)
	}
	equal(t, *p.Last, b)
	event := record(p, 3, store.StatusQueued)
	event.Trigger = store.TriggerEvent
	event.Event = "evt-one"
	event = add(t, s, event)
	event, e = s.FinishRun(ctx, event.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonQueueAbandoned, Finished: instant})
	if e != nil || event.Reason != store.ReasonQueueAbandoned || event.Usage != (store.Usage{}) || event.Model != model {
		t.Fatal(event, e)
	}
}

// R-VM3F-L8AV R-VNBB-Z01K R-WYVE-W1YN R-X1B7-NLG1 R-X4YW-SWO4 R-VIFQ-FX2S R-MO4U-ESL4
func TestInvalidTransitionsAndPrecedence(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "example")
	base := record(p, 1, store.StatusRunning)
	invalid := []store.Run{}
	for _, mutate := range []func(*store.Run){func(r *store.Run) { r.ID = "bad" }, func(r *store.Run) { r.Prompt = "" }, func(r *store.Run) { r.Model = "" }, func(r *store.Run) { r.User = "" }, func(r *store.Run) { r.Trigger = "bad" }, func(r *store.Run) { r.Event = "event" }, func(r *store.Run) { r.Started = time.Time{} }, func(r *store.Run) { r.StdoutBytes = -1 }, func(r *store.Run) { r.StderrBytes = -1 }, func(r *store.Run) { r.Usage.Calls = 1 }, func(r *store.Run) { r.ExitCode = 1 }, func(r *store.Run) { r.Finished = instant }, func(r *store.Run) { r.StdoutBytes = 1 }, func(r *store.Run) { r.StdoutTruncated = true }, func(r *store.Run) { r.Reason = store.ReasonStartFailed }, func(r *store.Run) { r.Status = store.StatusExited }, func(r *store.Run) { r.Status = store.StatusFailed; r.Finished = instant; r.Reason = "bad" }, func(r *store.Run) {
		r.Status = store.StatusFailed
		r.Finished = instant.Add(-time.Hour)
		r.Reason = store.ReasonStartFailed
	}} {
		r := base
		mutate(&r)
		invalid = append(invalid, r)
	}
	for _, r := range invalid {
		v, e := s.AddRun(ctx, r)
		catalogError(t, e)
		equal(t, v, store.Run{})
	}
	r := base
	r.Prompt = "missing"
	_, e := s.AddRun(ctx, r)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	running := add(t, s, base)
	_, e = s.AddRun(ctx, base)
	catalogError(t, e)
	queued := add(t, s, record(p, 2, store.StatusQueued))
	ending := store.Ending{Status: store.StatusExited, Finished: instant}
	for _, mutate := range []func(*store.Ending){func(e *store.Ending) { e.Status = "bad" }, func(e *store.Ending) { e.ExitCode = -1 }, func(e *store.Ending) { e.ExitCode = 256 }, func(e *store.Ending) { e.Reason = "bad" }, func(e *store.Ending) { e.Finished = time.Time{} }, func(e *store.Ending) { e.StdoutBytes = -1 }, func(e *store.Ending) { e.StderrBytes = -1 }, func(e *store.Ending) { e.Usage.CostNanos = -1 }, func(e *store.Ending) {
		e.Status = store.StatusFailed
		e.Reason = store.ReasonStartFailed
		e.Usage.Calls = 1
	}} {
		v := ending
		mutate(&v)
		for _, id := range []string{running.ID, "missing"} {
			r, e := s.FinishRun(ctx, id, v)
			catalogError(t, e)
			equal(t, r, store.Run{})
		}
	}
	for _, pair := range []struct {
		id string
		e  store.Ending
	}{{queued.ID, ending}, {running.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonStartFailed, Finished: instant}}, {queued.ID, store.Ending{Status: store.StatusKilled, Finished: instant, Usage: store.Usage{Calls: 1}}}, {running.ID, store.Ending{Status: store.StatusKilled, Finished: instant.Add(-time.Hour)}}} {
		_, e = s.FinishRun(ctx, pair.id, pair.e)
		catalogError(t, e)
	}
	_, e = s.FinishRun(ctx, "missing", ending)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	_, e = s.StartRun(ctx, "missing")
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	_, e = s.StartRun(ctx, running.ID)
	if !errors.Is(e, store.ErrEnded) {
		t.Fatal(e)
	}
	_, e = s.FinishRun(ctx, running.ID, ending)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.FinishRun(ctx, running.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonStartFailed, Finished: instant})
	if !errors.Is(e, store.ErrEnded) {
		t.Fatal(e)
	}
	_, e = s.StartRun(ctx, running.ID)
	if !errors.Is(e, store.ErrEnded) {
		t.Fatal(e)
	}
	bad := draft(p.Name)
	bad.Model = ""
	_, e = s.Create(ctx, bad)
	catalogError(t, e)
	empty := ""
	_, _, e = s.Update(ctx, "missing", store.Change{Prompt: &empty})
	catalogError(t, e)
}

// R-XEQ3-V2LO R-XFY0-8UCD R-XH5W-MM32 R-XIDT-0DTR R-XJLP-E5KG R-WV7P-QQQK R-WWFM-4IH9
func TestKeepingDeletionAndDelivery(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "example")
	q := create(t, s, "other")
	now := instant.Add(10 * 24 * time.Hour)
	runs := []store.Run{}
	for i := 1; i <= 5; i++ {
		r := record(p, i, store.StatusRunning)
		r.Started = instant.Add(time.Duration(i) * time.Hour)
		r = add(t, s, r)
		if i <= 3 {
			v, e := s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: r.Started})
			if e != nil {
				t.Fatal(e)
			}
			r = v
		}
		runs = append(runs, r)
	}
	rs, e := s.PastKeeping(ctx, now, 3, 2)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, rs, []store.Run{runs[2], runs[1], runs[0]})
	rs, e = s.PastKeeping(ctx, runs[2].Started.Add(3*24*time.Hour), 3, 2)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, rs, []store.Run{runs[1], runs[0]})
	rs, e = s.PastKeeping(ctx, now, math.MaxInt64, 1)
	if e != nil || rs == nil || len(rs) != 0 {
		t.Fatal(rs, e)
	}
	for _, pair := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		rs, e = s.PastKeeping(ctx, now, pair[0], pair[1])
		catalogError(t, e)
		if rs != nil {
			t.Fatal(rs)
		}
	}
	event := record(p, 6, store.StatusQueued)
	event.Trigger = store.TriggerEvent
	event.Event = "event-one"
	event = add(t, s, event)
	yes, e := s.Delivered(ctx, p.ID, event.Event)
	if !yes || e != nil {
		t.Fatal(yes, e)
	}
	duplicate := event
	duplicate.ID = "prr_0000000000000007"
	_, e = s.AddRun(ctx, duplicate)
	if !errors.Is(e, store.ErrDelivered) {
		t.Fatal(e)
	}
	duplicate.Prompt = "missing"
	_, e = s.AddRun(ctx, duplicate)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	duplicate.Prompt = q.ID
	add(t, s, duplicate)
	if e = s.DeleteRun(ctx, event.ID); e != nil {
		t.Fatal(e)
	}
	yes, e = s.Delivered(ctx, p.ID, event.Event)
	if !yes || e != nil {
		t.Fatal(yes, e)
	}
	duplicate.Prompt = p.ID
	duplicate.ID = "prr_0000000000000008"
	_, e = s.AddRun(ctx, duplicate)
	if !errors.Is(e, store.ErrDelivered) {
		t.Fatal(e)
	}
	_, e = s.RunByID(ctx, event.ID)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if e = s.DeleteRun(ctx, event.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = s.Subscribe(ctx, p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	_, e = s.Find(ctx, p.Owner, p.Name)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	for _, r := range runs {
		_, e = s.RunByID(ctx, r.ID)
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal(e)
		}
	}
	yes, e = s.Delivered(ctx, p.ID, event.Event)
	if e != nil || yes {
		t.Fatal(yes, e)
	}
	yes, e = s.Taken(ctx, p.Name)
	if e != nil || yes {
		t.Fatal(yes, e)
	}
	yes, e = s.Delivered(ctx, q.ID, event.Event)
	if e != nil || !yes {
		t.Fatal(yes, e)
	}
	if e = s.Delete(ctx, p.ID); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	create(t, s, p.Name)
}

// R-W1Y4-K8XW R-XM1I-5P1U
func TestReopenAllContent(t *testing.T) {
	s, d, path := setup(t)
	p := create(t, s, "example")
	q := create(t, s, "removed")
	if _, e := s.Subscribe(ctx, p.ID, "repo.*"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Subscribe(ctx, p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Unsubscribe(ctx, p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	system := "replacement"
	if _, _, e := s.Update(ctx, p.ID, store.Change{System: &system}); e != nil {
		t.Fatal(e)
	}
	a := record(p, 1, store.StatusQueued)
	a.Trigger = store.TriggerEvent
	a.Event = "event-a"
	add(t, s, a)
	if _, e := s.StartRun(ctx, a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.FinishRun(ctx, a.ID, store.Ending{Status: store.StatusExited, ExitCode: 4, Finished: instant, StdoutBytes: 3, StderrBytes: 4, StdoutTruncated: true, StderrTruncated: true, Usage: store.Usage{Calls: 2, ToolCalls: 3, InputTokens: 4, CachedTokens: 5, OutputTokens: 6, ReasoningTokens: 7, CostNanos: 8}}); e != nil {
		t.Fatal(e)
	}
	b := record(p, 2, store.StatusQueued)
	b.Trigger = store.TriggerEvent
	b.Event = "event-b"
	add(t, s, b)
	if e := s.DeleteRun(ctx, b.ID); e != nil {
		t.Fatal(e)
	}
	add(t, s, record(q, 3, store.StatusRunning))
	if e := s.Delete(ctx, q.ID); e != nil {
		t.Fatal(e)
	}
	before := content(t, s)
	rs, e := s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, bytes.NewReader([]byte{9, 8, 7, 6, 5, 4, 3, 2}))
	equal(t, content(t, s), before)
	after, e := s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, after, rs)
	for _, event := range []string{"event-a", "event-b"} {
		yes, e := s.Delivered(ctx, p.ID, event)
		if e != nil || !yes {
			t.Fatal(event, yes, e)
		}
	}
}

// R-WAHF-8N4R R-MMWY-10UF R-9IVQ-UQQ7 R-WRK0-LFIH R-X2J4-1D6Q R-X66T-6OET R-XKTL-RXB5 R-XN9E-JGSJ R-XQX3-OS0M
func TestConcurrentTransactions(t *testing.T) {
	s, _, _ := setup(t)
	parallel := func(f func(int)) {
		t.Helper()
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Go(func() { f(i) })
		}
		wg.Wait()
	}
	var mu sync.Mutex
	success := 0
	parallel(func(_ int) {
		_, e := s.Create(ctx, draft("same"))
		if e == nil {
			mu.Lock()
			success++
			mu.Unlock()
		} else if !errors.Is(e, store.ErrNameTaken) {
			t.Error(e)
		}
	})
	equal(t, success, 1)
	p, e := s.Find(ctx, "alice", "same")
	if e != nil {
		t.Fatal(e)
	}
	model := "updated"
	groups := []string{"files"}
	var wg sync.WaitGroup
	wg.Go(func() {
		if _, _, e := s.Update(ctx, p.ID, store.Change{Model: &model}); e != nil {
			t.Error(e)
		}
	})
	wg.Go(func() {
		if _, _, e := s.Update(ctx, p.ID, store.Change{Tools: &groups}); e != nil {
			t.Error(e)
		}
	})
	wg.Wait()
	p, e = s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, p.Model, model)
	equal(t, p.Tools, groups)
	text := "updated text"
	success = 0
	parallel(func(_ int) {
		_, changed, e := s.Update(ctx, p.ID, store.Change{Prompt: &text})
		if e != nil {
			t.Error(e)
		}
		if changed {
			mu.Lock()
			success++
			mu.Unlock()
		}
	})
	equal(t, success, 1)
	parallel(func(_ int) {
		if _, e := s.Subscribe(ctx, p.ID, "repo.pushed"); e != nil {
			t.Error(e)
		}
	})
	p, e = s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, len(p.Subscriptions), 1)
	equal(t, p.Prompt, text)
	r := add(t, s, record(p, 1, store.StatusQueued))
	success = 0
	parallel(func(_ int) {
		_, e := s.StartRun(ctx, r.ID)
		if e == nil {
			mu.Lock()
			success++
			mu.Unlock()
		} else if !errors.Is(e, store.ErrEnded) {
			t.Error(e)
		}
	})
	equal(t, success, 1)
	success = 0
	var winning store.Run
	parallel(func(i int) {
		ended, e := s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, ExitCode: i, Finished: instant})
		if e == nil {
			expected := r
			expected.Status = store.StatusExited
			expected.ExitCode = i
			expected.Finished = normalized(instant)
			if !reflect.DeepEqual(ended, expected) {
				t.Errorf("winning record %#v want %#v", ended, expected)
			}
			mu.Lock()
			winning = expected
			success++
			mu.Unlock()
		} else if !errors.Is(e, store.ErrEnded) {
			t.Error(e)
		}
	})
	equal(t, success, 1)
	persisted, e := s.RunByID(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, persisted, winning)
	success = 0
	var eventWinner store.Run
	parallel(func(i int) {
		r := record(p, i+2, store.StatusQueued)
		r.Trigger = store.TriggerEvent
		r.Event = "shared-event"
		added, e := s.AddRun(ctx, r)
		if e == nil {
			r.Started = normalized(r.Started)
			if !reflect.DeepEqual(added, r) {
				t.Errorf("event run %#v want %#v", added, r)
			}
			mu.Lock()
			eventWinner = r
			success++
			mu.Unlock()
		} else if !errors.Is(e, store.ErrDelivered) {
			t.Error(e)
		}
	})
	equal(t, success, 1)
	all, e := s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	matched := []store.Run{}
	for _, v := range all {
		if v.Event == "shared-event" {
			matched = append(matched, v)
		}
	}
	equal(t, matched, []store.Run{eventWinner})
	q := create(t, s, "delete-race")
	wg.Go(func() {
		_, e := s.AddRun(ctx, record(q, 20, store.StatusRunning))
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			t.Error(e)
		}
	})
	wg.Go(func() {
		if e := s.Delete(ctx, q.ID); e != nil {
			t.Error(e)
		}
	})
	wg.Wait()
	rs, e := s.Runs(ctx, q.ID)
	if e != nil || len(rs) != 0 {
		t.Fatal(rs, e)
	}
}

// R-XOHA-X8J8 R-XPP7-B09X
func TestEveryMethodFailureAndCancellation(t *testing.T) {
	s, d, _ := setup(t)
	p := create(t, s, "example")
	r := add(t, s, record(p, 1, store.StatusRunning))
	methods := []func(context.Context) error{
		func(c context.Context) error { _, e := s.Find(c, p.Owner, p.Name); return e }, func(c context.Context) error { _, e := s.List(c, p.Owner); return e }, func(c context.Context) error { _, e := s.Taken(c, p.Name); return e }, func(c context.Context) error { _, e := s.Create(c, store.Draft{}); return e }, func(c context.Context) error { _, _, e := s.Update(c, p.ID, store.Change{}); return e }, func(c context.Context) error { return s.Delete(c, p.ID) }, func(c context.Context) error { _, e := s.Subscribe(c, p.ID, "bad"); return e }, func(c context.Context) error { _, e := s.Unsubscribe(c, p.ID, "bad"); return e }, func(c context.Context) error { _, e := s.Subscribers(c, "repo.pushed"); return e }, func(c context.Context) error { _, e := s.Delivered(c, p.ID, "event"); return e }, func(c context.Context) error { _, e := s.AddRun(c, store.Run{}); return e }, func(c context.Context) error { _, e := s.StartRun(c, r.ID); return e }, func(c context.Context) error { _, e := s.FinishRun(c, r.ID, store.Ending{}); return e }, func(c context.Context) error { _, e := s.FindRun(c, p.Owner, r.ID); return e }, func(c context.Context) error { _, e := s.RunByID(c, r.ID); return e }, func(c context.Context) error { _, e := s.Runs(c, p.ID); return e }, func(c context.Context) error { _, e := s.Running(c); return e }, func(c context.Context) error { _, e := s.Queued(c); return e }, func(c context.Context) error { _, e := s.PastKeeping(c, instant, 0, 0); return e }, func(c context.Context) error { return s.DeleteRun(c, r.ID) },
	}
	before := content(t, s)
	beforeRuns, e := s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	for _, f := range methods {
		e = f(c)
		catalogError(t, e)
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	}
	d.SetFailing(true)
	for _, f := range methods {
		catalogError(t, f(ctx))
		if e := f(c); !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	}
	d.SetFailing(false)
	equal(t, content(t, s), before)
	after, e := s.Runs(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, after, beforeRuns)
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	for _, f := range methods {
		catalogError(t, f(ctx))
	}
}

// R-VUMQ-9MHQ R-VVUM-NE8F R-VX2J-15Z4 R-VYAF-EXPT R-VZIB-SPGI
func TestSchema(t *testing.T) {
	_, d, _ := setup(t)
	e := d.Read(ctx, func(tx *sql.Tx) error {
		rows, e := tx.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if e != nil {
			return e
		}
		tables := []string{}
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				return e
			}
			tables = append(tables, name)
		}
		e = errors.Join(rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		equal(t, tables, []string{"event_runs", "prompts", "runs", "schema_migrations", "subscriptions"})
		type col struct {
			name, typ string
			null, pk  int
		}
		shapes := map[string][]col{"prompts": {{"id", "TEXT", 1, 1}, {"name", "TEXT", 1, 0}, {"owner_id", "TEXT", 1, 0}, {"owner_email", "TEXT", 1, 0}, {"model", "TEXT", 1, 0}, {"prompt", "TEXT", 1, 0}, {"system", "TEXT", 1, 0}, {"tools", "TEXT", 1, 0}, {"schema", "TEXT", 0, 0}, {"created", "TEXT", 1, 0}}, "subscriptions": {{"prompt", "TEXT", 1, 1}, {"event", "TEXT", 1, 2}, {"created", "TEXT", 1, 0}}, "event_runs": {{"prompt", "TEXT", 1, 1}, {"event", "TEXT", 1, 2}}, "runs": {{"id", "TEXT", 1, 1}, {"prompt", "TEXT", 1, 0}, {"model", "TEXT", 1, 0}, {"user_id", "TEXT", 1, 0}, {"request_id", "TEXT", 1, 0}, {"trigger_kind", "TEXT", 1, 0}, {"event", "TEXT", 1, 0}, {"status", "TEXT", 1, 0}, {"exit_code", "INTEGER", 1, 0}, {"started", "TEXT", 1, 0}, {"finished", "TEXT", 0, 0}, {"stdout_bytes", "INTEGER", 1, 0}, {"stderr_bytes", "INTEGER", 1, 0}, {"stdout_truncated", "INTEGER", 1, 0}, {"stderr_truncated", "INTEGER", 1, 0}, {"reason", "TEXT", 1, 0}, {"calls", "INTEGER", 1, 0}, {"tool_calls", "INTEGER", 1, 0}, {"input_tokens", "INTEGER", 1, 0}, {"cached_tokens", "INTEGER", 1, 0}, {"output_tokens", "INTEGER", 1, 0}, {"reasoning_tokens", "INTEGER", 1, 0}, {"cost_nanos", "INTEGER", 1, 0}}}
		for table, want := range shapes {
			var count int
			if e = tx.QueryRow("SELECT count(*) FROM " + table).Scan(&count); e != nil {
				return e
			}
			equal(t, count, 0)
			rows, e = tx.Query("PRAGMA table_info(" + table + ")")
			if e != nil {
				return e
			}
			got := []col{}
			for rows.Next() {
				var c col
				var cid int
				var defaultValue any
				if e = rows.Scan(&cid, &c.name, &c.typ, &c.null, &defaultValue, &c.pk); e != nil {
					return e
				}
				equal(t, cid, len(got))
				got = append(got, c)
			}
			e = errors.Join(rows.Err(), rows.Close())
			if e != nil {
				return e
			}
			equal(t, got, want)
		}
		rows, e = tx.Query("PRAGMA index_list(prompts)")
		if e != nil {
			return e
		}
		indexes := []string{}
		for rows.Next() {
			var seq, unique, partial int
			var name, origin string
			if e = rows.Scan(&seq, &name, &unique, &origin, &partial); e != nil {
				return e
			}
			if unique == 1 {
				indexes = append(indexes, name)
			}
		}
		e = errors.Join(rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		found := false
		for _, index := range indexes {
			rows, e = tx.Query("PRAGMA index_info('" + index + "')")
			if e != nil {
				return e
			}
			cols := []string{}
			for rows.Next() {
				var seq, cid int
				var name string
				if e = rows.Scan(&seq, &cid, &name); e != nil {
					return e
				}
				cols = append(cols, name)
			}
			e = errors.Join(rows.Err(), rows.Close())
			if e != nil {
				return e
			}
			found = found || slices.Equal(cols, []string{"name"})
		}
		if !found {
			t.Fatal("missing unique name index")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

// R-VM3F-L8AV R-WXNI-IA7Y
func TestValidFailedRunRecord(t *testing.T) {
	s, d, path := setup(t)
	p := create(t, s, "failed-record")
	r := record(p, 31, store.StatusFailed)
	r.Reason = store.ReasonStartFailed
	r.Finished = instant.Add(time.Minute)
	r.StdoutBytes = 5
	r.StderrBytes = 7
	r.StdoutTruncated = true
	r.StderrTruncated = true
	expected := r
	expected.Started = normalized(r.Started)
	expected.Finished = normalized(r.Finished)
	got, e := s.AddRun(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, expected)
	got, e = s.RunByID(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, expected)
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, bytes.NewReader(make([]byte, 8)))
	got, e = s.RunByID(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, expected)
}

// R-W5LT-PK5Z
func TestCreateUsesFirstUnusedCandidate(t *testing.T) {
	s, d, _ := setup(t)
	create(t, s, "first")
	random := bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 0xab})
	s = store.New(d, store.Config{Now: clock, Rand: random})
	p, e := s.Create(ctx, draft("after-collision"))
	if e != nil {
		t.Fatal(e)
	}
	equal(t, p.ID, "prm_0102030405060708")
	equal(t, random.Len(), 1)
	got, e := s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, p)
}

// R-XB2E-PRDL R-XDI7-HAUZ R-WSRW-Z796
func TestCatalogGlobalListsSortIDs(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "alice-list")
	other := draft("bob-list")
	other.Owner = "bob"
	q, e := s.Create(ctx, other)
	if e != nil {
		t.Fatal(e)
	}
	a := add(t, s, record(p, 8, store.StatusRunning))
	b := add(t, s, record(q, 3, store.StatusRunning))
	c := add(t, s, record(p, 9, store.StatusQueued))
	d := add(t, s, record(q, 2, store.StatusQueued))
	got, e := s.Running(ctx)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, []store.Run{b, a})
	got, e = s.Queued(ctx)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, []store.Run{d, c})
	if _, e = s.Subscribe(ctx, q.ID, "repo.*"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Subscribe(ctx, p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Subscribe(ctx, p.ID, "repo.*"); e != nil {
		t.Fatal(e)
	}
	p, e = s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	q, e = s.Find(ctx, q.Owner, q.Name)
	if e != nil {
		t.Fatal(e)
	}
	ps, e := s.Subscribers(ctx, "repo.pushed")
	if e != nil {
		t.Fatal(e)
	}
	equal(t, ps, []store.Prompt{p, q})
}

// R-XEQ3-V2LO
func TestRetentionAcrossPrompts(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "first-retention")
	q := create(t, s, "second-retention")
	expected := []store.Run{}
	for _, prompt := range []store.Prompt{q, p} {
		kept := record(prompt, 10+len(expected), store.StatusRunning)
		kept.Started = instant.Add(time.Hour)
		add(t, s, kept)
		older := record(prompt, 20+len(expected), store.StatusFailed)
		older.Reason = store.ReasonStartFailed
		older.Finished = instant
		older = add(t, s, older)
		expected = append(expected, older)
	}
	got, e := s.PastKeeping(ctx, instant.Add(4*24*time.Hour), 3, 1)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, []store.Run{expected[1], expected[0]})
}

// R-YAWY-LH7K: every byte obeys the name alphabet and leading-byte rule.
func TestNameByteAlphabet(t *testing.T) {
	for n := 0; n < 256; n++ {
		b := byte(n)
		allowed := b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-'
		for _, tc := range []struct {
			name string
			want bool
		}{
			{string([]byte{'a', b}), allowed},
			{string([]byte{b, 'a'}), allowed && b != '-'},
			{string([]byte{b}), allowed && b != '-'},
		} {
			if got := store.ValidName(tc.name); got != tc.want {
				t.Fatalf("name %q: got %t want %t", tc.name, got, tc.want)
			}
		}
	}
}

// R-MJ98-VPMC R-MKH5-9HD1 R-0P8E-Z966 R-MLP1-N93Q
func TestUpdateSchemaClearPersistsAndPreservesCatalog(t *testing.T) {
	s, d, path := setup(t)
	p := create(t, s, "schema-clear")
	other := create(t, s, "other-schema")
	var e error
	p, e = s.Subscribe(ctx, p.ID, "repo.pushed")
	if e != nil {
		t.Fatal(e)
	}
	r := record(p, 91, store.StatusRunning)
	r.Trigger, r.Event = store.TriggerEvent, "delivered-event"
	r = add(t, s, r)
	p, e = s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	check := func() {
		t.Helper()
		read, e := s.Find(ctx, p.Owner, p.Name)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, read, p)
		equal(t, content(t, s), []store.Prompt{other, p})
		got, e := s.RunByID(ctx, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, got, r)
		yes, e := s.Delivered(ctx, p.ID, r.Event)
		if e != nil || !yes {
			t.Fatal(yes, e)
		}
	}
	for _, raw := range []json.RawMessage{nil, {}} {
		schema := json.RawMessage(` {"arbitrary":true} `)
		got, changed, e := s.Update(ctx, p.ID, store.Change{Schema: &schema})
		if e != nil || !changed {
			t.Fatal(got, changed, e)
		}
		p.Schema = schema
		equal(t, got, p)
		got, changed, e = s.Update(ctx, p.ID, store.Change{Schema: &raw})
		if e != nil || !changed || got.Schema != nil {
			t.Fatal(got, changed, e)
		}
		p.Schema = nil
		equal(t, got, p)
		check()
		for _, ch := range []store.Change{{Schema: &raw}, {}} {
			got, changed, e = s.Update(ctx, p.ID, ch)
			if e != nil || changed {
				t.Fatal(got, changed, e)
			}
			equal(t, got, p)
		}
		got, changed, e = s.Update(ctx, "unknown", store.Change{Schema: &raw})
		if !errors.Is(e, store.ErrNotFound) || changed {
			t.Fatal(got, changed, e)
		}
		equal(t, got, store.Prompt{})
		check()
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, bytes.NewReader(make([]byte, 8)))
	check()
}
