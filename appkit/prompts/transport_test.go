package prompts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/services"
)

// R-BXFQ-IX26
func transportCall(ctx context.Context, t *testing.T, c *prompts.Client, operation string, value string) (any, error) {
	t.Helper()
	var got, zero any
	var err error
	switch operation {
	case "show":
		got, err = c.Show(ctx, value)
		zero = prompts.Prompt{}
	case "run":
		got, err = c.Run(ctx, value, json.RawMessage(`{"value":true}`))
		zero = prompts.Start{}
	case "result":
		got, err = c.Result(ctx, value)
		zero = prompts.Outcome{}
	default:
		t.Fatalf("unknown operation %s", operation)
	}
	if err != nil && !reflect.DeepEqual(got, zero) {
		t.Errorf("error returned nonzero value: %+v, %v", got, err)
	}
	return got, err
}

func transportError(t *testing.T, err error) {
	t.Helper()
	var refusal *prompts.Refusal
	var httpError *prompts.HTTPError
	var rpcError *mcp.RPCError
	if err == nil || errors.As(err, &refusal) || errors.As(err, &httpError) || errors.As(err, &rpcError) {
		t.Fatalf("unexpected error classification: %v", err)
	}
}

// R-BSK4-ZU3E
func TestRPCFailures(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		for _, status := range []int{200, 201, 302, 400, 500} {
			for _, media := range []string{"application/json", "text/plain", "text/event-stream"} {
				if media == "text/event-stream" && status != 200 {
					continue
				}
				for _, data := range []string{"", `,"data":null`, `,"data":{"retry":false,"detail":[1,"x"]}`} {
					t.Run(fmt.Sprintf("%s/%d/%s/%s", operation, status, media, data), func(t *testing.T) {
						entry := server(t, func(w http.ResponseWriter, r *http.Request) {
							req := request(t, r)
							body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"test protocol refusal"%s}}`, req["id"], data)
							if media == "text/event-stream" {
								body = "data: " + body + "\n\n"
							}
							w.Header().Set("Content-Type", media)
							w.WriteHeader(status)
							_, _ = io.WriteString(w, body)
						})
						writer, _ := writer(t)
						c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: writer})
						_, err := transportCall(context.Background(), t, c, operation, "id")
						var rpc *mcp.RPCError
						var ref *prompts.Refusal
						var he *prompts.HTTPError
						if err == nil || !strings.HasPrefix(err.Error(), "prompts: ") || !errors.As(err, &rpc) || errors.As(err, &ref) || errors.As(err, &he) {
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
}

// R-25U1-B9LY R-289U-2T3C R-BW7U-55BH
func TestHTTPFailuresAndRedirects(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		for _, status := range []int{200, 201, 300, 301, 302, 303, 304, 305, 307, 308, 400, 401, 404, 500, 503} {
			for _, body := range []string{"", "plain response", `{"jsonrpc":"1.0","result":{}}`, `[]`, strings.Repeat("abcdef", 1000)} {
				for _, location := range []string{"", "/redirected"} {
					for _, media := range []string{"application/json", "text/plain", "application/octet-stream"} {
						t.Run(fmt.Sprintf("%s/%d/%d/%s/%s", operation, status, len(body), location, media), func(t *testing.T) {
							var calls atomic.Int32
							entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
								calls.Add(1)
								w.Header().Set("Content-Type", media)
								if location != "" {
									w.Header().Set("Location", location)
								}
								w.WriteHeader(status)
								_, _ = io.WriteString(w, body)
							})
							tw, _ := writer(t)
							c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: tw})
							_, err := transportCall(context.Background(), t, c, operation, "id")
							var he *prompts.HTTPError
							var ref *prompts.Refusal
							var rpc *mcp.RPCError
							if !errors.As(err, &he) || errors.As(err, &ref) || errors.As(err, &rpc) {
								t.Fatal(err)
							}
							expected := body
							if status == 304 {
								expected = ""
							}
							if len(expected) > 4096 {
								expected = expected[:4096]
							}
							if he.StatusCode != status || he.Body != expected || he.Error() != fmt.Sprintf("prompts: unexpected HTTP response (status %d)", status) {
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
	}
	for _, status := range []int{-1, 0, 599} {
		he := &prompts.HTTPError{StatusCode: status, Body: "not included"}
		if he.Error() != fmt.Sprintf("prompts: unexpected HTTP response (status %d)", status) {
			t.Fatal(he)
		}
	}
}

// R-BYNM-WOSV
func TestTransportFailures(t *testing.T) {
	tw, capture := writer(t)
	c := prompts.NewClient(prompts.ClientConfig{Service: services.Entry{Name: "prompts", Socket: filepath.Join(t.TempDir(), "missing")}, Telemetry: tw})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, ctx := range []context.Context{context.Background(), canceled, expired} {
		for _, operation := range []string{"show", "run", "result"} {
			_, err := transportCall(ctx, t, c, operation, "id")
			transportError(t, err)
			if !strings.HasPrefix(err.Error(), "prompts: ") {
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
	}
	if err := tw.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 9 {
		t.Fatal(len(trail))
	}
	for _, e := range trail {
		if e.Name != "sibling.called" || e.Attrs["status"] != int64(0) {
			t.Fatal(e)
		}
	}
}

// R-271X-P1CN
func TestCancellationWhileReadingResponse(t *testing.T) {
	for _, operation := range []string{"show", "run", "result"} {
		for _, tc := range []struct {
			status         int
			media, partial string
		}{{200, "application/json", `{"jsonrpc":"2.0","result":`}, {200, "text/event-stream", "data: {"}, {200, "text/plain", "partial"}, {500, "application/json", "partial"}, {302, "text/plain", "partial"}} {
			t.Run(fmt.Sprintf("%s/%d/%s", operation, tc.status, tc.media), func(t *testing.T) {
				flushed := make(chan struct{})
				release := make(chan struct{})
				ended := make(chan struct{})
				entry := server(t, func(w http.ResponseWriter, r *http.Request) {
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
				c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: tw})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan error, 1)
				go func() { _, err := transportCall(ctx, t, c, operation, "id"); result <- err }()
				<-flushed
				cancel()
				close(release)
				err := <-result
				transportError(t, err)
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				<-ended
			})
		}
	}
}

// R-C2BC-200Y
func TestConcurrentCalls(t *testing.T) {
	entry := server(t, func(w http.ResponseWriter, r *http.Request) {
		req := request(t, r)
		var params struct {
			Name      string `json:"name"`
			Arguments struct {
				Name  string          `json:"name"`
				Run   string          `json:"run"`
				Input json.RawMessage `json:"input"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(req["params"], &params); err != nil {
			t.Error(err)
			return
		}
		id := params.Arguments.Name
		if params.Name == "result" {
			id = params.Arguments.Run
		}
		if r.Header.Get("X-User-Id") != id || r.Header.Get("X-Request-Id") != "request"+id {
			t.Error("caller mixed", id, r.Header)
		}
		result := output(params.Name)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(result), &fields); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(id)
		fields["id"] = encoded
		body, err := json.Marshal(fields)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		respond(w, req, `{"structuredContent":`+string(body)+`}`)
	})
	tw, capture := writer(t)
	c := prompts.NewClient(prompts.ClientConfig{Service: entry, Telemetry: tw})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			id := fmt.Sprint(i)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: id, RequestID: "request" + id})
			for _, operation := range []string{"show", "run", "result"} {
				value, err := transportCall(ctx, t, c, operation, id)
				if err != nil {
					t.Error(err)
					continue
				}
				var got string
				switch v := value.(type) {
				case prompts.Prompt:
					got = v.ID
				case prompts.Start:
					got = v.ID
				case prompts.Outcome:
					got = v.ID
				}
				if got != id {
					t.Errorf("%s returned %s, want %s", operation, got, id)
				}
			}
		})
	}
	wg.Wait()
	if err := tw.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 96 {
		t.Fatal(len(trail))
	}
	for _, e := range trail {
		if e.RequestID != "request"+e.User {
			t.Fatal(e)
		}
	}
}

