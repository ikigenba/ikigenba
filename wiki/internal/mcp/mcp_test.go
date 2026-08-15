package mcp

import (
	"appkit"
	"appkit/server"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"wiki/internal/ask"
	"wiki/internal/llm"
	"wiki/internal/llmtest"
	"wiki/internal/retrieve"

	appdb "appkit/db"
	appkitmcp "appkit/mcp"

	wikidb "wiki/internal/db"

	paging "wiki/internal/page"

	wikidomain "wiki/internal/wiki"
)

func TestHealthToolReturnsAppkitEnvelope(t *testing.T) {
	h := newTestHandler(t)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"health"}}`)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if len(got.Result.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(got.Result.Content))
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(got.Result.Content[0].Text), &env); err != nil {
		t.Fatalf("health text JSON: %v", err)
	}
	if env["service"] != "wiki" {
		t.Fatalf("service = %v, want wiki", env["service"])
	}
	if env["version"] != "test-version" {
		t.Fatalf("version = %v, want test-version", env["version"])
	}
	if env["status"] != "ok" {
		t.Fatalf("status = %v, want ok", env["status"])
	}
}

func TestInitializeAdvertisesWikiMCPServer(t *testing.T) {
	// R-6RVX-P1IG
	// R-YG82-DV8D
	h := newTestHandler(t)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":"init","method":"initialize"}`)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if got.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("protocolVersion = %q, want 2025-06-18", got.Result.ProtocolVersion)
	}
	if got.Result.Capabilities.Tools == nil {
		t.Fatal("capabilities.tools is nil")
	}
	if got.Result.ServerInfo.Name != "wiki" {
		t.Fatalf("serverInfo.name = %q, want wiki", got.Result.ServerInfo.Name)
	}
	if got.Result.ServerInfo.Version != "test-version" {
		t.Fatalf("serverInfo.version = %q, want test-version", got.Result.ServerInfo.Version)
	}
	if got.Result.Instructions != Instructions {
		t.Fatalf("instructions = %q, want pinned wiki instructions", got.Result.Instructions)
	}
	for _, phrase := range []string{"notes", "second brain", "entities", "events", "concepts", "guide"} {
		if !strings.Contains(strings.ToLower(got.Result.Instructions), phrase) {
			t.Fatalf("instructions = %q, want routing phrase %q", got.Result.Instructions, phrase)
		}
	}
}

func TestToolsListAdvertisesConfiguredWikiSurface(t *testing.T) {
	// R-JKMR-5MV1
	// R-MUQ4-K1JS
	// R-YF06-03HO
	// R-ENK6-P4KR
	h := gatedHandler(t, newTestHandler(t,
		WithIngestService(&capturingWiki{}),
		WithJobStatusService(&capturingWiki{}),
		WithJobAbortService(&capturingWiki{}),
		WithJobRerunService(&capturingWiki{}),
		WithJobListService(&capturingWiki{}),
		WithJobsCountService(&capturingWiki{}),
		WithMergeService(&capturingWiki{}, &capturingWiki{}),
		WithMergeListService(&capturingWiki{}),
		WithAskFunc((&capturingAsker{}).Ask),
		WithSubjectListService(&capturingWiki{}),
		WithClaimListService(&capturingWiki{}),
		WithPagePathService(&capturingWiki{}),
		WithScopeService(testScopeService{}),
	))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name         string         `json:"name"`
				InputSchema  map[string]any `json:"inputSchema"`
				OutputSchema map[string]any `json:"outputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, rec.Body.Bytes(), &got)
	names := map[string]bool{}
	for _, tool := range got.Result.Tools {
		if names[tool.Name] {
			t.Fatalf("tools/list duplicated %s", tool.Name)
		}
		names[tool.Name] = true
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("%s schema type = %v, want object", tool.Name, tool.InputSchema["type"])
		}
		if tool.Name == "guide" && tool.OutputSchema != nil {
			t.Fatalf("guide outputSchema = %#v, want omitted", tool.OutputSchema)
		}
		if tool.Name != "guide" && tool.Name != "health" && tool.Name != "reflection" && tool.OutputSchema == nil {
			t.Fatalf("%s outputSchema is nil", tool.Name)
		}
	}
	want := map[string]bool{
		"ingest":               true,
		"status":               true,
		"abort":                true,
		"rerun":                true,
		"jobs":                 true,
		"jobs_count":           true,
		"merge":                true,
		"merges":               true,
		"ask":                  true,
		"subjects":             true,
		"claims":               true,
		"page":                 true,
		"scopes":               true,
		"scope_create":         true,
		"scope_delete":         true,
		"scope_set_visibility": true,
		"instructions":         true,
		"guide":                true,
		"health":               true,
		"reflection":           true,
	}
	if len(names) != len(want) {
		t.Fatalf("tools/list names = %#v, want exact %#v", names, want)
	}
	for name := range want {
		if !names[name] {
			t.Fatalf("tools/list missing %s in %#v", name, names)
		}
	}
	for name := range names {
		if !want[name] {
			t.Fatalf("tools/list included unexpected %s in %#v", name, names)
		}
	}
	if names["llm_calls"] {
		t.Fatal("tools/list retained retired llm_calls capability")
	}
	verbs := make(map[string]bool, len(names)-1)
	for name := range names {
		if name != "guide" {
			verbs[name] = true
		}
	}
	if len(verbs) != 19 {
		t.Fatalf("non-guide verb membership = %#v, want exactly nineteen verbs", verbs)
	}
	if strings.Contains(Instructions, "llm_calls") || strings.Contains(guideDoc, "llm_calls") {
		t.Fatal("initialize instructions or guide advertises retired llm_calls capability")
	}
}

func TestGuideIsInputFreeSuccessfulAndDoesNotCallDomainServices(t *testing.T) {
	// R-YDS9-MBQZ
	// R-EPZZ-GO25
	wiki := &capturingWiki{jobCount: 41}
	before := *wiki
	h := gatedHandler(t, newTestHandler(t,
		WithIngestService(wiki),
		WithJobsCountService(wiki),
	))

	var schema map[string]any
	for _, tool := range Tools() {
		if tool.Name == "guide" {
			schema = tool.InputSchema
		}
	}
	if !reflect.DeepEqual(schema, map[string]any{"type": "object"}) {
		t.Fatalf("guide input schema = %#v, want input-free object schema", schema)
	}

	for _, request := range []string{
		`{"jsonrpc":"2.0","id":"guide-absent","method":"tools/call","params":{"name":"guide"}}`,
		`{"jsonrpc":"2.0","id":"guide-empty","method":"tools/call","params":{"name":"guide","arguments":{}}}`,
	} {
		rec := callMCP(t, h, request, "owner@example.com")
		if rec.Code != http.StatusOK {
			t.Fatalf("guide status = %d, want 200", rec.Code)
		}
		var got struct {
			Result struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				StructuredContent any  `json:"structuredContent"`
				IsError           bool `json:"isError"`
			} `json:"result"`
		}
		decodeJSON(t, rec.Body.Bytes(), &got)
		if got.Result.IsError {
			t.Fatalf("guide result marked as error: %s", rec.Body.String())
		}
		if len(got.Result.Content) != 1 || strings.TrimSpace(got.Result.Content[0].Text) == "" {
			t.Fatalf("guide content = %#v, want one non-empty document", got.Result.Content)
		}
		if got.Result.Content[0].Type != "text" || got.Result.StructuredContent != nil {
			t.Fatalf("guide result = %#v, want text only", got.Result)
		}
		if got.Result.Content[0].Text != guideDoc {
			t.Fatal("guide result does not match embedded document")
		}
	}
	if !reflect.DeepEqual(*wiki, before) {
		t.Fatalf("domain service state changed from %#v to %#v", before, *wiki)
	}
}

func TestScopedContentToolsRequireExplicitScope(t *testing.T) {
	// R-H8HN-EXJE
	wiki := &capturingWiki{pathSubjects: map[string]subject{"entity/from": {ID: "from"}, "entity/to": {ID: "to"}}}
	tools := Tools(
		WithIngestService(wiki), WithJobStatusService(wiki), WithJobAbortService(wiki),
		WithJobRerunService(wiki), WithJobListService(wiki), WithJobsCountService(wiki),
		WithMergeService(wiki, wiki), WithMergeListService(wiki), WithAskFunc((&capturingAsker{}).Ask),
		WithSubjectListService(wiki), WithClaimListService(wiki), WithPagePathService(wiki),
		WithScopeService(testScopeService{}),
	)
	scoped := map[string]bool{"ingest": true, "ask": true, "subjects": true, "claims": true, "page": true, "merge": true, "merges": true, "jobs": true, "jobs_count": true}
	unscoped := map[string]bool{"status": true, "abort": true, "rerun": true, "guide": true}
	for _, tool := range tools {
		properties, _ := tool.InputSchema["properties"].(map[string]any)
		if scoped[tool.Name] {
			if _, ok := properties["scope"]; !ok || !containsString(stringValues(tool.InputSchema["required"]), "scope") {
				t.Fatalf("%s schema = %#v, want required scope", tool.Name, tool.InputSchema)
			}
			result, err := tool.Handler(context.Background(), json.RawMessage(`{}`), server.Identity{})
			if err != nil || resultCode(result) != "validation" || !strings.Contains(resultMessage(result), "scope is required") {
				t.Fatalf("%s missing-scope result = %#v, %v", tool.Name, result, err)
			}
		}
		if unscoped[tool.Name] {
			if _, ok := properties["scope"]; ok {
				t.Fatalf("%s unexpectedly declares scope: %#v", tool.Name, tool.InputSchema)
			}
		}
	}
}

func TestUnknownScopeReturnsTypedErrorBeforeContentCall(t *testing.T) {
	// R-H9PJ-SPA3
	wiki := &capturingWiki{}
	tool := namedTool(t, Tools(WithIngestService(wiki), WithScopeService(testScopeService{})), "ingest")
	result, err := tool.Handler(context.Background(), json.RawMessage(`{"scope":"ghost","text":"must not write"}`), server.Identity{})
	if err != nil || resultCode(result) != "scope_not_found" || !strings.Contains(resultMessage(result), "scopes") {
		t.Fatalf("unknown-scope result = %#v, %v", result, err)
	}
	if wiki.ingestText != "" {
		t.Fatalf("ingest was called with %q for an unknown scope", wiki.ingestText)
	}
}

func TestScopeManagementLifecycle(t *testing.T) {
	// R-HAXG-6H0S
	// R-HC5C-K8RH
	// R-HDD8-Y0I6
	// R-HFT1-PJZK
	ctx := context.Background()
	db := migratedMCPDB(t, ctx)
	defer db.Close()
	store := wikidomain.NewScopeStore(db)
	wiki := &capturingWiki{}
	tools := Tools(WithScopeService(store), WithIngestService(wiki))

	listed := structuredCall(t, namedTool(t, tools, "scopes"), `{}`)
	scopes := mapSlice(listed["scopes"])
	if len(scopes) != 1 || scopes[0]["name"] != "default" || scopes[0]["visibility"] != "private" || scopes[0]["created_at"] == nil {
		t.Fatalf("fresh scopes = %#v, want default with visibility and created_at", scopes)
	}

	created := structuredCall(t, namedTool(t, tools, "scope_create"), `{"name":"team-x"}`)
	if created["name"] != "team-x" || created["visibility"] != "private" {
		t.Fatalf("scope_create = %#v", created)
	}
	structuredCall(t, namedTool(t, tools, "ingest"), `{"scope":"team-x","text":"accepted immediately"}`)
	if wiki.ingestScope != "team-x" || wiki.ingestText != "accepted immediately" {
		t.Fatalf("immediate ingest scope/text = %q/%q", wiki.ingestScope, wiki.ingestText)
	}

	create := namedTool(t, tools, "scope_create")
	for _, name := range []string{"UPPER", "has space", "-leading", " team-x ", "", strings.Repeat("a", 65)} {
		result, err := create.Handler(ctx, mustRaw(t, map[string]string{"name": name}), server.Identity{})
		if err != nil || resultCode(result) != "validation" || !strings.Contains(resultMessage(result), "slug") {
			t.Fatalf("scope_create %q = %#v, %v", name, result, err)
		}
	}
	for _, name := range []string{"default", "team-x"} {
		result, _ := create.Handler(ctx, mustRaw(t, map[string]string{"name": name}), server.Identity{})
		if resultCode(result) != "scope_exists" {
			t.Fatalf("duplicate %q code = %q", name, resultCode(result))
		}
	}

	visibility := namedTool(t, tools, "scope_set_visibility")
	visibilityProperty := visibility.InputSchema["properties"].(map[string]any)["visibility"].(map[string]any)
	if got := stringValues(visibilityProperty["enum"]); !reflect.DeepEqual(got, []string{"private", "public"}) {
		t.Fatalf("visibility enum = %#v", got)
	}
	for _, value := range []string{"public", "private"} {
		got := structuredCall(t, visibility, fmt.Sprintf(`{"name":"team-x","visibility":%q}`, value))
		if got["visibility"] != value {
			t.Fatalf("set visibility %q = %#v", value, got)
		}
	}
	listed = structuredCall(t, namedTool(t, tools, "scopes"), `{}`)
	scopes = mapSlice(listed["scopes"])
	if len(scopes) != 2 || scopes[0]["name"] != "default" || scopes[1]["name"] != "team-x" || scopes[1]["visibility"] != "private" {
		t.Fatalf("ordered scopes after visibility flips = %#v", scopes)
	}
	bad, _ := visibility.Handler(ctx, json.RawMessage(`{"name":"team-x","visibility":"friends"}`), server.Identity{})
	if resultCode(bad) != "validation" || !strings.Contains(resultMessage(bad), "{private, public}") {
		t.Fatalf("bad visibility = %#v", bad)
	}
}

func TestScopeDeleteRefusesDefaultAndRemovesOnlyNamedScopeContent(t *testing.T) {
	// R-HEL5-BS8V
	ctx := context.Background()
	db := migratedMCPDB(t, ctx)
	defer db.Close()
	store := wikidomain.NewScopeStore(db)
	for _, name := range []string{"s1", "s2"} {
		if _, err := store.Create(ctx, name); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	for _, row := range []struct{ id, scope string }{{"job-s1", "s1"}, {"job-s2", "s2"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO jobs (id, scope, owner_id, owner_email, source_text, source_hash, title, tags, status, received_at) VALUES (?, ?, 'owner', 'owner@example.com', 'text', ?, '', '[]', 'pending', 1)`, row.id, row.scope, "hash-"+row.id); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}
	tool := namedTool(t, Tools(WithScopeService(store)), "scope_delete")
	refused, _ := tool.Handler(ctx, json.RawMessage(`{"name":"default"}`), server.Identity{})
	if resultCode(refused) != "scope_is_default" {
		t.Fatalf("default delete = %#v", refused)
	}
	deleted := structuredCall(t, tool, `{"name":"s1"}`)
	if deleted["name"] != "s1" || deleted["deleted"] != true {
		t.Fatalf("delete s1 = %#v", deleted)
	}
	for _, tc := range []struct {
		scope string
		want  int
	}{{"s1", 0}, {"s2", 1}} {
		var got int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE scope = ?`, tc.scope).Scan(&got); err != nil || got != tc.want {
			t.Fatalf("jobs in %s = %d, %v; want %d", tc.scope, got, err, tc.want)
		}
	}
	missing, _ := tool.Handler(ctx, json.RawMessage(`{"name":"ghost"}`), server.Identity{})
	if resultCode(missing) != "not_found" {
		t.Fatalf("unknown delete = %#v", missing)
	}
}

func TestScopedListsPartitionJobsCountsAndMerges(t *testing.T) {
	// R-HH0Y-3BQ9
	// R-HI8U-H3GY
	partition := &scopePartitionService{}
	tools := Tools(WithJobListService(partition), WithJobsCountService(partition), WithMergeListService(partition), WithScopeService(partition))
	jobs1 := structuredCall(t, namedTool(t, tools, "jobs"), `{"scope":"s1"}`)
	jobs2 := structuredCall(t, namedTool(t, tools, "jobs"), `{"scope":"s2"}`)
	count1 := structuredCall(t, namedTool(t, tools, "jobs_count"), `{"scope":"s1"}`)
	count2 := structuredCall(t, namedTool(t, tools, "jobs_count"), `{"scope":"s2"}`)
	if len(mapSlice(jobs1["jobs"])) != 1 || len(mapSlice(jobs2["jobs"])) != 0 || count1["count"] != float64(1) || count2["count"] != float64(0) {
		t.Fatalf("partitioned jobs/counts = %#v %#v %#v %#v", jobs1, jobs2, count1, count2)
	}
	merges1 := structuredCall(t, namedTool(t, tools, "merges"), `{"scope":"s1"}`)
	merges2 := structuredCall(t, namedTool(t, tools, "merges"), `{"scope":"s2"}`)
	if len(mapSlice(merges1["merges"])) != 1 || len(mapSlice(merges2["merges"])) != 0 {
		t.Fatalf("partitioned merges = %#v / %#v", merges1, merges2)
	}
}

func TestScopeDiscoveryAppearsOnlyInInstructionsAndGuideDescription(t *testing.T) {
	// R-HJGQ-UV7N
	if !strings.Contains(strings.ToLower(Instructions), "scope") {
		t.Fatalf("instructions omit scope: %q", Instructions)
	}
	for _, phrase := range []string{"requires a `scope`", "`scopes`", "`scope_create`", "`scope_delete`", "`scope_set_visibility`", `"scope":"robotics"`} {
		if !strings.Contains(guideDoc, phrase) {
			t.Fatalf("guide omits %q", phrase)
		}
	}
	for _, tool := range Tools(WithScopeService(testScopeService{})) {
		mentionsGuide := strings.Contains(strings.ToLower(tool.Description), "guide")
		if mentionsGuide != (tool.Name == "guide") {
			t.Fatalf("%s description guide pointer = %v", tool.Name, mentionsGuide)
		}
	}
}

func TestInstructionsSchemaAndGetSetClearThroughHandler(t *testing.T) {
	// R-8PMQ-S0KJ
	ctx := context.Background()
	db := migratedMCPDB(t, ctx)
	defer db.Close()
	store := wikidomain.NewScopeStore(db)
	h := gatedHandler(t, newTestHandler(t, WithScopeService(store)))

	list := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")
	var listed struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, list.Body.Bytes(), &listed)
	schema := toolSchema(t, listed.Result.Tools, "instructions")
	if got := stringValues(schema["required"]); !reflect.DeepEqual(got, []string{"scope", "action"}) {
		t.Fatalf("instructions required = %#v", got)
	}
	action := schema["properties"].(map[string]any)["action"].(map[string]any)
	if got := stringValues(action["enum"]); !reflect.DeepEqual(got, []string{"get", "set"}) {
		t.Fatalf("instructions action enum = %#v", got)
	}

	call := func(id, args string) toolResult {
		rec := callMCP(t, h, fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"method":"tools/call","params":{"name":"instructions","arguments":%s}}`, id, args), "owner@example.com")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d; body=%s", id, rec.Code, rec.Body.String())
		}
		return decodeToolResult(t, rec.Body.Bytes())
	}
	assertValue := func(id, args, want string) {
		got := call(id, args)
		if got.IsError || got.StructuredContent["scope"] != "default" || got.StructuredContent["instructions"] != want {
			t.Fatalf("%s result = %#v, want instructions %q", id, got, want)
		}
	}
	assertValue("get-empty", `{"scope":"default","action":"get"}`, "")
	assertValue("set", `{"scope":"default","action":"set","text":"  fictional α  "}`, "  fictional α  ")
	assertValue("get-set", `{"scope":"default","action":"get"}`, "  fictional α  ")
	assertValue("clear", `{"scope":"default","action":"set","text":""}`, "")
	assertValue("get-cleared", `{"scope":"default","action":"get"}`, "")
}

