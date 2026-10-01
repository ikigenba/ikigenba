package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/dummy/internal/tools"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

var caller = identity.Caller{UserID: "tool-tester"}

func clientOver(t *testing.T, s *widget.Store) *mcp.Client {
	t.Helper()
	t.Setenv(services.Variable, "")
	srv := mcp.NewServer(mcp.ServerConfig{Name: "test", Stderr: io.Discard})
	// R-2NUZ-0HJ1: using the public registration signature.
	tools.Register(srv, s)
	httpServer := httptest.NewServer(identity.Require("test", io.Discard, srv))
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

func TestAdvertisedTools(t *testing.T) {
	c := clientOver(t, widget.NewStore())
	infos, err := c.ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	// R-2RIO-5SR4: typed registration advertises both output schemas in order.
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
	// R-E4A6-X3DY, R-EK4V-W40Z.
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
	// R-EADO-TY3F, R-EE1D-Z9BI, R-ENSL-1F92, R-EQ8D-SYQG.
	jsonEqual(t, infos[0].OutputSchema, `{"type":"object","properties":{"widgets":{"type":"array","items":`+widgetSchema+`,"description":"Every widget, oldest first."}},"required":["widgets"],"additionalProperties":false}`)
	jsonEqual(t, infos[1].InputSchema, widgetSchema)
	jsonEqual(t, infos[1].OutputSchema, widgetSchema)
}

func widgetJSON(t *testing.T, w widget.Widget) string {
	t.Helper()
	encoded, err := json.Marshal(struct {
		Name   string `json:"name"`
		Count  int    `json:"count"`
		Status string `json:"status"`
	}{w.Name, w.Count, string(w.Status)})
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
	if string(members["structuredContent"]) != want {
		t.Fatalf("structuredContent = %s; want %s", members["structuredContent"], want)
	}
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(members["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Type != "text" || content[0].Text != want {
		t.Fatalf("content = %s", members["content"])
	}
	expected, _ := json.Marshal([]map[string]string{{"type": "text", "text": want}})
	jsonEqual(t, members["content"], string(expected))
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
	s := widget.NewStore()
	_, errs := s.Create(widget.Draft{Name: "last \"widget\"", Count: 8, Status: widget.StatusPaused})
	if errs.Any() {
		t.Fatal(errs)
	}
	c := clientOver(t, s)
	before := s.All()
	// R-ESO6-KI7U, R-EV3Z-C1P8, R-EYRO-HCXB.
	for _, args := range []json.RawMessage{nil, json.RawMessage(`{}`)} {
		assertSuccess(t, call(t, c, "list_widgets", args), listJSON(t, before))
		if !reflect.DeepEqual(s.All(), before) {
			t.Fatal("listing changed the store")
		}
	}
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
			s, expected := widget.NewStore(), widget.NewStore()
			c := clientOver(t, s)
			input := widgetJSON(t, widget.Widget(draft))
			w, errs := expected.Create(draft)
			result := call(t, c, "create_widget", json.RawMessage(input))
			// R-5XXV-7CQL: effects match one domain Create, including refusal.
			if !reflect.DeepEqual(s.All(), expected.All()) {
				t.Fatalf("store = %+v; want %+v", s.All(), expected.All())
			}
			if !errs.Any() {
				// R-F2FD-MO5E: ordered compact widget object and matching text.
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
			s := widget.NewStore()
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
	s := widget.NewStore()
	c := clientOver(t, s)
	// R-FAYO-B2C9: integral JSON values beyond either int64 bound.
	for _, number := range []string{"-9223372036854775809", "9223372036854775808"} {
		result := call(t, c, "create_widget", json.RawMessage(`{"name":"new","count":`+number+`,"status":"active"}`))
		assertError(t, result, "invalid arguments:\ncount: must be between -9223372036854775808 and 9223372036854775807, got "+number)
	}
}

func TestConcurrentCreateSameName(t *testing.T) {
	s := widget.NewStore()
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
