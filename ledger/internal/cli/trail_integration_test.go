package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/pages"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

var trailMarkers = []string{"GHIJKLMNGHIJKLMNGHIJKLMN", "OPQRSTUVOPQRSTUVOPQRSTUV", "WXYZ1234WXYZ1234WXYZ1234", "ZYXWVUTSZYXWVUTSZYXWVUTS", "HGFEDCBAHGFEDCBAHGFEDCBA", "QRSTUVWXQRSTUVWXQRSTUVWX", "IJKLMNOPIJKLMNOPIJKLMNOP"}

type trailFake struct {
	mu       sync.Mutex
	requests []integrationRequest
}

func (f *trailFake) snapshot() []integrationRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]integrationRequest(nil), f.requests...)
}
func (f *trailFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, integrationRequest{Method: r.Method, Path: r.URL.Path, ID: r.Header.Get("X-Request-Id"), User: r.Header.Get("X-User-Id"), Email: r.Header.Get("X-User-Email")})
	f.mu.Unlock()
	if strings.Contains(r.URL.Path, "http-fail") && !strings.Contains(r.URL.Path, "show-http-fail") {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, strings.Join(trailMarkers, " "))
		return
	}
	if r.Method == "HEAD" {
		w.Header().Set("Content-Length", "12")
		w.Header().Set("ETag", `"chosen-hash"`)
		w.Header().Set("Last-Modified", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		w.Header().Set("Content-Type", trailMarkers[4])
		w.WriteHeader(200)
		return
	}
	var input struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			Arguments struct {
				Path string `json:"path"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		w.WriteHeader(400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(input.Params.Arguments.Path, "show-http-fail") {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, strings.Join(trailMarkers, " "))
		return
	}
	if strings.Contains(input.Params.Arguments.Path, "show-fail") {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": strings.Join(trailMarkers, " ")}}}})
		return
	}
	metadata := map[string]any{"id": "art_0123456789abcdef", "path": input.Params.Arguments.Path, "bytes": 12, "content_type": trailMarkers[4], "sha256": "chosen-hash", "owner": trailMarkers[5], "url": "https://" + trailMarkers[6] + ".example.test/file", "created": "2026-10-01T00:00:00Z", "modified": "2026-10-01T00:00:00Z"}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": map[string]any{"content": []any{}, "structuredContent": metadata}})
}
func trailServices(t *testing.T) (string, *trailFake) {
	t.Helper()
	fake := &trailFake{}
	socket := integrationServer(t, fake)
	path := filepath.Join(t.TempDir(), "services.json")
	integrationServices(t, path, map[string]string{"artifacts": socket}, "")
	return path, fake
}

type trailRequestWant struct {
	method, path, user string
	status             int
}

type trailDriver struct {
	t        *testing.T
	r        *integrationRun
	next     int
	calls    map[string]string
	expected map[string]trailRequestWant
}

func (d *trailDriver) call(name string, args map[string]any, refused bool) map[string]any {
	d.t.Helper()
	d.next++
	q := fmt.Sprintf("trail-call-%03d", d.next)
	d.calls[q] = name
	if d.expected == nil {
		d.expected = map[string]trailRequestWant{}
	}
	d.expected[q] = trailRequestWant{method: "POST", path: "/mcp", user: "owner", status: 200}
	result, err := d.r.callAs(name, args, identity.Caller{UserID: "owner", Email: trailMarkers[0] + "@example.test", RequestID: q})
	if err != nil {
		d.t.Fatalf("%s: %v", name, err)
	}
	raw, err := result.MarshalJSON()
	if err != nil {
		d.t.Fatal(err)
	}
	if result.IsError() != refused {
		d.t.Fatalf("%s error=%v want %v: %s", name, result.IsError(), refused, raw)
	}
	var envelope struct {
		Content map[string]any `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		d.t.Fatal(err)
	}
	return envelope.Content
}
func (d *trailDriver) pages() {
	d.t.Helper()
	for _, user := range []string{"", "owner"} {
		for _, path := range []string{"/", "/tools", "/about", "/unknown"} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				r, err := http.NewRequest(method, d.r.base+path+"?ignored=1", nil)
				if err != nil {
					d.t.Fatal(err)
				}
				if user != "" {
					r.Header.Set("X-User-Id", user)
					r.Header.Set("X-User-Email", trailMarkers[0]+"@example.test")
				}
				r.Header.Set("X-Request-Id", fmt.Sprintf("page-%s-%s-%s", user, method, path))
				response, err := d.r.http.Do(r)
				if err != nil {
					d.t.Fatal(err)
				}
				if d.expected == nil {
					d.expected = map[string]trailRequestWant{}
				}
				d.expected[r.Header.Get("X-Request-Id")] = trailRequestWant{method: method, path: path, user: user, status: response.StatusCode}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		}
	}
}
func trailObject(t *testing.T, value any) map[string]any {
	t.Helper()
	v, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %#v", value)
	}
	return v
}
func (d *trailDriver) exercise() {
	// Every tool is first refused through a value its declared input cannot accept.
	for _, entry := range tools.Catalog() {
		bad := map[string]any{"unexpected": true}
		if entry.Name == "post" || entry.Name == "annotate" || entry.Name == "reverse" {
			bad["payee"] = trailMarkers[1]
			bad["narration"] = trailMarkers[2]
			bad["metadata"] = map[string]string{"word": trailMarkers[3]}
		}
		if entry.Name == "add_note" {
			bad["text"] = trailMarkers[3]
		}
		if entry.Name == "journal" {
			bad["payee"] = trailMarkers[1]
		}
		d.call(entry.Name, bad, true)
	}
	d.call("book", map[string]any{}, false)
	d.call("accounts", map[string]any{}, false)
	d.call("set_book", map[string]any{"fiscal_year_start": "04-01"}, false)
	for _, name := range []string{"Assets:Bank", "Expenses:Office", "Assets:Unused"} {
		d.call("open_account", map[string]any{"name": name, "date": "2026-10-01"}, false)
	}
	d.call("close_account", map[string]any{"name": "Assets:Unused", "date": "2026-10-02"}, false)
	d.call("account", map[string]any{"name": "Assets:Bank"}, false)
	post := map[string]any{"date": "2026-10-03", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "-12.00"}, map[string]any{"account": "Expenses:Office"}}, "payee": trailMarkers[1], "narration": trailMarkers[2], "metadata": map[string]string{"chosen": trailMarkers[3]}, "key": "entry-key"}
	output := d.call("post", post, false)
	txn := trailObject(d.t, output["transaction"])["id"].(string)
	d.call("post", post, false)
	d.call("transaction", map[string]any{"transaction": txn}, false)
	d.call("annotate", map[string]any{"transaction": txn, "payee": trailMarkers[1], "narration": trailMarkers[2], "metadata": map[string]string{"second": trailMarkers[3]}, "flag": "cleared"}, false)
	d.call("history", map[string]any{"transaction": txn}, false)
	reverse := map[string]any{"transaction": txn, "date": "2026-10-04", "payee": trailMarkers[1], "narration": trailMarkers[2], "metadata": map[string]string{"reverse": trailMarkers[3]}}
	d.call("reverse", reverse, false)
	d.call("reverse", reverse, false)
	assertion := d.call("assert_balance", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "amount": "1.00"}, false)
	assertionID := assertion["id"].(string)
	d.call("check", map[string]any{}, false)
	d.call("retract_assertion", map[string]any{"assertion": assertionID}, false)
	d.call("retract_assertion", map[string]any{"assertion": assertionID}, false)
	note := d.call("add_note", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "text": trailMarkers[3]}, false)
	noteID := note["id"].(string)
	d.call("retract_note", map[string]any{"note": noteID}, false)
	d.call("retract_note", map[string]any{"note": noteID}, false)
	document := d.call("add_document", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "path": "receipt.pdf", "transaction": txn}, false)
	documentID := document["id"].(string)
	d.call("retract_document", map[string]any{"document": documentID}, false)
	d.call("retract_document", map[string]any{"document": documentID}, false)
	for _, path := range []string{"http-fail.pdf", "show-fail.pdf", "show-http-fail.pdf"} {
		d.call("add_document", map[string]any{"account": "Assets:Bank", "date": "2026-10-05", "path": path}, true)
	}
	d.call("journal", map[string]any{"payee": trailMarkers[1]}, false)
	d.call("register", map[string]any{"account": "Assets:Bank"}, false)
	d.call("balances", map[string]any{}, false)
	d.call("balance_sheet", map[string]any{}, false)
	d.call("income_statement", map[string]any{"from": "2026-10-01", "to": "2026-10-26"}, false)
	d.pages()
}
func trailMutation(name string) bool {
	for _, n := range []string{trail.EventTransactionPosted, trail.EventAccountOpened, trail.EventAccountClosed, trail.EventTransactionAnnotated, trail.EventTransactionReversed, trail.EventAssertionAdded, trail.EventAssertionRetracted, trail.EventNoteAdded, trail.EventNoteRetracted, trail.EventDocumentAdded, trail.EventDocumentRetracted, trail.EventBookChanged} {
		if n == name {
			return true
		}
	}
	return false
}
func trailVerifyRequests(t *testing.T, all []telemetry.Event, expected map[string]trailRequestWant) {
	t.Helper()
	byID := map[string][]telemetry.Event{}
	for _, e := range all {
		if e.Name == "service.started" || e.Name == "service.stopping" {
			continue
		}
		byID[e.RequestID] = append(byID[e.RequestID], e)
	}
	if len(byID) == 0 {
		t.Fatal("no requests")
	}
	if len(byID) != len(expected) {
		t.Fatalf("requests count %d want %d", len(byID), len(expected))
	}
	for id, es := range byID {
		want, ok := expected[id]
		if !ok {
			t.Fatal("unexpected request", id)
		}
		if es[0].Name != "request.started" || es[len(es)-1].Name != "request.finished" {
			t.Fatalf("request %s is not bracketed: %+v", id, es)
		}
		starts, finishes, mutations, called := 0, 0, 0, -1
		mutation := -1
		for i, e := range es {
			if e.RequestID != id || e.User != es[0].User {
				t.Fatal("request identity", e)
			}
			switch e.Name {
			case "request.started":
				starts++
				if len(e.Attrs) != 2 || e.Attrs["method"] != want.method || e.Attrs["path"] != want.path || e.User != want.user {
					t.Fatal(e)
				}
				if strings.Contains(fmt.Sprint(e.Attrs["path"]), "?") {
					t.Fatal("query in path")
				}
			case "request.finished":
				finishes++
				if fmt.Sprint(e.Attrs["status"]) != fmt.Sprint(want.status) {
					t.Fatal(e)
				}
			case "tool.called":
				called = i
			}
			if trailMutation(e.Name) {
				mutations++
				mutation = i
			}
		}
		if starts != 1 || finishes != 1 || mutations > 1 || mutation >= 0 && (called < mutation) {
			t.Fatalf("request order %s: %+v", id, es)
		}
	}
}
func trailVerifyBus(t *testing.T, we []telemetry.Event, ee []events.Event) {
	t.Helper()
	matched := map[int]bool{}
	for _, e := range ee {
		found := -1
		for i, w := range we {
			if w.RequestID != e.RequestID || w.User != e.User {
				continue
			}
			want := trail.EventTransactionPosted
			if e.Attrs["reverses"] != "" {
				want = trail.EventTransactionReversed
			}
			if w.Name != want {
				continue
			}
			if found != -1 {
				t.Fatal("duplicate trail match")
			}
			found = i
			if want == trail.EventTransactionPosted {
				if !reflect.DeepEqual(w.Attrs, telemetry.Attrs(e.Attrs)) {
					t.Fatal("bus/trail attrs differ")
				}
			} else {
				for _, k := range []string{"transaction", "reverses", "date"} {
					if w.Attrs[k] != e.Attrs[k] {
						t.Fatal("reversal mismatch")
					}
				}
			}
		}
		if found < 0 || matched[found] {
			t.Fatal("missing or duplicate match")
		}
		matched[found] = true
		called := -1
		for i, w := range we {
			if w.RequestID == e.RequestID && w.Name == "tool.called" {
				called = i
			}
		}
		if called < found {
			t.Fatal("mutation follows tool.called")
		}
	}
	for i, w := range we {
		if (w.Name == trail.EventTransactionPosted || w.Name == trail.EventTransactionReversed) && !matched[i] {
			t.Fatal("unmatched mutation", w)
		}
	}
}

