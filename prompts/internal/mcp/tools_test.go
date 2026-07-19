package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appkitdb "appkit/db"
	"appkit/server"

	"prompts/internal/calls"
	promptsdb "prompts/internal/db"
	"prompts/internal/prompt"
	"prompts/internal/sandbox"
)

func TestCallsFiltersWindowAndOmitsBodiesFromList(t *testing.T) {
	// R-6DJD-SKVZ
	svc, store := phase42Service(t)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seedCalls(t, store,
		phase42Row("match-1", "wiki.compile", "service:wiki", "other@example.com", base.Add(time.Hour), 1),
		phase42Row("wrong-name", "crm.summarize", "service:crm", "", base.Add(2*time.Hour), 2),
		phase42Row("match-2", "wiki.compile", "user:peer@example.com", "peer@example.com", base.Add(3*time.Hour), 3),
		phase42Row("too-late", "wiki.compile", "service:wiki", "", base.Add(25*time.Hour), 4),
	)
	out := phase42Call(t, svc, "calls", map[string]any{
		"name": "wiki.compile", "since": base.Format(time.RFC3339), "until": base.Add(24 * time.Hour).Format(time.RFC3339),
	})
	rows := out["calls"].([]map[string]any)
	if got := []string{rows[0]["id"].(string), rows[1]["id"].(string)}; !reflect.DeepEqual(got, []string{"match-2", "match-1"}) {
		t.Fatalf("filtered ids = %v, want [match-2 match-1]", got)
	}
	for _, row := range rows {
		if _, ok := row["request_body"]; ok {
			t.Fatalf("list row leaked request_body: %#v", row)
		}
		if _, ok := row["response_body"]; ok {
			t.Fatalf("list row leaked response_body: %#v", row)
		}
	}
}

func TestCallsDetailShowsRetainedThenPrunedBodies(t *testing.T) {
	// R-6ERA-6CMO
	svc, store := phase42Service(t)
	ended := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	seedCalls(t, store, phase42Row("detail", "wiki.compile", "service:wiki", "", ended, 1))
	before := phase42Call(t, svc, "calls", map[string]any{"call_id": "detail"})
	if before["request_body"] != "request-detail" || before["response_body"] != "response-detail" {
		t.Fatalf("retained bodies = (%v, %v)", before["request_body"], before["response_body"])
	}
	if _, ok := before["bodies_pruned"]; ok {
		t.Fatalf("retained detail marked pruned: %#v", before)
	}
	if n, err := store.PruneBodies(t.Context(), ended.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("PruneBodies = (%d, %v), want (1, nil)", n, err)
	}
	after := phase42Call(t, svc, "calls", map[string]any{"call_id": "detail"})
	if after["bodies_pruned"] != true {
		t.Fatalf("pruned detail marker = %#v", after["bodies_pruned"])
	}
	if _, ok := after["request_body"]; ok {
		t.Fatalf("pruned detail retained request_body: %#v", after)
	}
	if _, ok := after["response_body"]; ok {
		t.Fatalf("pruned detail retained response_body: %#v", after)
	}
	res := phase42RawCall(t, svc, "calls", map[string]any{"call_id": "missing"})
	structured := res["structuredContent"].(map[string]any)
	if !isError(res) || fmt.Sprint(structured["code"]) != "not_found" {
		t.Fatalf("missing call result = %#v, want not_found", res)
	}
}

func TestUsageGroupsByNameWithExactTotals(t *testing.T) {
	// R-6FZ6-K4DD
	svc, store := phase42Service(t)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seedCalls(t, store,
		phase42Row("a", "wiki.compile", "service:wiki", "", base, 1),
		phase42Row("b", "wiki.compile", "service:wiki", "", base.Add(time.Hour), 2),
		phase42Row("c", "crm.summarize", "service:crm", "", base.Add(2*time.Hour), 3),
	)
	buckets := phase42Call(t, svc, "usage", map[string]any{"group_by": "name"})["buckets"].([]map[string]any)
	byKey := phase42Buckets(buckets)
	assertUsageBucket(t, byKey["wiki.compile"], 2, 30, 15, 45, 0.3)
	assertUsageBucket(t, byKey["crm.summarize"], 1, 30, 15, 45, 0.3)
}

