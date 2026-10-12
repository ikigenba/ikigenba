package transfers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

type workRequest struct {
	service, method, path, format string
	headers                       http.Header
	body                          []byte
}
type workDrafts struct{ st *store.Store }

func (d workDrafts) Attachable(ctx context.Context, c identity.Caller, _ connections.Token, id string, _ store.Attachment) (store.Draft, error) {
	return d.st.GetDraft(ctx, c.UserID, id)
}

type workFixture struct {
	e            *Engine
	st           *store.Store
	db           *db.DB
	writer       *telemetry.Writer
	emitter      *events.Emitter
	capture      *telemetry.Capture
	bus          *events.Capture
	mu           sync.Mutex
	requests     []workRequest
	tokens       int
	upload       []byte
	url          string
	payload      string
	files        map[string]string
	tokenHook    func(http.ResponseWriter, *http.Request, int) bool
	artifactHook func(http.ResponseWriter, *http.Request) bool
	gmailHook    func(http.ResponseWriter, *http.Request) bool
}

func newWorkFixture(t *testing.T) *workFixture {
	t.Helper()
	st, d := testStore(t)
	f := &workFixture{st: st, db: d, payload: "payload", files: map[string]string{"new.bin": "new payload", "old.bin": "replaced old payload"}, capture: &telemetry.Capture{}, bus: &events.Capture{}}
	f.writer = telemetry.New(telemetry.Config{Service: "gmail", Version: "fixture", Sink: f.capture, Stderr: io.Discard, Now: fClock, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 65536))})
	f.emitter = events.New(events.Config{Service: "gmail", Sink: f.bus, Stderr: io.Discard, Now: fClock, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 65536)), Emits: trail.Emits(), Telemetry: f.writer})
	t.Cleanup(func() { f.emitter.Shutdown(context.Background()); f.writer.Shutdown(context.Background(), "fixture") })
	cc := connections.NewClient(connections.ClientConfig{Telemetry: f.writer, Service: testSocket(t, "connection", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record("connections", r, nil)
		f.mu.Lock()
		f.tokens++
		n := f.tokens
		f.mu.Unlock()
		if f.tokenHook != nil && f.tokenHook(w, r, n) {
			return
		}
		token := "fresh-token"
		if n == 1 {
			token = "validation-token"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires": "2026-10-12T12:00:00Z", "account": "sender@fixture.invalid", "scopes": []string{}})
	}))})
	ac := artifacts.NewClient(artifacts.ClientConfig{Telemetry: f.writer, Service: testSocket(t, "artifact", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record("artifacts", r, nil)
		if f.artifactHook != nil && f.artifactHook(w, r) {
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/files/")
		switch r.Method {
		case http.MethodHead:
			workFileHeaders(w, int64(len(f.files[path])))
		case http.MethodGet:
			workFileHeaders(w, int64(len(f.files[path])))
			_, _ = io.WriteString(w, f.files[path])
		case http.MethodPut:
			b, err := io.ReadAll(r.Body)
			if err != nil {
				return
			}
			f.record("put-body", r, b)
			workMetadata(w, path, int64(len(b)))
		}
	}))})
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record("gmail", r, nil)
		if f.gmailHook != nil && f.gmailHook(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/gmail/v1/users/me/labels":
			_, _ = io.WriteString(w, `{"labels":[]}`)
		case strings.HasPrefix(r.URL.Path, "/upload/"):
			w.Header().Set("Location", f.url+"/session")
			w.WriteHeader(200)
		case r.URL.Path == "/session":
			b, err := io.ReadAll(r.Body)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.upload = append(f.upload, b...)
			f.mu.Unlock()
			rangeValue := r.Header.Get("Content-Range")
			var start, end, total int64
			_, _ = fmt.Sscanf(rangeValue, "bytes %d-%d/%d", &start, &end, &total)
			if end+1 < total {
				w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", end))
				w.WriteHeader(308)
			} else {
				_, _ = io.WriteString(w, `{"id":"remote-draft","message":{"id":"updated-message","threadId":"updated-thread"}}`)
			}
		case strings.Contains(r.URL.Path, "/attachments/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"size": len(f.payload), "data": base64.URLEncoding.EncodeToString([]byte(f.payload))})
		case r.URL.Query().Get("format") == "raw":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "message", "raw": base64.URLEncoding.EncodeToString([]byte(f.payload))})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "message", "threadId": "thread", "sizeEstimate": len(f.payload), "payload": map[string]any{"headers": []map[string]string{{"name": "Subject", "value": "chosen subject"}}, "parts": []any{map[string]any{"partId": "2", "filename": "part.bin", "mimeType": "application/example", "body": map[string]any{"size": len(f.payload), "attachmentId": "resolved-part"}}}}})
		}
	}))
	f.url = gs.URL
	t.Cleanup(gs.Close)
	f.e = New(Config{Store: st, Gmail: gmail.NewClient(gmail.Config{HTTP: gs.Client(), BaseURL: gs.URL, Telemetry: f.writer}), Artifacts: ac, Connections: cc, Drafts: workDrafts{st}, Telemetry: f.writer, Events: f.emitter, ConnectURL: "https://connect.fixture.invalid", MaxBytes: 40 << 20, MaxActive: 2, MaxQueued: 32, Now: fClock})
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); f.e.Drain(ctx) })
	return f
}
func workFileHeaders(w http.ResponseWriter, n int64) {
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	w.Header().Set("Content-Type", "application/example")
	w.Header().Set("ETag", "\"fixture-hash\"")
	w.Header().Set("Last-Modified", fClock().UTC().Format(http.TimeFormat))
}
func workMetadata(w http.ResponseWriter, path string, n int64) {
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "artifact-id", "path": path, "bytes": n, "content_type": "application/example", "sha256": "fixture-hash", "owner": "caller", "created": fClock().Format(time.RFC3339), "modified": fClock().Format(time.RFC3339), "url": "https://artifact.fixture.invalid/" + path})
}
func (f *workFixture) record(service string, r *http.Request, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, workRequest{service, r.Method, r.URL.Path, r.URL.Query().Get("format"), r.Header.Clone(), body})
}
func (f *workFixture) snapshot() []workRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]workRequest(nil), f.requests...)
}
func workAwait(t *testing.T, condition func() bool) {
	t.Helper()
	workAwaitFor(t, time.Second, condition)
}
func workAwaitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for !condition() {
		select {
		case <-ctx.Done():
			t.Fatal("condition did not become true before deadline")
		default:
			runtime.Gosched()
		}
	}
}
func workEnd(t *testing.T, f *workFixture, tr store.Transfer) store.Transfer {
	t.Helper()
	var got store.Transfer
	workAwaitFor(t, 5*time.Second, func() bool {
		var err error
		got, err = f.st.GetTransfer(context.Background(), tr.UserID, tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.Status == store.StatusDone || got.Status == store.StatusFailed
	})
	return got
}
func workPull(ctx context.Context, t *testing.T, f *workFixture, attachment string) store.Transfer {
	t.Helper()
	tr, err := f.e.Pull(ctx, testCaller, PullRequest{Message: "message", Attachment: attachment, Path: "pulled.bin"})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}
func workDraft(t *testing.T, f *workFixture, attachments []store.Attachment) store.Draft {
	t.Helper()
	d, err := f.st.AddDraft(context.Background(), store.Draft{UserID: testCaller.UserID, GmailDraft: "remote-draft", Message: "original-message", Thread: "original-thread", To: []string{"recipient@fixture.invalid"}, Subject: "stored subject", Body: "stored body", HeaderInReplyTo: "<reply@fixture.invalid>", HeaderReferences: "<reference@fixture.invalid>", Attachments: attachments})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range attachments {
		d, err = f.st.AttachToDraft(context.Background(), d.ID, a, d.Message, d.Thread)
		if err != nil {
			t.Fatal(err)
		}
	}
	return d
}
func workPush(t *testing.T, f *workFixture, d store.Draft) store.Transfer {
	t.Helper()
	tr, err := f.e.Push(context.Background(), testCaller, PushRequest{Path: "new.bin", Draft: d.ID})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// R-IFXC-WCSY R-IH59-A4JN R-IY9Q-CU16 R-IJL2-1O11
func TestWorkPullIdentityFreshTokenBytes(t *testing.T) {
	for _, attachment := range []string{"", "2"} {
		t.Run(attachment, func(t *testing.T) {
			f := newWorkFixture(t)
			gate := make(chan struct{})
			entered := make(chan struct{})
			f.tokenHook = func(_ http.ResponseWriter, r *http.Request, n int) bool {
				if n == 2 {
					close(entered)
					select {
					case <-gate:
					case <-r.Context().Done():
						return true
					}
				}
				return false
			}
			ctx, cancel := context.WithCancel(context.Background())
			expiry := fClock().Add(time.Hour).Format(time.RFC3339)
			tr, err := f.e.Pull(ctx, testCaller, PullRequest{Message: "message", Attachment: attachment, Path: "pulled.bin", Expires: &expiry})
			if err != nil {
				t.Fatal(err)
			}
			workAwait(t, func() bool {
				select {
				case <-entered:
					return true
				default:
					return false
				}
			})
			cancel()
			close(gate)
			got := workEnd(t, f, tr)
			if got.Status != store.StatusDone || got.Bytes != int64(len(f.payload)) || got.URL != "https://artifact.fixture.invalid/pulled.bin" || got.ArtifactID != "artifact-id" || got.Message != "message" || got.Link != urls.MessageLink("message") {
				t.Fatalf("ending %+v", got)
			}
			puts, tokenRequests := 0, 0
			for _, r := range f.snapshot() {
				if r.service == "connections" {
					tokenRequests++
				}
				if r.service == "gmail" && tokenRequests >= 2 && r.headers.Get("Authorization") != "" && r.headers.Get("Authorization") != "Bearer fresh-token" {
					t.Fatal("work used stale token", r)
				}
				if r.service == "artifacts" || r.service == "connections" {
					if r.headers.Get("X-User-Id") != tr.UserID || r.headers.Get("X-User-Email") != tr.UserEmail || r.headers.Get("X-Request-Id") != tr.RequestID {
						t.Fatal(r)
					}
				}
				if r.service == "gmail" && ((attachment == "" && r.format == "raw") || strings.Contains(r.path, "/attachments/")) {
					if r.headers.Get("Authorization") != "Bearer fresh-token" {
						t.Fatal("stale work token")
					}
				}
				if r.service == "put-body" {
					puts++
					if string(r.body) != f.payload || r.headers.Get("Content-Type") != tr.ContentType || r.headers.Get("X-Artifact-Expires") != expiry {
						t.Fatal(r)
					}
				}
			}
			if puts != 1 {
				t.Fatalf("puts=%d", puts)
			}
			_ = f.writer.Flush(context.Background())
			for _, ev := range f.capture.Events() {
				if ev.Name == gmail.Called && (ev.RequestID != tr.RequestID || ev.User != tr.UserID) {
					t.Fatal(ev)
				}
			}
		})
	}
}

// R-WTSX-9RT2 R-WV0T-NJJR R-IN8R-6Z94
func TestWorkPullFailureMapping(t *testing.T) {
	cases := []struct {
		name, stage, body, reason string
		status                    int
	}{{"token disconnected", "token", `{"error":"not_connected"}`, store.ReasonReconnect, 404}, {"token reconnect", "token", `{"error":"reconnect","url":"https://repair.fixture.invalid"}`, store.ReasonReconnect, 409}, {"token unavailable", "token", `{"error":"unavailable"}`, store.ReasonProvider, 503}, {"gone", "gmail", "", store.ReasonNotFound, 404}, {"grant", "gmail", "", store.ReasonReconnect, 401}, {"large", "gmail", "", store.ReasonTooLarge, 413}, {"bad provider", "gmail", "", store.ReasonProvider, 500}, {"forbidden", "put", "", store.ReasonForbidden, 403}, {"destination large", "put", "", store.ReasonTooLarge, 413}, {"bad path", "put", "", store.ReasonArtifacts, 400}, {"unavailable", "put", "", store.ReasonArtifacts, 503}, {"other status", "put", "", store.ReasonArtifacts, 418}, {"bad metadata", "put", "{", store.ReasonArtifacts, 200}}
	for _, attachment := range []string{"", "2"} {
		for _, tc := range cases {
			t.Run(tc.name+attachment, func(t *testing.T) {
				f := newWorkFixture(t)
				f.tokenHook = func(w http.ResponseWriter, _ *http.Request, n int) bool {
					if tc.stage != "token" || n != 2 {
						return false
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
					return true
				}
				f.gmailHook = func(w http.ResponseWriter, r *http.Request) bool {
					isBytes := r.URL.Query().Get("format") == "raw" || strings.Contains(r.URL.Path, "/attachments/")
					if tc.stage != "gmail" || !isBytes {
						return false
					}
					w.WriteHeader(tc.status)
					return true
				}
				f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
					if tc.stage != "put" || r.Method != http.MethodPut {
						return false
					}
					b, _ := io.ReadAll(r.Body)
					f.record("put-body", r, b)
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
					return true
				}
				got := workEnd(t, f, workPull(context.Background(), t, f, attachment))
				if got.Status != store.StatusFailed || got.Reason != tc.reason {
					t.Fatal(got)
				}
				if tc.stage == "put" && got.Bytes != int64(len(f.payload)) {
					t.Fatalf("moved=%d want=%d", got.Bytes, len(f.payload))
				}
				if tc.stage != "put" && got.Bytes != 0 {
					t.Fatal("moved bytes on source failure")
				}
				for _, r := range f.snapshot() {
					if tc.stage != "put" && r.service == "artifacts" && r.method == http.MethodPut {
						t.Fatal("put after source failure")
					}
					if tc.stage == "token" && r.service == "gmail" && r.headers.Get("Authorization") == "Bearer fresh-token" {
						t.Fatal("Gmail after token failure")
					}
				}
			})
		}
	}
	// Runtime size has grown beyond validation's estimate.
	for _, attachment := range []string{"", "2"} {
		t.Run("grown"+attachment, func(t *testing.T) {
			f := newWorkFixture(t)
			f.e.cfg.MaxBytes = 10
			f.gmailHook = func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Query().Get("format") == "raw" {
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "message", "raw": base64.URLEncoding.EncodeToString(bytes.Repeat([]byte("x"), 11))})
					return true
				}
				if strings.Contains(r.URL.Path, "/attachments/") {
					_ = json.NewEncoder(w).Encode(map[string]any{"size": 11, "data": base64.URLEncoding.EncodeToString(bytes.Repeat([]byte("x"), 11))})
					return true
				}
				return false
			}
			got := workEnd(t, f, workPull(context.Background(), t, f, attachment))
			if got.Reason != store.ReasonTooLarge || got.Bytes != 0 {
				t.Fatal(got)
			}
			for _, r := range f.snapshot() {
				if r.service == "artifacts" && r.method == http.MethodPut {
					t.Fatal("put oversized")
				}
			}
		})
	}
}