// R-7KXR-0QIA R-7M5N-EI8Z R-OKZX-ES5E R-ONFQ-6BMS R-7S95-BCYG R-OPVI-XV46 R-OR3F-BMUV R-OSBB-PELK
func TestTrailRunAndSecrecy(t *testing.T) {
	path, fake := trailServices(t)
	r := startIntegration(t, integrationOptions{path: path})
	d := trailDriver{t: t, r: r, calls: map[string]string{}}
	d.exercise()
	if code := r.stop(); code != 0 {
		t.Fatal(code, r.err.String())
	}
	we, ee := r.capture.Events(), r.bus.Events()
	if len(ee) != 2 {
		t.Fatal("post and reversal bus count", len(ee))
	}
	allowed := map[string]bool{"service.started": true, "service.stopping": true, "request.started": true, "request.finished": true, "tool.called": true, "event.lost": true, "sibling.called": true}
	for _, e := range we {
		if !allowed[e.Name] && !trailMutation(e.Name) {
			t.Fatal("unexpected event", e.Name)
		}
		if e.Name == "sibling.called" && e.Attrs["target"] != "artifacts" {
			t.Fatal(e)
		}
	}
	trailVerifyRequests(t, we, d.expected)
	trailVerifyBus(t, we, ee)
	requests := fake.snapshot()
	siblings := []telemetry.Event{}
	for _, e := range we {
		if e.Name == "sibling.called" {
			siblings = append(siblings, e)
		}
	}
	if len(siblings) != len(requests) || len(requests) != 7 {
		t.Fatalf("siblings %d fake %d", len(siblings), len(requests))
	}
	for i, req := range requests {
		e := siblings[i]
		if e.Attrs["method"] != req.Method || e.Attrs["path"] != req.Path || e.RequestID != req.ID || e.User != req.User || d.calls[req.ID] != "add_document" {
			t.Fatalf("sibling mismatch %+v %+v", e, req)
		}
	}
	wc, err := json.Marshal(we)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := json.Marshal(ee)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range trailMarkers {
		for _, data := range []string{string(wc), string(ec), r.out.String(), r.err.String()} {
			if strings.Contains(strings.ToUpper(data), marker) {
				t.Fatal("secret leaked", marker, data)
			}
		}
	}
}

