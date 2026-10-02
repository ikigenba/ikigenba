package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy/internal/cli"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/tools"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

var caller = identity.Caller{UserID: "tool-tester"}

func clientOver(t *testing.T, s *widget.Store) *mcp.Client {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _, _ := capturingWriter(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: cli.Version, Telemetry: writer})
	// R-L86I-LW1U: using the public registration signature.
	tools.Register(srv, s, writer)
	httpServer := httptest.NewServer(identity.Require(srv))
	t.Cleanup(httpServer.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: httpServer.URL, HTTPClient: httpServer.Client()})
}

func call(t *testing.T, c *mcp.Client, name string, args json.RawMessage) mcp.Result {
	t.Helper()
	result, err := c.CallTool(context.Background(), caller, name, args)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func jsonEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON = %s; want %s", got, want)
	}
}

const widgetSchema = `{"type":"object","properties":{"name":{"type":"string","description":"The widget's name: 1 to 40 characters after trimming, unique."},"count":{"type":"integer","description":"How many: a whole number, zero or more."},"status":{"type":"string","enum":["active","paused","retired"],"description":"The widget's status."}},"required":["name","count","status"],"additionalProperties":false}`

var widgetOutputSchema = strings.Replace(strings.Replace(widgetSchema, `"properties":{`, `"properties":{"id":{"type":"string","description":"The widget's id, which names it in dummy's telemetry trail."},`, 1), `"required":["name"`, `"required":["id","name"`, 1)

func knownSource() io.Reader {
	data := make([]byte, 8*256)
	for i := range 256 {
		data[i*8] = byte(i)
	}
	return bytes.NewReader(data)
}

func capturingWriter(t *testing.T) (*telemetry.Writer, *telemetry.Capture, *bytes.Buffer) {
	t.Helper()
	capture := &telemetry.Capture{}
	stderr := &bytes.Buffer{}
	writer := telemetry.New(telemetry.Config{Service: panel.ServiceName, Version: cli.Version, Sink: capture, Stderr: stderr, Now: func() time.Time { return time.Unix(1000, 0) }, Sleep: func(context.Context, time.Duration) { t.Error("unexpected telemetry retry") }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 4096))})
	t.Cleanup(func() {
		writer.Shutdown(context.Background(), "test complete")
		if stderr.Len() != 0 {
			t.Errorf("telemetry stderr: %s", stderr)
		}
	})
	return writer, capture, stderr
}

func TestAdvertisedTools(t *testing.T) {
	c := clientOver(t, widget.NewStore(knownSource()))
	infos, err := c.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	// R-L9EE-ZNSJ: typed registration advertises both output schemas in order.
	if len(infos) != 2 || infos[0].Name != "list_widgets" || infos[1].Name != "create_widget" || len(infos[0].OutputSchema) == 0 || len(infos[1].OutputSchema) == 0 {
		t.Fatalf("tools = %+v", infos)
	}
	// R-E0MH-RS5V, R-EHP3-4KJL.
	if infos[0].Description != "List the widgets, oldest first." {
		t.Fatal(infos[0].Description)
	}
	if infos[1].Description != "Create a widget and return it.\n\nThe name is trimmed of surrounding white space and must then be 1 to 40 characters and not already taken (letter case counts). The count is a whole number, zero or more. Every rule the arguments break is reported in one error, and nothing is created unless all of them hold." {
		t.Fatal(infos[1].Description)
	}
	// R-CJ5K-XZLQ, R-CKDH-BRCF.
	for i, info := range infos {
		a := info.Annotations
		if a.ReadOnlyHint == nil || *a.ReadOnlyHint != (i == 0) || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint || a.IdempotentHint != nil {
			t.Fatalf("annotations = %+v", a)
		}
		want := mcp.Read
		if i == 1 {
			want = mcp.Additive
		}
		if info.Effect() != want {
			t.Fatalf("effect = %v", info.Effect())
		}
	}
	// R-E6PZ-OMVC.
	jsonEqual(t, infos[0].InputSchema, `{"type":"object","additionalProperties":false}`)
	// R-EADO-TY3F, R-LD24-4Z0M, R-LBU7-R79X, R-ENSL-1F92, R-LEA0-IQRB.
	jsonEqual(t, infos[0].OutputSchema, `{"type":"object","properties":{"widgets":{"type":"array","items":`+widgetOutputSchema+`,"description":"Every widget, oldest first."}},"required":["widgets"],"additionalProperties":false}`)
	jsonEqual(t, infos[1].InputSchema, widgetSchema)
	jsonEqual(t, infos[1].OutputSchema, widgetOutputSchema)
}

