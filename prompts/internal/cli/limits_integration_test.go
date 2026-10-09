package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
	"golang.org/x/sys/unix"
)

func integrationChangedLookup(f *serveFixture, replacements map[string]string) *atomic.Bool {
	changed := &atomic.Bool{}
	original := f.p.LookupEnv
	f.p.LookupEnv = func(key string) (string, bool) {
		value, ok := original(key)
		if changed.Load() {
			if replacement, found := replacements[key]; found {
				return replacement, true
			}
		}
		return value, ok
	}
	return changed
}

func integrationSink(f *serveFixture) *observedServeSink {
	s := &observedServeSink{events: make(chan telemetry.Event, 1024)}
	f.p.Sink = s
	return s
}
func integrationAnswer(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
	for _, event := range []string{`{"type":"message_start","message":{"id":"integration","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, string(delta), `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`, `{"type":"message_stop"}`} {
		_, _ = io.WriteString(w, "data: "+event+"\n\n")
	}
}
func integrationProvider(t *testing.T, f *serveFixture, text string) *atomic.Int64 {
	t.Helper()
	count := &atomic.Int64{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count.Add(1); integrationAnswer(w, text) }))
	t.Cleanup(provider.Close)
	f.p.BaseURL = provider.URL
	f.env[agent.KeyVariable(agentkit.HostAnthropic)] = "integration-key"
	return count
}
func integrationPrompt(t *testing.T, f *serveFixture, name string, groups []string) tools.Prompt {
	t.Helper()
	return decodeServe[tools.Prompt](t, f.call(t, "create", map[string]any{"name": name, "model": serveChatModel(t), "prompt": "supplied prompt", "tools": groups}))
}
func integrationRun(t *testing.T, f *serveFixture, sink *observedServeSink, name string) tools.RunResult {
	t.Helper()
	started := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": name}))
	awaitServeEvent(t, sink, "run.finished", started.ID)
	return decodeServe[tools.RunResult](t, f.call(t, "result", map[string]any{"run": started.ID}))
}
func integrationFile(t *testing.T, f *serveFixture, prompt, id, file string) []byte {
	t.Helper()
	root, e := os.OpenRoot(f.p.Dir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = root.Close() }()
	b, e := root.ReadFile(filepath.Join("state", "runs", prompt, id, file))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func integrationGet(t *testing.T, f *serveFixture, path string) []byte {
	t.Helper()
	r, e := http.NewRequest("GET", "http://backend"+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-User-Id", "fixture-user")
	response, e := f.http.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = response.Body.Close() }()
	b, e := io.ReadAll(response.Body)
	if e != nil || response.StatusCode != 200 {
		t.Fatal(e, response.StatusCode)
	}
	return b
}

// R-M2IU-55LV R-M3QQ-IXCK R-M4YM-WP39 R-M66J-AGTY R-M7EF-O8KN R-M9U8-FS21 R-MB24-TJSQ R-MCA1-7BJF
func TestRunProcessSeamsAndStateIsolation(t *testing.T) {
	f := newServeFixture(t)
	tree := t.TempDir()
	f.p.Dir = filepath.Join(tree, "data")
	f.p.Cgroup = filepath.Join(tree, "control")
	if e := os.MkdirAll(f.p.Cgroup, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(f.p.Cgroup, "cgroup.procs"), []byte("73"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(tree, "sentinel"), []byte("supplied sentinel"), 0600); e != nil {
		t.Fatal(e)
	}
	f.p.Rand = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 65536))
	sink := integrationSink(f)
	count := integrationProvider(t, f, "supplied final answer")
	f.start(t)
	before := readTree(t, tree)
	p := integrationPrompt(t, f, "seam-prompt", []string{})
	if p.ID != store.PromptPrefix+strings.Repeat("5a", 8) || p.Created != serveTime.UTC().Truncate(time.Second).Format(time.RFC3339) {
		t.Fatal(p)
	}
	result := integrationRun(t, f, sink, p.Name)
	if result.ID != store.RunPrefix+strings.Repeat("5a", 8) || result.Started != p.Created || result.Status != store.StatusExited || result.ExitCode == nil || *result.ExitCode != 0 || result.Stdout == nil || !strings.Contains(*result.Stdout, "supplied final answer") || count.Load() == 0 {
		t.Fatal(result, count.Load())
	}
	if info, e := os.Stat(filepath.Join(f.p.Dir, "state", "runs", p.ID, result.ID)); e != nil || !info.IsDir() {
		t.Fatal(info, e)
	}
	_ = integrationGet(t, f, "/"+p.Name+"/runs/"+result.ID+"/")
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	after := readTree(t, tree)
	outside := func(files map[string]string) map[string]string {
		m := map[string]string{}
		for name, value := range files {
			if name != "data" && name != "control" && !strings.HasPrefix(name, "data/") && !strings.HasPrefix(name, "control/") {
				m[name] = value
			}
		}
		return m
	}
	if !reflect.DeepEqual(outside(before), outside(after)) {
		t.Fatal("writes outside state and cgroup", outside(before), outside(after))
	}
	allowed := strings.Fields("DRAIN_SECONDS PROMPT_SECONDS OUTPUT_MAX_BYTES RUN_MAX_TOOL_CALLS RUN_MEMORY_MAX_BYTES RUNS_MEMORY_MAX_BYTES RUNS_CPU_PERCENT RUN_PIDS_MAX RUN_MAX_ACTIVE RUN_MAX_QUEUED RUN_KEEP_DAYS RUN_KEEP_COUNT ANTHROPIC_API_KEY OPENAI_API_KEY GEMINI_API_KEY XAI_API_KEY OPENROUTER_API_KEY IKIGENBA_SERVICES LISTEN_PID LISTEN_FDS NOTIFY_SOCKET PATH")
	for _, key := range f.lookups {
		found := false
		for _, a := range allowed {
			found = found || key == a
		}
		if !found {
			t.Fatal("unexpected lookup", key)
		}
	}
	es := sink.capture.Events()
	if es[0].Name != "service.started" || es[len(es)-1].Name != "service.stopping" {
		t.Fatal(es)
	}
	for _, same := range []bool{true, false} {
		next := newServeFixture(t)
		if same {
			next.p.Dir = f.p.Dir
		}
		next.start(t)
		listed := decodeServe[tools.PromptList](t, next.call(t, "list", map[string]any{}))
		if same && (len(listed.Prompts) != 1 || listed.Prompts[0].ID != p.ID) || !same && len(listed.Prompts) != 0 {
			t.Fatal(listed)
		}
		if code := next.stop(t); code != 0 {
			t.Fatal(code)
		}
	}
}

// R-85IS-KFH9 R-8F9Z-MLET
func TestRunSnapshotsProviderKeysAndAcceptsAbsentKeys(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			f := newServeFixture(t)
			sink := integrationSink(f)
			keyRequests := make(chan string, 4)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				keyRequests <- r.Header.Get("X-Api-Key")
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				headers, err := json.Marshal(r.Header)
				if err != nil {
					t.Error(err)
				}
				for i := 1; i < 5; i++ {
					other := []byte(fmt.Sprintf("snapshot-provider-%d", i))
					if bytes.Contains(body, other) || bytes.Contains(headers, other) {
						t.Error("request carried another provider key")
					}
				}
				integrationAnswer(w, "supplied")
			}))
			defer provider.Close()
			f.p.BaseURL = provider.URL
			hosts := []agentkit.Host{agentkit.HostAnthropic, agentkit.HostOpenAI, agentkit.HostGemini, agentkit.HostXAI, agentkit.HostOpenRouter}
			if !absent {
				for i, h := range hosts {
					f.env[agent.KeyVariable(h)] = fmt.Sprintf("snapshot-provider-%d", i)
				}
			}
			replacements := map[string]string{}
			for _, h := range hosts {
				replacements[agent.KeyVariable(h)] = "changed-provider-key"
			}
			changed := integrationChangedLookup(f, replacements)
			f.start(t)
			changed.Store(true)
			if got := decodeServe[tools.PromptList](t, f.call(t, "list", map[string]any{})); len(got.Prompts) != 0 {
				t.Fatal(got)
			}
			p := integrationPrompt(t, f, "keys", []string{})
			started := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
			if started.Status != store.StatusRunning && started.Status != store.StatusQueued {
				t.Fatal(started)
			}
			awaitServeEvent(t, sink, "run.finished", started.ID)
			if !absent {
				if key := serveWait(t, keyRequests); key != "snapshot-provider-0" {
					t.Fatal(key)
				}
			}
			if code := f.stop(t); code != 0 || f.err.String() != "" {
				t.Fatal(code, f.err.String())
			}
			for _, h := range hosts {
				n := 0
				for _, k := range f.lookups {
					if k == agent.KeyVariable(h) {
						n++
					}
				}
				if n != 1 {
					t.Fatal(h, n)
				}
			}
		})
	}
}

// R-A4EP-MNFZ
func TestRunMakesProcessNonDumpableBeforeFirstRun(t *testing.T) {
	if e := unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0) }()
	f := newServeFixture(t)
	random := &serveBlockingRandom{source: serveRandom(), entered: make(chan struct{}), release: make(chan struct{})}
	f.p.Rand = random
	integrationProvider(t, f, "supplied")
	sink := integrationSink(f)
	f.start(t)
	p := integrationPrompt(t, f, "dumpable", []string{})
	random.hold()
	answer := make(chan tools.Started, 1)
	go func() { answer <- decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name})) }()
	serveWait(t, random.entered)
	dumpable, e := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if e != nil || dumpable != 0 {
		t.Fatal(dumpable, e)
	}
	close(random.release)
	started := serveWait(t, answer)
	awaitServeEvent(t, sink, "run.finished", started.ID)
	if code := f.stop(t); code != 0 {
		t.Fatal(code)
	}
}

// R-6017-FXBE
func TestRunOutputLimitChangesOnlyAcrossStarts(t *testing.T) {
	var dir, prompt string
	var firstID string
	var firstFile []byte
	for i, limit := range []string{"1024", "4096"} {
		f := newServeFixture(t)
		if i == 0 {
			dir = f.p.Dir
		} else {
			f.p.Dir = dir
			f.p.Rand = &serveReadProbe{next: 1000}
		}
		f.env["OUTPUT_MAX_BYTES"] = limit
		sink := integrationSink(f)
		integrationProvider(t, f, strings.Repeat("x", 2000))
		f.start(t)
		if i == 0 {
			p := integrationPrompt(t, f, "output-limit", []string{})
			prompt = p.ID
		}
		result := integrationRun(t, f, sink, "output-limit")
		want := 1024
		if i == 1 {
			want = 2000
		}
		if result.Status != store.StatusExited || result.ExitCode == nil || *result.ExitCode != 0 || result.StdoutBytes != int64(want) || result.Truncated != (i == 0) {
			t.Fatal(result)
		}
		file := integrationFile(t, f, prompt, result.ID, runs.StdoutFile)
		if len(file) != want || string(file) != strings.Repeat("x", want) {
			t.Fatal(len(file))
		}
		if code := f.stop(t); code != 0 {
			t.Fatal(code)
		}
		if i == 0 {
			firstID = result.ID
			firstFile = file
		} else {
			if !bytes.Equal(firstFile, integrationFile(t, f, prompt, firstID, runs.StdoutFile)) {
				t.Fatal("prior file changed")
			}
			d, st := serveCatalog(t, dir)
			old, e := st.RunByID(context.Background(), firstID)
			_ = d.Close()
			if e != nil || old.StdoutBytes != 1024 || !old.StdoutTruncated {
				t.Fatal(old, e)
			}
		}
	}
}

// R-62H0-7GSS R-664P-CS0V
func TestRunTimersAndSettingsStayAtStartValues(t *testing.T) {
	f := newServeFixture(t)
	f.env["PROMPT_SECONDS"] = "30"
	f.env["RUN_MAX_ACTIVE"] = "1"
	f.env["OUTPUT_MAX_BYTES"] = "1024"
	timers := make(chan chan time.Time, 4)
	durations := make(chan time.Duration, 4)
	f.p.ScriptAfter = func(d time.Duration) <-chan time.Time {
		timer := make(chan time.Time, 1)
		durations <- d
		timers <- timer
		return timer
	}
	entered := make(chan struct{}, 4)
	releases := make(chan chan struct{}, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := make(chan struct{})
		releases <- release
		entered <- struct{}{}
		select {
		case <-release:
			integrationAnswer(w, strings.Repeat("x", 2000))
		case <-r.Context().Done():
		}
	}))
	defer provider.Close()
	f.p.BaseURL = provider.URL
	f.env[agent.KeyVariable(agentkit.HostAnthropic)] = "integration-key"
	sink := integrationSink(f)
	changed := integrationChangedLookup(f, map[string]string{"RUN_MAX_ACTIVE": "2", "OUTPUT_MAX_BYTES": "4096", "PROMPT_SECONDS": "90"})
	f.start(t)
	changed.Store(true)
	p := integrationPrompt(t, f, "timer-limit", []string{})
	first := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
	serveWait(t, entered)
	releaseFirst := serveWait(t, releases)
	firstTimer := serveWait(t, timers)
	if d := serveWait(t, durations); d != 30*time.Second {
		t.Fatal(d)
	}
	second := decodeServe[tools.Started](t, f.call(t, "run", map[string]any{"name": p.Name}))
	if first.Status != store.StatusRunning || second.Status != store.StatusQueued {
		t.Fatal(first, second)
	}
	select {
	case <-timers:
		t.Fatal("queued timer started")
	default:
	}
	close(releaseFirst)
	awaitServeEvent(t, sink, "run.finished", first.ID)
	serveWait(t, entered)
	releaseSecond := serveWait(t, releases)
	_ = serveWait(t, timers)
	if d := serveWait(t, durations); d != 30*time.Second {
		t.Fatal(d)
	}
	result := decodeServe[tools.RunResult](t, f.call(t, "result", map[string]any{"run": first.ID}))
	if result.StdoutBytes != 1024 || !result.Truncated {
		t.Fatal(result)
	}
	close(releaseSecond)
	awaitServeEvent(t, sink, "run.finished", second.ID)
	// The injected timer was allocated for the running child and can be driven without waiting.
	firstTimer <- serveTime
	if code := f.stop(t); code != 0 {
		t.Fatal(code)
	}
	for _, k := range []string{"PROMPT_SECONDS", "OUTPUT_MAX_BYTES", "RUN_MAX_ACTIVE"} {
		n := 0
		for _, key := range f.lookups {
			if key == k {
				n++
			}
		}
		if n != 1 {
			t.Fatal(k, n)
		}
	}
}

func integrationToolAnswer(w http.ResponseWriter, n int, name string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: "+`{"type":"message_start","message":{"id":"tool-fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}}`+"\n\n")
	for i := 0; i < n; i++ {
		start, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("fixture-call-%d", i), "name": name, "input": map[string]any{}}})
		delta, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]string{"type": "input_json_delta", "partial_json": `{"path":"."}`}})
		for _, event := range []string{string(start), string(delta), fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i)} {
			_, _ = io.WriteString(w, "data: "+event+"\n\n")
		}
	}
	for _, event := range []string{`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`, `{"type":"message_stop"}`} {
		_, _ = io.WriteString(w, "data: "+event+"\n\n")
	}
}
func integrationLogs(t *testing.T, b []byte) []agentkit.LogRecord {
	t.Helper()
	var records []agentkit.LogRecord
	scan := bufio.NewScanner(bytes.NewReader(b))
	for scan.Scan() {
		var record agentkit.LogRecord
		if e := json.Unmarshal(scan.Bytes(), &record); e != nil {
			t.Fatal(e)
		}
		records = append(records, record)
	}
	if e := scan.Err(); e != nil {
		t.Fatal(e)
	}
	return records
}

