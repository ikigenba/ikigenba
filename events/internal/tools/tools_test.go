package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
	"github.com/ikigenba/ikigenba/events/internal/tools"
)

type harness struct {
	t       *testing.T
	d       *db.DB
	st      *store.Store
	client  *mcp.Client
	writer  *telemetry.Writer
	capture *telemetry.Capture
	now     time.Time
}

var caller = identity.Caller{UserID: "operator", Email: "operator@example.test", RequestID: "request-one"}

func newHarness(t *testing.T, shared bool) *harness {
	t.Helper()
	t.Setenv(services.Variable, "")
	h := &harness{t: t, now: time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.FixedZone("offset", 3600)), capture: &telemetry.Capture{}}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "log.db"), Migrations: events.Migrations(), Now: func() time.Time { return h.now }})
	if err != nil {
		t.Fatal(err)
	}
	h.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	h.st = store.New(d, store.Config{DepthMax: 8, Now: func() time.Time { return h.now }})
	newWriter := func(capture *telemetry.Capture) *telemetry.Writer {
		w := telemetry.New(telemetry.Config{Service: "events", Sink: capture, Stderr: io.Discard, Now: func() time.Time { return h.now }, Rand: strings.NewReader(strings.Repeat("x", 65536)), Sleep: func(context.Context, time.Duration) {}})
		t.Cleanup(func() { w.Shutdown(context.Background(), "test finished") })
		return w
	}
	h.writer = newWriter(h.capture)
	serverWriter := h.writer
	if !shared {
		serverWriter = newWriter(&telemetry.Capture{})
	}
	srv := mcp.NewServer(mcp.ServerConfig{Name: "events", Telemetry: serverWriter, Instructions: func(context.Context) string { return "test" }})
	// R-CVXS-MZPT R-CX5P-0RGI
	cfg := tools.Config{h.st, h.writer}
	tools.Register(srv, cfg)
	ts := httptest.NewServer(identity.Require(telemetry.Middleware(serverWriter, srv)))
	t.Cleanup(ts.Close)
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: ts.URL, HTTPClient: ts.Client()})
	return h
}

func (h *harness) call(name, args string) mcp.Result {
	h.t.Helper()
	return h.callAs(caller, name, args)
}
func (h *harness) callAs(c identity.Caller, name, args string) mcp.Result {
	h.t.Helper()
	result, err := h.client.CallTool(context.Background(), c, name, json.RawMessage(args))
	if err != nil {
		h.t.Fatal(err)
	}
	return result
}
func (h *harness) declare(name string, d store.Declaration) {
	h.t.Helper()
	if err := h.st.Declare(context.Background(), name, d); err != nil {
		h.t.Fatal(err)
	}
}
func (h *harness) seed(n int) {
	h.t.Helper()
	h.declare("producer", store.Declaration{Emits: []appEvents.Emission{{Event: "item.changed", Attrs: []string{"number", "large", "unsigned", "enabled", "label"}}, {Event: "item.quiet", Attrs: []string{}}}})
	h.declare("worker", store.Declaration{Accepts: []string{"item.changed"}})
	for i := 1; i <= n; i++ {
		e := appEvents.Event{ID: fmt.Sprintf("evt_%016x", i), Time: h.now.Add(time.Duration(i) * time.Second), Service: "producer", Name: "item.changed", RequestID: fmt.Sprintf("request-%d", i), User: "alice", Cause: "evt_ffffffffffffffff", Depth: 1, Attrs: appEvents.Attrs{"number": int64(i), "large": int64(9007199254740993), "unsigned": uint64(9223372036854775808), "enabled": true, "label": "hello"}}
		if i%2 == 0 {
			e.User = ""
			e.RequestID = ""
			e.Cause = ""
			e.Depth = 0
		}
		if err := h.st.Deliver(context.Background(), e); err != nil {
			h.t.Fatal(err)
		}
	}
}
func (h *harness) pause() {
	h.t.Helper()
	if err := h.st.Pause(context.Background(), "worker", 1, "subscriber failed"); err != nil {
		h.t.Fatal(err)
	}
}