func TestInstructionsErrorsAreTypedAndPreserveStoredValue(t *testing.T) {
	// R-8QUN-5SB8
	ctx := context.Background()
	db := migratedMCPDB(t, ctx)
	defer db.Close()
	store := wikidomain.NewScopeStore(db)
	h := gatedHandler(t, newTestHandler(t, WithScopeService(store)))
	call := func(id, args string) toolResult {
		rec := callMCP(t, h, fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"method":"tools/call","params":{"name":"instructions","arguments":%s}}`, id, args), "owner@example.com")
		return decodeToolResult(t, rec.Body.Bytes())
	}
	call("seed", `{"scope":"default","action":"set","text":"keep me"}`)
	for _, tc := range []struct {
		id, args, code, message string
	}{
		{"missing-text", `{"scope":"default","action":"set"}`, "validation", "text is required"},
		{"missing-scope", `{"scope":"ghost","action":"get"}`, "scope_not_found", "scopes"},
		{"too-long", fmt.Sprintf(`{"scope":"default","action":"set","text":%q}`, strings.Repeat("界", wikidomain.InstructionsCharCap+1)), "instructions_too_long", "4000"},
	} {
		got := call(tc.id, tc.args)
		if !got.IsError || got.StructuredContent["code"] != tc.code || !strings.Contains(fmt.Sprint(got.StructuredContent["message"]), tc.message) {
			t.Fatalf("%s result = %#v, want %s mentioning %q", tc.id, got, tc.code, tc.message)
		}
	}
	got := call("unchanged", `{"scope":"default","action":"get"}`)
	if got.IsError || got.StructuredContent["instructions"] != "keep me" {
		t.Fatalf("value after rejected set = %#v", got)
	}
}

func TestGuideAndInitializeDescribeScopeInstructionsWithoutExtraPointers(t *testing.T) {
	// R-8S2J-JK1X
	for _, phrase := range []string{"`instructions`", "override the base inference rules", "future inference", "`rerun`"} {
		if !strings.Contains(guideDoc, phrase) {
			t.Fatalf("guide omits %q", phrase)
		}
	}
	if !strings.Contains(strings.ToLower(Instructions), "scope instructions") {
		t.Fatalf("initialize instructions omit scope instructions: %q", Instructions)
	}
	for _, tool := range Tools(WithScopeService(testScopeService{})) {
		mentionsGuide := strings.Contains(strings.ToLower(tool.Description), "guide")
		if mentionsGuide != (tool.Name == "guide") {
			t.Fatalf("%s description guide pointer = %v", tool.Name, mentionsGuide)
		}
	}
}

func TestGuideAndInitializeDescribeCorrectionsWithoutExtraPointers(t *testing.T) {
	// R-7XMB-FI0R
	guide := strings.ToLower(guideDoc)
	for _, phrase := range []string{
		"state facts by ingesting prose",
		"explicit ruling",
		"plain assertions",
		"whether those claims were ingested before or after",
		"latest ruling wins",
		"newer statement of the fact",
		"suppression ledger",
		"suppressed_by",
		"re-digest a document under the current standing corrections",
	} {
		if !strings.Contains(guide, phrase) {
			t.Fatalf("guide omits corrections phrase %q", phrase)
		}
	}
	if !strings.Contains(strings.ToLower(Instructions), "corrections") {
		t.Fatalf("initialize instructions omit corrections: %q", Instructions)
	}
	for _, tool := range Tools() {
		mentionsGuide := strings.Contains(strings.ToLower(tool.Description), "guide")
		if mentionsGuide != (tool.Name == "guide") {
			t.Fatalf("%s description guide pointer = %v", tool.Name, mentionsGuide)
		}
	}
}

func TestDomainOutputSchemasMirrorRepresentativeStructuredResults(t *testing.T) {
	// R-ER7V-UFSU
	wiki := &capturingWiki{
		status:       jobStatus{Status: "done"},
		pathSubjects: map[string]subject{"entity/from": {ID: "from"}, "entity/to": {ID: "to"}},
		page:         page{Title: "Title", Body: "Body"},
	}
	tools := Tools(
		WithIngestService(wiki), WithJobStatusService(wiki), WithJobAbortService(wiki),
		WithJobRerunService(wiki), WithJobListService(wiki), WithJobsCountService(wiki),
		WithMergeService(wiki, wiki), WithMergeListService(wiki),
		WithAskFunc((&capturingAsker{}).Ask), WithSubjectListService(wiki),
		WithClaimListService(wiki), WithPagePathService(wiki),
	)
	args := map[string]string{
		"ingest": `{"scope":"default","text":"source"}`, "status": `{"job_id":"job"}`, "abort": `{"job_id":"job"}`,
		"rerun": `{"job_id":"job"}`, "jobs": `{"scope":"default"}`, "jobs_count": `{"scope":"default"}`,
		"merge": `{"scope":"default","from":"entity/from","to":"entity/to"}`, "merges": `{"scope":"default"}`,
		"ask": `{"scope":"default","question":"question"}`, "subjects": `{"scope":"default"}`, "claims": `{"scope":"default","subject":"entity/from"}`,
		"page": `{"scope":"default","subject":"entity/from"}`,
	}
	nested := map[string]map[string][]string{
		"jobs":     {"jobs": {"id", "owner_email", "owner_id", "title", "tags", "status", "received_at", "started_at", "finished_at", "error"}},
		"merges":   {"merges": {"norm_name", "subject_id", "name", "owner_email", "owner_id", "created_at"}},
		"subjects": {"subjects": {"path", "type", "name", "has_page"}},
		"claims":   {"claims": {"id", "kind", "text", "job", "suppressed"}},
		"ask":      {"citations": {"url", "title"}},
	}
	identity := server.Identity{OwnerEmail: "owner@example.com"}
	seen := 0
	for _, tool := range tools {
		raw, ok := args[tool.Name]
		if !ok {
			continue
		}
		seen++
		result, err := tool.Handler(context.Background(), json.RawMessage(raw), identity)
		if err != nil {
			t.Fatalf("%s handler: %v", tool.Name, err)
		}
		encoded, err := json.Marshal(result["structuredContent"])
		if err != nil {
			t.Fatalf("%s marshal structuredContent: %v", tool.Name, err)
		}
		var payload map[string]any
		decodeJSON(t, encoded, &payload)
		properties := tool.OutputSchema["properties"].(map[string]any)
		if !maps.EqualFunc(properties, payload, func(any, any) bool { return true }) {
			t.Fatalf("%s schema keys = %#v, payload keys = %#v", tool.Name, properties, payload)
		}
		for _, required := range stringValues(tool.OutputSchema["required"]) {
			if _, ok := payload[required]; !ok {
				t.Fatalf("%s required field %q missing from payload %#v", tool.Name, required, payload)
			}
		}
		for field, wantFields := range nested[tool.Name] {
			arraySchema := properties[field].(map[string]any)
			itemProperties := arraySchema["items"].(map[string]any)["properties"].(map[string]any)
			for _, want := range wantFields {
				if _, ok := itemProperties[want]; !ok {
					t.Fatalf("%s.%s item schema missing %s", tool.Name, field, want)
				}
			}
		}
	}
	if seen != 12 {
		t.Fatalf("exercised %d domain tools, want 12", seen)
	}
}

func TestDomainValidationErrorsCarryTypedWellFormedEnvelopes(t *testing.T) {
	// R-ESFS-87JJ
	// R-EXBD-RAIB
	wiki := &capturingWiki{pathSubjects: map[string]subject{
		"entity/one": {ID: "same"}, "entity/two": {ID: "same"},
	}}
	h := gatedHandler(t, newTestHandler(t,
		WithIngestService(wiki), WithJobStatusService(wiki), WithJobAbortService(wiki),
		WithJobRerunService(wiki), WithJobListService(wiki), WithMergeService(wiki, wiki),
		WithAskFunc((&capturingAsker{}).Ask), WithClaimListService(wiki), WithPagePathService(wiki),
	))
	cases := map[string]string{
		"malformed arguments": `{"name":"ingest","arguments":[]}`,
		"ingest text":         `{"name":"ingest","arguments":{"scope":"default"}}`,
		"merge from":          `{"name":"merge","arguments":{"scope":"default","to":"entity/two"}}`,
		"merge to":            `{"name":"merge","arguments":{"scope":"default","from":"entity/one"}}`,
		"status job":          `{"name":"status","arguments":{}}`,
		"abort job":           `{"name":"abort","arguments":{}}`,
		"rerun job":           `{"name":"rerun","arguments":{}}`,
		"ask question":        `{"name":"ask","arguments":{"scope":"default"}}`,
		"claims subject":      `{"name":"claims","arguments":{"scope":"default"}}`,
		"page subject":        `{"name":"page","arguments":{"scope":"default"}}`,
		"since":               `{"name":"jobs","arguments":{"scope":"default","since":"yesterday"}}`,
		"until":               `{"name":"jobs","arguments":{"scope":"default","until":"tomorrow"}}`,
		"cursor":              `{"name":"jobs","arguments":{"scope":"default","cursor":"invalid"}}`,
		"same subject":        `{"name":"merge","arguments":{"scope":"default","from":"entity/one","to":"entity/two"}}`,
	}
	closed := map[string]bool{"validation": true, "not_found": true, "conflict": true, "too_large": true, "source_unavailable": true, "internal": true}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			request := `{"jsonrpc":"2.0","id":"validation","method":"tools/call","params":` + params + `}`
			result := decodeToolResult(t, callMCP(t, h, request, "owner@example.com").Body.Bytes())
			code, _ := result.StructuredContent["code"].(string)
			if !result.IsError || code != "validation" {
				t.Fatalf("result = %#v, want validation error", result)
			}
			if len(result.Content) != 1 || result.Content[0].Type != "text" || strings.TrimSpace(result.Content[0].Text) == "" {
				t.Fatalf("content = %#v, want one human text message", result.Content)
			}
			if !closed[code] {
				t.Fatalf("code = %q, outside closed vocabulary", code)
			}
		})
	}
}

func TestDiscoveryGuideIsReferencedOnlyByInstructionsAndGuideDescription(t *testing.T) {
	// R-YHFY-RMZ2
	if !strings.Contains(strings.ToLower(Instructions), "guide") {
		t.Fatal("instructions do not point to guide")
	}

	references := 1
	for _, tool := range Tools(
		WithIngestService(&capturingWiki{}),
		WithJobStatusService(&capturingWiki{}),
		WithJobAbortService(&capturingWiki{}),
		WithJobRerunService(&capturingWiki{}),
		WithJobListService(&capturingWiki{}),
		WithJobsCountService(&capturingWiki{}),
		WithMergeService(&capturingWiki{}, &capturingWiki{}),
		WithMergeListService(&capturingWiki{}),
		WithAskFunc((&capturingAsker{}).Ask),
		WithSubjectListService(&capturingWiki{}),
		WithClaimListService(&capturingWiki{}),
		WithPagePathService(&capturingWiki{}),
	) {
		mentions := strings.Contains(strings.ToLower(tool.Description), "guide")
		if tool.Name == "guide" && !mentions {
			t.Fatal("guide description does not identify itself")
		}
		if tool.Name != "guide" && mentions {
			t.Fatalf("%s description unexpectedly points to guide", tool.Name)
		}
		if mentions {
			references++
		}
	}
	if references != 2 {
		t.Fatalf("guide reference locations = %d, want exactly 2", references)
	}
}

func TestToolsListInputSchemasUseValidRequiredFields(t *testing.T) {
	// R-N4KO-2WTZ
	// R-3G73-064M
	h := gatedHandler(t, newTestHandler(t,
		WithIngestService(&capturingWiki{}),
		WithJobStatusService(&capturingWiki{}),
		WithJobAbortService(&capturingWiki{}),
		WithJobRerunService(&capturingWiki{}),
		WithJobListService(&capturingWiki{}),
		WithJobsCountService(&capturingWiki{}),
		WithMergeService(&capturingWiki{}, &capturingWiki{}),
		WithMergeListService(&capturingWiki{}),
		WithAskFunc((&capturingAsker{}).Ask),
		WithSubjectListService(&capturingWiki{}),
		WithClaimListService(&capturingWiki{}),
		WithPagePathService(&capturingWiki{}),
	))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, rec.Body.Bytes(), &got)
	if len(got.Result.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}
	seenSubjects := false
	optionalOnly := map[string]bool{}
	for _, tool := range got.Result.Tools {
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("%s schema type = %v, want object", tool.Name, tool.InputSchema["type"])
		}
		required, ok := tool.InputSchema["required"]
		if _, tracked := optionalOnly[tool.Name]; tracked {
			optionalOnly[tool.Name] = true
			if ok {
				t.Fatalf("%s schema required = %#v, want omitted", tool.Name, required)
			}
			continue
		}
		if tool.Name == "subjects" {
			seenSubjects = true
		}
		if !ok {
			continue
		}
		values, ok := required.([]any)
		if !ok {
			t.Fatalf("%s schema required = %T, want array of strings", tool.Name, required)
		}
		if len(values) == 0 {
			t.Fatalf("%s schema required is empty, want omitted or non-empty", tool.Name)
		}
		for i, value := range values {
			if _, ok := value.(string); !ok {
				t.Fatalf("%s schema required[%d] = %T, want string", tool.Name, i, value)
			}
		}
	}
	if !seenSubjects {
		t.Fatal("tools/list missing subjects tool")
	}
	for name, seen := range optionalOnly {
		if !seen {
			t.Fatalf("tools/list missing %s tool", name)
		}
	}
}

func TestToolsListInputSchemasOmitAdditionalProperties(t *testing.T) {
	// R-1Y7B-TN7Y
	h := gatedHandler(t, newTestHandler(t,
		WithIngestService(&capturingWiki{}),
		WithJobStatusService(&capturingWiki{}),
		WithJobAbortService(&capturingWiki{}),
		WithJobRerunService(&capturingWiki{}),
		WithJobListService(&capturingWiki{}),
		WithJobsCountService(&capturingWiki{}),
		WithMergeService(&capturingWiki{}, &capturingWiki{}),
		WithMergeListService(&capturingWiki{}),
		WithAskFunc((&capturingAsker{}).Ask),
		WithSubjectListService(&capturingWiki{}),
		WithClaimListService(&capturingWiki{}),
		WithPagePathService(&capturingWiki{}),
	))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema any    `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, rec.Body.Bytes(), &got)
	if len(got.Result.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}

	var assertKeyAbsent func(t *testing.T, value any)
	assertKeyAbsent = func(t *testing.T, value any) {
		t.Helper()
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "additionalProperties" {
					t.Fatal("inputSchema contains additionalProperties")
				}
				assertKeyAbsent(t, child)
			}
		case []any:
			for _, child := range value {
				assertKeyAbsent(t, child)
			}
		}
	}
	for _, tool := range got.Result.Tools {
		t.Run(tool.Name, func(t *testing.T) {
			if tool.InputSchema == nil {
				t.Fatal("inputSchema is missing")
			}
			assertKeyAbsent(t, tool.InputSchema)
		})
	}
}

