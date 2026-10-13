package web_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/web"
)

// Each read has a distinct base-96 suffix, and every output byte is >= 0xa0.
type secrecyBytes struct{ n uint64 }

func (r *secrecyBytes) Read(p []byte) (int, error) {
	r.n++
	n := r.n
	for i := len(p) - 1; i >= 0; i-- {
		p[i] = 0xa0 + byte(n%96)
		n /= 96
	}
	return len(p), nil
}

type secrecyFake struct {
	mu         sync.Mutex
	rows       []map[string]any
	mode, echo string
	hold       <-chan struct{}
	arrived    chan struct{}
	written    chan struct{}
	canceled   chan struct{}
}

func (f *secrecyFake) set(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *secrecyFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	mode, echo, rows, hold, arrived := f.mode, f.echo, f.rows, f.hold, f.arrived
	written, canceled := f.written, f.canceled
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if mode == "record_failure" && strings.HasSuffix(r.URL.Path, "/record") || mode == "rows_failure" && strings.HasSuffix(r.URL.Path, "/rows") {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, echo)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/rows") {
		if arrived != nil {
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if mode == "malformed" {
			_, _ = fmt.Fprintln(w, echo)
			return
		}
		for _, row := range rows {
			_ = json.NewEncoder(w).Encode(row)
		}
		if written != nil {
			w.(http.Flusher).Flush()
			close(written)
			<-r.Context().Done()
			close(canceled)
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_fixture", "scraper": "fmcsa", "status": "done", "started": "2026-01-01T00:00:00Z", "finished": "2026-01-01T01:00:00Z", "rows": len(rows), "new": len(rows), "changed": 0, "unchanged": 0, "gone": 0})
}

// Fixtures implement the marker vocabulary exercised by the run test.
func secrecyMarkers(t *testing.T) ([]string, string) {
	t.Helper()

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	markers := make([]string, 10)
	for i := range markers {
		seed := sha256.Sum256([]byte(fmt.Sprintf("marker-fixture-%d", i)))
		var b [24]byte
		for j := range b {
			b[j] = alphabet[int(seed[j])%len(alphabet)]
		}
		// Spacing the forced letters eight apart protects every sliding window.
		for j := 0; j < len(b); j += 8 {
			b[j] = 'G' + seed[j+1]%20
		}
		markers[i] = string(b[:])
	}
	phone := "8"
	phoneSeed := sha256.Sum256([]byte("phone-fixture"))
	for i := 1; i < 10; i++ {
		phone += string('0' + phoneSeed[i]%10)
	}
	for i, m := range markers {
		if len(m) != 24 {
			t.Fatal("marker width")
		}
		for _, b := range m {
			if !strings.ContainsRune(alphabet, b) {
				t.Fatal("marker alphabet")
			}
		}
		for j := 0; j <= len(m)-8; j++ {
			if !strings.ContainsAny(m[j:j+8], "GHIJKLMNOPQRSTUVWXYZ") {
				t.Fatal("marker window")
			}
		}
		for k, n := range markers {
			if i != k && strings.Contains(m, n) {
				t.Fatal("overlapping markers")
			}
		}
	}
	return markers, phone
}
func secrecyAbsent(t *testing.T, where string, value any, markers []string) {
	t.Helper()
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
	}

	checkText := func(text string) {
		lower := strings.ToLower(text)
		for i, m := range markers {
			if strings.Contains(lower, strings.ToLower(m)) {
				t.Fatalf("%s leaked marker %d", where, i)
			}
		}
	}
	// Search the serialized event and every decoded field, including escaped JSON strings.
	checkText(string(raw))
	var fields any
	if json.Unmarshal(raw, &fields) == nil {
		var checkField func(any)
		checkField = func(v any) {
			switch f := v.(type) {
			case string:
				checkText(f)
			case []any:
				for _, x := range f {
					checkField(x)
				}
			case map[string]any:
				for k, x := range f {
					checkText(k)
					checkField(x)
				}
			}
		}
		checkField(fields)
	}
}

// R-MQOB-QYCO R-MRW8-4Q3D R-MT44-IHU2
func TestHandlerSecrecyWhileDatabaseFails(t *testing.T) {
	t.Setenv(services.Variable, "")
	markers, phone := secrecyMarkers(t)
	forbidden := append(append([]string{}, markers...), phone)
	row := map[string]any{"scraper": "fmcsa", "run": "run_fixture", "source_id": "101", "collected": "2026-01-01T00:00:00Z", "digest": "fixture-digest", "name": markers[0], "names": []string{markers[1], markers[2]}, "address": map[string]any{"street": markers[3], "city": markers[4], "state": "MN", "zip": "55072", "county": "115"}, "people": []map[string]string{{"name": markers[5], "role": markers[6]}}, "facts": map[string]any{"phone": phone, "email": markers[7] + "@" + markers[8] + ".example", "power_units": 3, "drivers": 4, "class": []string{"Authorized For Hire"}, "cargo": []string{"General Freight"}, "mcs150_date": "2025-01-01"}, "raw": map[string]any{"private": markers[9]}}
	f := &secrecyFake{rows: []map[string]any{row}, echo: strings.Join(forbidden, " ")}
	socketDir, err := os.MkdirTemp("", "cand-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "scrapes")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	sibling := &http.Server{Handler: f, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = sibling.Serve(ln) }()
	defer func() { _ = sibling.Close() }()
	now := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC) }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: candidates.Migrations(), Now: now, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	st := store.New(d, store.Config{Now: now, Rand: &secrecyBytes{}})
	tc, ec := &telemetry.Capture{}, &events.Capture{}
	sleep := func(ctx context.Context, _ time.Duration) { <-ctx.Done() }
	writer := telemetry.New(telemetry.Config{Service: "candidates", Sink: tc, Now: now, Rand: &secrecyBytes{}, Sleep: sleep, Stderr: io.Discard})
	writer.Ready()
	emitter := events.New(events.Config{Service: "candidates", Sink: ec, Now: now, Rand: &secrecyBytes{}, Sleep: sleep, Telemetry: writer, Emits: trail.Emits(), Stderr: io.Discard})
	client := scrapes.NewClient(scrapes.ClientConfig{Service: services.Entry{Name: "scrapes", Socket: socket}, Telemetry: writer})
	engine := passes.New(passes.Config{Store: st, Sources: sources.New(fmcsa.New()), Scrapes: client, Telemetry: writer, Events: emitter})
	defer func() {
		engine.Drain(context.Background())
		emitter.Shutdown(context.Background())
		writer.Shutdown(context.Background(), "fixture-stop")
	}()
	cfg := web.Config{Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: writer}), Store: st, Passes: engine, Telemetry: writer, Events: emitter, Now: now}
	handler := web.Handler(cfg)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	mcpClient := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Timeout: 5 * time.Second}})
	caller := identity.Caller{UserID: "reader", Email: "reader@example.test", RequestID: "secrecy-request"}
	refusals := []mcp.Result{}
	call := func(name string, args any, wantError bool) map[string]json.RawMessage {
		t.Helper()
		a, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		result, err := mcpClient.CallTool(context.Background(), caller, name, a)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError() != wantError {
			t.Fatalf("%s error=%v want=%v", name, result.IsError(), wantError)
		}
		if result.IsError() {
			refusals = append(refusals, result)
			return nil
		}
		b, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]json.RawMessage
		_ = json.Unmarshal(b, &envelope)
		var value map[string]json.RawMessage
		if err = json.Unmarshal(envelope["structuredContent"], &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	text := func(v map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(v[key], &s)
		return s
	}
	wait := func(id, reason string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			p := call("pass", map[string]any{"pass": id}, false)
			if text(p, "status") != store.StatusRunning {
				if reason == "" && text(p, "status") != store.StatusDone || reason != "" && (text(p, "status") != store.StatusFailed || text(p, "reason") != reason) {
					t.Fatal("baseline pass ending mismatch")
				}
				return
			}
			if ctx.Err() != nil {
				t.Fatal("baseline pass did not finish")
			}
		}
	}
	first := call("rebuild", map[string]any{}, false)
	passID := text(first, "id")
	wait(passID, "")
	named := call("candidates", map[string]any{"name": markers[0]}, false)
	call("candidates", map[string]any{"phone": phone}, false)
	var list []map[string]json.RawMessage
	if err = json.Unmarshal(named["candidates"], &list); err != nil || len(list) != 1 {
		t.Fatal("baseline candidate missing")
	}
	id := text(list[0], "id")
	args := map[string]any{"shortlist": map[string]any{}, "candidate": map[string]any{"candidate": id}, "passes": map[string]any{}, "pass": map[string]any{"pass": passID}, "rebuild": map[string]any{}, "decide": map[string]any{"kind": store.KindInclude, "candidate": id, "why": markers[9]}}
	for _, entry := range tools.Catalog() {
		if entry.Name != "rebuild" && entry.Name != "candidates" {
			call(entry.Name, args[entry.Name], false)
		}
	}
	for _, mode := range []string{"rows_failure", "malformed"} {
		f.set(mode)
		failed := call("rebuild", map[string]any{}, false)
		wait(text(failed, "id"), store.ReasonScrapes)
	}
	f.set("record_failure")
	recordFailure := events.Event{ID: "evt_abababababababab", Time: now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_failure", "status": "done"}, Seq: 1, Received: now()}
	rb, _ := json.Marshal(recordFailure)
	var rv map[string]any
	_ = json.Unmarshal(rb, &rv)
	rv["attempt"] = 1
	rb, _ = json.Marshal(rv)
	recordAnswer := request(handler, "POST", "/events", string(rb), "")
	if recordAnswer.Code != 200 {
		t.Fatal("baseline failed record delivery refused")
	}
	secrecyAbsent(t, "baseline failed record answer", recordAnswer.Body.Bytes(), forbidden)
	f.set("")

	// Hold before the first row, then stream a complete batch and keep the response
	// open: the sibling's cancellation proves the failed Attach rejected that batch.
	hold, arrived, written, canceled := make(chan struct{}), make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	rows := make([]map[string]any, passes.BatchSize)
	for i := range rows {
		r := make(map[string]any, len(row))
		for k, v := range row {
			r[k] = v
		}
		r["source_id"] = fmt.Sprintf("%d", i+101)
		rows[i] = r
	}
	f.mu.Lock()
	f.rows = rows
	f.hold = hold
	f.arrived = arrived
	f.written = written
	f.canceled = canceled
	f.mu.Unlock()
	caller.RequestID = "refused-batch-request"
	active := call("rebuild", map[string]any{}, false)
	activeID := text(active, "id")
	caller.RequestID = "failing-tool-request"
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("held rows request missing")
	}
	d.SetFailing(true)
	close(hold)
	for _, ch := range []chan struct{}{written, canceled} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("failed batch was not sent and rejected")
		}
	}
	for _, entry := range tools.Catalog() {
		if entry.Name != "candidates" {
			call(entry.Name, args[entry.Name], true)
		}
	}
	call("candidates", map[string]any{"name": markers[0]}, true)
	call("candidates", map[string]any{"phone": phone}, true)
	for _, path := range []string{"/", "/candidates/" + id, "/passes/" + passID, "/tools", "/about", "/absent"} {
		response := request(handler, "GET", path, "", "reader")
		secrecyAbsent(t, "failing page", response.Body.Bytes(), forbidden)
	}
	answers := [][]byte{}
	for i, status := range []string{"done", "failed"} {
		e := events.Event{ID: fmt.Sprintf("evt_%016x", i+101), Time: now(), Service: "scrapes", Name: "scrape.finished", Attrs: events.Attrs{"scraper": "fmcsa", "run": "run_delivery", "status": status}, Seq: 1, Received: now()}
		b, _ := json.Marshal(e)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		v["attempt"] = 1
		b, _ = json.Marshal(v)
		response := request(handler, "POST", "/events", string(b), "")
		if status == "done" && response.Code != 500 || status == "failed" && response.Code != 200 {
			t.Fatalf("failing delivery status %s: %d", status, response.Code)
		}
		answers = append(answers, response.Body.Bytes())
	}
	for _, method := range []string{"GET", "POST"} {
		response := request(handler, method, "/declarations", "", "")
		answers = append(answers, response.Body.Bytes())
	}
	engine.Drain(context.Background())
	emitter.Shutdown(context.Background())
	writer.Shutdown(context.Background(), "fixture-stop")
	trailPasses, busPasses, activeRequests, activeReads := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	stamp := now().UTC().Truncate(time.Microsecond)
	for _, e := range tc.Events() {
		secrecyAbsent(t, "failing trail event", e, forbidden)
		if !e.Time.Equal(stamp) {
			t.Fatal("handler trail clock mismatch")
		}
		if e.Name == "pass.started" || e.Name == "pass.finished" {
			pid, _ := e.Attrs["pass"].(string)
			trailPasses[e.Name+"|"+pid]++
			if pid == activeID && e.RequestID != "refused-batch-request" {
				t.Fatal("refused batch pass trail request id")
			}
		}
		if e.RequestID == "refused-batch-request" {
			activeRequests[e.Name]++
			if e.Name == "sibling.called" {
				path, _ := e.Attrs["path"].(string)
				activeReads[path]++
			}
		}
	}
	for _, e := range ec.Events() {
		secrecyAbsent(t, "failing bus event", e, forbidden)
		if !e.Time.Equal(stamp) {
			t.Fatal("handler bus clock mismatch")
		}
		pid, _ := e.Attrs["pass"].(string)
		busPasses[e.Name+"|"+pid]++
		if pid == activeID && e.RequestID != "refused-batch-request" {
			t.Fatal("refused batch bus request id")
		}
	}
	for _, expected := range []struct{ name, id string }{{"pass.started", passID}, {"pass.finished", passID}, {"pass.started", activeID}} {
		key := expected.name + "|" + expected.id
		if trailPasses[key] != 1 || busPasses[key] != 1 {
			t.Fatalf("handler pass capture missing %s: trail=%d bus=%d", key, trailPasses[key], busPasses[key])
		}
	}
	for _, name := range []string{"request.started", "request.finished", "tool.called"} {
		if activeRequests[name] != 1 {
			t.Fatalf("refused batch request capture %s=%d", name, activeRequests[name])
		}
	}
	for _, path := range []string{"/scrapers/fmcsa/current", "/runs/run_fixture/rows"} {
		if activeReads[path] != 1 {
			t.Fatalf("refused batch sibling capture %s=%d", path, activeReads[path])
		}
	}

	for _, r := range refusals {
		secrecyAbsent(t, "failing refused tool", r, forbidden)
	}
	for _, answer := range answers {
		secrecyAbsent(t, "failing event answer", answer, forbidden)
	}
}