// R-IPOJ-YIQI R-WW8Q-1BAG R-5W7P-YKGR
func TestWorkPushRebuildAndCommit(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, []store.Attachment{{Path: "old.bin", Name: "saved name", ContentType: "text/plain", Bytes: 1}})
	got := workEnd(t, f, workPush(t, f, d))
	if got.Status != store.StatusDone || got.Bytes != int64(len(f.files["new.bin"])) || got.Message != "updated-message" || got.Link != urls.DraftLink("updated-message") || got.ArtifactID != "" || got.URL != "http://artifact.example.test/files/new.bin" {
		t.Fatal(got)
	}
	saved, err := f.st.GetDraft(context.Background(), d.UserID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]store.Attachment(nil), d.Attachments...), store.Attachment{Path: "new.bin", Name: "new.bin", ContentType: "application/example", Bytes: got.Bytes})
	if !reflect.DeepEqual(saved.Attachments, want) || saved.Message != got.Message || saved.Thread != "updated-thread" {
		t.Fatal(saved)
	}
	gets := []string{}
	uploadStarted := false
	for _, r := range f.snapshot() {
		if r.service == "gmail" {
			uploadStarted = true
			if r.path == "/upload/gmail/v1/users/me/drafts/remote-draft" && r.headers.Get("Authorization") != "Bearer fresh-token" {
				t.Fatal(r)
			}
		}
		if r.service == "artifacts" && r.method == http.MethodGet {
			if uploadStarted {
				t.Fatal("late Get")
			}
			gets = append(gets, r.path)
			if r.headers.Get("X-User-Id") != testCaller.UserID {
				t.Fatal(r)
			}
		}
	}
	if !reflect.DeepEqual(gets, []string{"/files/old.bin", "/files/new.bin"}) {
		t.Fatal(gets)
	}
	f.mu.Lock()
	raw := append([]byte(nil), f.upload...)
	f.mu.Unlock()
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	from, err := m.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Address != "sender@fixture.invalid" {
		t.Fatal(from, err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || subject != d.Subject || m.Header.Get("In-Reply-To") != d.HeaderInReplyTo || m.Header.Get("References") != d.HeaderReferences {
		t.Fatal(m.Header, err)
	}
	_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	for i, expected := range []string{d.Body, f.files["old.bin"], f.files["new.bin"]} {
		p, e := mr.NextPart()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		if e != nil || string(b) != expected {
			t.Fatalf("part %d body=%q err=%v", i, b, e)
		}
	}
	if _, err = mr.NextPart(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

// R-WXGM-F315 R-IVS1-VDFZ
func TestWorkPushFailures(t *testing.T) {
	cases := []struct {
		name, stage, body, reason string
		status                    int
	}{{"not connected", "token", `{"error":"not_connected"}`, store.ReasonReconnect, 404}, {"reconnect", "token", `{"error":"reconnect","url":"repair"}`, store.ReasonReconnect, 409}, {"provider token", "token", `{"error":"provider"}`, store.ReasonProvider, 502}, {"gone draft", "gmail", "", store.ReasonNotFound, 404}, {"unauthorized", "gmail", "", store.ReasonReconnect, 401}, {"too large", "gmail", "", store.ReasonTooLarge, 413}, {"provider upload", "gmail", "", store.ReasonProvider, 500}, {"bad stream", "get", "", store.ReasonArtifacts, 200}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWorkFixture(t)
			d := workDraft(t, f, nil)
			f.tokenHook = func(w http.ResponseWriter, _ *http.Request, n int) bool {
				if tc.stage != "token" || n != 2 {
					return false
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
				return true
			}
			f.gmailHook = func(w http.ResponseWriter, r *http.Request) bool {
				if tc.stage != "gmail" || !strings.HasPrefix(r.URL.Path, "/upload/") {
					return false
				}
				w.WriteHeader(tc.status)
				return true
			}
			f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
				if tc.stage != "get" || r.Method != http.MethodGet {
					return false
				}
				workFileHeaders(w, int64(len(f.files["new.bin"])+1))
				_, _ = io.WriteString(w, f.files["new.bin"])
				return true
			}
			got := workEnd(t, f, workPush(t, f, d))
			if got.Status != store.StatusFailed || got.Reason != tc.reason {
				t.Fatal(got)
			}
			wantBytes := int64(0)
			if tc.stage == "get" {
				wantBytes = int64(len(f.files["new.bin"]))
			}
			if got.Bytes != wantBytes {
				t.Fatalf("bytes=%d want=%d", got.Bytes, wantBytes)
			}
			saved, err := f.st.GetDraft(context.Background(), d.UserID, d.ID)
			if err != nil || !reflect.DeepEqual(saved, d) {
				t.Fatal(saved, err)
			}
			for _, r := range f.snapshot() {
				if tc.stage == "token" && ((r.service == "artifacts" && r.method == http.MethodGet) || r.service == "gmail") {
					t.Fatal("requests after token failure", r)
				}
			}
		})
	}
}

// R-IOGN-KQZT
func TestWorkPushEndedDraft(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, nil)
	f.tokenHook = func(_ http.ResponseWriter, _ *http.Request, n int) bool {
		if n == 2 {
			if _, err := f.st.MarkDraftDiscarded(context.Background(), d.ID); err != nil {
				t.Error(err)
			}
		}
		return false
	}
	got := workEnd(t, f, workPush(t, f, d))
	if got.Reason != store.ReasonNotFound || got.Bytes != 0 {
		t.Fatal(got)
	}
	for _, r := range f.snapshot() {
		if r.service == "gmail" || (r.service == "artifacts" && r.method == http.MethodGet) {
			t.Fatal("request for ended draft", r)
		}
	}
}

