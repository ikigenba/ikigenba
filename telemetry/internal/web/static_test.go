package web_test

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

func TestSharedFiles(t *testing.T) {
	// R-QPR8-SG98 R-QVUQ-PAYP R-QX2N-32PE R-QYAJ-GUG3 R-QZIF-UM6S R-R0QC-8DXH R-R1Y8-M5O6 R-R364-ZXEV R-R4E1-DP5K R-R5LX-RGW9
	f := newFixture(t)
	second := web.Handler(freshServer(t, f, f.cfg))
	static := page.Static()
	for _, tc := range []struct{ name, content string }{{"theme.css", "text/css; charset=utf-8"}, {"launcher.js", "text/javascript; charset=utf-8"}, {"InterVariable.woff2", "font/woff2"}, {"InterVariable-Italic.woff2", "font/woff2"}, {"JetBrainsMono.woff2", "font/woff2"}, {"OFL.txt", "text/plain; charset=utf-8"}, {"TABLER-LICENSE.txt", "text/plain; charset=utf-8"}} {
		path := page.StaticPrefix + tc.name
		get := request(f.h, "GET", path, "u")
		again := request(second, "GET", path, "u")
		equalResponse(t, get, again)
		if get.Code != 200 || get.Body.Len() == 0 || !reflect.DeepEqual(get.Header().Values("Content-Type"), []string{tc.content}) || !reflect.DeepEqual(get.Header().Values("Cache-Control"), []string{"no-cache"}) || len(get.Header().Values("ETag")) != 1 {
			t.Fatal(tc, get.Code, get.Header())
		}
		etag := get.Header().Get("ETag")
		if !strongTag(etag) {
			t.Fatal(etag)
		}
		head := request(f.h, "HEAD", path, "u")
		if head.Code != 200 || head.Body.Len() != 0 {
			t.Fatal(head)
		}
		for _, name := range []string{"Content-Type", "Cache-Control", "ETag"} {
			if head.Header().Get(name) != get.Header().Get(name) {
				t.Fatal(name)
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, value := range []string{"*", etag, "W/" + etag, `"different", , W/` + etag + ", "} {
				r := httptest.NewRequest(method, "http://example"+path, nil)
				r.Header.Set("X-User-Id", "u")
				r.Header.Set("If-None-Match", value)
				r.Header.Set("If-Modified-Since", "Wed, 01 Jan 2099 00:00:00 GMT")
				out, want := httptest.NewRecorder(), httptest.NewRecorder()
				f.h.ServeHTTP(out, r)
				static.ServeHTTP(want, r)
				equalResponse(t, out, want)
				if out.Code != 304 || out.Body.Len() != 0 || out.Header().Get("ETag") != etag || out.Header().Get("Cache-Control") != "no-cache" {
					t.Fatal(out)
				}
			}
		}
		for _, value := range []string{`"different"`, `W/"different", "other"`} {
			r := httptest.NewRequest("GET", "http://example"+path, nil)
			r.Header.Set("X-User-Id", "u")
			r.Header.Set("If-None-Match", value)
			r.Header.Set("If-Modified-Since", "Wed, 01 Jan 2099 00:00:00 GMT")
			out := httptest.NewRecorder()
			f.h.ServeHTTP(out, r)
			if out.Code != 200 || out.Body.String() != get.Body.String() || out.Header().Get("ETag") != etag {
				t.Fatal(out)
			}
		}
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
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
	for _, path := range []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/nope.css", "/_appkit/theme.css/", "/_appkit/theme.css/x", "/_appkit/THEME.CSS"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
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
