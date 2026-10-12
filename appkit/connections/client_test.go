package connections_test

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

	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func writer(t *testing.T) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	var diagnostics bytes.Buffer
	w := telemetry.New(telemetry.Config{Service: "connector", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return time.Unix(100, 0) }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 256)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected retry") }})
	t.Cleanup(func() {
		w.Shutdown(context.Background(), "test done")
		if diagnostics.Len() != 0 {
			t.Errorf("diagnostics: %s", diagnostics.String())
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
	return services.Entry{Name: "connections-42", Socket: socket, URL: "invalid public URL"}
}
func answer(t *testing.T, status int, body string) *connections.Client {
	t.Helper()
	entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	w, _ := writer(t)
	return connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w})
}

var refusals = []error{connections.ErrUnknownProvider, connections.ErrNotConnected, connections.ErrProvider, connections.ErrUnavailable}

func checkError(t *testing.T, token connections.Token, err, sentinel error, reconnect, unexpected bool) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(token, connections.Token{}) {
		t.Fatalf("token=%+v error=%v", token, err)
	}
	for _, candidate := range refusals {
		if errors.Is(err, candidate) != errors.Is(sentinel, candidate) {
			t.Fatalf("classification %v as %v", err, candidate)
		}
	}
	var re *connections.ReconnectError
	var he *connections.HTTPError
	if errors.As(err, &re) != reconnect || errors.As(err, &he) != unexpected {
		t.Fatalf("typed classification %v", err)
	}
}

// R-ZZ36-VUGN R-POEX-7Y80 R-ZO9M-NIZT R-ZPHJ-1AQI R-ZQPF-F2H7 R-ZY0T-POXD R-ZZ8Q-3GO2
func TestPublicTypes(t *testing.T) {
	w, _ := writer(t)
	cfg := connections.ClientConfig{Service: services.Entry{Name: "broker"}, Telemetry: w}
	var configShape struct {
		Service   services.Entry
		Telemetry *telemetry.Writer
	} = cfg
	clients := []*connections.Client{connections.NewClient(cfg)}
	methods := []func(context.Context, string) (connections.Token, error){clients[0].Token}
	tok := connections.Token{AccessToken: "access", Expires: time.Unix(1, 0), Account: "account", Scopes: []string{"scope"}}
	var tokenShape struct {
		AccessToken string
		Expires     time.Time
		Account     string
		Scopes      []string
	} = tok
	re := connections.ReconnectError{URL: "url"}
	var reconnectShape struct{ URL string } = re
	he := connections.HTTPError{StatusCode: 599, Body: "body"}
	var httpShape struct {
		StatusCode int
		Body       string
	} = he
	errorMethods := []func() string{re.Error, he.Error}
	if configShape.Telemetry != w || configShape.Service.Name != "broker" || methods[0] == nil || tokenShape.AccessToken != "access" || !tokenShape.Expires.Equal(tok.Expires) || tokenShape.Account != "account" || !reflect.DeepEqual(tokenShape.Scopes, tok.Scopes) || reconnectShape.URL != "url" || httpShape.Body != "body" || httpShape.StatusCode != 599 || errorMethods[0]() == "" || errorMethods[1]() == "" {
		t.Fatal("public types")
	}
}

// R-ZT58-6LYL R-PY64-A45K R-0GBB-G91S
func TestErrorValues(t *testing.T) {
	for i, err := range refusals {
		if err == nil || !strings.HasPrefix(err.Error(), "connections: ") {
			t.Fatal(err)
		}
		for j, other := range refusals {
			if errors.Is(err, other) != (i == j) {
				t.Fatal("sentinels overlap")
			}
		}
	}
	for _, code := range []int{0, 404, -1, 599} {
		he := &connections.HTTPError{StatusCode: code, Body: "not included"}
		if he.Error() != fmt.Sprintf("connections: unexpected HTTP response (status %d)", code) {
			t.Fatal(he)
		}
	}
	for _, location := range []string{"", "https://host/connect?x=y"} {
		err := &connections.ReconnectError{URL: location}
		if !strings.HasPrefix(err.Error(), "connections: ") || !strings.Contains(err.Error(), location) {
			t.Fatal(err)
		}
	}
}

