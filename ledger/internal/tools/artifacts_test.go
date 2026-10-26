package tools_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

type artifactRequest struct{ Method, Path, Tool, Argument, User, Email, Request string }
type artifactFake struct {
	mu       sync.Mutex
	requests []artifactRequest
	head     int
	show     string
	id       string
}

func (f *artifactFake) received() []artifactRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]artifactRequest{}, f.requests...)
}
func (f *artifactFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	got := artifactRequest{Method: r.Method, Path: r.URL.Path, User: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email"), Request: r.Header.Get("X-Request-Id")}
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			Name      string `json:"name"`
			Arguments struct {
				Path string `json:"path"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
			w.WriteHeader(400)
			return
		}
		got.Tool = rpc.Params.Name
		got.Argument = rpc.Params.Arguments.Path
	}
	f.mu.Lock()
	f.requests = append(f.requests, got)
	f.mu.Unlock()
	if r.Method == http.MethodHead {
		if f.head != 200 {
			w.WriteHeader(f.head)
			return
		}
		w.Header().Set("Content-Length", "8")
		w.Header().Set("ETag", `"digest"`)
		w.Header().Set("Last-Modified", "Wed, 01 Apr 2026 00:00:00 GMT")
		w.WriteHeader(200)
		return
	}
	if f.show == "http" {
		w.WriteHeader(503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	md := map[string]any{"id": f.id, "path": got.Argument, "bytes": 8, "content_type": "application/pdf", "sha256": "digest", "owner": "owner", "created": "2026-04-01T00:00:00Z", "modified": "2026-04-01T00:00:00Z", "url": "https://artifacts.example.test/files/invoice.pdf"}
	result := map[string]any{"content": []map[string]string{{"type": "text", "text": "metadata"}}, "structuredContent": md}
	switch f.show {
	case "refusal":
		result = map[string]any{"content": []map[string]string{{"type": "text", "text": "not_found: private-path"}}, "isError": true}
	case "malformed":
		result["structuredContent"] = map[string]any{"id": 3}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
}
func (f *artifactFake) factory(t *testing.T) func(*telemetry.Writer) *artifacts.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "ledger-art-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: f, ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close(); <-done; _ = os.RemoveAll(dir) })
	return func(w *telemetry.Writer) *artifacts.Client {
		return artifacts.NewClient(artifacts.ClientConfig{Service: services.Entry{Name: "artifacts", Socket: socket}, Telemetry: w})
	}
}

// R-K33N-3L2K R-K4BJ-HCT9 R-K5JF-V4JY R-KP1T-ZGF2
func TestDocumentsThroughArtifacts(t *testing.T) {
	f := &artifactFake{head: 200, id: "artifact-kept"}
	h := newHarness(t, f.factory(t))
	h.open("Assets:Bank")
	h.open("Income:Sales")
	txn := h.post("invoice")
	if len(f.received()) != 0 {
		t.Fatal(f.received())
	}
	args := tools.AddDocumentArgs{Account: "Assets:Bank", Date: "2026-04-02", Path: "invoices/one.pdf", Transaction: &txn.Transaction.ID}
	d := success[tools.Document](h, "add_document", args)
	if d.Artifact != f.id || d.Path != args.Path || d.Transaction != txn.Transaction.ID || d.Account != args.Account || d.Date != args.Date || d.AddedBy != h.caller.UserID || d.Added != stamp(h.now) || d.Retracted != "" || d.RetractedBy != "" {
		t.Fatal(d)
	}
	requests := f.received()
	if len(requests) != 2 {
		t.Fatal(requests)
	}
	if requests[0].Method != http.MethodHead || requests[0].Path != "/files/"+args.Path || requests[1].Method != http.MethodPost || requests[1].Path != "/mcp" || requests[1].Tool != "show" || requests[1].Argument != args.Path {
		t.Fatal(requests)
	}
	for _, r := range requests {
		if r.User != h.caller.UserID || r.Email != h.caller.Email || r.Request != h.caller.RequestID {
			t.Fatal(r)
		}
	}
	read := success[tools.AccountRead](h, "account", tools.AccountArgs{Name: args.Account})
	if len(read.Documents) != 1 {
		t.Fatal(read)
	}
	equal(t, read.Documents[0], d)
	retracted := success[tools.DocumentChange](h, "retract_document", tools.DocumentArgs{Document: d.ID})
	if !retracted.Changed || retracted.Document.Retracted != stamp(h.now) || retracted.Document.RetractedBy != h.caller.UserID {
		t.Fatal(retracted)
	}
	again := success[tools.DocumentChange](h, "retract_document", tools.DocumentArgs{Document: d.ID})
	if again.Changed {
		t.Fatal(again)
	}
	equal(t, again.Document, retracted.Document)
	if len(f.received()) != 2 {
		t.Fatal(f.received())
	}
}

// R-K33N-3L2K R-JB1Y-AV0L R-J8M5-JBJ7
func TestDocumentRefusalOrder(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		head                                      int
		show, id, path, account, date, word, text string
		wantRequests                              int
		failing, locked, noClient                 bool
	}{
		{name: "empty first", head: 200, path: "", account: "missing", date: "bad", word: tools.RefusalInvalid, text: tools.RefuseInvalid, failing: true},
		{name: "unknown account", head: 200, path: "invoice.pdf", account: "Assets:Missing", date: "2026-04-02", word: tools.RefusalNotFound, text: tools.RefuseNotFound},
		{name: "locked", head: 200, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalInvalid, text: tools.RefuseLocked, locked: true},
		{name: "database", head: 200, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: store.Unreachable, failing: true},
		{name: "no client", path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, noClient: true},
		{name: "bad path locally", head: 200, path: "bad path", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalInvalid, text: tools.RefuseInvalid},
		{name: "head bad", head: 400, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalInvalid, text: tools.RefuseInvalid, wantRequests: 1},
		{name: "head missing", head: 404, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalNotFound, text: tools.RefuseNoArtifact, wantRequests: 1},
		{name: "head unavailable", head: 503, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, wantRequests: 1},
		{name: "head forbidden", head: 403, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, wantRequests: 1},
		{name: "show refused", head: 200, show: "refusal", path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, wantRequests: 2},
		{name: "show unavailable", head: 200, show: "http", path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, wantRequests: 2},
		{name: "show malformed", head: 200, show: "malformed", path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, wantRequests: 2},
		{name: "show no id", head: 200, path: "invoice.pdf", account: "Assets:Bank", date: "2026-04-02", word: tools.RefusalUnavailable, text: tools.RefuseArtifacts, wantRequests: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &artifactFake{head: tc.head, show: tc.show, id: tc.id}
			var h *harness
			if tc.noClient {
				h = newHarness(t)
			} else {
				h = newHarness(t, f.factory(t))
			}
			h.open("Assets:Bank")
			if tc.locked {
				success[tools.BookChange](h, "set_book", tools.SetBookArgs{LockDate: ptr("2026-04-02")})
			}
			h.flush()
			trailCount, busCount := len(mutations(h)), len(h.bus.Events())
			h.db.SetFailing(tc.failing)
			h.refuse("add_document", tools.AddDocumentArgs{Account: tc.account, Date: tc.date, Path: tc.path}, tc.word+": "+tc.text)
			h.db.SetFailing(false)
			read := success[tools.AccountRead](h, "account", tools.AccountArgs{Name: "Assets:Bank"})
			if len(read.Documents) != 0 {
				t.Fatal(read)
			}
			h.flush()
			if len(mutations(h)) != trailCount || len(h.bus.Events()) != busCount || len(f.received()) != tc.wantRequests {
				t.Fatal(f.received(), mutations(h), h.bus.Events())
			}
		})
	}
}
