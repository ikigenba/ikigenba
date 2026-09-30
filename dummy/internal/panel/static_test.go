package panel

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

var sharedFiles = []struct{ name, contentType string }{
	{"theme.css", "text/css; charset=utf-8"},
	{"launcher.js", "text/javascript; charset=utf-8"},
	{"InterVariable.woff2", "font/woff2"},
	{"InterVariable-Italic.woff2", "font/woff2"},
	{"JetBrainsMono.woff2", "font/woff2"},
	{"OFL.txt", "text/plain; charset=utf-8"},
	{"TABLER-LICENSE.txt", "text/plain; charset=utf-8"},
}

func staticTestHandler() http.Handler {
	return Handler(widget.NewStore(), func(appkit.User) appkit.Banner { return appkit.Banner{} }, io.Discard)
}

func staticStrongTag(tag string) bool {
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return false
	}
	for _, b := range []byte(tag[1 : len(tag)-1]) {
		if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
			return false
		}
	}
	return true
}

// R-SC7S-5VXC R-SDFO-JNO1
func TestSharedStaticDelegation(t *testing.T) {
	if appkit.StaticPrefix != "/_appkit/" {
		t.Fatalf("static prefix = %q", appkit.StaticPrefix)
	}
	paths := []string{"/_appkit/", "/_appkit/missing", "/_appkit/../widgets", "/_appkit/%2Ftheme.css", "/_appkit/%74heme.css"}
	for _, file := range sharedFiles {
		paths = append(paths, "/_appkit/"+file.name, "/_appkit/"+file.name+"/extra", "/_appkit/"+strings.ToUpper(file.name))
	}
	h := staticTestHandler()
	static := appkit.Static()
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, fields := range [][]string{nil, {"*"}, {`W/"stale", "other"`}, {`"bad`}, {`"a"`, `"b"`}} {
				r := pageTestRequest(method, path)
				r.Header["If-None-Match"] = fields
				r.Header.Set("If-Modified-Since", "Wed, 21 Oct 2099 07:28:00 GMT")
				r.Header.Set("Range", "bytes=0-7")
				r.Header.Set("If-Match", "*")
				r.Header.Set("If-Range", `"stale"`)
				r.Header.Set("If-Unmodified-Since", "Wed, 21 Oct 2015 07:28:00 GMT")
				got := pageTestResponse(h, r)
				want := pageTestResponse(static, r.Clone(r.Context()))
				if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
					t.Fatalf("%s %s validators %q: got %d %v, want %d %v", method, path, fields, got.Code, got.Header(), want.Code, want.Header())
				}
			}
		}
	}
}

// R-SENK-XFEQ R-SFVH-B75F R-SH3D-OYW4
func TestSharedStaticContentAndCache(t *testing.T) {
	first, second := staticTestHandler(), staticTestHandler()
	for _, file := range sharedFiles {
		path := "/_appkit/" + file.name
		base := pageTestResponse(first, pageTestRequest("GET", path))
		if base.Code != 200 || base.Body.Len() == 0 || !reflect.DeepEqual(base.Header().Values("Content-Type"), []string{file.contentType}) {
			t.Fatalf("%s: %d %v body length %d", path, base.Code, base.Header(), base.Body.Len())
		}
		for _, h := range []http.Handler{first, second} {
			got := pageTestResponse(h, pageTestRequest("GET", path))
			if got.Body.String() != base.Body.String() || got.Header().Get("ETag") != base.Header().Get("ETag") {
				t.Fatalf("unstable content or tag: %s", path)
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, validator := range []string{"", "*"} {
				r := pageTestRequest(method, path)
				if validator != "" {
					r.Header.Set("If-None-Match", validator)
				}
				got := pageTestResponse(first, r)
				tags := got.Header().Values("ETag")
				if len(tags) != 1 || !staticStrongTag(tags[0]) || !reflect.DeepEqual(got.Header().Values("Cache-Control"), []string{"no-cache"}) {
					t.Fatalf("%s cache headers: %v", path, got.Header())
				}
			}
		}
	}
}

// R-SJJ6-GIDI R-SKR2-UA47
func TestSharedStaticRevalidation(t *testing.T) {
	h := staticTestHandler()
	for _, file := range sharedFiles {
		path := "/_appkit/" + file.name
		base := pageTestResponse(h, pageTestRequest("GET", path))
		tag := base.Header().Get("ETag")
		for _, condition := range []string{"*", tag, "W/" + tag, " ,\tW/" + tag + "\t,", `"stale", ` + tag + `, W/"other"`, tag + ", " + tag} {
			for _, method := range []string{"GET", "HEAD"} {
				for _, modified := range []string{"", "Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2099 07:28:00 GMT", "malformed"} {
					r := pageTestRequest(method, path)
					r.Header.Set("If-None-Match", condition)
					r.Header.Set("If-Modified-Since", modified)
					got := pageTestResponse(h, r)
					if got.Code != 304 || got.Body.Len() != 0 || got.Header().Get("ETag") != tag {
						t.Fatalf("%s %s matching %q modified %q: %d %v", method, path, condition, modified, got.Code, got.Header())
					}
				}
			}
		}
		for _, condition := range []string{`"stale"`, `W/"stale", "other"`, ` , "stale" , , W/"other" ,`, `"*"`, ",,"} {
			for _, modified := range []string{"", "Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2099 07:28:00 GMT", "malformed"} {
				r := pageTestRequest("GET", path)
				r.Header.Set("If-None-Match", condition)
				r.Header.Set("If-Modified-Since", modified)
				got := pageTestResponse(h, r)
				if got.Code != 200 || got.Header().Get("ETag") != tag || got.Body.String() != base.Body.String() {
					t.Fatalf("%s nonmatching %q modified %q: %d %v", path, condition, modified, got.Code, got.Header())
				}
			}
		}
	}
}

// R-SLYZ-81UW
func TestSharedStaticRefusedMethods(t *testing.T) {
	h := staticTestHandler()
	for _, file := range sharedFiles {
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `"stale"`, "malformed"} {
				r := pageTestRequest(method, "/_appkit/"+file.name)
				r.Header.Set("If-None-Match", validator)
				got := pageTestResponse(h, r)
				if got.Code != 405 || got.Header().Get("Allow") != "GET, HEAD" || len(got.Header().Values("ETag")) != 0 {
					t.Fatalf("%s %s: %d %v", method, r.URL.Path, got.Code, got.Header())
				}
			}
		}
	}
}

// R-4FAJ-45J6
func TestSharedStaticMissingPaths(t *testing.T) {
	h := staticTestHandler()
	paths := []string{"/_appkit/", "/_appkit/missing", "/_appkit/banner.html"}
	for _, file := range sharedFiles {
		paths = append(paths, "/_appkit/"+file.name+"/extra", "/_appkit/"+file.name+"x", "/_appkit/"+file.name+"%2Fextra", "/_appkit/"+strings.ToUpper(file.name))
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `"stale"`, "malformed"} {
				r := pageTestRequest(method, path)
				r.Header.Set("If-None-Match", validator)
				got := pageTestResponse(h, r)
				if got.Code != 404 || len(got.Header().Values("ETag")) != 0 {
					t.Fatalf("%s %s: %d %v", method, path, got.Code, got.Header())
				}
			}
		}
	}
}
