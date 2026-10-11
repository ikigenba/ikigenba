package transfers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/trail"
	"github.com/ikigenba/ikigenba/gdrive/internal/transfers"
)

var caller = identity.Caller{UserID: "alice", Email: "alice@test", RequestID: "req-alice"}
var instant = time.Date(2026, 10, 11, 12, 0, 0, 900000000, time.UTC)

type sequence struct{ n byte }

func (s *sequence) Read(p []byte) (int, error) {
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

type request struct {
	Method, Path, Query, Auth, User, Email, ID string
	Body                                       []byte
	Header                                     http.Header
}
type rig struct {
	t                                      *testing.T
	db                                     *db.DB
	st                                     *store.Store
	e                                      *transfers.Engine
	cfg                                    transfers.Config
	mu                                     sync.Mutex
	reqs                                   []request
	tokenStatus                            int
	tokenBody                              string
	workTokenStatus                        int
	workTokenBody                          string
	tokenCount                             int
	headStatus                             int
	headSize                               int64
	putStatus                              int
	getSize                                *int64
	getType                                string
	getStatus                              int
	mediaStatus                            int
	showStatus                             int
	mime                                   string
	size                                   int64
	target, targetMime                     string
	payload                                string
	holdToken, holdMedia, holdPut          chan struct{}
	tokenEntered, mediaEntered, putEntered chan struct{}
	uploadStatus, chunkStatus              int
	mediaFailureBody                       string
	driveServer                            *httptest.Server
	sink                                   *telemetry.Capture
	bus                                    *events.Capture
	w                                      *telemetry.Writer
	em                                     *events.Emitter
}

func unixServer(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gdr-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); _ = os.RemoveAll(dir) })
	return socket
}
func (r *rig) record(req *http.Request, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, request{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Get("Authorization"), req.Header.Get("X-User-Id"), req.Header.Get("X-User-Email"), req.Header.Get("X-Request-Id"), body, req.Header.Clone()})
}
func (r *rig) snapshot() []request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]request(nil), r.reqs...)
}
func waitHold(ctx context.Context, hold chan struct{}, entered chan struct{}) {
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
}
func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv(services.Variable, "")
	r := &rig{t: t, tokenStatus: 200, tokenBody: `{"access_token":"tok-call","expires":"2027-01-01T00:00:00Z","account":"account","scopes":[]}`, headStatus: 404, headSize: 4, putStatus: 201, getStatus: 200, mediaStatus: 200, showStatus: 200, mime: "application/octet-stream", size: 4, payload: "data"}
	now := func() time.Time { return instant }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "gdrive.db"), Migrations: gdrive.Migrations(), Now: now, Service: "test", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	r.db = d
	t.Cleanup(func() { _ = d.Close() })
	r.st = store.New(d, store.Config{Now: now, Rand: &sequence{}})
	r.sink = &telemetry.Capture{}
	r.bus = &events.Capture{}
	r.w = telemetry.New(telemetry.Config{Service: "test", Sink: r.sink, Stderr: io.Discard, Now: now, Rand: &sequence{}, Sleep: func(context.Context, time.Duration) {}})
	r.em = events.New(events.Config{Service: "test", Sink: r.bus, Stderr: io.Discard, Now: now, Rand: &sequence{}, Sleep: func(context.Context, time.Duration) {}, Emits: trail.Emits()})
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r.e.Drain(ctx)
		r.em.Shutdown(context.Background())
		r.w.Shutdown(context.Background(), "test")
	})
	conn := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.record(q, nil)
		r.mu.Lock()
		r.tokenCount++
		n := r.tokenCount
		r.mu.Unlock()
		status, body := r.tokenStatus, r.tokenBody
		if n > 1 {
			waitHold(q.Context(), r.holdToken, r.tokenEntered)
			if r.workTokenStatus != 0 {
				status, body = r.workTokenStatus, r.workTokenBody
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	art := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.Method == "PUT" {
			waitHold(q.Context(), r.holdPut, r.putEntered)
			body, _ := io.ReadAll(q.Body)
			r.record(q, body)
			w.WriteHeader(r.putStatus)
			if r.putStatus == 201 {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "artifact", "path": "output", "bytes": len(body), "content_type": "application/octet-stream", "sha256": "sha", "owner": "alice", "created": "2026-10-11T12:00:00Z", "modified": "2026-10-11T12:00:00Z", "url": "https://art.test/files/output"})
			}
			return
		}
		r.record(q, nil)
		status := r.headStatus
		if q.Method == "GET" {
			status = r.getStatus
		}
		if status == 200 {
			size, ct := r.headSize, "application/octet-stream"
			if q.Method == "GET" {
				if r.getSize != nil {
					size = *r.getSize
				}
				if r.getType != "" {
					ct = r.getType
				}
			}
			w.Header().Set("Content-Length", fmt.Sprint(size))
			w.Header().Set("Content-Type", ct)
			w.Header().Set("ETag", `"sha"`)
			w.Header().Set("Last-Modified", instant.UTC().Format(http.TimeFormat))
		}
		w.WriteHeader(status)
		if q.Method == "GET" && status == 200 {
			_, _ = io.WriteString(w, r.payload)
		}
	}))
	r.driveServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		body, _ := io.ReadAll(q.Body)
		r.record(q, body)
		if q.URL.Path == "/drive/v3/files/root" {
			_, _ = io.WriteString(w, `{"id":"root-folder"}`)
			return
		}
		if q.URL.Query().Get("alt") == "media" || strings.HasSuffix(q.URL.Path, "/export") {
			w.WriteHeader(r.mediaStatus)
			if r.mediaStatus != 200 {
				_, _ = io.WriteString(w, r.mediaFailureBody)
			}
			if r.mediaStatus == 200 {
				waitHold(q.Context(), r.holdMedia, r.mediaEntered)
				_, _ = io.WriteString(w, r.payload)
			}
			return
		}
		if strings.HasPrefix(q.URL.Path, "/upload/") {
			if r.uploadStatus != 0 {
				w.WriteHeader(r.uploadStatus)
				return
			}
			w.Header().Set("Location", r.driveServer.URL+"/session?opaque=private")
			w.WriteHeader(200)
			return
		}
		if q.Method == "PUT" {
			if r.chunkStatus != 0 {
				w.WriteHeader(r.chunkStatus)
				return
			}
			writeFile(w, "created", "uploaded", r.mime, r.size, "", "")
			return
		}
		w.WriteHeader(r.showStatus)
		if r.showStatus == 200 {
			id := strings.TrimPrefix(q.URL.Path, "/drive/v3/files/")
			mime := r.mime
			if id == r.target && r.targetMime != "" {
				mime = r.targetMime
			}
			writeFile(w, id, "source-name", mime, r.size, r.target, r.targetMime)
		}
	}))
	t.Cleanup(r.driveServer.Close)
	r.cfg = transfers.Config{Store: r.st, Drive: drive.NewClient(drive.Config{HTTP: r.driveServer.Client(), BaseURL: r.driveServer.URL, Telemetry: r.w}), Artifacts: artifacts.NewClient(artifacts.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "artifacts", Socket: art, URL: "https://art.test"}}), Connections: connections.NewClient(connections.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "connections", Socket: conn}}), Telemetry: r.w, Events: r.em, ConnectURL: "https://broker.test/google/connect", MaxBytes: 100, MaxActive: 1, MaxQueued: 4, KeepDays: 30, KeepCount: 100, Now: now}
	r.e = transfers.New(r.cfg)
	return r
}
func writeFile(w http.ResponseWriter, id, name, mime string, size int64, target, targetMime string) {
	obj := map[string]any{"id": id, "name": name, "mimeType": mime, "size": fmt.Sprint(size), "modifiedTime": "2026-10-11T12:00:00Z", "webViewLink": "https://drive.test/file"}
	if target != "" && mime == "application/vnd.google-apps.shortcut" {
		obj["shortcutDetails"] = map[string]string{"targetId": target, "targetMimeType": targetMime}
	}
	_ = json.NewEncoder(w).Encode(obj)
}
func (r *rig) finish(id string) store.Transfer {
	r.t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		tr, err := r.st.Get(context.Background(), caller.UserID, id)
		if err != nil {
			r.t.Fatal(err)
		}
		if tr.Status == store.StatusDone || tr.Status == store.StatusFailed {
			return tr
		}
		select {
		case <-deadline.C:
			r.t.Fatal("transfer did not finish")
		default:
			runtime.Gosched()
		}
	}
}
func (r *rig) flush() {
	r.t.Helper()
	if err := r.w.Flush(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	if err := r.em.Flush(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}
func (r *rig) refusal(tr store.Transfer, err error, word, url string) {
	r.t.Helper()
	var refusal *transfers.Refusal
	if tr != (store.Transfer{}) || !errors.As(err, &refusal) || refusal.Word != word || refusal.URL != url || refusal.Error() != word {
		r.t.Fatal(tr, err, word, url)
	}
	list, e := r.st.List(context.Background(), caller.UserID)
	if e != nil || len(list) != 0 {
		r.t.Fatal(list, e)
	}
	r.flush()
	for _, q := range r.snapshot() {
		if q.Method == "PUT" || strings.HasPrefix(q.Path, "/upload/") {
			r.t.Fatal(q)
		}
	}
	for _, ev := range r.sink.Events() {
		if strings.HasPrefix(ev.Name, "transfer.") {
			r.t.Fatal(ev)
		}
	}
	if len(r.bus.Events()) != 0 {
		r.t.Fatal(r.bus.Events())
	}
}

// R-K6DC-1ILQ R-YN5T-RQRQ R-6PDX-HD45 R-LD1T-FHK1 R-YS1F-ATQI R-72ST-OU9S R-KDOQ-C51W
func TestToken(t *testing.T) {
	words := []string{transfers.RefusalNotConnected, transfers.RefusalReconnect, transfers.RefusalNotAFile, transfers.RefusalNotExportable, transfers.RefusalFormat, transfers.RefusalExpires, transfers.RefusalTooLarge, transfers.RefusalNotFound, transfers.RefusalProvider, transfers.RefusalPath, transfers.RefusalUnavailable, transfers.RefusalStopping, transfers.RefusalQueueFull}
	want := []string{"not_connected", "reconnect", "not_a_file", "not_exportable", "format", "expires", "too_large", "not_found", "provider", "path", "unavailable", "stopping", "queue_full"}
	if !reflect.DeepEqual(words, want) || transfers.Provider != "google" || transfers.ProgressStep != 1<<20 {
		t.Fatal(words)
	}
	for _, tc := range []struct {
		status          int
		body, word, url string
	}{{200, `{"access_token":"token","expires":"2027-01-01T00:00:00Z","account":"account","scopes":[]}`, "", ""}, {404, `{"error":"not_connected"}`, "not_connected", "https://broker.test/google/connect"}, {409, `{"error":"reconnect","url":"https://again.test"}`, "reconnect", "https://again.test"}, {404, `{"error":"unknown_provider"}`, "provider", ""}, {502, `{"error":"provider"}`, "provider", ""}, {503, `{"error":"unavailable"}`, "unavailable", ""}, {500, `bad`, "unavailable", ""}, {200, `bad`, "unavailable", ""}} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			r := newRig(t)
			r.tokenStatus, r.tokenBody = tc.status, tc.body
			tok, err := r.e.Token(identity.NewContext(context.Background(), identity.Caller{UserID: "wrong"}), caller)
			if tc.word == "" {
				if err != nil || tok != "token" {
					t.Fatal(tok, err)
				}
			} else {
				if tok != "" {
					t.Fatal(tok)
				}
				r.refusal(store.Transfer{}, err, tc.word, tc.url)
			}
			reqs := r.snapshot()
			if len(reqs) != 1 || reqs[0].Path != "/token/google" || reqs[0].User != caller.UserID || reqs[0].Email != caller.Email || reqs[0].ID != caller.RequestID {
				t.Fatal(reqs)
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Connections = nil
		r.e = transfers.New(r.cfg)
		tok, err := r.e.Token(context.Background(), caller)
		if tok != "" {
			t.Fatal(tok)
		}
		r.refusal(store.Transfer{}, err, "unavailable", "")
		if len(r.snapshot()) != 0 {
			t.Fatal(r.snapshot())
		}
	})
	t.Run("socket unavailable", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Connections = connections.NewClient(connections.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "connections", Socket: filepath.Join(t.TempDir(), "missing")}})
		r.e = transfers.New(r.cfg)
		_, err := r.e.Token(context.Background(), caller)
		r.refusal(store.Transfer{}, err, "unavailable", "")
	})
}

