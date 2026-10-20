package artifacts_test

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

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func showRequest(t *testing.T, r *http.Request) map[string]json.RawMessage {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&root); err != nil {
		t.Error(err)
	}
	return root
}
func showRespond(w http.ResponseWriter, r map[string]json.RawMessage, result string) {
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, r["id"], result)
}
func showAnswer(t *testing.T, media, result string) *artifacts.Client {
	t.Helper()
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		root := showRequest(t, r)
		w.Header().Set("Content-Type", media)
		if media == "text/event-stream" {
			_, _ = io.WriteString(w, "data: ")
		}
		showRespond(w, root, result)
		if media == "text/event-stream" {
			_, _ = io.WriteString(w, "\n\n")
		}
	})
	w, _ := writer(t)
	return artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
}

// R-6HAW-PO48 R-5Z0E-Z3ZT
func showCall(ctx context.Context, t *testing.T, c *artifacts.Client, value string) (artifacts.Metadata, error) {
	t.Helper()
	m, err := c.Show(ctx, value)
	if err != nil && m != (artifacts.Metadata{}) {
		t.Errorf("error returned nonzero metadata: %+v, %v", m, err)
	}
	return m, err
}
func showSentinel(err error) bool {
	for _, sentinel := range sentinels() {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}
func showMalformed(t *testing.T, err error) {
	t.Helper()
	plainError(t, err)
	var refusal *artifacts.Refusal
	var rpc *mcp.RPCError
	if errors.As(err, &refusal) || errors.As(err, &rpc) || !strings.HasPrefix(err.Error(), "artifacts: ") {
		t.Fatal(err)
	}
}
func showTransportError(t *testing.T, err error) { t.Helper(); showMalformed(t, err) }

// R-61G7-QNH7 R-62O4-4F7W R-63W0-I6YL R-P9S4-MPBO R-Z9HA-UNW2 R-ZBX3-M7DG
func TestShowRequests(t *testing.T) {
	for _, value := range []string{"", "a/b?% 日本\n\""} {
		for _, caller := range []*identity.Caller{nil, {UserID: "user", Email: "email", RequestID: "request"}, {RequestID: "partial"}} {
			{
				t.Run(fmt.Sprint("show", value, caller), func(t *testing.T) {
					var calls atomic.Int32
					entry := serve(t, func(out http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						root := showRequest(t, r)
						if r.Method != "POST" || r.URL.Path != "/mcp" || r.URL.RawQuery != "" || r.Host != "artifacts-2" {
							t.Error("request", r)
						}
						for name, want := range map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "MCP-Protocol-Version": mcp.ProtocolVersion, "Mcp-Method": "tools/call", "Mcp-Name": "show"} {
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
						if json.Unmarshal(params["name"], &name) != nil || name != "show" || json.Unmarshal(params["arguments"], &args) != nil || json.Unmarshal(params["_meta"], &meta) != nil {
							t.Error("params", params)
						}
						expectedArgs := map[string]any{"path": value}
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
						showRespond(out, root, `{"isError":true}`)
					})
					w, capture := writer(t)
					c := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
					ctx := events.NewContext(context.Background(), events.Cause{ID: "cause", Depth: 3})
					if caller != nil {
						ctx = identity.NewContext(ctx, *caller)
					}
					_, err := showCall(ctx, t, c, value)
					var ref *artifacts.Refusal
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
					if e.Name != "sibling.called" || e.Service != "connector" || e.User != user || e.RequestID != req || !e.Time.Equal(fixedTime) || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"target": entry.Name, "method": "POST", "path": "/mcp", "status": int64(200), "duration_us": int64(0)}) {
						t.Fatal(e)
					}
				})
			}
		}
	}
}

