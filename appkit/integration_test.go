package appkit

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// R-LLKL-VIW8
func TestEmbeddedAssetsIgnoreWorkingDirectory(t *testing.T) {
	staticNames := []string{
		"InterVariable-Italic.woff2", "InterVariable.woff2", "JetBrainsMono.woff2",
		"OFL.txt", "TABLER-LICENSE.txt", "launcher.js", "theme.css",
	}
	banner := Banner{Service: "dummy", Email: "user@example.test", ProfileURL: "/profile", LogoutURL: "/logout",
		Services: []Service{{Name: "dummy", URL: "/", Enabled: true, Current: true}}}
	render := func() []byte {
		var output bytes.Buffer
		if executeErr := Templates().ExecuteTemplate(&output, "banner", banner); executeErr != nil {
			t.Fatal(executeErr)
		}
		return output.Bytes()
	}
	serve := func() map[string][]byte {
		handler := Static()
		bodies := make(map[string][]byte)
		for _, name := range staticNames {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, StaticPrefix+name, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("%s: status %d", name, recorder.Code)
			}
			bodies[name] = recorder.Body.Bytes()
		}
		return bodies
	}
	wantBanner := render()
	wantBodies := serve()
	for range 2 {
		t.Chdir(t.TempDir())
		if got := render(); !bytes.Equal(got, wantBanner) {
			t.Fatal("banner output changed with working directory")
		}
		for name, got := range serve() {
			if !bytes.Equal(got, wantBodies[name]) {
				t.Errorf("%s: body changed with working directory", name)
			}
		}
	}
}

func TestConsumerWiresServicesTemplatesAndStatic(t *testing.T) {
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	writeServices := func(contents string) {
		if err := os.WriteFile(servicesPath, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeServices(`{"services":[{"name":"dummy","url":"/dummy","icon":"<svg data-service-icon=\"dummy\"></svg>","enabled":true},{"name":"calendar","url":"/calendar","icon":"","enabled":false}]}`)
	t.Setenv("IKIGENBA_SERVICES", servicesPath)
	kit := New("dummy")
	pages := fstest.MapFS{"templates/page.html": {Data: []byte(`{{define "page"}}<link rel="stylesheet" href="{{.Stylesheet}}">{{template "banner" .Banner}}{{end}}`)}}
	templates, err := Templates().ParseFS(pages, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	user := User{Email: "user+tag@example.test", ProfileURL: "/profile", LogoutURL: "/logout"}
	mux := http.NewServeMux()
	mux.Handle(StaticPrefix, Static())
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		data := struct {
			Stylesheet string
			Banner     Banner
		}{Stylesheet: StaticPrefix + "theme.css", Banner: kit.Banner(user)}
		if executeErr := templates.ExecuteTemplate(w, "page", data); executeErr != nil {
			t.Errorf("execute consumer page: %v", executeErr)
		}
	})
	request := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	page := request("/")
	for _, hook := range []string{
		`href="/_appkit/theme.css"`, `data-service="dummy"`, `href="/profile"`,
		`user&#43;tag@example.test`, `action="/logout"`, `Sign out`,
		`popovertarget="services"`, `src="/_appkit/launcher.js"`,
		`href="/dummy"`, `aria-current="page"`, `<svg data-service-icon="dummy"></svg>`,
		`calendar is unavailable`,
	} {
		if !strings.Contains(page.Body.String(), hook) {
			t.Errorf("consumer page missing hook %q", hook)
		}
	}
	for _, name := range []string{"theme.css", "launcher.js"} {
		resource := request(StaticPrefix + name)
		want, readErr := assetsFS.ReadFile("assets/" + name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if resource.Code != http.StatusOK || !bytes.Equal(resource.Body.Bytes(), want) {
			t.Errorf("consumer's %s link: status %d or wrong bytes", name, resource.Code)
		}
	}
	writeServices("malformed")
	page = request("/")
	if page.Code != http.StatusOK {
		t.Fatalf("page with malformed services: status %d", page.Code)
	}
	for _, hook := range []string{`data-service="dummy"`, `href="/profile"`, `Sign out`} {
		if !strings.Contains(page.Body.String(), hook) {
			t.Errorf("page with malformed services missing hook %q", hook)
		}
	}
	for _, hook := range []string{`popovertarget=`, `id="services"`, `<script`} {
		if strings.Contains(page.Body.String(), hook) {
			t.Errorf("page with malformed services includes launcher hook %q", hook)
		}
	}
}
