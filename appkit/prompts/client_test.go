package prompts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func writer(t *testing.T) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	var diagnostics bytes.Buffer
	w := telemetry.New(telemetry.Config{Service: "reader", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return time.Unix(100, 0) }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 256)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected retry") }})
	t.Cleanup(func() {
		w.Shutdown(context.Background(), "test done")
		if diagnostics.Len() != 0 {
			t.Error(diagnostics.String())
		}
	})
	return w, capture
}
func server(t *testing.T, handler http.HandlerFunc) services.Entry {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close(); <-done })
	return services.Entry{Name: "prompts-42", Socket: socket, URL: "invalid public URL"}
}
func request(t *testing.T, r *http.Request) map[string]json.RawMessage {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&root); err != nil {
		t.Error(err)
	}
	return root
}
func respond(w http.ResponseWriter, r map[string]json.RawMessage, result string) {
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, r["id"], result)
}
func answer(t *testing.T, status int, media, result string) *prompts.Client {
	t.Helper()
	entry := server(t, func(w http.ResponseWriter, r *http.Request) {
		root := request(t, r)
		w.Header().Set("Content-Type", media)
		w.WriteHeader(status)
		if media == "text/event-stream" {
			_, _ = io.WriteString(w, "data: ")
			respond(w, root, result)
			_, _ = io.WriteString(w, "\n\n")
		} else {
			respond(w, root, result)
		}
	})
	w, _ := writer(t)
	return prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w})
}

// R-BXFQ-IX26
func call(ctx context.Context, t *testing.T, c *prompts.Client, operation string, value string, input json.RawMessage) (any, error) {
	t.Helper()
	var got, zero any
	var err error
	switch operation {
	case "show":
		got, err = c.Show(ctx, value)
		zero = prompts.Prompt{}
	case "run":
		got, err = c.Run(ctx, value, input)
		zero = prompts.Start{}
	case "result":
		got, err = c.Result(ctx, value)
		zero = prompts.Outcome{}
	default:
		t.Fatal(operation)
	}
	if err != nil && !reflect.DeepEqual(got, zero) {
		t.Errorf("error returned partial output: %#v: %v", got, err)
	}
	return got, err
}
func malformed(t *testing.T, err error) {
	t.Helper()
	var refusal *prompts.Refusal
	var httpError *prompts.HTTPError
	var rpc *mcp.RPCError
	if err == nil || !strings.HasPrefix(err.Error(), "prompts: ") || errors.As(err, &refusal) || errors.As(err, &httpError) || errors.As(err, &rpc) {
		t.Fatalf("malformed classification: %v", err)
	}
}

const validPrompt = `{"id":"p","name":"name","model":"model","prompt":"body","system":"system","tools":[]}`
const validStart = `{"id":"r","status":"future-status"}`
const validOutcome = `{"id":"r","prompt":"name","model":"model","status":"future-status","started":"2030-01-02T03:04:05+02:00"}`

func output(operation string) string {
	switch operation {
	case "show":
		return validPrompt
	case "run":
		return validStart
	default:
		return validOutcome
	}
}
func mutate(t *testing.T, body, key string, value json.RawMessage, remove bool) string {
	t.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &members); err != nil {
		t.Fatal(err)
	}
	if remove {
		delete(members, key)
	} else {
		members[key] = value
	}
	b, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// R-XZX4-55SZ R-AZAJ-TCAQ R-B0IG-741F R-B1QC-KVS4 R-B2Y8-YNIT R-B465-CF9I R-B5E1-Q707 R-B6LY-3YQW R-B7TU-HQHL