func widgetJSON(t *testing.T, w widget.Widget) string {
	t.Helper()
	encoded, err := json.Marshal(struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Count  int    `json:"count"`
		Status string `json:"status"`
	}{w.ID, w.Name, w.Count, string(w.Status)})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func listJSON(t *testing.T, widgets []widget.Widget) string {
	t.Helper()
	objects := make([]string, len(widgets))
	for i, w := range widgets {
		objects[i] = widgetJSON(t, w)
	}
	return `{"widgets":[` + strings.Join(objects, ",") + `]}`
}

func assertSuccess(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	if _, exists := members["isError"]; exists {
		t.Fatalf("unexpected isError: %s", raw)
	}
	jsonEqual(t, members["structuredContent"], want)
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(members["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Type != "text" {
		t.Fatalf("content = %s", members["content"])
	}
	jsonEqual(t, json.RawMessage(content[0].Text), want)
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(content[0].Text)); err != nil {
		t.Fatal(err)
	}
	if content[0].Text != compact.String() {
		t.Fatalf("text has whitespace outside strings: %s", members["content"])
	}
	if !reflect.DeepEqual(jsonTokens(t, content[0].Text), jsonTokens(t, string(members["structuredContent"]))) || !reflect.DeepEqual(jsonTokens(t, content[0].Text), jsonTokens(t, want)) {
		t.Fatalf("text and structured content differ in member order: %s", members["content"])
	}
	expected, _ := json.Marshal([]map[string]string{{"type": "text", "text": content[0].Text}})
	jsonEqual(t, members["content"], string(expected))
}

func jsonTokens(t *testing.T, raw string) []json.Token {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	var tokens []json.Token
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return tokens
		}
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
}

func assertError(t *testing.T, r mcp.Result, text string) {
	t.Helper()
	got, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(got, &members); err != nil {
		t.Fatal(err)
	}
	delete(members, "_meta")
	got, err = json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(mcp.ErrorResult(text))
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, got, string(want))
}

func TestListResultAndReadOnly(t *testing.T) {
	s := widget.NewStore(knownSource())
	_, errs := s.Create(widget.Draft{Name: "last \"widget\"", Count: 8, Status: widget.StatusPaused})
	if errs.Any() {
		t.Fatal(errs)
	}
	c := clientOver(t, s)
	before := s.All()
	// R-LFHW-WII0, R-9E70-T554, R-EYRO-HCXB.
	for _, args := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		assertSuccess(t, call(t, c, "list_widgets", args), listJSON(t, before))
		if !reflect.DeepEqual(s.All(), before) {
			t.Fatal("listing changed the store")
		}
	}
}

func draftInput(d widget.Draft) any {
	return struct {
		Name   string        `json:"name"`
		Count  int           `json:"count"`
		Status widget.Status `json:"status"`
	}{d.Name, d.Count, d.Status}
}

func TestCreateMatchesStoreOutcome(t *testing.T) {
	cases := []widget.Draft{
		{Name: " \tnew \"雪\"\n", Count: 7, Status: widget.StatusPaused},
		{Name: "Alpha", Count: 0, Status: widget.StatusRetired},
		{Name: " \n", Count: 1, Status: widget.StatusActive},
		{Name: strings.Repeat("雪", widget.MaxNameRunes+1), Count: 1, Status: widget.StatusActive},
		{Name: " alpha ", Count: 1, Status: widget.StatusActive},
		{Name: "new", Count: -1, Status: widget.StatusActive},
		{Name: "alpha", Count: -3, Status: widget.StatusPaused},
	}
	for i, draft := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s, expected := widget.NewStore(knownSource()), widget.NewStore(knownSource())
			c := clientOver(t, s)
			inputBytes, err := json.Marshal(draftInput(draft))
			if err != nil {
				t.Fatal(err)
			}
			input := string(inputBytes)
			w, errs := expected.Create(draft)
			result := call(t, c, "create_widget", json.RawMessage(input))
			// R-5XXV-7CQL: effects match one domain Create, including refusal.
			if !reflect.DeepEqual(s.All(), expected.All()) {
				t.Fatalf("store = %+v; want %+v", s.All(), expected.All())
			}
			if !errs.Any() {
				// R-9FEX-6WVT: ordered compact widget object and matching text.
				assertSuccess(t, result, widgetJSON(t, w))
			} else {
				// R-M5QI-CZCS: exact error members and field order.
				text := "invalid arguments:"
				if errs.Name != "" {
					text += "\nname: " + errs.Name
				}
				if errs.Count != "" {
					text += "\ncount: " + errs.Count
				}
				assertError(t, result, text)
			}
		})
	}
}