func TestReflectionToolReturnsEmptyEventEdges(t *testing.T) {
	h := gatedHandler(t, newTestHandler(t))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"reflection",
		"method":"tools/call",
		"params":{"name":"reflection","arguments":{}}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Publishes  []string `json:"publishes"`
		Subscribes []string `json:"subscribes"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if len(body.Publishes) != 0 || len(body.Subscribes) != 0 {
		t.Fatalf("reflection = %#v, want empty publishes/subscribes", body)
	}
}

func TestUnknownDomainResourcesReturnTypedNotFoundErrors(t *testing.T) {
	// R-ETNO-LZA8
	wiki := &capturingWiki{
		statusErr: sql.ErrNoRows,
		claimsErr: sql.ErrNoRows,
		pageErr:   sql.ErrNoRows,
		pathErrs:  map[string]error{"entity/missing": sql.ErrNoRows},
	}
	missing := missingJobs{}
	h := gatedHandler(t, newTestHandler(t,
		WithJobStatusService(wiki),
		WithJobAbortService(missing),
		WithJobRerunService(missing),
		WithClaimsService(wiki),
		WithPagePathService(wiki),
		WithMergeService(wiki, wiki),
	))
	for _, tc := range []struct {
		name string
		body string
		kind string
		id   string
	}{
		{
			name: "status",
			body: `{"jsonrpc":"2.0","id":"status","method":"tools/call","params":{"name":"status","arguments":{"job_id":"job-missing"}}}`,
			kind: "job",
			id:   "job-missing",
		},
		{
			name: "abort",
			body: `{"jsonrpc":"2.0","id":"abort","method":"tools/call","params":{"name":"abort","arguments":{"job_id":"job-missing"}}}`,
			kind: "job",
			id:   "job-missing",
		},
		{
			name: "rerun",
			body: `{"jsonrpc":"2.0","id":"rerun","method":"tools/call","params":{"name":"rerun","arguments":{"job_id":"job-missing"}}}`,
			kind: "job",
			id:   "job-missing",
		},
		{
			name: "claims",
			body: `{"jsonrpc":"2.0","id":"claims","method":"tools/call","params":{"name":"claims","arguments":{"scope":"default","subject":"entity/missing"}}}`,
			kind: "subject",
			id:   "entity/missing",
		},
		{
			name: "page",
			body: `{"jsonrpc":"2.0","id":"page","method":"tools/call","params":{"name":"page","arguments":{"scope":"default","subject":"entity/missing"}}}`,
			kind: "subject",
			id:   "entity/missing",
		},
		{
			name: "merge",
			body: `{"jsonrpc":"2.0","id":"merge","method":"tools/call","params":{"name":"merge","arguments":{"scope":"default","from":"entity/missing","to":"entity/other"}}}`,
			kind: "subject",
			id:   "entity/missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := callMCP(t, h, tc.body, "owner@example.com")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var raw struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			decodeJSON(t, rec.Body.Bytes(), &raw)
			if !raw.Result.IsError || raw.Result.StructuredContent["code"] != "not_found" {
				t.Fatalf("result = %#v, want typed not_found error", raw.Result)
			}
		})
	}
}

func TestIngestToolUsesAuthenticatedIdentity(t *testing.T) {
	// R-MVY0-XTAH
	// R-YINV-5EPR
	// R-EMCA-BCU2
	wiki := &capturingWiki{ingestID: "job-123"}
	h := gatedHandler(t, newTestHandler(t, WithIngestService(wiki)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"ingest",
		"method":"tools/call",
		"params":{
			"name":"ingest",
			"arguments":{"scope":"default","text":"source text","title":"Source","tags":["one","two"]}
		}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if wiki.ingestOwner != "owner@example.com" {
		t.Fatalf("ingest owner = %q, want authenticated owner", wiki.ingestOwner)
	}
	if wiki.ingestText != "source text" || wiki.ingestTitle != "Source" {
		t.Fatalf("ingest captured text/title = %q/%q", wiki.ingestText, wiki.ingestTitle)
	}
	if len(wiki.ingestTags) != 2 || wiki.ingestTags[0] != "one" || wiki.ingestTags[1] != "two" {
		t.Fatalf("ingest tags = %#v, want [one two]", wiki.ingestTags)
	}
	var body struct {
		JobID string `json:"job_id"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if body.JobID != "job-123" {
		t.Fatalf("job_id = %q, want job-123", body.JobID)
	}
	result := decodeToolResult(t, rec.Body.Bytes())
	want := map[string]any{"job_id": "job-123"}
	if !reflect.DeepEqual(result.StructuredContent, want) {
		t.Fatalf("structuredContent = %#v, want %#v", result.StructuredContent, want)
	}
	var mirrored map[string]any
	decodeJSON(t, []byte(result.Content[0].Text), &mirrored)
	if !reflect.DeepEqual(mirrored, want) {
		t.Fatalf("text content = %#v, want %#v", mirrored, want)
	}
}