func TestPublicTypes(t *testing.T) {
	w, _ := writer(t)
	constructors := []func(prompts.ClientConfig) *prompts.Client{prompts.NewClient}
	cfg := prompts.ClientConfig{services.Entry{}, w}
	var cfgShape struct {
		Service   services.Entry
		Telemetry *telemetry.Writer
	} = cfg
	c := constructors[0](cfg)
	show := []func(context.Context, string) (prompts.Prompt, error){c.Show}
	run := []func(context.Context, string, json.RawMessage) (prompts.Start, error){c.Run}
	result := []func(context.Context, string) (prompts.Outcome, error){c.Result}
	p := prompts.Prompt{"id", "name", "model", "prompt", "system", []string{"tool"}}
	var ps struct {
		ID, Name, Model, Prompt, System string
		Tools                           []string
	} = p
	st := prompts.Start{"id", "status", "reason"}
	var ss struct{ ID, Status, Reason string } = st
	code := 3
	out := prompts.Outcome{"id", "prompt", "model", "status", "reason", &code, time.Unix(1, 0), time.Unix(2, 0), "stdout", 1, 2, 3, 4, 5}
	var os struct {
		ID, Prompt, Model, Status, Reason                                   string
		ExitCode                                                            *int
		Started, Finished                                                   time.Time
		Stdout                                                              string
		InputTokens, CachedTokens, OutputTokens, ReasoningTokens, CostNanos int64
	} = out
	ref := prompts.Refusal{"text"}
	var rs struct{ Text string } = ref
	he := prompts.HTTPError{599, "body"}
	var hs struct {
		StatusCode int
		Body       string
	} = he
	errorMethods := []func() string{ref.Error, he.Error}
	if cfgShape.Telemetry != w || c == nil || show[0] == nil || run[0] == nil || result[0] == nil || !reflect.DeepEqual(prompts.Prompt(ps), p) || prompts.Start(ss) != st || prompts.Outcome(os) != out || rs.Text != "text" || hs.StatusCode != 599 || hs.Body != "body" || errorMethods[0]() == "" || errorMethods[1]() == "" {
		t.Fatal("public shape")
	}
}

// R-B91Q-VI8A
func TestConstruction(t *testing.T) {
	func() {
		defer func() {
			if v := recover(); !strings.Contains(fmt.Sprint(v), "telemetry writer is nil") {
				t.Errorf("panic=%v", v)
			}
		}()
		prompts.NewClient(prompts.ClientConfig{})
		t.Error("no panic")
	}()
	w, capture := writer(t)
	var contacted atomic.Int32
	live := server(t, func(out http.ResponseWriter, _ *http.Request) { contacted.Add(1); out.WriteHeader(500) })
	for _, entry := range []services.Entry{{}, {Name: "invalid /", Socket: filepath.Join(t.TempDir(), "absent")}, live} {
		if prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w}) == nil {
			t.Fatal("nil client")
		}
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if contacted.Load() != 0 || len(capture.Events()) != 0 {
		t.Fatal("constructor performed I/O")
	}
}