func TestArgumentOffencesLeaveStoreUnchanged(t *testing.T) {
	cases := []struct{ name, args string }{
		{"list_widgets", `{"unexpected":1}`},
		{"create_widget", `{"name":"new","count":1,"status":"unknown"}`},
		{"create_widget", `{"name":"alpha","count":"-2","status":"active"}`},
		{"create_widget", `{"name":"new","count":1.5,"status":"active"}`},
		{"create_widget", `{"name":"new","status":"active"}`},
		{"create_widget", `{"name":"new","count":1,"status":"active","extra":true}`},
	}
	// R-F8IV-JIUV: decode offences preclude any mutation for both tools.
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s := widget.NewStore(knownSource())
			c := clientOver(t, s)
			before := s.All()
			result := call(t, c, tc.name, json.RawMessage(tc.args))
			if !result.IsError() {
				t.Fatal("offence accepted")
			}
			if !reflect.DeepEqual(s.All(), before) {
				t.Fatal("decode offence changed store")
			}
		})
	}
}

func TestCountRange(t *testing.T) {
	s := widget.NewStore(knownSource())
	c := clientOver(t, s)
	// R-FAYO-B2C9: integral JSON values beyond either int64 bound.
	for _, number := range []string{"-9223372036854775809", "9223372036854775808"} {
		result := call(t, c, "create_widget", json.RawMessage(`{"name":"new","count":`+number+`,"status":"active"}`))
		assertError(t, result, "invalid arguments:\ncount: must be between -9223372036854775808 and 9223372036854775807, got "+number)
	}
}

func TestConcurrentCreateSameName(t *testing.T) {
	s := widget.NewStore(knownSource())
	c := clientOver(t, s)
	before := s.All()
	const n = 16
	results := make([]mcp.Result, n)
	errors := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errors[i] = c.CallTool(context.Background(), caller, "create_widget", json.RawMessage(`{"name":"contended","count":4,"status":"active"}`))
		})
	}
	close(start)
	wg.Wait()
	// R-M6YE-QR3H: exactly one success and one additional widget.
	successes := 0
	for i, result := range results {
		if errors[i] != nil {
			t.Fatal(errors[i])
		}
		if result.IsError() {
			assertError(t, result, "invalid arguments:\nname: that name is already taken")
		} else {
			raw, _ := json.Marshal(result)
			var members map[string]json.RawMessage
			if err := json.Unmarshal(raw, &members); err != nil {
				t.Fatal(err)
			}
			if _, exists := members["isError"]; exists {
				t.Fatal("success has isError")
			}
			successes++
		}
	}
	if successes != 1 || len(s.All()) != len(before)+1 {
		t.Fatalf("successes %d; widgets %d", successes, len(s.All()))
	}
}

// R-LGPT-AA8P, R-LHXP-O1ZE.
func TestToolDomainTelemetry(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, capture, stderr := capturingWriter(t)
	store := widget.NewStore(knownSource())
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Version: cli.Version, Telemetry: writer})
	tools.Register(srv, store, writer)
	server := httptest.NewServer(identity.Require(srv))
	t.Cleanup(server.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client()})
	cases := []struct {
		name, args string
		accepted   bool
	}{
		{"create_widget", `{"name":" new private name ","count":7,"status":"paused"}`, true},
		{"list_widgets", `{}`, false},
		{"create_widget", `{"name":"alpha","count":-1,"status":"active"}`, false},
		{"create_widget", `{"name":"newer","count":7,"status":"unknown"}`, false},
		{"list_widgets", `{"unknown":1}`, false},
	}
	for i, tc := range cases {
		before := len(capture.Events())
		caller := identity.Caller{UserID: fmt.Sprintf("user-%d", i), RequestID: fmt.Sprintf("request-%d", i)}
		result, err := client.CallTool(context.Background(), caller, tc.name, json.RawMessage(tc.args))
		if err != nil {
			t.Fatal(err)
		}
		if tc.accepted && result.IsError() {
			t.Fatal("creation refused")
		}
		if err := writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := capture.Events()[before:]
		if tc.accepted {
			if len(events) != 2 || events[0].Name != "widget.created" || events[1].Name != "tool.called" {
				t.Fatalf("accepted events: %+v", events)
			}
			all := store.All()
			event := events[0]
			if event.RequestID != caller.RequestID || event.User != caller.UserID || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"widget": all[len(all)-1].ID}) {
				t.Fatalf("created event: %+v", event)
			}
		} else if len(events) != 1 || events[0].Name != "tool.called" {
			t.Fatalf("noncreation events: %+v", events)
		}
		if stderr.Len() != 0 {
			t.Fatalf("telemetry stderr: %s", stderr)
		}
	}
}