type trailReject struct {
	mu       sync.Mutex
	received []events.Event
}

func (s *trailReject) Deliver(_ context.Context, e events.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, e)
	return fmt.Errorf("permanently refused: %w", events.ErrRejected)
}
func (s *trailReject) snapshot() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]events.Event(nil), s.received...)
}

type trailFailure struct{ entered chan struct{} }

func (s trailFailure) Deliver(ctx context.Context, _ events.Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return fmt.Errorf("offline: %w", ctx.Err())
}
func trailSimple(t *testing.T, r *integrationRun) {
	t.Helper()
	d := trailDriver{t: t, r: r, calls: map[string]string{}}
	for _, name := range []string{"Assets:Bank", "Expenses:Office"} {
		d.call("open_account", map[string]any{"name": name, "date": "2026-10-01"}, false)
	}
	out := d.call("post", map[string]any{"date": "2026-10-02", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "-1"}, map[string]any{"account": "Expenses:Office"}}, "payee": trailMarkers[1], "narration": trailMarkers[2], "metadata": map[string]string{"word": trailMarkers[3]}}, false)
	id := trailObject(t, out["transaction"])["id"].(string)
	d.call("reverse", map[string]any{"transaction": id, "date": "2026-10-03", "payee": trailMarkers[1], "narration": trailMarkers[2], "metadata": map[string]string{"word": trailMarkers[3]}}, false)
}

