package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

var childInstant = time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == agent.Command {
		os.Exit(agent.Run(context.Background(), agent.Process{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, LookupEnv: os.LookupEnv, Now: func() time.Time { return childInstant }}))
	}
	os.Exit(m.Run())
}

type runFixture struct {
	*fixture
	cfg     runs.Config
	seen    chan []byte
	release chan struct{}
	timers  chan chan time.Time
	countMu sync.Mutex
	count   int
}

func runSetup(t *testing.T, change func(*runs.Config)) *runFixture {
	t.Helper()
	f := &runFixture{fixture: setup(t), seen: make(chan []byte, 64), release: make(chan struct{}), timers: make(chan chan time.Time, 64)}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		f.countMu.Lock()
		f.count++
		f.countMu.Unlock()
		f.seen <- b
		select {
		case <-f.release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range []struct {
			event string
			v     any
		}{
			{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]int{"input_tokens": 3}}}},
			{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "answer from provider"}}},
			{"message_delta", map[string]any{"type": "message_delta", "usage": map[string]int{"output_tokens": 2}, "delta": map[string]string{"stop_reason": "end_turn"}}},
			{"message_stop", map[string]string{"type": "message_stop"}},
		} {
			data, _ := json.Marshal(frame.v)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame.event, data)
		}
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(provider.CloseClientConnections)
	f.cfg = runs.Config{Store: f.st, Writer: f.w, Runs: filepath.Join(f.dir, "runs"), Path: "", BaseURL: provider.URL, Keys: map[agentkit.Host]string{agentkit.HostAnthropic: "fixture-key"}, Cgroup: filepath.Join(f.dir, "groups"), PromptSeconds: 60, OutputMaxBytes: 1024, MaxToolCalls: 2, KeepDays: 30, KeepCount: 100, RunMemoryMaxBytes: 1024, RunPidsMax: 8, MaxActive: 1, MaxQueued: 10, Now: func() time.Time { f.clockMu.Lock(); defer f.clockMu.Unlock(); return f.now }, ScriptAfter: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); f.timers <- ch; return ch }, Rand: &sequence{}}
	if err := os.Mkdir(f.cfg.Cgroup, 0700); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&f.cfg)
	}
	f.core = runs.New(f.cfg)
	f.srv = mcp.NewServer(mcp.ServerConfig{Name: "prompts", Version: "fixture", Telemetry: f.w})
	tools.Register(f.srv, tools.Config{Store: f.st, Runs: f.core, Telemetry: f.w})
	server := httptest.NewServer(telemetry.Middleware(f.w, identity.Require(f.srv)))
	t.Cleanup(server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL})
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); f.core.Drain(ctx) })
	return f
}
func (f *runFixture) seedRun(t *testing.T, name string) tools.Prompt {
	t.Helper()
	chosen := ""
	for _, entry := range agentkit.Catalog() {
		o, err := agent.Offering(entry.Model)
		if err == nil && o.Host == agentkit.HostAnthropic {
			chosen = entry.Model
			break
		}
	}
	if chosen == "" {
		t.Fatal("no Anthropic offering")
	}
	b, _ := json.Marshal(map[string]any{"name": name, "model": chosen, "prompt": "test prompt"})
	return decode[tools.Prompt](t, f.call(t, "create", string(b)))
}
func (f *runFixture) start(t *testing.T, name, input string) tools.Started {
	t.Helper()
	a := `{"name":"` + name + `"`
	if input != "" {
		a += `,"input":` + input
	}
	return decode[tools.Started](t, f.call(t, "run", a+`}`))
}
func (f *runFixture) entered(t *testing.T) {
	t.Helper()
	select {
	case <-f.seen:
	case <-time.After(10 * time.Second):
		t.Fatal("child provider deadline")
	}
}
func (f *runFixture) record(t *testing.T, id string) store.Run {
	t.Helper()
	r, err := f.st.RunByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *runFixture) ended(t *testing.T, id string) store.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		r := f.record(t, id)
		if r.Status != store.StatusRunning && r.Status != store.StatusQueued {
			return r
		}
		select {
		case <-ctx.Done():
			t.Fatal("child completion deadline")
		default:
			runtime.Gosched()
		}
	}
}
func runArg(id string) string       { b, _ := json.Marshal(tools.ResultArgs{Run: id}); return string(b) }
func (f *runFixture) requests() int { f.countMu.Lock(); defer f.countMu.Unlock(); return f.count }
func noUsage(t *testing.T, r tools.RunResult) {
	t.Helper()
	if r.Finished != nil || r.ExitCode != nil || r.Calls != nil || r.ToolCalls != nil || r.InputTokens != nil || r.CachedTokens != nil || r.OutputTokens != nil || r.ReasoningTokens != nil || r.CostNanos != nil {
		t.Fatalf("unexpected terminal fields %#v", r)
	}
}
func usage(t *testing.T, r tools.RunEntry) {
	t.Helper()
	if r.Calls == nil || r.ToolCalls == nil || r.InputTokens == nil || r.CachedTokens == nil || r.OutputTokens == nil || r.ReasoningTokens == nil || r.CostNanos == nil {
		t.Fatalf("missing usage %#v", r)
	}
}

