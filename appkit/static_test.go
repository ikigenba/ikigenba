package appkit_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit"
)

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
	data, err := appkit.AssetsFS.ReadFile("assets/" + name)
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

func staticConditionalResponse(handler http.Handler, method, target string, headers http.Header) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	request.Header = headers.Clone()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func staticNonmatchingHeaders(etag string) []http.Header {
	nonmatch := etag[:len(etag)-1] + `!"`
	return []http.Header{
		{},
		{"If-None-Match": {""}},
		{"If-None-Match": {nonmatch}},
		{"If-None-Match": {"W/" + nonmatch + ", , " + nonmatch}},
		{"If-None-Match": {nonmatch}, "If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
		{"If-None-Match": {"W/" + nonmatch}, "If-Modified-Since": {"Tue, 01 Jan 1980 00:00:00 GMT"}},
		{"If-None-Match": {", ,\t,"}, "If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
	}
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
	typed, ok := any(factory).(func() http.Handler)
	if !ok {
		t.Fatalf("Static has type %T, want func() http.Handler", factory)
	}
	if typed() == nil {
		t.Fatal("Static returned a nil handler")
	}
}

func TestStaticGETBytes(t *testing.T) {
	// R-41EX-WQFL
	handler := appkit.Static()
	for _, file := range staticFiles {
		t.Run(file.name, func(t *testing.T) {
			want := staticBytes(t, file.name)
			etag := staticResponse(handler, http.MethodGet, appkit.StaticPrefix+file.name).Header().Get("ETag")
			for _, target := range []string{
				appkit.StaticPrefix + file.name,
				appkit.StaticPrefix + file.name + "?download=1&path=banner.html",
				"/%5Fappkit/" + file.name,
				"/_appkit%2F" + file.name,
			} {
				for _, headers := range staticNonmatchingHeaders(etag) {
					response := staticConditionalResponse(handler, http.MethodGet, target, headers)
					if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), want) {
						t.Errorf("GET %s with %v: status %d, body equals asset: %t", target, headers, response.Code, bytes.Equal(response.Body.Bytes(), want))
					}
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
	// R-42MU-AI6A
	handler := appkit.Static()
	for _, file := range staticFiles {
		get := staticResponse(handler, http.MethodGet, appkit.StaticPrefix+file.name)
		for _, target := range []string{appkit.StaticPrefix + file.name, "/%5Fappkit/" + file.name + "?x=1"} {
			for _, headers := range staticNonmatchingHeaders(get.Header().Get("ETag")) {
				response := staticConditionalResponse(handler, http.MethodHead, target, headers)
				if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != get.Header().Get("Content-Type") {
					t.Errorf("HEAD %s with %v: status %d, body length %d, Content-Type %q; GET Content-Type %q", target, headers, response.Code, response.Body.Len(), response.Header().Get("Content-Type"), get.Header().Get("Content-Type"))
				}
			}
		}
	}
}

func TestStaticUnknownPaths(t *testing.T) {
	// R-71YB-5NNS
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
			for _, headers := range []http.Header{{}, {"If-None-Match": {"*"}}, {"Range": {"bytes=0-3"}}} {
				response := staticConditionalResponse(handler, method, path, headers)
				if response.Code != http.StatusNotFound {
					t.Errorf("%s %s with %v: status %d, want 404", method, path, headers, response.Code)
				}
			}
		}
	}
}

func TestStaticDisallowedMethods(t *testing.T) {
	// R-7367-JFEH
	handler := appkit.Static()
	for _, file := range staticFiles {
		want := staticBytes(t, file.name)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "CUSTOM", "get", "head"} {
			for _, headers := range []http.Header{{}, {"If-None-Match": {"*"}}, {"Range": {"bytes=0-3"}}} {
				response := staticConditionalResponse(handler, method, appkit.StaticPrefix+file.name, headers)
				if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" || bytes.Equal(response.Body.Bytes(), want) {
					t.Errorf("%s %s with %v: status %d, Allow %q, body equals asset: %t", method, file.name, headers, response.Code, response.Header().Get("Allow"), bytes.Equal(response.Body.Bytes(), want))
				}
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

func TestStaticStrongEntityTags(t *testing.T) {
	// R-74E3-X756
	for _, file := range staticFiles {
		etag := staticResponse(appkit.Static(), http.MethodGet, appkit.StaticPrefix+file.name).Header().Get("ETag")
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, headers := range staticNonmatchingHeaders(etag) {
				response := staticConditionalResponse(appkit.Static(), method, appkit.StaticPrefix+file.name, headers)
				assertStrongEntityTag(t, response.Header().Values("ETag"))
			}
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, headers := range []http.Header{
				{"Range": {"bytes=0-3"}},
				{"If-Match": {`"different"`}},
				{"If-Unmodified-Since": {"Tue, 01 Jan 1980 00:00:00 GMT"}},
				{"If-Range": {`"different"`}},
				{"If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
			} {
				response := staticConditionalResponse(appkit.Static(), method, appkit.StaticPrefix+file.name, headers)
				if response.Code == http.StatusOK {
					assertStrongEntityTag(t, response.Header().Values("ETag"))
				}
			}
		}
	}
}

func assertStrongEntityTag(t *testing.T, values []string) {
	t.Helper()
	if len(values) != 1 {
		t.Fatalf("ETag headers = %q, want exactly one", values)
	}
	value := values[0]
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		t.Fatalf("ETag = %q, want a double-quoted strong entity-tag", value)
	}
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if character < 0x21 || character == '"' || character == 0x7f {
			t.Fatalf("ETag = %q, contains invalid opaque-tag byte", value)
		}
	}
}

func TestStaticCacheControl(t *testing.T) {
	// R-EH0Y-OJ8X
	for _, file := range staticFiles {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, headers := range []http.Header{
				{},
				{"If-None-Match": {"*"}},
				{"If-None-Match": {`"different"`}, "If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
				{"Range": {"bytes=0-3"}},
				{"If-Match": {`"different"`}},
				{"If-Unmodified-Since": {"Tue, 01 Jan 1980 00:00:00 GMT"}},
				{"If-Range": {`"different"`}},
				{"If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
			} {
				response := staticConditionalResponse(appkit.Static(), method, appkit.StaticPrefix+file.name, headers)
				if response.Code == http.StatusOK || response.Code == http.StatusNotModified {
					if values := response.Header().Values("Cache-Control"); len(values) != 1 || values[0] != "no-cache" {
						t.Errorf("%s %s with %v: Cache-Control = %q", method, file.name, headers, values)
					}
				}
			}
		}
	}
}

func TestStaticIfNoneMatch(t *testing.T) {
	// R-EJGR-G2QB
	for _, file := range staticFiles {
		handler := appkit.Static()
		path := appkit.StaticPrefix + file.name
		etag := staticResponse(handler, http.MethodGet, path).Header().Get("ETag")
		cases := []struct {
			value string
			match bool
		}{
			{"*", true},
			{etag, true},
			{"W/" + etag, true},
			{`"different", ` + etag, true},
			{etag + `, "different"`, true},
			{`W/"different", W/` + etag + `, "other"`, true},
			{"\t, , W/" + etag + "\t , ,\t", true},
			{`"contains,a,comma", ` + etag, true},
			{`"backslash\", ` + etag, true},
			{"\"\x80\xff\", " + etag, true},
			{`""`, etag == `""`},
			{`W/""`, etag == `""`},
			{`"different"`, etag == `"different"`},
			{`W/"different", , "other"`, etag == `"different"` || etag == `"other"`},
			{", ,\t,", false},
			{`"contains,a,comma", "backslash\"`, etag == `"contains,a,comma"` || etag == `"backslash\"`},
			{"\"\x80\xff\"", etag == "\"\x80\xff\""},
			{`"prefix` + etag[1:], false},
			{etag[:len(etag)-1] + `suffix"`, false},
		}
		for _, item := range cases {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := staticConditionalResponse(handler, method, path, http.Header{"If-None-Match": {item.value}})
				want := http.StatusOK
				if item.match {
					want = http.StatusNotModified
				}
				if response.Code != want {
					t.Errorf("%s %s If-None-Match %q: status %d, want %d", method, file.name, item.value, response.Code, want)
				}
			}
		}
	}
}

func TestStaticNotModified(t *testing.T) {
	// R-43UQ-O9WZ
	for _, file := range staticFiles {
		handler := appkit.Static()
		path := appkit.StaticPrefix + file.name
		etag := staticResponse(handler, http.MethodGet, path).Header().Get("ETag")
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, condition := range []string{"*", etag, "W/" + etag, `"other", W/` + etag + ", ,"} {
				for _, modifiedSince := range []string{"", "Tue, 01 Jan 1980 00:00:00 GMT", "Tue, 01 Jan 2030 00:00:00 GMT"} {
					headers := http.Header{"If-None-Match": {condition}}
					if modifiedSince != "" {
						headers.Set("If-Modified-Since", modifiedSince)
					}
					response := staticConditionalResponse(handler, method, path, headers)
					values := response.Header().Values("ETag")
					if response.Code != http.StatusNotModified || response.Body.Len() != 0 || len(values) != 1 || values[0] != etag {
						t.Errorf("%s %s with %v: status %d, body length %d, ETag %q; want 304, empty body, %q", method, file.name, headers, response.Code, response.Body.Len(), values, etag)
					}
				}
			}
		}
	}
}
