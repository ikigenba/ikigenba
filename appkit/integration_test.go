package appkit

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// R-675V-K57R
func TestEmbeddedAssetsIgnoreWorkingDirectory(t *testing.T) {
	wantPaths := []string{
		"assets/InterVariable-Italic.woff2", "assets/InterVariable.woff2",
		"assets/JetBrainsMono.woff2", "assets/OFL.txt", "assets/TABLER-LICENSE.txt",
		"assets/banner.html", "assets/launcher.js", "assets/theme.css",
	}
	var paths []string
	err := fs.WalkDir(assetsFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range wantPaths {
		if !slices.Contains(paths, path) {
			t.Errorf("asset %s is not embedded", path)
		}
	}
	snapshots := make(map[string][]byte)
	for _, path := range wantPaths {
		disk, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			t.Fatal(readErr)
		}
		embedded, readErr := assetsFS.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(embedded, disk) {
			t.Fatalf("embedded asset %s differs from the source bytes", path)
		}
		snapshots[path] = disk
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
	wantBanner := render()
	for range 2 {
		t.Chdir(t.TempDir())
		if got := render(); !bytes.Equal(got, wantBanner) {
			t.Fatal("banner output changed with working directory")
		}
		handler := Static()
		for _, path := range wantPaths {
			if path == "assets/banner.html" {
				continue
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, StaticPrefix+path[len("assets/"):], nil))
			if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), snapshots[path]) {
				t.Errorf("%s: status %d or body changed with working directory", path, recorder.Code)
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
