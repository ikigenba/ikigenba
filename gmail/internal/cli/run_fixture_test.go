package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
)

type runOptions struct {
	DefaultSinks                         bool
	ConnectionsHandler                   http.Handler
	ConnectionsURL                       string
	Dir                                  string
	Env                                  map[string]string
	MissingConnections, MissingArtifacts bool
	ServicesPath                         *string
	Transport                            http.RoundTripper
	Sink                                 telemetry.Sink
	EventSink                            events.Sink
	Rand                                 io.Reader
	Now                                  func() time.Time
	After                                func(time.Duration) <-chan time.Time
	Modify                               func(*cli.Process)
}
type outputBuffer struct {
	mu     sync.Mutex
	b      bytes.Buffer
	writes int
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes++
	return b.b.Write(p)
}
func (b *outputBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type runRandom struct{ n byte }

func (r *runRandom) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

type fixedRandom byte

func (r fixedRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type observedRequest struct {
	Method, Path string
	Header       http.Header
	Body         []byte
}
type fakeMail struct {
	mu       sync.Mutex
	requests []observedRequest
	n        int
	drafts   map[string]string
}

func jsonResponse(r *http.Request, code int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(b)), Request: r}
}
func (g *fakeMail) RoundTrip(r *http.Request) (*http.Response, error) {
	var b []byte
	var err error
	if r.Body != nil {
		b, err = io.ReadAll(r.Body)
	}
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, observedRequest{r.Method, r.URL.Path, r.Header.Clone(), b})
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/labels"):
		return jsonResponse(r, 200, map[string]any{"labels": []any{map[string]any{"id": "INBOX", "name": "INBOX", "type": "system"}}}), nil
	case strings.HasSuffix(path, "/attachments/blob"):
		return jsonResponse(r, 200, map[string]any{"size": 4, "data": base64.RawURLEncoding.EncodeToString([]byte("mail"))}), nil
	case strings.HasSuffix(path, "/messages"):
		return jsonResponse(r, 200, map[string]any{"messages": []any{map[string]any{"id": "message", "threadId": "thread"}}}), nil
	case strings.Contains(path, "/messages/"):
		if r.URL.Query().Get("format") == "raw" {
			return jsonResponse(r, 200, map[string]any{"raw": base64.RawURLEncoding.EncodeToString([]byte("raw-mail"))}), nil
		}
		return jsonResponse(r, 200, map[string]any{"id": "message", "threadId": "thread", "labelIds": []string{"INBOX"}, "snippet": "snippet", "internalDate": "1760000000000", "sizeEstimate": 8, "payload": map[string]any{"mimeType": "multipart/mixed", "headers": []any{map[string]any{"name": "Subject", "value": "subject"}}, "parts": []any{map[string]any{"partId": "part", "filename": "part.bin", "mimeType": "application/octet-stream", "body": map[string]any{"attachmentId": "blob", "size": 4}}}}}), nil
	case strings.Contains(path, "/threads/"):
		return jsonResponse(r, 200, map[string]any{"id": "thread", "messages": []any{}}), nil
	case strings.Contains(path, "/upload/"):
		resp := jsonResponse(r, 200, map[string]any{})
		resp.Header.Set("Location", "https://gmail.googleapis.com/session")
		return resp, nil
	case path == "/session":
		return jsonResponse(r, 200, map[string]any{"id": "google-draft", "message": map[string]any{"id": "draft-message-updated", "threadId": "draft-thread"}}), nil
	case strings.HasSuffix(path, "/drafts/send"):
		return jsonResponse(r, 200, map[string]any{"id": "sent-message", "threadId": "draft-thread"}), nil
	case strings.HasSuffix(path, "/drafts") && r.Method == http.MethodPost:
		g.n++
		id := fmt.Sprint("google-draft-", g.n)
		message := fmt.Sprint("draft-message-", g.n)
		g.drafts[id] = message
		return jsonResponse(r, 200, map[string]any{"id": id, "message": map[string]any{"id": message, "threadId": "draft-thread"}}), nil
	case strings.Contains(path, "/drafts/"):
		if r.Method == http.MethodDelete {
			return jsonResponse(r, 204, nil), nil
		}
		id := path[strings.LastIndex(path, "/")+1:]
		return jsonResponse(r, 200, map[string]any{"id": id, "message": map[string]any{"id": g.drafts[id], "threadId": "draft-thread"}}), nil
	}
	return jsonResponse(r, 404, map[string]any{}), nil
}
func (g *fakeMail) Requests() []observedRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]observedRequest{}, g.requests...)
}