// R-YPLM-JA94 R-YT9B-OLH7 R-YUH8-2D7W R-LE9P-T9AQ R-6RTQ-8WLJ R-6T1M-MOC8 R-6U9J-0G2X R-Z0KP-Z7XD R-Z1SM-CZO2 R-6VHF-E7TM R-KEWM-PWSL R-77OF-7X8K
func TestPullRefusalsAndOrder(t *testing.T) {
	for _, tc := range []struct {
		name, word string
		setup      func(*rig, *transfers.PullRequest)
	}{
		{"token first", "not_connected", func(r *rig, q *transfers.PullRequest) {
			r.tokenStatus = 404
			r.tokenBody = `{"error":"not_connected"}`
			s := "bad"
			q.Expires = &s
			r.headStatus = 400
		}},
		{"empty expiry", "expires", func(_ *rig, q *transfers.PullRequest) { s := ""; q.Expires = &s }},
		{"invalid expiry", "expires", func(_ *rig, q *transfers.PullRequest) { s := "garbage"; q.Expires = &s }},
		{"same second expiry", "expires", func(_ *rig, q *transfers.PullRequest) { s := "2026-10-11T12:00:00Z"; q.Expires = &s }},
		{"bad path before drive", "path", func(r *rig, _ *transfers.PullRequest) { r.headStatus = 400; r.showStatus = 404 }},
		{"artifact unavailable", "unavailable", func(r *rig, _ *transfers.PullRequest) { r.headStatus = 503 }},
		{"artifact forbidden", "unavailable", func(r *rig, _ *transfers.PullRequest) { r.headStatus = 403 }},
		{"missing artifacts", "unavailable", func(r *rig, _ *transfers.PullRequest) { r.cfg.Artifacts = nil; r.e = transfers.New(r.cfg) }},
		{"missing file", "not_found", func(r *rig, _ *transfers.PullRequest) { r.showStatus = 404 }},
		{"drive refuses", "provider", func(r *rig, _ *transfers.PullRequest) { r.showStatus = 403 }},
		{"drive fails", "provider", func(r *rig, _ *transfers.PullRequest) { r.showStatus = 500 }},
		{"folder", "not_a_file", func(r *rig, _ *transfers.PullRequest) { r.mime = "application/vnd.google-apps.folder" }},
		{"shortcut no target", "not_a_file", func(r *rig, _ *transfers.PullRequest) { r.mime = "application/vnd.google-apps.shortcut" }},
		{"shortcut to folder", "not_a_file", func(r *rig, _ *transfers.PullRequest) {
			r.mime = "application/vnd.google-apps.shortcut"
			r.target = "target"
			r.targetMime = "application/vnd.google-apps.folder"
		}},
		{"shortcut to shortcut", "not_a_file", func(r *rig, _ *transfers.PullRequest) {
			r.mime = "application/vnd.google-apps.shortcut"
			r.target = "target"
			r.targetMime = "application/vnd.google-apps.shortcut"
		}},
		{"other", "not_exportable", func(r *rig, _ *transfers.PullRequest) { r.mime = "application/vnd.google-apps.form" }},
		{"bytes format before size", "format", func(r *rig, q *transfers.PullRequest) { q.Format = "application/pdf"; r.size = 101 }},
		{"native format", "format", func(r *rig, q *transfers.PullRequest) {
			r.mime = "application/vnd.google-apps.document"
			q.Format = "image/png"
		}},
		{"oversize", "too_large", func(r *rig, _ *transfers.PullRequest) { r.size = 101 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			q := transfers.PullRequest{File: "file", Path: "output"}
			tc.setup(r, &q)
			tr, err := r.e.Pull(context.Background(), caller, q)
			url := ""
			if tc.word == "not_connected" {
				url = r.cfg.ConnectURL
			}
			r.refusal(tr, err, tc.word, url)
			reqs := r.snapshot()
			if tc.word == "expires" || tc.word == "not_connected" {
				if len(reqs) != 1 {
					t.Fatal(reqs)
				}
			}
			if tc.word == "path" || tc.word == "unavailable" {
				for _, req := range reqs {
					if strings.HasPrefix(req.Path, "/drive/") {
						t.Fatal(req)
					}
				}
			}
		})
	}
}

