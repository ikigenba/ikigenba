package transfers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
)

func testSocket(t *testing.T, name string, h http.Handler) services.Entry {
	t.Helper()
	dir, err := os.MkdirTemp("", "gmail-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, name)
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	return services.Entry{Name: name, Socket: socket, URL: "http://" + name + ".example.test"}
}
func testStore(t *testing.T) (*store.Store, *db.DB) {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 900000000, time.UTC) }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "gmail.db"), Migrations: app.Migrations(), Now: now, Service: "gmail", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	random := make([]byte, 8192)
	for i := range random {
		random[i] = byte(i / 8)
	}
	return store.New(d, store.Config{Now: now, Rand: bytes.NewReader(random)}), d
}

type draftFake struct {
	calls int
	c     identity.Caller
	tok   connections.Token
	id    string
	a     store.Attachment
	draft store.Draft
	err   error
}

func (d *draftFake) Attachable(ctx context.Context, _ identity.Caller, tok connections.Token, id string, a store.Attachment) (store.Draft, error) {
	d.calls++
	d.c, _ = identity.FromContext(ctx)
	d.tok = tok
	d.id = id
	d.a = a
	return d.draft, d.err
}

type validationFixture struct {
	capture                              *telemetry.Capture
	bus                                  *events.Capture
	writer                               *telemetry.Writer
	emitter                              *events.Emitter
	e                                    *Engine
	st                                   *store.Store
	db                                   *db.DB
	dr                                   *draftFake
	mu                                   sync.Mutex
	headStatus, gmailStatus, tokenStatus int
	tokenBody                            string
	headBytes, partBytes, messageBytes   int64
	calls                                []string
	headers                              []http.Header
}

func newValidationFixture(t *testing.T) *validationFixture {
	t.Helper()
	st, d := testStore(t)
	f := &validationFixture{st: st, db: d, headStatus: 404, gmailStatus: 200, tokenStatus: 200, headBytes: 4, partBytes: 4, messageBytes: 4, dr: &draftFake{draft: store.Draft{ID: "local-draft", Message: "draft-message"}}}
	draft, err := st.AddDraft(context.Background(), store.Draft{UserID: testCaller.UserID, GmailDraft: "remote-draft", Message: "draft-message"})
	if err != nil {
		t.Fatal(err)
	}
	f.dr.draft = draft
	now := fClock
	f.capture = &telemetry.Capture{}
	f.bus = &events.Capture{}
	f.writer = telemetry.New(telemetry.Config{Service: "gmail", Version: "test", Sink: f.capture, Stderr: io.Discard, Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 8192))})
	f.emitter = events.New(events.Config{Service: "gmail", Sink: f.bus, Stderr: io.Discard, Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 8192)), Emits: trail.Emits(), Telemetry: f.writer})
	t.Cleanup(func() { f.emitter.Shutdown(context.Background()); f.writer.Shutdown(context.Background(), "test") })
	cc := connections.NewClient(connections.ClientConfig{Telemetry: f.writer, Service: testSocket(t, "c", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		if r.URL.Path != "/token/"+Provider || r.Method != http.MethodGet {
			t.Errorf("token route %s %s", r.Method, r.URL.Path)
		}
		f.calls = append(f.calls, "token")
		f.headers = append(f.headers, r.Header.Clone())
		status, body := f.tokenStatus, f.tokenBody
		f.mu.Unlock()
		w.WriteHeader(status)
		if body != "" {
			_, _ = io.WriteString(w, body)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-token", "expires": "2026-10-12T12:00:00Z", "account": "sender@example.test", "scopes": []string{}})
	}))})
	ac := artifacts.NewClient(artifacts.ClientConfig{Telemetry: f.writer, Service: testSocket(t, "a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method)
		f.headers = append(f.headers, r.Header.Clone())
		status, n := f.headStatus, f.headBytes
		f.mu.Unlock()
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", jsonNumber(n))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("ETag", "\"hash\"")
			w.Header().Set("Last-Modified", "Sun, 11 Oct 2026 12:00:00 GMT")
			w.WriteHeader(status)
			return
		}
		<-r.Context().Done()
	}))})
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "gmail:"+r.Method+":"+r.URL.Path+":"+r.URL.Query().Get("format"))
		status, part, size := f.gmailStatus, f.partBytes, f.messageBytes
		f.mu.Unlock()
		if r.URL.Path == "/gmail/v1/users/me/labels" && status == 404 {
			status = 200
		}
		w.WriteHeader(status)
		if status != 200 {
			return
		}
		if r.URL.Path == "/gmail/v1/users/me/labels" {
			_, _ = io.WriteString(w, `{"labels":[]}`)
			return
		}
		if r.URL.Query().Get("format") == "raw" || r.URL.Path == "/gmail/v1/users/me/messages/message/attachments/attachment-id" {
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "message", "threadId": "thread", "sizeEstimate": size, "payload": map[string]any{"headers": []map[string]string{{"name": "Subject", "value": "chosen-subject"}}, "parts": []any{map[string]any{"partId": "2", "filename": "chosen-file", "mimeType": "application/octet-stream", "body": map[string]any{"size": part, "attachmentId": "attachment-id"}}}}})
	}))
	t.Cleanup(gs.Close)
	f.e = New(Config{Store: st, Telemetry: f.writer, Events: f.emitter, Gmail: gmail.NewClient(gmail.Config{HTTP: gs.Client(), BaseURL: gs.URL}), Artifacts: ac, Connections: cc, Drafts: f.dr, ConnectURL: "https://connect.example.test/google/connect", MaxBytes: 4, MaxActive: 1, MaxQueued: 1, Now: func() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 900000000, time.UTC) }})
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); f.e.Drain(ctx) })
	return f
}
func fClock() time.Time         { return time.Date(2026, 10, 11, 12, 0, 0, 900000000, time.UTC) }
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

