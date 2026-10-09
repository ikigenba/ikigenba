package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	assets "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
	"github.com/ikigenba/ikigenba/telemetry/internal/tools"
)

var ctx = context.Background()
var caller = identity.Caller{UserID: "reader", RequestID: "call"}
var fixed = time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)

type rig struct {
	database *db.DB
	s        *store.Store
	c        *mcp.Client
	w        *telemetry.Writer
	capture  *telemetry.Capture
}

func setup(t *testing.T, events ...telemetry.Event) *rig {
	t.Helper()
	return setupWithSink(t, false, events...)
}
func setupWithSink(t *testing.T, ownSink bool, events ...telemetry.Event) *rig {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	database, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "trail.db"), Migrations: assets.Migrations(), Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	s := store.New(database)
	for _, e := range events {
		if err := s.Deliver(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	capture := &telemetry.Capture{}
	var sink telemetry.Sink = capture
	if ownSink {
		sink = s
	}
	w := telemetry.New(telemetry.Config{Service: "telemetry", Version: "test", Sink: sink, Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}, Now: func() time.Time { return fixed }, Rand: bytes.NewReader(make([]byte, 4096))})
	t.Cleanup(func() { w.Shutdown(ctx, "done") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "telemetry", Version: "test", Telemetry: w})
	tools.Register(srv, s) // R-UPGW-IP2A
	h := httptest.NewServer(identity.Require(srv))
	t.Cleanup(h.Close)
	return &rig{database, s, mcp.NewClient(mcp.ClientConfig{Endpoint: h.URL, HTTPClient: h.Client()}), w, capture}
}
func call(t *testing.T, r *rig, name, args string) mcp.Result {
	t.Helper()
	result, err := r.c.CallTool(ctx, caller, name, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func encoded(t *testing.T, r mcp.Result) []byte {
	t.Helper()
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func equalJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// R-BCD9-RZAL: both success representations preserve order and compact text.
func success(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	if r.IsError() {
		t.Fatalf("refused: %s", encoded(t, r))
	}
	var envelope struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct{ Type, Text string }
		IsError    *bool `json:"isError"`
	}
	if err := json.Unmarshal(encoded(t, r), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.IsError != nil || len(envelope.Content) != 1 || envelope.Content[0].Type != "text" {
		t.Fatalf("bad success %s", encoded(t, r))
	}
	equalJSON(t, envelope.Structured, []byte(want))
	if !bytes.Equal(envelope.Structured, []byte(want)) {
		t.Fatalf("member order/representation got %s want %s", envelope.Structured, want)
	}
	compact := new(bytes.Buffer)
	if err := json.Compact(compact, []byte(envelope.Content[0].Text)); err != nil {
		t.Fatal(err)
	}
	if compact.String() != envelope.Content[0].Text || !bytes.Equal(envelope.Structured, compact.Bytes()) {
		t.Fatalf("representations differ: %s", encoded(t, r))
	}
}
func refusal(t *testing.T, r mcp.Result, text string) {
	t.Helper()
	if !r.IsError() {
		t.Fatalf("expected refusal %s", encoded(t, r))
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(encoded(t, r), &obj); err != nil {
		t.Fatal(err)
	}
	delete(obj, "_meta")
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	equalJSON(t, b, encoded(t, mcp.ErrorResult(text)))
}
func event(name, service, user, request string, offset int, attrs telemetry.Attrs) telemetry.Event {
	return telemetry.Event{Time: fixed.Add(time.Duration(offset) * time.Minute), Service: service, Name: name, User: user, RequestID: request, Attrs: attrs}
}
func recordsJSON(t *testing.T, es ...telemetry.Event) string {
	t.Helper()
	out := "{\"records\":["
	for i, e := range es {
		b, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			out += ","
		}
		out += string(b)
	}
	return out + "]}"
}

// R-B7HO-8WBT R-B8PK-MO2I R-B9XH-0FT7 R-9GWP-772R
func TestToolsList(t *testing.T) {
	r := setup(t)
	infos, err := r.c.ListTools(ctx, caller)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 4 {
		t.Fatalf("tools: %v", infos)
	}
	// R-9C13-O43Z R-9D90-1VUO R-9EGW-FNLD
	{
		info := infos[0]
		if info.Name != "catalog" || info.Description == "" || info.Effect() != mcp.Read {
			t.Fatalf("metadata: %+v", info)
		}
		a := info.Annotations
		if a.ReadOnlyHint == nil || !*a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint != nil {
			t.Fatalf("annotations: %+v", a)
		}
		equalJSON(t, schemaWithoutDescriptions(t, info.InputSchema), []byte(`{"type":"object","properties":{"service":{"type":"string"},"event":{"type":"string"}},"additionalProperties":false}`))
		equalJSON(t, schemaWithoutDescriptions(t, info.OutputSchema), []byte(`{"type":"object","properties":{"services":{"type":"array","items":{"type":"object","properties":{"service":{"type":"string"},"events":{"type":"array","items":{"type":"object","properties":{"event":{"type":"string"},"count":{"type":"integer"},"last_seen":{"type":"string"},"attrs":{"type":"array","items":{"type":"string"}}},"required":["event","count","last_seen","attrs"],"additionalProperties":false}}},"required":["service","events"],"additionalProperties":false}}},"required":["services"],"additionalProperties":false}`))
	}
	// R-9FOS-TFC2 R-9I4L-KYTG R-9JCH-YQK5
	{
		info := infos[1]
		if info.Name != "search" || info.Description == "" || info.Effect() != mcp.Read {
			t.Fatalf("metadata: %+v", info)
		}
		a := info.Annotations
		if a.ReadOnlyHint == nil || !*a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint != nil {
			t.Fatalf("annotations: %+v", a)
		}
		equalJSON(t, schemaWithoutDescriptions(t, info.InputSchema), []byte(`{"type":"object","properties":{"since":{"type":"string"},"until":{"type":"string"},"services":{"type":"array","items":{"type":"string"}},"events":{"type":"array","items":{"type":"string"}},"user":{"type":"string"},"request_id":{"type":"string"},"attrs":{"type":"object"},"limit":{"type":"integer"},"cursor":{"type":"string"}},"additionalProperties":false}`))
		equalJSON(t, schemaWithoutDescriptions(t, info.OutputSchema), []byte(`{"type":"object","properties":{"records":{"type":"array","items":{"type":"object","properties":{"time":{"type":"string"},"service":{"type":"string"},"event":{"type":"string"},"request_id":{"type":"string"},"user":{"type":"string"},"attrs":{"type":"object"}},"required":["time","service","event","request_id","user","attrs"],"additionalProperties":false}},"cursor":{"type":"string"}},"required":["records"],"additionalProperties":false}`))
	}
	// R-9KKE-CIAU R-9LSA-QA1J R-9N07-41S8
	{
		info := infos[2]
		if info.Name != "count" || info.Description == "" || info.Effect() != mcp.Read {
			t.Fatalf("metadata: %+v", info)
		}
		a := info.Annotations
		if a.ReadOnlyHint == nil || !*a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint != nil {
			t.Fatalf("annotations: %+v", a)
		}
		equalJSON(t, schemaWithoutDescriptions(t, info.InputSchema), []byte(`{"type":"object","properties":{"since":{"type":"string"},"until":{"type":"string"},"services":{"type":"array","items":{"type":"string"}},"events":{"type":"array","items":{"type":"string"}},"user":{"type":"string"},"request_id":{"type":"string"},"attrs":{"type":"object"},"by":{"type":"string"}},"additionalProperties":false}`))
		equalJSON(t, schemaWithoutDescriptions(t, info.OutputSchema), []byte(`{"type":"object","properties":{"total":{"type":"integer"},"groups":{"type":"array","items":{"type":"object","properties":{"key":{"type":"string"},"count":{"type":"integer"}},"required":["key","count"],"additionalProperties":false}}},"required":["total"],"additionalProperties":false}`))
	}
	// R-9O83-HTIX R-9PFZ-VL9M R-9QNW-9D0B
	{
		info := infos[3]
		if info.Name != "trace" || info.Description == "" || info.Effect() != mcp.Read {
			t.Fatalf("metadata: %+v", info)
		}
		a := info.Annotations
		if a.ReadOnlyHint == nil || !*a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint != nil {
			t.Fatalf("annotations: %+v", a)
		}
		equalJSON(t, schemaWithoutDescriptions(t, info.InputSchema), []byte(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`))
		equalJSON(t, schemaWithoutDescriptions(t, info.OutputSchema), []byte(`{"type":"object","properties":{"records":{"type":"array","items":{"type":"object","properties":{"time":{"type":"string"},"service":{"type":"string"},"event":{"type":"string"},"request_id":{"type":"string"},"user":{"type":"string"},"attrs":{"type":"object"}},"required":["time","service","event","request_id","user","attrs"],"additionalProperties":false}}},"required":["records"],"additionalProperties":false}`))
	}
}

// R-BUNR-IJF0 R-BVVN-WB5P R-BB5D-E7JW R-C5MU-YH39
func TestCatalogTraceAndReadOnly(t *testing.T) {
	a := event("request.start", "beta", "u", "req", 0, telemetry.Attrs{"status": int64(200)})
	b := event("request.finish", "alpha", "", "req", 1, telemetry.Attrs{})
	c := event("request.start", "beta", "v", "req", 0, telemetry.Attrs{"z": true})
	r := setup(t, a, b, c)
	success(t, call(t, r, "trace", `{"request_id":"req"}`), recordsJSON(t, a, c, b))
	success(t, call(t, r, "search", `{}`), recordsJSON(t, b, c, a))
	success(t, call(t, r, "trace", `{"request_id":"none"}`), `{"records":[]}`)
	success(t, call(t, r, "catalog", `{}`), `{"services":[{"service":"alpha","events":[{"event":"request.finish","count":1,"last_seen":"2026-10-02T14:01:00.000000Z","attrs":[]}]},{"service":"beta","events":[{"event":"request.start","count":2,"last_seen":"2026-10-02T14:00:00.000000Z","attrs":["status","z"]}]}]}`)
	success(t, call(t, r, "catalog", `{"service":"beta","event":"request.start"}`), `{"services":[{"service":"beta","events":[{"event":"request.start","count":2,"last_seen":"2026-10-02T14:00:00.000000Z","attrs":["status","z"]}]}]}`)
	for _, args := range []string{`{"service":"missing"}`, `{"event":"missing"}`} {
		success(t, call(t, r, "catalog", args), `{"services":[]}`)
	}
	success(t, call(t, r, "catalog", `{"service":"","event":""}`), `{"services":[{"service":"alpha","events":[{"event":"request.finish","count":1,"last_seen":"2026-10-02T14:01:00.000000Z","attrs":[]}]},{"service":"beta","events":[{"event":"request.start","count":2,"last_seen":"2026-10-02T14:00:00.000000Z","attrs":["status","z"]}]}]}`)
	for _, tool := range []string{"catalog", "search", "count", "trace"} {
		args := `{}`
		if tool == "trace" {
			args = `{"request_id":"req"}`
		}
		call(t, r, tool, args)
	}
	success(t, call(t, r, "search", `{}`), recordsJSON(t, b, c, a))
	if err := r.s.Sweep(ctx, fixed.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	success(t, call(t, r, "trace", `{"request_id":"req"}`), recordsJSON(t, b))
	success(t, call(t, r, "search", `{}`), recordsJSON(t, b))
	success(t, call(t, r, "count", `{}`), `{"total":1}`)
}

// R-CAKF-ZA46 R-JHOV-XQDA R-C82N-Q0KN R-A1MZ-PAOK R-CJ1R-5Y8W
func TestSearchFilterAndCount(t *testing.T) {
	a := event("test.one", "alpha", "u", "req", 0, telemetry.Attrs{"status": int64(500), "flag": true, "text": "ok"})
	b := event("test.two", "beta", "", "", 1, telemetry.Attrs{"status": "500", "flag": false})
	c := event("test.one", "alpha", "u", "req", 2, telemetry.Attrs{"status": int64(500), "flag": true, "text": "ok"})
	r := setup(t, a, b, c)
	cases := []struct {
		args   string
		events []telemetry.Event
	}{
		{`{"since":"2026-10-02T09:00:00-05:00","until":"2026-10-02T14:02:00Z","services":["unused","alpha"],"events":["test.one"],"user":"u","request_id":"req","attrs":{"status":500,"flag":true,"text":"ok"}}`, []telemetry.Event{a}},
		{`{"user":"","request_id":""}`, []telemetry.Event{b}},
		{`{"attrs":{"status":500}}`, []telemetry.Event{c, a}},
		{`{"attrs":{"status":"500"}}`, []telemetry.Event{b}},
		{`{"attrs":{"flag":false}}`, []telemetry.Event{b}},
		{`{"attrs":{"status":"wrong","status":500}}`, []telemetry.Event{c, a}},
		{`{"since":"2026-10-02T14:01:00.000001Z","until":"2026-10-02T14:02:00.000001Z"}`, []telemetry.Event{c}},
		{`{"since":"2026-10-02T14:02:00Z","until":"2026-10-02T14:02:00Z"}`, nil},
		{`{"since":"2026-10-02T14:03:00Z","until":"2026-10-02T14:01:00Z"}`, nil},
		{`{"services":["absent"]}`, nil},
		{`{"attrs":{"status":null}}`, nil}, {`{"attrs":{"status":{}}}`, nil}, {`{"attrs":{"status":[]}}`, nil}, {`{"attrs":{"status":1e400}}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			success(t, call(t, r, "search", tc.args), recordsJSON(t, tc.events...))
			total := len(tc.events)
			expected, _ := json.Marshal(map[string]int{"total": total})
			success(t, call(t, r, "count", tc.args), string(expected))
			if total == 0 {
				args := tc.args[:len(tc.args)-1] + `,"by":"service"}`
				success(t, call(t, r, "count", args), `{"total":0,"groups":[]}`)
			}
		})
	}
}

// R-BBES-HOSE
func TestTopLevelNulls(t *testing.T) {
	r := setup(t, event("test.one", "alpha", "u", "r", 0, nil), event("test.two", "beta", "", "", 1, nil))
	for _, tool := range []string{"search", "count"} {
		base := encoded(t, call(t, r, tool, `{}`))
		args := `{"since":null,"until":null,"user":null,"request_id":null`
		if tool == "search" {
			args += `,"limit":null,"cursor":null}`
		} else {
			args += `,"by":null}`
		}
		equalJSON(t, encoded(t, call(t, r, tool, args)), base)
	}
}

// R-9T3P-0WHP R-9VJH-SFZ3 R-9WRE-67PS R-9Z76-XR76 R-A0F3-BIXV R-B7R3-CDKB
func TestRefusalsAndOutcomes(t *testing.T) {
	r := setup(t)
	cases := []struct{ tool, args, text string }{
		{"search", `{"since":"yesterday","until":"bad","limit":0,"cursor":"page2"}`, fmt.Sprintf(tools.BadSince, "yesterday")},
		{"search", `{"until":"bad","limit":0,"cursor":"page2"}`, fmt.Sprintf(tools.BadUntil, "bad")},
		{"search", `{"since":""}`, fmt.Sprintf(tools.BadSince, "")},
		{"search", `{"limit":0,"cursor":"page2"}`, fmt.Sprintf(tools.BadLimit, 0)},
		{"search", `{"limit":501}`, fmt.Sprintf(tools.BadLimit, 501)},
		{"search", `{"cursor":"page2"}`, tools.BadCursor},
		{"count", `{"since":"bad","until":"bad","by":"path"}`, fmt.Sprintf(tools.BadSince, "bad")},
		{"count", `{"until":"2026-10-02 14:00","by":"path"}`, fmt.Sprintf(tools.BadUntil, "2026-10-02 14:00")},
		{"trace", `{"request_id":null}`, "invalid arguments:\nrequest_id: expected string, got null"},
	}
	for _, by := range []string{"path", "status", "attrs.", ""} {
		args, _ := json.Marshal(map[string]string{"by": by})
		cases = append(cases, struct{ tool, args, text string }{"count", string(args), fmt.Sprintf(tools.BadBy, by)})
	}
	for _, tc := range cases {
		refusal(t, call(t, r, tc.tool, tc.args), tc.text)
	}
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	events := r.capture.Events()
	if len(events) != len(cases) {
		t.Fatalf("events %d want %d", len(events), len(cases))
	}
	for i, e := range events {
		if e.Name != "tool.called" || e.Attrs["outcome"] != func() string {
			if cases[i].tool == "trace" {
				return "invalid_arguments"
			}
			return "error"
		}() {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
}

// R-9XZA-JZGH R-C6UR-C8TY
func TestPagesAndCursorPositions(t *testing.T) {
	es := []telemetry.Event{event("test.one", "alpha", "", "r", 0, nil), event("test.two", "beta", "", "r", 0, nil), event("test.three", "alpha", "", "r", 1, nil), event("test.four", "beta", "", "r", 2, nil)}
	r := setup(t, es...)
	var all []json.RawMessage
	cursor := ""
	for pageIndex := 0; pageIndex < 2; pageIndex++ {
		args := `{"limit":2}`
		if cursor != "" {
			b, _ := json.Marshal(map[string]any{"limit": 2, "cursor": cursor})
			args = string(b)
		}
		res := call(t, r, "search", args)
		if res.IsError() {
			t.Fatal(string(encoded(t, res)))
		}
		var env struct {
			Structured struct {
				Records []json.RawMessage
				Cursor  string
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(encoded(t, res), &env); err != nil {
			t.Fatal(err)
		}
		if len(env.Structured.Records) != 2 {
			t.Fatalf("page length %s", encoded(t, res))
		}
		if pageIndex == 0 && env.Structured.Cursor == "" || pageIndex == 1 && env.Structured.Cursor != "" {
			t.Fatalf("cursor %s", encoded(t, res))
		}
		want := recordsJSON(t, es[3], es[2])
		if pageIndex == 1 {
			want = recordsJSON(t, es[1], es[0])
		}
		if env.Structured.Cursor != "" {
			encodedCursor, err := json.Marshal(env.Structured.Cursor)
			if err != nil {
				t.Fatal(err)
			}
			want = want[:len(want)-1] + `,"cursor":` + string(encodedCursor) + "}"
		}
		success(t, res, want)
		if pageIndex == 0 {
			cursor = env.Structured.Cursor
			success(t, call(t, r, "search", `{"services":["alpha"],"limit":2,"cursor":"`+cursor+`"}`), recordsJSON(t, es[0]))
			damaged := "A" + cursor[1:]
			if damaged == cursor {
				damaged = "B" + cursor[1:]
			}
			refusal(t, call(t, r, "search", `{"cursor":"`+damaged+`"}`), tools.BadCursor)
		}
		all = append(all, env.Structured.Records...)
	}
	b, err := json.Marshal(struct {
		Records []json.RawMessage `json:"records"`
	}{all})
	if err != nil {
		t.Fatal(err)
	}
	equalJSON(t, b, []byte(recordsJSON(t, es[3], es[2], es[1], es[0])))
	many := make([]telemetry.Event, 51)
	for i := range many {
		many[i] = event("test.item", "alpha", "", "", i, nil)
	}
	rr := setup(t, many...)
	res := call(t, rr, "search", `{}`)
	var env struct {
		Structured struct {
			Records []json.RawMessage
			Cursor  string
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(encoded(t, res), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Structured.Records) != 50 || env.Structured.Cursor == "" {
		t.Fatalf("default page %s", encoded(t, res))
	}
	success(t, call(t, rr, "search", `{"limit":500}`), recordsJSON(t, reverse(many)...))
}
func reverse(es []telemetry.Event) []telemetry.Event {
	out := append([]telemetry.Event{}, es...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// R-A2UW-32F9 R-CFE2-0N0T R-CGLY-EERI
func TestCountGroups(t *testing.T) {
	r := setup(t,
		event("test.one", "z", "", "a", -1, telemetry.Attrs{"status": int64(200), "flag": true}),
		event("test.two", "a", "u", "b", 0, telemetry.Attrs{"status": "200", "flag": false}),
		event("test.one", "z", "u", "a", 0, telemetry.Attrs{"status": int64(500)}),
		event("test.two", "a", "", "b", 60, nil))
	cases := []struct{ by, want string }{
		{"service", `{"total":4,"groups":[{"key":"a","count":2},{"key":"z","count":2}]}`},
		{"event", `{"total":4,"groups":[{"key":"test.one","count":2},{"key":"test.two","count":2}]}`},
		{"user", `{"total":4,"groups":[{"key":"","count":2},{"key":"u","count":2}]}`},
		{"request_id", `{"total":4,"groups":[{"key":"a","count":2},{"key":"b","count":2}]}`},
		{"attrs.status", `{"total":4,"groups":[{"key":"200","count":2},{"key":"500","count":1}]}`},
		{"attrs.flag", `{"total":4,"groups":[{"key":"false","count":1},{"key":"true","count":1}]}`},
		{"attrs.missing", `{"total":4,"groups":[]}`},
		{"minute", `{"total":4,"groups":[{"key":"2026-10-02T13:59:00Z","count":1},{"key":"2026-10-02T14:00:00Z","count":2},{"key":"2026-10-02T15:00:00Z","count":1}]}`},
		{"hour", `{"total":4,"groups":[{"key":"2026-10-02T13:00:00Z","count":1},{"key":"2026-10-02T14:00:00Z","count":2},{"key":"2026-10-02T15:00:00Z","count":1}]}`},
		{"day", `{"total":4,"groups":[{"key":"2026-10-02T00:00:00Z","count":4}]}`},
	}
	for _, tc := range cases {
		args, _ := json.Marshal(map[string]string{"by": tc.by})
		success(t, call(t, r, "count", string(args)), tc.want)
	}
	success(t, call(t, r, "count", `{"services":["z"],"by":"service"}`), `{"total":2,"groups":[{"key":"z","count":2}]}`)
}

// R-9RVS-N4R0 R-YQO5-O9FH
func TestFailedReadsAndWriterIsolation(t *testing.T) {
	r := setup(t, event("test.one", "alpha", "", "r", 0, nil))
	before, err := r.s.Search(ctx, store.Filter{}, 500, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"catalog", "search", "count", "trace"} {
		args := `{}`
		if tool == "trace" {
			args = `{"request_id":"r"}`
		}
		call(t, r, tool, args)
	}
	after, err := r.s.Search(ctx, store.Filter{}, 500, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("trail changed: %+v %+v", before, after)
	}
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	events := r.capture.Events()
	if len(events) != 4 {
		t.Fatalf("writer events: %+v", events)
	}
	for _, e := range events {
		if e.Name != "tool.called" || e.Attrs["outcome"] != "ok" {
			t.Fatalf("extra/wrong event %+v", e)
		}
	}
	r.database.SetFailing(true)
	for _, tc := range []struct{ name, args string }{{"catalog", `{}`}, {"search", `{}`}, {"search", `{"services":["absent"]}`}, {"search", `{"attrs":{"status":null}}`}, {"search", `{"since":"2026-10-03T00:00:00Z","until":"2026-10-02T00:00:00Z"}`}, {"count", `{}`}, {"count", `{"by":"service"}`}, {"count", `{"by":"attrs.missing"}`}, {"count", `{"attrs":{"status":null}}`}, {"trace", `{"request_id":"missing"}`}} {
		before := len(r.capture.Events())
		refusal(t, call(t, r, tc.name, tc.args), tools.ReadFailed)
		if err := r.w.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		added := r.capture.Events()[before:]
		if len(added) != 1 || added[0].Name != "tool.called" || added[0].Attrs["outcome"] != "error" {
			t.Fatalf("%s failure events: %+v", tc.name, added)
		}
	}
	refusal(t, call(t, r, "search", `{"cursor":"page2"}`), tools.BadCursor)
	refusal(t, call(t, r, "count", `{"by":"path"}`), fmt.Sprintf(tools.BadBy, "path"))
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	events = r.capture.Events()
	if len(events) != 16 {
		t.Fatalf("events: %+v", events)
	}
	for _, e := range events[4:] {
		if e.Name != "tool.called" || e.Attrs["outcome"] != "error" {
			t.Fatalf("wrong outcome %+v", e)
		}
	}
}

// R-CAKF-ZA46 R-JHOV-XQDA R-BB5D-E7JW
func TestNumberFormsAndRecordTime(t *testing.T) {
	minusZero := json.Number("-0")
	var negativeZero float64
	if err := json.Unmarshal([]byte(minusZero), &negativeZero); err != nil {
		t.Fatal(err)
	}
	nums := []struct {
		input string
		value any
	}{
		{"18446744073709551615", uint64(18446744073709551615)},
		{"-9223372036854775808", int64(-9223372036854775808)},
		{"5e2", float64(500)},
		{"1.25", float64(1.25)},
		{"-0", negativeZero},
	}
	for _, tc := range nums {
		t.Run(tc.input, func(t *testing.T) {
			a := event("test.number", "alpha", "u", "r", 0, telemetry.Attrs{"value": tc.value})
			a.Time = time.Date(2026, 10, 2, 9, 0, 0, 123456789, time.FixedZone("offset", -5*3600))
			r := setup(t, a, event("test.string", "alpha", "u", "r", 0, telemetry.Attrs{"value": tc.input}))
			success(t, call(t, r, "search", `{"attrs":{"value":`+tc.input+`}}`), recordsJSON(t, a))
			success(t, call(t, r, "count", `{"attrs":{"value":`+tc.input+`}}`), `{"total":1}`)
		})
	}
}

// R-YQO5-O9FH R-BUNR-IJF0 R-BVVN-WB5P
func TestLiveTrailAndWriterSink(t *testing.T) {
	a := event("test.one", "alpha", "", "r", 0, nil)
	r := setupWithSink(t, true, a)
	success(t, call(t, r, "trace", `{"request_id":"r"}`), recordsJSON(t, a))
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rs, err := r.s.Trace(ctx, caller.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Event != "tool.called" {
		t.Fatalf("writer not in trail %+v", rs)
	}
	b := event("test.two", "beta", "", "r", 1, nil)
	if err := r.s.Deliver(ctx, b); err != nil {
		t.Fatal(err)
	}
	success(t, call(t, r, "trace", `{"request_id":"r"}`), recordsJSON(t, a, b))
	success(t, call(t, r, "catalog", `{"service":"beta"}`), `{"services":[{"service":"beta","events":[{"event":"test.two","count":1,"last_seen":"2026-10-02T14:01:00.000000Z","attrs":[]}]}]}`)
}

func schemaWithoutDescriptions(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var strip func(any)
	strip = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			delete(v, "description")
			for _, child := range v {
				strip(child)
			}
		case []any:
			for _, child := range v {
				strip(child)
			}
		}
	}
	strip(value)
	result, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// R-99LA-WKML
func TestRefusalConstants(t *testing.T) {
	const read string = tools.ReadFailed
	const cursor string = tools.BadCursor
	if read == "" || cursor == "" {
		t.Fatal("empty refusal")
	}
	const since string = tools.BadSince
	const until string = tools.BadUntil
	const by string = tools.BadBy
	const limit string = tools.BadLimit
	for _, format := range []string{since, until, by} {
		if strings.Count(format, "%") != 1 || !strings.Contains(format, "%s") {
			t.Fatal("string refusal format", format)
		}
	}
	if strings.Count(limit, "%") != 1 || !strings.Contains(limit, "%d") {
		t.Fatal("limit refusal format", tools.BadLimit)
	}
}