// R-9M4N-E8ZA
func TestDeletedType(t *testing.T) {
	v := tools.Deleted{Deleted: true, ID: "prompt-value"}
	b, err := json.Marshal(v)
	if err != nil || string(b) != `{"deleted":true,"id":"prompt-value"}` {
		t.Fatalf("%s %v", b, err)
	}
	f := setup(t)
	infos, err := f.client.ListTools(context.Background(), f.caller)
	if err != nil {
		t.Fatal(err)
	}
	var info mcp.ToolInfo
	for _, v := range infos {
		if v.Name == "delete" {
			info = v
		}
	}
	var schema map[string]any
	if err = json.Unmarshal(info.OutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema["required"], []any{"deleted", "id"}) {
		t.Fatal(schema)
	}
}

// R-BVTX-0VNT R-BX1T-ENEI R-C1XE-XQDA R-C35B-BI3Z R-C80W-UL2R R-C0PI-JYML R-BZHM-66VW
func TestRunResultsAndQueuedCancel(t *testing.T) {
	f := runSetup(t, nil)
	p := f.seedRun(t, "daily")
	if got := string(content(t, f.call(t, "runs", `{"name":"daily"}`))); got != `{"runs":[]}` {
		t.Fatal(got)
	}
	before, err := f.st.Find(context.Background(), f.caller.UserID, p.Name)
	if err != nil {
		t.Fatal(err)
	}
	a := f.rawStart(t, p.Name, `{ "b" : [ ], "a":1 }`)
	if a.Status != store.StatusRunning || a.Reason != nil {
		t.Fatal(a)
	}
	f.entered(t)
	r := f.record(t, a.ID)
	all, err := f.st.Runs(context.Background(), p.ID)
	if err != nil || len(all) != 1 {
		t.Fatal(all, err)
	}
	if r.Prompt != p.ID || r.Model != p.Model || r.User != f.caller.UserID || r.RequestID != fmt.Sprintf("call-%d", f.n) || r.Trigger != store.TriggerManual || r.Event != "" {
		t.Fatal(r)
	}
	after, err := f.st.Find(context.Background(), f.caller.UserID, p.Name)
	before.Last = after.Last
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("prompt mutated %#v %#v %v", before, after, err)
	}
	for _, input := range []string{`{ "b" : [ ], "a":1 }`, `{"since":"2026-10-04","channels":["sales","support"],"dry_run":false}`, ""} {
		id := a.ID
		if input != `{ "b" : [ ], "a":1 }` {
			id = f.start(t, p.Name, input).ID
		}
		data, e := os.ReadFile(filepath.Join(f.core.Folder(f.record(t, id)), runs.InputFile))
		want := input
		if want == "" {
			want = "{}"
		}
		if e != nil || string(data) != want {
			t.Fatalf("input %q %v", data, e)
		}
		if id != a.ID {
			content(t, f.call(t, "cancel", runArg(id)))
		}
	}
	live := decode[tools.RunResult](t, f.call(t, "result", runArg(a.ID)))
	noUsage(t, live)
	if live.Status != store.StatusRunning || live.StdoutBytes != 0 || live.Stdout == nil || *live.Stdout != "" {
		t.Fatal(live)
	}
	queued := f.start(t, p.Name, "")
	if queued.Status != store.StatusQueued {
		t.Fatal(queued)
	}
	qr := f.record(t, queued.ID)
	folder := f.core.Folder(qr)
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	q := decode[tools.RunResult](t, f.call(t, "result", runArg(queued.ID)))
	noUsage(t, q)
	if q.Status != store.StatusQueued || q.StdoutBytes != 0 || q.StderrBytes != 0 || q.Stdout == nil || *q.Stdout != "" || q.Stderr == nil || *q.Stderr != "" || q.TranscriptBytes == nil || *q.TranscriptBytes != 0 || q.Files == nil || len(*q.Files) != 0 {
		t.Fatal(q)
	}
	canceled := decode[tools.RunEntry](t, f.call(t, "cancel", runArg(queued.ID)))
	stored := f.record(t, queued.ID)
	entryMatches(t, canceled, stored)
	if canceled.ID != stored.ID || canceled.Status != stored.Status || stored.Status != store.StatusKilled || canceled.Finished == nil || canceled.ExitCode != nil {
		t.Fatal(canceled, stored)
	}
	afterEntries, err := os.ReadDir(folder)
	if err != nil || !reflect.DeepEqual(entries, afterEntries) {
		t.Fatalf("queued folder changed %v", err)
	}
	if f.record(t, a.ID).Status != store.StatusRunning {
		t.Fatal("cancel altered active")
	}
	close(f.release)
	finished := f.ended(t, a.ID)
	if finished.Status != store.StatusExited || finished.ExitCode != 0 {
		t.Fatal(finished)
	}
	if f.requests() != 1 {
		t.Fatal("queued child started")
	}
	final := decode[tools.RunResult](t, f.call(t, "result", runArg(a.ID)))
	if final.Stdout == nil || *final.Stdout != "answer from provider" {
		t.Fatal(final)
	}
	killed := decode[tools.RunResult](t, f.call(t, "result", runArg(queued.ID)))
	if killed.Status != store.StatusKilled || *killed.Stdout != "" || *killed.Stderr != "" || killed.StdoutBytes != 0 || killed.StderrBytes != 0 || *killed.TranscriptBytes != 0 || len(*killed.Files) != 0 {
		t.Fatal(killed)
	}
	if err = os.RemoveAll(f.core.Folder(finished)); err != nil {
		t.Fatal(err)
	}
	listed := decode[tools.RunList](t, f.call(t, "runs", `{"name":"daily"}`))
	records, err := f.st.Runs(context.Background(), p.ID)
	if err != nil || len(records) != len(listed.Runs) {
		t.Fatal(records, listed, err)
	}
	for i, entry := range listed.Runs {
		if entry.ID != records[i].ID || entry.Status != records[i].Status {
			t.Fatal(entry, records[i])
		}
		usage(t, entry)
		entryMatches(t, entry, records[i])
	}
	for _, id := range []string{p.ID, p.Name, "prr_ffffffffffffffff"} {
		refused(t, f.call(t, "result", runArg(id)), fmt.Sprintf(tools.MissingRun, id))
	}
	f.caller.UserID = "another"
	refused(t, f.call(t, "result", runArg(a.ID)), fmt.Sprintf(tools.MissingRun, a.ID))
	refused(t, f.call(t, "runs", `{"name":"daily"}`), fmt.Sprintf(tools.MissingPrompt, p.Name))
	f.caller.UserID = "owner"
	if err = f.st.DeleteRun(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	refused(t, f.call(t, "result", runArg(a.ID)), fmt.Sprintf(tools.MissingRun, a.ID))
}

