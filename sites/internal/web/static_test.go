package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// R-QGSS-CYWW R-YUJU-KIST R-RH3C-EKZZ R-DMWP-R6AY
// R-W6K9-QDFF R-B6BE-NEUY R-YWZN-C2A7 R-F506-CZOG R-F682-QRF5
func TestSharedFiles(t *testing.T) {
	f := fresh(t)
	other := fresh(t)
	t.Chdir(f.root)
	files := map[string]string{"theme.css": "text/css; charset=utf-8", "launcher.js": "text/javascript; charset=utf-8", "InterVariable.woff2": "font/woff2", "InterVariable-Italic.woff2": "font/woff2", "JetBrainsMono.woff2": "font/woff2", "OFL.txt": "text/plain; charset=utf-8", "TABLER-LICENSE.txt": "text/plain; charset=utf-8"}
	tags := map[string]string{}
	for name, contentType := range files {
		path := "/_appkit/" + name
		got := f.get(t, "GET", path, "sites", "", nil)
		tag := got.Header().Get("ETag")
		if got.Code != 200 || got.Body.Len() == 0 || len(got.Header().Values("Content-Type")) != 1 || got.Header().Get("Content-Type") != contentType || len(got.Header().Values("ETag")) != 1 || len(got.Header().Values("Cache-Control")) != 1 || got.Header().Get("Cache-Control") != "no-cache" || len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
			t.Fatalf("%s: %d %v", name, got.Code, got.Header())
		}
		for _, b := range []byte(tag[1 : len(tag)-1]) {
			if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
				t.Fatalf("invalid tag %q", tag)
			}
		}
		for body, old := range tags {
			if body != got.Body.String() && tag == old {
				t.Fatal("different files have same entity tag")
			}
		}
		tags[got.Body.String()] = tag
		for _, h := range []*fixture{f, other} {
			repeat := h.get(t, "GET", path, "Sites:80", "user", nil)
			if repeat.Body.String() != got.Body.String() || repeat.Header().Get("ETag") != tag {
				t.Fatal("shared file varies")
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, match := range []string{"*", tag, "W/" + tag, " , \"other\",\tW/" + tag + " , "} {
				r := f.get(t, method, path, "sites", "", map[string]string{"If-None-Match": match, "If-Modified-Since": "bad"})
				if r.Code != 304 || r.Body.Len() != 0 || r.Header().Get("ETag") != tag {
					t.Fatalf("match %s %s: %d", name, match, r.Code)
				}
			}
			r := f.get(t, method, path, "sites", "", map[string]string{"If-None-Match": "W/\"different\", \"other\"", "If-Modified-Since": "Wed, 21 Oct 2099 07:28:00 GMT"})
			if r.Code != 200 || r.Header().Get("ETag") != tag || r.Header().Get("Content-Type") != contentType || (method == "GET" && r.Body.String() != got.Body.String()) || (method == "HEAD" && r.Body.Len() != 0) {
				t.Fatalf("nonmatch %s: %d", name, r.Code)
			}
		}
		head := f.get(t, "HEAD", path, "sites", "", nil)
		if head.Code != 200 || head.Body.Len() != 0 {
			t.Fatal("HEAD")
		}
		for _, key := range []string{"Content-Type", "ETag", "Cache-Control"} {
			if head.Header().Get(key) != got.Header().Get(key) {
				t.Fatal("HEAD header", key)
			}
		}
		for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS"} {
			r := f.get(t, method, path, "sites", "user", map[string]string{"If-None-Match": "*"})
			if r.Code != http.StatusMethodNotAllowed || len(r.Header().Values("Allow")) != 1 || r.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("method", method, r.Code)
			}
		}
	}
	for _, path := range []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/nope.css", "/_appkit/theme.css/", "/_appkit/theme.css/x", "/_appkit/THEME.CSS"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := f.get(t, method, path, "sites", "", map[string]string{"If-None-Match": "*"})
			if r.Code != 404 || strings.Contains(r.Body.String(), "There is nothing at this address.") {
				t.Fatalf("unknown static %s %s %d", method, path, r.Code)
			}
		}
	}
}