// R-PPMT-LPYP
func TestConstruction(t *testing.T) {
	func() {
		defer func() {
			if value := recover(); !strings.Contains(fmt.Sprint(value), "telemetry writer is nil") {
				t.Fatalf("panic=%v", value)
			}
		}()
		connections.NewClient(connections.ClientConfig{})
		t.Error("missing panic")
	}()
	w, capture := writer(t)
	for _, entry := range []services.Entry{{}, {Name: "invalid host /", Socket: filepath.Join(t.TempDir(), "missing")}} {
		if connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w}) == nil {
			t.Fatal("nil client")
		}
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(capture.Events()) != 0 {
		t.Fatal("construction contacted service")
	}
}

// R-PQUP-ZHPE R-PS2M-D9G3 R-044B-MJMU R-PTAI-R16S R-PUIF-4SXH
func TestRequestAndTelemetry(t *testing.T) {
	for _, caller := range []*identity.Caller{nil, {UserID: "user", Email: "email", RequestID: "request"}, {RequestID: "id"}} {
		for _, provider := range []string{"google", "", "a?b", "a/b", "goo%67le", "a b", "日本"} {
			t.Run(fmt.Sprintf("%v/%s", caller, provider), func(t *testing.T) {
				var calls atomic.Int32
				entry := server(t, func(out http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					data, err := io.ReadAll(r.Body)
					if err != nil || len(data) != 0 || r.Method != "GET" || r.Host != "connections-42" || r.URL.Path != "/token/"+provider || r.URL.RawQuery != "" {
						t.Error("request changed", r, err)
					}
					expected := &http.Request{Header: make(http.Header)}
					if caller != nil {
						identity.Forward(*caller, expected)
					}
					for _, name := range []string{"X-User-Id", "X-User-Email", "X-Request-Id", "X-Event-Cause", "X-Event-Depth"} {
						if !reflect.DeepEqual(r.Header.Values(name), expected.Header.Values(name)) {
							t.Errorf("%s=%v want %v", name, r.Header.Values(name), expected.Header.Values(name))
						}
					}
					out.WriteHeader(404)
					_, _ = io.WriteString(out, `{"error":"unknown_provider"}`)
				})
				w, capture := writer(t)
				ctx := events.NewContext(context.Background(), events.Cause{ID: "cause", Depth: 3})
				if caller != nil {
					ctx = identity.NewContext(ctx, *caller)
				}
				_, err := connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w}).Token(ctx, provider)
				if !errors.Is(err, connections.ErrUnknownProvider) {
					t.Fatal(err)
				}
				if err := w.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				trail := capture.Events()
				if calls.Load() != 1 || len(trail) != 1 {
					t.Fatalf("calls=%d trail=%v", calls.Load(), trail)
				}
				event := trail[0]
				user, request := "", ""
				if caller != nil {
					user, request = caller.UserID, caller.RequestID
				}
				if event.Name != "sibling.called" || event.Service != "connector" || event.User != user || event.RequestID != request || !event.Time.Equal(time.Unix(100, 0)) || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"target": entry.Name, "method": "GET", "path": "/token/" + provider, "status": int64(404), "duration_us": int64(0)}) {
					t.Fatal(event)
				}
			})
		}
	}
}

