package trail_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/tools"
)

type trailBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *trailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *trailBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type trailRandom struct {
	n     byte
	fixed bool
}

func (r *trailRandom) Read(p []byte) (int, error) {
	for i := range p {
		if r.fixed {
			p[i] = 0x5a
		} else {
			r.n++
			p[i] = r.n
		}
	}
	return len(p), nil
}
func trailTime() time.Time {
	return time.Date(2026, 8, 3, 5, 6, 7, 123456789, time.FixedZone("fixture", -3600))
}

type trailSource struct {
	mu       sync.Mutex
	mode     string
	requests int
	record   map[string]string
	echo     string
	gate     <-chan struct{}
	closed   func()
	now      func() time.Time
}

func (s *trailSource) set(mode string) { s.mu.Lock(); defer s.mu.Unlock(); s.mode = mode }
func (s *trailSource) count() int      { s.mu.Lock(); defer s.mu.Unlock(); return s.requests }
func (s *trailSource) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.requests++
	mode := s.mode
	rec := map[string]string{}
	for k, v := range s.record {
		rec[k] = v
	}
	echo := s.echo
	gate, closed := s.gate, s.closed
	s.mu.Unlock()
	if r.URL.Query().Get("$select") == "" && gate != nil {
		select {
		case <-gate:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	var body []byte
	status := 200
	if r.URL.Query().Get("$select") != "" {
		body = []byte(`[{"count":"1"}]`)
	} else {
		switch mode {
		case "error":
			status = 500
			body = []byte(echo)
		case "invalid":
			body = []byte("invalid " + echo)
		default:
			rec["dot_number"] = "1234"
			rec["phy_state"] = "MN"
			body, _ = json.Marshal([]any{rec})
		}
	}
	bodyReader := io.NopCloser(bytes.NewReader(body))
	if r.URL.Query().Get("$select") == "" && closed != nil {
		bodyReader = &trailBody{ReadCloser: bodyReader, closed: closed}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: bodyReader, Request: r}, nil
}

type trailBody struct {
	io.ReadCloser
	closed func()
}

func (b *trailBody) Close() error { e := b.ReadCloser.Close(); b.closed(); return e }

type trailRun struct {
	t         *testing.T
	dir, base string
	client    *mcp.Client
	http      *http.Client
	sink      *telemetry.Capture
	bus       *events.Capture
	out, err  *trailBuffer
	cancel    context.CancelFunc
	done      chan int
	stopped   bool
}

func startTrail(t *testing.T, dir string, src *trailSource, fixed bool) *trailRun {
	t.Helper()
	t.Setenv(services.Variable, "")
	if dir == "" {
		dir = t.TempDir()
	}
	pressure := filepath.Join(dir, "pressure")
	if e := os.WriteFile(pressure, []byte("some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"), 0600); e != nil {
		t.Fatal(e)
	}
	f := &trailRun{t: t, dir: dir, sink: &telemetry.Capture{}, bus: &events.Capture{}, out: &trailBuffer{}, err: &trailBuffer{}, done: make(chan int, 1)}
	sd, e := os.MkdirTemp("", "scrapes-trail-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sd) })
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sd, "ready"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	env := map[string]string{"LISTEN_PID": "77", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), "FMCSA_STATES": "MN", "SCRAPE_KEEP_RUNS": "20"}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	now := src.now
	if now == nil {
		now = trailTime
	}
	p := cli.Process{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Pid: 77, Stdout: f.out, Stderr: f.err, Version: "trail-fixture", Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Now: now, Sleep: func(context.Context, time.Duration) {}, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: &trailRandom{fixed: fixed}, Dir: dir, Pressure: pressure, Sink: f.sink, EventSink: f.bus, Banner: func(page.User) page.Banner { return page.Banner{Service: "fixture"} }, HTTP: &http.Client{Transport: src}}
	go func() { f.done <- cli.Run(ctx, p) }()
	t.Cleanup(func() {
		if !f.stopped {
			f.stop()
		}
	})
	_ = notify.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 64)
	n, _, e := notify.ReadFromUnix(buf)
	if e != nil || string(buf[:n]) != "READY=1" {
		t.Fatalf("ready: %v %s", e, f.err.String())
	}
	f.base = "http://" + ln.Addr().String()
	f.http = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.base + "/mcp", HTTPClient: f.http, Name: "trail-fixture", Version: "fixture"})
	return f
}
func (f *trailRun) stop() {
	f.t.Helper()
	f.cancel()
	select {
	case code := <-f.done:
		f.stopped = true
		if code != 0 {
			f.t.Fatalf("Run exit %d: %s", code, f.err.String())
		}
	case <-time.After(10 * time.Second):
		f.t.Fatal("Run did not stop")
	}
}
func (f *trailRun) call(c identity.Caller, name string, args any) mcp.Result {
	f.t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		f.t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := f.client.CallTool(ctx, c, name, b)
	if e != nil {
		f.t.Fatal(e)
	}
	return r
}
func payload(t *testing.T, r mcp.Result) json.RawMessage {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	if len(v.Structured) > 0 {
		return v.Structured
	}
	if len(v.Content) > 0 {
		return json.RawMessage(v.Content[0].Text)
	}
	t.Fatal("empty result")
	return nil
}