// R-LFHM-711F R-758M-GDR6 R-Z940-NM48
func TestPushRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, word string
		setup      func(*rig, *transfers.PushRequest)
	}{
		{"bad path", "path", func(r *rig, _ *transfers.PushRequest) { r.headStatus = 400 }},
		{"missing", "not_found", func(r *rig, _ *transfers.PushRequest) { r.headStatus = 404 }},
		{"unavailable", "unavailable", func(r *rig, _ *transfers.PushRequest) { r.headStatus = 503 }},
		{"forbidden head", "unavailable", func(r *rig, _ *transfers.PushRequest) { r.headStatus = 403 }},
		{"unexpected head", "unavailable", func(r *rig, _ *transfers.PushRequest) { r.headStatus = 500 }},
		{"large", "too_large", func(r *rig, _ *transfers.PushRequest) { r.headSize = 101 }},
		{"target absent", "not_found", func(r *rig, q *transfers.PushRequest) { q.File = "replace"; r.showStatus = 404 }},
		{"target refuses", "provider", func(r *rig, q *transfers.PushRequest) { q.File = "replace"; r.showStatus = 403 }},
		{"target fails", "provider", func(r *rig, q *transfers.PushRequest) { q.File = "replace"; r.showStatus = 500 }},
		{"target folder", "not_a_file", func(r *rig, q *transfers.PushRequest) {
			q.File = "replace"
			r.mime = "application/vnd.google-apps.folder"
		}},
		{"parent file", "not_a_file", func(_ *rig, q *transfers.PushRequest) { q.Folder = "parent" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.headStatus = 200
			q := transfers.PushRequest{Path: "input"}
			tc.setup(r, &q)
			tr, err := r.e.Push(context.Background(), caller, q)
			r.refusal(tr, err, tc.word, "")
		})
	}
}

// R-ZBJT-F5LM
func TestDatabaseAddFailure(t *testing.T) {
	for _, push := range []bool{false, true} {
		t.Run(fmt.Sprint(push), func(t *testing.T) {
			r := newRig(t)
			r.headStatus = 200
			r.db.SetFailing(true)
			var tr store.Transfer
			var err error
			if push {
				tr, err = r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "input"})
			} else {
				tr, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "file", Path: "output"})
			}
			var refusal *transfers.Refusal
			if tr != (store.Transfer{}) || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &refusal) {
				t.Fatal(tr, err)
			}
			r.db.SetFailing(false)
			r.refusalEffectsOnly()
		})
	}
}
func (r *rig) refusalEffectsOnly() {
	r.flush()
	ts, err := r.st.List(context.Background(), caller.UserID)
	if err != nil || len(ts) != 0 || len(r.bus.Events()) != 0 {
		r.t.Fatal(ts, err, r.bus.Events())
	}
	for _, q := range r.snapshot() {
		if q.Method == "PUT" || strings.HasPrefix(q.Path, "/upload/") {
			r.t.Fatal(q)
		}
	}
}

