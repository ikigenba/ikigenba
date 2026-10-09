package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/runner"
)

// R-856V-HK2B
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == agent.Command {
		os.Exit(agent.Run(context.Background(), agent.Process{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, LookupEnv: os.LookupEnv, Now: func() time.Time { return time.Unix(123, 0) }}))
	}
	os.Exit(m.Run())
}
func model(t *testing.T) string {
	t.Helper()
	for _, entry := range agentkit.Catalog() {
		off, e := agent.Offering(entry.Model)
		if e == nil && off.Host == agentkit.HostAnthropic {
			return entry.Model
		}
	}
	t.Fatal("no Anthropic offering")
	return ""
}
func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func read(t *testing.T, p string) []byte {
	t.Helper()
	b, e := readFixture(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func write(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func records(t *testing.T, dir string) []agentkit.LogRecord {
	t.Helper()
	data := read(t, filepath.Join(dir, "transcript.jsonl"))
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatal("incomplete transcript")
	}
	var records []agentkit.LogRecord
	for _, line := range bytes.Split(data[:len(data)-1], []byte("\n")) {
		var r agentkit.LogRecord
		if e := json.Unmarshal(line, &r); e != nil {
			t.Fatal(e)
		}
		records = append(records, r)
	}
	return records
}
func count(rs []agentkit.LogRecord, kind string) int {
	n := 0
	for _, r := range rs {
		if string(r.Type) == kind {
			n++
		}
	}
	return n
}

type input struct {
	io.Reader
	closed int
	err    error
}

func (s *input) Close() error { s.closed++; return nil }
func (s *input) Read(b []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.Reader.Read(b)
}

type output struct {
	bytes.Buffer
	writes int
}

func (w *output) Write(b []byte) (int, error) { w.writes++; return w.Buffer.Write(b) }

type fixture struct {
	dir, work       string
	env             map[string]string
	lookups         []string
	in              *input
	out, diagnostic output
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if e := os.Mkdir(work, 0700); e != nil {
		t.Fatal(e)
	}
	return &fixture{dir: dir, work: work, env: map[string]string{"IKIGENBA_RUN_DIR": dir, "IKIGENBA_RUN_ID": "run-fixture", "IKIGENBA_WORK_DIR": work}}
}

// R-87MO-93JP R-88UK-MVAE
func (f *fixture) run(t *testing.T, s agent.Spec) int { t.Helper(); return f.raw(marshal(t, s)) }
func (f *fixture) raw(data []byte) int {
	f.in = &input{Reader: bytes.NewReader(data)}
	return agent.Run(context.Background(), agent.Process{Stdin: f.in, Stdout: &f.out, Stderr: &f.diagnostic, LookupEnv: func(k string) (string, bool) { f.lookups = append(f.lookups, k); v, ok := f.env[k]; return v, ok }, Now: func() time.Time { return time.Unix(123, 0) }})
}
func event(w io.Writer, v any) { b, _ := json.Marshal(v); _, _ = fmt.Fprintf(w, "data: %s\n\n", b) }
func answer(w http.ResponseWriter, text string, tool string, args any) {
	w.Header().Set("Content-Type", "text/event-stream")
	event(w, map[string]any{"type": "message_start", "message": map[string]any{"id": "message-fixture", "type": "message", "role": "assistant", "content": []any{}, "usage": map[string]any{"input_tokens": 18}}})
	block := map[string]any{"type": "text", "text": ""}
	delta := map[string]any{"type": "text_delta", "text": text[:len(text)/2]}
	reason := "end_turn"
	if tool != "" {
		block = map[string]any{"type": "tool_use", "id": "call-fixture", "name": tool, "input": map[string]any{}}
		b, _ := json.Marshal(args)
		delta = map[string]any{"type": "input_json_delta", "partial_json": string(b)}
		reason = "tool_use"
	}
	event(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": block})
	event(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": delta})
	event(w, map[string]any{"type": "content_block_stop", "index": 0})
	if tool == "" {
		event(w, map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}})
		event(w, map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": text[len(text)/2:]}})
		event(w, map[string]any{"type": "content_block_stop", "index": 1})
	}
	event(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason}, "usage": map[string]any{"output_tokens": 7}})
	event(w, map[string]any{"type": "message_stop"})
}
func spec(t *testing.T, url string) agent.Spec {
	return agent.Spec{Model: model(t), Prompt: "the author's prompt", Key: "fixture-secret-key", BaseURL: url, MaxToolCalls: 3}
}

// R-86ER-VBT0 R-8A2H-0N13 R-8BAD-EERS R-8CI9-S6IH R-8DQ6-5Y96 R-8EY2-JPZV R-8HDV-B9H9 R-8ILR-P17Y R-8JTO-2SYN R-8L1K-GKPC R-8M9G-UCG1 R-8NHD-846Q R-8OP9-LVXF R-8PX5-ZNO4
func TestPublicHelpers(t *testing.T) {
	s := agent.Spec{Model: "model", Key: "key", System: "system", Prompt: "prompt", Input: json.RawMessage(`{}`), Tools: []string{agent.GroupFiles, agent.GroupBash, agent.GroupSuite}, Schema: json.RawMessage(`{}`), MaxToolCalls: 1, UserID: "user", Email: "email", RequestID: "request", EventID: "event", EventDepth: 2, BaseURL: "url"}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(marshal(t, s), &fields); e != nil {
		t.Fatal(e)
	}
	want := []string{"model", "key", "system", "prompt", "input", "tools", "schema", "max_tool_calls", "user_id", "email", "request_id", "event_id", "event_depth", "base_url"}
	if len(fields) != len(want) {
		t.Fatal(fields)
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Fatal(key)
		}
	}
	if agent.ExitAnswered != 0 || agent.ExitFailed != 1 || agent.ExitLimit != 2 || agent.ExitNoOutput != 3 || agent.ExitUnusable != 4 || agent.GroupFiles != "files" || agent.GroupBash != "bash" || agent.GroupSuite != "suite" {
		t.Fatal("constants")
	}
	for _, c := range []string{agent.Failed, agent.LimitReached, agent.NoOutput, agent.Unusable} {
		if strings.Count(c, "%") != 1 || strings.Count(c, "%s") != 1 {
			t.Fatal(c)
		}
	}
	for _, groups := range [][]string{nil, {}, {agent.GroupSuite, agent.GroupFiles}} {
		if !agent.ValidGroups(groups) {
			t.Fatal(groups)
		}
	}
	for _, groups := range [][]string{{"files", "web"}, {"bash", "bash"}, {""}, {"Files"}} {
		if agent.ValidGroups(groups) {
			t.Fatal(groups)
		}
	}
	for _, entry := range agentkit.Catalog() {
		got, e := agent.Offering(entry.Model)
		want, e2 := agentkit.Lookup(entry.Model, "", "")
		if e != nil || e2 != nil || got.ID != want.ID || got.Host != want.Host || got.WireName != want.WireName || got.WireModel != want.WireModel {
			t.Fatal(entry.Model)
		}
	}
	for _, m := range []string{"", "absent-fixture-model"} {
		if _, e := agent.Offering(m); !errors.Is(e, agentkit.ErrNotFound) {
			t.Fatal(e)
		}
	}
	for h, w := range map[agentkit.Host]string{agentkit.HostAnthropic: "ANTHROPIC_API_KEY", agentkit.HostOpenAI: "OPENAI_API_KEY", agentkit.HostGemini: "GEMINI_API_KEY", agentkit.HostXAI: "XAI_API_KEY", agentkit.HostOpenRouter: "OPENROUTER_API_KEY", "other": ""} {
		if agent.KeyVariable(h) != w {
			t.Fatal(h)
		}
	}
	for _, in := range []string{"", " { } "} {
		if got, e := agent.UserMessage("p", []byte(in)); e != nil || got != "p" {
			t.Fatal(got, e)
		}
	}
	if got, e := agent.UserMessage("p", []byte(`{ "b": 1, "a": [2] }`)); e != nil || got != "p\n\n{\"b\":1,\"a\":[2]}" {
		t.Fatal(got, e)
	}
	for _, in := range []string{`{"a":`, `{} {}`, `x`} {
		if _, e := agent.UserMessage("p", []byte(in)); e == nil {
			t.Fatal(in)
		}
	}
}