var testCaller = identity.Caller{UserID: "caller", Email: "caller@example.test", RequestID: "request"}

func assertRefused(t *testing.T, f *validationFixture, got store.Transfer, err error, word, url string) {
	t.Helper()
	if got != (store.Transfer{}) {
		t.Fatalf("nonzero transfer: %+v", got)
	}
	var r *Refusal
	if !errors.As(err, &r) || r.Word != word || r.URL != url {
		t.Fatalf("refusal = %#v, %v; want %s %s", r, err, word, url)
	}
	ts, err := f.st.ListTransfers(context.Background(), testCaller.UserID)
	if err != nil || len(ts) != 0 {
		t.Fatalf("writes on refusal: %+v %v", ts, err)
	}
	_ = f.writer.Flush(context.Background())
	_ = f.emitter.Flush(context.Background())
	for _, ev := range f.capture.Events() {
		if ev.Name == trail.TransferStarted || ev.Name == trail.TransferFinished {
			t.Fatal("event on refusal", ev)
		}
	}
	if len(f.bus.Events()) != 0 {
		t.Fatal("bus event on refusal")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == "PUT" || (strings.HasPrefix(c, "gmail:") && !strings.HasPrefix(c, "gmail:GET:")) {
			t.Fatal("put on refusal")
		}
	}
}

// R-WMHI-Z5CW R-WNPF-CX3L
func TestComposition(t *testing.T) {
	d := store.Draft{To: []string{"Chosen <chosen@example.test>", "bad address", "last@example.test"}, Cc: []string{"cc@example.test", "bad address", "Copy <copy@example.test>"}, Bcc: []string{"Blind <bcc@example.test>", "bad address", "last-blind@example.test"}, Subject: "subject", Body: "body", HeaderInReplyTo: "reply", HeaderReferences: "references", Attachments: []store.Attachment{{Name: "first", ContentType: "text/plain", Bytes: 3}, {Name: "second", ContentType: "binary", Bytes: 5}}}
	c := Composition(d, "sender@example.test")
	if c.From != (mail.Address{Address: "sender@example.test"}) || !reflect.DeepEqual(c.To, []mail.Address{{Name: "Chosen", Address: "chosen@example.test"}, {Address: "last@example.test"}}) || !reflect.DeepEqual(c.Cc, []mail.Address{{Address: "cc@example.test"}, {Name: "Copy", Address: "copy@example.test"}}) || !reflect.DeepEqual(c.Bcc, []mail.Address{{Name: "Blind", Address: "bcc@example.test"}, {Address: "last-blind@example.test"}}) || c.Subject != d.Subject || c.Text != d.Body || c.InReplyTo != d.HeaderInReplyTo || c.References != d.HeaderReferences {
		t.Fatalf("composition %+v", c)
	}
	if len(c.Attachments) != len(d.Attachments) {
		t.Fatalf("attachments = %d; want %d", len(c.Attachments), len(d.Attachments))
	}
	for i, p := range c.Attachments {
		a := d.Attachments[i]
		if p.Name != a.Name || p.ContentType != a.ContentType || p.Bytes != a.Bytes || p.Open != nil {
			t.Fatalf("part %+v", p)
		}
	}
}