// R-Z6O7-W2MU R-ZABX-1DUX R-76GI-U5HV R-ZCRP-SXCB R-K55F-NQV1 R-ZF7I-KGTP R-6WPB-RZKB R-5HT7-0RET R-ZOYP-MMR9
func TestSuccessfulTransfers(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		push                                bool
		mime, format, file, folder, newName string
		zeroExpiry                          bool
	}{{name: "pull bytes"}, {name: "pull zero expiry", zeroExpiry: true}, {name: "pull document", mime: "application/vnd.google-apps.document"}, {name: "pull PDF", mime: "application/vnd.google-apps.document", format: "application/pdf"}, {name: "pull shortcut", mime: "application/vnd.google-apps.shortcut"}, {name: "create root", push: true}, {name: "create named", push: true, newName: "new-name"}, {name: "create folder", push: true, folder: "folder", mime: "application/vnd.google-apps.folder"}, {name: "replace keeps name", push: true, file: "replace", folder: "ignored"}, {name: "replace renames", push: true, file: "replace", newName: "new-name"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			if tc.mime != "" {
				r.mime = tc.mime
			}
			r.headStatus = 200
			if tc.push {
				r.payload = "updated"
				size := int64(len(r.payload))
				r.getSize = &size
				r.getType = "example/type"
			}
			r.cfg.MaxBytes = 4
			r.e = transfers.New(r.cfg)
			r.workTokenStatus = 200
			r.workTokenBody = `{"access_token":"tok-work","expires":"2027-01-01T00:00:00Z","account":"account","scopes":[]}`
			r.holdToken = make(chan struct{})
			r.tokenEntered = make(chan struct{}, 1)
			if tc.mime == "application/vnd.google-apps.shortcut" {
				r.target = "target"
				r.targetMime = "application/octet-stream"
			}
			ctx, cancel := context.WithCancel(events.NewContext(context.Background(), events.Cause{ID: "evt_0123456789abcdef", Depth: 2}))
			var tr store.Transfer
			var err error
			expiry := "2026-10-11T12:00:01Z"
			if tc.push {
				tr, err = r.e.Push(ctx, caller, transfers.PushRequest{Path: "dir/input", File: tc.file, Folder: tc.folder, Name: tc.newName})
			} else {
				tr, err = r.e.Pull(ctx, caller, transfers.PullRequest{File: "source", Path: "output", Format: tc.format, Expires: func() *string {
					if tc.zeroExpiry {
						return nil
					}
					return &expiry
				}()})
			}
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			if tr.Status != store.StatusRunning || tr.UserID != caller.UserID || tr.UserEmail != caller.Email || tr.RequestID != caller.RequestID || tr.Bytes != 0 || !tr.Finished.IsZero() {
				t.Fatal(tr)
			}
			if tc.push {
				wantName := tc.newName
				if wantName == "" {
					wantName = "input"
					if tc.file != "" {
						wantName = "source-name"
					}
				}
				if tr.Direction != store.DirectionPush || tr.File != tc.file || tr.Name != wantName || tr.Kind != "file" || tr.ContentType != "application/octet-stream" || tr.Format != "" || tr.Path != "dir/input" {
					t.Fatal(tr)
				}
			} else {
				wantFile := "source"
				if r.target != "" {
					wantFile = "target"
				}
				wantFormat := tc.format
				wantCT := "application/octet-stream"
				if tc.mime == "application/vnd.google-apps.document" {
					if wantFormat == "" {
						wantFormat = drive.DefaultFormat(drive.KindDocument)
					}
					wantCT = wantFormat
				}
				if tr.Direction != store.DirectionPull || tr.File != wantFile || tr.Name != "source-name" || tr.Path != "output" || tr.ContentType != wantCT || tr.Format != wantFormat || tr.Kind != string(drive.KindOf(func() string {
					if r.target != "" {
						return r.targetMime
					}
					return r.mime
				}())) {
					t.Fatal(tr)
				}
			}
			select {
			case <-r.tokenEntered:
			case <-time.After(time.Second):
				t.Fatal("work token")
			}
			close(r.holdToken)
			done := r.finish(tr.ID)
			r.e.Drain(context.Background())
			if done.Status != store.StatusDone || done.Reason != "" || done.Bytes != int64(len(r.payload)) || done.URL != "https://art.test/files/output" && !tc.push || tc.push && done.URL != "https://art.test/files/dir/input" || done.Link != "https://drive.test/file" || done.Finished.IsZero() {
				t.Fatal(done)
			}
			if tc.push && (done.File != "created" || done.Name != "uploaded" || done.ArtifactID != "") {
				t.Fatal(done)
			}
			if !tc.push && done.ArtifactID != "artifact" {
				t.Fatal(done)
			}
			r.flush()
			eventsSeen := 0
			for _, ev := range r.sink.Events() {
				if ev.Name == trail.Started || ev.Name == trail.Finished {
					eventsSeen++
					if ev.RequestID != caller.RequestID || ev.User != caller.UserID {
						t.Fatal(ev)
					}
				}
				if ev.Name == drive.Called && (ev.RequestID != caller.RequestID || ev.User != caller.UserID) {
					t.Fatal(ev)
				}
			}
			if eventsSeen != 2 || len(r.bus.Events()) != 2 {
				t.Fatal(eventsSeen, r.bus.Events())
			}
			for i, ev := range r.bus.Events() {
				want := trail.Started
				if i == 1 {
					want = trail.Finished
				}
				eventTransfer := tr
				if i == 1 {
					eventTransfer = done
				}
				attrs := events.Attrs{"transfer": eventTransfer.ID, "direction": eventTransfer.Direction, "file": eventTransfer.File, "path": eventTransfer.Path}
				if i == 1 {
					attrs["status"] = done.Status
					attrs["reason"] = done.Reason
					attrs["bytes"] = done.Bytes
				}
				if ev.Name != want || ev.Cause != "evt_0123456789abcdef" || ev.Depth != 3 || !reflect.DeepEqual(ev.Attrs, attrs) {
					t.Fatal(ev, attrs)
				}
				matched := 0
				for _, tel := range r.sink.Events() {
					if tel.Name == want && tel.Attrs["transfer"] == tr.ID {
						matched++
						if !reflect.DeepEqual(tel.Attrs, telemetry.Attrs(attrs)) {
							t.Fatal(tel, attrs)
						}
					}
				}
				if matched != 1 {
					t.Fatal("telemetry pair", matched)
				}
			}
			reqs := r.snapshot()
			tokenRequests, headRequests, putRequests := 0, 0, 0
			getRequests, mediaRequests, exportRequests := 0, 0, 0
			freshRead := false
			for _, q := range reqs {
				if q.Method == "GET" && strings.HasPrefix(q.Path, "/files/") {
					getRequests++
				}
				if strings.Contains(q.Query, "alt=media") {
					mediaRequests++
				}
				if strings.HasSuffix(q.Path, "/export") {
					exportRequests++
				}
				if q.Path == "/token/google" {
					tokenRequests++
				}
				if q.Method == "HEAD" {
					headRequests++
				}
				if q.Path == "/drive/v3/files" {
					t.Fatal("push searched", q)
				}
				if q.Path == "/token/google" || strings.HasPrefix(q.Path, "/files/") {
					if q.User != caller.UserID || q.Email != caller.Email || q.ID != caller.RequestID {
						t.Fatal(q)
					}
				}
				if q.Method == "PUT" && strings.HasPrefix(q.Path, "/files/") {
					putRequests++
					if string(q.Body) != r.payload || q.Header.Get("X-Artifact-Expires") != func() string {
						if tc.zeroExpiry {
							return ""
						}
						return expiry
					}() || q.Header.Get("Content-Type") != tr.ContentType {
						t.Fatal(q)
					}
				}
				if strings.HasPrefix(q.Path, "/drive/v3/files/"+tr.File) && q.Auth == "Bearer tok-work" {
					freshRead = true
				}
				if q.Path == "/drive/v3/files/"+tr.File && q.Header.Get("Authorization") == "Bearer tok-work" {
					if tc.push {
						t.Fatal("unexpected show in work")
					}
				}
				if q.Path == "/session" && string(q.Body) != r.payload {
					t.Fatal(q)
				}
				if strings.HasPrefix(q.Path, "/upload/") {
					if q.Header.Get("X-Upload-Content-Length") != fmt.Sprint(len(r.payload)) || q.Header.Get("X-Upload-Content-Type") != r.getType {
						t.Fatal(q)
					}
					var fields map[string]any
					if err := json.Unmarshal(q.Body, &fields); err != nil {
						t.Fatal(err)
					}
					if tc.file != "" {
						if q.Method != "PATCH" || len(fields) > 1 || tc.newName == "" && len(fields) != 0 || tc.newName != "" && fields["name"] != tc.newName {
							t.Fatal(q)
						}
					} else {
						parent := tc.folder
						if parent == "" {
							parent = "root-folder"
						}
						if q.Method != "POST" || fields["name"] != tr.Name || !reflect.DeepEqual(fields["parents"], []any{parent}) {
							t.Fatal(q)
						}
					}
				}
				if strings.HasPrefix(q.Path, "/upload/") && q.Auth != "Bearer tok-work" {
					t.Fatal(q)
				}
				if q.Path == "/session" && q.Auth != "" {
					t.Fatal(q)
				}
				if tc.push && tc.file != "" && q.Path == "/drive/v3/files/ignored" {
					t.Fatal(q)
				}
			}
			if tokenRequests != 2 || headRequests != 1 || tc.push && getRequests != 1 || (!tc.push && (putRequests != 1 || !freshRead || tr.Format == "" && (mediaRequests != 1 || exportRequests != 0) || tr.Format != "" && (exportRequests != 1 || mediaRequests != 0))) {
				t.Fatal(reqs)
			}
		})
	}
}