// R-8R52-DFET R-8SCY-R75I R-8TKV-4YW7 R-8W0N-WIDL R-8X8K-AA4A R-8YGG-O1UZ R-8ZOD-1TLO R-96ZR-CG1U R-987N-Q7SJ R-9J6R-65GS R-9LMJ-XOY6
func TestRefusals(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); answer(w, "answer", "", nil) }))
	defer server.Close()
	for _, raw := range [][]byte{nil, []byte(`[]`), []byte(`{"model":1}`), append(marshal(t, spec(t, server.URL)), []byte(` {}`)...), append(bytes.TrimSuffix(marshal(t, spec(t, server.URL)), []byte("}")), []byte(`,"api_key":"x"}`)...)} {
		f := setup(t)
		if got := f.raw(raw); got != agent.ExitUnusable || f.in.closed != 1 {
			t.Fatal(got, f.in.closed)
		}
	}
	cases := []func(*agent.Spec, *fixture){func(s *agent.Spec, _ *fixture) { s.Model = "" }, func(s *agent.Spec, _ *fixture) { s.Prompt = "" }, func(s *agent.Spec, _ *fixture) { s.Model = "unknown-model" }, func(s *agent.Spec, _ *fixture) { s.Tools = []string{"bad"} }, func(s *agent.Spec, _ *fixture) { s.MaxToolCalls = -1 }, func(s *agent.Spec, _ *fixture) { s.EventDepth = -1 }, func(s *agent.Spec, _ *fixture) { s.Input = json.RawMessage(`[]`) }, func(s *agent.Spec, _ *fixture) { s.Input = json.RawMessage(`"x"`) }, func(s *agent.Spec, _ *fixture) { s.Input = json.RawMessage(`1`) }, func(s *agent.Spec, _ *fixture) { s.BaseURL = "/relative" }, func(s *agent.Spec, _ *fixture) { s.BaseURL = "ftp://host/path" }, func(s *agent.Spec, _ *fixture) { s.BaseURL = "http:///path" }, func(s *agent.Spec, f *fixture) {
		s.Tools = []string{agent.GroupFiles}
		f.env["IKIGENBA_WORK_DIR"] = filepath.Join(f.dir, "absent")
	}, func(s *agent.Spec, _ *fixture) { s.Tools = []string{agent.GroupSuite} }, func(s *agent.Spec, f *fixture) {
		s.Tools = []string{agent.GroupSuite}
		f.env["IKIGENBA_SERVICES"] = filepath.Join(f.dir, "absent")
	}, func(s *agent.Spec, _ *fixture) { s.Schema = json.RawMessage(`{"$ref":"x"}`) }}
	for _, key := range []string{"IKIGENBA_RUN_DIR", "IKIGENBA_RUN_ID", "IKIGENBA_WORK_DIR"} {
		key := key
		cases = append(cases, func(_ *agent.Spec, f *fixture) { delete(f.env, key) }, func(_ *agent.Spec, f *fixture) { f.env[key] = "" })
	}
	for i, mutate := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := setup(t)
			s := spec(t, server.URL)
			mutate(&s, f)
			if got := f.run(t, s); got != agent.ExitUnusable || f.in.closed != 1 || f.out.Len() != 0 {
				t.Fatal(got, f.out.String())
			}
			if _, e := os.Stat(filepath.Join(f.dir, "transcript.jsonl")); !os.IsNotExist(e) {
				t.Fatal("refusal created transcript", e)
			}
		})
	}
	f := setup(t)
	f.env["IKIGENBA_RUN_DIR"] = filepath.Join(f.dir, "absent")
	if f.run(t, spec(t, server.URL)) != agent.ExitUnusable {
		t.Fatal("missing transcript directory")
	}
	bad := &input{err: errors.New("read\nfailed")}
	var diagnostic output
	code := agent.Run(context.Background(), agent.Process{Stdin: bad, Stderr: &diagnostic})
	if code != agent.ExitUnusable || bad.closed != 1 || diagnostic.writes != 1 || strings.Count(diagnostic.String(), "\n") != 1 {
		t.Fatal(code, bad.closed, diagnostic.String())
	}
	if requests.Load() != 0 {
		t.Fatal(requests.Load())
	}
}

