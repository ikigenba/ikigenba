package web_test

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
)

// R-MML2-CMH2 R-8GSY-0FTF R-8J8Q-RZAT R-DMWP-R6AY R-F04K-TWPO
// R-8MWF-XAIW R-MP0V-45YG R-MQ8R-HXP5 R-F506-CZOG R-MRGN-VPFU
func TestSharedFiles(t *testing.T) {
	f := fresh(t)
	other := fresh(t)
	emptyDirectory := t.TempDir()
	t.Chdir(f.root)
	files := sharedFiles(t, f)
	identities := []map[string]string{nil, {"X-User-Id": ""}, {"X-User-Id": "user"}}
	tags := map[string]string{}
	for name, contentType := range files {
		path := page.StaticPrefix + name
		cache := "no-cache"
		if contentType == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		got := f.get(t, "GET", path, "sites", "", nil)
		tag := got.Header().Get("ETag")
		if got.Code != 200 || got.Body.Len() == 0 || len(got.Header().Values("Content-Type")) != 1 || got.Header().Get("Content-Type") != contentType || len(got.Header().Values("ETag")) != 1 || len(got.Header().Values("Cache-Control")) != 1 || got.Header().Get("Cache-Control") != cache || len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
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
		for _, directory := range []string{f.root, emptyDirectory} {
			t.Chdir(directory)
			for _, h := range []*fixture{f, other} {
				for _, identity := range identities {
					repeat := h.get(t, "GET", path, "Sites:80", "", identity)
					if repeat.Code != 200 || repeat.Body.String() != got.Body.String() || len(repeat.Header().Values("Content-Type")) != 1 || repeat.Header().Get("Content-Type") != contentType || repeat.Header().Get("ETag") != tag {
						t.Fatalf("shared file %s varies with directory, handler or identity: %d %v", name, repeat.Code, repeat.Header())
					}
					sharedCacheHeaders(t, repeat.Header(), tag, cache)
				}
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, match := range []string{"*", tag, "W/" + tag, " , \"other\",\tW/" + tag + " , "} {
				for _, modified := range []string{"", "bad", "Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2099 07:28:00 GMT"} {
					headers := map[string]string{"If-None-Match": match}
					if modified != "" {
						headers["If-Modified-Since"] = modified
					}
					r := f.get(t, method, path, "sites", "", headers)
					if r.Code != 304 || r.Body.Len() != 0 || r.Header().Get("ETag") != tag {
						t.Fatalf("match %s %s modified %q: %d", name, match, modified, r.Code)
					}
					sharedCacheHeaders(t, r.Header(), tag, cache)
				}
			}
			for _, identity := range identities {
				for _, modified := range []string{"", "bad", "Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2099 07:28:00 GMT"} {
					headers := map[string]string{"If-None-Match": " , W/\"different-" + tag[1:] + ", \"other-" + tag[1:] + " , "}
					for key, value := range identity {
						headers[key] = value
					}
					if modified != "" {
						headers["If-Modified-Since"] = modified
					}
					r := f.get(t, method, path, "sites", "", headers)
					if r.Code != 200 || r.Header().Get("ETag") != tag || len(r.Header().Values("Content-Type")) != 1 || r.Header().Get("Content-Type") != contentType || (method == "GET" && r.Body.String() != got.Body.String()) || (method == "HEAD" && r.Body.Len() != 0) {
						t.Fatalf("nonmatch %s %s with If-Modified-Since %q: %d %v", name, method, modified, r.Code, r.Header())
					}
					sharedCacheHeaders(t, r.Header(), tag, cache)
				}
			}
		}
		for _, identity := range identities {
			head := f.get(t, "HEAD", path, "sites", "", identity)
			if head.Code != 200 || head.Body.Len() != 0 {
				t.Fatalf("HEAD %s: %d", name, head.Code)
			}
			for _, key := range []string{"Content-Type", "ETag", "Cache-Control"} {
				if len(head.Header().Values(key)) != 1 || head.Header().Get(key) != got.Header().Get(key) {
					t.Fatalf("HEAD %s header %s: %v", name, key, head.Header())
				}
			}
		}
		for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS"} {
			r := f.get(t, method, path, "sites", "user", map[string]string{"If-None-Match": "*"})
			if r.Code != http.StatusMethodNotAllowed || len(r.Header().Values("Allow")) != 1 || r.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("method", method, r.Code)
			}
		}
	}
	unknownPaths := []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/launcher.js", "/_appkit/nope.css"}
	for name := range files {
		path := page.StaticPrefix + name
		unknownPaths = append(unknownPaths, path+"/", path+"/x", page.StaticPrefix+strings.ToUpper(name))
	}
	notfound := notfoundBody(t, f)
	for _, path := range unknownPaths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `W/"stale"`} {
				for _, identity := range identities {
					headers := map[string]string{"If-None-Match": validator}
					for key, value := range identity {
						headers[key] = value
					}
					r := f.get(t, method, path, "sites", "", headers)
					if r.Code != 404 || r.Body.String() == notfound {
						t.Fatalf("unknown static %s %s %d", method, path, r.Code)
					}
				}
			}
		}
	}
}

func sharedCacheHeaders(t *testing.T, headers http.Header, tag, cache string) {
	t.Helper()
	if len(headers.Values("ETag")) != 1 || headers.Get("ETag") != tag || len(headers.Values("Cache-Control")) != 1 || headers.Get("Cache-Control") != cache {
		t.Fatalf("shared file cache headers: %v", headers)
	}
}

// R-MLD5-YUQD
func sharedFiles(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	files := map[string]string{
		"theme.css":   "text/css; charset=utf-8",
		"feedback.js": "text/javascript; charset=utf-8", "favicon.svg": "image/svg+xml",
		"OFL.txt": "text/plain; charset=utf-8", "TABLER-LICENSE.txt": "text/plain; charset=utf-8",
	}
	css := f.get(t, "GET", page.StaticPrefix+"theme.css", "sites", "", nil)
	if css.Code != http.StatusOK {
		t.Fatalf("stylesheet discovery: %d", css.Code)
	}
	files[strings.TrimPrefix(page.PreloadURL(), page.StaticPrefix)] = "font/woff2"
	for _, match := range regexp.MustCompile(`url\("([^"]*)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		name := match[1]
		if strings.HasSuffix(name, ".woff2") && !strings.ContainsAny(name, `/\:"?#%`) {
			files[name] = "font/woff2"
		}
	}
	return files
}

// R-8WNM-ZGGG
func TestSharedFilesPlainFontAliasesMissing(t *testing.T) {
	f := fresh(t)
	hashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for name, contentType := range sharedFiles(t, f) {
		if contentType != "font/woff2" || !hashed.MatchString(name) {
			continue
		}
		path := page.StaticPrefix + hashed.ReplaceAllString(name, ".woff2")
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `W/"stale"`} {
				r := f.get(t, method, path, "sites", "", map[string]string{"If-None-Match": validator})
				if r.Code != http.StatusNotFound {
					t.Fatalf("%s %s: %d", method, path, r.Code)
				}
			}
		}
	}
}

func notfoundBody(t *testing.T, f *fixture) string {
	t.Helper()
	templates, err := page.Templates().ParseFS(sites.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, "notfound", pages.NoticeData{Banner: f.cfg.Banner(page.User{})}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
