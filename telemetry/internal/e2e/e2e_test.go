package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"
	"testing"
	"time"

	appkittelemetry "appkit/telemetry"
	"telemetry/internal/record"
)

func TestChassisRecorderRoundTripsThroughComposedService(t *testing.T) {
	binary := buildTelemetry(t)
	process := startTelemetry(t, binary, filepath.Join(t.TempDir(), "telemetry.db"))
	defer process.stop(t)

	observer := &ingestObserver{transport: http.DefaultTransport}
	recorder := appkittelemetry.New(appkittelemetry.Options{
		Service:    "recorder-producer",
		IngestURL:  process.baseURL + "/ingest",
		Enabled:    true,
		Capacity:   8,
		BatchMax:   8,
		FlushEvery: time.Hour,
		Client:     &http.Client{Timeout: 5 * time.Second, Transport: observer},
	})
	ctx, cancel := context.WithCancel(context.Background())
	recorder.Start(ctx)
	recorder.Record(appkittelemetry.Record{
		CorrelationID: "recorder-wire-contract",
		Service:       "recorder-producer",
		Kind:          appkittelemetry.KindRequest,
		Op:            "wire.roundtrip",
		Params:        map[string]any{"source": "real-recorder"},
		Outcome:       &appkittelemetry.Outcome{Status: "ok", DurationMS: 7},
	})
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := recorder.Close(closeCtx); err != nil {
		t.Fatalf("close recorder: %v", err)
	}
	cancel()

	stored, rejected, requests, observeErr := observer.result()
	if observeErr != nil {
		t.Fatalf("observe recorder ingest response: %v", observeErr)
	}
	chain := callRecords(t, process, "chain", map[string]any{"correlation_id": "recorder-wire-contract"})
	if len(chain) != 1 {
		t.Fatalf("chain record count = %d, want 1", len(chain))
	}
	emitted := chain[0]
	got := callRecord(t, process, "get", map[string]any{"id": emitted.ID})

	// R-5PIJ-TFHS
	if requests != 1 || stored != 1 || rejected != 0 {
		t.Fatalf("recorder ingest totals = requests:%d stored:%d rejected:%d, want 1, 1, 0", requests, stored, rejected)
	}
	if emitted.ID == "" || emitted.Time == "" {
		t.Fatalf("recorder-stamped identity = id:%q time:%q, want both non-empty", emitted.ID, emitted.Time)
	}
	if emitted.CorrelationID != "recorder-wire-contract" || emitted.Service != "recorder-producer" || emitted.Op != "wire.roundtrip" {
		t.Fatalf("chain returned wrong recorder record: %+v", emitted)
	}
	if !reflect.DeepEqual(got, emitted) {
		t.Fatalf("get record = %#v, want chain record %#v", got, emitted)
	}
}

type ingestObserver struct {
	transport http.RoundTripper
	stored    int
	rejected  int
	requests  int
	err       error
}

func (o *ingestObserver) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := o.transport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if request.URL.Path != "/ingest" {
		return response, nil
	}
	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		o.err = readErr
		return response, nil
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	var result struct {
		Stored   int `json:"stored"`
		Rejected int `json:"rejected"`
	}
	if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
		o.err = decodeErr
		return response, nil
	}
	o.requests++
	o.stored += result.Stored
	o.rejected += result.Rejected
	return response, nil
}

func (o *ingestObserver) result() (stored, rejected, requests int, err error) {
	return o.stored, o.rejected, o.requests, o.err
}

