package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appkitdb "appkit/db"
	"appkit/server"
	"telemetry/internal/db"
	"telemetry/internal/record"
	telemetrytime "telemetry/internal/telemetry"
)

func TestLoopbackPostStoresWellFormedRecordsVerbatim(t *testing.T) {
	env := newTestEnvironment(t)
	body := `{"records":[` +
		`{"id":"01H00000000000000000000001","time":"2026-08-03T04:05:06Z","correlation_id":"chain-a","service":"alpha","kind":"request","actor":{"owner_email":"owner@example.test","client_id":"client-a"},"op":"widgets.read","params":{ "kept" : true },"outcome":{"status":"ok"}},` +
		`{"id":"01H00000000000000000000002","time":"2026-08-03T04:05:07Z","correlation_id":"chain-b","service":"beta","kind":"outbound","actor":{},"op":"widgets.write","params":{"number": 42},"outcome":{}}]}`
	response := env.post(t, body, "")
	if response.status != http.StatusAccepted || response.body.Stored != 2 || response.body.Rejected != 0 {
		t.Fatalf("POST response = status %d body %+v, want 202 stored=2 rejected=0", response.status, response.body)
	}
	wants := []struct {
		id, op, service, correlationID, params string
	}{
		{"01H00000000000000000000001", "widgets.read", "alpha", "chain-a", `{ "kept" : true }`},
		{"01H00000000000000000000002", "widgets.write", "beta", "chain-b", `{"number": 42}`},
	}
	for _, want := range wants {
		got, found, err := env.store.Get(context.Background(), want.id)
		if err != nil || !found {
			t.Fatalf("Get(%q) = (%+v, %t, %v)", want.id, got, found, err)
		}
		// R-VIUF-3BD6
		if got.ID != want.id || got.Op != want.op || got.Service != want.service || got.CorrelationID != want.correlationID || string(got.Params) != want.params {
			t.Fatalf("stored record = %+v params=%q, want id=%q op=%q service=%q correlation=%q params=%q", got, got.Params, want.id, want.op, want.service, want.correlationID, want.params)
		}
	}
}

func TestMixedBatchRejectsOnlyMalformedRecords(t *testing.T) {
	env := newTestEnvironment(t)
	items := []record.Record{
		testRecord("01H00000000000000000000001"),
		testRecord("01H00000000000000000000002"),
		testRecord("01H00000000000000000000003"),
		testRecord("01H00000000000000000000004"),
		testRecord("01H00000000000000000000005"),
	}
	items[1].Kind = "mystery"
	items[2].CorrelationID = ""
	items[3].Time = "yesterday"
	items[4].Params = json.RawMessage(`["not","an","object"]`)
	encoded, err := json.Marshal(Request{Records: items})
	if err != nil {
		t.Fatal(err)
	}
	response := env.post(t, string(encoded), "")
	stored, _, err := env.store.Search(context.Background(), db.Query{})
	if err != nil {
		t.Fatal(err)
	}
	// R-VK2B-H33V
	if response.status != http.StatusAccepted || response.body.Stored != 1 || response.body.Rejected != 4 || len(stored) != 1 || stored[0].ID != items[0].ID {
		t.Fatalf("mixed batch response=%d %+v stored ids=%v, want only %q", response.status, response.body, recordIDs(stored), items[0].ID)
	}
	for _, rejected := range items[1:] {
		if _, found, getErr := env.store.Get(context.Background(), rejected.ID); getErr != nil || found {
			t.Fatalf("malformed record %q found=%t err=%v", rejected.ID, found, getErr)
		}
		if !strings.Contains(env.logs.String(), rejected.ID) {
			t.Errorf("warn log does not identify rejected record %q: %s", rejected.ID, env.logs.String())
		}
	}
	for _, field := range []string{"kind", "correlation_id", "time", "params"} {
		if !strings.Contains(env.logs.String(), "field="+field) {
			t.Errorf("warn logs do not name failing field %q: %s", field, env.logs.String())
		}
	}
}