// R-8USR-IQMW R-93C2-74TR R-99FK-3ZJ8 R-9MUG-BGOV R-9QI5-GRWY R-9RQ1-UJNN R-9SXY-8BEC R-9U5U-M351 R-A1H8-WPL7
func TestOneTurn(t *testing.T) {
	for _, system := range []string{"", " \n\t", "author system"} {
		for _, emptyKey := range []bool{false, true} {
			f := setup(t)
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPost || r.URL.Path != "/provider" || f.in.closed != 1 {
					t.Error("request seam", r.Method, r.URL.Path, f.in.closed)
				}
				want := "fixture-secret-key"
				if emptyKey {
					want = ""
				}
				if r.Header.Get("x-api-key") != want {
					t.Error("key")
				}
				answer(w, "exact answer\n", "", nil)
			}))
			s := spec(t, server.URL+"/provider")
			s.System = system
			if strings.TrimSpace(system) != "" {
				s.Input = json.RawMessage(`{ "b":1, "a":[2] }`)
			}
			if emptyKey {
				s.Key = ""
			}
			f.env["IKIGENBA_SERVICES"] = filepath.Join(f.dir, "absent")
			write(t, filepath.Join(f.dir, "transcript.jsonl"), []byte("old"))
			if f.run(t, s) != agent.ExitAnswered || f.out.String() != "exact answer\n" || f.diagnostic.Len() != 0 || requests != 1 {
				t.Fatal(f.out.String(), f.diagnostic.String(), requests)
			}
			server.Close()
			rs := records(t, f.dir)
			if string(rs[0].Type) != "conversation" || string(rs[len(rs)-1].Type) != "summary" {
				t.Fatal(rs)
			}
			for i, r := range rs {
				if r.ID != "run-fixture" || r.Seq != i || !r.Time.Equal(time.Unix(123, 0)) {
					t.Fatal(r)
				}
			}
			off, _ := agent.Offering(s.Model)
			c := rs[0].Conversation
			if c.Identity.Endpoint != string(off.ID) || c.Identity.AuthMode != "api_key" || c.Identity.Model != off.WireModel || !reflect.DeepEqual(c.Settings, agentkit.Settings{}) || c.Limits.MaxToolCalls != s.MaxToolCalls || c.Output != nil || len(c.Tools) != 0 {
				t.Fatal(c)
			}
			systems, users := 0, 0
			turnAt := -1
			for i, r := range rs {
				if string(r.Type) == "turn_start" {
					turnAt = i
				}
				if r.Message != nil {
					switch r.Message.Role {
					case agentkit.RoleSystem:
						systems++
						if turnAt != -1 || len(r.Message.Blocks) != 1 || r.Message.Blocks[0].(agentkit.Text).Text != system {
							t.Fatal(r)
						}
					case agentkit.RoleUser:
						users++
						if i != turnAt+1 || r.Message.Blocks[0].(agentkit.Text).Text != expectedUser(t, s) {
							t.Fatal(r)
						}
					}
				}
			}
			wantSystems := 0
			if strings.TrimSpace(system) != "" {
				wantSystems = 1
			}
			if systems != wantSystems || users != 1 || count(rs, "turn_start") != 1 {
				t.Fatal(systems, users)
			}
			if !reflect.DeepEqual(f.lookups, []string{"IKIGENBA_RUN_DIR", "IKIGENBA_RUN_ID", "IKIGENBA_WORK_DIR"}) {
				t.Fatal(f.lookups)
			}
		}
	}
}