// R-BA9N-99YZ R-BBHJ-N1PO R-BDXC-EL72 R-BGD5-64OG R-BHL1-JWF5 R-BISX-XO5U
func TestRequests(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		for _, value := range []string{"", "a/b?% 日本\n\""} {
			for _, caller := range []*identity.Caller{nil, {UserID: "user", Email: "email", RequestID: "request"}, {RequestID: "partial"}} {
				for _, input := range []json.RawMessage{nil, json.RawMessage(` {"arbitrary":[null,1,"x"],"name":false} `)} {
					t.Run(fmt.Sprint(operation, value, caller, string(input)), func(t *testing.T) {
						var calls atomic.Int32
						entry := server(t, func(out http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							root := request(t, r)
							if r.Method != "POST" || r.URL.Path != "/mcp" || r.URL.RawQuery != "" || r.Host != "prompts-42" {
								t.Error("request", r)
							}
							for name, want := range map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "MCP-Protocol-Version": mcp.ProtocolVersion, "Mcp-Method": "tools/call", "Mcp-Name": operation} {
								if r.Header.Get(name) != want {
									t.Error(name, r.Header.Values(name))
								}
							}
							expected := &http.Request{Header: make(http.Header)}
							if caller != nil {
								identity.Forward(*caller, expected)
							}
							for _, name := range []string{"X-User-Id", "X-User-Email", "X-Request-Id", "X-Event-Cause", "X-Event-Depth"} {
								if !reflect.DeepEqual(r.Header.Values(name), expected.Header.Values(name)) {
									t.Error(name, r.Header.Values(name))
								}
							}
							var id int64
							var method, version string
							var params map[string]json.RawMessage
							if len(root) != 4 || json.Unmarshal(root["id"], &id) != nil || json.Unmarshal(root["method"], &method) != nil || method != "tools/call" || json.Unmarshal(root["jsonrpc"], &version) != nil || version != "2.0" || json.Unmarshal(root["params"], &params) != nil || len(params) != 3 {
								t.Error("RPC shape", root)
							}
							var name string
							var args map[string]json.RawMessage
							var meta map[string]json.RawMessage
							if json.Unmarshal(params["name"], &name) != nil || name != operation || json.Unmarshal(params["arguments"], &args) != nil || json.Unmarshal(params["_meta"], &meta) != nil {
								t.Error("params", params)
							}
							key := "name"
							if operation == "result" {
								key = "run"
							}
							expectedArgs := map[string]any{key: value}
							if operation == "run" && input != nil {
								expectedArgs["input"] = input
							}
							want, _ := json.Marshal(expectedArgs)
							var a, b any
							_ = json.Unmarshal(params["arguments"], &a)
							_ = json.Unmarshal(want, &b)
							if !reflect.DeepEqual(a, b) {
								t.Error("arguments", a, b)
							}
							var protocolVersion string
							var capabilities map[string]json.RawMessage
							if json.Unmarshal(meta["io.modelcontextprotocol/protocolVersion"], &protocolVersion) != nil || protocolVersion != mcp.ProtocolVersion || json.Unmarshal(meta["io.modelcontextprotocol/clientCapabilities"], &capabilities) != nil || capabilities == nil || len(capabilities) != 0 {
								t.Error("meta", meta)
							}
							out.Header().Set("Content-Type", "application/json")
							respond(out, root, `{"isError":true}`)
						})
						w, capture := writer(t)
						c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w})
						ctx := events.NewContext(context.Background(), events.Cause{ID: "cause", Depth: 3})
						if caller != nil {
							ctx = identity.NewContext(ctx, *caller)
						}
						_, err := call(ctx, t, c, operation, value, input)
						var ref *prompts.Refusal
						if !errors.As(err, &ref) {
							t.Fatal(err)
						}
						if err := w.Flush(context.Background()); err != nil {
							t.Fatal(err)
						}
						trail := capture.Events()
						if calls.Load() != 1 || len(trail) != 1 {
							t.Fatal(calls.Load(), trail)
						}
						user, req := "", ""
						if caller != nil {
							user, req = caller.UserID, caller.RequestID
						}
						e := trail[0]
						if e.Name != "sibling.called" || e.Service != "reader" || e.User != user || e.RequestID != req || !e.Time.Equal(time.Unix(100, 0)) || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"target": entry.Name, "method": "POST", "path": "/mcp", "status": int64(200), "duration_us": int64(0)}) {
							t.Fatal(e)
						}
					})
				}
			}
		}
	}
}

// R-BF58-SCXR
func TestInvalidInput(t *testing.T) {
	var calls atomic.Int32
	entry := server(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	w, capture := writer(t)
	c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w})
	for _, body := range []string{"", " ", "null", "[]", "1", "true", `"x"`, `{`, `{} {}`, `{} null`, `{"x":NaN}`, string([]byte{123, 34, 120, 34, 58, 34, 255, 34, 125})} {
		_, err := call(context.Background(), t, c, "run", "", json.RawMessage(body))
		var refusal *prompts.Refusal
		var httpError *prompts.HTTPError
		if err == nil || !strings.HasPrefix(err.Error(), "prompts: ") || errors.As(err, &refusal) || errors.As(err, &httpError) {
			t.Fatalf("invalid-input classification: %v", err)
		}
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || len(capture.Events()) != 0 {
		t.Fatal("invalid input sent", calls.Load(), capture.Events())
	}
}

