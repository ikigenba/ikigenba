package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
)

// R-XRNT-FQ8U
func TestRunEventsFollowTheirActualRequest(t *testing.T) {
	f := newStartFixture(t)
	repos := t.TempDir()
	startRepo(t, f, repos)
	f.set("REPOS_DIR", repos)
	f.start(t)
	sent := map[string]struct{ method, path, event string }{
		"event-create":  {http.MethodPost, "/mcp", "site.created"},
		"event-publish": {http.MethodPost, "/mcp", "site.published"},
		"event-list":    {http.MethodPost, "/mcp", ""},
		"event-view":    {http.MethodGet, "/blog/", "site.viewed"},
	}
	for _, call := range []struct{ id, name, args string }{
		{"event-create", "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public"}`},
		{"event-publish", "publish", `{"name":"blog"}`},
		{"event-list", "list", `{}`},
	} {
		result, err := f.mcp.CallTool(context.Background(), identity.Caller{UserID: "owner", RequestID: call.id}, call.name, json.RawMessage(call.args))
		if err != nil || result.IsError() {
			t.Fatalf("%s: %#v %v", call.name, result, err)
		}
	}
	req, err := http.NewRequest(http.MethodGet, "http://sites/blog/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Request-Id", "event-view")
	response, err := f.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode, readErr, closeErr)
	}
	if code := f.stop(t); code != cli.ExitSuccess {
		t.Fatal(code, f.err.text())
	}
	starts, finishes := map[string]int{}, map[string]int{}
	active, tools, siteEvents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, event := range f.capture.Events() {
		if event.Name == "service.started" || event.Name == "service.stopping" {
			continue
		}
		request, ok := sent[event.RequestID]
		if !ok {
			t.Fatalf("event has no actual request: %#v", event)
		}
		if event.Name == "request.started" {
			starts[event.RequestID]++
			if starts[event.RequestID] != 1 || event.Attrs["method"] != request.method || event.Attrs["path"] != request.path {
				t.Fatalf("start does not match request: %#v", event)
			}
			active[event.RequestID] = true
			continue
		}
		if !active[event.RequestID] {
			t.Fatalf("event outside its request handling: %#v", event)
		}
		switch event.Name {
		case "request.finished":
			finishes[event.RequestID]++
			active[event.RequestID] = false
		case "tool.called":
			if request.path != "/mcp" {
				t.Fatalf("tool event belongs to site request: %#v", event)
			}
			tools[event.RequestID] = true
		default:
			if request.event == "" || event.Name != request.event {
				t.Fatalf("site event belongs to another request: %#v", event)
			}
			siteEvents[event.RequestID] = true
		}
	}
	for id, request := range sent {
		if starts[id] != 1 || finishes[id] != 1 {
			t.Fatalf("request %s starts=%d finishes=%d", id, starts[id], finishes[id])
		}
		if request.path == "/mcp" && !tools[id] {
			t.Fatalf("no tool.called for %s", id)
		}
		if request.event != "" && !siteEvents[id] {
			t.Fatalf("no %s for %s", request.event, id)
		}
	}
}
