package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/tools"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-I5OJ-52RW R-I84B-WM9A R-I9C8-ADZZ R-HYD4-UGBQ
func TestUnreachableToolResultsAndTelemetry(t *testing.T) {
	t.Setenv(services.Variable, "")
	store, handle := toolsDatabaseStore(t)
	_, errs := toolsStoreCreate(t, store, widget.Draft{Name: "existing", Count: 3, Status: widget.StatusActive})
	if errs.Any() {
		t.Fatal(errs)
	}
	writer, capture, stderr := capturingWriter(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer})
	tools.Register(srv, store, writer)
	server := httptest.NewServer(identity.Require(srv))
	t.Cleanup(server.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client()})
	before := toolsStoreAll(t, store)
	cases := []struct{ name, args string }{
		{"list_widgets", "{}"}, {"list_widgets", ""},
		{"create_widget", `{"name":"fresh","count":3,"status":"active"}`},
		{"create_widget", `{"name":"existing","count":-2,"status":"paused"}`},
		{"create_widget", `{"name":"","count":-1,"status":"retired"}`},
	}
	handle.SetFailing(true)
	for i, tc := range cases {
		c := identity.Caller{UserID: fmt.Sprintf("user-%d", i), RequestID: fmt.Sprintf("failure-%d", i)}
		index := len(capture.Events())
		var args json.RawMessage
		if tc.args != "" {
			args = json.RawMessage(tc.args)
		}
		result, err := client.CallTool(context.Background(), c, tc.name, args)
		if err != nil {
			t.Fatal(err)
		}
		assertError(t, result, widget.Unreachable)
		if err := writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := capture.Events()[index:]
		if len(events) != 1 || events[0].Name != "tool.called" || events[0].Attrs["outcome"] != "error" || events[0].User != c.UserID || events[0].RequestID != c.RequestID {
			t.Fatalf("failure telemetry: %+v", events)
		}
	}
	handle.SetFailing(false)
	if !slices.Equal(before, toolsStoreAll(t, store)) {
		t.Fatal("failed tools mutated widgets")
	}
	// Listing the restored store still observes every original widget.
	result := call(t, client, "list_widgets", json.RawMessage("{}"))
	var got struct {
		Widgets []struct {
			ID, Name string
			Count    int
			Status   widget.Status
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(members["structuredContent"], &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Widgets) != len(before) || got.Widgets[0].ID != before[0].ID {
		t.Fatalf("restored listing: %+v", got)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("telemetry stderr: %s", stderr)
	}
}
