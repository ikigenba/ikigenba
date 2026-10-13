package tools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
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
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

var handlerCaller = identity.Caller{UserID: "mail-owner", Email: "owner@example.test", RequestID: "request-fixture"}

const handlerConnect = "https://connections.example.test/google/connect"
const handlerToken = "synthetic-access-token"

func handlerClock() time.Time { return time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC) }
func handlerEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func handlerJSON(t *testing.T, b []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatal(err)
	}
}
func handlerSocket(t *testing.T, name string, h http.Handler) services.Entry {
	t.Helper()
	dir, err := os.MkdirTemp("", "scratch.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, name)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	return services.Entry{Name: name, Socket: path, URL: "https://" + name + ".example.test"}
}

type handlerRequest struct {
	Method, Path string
	Query        map[string][]string
	Header       http.Header
	Body         []byte
}
type handlerFixture struct {
	mu          sync.Mutex
	requests    []handlerRequest
	tokenStatus int
	tokenBody   string
	gmailStatus int
	headStatus  int
	headBytes   int64
	messages    int
	pullReady   bool
	st          *store.Store
	database    *db.DB
	gc          *gmail.Client
	dr          *drafts.Service
	eng         *transfers.Engine
	client      *mcp.Client
	telemetry   *telemetry.Capture
	bus         *events.Capture
	writer      *telemetry.Writer
	emitter     *events.Emitter
}

func (f *handlerFixture) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, handlerRequest{r.Method, r.URL.Path, r.URL.Query(), r.Header.Clone(), b})
}
func (f *handlerFixture) calls() []handlerRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]handlerRequest{}, f.requests...)
}
func (f *handlerFixture) reset() { f.mu.Lock(); defer f.mu.Unlock(); f.requests = nil }
func handlerMail(i int) map[string]any {
	id := fmt.Sprintf("msg-%02d", i)
	return map[string]any{"id": id, "threadId": "thread-fixture", "internalDate": strconv.FormatInt(int64(i)*1000, 10), "sizeEstimate": 4, "snippet": "snippet-fixture", "labelIds": []string{"INBOX", "UNREAD"}, "payload": map[string]any{"mimeType": "multipart/mixed", "headers": []map[string]string{{"name": "From", "value": "Sender <sender@example.test>"}, {"name": "To", "value": "Receiver <receiver@example.test>"}, {"name": "Cc", "value": "Copy <copy@example.test>"}, {"name": "Subject", "value": "subject-fixture"}, {"name": "Message-ID", "value": "<fixture@example.test>"}, {"name": "In-Reply-To", "value": "<original@example.test>"}, {"name": "Reply-To", "value": "reply@example.test"}, {"name": "References", "value": "<reference@example.test>"}}, "parts": []any{map[string]any{"mimeType": "text/plain", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("body-fixture"))}}, map[string]any{"partId": "part-fixture", "filename": "file-fixture.bin", "mimeType": "application/octet-stream", "body": map[string]any{"size": 4, "attachmentId": "attachment-fixture"}}}}}
}
func (f *handlerFixture) serveGmail(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	f.mu.Lock()
	status, n, pullReady := f.gmailStatus, f.messages, f.pullReady
	f.mu.Unlock()
	if status == 404 && strings.HasSuffix(r.URL.Path, "/labels") {
		status = 200
	}
	if status != 200 {
		w.WriteHeader(status)
		return
	}
	path := r.URL.Path
	var answer any
	switch {
	case strings.HasSuffix(path, "/labels"):
		answer = map[string]any{"labels": []map[string]string{{"id": "INBOX", "name": "INBOX", "type": "system"}, {"id": "SENT", "name": "SENT", "type": "system"}, {"id": "custom-fixture", "name": "Chosen Label", "type": "user"}}}
	case path == "/gmail/v1/users/me/messages":
		limit, _ := strconv.Atoi(r.URL.Query().Get("maxResults"))
		start := 0
		start, _ = strconv.Atoi(r.URL.Query().Get("pageToken"))
		ids := []map[string]string{}
		for i := start; i < n && i < start+limit; i++ {
			ids = append(ids, map[string]string{"id": fmt.Sprintf("msg-%02d", n-i)})
		}
		obj := map[string]any{"messages": ids}
		if start+limit < n {
			obj["nextPageToken"] = strconv.Itoa(start + limit)
		}
		answer = obj
	case strings.Contains(path, "/threads/"):
		list := []any{}
		for i := n; i > 0; i-- {
			list = append(list, handlerMail(i))
		}
		answer = map[string]any{"messages": list}
	case path == "/gmail/v1/users/me/drafts" && r.Method == http.MethodPost:
		answer = map[string]any{"id": "remote-fixture", "message": map[string]string{"id": "draft-message", "threadId": "thread-fixture"}}
	case strings.HasSuffix(path, "/drafts/send"):
		answer = map[string]string{"id": "sent-message", "threadId": "sent-thread"}
	case strings.Contains(path, "/drafts/"):
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		answer = map[string]any{"id": "remote-fixture", "message": map[string]string{"id": "draft-message", "threadId": "thread-fixture"}}
	case strings.Contains(path, "/attachments/") && pullReady:
		answer = map[string]any{"size": 4, "data": base64.RawURLEncoding.EncodeToString([]byte("mail"))}
	case r.URL.Query().Get("format") == "raw" || strings.Contains(path, "/attachments/"):
		<-r.Context().Done()
		return
	default:
		i, _ := strconv.Atoi(strings.TrimPrefix(path, "/gmail/v1/users/me/messages/msg-"))
		if i == 0 {
			i = 1
		}
		answer = handlerMail(i)
	}
	_ = json.NewEncoder(w).Encode(answer)
}
func newHandlerFixture(t *testing.T, connection bool) *handlerFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &handlerFixture{tokenStatus: 200, gmailStatus: 200, headStatus: 200, headBytes: 4, messages: 1, telemetry: &telemetry.Capture{}, bus: &events.Capture{}}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "gmail.db"), Migrations: root.Migrations(), Now: handlerClock})
	if err != nil {
		t.Fatal(err)
	}
	f.database = d
	t.Cleanup(func() { _ = d.Close() })
	random := make([]byte, 8192)
	for i := range random {
		random[i] = byte(i / 8)
	}
	f.st = store.New(d, store.Config{Now: handlerClock, Rand: bytes.NewReader(random)})
	f.writer = telemetry.New(telemetry.Config{Service: "gmail", Version: "fixture", Sink: f.telemetry, Stderr: io.Discard, Now: handlerClock, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 8192))})
	f.emitter = events.New(events.Config{Service: "gmail", Sink: f.bus, Stderr: io.Discard, Now: handlerClock, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 8192)), Emits: trail.Emits(), Telemetry: f.writer})
	t.Cleanup(func() { f.emitter.Shutdown(context.Background()); f.writer.Shutdown(context.Background(), "fixture") })
	var cc *connections.Client
	if connection {
		cc = connections.NewClient(connections.ClientConfig{Telemetry: f.writer, Service: handlerSocket(t, "connections", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.record(r)
			f.mu.Lock()
			status, body := f.tokenStatus, f.tokenBody
			f.mu.Unlock()
			w.WriteHeader(status)
			if body != "" {
				_, _ = io.WriteString(w, body)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": handlerToken, "expires": handlerClock().Add(time.Hour).Format(time.RFC3339), "account": "sender@example.test", "scopes": []string{}})
		}))})
	}
	ac := artifacts.NewClient(artifacts.ClientConfig{Telemetry: f.writer, Service: handlerSocket(t, "artifacts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		f.mu.Lock()
		status, size, pullReady := f.headStatus, f.headBytes, f.pullReady
		f.mu.Unlock()
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			w.Header().Set("ETag", `"fixture-hash"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Last-Modified", handlerClock().Format(http.TimeFormat))
			w.WriteHeader(status)
			return
		}
		if r.Method == http.MethodPut && pullReady {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "artifact-fixture", "path": strings.TrimPrefix(r.URL.Path, "/files/"), "bytes": 4, "content_type": "application/octet-stream", "sha256": "fixture-hash", "owner": handlerCaller.UserID, "created": handlerClock().Format(time.RFC3339), "modified": handlerClock().Format(time.RFC3339), "url": "https://artifacts.example.test/files/folder/file.bin"})
			return
		}
		<-r.Context().Done()
	}))})
	gs := httptest.NewServer(http.HandlerFunc(f.serveGmail))
	t.Cleanup(gs.Close)
	f.gc = gmail.NewClient(gmail.Config{HTTP: gs.Client(), BaseURL: gs.URL, Telemetry: f.writer})
	f.dr = drafts.New(drafts.Config{Store: f.st, Gmail: f.gc, Connections: cc, Telemetry: f.writer, Events: f.emitter, ConnectURL: handlerConnect})
	f.eng = transfers.New(transfers.Config{Store: f.st, Gmail: f.gc, Artifacts: ac, Connections: cc, Drafts: f.dr, Telemetry: f.writer, Events: f.emitter, ConnectURL: handlerConnect, MaxBytes: 1024, MaxActive: 1, MaxQueued: 1, Now: handlerClock})
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); f.eng.Drain(ctx) })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "gmail", Version: "fixture", Telemetry: f.writer})
	tools.Register(srv, tools.Config{Transfers: f.eng, Drafts: f.dr, Gmail: f.gc, Store: f.st})
	hs := httptest.NewServer(identity.Require(srv))
	t.Cleanup(hs.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: hs.URL, HTTPClient: hs.Client()})
	return f
}
func (f *handlerFixture) call(t *testing.T, name string, args any, wantError string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.client.CallTool(context.Background(), handlerCaller, name, b)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Content    []struct{ Type, Text string }
		Structured json.RawMessage `json:"structuredContent"`
	}
	handlerJSON(t, wire, &out)
	if result.IsError() != (wantError != "") {
		t.Fatalf("unexpected result: %s, wanted error %q", wire, wantError)
	}
	handlerEqual(t, len(out.Content), 1)
	handlerEqual(t, out.Content[0].Type, "text")
	if wantError != "" {
		handlerEqual(t, out.Content[0].Text, wantError)
		if out.Structured != nil {
			t.Fatalf("structured refusal: %s", wire)
		}
		return nil
	}
	if !json.Valid(out.Structured) {
		t.Fatalf("missing structured success: %s", wire)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, out.Structured); err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, out.Content[0].Text, compact.String())
	return out.Structured
}
func (f *handlerFixture) noMutation(t *testing.T, before []store.Draft) {
	t.Helper()
	ds, err := f.st.ListDrafts(context.Background(), handlerCaller.UserID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, ds, before)
	ts, err := f.st.ListTransfers(context.Background(), handlerCaller.UserID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, len(ts), 0)
	for _, r := range f.calls() {
		if strings.HasPrefix(r.Path, "/files/") {
			t.Fatalf("unexpected artifact call: %+v", r)
		}
	}
	if err := f.emitter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.telemetry.Events() {
		if strings.HasPrefix(e.Name, "transfer.") || strings.HasPrefix(e.Name, "draft.") {
			t.Fatal(e)
		}
	}
	handlerEqual(t, len(f.bus.Events()), 0)
}
func handlerRefusal(word string) string {
	sentences := map[string]string{"not_connected": tools.RefuseNotConnected, "reconnect": tools.RefuseReconnect, "not_found": tools.RefuseNotFound, "too_large": tools.RefuseTooLarge, "provider": tools.RefuseProvider, "path": tools.RefusePath, "expires": tools.RefuseExpires, "unavailable": tools.RefuseUnavailable, "stopping": tools.RefuseStopping, "queue_full": tools.RefuseQueueFull, "not_open": tools.RefuseNotOpen, "busy": tools.RefuseBusy, "to": tools.RefuseTo, "address": tools.RefuseAddress}
	return word + ": " + sentences[word]
}

// R-YEKN-EC0U R-3M19-1C7O R-FFUO-QJLJ
func TestHandlersConnectionPrecedesArguments(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		connection      bool
		status          int
		body, word, url string
	}{{"missing", true, 404, `{"error":"not_connected"}`, "not_connected", handlerConnect}, {"broken", true, 409, `{"error":"reconnect","url":"https://reconnect.example.test/fixture"}`, "reconnect", "https://reconnect.example.test/fixture"}, {"absent", false, 200, "", "unavailable", ""}} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newHandlerFixture(t, scenario.connection)
			f.tokenStatus = scenario.status
			f.tokenBody = scenario.body
			for _, v := range []struct {
				name string
				args any
			}{{"search", map[string]any{"query": "private-subject", "limit": 0, "page": "bogus"}}, {"thread", map[string]any{"thread": ""}}, {"show", map[string]any{"message": ""}}, {"labels", map[string]any{}}, {"pull", map[string]any{"message": "secret-message", "path": "secret-file", "expires": ""}}, {"compose", map[string]any{"to": []string{"private-address"}}}, {"push", map[string]any{"path": "secret-file", "draft": "unknown"}}, {"send", map[string]any{"draft": "unknown"}}, {"discard", map[string]any{"draft": "unknown"}}} {
				f.reset()
				want := handlerRefusal(scenario.word)
				if scenario.url != "" {
					want += " " + scenario.url
				}
				f.call(t, v.name, v.args, want)
				calls := f.calls()
				if scenario.connection {
					handlerEqual(t, len(calls), 1)
					handlerEqual(t, calls[0].Path, "/token/google")
					handlerEqual(t, calls[0].Header.Get("X-User-Id"), handlerCaller.UserID)
				} else {
					handlerEqual(t, len(calls), 0)
				}
				f.noMutation(t, []store.Draft{})
			}
		})
	}
}

// R-YFSJ-S3RJ R-FH2L-4BC8 R-3N95-F3YD R-YRZJ-LT6H R-3POY-6NFR R-YWV5-4W59 R-YY31-INVY R-YZAX-WFMN
func TestHandlersReadMail(t *testing.T) {
	f := newHandlerFixture(t, true)
	f.messages = paging.DefaultLimit + 5
	query := "from:sender@example.test  has:attachment\tnewer_than:7d"
	for _, name := range []string{"search", "thread"} {
		f.reset()
		args := map[string]any{"query": query, "label": "cHoSeN lAbEl"}
		if name == "thread" {
			args = map[string]any{"thread": "thread-fixture"}
		}
		var first tools.MessageList
		handlerJSON(t, f.call(t, name, args, ""), &first)
		handlerEqual(t, len(first.Messages), paging.DefaultLimit)
		if first.Next == nil {
			t.Fatal("missing next")
		}
		args["page"] = *first.Next
		var second tools.MessageList
		handlerJSON(t, f.call(t, name, args, ""), &second)
		handlerEqual(t, len(second.Messages), 5)
		if second.Next != nil {
			t.Fatal("last page has next")
		}
		all := append([]tools.Summary{}, first.Messages...)
		all = append(all, second.Messages...)
		for i, s := range all {
			n := paging.DefaultLimit + 5 - i
			if name == "thread" {
				n = i + 1
			}
			want := tools.SummaryOf(gmail.Summary{ID: fmt.Sprintf("msg-%02d", n), Thread: "thread-fixture", Date: time.Unix(int64(n), 0), From: "Sender <sender@example.test>", To: "Receiver <receiver@example.test>", Cc: "Copy <copy@example.test>", Subject: "subject-fixture", Snippet: "snippet-fixture", Labels: []string{"INBOX", "UNREAD"}, Unread: true, Attachments: 1, Link: urls.MessageLink(fmt.Sprintf("msg-%02d", n))})
			handlerEqual(t, s, want)
		}
		tokens := 0
		lists := 0
		for _, r := range f.calls() {
			if r.Path == "/token/google" {
				tokens++
				handlerEqual(t, r.Method, "GET")
				handlerEqual(t, r.Header.Get("X-User-Id"), handlerCaller.UserID)
			} else {
				handlerEqual(t, r.Header.Get("Authorization"), "Bearer "+handlerToken)
			}
			if strings.Contains(r.Path, "/threads/") {
				handlerEqual(t, r.Path, "/gmail/v1/users/me/threads/thread-fixture")
			}
			if r.Path == "/gmail/v1/users/me/messages" {
				lists++
				handlerEqual(t, r.Query["q"], []string{query})
				handlerEqual(t, r.Query["labelIds"], []string{"custom-fixture"})
				if lists == 2 {
					handlerEqual(t, r.Query["pageToken"], []string{strconv.Itoa(paging.DefaultLimit)})
				}
			}
		}
		handlerEqual(t, tokens, 2)
	}
	f.reset()
	var empty tools.MessageList
	f.messages = 0
	handlerJSON(t, f.call(t, "search", map[string]any{"query": ""}, ""), &empty)
	handlerEqual(t, empty.Messages, []tools.Summary{})
	if empty.Next != nil {
		t.Fatal(empty)
	}
	for _, r := range f.calls() {
		if r.Path == "/gmail/v1/users/me/messages" {
			handlerEqual(t, r.Query["q"], []string(nil))
			handlerEqual(t, r.Query["labelIds"], []string(nil))
			handlerEqual(t, r.Query["pageToken"], []string(nil))
		}
	}
	f.messages = 1
	f.reset()
	var full tools.Message
	handlerJSON(t, f.call(t, "show", tools.ShowArgs{Message: "msg-01"}, ""), &full)
	want := tools.MessageOf(gmail.Message{ID: "msg-01", Thread: "thread-fixture", Date: time.Unix(1, 0), From: "Sender <sender@example.test>", To: "Receiver <receiver@example.test>", Cc: "Copy <copy@example.test>", Subject: "subject-fixture", Snippet: "snippet-fixture", Labels: []string{"INBOX", "UNREAD"}, Unread: true, Attachments: []gmail.Attachment{{ID: "part-fixture", Name: "file-fixture.bin", MimeType: "application/octet-stream", Bytes: 4}}, Link: urls.MessageLink("msg-01"), MessageID: "<fixture@example.test>", InReplyTo: "<original@example.test>", Body: "body-fixture"})
	handlerEqual(t, full, want)
	tokens := 0
	for _, r := range f.calls() {
		if r.Path == "/token/google" {
			tokens++
		} else {
			handlerEqual(t, r.Header.Get("Authorization"), "Bearer "+handlerToken)
		}
	}
	handlerEqual(t, tokens, 1)
	seenMessage := false
	for _, r := range f.calls() {
		if strings.Contains(r.Path, "/messages/") {
			handlerEqual(t, r.Path, "/gmail/v1/users/me/messages/msg-01")
			seenMessage = true
		}
	}
	handlerEqual(t, seenMessage, true)
	f.reset()
	var labels tools.LabelList
	handlerJSON(t, f.call(t, "labels", tools.NoArgs{}, ""), &labels)
	handlerEqual(t, labels.Labels, []tools.Label{{Name: "INBOX", System: true}, {Name: "SENT", System: true}, {Name: "Chosen Label", System: false}})
	for _, name := range []string{"show", "thread"} {
		f.reset()
		key := "message"
		if name == "thread" {
			key = "thread"
		}
		f.call(t, name, map[string]any{key: ""}, handlerRefusal("not_found"))
		handlerEqual(t, len(f.calls()), 1)
	}
	f.noMutation(t, []store.Draft{})
}

// R-MSW3-RK24 R-MU40-5BST R-YZAX-WFMN
func TestHandlersReadRefusals(t *testing.T) {
	for _, name := range []string{"search", "thread", "show", "labels"} {
		for _, code := range []int{401, 500, 404} {
			// labels.list's 404 is a provider failure; an unknown message/thread is not found.
			if code == 404 && (name == "search" || name == "labels") {
				continue
			}
			t.Run(name+strconv.Itoa(code), func(t *testing.T) {
				f := newHandlerFixture(t, true)
				f.gmailStatus = code
				word := "provider"
				if code == 401 {
					word = "reconnect"
				}
				if code == 404 {
					word = "not_found"
				}
				want := handlerRefusal(word)
				if code == 401 {
					want += " " + handlerConnect
				}
				args := map[string]any{}
				switch name {
				case "search":
					args["query"] = "secret-subject"
				case "thread":
					args["thread"] = "secret-thread"
				case "show":
					args["message"] = "secret-message"
				}
				f.call(t, name, args, want)
				f.noMutation(t, []store.Draft{})
			})
		}
	}
	f := newHandlerFixture(t, true)
	f.call(t, "thread", map[string]any{"thread": "thread-fixture", "page": "x"}, handlerRefusal("provider"))
	f.reset()
	f.call(t, "search", map[string]any{"query": "private-subject", "label": "absent-label"}, handlerRefusal("not_found"))
	for _, r := range f.calls() {
		if r.Path == "/gmail/v1/users/me/messages" {
			t.Fatal("listed unknown label")
		}
	}
	f.noMutation(t, []store.Draft{})
}

func (f *handlerFixture) seedDraft(t *testing.T) store.Draft {
	t.Helper()
	d, err := f.st.AddDraft(context.Background(), store.Draft{GmailDraft: "remote-fixture", Message: "draft-message", Thread: "thread-fixture", UserID: handlerCaller.UserID, To: []string{"receiver@example.test"}, Subject: "stored-subject", Body: "stored-body"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// R-YN3Y-2Q7P R-N07I-26IA
func TestHandlersStoredCallsWithoutConnection(t *testing.T) {
	f := newHandlerFixture(t, true)
	f.tokenStatus = 404
	f.tokenBody = `{"error":"not_connected"}`
	d := f.seedDraft(t)
	ended, err := f.st.MarkDraftSent(context.Background(), d.ID, "sent-message", "sent-thread", handlerClock())
	if err != nil {
		t.Fatal(err)
	}
	second := f.seedDraft(t)
	discarded, err := f.st.MarkDraftDiscarded(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []store.Draft{ended, discarded} {
		var got tools.Draft
		handlerJSON(t, f.call(t, "draft", tools.DraftArgs{Draft: d.ID}, ""), &got)
		handlerEqual(t, got, tools.DraftOf(d, true))
	}
	for _, v := range []struct {
		name string
		args any
	}{{"drafts", tools.NoArgs{}}, {"transfers", tools.NoArgs{}}} {
		f.call(t, v.name, v.args, "")
	}
	f.call(t, "draft", tools.DraftArgs{Draft: "absent"}, handlerRefusal("not_found"))
	f.call(t, "transfer", tools.TransferArgs{Transfer: "absent"}, handlerRefusal("not_found"))
	tr, err := f.st.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Message: "message", Path: "path", UserID: handlerCaller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	var got tools.Record
	handlerJSON(t, f.call(t, "transfer", tools.TransferArgs{Transfer: tr.ID}, ""), &got)
	handlerEqual(t, got, tools.RecordOf(tr))
	handlerEqual(t, len(f.calls()), 0)
}

// R-FH2L-4BC8 R-MXRP-AN0W R-MYZL-OERL R-N07I-26IA
func TestHandlersDraftLifecycle(t *testing.T) {
	f := newHandlerFixture(t, true)
	f.call(t, "compose", map[string]any{}, handlerRefusal("to"))
	handlerEqual(t, len(f.calls()), 1)
	f.reset()
	subject := ""
	args := tools.ComposeArgs{To: []string{"Receiver <receiver@example.test>", "second@example.test"}, Cc: []string{"copy@example.test"}, Bcc: []string{"hidden@example.test"}, Subject: &subject, Body: "chosen-body"}
	var made tools.Draft
	handlerJSON(t, f.call(t, "compose", args, ""), &made)
	stored, err := f.st.GetDraft(context.Background(), handlerCaller.UserID, made.ID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, made, tools.DraftOf(stored, false))
	handlerEqual(t, stored.To, args.To)
	handlerEqual(t, stored.Cc, args.Cc)
	handlerEqual(t, stored.Bcc, args.Bcc)
	handlerEqual(t, stored.Subject, "")
	handlerEqual(t, stored.Body, args.Body)
	for _, r := range f.calls() {
		if r.Method == "POST" && r.Path == "/gmail/v1/users/me/drafts" {
			var raw struct{ Message struct{ Raw string } }
			handlerJSON(t, r.Body, &raw)
			decoded, err := base64.URLEncoding.DecodeString(raw.Message.Raw)
			if err != nil {
				t.Fatal(err)
			}
			mail, err := mail.ReadMessage(bytes.NewReader(decoded))
			if err != nil {
				t.Fatal(err)
			}
			handlerEqual(t, mail.Header.Get("Subject"), "")
		}
	}
	f.reset()
	var read tools.Draft
	handlerJSON(t, f.call(t, "draft", tools.DraftArgs{Draft: made.ID}, ""), &read)
	current, err := f.st.GetDraft(context.Background(), handlerCaller.UserID, made.ID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, read, tools.DraftOf(current, true))
	handlerEqual(t, len(f.calls()), 2)
	f.reset()
	var sent tools.Draft
	handlerJSON(t, f.call(t, "send", tools.DraftArgs{Draft: made.ID}, ""), &sent)
	current, err = f.st.GetDraft(context.Background(), handlerCaller.UserID, made.ID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, sent, tools.DraftOf(current, false))
	handlerEqual(t, sent.Status, store.DraftSent)
	f.call(t, "send", tools.DraftArgs{Draft: made.ID}, handlerRefusal("not_open"))
	another := f.seedDraft(t)
	var discarded tools.Draft
	handlerJSON(t, f.call(t, "discard", tools.DraftArgs{Draft: another.ID}, ""), &discarded)
	current, err = f.st.GetDraft(context.Background(), handlerCaller.UserID, another.ID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, discarded, tools.DraftOf(current, false))
	handlerEqual(t, discarded.Status, store.DraftDiscarded)
	f.reset()
	f.call(t, "send", tools.DraftArgs{Draft: "absent"}, handlerRefusal("not_found"))
	handlerEqual(t, len(f.calls()), 1)
	f.reset()
	f.call(t, "compose", map[string]any{"to": []string{"private-invalid-address"}}, handlerRefusal("address"))
	handlerEqual(t, len(f.calls()), 1)
}

// R-Z0IU-A7DC R-Z2YN-1QUQ R-FH2L-4BC8
func TestHandlersTransfers(t *testing.T) {
	for _, name := range []string{"pull", "push"} {
		t.Run(name, func(t *testing.T) {
			f := newHandlerFixture(t, true)
			d := f.seedDraft(t)
			args := map[string]any{"message": "msg-01", "path": "folder/file.bin", "attachment": "part-fixture", "expires": handlerClock().Add(time.Hour).Format(time.RFC3339)}
			if name == "push" {
				args = map[string]any{"path": "folder/file.bin", "draft": d.ID}
			}
			f.headStatus = 400
			f.call(t, name, args, handlerRefusal("path"))
			handlerEqual(t, len(f.calls()), 2)
			handlerEqual(t, f.calls()[0].Path, "/token/google")
			f.headStatus = 200
			f.reset()
			if name == "pull" {
				for _, expiry := range []string{"", "invalid-expiry", handlerClock().Add(-time.Hour).Format(time.RFC3339)} {
					f.reset()
					args["expires"] = expiry
					f.call(t, name, args, handlerRefusal("expires"))
					handlerEqual(t, len(f.calls()), 1)
				}
				args["expires"] = handlerClock().Add(time.Hour).Format(time.RFC3339)
			}
			var rec tools.Record
			handlerJSON(t, f.call(t, name, args, ""), &rec)
			got, err := f.st.GetTransfer(context.Background(), handlerCaller.UserID, rec.ID)
			if err != nil {
				t.Fatal(err)
			}
			handlerEqual(t, rec, tools.RecordOf(got))
			handlerEqual(t, got.Path, "folder/file.bin")
			handlerEqual(t, got.UserID, handlerCaller.UserID)
			handlerEqual(t, got.UserEmail, handlerCaller.Email)
			handlerEqual(t, got.RequestID, handlerCaller.RequestID)
			if name == "pull" {
				handlerEqual(t, got.Message, "msg-01")
				handlerEqual(t, got.Attachment, "part-fixture")
				handlerEqual(t, got.Name, "file-fixture.bin")
			} else {
				handlerEqual(t, got.Draft, d.ID)
				handlerEqual(t, got.Message, d.Message)
				handlerEqual(t, got.Name, "file.bin")
			}
		})
	}
	t.Run("pull_expiry_reaches_artifacts", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		f.mu.Lock()
		f.pullReady = true
		f.mu.Unlock()
		expiry := handlerClock().Add(7 * time.Hour).Format(time.RFC3339)
		var rec tools.Record
		handlerJSON(t, f.call(t, "pull", tools.PullArgs{Message: "msg-01", Path: "folder/file.bin", Attachment: "part-fixture", Expires: &expiry}, ""), &rec)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		f.eng.Drain(ctx)
		if ctx.Err() != nil {
			t.Fatal("pull did not finish", ctx.Err())
		}
		got, err := f.st.GetTransfer(context.Background(), handlerCaller.UserID, rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		handlerEqual(t, got.Status, store.StatusDone)
		puts := 0
		for _, r := range f.calls() {
			if r.Method == http.MethodPut && strings.HasPrefix(r.Path, "/files/") {
				puts++
				handlerEqual(t, r.Path, "/files/folder/file.bin")
				handlerEqual(t, r.Header.Get("X-Artifact-Expires"), expiry)
				handlerEqual(t, string(r.Body), "mail")
			}
		}
		handlerEqual(t, puts, 1)
	})
}

// R-MWJS-WVA7
func TestHandlersStorageFailures(t *testing.T) {
	for _, name := range []string{"pull", "push", "compose", "send", "discard", "draft"} {
		t.Run(name, func(t *testing.T) {
			f := newHandlerFixture(t, true)
			d := f.seedDraft(t)
			f.database.SetFailing(true)
			args := map[string]any{"draft": d.ID}
			switch name {
			case "pull":
				args = map[string]any{"message": "msg-01", "attachment": "part-fixture", "path": "folder/file.bin"}
			case "push":
				args["path"] = "folder/file.bin"
			case "compose":
				args = map[string]any{"to": []string{"receiver@example.test"}}
			}
			f.call(t, name, args, handlerRefusal("unavailable"))
			f.database.SetFailing(false)
			ds, err := f.st.ListDrafts(context.Background(), handlerCaller.UserID)
			if err != nil {
				t.Fatal(err)
			}
			handlerEqual(t, ds, []store.Draft{d})
			ts, err := f.st.ListTransfers(context.Background(), handlerCaller.UserID)
			if err != nil {
				t.Fatal(err)
			}
			handlerEqual(t, len(ts), 0)
		})
	}
}

// R-3M19-1C7O R-Z0IU-A7DC R-Z2YN-1QUQ R-MYZL-OERL
func TestHandlersRemainingServiceRefusals(t *testing.T) {
	t.Run("stopping", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		f.eng.Drain(context.Background())
		f.call(t, "pull", map[string]any{"message": "msg-01", "path": "whole-message"}, handlerRefusal("stopping"))
		f.call(t, "pull", map[string]any{"message": "msg-01", "path": "part", "attachment": "unknown-part"}, handlerRefusal("not_found"))
	})
	t.Run("queue_and_busy", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		d := f.seedDraft(t)
		args := tools.PushArgs{Path: "file.bin", Draft: d.ID}
		f.call(t, "push", args, "")
		f.call(t, "push", args, "")
		f.call(t, "push", args, handlerRefusal("queue_full"))
		for _, name := range []string{"send", "discard"} {
			f.call(t, name, tools.DraftArgs{Draft: d.ID}, handlerRefusal("busy"))
		}
	})
	t.Run("too_large", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		f.headBytes = 2048
		f.call(t, "push", tools.PushArgs{Path: "file.bin", Draft: "unknown"}, handlerRefusal("too_large"))
	})
	t.Run("not_open", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		d := f.seedDraft(t)
		if _, err := f.st.MarkDraftDiscarded(context.Background(), d.ID); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"send", "discard", "push"} {
			args := map[string]any{"draft": d.ID}
			if name == "push" {
				args["path"] = "file.bin"
			}
			f.call(t, name, args, handlerRefusal("not_open"))
		}
	})
	t.Run("provider", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		d := f.seedDraft(t)
		f.gmailStatus = 500
		for _, name := range []string{"draft", "send", "discard"} {
			f.call(t, name, tools.DraftArgs{Draft: d.ID}, handlerRefusal("provider"))
		}
	})
}

// R-MXRP-AN0W
func TestHandlersComposeOptionalAndReply(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(strconv.FormatBool(explicit), func(t *testing.T) {
			f := newHandlerFixture(t, true)
			args := map[string]any{"reply_to": "msg-01"}
			if explicit {
				args["subject"] = ""
			}
			var got tools.Draft
			handlerJSON(t, f.call(t, "compose", args, ""), &got)
			d, err := f.st.GetDraft(context.Background(), handlerCaller.UserID, got.ID)
			if err != nil {
				t.Fatal(err)
			}
			handlerEqual(t, got, tools.DraftOf(d, false))
			handlerEqual(t, d.ReplyTo, "msg-01")
			handlerEqual(t, d.To, []string{"reply@example.test"})
			handlerEqual(t, d.Cc, []string{})
			handlerEqual(t, d.Bcc, []string{})
			handlerEqual(t, d.Body, "")
			subject := "Re: subject-fixture"
			if explicit {
				subject = ""
			}
			handlerEqual(t, d.Subject, subject)
			tokens := 0
			for _, r := range f.calls() {
				if r.Path == "/token/google" {
					tokens++
				}
				if r.Method == "POST" {
					var payload struct {
						Message struct {
							Thread string `json:"threadId"`
						}
					}
					handlerJSON(t, r.Body, &payload)
					handlerEqual(t, payload.Message.Thread, "thread-fixture")
				}
			}
			handlerEqual(t, tokens, 1)
		})
	}
	f := newHandlerFixture(t, true)
	var got tools.Draft
	handlerJSON(t, f.call(t, "compose", map[string]any{"to": []string{"receiver@example.test"}}, ""), &got)
	d, err := f.st.GetDraft(context.Background(), handlerCaller.UserID, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, d.Subject, "")
	handlerEqual(t, d.ReplyTo, "")
	handlerEqual(t, d.Body, "")
}

// R-YZAX-WFMN
func TestHandlersReadsPreserveExistingRecords(t *testing.T) {
	f := newHandlerFixture(t, true)
	d := f.seedDraft(t)
	tr, err := f.st.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Message: "message", Path: "file", UserID: handlerCaller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		name string
		args any
	}{{"search", map[string]any{"query": ""}}, {"thread", tools.ThreadArgs{Thread: "thread-fixture"}}, {"show", tools.ShowArgs{Message: "msg-01"}}, {"labels", tools.NoArgs{}}, {"drafts", tools.NoArgs{}}, {"transfers", tools.NoArgs{}}, {"transfer", tools.TransferArgs{Transfer: tr.ID}}} {
		f.call(t, v.name, v.args, "")
	}
	ds, err := f.st.ListDrafts(context.Background(), handlerCaller.UserID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, ds, []store.Draft{d})
	ts, err := f.st.ListTransfers(context.Background(), handlerCaller.UserID)
	if err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, ts, []store.Transfer{tr})
	for _, r := range f.calls() {
		if strings.HasPrefix(r.Path, "/files/") {
			t.Fatal(r)
		}
	}
	if err := f.emitter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	handlerEqual(t, len(f.bus.Events()), 0)
	for _, e := range f.telemetry.Events() {
		if strings.HasPrefix(e.Name, "transfer.") || strings.HasPrefix(e.Name, "draft.") {
			t.Fatal(e)
		}
	}
}

// R-Z0IU-A7DC R-N07I-26IA R-MYZL-OERL R-Z2YN-1QUQ
func TestHandlersWholePullAndDraftOwnership(t *testing.T) {
	t.Run("whole_message", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		var got tools.Record
		handlerJSON(t, f.call(t, "pull", map[string]any{"message": "msg-01", "path": "whole.eml"}, ""), &got)
		tr, err := f.st.GetTransfer(context.Background(), handlerCaller.UserID, got.ID)
		if err != nil {
			t.Fatal(err)
		}
		handlerEqual(t, got, tools.RecordOf(tr))
		handlerEqual(t, tr.Attachment, "")
		handlerEqual(t, tr.Name, "subject-fixture")
		handlerEqual(t, tr.ContentType, transfers.ContentTypeMessage)
	})
	t.Run("other_caller", func(t *testing.T) {
		f := newHandlerFixture(t, true)
		d, err := f.st.AddDraft(context.Background(), store.Draft{GmailDraft: "other-remote", Message: "other-message", UserID: "another-user"})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"draft", "send", "discard", "push"} {
			args := map[string]any{"draft": d.ID}
			if name == "push" {
				args["path"] = "file.bin"
			}
			f.call(t, name, args, handlerRefusal("not_found"))
		}
		after, err := f.st.GetDraft(context.Background(), "another-user", d.ID)
		if err != nil {
			t.Fatal(err)
		}
		handlerEqual(t, after, d)
	})
}

// R-3OH1-SVP2 R-3KTC-NKGZ R-3M19-1C7O
func TestSearchPagingValidation(t *testing.T) {
	f := newHandlerFixture(t, true)
	for _, limit := range []int{0, -1, paging.MaxLimit + 1} {
		_, err := paging.Window(&limit)
		for _, page := range []string{"", "bogus"} {
			f.reset()
			f.call(t, "search", map[string]any{"query": "", "page": page, "limit": limit}, err.Error())
			for _, r := range f.calls() {
				if r.Path != "/token/google" {
					t.Fatal(r)
				}
			}
		}
	}
	f.gmailStatus = 400
	f.call(t, "search", map[string]any{"query": "", "page": "bogus"}, handlerRefusal("provider"))
}

// R-3N95-F3YD R-3POY-6NFR
func TestSearchWindowAndOptionalArguments(t *testing.T) {
	for _, limit := range []any{nil, 10, paging.MaxLimit} {
		f := newHandlerFixture(t, true)
		n := paging.DefaultLimit
		if l, ok := limit.(int); ok {
			n = l
		}
		f.messages = n + 2
		if limit == nil {
			f.messages = n + 5
		}
		args := map[string]any{"query": "query-fixture", "page": nil, "limit": limit}
		var first, second tools.MessageList
		handlerJSON(t, f.call(t, "search", args, ""), &first)
		handlerEqual(t, len(first.Messages), n)
		if first.Next == nil {
			t.Fatal(first)
		}
		args["page"] = first.Next
		handlerJSON(t, f.call(t, "search", args, ""), &second)
		handlerEqual(t, len(second.Messages), f.messages-n)
		if second.Next != nil {
			t.Fatal(second)
		}
		all := append([]tools.Summary{}, first.Messages...)
		all = append(all, second.Messages...)
		for i, s := range all {
			handlerEqual(t, s.ID, fmt.Sprintf("msg-%02d", f.messages-i))
		}
		for _, r := range f.calls() {
			if r.Path == "/gmail/v1/users/me/messages" {
				handlerEqual(t, r.Query["maxResults"], []string{strconv.Itoa(n)})
				handlerEqual(t, r.Query["q"], []string{"query-fixture"})
			}
		}
	}
}

// R-3QWU-KF6G
func TestDraftListingDoesNotRefresh(t *testing.T) {
	f := newHandlerFixture(t, true)
	d := f.seedDraft(t)
	f.gmailStatus = 404
	f.reset()
	var list tools.DraftList
	handlerJSON(t, f.call(t, "drafts", tools.DraftsArgs{}, ""), &list)
	handlerEqual(t, list.Drafts, []tools.Draft{tools.DraftOf(d, false)})
	handlerEqual(t, len(f.calls()), 0)
}