// R-HNVO-3MQZ
func TestConstants(t *testing.T) {
	words := []string{RefusalNotConnected, RefusalReconnect, RefusalExpires, RefusalTooLarge, RefusalNotFound, RefusalNotOpen, RefusalBusy, RefusalTo, RefusalAddress, RefusalProvider, RefusalPath, RefusalUnavailable, RefusalStopping, RefusalQueueFull}
	want := []string{"not_connected", "reconnect", "expires", "too_large", "not_found", "not_open", "busy", "to", "address", "provider", "path", "unavailable", "stopping", "queue_full"}
	if !reflect.DeepEqual(words, want) || Provider != "google" || ContentTypeMessage != "message/rfc822" || ProgressStep != 1<<20 {
		t.Fatal("constants differ")
	}
	for _, w := range words {
		r := &Refusal{Word: w, URL: "chosen"}
		if r.Error() != w {
			t.Fatal(r.Error())
		}
	}
}

// R-WL9M-LDM7 R-HMNR-PV0A R-HQBG-V68D
func TestTokenIdentityAndSeam(t *testing.T) {
	f := newValidationFixture(t)
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "wrong"})
	tok, err := Token(ctx, f.e.cfg.Connections, f.e.ConnectURL(), testCaller)
	if err != nil || tok.AccessToken != "test-token" || tok.Account != "sender@example.test" || len(tok.Scopes) != 0 || tok.Expires.Format(time.RFC3339) != "2026-10-12T12:00:00Z" {
		t.Fatalf("token %+v %v", tok, err)
	}
	value, err := f.e.Token(ctx, testCaller)
	if err != nil || value != tok.AccessToken || f.e.ConnectURL() != f.e.cfg.ConnectURL {
		t.Fatal(value, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
	for _, h := range f.headers {
		if h.Get("X-User-Id") != testCaller.UserID || h.Get("X-User-Email") != testCaller.Email || h.Get("X-Request-Id") != testCaller.RequestID {
			t.Fatal(h)
		}
	}
	var _ Drafts = f.dr
	_ = f.e.Recover
	_ = f.e.Drain
	_ = PullRequest{"message", "2", "file", nil}
	_ = PushRequest{"file", "draft"}
}

// R-HRJD-8XZ2 R-IDHK-4TBK
func TestTokenRefusals(t *testing.T) {
	for _, tc := range []struct {
		status          int
		body, word, url string
	}{{404, `{"error":"not_connected"}`, RefusalNotConnected, "connect"}, {409, `{"error":"reconnect","url":"repair"}`, RefusalReconnect, "repair"}, {404, `{"error":"unknown_provider"}`, RefusalProvider, ""}, {502, `{"error":"provider"}`, RefusalProvider, ""}, {503, `{"error":"unavailable"}`, RefusalUnavailable, ""}, {200, `{`, RefusalUnavailable, ""}, {418, `{}`, RefusalUnavailable, ""}} {
		t.Run(tc.word+jsonNumber(int64(tc.status)), func(t *testing.T) {
			f := newValidationFixture(t)
			f.tokenStatus, f.tokenBody = tc.status, tc.body
			tok, err := Token(context.Background(), f.e.cfg.Connections, "connect", testCaller)
			var r *Refusal
			if !reflect.DeepEqual(tok, connections.Token{}) || !errors.As(err, &r) || r.Word != tc.word || r.URL != tc.url {
				t.Fatalf("token %+v %v", tok, err)
			}
			value, err := f.e.Token(context.Background(), testCaller)
			if value != "" || !errors.As(err, &r) {
				t.Fatal(value, err)
			}
		})
	}
	tok, err := Token(context.Background(), nil, "connect", testCaller)
	var r *Refusal
	if !reflect.DeepEqual(tok, connections.Token{}) || !errors.As(err, &r) || r.Word != RefusalUnavailable {
		t.Fatal(tok, err)
	}
}

// R-HSR9-MPPR R-HP3K-HEHO R-I4Y9-GF4P R-I9TU-ZI3H
func TestTokenPrecedesAllValidation(t *testing.T) {
	for _, push := range []bool{false, true} {
		t.Run(jsonNumber(int64(boolNumber(push))), func(t *testing.T) {
			f := newValidationFixture(t)
			f.tokenStatus = 404
			f.tokenBody = `{"error":"not_connected"}`
			f.e.cfg.Artifacts = nil
			bad := "bad"
			var got store.Transfer
			var err error
			if push {
				got, err = f.e.Push(context.Background(), testCaller, PushRequest{Path: "invalid path", Draft: "absent"})
			} else {
				got, err = f.e.Pull(context.Background(), testCaller, PullRequest{Message: "absent", Path: "invalid path", Expires: &bad})
			}
			assertRefused(t, f, got, err, RefusalNotConnected, f.e.ConnectURL())
			f.mu.Lock()
			defer f.mu.Unlock()
			if !reflect.DeepEqual(f.calls, []string{"token"}) {
				t.Fatal(f.calls)
			}
		})
	}
}
func boolNumber(b bool) int {
	if b {
		return 1
	}
	return 0
}

// R-HTZ6-0HGG
func TestExpiryRefusals(t *testing.T) {
	for _, expiry := range []string{"", "garbage", "2026-10-11T12:00:00Z", "2026-10-11T11:59:59Z"} {
		t.Run(expiry, func(t *testing.T) {
			f := newValidationFixture(t)
			got, err := f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "file", Expires: &expiry})
			assertRefused(t, f, got, err, RefusalExpires, "")
			f.mu.Lock()
			defer f.mu.Unlock()
			if !reflect.DeepEqual(f.calls, []string{"token"}) {
				t.Fatal(f.calls)
			}
		})
	}
	f := newValidationFixture(t)
	expiry := "2026-10-11T12:00:01Z"
	got, err := f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "file", Expires: &expiry})
	if err != nil || got.ID == "" {
		t.Fatal(got, err)
	}
}