// R-9WLN-DMMF R-9XTJ-RED4 R-A2P5-AHBW R-A54Y-20TA
func TestProviderFailures(t *testing.T) {
	for _, status := range []int{401, 429, 500, 529, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := setup(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if status == 0 {
					h, ok := w.(http.Hijacker)
					if !ok {
						t.Error("hijack")
						return
					}
					conn, _, e := h.Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					_ = conn.Close()
					return
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"provider failure"}}`))
			}))
			defer server.Close()
			if f.run(t, spec(t, server.URL)) != agent.ExitFailed || requests.Load() != 1 || f.out.Len() != 0 || f.diagnostic.writes != 1 {
				t.Fatal(f.diagnostic.String(), requests.Load())
			}
			rs := records(t, f.dir)
			var terminal *agentkit.Error
			for _, r := range rs {
				if r.Err != nil {
					terminal = r.Err
				}
			}
			if terminal == nil {
				t.Fatal("missing error")
			}
			want := "prompts: " + fmt.Sprintf(agent.Failed, strings.ReplaceAll(terminal.Error(), "\n", " ")) + "\n"
			if f.diagnostic.String() != want {
				t.Fatal(f.diagnostic.String(), want)
			}
			if status == 401 && !strings.HasPrefix(terminal.Error(), "auth: ") {
				t.Fatal(terminal)
			}
		})
	}
}

// R-KL59-8LHC R-A09C-IXUI
func TestStructuredOutput(t *testing.T) {
	for _, valid := range []bool{true, false} {
		f := setup(t)
		n := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n++
			text := `{"a": "x"}`
			if !valid {
				text = "not an object"
			}
			answer(w, text, "", nil)
		}))
		s := spec(t, server.URL)
		s.Schema = json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":false}`)
		got := f.run(t, s)
		server.Close()
		if valid {
			if got != agent.ExitAnswered || f.out.String() != `{"a": "x"}` || f.diagnostic.Len() != 0 || n != 1 {
				t.Fatal(got, f.out.String(), f.diagnostic.String())
			}
		} else {
			if got != agent.ExitNoOutput || n != agentkit.DefaultOutputAttempts || f.out.Len() != 0 || f.diagnostic.writes != 1 {
				t.Fatal(got, n, f.diagnostic.String())
			}
			for _, r := range records(t, f.dir) {
				if r.Err != nil {
					if f.diagnostic.String() != "prompts: "+fmt.Sprintf(agent.NoOutput, strings.ReplaceAll(r.Err.Error(), "\n", " "))+"\n" {
						t.Fatal(f.diagnostic.String())
					}
				}
			}
		}
		c := records(t, f.dir)[0].Conversation
		if c.Output == nil || c.Output.MaxAttempts != 0 || !jsonEqual(c.Output.Schema, s.Schema) {
			t.Fatal(c.Output)
		}
	}
}
func jsonEqual(a, b []byte) bool {
	var av, bv any
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}