// R-9NV6-1BD9 R-Q3LM-V5MJ R-Q4TJ-8XD8 R-Q61F-MP3X
func TestSuccess(t *testing.T) {
	started, _ := time.Parse(time.RFC3339, "2030-01-02T03:04:05+02:00")
	finished, _ := time.Parse(time.RFC3339, "2030-01-02T08:09:10-03:00")
	code := 0
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, operation := range []string{"show", "run", "result"} {
			for _, full := range []bool{false, true} {
				body := output(operation)
				var expected any
				switch operation {
				case "show":
					expected = prompts.Prompt{ID: "p", Name: "name", Model: "model", Prompt: "body", System: "system", Tools: []string{}}
					if full {
						body = mutate(t, body, "tools", json.RawMessage(`["z","a","z",""]`), false)
						p := expected.(prompts.Prompt)
						p.Tools = []string{"z", "a", "z", ""}
						expected = p
					}
				case "run":
					expected = prompts.Start{ID: "r", Status: "future-status"}
					if full {
						body = mutate(t, body, "reason", json.RawMessage(`"because\n理由"`), false)
						s := expected.(prompts.Start)
						s.Reason = "because\n理由"
						expected = s
					}
				case "result":
					expected = prompts.Outcome{ID: "r", Prompt: "name", Model: "model", Status: "future-status", Started: started.UTC()}
					if full {
						body = strings.TrimSuffix(body, "}") + `,"reason":"because","stdout":"answer\n日本","finished":"2030-01-02T08:09:10-03:00","exit_code":0,"input_tokens":9223372036854775807,"cached_tokens":-1,"output_tokens":2,"reasoning_tokens":3,"cost_nanos":-9223372036854775808}`
						expected = prompts.Outcome{ID: "r", Prompt: "name", Model: "model", Status: "future-status", Reason: "because", ExitCode: &code, Started: started.UTC(), Finished: finished.UTC(), Stdout: "answer\n日本", InputTokens: 9223372036854775807, CachedTokens: -1, OutputTokens: 2, ReasoningTokens: 3, CostNanos: -9223372036854775808}
					}
				}
				// Unknown and case-distinct members must not affect known members; duplicate output members use their last value.
				body = `{"id":null,` + strings.TrimPrefix(strings.TrimSuffix(body, "}"), "{") + `,"ID":null,"STATUS":{},"future":[null,{"x":1}]}`
				for _, isError := range []string{"false", "null", "1", `"true"`} {
					result := `{"isError":` + isError + `,"structuredContent":` + body + `,"content":[{"type":"text","text":"invalid fallback"}]}`
					if full {
						result = `{"resultType":"complete",` + strings.TrimPrefix(result, "{")
					}
					got, err := call(context.Background(), t, answer(t, 200, media, result), operation, "", nil)
					if err != nil || !reflect.DeepEqual(got, expected) {
						t.Fatalf("%s %s full=%v got=%#v want=%#v err=%v", media, operation, full, got, expected, err)
					}
				}
			}
		}
	}
	// Empty strings are valid and there is no validation of statuses or names.
	for _, operation := range []string{"show", "run", "result"} {
		body := output(operation)
		var members map[string]json.RawMessage
		_ = json.Unmarshal([]byte(body), &members)
		for key, value := range members {
			if len(value) > 0 && value[0] == '"' && key != "started" {
				body = mutate(t, body, key, json.RawMessage(`""`), false)
			}
		}
		if _, err := call(context.Background(), t, answer(t, 200, "application/json", `{"structuredContent":`+body+`}`), operation, "", nil); err != nil {
			t.Fatal(err)
		}
	}
}

