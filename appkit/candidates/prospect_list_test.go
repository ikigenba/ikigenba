package candidates_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
)

func unknownProspectList(w http.ResponseWriter, req map[string]json.RawMessage, media string, code int, message string) {
	w.Header().Set("Content-Type", media)
	if media == "text/event-stream" {
		_, _ = io.WriteString(w, "data: ")
	}
	encoded, _ := json.Marshal(message)
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%s,"data":{"old":true}}}`, req["id"], code, encoded)
	if media == "text/event-stream" {
		_, _ = io.WriteString(w, "\n\n")
	}
}

// R-E22U-2TPC
func TestProspectListFallback(t *testing.T) {
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, id := range []string{"pl_7", "coh_7", "pl_", "", "xpl_7", "pl_日本/\n\""} {
			for _, outcome := range []string{"success", "empty", "not_found", "unavailable", "stopping", "invalid", "busy", "unknown", "malformed", "protocol", "rpc", "http", "redirect", "disconnect"} {
				t.Run(media+"/"+id+"/"+outcome, func(t *testing.T) {
					var calls atomic.Int32
					entry := server(t, func(w http.ResponseWriter, r *http.Request) {
						req := request(t, r)
						n := calls.Add(1)
						operation, value := "prospect_list", id
						if n > 1 {
							operation = "cohort"
							if strings.HasPrefix(id, "pl_") {
								value = "coh_" + id[3:]
							}
						}
						var params struct {
							Name      string
							Arguments map[string]string
						}
						if err := json.Unmarshal(req["params"], &params); err != nil {
							t.Error(err)
						}
						if n > 2 || params.Name != operation || !reflect.DeepEqual(params.Arguments, map[string]string{operation: value}) || r.Header.Get("Mcp-Name") != operation || r.Method != "POST" || r.URL.Path != "/mcp" || r.URL.RawQuery != "" || r.Host != "candidates-42" || r.Header.Get("X-User-Id") != "user" || r.Header.Get("X-User-Email") != "email" || r.Header.Get("X-Request-Id") != "request" {
							t.Errorf("request %d: params=%+v headers=%v", n, params, r.Header)
						}
						if n == 1 {
							unknownProspectList(w, req, media, mcp.CodeInvalidParams, "Unknown tool: prospect_list")
							return
						}
						w.Header().Set("Content-Type", "application/json")
						switch outcome {
						case "success":
							respond(w, req, `{"structuredContent":{"members":[{"candidate":`+minimalBusiness+`,"excluded":true},{"candidate":`+minimalBusiness+`}]}}`)
						case "empty":
							respond(w, req, `{"structuredContent":{"members":[]}}`)
						case "malformed":
							respond(w, req, `{"structuredContent":{"members":null}}`)
						case "protocol":
							respond(w, req, `{"isError":true,"isError":true}`)
						case "rpc":
							unknownProspectList(w, req, media, mcp.CodeInvalidParams, "Unknown tool: prospect_list")
						case "http", "redirect":
							if outcome == "redirect" {
								w.Header().Set("Location", "/other")
								w.WriteHeader(302)
							} else {
								w.WriteHeader(503)
							}
							_, _ = io.WriteString(w, strings.Repeat("x", 5000))
						case "disconnect":
							conn, _, err := w.(http.Hijacker).Hijack()
							if err != nil {
								t.Error(err)
								return
							}
							_ = conn.Close()
						default:
							respond(w, req, `{"isError":true,"content":[{"type":"text","text":"`+outcome+`: detail"}]}`)
						}
					})
					tw, capture := writer(t)
					client := candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: tw})
					ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "user", Email: "email", RequestID: "request"})
					got, err := client.ProspectList(ctx, id)
					if calls.Load() != 2 {
						t.Fatalf("requests=%d", calls.Load())
					}
					if outcome != "success" && outcome != "empty" && got != nil {
						t.Fatalf("partial members: %#v", got)
					}
					switch outcome {
					case "success":
						b := candidates.Business{ID: "id", Status: "active", Name: "business", Names: []string{}, People: []candidates.Person{}}
						if err != nil || !reflect.DeepEqual(got, []candidates.Member{{Business: b, Excluded: true}, {Business: b}}) {
							t.Fatalf("%#v %v", got, err)
						}
					case "empty":
						if err != nil || got == nil || len(got) != 0 {
							t.Fatalf("%#v %v", got, err)
						}
					case "not_found", "unavailable", "stopping":
						want := map[string]error{"not_found": candidates.ErrNotFound, "unavailable": candidates.ErrUnavailable, "stopping": candidates.ErrStopping}[outcome]
						if !errors.Is(err, want) {
							t.Fatal(err)
						}
					case "invalid", "busy", "unknown":
						var refusal *candidates.Refusal
						if !errors.As(err, &refusal) || refusal.Text != outcome+": detail" {
							t.Fatal(err)
						}
					case "malformed", "protocol":
						malformed(t, err)
					case "rpc":
						var rpc *mcp.RPCError
						if !errors.As(err, &rpc) || rpc.Code != mcp.CodeInvalidParams || rpc.Message != "Unknown tool: prospect_list" {
							t.Fatal(err)
						}
					case "http", "redirect":
						var he *candidates.HTTPError
						status := 503
						if outcome == "redirect" {
							status = 302
						}
						if !errors.As(err, &he) || he.StatusCode != status || he.Body != strings.Repeat("x", 4096) {
							t.Fatal(err)
						}
					case "disconnect":
						transportError(t, err)
					}
					if err := tw.Flush(context.Background()); err != nil {
						t.Fatal(err)
					}
					trail := capture.Events()
					if len(trail) != 2 {
						t.Fatal(trail)
					}
					for _, e := range trail {
						if e.Name != "sibling.called" || e.RequestID != "request" || e.User != "user" || e.Attrs["path"] != "/mcp" {
							t.Fatal(e)
						}
					}
				})
			}
		}
	}
}

// R-E3AQ-GLG1
func TestProspectListDoesNotRetryOtherRPCError(t *testing.T) {
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, rpc := range []struct {
			code    int
			message string
		}{{mcp.CodeInternalError, "Unknown tool: prospect_list"}, {mcp.CodeInvalidParams, "Unknown tool: cohort"}, {mcp.CodeInvalidParams, "Unknown tool: prospect_list "}} {
			var calls atomic.Int32
			entry := server(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				unknownProspectList(w, request(t, r), media, rpc.code, rpc.message)
			})
			tw, _ := writer(t)
			got, err := candidates.NewClient(candidates.ClientConfig{Service: entry, Telemetry: tw}).ProspectList(context.Background(), "pl_7")
			var found *mcp.RPCError
			if calls.Load() != 1 || got != nil || !errors.As(err, &found) || found.Code != rpc.code || found.Message != rpc.message || string(found.Data) != `{"old":true}` {
				t.Fatalf("calls=%d got=%#v err=%v", calls.Load(), got, err)
			}
		}
	}
}

// R-E22U-2TPC
func fallbackServer(t *testing.T, operation string, handler http.HandlerFunc) services.Entry {
	t.Helper()
	return server(t, func(w http.ResponseWriter, r *http.Request) {
		if operation == "fallback" && r.Header.Get("Mcp-Name") == "prospect_list" {
			unknownProspectList(w, request(t, r), "application/json", mcp.CodeInvalidParams, "Unknown tool: prospect_list")
			return
		}
		handler(w, r)
	})
}