func TestUsageGroupsDistinctOriginKinds(t *testing.T) {
	// R-6H72-XW42
	svc, store := phase42Service(t)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seedCalls(t, store,
		phase42Row("user", "wiki.compile", "user:alice@example.com", "alice@example.com", base, 1),
		phase42Row("trigger", "wiki.compile", "trigger:cron", "", base, 1),
		phase42Row("service", "wiki.compile", "service:wiki", "", base, 1),
	)
	buckets := phase42Call(t, svc, "usage", map[string]any{"group_by": "origin"})["buckets"].([]map[string]any)
	byKey := phase42Buckets(buckets)
	for _, key := range []string{"user:alice@example.com", "trigger:cron", "service:wiki"} {
		if byKey[key] == nil || byKey[key]["calls"] != int64(1) {
			t.Fatalf("origin bucket %q = %#v, want one call", key, byKey[key])
		}
	}
}

func TestCallsAreIdentityGatedButNotRowFiltered(t *testing.T) {
	// R-6IEZ-BNUR
	svc, store := phase42Service(t)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seedCalls(t, store,
		phase42Row("peer", "wiki.compile", "user:peer@example.com", "peer@example.com", base, 1),
		phase42Row("service", "wiki.compile", "service:wiki", "", base.Add(time.Minute), 1),
	)
	res := phase42RawCallAs(t, svc, "calls", map[string]any{}, server.Identity{OwnerEmail: "caller@example.com"})
	rows := res["structuredContent"].(map[string]any)["calls"].([]map[string]any)
	owners := map[string]bool{}
	for _, row := range rows {
		owners[row["owner_email"].(string)] = true
	}
	if !owners["peer@example.com"] || !owners[""] {
		t.Fatalf("caller-scoped result owners = %v, want peer and ownerless rows", owners)
	}
}