// R-HWEY-S0XU R-HXMV-5SOJ R-WOXB-QOUA R-WQ58-4GKZ R-WRD4-I8BO
func TestPullValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setup     func(*validationFixture)
		request   PullRequest
		word, url string
	}{{"bad path", func(f *validationFixture) { f.headStatus = 400 }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalPath, ""}, {"broken artifacts", func(f *validationFixture) { f.headStatus = 503 }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalUnavailable, ""}, {"missing artifacts", func(f *validationFixture) { f.e.cfg.Artifacts = nil }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalUnavailable, ""}, {"missing message", func(f *validationFixture) { f.gmailStatus = 404 }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalNotFound, ""}, {"expired grant", func(f *validationFixture) { f.gmailStatus = 401 }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalReconnect, "https://connect.example.test/google/connect"}, {"broken provider", func(f *validationFixture) { f.gmailStatus = 500 }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalProvider, ""}, {"missing part", func(*validationFixture) {}, PullRequest{Message: "message", Attachment: "9", Path: "file"}, RefusalNotFound, ""}, {"large part", func(f *validationFixture) { f.partBytes = 5 }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalTooLarge, ""}, {"large message", func(f *validationFixture) { f.messageBytes = 5 }, PullRequest{Message: "message", Path: "file"}, RefusalTooLarge, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newValidationFixture(t)
			tc.setup(f)
			got, err := f.e.Pull(context.Background(), testCaller, tc.request)
			assertRefused(t, f, got, err, tc.word, tc.url)
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.calls) > 1 && f.calls[1] != "HEAD" {
				t.Fatal(f.calls)
			}
			if tc.name == "bad path" || tc.name == "broken artifacts" {
				if len(f.calls) != 2 {
					t.Fatal(f.calls)
				}
			}
		})
	}
}

