package candidates_test

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
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
	return services.Entry{Name: "candidates-42", Socket: socket, URL: "invalid public URL"}
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
func answer(t *testing.T, status int, media, result string) *candidates.Client {
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
	return candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: w})
}

func sentinel(err error) bool {
	return errors.Is(err, candidates.ErrNotFound) || errors.Is(err, candidates.ErrUnavailable) || errors.Is(err, candidates.ErrStopping)
}
func malformed(t *testing.T, err error) {
	t.Helper()
	var refusal *candidates.Refusal
	var he *candidates.HTTPError
	var rpc *mcp.RPCError
	if err == nil || sentinel(err) || !strings.HasPrefix(err.Error(), "candidates: ") || errors.As(err, &refusal) || errors.As(err, &he) || errors.As(err, &rpc) {
		t.Fatalf("malformed classification: %v", err)
	}
}

const minimalBusiness = `{"id":"id","status":"active","name":"business"}`

func output(operation string) string {
	if operation != "candidate" {
		return `{"members":[{"candidate":` + minimalBusiness + `}]}`
	}
	return `{"candidate":` + minimalBusiness + `}`
}

// R-E0UX-P1YN R-E3AQ-GLG1 R-P8BX-VFU8 R-P9JU-97KX R-PARQ-MZBM R-PBZN-0R2B R-PD7J-EIT0 R-PEFF-SAJP
func TestRequests(t *testing.T) {
	for _, operation := range []string{"cohort", "candidate", "prospect_list"} {
		for _, value := range []string{"", "a/b?% 日本\n\""} {
			for _, caller := range []*identity.Caller{nil, {UserID: "user", Email: "email", RequestID: "request"}, {RequestID: "partial"}} {
				{
					t.Run(fmt.Sprint(operation, value, caller), func(t *testing.T) {
						var calls atomic.Int32
						entry := server(t, func(out http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							root := request(t, r)
							if r.Method != "POST" || r.URL.Path != "/mcp" || r.URL.RawQuery != "" || r.Host != "candidates-42" {
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
							expectedArgs := map[string]any{operation: value}
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
						c := candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: w})
						ctx := events.NewContext(context.Background(), events.Cause{ID: "cause", Depth: 3})
						if caller != nil {
							ctx = identity.NewContext(ctx, *caller)
						}
						_, err := transportCall(ctx, t, c, operation, value)
						var ref *candidates.Refusal
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

// R-PD7J-EIT0
func TestTelemetryDuration(t *testing.T) {
	for _, operation := range []string{"cohort", "candidate", "prospect_list"} {
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
				c := candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: w})
				_, err := transportCall(context.Background(), t, c, operation, "name")
				var ref *candidates.Refusal
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

// R-P741-HO3J
func TestConstruction(t *testing.T) {
	func() {
		defer func() {
			if v := recover(); !strings.Contains(fmt.Sprint(v), "telemetry writer is nil") {
				t.Errorf("panic=%v", v)
			}
		}()
		candidates.NewClient(candidates.ClientConfig{})
		t.Error("no panic")
	}()
	w, capture := writer(t)
	var contacted atomic.Int32
	live := server(t, func(out http.ResponseWriter, _ *http.Request) { contacted.Add(1); out.WriteHeader(500) })
	for _, entry := range []services.Entry{{}, {Name: "invalid /", Socket: filepath.Join(t.TempDir(), "absent")}, live} {
		if candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: w}) == nil {
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

// R-PPEJ-887Y
func TestMalformedProtocolResponses(t *testing.T) {
	for _, operation := range []string{"cohort", "candidate", "prospect_list"} {
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
					c := candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: w})
					_, err := transportCall(context.Background(), t, c, operation, "name")
					malformed(t, err)
				})
			}
		}
	}
}