var trailCaller = identity.Caller{UserID: "trail-user", Email: "fixture@example.test", RequestID: "tool-request"}

func (f *trailRun) scrape() string {
	f.t.Helper()
	r := f.call(trailCaller, "scrape", tools.ScrapeArgs{Scraper: "fmcsa"})
	if r.IsError() {
		f.t.Fatalf("scrape refused: %s", payload(f.t, r))
	}
	var v runs.Record
	if e := json.Unmarshal(payload(f.t, r), &v); e != nil {
		f.t.Fatal(e)
	}
	if v.ID == "" {
		f.t.Fatal("no run id")
	}
	return v.ID
}
func (f *trailRun) await(id, status, reason string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			f.t.Fatal("run did not end")
		default:
		}
		var v runs.Record
		r := f.call(trailCaller, "run", tools.RunArgs{Run: id})
		if r.IsError() {
			f.t.Fatalf("run refused: %s", payload(f.t, r))
		}
		if e := json.Unmarshal(payload(f.t, r), &v); e != nil {
			f.t.Fatal(e)
		}
		if v.Status != "running" {
			if v.Status != status || v.Reason != reason {
				f.t.Fatalf("run status/reason %s/%s, want %s/%s", v.Status, v.Reason, status, reason)
			}
			return
		}
		runtime.Gosched()
	}
}
func (f *trailRun) request(method, path, body, q string, user bool) (int, []byte) {
	f.t.Helper()
	r, e := http.NewRequest(method, f.base+path, strings.NewReader(body))
	if e != nil {
		f.t.Fatal(e)
	}
	if q != "" {
		r.Header.Set("X-Request-Id", q)
	}
	if user {
		r.Header.Set("X-User-Id", trailCaller.UserID)
		r.Header.Set("X-User-Email", trailCaller.Email)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	res, e := f.http.Do(r)
	if e != nil {
		f.t.Fatal(e)
	}
	defer func() { _ = res.Body.Close() }()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	return res.StatusCode, b
}
func (f *trailRun) delivery(name, q string) (int, []byte) {
	f.t.Helper()
	e := events.Event{ID: "evt_1234567890abcdef", Time: trailTime().UTC().Truncate(time.Microsecond), Service: "cron", Name: name, RequestID: q, Attrs: events.Attrs{}, Seq: 1, Received: trailTime().UTC().Truncate(time.Microsecond)}
	b, err := json.Marshal(e)
	if err != nil {
		f.t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		f.t.Fatal(err)
	}
	v["attempt"] = 1
	b, err = json.Marshal(v)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.request("POST", "/events", string(b), q, false)
}
func trailDB(t *testing.T, dir string) (*store.Store, *db.DB) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scrapes.db"), Migrations: app.Migrations(), Now: trailTime})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	return store.New(d, store.Config{Now: trailTime, Rand: &trailRandom{}, Runs: filepath.Join(dir, "runs")}), d
}