func TestIngestStoresAndJobsExposeOwnerPairKeyedOnID(t *testing.T) {
	// R-1QO4-77EI
	// R-1T3W-YQVW
	ctx := context.Background()
	conn := migratedMCPDB(t, ctx)
	defer conn.Close()
	svc := wikidomain.NewService(conn, nil, nil, func() time.Time { return time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC) })
	h := gatedHandler(t, newTestHandler(t, WithIngestService(svc), WithJobListService(mcpJobLister{store: wikidomain.NewJobStore(conn)})))

	for i, email := range []string{"alice@example.com", ""} {
		rec := callMCPIdentity(t, h, `{"jsonrpc":"2.0","id":"ingest","method":"tools/call","params":{"name":"ingest","arguments":{"scope":"default","text":"source `+fmt.Sprint(i)+`"}}}`, "id-1", email)
		if rec.Code != http.StatusOK {
			t.Fatalf("ingest %d status = %d, want 200; body=%s", i, rec.Code, rec.Body.String())
		}
		var result struct {
			JobID string `json:"job_id"`
		}
		decodeToolText(t, rec.Body.Bytes(), &result)
		var ownerID, ownerEmail string
		if err := conn.QueryRowContext(ctx, `SELECT owner_id, owner_email FROM jobs WHERE id = ?`, result.JobID).Scan(&ownerID, &ownerEmail); err != nil {
			t.Fatalf("read job owner pair: %v", err)
		}
		if ownerID != "id-1" || ownerEmail != email {
			t.Fatalf("stored owner pair = %q/%q, want id-1/%q", ownerID, ownerEmail, email)
		}
	}

	rec := callMCPIdentity(t, h, `{"jsonrpc":"2.0","id":"jobs","method":"tools/call","params":{"name":"jobs","arguments":{"scope":"default","limit":10}}}`, "id-1", "alice@example.com")
	var page map[string]any
	decodeToolText(t, rec.Body.Bytes(), &page)
	items := page["jobs"].([]any)
	var item map[string]any
	for _, raw := range items {
		candidate := raw.(map[string]any)
		if candidate["owner_email"] == "alice@example.com" {
			item = candidate
		}
	}
	if item == nil {
		t.Fatalf("jobs page has no alice owner snapshot: %#v", page)
	}
	if item["owner_id"] != "id-1" || item["owner_email"] != "alice@example.com" {
		t.Fatalf("jobs owner pair = %#v, want id and email snapshot", item)
	}
	if _, exists := item["owner"]; exists {
		t.Fatalf("jobs item retained owner field: %#v", item)
	}
}

func TestMergeStoresAndMergesExposeOwnerPairKeyedOnID(t *testing.T) {
	// R-1RW0-KZ57
	// R-1VJP-QADA
	// R-E198-AY8Z
	ctx := context.Background()
	conn := migratedMCPDB(t, ctx)
	defer conn.Close()
	subjects := wikidomain.NewSubjectStore(conn)
	for _, subject := range []wikidomain.Subject{
		{ID: "winner-1", Name: "Winner One", Type: "entity"},
		{ID: "loser-1", Name: "Loser One", Type: "entity"},
		{ID: "winner-2", Name: "Winner Two", Type: "entity"},
		{ID: "loser-2", Name: "Loser Two", Type: "entity"},
	} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("save subject %s: %v", subject.ID, err)
		}
	}
	svc := wikidomain.NewService(conn, nil, ownerPairCompiler{}, func() time.Time { return time.Date(2026, 7, 19, 13, 0, 0, 0, time.UTC) })
	h := gatedHandler(t, newTestHandler(t,
		WithMergeService(subjects, svc),
		WithMergeListService(mcpMergeLister{store: wikidomain.NewAliasStore(conn)}),
	))

	for i, email := range []string{"alice@example.com", ""} {
		n := i + 1
		word := []string{"one", "two"}[i]
		rec := callMCPIdentity(t, h, `{"jsonrpc":"2.0","id":"merge","method":"tools/call","params":{"name":"merge","arguments":{"scope":"default","from":"entity/loser-`+word+`","to":"entity/winner-`+word+`"}}}`, "id-1", email)
		if rec.Code != http.StatusOK {
			t.Fatalf("merge %d status = %d, want 200; body=%s", n, rec.Code, rec.Body.String())
		}
		var result struct {
			JobID string `json:"job_id"`
		}
		decodeToolText(t, rec.Body.Bytes(), &result)
		var ownerID, ownerEmail string
		if err := conn.QueryRowContext(ctx, `SELECT owner_id, owner_email FROM jobs WHERE id = ?`, result.JobID).Scan(&ownerID, &ownerEmail); err != nil {
			t.Fatalf("read merge job owner pair: %v", err)
		}
		if ownerID != "id-1" || ownerEmail != email {
			t.Fatalf("merge job owner pair = %q/%q, want id-1/%q", ownerID, ownerEmail, email)
		}
		if processed, err := svc.ProcessNext(ctx); err != nil || !processed {
			t.Fatalf("ProcessNext = %v/%v, want true/nil", processed, err)
		}
		alias, err := wikidomain.NewAliasStore(conn).GetByNormName(ctx, fmt.Sprintf("Loser %s", []string{"One", "Two"}[i]))
		if err != nil {
			t.Fatalf("read alias: %v", err)
		}
		if alias.OwnerID != "id-1" || alias.OwnerEmail != email {
			t.Fatalf("alias owner pair = %q/%q, want id-1/%q", alias.OwnerID, alias.OwnerEmail, email)
		}
	}

	rec := callMCPIdentity(t, h, `{"jsonrpc":"2.0","id":"merges","method":"tools/call","params":{"name":"merges","arguments":{"scope":"default","limit":2}}}`, "id-1", "alice@example.com")
	var page map[string]any
	decodeToolText(t, rec.Body.Bytes(), &page)
	var item map[string]any
	for _, raw := range page["merges"].([]any) {
		candidate := raw.(map[string]any)
		if candidate["owner_email"] == "alice@example.com" {
			item = candidate
		}
	}
	if item == nil || item["owner_id"] != "id-1" {
		t.Fatalf("merges owner pair = %#v, want alice/id-1", page)
	}
	if _, exists := item["created_by"]; exists {
		t.Fatalf("merges item retained created_by: %#v", item)
	}
}

func TestOpaqueDomainFailureReturnsInternalError(t *testing.T) {
	// R-EW3H-DIRM
	h := gatedHandler(t, newTestHandler(t, WithIngestService(failingIngest{err: errors.New("backend unavailable")})))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"ingest","method":"tools/call","params":{"name":"ingest","arguments":{"scope":"default","text":"source"}}}`, "owner@example.com")
	result := decodeToolResult(t, rec.Body.Bytes())
	if !result.IsError || result.StructuredContent["code"] != "internal" {
		t.Fatalf("result = %#v, want typed internal error", result)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "backend unavailable") {
		t.Fatalf("content = %#v, want opaque backend message", result.Content)
	}
}

func TestJobStatusToolReturnsDomainStatus(t *testing.T) {
	// R-MX5X-BL16
	// R-YINV-5EPR
	received := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	finished := received.Add(3 * time.Second)
	wiki := &capturingWiki{status: jobStatus{
		ID:         "job-123",
		Status:     "done",
		ReceivedAt: received,
		FinishedAt: &finished,
		Subjects:   []string{"subject-1"},
	}}
	h := gatedHandler(t, newTestHandler(t, WithJobStatusService(wiki)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"status",
		"method":"tools/call",
		"params":{"name":"status","arguments":{"job_id":"job-123"}}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if wiki.statusJobID != "job-123" {
		t.Fatalf("status job id = %q, want job-123", wiki.statusJobID)
	}
	var body jobStatus
	decodeToolText(t, rec.Body.Bytes(), &body)
	if body.ID != "" || body.Status != "done" {
		t.Fatalf("job status = %#v, want done with no serialized job id", body)
	}
	if len(body.Subjects) != 1 || body.Subjects[0] != "subject-1" {
		t.Fatalf("subjects = %#v, want [subject-1]", body.Subjects)
	}
}

func TestPageToolUsesTypeNormNamePath(t *testing.T) {
	// R-01OQ-Y5YV
	// R-YINV-5EPR
	wiki := &capturingWiki{page: page{
		ID:        "page-1",
		SubjectID: "subject-1",
		Title:     "Acme Robotics",
		Body:      "Acme Robotics overview.",
	}}
	h := gatedHandler(t, newTestHandler(t, WithPagePathService(wiki)))
	listRec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")
	if listRec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", listRec.Code)
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, listRec.Body.Bytes(), &list)
	foundPageSchema := false
	for _, tool := range list.Result.Tools {
		if tool.Name != "page" {
			continue
		}
		foundPageSchema = true
		required, ok := tool.InputSchema["required"].([]any)
		if !ok || len(required) != 2 || required[0] != "scope" || required[1] != "subject" {
			t.Fatalf("page required = %#v, want [scope subject]", tool.InputSchema["required"])
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("page properties = %T, want object", tool.InputSchema["properties"])
		}
		subject, ok := properties["subject"].(map[string]any)
		if !ok || subject["type"] != "string" {
			t.Fatalf("page subject schema = %#v, want string property", properties["subject"])
		}
		for _, forbidden := range []string{"path", "subject_id"} {
			if _, ok := properties[forbidden]; ok {
				t.Fatalf("page properties = %#v, want no %s argument", properties, forbidden)
			}
		}
	}
	if !foundPageSchema {
		t.Fatal("tools/list missing page schema")
	}

	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"page",
		"method":"tools/call",
		"params":{"name":"page","arguments":{"scope":"default","subject":"entity/acme-robotics"}}
	}`, "owner@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("page status = %d, want 200", rec.Code)
	}
	if wiki.pagePath != "entity/acme-robotics" {
		t.Fatalf("page path = %q, want type/norm_name path", wiki.pagePath)
	}
	var body struct {
		Subject string `json:"subject"`
		Title   string `json:"title"`
		Body    string `json:"body"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if body.Subject != "entity/acme-robotics" || body.Title != "Acme Robotics" {
		t.Fatalf("page body = %#v, want returned page", body)
	}
}

func TestReadToolsSerializePublicPathsWithoutSubjectIDs(t *testing.T) {
	// R-03GW-PX5K
	// R-YINV-5EPR
	internalSubjectID := "01HZX4Q0SUBJECTULID00000001"
	wiki := &capturingWiki{
		status: jobStatus{
			ID:       "job-123",
			Status:   "done",
			Subjects: []string{"entity/acme-robotics"},
		},
		subjects: []subject{{
			ID:       internalSubjectID,
			Name:     "Acme Robotics",
			NormName: "acme-robotics",
			Type:     "entity",
		}},
		claims: []claim{{
			ID:        "claim-1",
			SubjectID: internalSubjectID,
			JobID:     "job-123",
			Body:      "Acme Robotics runs a Tulsa lab.",
		}},
		page: page{
			ID:        "page-1",
			SubjectID: internalSubjectID,
			Title:     "Acme Robotics",
			Body:      "Acme Robotics overview.",
		},
	}
	h := gatedHandler(t, newTestHandler(t,
		WithJobStatusService(wiki),
		WithSubjectsService(wiki),
		WithClaimsService(wiki),
		WithPagePathService(wiki),
	))

	for _, tc := range []struct {
		name    string
		request string
		check   func(t *testing.T, text string)
	}{
		{
			name: "status",
			request: `{"jsonrpc":"2.0","id":"status","method":"tools/call","params":{
				"name":"status","arguments":{"job_id":"job-123"}
			}}`,
			check: func(t *testing.T, text string) {
				t.Helper()
				var body struct {
					Status   string   `json:"status"`
					Subjects []string `json:"subjects"`
				}
				decodeJSON(t, []byte(text), &body)
				if strings.Contains(text, `"job_id"`) {
					t.Fatalf("status body = %s, want no serialized job_id", text)
				}
				if body.Status != "done" {
					t.Fatalf("status body = %#v, want status field", body)
				}
				if len(body.Subjects) != 1 || body.Subjects[0] != "entity/acme-robotics" {
					t.Fatalf("status subjects = %#v, want public path", body.Subjects)
				}
			},
		},
		{
			name: "subjects",
			request: `{"jsonrpc":"2.0","id":"subjects","method":"tools/call","params":{
				"name":"subjects","arguments":{"scope":"default","type":"entity","name":"acme"}
			}}`,
			check: func(t *testing.T, text string) {
				t.Helper()
				var body struct {
					Subjects []struct {
						Path    string `json:"path"`
						Name    string `json:"name"`
						Type    string `json:"type"`
						HasPage bool   `json:"has_page"`
					} `json:"subjects"`
					Next string `json:"next_cursor"`
				}
				decodeJSON(t, []byte(text), &body)
				if !strings.Contains(text, `"has_page"`) {
					t.Fatalf("subjects body = %s, want has_page field", text)
				}
				if len(body.Subjects) != 1 || body.Subjects[0].Path != "entity/acme-robotics" || body.Subjects[0].Name != "Acme Robotics" || body.Subjects[0].Type != "entity" || body.Next != "" {
					t.Fatalf("subjects body = %#v, want public path/name/type", body)
				}
			},
		},
		{
			name: "claims",
			request: `{"jsonrpc":"2.0","id":"claims","method":"tools/call","params":{
				"name":"claims","arguments":{"scope":"default","subject":"entity/acme-robotics"}
			}}`,
			check: func(t *testing.T, text string) {
				t.Helper()
				var body struct {
					Claims []struct {
						ID   string `json:"id"`
						Text string `json:"text"`
						Job  string `json:"job"`
					} `json:"claims"`
					Next string `json:"next_cursor"`
				}
				decodeJSON(t, []byte(text), &body)
				for _, forbidden := range []string{`"path"`, `"body"`} {
					if strings.Contains(text, forbidden) {
						t.Fatalf("claims body = %s, leaked %s", text, forbidden)
					}
				}
				if len(body.Claims) != 1 || body.Claims[0].ID != "claim-1" || body.Claims[0].Text != "Acme Robotics runs a Tulsa lab." || body.Claims[0].Job != "job-123" || body.Next != "" {
					t.Fatalf("claims body = %#v, want public id/text/job", body)
				}
			},
		},
		{
			name: "page",
			request: `{"jsonrpc":"2.0","id":"page","method":"tools/call","params":{
				"name":"page","arguments":{"scope":"default","subject":"entity/acme-robotics"}
			}}`,
			check: func(t *testing.T, text string) {
				t.Helper()
				var body struct {
					Subject string `json:"subject"`
					Title   string `json:"title"`
					Body    string `json:"body"`
				}
				decodeJSON(t, []byte(text), &body)
				if strings.Contains(text, `"path"`) {
					t.Fatalf("page body = %s, want subject field instead of path", text)
				}
				if body.Subject != "entity/acme-robotics" || body.Title != "Acme Robotics" || body.Body != "Acme Robotics overview." {
					t.Fatalf("page body = %#v, want public path/title/body", body)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := callMCP(t, h, tc.request, "owner@example.com")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			text := toolTextString(t, rec.Body.Bytes())
			if tc.name != "claims" && !strings.Contains(text, "entity/acme-robotics") {
				t.Fatalf("tool text = %s, want public path", text)
			}
			for _, forbidden := range []string{internalSubjectID, "SubjectID", "subject_id", "NormName", "norm_name", "job_id"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("tool text = %s, leaked %q", text, forbidden)
				}
			}
			tc.check(t, text)
		})
	}
}

func TestAskToolUsesAuthenticatedIdentity(t *testing.T) {
	// R-MYDT-PCRV
	asker := &capturingAsker{answer: answer{
		Found: true,
		Text:  "Ada wrote the note.",
		Citations: []citation{{
			Path:  "entity/ada",
			Title: "Ada",
		}},
	}}
	h := gatedHandler(t, newTestHandler(t, WithAskFunc(asker.Ask)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"ask",
		"method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"Who wrote it?"}}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if asker.owner != "owner@example.com" {
		t.Fatalf("ask owner = %q, want authenticated owner", asker.owner)
	}
	if asker.question != "Who wrote it?" {
		t.Fatalf("ask question = %q, want forwarded question", asker.question)
	}
	var body struct {
		Found     bool   `json:"found"`
		Answer    string `json:"answer"`
		Citations []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"citations"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if !body.Found || body.Answer != "Ada wrote the note." {
		t.Fatalf("answer = %#v, want found Ada answer", body)
	}
	if len(body.Citations) != 1 || body.Citations[0].URL != "https://int.ikigenba.com/srv/wiki/subject/entity/ada" {
		t.Fatalf("citations = %#v, want front-door Ada citation", body.Citations)
	}
}

func TestAskToolReturnsURLTitleCitations(t *testing.T) {
	// R-044J-PPG9
	asker := &capturingAsker{answer: answer{
		Found: true,
		Text:  "Ada wrote the note.",
		Citations: []citation{{
			Path:  "person/ada-lovelace",
			Title: "Ada Lovelace",
		}},
	}}
	h := gatedHandler(t, newTestHandler(t, WithAskFunc(asker.Ask)))
	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"ask",
		"method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"Who wrote it?"}}
	}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d, want 200", rec.Code)
	}
	var body struct {
		Found     bool   `json:"found"`
		Answer    string `json:"answer"`
		Citations []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Subject string `json:"subject"`
		} `json:"citations"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if !body.Found || body.Answer != "Ada wrote the note." {
		t.Fatalf("ask body = %#v, want found answer text", body)
	}
	if len(body.Citations) != 1 || body.Citations[0].URL != "https://int.ikigenba.com/srv/wiki/subject/person/ada-lovelace" || body.Citations[0].Title != "Ada Lovelace" {
		t.Fatalf("ask citations = %#v, want URL/title citation", body.Citations)
	}
	if body.Citations[0].Subject != "" {
		t.Fatalf("ask citation subject = %q, want omitted internal subject id", body.Citations[0].Subject)
	}
}