func phase42Service(t *testing.T) (*prompt.Service, *calls.Store) {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "phase42.db"))
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migrations, err := appkitdb.LoadMigrations(promptsdb.FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(t.Context(), conn, migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	callStore := calls.NewStore(conn)
	promptStore := prompt.NewStore(conn)
	promptStore.Calls = callStore
	sb, err := sandbox.New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	return prompt.NewService(promptStore, sb, t.TempDir(), &fakeRunner{}), callStore
}

func phase42Row(id, name, origin, owner string, started time.Time, multiplier int64) calls.Row {
	request, response := "request-"+id, "response-"+id
	return calls.Row{
		ID: id, Class: calls.ClassCompletion, Origin: origin, Name: name, OwnerEmail: owner,
		Provider: "anthropic", Model: "claude-sonnet", InputTokens: 10 * multiplier,
		OutputTokens: 5 * multiplier, TotalTokens: 15 * multiplier, CostUSD: 0.1 * float64(multiplier),
		RequestBody: &request, ResponseBody: &response, StartedAt: started, EndedAt: started.Add(time.Minute),
	}
}

func seedCalls(t *testing.T, store *calls.Store, rows ...calls.Row) {
	t.Helper()
	for _, row := range rows {
		if err := store.Insert(t.Context(), row); err != nil {
			t.Fatalf("Insert %s: %v", row.ID, err)
		}
	}
}

func phase42Call(t *testing.T, svc *prompt.Service, name string, args map[string]any) map[string]any {
	t.Helper()
	res := phase42RawCall(t, svc, name, args)
	if isError(res) {
		t.Fatalf("%s returned error: %#v", name, res)
	}
	return res["structuredContent"].(map[string]any)
}

func phase42RawCall(t *testing.T, svc *prompt.Service, name string, args map[string]any) map[string]any {
	t.Helper()
	return phase42RawCallAs(t, svc, name, args, server.Identity{OwnerEmail: "owner@example.com"})
}

func phase42RawCallAs(t *testing.T, svc *prompt.Service, name string, args map[string]any, identity server.Identity) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	for _, tool := range Tools(svc, "") {
		if tool.Name == name {
			res, err := tool.Handler(context.Background(), encoded, identity)
			if err != nil {
				t.Fatalf("%s handler: %v", name, err)
			}
			return res
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func phase42Buckets(buckets []map[string]any) map[string]map[string]any {
	result := make(map[string]map[string]any, len(buckets))
	for _, bucket := range buckets {
		result[bucket["key"].(string)] = bucket
	}
	return result
}

func assertUsageBucket(t *testing.T, got map[string]any, count, input, output, total int64, cost float64) {
	t.Helper()
	if got == nil || got["calls"] != count || got["input_tokens"] != input || got["output_tokens"] != output || got["total_tokens"] != total {
		t.Fatalf("usage bucket = %#v, want calls=%d input=%d output=%d total=%d", got, count, input, output, total)
	}
	if value, ok := got["cost_usd"].(float64); !ok || math.Abs(value-cost) > 1e-9 {
		t.Fatalf("cost_usd = %#v, want %v", got["cost_usd"], cost)
	}
}

func TestConfigSchemaIncludesProviderModelAndOptionalExpansion(t *testing.T) {
	// R-KE1K-MUZ4
	createConfig := inputConfigSchema(t, "create")
	updateConfig := inputConfigSchema(t, "update")
	if !reflect.DeepEqual(createConfig, updateConfig) {
		t.Fatalf("create and update config schemas differ:\ncreate=%#v\nupdate=%#v", createConfig, updateConfig)
	}

	properties, ok := createConfig["properties"].(map[string]any)
	if !ok {
		t.Fatalf("config properties has type %T: %#v", createConfig["properties"], createConfig["properties"])
	}
	wantTypes := map[string]string{
		"provider":           "string",
		"model":              "string",
		"temperature":        "number",
		"top_p":              "number",
		"max_tokens":         "integer",
		"effort":             "string",
		"thinking_budget":    "integer",
		"thinking_level":     "string",
		"thinking":           "boolean",
		"max_attempts":       "integer",
		"base_delay":         "string",
		"max_delay":          "string",
		"max_elapsed":        "string",
		"ignore_retry_after": "boolean",
		"tool_loop_limit":    "integer",
		"base_url":           "string",
	}
	for key, wantType := range wantTypes {
		prop, ok := properties[key].(map[string]any)
		if !ok {
			t.Fatalf("config property %q missing or wrong type: %#v", key, properties[key])
		}
		if got := prop["type"]; got != wantType {
			t.Fatalf("config property %q type = %v, want %q", key, got, wantType)
		}
		if _, hasEnum := prop["enum"]; hasEnum {
			t.Fatalf("config property %q must not define an enum: %#v", key, prop)
		}
	}
	if len(properties) != len(wantTypes) {
		t.Fatalf("config property count = %d, want %d: %#v", len(properties), len(wantTypes), properties)
	}
}

func TestCreateAndUpdateConfigRequireOnlyModel(t *testing.T) {
	// R-20UM-JESS
	for _, toolName := range []string{"create", "update"} {
		config := inputConfigSchema(t, toolName)
		required, ok := config["required"].([]string)
		if !ok {
			t.Fatalf("%s config required field has type %T: %#v", toolName, config["required"], config["required"])
		}
		if !reflect.DeepEqual(required, []string{"model"}) {
			t.Fatalf("%s required config keys = %v, want [model]", toolName, required)
		}
		properties := config["properties"].(map[string]any)
		if _, ok := properties["provider"]; !ok {
			t.Fatalf("%s config does not expose optional provider: %#v", toolName, properties)
		}
	}
}

func TestDescribeDescriptorDocumentsExpandedConfigAndJSONL(t *testing.T) {
	description, ok := findToolDescriptor(t, "describe")["description"].(string)
	if !ok || description == "" {
		t.Fatalf("describe descriptor has no description: %#v", findToolDescriptor(t, "describe")["description"])
	}
	for _, want := range []string{
		"anthropic",
		"openai",
		"google",
		"zai",
		"provider",
		"model",
		"temperature",
		"top_p",
		"max_tokens",
		"effort",
		"thinking_budget",
		"thinking_level",
		"thinking",
		"max_attempts",
		"base_delay",
		"max_delay",
		"max_elapsed",
		"ignore_retry_after",
		"tool_loop_limit",
		"base_url",
		"sampling",
		"retry/backoff",
		"LogRecord JSONL",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("describe descriptor missing %q:\n%s", want, description)
		}
	}
}

func inputConfigSchema(t *testing.T, toolName string) map[string]any {
	t.Helper()
	toolDesc := findToolDescriptor(t, toolName)
	inputSchema, ok := toolDesc["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("%s inputSchema has type %T: %#v", toolName, toolDesc["inputSchema"], toolDesc["inputSchema"])
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s properties has type %T: %#v", toolName, inputSchema["properties"], inputSchema["properties"])
	}
	config, ok := properties["config"].(map[string]any)
	if !ok {
		t.Fatalf("%s config schema has type %T: %#v", toolName, properties["config"], properties["config"])
	}
	return config
}

func findToolDescriptor(t *testing.T, name string) map[string]any {
	t.Helper()
	for _, tool := range Tools(nil, "") {
		if tool.Name == name {
			return map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			}
		}
	}
	t.Fatalf("tool descriptor %q not found", name)
	return nil
}