func jsonValue(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return normalize(v)
}
func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		r, ok := new(big.Rat).SetString(string(x))
		if !ok {
			panic("invalid JSON number")
		}
		return r
	case []any:
		for i := range x {
			x[i] = normalize(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = normalize(x[k])
		}
	}
	return v
}
func equalJSON(t *testing.T, got, want []byte) {
	t.Helper()
	if !reflect.DeepEqual(jsonValue(t, got), jsonValue(t, want)) {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}
func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func success(t *testing.T, r mcp.Result) []byte {
	t.Helper()
	// R-DE8A-DJU8 R-CYDL-EJ77
	if r.IsError() {
		b, _ := r.MarshalJSON()
		t.Fatalf("unexpected refusal: %s", b)
	}
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["isError"]; ok {
		t.Fatal("success carries isError")
	}
	structured, ok := result["structuredContent"]
	if !ok {
		t.Fatal("missing structuredContent")
	}
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Type != "text" {
		t.Fatalf("content = %+v", content)
	}
	equalJSON(t, []byte(content[0].Text), structured)
	var compact bytes.Buffer
	if err := json.Compact(&compact, structured); err != nil {
		t.Fatal(err)
	}
	if content[0].Text != compact.String() {
		t.Fatalf("text ordering or whitespace differs: %q, %q", content[0].Text, compact.String())
	}
	return structured
}
func refusal(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	// R-DFG6-RBKX
	if !r.IsError() {
		t.Fatal("expected refusal")
	}
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["structuredContent"]; ok {
		t.Fatal("refusal carries structuredContent")
	}
	var content []map[string]string
	if err := json.Unmarshal(result["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || len(content[0]) != 2 || content[0]["type"] != "text" || content[0]["text"] != want {
		t.Fatalf("content = %+v, want %q", content, want)
	}
}
func keys(t *testing.T, raw []byte) []string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	if _, err := d.Token(); err != nil {
		t.Fatal(err)
	}
	var out []string
	for d.More() {
		key, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, key.(string))
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func checkKeys(t *testing.T, raw []byte, want ...string) {
	t.Helper()
	if got := keys(t, raw); !slices.Equal(got, want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
}
func (h *harness) ownEvents() []telemetry.Event {
	h.t.Helper()
	if err := h.writer.Flush(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	return h.capture.Events()
}
func (h *harness) unchanged(before []byte) {
	h.t.Helper()
	if got := h.snapshot(); !bytes.Equal(got, before) {
		h.t.Fatalf("store changed\nbefore %s\nafter %s", before, got)
	}
}
func (h *harness) snapshot() []byte {
	h.t.Helper()
	ctx := context.Background()
	subs, err := h.st.Subscribers(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	head, err := h.st.Head(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	catalog, err := h.st.Catalog(ctx, "", "")
	if err != nil {
		h.t.Fatal(err)
	}
	page, err := h.st.Search(ctx, store.Filter{}, nil, paging.MaxLimit+1)
	if err != nil {
		h.t.Fatal(err)
	}
	return marshal(h.t, []any{subs, head, catalog, page})
}

func TestToolList(t *testing.T) {
	// R-D21A-JUFA R-YW1S-U90F R-SYH1-ZZ80 R-9E6P-K6S1 R-9GMI-BQ9F R-T0WU-RIPE R-T24R-5AG3 R-T3CN-J26S R-DBSH-M0CU
	h := newHarness(t, false)
	listed, err := h.client.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"catalog", "search", "subscribers", "skip", "resume"}
	if len(listed) != len(wantNames) {
		t.Fatalf("tools = %v", listed)
	}
	schemas := []string{`{"type":"object","properties":{"service":{"type":"string"},"event":{"type":"string"}},"additionalProperties":false}`, `{"type":"object","properties":{"since":{"type":"string"},"until":{"type":"string"},"services":{"type":"array","items":{"type":"string"}},"events":{"type":"array","items":{"type":"string"}},"user":{"type":"string"},"request_id":{"type":"string"},"cause":{"type":"string"},"attrs":{"type":"object"},"limit":{"type":"integer"},"page":{"type":"string"}},"additionalProperties":false}`, `{"type":"object","additionalProperties":false}`, `{"type":"object","properties":{"service":{"type":"string"}},"required":["service"],"additionalProperties":false}`, `{"type":"object","properties":{"service":{"type":"string"}},"required":["service"],"additionalProperties":false}`}
	propertyOrder := [][]string{{"service", "event"}, {"since", "until", "services", "events", "user", "request_id", "cause", "attrs", "limit", "page"}, nil, {"service"}, {"service"}}
	for i, tool := range listed {
		if tool.Name != wantNames[i] || tool.Description == "" {
			t.Fatalf("tool %d = %+v", i, tool)
		}
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if _, ok := schema["$schema"]; ok {
			t.Fatal("schema carries $schema")
		}
		if raw, ok := schema["properties"]; ok {
			if got := keys(t, raw); !slices.Equal(got, propertyOrder[i]) {
				t.Fatalf("properties = %v", got)
			}
			var props map[string]map[string]json.RawMessage
			if err := json.Unmarshal(raw, &props); err != nil {
				t.Fatal(err)
			}
			if i == 1 {
				for name, want := range map[string]string{"limit": paging.LimitDescription, "page": paging.PageDescription} {
					equalJSON(t, props[name]["description"], marshal(t, want))
				}
			}
			for _, p := range props {
				delete(p, "description")
			}
			schema["properties"] = marshal(t, props)
		}
		equalJSON(t, marshal(t, schema), []byte(schemas[i]))
		if i == 1 {
			var schema map[string]json.RawMessage
			if err := json.Unmarshal(tool.OutputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			checkKeys(t, schema["properties"], "records", "next")
			var props map[string]json.RawMessage
			if err := json.Unmarshal(schema["properties"], &props); err != nil {
				t.Fatal(err)
			}
			equalJSON(t, props["next"], marshal(t, map[string]string{"type": "string", "description": paging.NextDescription}))
			var required []string
			if raw, ok := schema["required"]; ok {
				if err := json.Unmarshal(raw, &required); err != nil {
					t.Fatal(err)
				}
			}
			if slices.Contains(required, "next") {
				t.Fatal("next is required")
			}
		}
		var output map[string]any
		if err := json.Unmarshal(tool.OutputSchema, &output); err != nil {
			t.Fatal(err)
		}
		if output["type"] != "object" {
			t.Fatalf("output schema = %s", tool.OutputSchema)
		}
		wantEffect := mcp.Read
		readOnly, destructive := true, false
		if i == 3 {
			wantEffect = mcp.Destructive
			readOnly = false
			destructive = true
		}
		if i == 4 {
			wantEffect = mcp.Additive
			readOnly = false
		}
		if tool.Effect() != wantEffect || tool.Annotations.ReadOnlyHint == nil || *tool.Annotations.ReadOnlyHint != readOnly || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != destructive || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint || tool.Annotations.IdempotentHint != nil {
			t.Fatalf("annotations = %+v", tool.Annotations)
		}
	}
	h.d.SetFailing(true)
	failed, err := h.client.ListTools(context.Background(), caller)
	h.d.SetFailing(false)
	if err != nil || !reflect.DeepEqual(failed, listed) {
		t.Fatalf("failed-db list = %+v, %v", failed, err)
	}
}

func TestCatalog(t *testing.T) {
	// R-A3SL-LDCM R-DLJO-O6AE R-CZLH-SAXW R-YR67-B61N
	h := newHarness(t, false)
	h.seed(2)
	before := h.snapshot()
	for _, args := range []string{`{}`, `{"service":"","event":""}`, `{"service":"producer"}`, `{"event":"item.changed"}`, `{"service":"worker","event":"item.quiet"}`, `{"service":"unknown"}`} {
		var input struct{ Service, Event string }
		if err := json.Unmarshal([]byte(args), &input); err != nil {
			t.Fatal(err)
		}
		entries, err := h.st.Catalog(context.Background(), input.Service, input.Event)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			producers := make([]map[string]any, 0, len(e.Emits))
			for _, p := range e.Emits {
				producers = append(producers, map[string]any{"service": p.Service, "attrs": p.Attrs})
			}
			v := map[string]any{"event": e.Event, "emits": producers, "accepts": e.Accepts, "count": e.Count}
			if e.Count > 0 {
				v["last_seen"] = e.LastSeen.UTC().Format("2006-01-02T15:04:05.000000Z")
			}
			want = append(want, v)
		}
		out := success(t, h.call("catalog", args))
		equalJSON(t, out, marshal(t, map[string]any{"events": want}))
		checkKeys(t, out, "events")
		var decoded struct {
			Events []json.RawMessage `json:"events"`
		}
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatal(err)
		}
		for i, raw := range decoded.Events {
			k := []string{"event", "emits", "accepts", "count"}
			if entries[i].Count > 0 {
				k = append(k, "last_seen")
			}
			checkKeys(t, raw, k...)
			var item struct {
				Emits []json.RawMessage `json:"emits"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatal(err)
			}
			for _, p := range item.Emits {
				checkKeys(t, p, "service", "attrs")
			}
		}
		h.unchanged(before)
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestSearchFiltersAndRecords(t *testing.T) {
	// R-9NXW-MCPL R-DP7D-THIH R-9STI-5FOD R-9QDP-DW6Z R-YR67-B61N
	h := newHarness(t, false)
	h.seed(3)
	before := h.snapshot()
	ctx := context.Background()
	str := func(v string) *string { return &v }
	since := h.now.Add(2 * time.Second)
	until := h.now.Add(3 * time.Second)
	cases := []struct {
		args   string
		filter store.Filter
		limit  int
		before *int64
	}{
		{`{}`, store.Filter{}, paging.DefaultLimit, nil},
		{`{"since":null,"until":null,"user":null,"request_id":null,"cause":null,"limit":null,"page":null}`, store.Filter{}, paging.DefaultLimit, nil},
		{`{"page":"","limit":3.0}`, store.Filter{}, 3, nil},
		{fmt.Sprintf(`{"since":%q,"until":%q}`, since.Format(time.RFC3339Nano), until.Format(time.RFC3339Nano)), store.Filter{Since: &since, Until: &until}, paging.DefaultLimit, nil},
		{`{"services":["missing","producer"],"events":["item.quiet","item.changed"],"user":"alice","request_id":"request-1","cause":"evt_ffffffffffffffff"}`, store.Filter{Services: []string{"missing", "producer"}, Events: []string{"item.quiet", "item.changed"}, User: str("alice"), RequestID: str("request-1"), Cause: str("evt_ffffffffffffffff")}, paging.DefaultLimit, nil},
		{`{"user":"","request_id":"","cause":""}`, store.Filter{User: str(""), RequestID: str(""), Cause: str("")}, paging.DefaultLimit, nil},
		{`{"services":[],"events":[],"attrs":{}}`, store.Filter{}, paging.DefaultLimit, nil},
		{`{"attrs":{"number":1,"enabled":true,"label":"hello","large":9007199254740993,"unsigned":9223372036854775808}}`, store.Filter{Attrs: appEvents.Attrs{"number": int64(1), "enabled": true, "label": "hello", "large": int64(9007199254740993), "unsigned": uint64(9223372036854775808)}}, paging.DefaultLimit, nil},
		{`{"attrs":{"number":1.0}}`, store.Filter{Attrs: appEvents.Attrs{"number": float64(1)}}, paging.DefaultLimit, nil},
		{`{"attrs":{"number":1e0}}`, store.Filter{Attrs: appEvents.Attrs{"number": float64(1)}}, paging.DefaultLimit, nil},
		{`{"attrs":{"large":9007199254740992}}`, store.Filter{Attrs: appEvents.Attrs{"large": int64(9007199254740992)}}, paging.DefaultLimit, nil},
		{`{"limit":1}`, store.Filter{}, 1, nil},
		{`{"limit":500,"events":["missing"]}`, store.Filter{Events: []string{"missing"}}, paging.MaxLimit, nil},
	}
	first, err := h.st.Search(ctx, store.Filter{}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		args   string
		filter store.Filter
		limit  int
		before *int64
	}{fmt.Sprintf(`{"limit":1,"page":%q}`, paging.Token{Tool: "search"}.Encode(first[0].Seq)), store.Filter{}, 1, &first[0].Seq})
	for _, c := range cases {
		page, err := h.st.Search(ctx, c.filter, c.before, c.limit+1)
		if err != nil {
			t.Fatal(err)
		}
		out := success(t, h.call("search", c.args))
		records := make([]json.RawMessage, 0, len(page))
		for _, e := range page[:min(len(page), c.limit)] {
			raw, err := e.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, raw)
		}
		want := map[string]any{"records": records}
		k := []string{"records"}
		if len(page) > c.limit {
			want["next"] = paging.Token{Tool: "search"}.Encode(page[c.limit-1].Seq)
			k = append(k, "next")
		}
		equalJSON(t, out, marshal(t, want))
		checkKeys(t, out, k...)
		h.unchanged(before)
	}
	for _, value := range []string{"null", "[]", "{}", "1e9999"} {
		out := success(t, h.call("search", `{"attrs":{"number":`+value+`}}`))
		equalJSON(t, out, []byte(`{"records":[]}`))
		checkKeys(t, out, "records")
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestSearchDefaultAndNullLimit(t *testing.T) {
	// R-9NXW-MCPL R-9QDP-DW6Z
	h := newHarness(t, false)
	h.seed(paging.DefaultLimit + 1)
	for _, args := range []string{`{}`, `{"limit":null}`, `{"page":null}`, `{"page":""}`} {
		out := success(t, h.call("search", args))
		var page struct {
			Records []json.RawMessage `json:"records"`
			Next    string            `json:"next"`
		}
		if err := json.Unmarshal(out, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != paging.DefaultLimit || page.Next != (paging.Token{Tool: "search"}).Encode(int64(2)) {
			t.Fatalf("default page = %s", out)
		}
	}
}

func TestSearchPageTraversalAndChangedFilters(t *testing.T) {
	// R-9NXW-MCPL R-9QDP-DW6Z R-YR67-B61N
	h := newHarness(t, false)
	h.seed(6)
	before := h.snapshot()
	token := paging.Token{Tool: "search"}
	page := ""
	var seen []int64
	for round := 0; round < 3; round++ {
		out := success(t, h.call("search", fmt.Sprintf(`{"limit":2,"page":%q}`, page)))
		var result struct {
			Records []struct {
				Seq int64 `json:"seq"`
			} `json:"records"`
			Next *string `json:"next"`
		}
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Records) != 2 {
			t.Fatalf("page = %s", out)
		}
		for _, record := range result.Records {
			seen = append(seen, record.Seq)
		}
		if round < 2 {
			checkKeys(t, out, "records", "next")
			want := token.Encode(result.Records[1].Seq)
			if result.Next == nil || *result.Next != want {
				t.Fatalf("next = %v, want %s", result.Next, want)
			}
			page = *result.Next
		} else {
			checkKeys(t, out, "records")
			if result.Next != nil {
				t.Fatal("full final page carries next")
			}
		}
	}
	if !slices.Equal(seen, []int64{6, 5, 4, 3, 2, 1}) {
		t.Fatalf("sequence = %v", seen)
	}
	// Reuse a token issued with a different filter, continuing by sequence.
	out := success(t, h.call("search", `{"limit":1,"user":"alice"}`))
	var first struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal(out, &first); err != nil {
		t.Fatal(err)
	}
	changed := success(t, h.call("search", fmt.Sprintf(`{"limit":2,"user":"","page":%q}`, first.Next)))
	var result struct {
		Records []struct {
			Seq int64 `json:"seq"`
		} `json:"records"`
	}
	if err := json.Unmarshal(changed, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || result.Records[0].Seq != 4 || result.Records[1].Seq != 2 {
		t.Fatalf("changed filters = %s", changed)
	}
	checkKeys(t, changed, "records")
	h.unchanged(before)
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
	// The caller may continue after retention has removed the boundary.
	if err := h.st.Advance(context.Background(), "worker", 6); err != nil {
		t.Fatal(err)
	}
	if err := h.st.Sweep(context.Background(), h.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	equalJSON(t, success(t, h.call("search", fmt.Sprintf(`{"page":%q}`, first.Next))), []byte(`{"records":[]}`))
}

func TestSearchEmptyAndNumericValues(t *testing.T) {
	// R-9NXW-MCPL R-9STI-5FOD
	h := newHarness(t, false)
	equalJSON(t, success(t, h.call("search", `{}`)), []byte(`{"records":[]}`))
	h.seed(1)
	for i, number := range []float64{1000, 1.5} {
		e := appEvents.Event{ID: fmt.Sprintf("evt_%016x", i+2), Time: h.now, Service: "producer", Name: "item.changed", Attrs: appEvents.Attrs{"number": number}}
		if err := h.st.Deliver(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		text   string
		number float64
	}{{"1e3", 1000}, {"1.5", 1.5}} {
		out := success(t, h.call("search", `{"attrs":{"number":`+c.text+`}}`))
		page, err := h.st.Search(context.Background(), store.Filter{Attrs: appEvents.Attrs{"number": c.number}}, nil, paging.DefaultLimit+1)
		if err != nil || len(page) != 1 {
			t.Fatalf("numeric fixture search: %+v, %v", page, err)
		}
		record, err := page[0].MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		equalJSON(t, out, marshal(t, map[string]any{"records": []json.RawMessage{record}}))
	}
}

func TestSearchRefusalOrder(t *testing.T) {
	// R-T9G5-FWW9 R-9WH7-AQWG R-9YX0-2ADU R-A1CS-TTV8 R-9LI3-UT87
	h := newHarness(t, false)
	h.seed(1)
	before := h.snapshot()
	for _, failing := range []bool{false, true} {
		h.d.SetFailing(failing)
		for _, value := range []string{"", "2026-01-02", "today", "2026-01-02T03:04:05", "line\nbreak"} {
			refusal(t, h.call("search", fmt.Sprintf(`{"since":%q,"until":"bad","limit":0,"page":"page2"}`, value)), timeRefusal(t, "since", value))
			refusal(t, h.call("search", fmt.Sprintf(`{"until":%q,"limit":0,"page":"page2"}`, value)), timeRefusal(t, "until", value))
		}
		for _, limit := range []int64{-1, 0, paging.MaxLimit + 1, 9223372036854775807} {
			refusal(t, h.call("search", fmt.Sprintf(`{"limit":%d,"page":"page2"}`, limit)), limitRefusal(t, limit))
		}
		for _, page := range []string{"page2", (paging.Token{Tool: "other"}).Encode(int64(1)), "1", (paging.Token{Tool: "search"}).Encode("wrong-type")} {
			refusal(t, h.call("search", fmt.Sprintf(`{"page":%q}`, page)), paging.BadPage)
		}
		if failing {
			refusal(t, h.call("search", `{}`), tools.Unreachable)
		}
		h.d.SetFailing(false)
		h.unchanged(before)
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func subscriberJSON(s store.Subscriber) map[string]any {
	v := map[string]any{"service": s.Service, "status": string(s.Status), "cursor": s.Cursor, "lag": s.Lag, "since": s.Since.UTC().Format("2006-01-02T15:04:05.000000Z")}
	if s.Reason != nil {
		v["reason"] = map[string]any{"event": s.Reason.Event, "name": s.Reason.Name, "seq": s.Reason.Seq, "error": s.Reason.Error}
	}
	return v
}
func checkSubscriber(t *testing.T, raw []byte, s store.Subscriber) {
	t.Helper()
	equalJSON(t, raw, marshal(t, subscriberJSON(s)))
	k := []string{"service", "status", "cursor", "lag", "since"}
	if s.Reason != nil {
		k = append(k, "reason")
	}
	checkKeys(t, raw, k...)
	if s.Reason != nil {
		var v map[string]json.RawMessage
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		checkKeys(t, v["reason"], "event", "name", "seq", "error")
	}
}

func TestSubscribers(t *testing.T) {
	// R-A68E-CWU0 R-D0TE-62OL R-CZLH-SAXW R-YR67-B61N
	h := newHarness(t, false)
	equalJSON(t, success(t, h.call("subscribers", `{}`)), []byte(`{"subscribers":[]}`))
	h.seed(2)
	h.declare("z-gone", store.Declaration{Accepts: []string{"item.changed"}})
	if err := h.st.Forget(context.Background(), "z-gone"); err != nil {
		t.Fatal(err)
	}
	h.pause()
	before := h.snapshot()
	subs, err := h.st.Subscribers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := success(t, h.call("subscribers", `{}`))
	checkKeys(t, out, "subscribers")
	var v struct {
		Subscribers []json.RawMessage `json:"subscribers"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Subscribers) != len(subs) {
		t.Fatalf("subscribers = %s", out)
	}
	for i, s := range subs {
		checkSubscriber(t, v.Subscribers[i], s)
	}
	h.unchanged(before)
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestSkipAndResume(t *testing.T) {
	// R-E06H-9F6Q R-YTM0-2PJ1 R-E2MA-0YO4 R-YR67-B61N
	for _, name := range []string{"skip", "resume"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, true)
			h.seed(2)
			h.declare("z-observer", store.Declaration{Accepts: []string{"item.changed"}})
			h.pause()
			before, err := h.st.Subscribers(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			out := success(t, h.call(name, `{"service":"worker"}`))
			subs, err := h.st.Subscribers(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			checkSubscriber(t, out, subs[0])
			if !reflect.DeepEqual(subs[1:], before[1:]) {
				t.Fatalf("other subscribers changed: %+v, %+v", subs, before)
			}
			wantCursor := int64(0)
			if name == "skip" {
				wantCursor = 1
			}
			if subs[0].Status != store.StatusOK || subs[0].Cursor != wantCursor || subs[0].Reason != nil || subs[0].Lag != 2-wantCursor {
				t.Fatalf("after = %+v", subs[0])
			}
			events := h.ownEvents()
			skipped, called := -1, -1
			for i, e := range events {
				switch e.Name {
				case "event.skipped":
					if skipped >= 0 {
						t.Fatal("duplicate event.skipped")
					}
					skipped = i
					if e.RequestID != caller.RequestID || e.User != caller.UserID || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"event": "evt_0000000000000001", "service": "worker"}) {
						t.Fatalf("skip event = %+v", e)
					}
				case "tool.called":
					called = i
				case "request.started", "request.finished":
				default:
					t.Fatalf("unexpected event: %+v", e)
				}
			}
			if name == "skip" && (skipped < 0 || called < 0 || skipped >= called) {
				t.Fatalf("skip/call positions %d/%d: %v", skipped, called, events)
			}
			if name == "resume" && skipped >= 0 {
				t.Fatal("resume recorded skip")
			}
		})
	}
}

func TestControlRefusals(t *testing.T) {
	// R-TFJN-CRLQ R-YSE3-OXSC R-YR67-B61N
	h := newHarness(t, false)
	h.seed(1)
	h.declare("gone", store.Declaration{Accepts: []string{"item.changed"}})
	if err := h.st.Forget(context.Background(), "gone"); err != nil {
		t.Fatal(err)
	}
	before := h.snapshot()
	for _, tool := range []string{"skip", "resume"} {
		for _, name := range []string{"", "missing", "producer", "worker", "gone", "quote'\nline"} {
			want := fmt.Sprintf(tools.NoSubscriber, name)
			if name == "worker" || name == "gone" {
				want = fmt.Sprintf(tools.NotPaused, name)
			}
			if !strings.Contains(want, name) {
				t.Fatal("refusal lost service", name)
			}
			refusal(t, h.call(tool, fmt.Sprintf(`{"service":%q}`, name)), want)
			h.unchanged(before)
		}
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func invalidArguments(t *testing.T, r mcp.Result, offences int, required ...string) {
	t.Helper()
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("invalid refusal = %s", b)
	}
	text := result.Content[0].Text
	refusal(t, r, text)
	lines := strings.Split(text, "\n")
	if len(lines) != offences+1 || lines[0] != "invalid arguments:" {
		t.Fatalf("invalid refusal has wrong heading or offence count: %s", b)
	}
	for _, line := range lines[1:] {
		if line == "" {
			t.Fatalf("empty offence line: %s", b)
		}
	}
	for _, want := range required {
		if !slices.Contains(lines[1:], want) {
			t.Fatalf("invalid refusal lacks %q: %s", want, b)
		}
	}
}

func TestInvalidArguments(t *testing.T) {
	// R-9J2B-39QT R-YSE3-OXSC
	h := newHarness(t, false)
	h.seed(1)
	h.pause()
	before := h.snapshot()
	cases := []struct{ tool, args string }{
		{"catalog", `[]`}, {"catalog", `{"service":null}`}, {"catalog", `{"event":3}`}, {"search", `{"since":3}`}, {"search", `{"until":true}`}, {"search", `{"services":null}`}, {"search", `{"events":[4]}`}, {"search", `{"events":null}`}, {"search", `{"services":[4]}`}, {"search", `{"user":false}`}, {"search", `{"request_id":[]}`}, {"search", `{"cause":{}}`}, {"search", `{"attrs":null}`}, {"search", `{"attrs":[]}`}, {"search", `{"limit":3.5}`}, {"search", `{"limit":9223372036854775808}`}, {"search", `{"page":1}`}, {"search", `{"cursor":""}`}, {"skip", `{}`}, {"skip", `{"service":null}`}, {"resume", `{}`}, {"resume", `{"service":true}`},
	}
	for _, failing := range []bool{false, true} {
		h.d.SetFailing(failing)
		for _, tool := range []string{"catalog", "search", "subscribers", "skip", "resume"} {
			for _, nonObject := range []string{`[]`, `null`, `"text"`, `42`, `true`} {
				invalidArguments(t, h.call(tool, nonObject), 1)
			}
			args := `{"bogus":"x"}`
			if tool == "skip" || tool == "resume" {
				args = `{"service":"worker","bogus":"x"}`
			}
			refusal(t, h.call(tool, args), "invalid arguments:\nbogus: unknown field")
		}
		for _, c := range cases {
			invalidArguments(t, h.call(c.tool, c.args), 1)
		}
		refusal(t, h.call("search", `{"cursor":""}`), "invalid arguments:\ncursor: unknown field")
		invalidArguments(t, h.call("search", `{"since":3,"until":false,"limit":3.5,"page":42,"cursor":"","bogus":"x"}`), 6, "cursor: unknown field", "bogus: unknown field")
		for _, tool := range []string{"skip", "resume"} {
			invalidArguments(t, h.call(tool, `{"bogus":"x","extra":true}`), 3, "service: missing required field", "bogus: unknown field", "extra: unknown field")
			invalidArguments(t, h.call(tool, `{"service":false,"bogus":"x","extra":true}`), 3, "bogus: unknown field", "extra: unknown field")
			refusal(t, h.call(tool, `{}`), "invalid arguments:\nservice: missing required field")
		}
		h.d.SetFailing(false)
		h.unchanged(before)
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestUnavailableLog(t *testing.T) {
	// R-9LI3-UT87 R-YSE3-OXSC
	h := newHarness(t, false)
	h.seed(2)
	h.pause()
	before := h.snapshot()
	for _, tool := range []string{"catalog", "search", "subscribers", "skip", "resume"} {
		h.d.SetFailing(true)
		args := `{}`
		if tool == "skip" || tool == "resume" {
			args = `{"service":"worker"}`
		}
		refusal(t, h.call(tool, args), tools.Unreachable)
		h.d.SetFailing(false)
		h.unchanged(before)
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestUnknownTools(t *testing.T) {
	// R-YPYA-XEAY
	h := newHarness(t, false)
	h.seed(1)
	before := h.snapshot()
	for _, name := range []string{"emit", "", "unknown", "catalogue"} {
		_, err := h.client.CallTool(context.Background(), caller, name, json.RawMessage(`{}`))
		var rpc *mcp.RPCError
		if !errors.As(err, &rpc) || rpc.Code != mcp.CodeInvalidParams || rpc.Message != "Unknown tool: "+name {
			t.Fatalf("%q error = %v", name, err)
		}
		h.unchanged(before)
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestEveryCallerSeesSameBus(t *testing.T) {
	// R-DHVZ-IV2B
	h := newHarness(t, false)
	h.seed(2)
	other := identity.Caller{UserID: "other", Email: "other@example.test", RequestID: "other-request"}
	for _, name := range []string{"catalog", "search", "subscribers", "skip", "resume"} {
		args := `{}`
		if name == "skip" || name == "resume" {
			args = `{"service":"missing"}`
		}
		one := h.callAs(caller, name, args)
		two := h.callAs(other, name, args)
		if one.IsError() != two.IsError() {
			t.Fatal("caller changed outcome")
		}
		b1, err := one.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		b2, err := two.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		equalJSON(t, b1, b2)
	}
}

// R-9BQW-SNAN
func TestCopyConstants(_ *testing.T) {
	type copyText string
	const (
		unreachable = tools.Unreachable
		badTime     = tools.BadTime
		missing     = tools.NoSubscriber
		paused      = tools.NotPaused
	)
	_ = []copyText{unreachable, badTime, missing, paused}
}

func timeRefusal(t *testing.T, field, value string) string {
	t.Helper()
	text := fmt.Sprintf(tools.BadTime, field, value)
	if !strings.Contains(text, value) {
		t.Fatal("refusal lost supplied time")
	}
	return text
}

func limitRefusal(t *testing.T, limit int64) string {
	t.Helper()
	l := int(limit)
	_, err := paging.Window(&l)
	if err == nil {
		t.Fatal("expected invalid limit fixture")
	}
	text := err.Error()
	if !strings.Contains(text, strconv.FormatInt(limit, 10)) {
		t.Fatal("refusal lost supplied limit")
	}
	return text
}