func TestAskToolUsesPhase17InputAndResultShape(t *testing.T) {
	asker := &capturingAsker{answer: answer{
		Found: true,
		Text:  "Ada wrote the note.",
		Citations: []citation{{
			Path:  "person/ada",
			Title: "Ada",
		}},
	}}
	h := gatedHandler(t, newTestHandler(t, WithAskFunc(asker.Ask)))
	listRec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")
	if listRec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", listRec.Code)
	}
	var list struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, listRec.Body.Bytes(), &list)
	foundAskSchema := false
	for _, tool := range list.Result.Tools {
		if tool.Name != "ask" {
			continue
		}
		foundAskSchema = true
		required, ok := tool.InputSchema["required"].([]any)
		if !ok || len(required) != 2 || required[0] != "scope" || required[1] != "question" {
			t.Fatalf("ask required = %#v, want [scope question]", tool.InputSchema["required"])
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("ask properties = %T, want object", tool.InputSchema["properties"])
		}
		question, ok := properties["question"].(map[string]any)
		if !ok || question["type"] != "string" {
			t.Fatalf("ask question schema = %#v, want string property", properties["question"])
		}
	}
	if !foundAskSchema {
		t.Fatal("tools/list missing ask schema")
	}

	rec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"ask",
		"method":"tools/call",
		"params":{"name":"ask","arguments":{"scope":"default","question":"Who wrote it?"}}
	}`, "owner@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d, want 200", rec.Code)
	}
	var body struct {
		Found     bool   `json:"found"`
		Answer    string `json:"answer"`
		Citations []struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"citations"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if !body.Found || body.Answer != "Ada wrote the note." {
		t.Fatalf("ask body = %#v, want found answer text", body)
	}
	if len(body.Citations) != 1 || body.Citations[0].URL != "https://int.ikigenba.com/srv/wiki/subject/person/ada" || body.Citations[0].Title != "Ada" {
		t.Fatalf("ask citations = %#v, want URL/title citation", body.Citations)
	}
}

func TestAskToolCitationContainsURLAndTitleWithoutPath(t *testing.T) {
	// R-Y7OR-PH1I
	result := askToolResult(answer{
		Found: true,
		Text:  "The TSR is documented.",
		Citations: []citation{{
			Path:  "entity/tsr",
			Title: "TSR",
		}},
	}, "https://acct.ikigenba.com/srv/wiki/")

	citations, ok := result["citations"].([]map[string]string)
	if !ok || len(citations) != 1 {
		t.Fatalf("citations = %#v, want one citation", result["citations"])
	}
	want := map[string]string{
		"url":   "https://acct.ikigenba.com/srv/wiki/entity/tsr",
		"title": "TSR",
	}
	if !maps.Equal(citations[0], want) {
		t.Fatalf("citation = %#v, want %#v", citations[0], want)
	}
	if _, exists := citations[0]["path"]; exists {
		t.Fatalf("citation = %#v, path must not be emitted", citations[0])
	}
}

func TestAskCitationTierTracksCurrentScopeVisibility(t *testing.T) {
	// R-I0JC-7NLD
	ctx := context.Background()
	db := migratedMCPDB(t, ctx)
	defer db.Close()
	store := wikidomain.NewScopeStore(db)
	asker := &capturingAsker{answer: answer{
		Found: true,
		Text:  "The TSR is documented.",
		Citations: []citation{{
			Path:  "entity/tsr",
			Title: "TSR",
		}},
	}}
	h := gatedHandler(t, newTestHandlerWithAuthServer(t, "https://acct.ikigenba.com/", WithScopeService(store), WithAskFunc(asker.Ask)))
	askURL := func() string {
		t.Helper()
		rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"ask","method":"tools/call","params":{"name":"ask","arguments":{"scope":"default","question":"Where is the TSR?"}}}`, "owner@example.com")
		var body struct {
			Citations []struct {
				URL string `json:"url"`
			} `json:"citations"`
		}
		decodeToolText(t, rec.Body.Bytes(), &body)
		if len(body.Citations) != 1 {
			t.Fatalf("citations = %#v, want one", body.Citations)
		}
		return body.Citations[0].URL
	}
	if got, want := askURL(), "https://acct.ikigenba.com/srv/wiki/private/default/subject/entity/tsr"; got != want {
		t.Fatalf("private citation URL = %q, want %q", got, want)
	}
	flip := callMCP(t, h, `{"jsonrpc":"2.0","id":"visibility","method":"tools/call","params":{"name":"scope_set_visibility","arguments":{"name":"default","visibility":"public"}}}`, "owner@example.com")
	if flip.Code != http.StatusOK {
		t.Fatalf("scope_set_visibility status = %d, want 200; body=%s", flip.Code, flip.Body.String())
	}
	if got, want := askURL(), "https://acct.ikigenba.com/srv/wiki/public/default/subject/entity/tsr"; got != want {
		t.Fatalf("public citation URL = %q, want %q", got, want)
	}
}

func TestAskToolCitationURLUsesFrontDoorSubjectRoute(t *testing.T) {
	// R-HOJB-ZR3T
	tests := []struct {
		name       string
		authServer string
		wantURL    string
	}{
		{name: "production", authServer: "https://acct.ikigenba.com", wantURL: "https://acct.ikigenba.com/srv/wiki/subject/entity/tsr"},
		{name: "local", authServer: "http://localhost:8080", wantURL: "http://localhost:8080/srv/wiki/subject/entity/tsr"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			asker := &capturingAsker{answer: answer{
				Found:     true,
				Text:      "The TSR is documented.",
				Citations: []citation{{Path: "entity/tsr", Title: "TSR"}},
			}}
			h := gatedHandler(t, newTestHandlerWithAuthServer(t, tc.authServer, WithAskFunc(asker.Ask)))
			rec := callMCP(t, h, `{
				"jsonrpc":"2.0",
				"id":"ask",
				"method":"tools/call",
				"params":{"name":"ask","arguments":{"scope":"default","question":"What is the TSR?"}}
			}`, "owner@example.com")
			if rec.Code != http.StatusOK {
				t.Fatalf("ask status = %d, want 200", rec.Code)
			}
			var body struct {
				Citations []struct {
					URL string `json:"url"`
				} `json:"citations"`
			}
			decodeToolText(t, rec.Body.Bytes(), &body)
			if len(body.Citations) != 1 || body.Citations[0].URL != tc.wantURL {
				t.Fatalf("citations = %#v, want URL %q", body.Citations, tc.wantURL)
			}
		})
	}
}

func TestAskToolLinkifiesFirstSubjectMentionAndKeepsCitations(t *testing.T) {
	// R-8DB1-UI1Q
	ctx := context.Background()
	conn := migratedMCPDB(t, ctx)
	defer conn.Close()
	subjects := wikidomain.NewSubjectStore(conn)
	pages := wikidomain.NewPageStore(conn)
	acme := wikidomain.Subject{ID: "subject-acme", Name: "Acme Corp", Type: "entity"}
	if err := subjects.Save(ctx, acme); err != nil {
		t.Fatalf("Save subject: %v", err)
	}
	if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-acme", SubjectID: acme.ID, Title: acme.Name, Body: "Acme Corp profile."}); err != nil {
		t.Fatalf("Upsert page: %v", err)
	}
	provider := &mcpScriptedProvider{responses: []string{
		`{"sub_queries":["Acme Corp"]}`,
		`{"found":true,"text":"Acme Corp builds widgets. Acme Corp is based here.","citations":[{"path":"entity/acme-corp","title":"Acme Corp"}]}`,
	}}
	asker := ask.New(
		&mcpScriptedSearch{result: retrieve.Result{Hits: []retrieve.Hit{{PageID: acme.ID, Path: "entity/acme-corp", Title: acme.Name}}, TopDense: 0.8}},
		subjects,
		pages,
		llmtest.NewClient(t, provider),
		llm.CallSite{Config: llm.Config{Model: "ask-subject-test"}},
		llm.CallSite{Config: llm.Config{Model: "ask-synthesis-test"}},
	)
	h := gatedHandler(t, newTestHandlerWithAuthServer(t, "https://acct.ikigenba.com", WithAskFunc(asker.Ask), WithMentionLinkifier(wikidomain.NewService(conn, nil, nil, nil))))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"ask","method":"tools/call","params":{"name":"ask","arguments":{"scope":"default","question":"What does Acme Corp do?"}}}`, "owner@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("ask status = %d, want 200", rec.Code)
	}
	var body struct {
		Answer    string `json:"answer"`
		Citations []struct {
			URL string `json:"url"`
		} `json:"citations"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if !strings.Contains(body.Answer, "[Acme Corp](https://acct.ikigenba.com/srv/wiki/subject/entity/acme-corp)") {
		t.Fatalf("answer = %q, want inline subject link", body.Answer)
	}
	if len(body.Citations) != 1 || body.Citations[0].URL != "https://acct.ikigenba.com/srv/wiki/subject/entity/acme-corp" {
		t.Fatalf("citations = %#v, want unchanged front-door citation", body.Citations)
	}
}

func TestPageToolLinkifiesOtherSubjectBeforeFooterAndExcludesSelf(t *testing.T) {
	// R-8EIY-89SF
	ctx := context.Background()
	conn := migratedMCPDB(t, ctx)
	defer conn.Close()
	subjects := wikidomain.NewSubjectStore(conn)
	pages := wikidomain.NewPageStore(conn)
	a := wikidomain.Subject{ID: "subject-a", Name: "Atlas", Type: "entity"}
	b := wikidomain.Subject{ID: "subject-b", Name: "Borealis", Type: "entity"}
	for _, subject := range []wikidomain.Subject{a, b} {
		if err := subjects.Save(ctx, subject); err != nil {
			t.Fatalf("Save subject %s: %v", subject.ID, err)
		}
	}
	if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-a", SubjectID: a.ID, Title: a.Name, Body: "Atlas works with Borealis."}); err != nil {
		t.Fatalf("Upsert page A: %v", err)
	}
	if err := pages.Upsert(ctx, wikidomain.Page{ID: "page-b", SubjectID: b.ID, Title: b.Name, Body: "Borealis profile."}); err != nil {
		t.Fatalf("Upsert page B: %v", err)
	}
	svc := wikidomain.NewService(conn, nil, nil, nil)
	h := gatedHandler(t, newTestHandlerWithAuthServer(t, "https://acct.ikigenba.com", WithPagePathService(mcpPageService{resolver: wikidomain.NewResolver(conn), service: svc}), WithMentionLinkifier(svc)))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"page","method":"tools/call","params":{"name":"page","arguments":{"scope":"default","subject":"entity/atlas"}}}`, "owner@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("page status = %d, want 200", rec.Code)
	}
	var body struct {
		Body string `json:"body"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if !strings.Contains(body.Body, "Atlas works with [Borealis](https://acct.ikigenba.com/srv/wiki/subject/entity/borealis).") {
		t.Fatalf("body = %q, want inline link for other subject", body.Body)
	}
	if strings.Contains(body.Body, "[Atlas](https://acct.ikigenba.com/srv/wiki/subject/entity/atlas)") {
		t.Fatalf("body = %q, must exclude page subject from inline links", body.Body)
	}
	if !strings.Contains(body.Body, "\n\n---\n\n## Links\n\n### Mentions\n- [Borealis](entity/borealis)") || !strings.Contains(body.Body, "\n\n### Mentioned by\n") {
		t.Fatalf("body = %q, want D12 footer after linkified prose", body.Body)
	}
}