// R-24M4-XHV9 R-9NV6-1BD9
func TestMalformedStructuredOutputs(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		valid := output(operation)
		results := []string{`{}`, `{"structuredContent":null}`, `{"structuredContent":[]}`, `{"structuredContent":"text"}`, `{"structuredContent":1}`, `{"structuredContent":true}`, `{"structuredContent":{},"content":[{"type":"text","text":` + fmt.Sprintf("%q", valid) + `}]}`}
		required := []string{"id", "status"}
		optional := []string{"reason"}
		if operation == "show" {
			required = []string{"id", "name", "model", "prompt", "system", "tools"}
			optional = nil
		}
		if operation == "result" {
			required = []string{"id", "prompt", "model", "status", "started"}
			optional = []string{"reason", "stdout", "finished", "exit_code", "input_tokens", "cached_tokens", "output_tokens", "reasoning_tokens", "cost_nanos"}
		}
		for _, key := range required {
			results = append(results, `{"structuredContent":`+mutate(t, valid, key, nil, true)+`}`)
			caseOnly := mutate(t, valid, key, nil, true)
			caseOnly = mutate(t, caseOnly, strings.ToUpper(key), json.RawMessage(`"case only"`), false)
			results = append(results, `{"structuredContent":`+caseOnly+`}`)
		}
		for _, key := range append(required, optional...) {
			bad := []string{"null", "true", "[]", "{}", "1", `"wrong"`}
			if key == "tools" {
				bad = append(bad, `[null]`, `[1]`, `["ok",false]`, `[{}]`)
			}
			for _, value := range bad {
				if key == "tools" && value == "[]" {
					continue
				}
				// A string is valid except where the contract requires a timestamp, number or tools array.
				numeric := key == "exit_code" || strings.HasSuffix(key, "tokens") || key == "cost_nanos"
				if value == `"wrong"` && key != "tools" && key != "started" && key != "finished" && !numeric {
					continue
				}
				if value == "1" && numeric {
					continue
				}
				results = append(results, `{"structuredContent":`+mutate(t, valid, key, json.RawMessage(value), false)+`}`)
			}
			if key == "exit_code" || strings.HasSuffix(key, "tokens") || key == "cost_nanos" {
				for _, value := range []string{"1.5", "1e2", "9223372036854775808", "-9223372036854775809"} {
					results = append(results, `{"structuredContent":`+mutate(t, valid, key, json.RawMessage(value), false)+`}`)
				}
			}
		}
		// Last duplicate wins even when the final occurrence is invalid.
		results = append(results, `{"structuredContent":`+strings.TrimSuffix(valid, "}")+`,"id":null}}`)
		// Duplicate result members are forbidden even when they would otherwise be a refusal.
		results = append(results, `{"isError":true,"isError":true}`, `{"structuredContent":`+valid+`,"structuredContent":`+valid+`}`)
		for i, result := range results {
			t.Run(fmt.Sprint(operation, "/", i), func(t *testing.T) {
				_, err := call(context.Background(), t, answer(t, 200, "application/json", result), operation, "id", nil)
				malformed(t, err)
			})
		}
	}
}

// R-BOWF-UIVB R-BQ4C-8AM0
func TestRefusals(t *testing.T) {
	fixtures := []struct{ result, want string }{
		{`{"isError":true}`, ""},
		{`{"isError":true,"content":null}`, ""},
		{`{"isError":true,"content":{}}`, ""},
		{`{"isError":true,"content":[{"type":"text","text":"first"},{"type":"image","text":"ignore"},{"type":"text","text":null},null,1,[],{"Type":"text","text":"ignore"},{"type":"text","Text":"ignore"},{"type":"text","text":""},{"type":"text","text":"last\nline"}],"structuredContent":{}}`, "first\n\nlast\nline"},
		{`{"isError":true,"content":[{"type":"text","text":"old","text":"new"},{"type":"image","type":"text","text":"second"}]}`, "new\nsecond"},
	}
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, operation := range []string{"show", "run", "result"} {
			for _, fixture := range fixtures {
				_, err := call(context.Background(), t, answer(t, 200, media, fixture.result), operation, "id", nil)
				var ref *prompts.Refusal
				var he *prompts.HTTPError
				if !errors.As(err, &ref) || ref.Text != fixture.want || err.Error() != "prompts: "+fixture.want || errors.As(err, &he) {
					t.Fatalf("refusal=%#v err=%v want=%q", ref, err, fixture.want)
				}
			}
		}
	}
	for _, text := range []string{"", "arbitrary\n日本", "not found is just text"} {
		if (&prompts.Refusal{Text: text}).Error() != "prompts: "+text {
			t.Fatal(text)
		}
	}
}

