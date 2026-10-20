package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/links"
	"github.com/ikigenba/ikigenba/campaigns/internal/pages"
	"github.com/ikigenba/ikigenba/campaigns/internal/settings"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/tools"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
	"github.com/ikigenba/ikigenba/campaigns/internal/web"
)

func fixture(t *testing.T) (web.Config, *telemetry.Capture, *events.Capture) {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC) }
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "db"), Migrations: campaigns.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	rnd := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 100000))
	tc, ec := &telemetry.Capture{}, &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: tc, Now: now, Rand: rnd, Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: ec, Now: now, Rand: rnd, Telemetry: w, Emits: trail.Emits(), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { em.Shutdown(context.Background()); w.Shutdown(context.Background(), "test") })
	return web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), Store: store.New(d, store.Config{Now: now, Rand: rnd}), Telemetry: w, Events: em, Settings: settings.Defaults()}, tc, ec
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://campaigns.test"+path, nil)
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", "mgreenly+fake00@gmail.com")
	r.Header.Set("X-Request-Id", "chosen-request")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %v %q / %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-PXE4-EPMC R-PYM0-SHD1 R-3AHY-9SNL R-Q29P-XSL4 R-Q75B-GVJW R-Q8D7-UNAL R-Q9L4-8F1A R-3J3X-X9WS
func TestRouteDispatch(t *testing.T) {
	cfg, tc, ec := fixture(t)
	h := web.Handler(cfg)
	ps, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: cfg.Banner, Pages: ps, Store: cfg.Store, MCP: cfg.MCP}))
	for _, user := range []string{"", "owner"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, path := range []string{"/declarations", "/events", "/mcp", "/_appkit/theme.css", "/", "/about", "/tools", "/nope", "/campaigns/camp_5a5a5a5a5a5a5a5a", "/t", "/mcp/", "/events/", "/declarations/", "/_appkit"} {
				if path == "/mcp" && user != "" {
					continue
				}
				a := request(h, method, path, user)
				if len(a.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("cookie")
				}
				var expected http.Handler
				switch path {
				case "/declarations":
					expected = events.DeclarationsHandler(cfg.Events, nil)
				case "/events":
					expected = events.DeliveryHandler(nil)
				case "/mcp":
					expected = identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("guest reached MCP") }))
				case "/_appkit/theme.css":
					expected = page.Static()
				default:
					expected = ph
				}
				same(t, a, request(expected, method, path, user))
			}
		}
	}
	if e = cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, event := range tc.Events() {
		if event.Name != "request.started" && event.Name != "request.finished" {
			t.Fatal(event)
		}
	}
	if len(ec.Events()) != 0 {
		t.Fatal("bus event")
	}
}

// R-Q5XF-33T7
func TestGuestTrackingDispatch(t *testing.T) {
	cfg, _, _ := fixture(t)
	c, e := cfg.Store.CreateCampaign(context.Background(), store.Campaign{CreatedUserID: "owner", Name: "chosen campaign", Channels: []string{store.ChannelMail}, Destination: "https://destination.test/"})
	if e != nil {
		t.Fatal(e)
	}
	m, _, e := cfg.Store.AddMember(context.Background(), store.MemberInput{Campaign: c.ID, UserID: "owner", Candidate: "cand_1234567890abcdef", Channels: c.Channels})
	if e != nil {
		t.Fatal(e)
	}
	h := web.Handler(cfg)
	direct := links.Handler(links.Config{Store: cfg.Store, Telemetry: cfg.Telemetry, DefaultDestination: cfg.Settings.DefaultDestination})
	for _, u := range []string{"", "owner"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, path := range []string{"/t/" + m.Pieces[0].Piece.Token, "/t/unknown", "/t/"} {
				a, b := request(h, method, path, u), request(direct, method, path, u)
				same(t, a, b)
				if len(a.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("cookie")
				}
			}
		}
	}
	recorded, e := cfg.Store.GetPiece(context.Background(), m.Pieces[0].Piece.ID)
	if e != nil || recorded.Engagement.Opens != 8 {
		t.Fatal(recorded, e)
	}
}

// R-Q4PI-PC2I R-PZTX-693Q
func TestMCPRegistrationAndCaller(t *testing.T) {
	cfg, tc, _ := fixture(t)
	h := web.Handler(cfg)
	catalog := tools.Catalog()
	registered := cfg.MCP.Tools()
	if len(registered) != len(catalog) {
		t.Fatalf("%d tools", len(registered))
	}
	for i, entry := range catalog {
		if registered[i].Name != entry.Name {
			t.Fatalf("catalog mismatch at %d", i)
		}
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	caller := identity.Caller{UserID: "chosen-owner", Email: "mgreenly+fake00@gmail.com", RequestID: "chosen-id"}
	result, e := client.CallTool(context.Background(), caller, "create_campaign", json.RawMessage(`{"name":"chosen name","channels":["mail"],"destination":"https://destination.test/"}`))
	if e != nil || result.IsError() {
		t.Fatal(result, e)
	}
	rows, e := cfg.Store.ListCampaigns(context.Background(), "", "", 10)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if rows[0].Campaign.CreatedUserID != caller.UserID || rows[0].Campaign.CreatedUserEmail != caller.Email {
		t.Fatal(rows)
	}
	if e = cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, event := range tc.Events() {
		if event.RequestID != caller.RequestID || event.User != caller.UserID {
			t.Fatal(event)
		}
	}
}

// R-3AHY-9SNL
func TestDeclarationsIgnoreRequestInputs(t *testing.T) {
	cfg, _, _ := fixture(t)
	h := web.Handler(cfg)
	expected := events.DeclarationsHandler(cfg.Events, nil)
	for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS"} {
		for _, users := range [][]string{nil, {""}, {"owner", "other"}} {
			for _, target := range []string{"/declarations", "/declarations?query=chosen"} {
				makeRequest := func() *http.Request {
					r := httptest.NewRequest(method, target, bytes.NewBufferString("body-chosen"))
					if users != nil {
						r.Header["X-User-Id"] = users
					}
					r.Header.Set("X-User-Email", "mgreenly+fake00@gmail.com")
					r.Header.Set("X-Request-Id", "declarations-request")
					r.Header.Set("X-Custom", "header-chosen")
					return r
				}
				a, b := httptest.NewRecorder(), httptest.NewRecorder()
				h.ServeHTTP(a, makeRequest())
				expected.ServeHTTP(b, makeRequest())
				same(t, a, b)
			}
		}
	}
}
