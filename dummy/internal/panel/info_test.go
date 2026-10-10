package panel_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
)

// R-XXWF-XHRD R-85XR-KVJU R-XZ4C-B9I2 R-Y0C8-P18R R-Y1K5-2SZG
func TestInformationDeclarations(t *testing.T) {
	const description string = panel.Description
	if description == "" {
		t.Fatal("empty description")
	}
	for _, char := range description {
		if char == '"' || char == '\\' || char < 0x20 || char == 0x7f {
			t.Fatalf("description contains forbidden code point %U", char)
		}
	}
	banner := page.Banner{Service: "supplied-service", Release: "supplied-release", Commit: "supplied-commit"}
	about := panel.AboutData(struct {
		Banner      page.Banner
		Description string
	}{banner, description})
	tool := panel.Tool(struct{ Name, Description string }{"supplied-tool", "supplied-description"})
	tools := panel.ToolsData(struct {
		Banner page.Banner
		Tools  []panel.Tool
	}{banner, []panel.Tool{tool}})
	if !reflect.DeepEqual(about.Banner, banner) || about.Description != description || !reflect.DeepEqual(tools.Banner, banner) || !slices.Equal(tools.Tools, []panel.Tool{tool}) {
		t.Fatal("declaration values changed")
	}
}

// R-Y3ZX-UCGU
func TestInformationTemplatesExecute(t *testing.T) {
	banner := page.Banner{Service: "fixture-service", Release: "fixture-release", Commit: "fixture-commit", Email: "fixture@example.test"}
	for _, data := range []panel.AboutData{{}, {Banner: banner, Description: "fixture description <&>"}} {
		renderPanelTemplate(t, "about", data)
	}
	for _, data := range []panel.ToolsData{{}, {Banner: banner, Tools: []panel.Tool{}}, {Banner: banner, Tools: []panel.Tool{{Name: "fixture-tool", Description: "fixture description <&>"}, {Name: "second-tool", Description: "second description"}}}} {
		renderPanelTemplate(t, "tools", data)
	}
}

// R-Y57U-847J R-Y6FQ-LVY8
func TestInformationPagesUseBannerAndRegisteredTools(t *testing.T) {
	t.Setenv(services.Variable, "")
	writer, _, _ := panelTestTelemetry(t, io.Discard)
	srv := mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer})
	var drawn page.Banner
	calls := 0
	banner := func(u page.User) page.Banner {
		calls++
		drawn = page.Banner{Service: "fixture-service", Release: "fixture-release", Commit: "fixture-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Trail: []page.Level{{Name: "discarded", URL: "/discarded"}, {Name: "also discarded", URL: "/other"}}}
		return drawn
	}
	h := panel.Handler(panelTestStore(t), banner, srv, writer)
	check := func(path string) {
		t.Helper()
		registered := srv.Tools()
		request := pageTestRequest("GET", path+"?q=/unknown")
		request.Header.Set("X-Forwarded-Proto", "http")
		request.Host = "unrelated.test"
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-Original-URI", "/different")
		before := calls
		got := pageTestResponse(h, request)
		if calls != before+1 {
			t.Fatal("page did not fetch its banner exactly once")
		}
		wantBanner := drawn
		wantBanner.Trail = []page.Level{{Name: strings.TrimPrefix(path, "/"), URL: path}}
		var data any = panel.AboutData{Banner: wantBanner, Description: panel.Description}
		if path == "/tools" {
			list := make([]panel.Tool, len(registered))
			for i, entry := range registered {
				list[i] = panel.Tool{Name: entry.Name, Description: entry.Description}
			}
			data = panel.ToolsData{Banner: wantBanner, Tools: list}
		}
		want := renderPanelTemplate(t, strings.TrimPrefix(path, "/"), data)
		if got.Code != 200 || !reflect.DeepEqual(got.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || got.Body.String() != want {
			t.Fatalf("%s: status=%d headers=%v body differs=%v", path, got.Code, got.Header(), got.Body.String() != want)
		}
	}
	check("/about")
	check("/tools")
	mcp.AddTool(srv, mcp.Tool[struct{}, struct{}]{Name: "fixture_extra", Description: "List the supplied extra fixture.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, struct{}) (struct{}, error) { return struct{}{}, nil }})
	check("/tools")
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client()})
	listed, err := client.ListTools(context.Background(), identity.Caller{UserID: "fixture-user"})
	if err != nil {
		t.Fatal(err)
	}
	registered := srv.Tools()
	if len(listed) != len(registered) {
		t.Fatal("MCP and page tool count differ")
	}
	for i, entry := range listed {
		if entry.Name != registered[i].Name || entry.Description != registered[i].Description {
			t.Fatal("MCP and page tool order or descriptions differ")
		}
	}
}

// R-Y7NM-ZNOX R-83HY-TC2G
func TestInformationMethods(t *testing.T) {
	h := coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard)
	for _, path := range []string{"/about", "/tools"} {
		get := pageTestResponse(h, pageTestRequest("GET", path))
		head := pageTestResponse(h, pageTestRequest("HEAD", path))
		if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
			t.Fatalf("HEAD %s differs from GET", path)
		}
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			r := pageTestRequest(method, path)
			got := pageTestResponse(h, r)
			if got.Code != 405 || !reflect.DeepEqual(got.Header().Values("Allow"), []string{"GET, HEAD"}) {
				t.Fatalf("%s %s: %d %v", method, path, got.Code, got.Header())
			}
			pageTestFailure(t, got, r, panel.MethodNotAllowedMessage)
		}
	}
}

// R-84PV-73T5 R-875N-YNAJ
func TestInformationPagesIndependentOfStore(t *testing.T) {
	for _, populated := range []bool{false, true} {
		store, handle := panelDatabaseStore(t)
		if populated {
			for _, name := range []string{"first fixture", "second fixture"} {
				tableTestCreate(t, store, name)
			}
		}
		h := coreHandler(t, store, pageTestEchoingBanner(), io.Discard)
		for _, path := range []string{"/about", "/tools"} {
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
				before := panelStoreAll(t, store)
				request := pageTestRequest(method, path)
				request.Body = io.NopCloser(bytes.NewBufferString("name=fresh&count=2&status=active"))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				reachable := pageTestResponse(h, request)
				if !slices.Equal(before, panelStoreAll(t, store)) {
					t.Fatal("reachable information request changed store")
				}
				handle.SetFailing(true)
				if _, err := store.All(context.Background()); err == nil {
					t.Fatal("store remains reachable")
				}
				unreachableRequest := request.Clone(request.Context())
				unreachableRequest.Body = io.NopCloser(bytes.NewBufferString("name=fresh&count=2&status=active"))
				unreachable := pageTestResponse(h, unreachableRequest)
				handle.SetFailing(false)
				if reachable.Code != unreachable.Code || !reflect.DeepEqual(reachable.Header(), unreachable.Header()) || reachable.Body.String() != unreachable.Body.String() {
					t.Fatalf("%s %s depends on store reachability", method, path)
				}
				if !slices.Equal(before, panelStoreAll(t, store)) {
					t.Fatal("information request changed store")
				}
			}
		}
	}
}
