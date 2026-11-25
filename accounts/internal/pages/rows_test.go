package pages

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-D35O-TIZX distinguishes non-nil empty slices from nil at the template input.
func TestEmptyLandingData(t *testing.T) {
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 11, 18, 14, 5, 9, 0, time.UTC) }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "accounts.db"), Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	var got LandingData
	capture := func(data LandingData) string {
		got = data
		return ""
	}
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.templates.Funcs(template.FuncMap{"capture": capture}).Parse(`{{define "landing"}}{{capture .}}{{end}}`); err != nil {
		t.Fatal(err)
	}
	writer := telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: now})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "test") })
	cfg := Config{
		Banner: func(u page.User) page.Banner {
			return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Trail: []page.Level{{Name: "sample-level"}}}
		},
		Pages: set, Store: store.New(d, store.Config{Now: now}), MCP: mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Telemetry: writer}), Configured: true,
		Domain: "fixture.test", Now: now,
	}
	handler := identity.Optional(Handler(cfg))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		got = LandingData{}
		r := httptest.NewRequest(method, "/", nil)
		r.Header.Set("X-User-Id", "test-user")
		const email = "chosen@example.test"
		r.Header["X-User-Email"] = []string{email, "ignored@example.test"}
		auth := Auth(r, cfg.ServicesPath)
		want := LandingData{
			Banner:     page.Banner{Email: email, ProfileURL: auth + "/", LogoutURL: auth + "/logout"},
			Configured: true, Domain: cfg.Domain, Hosts: []HostRow{}, Accounts: []AccountRow{}, Users: []UserRow{},
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s returned %d and landing data %#v, want %d and %#v", method, w.Code, got, http.StatusOK, want)
		}
	}
}

// R-UY9S-SWPB
func TestEmptyHostAndReportLists(t *testing.T) {
	at := time.Date(2026, 11, 18, 14, 5, 9, 0, time.UTC)
	host := hostView(store.Host{Name: "host01", State: store.HostRunning}, "", at)
	if host.Allowlist == nil || len(host.Allowlist) != 0 {
		t.Fatalf("host allowlist = %#v, want non-nil empty slice", host.Allowlist)
	}
	if got := reportView(nil, 7); got != nil {
		t.Fatalf("missing report = %#v, want nil", got)
	}
	for _, total := range []int{0, 3} {
		got := reportView(&store.Report{Received: at, Body: protocol.Report{BanTotal: total}}, 7)
		if got == nil || got.BanTotal != total ||
			got.FailedUnits == nil || len(got.FailedUnits) != 0 ||
			got.Slices == nil || len(got.Slices) != 0 ||
			got.Bans == nil || len(got.Bans) != 0 ||
			got.Allowlist == nil || len(got.Allowlist) != 0 {
			t.Fatalf("report = %#v, want non-nil empty lists and ban total %d", got, total)
		}
	}
}