type runFixture struct {
	t                      *testing.T
	Dir                    string
	Process                cli.Process
	Cancel                 context.CancelCauseFunc
	Done                   chan int
	Client                 *mcp.Client
	HTTP                   *http.Client
	Base                   string
	Telemetry              *telemetry.Capture
	Events                 *events.Capture
	Out, Err               *outputBuffer
	Mail                   *fakeMail
	mu                     sync.Mutex
	Connections, Artifacts []observedRequest
	ArtifactData           map[string][]byte
	stopped                bool
}

func (f *runFixture) SiblingRequests() ([]observedRequest, []observedRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]observedRequest{}, f.Connections...), append([]observedRequest{}, f.Artifacts...)
}
func (f *runFixture) artifact(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Artifacts = append(f.Artifacts, observedRequest{r.Method, r.URL.Path, r.Header.Clone(), b})
	p := strings.TrimPrefix(r.URL.Path, "/files/")
	data, ok := f.ArtifactData[p]
	if r.Method == http.MethodPut {
		data = b
		f.ArtifactData[p] = data
		ok = true
	}
	if !ok {
		w.WriteHeader(404)
		return
	}
	w.Header().Set("ETag", `"`+strings.Repeat("a", 64)+`"`)
	w.Header().Set("Last-Modified", "Thu, 01 Oct 2026 00:00:00 GMT")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
		return
	}
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "artifact", "path": p, "content_type": "application/octet-stream", "sha256": strings.Repeat("a", 64), "owner": r.Header.Get("X-User-Id"), "url": "https://artifacts.test/artifact", "bytes": len(data), "created": "2026-10-01T00:00:00Z", "modified": "2026-10-01T00:00:00Z"})
}
func startRun(t *testing.T, o runOptions) *runFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &runFixture{t: t, Dir: o.Dir, Done: make(chan int, 1), Telemetry: &telemetry.Capture{}, Events: &events.Capture{}, Out: &outputBuffer{}, Err: &outputBuffer{}, ArtifactData: map[string][]byte{"push.bin": []byte("push")}, Mail: &fakeMail{drafts: map[string]string{}}}
	if f.Dir == "" {
		f.Dir = t.TempDir()
	}
	socketDir, err := os.MkdirTemp("", "gmail-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	entries := []services.Entry{}
	sibling := func(name string, h http.Handler) {
		ln, e := net.Listen("unix", filepath.Join(socketDir, name))
		if e != nil {
			t.Fatal(e)
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })
		entries = append(entries, services.Entry{Name: name, Socket: ln.Addr().String(), URL: "https://" + name + ".test"})
	}
	if !o.MissingConnections {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.Connections = append(f.Connections, observedRequest{r.Method, r.URL.Path, r.Header.Clone(), nil})
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-token", "expires": "2099-01-01T00:00:00Z", "account": "sender@example.test", "scopes": []string{}})
		})
		if o.ConnectionsHandler != nil {
			h = func(w http.ResponseWriter, r *http.Request) {
				f.mu.Lock()
				f.Connections = append(f.Connections, observedRequest{r.Method, r.URL.Path, r.Header.Clone(), nil})
				f.mu.Unlock()
				o.ConnectionsHandler.ServeHTTP(w, r)
			}
		}
		sibling("connections", h)
		if o.ConnectionsURL != "" {
			entries[len(entries)-1].URL = o.ConnectionsURL
		}
	}
	if !o.MissingArtifacts {
		sibling("artifacts", http.HandlerFunc(f.artifact))
	}
	if o.DefaultSinks {
		sibling("telemetry", telemetry.IngestHandler(f.Telemetry))
		sibling("events", events.EmitHandler(f.Events))
	}
	path := filepath.Join(socketDir, "services.json")
	rows := []map[string]any{}
	for _, e := range entries {
		rows = append(rows, map[string]any{"name": e.Name, "url": e.URL, "socket": e.Socket, "description": "fixture", "enabled": true, "mcp": false})
	}
	b, _ := json.Marshal(map[string]any{"services": rows})
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if o.DefaultSinks {
		t.Setenv(services.Variable, path)
	}
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(socketDir, "notify"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), services.Variable: path}
	for k, v := range o.Env {
		env[k] = v
	}
	if o.ServicesPath != nil {
		env[services.Variable] = *o.ServicesPath
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	}
	after := o.After
	if after == nil {
		after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	}
	rnd := o.Rand
	if rnd == nil {
		rnd = &runRandom{}
	}
	tr := o.Transport
	if tr == nil {
		tr = f.Mail
	}
	sink := o.Sink
	if sink == nil {
		sink = f.Telemetry
	}
	es := o.EventSink
	if es == nil {
		es = f.Events
	}
	f.Process = cli.Process{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Unsetenv: func(string) error { return nil }, Pid: 123, Stdout: f.Out, Stderr: f.Err, Version: "fixture-version", Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("fd %d", fd)
		}
		return ln, nil
	}, Now: now, Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, After: after, Rand: rnd, Dir: f.Dir, Sink: sink, EventSink: es, Banner: func(page.User) page.Banner { return page.Banner{Service: "fixture"} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "fixture", Version: "fixture-version", Telemetry: w})
	}, HTTP: &http.Client{Transport: tr}}
	if o.DefaultSinks {
		f.Process.Sink = nil
		f.Process.EventSink = nil
	}
	if o.Modify != nil {
		o.Modify(&f.Process)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	f.Cancel = cancel
	go func() { f.Done <- cli.Run(ctx, f.Process) }()
	_ = notify.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 64)
	n, _, err := notify.ReadFromUnix(buf)
	if err != nil {
		cancel(fmt.Errorf("fixture stopped"))
		select {
		case code := <-f.Done:
			t.Fatalf("ready: %v; code %d; stderr %s", err, code, f.Err.String())
		default:
			t.Fatalf("ready: %v; stderr %s", err, f.Err.String())
		}
	}
	if string(buf[:n]) != "READY=1" {
		t.Fatalf("notify %q", buf[:n])
	}
	f.Base = "http://" + ln.Addr().String()
	f.HTTP = &http.Client{}
	f.Client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.Base + "/mcp", HTTPClient: f.HTTP, Name: "test", Version: "test"})
	t.Cleanup(func() {
		if !f.stopped {
			f.stop()
		}
	})
	return f
}
func (f *runFixture) stop() int {
	f.t.Helper()
	f.Cancel(fmt.Errorf("fixture stop"))
	select {
	case code := <-f.Done:
		f.stopped = true
		return code
	case <-time.After(10 * time.Second):
		f.t.Fatal("run did not stop")
		return -1
	}
}