// R-BKUT-KXZK R-BM2P-YPQ9 R-BNAM-CHGY
func TestDeleteRunningAndQueued(t *testing.T) {
	f := runSetup(t, func(c *runs.Config) { c.MaxActive = 2 })
	p := f.seedRun(t, "daily")
	other := f.seedRun(t, "other")
	never := f.seedRun(t, "never")
	a := f.start(t, p.Name, "")
	f.entered(t)
	b := f.start(t, other.Name, "")
	f.entered(t)
	q := f.start(t, p.Name, "")
	if q.Status != store.StatusQueued {
		t.Fatal(q)
	}
	pid := f.pid(t, a.ID)
	otherBefore := f.record(t, b.ID)
	otherFolder := f.core.Folder(otherBefore)
	otherFiles := folderSnapshot(t, otherFolder)
	otherPID := f.pid(t, b.ID)
	otherPrompt, err := f.st.Find(context.Background(), "owner", other.Name)
	if err != nil {
		t.Fatal(err)
	}
	neverPrompt, err := f.st.Find(context.Background(), "owner", never.Name)
	if err != nil {
		t.Fatal(err)
	}
	deleted := decode[tools.Deleted](t, f.call(t, "delete", `{"name":"daily"}`))
	f.gonePID(t, pid)
	if !deleted.Deleted || deleted.ID != p.ID {
		t.Fatal(deleted)
	}
	for _, id := range []string{a.ID, q.ID} {
		_, err = f.st.RunByID(context.Background(), id)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		refused(t, f.call(t, "result", runArg(id)), fmt.Sprintf(tools.MissingRun, id))
		refused(t, f.call(t, "cancel", runArg(id)), fmt.Sprintf(tools.MissingRun, id))
	}
	_, err = f.st.Find(context.Background(), "owner", p.Name)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	taken, err := f.st.Taken(context.Background(), p.Name)
	if err != nil || taken {
		t.Fatal(taken, err)
	}
	_, err = os.Stat(filepath.Join(f.cfg.Runs, p.ID))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.record(t, b.ID), otherBefore) {
		t.Fatal("other run changed")
	}
	if !reflect.DeepEqual(otherFiles, folderSnapshot(t, otherFolder)) {
		t.Fatal("other folder contents changed")
	}
	alivePID(t, otherPID)
	for _, before := range []store.Prompt{otherPrompt, neverPrompt} {
		after, err := f.st.Find(context.Background(), before.Owner, before.Name)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("other prompt changed", before, after, err)
		}
	}
	refused(t, f.call(t, "delete", `{"name":"daily"}`), fmt.Sprintf(tools.MissingPrompt, p.Name))
	f.caller.UserID = "another"
	refused(t, f.call(t, "delete", `{"name":"other"}`), fmt.Sprintf(tools.MissingPrompt, other.Name))
	f.caller.UserID = "owner"
	d := decode[tools.Deleted](t, f.call(t, "delete", `{"name":"never"}`))
	if !d.Deleted || d.ID != never.ID {
		t.Fatal(d)
	}
	_, err = f.st.Find(context.Background(), "owner", never.Name)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal("never-run prompt remains", err)
	}
	taken, err = f.st.Taken(context.Background(), never.Name)
	if err != nil || taken {
		t.Fatal(taken, err)
	}
	_, err = os.Stat(filepath.Join(f.cfg.Runs, never.ID))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("never-run folder remains", err)
	}
	if !reflect.DeepEqual(f.record(t, b.ID), otherBefore) {
		t.Fatal("second delete changed other run")
	}
	afterOther, err := f.st.Find(context.Background(), otherPrompt.Owner, otherPrompt.Name)
	if err != nil || !reflect.DeepEqual(otherPrompt, afterOther) {
		t.Fatal("second delete changed other prompt", err)
	}
	if !reflect.DeepEqual(otherFiles, folderSnapshot(t, otherFolder)) {
		t.Fatal("second delete changed other files")
	}
	alivePID(t, otherPID)
	for _, id := range []string{a.ID, q.ID} {
		_, err = f.st.RunByID(context.Background(), id)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatal("deleted run returned", err)
		}
	}
	close(f.release)
	f.ended(t, b.ID)
	if f.requests() != 2 {
		t.Fatal("deleted queued child started")
	}
}

