package web

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
)

// R-5ADI-UG1I
func webSharedFiles(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	files := map[string]string{
		"theme.css":          "text/css; charset=utf-8",
		"feedback.js":        "text/javascript; charset=utf-8",
		"favicon.svg":        "image/svg+xml",
		"OFL.txt":            "text/plain; charset=utf-8",
		"TABLER-LICENSE.txt": "text/plain; charset=utf-8",
	}
	css := webRequest(h, "GET", page.StaticPrefix+"theme.css", "user", "discovery", nil)
	if css.Code != http.StatusOK {
		t.Fatalf("stylesheet discovery: %d", css.Code)
	}
	files[strings.TrimPrefix(page.PreloadURL(), page.StaticPrefix)] = "font/woff2"
	for _, match := range regexp.MustCompile(`url\("([^"]*)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		name := match[1]
		if strings.HasSuffix(name, ".woff2") && !strings.ContainsAny(name, "/\\:\"?#%") {
			files[name] = "font/woff2"
		}
	}
	return files
}

// R-76T4-MSOY
func TestStaticDelegation(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	paths := []string{"/_appkit/", "/_appkit/launcher.js", "/_appkit/nope", "/_appkit/THEME.CSS", "/_appkit/theme.css/", "/_appkit/theme.css/x", "/_appkit/FEEDBACK.JS", "/_appkit/feedback.js/", "/%5fappkit/%66eedback.js", "/%5fappkit/%66avicon.svg", "/_appkit/FAVICON.SVG", "/_appkit/favicon.svg/", "/_appkit/favicon.svg/x", "/_appkit/../feedback.js"}
	for name := range webSharedFiles(t, h) {
		paths = append(paths, page.StaticPrefix+name)
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PATCH"} {
			for _, headers := range []http.Header{{}, {"Range": {"bytes=0-7"}}, {"If-None-Match": {"*"}}, {"If-Match": {"\"absent\""}}} {
				r := httptest.NewRequest(method, path, strings.NewReader("body"))
				r.Header = headers.Clone()
				r.Header.Set("X-User-Id", "user")
				got, want := httptest.NewRecorder(), httptest.NewRecorder()
				h.ServeHTTP(got, r.Clone(r.Context()))
				page.Static().ServeHTTP(want, r.Clone(r.Context()))
				if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
					t.Fatalf("%s %s headers %v: got %d %v, want %d %v", method, path, headers, got.Code, got.Header(), want.Code, want.Header())
				}
			}
		}
	}
}

// R-5BLF-87S7 R-BBMI-3ZO2 R-BCUE-HRER R-7AGT-S3X1 R-5E17-ZR9L
func TestSharedFilesAndHead(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	other := newWebFixture(t)
	h2 := Handler(other.cfg)
	for name, mime := range webSharedFiles(t, h) {
		path := page.StaticPrefix + name
		get := webRequest(h, "GET", path, "user", "one", nil)
		if get.Code != 200 || get.Body.Len() == 0 || !reflect.DeepEqual(get.Header().Values("Content-Type"), []string{mime}) {
			t.Fatalf("%s: %d %v bytes %d", path, get.Code, get.Header(), get.Body.Len())
		}
		assertWebETag(t, get.Header(), mime)
		for _, another := range []http.Handler{h, h2} {
			again := webRequest(another, "GET", path, "user", "two", nil)
			if again.Body.String() != get.Body.String() || again.Header().Get("ETag") != get.Header().Get("ETag") {
				t.Fatalf("%s changes between responses", path)
			}
		}
		head := webRequest(h, "HEAD", path, "user", "head", nil)
		if head.Code != 200 || head.Body.Len() != 0 {
			t.Fatalf("HEAD %s: %d %q", path, head.Code, head.Body.String())
		}
		for _, key := range []string{"Content-Type", "ETag", "Cache-Control"} {
			if !reflect.DeepEqual(head.Header().Values(key), get.Header().Values(key)) {
				t.Fatalf("HEAD %s differs for %s", path, key)
			}
		}
	}
}

func assertWebETag(t *testing.T, h http.Header, mime string) {
	t.Helper()
	tags := h.Values("ETag")
	if len(tags) != 1 || len(tags[0]) < 2 || tags[0][0] != '"' || tags[0][len(tags[0])-1] != '"' {
		t.Fatalf("not one strong entity tag: %v", tags)
	}
	for _, b := range []byte(tags[0][1 : len(tags[0])-1]) {
		if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
			t.Fatalf("invalid entity tag byte %x", b)
		}
	}
	cache := "no-cache"
	if mime == "font/woff2" {
		cache = "public, max-age=31536000, immutable"
	}
	if !reflect.DeepEqual(h.Values("Cache-Control"), []string{cache}) {
		t.Fatalf("Cache-Control %v", h.Values("Cache-Control"))
	}
}

// R-BE2A-VJ5G R-5CTB-LZIW
func TestSharedRevalidation(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	for name, mime := range webSharedFiles(t, h) {
		path := page.StaticPrefix + name
		get := webRequest(h, "GET", path, "user", "initial", nil)
		tag := get.Header().Get("ETag")
		for _, method := range []string{"GET", "HEAD"} {
			for _, match := range []string{"*", tag, "W/" + tag, ", \"other\",\tW/" + tag + " ,"} {
				for _, modified := range []string{"", "Mon, 01 Jan 1900 00:00:00 GMT", "Tue, 01 Jan 2100 00:00:00 GMT", "invalid"} {
					r := httptest.NewRequest(method, path, nil)
					r.Header.Set("X-User-Id", "user")
					r.Header.Set("If-None-Match", match)
					r.Header.Set("If-Modified-Since", modified)
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != tag {
						t.Fatalf("%s %s %q: %d %v", method, path, match, w.Code, w.Header())
					}
					assertWebETag(t, w.Header(), mime)
				}
			}
		}
		for _, miss := range []string{"\"other\"", ", W/\"first\", \"second\", "} {
			for _, modified := range []string{"", "Mon, 01 Jan 1900 00:00:00 GMT", "Tue, 01 Jan 2100 00:00:00 GMT", "invalid"} {
				r := httptest.NewRequest("GET", path, nil)
				r.Header.Set("X-User-Id", "user")
				r.Header.Set("If-None-Match", miss)
				if modified != "" {
					r.Header.Set("If-Modified-Since", modified)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 200 || w.Header().Get("ETag") != tag || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{mime}) || w.Body.String() != get.Body.String() {
					t.Fatalf("nonmatching validator %s %q: %d %v", path, miss, w.Code, w.Header())
				}
				assertWebETag(t, w.Header(), mime)
			}
		}
	}
}

// R-7GKB-OYMI R-7HS8-2QD7
func TestStaticRefusals(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	for _, validator := range []string{"", "*", "\"anything\""} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for name := range webSharedFiles(t, h) {
				r := httptest.NewRequest(method, page.StaticPrefix+name, nil)
				r.Header.Set("X-User-Id", "user")
				r.Header.Set("If-None-Match", validator)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) {
					t.Fatalf("%s %s: %d %v", method, name, w.Code, w.Header())
				}
			}
		}
		for _, path := range []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/launcher.js", "/_appkit/nope.css", "/_appkit/theme.css/", "/_appkit/theme.css/x", "/_appkit/THEME.CSS", "/_appkit/favicon.svg/", "/_appkit/favicon.svg/x", "/_appkit/FAVICON.SVG"} {
			for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("X-User-Id", "user")
				r.Header.Set("If-None-Match", validator)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != 404 {
					t.Fatalf("%s %s: %d", method, path, w.Code)
				}
			}
		}
	}
}

// R-BHQ0-0UDJ
func TestPlainFontAliasesMissing(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	hashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for name, mime := range webSharedFiles(t, h) {
		if mime != "font/woff2" || !hashed.MatchString(name) {
			continue
		}
		path := page.StaticPrefix + hashed.ReplaceAllString(name, ".woff2")
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `"stale"`, "malformed"} {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("X-User-Id", "user")
				r.Header.Set("If-None-Match", validator)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != http.StatusNotFound {
					t.Fatalf("%s %s validator %q: %d", method, path, validator, w.Code)
				}
			}
		}
	}
}