func TestAskResultCompositionLeavesSourceCitationPathRelative(t *testing.T) {
	// R-YA4K-H0IW
	answer := answer{
		Found:     true,
		Text:      "The TSR is documented.",
		Citations: []citation{{Path: "entity/tsr", Title: "TSR"}},
	}

	result := askToolResult(answer, "https://acct.ikigenba.com/srv/wiki/")
	if got := answer.Citations[0].Path; got != "entity/tsr" {
		t.Fatalf("source citation path = %q, want bare relative path", got)
	}
	citations := result["citations"].([]map[string]string)
	if got := citations[0]["url"]; got != "https://acct.ikigenba.com/srv/wiki/entity/tsr" {
		t.Fatalf("MCP citation URL = %q, want composed front-door URL", got)
	}
}

func TestJobControlToolsCallDomainServices(t *testing.T) {
	// R-38VO-PJOG
	// R-YINV-5EPR
	wiki := &capturingWiki{
		abortResult: abortResult{Aborted: true, Status: "aborted"},
		rerunResult: rerunResult{Requeued: true, Status: "pending"},
	}
	h := gatedHandler(t, newTestHandler(t,
		WithJobAbortService(wiki),
		WithJobRerunService(wiki),
	))

	abortRec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"abort",
		"method":"tools/call",
		"params":{"name":"abort","arguments":{"job_id":"job-123"}}
	}`, "owner@example.com")
	if abortRec.Code != http.StatusOK {
		t.Fatalf("abort status = %d, want 200", abortRec.Code)
	}
	if wiki.abortJobID != "job-123" {
		t.Fatalf("abort job id = %q, want job-123", wiki.abortJobID)
	}
	var abortBody struct {
		Aborted bool   `json:"aborted"`
		Status  string `json:"status"`
	}
	decodeToolText(t, abortRec.Body.Bytes(), &abortBody)
	if !abortBody.Aborted || abortBody.Status != "aborted" {
		t.Fatalf("abort body = %#v, want aborted result", abortBody)
	}

	rerunRec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"rerun",
		"method":"tools/call",
		"params":{"name":"rerun","arguments":{"job_id":"job-456"}}
	}`, "owner@example.com")
	if rerunRec.Code != http.StatusOK {
		t.Fatalf("rerun status = %d, want 200", rerunRec.Code)
	}
	if wiki.rerunJobID != "job-456" {
		t.Fatalf("rerun job id = %q, want job-456", wiki.rerunJobID)
	}
	var rerunBody struct {
		Requeued bool   `json:"requeued"`
		Status   string `json:"status"`
	}
	decodeToolText(t, rerunRec.Body.Bytes(), &rerunBody)
	if !rerunBody.Requeued || rerunBody.Status != "pending" {
		t.Fatalf("rerun body = %#v, want requeued result", rerunBody)
	}
}

func TestJobsCountUsesSameFiltersAsJobsAndReturnsOnlyCount(t *testing.T) {
	// R-Y36L-E3W6
	// R-37NS-BRXR
	// R-YINV-5EPR
	started := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	wiki := &capturingWiki{
		jobs: []job{
			{ID: "job-done", Status: "done", ReceivedAt: started},
			{ID: "job-failed", Status: "failed", ReceivedAt: started.Add(-time.Hour)},
		},
		jobCount: 2,
	}
	h := gatedHandler(t, newTestHandler(t,
		WithJobListService(wiki),
		WithJobsCountService(wiki),
	))
	args := `"arguments":{"scope":"default","status":["done","failed"],"since":"2026-06-22T00:00:00Z","until":"2026-06-23T00:00:00Z"}`

	jobsRec := callMCP(t, h, `{"jsonrpc":"2.0","id":"jobs","method":"tools/call","params":{"name":"jobs",`+args+`}}`, "owner@example.com")
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("jobs status = %d, want 200", jobsRec.Code)
	}
	var jobsBody struct {
		Jobs []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"jobs"`
		Next string `json:"next_cursor"`
	}
	decodeToolText(t, jobsRec.Body.Bytes(), &jobsBody)
	if len(wiki.jobFilter.Statuses) != 2 || wiki.jobFilter.Statuses[0] != "done" || wiki.jobFilter.Statuses[1] != "failed" {
		t.Fatalf("jobs statuses = %#v, want done+failed", wiki.jobFilter.Statuses)
	}
	if len(jobsBody.Jobs) != 2 || jobsBody.Jobs[0].ID != "job-done" || jobsBody.Jobs[1].Status != "failed" {
		t.Fatalf("jobs body = %#v, want filtered job items", jobsBody)
	}

	countRec := callMCP(t, h, `{"jsonrpc":"2.0","id":"count","method":"tools/call","params":{"name":"jobs_count",`+args+`}}`, "owner@example.com")
	if countRec.Code != http.StatusOK {
		t.Fatalf("jobs_count status = %d, want 200", countRec.Code)
	}
	var countBody map[string]any
	decodeToolText(t, countRec.Body.Bytes(), &countBody)
	if countBody["count"] != float64(2) {
		t.Fatalf("jobs_count body = %#v, want count 2", countBody)
	}
	if _, ok := countBody["jobs"]; ok {
		t.Fatalf("jobs_count body = %#v, want no jobs array", countBody)
	}
	if _, ok := countBody["next_cursor"]; ok {
		t.Fatalf("jobs_count body = %#v, want no cursor", countBody)
	}
	if len(wiki.jobCountFilter.Statuses) != 2 || wiki.jobCountFilter.Statuses[0] != "done" || wiki.jobCountFilter.Statuses[1] != "failed" {
		t.Fatalf("jobs_count statuses = %#v, want done+failed", wiki.jobCountFilter.Statuses)
	}
	if !wiki.jobCountFilter.Since.Equal(wiki.jobFilter.Since) || !wiki.jobCountFilter.Until.Equal(wiki.jobFilter.Until) {
		t.Fatalf("jobs_count filter = %#v, want same time bounds as jobs %#v", wiki.jobCountFilter, wiki.jobFilter)
	}
}

func TestJobsKindFilterDefaultsToIngestAndAcceptsMerge(t *testing.T) {
	// R-DWDM-RVA7
	// R-DYTF-JERL
	wiki := &capturingWiki{jobCount: 1}
	h := gatedHandler(t, newTestHandler(t,
		WithJobListService(wiki),
		WithJobsCountService(wiki),
	))

	jobsRec := callMCP(t, h, `{"jsonrpc":"2.0","id":"jobs","method":"tools/call","params":{"name":"jobs","arguments":{"scope":"default"}}}`, "owner@example.com")
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("jobs status = %d, want 200", jobsRec.Code)
	}
	if len(wiki.jobFilter.Kinds) != 1 || wiki.jobFilter.Kinds[0] != "ingest" {
		t.Fatalf("jobs kind filter = %#v, want default ingest", wiki.jobFilter.Kinds)
	}

	countRec := callMCP(t, h, `{"jsonrpc":"2.0","id":"count","method":"tools/call","params":{"name":"jobs_count","arguments":{"scope":"default","kind":["merge"]}}}`, "owner@example.com")
	if countRec.Code != http.StatusOK {
		t.Fatalf("jobs_count status = %d, want 200", countRec.Code)
	}
	if len(wiki.jobCountFilter.Kinds) != 1 || wiki.jobCountFilter.Kinds[0] != "merge" {
		t.Fatalf("jobs_count kind filter = %#v, want merge", wiki.jobCountFilter.Kinds)
	}
}

func TestJobsKindSchemaPublishesEnumAndRejectsUnknownKind(t *testing.T) {
	h := gatedHandler(t, newTestHandler(t,
		WithJobListService(&capturingWiki{}),
		WithJobsCountService(&capturingWiki{}),
	))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, rec.Body.Bytes(), &got)
	for _, name := range []string{"jobs", "jobs_count"} {
		schema := toolSchema(t, got.Result.Tools, name)
		properties := schema["properties"].(map[string]any)
		kind := properties["kind"].(map[string]any)
		items := kind["items"].(map[string]any)
		enum := items["enum"].([]any)
		if kind["type"] != "array" || len(enum) != 2 {
			t.Fatalf("%s kind schema = %#v, want array enum of two kinds", name, kind)
		}
		for _, want := range []string{"ingest", "merge"} {
			if !containsJSONValue(enum, want) {
				t.Fatalf("%s kind enum = %#v, missing %s", name, enum, want)
			}
		}
	}

	for _, name := range []string{"jobs", "jobs_count"} {
		rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"bad","method":"tools/call","params":{"name":"`+name+`","arguments":{"scope":"default","kind":["compact"]}}}`, "owner@example.com")
		text := toolTextString(t, rec.Body.Bytes())
		if !strings.Contains(text, "kind must be one of ingest, merge") {
			t.Fatalf("%s error = %q, want valid kind set", name, text)
		}
	}
}

func TestMergeToolResolvesPathsOnceAndQueuesJob(t *testing.T) {
	// R-E2H4-OPZO
	// R-YINV-5EPR
	wiki := &capturingWiki{
		ingestID: "job-merge",
		pathSubjects: map[string]subject{
			"entity/old-name": {ID: "subject-old"},
			"entity/new-name": {ID: "subject-new"},
		},
	}
	h := gatedHandler(t, newTestHandler(t, WithMergeService(wiki, wiki)))

	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"merge","method":"tools/call","params":{"name":"merge","arguments":{"scope":"default","from":"entity/old-name","to":"entity/new-name"}}}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("merge status = %d, want 200", rec.Code)
	}
	var body struct {
		JobID string `json:"job_id"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if body.JobID != "job-merge" {
		t.Fatalf("job_id = %q, want job-merge", body.JobID)
	}
	if wiki.mergeFrom != "subject-old" || wiki.mergeTo != "subject-new" {
		t.Fatalf("merge ids = %q -> %q, want subject-old -> subject-new", wiki.mergeFrom, wiki.mergeTo)
	}
	if wiki.pathLookups["entity/old-name"] != 1 || wiki.pathLookups["entity/new-name"] != 1 {
		t.Fatalf("path lookups = %#v, want each path resolved once", wiki.pathLookups)
	}
	if wiki.mergeOwner != "owner@example.com" {
		t.Fatalf("merge owner = %q, want authenticated owner", wiki.mergeOwner)
	}
}

func TestMergeToolReportsResolveAndEnqueueErrors(t *testing.T) {
	// R-E01B-X6IA
	// R-E3P1-2HQD
	t.Run("resolve", func(t *testing.T) {
		wiki := &capturingWiki{
			pathSubjects: map[string]subject{"entity/new-name": {ID: "subject-new"}},
			pathErrs:     map[string]error{"entity/missing": sql.ErrNoRows},
		}
		h := gatedHandler(t, newTestHandler(t, WithMergeService(wiki, wiki)))

		rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"merge","method":"tools/call","params":{"name":"merge","arguments":{"scope":"default","from":"entity/missing","to":"entity/new-name"}}}`, "owner@example.com")

		result := decodeToolResult(t, rec.Body.Bytes())
		if !result.IsError || result.StructuredContent["code"] != "not_found" {
			t.Fatalf("resolve error = %#v, want subject not_found", result)
		}
		if wiki.mergeFrom != "" || wiki.mergeTo != "" {
			t.Fatalf("merge called with %q -> %q, want no enqueue after resolve failure", wiki.mergeFrom, wiki.mergeTo)
		}
		if wiki.pathLookups["entity/missing"] != 1 || wiki.pathLookups["entity/new-name"] != 0 {
			t.Fatalf("path lookups = %#v, want failed from path resolved once and to path not resolved", wiki.pathLookups)
		}
	})

	t.Run("same id", func(t *testing.T) {
		wiki := &capturingWiki{
			pathSubjects: map[string]subject{
				"entity/current-name": {ID: "subject-same"},
				"entity/old-name":     {ID: "subject-same"},
			},
		}
		h := gatedHandler(t, newTestHandler(t, WithMergeService(wiki, wiki)))

		rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"merge","method":"tools/call","params":{"name":"merge","arguments":{"scope":"default","from":"entity/old-name","to":"entity/current-name"}}}`, "owner@example.com")

		text := toolTextString(t, rec.Body.Bytes())
		if !strings.Contains(text, "same subject") {
			t.Fatalf("same-id error = %q, want same subject", text)
		}
		if wiki.mergeFrom != "" || wiki.mergeTo != "" {
			t.Fatalf("merge called with %q -> %q, want no enqueue for same-id merge", wiki.mergeFrom, wiki.mergeTo)
		}
		if wiki.pathLookups["entity/old-name"] != 1 || wiki.pathLookups["entity/current-name"] != 1 {
			t.Fatalf("path lookups = %#v, want both paths resolved once", wiki.pathLookups)
		}
	})

	t.Run("enqueue", func(t *testing.T) {
		wiki := &capturingWiki{
			pathSubjects: map[string]subject{
				"entity/old-name": {ID: "subject-old"},
				"entity/new-name": {ID: "subject-new"},
			},
			mergeErr: errors.New("merge queue unavailable"),
		}
		h := gatedHandler(t, newTestHandler(t, WithMergeService(wiki, wiki)))

		rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"merge","method":"tools/call","params":{"name":"merge","arguments":{"scope":"default","from":"entity/old-name","to":"entity/new-name"}}}`, "owner@example.com")

		text := toolTextString(t, rec.Body.Bytes())
		if !strings.Contains(text, "merge queue unavailable") {
			t.Fatalf("enqueue error = %q, want merge queue unavailable", text)
		}
		if wiki.mergeFrom != "subject-old" || wiki.mergeTo != "subject-new" {
			t.Fatalf("merge ids = %q -> %q, want resolved ids forwarded before enqueue error", wiki.mergeFrom, wiki.mergeTo)
		}
		if wiki.pathLookups["entity/old-name"] != 1 || wiki.pathLookups["entity/new-name"] != 1 {
			t.Fatalf("path lookups = %#v, want each path resolved once before enqueue error", wiki.pathLookups)
		}
	})
}