// R-63OW-L8JH R-64WS-Z0A6
func TestRunToolLimitsChangeAcrossStartsAndAllowHugeValue(t *testing.T) {
	var dir, prompt string
	for i, limit := range []string{"1", "2", "99999999999999999999"} {
		f := newServeFixture(t)
		if i == 0 {
			dir = f.p.Dir
		} else {
			f.p.Dir = dir
			f.p.Rand = &serveReadProbe{next: uint64(i * 1000)}
		}
		f.env["RUN_MAX_TOOL_CALLS"] = limit
		sink := integrationSink(f)
		n := 2
		if i == 2 {
			n = 3
		}
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Messages []struct{ Role string }
				Tools    []struct{ Name string }
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			if len(body.Messages) == 1 {
				name := ""
				for _, tool := range body.Tools {
					if strings.EqualFold(tool.Name, "Glob") {
						name = tool.Name
					}
				}
				if name == "" {
					t.Error("missing offered Glob")
					w.WriteHeader(500)
					return
				}
				integrationToolAnswer(w, n, name)
			} else {
				integrationAnswer(w, "supplied")
			}
		}))
		f.p.BaseURL = provider.URL
		f.env[agent.KeyVariable(agentkit.HostAnthropic)] = "integration-key"
		f.start(t)
		if i == 0 {
			p := integrationPrompt(t, f, "tool-limit", []string{"files"})
			prompt = p.ID
		}
		result := integrationRun(t, f, sink, "tool-limit")
		wantCode := 0
		if i == 0 {
			wantCode = 2
		}
		if result.Status != store.StatusExited || result.ExitCode == nil || *result.ExitCode != wantCode {
			t.Fatal(result)
		}
		records := integrationLogs(t, integrationFile(t, f, prompt, result.ID, runs.TranscriptFile))
		limits, dispatches := 0, 0
		for _, record := range records {
			if record.Limit != nil {
				limits++
				if i != 0 || record.Limit.Max != 1 {
					t.Fatal(record)
				}
			}
			if record.ToolResult != nil {
				dispatches++
			}
		}
		if i == 0 && limits != 1 || i > 0 && (limits != 0 || dispatches != n) {
			t.Fatal("limits and dispatched calls", limits, dispatches, n)
		}
		if code := f.stop(t); code != 0 {
			t.Fatal(code)
		}
		provider.Close()
	}
}