// R-C5L4-31LD R-92G2-W8I0 R-C98T-8CTG
func TestCancelRunningAndRace(t *testing.T) {
	f := runSetup(t, func(c *runs.Config) { c.MaxActive = 2 })
	p := f.seedRun(t, "daily")
	a := f.start(t, p.Name, "")
	f.entered(t)
	b := f.start(t, p.Name, "")
	f.entered(t)
	pid := f.pid(t, a.ID)
	before := f.record(t, a.ID)
	targetFolder := f.core.Folder(before)
	targetFiles := folderSnapshot(t, targetFolder)
	if len(targetFiles[runs.TranscriptFile].Data) == 0 {
		t.Fatal("child has written no transcript")
	}
	otherBefore := f.record(t, b.ID)
	otherFolder := f.core.Folder(otherBefore)
	otherFiles := folderSnapshot(t, otherFolder)
	otherPID := f.pid(t, b.ID)
	f.caller.UserID = "another"
	refused(t, f.call(t, "cancel", runArg(a.ID)), fmt.Sprintf(tools.MissingRun, a.ID))
	if !reflect.DeepEqual(before, f.record(t, a.ID)) {
		t.Fatal("ownership refusal altered run")
	}
	alivePID(t, pid)
	f.caller.UserID = "owner"
	result := decode[tools.RunEntry](t, f.call(t, "cancel", runArg(a.ID)))
	after := f.record(t, a.ID)
	f.gonePID(t, pid)
	entryMatches(t, result, after)
	usage(t, result)
	if result.Status != store.StatusKilled || result.Finished == nil || result.ExitCode != nil || after.Status != result.Status || after.ID != result.ID {
		t.Fatal(result, after)
	}
	if !reflect.DeepEqual(targetFiles, folderSnapshot(t, targetFolder)) {
		t.Fatal("canceled child files changed")
	}
	if !reflect.DeepEqual(otherBefore, f.record(t, b.ID)) {
		t.Fatal("other run changed")
	}
	if !reflect.DeepEqual(otherFiles, folderSnapshot(t, otherFolder)) {
		t.Fatal("other run files changed")
	}
	alivePID(t, otherPID)
	refused(t, f.call(t, "cancel", runArg(a.ID)), fmt.Sprintf(tools.Ended, a.ID))
	if !reflect.DeepEqual(after, f.record(t, a.ID)) {
		t.Fatal("killed refusal altered run")
	}
	f.caller.UserID = "another"
	refused(t, f.call(t, "cancel", runArg(a.ID)), fmt.Sprintf(tools.MissingRun, a.ID))
	if !reflect.DeepEqual(after, f.record(t, a.ID)) {
		t.Fatal("foreign killed refusal altered run")
	}
	f.caller.UserID = "owner"
	close(f.release)
	raced := f.call(t, "cancel", runArg(b.ID))
	terminal := f.ended(t, b.ID)
	if raced.IsError() {
		refused(t, raced, fmt.Sprintf(tools.Ended, b.ID))
		if terminal.Status != store.StatusExited || terminal.ExitCode != 0 {
			t.Fatal(terminal)
		}
	} else {
		entry := decode[tools.RunEntry](t, raced)
		if entry.Status != store.StatusKilled || terminal.Status != store.StatusKilled {
			t.Fatal(entry, terminal)
		}
	}
	for i, status := range []string{store.StatusExited, store.StatusTimedOut, store.StatusFailed} {
		id, err := store.NewRunID(&sequence{n: byte(100 + i*16)})
		if err != nil {
			t.Fatal(err)
		}
		initial := store.StatusRunning
		if status == store.StatusFailed {
			initial = store.StatusQueued
		}
		rec, err := f.st.AddRun(context.Background(), store.Run{ID: id, Prompt: p.ID, Model: p.Model, User: "owner", RequestID: "seed-" + status, Trigger: store.TriggerManual, Started: childInstant, Status: initial})
		if err != nil {
			t.Fatal(err)
		}
		ending := store.Ending{Status: status, Finished: childInstant}
		if status == store.StatusFailed {
			ending.Reason = store.ReasonStartFailed
		}
		rec, err = f.st.FinishRun(context.Background(), rec.ID, ending)
		if err != nil {
			t.Fatal(err)
		}
		refused(t, f.call(t, "cancel", runArg(id)), fmt.Sprintf(tools.Ended, id))
		if !reflect.DeepEqual(rec, f.record(t, id)) {
			t.Fatal("ended refusal altered run")
		}
		f.caller.UserID = "another"
		refused(t, f.call(t, "cancel", runArg(id)), fmt.Sprintf(tools.MissingRun, id))
		if !reflect.DeepEqual(rec, f.record(t, id)) {
			t.Fatal("foreign ended refusal altered run")
		}
		f.caller.UserID = "owner"
	}
}

