package pages

import (
	"bytes"
	"context"
	"fmt"
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

// CapturePageData calls the real handler with a test template that captures its typed input.
func CapturePageData[T any](t *testing.T, cfg Config, r *http.Request, name string) T {
	t.Helper()
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got T
	called := false
	capture := func(data T) string {
		got, called = data, true
		return ""
	}
	if _, err := set.templates.Funcs(template.FuncMap{"capture": capture}).Parse(fmt.Sprintf(`{{define %q}}{{capture .}}{{end}}`, name)); err != nil {
		t.Fatal(err)
	}
	cfg.Pages = set
	w := httptest.NewRecorder()
	identity.Optional(Handler(cfg)).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !called {
		t.Fatalf("%s returned %d with template execution %v, want 200 and execution of %s", r.URL, w.Code, called, name)
	}
	return got
}

// R-MIW1-2B77 R-CS3J-6201 R-CVR8-BD84 distinguish empty slices from nil at the template input.
func TestEmptyPageLists(t *testing.T) {
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 11, 18, 14, 5, 9, 0, time.UTC) }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "accounts.db"), Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	var got LandingData
	captureLanding := func(data LandingData) string {
		got = data
		return ""
	}
	var gotHost HostData
	captureHost := func(data HostData) string {
		gotHost = data
		return ""
	}
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.templates.Funcs(template.FuncMap{"captureLanding": captureLanding, "captureHost": captureHost}).Parse(
		`{{define "landing"}}{{captureLanding .}}{{end}}{{define "host"}}{{captureHost .}}{{end}}`,
	); err != nil {
		t.Fatal(err)
	}
	writer := telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: now})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "test") })
	cfg := Config{
		Banner: func(u page.User) page.Banner {
			return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Trail: []page.Level{{Name: "sample-level"}}}
		},
		Pages: set, Store: store.New(d, store.Config{Now: now, Rand: bytes.NewReader([]byte("first-idsecondid"))}),
		MCP: mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Telemetry: writer}), Configured: true,
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
			Configured: true, Domain: cfg.Domain, Hosts: []HostRow{}, KeptVolumes: []KeptVolumeRow{}, Accounts: []AccountRow{}, Users: []UserRow{},
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s returned %d and landing data %#v, want %d and %#v", method, w.Code, got, http.StatusOK, want)
		}
	}
	h, _, err := cfg.Store.CreateHost(t.Context(), store.HostSpec{Type: "chosen-type", RootGB: 20, DataGB: 50}, identity.Caller{UserID: "caller"})
	if err != nil {
		t.Fatal(err)
	}
	checkHost := func(report *ReportView, backups []BackupRow) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/hosts/"+h.Name, nil)
		r.Header.Set("X-User-Id", "test-user")
		w := httptest.NewRecorder()
		gotHost = HostData{}
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !reflect.DeepEqual(gotHost.Host.Allowlist, []string{}) ||
			!reflect.DeepEqual(gotHost.Backups, backups) || gotHost.DatabaseCount != 0 ||
			!reflect.DeepEqual(gotHost.Restores, []RestoreRow{}) || gotHost.RestoreTotal != 0 ||
			!reflect.DeepEqual(gotHost.Report, report) {
			t.Fatalf("host response = %d with data %#v and report %#v, want empty lists and report %#v", w.Code, gotHost, gotHost.Report, report)
		}
	}
	checkHost(nil, []BackupRow{})
	for _, total := range []int{0, 3} {
		if _, err := cfg.Store.RecordReport(t.Context(), h.Name, protocol.Report{BanTotal: total}, now()); err != nil {
			t.Fatal(err)
		}
		checkHost(&ReportView{
			Received: now().Format(time.RFC3339), ReceivedText: now().Format(SecondLayout), Wanted: h.Version,
			FailedUnits: []string{}, Slices: []SliceRow{}, Bans: []BanRow{}, Allowlist: []string{}, BanTotal: total,
		}, []BackupRow{})
	}
	if _, _, err := cfg.Store.PlaceAccount(t.Context(), "chosen-account", h.Name, identity.Caller{UserID: "caller"}); err != nil {
		t.Fatal(err)
	}
	record, err := cfg.Store.Host(t.Context(), h.Name)
	if err != nil {
		t.Fatal(err)
	}
	checkHost(&ReportView{
		Received: now().Format(time.RFC3339), ReceivedText: now().Format(SecondLayout), Wanted: record.Version,
		FailedUnits: []string{}, Slices: []SliceRow{}, Bans: []BanRow{}, Allowlist: []string{}, BanTotal: 3,
	}, []BackupRow{{Account: "chosen-account", Health: store.BackupNone, Databases: []DatabaseRow{}}})
}