func TestComposedServiceRoundTripSurvivesRestart(t *testing.T) {
	binary := buildTelemetry(t)
	databasePath := filepath.Join(t.TempDir(), "telemetry.db")
	chainID := "incident-chain"
	chain, unrelated := incidentFixture(chainID)

	first := startTelemetry(t, binary, databasePath)
	allRecords := append(append([]record.Record{}, chain...), unrelated...)
	postIngest(t, first, allRecords)

	gotChain := callRecords(t, first, "chain", map[string]any{"correlation_id": chainID})
	wantChainIDs := recordIDs(chain)
	search := callRecords(t, first, "search", map[string]any{
		"service": "orders",
		"since":   "2026-08-03T10:02:00Z",
		"until":   "2026-08-03T10:05:00Z",
	})
	wantSearchIDs := []string{chain[5].ID, chain[3].ID, chain[2].ID}

	// R-WC40-9T5U
	if gotIDs := recordIDs(gotChain); !reflect.DeepEqual(gotIDs, wantChainIDs) {
		t.Fatalf("chain ids = %v, want every incident record in chronological order %v", gotIDs, wantChainIDs)
	}
	for _, item := range gotChain {
		if item.CorrelationID != chainID {
			t.Fatalf("chain leaked unrelated record %s from correlation %q", item.ID, item.CorrelationID)
		}
	}
	// R-WC40-9T5U
	if gotIDs := recordIDs(search); !reflect.DeepEqual(gotIDs, wantSearchIDs) {
		t.Fatalf("service/time search ids = %v, want descending narrowed subset %v", gotIDs, wantSearchIDs)
	}
	gotByID := callRecord(t, first, "get", map[string]any{"id": search[1].ID})
	postedByID := indexRecords(allRecords)
	// R-WC40-9T5U
	if want := postedByID[gotByID.ID]; gotByID.ID != search[1].ID || !bytes.Equal(gotByID.Params, want.Params) {
		t.Fatalf("get id=%q params=%s, want search id=%q with unchanged params bytes %s", gotByID.ID, gotByID.Params, search[1].ID, want.Params)
	}

	first.stop(t)
	second := startTelemetry(t, binary, databasePath)
	defer second.stop(t)
	restartedChain := callRecords(t, second, "chain", map[string]any{"correlation_id": chainID})
	restartedRecord := callRecord(t, second, "get", map[string]any{"id": gotByID.ID})

	// R-WDBW-NKWJ
	if !reflect.DeepEqual(restartedChain, gotChain) {
		t.Fatalf("chain after restart differs\nbefore: %#v\nafter:  %#v", gotChain, restartedChain)
	}
	// R-WDBW-NKWJ
	if !reflect.DeepEqual(restartedRecord, gotByID) || !bytes.Equal(restartedRecord.Params, postedByID[gotByID.ID].Params) {
		t.Fatalf("get after restart = %#v, want durable record %#v", restartedRecord, gotByID)
	}
}

type serviceProcess struct {
	baseURL string
	client  *http.Client
	cmd     *exec.Cmd
	done    chan error
	stderr  *bytes.Buffer
	stopped bool
}

func buildTelemetry(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "telemetry")
	command := exec.Command("go", "build", "-o", binary, "../../cmd/telemetry")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build real telemetry binary: %v\n%s", err, output)
	}
	return binary
}

func startTelemetry(t *testing.T, binary, databasePath string) *serviceProcess {
	t.Helper()
	port := ephemeralPort(t)
	command := exec.Command(binary, "serve")
	command.Env = append(os.Environ(),
		"TELEMETRY_IP=127.0.0.1",
		"TELEMETRY_PORT="+strconv.Itoa(port),
		"TELEMETRY_DB_PATH="+databasePath,
		"TELEMETRY_ENABLED=false",
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start telemetry: %v", err)
	}
	process := &serviceProcess{
		baseURL: "http://127.0.0.1:" + strconv.Itoa(port),
		client:  &http.Client{Timeout: 5 * time.Second},
		cmd:     command,
		done:    make(chan error, 1),
		stderr:  &stderr,
	}
	go func() { process.done <- command.Wait() }()
	t.Cleanup(func() { process.stop(t) })
	waitUntilReady(t, process)
	return process
}

func waitUntilReady(t *testing.T, process *serviceProcess) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	for {
		response, err := client.Get(process.baseURL + "/health")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				process.stop(t)
				t.Fatalf("GET /health status = %d", response.StatusCode)
			}
			return
		}
		select {
		case waitErr := <-process.done:
			process.stopped = true
			t.Fatalf("telemetry exited before readiness: %v\n%s", waitErr, process.stderr.String())
		case <-ctx.Done():
			process.stop(t)
			t.Fatalf("telemetry readiness timed out: %v\n%s", ctx.Err(), process.stderr.String())
		default:
		}
	}
}

func (process *serviceProcess) stop(t *testing.T) {
	t.Helper()
	if process.stopped {
		return
	}
	process.stopped = true
	if err := process.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("signal telemetry process: %v", err)
	}
	select {
	case err := <-process.done:
		if err != nil {
			t.Errorf("telemetry shutdown: %v\n%s", err, process.stderr.String())
		}
	case <-time.After(5 * time.Second):
		_ = process.cmd.Process.Kill()
		<-process.done
		t.Errorf("telemetry did not stop after SIGTERM\n%s", process.stderr.String())
	}
}

func ephemeralPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate ephemeral port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release ephemeral port: %v", err)
	}
	return port
}