// R-OONM-K3DH
func TestTrailRejectedBus(t *testing.T) {
	sink := &trailReject{}
	r := startIntegration(t, integrationOptions{bus: sink})
	trailSimple(t, r)
	r.stop()
	we, ee := r.capture.Events(), sink.snapshot()
	if len(ee) != 2 {
		t.Fatal(len(ee))
	}
	for _, e := range ee {
		loss, change := 0, 0
		for _, w := range we {
			if w.RequestID != e.RequestID {
				continue
			}
			if w.Name == "event.lost" {
				loss++
				if w.Attrs["event"] != e.ID || w.Attrs["cause"] != e.Cause || w.User != e.User {
					t.Fatal(w)
				}
			}
			if trailMutation(w.Name) {
				change++
			}
		}
		if loss != 1 || change != 1 {
			t.Fatalf("loss/change %d %d", loss, change)
		}
	}
}

// R-7PTC-JTH2
func TestTrailSurvivesBusFailure(t *testing.T) {
	path, _ := trailServices(t)
	a := startIntegration(t, integrationOptions{path: path, env: map[string]string{"DRAIN_SECONDS": "1"}})
	trailSimple(t, a)
	a.stop()
	f := trailFailure{make(chan struct{}, 1)}
	b := startIntegration(t, integrationOptions{path: path, bus: f, env: map[string]string{"DRAIN_SECONDS": "1"}})
	trailSimple(t, b)
	select {
	case <-f.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("bus never called")
	}
	b.stop()
	selectChanges := func(es []telemetry.Event) []telemetry.Event {
		out := []telemetry.Event{}
		for _, e := range es {
			if trailMutation(e.Name) {
				out = append(out, e)
			}
		}
		return out
	}
	if !reflect.DeepEqual(selectChanges(a.capture.Events()), selectChanges(b.capture.Events())) {
		t.Fatalf("bus failure changed trail: %+v %+v", selectChanges(a.capture.Events()), selectChanges(b.capture.Events()))
	}
}