// R-BUM0-N3X4 R-BVTX-0VNT
func TestRunRefusalOrderAndFailedStart(t *testing.T) {
	t.Run("unavailable and stopping", func(t *testing.T) {
		f := runSetup(t, func(c *runs.Config) { c.Unavailable = "fixture reason" })
		f.seedRun(t, "daily")
		refused(t, f.call(t, "run", `{"name":"daily"}`), fmt.Sprintf(runs.NoRuns, "fixture reason"))
		f.core.Drain(context.Background())
		for _, input := range []string{"", `{}`} {
			args := `{"name":"daily"`
			if input != "" {
				args += `,"input":` + input
			}
			refused(t, f.call(t, "run", args+`}`), runs.Stopping)
		}
		refused(t, f.call(t, "run", `{"name":"absent"}`), fmt.Sprintf(tools.MissingPrompt, "absent"))
		f.caller.UserID = "another"
		refused(t, f.call(t, "run", `{"name":"daily"}`), fmt.Sprintf(tools.MissingPrompt, "daily"))
	})
	t.Run("full queue", func(t *testing.T) {
		f := runSetup(t, nil)
		p := f.seedRun(t, "daily")
		f.start(t, p.Name, "")
		f.entered(t)
		for i := 0; i < 10; i++ {
			q := f.start(t, p.Name, "")
			if q.Status != store.StatusQueued {
				t.Fatal(q)
			}
		}
		refused(t, f.call(t, "run", `{"name":"daily"}`), fmt.Sprintf(runs.QueueFull, int64(10)))
		refused(t, f.call(t, "run", `{"name":"absent"}`), fmt.Sprintf(tools.MissingPrompt, "absent"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f.core.Drain(ctx)
		refused(t, f.call(t, "run", `{"name":"daily","input":{}}`), runs.Stopping)
	})
	t.Run("failed start is success", func(t *testing.T) {
		f := runSetup(t, func(c *runs.Config) { c.Cgroup = filepath.Join(c.Runs, "missing", "groups") })
		p := f.seedRun(t, "daily")
		a := f.start(t, p.Name, "")
		r := f.record(t, a.ID)
		if a.Status != store.StatusFailed || a.Reason == nil || r.Reason != *a.Reason || r.Status != a.Status || r.Model != p.Model || r.Prompt != p.ID || r.User != f.caller.UserID || r.Trigger != store.TriggerManual || r.Event != "" || r.RequestID != fmt.Sprintf("call-%d", f.n) {
			t.Fatal(a, r)
		}
		if f.requests() != 0 {
			t.Fatal("failed start reached provider")
		}
	})
}

type heldReader struct {
	entered chan struct{}
	release chan struct{}
	data    sequence
}

func (r *heldReader) Read(b []byte) (int, error) {
	close(r.entered)
	<-r.release
	return r.data.Read(b)
}
func held(t *testing.T) *heldReader {
	t.Helper()
	return &heldReader{entered: make(chan struct{}), release: make(chan struct{})}
}
func receive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("synchronization deadline")
	}
}

