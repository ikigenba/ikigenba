package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/pages"
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
	page, err := h.st.Search(ctx, store.Filter{}, 500, "")
	if err != nil {
		h.t.Fatal(err)
	}
	return marshal(h.t, []any{subs, head, catalog, page})
}

func TestToolList(t *testing.T) {
	// R-D21A-JUFA R-YW1S-U90F R-D5OZ-P5ND R-D6WW-2XE2 R-D84S-GP4R R-D9CO-UGVG R-DAKL-88M5 R-DBSH-M0CU
	h := newHarness(t, false)
	listed, err := h.client.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"catalog", "search", "subscribers", "skip", "resume"}
	if len(listed) != len(wantNames) {
		t.Fatalf("tools = %v", listed)
	}
	descriptions := []string{"Every event the suite emits, who emits and accepts it, counts and last seen.", "The retained log, newest first, filtered by service, event, user, request id, cause or attributes.", "Each subscriber's status, reason, cursor and lag.", "Skip the event a paused subscriber is stuck on and resume it.", "Retry the event a paused subscriber is stuck on."}
	schemas := []string{`{"type":"object","properties":{"service":{"type":"string"},"event":{"type":"string"}},"additionalProperties":false}`, `{"type":"object","properties":{"since":{"type":"string"},"until":{"type":"string"},"services":{"type":"array","items":{"type":"string"}},"events":{"type":"array","items":{"type":"string"}},"user":{"type":"string"},"request_id":{"type":"string"},"cause":{"type":"string"},"attrs":{"type":"object"},"limit":{"type":"integer"},"cursor":{"type":"string"}},"additionalProperties":false}`, `{"type":"object","additionalProperties":false}`, `{"type":"object","properties":{"service":{"type":"string"}},"required":["service"],"additionalProperties":false}`, `{"type":"object","properties":{"service":{"type":"string"}},"required":["service"],"additionalProperties":false}`}
	propertyOrder := [][]string{{"service", "event"}, {"since", "until", "services", "events", "user", "request_id", "cause", "attrs", "limit", "cursor"}, nil, {"service"}, {"service"}}
	for i, tool := range listed {
		if tool.Name != wantNames[i] || strings.SplitN(tool.Description, "\n", 2)[0] != descriptions[i] {
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
			for _, p := range props {
				delete(p, "description")
			}
			schema["properties"] = marshal(t, props)
		}
		equalJSON(t, marshal(t, schema), []byte(schemas[i]))
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
	// R-YYHL-LSHT R-DLJO-O6AE R-CZLH-SAXW R-YR67-B61N
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
	// R-VQH6-MZ9J R-DP7D-THIH R-DQFA-7996 R-DRN6-L0ZV R-VRP3-0R08 R-YR67-B61N
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
		after  store.Cursor
	}{
		{`{}`, store.Filter{}, 50, ""},
		{`{"since":null,"until":null,"user":null,"request_id":null,"cause":null,"limit":null,"cursor":null}`, store.Filter{}, 50, ""},
		{`{"cursor":"","limit":3.0}`, store.Filter{}, 3, ""},
		{fmt.Sprintf(`{"since":%q,"until":%q}`, since.Format(time.RFC3339Nano), until.Format(time.RFC3339Nano)), store.Filter{Since: &since, Until: &until}, 50, ""},
		{`{"services":["missing","producer"],"events":["item.quiet","item.changed"],"user":"alice","request_id":"request-1","cause":"evt_ffffffffffffffff"}`, store.Filter{Services: []string{"missing", "producer"}, Events: []string{"item.quiet", "item.changed"}, User: str("alice"), RequestID: str("request-1"), Cause: str("evt_ffffffffffffffff")}, 50, ""},
		{`{"user":"","request_id":"","cause":""}`, store.Filter{User: str(""), RequestID: str(""), Cause: str("")}, 50, ""},
		{`{"services":[],"events":[],"attrs":{}}`, store.Filter{}, 50, ""},
		{`{"attrs":{"number":1,"enabled":true,"label":"hello","large":9007199254740993,"unsigned":9223372036854775808}}`, store.Filter{Attrs: appEvents.Attrs{"number": int64(1), "enabled": true, "label": "hello", "large": int64(9007199254740993), "unsigned": uint64(9223372036854775808)}}, 50, ""},
		{`{"attrs":{"number":1.0}}`, store.Filter{Attrs: appEvents.Attrs{"number": float64(1)}}, 50, ""},
		{`{"attrs":{"number":1e0}}`, store.Filter{Attrs: appEvents.Attrs{"number": float64(1)}}, 50, ""},
		{`{"attrs":{"large":9007199254740992}}`, store.Filter{Attrs: appEvents.Attrs{"large": int64(9007199254740992)}}, 50, ""},
		{`{"limit":1}`, store.Filter{}, 1, ""},
		{`{"limit":500,"events":["missing"]}`, store.Filter{Events: []string{"missing"}}, 500, ""},
	}
	first, err := h.st.Search(ctx, store.Filter{}, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		args   string
		filter store.Filter
		limit  int
		after  store.Cursor
	}{fmt.Sprintf(`{"limit":1,"cursor":%q}`, first.Next), store.Filter{}, 1, first.Next})
	for _, c := range cases {
		page, err := h.st.Search(ctx, c.filter, c.limit, c.after)
		if err != nil {
			t.Fatal(err)
		}
		out := success(t, h.call("search", c.args))
		records := make([]json.RawMessage, 0, len(page.Records))
		for _, e := range page.Records {
			raw, err := e.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			records = append(records, raw)
		}
		want := map[string]any{"records": records}
		k := []string{"records"}
		if page.Next != "" {
			want["cursor"] = string(page.Next)
			k = append(k, "cursor")
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
	// R-DRN6-L0ZV R-VRP3-0R08
	h := newHarness(t, false)
	h.seed(51)
	for _, args := range []string{`{}`, `{"limit":null}`} {
		out := success(t, h.call("search", args))
		var page struct {
			Records []json.RawMessage `json:"records"`
			Cursor  string            `json:"cursor"`
		}
		if err := json.Unmarshal(out, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != 50 || page.Cursor == "" {
			t.Fatalf("default page = %s", out)
		}
		p, err := h.st.Search(context.Background(), store.Filter{}, 50, "")
		if err != nil {
			t.Fatal(err)
		}
		if page.Cursor != string(p.Next) {
			t.Fatalf("cursor = %q, want %q", page.Cursor, p.Next)
		}
	}
}

func TestSearchEmptyAndNumericValues(t *testing.T) {
	// R-VQH6-MZ9J R-DQFA-7996 R-VRP3-0R08
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
		page, err := h.st.Search(context.Background(), store.Filter{Attrs: appEvents.Attrs{"number": c.number}}, 50, "")
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("numeric fixture search: %+v, %v", page, err)
		}
		record, err := page.Records[0].MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		equalJSON(t, out, marshal(t, map[string]any{"records": []json.RawMessage{record}}))
	}
}