// R-K45G-LS9A
func TestWorkPushDraftEndsDuringUpload(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, nil)
	f.gmailHook = func(_ http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/session" {
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Error(err)
			}
			if _, err := f.st.MarkDraftDiscarded(context.Background(), d.ID); err != nil {
				t.Error(err)
			}
		}
		return false
	}
	got := workEnd(t, f, workPush(t, f, d))
	if got.Reason != store.ReasonNotFound || got.Bytes != int64(len(f.files["new.bin"])) {
		t.Fatal(got)
	}
	saved, err := f.st.GetDraft(context.Background(), d.UserID, d.ID)
	if err != nil || len(saved.Attachments) != 0 || saved.Status != store.DraftDiscarded {
		t.Fatal(saved, err)
	}
}

// R-IPOJ-YIQI R-IWZY-956O
func TestWorkPushGetFailureClosesEarlierBody(t *testing.T) {
	for _, status := range []int{404, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			f := newWorkFixture(t)
			d := workDraft(t, f, []store.Attachment{{Path: "old.bin", Name: "old.bin", ContentType: "text/plain", Bytes: 1}})
			closed := make(chan struct{})
			f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method != http.MethodGet {
					return false
				}
				if r.URL.Path == "/files/old.bin" {
					workFileHeaders(w, 100)
					_, _ = io.WriteString(w, "x")
					if flush, ok := w.(http.Flusher); ok {
						flush.Flush()
					}
					<-r.Context().Done()
					close(closed)
				} else {
					w.WriteHeader(status)
				}
				return true
			}
			got := workEnd(t, f, workPush(t, f, d))
			want := store.ReasonArtifacts
			if status == 404 {
				want = store.ReasonNotFound
			}
			if got.Reason != want || got.Bytes != 0 {
				t.Fatal(got)
			}
			workAwait(t, func() bool {
				select {
				case <-closed:
					return true
				default:
					return false
				}
			})
			for _, r := range f.snapshot() {
				if r.service == "gmail" {
					t.Fatal("Gmail after Get failure")
				}
			}
			saved, err := f.st.GetDraft(context.Background(), d.UserID, d.ID)
			if err != nil || !reflect.DeepEqual(saved, d) {
				t.Fatal(saved, err)
			}
		})
	}
}