// R-6193-TP23
func TestRunPrunesWithNewRetentionSettingsBeforeReady(t *testing.T) {
	f := newServeFixture(t)
	sink := integrationSink(f)
	integrationProvider(t, f, "supplied")
	now := &atomic.Int64{}
	now.Store(serveTime.Add(-10 * 24 * time.Hour).UnixNano())
	f.p.Now = func() time.Time { return time.Unix(0, now.Load()).UTC() }
	f.start(t)
	p := integrationPrompt(t, f, "retention", []string{})
	var made []tools.RunResult
	for i := 0; i < 5; i++ {
		made = append(made, integrationRun(t, f, sink, p.Name))
		now.Add(int64(24 * time.Hour))
	}
	if code := f.stop(t); code != 0 {
		t.Fatal(code)
	}
	d, st := serveCatalog(t, f.p.Dir)
	expected, e := st.PastKeeping(context.Background(), serveTime, 3, 2)
	if e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	if len(expected) == 0 || len(expected) == len(made) {
		t.Fatal("retention fixture does not distinguish bounds", expected)
	}
	next := newServeFixture(t)
	next.p.Dir = f.p.Dir
	next.env["RUN_KEEP_DAYS"] = "3"
	next.env["RUN_KEEP_COUNT"] = "2"
	next.start(t)
	d, st = serveCatalog(t, f.p.Dir)
	remaining, e := st.Runs(context.Background(), p.ID)
	_ = d.Close()
	if e != nil {
		t.Fatal(e)
	}
	removed := map[string]bool{}
	for _, r := range expected {
		removed[r.ID] = true
	}
	if len(remaining) != len(made)-len(expected) {
		t.Fatal(remaining, expected)
	}
	for _, r := range made {
		_, e := os.Stat(filepath.Join(f.p.Dir, "state", "runs", p.ID, r.ID))
		if removed[r.ID] && !os.IsNotExist(e) || !removed[r.ID] && e != nil {
			t.Fatal(r.ID, e)
		}
		for _, kept := range remaining {
			if removed[kept.ID] {
				t.Fatal("retained expired record", kept)
			}
		}
	}
	if code := next.stop(t); code != 0 {
		t.Fatal(code)
	}
}

