package web_test

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
)

// R-4SK2-TXZD R-F7T9-8UNW R-F915-MMEL R-4UZV-LHGR
func TestSharedFilesAndContentIdentity(t *testing.T) {
	f := setup(t)
	t.Chdir(t.TempDir())
	paths := sharedFiles(t, f.h)
	tags := map[string][]byte{}
	for path, media := range paths {
		got := answer(f.h, request("GET", path, "u", "get"))
		if got.Code != 200 || got.Body.Len() == 0 || len(got.Header().Values("Content-Type")) != 1 || got.Header().Get("Content-Type") != media {
			t.Fatalf("%s: %d %v", path, got.Code, got.Header())
		}
		etag := got.Header().Get("ETag")
		if len(got.Header().Values("ETag")) != 1 || !strongTag(etag) {
			t.Fatalf("tag %q", etag)
		}
		cache := "no-cache"
		if media == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		if len(got.Header().Values("Cache-Control")) != 1 || got.Header().Get("Cache-Control") != cache {
			t.Fatalf("cache %v", got.Header())
		}
		if old, ok := tags[etag]; ok && !bytes.Equal(old, got.Body.Bytes()) {
			t.Fatal("different contents share tag")
		}
		tags[etag] = append([]byte(nil), got.Body.Bytes()...)
		head := answer(f.h, request("HEAD", path, "u", "head"))
		if head.Code != 200 || head.Body.Len() != 0 {
			t.Fatalf("head %d %q", head.Code, head.Body.String())
		}
		for _, key := range []string{"Content-Type", "ETag", "Cache-Control"} {
			if head.Header().Get(key) != got.Header().Get(key) {
				t.Fatalf("head differs for %s", key)
			}
		}
		// Each fresh handler and working directory sees embedded bytes, even on DB failure.
		t.Chdir(t.TempDir())
		f.d.SetFailing(true)
		second := setup(t)
		second.d.SetFailing(true)
		sameAnswer(t, got, answer(second.h, request("GET", path, "u", "again")))
		sameAnswer(t, got, answer(f.h, request("GET", path, "u", "failing")))
		f.d.SetFailing(false)
	}
}
func strongTag(s string) bool {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return false
	}
	for _, b := range []byte(s[1 : len(s)-1]) {
		if b != 0x21 && (b < 0x23 || b > 0x7e) && b < 0x80 {
			return false
		}
	}
	return true
}

// R-FA92-0E5A R-F7T9-8UNW R-4TRZ-7PQ2 R-FDWR-5PDD R-4W7R-Z97G
func TestSharedFileConditionalAndUnknownRequests(t *testing.T) {
	f := setup(t)
	paths := sharedFiles(t, f.h)
	for path := range paths {
		original := answer(f.h, request("GET", path, "u", "seed"))
		tag := original.Header().Get("ETag")
		for _, method := range []string{"GET", "HEAD"} {
			for _, match := range []string{"*", tag, "W/" + tag, "\t\"other\" , , W/" + tag + "\t ,", " , " + tag + ", "} {
				for _, modified := range []string{"", "Tue, 01 Jan 2030 00:00:00 GMT", "invalid"} {
					r := request(method, path, "u", "matched")
					r.Header.Set("If-None-Match", match)
					if modified != "" {
						r.Header.Set("If-Modified-Since", modified)
					}
					got := answer(f.h, r)
					if got.Code != 304 || got.Body.Len() != 0 || got.Header().Get("ETag") != tag || len(got.Header().Values("ETag")) != 1 || len(got.Header().Values("Cache-Control")) != 1 || got.Header().Get("Cache-Control") != original.Header().Get("Cache-Control") {
						t.Fatalf("match %q: %d %v %q", match, got.Code, got.Header(), got.Body.String())
					}
				}
			}
			for _, match := range []string{`"other"`, `W/"other"`, ` , W/"other", "another" , `} {
				for _, modified := range []string{"", "Tue, 01 Jan 2030 00:00:00 GMT", "invalid"} {
					r := request(method, path, "u", "unmatched")
					r.Header.Set("If-None-Match", match)
					if modified != "" {
						r.Header.Set("If-Modified-Since", modified)
					}
					got := answer(f.h, r)
					if got.Code != 200 || got.Header().Get("ETag") != tag || len(got.Header().Values("Content-Type")) != 1 || got.Header().Get("Content-Type") != original.Header().Get("Content-Type") {
						t.Fatalf("nonmatch %q: %d %v", match, got.Code, got.Header())
					}
					want := original.Body.String()
					if method == "HEAD" {
						want = ""
					}
					if got.Body.String() != want {
						t.Fatal("conditional body changed")
					}
				}
			}
		}
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "CUSTOM"} {
			for _, match := range []string{"", "*", tag, "W/" + tag, `"other"`} {
				r := request(method, path, "u", "method")
				if match != "" {
					r.Header.Set("If-None-Match", match)
				}
				got := answer(f.h, r)
				if got.Code != 405 || len(got.Header().Values("Allow")) != 1 || got.Header().Get("Allow") != "GET, HEAD" {
					t.Fatalf("method %s: %d %v", method, got.Code, got.Header())
				}
			}
		}
	}
	unknown := []string{page.StaticPrefix, page.StaticPrefix + "banner.html", page.StaticPrefix + "launcher.js", page.StaticPrefix + "nope.css"}
	unhashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for path := range paths {
		unknown = append(unknown, path+"/", path+"/x", path+"suffix", page.StaticPrefix+strings.ToUpper(strings.TrimPrefix(path, page.StaticPrefix)))
		if unhashed.MatchString(path) {
			unknown = append(unknown, unhashed.ReplaceAllString(path, ".woff2"))
		}
	}
	cssETag := answer(f.h, request("GET", page.StaticPrefix+"theme.css", "u", "unknown_seed")).Header().Get("ETag")
	for _, path := range unknown {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "CUSTOM"} {
			for _, match := range []string{"", "*", `"other"`, cssETag, "W/" + cssETag} {
				r := request(method, path, "u", "unknown")
				if match != "" {
					r.Header.Set("If-None-Match", match)
				}
				got := answer(f.h, r)
				if got.Code != 404 {
					t.Fatalf("unknown %s %s: %d", method, path, got.Code)
				}
			}
		}
	}
}

// R-4RC6-G68O
func sharedFiles(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	stylesheet := answer(h, request("GET", page.StaticPrefix+"theme.css", "u", "css"))
	if stylesheet.Code != 200 {
		t.Fatalf("stylesheet status %d", stylesheet.Code)
	}
	paths := map[string]string{
		page.StaticPrefix + "theme.css":          "text/css; charset=utf-8",
		page.StaticPrefix + "feedback.js":        "text/javascript; charset=utf-8",
		page.StaticPrefix + "favicon.svg":        "image/svg+xml",
		page.StaticPrefix + "OFL.txt":            "text/plain; charset=utf-8",
		page.StaticPrefix + "TABLER-LICENSE.txt": "text/plain; charset=utf-8",
		page.PreloadURL():                        "font/woff2",
	}
	for _, match := range regexp.MustCompile(`url\("([^"/\\:?#%]+\.woff2)"\)`).FindAllStringSubmatch(stylesheet.Body.String(), -1) {
		paths[page.StaticPrefix+match[1]] = "font/woff2"
	}
	return paths
}