// R-YUKM-C10H
func TestRunPromptDeletedDuringAdmission(t *testing.T) {
	rand := held(t)
	f := runSetup(t, func(c *runs.Config) { c.Rand = rand })
	p := f.seedRun(t, "daily")
	result := make(chan mcp.Result, 1)
	errs := make(chan error, 1)
	go func() {
		r, err := f.client.CallTool(context.Background(), f.caller, "run", json.RawMessage(`{"name":"daily"}`))
		errs <- err
		result <- r
	}()
	receive(t, rand.entered)
	if err := f.st.Delete(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	close(rand.release)
	var r mcp.Result
	select {
	case r = <-result:
	case <-time.After(10 * time.Second):
		t.Fatal("run deadline")
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	refused(t, r, fmt.Sprintf(tools.MissingPrompt, p.Name))
	id, err := store.NewRunID(&sequence{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(filepath.Join(f.cfg.Runs, p.ID, id))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if f.requests() != 0 {
		t.Fatal("deleted admission started")
	}
	assertNoRunEvents(t, f)
}
func assertNoRunEvents(t *testing.T, f *runFixture) {
	t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.capture.Events() {
		if strings.HasPrefix(e.Name, "run.") {
			t.Fatal(e)
		}
	}
}

// R-1533-Y9T7
func TestRunCutOffEndsHandler(t *testing.T) {
	for _, drain := range []bool{false, true} {
		t.Run(fmt.Sprint(drain), func(t *testing.T) {
			rand := held(t)
			f := runSetup(t, func(c *runs.Config) { c.Rand = rand })
			f.seedRun(t, "daily")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "http://prompts.test/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"name":"daily"}}}`)).WithContext(ctx)
			req.Header.Set("X-User-Id", "owner")
			req.Header.Set("X-Request-Id", "cutoff")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			done := make(chan struct{})
			returned := false
			go func() { defer close(done); identity.Require(f.srv).ServeHTTP(response, req); returned = true }()
			receive(t, rand.entered)
			drainDone := make(chan struct{})
			if drain {
				dctx, dcancel := context.WithCancel(context.Background())
				dcancel()
				observed := &observedContext{Context: dctx, entered: make(chan struct{})}
				go func() { f.core.Drain(observed); close(drainDone) }()
				receive(t, observed.entered)
				refused(t, f.call(t, "run", `{"name":"daily"}`), runs.Stopping)
			} else {
				cancel()
			}
			close(rand.release)
			receive(t, done)
			if drain {
				receive(t, drainDone)
			}
			if returned || response.Body.Len() != 0 {
				t.Fatalf("handler returned=%v wrote=%s", returned, response.Body)
			}
			assertNoRunFiles(t, f.fixture)
			assertNoRunEvents(t, f)
			for _, e := range f.capture.Events() {
				if e.RequestID == "cutoff" && e.Name == "tool.called" {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-0LKP-TXY3
func TestRunDiskFailureRefusal(t *testing.T) {
	f := runSetup(t, nil)
	p := f.seedRun(t, "daily")
	if err := os.MkdirAll(f.cfg.Runs, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := root.Chmod("runs", 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Chmod("runs", 0700); err != nil {
			t.Error(err)
		}
	})
	refused(t, f.call(t, "run", `{"name":"daily"}`), store.Unreachable)
	requestID := fmt.Sprintf("call-%d", f.n)
	rs, err := f.st.Runs(context.Background(), p.ID)
	if err != nil || len(rs) != 0 {
		t.Fatal(rs, err)
	}
	assertNoRunFiles(t, f.fixture)
	if f.requests() != 0 {
		t.Fatal("disk failure reached provider")
	}
	assertNoRunEvents(t, f)
	called := 0
	for _, e := range f.capture.Events() {
		if e.RequestID == requestID && e.Name == "tool.called" {
			called++
			if e.Attrs["outcome"] != "error" {
				t.Fatal(e)
			}
		}
	}
	if called != 1 {
		t.Fatal(called)
	}
}

// R-AST4-S7XL
func TestEveryToolTrace(t *testing.T) {
	f := runSetup(t, func(c *runs.Config) { c.Cgroup = filepath.Join(c.Runs, "missing", "groups") })
	type expected struct {
		tool, kind, outcome string
		allowed             map[string]bool
	}
	want := map[string]expected{}
	call := func(name, args, kind string, allowed ...string) mcp.Result {
		r := f.call(t, name, args)
		outcome := "ok"
		if r.IsError() {
			outcome = "error"
		}
		events := map[string]bool{"request.started": true, "request.finished": true, "tool.called": true}
		for _, event := range allowed {
			events[event] = true
		}
		want[fmt.Sprintf("call-%d", f.n)] = expected{name, kind, outcome, events}
		return r
	}
	decode[tools.Prompt](t, call("create", createArgs("daily"), "additive", "prompt.created"))
	content(t, call("list", `{}`, "read"))
	content(t, call("show", `{"name":"daily"}`, "read"))
	content(t, call("update", `{"name":"daily","prompt":"changed"}`, "additive", "prompt.updated"))
	for i := 0; i < 2; i++ {
		content(t, call("subscribe", `{"name":"daily","event":"repo.pushed"}`, "additive"))
	}
	content(t, call("unsubscribe", `{"name":"daily","event":"repo.pushed"}`, "destructive"))
	started := decode[tools.Started](t, call("run", `{"name":"daily"}`, "additive", "run.finished"))
	if started.Status != store.StatusFailed {
		t.Fatal(started)
	}
	content(t, call("runs", `{"name":"daily"}`, "read"))
	content(t, call("result", runArg(started.ID), "read"))
	refused(t, call("cancel", runArg(started.ID), "destructive"), fmt.Sprintf(tools.Ended, started.ID))
	content(t, call("delete", `{"name":"daily"}`, "destructive", "prompt.deleted"))
	for _, row := range []struct{ name, args, kind string }{
		{"show", `{"name":"daily"}`, "read"}, {"create", createArgs("events"), "additive"}, {"update", `{"name":"daily"}`, "additive"}, {"delete", `{"name":"daily"}`, "destructive"}, {"subscribe", `{"name":"daily","event":"repo.pushed"}`, "additive"}, {"unsubscribe", `{"name":"daily","event":"repo.pushed"}`, "destructive"}, {"run", `{"name":"daily"}`, "additive"}, {"runs", `{"name":"daily"}`, "read"}, {"result", runArg(started.ID), "read"}, {"cancel", runArg(started.ID), "destructive"},
	} {
		if !call(row.name, row.args, row.kind).IsError() {
			t.Fatal(row.name)
		}
	}
	f.d.SetFailing(true)
	if !call("list", `{}`, "read").IsError() {
		t.Fatal("list refusal")
	}
	f.d.SetFailing(false)
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range f.capture.Events() {
		w, ok := want[e.RequestID]
		if !ok {
			continue
		}
		if !w.allowed[e.Name] {
			t.Fatal("extra event", e)
		}
		if e.Name == "tool.called" {
			counts[e.RequestID]++
			if e.Attrs["tool"] != w.tool || e.Attrs["kind"] != w.kind || e.Attrs["outcome"] != w.outcome {
				t.Fatal(e, w)
			}
		}
	}
	for id := range want {
		if counts[id] != 1 {
			t.Fatal(id, counts[id])
		}
	}
}

type observedContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
}

func (c *observedContext) Err() error { c.once.Do(func() { close(c.entered) }); return c.Context.Err() }

// appkit's client marshals RawMessage compactly, so this exact-byte request
// exercises the one request representation that the client cannot send.
func (f *runFixture) rawStart(t *testing.T, name, input string) tools.Started {
	t.Helper()
	f.n++
	req := httptest.NewRequest(http.MethodPost, "http://prompts.test/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"name":"`+name+`","input":`+input+`}}}`))
	req.Header.Set("X-User-Id", f.caller.UserID)
	req.Header.Set("X-Request-Id", fmt.Sprintf("call-%d", f.n))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	identity.Require(f.srv).ServeHTTP(response, req)
	var envelope struct{ Result mcp.Result }
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err, response.Body)
	}
	return decode[tools.Started](t, envelope.Result)
}

func (f *runFixture) pid(t *testing.T, id string) int {
	t.Helper()
	root, err := os.OpenRoot(f.cfg.Cgroup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	b, err := root.ReadFile(filepath.Join(id, "cgroup.procs"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}
func (f *runFixture) gonePID(t *testing.T, pid int) {
	t.Helper()
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child process remains: %v", err)
	}
}
func entryMatches(t *testing.T, e tools.RunEntry, r store.Run) {
	t.Helper()
	if e.ID != r.ID || e.Model != r.Model || e.Status != r.Status || e.Trigger != r.Trigger || e.Started != r.Started.UTC().Format(time.RFC3339) || e.Truncated != r.Truncated() {
		t.Fatal(e, r)
	}
	if e.Finished != nil && *e.Finished != r.Finished.UTC().Format(time.RFC3339) {
		t.Fatal(e, r)
	}
	for _, pair := range []struct {
		got  *int64
		want int64
	}{{e.Calls, r.Usage.Calls}, {e.ToolCalls, r.Usage.ToolCalls}, {e.InputTokens, r.Usage.InputTokens}, {e.CachedTokens, r.Usage.CachedTokens}, {e.OutputTokens, r.Usage.OutputTokens}, {e.ReasoningTokens, r.Usage.ReasoningTokens}, {e.CostNanos, r.Usage.CostNanos}} {
		if pair.got == nil || *pair.got != pair.want {
			t.Fatal(e, r)
		}
	}
	if r.Status == store.StatusExited && (e.ExitCode == nil || *e.ExitCode != r.ExitCode) {
		t.Fatal(e, r)
	}
}

type folderEntry struct {
	Mode fs.FileMode
	Data []byte
}

func folderSnapshot(t *testing.T, folder string) map[string]folderEntry {
	t.Helper()
	root, err := os.OpenRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	entries := map[string]folderEntry{}
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := folderEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			entry.Data, err = root.ReadFile(path)
			if err != nil {
				return err
			}
		}
		entries[path] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
func alivePID(t *testing.T, pid int) {
	t.Helper()
	root, err := os.OpenRoot("/proc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	stat, err := root.ReadFile(fmt.Sprintf("%d/stat", pid))
	if err != nil {
		t.Fatal("other child disappeared", err)
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		t.Fatal("invalid process stat")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) == 0 || fields[0] == "Z" || fields[0] == "X" {
		t.Fatal("other child is not alive", string(stat))
	}
}