// R-94JY-KWKG R-9O2C-P8FK R-KJXC-UTQN R-9Z1G-563T R-A6CU-FSJZ R-ZZAG-7Y11
func TestToolRounds(t *testing.T) {
	for _, limit := range []int{0, 1, 3} {
		f := setup(t)
		n := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n++
			if n > 1 {
				rs := records(t, f.dir)
				if count(rs, "usage") != n-1 || count(rs, "tool_use") != n-1 || count(rs, "tool_result") != n-1 {
					t.Error("unflushed records")
				}
			}
			if n <= 3 {
				answer(w, "", "Glob", map[string]any{"pattern": "*"})
			} else {
				answer(w, "done", "", nil)
			}
		}))
		s := spec(t, server.URL)
		s.Tools = []string{agent.GroupFiles}
		s.MaxToolCalls = limit
		got := f.run(t, s)
		server.Close()
		rs := records(t, f.dir)
		wantTools := []string{"Read", "Write", "Edit", "Glob", "Grep"}
		for i, tool := range rs[0].Conversation.Tools {
			if tool.Name != wantTools[i] {
				t.Fatal(tool)
			}
		}
		if len(rs[0].Conversation.Tools) != len(wantTools) {
			t.Fatal(rs[0])
		}
		if limit == 1 {
			if got != agent.ExitLimit || n != 2 || count(rs, "tool_use") != 2 || count(rs, "tool_result") != 1 || count(rs, "limit") != 1 || f.out.Len() != 0 || !strings.Contains(f.diagnostic.String(), agentkit.ErrLimitExceeded.Error()) {
				t.Fatal(got, n, f.diagnostic.String())
			}
		} else {
			if got != agent.ExitAnswered || n != 4 || count(rs, "usage") != 4 || count(rs, "tool_use") != 3 || count(rs, "tool_result") != 3 {
				t.Fatal(got, n)
			}
		}
		for _, r := range rs {
			if string(r.Type) == "usage" && (r.Usage == nil || r.Cost == nil || r.Usage.InputTokens != 18 || r.Usage.OutputTokens != 7 || *r.Cost != expectedCost(t, s.Model, *r.Usage)) {
				t.Fatal(r)
			}
		}
	}
}