// R-HWXU-4EC7 R-HY5Q-I62W R-UB8G-HTUT
func TestRunTrailSourceAndDelivery(t *testing.T) {
	src := &trailSource{}
	f := startTrail(t, "", src, false)
	id := f.scrape()
	f.await(id, "done", "")
	first := src.count()
	ignoredStatus, _ := f.delivery("cron.scrape_bogus.fired", "ignored-delivery")
	deliveryStatus, _ := f.delivery("cron.scrape_fmcsa.fired", "cron-delivery")
	if ignoredStatus != 404 || deliveryStatus != 200 {
		t.Fatalf("delivery status %d/%d", ignoredStatus, deliveryStatus)
	}
	deliveryID := ""
	st, _ := trailDB(t, f.dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		rs, e := st.ListRunsAfter(ctx, "fmcsa", "", "", 20)
		if e != nil {
			t.Fatal(e)
		}
		found := ""
		for _, r := range rs {
			if r.RequestID == "cron-delivery" {
				found = r.ID
			}
		}
		if found != "" {
			f.await(found, "done", "")
			deliveryID = found
			break
		}
		if ctx.Err() != nil {
			t.Fatal("delivery recorded no run")
		}
		runtime.Gosched()
	}
	f.stop()

	counts := map[string]int{}
	pairs := map[string]map[string]int{}
	for _, e := range f.sink.Events() {
		if !trailNameAllowed(e.Name) {
			t.Errorf("unexpected event %s", e.Name)
		}
		if e.Name == "source.called" {
			counts[e.RequestID]++
			wantUser := ""
			if e.RequestID == trailCaller.RequestID {
				wantUser = trailCaller.UserID
			} else if e.RequestID != "cron-delivery" {
				t.Errorf("source request id %s", e.RequestID)
			}
			if e.User != wantUser {
				t.Errorf("source user %q", e.User)
			}
			if len(e.Attrs) != 3 || e.Attrs["scraper"] != "fmcsa" || e.Attrs["status"] != int64(200) || e.Attrs["duration_us"] != int64(0) {
				t.Errorf("source attrs %#v", e.Attrs)
			}
		}
		if e.RequestID == "cron-delivery" || e.RequestID == "ignored-delivery" {
			if e.User != "" {
				t.Errorf("delivery user %q", e.User)
			}
			if pairs[e.RequestID] == nil {
				pairs[e.RequestID] = map[string]int{}
			}
			pairs[e.RequestID][e.Name]++
			wantStatus := deliveryStatus
			if e.RequestID == "ignored-delivery" {
				wantStatus = ignoredStatus
			}
			if (e.Name == "scrape.started" || e.Name == "scrape.collected" || e.Name == "scrape.loaded" || e.Name == "scrape.finished") && e.Attrs["run"] != deliveryID {
				t.Errorf("delivery lifecycle belongs to %v, want %s", e.Attrs["run"], deliveryID)
			}
			if e.Name == "request.finished" && fmt.Sprint(e.Attrs["status"]) != fmt.Sprint(wantStatus) {
				t.Errorf("delivery status %#v", e.Attrs)
			}
			if e.Name != "request.started" && e.Name != "request.finished" && e.Name != "scrape.started" && e.Name != "scrape.collected" && e.Name != "scrape.loaded" && e.Name != "scrape.finished" && e.Name != "source.called" {
				t.Errorf("delivery extra event %s", e.Name)
			}
		}
	}
	if first == 0 || src.count() == first || counts[trailCaller.RequestID] != first || counts["cron-delivery"] != src.count()-first {
		t.Fatalf("source counts %v transport %d/%d", counts, first, src.count())
	}
	for _, q := range []string{"cron-delivery", "ignored-delivery"} {
		if pairs[q]["request.started"] != 1 || pairs[q]["request.finished"] != 1 {
			t.Errorf("request pair %s: %v", q, pairs[q])
		}
	}
	if len(pairs["ignored-delivery"]) != 2 {
		t.Errorf("ignored delivery events %v", pairs["ignored-delivery"])
	}
	if pairs["cron-delivery"]["scrape.started"] != 1 || pairs["cron-delivery"]["scrape.finished"] != 1 {
		t.Errorf("delivery lifecycle %v", pairs["cron-delivery"])
	}
}

// R-TWLN-WKYH
func TestRunTrailUsesInjectedClockAndRandom(t *testing.T) {
	f := startTrail(t, "", &trailSource{}, true)
	id := f.scrape()
	f.await(id, "done", "")
	f.request("GET", "/about", "", "", true)
	f.stop()
	want := trailTime().UTC().Truncate(time.Microsecond)
	minted := map[string]int{}
	for _, e := range f.sink.Events() {
		if !e.Time.Equal(want) || e.Time.Location() != time.UTC {
			t.Errorf("stamp %v want %v", e.Time, want)
		}
		if e.RequestID == strings.Repeat("5a", 16) {
			minted[e.Name]++
		}
	}
	if minted["request.started"] != 1 || minted["request.finished"] != 1 {
		t.Errorf("minted request pair %v", minted)
	}
	es := f.bus.Events()
	if len(es) != 2 {
		t.Fatalf("bus events %d", len(es))
	}
	for i, e := range es {
		if e.ID != "evt_5a5a5a5a5a5a5a5a" || !e.Time.Equal(want) || e.Time.Location() != time.UTC {
			t.Errorf("bus stamp/id %#v", e)
		}
		if e.Name != []string{"scrape.started", "scrape.finished"}[i] {
			t.Errorf("bus lifecycle %s", e.Name)
		}
	}
}