func TestMalformedEnvelopeRefusesWholeRequestAndDroppedCount(t *testing.T) {
	env := newTestEnvironment(t)
	cases := []string{
		`not json`,
		`[]`,
		`{}`,
		`{"records":"not an array"}`,
		`{"records":"not an array","dropped":5}`,
	}
	for _, body := range cases {
		response := env.post(t, body, "")
		if response.status != http.StatusBadRequest {
			t.Errorf("POST %q status = %d, want 400", body, response.status)
		}
	}
	stored, _, err := env.store.Search(context.Background(), db.Query{})
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := env.store.DroppedTotal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// R-VLA7-UUUK
	if len(stored) != 0 || dropped != 0 {
		t.Fatalf("malformed envelopes stored %d records and dropped total %d, want both zero", len(stored), dropped)
	}
}

func TestForwardedRequestIsHiddenWhileByteIdenticalLoopbackRequestStores(t *testing.T) {
	env := newTestEnvironment(t)
	body := marshalRequest(t, Request{Records: []record.Record{testRecord("01H00000000000000000000001")}})
	forwarded := env.post(t, body, "https")
	if forwarded.status != http.StatusNotFound || forwarded.raw != "404 page not found\n" {
		t.Fatalf("forwarded POST = status %d body %q, want bare 404", forwarded.status, forwarded.raw)
	}
	if _, found, err := env.store.Get(context.Background(), "01H00000000000000000000001"); err != nil || found {
		t.Fatalf("forwarded POST stored record: found=%t err=%v", found, err)
	}
	loopback := env.post(t, body, "")
	_, found, err := env.store.Get(context.Background(), "01H00000000000000000000001")
	// R-VMI4-8ML9
	if loopback.status != http.StatusAccepted || loopback.body.Stored != 1 || !found || err != nil {
		t.Fatalf("loopback POST response=%d %+v found=%t err=%v", loopback.status, loopback.body, found, err)
	}
}

func TestDroppedCountsAccumulateAndZeroOrAbsentDoNothing(t *testing.T) {
	env := newTestEnvironment(t)
	requests := []Request{
		{Records: []record.Record{testRecord("01H00000000000000000000001")}, Dropped: 7},
		{Records: []record.Record{testRecord("01H00000000000000000000002")}, Dropped: 3},
		{Records: []record.Record{testRecord("01H00000000000000000000003")}, Dropped: 0},
		{Records: []record.Record{testRecord("01H00000000000000000000004")}},
	}
	for _, request := range requests {
		response := env.post(t, marshalRequest(t, request), "")
		if response.status != http.StatusAccepted {
			t.Fatalf("POST status = %d", response.status)
		}
	}
	total, err := env.store.DroppedTotal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var service string
	var rows int
	if err := env.database.QueryRow(`SELECT MIN(service), COUNT(*) FROM ingest_drops`).Scan(&service, &rows); err != nil {
		t.Fatal(err)
	}
	// R-VNQ0-MEBY
	if total != 10 || service != "service-a" || rows != 2 {
		t.Fatalf("dropped total=%d service=%q rows=%d, want 10 attributed to service-a in two rows", total, service, rows)
	}
}

func TestMethodIsRestrictedButContentTypeIsNot(t *testing.T) {
	env := newTestEnvironment(t)
	getResponse, err := env.client.Get(env.url + Path)
	if err != nil {
		t.Fatal(err)
	}
	_ = getResponse.Body.Close()
	if getResponse.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", getResponse.StatusCode)
	}
	body := marshalRequest(t, Request{Records: []record.Record{testRecord("01H00000000000000000000001")}})
	request, err := http.NewRequest(http.MethodPost, env.url+Path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "text/plain")
	response, err := env.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST with wrong Content-Type status = %d, want 202", response.StatusCode)
	}
}

