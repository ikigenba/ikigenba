package pages_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
)

func render(t *testing.T, name string, data any) string {
	t.Helper()
	set, err := page.Templates().ParseFS(sessions.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := set.ExecuteTemplate(&out, name, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// R-0Q1D-0A3D R-0R99-E1U2 R-0SH5-RTKR R-0TP2-5LBG
// R-0XCR-AWJJ R-0YKN-OOA8 R-0ZSK-2G0X R-110G-G7RM
// R-128C-TZIB R-13G9-7R90 R-14O5-LIZP R-15W1-ZAQE
// R-1D7G-9X6K R-1EFC-NOX9
func TestLoadAndWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	if pages.ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || pages.AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("page levels")
	}
	const description string = pages.Description
	if description == "" || strings.Contains(description, "\n") {
		t.Fatal("description must be one line")
	}
	banner := page.Banner{Service: "fixture-service", Release: "release-fixture", Commit: "commit-fixture", Email: "caller@example.test", ProfileURL: "https://profile.example.test/", LogoutURL: "https://profile.example.test/logout"}
	weeks := make([]pages.Week, 9)
	for i := range weeks {
		kinds := make([]pages.FiguresRow, 7)
		for j := range kinds {
			kinds[j] = pages.FiguresRow{"fixture-kind", pages.Duration{119, 0, 1}, pages.Duration{360000, 100, 0}, pages.Duration{43559, 12, 5}, "18,204", "0.07"}
		}
		total := kinds[0]
		total.Kind = ""
		if i == 0 {
			total.Attention = pages.Duration{}
		}
		weeks[i] = pages.Week{"2026-10-26", kinds, total, 37}
	}
	cases := []struct {
		name string
		data any
	}{
		{"landing", pages.LandingData{banner, pages.Duration{900, 0, 15}, nil}},
		{"landing", pages.LandingData{banner, pages.Duration{900, 0, 15}, weeks}},
		{"tools", pages.ToolsData{banner, []pages.ToolRow{}}},
		{"tools", pages.ToolsData{banner, []pages.ToolRow{{"fixture_tool", "Fixture description."}}}},
		{"about", pages.AboutData{banner, pages.Description}},
		{"about", pages.AboutData{page.Banner{}, pages.Description}},
		{"notfound", pages.NotFoundData{banner}},
	}
	for _, tc := range cases {
		for range 2 {
			load := pages.Load
			set, err := load()
			if err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rec := httptest.NewRecorder()
				rec.Header().Add("Content-Type", "discard")
				rec.Header().Add("Content-Type", "also discard")
				write := set.Write
				write(rec, httptest.NewRequest(method, "/", nil), http.StatusAccepted, tc.name, tc.data)
				if rec.Code != http.StatusAccepted {
					t.Fatalf("%s: status %d", tc.name, rec.Code)
				}
				if got := rec.Header().Values("Content-Type"); len(got) != 1 || got[0] != "text/html; charset=utf-8" {
					t.Fatalf("content type %v", got)
				}
				want := render(t, tc.name, tc.data)
				if method == http.MethodHead {
					want = ""
				}
				if rec.Body.String() != want {
					t.Fatalf("%s %s: rendered bytes differ", method, tc.name)
				}
			}
		}
	}
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing-template", "tools"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := httptest.NewRecorder()
			set.Write(rec, httptest.NewRequest(method, "/", nil), http.StatusOK, name, struct{ Banner page.Banner }{banner})
			if rec.Code != 500 || rec.Body.Len() != 0 || len(rec.Header().Values("Content-Type")) != 1 || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("failed execution: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
			}
		}
	}
}
