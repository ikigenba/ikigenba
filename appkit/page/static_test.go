package page

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

var staticFiles = []struct {
	name        string
	contentType string
}{
	{"theme.css", "text/css; charset=utf-8"},
	{"feedback.js", "text/javascript; charset=utf-8"},
	{"favicon.svg", "image/svg+xml"},
	{"InterVariable.woff2", "font/woff2"},
	{"InterVariable-Italic.woff2", "font/woff2"},
	{"JetBrainsMono.woff2", "font/woff2"},
	{"OFL.txt", "text/plain; charset=utf-8"},
	{"TABLER-LICENSE.txt", "text/plain; charset=utf-8"},
}

func staticBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := assetsFS.ReadFile("assets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func staticPath(t *testing.T, name string) string {
	t.Helper()
	if strings.HasSuffix(name, ".woff2") {
		digest := sha256.Sum256(staticBytes(t, name))
		return StaticPrefix + strings.TrimSuffix(name, ".woff2") + fmt.Sprintf(".%x.woff2", digest[:8])
	}
	return StaticPrefix + name
}

func staticServedBytes(t *testing.T, name string) []byte {
	t.Helper()
	data := staticBytes(t, name)
	if name == "theme.css" {
		for _, font := range []string{"InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2"} {
			data = bytes.ReplaceAll(data, []byte(`url("`+font+`")`), []byte(`url("`+strings.TrimPrefix(staticPath(t, font), StaticPrefix)+`")`))
		}
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
	// R-J29M-1X4L
	const prefix = StaticPrefix
	if prefix != "/_appkit/" {
		t.Fatalf("StaticPrefix = %q", prefix)
	}
}

func TestStaticFactory(t *testing.T) {
	// R-J4PE-TGLZ
	factory := Static
	typed, ok := any(factory).(func() http.Handler)
	if !ok {
		t.Fatalf("Static has type %T, want func() http.Handler", factory)
	}
	if typed() == nil {
		t.Fatal("Static returned a nil handler")
	}
}

func TestStaticGETBytes(t *testing.T) {
	// R-JPX2-GEHZ
	handler := Static()
	for _, file := range staticFiles {
		t.Run(file.name, func(t *testing.T) {
			want := staticServedBytes(t, file.name)
			etag := staticResponse(handler, http.MethodGet, staticPath(t, file.name)).Header().Get("ETag")
			for _, target := range []string{
				staticPath(t, file.name),
				staticPath(t, file.name) + "?download=1&path=banner.html",
				"/%5Fappkit/" + strings.TrimPrefix(staticPath(t, file.name), StaticPrefix),
				"/_appkit%2F" + strings.TrimPrefix(staticPath(t, file.name), StaticPrefix),
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
	// R-JR4Y-U68O
	handler := Static()
	for _, file := range staticFiles {
		etag := staticResponse(handler, http.MethodGet, staticPath(t, file.name)).Header().Get("ETag")
		headers := append(staticNonmatchingHeaders(etag),
			http.Header{"Range": {"bytes=0-3"}},
			http.Header{"If-Match": {`"different"`}},
			http.Header{"If-Unmodified-Since": {"Tue, 01 Jan 1980 00:00:00 GMT"}},
			http.Header{"If-Range": {`"different"`}},
			http.Header{"If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
		)
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, header := range headers {
				response := staticConditionalResponse(handler, method, staticPath(t, file.name), header)
				if response.Code == http.StatusOK {
					if values := response.Header().Values("Content-Type"); len(values) != 1 || values[0] != file.contentType {
						t.Errorf("%s %s with %v: Content-Type %q; want exactly %q", method, file.name, header, values, file.contentType)
					}
				}
			}
		}
	}
}

func TestStaticHEAD(t *testing.T) {
	// R-J8D3-YRU2
	handler := Static()
	for _, file := range staticFiles {
		get := staticResponse(handler, http.MethodGet, staticPath(t, file.name))
		for _, target := range []string{staticPath(t, file.name), "/%5Fappkit/" + strings.TrimPrefix(staticPath(t, file.name), StaticPrefix) + "?x=1"} {
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
	// R-J9L0-CJKR
	paths := []string{StaticPrefix, StaticPrefix + "banner.html", StaticPrefix + "launcher.js", "/", "/_appkit", "/_appkit/missing", "/_appkit/assets/theme.css"}
	for _, file := range staticFiles {
		paths = append(paths,
			staticPath(t, file.name)+"/",
			staticPath(t, file.name)+"/child",
			StaticPrefix+strings.ToUpper(strings.TrimPrefix(staticPath(t, file.name), StaticPrefix)),
			StaticPrefix+"./"+file.name,
			StaticPrefix+"/"+file.name,
			StaticPrefix+"child/../"+file.name,
			"/"+file.name,
			"/_appkit-other/"+file.name,
			staticPath(t, file.name)+"%2F",
		)
	}
	handler := Static()
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
	// R-JASW-QBBG
	handler := Static()
	for _, file := range staticFiles {
		want := staticServedBytes(t, file.name)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "CUSTOM", "get", "head"} {
			for _, headers := range []http.Header{{}, {"If-None-Match": {"*"}}, {"Range": {"bytes=0-3"}}} {
				response := staticConditionalResponse(handler, method, staticPath(t, file.name), headers)
				if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" || bytes.Equal(response.Body.Bytes(), want) {
					t.Errorf("%s %s with %v: status %d, Allow %q, body equals asset: %t", method, file.name, headers, response.Code, response.Header().Get("Allow"), bytes.Equal(response.Body.Bytes(), want))
				}
			}
		}
	}
}

func TestStaticConcurrentFactories(t *testing.T) {
	// R-JI4B-0XRM
	type requestCase struct {
		method string
		path   string
	}
	var cases []requestCase
	for _, file := range staticFiles {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			cases = append(cases, requestCase{method, staticPath(t, file.name)})
		}
	}
	cases = append(cases, requestCase{http.MethodOptions, StaticPrefix + "banner.html"})
	baseline := Static()
	var expected []*httptest.ResponseRecorder
	for _, item := range cases {
		expected = append(expected, staticResponse(baseline, item.method, item.path))
	}
	errors := make(chan string, 6*4*len(cases))
	var workers sync.WaitGroup
	for range 6 {
		handler := Static()
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
	// R-JC0T-4325
	for _, file := range staticFiles {
		etag := staticResponse(Static(), http.MethodGet, staticPath(t, file.name)).Header().Get("ETag")
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, headers := range staticNonmatchingHeaders(etag) {
				response := staticConditionalResponse(Static(), method, staticPath(t, file.name), headers)
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
				response := staticConditionalResponse(Static(), method, staticPath(t, file.name), headers)
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
	// R-4WWZ-PPFN
	// R-4Y4W-3H6C
	for _, file := range staticFiles {
		want := "no-cache"
		if file.contentType == "font/woff2" {
			want = "public, max-age=31536000, immutable"
		}
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
				response := staticConditionalResponse(Static(), method, staticPath(t, file.name), headers)
				if response.Code == http.StatusOK || response.Code == http.StatusNotModified {
					if values := response.Header().Values("Cache-Control"); len(values) != 1 || values[0] != want {
						t.Errorf("%s %s with %v: Cache-Control = %q", method, file.name, headers, values)
					}
				}
			}
		}
	}
}

func TestStaticIfNoneMatch(t *testing.T) {
	// R-JFOI-9EA8
	for _, file := range staticFiles {
		handler := Static()
		path := staticPath(t, file.name)
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
	// R-JGWE-N60X
	for _, file := range staticFiles {
		handler := Static()
		path := staticPath(t, file.name)
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

func TestPreloadURLSignature(t *testing.T) {
	// R-530H-MK54
	if _, ok := any(PreloadURL).(func() string); !ok {
		t.Fatalf("PreloadURL has type %T, want func() string", PreloadURL)
	}
}

func TestPreloadURL(t *testing.T) {
	// R-548E-0BVT
	want := staticPath(t, "InterVariable.woff2")
	for range 3 {
		if got := PreloadURL(); got != want {
			t.Fatalf("PreloadURL = %q, want %q", got, want)
		}
	}
}

func TestFontHashedPaths(t *testing.T) {
	// R-4QTH-SUQ6
	for _, font := range []string{"InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2"} {
		path := staticPath(t, font)
		got := staticResponse(Static(), http.MethodGet, path)
		if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), staticBytes(t, font)) {
			t.Fatalf("%s: status %d, font bytes differ: %t", path, got.Code, !bytes.Equal(got.Body.Bytes(), staticBytes(t, font)))
		}
	}
}

func TestFontUnservedNames(t *testing.T) {
	// R-50KO-V0NQ
	for _, font := range []string{"InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2"} {
		correctPath := staticPath(t, font)
		digest := strings.TrimSuffix(strings.TrimPrefix(correctPath, StaticPrefix+strings.TrimSuffix(font, ".woff2")+"."), ".woff2")
		wrong := "0" + digest[1:]
		if wrong == digest {
			wrong = "1" + digest[1:]
		}
		paths := []string{StaticPrefix + font}
		for _, hash := range []string{wrong, strings.ToUpper(digest), digest[:15], digest + "0", "f", strings.Repeat("A", 64)} {
			if hash != digest {
				paths = append(paths, StaticPrefix+strings.TrimSuffix(font, ".woff2")+"."+hash+".woff2")
			}
		}
		for _, path := range paths {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "CUSTOM"} {
				if got := staticResponse(Static(), method, path); got.Code != http.StatusNotFound {
					t.Errorf("%s %s: status %d, want 404", method, path, got.Code)
				}
			}
		}
	}
}

func TestStylesheetExactRewrite(t *testing.T) {
	// R-4UH6-Y5Y9
	got := staticResponse(Static(), http.MethodGet, StaticPrefix+"theme.css")
	if !bytes.Equal(got.Body.Bytes(), staticServedBytes(t, "theme.css")) {
		t.Fatal("stylesheet differs from exact font URL replacement")
	}
}

func TestStylesheetReferencesHashedFonts(t *testing.T) {
	// R-4VP3-BXOY
	got := staticResponse(Static(), http.MethodGet, StaticPrefix+"theme.css")
	for _, font := range []string{"InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2"} {
		want := `url("` + strings.TrimPrefix(staticPath(t, font), StaticPrefix) + `")`
		if !strings.Contains(got.Body.String(), want) {
			t.Errorf("stylesheet lacks %s", want)
		}
	}
}