func TestOversizedStreamIsCutOffAtCapWithoutStorage(t *testing.T) {
	env := newTestEnvironment(t)
	total := int64(bodyLimit + (2 << 20))
	source := newStreamingBody([]byte(`{"records":[],"padding":"`), total, int64(bodyLimit+(64<<10)))
	request, err := http.NewRequest(http.MethodPost, env.url+Path, source)
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = total
	response, err := env.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	stored, _, err := env.store.Search(context.Background(), db.Query{})
	if err != nil {
		t.Fatal(err)
	}
	// R-VQ5T-DXTC
	if response.StatusCode != http.StatusRequestEntityTooLarge || len(stored) != 0 || source.count.Load() >= total {
		t.Fatalf("oversized POST status=%d body=%q stored=%d bytes read=%d of %d", response.StatusCode, responseBody, len(stored), source.count.Load(), total)
	}
}

type testEnvironment struct {
	url      string
	client   *http.Client
	store    *db.Store
	database *sql.DB
	logs     *bytes.Buffer
}

type postResponse struct {
	status int
	body   Response
	raw    string
}

func newTestEnvironment(t *testing.T) *testEnvironment {
	t.Helper()
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open real sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	store := db.NewStore(database)
	clock := fixedClock{at: time.Date(2026, 8, 3, 4, 5, 6, 0, time.UTC)}
	var _ telemetrytime.Clock = clock
	httpServer, err := server.New(server.Options{
		Addr:       listener.Addr().String(),
		Logger:     logger,
		ResourceID: "https://example.test/srv/telemetry/",
		AuthServer: "https://example.test/",
		Version:    "test",
		Service:    "telemetry",
		DB:         database,
		Register: func(rt *server.Router) error {
			Mount(rt, store, clock)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("construct chassis server: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		<-serveDone
	})
	return &testEnvironment{
		url:      "http://" + listener.Addr().String(),
		client:   &http.Client{Timeout: 5 * time.Second},
		store:    store,
		database: database,
		logs:     logs,
	}
}

func (env *testEnvironment) post(t *testing.T, body, forwardedProto string) postResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, env.url+Path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if forwardedProto != "" {
		request.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	response, err := env.client.Do(request)
	if err != nil {
		t.Fatalf("POST %s: %v", Path, err)
	}
	encoded, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	result := postResponse{status: response.StatusCode, raw: string(encoded)}
	if response.StatusCode == http.StatusAccepted {
		if err := json.Unmarshal(encoded, &result.body); err != nil {
			t.Fatalf("decode response %q: %v", encoded, err)
		}
	}
	return result
}

func marshalRequest(t *testing.T, request Request) string {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func testRecord(id string) record.Record {
	return record.Record{
		ID: id, Time: "2026-08-03T04:05:06Z", CorrelationID: "chain-a",
		Service: "service-a", Kind: record.KindRequest, Op: "widgets.read",
		Params: json.RawMessage(`{"visible":true}`), Outcome: record.Outcome{Status: "ok"},
	}
}

func recordIDs(records []record.Record) []string {
	result := make([]string, len(records))
	for i, item := range records {
		result[i] = item.ID
	}
	return result
}

type fixedClock struct{ at time.Time }

func (clock fixedClock) Now() time.Time { return clock.at }

type streamingBody struct {
	prefix    []byte
	total     int64
	pauseAt   int64
	count     atomic.Int64
	closed    chan struct{}
	closeOnce sync.Once
}

func newStreamingBody(prefix []byte, total, pauseAt int64) *streamingBody {
	return &streamingBody{prefix: prefix, total: total, pauseAt: pauseAt, closed: make(chan struct{})}
}

func (body *streamingBody) Read(p []byte) (int, error) {
	read := body.count.Load()
	if read >= body.total {
		return 0, io.EOF
	}
	if read >= body.pauseAt {
		<-body.closed
		return 0, io.ErrClosedPipe
	}
	remaining := body.pauseAt - read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	for i := range p {
		position := read + int64(i)
		if position < int64(len(body.prefix)) {
			p[i] = body.prefix[position]
		} else {
			p[i] = 'a'
		}
	}
	body.count.Add(int64(len(p)))
	return len(p), nil
}

func (body *streamingBody) Close() error {
	body.closeOnce.Do(func() { close(body.closed) })
	return nil
}