// R-7TH1-P4P5
func TestTrailInjectedTimeAndRandom(t *testing.T) {
	r := startIntegration(t, integrationOptions{random: integrationConstant(0x5a)})
	d := trailDriver{t: t, r: r, calls: map[string]string{}}
	for _, name := range []string{"Assets:Bank", "Expenses:Office"} {
		d.call("open_account", map[string]any{"name": name, "date": "2026-10-01"}, false)
	}
	d.call("post", map[string]any{"date": "2026-10-02", "postings": []any{map[string]any{"account": "Assets:Bank", "amount": "1"}, map[string]any{"account": "Expenses:Office"}}, "payee": trailMarkers[1], "narration": trailMarkers[2], "metadata": map[string]string{"word": trailMarkers[3]}}, false)
	result, err := r.callAs("book", map[string]any{}, identity.Caller{UserID: "owner", Email: trailMarkers[0] + "@example.test"})
	if err != nil || result.IsError() {
		t.Fatal(result, err)
	}
	r.stop()
	want := integrationTime.UTC().Truncate(time.Microsecond)
	minted := 0
	for _, e := range r.capture.Events() {
		if !e.Time.Equal(want) {
			t.Fatal(e.Time, want)
		}
		if e.Name == "request.started" && e.RequestID == strings.Repeat("5a", 16) {
			minted++
		}
	}
	if minted != 1 {
		t.Fatal("minted request count", minted)
	}
	es := r.bus.Events()
	if len(es) != 1 || es[0].ID != "evt_5a5a5a5a5a5a5a5a" || !es[0].Time.Equal(want) {
		t.Fatal(es)
	}
}

// R-EKDI-KOM8
func TestTrailNoAcceptedEvents(t *testing.T) {
	r := startIntegration(t, integrationOptions{})
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE"} {
		for _, body := range []string{"", `{}`, `{"id":"evt_0123456789abcdef","name":"transaction.posted","attrs":{}}`, "malformed"} {
			for _, user := range []string{"", "owner"} {
				req, err := http.NewRequest(method, r.base+"/events?x=1", bytes.NewBufferString(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if user != "" {
					req.Header.Set("X-User-Id", user)
					req.Header.Set("X-User-Email", trailMarkers[0]+"@example.test")
				}
				response, err := r.http.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				reference := httptest.NewRecorder()
				copyRequest := httptest.NewRequest(method, "/events?x=1", bytes.NewBufferString(body))
				copyRequest.Header = req.Header.Clone()
				events.DeliveryHandler(nil).ServeHTTP(reference, copyRequest)
				want := reference.Body.Bytes()
				if method == "HEAD" {
					want = nil
				}
				if response.StatusCode != reference.Code || response.Header.Get("Content-Type") != reference.Header().Get("Content-Type") || !bytes.Equal(raw, want) {
					t.Fatalf("%s %q: %d %q want %d %q", method, body, response.StatusCode, raw, reference.Code, want)
				}
			}
		}
	}
	r.stop()
}

// R-PYS6-ERES
func TestTrailDescriptionMatchesEmbeddedManifest(t *testing.T) {
	b, err := fs.ReadFile(ledger.Etc(), "manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "description = \""+pages.Description+"\"\n") {
		t.Fatal("description differs from deployment asset")
	}
}
