package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	appkitdb "appkit/db"
	appkitmcp "appkit/mcp"
	"telemetry/internal/db"
	"telemetry/internal/record"
)

func TestToolListIsExactReadOnlySurface(t *testing.T) {
	env := newMCPEnvironment(t)
	result := env.rpc(t, "tools/list", map[string]any{})
	tools := result["tools"].([]any)
	var names []string
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		description := tool["description"].(string)
		names = append(names, name)
		for _, forbidden := range []string{"delete", "redact", "annotate", "purge", "stats", "aggregate", "export"} {
			if strings.Contains(strings.ToLower(name+" "+description), forbidden) {
				t.Errorf("tool %q contains forbidden surface word %q", name, forbidden)
			}
		}
	}
	sort.Strings(names)
	// R-VW9B-ASIT
	if want := []string{"chain", "get", "guide", "health", "reflection", "search"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("tools/list names = %v, want exact set %v", names, want)
	}
}

func TestSearchFiltersComposeAndDigestCrossesServices(t *testing.T) {
	env := newMCPEnvironment(t)
	base := mcpRecord(1, "2026-08-03T05:00:00Z")
	base.Outcome.SHA256 = "shared-digest"
	seed := []record.Record{base}
	mutations := []func(*record.Record){
		func(r *record.Record) { r.Service = "other" },
		func(r *record.Record) { r.Kind = record.KindOutbound },
		func(r *record.Record) { r.Actor.OwnerEmail = "other@example.test" },
		func(r *record.Record) { r.Op = "unrelated.operation" },
		func(r *record.Record) { r.Outcome.Status = "error" },
		func(r *record.Record) { r.Time = "2026-08-04T05:00:00Z" },
	}
	for i, mutate := range mutations {
		item := base
		item.ID = recordID(i + 2)
		mutate(&item)
		seed = append(seed, item)
	}
	otherService := base
	otherService.ID = recordID(20)
	otherService.Service = "digest-peer"
	otherService.Actor.OwnerEmail = "peer@example.test"
	seed = append(seed, otherService)
	differentDigest := base
	differentDigest.ID = recordID(21)
	differentDigest.Outcome.SHA256 = "different"
	seed = append(seed, differentDigest)
	env.insert(t, seed...)

	result := env.call(t, "search", map[string]any{
		"service": "alpha", "kind": "request", "owner_email": "owner@example.test",
		"op_contains": "WIDGET", "status": "ok", "since": "2026-08-03T04:00:00Z", "until": "2026-08-03T06:00:00Z",
	})
	records := structuredRecords(t, result)
	// R-VXH7-OK9I
	if len(records) != 2 || records[0]["id"] != differentDigest.ID || records[1]["id"] != base.ID {
		t.Fatalf("AND search returned ids %v, want only records matching all six axes", mapIDs(records))
	}
	digestResult := env.call(t, "search", map[string]any{"sha256": "shared-digest"})
	digestRecords := structuredRecords(t, digestResult)
	services := map[string]bool{}
	for _, item := range digestRecords {
		if item["outcome"].(map[string]any)["sha256"] != "shared-digest" {
			t.Fatalf("digest result contains different digest: %v", item)
		}
		services[item["service"].(string)] = true
	}
	if !services["alpha"] || !services["digest-peer"] {
		t.Fatalf("digest search services = %v, want alpha and digest-peer", services)
	}
}

func TestSearchLimitsValidationAndCursorPages(t *testing.T) {
	env := newMCPEnvironment(t)
	items := make([]record.Record, 0, 52)
	for i := 1; i <= 52; i++ {
		items = append(items, mcpRecord(i, time.Date(2026, 8, 3, 0, 0, i, 0, time.UTC).Format(time.RFC3339Nano)))
	}
	env.insert(t, items...)
	first := env.call(t, "search", map[string]any{})
	firstRecords := structuredRecords(t, first)
	structured := first["structuredContent"].(map[string]any)
	cursor := structured["next_cursor"].(string)
	second := env.call(t, "search", map[string]any{"cursor": cursor})
	secondRecords := structuredRecords(t, second)
	seen := map[string]bool{}
	for _, item := range firstRecords {
		seen[item["id"].(string)] = true
	}
	for _, item := range secondRecords {
		if seen[item["id"].(string)] {
			t.Fatalf("record repeated across cursor pages: %s", item["id"])
		}
	}
	accepted := env.call(t, "search", map[string]any{"limit": 500})
	for _, bad := range []int{0, 501} {
		failure := env.call(t, "search", map[string]any{"limit": bad})
		errorContent, _ := failure["structuredContent"].(map[string]any)
		_, hasRecords := errorContent["records"]
		if failure["isError"] != true || errorContent["code"] != "validation" || !strings.Contains(resultText(failure), "limit") || hasRecords {
			t.Errorf("limit %d result = %v, want named validation error without records", bad, failure)
		}
	}
	// R-VYP4-2C07
	if len(firstRecords) != 50 || cursor == "" || len(secondRecords) != 2 || second["structuredContent"].(map[string]any)["next_cursor"] != "" || len(structuredRecords(t, accepted)) != 52 {
		t.Fatalf("pagination default=%d second=%d cursor=%q last=%v accepted=%d", len(firstRecords), len(secondRecords), cursor, second["structuredContent"], len(structuredRecords(t, accepted)))
	}
}