// R-I665-U6VE R-IB1R-D9U6 R-I8LY-LQCS
func TestAcceptedRecordsAndDraftArgument(t *testing.T) {
	for _, push := range []bool{false, true} {
		t.Run(jsonNumber(int64(boolNumber(push))), func(t *testing.T) {
			f := newValidationFixture(t)
			var got store.Transfer
			var err error
			if push {
				f.headStatus = 200
				got, err = f.e.Push(context.Background(), testCaller, PushRequest{Path: "folder/file", Draft: "draft-argument"})
			} else {
				got, err = f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "folder/file"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.UserID != testCaller.UserID || got.UserEmail != testCaller.Email || got.RequestID != testCaller.RequestID || got.Status != store.StatusRunning || got.Path != "folder/file" {
				t.Fatalf("record %+v", got)
			}
			if push {
				if got.Direction != store.DirectionPush || got.Draft != f.dr.draft.ID || got.Message != f.dr.draft.Message || got.Attachment != "" || got.Name != "file" || got.ContentType != "application/octet-stream" || f.dr.calls != 1 || f.dr.c != testCaller || f.dr.tok.Account != "sender@example.test" || f.dr.id != "draft-argument" || f.dr.a != (store.Attachment{Path: "folder/file", Name: "file", ContentType: "application/octet-stream", Bytes: 4}) {
					t.Fatalf("push %+v draft %+v", got, f.dr)
				}
			} else if got.Direction != store.DirectionPull || got.Message != "message" || got.Attachment != "2" || got.Draft != "" || got.Name != "chosen-file" || got.ContentType != "application/octet-stream" {
				t.Fatalf("pull %+v", got)
			}
			saved, err := f.st.GetTransfer(context.Background(), testCaller.UserID, got.ID)
			if err != nil || saved.ID != got.ID {
				t.Fatal(saved, err)
			}
		})
	}
}

// R-I7E2-7YM3 R-HXMV-5SOJ R-I8LY-LQCS
func TestPushValidation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		size         int64
		draftErr     error
		nilArtifacts bool
		word         string
	}{{"path", 400, 4, nil, false, RefusalPath}, {"not found", 404, 4, nil, false, RefusalNotFound}, {"unavailable", 503, 4, nil, false, RefusalUnavailable}, {"too large", 200, 5, nil, false, RefusalTooLarge}, {"no sibling", 200, 4, nil, true, RefusalUnavailable}, {"draft refusal", 200, 4, &Refusal{Word: RefusalNotOpen}, false, RefusalNotOpen}, {"draft db", 200, 4, errors.New("broken store"), false, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newValidationFixture(t)
			f.headStatus, f.headBytes, f.dr.err = tc.status, tc.size, tc.draftErr
			if tc.nilArtifacts {
				f.e.cfg.Artifacts = nil
			}
			got, err := f.e.Push(context.Background(), testCaller, PushRequest{Path: "file", Draft: "draft"})
			if tc.word != "" {
				assertRefused(t, f, got, err, tc.word, "")
			} else {
				var r *Refusal
				if got != (store.Transfer{}) || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &r) {
					t.Fatal(got, err)
				}
			}
			if tc.draftErr == nil && f.dr.calls != 0 {
				t.Fatal("called draft before artifact validation")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, call := range f.calls {
				if len(call) > 6 && call[:6] == "gmail:" {
					t.Fatal(f.calls)
				}
			}
		})
	}
}

// R-I2IG-OVNB R-K2XK-80IL R-I665-U6VE R-IB1R-D9U6
func TestStoppingAndQueueRefusals(t *testing.T) {
	for _, push := range []bool{false, true} {
		t.Run(jsonNumber(int64(boolNumber(push))), func(t *testing.T) {
			f := newValidationFixture(t)
			if push {
				f.headStatus = 200
			}
			call := func() (store.Transfer, error) {
				if push {
					return f.e.Push(context.Background(), testCaller, PushRequest{Path: "file", Draft: "draft"})
				}
				return f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "file"})
			}
			first, err := call()
			if err != nil || first.Status != store.StatusRunning {
				t.Fatal(first, err)
			}
			second, err := call()
			if err != nil || second.Status != store.StatusQueued {
				t.Fatal(second, err)
			}
			third, err := call()
			var r *Refusal
			if third != (store.Transfer{}) || !errors.As(err, &r) || r.Word != RefusalQueueFull {
				t.Fatal(third, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			f.e.Drain(ctx)
			last, err := call()
			if last != (store.Transfer{}) || !errors.As(err, &r) || r.Word != RefusalStopping {
				t.Fatal(last, err)
			}
		})
	}
}