func TestMergesToolReturnsAuditPage(t *testing.T) {
	// R-E2H4-OPZO
	// R-E4WX-G9H2
	// R-YINV-5EPR
	wiki := &capturingWiki{
		merges: []alias{{
			NormName:  "old name",
			SubjectID: "subject-new",
			Name:      "Old Name",
			OwnerID:   "owner-id", OwnerEmail: "owner@example.com",
			CreatedAt: "2026-06-24T12:00:00Z",
		}},
		mergesNext: "next-token",
	}
	h := gatedHandler(t, newTestHandler(t, WithMergeListService(wiki)))
	cursor := paging.EncodeCursor("2026-06-24T12:00:00Z", "old name")

	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"merges","method":"tools/call","params":{"name":"merges","arguments":{"scope":"default","limit":1,"cursor":"`+cursor+`"}}}`, "owner@example.com")

	if rec.Code != http.StatusOK {
		t.Fatalf("merges status = %d, want 200", rec.Code)
	}
	var body struct {
		Merges []struct {
			NormName   string `json:"norm_name"`
			SubjectID  string `json:"subject_id"`
			Name       string `json:"name"`
			OwnerID    string `json:"owner_id"`
			OwnerEmail string `json:"owner_email"`
			CreatedAt  string `json:"created_at"`
		} `json:"merges"`
		Next string `json:"next_cursor"`
	}
	decodeToolText(t, rec.Body.Bytes(), &body)
	if len(body.Merges) != 1 || body.Merges[0].NormName != "old name" || body.Merges[0].SubjectID != "subject-new" || body.Merges[0].OwnerEmail != "owner@example.com" {
		t.Fatalf("merges body = %#v, want audit alias row", body)
	}
	if body.Next != "next-token" {
		t.Fatalf("next cursor = %q, want next-token", body.Next)
	}
	if wiki.mergesPage.Limit != 1 || wiki.mergesPage.Cursor != cursor {
		t.Fatalf("merges paging = %#v, want forwarded limit/cursor", wiki.mergesPage)
	}
}

func TestJobsStatusSchemaPublishesEnumAndRejectsUnknownStatus(t *testing.T) {
	// R-Y4EH-RVMV
	h := gatedHandler(t, newTestHandler(t,
		WithJobListService(&capturingWiki{}),
		WithJobsCountService(&capturingWiki{}),
	))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "owner@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", rec.Code)
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeJSON(t, rec.Body.Bytes(), &got)
	for _, name := range []string{"jobs", "jobs_count"} {
		schema := toolSchema(t, got.Result.Tools, name)
		properties := schema["properties"].(map[string]any)
		status := properties["status"].(map[string]any)
		items := status["items"].(map[string]any)
		enum := items["enum"].([]any)
		if status["type"] != "array" || len(enum) != 5 {
			t.Fatalf("%s status schema = %#v, want array enum of five states", name, status)
		}
		for _, want := range []string{"pending", "working", "done", "failed", "aborted"} {
			if !containsJSONValue(enum, want) {
				t.Fatalf("%s status enum = %#v, missing %s", name, enum, want)
			}
		}
	}

	for _, name := range []string{"jobs", "jobs_count"} {
		rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"bad","method":"tools/call","params":{"name":"`+name+`","arguments":{"scope":"default","status":["paused"]}}}`, "owner@example.com")
		text := toolTextString(t, rec.Body.Bytes())
		if !strings.Contains(text, "status must be one of pending, working, done, failed, aborted") {
			t.Fatalf("%s error = %q, want valid status set", name, text)
		}
	}
}

