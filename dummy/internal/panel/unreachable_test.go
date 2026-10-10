package panel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-GLL5-JMNY R-GMT1-XEEN R-GO0Y-B65C R-HKY8-MZ63
// R-HUPF-P53N R-HOLX-SAE6 R-HHAJ-HNY0 R-GXS5-DC2W R-HR1Q-JTVK R-HS9M-XLM9
// R-L6YM-84B5 R-CO16-H2KI
func TestUnreachableWidgetsAnswers(t *testing.T) {
	t.Setenv(services.Variable, "")
	store, handle := panelDatabaseStore(t)
	if _, errs := panelStoreCreate(t, store, widget.Draft{Name: "existing", Count: 3, Status: widget.StatusActive}); errs.Any() {
		t.Fatal(errs)
	}
	writer, capture, stderr := panelTestTelemetry(t, &strings.Builder{})
	h := panel.Handler(store, pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	before := panelStoreAll(t, store)
	etag := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "").Header().Get("ETag")
	handle.SetFailing(true)
	// The handle's failure seam makes each store operation unreachable.
	if _, err := store.All(context.Background()); err == nil {
		t.Fatal("All succeeded while failing")
	}
	if _, err := store.Check(context.Background(), widget.Draft{}); err == nil {
		t.Fatal("Check succeeded while failing")
	}
	if _, _, err := store.Create(context.Background(), widget.Draft{}); err == nil {
		t.Fatal("Create succeeded while failing")
	}
	for _, path := range []string{"/widgets", "/widgets/table"} {
		for _, condition := range []string{"", "*", etag} {
			headers := tableTestIdentity()
			headers.Set("If-None-Match", condition)
			get := tableTestRequest(h, "GET", path, headers, "")
			head := tableTestRequest(h, "HEAD", path, headers, "")
			if get.Code != 503 || get.Body.String() != widget.Unreachable+"\n" || !reflect.DeepEqual(get.Header()["Content-Type"], []string{"text/plain; charset=utf-8"}) || get.Header().Get("ETag") != "" {
				t.Fatalf("GET %s: %d %v %q", path, get.Code, get.Header(), get.Body.String())
			}
			if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
				t.Fatalf("HEAD differs: %d %v %q", head.Code, head.Header(), head.Body.String())
			}
		}
	}
	for _, body := range []string{"name=fresh&count=2&status=active", "name=&count=bad&status=unknown", "name=existing&count=-1&status=retired"} {
		var baseline *httptest.ResponseRecorder
		for _, accept := range []string{"", "text/html", "application/json"} {
			headers := tableTestIdentity()
			headers.Set("Content-Type", "application/x-www-form-urlencoded")
			headers.Set("Accept", accept)
			out := tableTestRequest(h, "POST", "/widgets", headers, body)
			if out.Code != 503 || out.Body.String() != widget.Unreachable+"\n" || out.Header().Get("Content-Type") != "text/plain; charset=utf-8" || out.Header().Get("ETag") != "" {
				t.Fatalf("POST: %d %v %q", out.Code, out.Header(), out.Body.String())
			}
			if baseline != nil && (out.Code != baseline.Code || !reflect.DeepEqual(out.Header(), baseline.Header()) || out.Body.String() != baseline.Body.String()) {
				t.Fatal("Accept changed unreachable answer")
			}
			baseline = out
		}
	}
	handle.SetFailing(false)
	if !slices.Equal(before, panelStoreAll(t, store)) {
		t.Fatal("unreachable requests mutated widgets")
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, event := range capture.Events() {
		if event.Name == "widget.created" {
			t.Fatal("failure emitted creation")
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("healthy telemetry stderr: %s", stderr)
	}
}

// R-GP8U-OXW1
func TestUnreachableLeavesOtherRoutesUnchanged(t *testing.T) {
	store, handle := panelDatabaseStore(t)
	h := coreHandler(t, store, pageTestEchoingBanner(), &strings.Builder{})
	cases := []struct{ method, path, media, body string }{
		{"GET", "/", "", ""}, {"HEAD", "/", "", ""}, {"POST", "/", "", ""},
		{"PUT", "/widgets", "", ""}, {"POST", "/widgets/table", "", ""},
		{"POST", "/widgets", "text/plain", "name=new"}, {"GET", "/missing", "", ""},
		{"HEAD", "/missing", "", ""}, {"GET", "/_appkit/theme.css", "", ""},
		{"HEAD", "/_appkit/unknown", "", ""}, {"POST", "/_appkit/theme.css", "", ""},
		{"GET", "/widgets/", "", ""}, {"GET", "/mcp/", "", ""},
	}
	for _, tc := range cases {
		for _, user := range []string{"reader", ""} {
			headers := tableTestIdentity()
			headers.Set("X-User-Id", user)
			headers.Set("Content-Type", tc.media)
			handle.SetFailing(false)
			before := panelStoreAll(t, store)
			reachable := tableTestRequest(h, tc.method, tc.path, headers, tc.body)
			handle.SetFailing(true)
			unreachable := tableTestRequest(h, tc.method, tc.path, headers, tc.body)
			if reachable.Code != unreachable.Code || !reflect.DeepEqual(reachable.Header(), unreachable.Header()) || reachable.Body.String() != unreachable.Body.String() {
				t.Fatalf("%s %s user=%q depends on reachability", tc.method, tc.path, user)
			}
			handle.SetFailing(false)
			if !slices.Equal(before, panelStoreAll(t, store)) {
				t.Fatal("other route changed widgets")
			}
		}
	}
	// Missing identity also takes precedence on the widget and MCP routes.
	for _, path := range []string{"/widgets", "/widgets/table", "/mcp"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT"} {
			headers := http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}}
			good := tableTestRequest(h, method, path, headers, "name=fresh&count=1&status=active")
			handle.SetFailing(true)
			bad := tableTestRequest(h, method, path, headers, "name=fresh&count=1&status=active")
			handle.SetFailing(false)
			if good.Code != bad.Code || !reflect.DeepEqual(good.Header(), bad.Header()) || good.Body.String() != bad.Body.String() {
				t.Fatalf("missing identity %s %s changed", method, path)
			}
		}
	}
}