// R-06K4-E348 R-07S0-RUUX
func TestTokenAnswers(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"secret","expires":"2030-01-02T03:04:05+02:00","account":"acct","scopes":["one","two"],"extra":true}`,
		` {"ACCESS_TOKEN":"wrong","access_token":null,"access_token":"secret","expires":"2030-01-02T03:04:05+02:00","account":"acct","scopes":["one","two"]} `,
	} {
		tok, err := answer(t, 200, body).Token(context.Background(), "google")
		expiry, parseErr := time.Parse(time.RFC3339, "2030-01-02T03:04:05+02:00")
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if err != nil || tok.AccessToken != "secret" || !tok.Expires.Equal(expiry) || tok.Expires.Location().String() != expiry.Location().String() || tok.Account != "acct" || !reflect.DeepEqual(tok.Scopes, []string{"one", "two"}) {
			t.Fatalf("%+v %v", tok, err)
		}
	}
	tok, err := answer(t, 200, `{"access_token":"","expires":"2030-01-02T03:04:05Z","account":"","scopes":[]}`).Token(context.Background(), "google")
	if err != nil || tok.Scopes == nil || len(tok.Scopes) != 0 {
		t.Fatalf("%+v %v", tok, err)
	}
}

// R-06K4-E348 R-08ZX-5MLM R-0IR4-7SJ6
func TestMalformedTokens(t *testing.T) {
	valid := `{"access_token":"a","expires":"2030-01-02T03:04:05Z","account":"b","scopes":[]}`
	bodies := []string{"", "null", "[]", "true", "{", string([]byte{123, 34, 120, 34, 58, 34, 255, 34, 125}), valid + " {}", valid + " trailing", strings.Replace(valid, `"access_token"`, `"ACCESS_TOKEN"`, 1), strings.Replace(valid, `2030-01-02T03:04:05Z`, `bad`, 1), strings.TrimSuffix(valid, "}") + `,"account":null}`}
	for _, member := range []string{"access_token", "expires", "account", "scopes"} {
		for _, bad := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`1`), json.RawMessage(`true`), json.RawMessage(`{}`), json.RawMessage(`[null]`), json.RawMessage(`["ok",1]`)} {
			fields := map[string]any{"access_token": "a", "expires": "2030-01-02T03:04:05Z", "account": "b", "scopes": []string{}}
			fields[member] = bad
			body, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			bodies = append(bodies, string(body))
		}
		fields := map[string]any{"access_token": "a", "expires": "2030-01-02T03:04:05Z", "account": "b", "scopes": []string{}}
		delete(fields, member)
		body, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(body))
	}
	for i, body := range bodies {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			tok, err := answer(t, 200, body).Token(context.Background(), "google")
			checkError(t, tok, err, nil, false, false)
			if !strings.HasPrefix(err.Error(), "connections: ") {
				t.Fatal(err)
			}
		})
	}
}

// R-0A7T-JECB R-0CNM-AXTP R-0DVI-OPKE R-06K4-E348 R-0IR4-7SJ6
func TestRefusals(t *testing.T) {
	for i, tc := range []struct {
		status    int
		body      string
		sentinel  error
		reconnect bool
		location  string
	}{
		{404, `{"error":"unknown_provider"}`, connections.ErrUnknownProvider, false, ""}, {404, `{"error":"not_connected","unknown":{}}`, connections.ErrNotConnected, false, ""},
		{502, `{"error":"provider"}`, connections.ErrProvider, false, ""}, {503, `{"error":"unavailable"}`, connections.ErrUnavailable, false, ""},
		{409, `{"error":"reconnect","url":"https://host/connect"}`, nil, true, "https://host/connect"}, {409, `{"error":"wrong","error":"reconnect","URL":"wrong","url":""}`, nil, true, ""},
		{404, `{"ERROR":"wrong","error":"unknown_provider","extra":"` + strings.Repeat("x", 5000) + `"}`, connections.ErrUnknownProvider, false, ""},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			tok, err := answer(t, tc.status, tc.body).Token(context.Background(), "google")
			checkError(t, tok, err, tc.sentinel, tc.reconnect, false)
			if tc.reconnect {
				var re *connections.ReconnectError
				if !errors.As(err, &re) || re.URL != tc.location {
					t.Fatal(err)
				}
			}
		})
	}
}

// R-0F3F-2HB3 R-06K4-E348 R-PVQB-IKO6 R-0IR4-7SJ6
func TestUnexpectedResponses(t *testing.T) {
	bodies := []string{"", "not JSON", "null", "[]", `{"ERROR":"unknown_provider"}`, `{"error":null}`, `{"error":"unknown_provider"} {}`, `{"error":"unknown_provider"} trailing`, `{"error":"unknown_provider"}` + strings.Repeat(" ", 5000) + `{}`, `{"error":"unknown_provider","error":null}`, `{"error":"unknown_provider","error":"wrong"}`, strings.Repeat("a", 8000), `{"error":"reconnect"}`, `{"error":"reconnect","url":null}`, `{"error":"provider"}`, `{"error":"unavailable"}`, `{"error":"not_connected"}`}
	for _, status := range []int{201, 302, 404, 409, 500, 502, 503} {
		for i, body := range bodies {
			t.Run(fmt.Sprintf("%d/%d", status, i), func(t *testing.T) {
				var expected error
				if status == 404 && body == `{"error":"not_connected"}` {
					expected = connections.ErrNotConnected
				}
				if status == 502 && body == `{"error":"provider"}` {
					expected = connections.ErrProvider
				}
				if status == 503 && body == `{"error":"unavailable"}` {
					expected = connections.ErrUnavailable
				}
				var calls atomic.Int32
				entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, body)
				})
				w, _ := writer(t)
				tok, err := connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w}).Token(context.Background(), "google")
				checkError(t, tok, err, expected, false, expected == nil)
				if expected == nil {
					var he *connections.HTTPError
					if !errors.As(err, &he) {
						t.Fatal(err)
					}
					prefix := body
					if len(prefix) > 4096 {
						prefix = prefix[:4096]
					}
					if he.StatusCode != status || he.Body != prefix {
						t.Fatalf("status=%d body=%q", he.StatusCode, he.Body)
					}
				}
				if calls.Load() != 1 {
					t.Fatal("redirect followed")
				}
			})
		}
	}
}

// R-0JZ0-LK9V R-0IR4-7SJ6 R-PTAI-R16S
func TestTransportFailures(t *testing.T) {
	w, capture := writer(t)
	client := connections.NewClient(connections.ClientConfig{Service: services.Entry{Name: "broker", Socket: filepath.Join(t.TempDir(), "missing")}, Telemetry: w})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, tc := range []struct {
		ctx      context.Context
		expected error
	}{{context.Background(), nil}, {canceled, context.Canceled}, {expired, context.DeadlineExceeded}} {
		tok, err := client.Token(tc.ctx, "google")
		checkError(t, tok, err, nil, false, false)
		if tc.expected != nil {
			if !errors.Is(err, tc.expected) {
				t.Fatal(err)
			}
		} else {
			var op *net.OpError
			if !errors.As(err, &op) || op.Op != "dial" {
				t.Fatal(err)
			}
		}
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 3 {
		t.Fatal(trail)
	}
	for _, e := range trail {
		if e.Name != "sibling.called" || e.Attrs["status"] != int64(0) {
			t.Fatal(e)
		}
	}
}

// R-7DJ4-L6KG
func TestClosesLargeResponse(t *testing.T) {
	failed := make(chan error, 1)
	entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		block := bytes.Repeat([]byte{'x'}, 64*1024)
		for range 1024 {
			if _, err := w.Write(block); err != nil {
				failed <- err
				return
			}
		}
		failed <- nil
	})
	w, _ := writer(t)
	tok, err := connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w}).Token(context.Background(), "google")
	checkError(t, tok, err, nil, false, true)
	if err := <-failed; err == nil {
		t.Fatal("server wrote entire body; response not closed")
	}
}

// R-0MET-D3R9
func TestConcurrentCalls(t *testing.T) {
	entry := server(t, func(w http.ResponseWriter, r *http.Request) {
		n := strings.TrimPrefix(r.URL.Path, "/token/")
		if r.Header.Get("X-User-Id") != n {
			t.Error("caller mixed")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": n, "expires": "2030-01-02T03:04:05Z", "account": n, "scopes": []string{n}})
	})
	w, capture := writer(t)
	client := connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			n := fmt.Sprint(i)
			tok, err := client.Token(identity.NewContext(context.Background(), identity.Caller{UserID: n, RequestID: "id" + n}), n)
			if err != nil || tok.AccessToken != n || tok.Account != n || !reflect.DeepEqual(tok.Scopes, []string{n}) {
				t.Errorf("%+v %v", tok, err)
			}
		})
	}
	wg.Wait()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 32 {
		t.Fatal(len(trail))
	}
	for _, e := range trail {
		if e.RequestID != "id"+e.User || e.Attrs["path"] != "/token/"+e.User {
			t.Fatal(e)
		}
	}
}

// R-DZSF-MLPW R-E10C-0DGL
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
	os.Stdout, os.Stderr = writeOut, writeErr
	oldLog := log.Writer()
	var logOutput bytes.Buffer
	log.SetOutput(&logOutput)
	defer func() {
		os.Stdout, os.Stderr = stdout, stderr
		log.SetOutput(oldLog)
		_ = readOut.Close()
		_ = readErr.Close()
		_ = writeOut.Close()
		_ = writeErr.Close()
	}()
	entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"same","expires":"2030-01-02T03:04:05Z","account":"same","scopes":[]}`)
	})
	w, _ := writer(t)
	failures := []struct {
		status int
		body   string
	}{{404, `{"error":"not_connected"}`}, {409, `{"error":"reconnect","url":"same"}`}, {502, `{"error":"provider"}`}, {503, `{"error":"unavailable"}`}, {500, "failed"}, {200, "invalid"}}
	var previousErrors []string
	var previous connections.Token
	for i := range 2 {
		t.Chdir(t.TempDir())
		client := connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w})
		tok, err := client.Token(context.Background(), "google")
		if err != nil || i > 0 && !reflect.DeepEqual(tok, previous) {
			t.Fatalf("%+v %v", tok, err)
		}
		previous = tok
		var currentErrors []string
		for _, failure := range failures {
			failing := answer(t, failure.status, failure.body)
			zero, failureErr := failing.Token(context.Background(), "google")
			if failureErr == nil || !reflect.DeepEqual(zero, connections.Token{}) {
				t.Fatal(zero, failureErr)
			}
			currentErrors = append(currentErrors, failureErr.Error())
		}
		if i > 0 && !reflect.DeepEqual(currentErrors, previousErrors) {
			t.Fatal("errors changed with working directory")
		}
		previousErrors = currentErrors

		_ = (&connections.HTTPError{StatusCode: 599}).Error()
		_ = (&connections.ReconnectError{URL: "same"}).Error()
		for _, sentinel := range refusals {
			_ = sentinel.Error()
		}
	}
	_ = writeOut.Close()
	_ = writeErr.Close()
	out, outErr := io.ReadAll(readOut)
	diagnostics, diagErr := io.ReadAll(readErr)
	if outErr != nil || diagErr != nil || len(out) != 0 || len(diagnostics) != 0 || logOutput.Len() != 0 {
		t.Fatalf("output=%q stderr=%q log=%q errors=%v %v", out, diagnostics, logOutput.String(), outErr, diagErr)
	}
}

