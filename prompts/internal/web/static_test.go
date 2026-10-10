package web_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

func invoke(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func staticPaths(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	paths := map[string]string{page.StaticPrefix + "theme.css": "text/css; charset=utf-8", page.StaticPrefix + "feedback.js": "text/javascript; charset=utf-8", page.StaticPrefix + "favicon.svg": "image/svg+xml", page.StaticPrefix + "OFL.txt": "text/plain; charset=utf-8", page.StaticPrefix + "TABLER-LICENSE.txt": "text/plain; charset=utf-8", page.PreloadURL(): "font/woff2"}
	w := invoke(h, req("GET", page.StaticPrefix+"theme.css"))
	equal(t, w.Code, 200)
	re := regexp.MustCompile(`url\("([^/\\:"?#%]+\.woff2)"\)`)
	for _, match := range re.FindAllStringSubmatch(w.Body.String(), -1) {
		paths[page.StaticPrefix+match[1]] = "font/woff2"
	}
	return paths
}
func strong(tag string) bool {
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

// R-U51T-R2F6 R-U69Q-4U5V R-DG1P-ZMC3 R-DH9M-DE2S R-DIHI-R5TH R-DJPF-4XK6 R-U8PI-WDN9
func TestSharedFiles(t *testing.T) {
	f := setup(t)
	h := fullHandler(f)
	paths := staticPaths(t, h)
	first := map[string]*httptest.ResponseRecorder{}
	other := fullHandler(f)
	for path, typ := range paths {
		g := invoke(h, req("GET", path))
		equal(t, g.Code, 200)
		if g.Body.Len() == 0 {
			t.Fatalf("empty %s", path)
		}
		equal(t, g.Header().Values("Content-Type"), []string{typ})
		tag := g.Header().Get("ETag")
		if !strong(tag) {
			t.Fatalf("weak or invalid entity tag %q", tag)
		}
		equal(t, g.Header().Values("ETag"), []string{tag})
		cache := "no-cache"
		if typ == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		equal(t, g.Header().Values("Cache-Control"), []string{cache})
		head := invoke(h, req("HEAD", path))
		equal(t, head.Code, 200)
		equal(t, head.Body.Len(), 0)
		for _, k := range []string{"Content-Type", "ETag", "Cache-Control"} {
			equal(t, head.Header().Values(k), g.Header().Values(k))
		}
		for _, next := range []http.Handler{h, other} {
			w := invoke(next, req("GET", path))
			equal(t, w.Body.Bytes(), g.Body.Bytes())
			equal(t, w.Header().Get("ETag"), tag)
		}
		first[path] = g
	}
	for a, g := range first {
		for b, w := range first {
			if a != b && !bytes.Equal(g.Body.Bytes(), w.Body.Bytes()) && g.Header().Get("ETag") == w.Header().Get("ETag") {
				t.Fatalf("tag collision %s %s", a, b)
			}
		}
	}
	// The handler must remain independent of working directory and database availability.
	t.Chdir(t.TempDir())
	if e := f.d.Close(); e != nil {
		t.Fatal(e)
	}
	for path, g := range first {
		w := invoke(h, req("GET", path))
		equal(t, w.Code, 200)
		equal(t, w.Body.Bytes(), g.Body.Bytes())
		equal(t, w.Header().Values("Content-Type"), g.Header().Values("Content-Type"))
	}
}

// R-DKXB-IPAV R-U7HM-ILWK
func TestSharedConditional(t *testing.T) {
	f := setup(t)
	h := fullHandler(f)
	for path, typ := range staticPaths(t, h) {
		g := invoke(h, req("GET", path))
		tag := g.Header().Get("ETag")
		for _, method := range []string{"GET", "HEAD"} {
			for _, ims := range []string{"", "Thu, 01 Jan 1970 00:00:00 GMT", "Thu, 01 Jan 2099 00:00:00 GMT", "invalid"} {
				for _, match := range []string{"*", tag, "W/" + tag, " , \"other-tag\",\tW/" + tag + " , "} {
					r := req(method, path)
					r.Header.Set("If-None-Match", match)
					if ims != "" {
						r.Header.Set("If-Modified-Since", ims)
					}
					w := invoke(h, r)
					equal(t, w.Code, 304)
					equal(t, w.Body.Len(), 0)
					equal(t, w.Header().Values("ETag"), []string{tag})
					equal(t, w.Header().Values("Cache-Control"), g.Header().Values("Cache-Control"))
				}
				for _, match := range []string{"\"unmatched\"", "W/\"unmatched\"", " , \"first-unmatched\", W/\"second-unmatched\", "} {
					r := req(method, path)
					r.Header.Set("If-None-Match", match)
					if ims != "" {
						r.Header.Set("If-Modified-Since", ims)
					}
					w := invoke(h, r)
					equal(t, w.Code, 200)
					equal(t, w.Header().Values("ETag"), []string{tag})
					equal(t, w.Header().Values("Content-Type"), []string{typ})
					equal(t, w.Header().Values("Cache-Control"), g.Header().Values("Cache-Control"))
					b := g.Body.String()
					if method == "HEAD" {
						b = ""
					}
					equal(t, w.Body.String(), b)
				}
			}
		}
	}
}

// R-DOL0-O0IY R-U9XF-A5DY R-DR0T-FK0C
func TestSharedRefusals(t *testing.T) {
	f := setup(t)
	h := fullHandler(f)
	paths := staticPaths(t, h)
	invalid := []string{page.StaticPrefix, page.StaticPrefix + "banner.html", page.StaticPrefix + "launcher.js", page.StaticPrefix + "nope.css", page.StaticPrefix + "theme.css/", page.StaticPrefix + "theme.css/x", page.StaticPrefix + "THEME.CSS"}
	hash := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for path, typ := range paths {
		if typ == "font/woff2" && hash.MatchString(path) {
			invalid = append(invalid, hash.ReplaceAllString(path, ".woff2"))
		}
		for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, etag := range []string{"", "*"} {
				r := req(m, path)
				if etag != "" {
					r.Header.Set("If-None-Match", etag)
				}
				w := invoke(h, r)
				equal(t, w.Code, 405)
				equal(t, w.Header().Values("Allow"), []string{"GET, HEAD"})
			}
		}
	}
	for _, path := range invalid {
		for _, m := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, etag := range []string{"", "*"} {
				r := req(m, path)
				if etag != "" {
					r.Header.Set("If-None-Match", etag)
				}
				w := invoke(h, r)
				equal(t, w.Code, 404)
				if w.Body.String() == missingBody(t) {
					t.Fatal("static path answered with prompt template")
				}
			}
		}
	}
}