// R-6XX8-5RB0 R-6Z54-JJ1P R-K8T4-T234 R-KA11-6TTT
func TestWorkFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		push   bool
		reason string
		setup  func(*rig)
	}{
		{"disconnected", false, "reconnect", func(r *rig) { r.workTokenStatus = 404; r.workTokenBody = `{"error":"not_connected"}` }},
		{"reconnect", false, "reconnect", func(r *rig) {
			r.workTokenStatus = 409
			r.workTokenBody = `{"error":"reconnect","url":"https://broker.test"}`
		}},
		{"provider token", false, "provider", func(r *rig) { r.workTokenStatus = 502; r.workTokenBody = `{"error":"provider"}` }},
		{"unavailable token", true, "provider", func(r *rig) { r.workTokenStatus = 503; r.workTokenBody = `{"error":"unavailable"}` }},
		{"media absent", false, "not_found", func(r *rig) { r.mediaStatus = 404 }},
		{"media refuses", false, "provider", func(r *rig) { r.mediaStatus = 403 }},
		{"media fails", false, "provider", func(r *rig) { r.mediaStatus = 500 }},
		{"put forbidden", false, "forbidden", func(r *rig) { r.putStatus = 403 }},
		{"put too large", false, "too_large", func(r *rig) { r.putStatus = 413 }},
		{"put unavailable", false, "artifacts", func(r *rig) { r.putStatus = 503 }},
		{"get absent", true, "not_found", func(r *rig) { r.getStatus = 404 }},
		{"get fails", true, "artifacts", func(r *rig) { r.getStatus = 503 }},
		{"export cap refused", false, "too_large", func(r *rig) {
			r.mime = "application/vnd.google-apps.document"
			r.mediaStatus = 403
			r.mediaFailureBody = `{"error":{"errors":[{"reason":"exportSizeLimitExceeded"}]}}`
		}},
		{"upload absent", true, "not_found", func(r *rig) { r.uploadStatus = 404 }},
		{"upload forbidden", true, "forbidden", func(r *rig) { r.uploadStatus = 403 }},
		{"upload failure", true, "provider", func(r *rig) { r.uploadStatus = 503 }},
		{"chunk forbidden", true, "forbidden", func(r *rig) { r.chunkStatus = 403 }},
		{"chunk failure", true, "provider", func(r *rig) { r.chunkStatus = 500 }},
		{"get ends early", true, "artifacts", func(r *rig) { r.headSize = 100 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.headStatus = 200
			tc.setup(r)
			var tr store.Transfer
			var err error
			if tc.push {
				tr, err = r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "input"})
			} else {
				tr, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "source", Path: "output"})
			}
			if err != nil {
				t.Fatal(err)
			}
			done := r.finish(tr.ID)
			r.e.Drain(context.Background())
			wantBytes := int64(0)
			if strings.HasPrefix(tc.name, "put ") || strings.HasPrefix(tc.name, "chunk ") || tc.name == "get ends early" {
				wantBytes = 4
			}
			if done.Status != store.StatusFailed || done.Reason != tc.reason || done.Bytes != wantBytes {
				t.Fatal(done)
			}
			reqs := r.snapshot()
			if r.workTokenStatus != 0 || r.mediaStatus != 200 || tc.push && r.getStatus != 200 {
				for _, q := range reqs {
					if q.Method == "PUT" || strings.HasPrefix(q.Path, "/upload/") {
						t.Fatal(q)
					}
				}
			}
			if r.workTokenStatus != 0 {
				for _, q := range reqs {
					if q.Method == "GET" && strings.HasPrefix(q.Path, "/files/") {
						t.Fatal(q)
					}
				}
			}
		})
	}
}

// R-5E5H-VG6Q R-Z48F-4J5G R-ZMIW-V39V R-ZNQT-8V0K R-ZTUB-5PQ1 R-K3XJ-9Z4C R-K7L8-FACF R-ZXI0-B0Y4
func TestQueueDrainAndFIFO(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			r := newRig(t)
			r.cfg.MaxQueued = 2
			r.e = transfers.New(r.cfg)
			r.holdMedia = make(chan struct{})
			r.mediaEntered = make(chan struct{}, 2)
			a, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "a", Path: "a"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-r.mediaEntered:
			case <-time.After(time.Second):
				t.Fatal("media did not start")
			}
			b, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "b", Path: "b"})
			if err != nil {
				t.Fatal(err)
			}
			c, err := r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "c"})
			if err == nil {
				t.Fatal("push must fail absent artifact", c)
			}
			c, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "c", Path: "c"})
			if err != nil {
				t.Fatal(err)
			}
			if a.Status != "running" || b.Status != "queued" || c.Status != "queued" {
				t.Fatal(a, b, c)
			}
			_, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "d", Path: "d"})
			var refusal *transfers.Refusal
			if !errors.As(err, &refusal) || refusal.Word != "queue_full" {
				t.Fatal(err)
			}
			if !cancelled {
				close(r.holdMedia)
				r.finish(a.ID)
				r.finish(b.ID)
				r.finish(c.ID)
				r.e.Drain(context.Background())
				r.flush()
				evs := r.bus.Events()
				if len(evs) != 6 {
					t.Fatal(evs)
				}
				for i, id := range []string{a.ID, b.ID, c.ID} {
					if evs[i*2].Name != trail.Started || evs[i*2].Attrs["transfer"] != id || evs[i*2+1].Name != trail.Finished || evs[i*2+1].Attrs["transfer"] != id {
						t.Fatal(evs)
					}
				}
			} else {
				ctx, cancel := context.WithCancel(context.Background())
				drained := make(chan struct{})
				go func() { r.e.Drain(ctx); close(drained) }()
				deadline := time.NewTimer(time.Second)
				defer deadline.Stop()
				for {
					bt, _ := r.st.Get(context.Background(), caller.UserID, b.ID)
					ct, _ := r.st.Get(context.Background(), caller.UserID, c.ID)
					if bt.Status == "failed" && ct.Status == "failed" {
						if bt.Reason != "interrupted" || ct.Reason != "interrupted" || bt.Bytes != 0 || ct.Bytes != 0 {
							t.Fatal(bt, ct)
						}
						break
					}
					select {
					case <-deadline.C:
						t.Fatal("queued not ended")
					default:
						runtime.Gosched()
					}
				}
				select {
				case <-drained:
					t.Fatal("drain returned with running transfer")
				default:
				}
				cancel()
				select {
				case <-drained:
				case <-time.After(time.Second):
					t.Fatal("drain did not cancel")
				}
				at := r.finish(a.ID)
				if at.Status != "failed" || at.Reason != "interrupted" {
					t.Fatal(at)
				}
				r.flush()
				evs := r.bus.Events()
				if len(evs) != 4 {
					t.Fatal(evs)
				}
				for _, ev := range evs {
					if ev.Name == trail.Started && ev.Attrs["transfer"] != a.ID {
						t.Fatal(ev)
					}
				}
			}
			_, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "e", Path: "e"})
			if !errors.As(err, &refusal) || refusal.Word != "stopping" {
				t.Fatal(err)
			}
		})
	}
}