type credentialTransport struct {
	next       http.RoundTripper
	credential atomic.Value
}

func (tr *credentialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", tr.credential.Load().(string))
	return tr.next.RoundTrip(clone)
}

type securitySink struct {
	observed *observedServeSink
	fail     bool
}

func (s securitySink) Deliver(ctx context.Context, event telemetry.Event) error {
	_ = s.observed.Deliver(ctx, event)
	if s.fail {
		return errors.New("fixture sink refusal")
	}
	return nil
}
func securityStrings(t *testing.T, values ...string) []string {
	t.Helper()
	var strings8 []string
	for _, value := range values {
		if len(value) < 16 {
			t.Fatal("short credential fixture")
		}
		for i := 0; i+8 <= len(value); i++ {
			part := value[i : i+8]
			hasUpper := false
			for _, c := range part {
				hasUpper = hasUpper || c >= 'A' && c <= 'Z'
			}
			if !hasUpper {
				t.Fatal("credential fixture lacks uppercase", part)
			}
			strings8 = append(strings8, part)
		}
	}
	return strings8
}
func securityCheck(t *testing.T, label string, b []byte, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("%s leaked secret fragment %q", label, secret)
		}
	}
}
func securityDisk(t *testing.T, f *serveFixture, secrets []string) {
	t.Helper()
	for path, b := range readTree(t, f.p.Dir) {
		securityCheck(t, "disk "+path, []byte(b), secrets)
	}
}
func securityToolAnswer(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "text/event-stream")
	command := `mkdir -p "$IKIGENBA_WORK_DIR/d"; cat /proc/$$/environ >"$IKIGENBA_WORK_DIR/env"; cat /proc/$$/environ >"$IKIGENBA_WORK_DIR/d/env"; cat /proc/$PPID/cmdline >"$IKIGENBA_WORK_DIR/cmdline"`
	arguments, _ := json.Marshal(map[string]string{"command": command})
	start, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "capture-call", "name": name, "input": map[string]any{}}})
	delta, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(arguments)}})
	for _, event := range []string{`{"type":"message_start","message":{"id":"capture-fixture","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}}`, string(start), string(delta), `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`, `{"type":"message_stop"}`} {
		_, _ = io.WriteString(w, "data: "+event+"\n\n")
	}
}