// R-63W0-I6YL
func TestShowTelemetryDuration(t *testing.T) {
	for _, elapsed := range []time.Duration{789*time.Microsecond + 900*time.Nanosecond, -time.Microsecond} {
		t.Run("show"+elapsed.String(), func(t *testing.T) {
			start := time.Unix(100, 0)
			var current atomic.Int64
			current.Store(start.UnixNano())
			var diagnostics bytes.Buffer
			capture := &telemetry.Capture{}
			w := telemetry.New(telemetry.Config{Service: "connector", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return time.Unix(0, current.Load()) }, Rand: bytes.NewReader(make([]byte, 16)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected retry") }})
			t.Cleanup(func() {
				w.Shutdown(context.Background(), "done")
				if diagnostics.Len() != 0 {
					t.Error(diagnostics.String())
				}
			})
			entry := serve(t, func(out http.ResponseWriter, r *http.Request) {
				root := showRequest(t, r)
				current.Store(start.Add(elapsed).UnixNano())
				out.Header().Set("Content-Type", "application/json")
				showRespond(out, root, `{"isError":true}`)
			})
			c := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
			_, err := showCall(context.Background(), t, c, "name")
			var ref *artifacts.Refusal
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

// R-69ZI-F1O2 R-653W-VYPA
func TestShowMalformedProtocolResponses(t *testing.T) {
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, mode := range []string{"wrong-id", "result-type", "non-object-result", "no-response", "invalid-event"} {
			if media == "application/json" && (mode == "no-response" || mode == "invalid-event") {
				continue
			}
			t.Run("show"+"/"+media+"/"+mode, func(t *testing.T) {
				entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
					root := showRequest(t, r)
					w.Header().Set("Content-Type", media)
					if mode == "no-response" {
						_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notice\"}\n\n")
						return
					}
					if mode == "invalid-event" {
						_, _ = io.WriteString(w, "data: not JSON\n\n")
						return
					}
					result := `{"structuredContent":` + metadataJSON() + `}`
					switch mode {
					case "wrong-id":
						root["id"] = json.RawMessage(`"different-request"`)
					case "result-type":
						result = `{"resultType":"future-type","structuredContent":` + metadataJSON() + `}`
					case "non-object-result":
						result = `[]`
					}
					if media == "text/event-stream" {
						_, _ = io.WriteString(w, "data: ")
					}
					showRespond(w, root, result)
					if media == "text/event-stream" {
						_, _ = io.WriteString(w, "\n\n")
					}
				})
				w, _ := writer(t)
				c := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
				_, err := showCall(context.Background(), t, c, "name")
				showMalformed(t, err)
			})
		}
	}
}

// R-6B7E-STER
func TestShowRPCFailures(t *testing.T) {
	for _, status := range []int{200, 201, 302, 400, 500} {
		for _, media := range []string{"application/json", "text/plain", "text/event-stream"} {

			for _, data := range []string{"", `,"data":null`, `,"data":{"retry":false,"detail":[1,"x"]}`} {
				t.Run(fmt.Sprintf("%d/%s/%s", status, media, data), func(t *testing.T) {
					entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
						req := showRequest(t, r)
						body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"test protocol refusal"%s}}`, req["id"], data)
						if media == "text/event-stream" {
							body = "data: " + body + "\n\n"
						}
						w.Header().Set("Content-Type", media)
						w.WriteHeader(status)
						_, _ = io.WriteString(w, body)
					})
					writer, _ := writer(t)
					c := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: writer})
					_, err := showCall(context.Background(), t, c, "id")
					var rpc *mcp.RPCError
					var ref *artifacts.Refusal
					var he *artifacts.HTTPError
					if err == nil || showSentinel(err) || !strings.HasPrefix(err.Error(), "artifacts: ") || !errors.As(err, &rpc) || errors.As(err, &ref) || errors.As(err, &he) {
						t.Fatal(err)
					}
					var expected json.RawMessage
					if data != "" {
						expected = json.RawMessage(strings.TrimPrefix(data, `,"data":`))
					}
					if rpc.Code != -32602 || rpc.Message != "test protocol refusal" || !bytes.Equal(rpc.Data, expected) {
						t.Fatalf("RPC error %+v data %s", rpc, rpc.Data)
					}
				})
			}
		}
	}
}

// R-6CFB-6L5G R-6G30-BWDJ
func TestShowHTTPFailuresAndRedirects(t *testing.T) {
	for _, status := range []int{200, 201, 300, 301, 302, 303, 304, 305, 307, 308, 400, 401, 403, 404, 413, 500, 503} {
		for _, body := range []string{"", "plain response", `{"jsonrpc":"1.0","result":{}}`, `[]`, strings.Repeat("abcdef", 1000)} {
			for _, location := range []string{"", "/redirected"} {
				for _, media := range []string{"application/json", "text/plain", "application/octet-stream"} {
					t.Run(fmt.Sprintf("%d/%d/%s/%s", status, len(body), location, media), func(t *testing.T) {
						var calls atomic.Int32
						entry := serve(t, func(w http.ResponseWriter, _ *http.Request) {
							calls.Add(1)
							w.Header().Set("Content-Type", media)
							if location != "" {
								w.Header().Set("Location", location)
							}
							w.WriteHeader(status)
							_, _ = io.WriteString(w, body)
						})
						tw, _ := writer(t)
						c := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: tw})
						_, err := showCall(context.Background(), t, c, "id")
						var he *artifacts.HTTPError
						var ref *artifacts.Refusal
						var rpc *mcp.RPCError
						if showSentinel(err) || !errors.As(err, &he) || errors.As(err, &ref) || errors.As(err, &rpc) {
							t.Fatal(err)
						}
						expected := body
						if status == 304 {
							expected = ""
						}
						if len(expected) > 4096 {
							expected = expected[:4096]
						}
						if he.StatusCode != status || he.Body != expected || he.Error() != fmt.Sprintf("artifacts: unexpected HTTP response (status %d)", status) {
							t.Fatalf("error=%+v body=%q", he, he.Body)
						}
						if calls.Load() != 1 {
							t.Fatal("request count", calls.Load())
						}
					})
				}
			}
		}
	}
	for _, status := range []int{-1, 0, 599} {
		he := &artifacts.HTTPError{StatusCode: status, Body: "not included"}
		if he.Error() != fmt.Sprintf("artifacts: unexpected HTTP response (status %d)", status) {
			t.Fatal(he)
		}
	}
}

// R-2TJH-LZ2T R-PC7X-E8T2 R-63W0-I6YL
func TestShowTransportFailures(t *testing.T) {
	tw, capture := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{Service: services.Entry{Name: "artifacts", Socket: filepath.Join(t.TempDir(), "missing")}, Telemetry: tw})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, ctx := range []context.Context{context.Background(), canceled, expired} {
		_, err := showCall(ctx, t, c, "id")
		showTransportError(t, err)
		if !strings.HasPrefix(err.Error(), "artifacts: ") {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			if !errors.Is(err, ctx.Err()) {
				t.Fatal(err)
			}
		} else {
			var op *net.OpError
			if !errors.As(err, &op) || op.Op != "dial" {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 3 {
		t.Fatal(len(trail))
	}
	for _, e := range trail {
		if e.Name != "sibling.called" || e.Attrs["status"] != int64(0) {
			t.Fatal(e)
		}
	}
}

// R-6EV3-Y4MU
func TestShowCancellationWhileReadingResponse(t *testing.T) {
	for _, tc := range []struct {
		status         int
		media, partial string
	}{{200, "application/json", `{"jsonrpc":"2.0","result":`}, {200, "text/event-stream", "data: {"}, {200, "text/plain", "partial"}, {500, "application/json", "partial"}, {302, "text/plain", "partial"}} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.media), func(t *testing.T) {
			flushed := make(chan struct{})
			release := make(chan struct{})
			ended := make(chan struct{})
			entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
				defer close(ended)
				w.Header().Set("Content-Type", tc.media)
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.partial)
				w.(http.Flusher).Flush()
				close(flushed)
				<-release
				<-r.Context().Done()
			})
			tw, _ := writer(t)
			c := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: tw})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := showCall(ctx, t, c, "id"); result <- err }()
			<-flushed
			cancel()
			close(release)
			err := <-result
			showTransportError(t, err)
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			<-ended
		})
	}
}

// R-6NEE-MITP
func TestShowResponseBodyClosedBeforeReturn(t *testing.T) {
	for _, outcome := range []string{"success", "refusal", "rpc-error", "protocol-error", "output-error"} {
		t.Run("show"+"/"+outcome, func(t *testing.T) {
			returned := make(chan struct{})
			writeResult := make(chan error, 1)
			entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
				req := showRequest(t, r)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: ")
				switch outcome {
				case "success":
					showRespond(w, req, `{"structuredContent":`+metadataJSON()+`}`)
				case "refusal":
					showRespond(w, req, `{"isError":true}`)
				case "rpc-error":
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"test refusal"}}`, req["id"])
				case "protocol-error":
					showRespond(w, req, `{"resultType":"unknown"}`)
				case "output-error":
					showRespond(w, req, `{"structuredContent":{}}`)
				}
				_, _ = io.WriteString(w, "\n\n")
				controller := http.NewResponseController(w)
				if err := controller.Flush(); err != nil {
					t.Errorf("flush response event: %v", err)
					writeResult <- nil
					return
				}
				// Continue only after the caller has returned, without canceling its context.
				<-returned
				chunk := []byte(strings.Repeat(":", (64<<10)-2) + "\n\n")
				for range 1024 {
					if _, err := w.Write(chunk); err != nil {
						writeResult <- err
						return
					}
					if err := controller.Flush(); err != nil {
						writeResult <- err
						return
					}
				}
				writeResult <- nil
			})
			w, _ := writer(t)
			client := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
			_, err := showCall(context.Background(), t, client, "id")
			close(returned)
			if writeErr := <-writeResult; writeErr == nil {
				t.Fatal("server wrote all 64 MiB after the call returned")
			}
			switch outcome {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			case "refusal":
				var refusal *artifacts.Refusal
				if !errors.As(err, &refusal) {
					t.Fatalf("expected refusal, got %v", err)
				}
			case "rpc-error":
				var rpc *mcp.RPCError
				if !errors.As(err, &rpc) {
					t.Fatalf("expected RPC error, got %v", err)
				}
			default:
				showMalformed(t, err)
			}
		})
	}
}

// R-6KYL-UZCB
func TestFilesRedirectWithoutLocation(t *testing.T) {
	for _, status := range []int{300, 301, 302, 303, 304, 305, 307, 308} {
		var calls atomic.Int32
		entry := serve(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(status) })
		w, _ := writer(t)
		c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
		for _, method := range []string{"PUT", "GET", "HEAD"} {
			_, err := invoke(context.Background(), c, method, "p")
			var he *artifacts.HTTPError
			if !errors.As(err, &he) || he.StatusCode != status {
				t.Fatal(err)
			}
		}
		if calls.Load() != 3 {
			t.Fatal(calls.Load())
		}
	}
}