func postIngest(t *testing.T, process *serviceProcess, records []record.Record) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"records": records})
	if err != nil {
		t.Fatal(err)
	}
	response, err := process.client.Post(process.baseURL+"/ingest", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /ingest: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /ingest status=%d body=%s", response.StatusCode, raw)
	}
	var result struct {
		Stored   int `json:"stored"`
		Rejected int `json:"rejected"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode ingest response %s: %v", raw, err)
	}
	if result.Stored != len(records) || result.Rejected != 0 {
		t.Fatalf("ingest result = %+v, want stored=%d rejected=0", result, len(records))
	}
}

func callRecords(t *testing.T, process *serviceProcess, tool string, arguments map[string]any) []record.Record {
	t.Helper()
	var content struct {
		Records []record.Record `json:"records"`
	}
	callTool(t, process, tool, arguments, &content)
	return content.Records
}

func callRecord(t *testing.T, process *serviceProcess, tool string, arguments map[string]any) record.Record {
	t.Helper()
	var content record.Record
	callTool(t, process, tool, arguments, &content)
	return content
}

func callTool(t *testing.T, process *serviceProcess, tool string, arguments map[string]any, content any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      tool,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": arguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, process.baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Owner-Id", "e2e-owner")
	request.Header.Set("X-Owner-Email", "e2e@example.test")
	request.Header.Set("X-Client-Id", "e2e-client")
	response, err := process.client.Do(request)
	if err != nil {
		t.Fatalf("POST /mcp %s: %v", tool, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp %s status=%d body=%s", tool, response.StatusCode, raw)
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode /mcp %s response %s: %v", tool, raw, err)
	}
	if len(envelope.Error) != 0 || envelope.Result.IsError {
		t.Fatalf("POST /mcp %s returned error: %s", tool, raw)
	}
	if err := json.Unmarshal(envelope.Result.StructuredContent, content); err != nil {
		t.Fatalf("decode /mcp %s structured content %s: %v", tool, envelope.Result.StructuredContent, err)
	}
}

func incidentFixture(chainID string) ([]record.Record, []record.Record) {
	chain := []record.Record{
		fixtureRecord(1, "2026-08-03T10:00:00Z", chainID, "gateway", record.KindRoot, "incident.start", `{"route":"/checkout"}`),
		fixtureRecord(2, "2026-08-03T10:01:00Z", chainID, "gateway", record.KindEdge, "http.accept", `{"method":"POST"}`),
		fixtureRecord(3, "2026-08-03T10:02:00Z", chainID, "orders", record.KindRequest, "orders.create", `{"cart_id":"cart-7"}`),
		fixtureRecord(4, "2026-08-03T10:03:00Z", chainID, "orders", record.KindOutbound, "inventory.reserve", `{"sku":"blue-widget","quantity":2}`),
		fixtureRecord(5, "2026-08-03T10:04:00Z", chainID, "inventory", record.KindRequest, "stock.reserve", `{"reservation":"res-9"}`),
		fixtureRecord(6, "2026-08-03T10:05:00Z", chainID, "orders", record.KindPublish, "order.created", `{"topic":"orders"}`),
		fixtureRecord(7, "2026-08-03T10:06:00Z", chainID, "worker", record.KindConsume, "order.created", `{"delivery":1}`),
		fixtureRecord(8, "2026-08-03T10:07:00Z", chainID, "worker", record.KindLifecycle, "worker.start", `{"version":"test"}`),
	}
	unrelated := []record.Record{
		fixtureRecord(90, "2026-08-03T10:00:30Z", "unrelated-chain", "gateway", record.KindRoot, "incident.start", `{"route":"/profile"}`),
		fixtureRecord(91, "2026-08-03T10:03:30Z", "unrelated-chain", "accounts", record.KindRequest, "profile.read", `{"user":"other"}`),
	}
	return chain, unrelated
}

func fixtureRecord(number int, at, chainID, service string, kind record.Kind, op, params string) record.Record {
	duration := int64(number)
	return record.Record{
		ID:            fmt.Sprintf("01H00000000000000000000%03d", number),
		Time:          at,
		CorrelationID: chainID,
		Service:       service,
		Kind:          kind,
		Actor:         record.Actor{OwnerEmail: "operator@example.test", ClientID: "incident-client"},
		Op:            op,
		Params:        json.RawMessage(params),
		Outcome:       record.Outcome{Status: "ok", DurationMS: &duration},
	}
}

func recordIDs(records []record.Record) []string {
	ids := make([]string, len(records))
	for index, item := range records {
		ids[index] = item.ID
	}
	return ids
}

func indexRecords(records []record.Record) map[string]record.Record {
	byID := make(map[string]record.Record, len(records))
	for _, item := range records {
		byID[item.ID] = item
	}
	return byID
}