func TestJobsRejectMalformedTimeAndCursorFilters(t *testing.T) {
	// R-3EZ6-MEDX
	h := gatedHandler(t, newTestHandler(t, WithJobListService(&capturingWiki{})))
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "since",
			body: `{"jsonrpc":"2.0","id":"bad-since","method":"tools/call","params":{"name":"jobs","arguments":{"scope":"default","since":"yesterday"}}}`,
			want: "since must be RFC3339",
		},
		{
			name: "until",
			body: `{"jsonrpc":"2.0","id":"bad-until","method":"tools/call","params":{"name":"jobs","arguments":{"scope":"default","until":"tomorrow"}}}`,
			want: "until must be RFC3339",
		},
		{
			name: "cursor",
			body: `{"jsonrpc":"2.0","id":"bad-cursor","method":"tools/call","params":{"name":"jobs","arguments":{"scope":"default","cursor":"not a cursor"}}}`,
			want: "cursor is invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := callMCP(t, h, tc.body, "owner@example.com")
			text := toolTextString(t, rec.Body.Bytes())
			if !strings.Contains(text, tc.want) {
				t.Fatalf("error = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestPaginatedListToolsForwardFiltersAndReturnNextCursors(t *testing.T) {
	// R-3A3L-3BF5
	// R-3CJD-UUWJ
	// R-YINV-5EPR
	started := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	jobCursor := paging.EncodeCursor("2026-06-22T00:00:00Z", "job-cursor")
	wiki := &capturingWiki{
		jobs: []job{{
			ID:      "job-123",
			OwnerID: "owner-id", OwnerEmail: "owner@example.com",
			Title:      "Source",
			Tags:       []string{"one"},
			Status:     "done",
			ReceivedAt: started,
		}},
		jobNext: "job-next",
		subjects: []subject{{
			Name:     "Acme Robotics",
			NormName: "acme-robotics",
			Type:     "entity",
			HasPage:  true,
		}},
		subjectNext: "subject-next",
		claims: []claim{{
			ID:    "claim-1",
			JobID: "job-123",
			Body:  "Acme Robotics runs a Tulsa lab.",
		}},
		claimsNext: "claim-next",
	}
	h := gatedHandler(t, newTestHandler(t,
		WithJobListService(wiki),
		WithSubjectListService(wiki),
		WithClaimListService(wiki),
	))

	jobsRec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"jobs",
		"method":"tools/call",
		"params":{"name":"jobs","arguments":{"scope":"default","status":["done"],"since":"2026-06-22T00:00:00Z","until":"2026-06-23T00:00:00Z","limit":1,"cursor":"`+jobCursor+`"}}
	}`, "owner@example.com")
	var jobsBody struct {
		Jobs []struct {
			ID     string   `json:"id"`
			Status string   `json:"status"`
			Tags   []string `json:"tags"`
		} `json:"jobs"`
		Next string `json:"next_cursor"`
	}
	decodeToolText(t, jobsRec.Body.Bytes(), &jobsBody)
	if len(wiki.jobFilter.Statuses) != 1 || wiki.jobFilter.Statuses[0] != "done" || wiki.jobPage.Limit != 1 || wiki.jobPage.Cursor != jobCursor {
		t.Fatalf("job list args = %#v/%#v, want forwarded filter/page", wiki.jobFilter, wiki.jobPage)
	}
	if len(jobsBody.Jobs) != 1 || jobsBody.Jobs[0].ID != "job-123" || jobsBody.Jobs[0].Status != "done" || jobsBody.Next != "job-next" {
		t.Fatalf("jobs body = %#v, want paginated job result", jobsBody)
	}

	subjectsRec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"subjects",
		"method":"tools/call",
		"params":{"name":"subjects","arguments":{"scope":"default","type":"entity","name":"robot","limit":2,"cursor":"subject-cursor"}}
	}`, "owner@example.com")
	var subjectsBody struct {
		Subjects []struct {
			Path    string `json:"path"`
			HasPage bool   `json:"has_page"`
		} `json:"subjects"`
		Next string `json:"next_cursor"`
	}
	decodeToolText(t, subjectsRec.Body.Bytes(), &subjectsBody)
	if wiki.subjectType != "entity" || wiki.subjectName != "robot" || wiki.subjectPage.Limit != 2 || wiki.subjectPage.Cursor != "subject-cursor" {
		t.Fatalf("subject list args = %q/%q/%#v, want forwarded filter/page", wiki.subjectType, wiki.subjectName, wiki.subjectPage)
	}
	if len(subjectsBody.Subjects) != 1 || subjectsBody.Subjects[0].Path != "entity/acme-robotics" || !subjectsBody.Subjects[0].HasPage || subjectsBody.Next != "subject-next" {
		t.Fatalf("subjects body = %#v, want paginated subject result", subjectsBody)
	}

	claimsRec := callMCP(t, h, `{
		"jsonrpc":"2.0",
		"id":"claims",
		"method":"tools/call",
		"params":{"name":"claims","arguments":{"scope":"default","subject":"entity/acme-robotics","limit":3,"cursor":"claim-cursor"}}
	}`, "owner@example.com")
	var claimsBody struct {
		Claims []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"claims"`
		Next string `json:"next_cursor"`
	}
	decodeToolText(t, claimsRec.Body.Bytes(), &claimsBody)
	if wiki.claimsSubject != "entity/acme-robotics" || wiki.claimsPage.Limit != 3 || wiki.claimsPage.Cursor != "claim-cursor" {
		t.Fatalf("claims args = %q/%#v, want forwarded subject/page", wiki.claimsSubject, wiki.claimsPage)
	}
	if len(claimsBody.Claims) != 1 || claimsBody.Claims[0].ID != "claim-1" || claimsBody.Claims[0].Text == "" || claimsBody.Next != "claim-next" {
		t.Fatalf("claims body = %#v, want paginated claims result", claimsBody)
	}
}

func TestMCPToolsAreBehindRequireIdentity(t *testing.T) {
	// R-MZLQ-34IK
	h := gatedHandler(t, newTestHandler(t, WithIngestService(&capturingWiki{})))
	rec := callMCP(t, h, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without authenticated identity", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("WWW-Authenticate challenge is empty")
	}
	var body map[string]any
	decodeJSON(t, rec.Body.Bytes(), &body)
	if body["error"] != "unauthorized" {
		t.Fatalf("error = %v, want unauthorized", body["error"])
	}
}

type capturingWiki struct {
	ingestID       string
	ingestScope    string
	ingestOwnerID  string
	ingestOwner    string
	ingestText     string
	ingestTitle    string
	ingestTags     []string
	statusJobID    string
	status         jobStatus
	statusErr      error
	abortJobID     string
	abortResult    abortResult
	rerunJobID     string
	rerunResult    rerunResult
	jobs           []job
	jobFilter      JobFilter
	jobPage        paging.Params
	jobNext        string
	jobCount       int
	jobCountFilter JobFilter
	pathSubjects   map[string]subject
	pathErrs       map[string]error
	pathLookups    map[string]int
	mergeFrom      string
	mergeTo        string
	mergeOwner     string
	mergeErr       error
	merges         []alias
	mergesPage     paging.Params
	mergesNext     string
	claims         []claim
	claimsErr      error
	claimsSubject  string
	claimsPage     paging.Params
	claimsNext     string
	page           page
	pagePath       string
	pageErr        error
	subjects       []subject
	subjectType    string
	subjectName    string
	subjectPage    paging.Params
	subjectNext    string
}

type testScopeService struct{}

func (testScopeService) Create(_ context.Context, name string) (wikidomain.Scope, error) {
	if name == "default" {
		return wikidomain.Scope{}, wikidomain.ErrScopeExists
	}
	return wikidomain.Scope{Name: name, Visibility: "private"}, nil
}

func (testScopeService) Get(_ context.Context, name string) (wikidomain.Scope, error) {
	if name == "default" {
		return wikidomain.Scope{Name: name, Visibility: "private"}, nil
	}
	return wikidomain.Scope{}, wikidomain.ErrScopeNotFound
}

func (testScopeService) List(context.Context) ([]wikidomain.Scope, error) {
	return []wikidomain.Scope{{Name: "default", Visibility: "private"}}, nil
}

func (testScopeService) SetVisibility(context.Context, string, string) error { return nil }
func (testScopeService) SetInstructions(context.Context, string, string) error {
	return nil
}
func (testScopeService) Delete(context.Context, string) error { return nil }

type scopePartitionService struct{}

func (*scopePartitionService) Get(_ context.Context, name string) (wikidomain.Scope, error) {
	if name == "s1" || name == "s2" {
		return wikidomain.Scope{Name: name, Visibility: "private"}, nil
	}
	return wikidomain.Scope{}, wikidomain.ErrScopeNotFound
}

func (*scopePartitionService) Create(context.Context, string) (wikidomain.Scope, error) {
	return wikidomain.Scope{}, nil
}
func (*scopePartitionService) List(context.Context) ([]wikidomain.Scope, error) { return nil, nil }
func (*scopePartitionService) SetVisibility(context.Context, string, string) error {
	return nil
}
func (*scopePartitionService) SetInstructions(context.Context, string, string) error { return nil }
func (*scopePartitionService) Delete(context.Context, string) error                  { return nil }
func (*scopePartitionService) ListJobsInScope(_ context.Context, scope string, _ JobFilter, _ paging.Params) ([]job, string, error) {
	if scope == "s1" {
		return []job{{ID: "job-s1", Status: "pending"}}, "", nil
	}
	return nil, "", nil
}

func (*scopePartitionService) CountJobsInScope(_ context.Context, scope string, _ JobFilter) (int, error) {
	if scope == "s1" {
		return 1, nil
	}
	return 0, nil
}

func (*scopePartitionService) ListMergesInScope(_ context.Context, scope string, _ paging.Params) ([]alias, string, error) {
	if scope == "s1" {
		return []alias{{NormName: "folded"}}, "", nil
	}
	return nil, "", nil
}

type missingJobs struct{}

func (missingJobs) Abort(context.Context, string) (abortResult, error) {
	return abortResult{}, sql.ErrNoRows
}

func (missingJobs) Rerun(context.Context, string) (rerunResult, error) {
	return rerunResult{}, sql.ErrNoRows
}

type failingIngest struct{ err error }

type mcpJobLister struct{ store *wikidomain.JobStore }

func (l mcpJobLister) ListJobsInScope(ctx context.Context, scope string, f JobFilter, p paging.Params) ([]wikidomain.Job, string, error) {
	return l.store.ListJobs(ctx, scope, wikidomain.JobFilter{
		Statuses: f.Statuses,
		Kinds:    f.Kinds,
		Since:    f.Since,
		Until:    f.Until,
	}, p)
}

type mcpMergeLister struct{ store *wikidomain.AliasStore }

func (l mcpMergeLister) ListMergesInScope(ctx context.Context, scope string, p paging.Params) ([]wikidomain.Alias, string, error) {
	return l.store.ListMergesInScope(ctx, scope, p)
}

type ownerPairCompiler struct{}

func (ownerPairCompiler) Compile(_ context.Context, _ llm.Attribution, subject wikidomain.Subject, _ []wikidomain.Claim) (string, string, error) {
	return subject.Name, "merged", nil
}

func (f failingIngest) Ingest(context.Context, string, string, string, string, string, []string) (string, error) {
	return "", f.err
}

func (w *capturingWiki) Ingest(_ context.Context, scope string, ownerID, ownerEmail, text, title string, tags []string) (string, error) {
	w.ingestScope = scope
	w.ingestOwnerID = ownerID
	w.ingestOwner = ownerEmail
	w.ingestText = text
	w.ingestTitle = title
	w.ingestTags = append([]string(nil), tags...)
	if w.ingestID == "" {
		return "job-1", nil
	}
	return w.ingestID, nil
}

func (w *capturingWiki) JobStatus(_ context.Context, jobID string) (jobStatus, error) {
	w.statusJobID = jobID
	if w.statusErr != nil {
		return jobStatus{}, w.statusErr
	}
	return w.status, nil
}

func (w *capturingWiki) Abort(_ context.Context, jobID string) (abortResult, error) {
	w.abortJobID = jobID
	return w.abortResult, nil
}

func (w *capturingWiki) Rerun(_ context.Context, jobID string) (rerunResult, error) {
	w.rerunJobID = jobID
	return w.rerunResult, nil
}

func (w *capturingWiki) ListJobsInScope(_ context.Context, _ string, f JobFilter, p paging.Params) ([]job, string, error) {
	w.jobFilter = f
	w.jobPage = p
	return w.jobs, w.jobNext, nil
}

func (w *capturingWiki) CountJobsInScope(_ context.Context, _ string, f JobFilter) (int, error) {
	w.jobCountFilter = f
	return w.jobCount, nil
}

func (w *capturingWiki) GetByPath(_ context.Context, _ string, path string) (subject, error) {
	if w.pathLookups == nil {
		w.pathLookups = map[string]int{}
	}
	w.pathLookups[path]++
	if err := w.pathErrs[path]; err != nil {
		return subject{}, err
	}
	if subject, ok := w.pathSubjects[path]; ok {
		return subject, nil
	}
	return subject{}, sql.ErrNoRows
}

func (w *capturingWiki) MergeSubjects(ctx context.Context, _ string, fromSubjectID, toSubjectID string) (string, error) {
	w.mergeFrom = fromSubjectID
	w.mergeTo = toSubjectID
	if id, ok := appkit.IdentityFrom(ctx); ok {
		w.mergeOwner = id.OwnerEmail
	}
	if w.mergeErr != nil {
		return "", w.mergeErr
	}
	if w.ingestID == "" {
		return "job-merge", nil
	}
	return w.ingestID, nil
}

func (w *capturingWiki) ListMergesInScope(_ context.Context, _ string, p paging.Params) ([]alias, string, error) {
	w.mergesPage = p
	return w.merges, w.mergesNext, nil
}

func (w *capturingWiki) Subjects(_ context.Context, _, _ string) ([]subject, error) {
	return w.subjects, nil
}

func (w *capturingWiki) ListInScope(_ context.Context, _, typ, name string, p paging.Params) ([]subject, string, error) {
	w.subjectType = typ
	w.subjectName = name
	w.subjectPage = p
	return w.subjects, w.subjectNext, nil
}

func (w *capturingWiki) ClaimsBySubject(_ context.Context, _ string) ([]claim, error) {
	if w.claimsErr != nil {
		return nil, w.claimsErr
	}
	return w.claims, nil
}

func (w *capturingWiki) ListBySubjectInScope(_ context.Context, _, subject string, p paging.Params) ([]claim, string, error) {
	w.claimsSubject = subject
	w.claimsPage = p
	if w.claimsErr != nil {
		return nil, "", w.claimsErr
	}
	return w.claims, w.claimsNext, nil
}

func (w *capturingWiki) PageByPathInScope(_ context.Context, _, path string) (page, error) {
	w.pagePath = path
	if w.pageErr != nil {
		return page{}, w.pageErr
	}
	return w.page, nil
}

type abortResult struct {
	Aborted bool
	Status  string
}

type rerunResult struct {
	Requeued bool
	Status   string
}

type job struct {
	ID         string
	OwnerID    string
	OwnerEmail string
	Title      string
	Tags       []string
	Status     string
	ReceivedAt time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	Error      string
}

type jobStatus struct {
	ID         string     `json:"job_id"`
	Status     string     `json:"status"`
	ReceivedAt time.Time  `json:"received_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Error      string     `json:"error"`
	Subjects   []string   `json:"subjects"`
}

type alias struct {
	NormName   string
	SubjectID  string
	Name       string
	OwnerID    string
	OwnerEmail string
	CreatedAt  string
}

type subject struct {
	ID       string
	Name     string
	NormName string
	Type     string
	HasPage  bool
}

type claim struct {
	ID        string
	SubjectID string
	JobID     string
	Body      string
}

type page struct {
	ID        string
	SubjectID string
	Title     string
	Body      string
	Footer    string
}

type mcpPageService struct {
	resolver *wikidomain.Resolver
	service  *wikidomain.Service
}

func (s mcpPageService) PageByPathInScope(ctx context.Context, scope, path string) (page, error) {
	subject, err := s.resolver.ResolveByPath(ctx, scope, path)
	if err != nil {
		return page{}, err
	}
	linked, err := s.service.PageWithLinks(ctx, scope, subject.ID)
	if err != nil {
		return page{}, err
	}
	body := wikidomain.RenderFooter(linked.Body, linked.Mentions, linked.MentionedBy)
	return page{
		ID:        linked.ID,
		SubjectID: subject.ID,
		Title:     linked.Title,
		Body:      linked.Body,
		Footer:    strings.TrimPrefix(body, linked.Body),
	}, nil
}

type mcpScriptedSearch struct {
	result retrieve.Result
}

func (s *mcpScriptedSearch) SearchAnalyzed(context.Context, string, llm.Attribution, any, retrieve.SearchLimits) (retrieve.Result, error) {
	return s.result, nil
}

type mcpScriptedProvider struct {
	responses []string
}

func (p *mcpScriptedProvider) RoundTrip(_ context.Context, _ *llmtest.Request) *llmtest.RoundTrip {
	if len(p.responses) == 0 {
		return mcpTextRoundTrip(`{"found":false}`)
	}
	text := p.responses[0]
	p.responses = p.responses[1:]
	return mcpTextRoundTrip(text)
}

func (p *mcpScriptedProvider) Name() string { return "mcp-scripted" }

func (p *mcpScriptedProvider) Pricing(string) (llmtest.Pricing, bool) {
	return llmtest.Pricing{Tiers: []llmtest.RateTier{{MinInputTokens: 0}}}, true
}

func mcpTextRoundTrip(text string) *llmtest.RoundTrip {
	return llmtest.NewRoundTrip(llmtest.Message{
		Role:   llmtest.RoleAssistant,
		Blocks: []llmtest.Block{llmtest.TextBlock{Text: text}},
	}, llmtest.FinishStop, llmtest.Usage{InputUncached: 1, Output: 1, Total: 2}, nil, nil, 0, false)
}

type answer struct {
	Found     bool
	Text      string
	Citations []citation
}

type citation struct {
	Path  string
	Title string
}

type capturingAsker struct {
	owner    string
	question string
	answer   answer
}

func (a *capturingAsker) Ask(_ context.Context, _ string, owner, question string) (answer, error) {
	a.owner = owner
	a.question = question
	return a.answer, nil
}

func migratedMCPDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	conn, err := appdb.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	migs, err := appdb.LoadMigrations(wikidb.FS, "migrations")
	if err != nil {
		conn.Close()
		t.Fatalf("LoadMigrations: %v", err)
	}
	if err := appdb.Migrate(ctx, conn, migs); err != nil {
		conn.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return conn
}

func newTestHandler(t *testing.T, opts ...Option) http.Handler {
	t.Helper()
	return newTestHandlerWithAuthServer(t, "https://int.ikigenba.com", opts...)
}

func newTestHandlerWithAuthServer(t *testing.T, authServer string, opts ...Option) http.Handler {
	t.Helper()
	var h http.Handler
	_, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: authServer,
		Version:    "test-version",
		Service:    "wiki",
		Register: func(rt *appkit.Router) error {
			var err error
			h, err = NewHandler(rt, opts...)
			return err
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if h == nil {
		t.Fatal("NewHandler returned nil handler")
	}
	return h
}

func gatedHandler(t *testing.T, mcp http.Handler) http.Handler {
	t.Helper()
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		ResourceID: "https://int.ikigenba.com/srv/wiki/mcp",
		AuthServer: "https://int.ikigenba.com",
		Version:    "test-version",
		Service:    "wiki",
		MCP:        mcp,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv.Handler
}

func callMCP(t *testing.T, h http.Handler, body, owner string) *httptest.ResponseRecorder {
	t.Helper()
	ownerID := ""
	if owner != "" {
		ownerID = "owner-id"
	}
	return callMCPIdentity(t, h, body, ownerID, owner)
}

func callMCPIdentity(t *testing.T, h http.Handler, body, ownerID, ownerEmail string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	if ownerID != "" {
		req.Header.Set("X-Owner-Id", ownerID)
		req.Header.Set("X-Owner-Email", ownerEmail)
		req.Header.Set("X-Client-Id", "client-1")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeToolText(t *testing.T, raw []byte, dst any) {
	t.Helper()
	decodeJSON(t, []byte(toolTextString(t, raw)), dst)
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent map[string]any `json:"structuredContent"`
	IsError           bool           `json:"isError"`
}

func decodeToolResult(t *testing.T, raw []byte) toolResult {
	t.Helper()
	var got struct {
		Result toolResult `json:"result"`
	}
	decodeJSON(t, raw, &got)
	return got.Result
}

func namedTool(t *testing.T, tools []appkitmcp.Tool, name string) appkitmcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return appkitmcp.Tool{}
}

func structuredCall(t *testing.T, tool appkitmcp.Tool, raw string) map[string]any {
	t.Helper()
	result, err := tool.Handler(context.Background(), json.RawMessage(raw), server.Identity{})
	if err != nil {
		t.Fatalf("%s handler: %v", tool.Name, err)
	}
	if code := resultCode(result); code != "" {
		t.Fatalf("%s returned %s: %s", tool.Name, code, resultMessage(result))
	}
	rawPayload, err := json.Marshal(result["structuredContent"])
	if err != nil {
		t.Fatalf("%s marshal structuredContent: %v", tool.Name, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		t.Fatalf("%s decode structuredContent: %v", tool.Name, err)
	}
	return payload
}

func resultCode(result map[string]any) string {
	payload, _ := result["structuredContent"].(map[string]any)
	if payload["code"] == nil {
		return ""
	}
	return fmt.Sprint(payload["code"])
}

func resultMessage(result map[string]any) string {
	payload, _ := result["structuredContent"].(map[string]any)
	message, _ := payload["message"].(string)
	return message
}

func mustRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return raw
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mapSlice(value any) []map[string]any {
	values, _ := value.([]any)
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]any); ok {
			out = append(out, item)
		}
	}
	return out
}

func toolTextString(t *testing.T, raw []byte) string {
	t.Helper()
	var got struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	decodeJSON(t, raw, &got)
	if len(got.Result.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(got.Result.Content))
	}
	return got.Result.Content[0].Text
}

func decodeJSON(t *testing.T, raw []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode JSON %s: %v", string(raw), err)
	}
}

func toolSchema(t *testing.T, tools []struct {
	Name        string         `json:"name"`
	InputSchema map[string]any `json:"inputSchema"`
}, name string,
) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool.InputSchema
		}
	}
	t.Fatalf("tools/list missing %s", name)
	return nil
}

func containsJSONValue(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringValues(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
