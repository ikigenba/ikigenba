package appkit_test

import (
	"bytes"
	"embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit"
)

//go:embed assets/theme.css assets/launcher.js assets/InterVariable.woff2 assets/InterVariable-Italic.woff2 assets/JetBrainsMono.woff2 assets/OFL.txt assets/TABLER-LICENSE.txt
var staticExpectedFS embed.FS

var staticFiles = []struct {
	name        string
	contentType string
}{
	{"theme.css", "text/css; charset=utf-8"},
	{"launcher.js", "text/javascript; charset=utf-8"},
	{"InterVariable.woff2", "font/woff2"},
	{"InterVariable-Italic.woff2", "font/woff2"},
	{"JetBrainsMono.woff2", "font/woff2"},
	{"OFL.txt", "text/plain; charset=utf-8"},
	{"TABLER-LICENSE.txt", "text/plain; charset=utf-8"},
}

func staticBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := staticExpectedFS.ReadFile("assets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func staticResponse(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

func TestStaticPrefix(t *testing.T) {
	// R-7MDN-MICX
	const prefix = appkit.StaticPrefix
	if prefix != "/_appkit/" {
		t.Fatalf("StaticPrefix = %q", prefix)
	}
}

func TestStaticFactory(t *testing.T) {
	// R-7NLK-0A3M
	factory := appkit.Static
	if got, want := reflect.TypeOf(factory), reflect.TypeFor[func() http.Handler](); got != want {
		t.Fatalf("Static type = %v, want %v", got, want)
	}
	if factory() == nil {
		t.Fatal("Static returned a nil handler")
	}
}

func TestStaticGETBytes(t *testing.T) {
	// R-7OTG-E1UB
	handler := appkit.Static()
	for _, file := range staticFiles {
		t.Run(file.name, func(t *testing.T) {
			want := staticBytes(t, file.name)
			for _, target := range []string{
				appkit.StaticPrefix + file.name,
				appkit.StaticPrefix + file.name + "?download=1&path=banner.html",
				"/%5Fappkit/" + file.name,
				"/_appkit%2F" + file.name,
			} {
				response := staticResponse(handler, http.MethodGet, target)
				if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
					t.Errorf("GET %s: status %d, body equals asset: %t", target, response.Code, bytes.Equal(response.Body.Bytes(), want))
				}
			}
		})
	}
}

func TestStaticContentTypes(t *testing.T) {
	// R-7Q1C-RTL0
	handler := appkit.Static()
	for _, file := range staticFiles {
		response := staticResponse(handler, http.MethodGet, appkit.StaticPrefix+file.name)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != file.contentType {
			t.Errorf("GET %s: status %d, Content-Type %q; want 200, %q", file.name, response.Code, response.Header().Get("Content-Type"), file.contentType)
		}
	}
}

func TestStaticHEAD(t *testing.T) {
	// R-7R99-5LBP
	handler := appkit.Static()
	for _, file := range staticFiles {
		get := staticResponse(handler, http.MethodGet, appkit.StaticPrefix+file.name)
		for _, target := range []string{appkit.StaticPrefix + file.name, "/%5Fappkit/" + file.name + "?x=1"} {
			response := staticResponse(handler, http.MethodHead, target)
			if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != get.Header().Get("Content-Type") {
				t.Errorf("HEAD %s: status %d, body length %d, Content-Type %q; GET Content-Type %q", target, response.Code, response.Body.Len(), response.Header().Get("Content-Type"), get.Header().Get("Content-Type"))
			}
		}
	}
}

func TestStaticUnknownPaths(t *testing.T) {
	// R-7SH5-JD2E
	paths := []string{appkit.StaticPrefix, appkit.StaticPrefix + "banner.html", "/", "/_appkit", "/_appkit/missing", "/_appkit/assets/theme.css"}
	for _, file := range staticFiles {
		paths = append(paths,
			appkit.StaticPrefix+file.name+"/",
			appkit.StaticPrefix+file.name+"/child",
			appkit.StaticPrefix+strings.ToUpper(file.name),
			appkit.StaticPrefix+"./"+file.name,
			appkit.StaticPrefix+"/"+file.name,
			appkit.StaticPrefix+"child/../"+file.name,
			"/"+file.name,
			"/_appkit-other/"+file.name,
			appkit.StaticPrefix+file.name+"%2F",
		)
	}
	handler := appkit.Static()
	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "CUSTOM"} {
			response := staticResponse(handler, method, path)
			if response.Code != http.StatusNotFound {
				t.Errorf("%s %s: status %d, want 404", method, path, response.Code)
			}
		}
	}
}

func TestStaticDisallowedMethods(t *testing.T) {
	// R-7TP1-X4T3
	handler := appkit.Static()
	for _, file := range staticFiles {
		want := staticBytes(t, file.name)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "CUSTOM", "get", "head"} {
			response := staticResponse(handler, method, appkit.StaticPrefix+file.name)
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" || bytes.Equal(response.Body.Bytes(), want) {
				t.Errorf("%s %s: status %d, Allow %q, body equals asset: %t", method, file.name, response.Code, response.Header().Get("Allow"), bytes.Equal(response.Body.Bytes(), want))
			}
		}
	}
}

func TestStaticConcurrentFactories(t *testing.T) {
	// R-7UWY-AWJS
	type requestCase struct {
		method string
		path   string
	}
	var cases []requestCase
	for _, file := range staticFiles {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			cases = append(cases, requestCase{method, appkit.StaticPrefix + file.name})
		}
	}
	cases = append(cases, requestCase{http.MethodOptions, appkit.StaticPrefix + "banner.html"})
	baseline := appkit.Static()
	var expected []*httptest.ResponseRecorder
	for _, item := range cases {
		expected = append(expected, staticResponse(baseline, item.method, item.path))
	}
	errors := make(chan string, 6*4*len(cases))
	var workers sync.WaitGroup
	for range 6 {
		handler := appkit.Static()
		for range 4 {
			workers.Go(func() {
				for index, item := range cases {
					got := staticResponse(handler, item.method, item.path)
					want := expected[index]
					if got.Code != want.Code || got.Header().Get("Content-Type") != want.Header().Get("Content-Type") || got.Header().Get("Allow") != want.Header().Get("Allow") || !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
						errors <- fmt.Sprintf("%s %s: concurrent handler differs from baseline", item.method, item.path)
					}
				}
			})
		}
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