var fixtureCaller = identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "request"}

func (f *runFixture) call(c identity.Caller, name string, args any) mcp.Result {
	f.t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := f.Client.CallTool(ctx, c, name, b)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}
func resultJSON(t *testing.T, r mcp.Result) map[string]any {
	t.Helper()
	var v map[string]any
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err = json.Unmarshal(b, &envelope); err != nil {
		t.Fatal(err)
	}
	payload := envelope.Structured
	if len(payload) == 0 && len(envelope.Content) > 0 {
		payload = []byte(envelope.Content[0].Text)
	}
	if err = json.Unmarshal(payload, &v); err != nil {
		t.Fatalf("result %s: %v", b, err)
	}
	return v
}
func (f *runFixture) awaitTransfer(c identity.Caller, id string) map[string]any {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			f.t.Fatal("transfer did not finish")
		default:
		}
		r := f.call(c, "transfer", map[string]any{"transfer": id})
		v := resultJSON(f.t, r)
		if v["status"] == "done" || v["status"] == "failed" {
			return v
		}
	}
}
func (f *runFixture) reopen() *store.Store {
	f.t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(f.Dir, "state", "gmail.db"), Migrations: app.Migrations(), Now: f.Process.Now})
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = d.Close() })
	return store.New(d, store.Config{Now: f.Process.Now, Rand: &runRandom{}})
}

func resultText(t *testing.T, r mcp.Result) string {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if e = json.Unmarshal(b, &v); e != nil || len(v.Content) != 1 {
		t.Fatalf("result %s", b)
	}
	return v.Content[0].Text
}

func assertRefusal(t *testing.T, r mcp.Result, text string) {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(b, &envelope); err != nil {
		t.Fatal(err)
	}
	if !r.IsError() {
		t.Fatalf("accepted: %s", b)
	}
	if _, ok := envelope["structuredContent"]; ok {
		t.Fatalf("structured refusal: %s", b)
	}
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err = json.Unmarshal(envelope["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Type != "text" || content[0].Text != text {
		t.Fatalf("refusal: %s", b)
	}
}