// R-IC9N-R1KV
func TestStoreFailureAtAcceptance(t *testing.T) {
	for _, push := range []bool{false, true} {
		t.Run(jsonNumber(int64(boolNumber(push))), func(t *testing.T) {
			f := newValidationFixture(t)
			f.db.SetFailing(true)
			defer f.db.SetFailing(false)
			var got store.Transfer
			var err error
			if push {
				f.headStatus = 200
				got, err = f.e.Push(context.Background(), testCaller, PushRequest{Path: "file", Draft: "draft"})
			} else {
				got, err = f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Attachment: "2", Path: "file"})
			}
			var r *Refusal
			if got != (store.Transfer{}) || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &r) {
				t.Fatal(got, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, call := range f.calls {
				if call == "PUT" {
					t.Fatal(f.calls)
				}
			}
		})
	}
}

// R-I4Y9-GF4P R-I9TU-ZI3H
func TestValidationPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*validationFixture)
		req   PullRequest
		word  string
	}{
		{"expiry before path", func(f *validationFixture) { f.headStatus = 400; f.gmailStatus = 404 }, PullRequest{Message: "message", Attachment: "9", Path: "file", Expires: pointer("garbage")}, RefusalExpires},
		{"path before message", func(f *validationFixture) { f.headStatus = 400; f.gmailStatus = 404 }, PullRequest{Message: "message", Attachment: "9", Path: "file"}, RefusalPath},
		{"message before part", func(f *validationFixture) { f.gmailStatus = 404; f.partBytes = 5 }, PullRequest{Message: "message", Attachment: "9", Path: "file"}, RefusalNotFound},
		{"part before size", func(f *validationFixture) { f.partBytes = 5 }, PullRequest{Message: "message", Attachment: "9", Path: "file"}, RefusalNotFound},
		{"size before drain", func(f *validationFixture) { f.partBytes = 5; f.e.Drain(context.Background()) }, PullRequest{Message: "message", Attachment: "2", Path: "file"}, RefusalTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newValidationFixture(t)
			tc.setup(f)
			got, err := f.e.Pull(context.Background(), testCaller, tc.req)
			assertRefused(t, f, got, err, tc.word, "")
		})
	}
	for _, tc := range []struct {
		name   string
		status int
		size   int64
		word   string
	}{{"path before size", 400, 5, RefusalPath}, {"artifact size before draft", 200, 5, RefusalTooLarge}, {"draft before drain", 200, 4, RefusalBusy}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newValidationFixture(t)
			f.headStatus, f.headBytes = tc.status, tc.size
			f.dr.err = &Refusal{Word: RefusalBusy}
			f.e.Drain(context.Background())
			got, err := f.e.Push(context.Background(), testCaller, PushRequest{Path: "file", Draft: "draft"})
			assertRefused(t, f, got, err, tc.word, "")
		})
	}
}
func pointer(s string) *string { return &s }

// R-HWEY-S0XU R-WRD4-I8BO R-I665-U6VE
func TestWholeMessageBoundaryAndExistingArtifact(t *testing.T) {
	f := newValidationFixture(t)
	f.headStatus = 200
	got, err := f.e.Pull(context.Background(), testCaller, PullRequest{Message: "message", Path: "file"})
	if err != nil || got.Name != "chosen-subject" || got.ContentType != ContentTypeMessage || got.Attachment != "" || got.Message != "message" {
		t.Fatal(got, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	heads := 0
	stat := false
	for i, c := range f.calls {
		if c == "HEAD" {
			heads++
			if i != 1 {
				t.Fatal("head did not precede Gmail", f.calls)
			}
		}
		if strings.HasSuffix(c, ":minimal") {
			stat = true
		}
	}
	if heads != 1 || !stat {
		t.Fatal(f.calls)
	}
	for _, h := range f.headers {
		if h.Get("X-User-Id") != testCaller.UserID || h.Get("X-User-Email") != testCaller.Email || h.Get("X-Request-Id") != testCaller.RequestID {
			t.Fatal(h)
		}
	}
}