// R-IWZY-956O
func TestWorkPushUploadFailureClosesUnreadBody(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, nil)
	closed := make(chan struct{})
	f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet {
			return false
		}
		workFileHeaders(w, 100)
		_, _ = io.WriteString(w, "x")
		if flush, ok := w.(http.Flusher); ok {
			flush.Flush()
		}
		<-r.Context().Done()
		close(closed)
		return true
	}
	f.gmailHook = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, "/upload/") {
			return false
		}
		w.WriteHeader(500)
		return true
	}
	got := workEnd(t, f, workPush(t, f, d))
	if got.Reason != store.ReasonProvider {
		t.Fatal(got)
	}
	workAwait(t, func() bool {
		select {
		case <-closed:
			return true
		default:
			return false
		}
	})
}

// R-WYOI-SURU
func TestWorkPushStreamsBeforeArtifactEnds(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, nil)
	total := 2*gmail.PushChunk + 257
	f.files["new.bin"] = strings.Repeat("x", total)
	firstPiece := make(chan struct{})
	release := make(chan struct{})
	sourceWaiting := make(chan struct{})
	f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet {
			return false
		}
		workFileHeaders(w, int64(total))
		_, err := io.WriteString(w, f.files["new.bin"][:2*gmail.PushChunk])
		if err != nil {
			return true
		}
		if flush, ok := w.(http.Flusher); ok {
			flush.Flush()
		}
		close(sourceWaiting)
		select {
		case <-release:
			_, _ = io.WriteString(w, f.files["new.bin"][2*gmail.PushChunk:])
		case <-r.Context().Done():
		}
		return true
	}
	f.gmailHook = func(_ http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/session" && strings.HasPrefix(r.Header.Get("Content-Range"), "bytes 0-") {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			if len(b) != gmail.PushChunk {
				t.Errorf("piece bytes=%d", len(b))
			}
			close(firstPiece)
		}
		return false
	}
	tr := workPush(t, f, d)
	workAwaitFor(t, 5*time.Second, func() bool {
		select {
		case <-firstPiece:
			return true
		default:
			return false
		}
	})
	workAwaitFor(t, 5*time.Second, func() bool {
		select {
		case <-sourceWaiting:
			return true
		default:
			return false
		}
	})
	close(release)
	got := workEnd(t, f, tr)
	if got.Status != store.StatusDone || got.Bytes != int64(total) {
		t.Fatal(got)
	}
}