// R-1T6I-11M9 R-1UEE-ETCY R-YZAY-8W1G R-Z0IU-MNS5 R-Z1QR-0FIU R-Z2YN-E79J R-Z46J-RZ08 R-Z5EG-5QQX R-Z6MC-JIHM
func TestRunCredentialAndProviderKeyIsolation(t *testing.T) {
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	principal := strings.Join([]string{"USER", "CREDENTIAL", "ABCDEF"}, "")
	bearer := strings.Join([]string{"PASS", "CREDENTIAL", "UVWXYZ"}, "")
	encoded := base64.StdEncoding.EncodeToString([]byte(principal + ":" + bearer))
	secrets := securityStrings(t, principal, bearer, encoded)
	hosts := []agentkit.Host{agentkit.HostAnthropic, agentkit.HostOpenAI, agentkit.HostGemini, agentkit.HostXAI, agentkit.HostOpenRouter}
	keys := []string{"ANTHROPICKEYABCDEFGH", "OPENAIKEYIJKLMNOPQR", "GEMINIKEYSTUVWXYZAB", "XAIKEYCDEFGHIJKLMN", "OPENROUTERKEYOPQRST"}
	keyStrings := securityStrings(t, keys...)
	for _, s := range secrets {
		for _, k := range keyStrings {
			if s == k {
				t.Fatal("credential and key fixtures overlap")
			}
		}
	}
	allSecrets := append(append([]string(nil), secrets...), keyStrings...)
	for _, failSink := range []bool{false, true} {
		t.Run(fmt.Sprint(failSink), func(t *testing.T) {
			f := newServeFixture(t)
			f.env["PATH"] = filepath.Dir(bash) + ":/bin:/usr/bin"
			servicesPath := filepath.Join(f.p.Dir, "services.json")
			if e := os.WriteFile(servicesPath, []byte(`{"services":[]}`), 0600); e != nil {
				t.Fatal(e)
			}
			f.env["IKIGENBA_SERVICES"] = servicesPath
			for i, h := range hosts {
				f.env[agent.KeyVariable(h)] = keys[i]
			}
			sink := &observedServeSink{events: make(chan telemetry.Event, 4096)}
			f.p.Sink = securitySink{observed: sink, fail: failSink}
			transport := &credentialTransport{next: f.http.Transport}
			transport.credential.Store("Bearer " + bearer)
			f.http.Transport = transport
			f.http.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			held := make(chan struct{}, 8)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
					w.WriteHeader(500)
					return
				}
				var message struct {
					Messages []struct{ Content json.RawMessage }
					Tools    []struct{ Name string }
				}
				if e = json.Unmarshal(body, &message); e != nil {
					t.Error(e)
					w.WriteHeader(500)
					return
				}
				if !bytes.Contains(body, []byte(`"type":"tool_result"`)) {
					name := ""
					for _, tool := range message.Tools {
						if tool.Name == "Bash" {
							name = tool.Name
						}
					}
					if name == "" {
						t.Error("provider did not receive Bash")
						w.WriteHeader(500)
						return
					}
					securityToolAnswer(w, name)
					return
				}
				var text string
				for _, m := range message.Messages {
					var blocks []struct{ Text string }
					if json.Unmarshal(m.Content, &blocks) == nil {
						for _, block := range blocks {
							text += block.Text
						}
					}
				}
				if strings.Contains(text, `"refuse":true`) {
					w.WriteHeader(401)
					_, _ = io.WriteString(w, "fixture provider refused")
					return
				}
				if strings.Contains(text, `"hold":true`) {
					held <- struct{}{}
					<-r.Context().Done()
					return
				}
				integrationAnswer(w, "supplied final answer")
			}))
			t.Cleanup(provider.Close)
			f.p.BaseURL = provider.URL
			f.start(t)
			call := func(name string, args any) mcp.Result {
				t.Helper()
				result := f.call(t, name, args)
				b, e := result.MarshalJSON()
				if e != nil {
					t.Fatal(e)
				}
				securityCheck(t, "tool "+name, b, allSecrets)
				return result
			}
			result := func(id string) tools.RunResult {
				t.Helper()
				v := decodeServe[tools.RunResult](t, call("result", map[string]any{"run": id}))
				if v.Stderr != nil {
					securityCheck(t, "child stderr", []byte(*v.Stderr), allSecrets)
				}
				return v
			}
			start := func(name string, hold, refuse bool) tools.Started {
				t.Helper()
				return decodeServe[tools.Started](t, call("run", map[string]any{"name": name, "input": map[string]bool{"hold": hold, "refuse": refuse}}))
			}
			finish := func(id string) tools.RunResult {
				t.Helper()
				awaitServeEvent(t, sink, "run.finished", id)
				r := result(id)
				if r.Status != store.StatusExited {
					t.Fatal(r)
				}
				return r
			}
			create := func(name string, groups []string) tools.Prompt {
				t.Helper()
				return decodeServe[tools.Prompt](t, call("create", map[string]any{"name": name, "model": serveChatModel(t), "prompt": "supplied security prompt", "tools": groups}))
			}
			capture := func(prompt, id string) {
				t.Helper()
				for _, file := range []string{"env", "d/env", "cmdline"} {
					b := integrationFile(t, f, prompt, id, filepath.Join(runs.WorkDir, file))
					if len(b) == 0 {
						t.Fatal("empty process capture", id, file)
					}
					securityCheck(t, "process "+id+"/"+file, b, allSecrets)
				}
			}
			for pass, header := range []string{"Bearer " + bearer, "Basic " + encoded} {
				transport.credential.Store(header)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				listed, e := f.client.ListTools(ctx, identity.Caller{UserID: "fixture-user", Email: "fixture@example.test"})
				cancel()
				if e != nil || len(listed) != 11 {
					t.Fatal(e, len(listed))
				}
				b, _ := json.Marshal(listed)
				securityCheck(t, "tools list", b, allSecrets)
				name := fmt.Sprintf("security-main-%d", pass)
				other := fmt.Sprintf("security-delete-%d", pass)
				suiteName := fmt.Sprintf("security-suite-%d", pass)
				p := create(name, []string{"files", "bash"})
				call("update", map[string]any{"name": name, "system": "supplied security system"})
				call("show", map[string]any{"name": name})
				call("list", map[string]any{})
				call("subscribe", map[string]any{"name": name, "event": "repo.pushed"})
				call("unsubscribe", map[string]any{"name": name, "event": "repo.pushed"})
				first := start(name, false, false)
				final := finish(first.ID)
				if final.ExitCode == nil || *final.ExitCode != 0 {
					t.Fatal(final)
				}
				capture(p.ID, first.ID)
				second := start(name, true, false)
				serveWait(t, held)
				r := result(second.ID)
				if r.Status != store.StatusRunning {
					t.Fatal(r)
				}
				capture(p.ID, second.ID)
				securityDisk(t, f, allSecrets)
				call("cancel", map[string]any{"run": second.ID})
				awaitServeEvent(t, sink, "run.finished", second.ID)
				refused := start(name, false, true)
				refusal := finish(refused.ID)
				if refusal.ExitCode == nil || *refusal.ExitCode == 0 || refusal.Stderr == nil || *refusal.Stderr == "" {
					t.Fatal(refusal)
				}
				capture(p.ID, refused.ID)
				suite := create(suiteName, []string{"suite"})
				unbuilt := start(suiteName, false, false)
				unusable := finish(unbuilt.ID)
				if unusable.ExitCode == nil || *unusable.ExitCode != agent.ExitUnusable || unusable.Stderr == nil || *unusable.Stderr == "" {
					t.Fatal(unusable)
				}
				call("runs", map[string]any{"name": name})
				if r := call("cancel", map[string]any{"run": first.ID}); !r.IsError() {
					t.Fatal("finished cancellation accepted")
				}
				paths := []string{"/", "/about", "/tools", "/_appkit/theme.css", "/" + name + "/", "/" + name, "/missing-security/", "/" + name + "/runs/" + first.ID + "/", "/" + name + "/runs/" + first.ID, "/" + name + "/runs/prr_ffffffffffffffff/"}
				for _, file := range []string{runs.InputFile, runs.StdoutFile, runs.StderrFile, runs.TranscriptFile, "work/", "work/d", "work/env", "work/d/env", "work/cmdline"} {
					paths = append(paths, "/"+name+"/runs/"+first.ID+"/"+file)
				}
				for _, pair := range [][2]string{{second.ID, ""}, {second.ID, runs.StdoutFile}, {refused.ID, ""}, {refused.ID, runs.StderrFile}} {
					path := "/" + name + "/runs/" + pair[0] + "/" + pair[1]
					paths = append(paths, path)
				}
				paths = append(paths, "/"+suite.Name+"/runs/"+unbuilt.ID+"/", "/"+suite.Name+"/runs/"+unbuilt.ID+"/stderr")
				for _, path := range paths {
					req, e := http.NewRequest("GET", "http://backend"+path, nil)
					if e != nil {
						t.Fatal(e)
					}
					req.Header.Set("X-User-Id", "fixture-user")
					response, e := f.http.Do(req)
					if e != nil {
						t.Fatal(path, e)
					}
					body, e := io.ReadAll(response.Body)
					_ = response.Body.Close()
					if e != nil {
						t.Fatal(e)
					}
					securityCheck(t, "GET "+path, body, allSecrets)
					if values := response.Header.Values("Set-Cookie"); len(values) != 0 {
						t.Fatal(path, values)
					}
				}
				deletion := create(other, []string{"files", "bash"})
				running := start(other, true, false)
				serveWait(t, held)
				if r := result(running.ID); r.Status != store.StatusRunning {
					t.Fatal(r)
				}
				capture(deletion.ID, running.ID)
				securityDisk(t, f, allSecrets)
				call("delete", map[string]any{"name": other})
			}
			if code := f.stop(t); code != 0 {
				t.Fatal(code)
			}
			securityDisk(t, f, allSecrets)
			securityCheck(t, "Run stderr", []byte(f.err.String()), allSecrets)
			for _, event := range sink.capture.Events() {
				b, e := event.MarshalJSON()
				if e != nil {
					t.Fatal(e)
				}
				securityCheck(t, "event", b, allSecrets)
			}
		})
	}
}