// R-YTXD-26RF: malformed file paths go to Files, proven by its plain catalog refusal.
func TestRunFileRouteDefinition(t *testing.T) {
	f := setup(t)
	h := fullHandler(f)
	f.d.SetFailing(true)
	for _, path := range []string{"/n/runs/r/stdout", "/n/runs/r/transcript.jsonl", "/n/runs/r/work/", "/n/runs/r/out/a.txt", "/n/runs/r//", "/n/runs/r/stdout/", "/n/runs/r/./stdout"} {
		w := invoke(h, req("GET", path))
		equal(t, w.Code, 503)
		equal(t, w.Header().Get("Content-Type"), "text/plain; charset=utf-8")
		equal(t, w.Body.String(), (store.Unreachable + "\n"))
	}
	for _, path := range []string{"/n/runs/r/", "/n/runs/r", "/n/runs/", "/n/other/r/stdout"} {
		w := invoke(h, req("GET", path))
		equal(t, w.Code, 503)
		equal(t, w.Header().Get("Content-Type"), "text/html; charset=utf-8")
		var expected bytes.Buffer
		set, e := page.Templates().ParseFS(prompts.Assets(), "*.html")
		if e != nil {
			t.Fatal(e)
		}
		if e := set.ExecuteTemplate(&expected, "unavailable", pages.NoticeData{Banner: page.Banner{Service: pages.ServiceName, Release: "fixture-release", Commit: "fixture-commit"}}); e != nil {
			t.Fatal(e)
		}
		equal(t, w.Body.String(), expected.String())
	}
	for _, path := range []string{"/_appkit/runs/r/stdout", "/_appkit/n/runs/r/stdout"} {
		w := invoke(h, req("GET", path))
		equal(t, w.Code, 404)
		if strings.Contains(w.Body.String(), (store.Unreachable + "\n")) {
			t.Fatal("shared route reached catalog")
		}
	}
}