// R-ZREI-E68N R-ZSME-RXZC R-KL04-MRI2
func TestRecoverAndPrune(t *testing.T) {
	r := newRig(t)
	a, err := r.st.Add(context.Background(), store.Transfer{Direction: "pull", Status: "running", Path: "output", UserID: caller.UserID, UserEmail: caller.Email, RequestID: caller.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.st.SetBytes(context.Background(), a.ID, 19)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.st.Add(context.Background(), store.Transfer{Direction: "push", Status: "queued", Path: "input", UserID: caller.UserID, RequestID: "bob-req"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.e.Recover(events.NewContext(context.Background(), events.Cause{ID: "evt_0123456789abcdef", Depth: 7})); err != nil {
		t.Fatal(err)
	}
	at, err := r.st.Get(context.Background(), caller.UserID, a.ID)
	if err != nil || at.Status != "failed" || at.Reason != "interrupted" || at.Bytes != 19 {
		t.Fatal(at, err)
	}
	bt, err := r.st.Get(context.Background(), caller.UserID, b.ID)
	if err != nil || bt.Status != "failed" || bt.Bytes != 0 {
		t.Fatal(bt, err)
	}
	r.flush()
	if len(r.bus.Events()) != 2 {
		t.Fatal(r.bus.Events())
	}
	for _, ev := range r.bus.Events() {
		if ev.Name != trail.Finished || ev.Cause != "" || ev.Depth != 0 {
			t.Fatal(ev)
		}
	}
	newest, err := r.st.Add(context.Background(), store.Transfer{Direction: "pull", Status: "running", Path: "newest", UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.st.Finish(context.Background(), newest.ID, store.Ending{Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := r.st.List(context.Background(), caller.UserID)
	eventsBefore := r.sink.Events()
	r.cfg.KeepDays = 32
	r.cfg.KeepCount = 1
	r.cfg.Now = func() time.Time { return instant.AddDate(0, 0, 31) }
	r.e = transfers.New(r.cfg)
	if err = r.e.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	ageRetained, _ := r.st.List(context.Background(), caller.UserID)
	if !reflect.DeepEqual(before, ageRetained) {
		t.Fatal("age protection", before, ageRetained)
	}
	r.cfg.KeepDays = 30
	r.cfg.KeepCount = 100
	r.cfg.Now = func() time.Time { return instant.AddDate(0, 0, 31) }
	r.e = transfers.New(r.cfg)
	if err = r.e.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	retained, _ := r.st.List(context.Background(), caller.UserID)
	if !reflect.DeepEqual(before, retained) {
		t.Fatal("count protection", before, retained)
	}
	unfinished, err := r.st.Add(context.Background(), store.Transfer{Direction: "pull", Status: "running", Path: "unfinished", UserID: "other"})
	if err != nil {
		t.Fatal(err)
	}
	r.cfg.KeepCount = 1
	r.cfg.Now = func() time.Time { return instant.AddDate(0, 0, 31) }
	r.e = transfers.New(r.cfg)
	if err = r.e.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = r.st.Get(context.Background(), caller.UserID, a.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = r.st.Get(context.Background(), caller.UserID, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	retained, _ = r.st.List(context.Background(), caller.UserID)
	wantNewest, _ := r.st.Get(context.Background(), caller.UserID, newest.ID)
	if len(retained) != 1 || retained[0] != wantNewest || wantNewest.Status != "done" {
		t.Fatal(retained)
	}
	stillActive, err := r.st.Get(context.Background(), "other", unfinished.ID)
	if err != nil || stillActive != unfinished {
		t.Fatal(stillActive, err)
	}
	r.flush()
	if len(r.snapshot()) != 0 || len(r.bus.Events()) != 2 || !reflect.DeepEqual(eventsBefore, r.sink.Events()) {
		t.Fatal(r.snapshot(), r.bus.Events())
	}
	r.db.SetFailing(true)
	if !errors.Is(r.e.Recover(context.Background()), store.ErrUnreachable) || !errors.Is(r.e.Prune(context.Background()), store.ErrUnreachable) {
		t.Fatal("database failure")
	}
	r.db.SetFailing(false)
}

// R-5FDE-97XF R-ZYPW-OSOT
func TestConcurrentBound(t *testing.T) {
	r := newRig(t)
	r.cfg.MaxQueued = 2
	arrived := make(chan struct{}, 20)
	headRelease := make(chan struct{})
	socket := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/files/next" {
			arrived <- struct{}{}
			select {
			case <-headRelease:
			case <-q.Context().Done():
				return
			}
		}
		w.WriteHeader(404)
	}))
	r.cfg.Artifacts = artifacts.NewClient(artifacts.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "artifacts", Socket: socket}})
	r.e = transfers.New(r.cfg)
	r.holdMedia = make(chan struct{})
	r.mediaEntered = make(chan struct{}, 1)
	first, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "first", Path: "first"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.mediaEntered:
	case <-time.After(time.Second):
		t.Fatal("work")
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			_, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "next", Path: "next"})
			outcomes <- err
		})
	}
	for range 20 {
		waitSignal(t, arrived, "twenty HEADs not concurrent")
	}
	close(headRelease)
	wg.Wait()
	close(outcomes)
	accepted, rejected := 0, 0
	for err := range outcomes {
		if err == nil {
			accepted++
		} else {
			var refusal *transfers.Refusal
			if !errors.As(err, &refusal) || refusal.Word != "queue_full" {
				t.Fatal(err)
			}
			rejected++
		}
	}
	if accepted != 2 || rejected != 18 {
		t.Fatal(accepted, rejected)
	}
	var race sync.WaitGroup
	actors := []func(){func() { _, _ = r.e.Token(context.Background(), caller) }, func() { _ = r.e.Prune(context.Background()) }, func() { _, _ = r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "absent"}) }, func() {
		_, _ = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "racing", Path: "racing"})
	}, func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); r.e.Drain(ctx) }}
	ready, start := make(chan struct{}, len(actors)), make(chan struct{})
	for _, actor := range actors {
		race.Go(func() { ready <- struct{}{}; <-start; actor() })
	}
	for range actors {
		waitSignal(t, ready, "actor not ready")
	}
	close(start)
	race.Wait()
	done := r.finish(first.ID)
	if done.Status != "failed" || done.Reason != "interrupted" {
		t.Fatal(done)
	}
}

// R-ZQ6M-0EHY R-KB8X-KLKI
func TestFailedFinishAndStartReleaseSlots(t *testing.T) {
	r := newRig(t)
	r.holdPut = make(chan struct{})
	r.putEntered = make(chan struct{}, 1)
	r.cfg.MaxQueued = 1
	r.e = transfers.New(r.cfg)
	a, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "a", Path: "a"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.putEntered:
	case <-time.After(time.Second):
		t.Fatal("put")
	}
	b, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "b", Path: "b"})
	if err != nil || b.Status != "queued" {
		t.Fatal(b, err)
	}
	r.db.SetFailing(true)
	close(r.holdPut)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		_, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "probe", Path: "probe"})
		if errors.Is(err, store.ErrUnreachable) {
			break
		}
		var refusal *transfers.Refusal
		if !errors.As(err, &refusal) || refusal.Word != "queue_full" {
			t.Fatal(err)
		}
		select {
		case <-deadline.C:
			t.Fatal("slot not released")
		default:
			runtime.Gosched()
		}
	}
	r.e.Drain(context.Background())
	r.db.SetFailing(false)
	at, _ := r.st.Get(context.Background(), caller.UserID, a.ID)
	bt, _ := r.st.Get(context.Background(), caller.UserID, b.ID)
	if at.Status != "running" || bt.Status != "queued" {
		t.Fatal(at, bt)
	}
	r.flush()
	evs := r.bus.Events()
	if len(evs) != 1 || evs[0].Name != trail.Started || evs[0].Attrs["transfer"] != a.ID {
		t.Fatal(evs)
	}
	if err = r.e.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.flush()
	if len(r.bus.Events()) != 3 {
		t.Fatal(r.bus.Events())
	}
}