// R-J1VJ-S85G
func TestWorkPushProgressWhileSourceWaits(t *testing.T) {
	f := newWorkFixture(t)
	d := workDraft(t, f, nil)
	prefix := int64(2 * ProgressStep)
	total := prefix + 123
	f.files["new.bin"] = strings.Repeat("p", int(total))
	waiting := make(chan struct{})
	release := make(chan struct{})
	f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet {
			return false
		}
		workFileHeaders(w, total)
		_, err := io.WriteString(w, f.files["new.bin"][:prefix])
		if err != nil {
			return true
		}
		if flush, ok := w.(http.Flusher); ok {
			flush.Flush()
		}
		close(waiting)
		select {
		case <-release:
			_, _ = io.WriteString(w, f.files["new.bin"][prefix:])
		case <-r.Context().Done():
		}
		return true
	}
	tr := workPush(t, f, d)
	workAwait(t, func() bool {
		select {
		case <-waiting:
			return true
		default:
			return false
		}
	})
	workAwait(t, func() bool {
		got, err := f.st.GetTransfer(context.Background(), tr.UserID, tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.StatusRunning || got.Bytes > prefix {
			t.Fatal(got)
		}
		return got.Bytes == prefix
	})
	close(release)
	got := workEnd(t, f, tr)
	if got.Bytes != total || got.Status != store.StatusDone {
		t.Fatal(got)
	}
}

