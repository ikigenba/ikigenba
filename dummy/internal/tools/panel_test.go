package tools_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func panelClient(t *testing.T, s *widget.Store, banner page.Banner) (*mcp.Client, http.Handler) {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _, _ := capturingWriter(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer})
	handler := panel.Handler(s, func(page.User) page.Banner { return banner }, srv, writer)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client()}), handler
}

func panelRequest(h http.Handler, method, path, body, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-User-Id", caller.UserID)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	return response
}

// R-44LP-BOCB: a successful creation appears in both first-window surfaces.
func TestToolCreationAppearsInPanel(t *testing.T) {
	for _, existing := range []int{0, 3, paging.DefaultLimit - 1} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			s, _ := toolsDatabaseStore(t)
			for i := range existing {
				_, fields := toolsStoreCreate(t, s, widget.Draft{Name: fmt.Sprintf("existing-%d", i), Count: i, Status: widget.StatusActive})
				if fields.Any() {
					t.Fatalf("seed rejected: %+v", fields)
				}
			}
			banner := page.Banner{Service: "supplied service", Email: "panel@example.test", ProfileURL: "/supplied-profile", LogoutURL: "/supplied-logout"}
			c, h := panelClient(t, s, banner)
			before := panelRequest(h, http.MethodGet, "/widgets/table", "", "")
			if before.Code != http.StatusOK || before.Header().Get("ETag") == "" {
				t.Fatal("initial table has no validator")
			}
			result := call(t, c, "create_widget", json.RawMessage(`{"name":"from MCP & panel","count":23,"status":"paused"}`))
			if result.IsError() {
				t.Fatal("creation failed")
			}
			raw, err := result.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err = json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			if _, present := object["isError"]; present {
				t.Fatal("successful create has isError member")
			}
			var answer struct {
				Widget widget.Widget `json:"structuredContent"`
			}
			if err = json.Unmarshal(raw, &answer); err != nil {
				t.Fatal(err)
			}
			widgets := toolsStoreAll(t, s)
			if len(widgets) == 0 || widgetJSON(t, widgets[len(widgets)-1]) != widgetJSON(t, answer.Widget) {
				t.Fatal("result differs from final stored widget")
			}
			set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/widgets", "/widgets/table"} {
				response := panelRequest(h, http.MethodGet, path, "", "")
				if response.Code != http.StatusOK {
					t.Fatalf("%s status = %d", path, response.Code)
				}
				var expected bytes.Buffer
				name, data := "table", any(widgets)
				if path == "/widgets" {
					name = "page"
					data = map[string]any{"Banner": banner, "Panel": true, "Message": "", "Count": len(widgets), "Table": widgets, "Page": "", "Next": "", "Form": panel.FormView{Statuses: widget.Statuses()}}
				}
				if err = set.ExecuteTemplate(&expected, name, data); err != nil {
					t.Fatal(err)
				}
				if response.Body.String() != expected.String() {
					t.Fatalf("%s differs from template", path)
				}
			}
			conditional := panelRequest(h, http.MethodGet, "/widgets/table", "", before.Header().Get("ETag"))
			if conditional.Code != http.StatusOK {
				t.Fatalf("stale validator status = %d", conditional.Code)
			}
		})
	}
}

func TestFormCreationAppearsInToolList(t *testing.T) {
	s := toolsTestStore(t)
	c, h := panelClient(t, s, page.Banner{})
	body := url.Values{"name": {" from form & tools "}, "count": {"9"}, "status": {"retired"}}.Encode()
	response := panelRequest(h, http.MethodPost, "/widgets", body, "")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("form status = %d: %s", response.Code, response.Body.String())
	}
	result := call(t, c, "list_widgets", nil)
	// R-RL9E-4UFF: the form-created widget is the final list object.
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		StructuredContent struct {
			Widgets []json.RawMessage `json:"widgets"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatal(err)
	}
	widgets := answer.StructuredContent.Widgets
	if len(widgets) == 0 {
		t.Fatal("empty widget list")
	}
	all := toolsStoreAll(t, s)
	want := widgetJSON(t, widget.Widget{ID: all[len(all)-1].ID, Name: "from form & tools", Count: 9, Status: widget.StatusRetired})
	jsonEqual(t, widgets[len(widgets)-1], want)
}