// R-KIKB-V80O R-ZLB0-HBJ6 R-0CDO-7WRF
func TestPullStreamingProgress(t *testing.T) {
	for _, progressFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(progressFailure), func(t *testing.T) {
			r := newRig(t)
			prefix := strings.Repeat("x", 2*transfers.ProgressStep)
			ready := make(chan struct{})
			release := make(chan struct{})
			received := make(chan struct{})
			tailGate := make(chan struct{})
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				if q.URL.Path == "/drive/v3/files/root" {
					_, _ = io.WriteString(w, `{"id":"root"}`)
					return
				}
				if q.URL.Query().Get("alt") != "media" {
					writeFile(w, "source", "source-name", "application/octet-stream", 4, "", "")
					return
				}
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(ready)
				select {
				case <-release:
				case <-q.Context().Done():
					return
				}
				_, _ = io.WriteString(w, prefix)
				w.(http.Flusher).Flush()
				select {
				case <-tailGate:
				case <-q.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "tail")
			}))
			defer source.Close()
			socket := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				if q.Method == "HEAD" {
					w.WriteHeader(404)
					return
				}
				buf := make([]byte, len(prefix))
				_, err := io.ReadFull(q.Body, buf)
				if err != nil {
					return
				}
				if string(buf) != prefix {
					panic("wrong stream")
				}
				close(received)
				_, _ = io.Copy(io.Discard, q.Body)
				w.WriteHeader(201)
				_, _ = io.WriteString(w, `{"id":"artifact","path":"output","bytes":2097156,"content_type":"application/octet-stream","sha256":"sha","owner":"alice","created":"2026-10-11T12:00:00Z","modified":"2026-10-11T12:00:00Z","url":"https://art.test/output"}`)
			}))
			r.cfg.Drive = drive.NewClient(drive.Config{HTTP: source.Client(), BaseURL: source.URL, Telemetry: r.w})
			r.cfg.Artifacts = artifacts.NewClient(artifacts.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "artifacts", Socket: socket}})
			r.e = transfers.New(r.cfg)
			tr, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "source", Path: "output"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-ready:
			case <-time.After(time.Second):
				t.Fatal("source")
			}
			if progressFailure {
				r.db.SetFailing(true)
			}
			close(release)
			select {
			case <-received:
			case <-time.After(time.Second):
				t.Fatal("stream buffered")
			}
			if progressFailure {
				r.db.SetFailing(false)
			} else {
				current, err := r.st.Get(context.Background(), caller.UserID, tr.ID)
				if err != nil || current.Status != "running" || current.Bytes != int64(len(prefix)) {
					t.Fatal(current, err)
				}
			}
			close(tailGate)
			done := r.finish(tr.ID)
			if done.Status != "done" || done.Bytes != int64(len(prefix)+4) {
				t.Fatal(done)
			}
		})
	}
}