// R-PTAI-R16S
func TestTelemetryDuration(t *testing.T) {
	for _, elapsed := range []time.Duration{789*time.Microsecond + 900*time.Nanosecond, -time.Microsecond} {
		t.Run(elapsed.String(), func(t *testing.T) {
			var clockReads atomic.Int32
			capture := &telemetry.Capture{}
			var diagnostics bytes.Buffer
			start := time.Unix(100, 0)
			w := telemetry.New(telemetry.Config{Service: "connector", Sink: capture, Stderr: &diagnostics, Now: func() time.Time {
				if clockReads.Add(1) == 1 {
					return start
				}
				return start.Add(elapsed)
			}, Rand: bytes.NewReader(make([]byte, 16)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected pause") }})
			t.Cleanup(func() {
				w.Shutdown(context.Background(), "done")
				if diagnostics.Len() != 0 {
					t.Fatal(diagnostics.String())
				}
			})
			entry := server(t, func(out http.ResponseWriter, _ *http.Request) { out.WriteHeader(500) })
			_, _ = connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w}).Token(context.Background(), "provider")
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			trail := capture.Events()
			expected := int64(elapsed / time.Microsecond)
			if expected < 0 {
				expected = 0
			}
			if len(trail) != 1 || trail[0].Attrs["duration_us"] != expected || clockReads.Load() != 3 {
				t.Fatal(trail, clockReads.Load())
			}
		})
	}
}

// R-PVQB-IKO6
func TestRedirectStatuses(t *testing.T) {
	for _, status := range []int{300, 301, 302, 303, 304, 305, 307, 308} {
		for _, location := range []string{"", "/another"} {
			t.Run(fmt.Sprint(status)+location, func(t *testing.T) {
				var calls atomic.Int32
				entry := server(t, func(out http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					if location != "" {
						out.Header().Set("Location", location)
					}
					out.WriteHeader(status)
				})
				w, _ := writer(t)
				tok, err := connections.NewClient(connections.ClientConfig{Service: entry, Telemetry: w}).Token(context.Background(), "google")
				checkError(t, tok, err, nil, false, true)
				var he *connections.HTTPError
				if !errors.As(err, &he) || he.StatusCode != status || calls.Load() != 1 {
					t.Fatal(err, calls.Load())
				}
			})
		}
	}
}
