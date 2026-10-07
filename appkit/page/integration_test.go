package page

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// R-JJC7-EPIB
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
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, staticPath(t, name), nil))
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