func gateway(t *testing.T, f *fixture, handler http.Handler) {
	t.Helper()
	dir, e := os.MkdirTemp("", "gateway-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(dir); e != nil {
			t.Error(e)
		}
	})
	socket := filepath.Join(dir, "mcp.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	path := filepath.Join(f.dir, "services.json")
	write(t, path, marshal(t, map[string]any{"services": []any{map[string]any{"name": "mcp", "url": "http://mcp", "description": "gateway", "socket": socket, "enabled": true, "mcp": true}}}))
	f.env["IKIGENBA_SERVICES"] = path
}
func rpc(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

// R-9ANG-HR9X R-9BVC-VJ0M R-9D39-9ARB R-9FJ2-0U8P
func TestSuiteBridge(t *testing.T) {
	for _, mode := range []string{"ok", "error", "rpc"} {
		for _, identityPresent := range []bool{false, true} {
			f := setup(t)
			listed := 0
			called := 0
			providerCalls := 0
			schema := json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"a":{"anyOf":[{"type":"array","items":{"type":"object","properties":{},"additionalProperties":false}},{"type":"null"}]}},"required":["a"]}`)
			clean := json.RawMessage(`{"type":"object","properties":{"a":{"anyOf":[{"type":"array","items":{"type":"object","properties":{}}},{"type":"null"}]}},"required":["a"]}`)
			gateway(t, f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/mcp" || f.in.closed != 1 {
					t.Error("gateway seam")
				}
				for k, v := range map[string]string{"X-User-Id": "user-fixture", "X-User-Email": "email-fixture", "X-Request-Id": "request-fixture", "X-Event-Cause": "event-fixture", "X-Event-Depth": "2"} {
					if !identityPresent {
						v = ""
					}
					if r.Header.Get(k) != v {
						t.Error(k, r.Header.Get(k), v)
					}
				}
				var req struct {
					ID     any
					Method string
					Params map[string]json.RawMessage
				}
				if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
					t.Error(e)
					return
				}
				switch req.Method {
				case "tools/list":
					listed++
					if providerCalls != 0 {
						t.Error("late listing")
					}
					if listed == 1 {
						rpc(w, req.ID, map[string]any{"tools": []any{map[string]any{"name": "suite_tool", "description": "gateway description", "inputSchema": schema}}, "nextCursor": "next-page"})
					} else {
						if string(req.Params["cursor"]) != `"next-page"` {
							t.Error(req.Params)
						}
						rpc(w, req.ID, map[string]any{"tools": []any{}})
					}
				case "tools/call":
					called++
					if string(req.Params["name"]) != `"suite_tool"` || !jsonEqual(req.Params["arguments"], []byte(`{"a":null}`)) {
						t.Error(req.Params)
					}
					switch mode {
					case "rpc":
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "gateway failed"}})
					case "error":
						rpc(w, req.ID, mcp.ErrorResult("tool failed"))
					default:
						rpc(w, req.ID, mcp.TextResult("tool answered"))
					}
				default:
					t.Error(req.Method)
				}
			}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				providerCalls++
				if listed != 2 {
					t.Error("listing incomplete")
				}
				if providerCalls == 1 {
					answer(w, "", "suite_tool", map[string]any{"a": nil})
				} else {
					answer(w, "done", "", nil)
				}
			}))
			s := spec(t, server.URL)
			s.Tools = []string{agent.GroupSuite, agent.GroupFiles}
			if identityPresent {
				s.UserID = "user-fixture"
				s.Email = "email-fixture"
				s.RequestID = "request-fixture"
				s.EventID = "event-fixture"
				s.EventDepth = 2
			}
			got := f.run(t, s)
			server.Close()
			if got != agent.ExitAnswered || providerCalls != 2 || listed != 2 || called != 1 {
				t.Fatal(got, f.diagnostic.String(), providerCalls, called)
			}
			rs := records(t, f.dir)
			tool := rs[0].Conversation.Tools[5]
			if tool.Name != "suite_tool" || tool.Description != "gateway description" || !jsonEqual(tool.Schema, clean) {
				t.Fatal(tool)
			}
			for _, r := range rs {
				if r.ToolResult != nil {
					want := ""
					if mode == "rpc" {
						want = "mcp: rpc error -32000: gateway failed"
					} else {
						result := mcp.TextResult("tool answered")
						if mode == "error" {
							result = mcp.ErrorResult("tool failed")
						}
						b, e := result.MarshalJSON()
						if e != nil {
							t.Fatal(e)
						}
						want = string(b)
					}
					if r.ToolResult.Content != want || r.ToolResult.IsError != (mode != "ok") {
						t.Fatal(r.ToolResult, want)
					}
				}
			}
		}
	}
}

// R-9EB5-N2I0 R-9HYU-SDQ3
func TestSuiteRefusals(t *testing.T) {
	for _, mode := range []string{"malformed", "http", "rpc", "bad-schema", "duplicate", "no-entry", "disabled", "empty-socket", "refused"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			var providerCalls int
			gateway(t, f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				switch mode {
				case "malformed":
					_, _ = w.Write([]byte(`{}`))
				case "http":
					w.WriteHeader(503)
				case "rpc":
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "error": map[string]any{"code": -32000, "message": "listing failed"}})
				default:
					name := "x"
					schema := json.RawMessage(`{"type":"object","properties":{}}`)
					if mode == "bad-schema" {
						schema = json.RawMessage(`{"$ref":"x"}`)
					}
					if mode == "duplicate" {
						name = "Read"
					}
					rpc(w, req["id"], map[string]any{"tools": []any{map[string]any{"name": name, "description": "description", "inputSchema": schema}}})
				}
			}))
			switch mode {
			case "no-entry":
				write(t, f.env["IKIGENBA_SERVICES"], []byte(`{"services":[]}`))
			case "disabled", "empty-socket", "refused":
				socket := filepath.Join(f.dir, "absent")
				enabled := mode != "disabled"
				if mode == "empty-socket" {
					socket = ""
				}
				write(t, f.env["IKIGENBA_SERVICES"], marshal(t, map[string]any{"services": []any{map[string]any{"name": "mcp", "url": "http://mcp", "description": "gateway", "socket": socket, "enabled": enabled, "mcp": true}}}))
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { providerCalls++; answer(w, "done", "", nil) }))
			defer server.Close()
			s := spec(t, server.URL)
			s.Tools = []string{agent.GroupFiles, agent.GroupSuite}
			if f.run(t, s) != agent.ExitUnusable || providerCalls != 0 {
				t.Fatal(f.diagnostic.String(), providerCalls)
			}
			if mode != "duplicate" {
				if _, e := os.Stat(filepath.Join(f.dir, "transcript.jsonl")); !os.IsNotExist(e) {
					t.Fatal(e)
				}
			}
		})
	}
}

func child(t *testing.T, f *fixture, s agent.Spec, path string) (int, string, string) {
	t.Helper()
	var out, errout bytes.Buffer
	env := []string{"HOME=" + f.dir, "PATH=" + path, "IKIGENBA_RUN_DIR=" + f.dir, "IKIGENBA_RUN_ID=child-fixture", "IKIGENBA_WORK_DIR=" + f.work}
	if services, ok := f.env["IKIGENBA_SERVICES"]; ok {
		env = append(env, "IKIGENBA_SERVICES="+services)
	}
	p, e := runner.Start(context.Background(), runner.Spec{Dir: f.dir, Env: env, Stdin: marshal(t, s), Stdout: &out, Stderr: &errout})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	select {
	case code := <-done:
		return code, out.String(), errout.String()
	case <-time.After(10 * time.Second):
		p.Kill()
		p.Wait()
		t.Fatal("child deadline")
		return 0, "", ""
	}
}

// R-95RU-YOB5 R-TFCS-MOSG R-KMD5-MD81 R-MPCQ-SKBT
func TestRealChildToolsAndCredential(t *testing.T) {
	bash, e := exec.LookPath("bash")
	if e != nil {
		t.Fatal(e)
	}
	f := setup(t)
	key := "aZ9!fixture-secret.Mixed_456"
	var n int
	gateway(t, f, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		noKeyFragment(t, key, data)
		for _, vs := range r.Header {
			for _, v := range vs {
				noKeyFragment(t, key, []byte(v))
			}
		}
		var req map[string]any
		if e = json.Unmarshal(data, &req); e != nil {
			t.Error(e)
		}
		if req["method"] == "tools/list" {
			rpc(w, req["id"], map[string]any{"tools": []any{map[string]any{"name": "suite_tool", "description": "description", "inputSchema": json.RawMessage(`{"type":"object","properties":{}}`)}}})
		} else {
			rpc(w, req["id"], mcp.TextResult("ok"))
		}
	}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		switch n {
		case 1:
			answer(w, "", "Write", map[string]any{"file_path": "a.txt", "content": "file content"})
		case 2:
			answer(w, "", "Bash", map[string]any{"command": "pwd; env; while IFS= read -r line; do printf '%s\\n' \"$line\"; done < \"$IKIGENBA_RUN_DIR/transcript.jsonl\" > copy.jsonl; if { : < /proc/$PPID/environ; } 2>/dev/null; then printf 'ENV_OPENED'; else printf 'ENV_DENIED'; fi; if { : < /proc/$PPID/mem; } 2>/dev/null; then printf 'MEM_OPENED'; else printf 'MEM_DENIED'; fi"})
		case 3:
			answer(w, "", "suite_tool", map[string]any{})
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"denied"}}`))
		}
	}))
	defer server.Close()
	s := spec(t, server.URL)
	s.Key = key
	s.Tools = []string{agent.GroupSuite, agent.GroupBash, agent.GroupFiles}
	code, out, diagnostic := child(t, f, s, filepath.Dir(bash))
	if code != agent.ExitFailed || out != "" || !strings.Contains(diagnostic, "auth:") {
		t.Fatal(code, out, diagnostic)
	}
	noKeyFragment(t, key, []byte(out), []byte(diagnostic))
	if string(read(t, filepath.Join(f.work, "a.txt"))) != "file content" {
		t.Fatal("Write root")
	}
	copyData := read(t, filepath.Join(f.work, "copy.jsonl"))
	var found bool
	for _, line := range bytes.Split(copyData, []byte("\n")) {
		var r agentkit.LogRecord
		if json.Unmarshal(line, &r) == nil && r.ToolUse != nil && r.ToolUse.Name == "Bash" {
			found = true
		}
	}
	if !found {
		t.Fatal("tool_use was not durable before dispatch")
	}
	rs := records(t, f.dir)
	names := []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash", "suite_tool"}
	for i, tool := range rs[0].Conversation.Tools {
		if tool.Name != names[i] {
			t.Fatal(tool)
		}
	}
	var bashResult string
	for _, r := range rs {
		if r.ToolResult != nil && strings.Contains(r.ToolResult.Content, f.work) {
			bashResult = r.ToolResult.Content
		}
	}
	if !strings.Contains(bashResult, "ENV_DENIED") || !strings.Contains(bashResult, "MEM_DENIED") || !strings.Contains(bashResult, "HOME="+f.dir) {
		t.Fatal(bashResult)
	}
	if e = filepath.WalkDir(f.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			data, err := readFixture(path)
			if err != nil {
				return err
			}
			for i := 0; i+8 <= len(key); i++ {
				if bytes.Contains(data, []byte(key[i:i+8])) {
					return fmt.Errorf("credential in %s", path)
				}
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func noKeyFragment(t *testing.T, key string, data ...[]byte) {
	t.Helper()
	for _, b := range data {
		for i := 0; i+8 <= len(key); i++ {
			if bytes.Contains(b, []byte(key[i:i+8])) {
				t.Error("credential fragment in child output")
			}
		}
	}
}

// R-MPCQ-SKBT
func TestRealChildCredentialTransportFailure(t *testing.T) {
	models := map[agentkit.Host]string{}
	for _, entry := range agentkit.Catalog() {
		off, err := agent.Offering(entry.Model)
		if err == nil && models[off.Host] == "" {
			models[off.Host] = entry.Model
		}
	}
	if models[agentkit.HostGemini] == "" {
		t.Fatal("no Gemini offering")
	}
	for host, model := range models {
		t.Run(string(host), func(t *testing.T) {
			f := setup(t)
			key := "aZ9!fixture-secret.Mixed_456"
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				// The provider receives the credential; the failure response never echoes it.
				found := false
				for _, values := range r.Header {
					for _, v := range values {
						found = found || strings.Contains(v, key)
					}
				}
				if !found {
					t.Error("provider did not receive credential")
				}
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer server.Close()
			s := spec(t, server.URL)
			s.Model, s.Key = model, key
			code, out, diagnostic := child(t, f, s, f.work)
			if code != agent.ExitFailed || requests.Load() == 0 || diagnostic == "" {
				t.Fatal(code, requests.Load(), diagnostic)
			}
			noKeyFragment(t, key, []byte(out), []byte(diagnostic))
			if err := filepath.WalkDir(f.dir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					noKeyFragment(t, key, read(t, path))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// R-96ZR-CG1U
func TestMissingBash(t *testing.T) {
	f := setup(t)
	s := spec(t, "http://127.0.0.1:1/provider")
	s.Tools = []string{agent.GroupBash}
	code, _, _ := child(t, f, s, f.work)
	if code != agent.ExitUnusable {
		t.Fatal(code)
	}
	if _, e := os.Stat(filepath.Join(f.dir, "transcript.jsonl")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

func readFixture(path string) ([]byte, error) {
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}

func expectedUser(t *testing.T, s agent.Spec) string {
	t.Helper()
	v, e := agent.UserMessage(s.Prompt, s.Input)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func expectedCost(t *testing.T, model string, usage agentkit.Usage) agentkit.Cost {
	t.Helper()
	off, e := agent.Offering(model)
	if e != nil {
		t.Fatal(e)
	}
	return off.Pricing.Cost(usage)
}
