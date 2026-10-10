package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

type sharedFile struct{ name, content string }

// R-0RX9-9LQX: discover font paths through the public stylesheet and preload API.
func sharedFiles(t *testing.T, h http.Handler) []sharedFile {
	t.Helper()
	files := []sharedFile{
		{"theme.css", "text/css; charset=utf-8"},
		{"feedback.js", "text/javascript; charset=utf-8"},
		{"favicon.svg", "image/svg+xml"},
		{"OFL.txt", "text/plain; charset=utf-8"},
		{"TABLER-LICENSE.txt", "text/plain; charset=utf-8"},
	}
	css := request(h, "GET", page.StaticPrefix+"theme.css", "u")
	if css.Code != http.StatusOK {
		t.Fatalf("stylesheet discovery: status %d", css.Code)
	}
	seen := map[string]bool{}
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			files = append(files, sharedFile{strings.TrimPrefix(path, page.StaticPrefix), "font/woff2"})
		}
	}
	add(page.PreloadURL())
	for _, match := range regexp.MustCompile(`url\("([^"]*)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		name := match[1]
		if strings.HasSuffix(name, ".woff2") && !strings.ContainsAny(name, "/\\:\"?#%") {
			add(page.StaticPrefix + name)
		}
	}
	return files
}

func TestSharedFiles(t *testing.T) {
	// R-0UD2-158B R-UQUD-O796 R-US2A-1YZV R-QVUQ-PAYP R-QZIF-UM6S R-UTA6-FQQK R-UUI2-TIH9 R-0VKY-EWZ0 R-R4E1-DP5K R-R5LX-RGW9
	f := newFixture(t)
	second := web.Handler(freshServer(t, f, f.cfg))
	static := page.Static()
	files := sharedFiles(t, f.h)
	for _, tc := range files {
		cache := "no-cache"
		if tc.content == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		path := page.StaticPrefix + tc.name
		get := request(f.h, "GET", path, "u")
		again := request(second, "GET", path, "u")
		equalResponse(t, get, again)
		equalResponse(t, get, request(f.h, "GET", path, "u"))
		equalResponse(t, get, request(static, "GET", path, "u"))
		// URL.Path, including an escaped filename's decoded first byte, selects the file.
		escaped := fmt.Sprintf("%s%%%02X%s", page.StaticPrefix, tc.name[0], tc.name[1:])
		equalResponse(t, get, request(f.h, "GET", escaped+"?ignored=1", "u"))
		if get.Code != 200 || get.Body.Len() == 0 || !reflect.DeepEqual(get.Header().Values("Content-Type"), []string{tc.content}) || !reflect.DeepEqual(get.Header().Values("Cache-Control"), []string{cache}) || len(get.Header().Values("ETag")) != 1 {
			t.Fatal(tc, get.Code, get.Header())
		}
		etag := get.Header().Get("ETag")
		if !strongTag(etag) {
			t.Fatal(etag)
		}
		head := request(f.h, "HEAD", path, "u")
		equalResponse(t, head, request(static, "HEAD", path, "u"))
		if head.Code != 200 || head.Body.Len() != 0 {
			t.Fatal(head)
		}
		for _, name := range []string{"Content-Type", "Cache-Control", "ETag"} {
			if !reflect.DeepEqual(head.Header().Values(name), get.Header().Values(name)) {
				t.Fatal(name)
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, value := range []string{"*", etag, "W/" + etag, `"different", , W/` + etag + ", ", " ,\tW/" + etag + "\t,", etag + ", " + etag} {
				for _, modified := range []string{"", "Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2099 07:28:00 GMT", "malformed"} {
					r := httptest.NewRequest(method, "http://example"+path, nil)
					r.Header.Set("X-User-Id", "u")
					r.Header.Set("If-None-Match", value)
					if modified != "" {
						r.Header.Set("If-Modified-Since", modified)
					}
					out, want := httptest.NewRecorder(), httptest.NewRecorder()
					f.h.ServeHTTP(out, r)
					static.ServeHTTP(want, r)
					equalResponse(t, out, want)
					if out.Code != 304 || out.Body.Len() != 0 || !reflect.DeepEqual(out.Header().Values("Cache-Control"), []string{cache}) || !reflect.DeepEqual(out.Header().Values("ETag"), []string{etag}) {
						t.Fatalf("%s %s matching %q modified %q: %d %v", method, path, value, modified, out.Code, out.Header())
					}
				}
			}
		}
		for _, value := range []string{`"different"`, `W/"different", "other"`, ` , "different" , , W/"other" ,`, `"*"`} {
			for _, modified := range []string{"", "Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2099 07:28:00 GMT", "malformed"} {
				r := httptest.NewRequest("GET", "http://example"+path, nil)
				r.Header.Set("X-User-Id", "u")
				r.Header.Set("If-None-Match", value)
				if modified != "" {
					r.Header.Set("If-Modified-Since", modified)
				}
				out := httptest.NewRecorder()
				f.h.ServeHTTP(out, r)
				if out.Code != 200 || out.Body.String() != get.Body.String() || !reflect.DeepEqual(out.Header().Values("ETag"), []string{etag}) || !reflect.DeepEqual(out.Header().Values("Cache-Control"), []string{cache}) {
					t.Fatalf("%s nonmatching %q modified %q: %d %v", path, value, modified, out.Code, out.Header())
				}
			}
		}
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CUSTOM"} {
			r := httptest.NewRequest(method, "http://example"+path, nil)
			r.Header.Set("X-User-Id", "u")
			r.Header.Set("If-None-Match", etag)
			out, want := httptest.NewRecorder(), httptest.NewRecorder()
			f.h.ServeHTTP(out, r)
			static.ServeHTTP(want, r)
			equalResponse(t, out, want)
			if out.Code != 405 || !reflect.DeepEqual(out.Header().Values("Allow"), []string{"GET, HEAD"}) {
				t.Fatal(out)
			}
		}
		for _, headers := range []map[string]string{{"Range": "bytes=0-7"}, {"If-Match": `"wrong"`}, {"If-Unmodified-Since": "Thu, 01 Jan 1970 00:00:00 GMT"}, {"Range": "bytes=0-7", "If-Range": etag}} {
			r := httptest.NewRequest("GET", "http://example"+path, nil)
			r.Header.Set("X-User-Id", "u")
			for k, v := range headers {
				r.Header.Set(k, v)
			}
			out, want := httptest.NewRecorder(), httptest.NewRecorder()
			f.h.ServeHTTP(out, r)
			static.ServeHTTP(want, r)
			equalResponse(t, out, want)
		}
	}
	invalid := []string{page.StaticPrefix, page.StaticPrefix + "banner.html", page.StaticPrefix + "nope.css", page.StaticPrefix + "launcher.js"}
	for _, tc := range files {
		invalid = append(invalid, page.StaticPrefix+tc.name+"/", page.StaticPrefix+tc.name+"/x", page.StaticPrefix+strings.ToUpper(tc.name))
	}
	for _, path := range invalid {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CUSTOM"} {
			r := httptest.NewRequest(method, "http://example"+path, nil)
			r.Header.Set("X-User-Id", "u")
			r.Header.Set("If-None-Match", "*")
			out, want := httptest.NewRecorder(), httptest.NewRecorder()
			f.h.ServeHTTP(out, r)
			static.ServeHTTP(want, r)
			equalResponse(t, out, want)
			if out.Code != 404 {
				t.Fatal(out)
			}
		}
	}
}
func strongTag(s string) bool {
	if len(s) < 2 || !strings.HasPrefix(s, `"`) || !strings.HasSuffix(s, `"`) {
		return false
	}
	for _, b := range []byte(s[1 : len(s)-1]) {
		if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
			return false
		}
	}
	return true
}

// R-UWXV-L1YN
func TestPlainFontAliasesMissing(t *testing.T) {
	f := newFixture(t)
	hashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for _, file := range sharedFiles(t, f.h) {
		if file.content != "font/woff2" || !hashed.MatchString(file.name) {
			continue
		}
		path := page.StaticPrefix + hashed.ReplaceAllString(file.name, ".woff2")
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CUSTOM"} {
			if got := request(f.h, method, path, "u"); got.Code != http.StatusNotFound {
				t.Fatalf("%s %s: status %d", method, path, got.Code)
			}
		}
	}
}