// R-HZDM-VXTL R-I0LJ-9PKA R-I1TF-NHAZ R-I31C-191O
func TestRunSecrecyAcrossSuccessAndFailures(t *testing.T) {
	// A deterministic sequence supplies random-looking markers without process randomness.
	markerRand := uint64(731)
	markers := []string{}
	marker := func(city bool) string {
		b := make([]byte, 24)
		alphabet := "ghijklmnopqrstuvwxyz"
		if !city {
			alphabet += "GHIJKLMNOPQRSTUVWXYZ0123456789"
		}
		for i := range b {
			markerRand = markerRand*6364136223846793005 + 1
			b[i] = alphabet[(markerRand>>32)%uint64(len(alphabet))]
		}
		if city {
			b[0] -= 'a' - 'A'
		}
		v := string(b)
		markers = append(markers, v)
		return v
	}
	record := map[string]string{}
	for _, k := range []string{"legal_name", "dba_name", "phy_street", "company_officer_1", "company_officer_2", "phone"} {
		record[k] = marker(false)
	}
	record["email_address"] = marker(false) + "@" + marker(false)
	record["phy_city"] = marker(true)
	record["dot_number"] = "1234"
	record["phy_state"] = "MN"
	// Validate the fixture rather than silently weaken the secrecy conditions.
	for i, m := range markers {
		if len(m) != 24 {
			t.Fatal("marker length")
		}
		for j := 0; j <= 16; j++ {
			if !strings.ContainsAny(m[j:j+8], "GHIJKLMNOPQRSTUVWXYZghijklmnopqrstuvwxyz") {
				t.Fatal("hex-only marker span")
			}
		}
		for j, n := range markers {
			if i != j && strings.Contains(n, m) {
				t.Fatal("overlapping markers")
			}
		}
	}
	dir := t.TempDir()
	st, d := trailDB(t, dir)
	ctx := context.Background()
	seed, e := st.AddRun(ctx, store.Run{Scraper: "fmcsa", UserID: trailCaller.UserID, RequestID: "prior-request"})
	if e != nil {
		t.Fatal(e)
	}
	src := &trailSource{record: record, echo: strings.Join(markers, " ")}
	if e := os.WriteFile(filepath.Join(dir, "runs"), []byte("fixture obstacle"), 0600); e != nil {
		t.Fatal(e)
	}
	f := startTrail(t, dir, src, false)
	f.await(seed.ID, "failed", "interrupted")
	reject := func(v any) {
		t.Helper()
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		for _, m := range markers {
			if bytes.Contains(b, []byte(m)) {
				t.Errorf("marker leaked in captured/refused value %s", b)
			}
		}
	}
	safeRequest := func(method, path string) (int, []byte) {
		t.Helper()
		s, b := f.request(method, path, "", "read-request", true)
		if s != 200 {
			reject(string(b))
		}
		return s, b
	}
	refused := func(name string, args any) {
		t.Helper()
		r := f.call(trailCaller, name, args)
		if !r.IsError() {
			t.Fatalf("expected refused %s", name)
		}
		reject(r)
	}
	for _, entry := range tools.Catalog() {
		refused(entry.Name, []any{})
	}
	failedStore := f.scrape()
	f.await(failedStore, "failed", "store")
	if src.count() < 2 {
		t.Fatal("store failure run did not request marker records")
	}
	if e := os.Remove(filepath.Join(dir, "runs")); e != nil {
		t.Fatal(e)
	}
	id := f.scrape()
	f.await(id, "done", "")
	answered := map[string]any{"scrapers": tools.NoArgs{}, "runs": tools.RunsArgs{Scraper: "fmcsa"}, "run": tools.RunArgs{Run: id}, "rows": tools.RowsArgs{Run: id, Name: markers[0]}, "row": tools.RowArgs{Run: id, SourceID: "1234"}}
	for name, args := range answered {
		r := f.call(trailCaller, name, args)
		if r.IsError() {
			t.Fatalf("valid %s refused", name)
		}
		if name == "rows" || name == "row" {
			b := payload(t, r)
			if !bytes.Contains(b, []byte(markers[0])) {
				t.Fatalf("%s did not expose fixture marker", name)
			}
		}
	}
	for _, path := range []string{"/", "/about", "/tools", "/runs/" + id, "/runs/" + id + "/rows/1234", "/unknown"} {
		s, b := safeRequest("GET", path)
		want := 200
		if path == "/unknown" {
			want = 404
		}
		if s != want {
			t.Fatalf("page %s status %d want %d", path, s, want)
		}
		if strings.HasSuffix(path, "/rows/1234") && (s != 200 || !bytes.Contains(b, []byte(markers[0]))) {
			t.Fatal("marker row page not served")
		}
	}
	paths := []string{"/scrapers/fmcsa/current", "/runs/" + id + "/record", "/runs/" + id + "/rows"}
	methods := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT"}
	for _, path := range paths {
		for _, method := range methods {
			status, body := safeRequest(method, path)
			want := 405
			if method == "GET" || method == "HEAD" {
				want = 200
			}
			if status != want {
				t.Fatalf("sibling %s %s status %d want %d", method, path, status, want)
			}
			if method == "GET" && strings.HasSuffix(path, "/rows") && !bytes.Contains(body, []byte(markers[0])) {
				t.Fatal("read rows missing fixture marker")
			}
		}
	}
	// Include read refusals for absent runs and scrapers.
	safeRequest("GET", "/scrapers/bogus/current")
	safeRequest("GET", "/runs/run_ffffffffffffffff/record")
	safeRequest("GET", "/runs/run_ffffffffffffffff/rows")
	for _, mode := range []string{"error", "invalid"} {
		src.set(mode)
		failed := f.scrape()
		f.await(failed, "failed", "source")
	}
	src.set("")
	safeDelivery := func(name, q string) { t.Helper(); _, b := f.delivery(name, q); reject(string(b)) }
	safeDelivery("cron.scrape_bogus.fired", "secrecy-ignored")
	safeDelivery("cron.scrape_fmcsa.fired", "secrecy-cron")
	waitctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		rs, e := st.ListRunsAfter(waitctx, "fmcsa", "", "", 20)
		if e != nil {
			t.Fatal(e)
		}
		found := ""
		for _, r := range rs {
			if r.RequestID == "secrecy-cron" {
				found = r.ID
			}
		}
		if found != "" {
			f.await(found, "done", "")
			break
		}
		if waitctx.Err() != nil {
			t.Fatal("delivery run missing")
		}
		runtime.Gosched()
	}
	// The catalog still describes a done run after its row file disappears.
	if e := os.Remove(filepath.Join(dir, "runs", id+".db")); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"run", "rows", "row"} {
		r := f.call(trailCaller, name, answered[name])
		if r.IsError() {
			reject(r)
		} else if name != "run" {
			t.Errorf("missing run file answered %s", name)
		}
	}
	for _, path := range []string{"/runs/" + id, "/runs/" + id + "/rows/1234", "/runs/" + id + "/record", "/runs/" + id + "/rows"} {
		for _, method := range methods {
			safeRequest(method, path)
		}
	}
	if e := d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "DROP TABLE runs")
		return e
	}); e != nil {
		t.Fatal(e)
	}
	// With the catalog unavailable every tool and sibling path is exercised again.
	for _, entry := range tools.Catalog() {
		args := answered[entry.Name]
		if entry.Name == "scrape" {
			args = tools.ScrapeArgs{Scraper: "fmcsa"}
		}
		r := f.call(trailCaller, entry.Name, args)
		if r.IsError() {
			reject(r)
		} else if entry.Name != "scrapers" {
			t.Errorf("broken database answered %s", entry.Name)
		}
	}
	for _, path := range paths {
		for _, method := range methods {
			safeRequest(method, path)
		}
	}
	refused("rows", tools.RowsArgs{Run: id, Name: markers[0]})
	safeDelivery("cron.scrape_bogus.fired", "broken-ignored")
	safeDelivery("cron.scrape_fmcsa.fired", "broken-cron")
	f.stop()
	for _, e := range f.sink.Events() {
		if !trailNameAllowed(e.Name) {
			t.Errorf("unexpected event %s", e.Name)
		}
		reject(e)
		reject(fmt.Sprintf("%#v", e))
	}
	for _, e := range f.bus.Events() {
		reject(e)
		reject(fmt.Sprintf("%#v", e))
	}
	reject(f.out.String())
	reject(f.err.String())
	if f.out.String() != "" {
		t.Errorf("serving stdout was not silent: %q", f.out.String())
	}
}

func trailNameAllowed(name string) bool {
	switch name {
	case "service.started", "service.stopping", "request.started", "request.finished", "tool.called", "event.lost", "source.called", "scrape.started", "scrape.collected", "scrape.loaded", "scrape.finished":
		return true
	}
	return false
}