func TestSearchRefusalOrder(t *testing.T) {
	// R-DU2Z-CKH9 R-DVAV-QC7Y R-DWIS-43YN R-YX9P-80R4 R-YOQE-JMK9
	h := newHarness(t, false)
	h.seed(1)
	before := h.snapshot()
	for _, failing := range []bool{false, true} {
		h.d.SetFailing(failing)
		for _, value := range []string{"", "2026-01-02", "today", "2026-01-02T03:04:05", "line\nbreak"} {
			refusal(t, h.call("search", fmt.Sprintf(`{"since":%q,"until":"bad","limit":0,"cursor":"page2"}`, value)), "since is not an RFC 3339 time: '"+value+"'")
			refusal(t, h.call("search", fmt.Sprintf(`{"until":%q,"limit":0,"cursor":"page2"}`, value)), "until is not an RFC 3339 time: '"+value+"'")
		}
		for _, limit := range []int64{-1, 0, 501, 9223372036854775807} {
			refusal(t, h.call("search", fmt.Sprintf(`{"limit":%d,"cursor":"page2"}`, limit)), fmt.Sprintf("limit must be between 1 and 500, got %d", limit))
		}
		refusal(t, h.call("search", `{"cursor":"page2"}`), "cursor is not one search issued")
		if failing {
			refusal(t, h.call("search", `{}`), "cannot reach the log; try again later")
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
	// R-YZPH-ZK8I R-D0TE-62OL R-CZLH-SAXW R-YR67-B61N
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
	// R-YUTW-GH9Q R-YSE3-OXSC R-YR67-B61N
	h := newHarness(t, false)
	h.seed(1)
	h.declare("gone", store.Declaration{Accepts: []string{"item.changed"}})
	if err := h.st.Forget(context.Background(), "gone"); err != nil {
		t.Fatal(err)
	}
	before := h.snapshot()
	for _, tool := range []string{"skip", "resume"} {
		for _, name := range []string{"", "missing", "producer", "worker", "gone", "quote'\nline"} {
			want := "no subscriber '" + name + "'"
			if name == "worker" || name == "gone" {
				want = "'" + name + "' is not paused"
			}
			refusal(t, h.call(tool, fmt.Sprintf(`{"service":%q}`, name)), want)
			h.unchanged(before)
		}
	}
	if got := h.ownEvents(); len(got) != 0 {
		t.Fatalf("own events = %v", got)
	}
}

func TestInvalidArguments(t *testing.T) {
	// R-YNII-5UTK R-YSE3-OXSC
	h := newHarness(t, false)
	h.seed(1)
	h.pause()
	before := h.snapshot()
	cases := []struct{ tool, args string }{
		{"catalog", `[]`}, {"catalog", `{"service":null}`}, {"catalog", `{"event":3}`}, {"search", `{"since":3}`}, {"search", `{"until":true}`}, {"search", `{"services":null}`}, {"search", `{"events":[4]}`}, {"search", `{"user":false}`}, {"search", `{"request_id":[]}`}, {"search", `{"cause":{}}`}, {"search", `{"attrs":null}`}, {"search", `{"attrs":[]}`}, {"search", `{"limit":3.5}`}, {"search", `{"limit":9223372036854775808}`}, {"search", `{"cursor":1}`}, {"skip", `{}`}, {"skip", `{"service":null}`}, {"resume", `{}`}, {"resume", `{"service":true}`},
	}
	for _, failing := range []bool{false, true} {
		h.d.SetFailing(failing)
		for _, tool := range []string{"catalog", "search", "subscribers", "skip", "resume"} {
			for _, nonObject := range []string{`[]`, `null`, `"text"`, `42`, `true`} {
				r := h.call(tool, nonObject)
				if !r.IsError() {
					t.Fatalf("accepted nonobject %s arguments %s", tool, nonObject)
				}
				b, err := r.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				var v struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				if err := json.Unmarshal(b, &v); err != nil {
					t.Fatal(err)
				}
				if len(v.Content) != 1 || !strings.HasPrefix(v.Content[0].Text, "invalid arguments:") {
					t.Fatalf("nonobject refusal = %s", b)
				}
			}
			args := `{"bogus":"x"}`
			if tool == "skip" || tool == "resume" {
				args = `{"service":"worker","bogus":"x"}`
			}
			refusal(t, h.call(tool, args), "invalid arguments:\nbogus: unknown field")
		}
		for _, c := range cases {
			r := h.call(c.tool, c.args)
			if !r.IsError() {
				t.Fatalf("accepted %s %s", c.tool, c.args)
			}
			b, err := r.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var v struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			if len(v.Content) != 1 || !strings.HasPrefix(v.Content[0].Text, "invalid arguments:\n") {
				t.Fatalf("invalid refusal = %s", b)
			}
		}
		for _, tool := range []string{"skip", "resume"} {
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
	// R-YOQE-JMK9 R-YSE3-OXSC
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
		refusal(t, h.call(tool, args), "cannot reach the log; try again later")
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

func TestLandingToolDescriptions(t *testing.T) {
	// R-A4TK-YX2K
	h := newHarness(t, false)
	listed, err := h.client.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	p := pages.New(pages.Config{Store: h.st})
	r := httptest.NewRequest(http.MethodGet, "https://events.space.test/", nil)
	r.Header.Set("X-User-Id", caller.UserID)
	r.Header.Set("X-User-Email", caller.Email)
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	p.Landing(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("landing status = %d", w.Code)
	}
	section := regexp.MustCompile(`(?s)<dl[^>]*id="tools"[^>]*>(.*?)</dl>`).FindStringSubmatch(w.Body.String())
	if len(section) != 2 {
		t.Fatal("missing tools dl")
	}
	rows := regexp.MustCompile(`(?s)<dt[^>]*data-tool="([^"]+)"[^>]*>.*?</dt>\s*<dd[^>]*>(.*?)</dd>`).FindAllStringSubmatch(section[1], -1)
	if len(rows) != len(listed) {
		t.Fatalf("tool rows = %d", len(rows))
	}
	for i, tool := range listed {
		if rows[i][1] != tool.Name || html.UnescapeString(strings.TrimSpace(rows[i][2])) != strings.SplitN(tool.Description, "\n", 2)[0] {
			t.Fatalf("row %v disagrees with %+v", rows[i], tool)
		}
	}
}