// R-BBHJ-N1PO
func TestRequestIDsUniqueAcrossCalls(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[int64]bool)
	entry := server(t, func(w http.ResponseWriter, r *http.Request) {
		root := request(t, r)
		var id int64
		if err := json.Unmarshal(root["id"], &id); err != nil {
			t.Error(err)
		}
		mu.Lock()
		if seen[id] {
			t.Errorf("request id reused: %d", id)
		}
		seen[id] = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		respond(w, root, `{"isError":true}`)
	})
	w, _ := writer(t)
	c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w})
	var wg sync.WaitGroup
	for range 8 {
		for _, operation := range []string{"show", "run", "result"} {
			wg.Go(func() {
				_, err := call(context.Background(), t, c, operation, "name", nil)
				var ref *prompts.Refusal
				if !errors.As(err, &ref) {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 24 {
		t.Fatal("request ids", len(seen))
	}
}

// R-24M4-XHV9
func TestMalformedProtocolResponses(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		for _, media := range []string{"application/json", "text/event-stream"} {
			for _, mode := range []string{"wrong-id", "result-type", "non-object-result", "no-response", "invalid-event"} {
				if media == "application/json" && (mode == "no-response" || mode == "invalid-event") {
					continue
				}
				t.Run(operation+"/"+media+"/"+mode, func(t *testing.T) {
					entry := server(t, func(w http.ResponseWriter, r *http.Request) {
						root := request(t, r)
						w.Header().Set("Content-Type", media)
						if mode == "no-response" {
							_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notice\"}\n\n")
							return
						}
						if mode == "invalid-event" {
							_, _ = io.WriteString(w, "data: not JSON\n\n")
							return
						}
						result := `{"structuredContent":` + output(operation) + `}`
						switch mode {
						case "wrong-id":
							root["id"] = json.RawMessage(`"different-request"`)
						case "result-type":
							result = `{"resultType":"future-type","structuredContent":` + output(operation) + `}`
						case "non-object-result":
							result = `[]`
						}
						if media == "text/event-stream" {
							_, _ = io.WriteString(w, "data: ")
						}
						respond(w, root, result)
						if media == "text/event-stream" {
							_, _ = io.WriteString(w, "\n\n")
						}
					})
					w, _ := writer(t)
					c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w})
					_, err := call(context.Background(), t, c, operation, "name", nil)
					malformed(t, err)
				})
			}
		}
	}
}

// R-BHL1-JWF5
func TestTelemetryDuration(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		for _, elapsed := range []time.Duration{789*time.Microsecond + 900*time.Nanosecond, -time.Microsecond} {
			t.Run(operation+elapsed.String(), func(t *testing.T) {
				start := time.Unix(100, 0)
				var current atomic.Int64
				current.Store(start.UnixNano())
				var diagnostics bytes.Buffer
				capture := &telemetry.Capture{}
				w := telemetry.New(telemetry.Config{Service: "reader", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return time.Unix(0, current.Load()) }, Rand: bytes.NewReader(make([]byte, 16)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected retry") }})
				t.Cleanup(func() {
					w.Shutdown(context.Background(), "done")
					if diagnostics.Len() != 0 {
						t.Error(diagnostics.String())
					}
				})
				entry := server(t, func(out http.ResponseWriter, r *http.Request) {
					root := request(t, r)
					current.Store(start.Add(elapsed).UnixNano())
					out.Header().Set("Content-Type", "application/json")
					respond(out, root, `{"isError":true}`)
				})
				c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: w})
				_, err := call(context.Background(), t, c, operation, "name", nil)
				var ref *prompts.Refusal
				if !errors.As(err, &ref) {
					t.Fatal(err)
				}
				if err := w.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				expected := int64(elapsed / time.Microsecond)
				if expected < 0 {
					expected = 0
				}
				trail := capture.Events()
				if len(trail) != 1 || trail[0].Attrs["duration_us"] != expected {
					t.Fatal(trail)
				}
			})
		}
	}
}