func TestChainIsCompleteOrderedAndHonestlyBounded(t *testing.T) {
	env := newMCPEnvironment(t)
	oldestOther := mcpRecord(1, "2026-08-03T01:00:00Z")
	oldestOther.CorrelationID = "other"
	chain := []record.Record{mcpRecord(2, "2026-08-03T02:00:00Z"), mcpRecord(3, "2026-08-03T03:00:00Z"), mcpRecord(4, "2026-08-03T04:00:00Z")}
	chain[0].Service, chain[1].Service, chain[2].Service = "alpha", "beta", "gamma"
	env.insert(t, oldestOther, chain[2], chain[0], chain[1])
	result := env.call(t, "chain", map[string]any{"correlation_id": "chain-a"})
	content := result["structuredContent"].(map[string]any)
	records := content["records"].([]any)
	unknown := env.call(t, "chain", map[string]any{"correlation_id": "unknown"})
	unknownContent := unknown["structuredContent"].(map[string]any)
	if content["count"] != float64(3) || content["retention_horizon"] != normalized(t, oldestOther.Time) || content["possibly_truncated"] != false || unknown["isError"] != nil || len(unknownContent["records"].([]any)) != 0 || unknownContent["count"] != float64(0) {
		t.Fatalf("chain result=%v unknown=%v", content, unknown)
	}
	for i, item := range records {
		if item.(map[string]any)["id"] != chain[i].ID {
			t.Fatalf("chain order = %v", records)
		}
	}

	env2 := newMCPEnvironment(t)
	env2.insert(t, chain...)
	atHorizon := env2.call(t, "chain", map[string]any{"correlation_id": "chain-a"})
	// R-VZX0-G3QW
	if atHorizon["structuredContent"].(map[string]any)["possibly_truncated"] != true {
		t.Fatalf("chain beginning at retention horizon not marked possibly truncated: %v", atHorizon)
	}
}

func TestGetPreservesObjectsAndReturnsNotFound(t *testing.T) {
	env := newMCPEnvironment(t)
	item := mcpRecord(1, "2026-08-03T01:00:00Z")
	item.Params = json.RawMessage(`{"nested":{"value":1}}`)
	item.Detail = json.RawMessage(`{"exact":[1,2,3]}`)
	env.insert(t, item)
	result := env.call(t, "get", map[string]any{"id": item.ID})
	contentBytes, _ := json.Marshal(result["structuredContent"])
	var got record.Record
	if err := json.Unmarshal(contentBytes, &got); err != nil {
		t.Fatal(err)
	}
	missing := env.call(t, "get", map[string]any{"id": recordID(99)})
	// R-W2CT-7N8A
	missingContent, _ := missing["structuredContent"].(map[string]any)
	if !bytes.Equal(got.Params, item.Params) || !bytes.Equal(got.Detail, item.Detail) || missing["isError"] != true || missingContent["code"] != "not_found" {
		t.Fatalf("get params=%s detail=%s missing=%v", got.Params, got.Detail, missing)
	}
}

func TestStructuredContractsAndGuideTextResult(t *testing.T) {
	env := newMCPEnvironment(t)
	item := mcpRecord(1, "2026-08-03T01:00:00Z")
	env.insert(t, item)
	listed := env.rpc(t, "tools/list", map[string]any{})["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, raw := range listed {
		tool := raw.(map[string]any)
		byName[tool["name"].(string)] = tool
	}
	results := map[string]map[string]any{
		"search": env.call(t, "search", map[string]any{}),
		"chain":  env.call(t, "chain", map[string]any{"correlation_id": "chain-a"}),
		"get":    env.call(t, "get", map[string]any{"id": item.ID}),
	}
	for name, result := range results {
		if byName[name]["outputSchema"] == nil || result["structuredContent"] == nil {
			t.Errorf("%s lacks structured contract: tool=%v result=%v", name, byName[name], result)
			continue
		}
		var mirrored any
		if err := json.Unmarshal([]byte(resultText(result)), &mirrored); err != nil {
			t.Errorf("%s text is not JSON: %v", name, err)
		}
		if !reflect.DeepEqual(mirrored, result["structuredContent"]) {
			t.Errorf("%s mirrored text differs", name)
		}
		assertSchemaShape(t, name, byName[name]["outputSchema"].(map[string]any), result["structuredContent"])
	}
	guideResult := env.call(t, "guide", map[string]any{})
	// R-W3KP-LEYZ
	if _, present := byName["guide"]["outputSchema"]; present || guideResult["structuredContent"] != nil || resultText(guideResult) == "" {
		t.Fatalf("guide descriptor/result not text-only: tool=%v result=%v", byName["guide"], guideResult)
	}
}