// R-KJS8-8ZRD
func TestPushStreamingPieces(t *testing.T) {
	r := newRig(t)
	prefix := strings.Repeat("y", drive.PushChunk)
	piece := make(chan struct{})
	release := make(chan struct{})
	var driveURL string
	dc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/drive/v3/files/root" {
			_, _ = io.WriteString(w, `{"id":"root"}`)
			return
		}
		if q.Method == "POST" {
			w.Header().Set("Location", driveURL+"/session")
			return
		}
		body, err := io.ReadAll(q.Body)
		if err != nil {
			return
		}
		if q.Header.Get("Content-Range") == fmt.Sprintf("bytes 0-%d/%d", drive.PushChunk-1, drive.PushChunk+4) {
			if string(body) != prefix {
				panic("piece mismatch")
			}
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", drive.PushChunk-1))
			w.WriteHeader(308)
			close(piece)
			return
		}
		if string(body) != "tail" {
			panic("tail mismatch")
		}
		writeFile(w, "created", "uploaded", "application/octet-stream", drive.PushChunk+4, "", "")
	}))
	driveURL = dc.URL
	defer dc.Close()
	socket := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(drive.PushChunk+4))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"sha"`)
		w.Header().Set("Last-Modified", instant.UTC().Format(http.TimeFormat))
		if q.Method == "HEAD" {
			return
		}
		_, _ = io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-q.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "tail")
	}))
	r.cfg.MaxBytes = drive.PushChunk + 4
	r.cfg.Drive = drive.NewClient(drive.Config{HTTP: dc.Client(), BaseURL: dc.URL, Telemetry: r.w})
	r.cfg.Artifacts = artifacts.NewClient(artifacts.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "artifacts", Socket: socket, URL: "https://art.test"}})
	r.e = transfers.New(r.cfg)
	tr, err := r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "input"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-piece:
	case <-time.After(3 * time.Second):
		t.Fatal("piece buffered")
	}
	close(release)
	done := r.finish(tr.ID)
	if done.Status != "done" || done.Bytes != drive.PushChunk+4 {
		t.Fatal(done)
	}
}

// R-KHCF-HG9Z
func TestEarlyDestinationFailureClosesSource(t *testing.T) {
	for _, push := range []bool{false, true} {
		t.Run(fmt.Sprint(push), func(t *testing.T) {
			r := newRig(t)
			closed := make(chan struct{})
			dc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				if q.URL.Path == "/drive/v3/files/root" {
					_, _ = io.WriteString(w, `{"id":"root"}`)
					return
				}
				if strings.HasPrefix(q.URL.Path, "/upload/") {
					w.WriteHeader(403)
					return
				}
				if q.URL.Query().Get("alt") != "media" {
					writeFile(w, "source", "source-name", "application/octet-stream", 4, "", "")
					return
				}
				w.WriteHeader(200)
				_, _ = io.WriteString(w, "first")
				w.(http.Flusher).Flush()
				<-q.Context().Done()
				close(closed)
			}))
			defer dc.Close()
			socket := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				if q.Method == "PUT" {
					w.Header().Set("Connection", "close")
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Length", "100")
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("ETag", `"sha"`)
				w.Header().Set("Last-Modified", instant.UTC().Format(http.TimeFormat))
				if q.Method == "HEAD" {
					return
				}
				_, _ = io.WriteString(w, "first")
				w.(http.Flusher).Flush()
				<-q.Context().Done()
				close(closed)
			}))
			r.cfg.Drive = drive.NewClient(drive.Config{HTTP: dc.Client(), BaseURL: dc.URL, Telemetry: r.w})
			r.cfg.Artifacts = artifacts.NewClient(artifacts.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "artifacts", Socket: socket}})
			r.e = transfers.New(r.cfg)
			var tr store.Transfer
			var err error
			if push {
				tr, err = r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "input"})
			} else {
				tr, err = r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "source", Path: "output"})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("source left open")
			}
			done := r.finish(tr.ID)
			if done.Status != "failed" || done.Reason != "forbidden" {
				t.Fatal(done)
			}
		})
	}
}
func TestBrokenDriveStreamClassification(t *testing.T) {
	for _, tc := range []struct {
		name, mime, reason string
		bytes              int
	}{{"media", "application/octet-stream", "provider", 12}, {"small export", "application/vnd.google-apps.document", "provider", 12}, {"capped export", "application/vnd.google-apps.document", "too_large", drive.ExportCap}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			dc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
				if q.URL.Path == "/drive/v3/files/root" {
					_, _ = io.WriteString(w, `{"id":"root"}`)
					return
				}
				if q.URL.Query().Get("alt") != "media" && !strings.HasSuffix(q.URL.Path, "/export") {
					writeFile(w, "source", "source-name", tc.mime, 4, "", "")
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(tc.bytes+100))
				_, _ = io.WriteString(w, strings.Repeat("z", tc.bytes))
				w.(http.Flusher).Flush()
			}))
			defer dc.Close()
			r.cfg.Drive = drive.NewClient(drive.Config{HTTP: dc.Client(), BaseURL: dc.URL, Telemetry: r.w})
			r.e = transfers.New(r.cfg)
			tr, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "source", Path: "output"})
			if err != nil {
				t.Fatal(err)
			}
			done := r.finish(tr.ID)
			if done.Status != "failed" || done.Reason != tc.reason || done.Bytes != int64(tc.bytes) {
				t.Fatal(done)
			}
		})
	}
}
func TestTokenFirstForPush(t *testing.T) {
	r := newRig(t)
	r.tokenStatus = 404
	r.tokenBody = `{"error":"not_connected"}`
	r.headStatus = 400
	tr, err := r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "bad"})
	r.refusal(tr, err, "not_connected", r.cfg.ConnectURL)
	if len(r.snapshot()) != 1 {
		t.Fatal(r.snapshot())
	}
}
func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(label)
	}
}
func (r *rig) waitStatus(user, id, status string) {
	r.t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		tr, err := r.st.Get(context.Background(), user, id)
		if err != nil {
			r.t.Fatal(err)
		}
		if tr.Status == status {
			return
		}
		select {
		case <-deadline.C:
			r.t.Fatal("status", status, tr)
		default:
			runtime.Gosched()
		}
	}
}
func TestSharedUsersFIFOWithIndividualGates(t *testing.T) {
	r := newRig(t)
	users := []identity.Caller{caller, {UserID: "bob", Email: "bob@test", RequestID: "req-bob"}, {UserID: "carol", Email: "carol@test", RequestID: "req-carol"}}
	gates := map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{}), "c": make(chan struct{})}
	entered := map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{}), "c": make(chan struct{})}
	dc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/drive/v3/files/root" {
			_, _ = io.WriteString(w, `{"id":"root"}`)
			return
		}
		id := strings.TrimPrefix(q.URL.Path, "/drive/v3/files/")
		if q.URL.Query().Get("alt") == "media" {
			close(entered[id])
			select {
			case <-gates[id]:
			case <-q.Context().Done():
				return
			}
			_, _ = io.WriteString(w, "data")
			return
		}
		writeFile(w, id, "name", "application/octet-stream", 4, "", "")
	}))
	t.Cleanup(dc.Close)
	r.headStatus = 200
	r.cfg.MaxQueued = 2
	r.cfg.Drive = drive.NewClient(drive.Config{HTTP: dc.Client(), BaseURL: dc.URL, Telemetry: r.w})
	r.e = transfers.New(r.cfg)
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); r.e.Drain(ctx) })
	records := make([]store.Transfer, 3)
	for i, id := range []string{"a", "b", "c"} {
		tr, err := r.e.Pull(context.Background(), users[i], transfers.PullRequest{File: id, Path: id})
		if err != nil {
			t.Fatal(err)
		}
		records[i] = tr
		if i == 0 {
			waitSignal(t, entered[id], "first did not run")
		}
	}
	for i := 1; i < 3; i++ {
		if records[i].Status != "queued" {
			t.Fatal(records)
		}
	}
	_, err := r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "overflow"})
	var refusal *transfers.Refusal
	if !errors.As(err, &refusal) || refusal.Word != "queue_full" {
		t.Fatal(err)
	}
	close(gates["a"])
	waitSignal(t, entered["b"], "next did not run")
	r.waitStatus(users[0].UserID, records[0].ID, "done")
	r.waitStatus(users[1].UserID, records[1].ID, "running")
	ct, err := r.st.Get(context.Background(), users[2].UserID, records[2].ID)
	if err != nil || ct.Status != "queued" {
		t.Fatal(ct, err)
	}
	select {
	case <-entered["c"]:
		t.Fatal("third started early")
	default:
	}
	r.flush()
	evs := r.bus.Events()
	if len(evs) != 3 || evs[0].Name != trail.Started || evs[0].User != users[0].UserID || evs[1].Name != trail.Finished || evs[1].Attrs["transfer"] != records[0].ID || evs[2].Name != trail.Started || evs[2].Attrs["transfer"] != records[1].ID || evs[2].User != users[1].UserID {
		t.Fatal(evs)
	}
	close(gates["b"])
	waitSignal(t, entered["c"], "third did not run")
	r.waitStatus(users[1].UserID, records[1].ID, "done")
	close(gates["c"])
	r.waitStatus(users[2].UserID, records[2].ID, "done")
	r.e.Drain(context.Background())
	_, err = r.e.Push(context.Background(), caller, transfers.PushRequest{Path: "stopping"})
	if !errors.As(err, &refusal) || refusal.Word != "stopping" {
		t.Fatal(err)
	}
}
func TestDrainEndsEveryExchangePhase(t *testing.T) {
	for _, phase := range []string{"connections", "drive", "artifacts"} {
		t.Run(phase, func(t *testing.T) {
			r := newRig(t)
			started := make(chan struct{})
			ended := make(chan struct{})
			prefix := strings.Repeat("p", 2*transfers.ProgressStep)
			if phase == "connections" {
				var calls int
				var mu sync.Mutex
				socket := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
					mu.Lock()
					calls++
					n := calls
					mu.Unlock()
					if n == 1 {
						_, _ = io.WriteString(w, r.tokenBody)
						return
					}
					close(started)
					<-q.Context().Done()
					close(ended)
				}))
				r.cfg.Connections = connections.NewClient(connections.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "connections", Socket: socket}})
			} else {
				dc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
					if q.URL.Path == "/drive/v3/files/root" {
						_, _ = io.WriteString(w, `{"id":"root"}`)
						return
					}
					if q.URL.Query().Get("alt") != "media" {
						writeFile(w, "source", "name", "application/octet-stream", 4, "", "")
						return
					}
					w.WriteHeader(200)
					if phase == "artifacts" {
						_, _ = io.WriteString(w, prefix)
					}
					w.(http.Flusher).Flush()
					if phase == "drive" {
						close(started)
					}
					<-q.Context().Done()
					if phase == "drive" {
						close(ended)
					}
				}))
				t.Cleanup(dc.Close)
				r.cfg.Drive = drive.NewClient(drive.Config{HTTP: dc.Client(), BaseURL: dc.URL, Telemetry: r.w})
				if phase == "artifacts" {
					socket := unixServer(t, http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
						if q.Method == "HEAD" {
							w.WriteHeader(404)
							return
						}
						data := make([]byte, len(prefix))
						if _, err := io.ReadFull(q.Body, data); err != nil {
							return
						}
						close(started)
						_, _ = io.Copy(io.Discard, q.Body)
						close(ended)
					}))
					r.cfg.Artifacts = artifacts.NewClient(artifacts.ClientConfig{Telemetry: r.w, Service: services.Entry{Name: "artifacts", Socket: socket}})
				}
			}
			r.e = transfers.New(r.cfg)
			t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); r.e.Drain(ctx) })
			tr, err := r.e.Pull(context.Background(), caller, transfers.PullRequest{File: "source", Path: "output"})
			if err != nil {
				t.Fatal(err)
			}
			waitSignal(t, started, "exchange not started")
			expected := int64(0)
			if phase == "artifacts" {
				expected = int64(len(prefix))
				deadline := time.NewTimer(time.Second)
				defer deadline.Stop()
				for {
					current, _ := r.st.Get(context.Background(), caller.UserID, tr.ID)
					if current.Bytes == expected {
						break
					}
					select {
					case <-deadline.C:
						t.Fatal("missing progress", current)
					default:
						runtime.Gosched()
					}
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			drained := make(chan struct{})
			go func() { r.e.Drain(ctx); close(drained) }()
			waitSignal(t, drained, "drain deadline")
			waitSignal(t, ended, "exchange left in flight")
			done := r.finish(tr.ID)
			if done.Status != "failed" || done.Reason != "interrupted" || done.Bytes != expected {
				t.Fatal(done)
			}
			r.flush()
			evs := r.bus.Events()
			if len(evs) != 2 || evs[1].Name != trail.Finished || evs[1].Attrs["bytes"] != expected || evs[1].Attrs["reason"] != "interrupted" {
				t.Fatal(evs)
			}
		})
	}
}