// R-A1JF-UMMZ R-A2RC-8EDO
func TestProcessIndependence(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr
	readOut, writeOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	readErr, writeErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldLog := log.Writer()
	var logOutput bytes.Buffer
	os.Stdout, os.Stderr = writeOut, writeErr
	log.SetOutput(&logOutput)
	defer func() {
		os.Stdout, os.Stderr = stdout, stderr
		log.SetOutput(oldLog)
		_ = readOut.Close()
		_ = readErr.Close()
		_ = writeOut.Close()
		_ = writeErr.Close()
	}()
	missingSocket := filepath.Join(t.TempDir(), "missing")
	var previousValues []any
	var previousErrors []string
	for i := range 2 {
		t.Chdir(t.TempDir())
		var values []any
		var messages []string
		func() {
			defer func() {
				if failure := recover(); failure != nil {
					messages = append(messages, fmt.Sprint(failure))
				} else {
					t.Error("nil writer did not panic")
				}
			}()
			prompts.NewClient(prompts.ClientConfig{})
		}()
		for _, operation := range []string{"show", "run", "result"} {
			c := answer(t, 200, "application/json", `{"structuredContent":`+output(operation)+`}`)
			value, err := transportCall(context.Background(), t, c, operation, "id")
			if err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
			for _, result := range []string{`{"isError":true,"content":[{"type":"text","text":"test refusal"}]}`, `{"structuredContent":null}`} {
				failing := answer(t, 200, "application/json", result)
				_, err := transportCall(context.Background(), t, failing, operation, "id")
				if err == nil {
					t.Fatal("missing error")
				}
				messages = append(messages, err.Error())
			}
			tw, _ := writer(t)
			unreachable := prompts.NewClient(prompts.ClientConfig{Service: services.Entry{Name: "prompts", Socket: missingSocket}, Telemetry: tw})
			_, transportFailure := transportCall(context.Background(), t, unreachable, operation, "id")
			if transportFailure == nil {
				t.Fatal("missing transport error")
			}
			messages = append(messages, transportFailure.Error())
			rpcEntry := server(t, func(w http.ResponseWriter, r *http.Request) {
				req := request(t, r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"protocol failure"}}`, req["id"])
			})
			rpcClient := prompts.NewClient(prompts.ClientConfig{Service: rpcEntry, Telemetry: tw})
			_, protocolFailure := transportCall(context.Background(), t, rpcClient, operation, "id")
			if protocolFailure == nil {
				t.Fatal("missing RPC error")
			}
			messages = append(messages, protocolFailure.Error())
			failing := answer(t, 500, "text/plain", `{}`)
			_, err = transportCall(context.Background(), t, failing, operation, "id")
			if err == nil {
				t.Fatal("missing status error")
			}
			messages = append(messages, err.Error())
		}
		messages = append(messages, (&prompts.Refusal{Text: "sample"}).Error(), (&prompts.HTTPError{StatusCode: 599, Body: "sample"}).Error())
		if i > 0 && (!reflect.DeepEqual(values, previousValues) || !reflect.DeepEqual(messages, previousErrors)) {
			t.Fatal("working directory changed results")
		}
		previousValues, previousErrors = values, messages
	}
	_ = writeOut.Close()
	_ = writeErr.Close()
	out, outErr := io.ReadAll(readOut)
	diagnostics, diagErr := io.ReadAll(readErr)
	if outErr != nil || diagErr != nil || len(out) != 0 || len(diagnostics) != 0 || logOutput.Len() != 0 {
		t.Fatalf("output=%q stderr=%q log=%q errors=%v %v", out, diagnostics, logOutput.String(), outErr, diagErr)
	}
}