func TestGuideDocumentsStableModelAndValidExamples(t *testing.T) {
	env := newMCPEnvironment(t)
	text := resultText(env.call(t, "guide", map[string]any{}))
	for _, required := range []string{"edge", "request", "outbound", "publish", "consume", "root", "lifecycle", "params", "outcome", "detail", `{"$elided":`, "bodies and conversations are never stored", "search", "since", "kind", "status", "op_contains", "get", "id", "chain", "correlation_id", "sha256"} {
		if !strings.Contains(text, required) {
			t.Errorf("guide missing %q", required)
		}
	}
	declared := map[string]bool{}
	properties := map[string]map[string]bool{}
	for _, raw := range env.rpc(t, "tools/list", map[string]any{})["tools"].([]any) {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		declared[name] = true
		properties[name] = map[string]bool{}
		input := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		for argument := range input {
			properties[name][argument] = true
		}
	}
	// R-W4SL-Z6PO
	validExample := declared["search"] && declared["get"] && declared["chain"]
	for _, argument := range []string{"since", "kind", "status", "op_contains", "sha256"} {
		validExample = validExample && properties["search"][argument]
	}
	validExample = validExample && properties["get"]["id"] && properties["chain"]["correlation_id"]
	if len(text) == 0 || !validExample {
		t.Fatalf("guide is empty or worked example tools drifted from declared surface")
	}
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type mcpEnvironment struct {
	url      string
	client   *http.Client
	store    *db.Store
	database *sql.DB
}

func newMCPEnvironment(t *testing.T) *mcpEnvironment {
	t.Helper()
	database := openMCPDatabase(t)
	store := db.NewStore(database)
	handler, err := appkitmcp.New(appkitmcp.Options{Service: "telemetry", Version: "test", Instructions: Instructions, Tools: Tools(store, fixedClock{at: time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)}), Health: func(context.Context) (map[string]any, error) { return map[string]any{"ok": true}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()); <-done })
	return &mcpEnvironment{url: "http://" + listener.Addr().String() + "/mcp", client: &http.Client{Timeout: 5 * time.Second}, store: store, database: database}
}

func (e *mcpEnvironment) rpc(t *testing.T, method string, params map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	response, err := e.client.Post(e.url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", method, response.StatusCode, raw)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode %s response %s: %v", method, raw, err)
	}
	if envelope["error"] != nil {
		t.Fatalf("%s RPC error: %v", method, envelope["error"])
	}
	return envelope["result"].(map[string]any)
}

func (e *mcpEnvironment) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	return e.rpc(t, "tools/call", map[string]any{"name": name, "arguments": args})
}
func (e *mcpEnvironment) insert(t *testing.T, items ...record.Record) {
	t.Helper()
	tx, err := e.database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	const statement = `INSERT INTO records (
		id, time, correlation_id, service, kind, owner_email, client_id, op,
		status, error, duration_ms, bytes, sha256, params, detail, received_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, item := range items {
		if err := item.Validate(); err != nil {
			t.Fatal(err)
		}
		item.Time = normalized(t, item.Time)
		if _, err := tx.ExecContext(context.Background(), statement,
			item.ID, item.Time, item.CorrelationID, item.Service, item.Kind,
			item.Actor.OwnerEmail, item.Actor.ClientID, item.Op, item.Outcome.Status,
			item.Outcome.Error, item.Outcome.DurationMS, item.Outcome.Bytes, item.Outcome.SHA256,
			testJSONValue(item.Params), testJSONValue(item.Detail), "2026-08-04T00:00:00.000000000Z",
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func testJSONValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func openMCPDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatal(err)
	}
	return database
}

func mcpRecord(n int, at string) record.Record {
	return record.Record{ID: recordID(n), Time: at, CorrelationID: "chain-a", Service: "alpha", Kind: record.KindRequest, Actor: record.Actor{OwnerEmail: "owner@example.test", ClientID: "client-a"}, Op: "widgets.read", Params: json.RawMessage(`{"visible":true}`), Outcome: record.Outcome{Status: "ok"}, Detail: json.RawMessage(`{"answer":42}`)}
}
func recordID(n int) string { return fmt.Sprintf("01H00000000000000000000%03d", n) }
func structuredRecords(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	raw := result["structuredContent"].(map[string]any)["records"].([]any)
	records := make([]map[string]any, len(raw))
	for i, item := range raw {
		records[i] = item.(map[string]any)
	}
	return records
}
func mapIDs(records []map[string]any) []any {
	result := make([]any, len(records))
	for i, item := range records {
		result[i] = item["id"]
	}
	return result
}
func resultText(result map[string]any) string {
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	return content[0].(map[string]any)["text"].(string)
}
func normalized(t *testing.T, value string) string {
	t.Helper()
	got, err := record.NormalizeTime(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertSchemaShape(t *testing.T, name string, schema map[string]any, value any) {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Errorf("%s structured result is not the declared object shape", name)
		return
	}
	required, _ := schema["required"].([]any)
	for _, raw := range required {
		field := raw.(string)
		if _, present := object[field]; !present {
			t.Errorf("%s structured result lacks required schema field %q", name, field)
		}
	}
}