// R-K5DC-ZJZZ
func TestWorkPullProgressFailureDoesNotStopCopy(t *testing.T) {
	f := newWorkFixture(t)
	f.payload = strings.Repeat("b", int(2*ProgressStep+4096))
	failed := make(chan struct{})
	f.gmailHook = func(_ http.ResponseWriter, r *http.Request) bool {
		if strings.Contains(r.URL.Path, "/attachments/") {
			f.db.SetFailing(true)
			close(failed)
		}
		return false
	}
	f.artifactHook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut {
			return false
		}
		buf := make([]byte, 2*ProgressStep)
		n, err := io.ReadFull(r.Body, buf)
		if err != nil {
			t.Error(err)
		}
		f.db.SetFailing(false)
		rest, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		f.record("put-body", r, append(buf[:n], rest...))
		workMetadata(w, "pulled.bin", int64(n+len(rest)))
		return true
	}
	tr := workPull(context.Background(), t, f, "2")
	workAwait(t, func() bool {
		select {
		case <-failed:
			return true
		default:
			return false
		}
	})
	var got store.Transfer
	workAwait(t, func() bool {
		value, err := f.st.GetTransfer(context.Background(), tr.UserID, tr.ID)
		if errors.Is(err, store.ErrUnreachable) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		got = value
		return got.Status == store.StatusDone || got.Status == store.StatusFailed
	})
	if got.Status != store.StatusDone || got.Bytes != int64(len(f.payload)) {
		t.Fatal(got)
	}
}
