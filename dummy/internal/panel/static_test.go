package panel_test

import (
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
)

type sharedFile struct{ name, contentType string }

// R-RC34-YCCP
func sharedFiles(t *testing.T, h http.Handler) []sharedFile {
	t.Helper()
	if page.StaticPrefix != "/_appkit/" {
		t.Fatalf("static prefix = %q", page.StaticPrefix)
	}
	files := []sharedFile{
		{"theme.css", "text/css; charset=utf-8"},
		{"feedback.js", "text/javascript; charset=utf-8"},
		{"favicon.svg", "image/svg+xml"},
		{"OFL.txt", "text/plain; charset=utf-8"},
		{"TABLER-LICENSE.txt", "text/plain; charset=utf-8"},
	}
	css := pageTestResponse(h, pageTestRequest("GET", page.StaticPrefix+"theme.css"))
	if css.Code != http.StatusOK {
		t.Fatalf("stylesheet discovery: status %d", css.Code)
	}
	seen := map[string]bool{}
	addFont := func(path string) {
		if !seen[path] {
			seen[path] = true
			files = append(files, sharedFile{strings.TrimPrefix(path, page.StaticPrefix), "font/woff2"})
		}
	}
	addFont(page.PreloadURL())
	for _, match := range regexp.MustCompile(`url\("([^"]*)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		name := match[1]
		if strings.HasSuffix(name, ".woff2") && !strings.ContainsAny(name, "/\\:\"?#%") {
			addFont(page.StaticPrefix + name)
		}
	}
	return files
}

func staticTestHandler(t *testing.T) http.Handler {
	return coreHandler(t, panelTestStore(t), func(page.User) page.Banner { return page.Banner{} }, io.Discard)
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

// R-19LL-IENZ
func TestSharedStaticDelegation(t *testing.T) {
	h := staticTestHandler(t)
	paths := []string{"/_appkit/", "/_appkit/missing", "/_appkit/../widgets", "/_appkit/%2Ftheme.css", "/_appkit/%74heme.css"}
	for _, file := range sharedFiles(t, h) {
		paths = append(paths, "/_appkit/"+file.name, "/_appkit/"+file.name+"/extra", "/_appkit/"+strings.ToUpper(file.name))
	}
	static := page.Static()
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

// R-RDB1-C43E R-Y960-B9D1 R-YADW-P13Q R-M9E7-IAKV
func TestSharedStaticContentAndCache(t *testing.T) {
	first, second := staticTestHandler(t), staticTestHandler(t)
	for _, file := range sharedFiles(t, first) {
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
		cache := "no-cache"
		if file.contentType == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, validator := range []string{"", "*"} {
				r := pageTestRequest(method, path)
				if validator != "" {
					r.Header.Set("If-None-Match", validator)
				}
				got := pageTestResponse(first, r)
				tags := got.Header().Values("ETag")
				if len(tags) != 1 || !staticStrongTag(tags[0]) || !reflect.DeepEqual(got.Header().Values("Cache-Control"), []string{cache}) {
					t.Fatalf("%s cache headers: %v", path, got.Header())
				}
			}
		}
	}
}

// R-YBLT-2SUF R-YCTP-GKL4
func TestSharedStaticRevalidation(t *testing.T) {
	h := staticTestHandler(t)
	for _, file := range sharedFiles(t, h) {
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
		for _, condition := range []string{`"stale"`, `W/"stale", "other"`, ` , "stale" , , W/"other" ,`, `"*"`} {
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

// R-MD1W-NLSY
func TestSharedStaticRefusedMethods(t *testing.T) {
	h := staticTestHandler(t)
	for _, file := range sharedFiles(t, h) {
		for _, method := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `"stale"`, "malformed"} {
				r := pageTestRequest(method, "/_appkit/"+file.name)
				r.Header.Set("If-None-Match", validator)
				got := pageTestResponse(h, r)
				if got.Code != 405 || got.Header().Get("Allow") != "GET, HEAD" {
					t.Fatalf("%s %s: %d %v", method, r.URL.Path, got.Code, got.Header())
				}
			}
		}
	}
}

// R-ME9T-1DJN
func TestSharedStaticMissingPaths(t *testing.T) {
	h := staticTestHandler(t)
	paths := []string{"/_appkit/", "/_appkit/missing", "/_appkit/banner.html"}
	for _, file := range sharedFiles(t, h) {
		paths = append(paths, "/_appkit/"+file.name+"/extra", "/_appkit/"+file.name+"x", "/_appkit/"+file.name+"%2Fextra", "/_appkit/"+strings.ToUpper(file.name))
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, validator := range []string{"", "*", `"stale"`, "malformed"} {
				r := pageTestRequest(method, path)
				r.Header.Set("If-None-Match", validator)
				got := pageTestResponse(h, r)
				if got.Code != 404 {
					t.Fatalf("%s %s: %d %v", method, path, got.Code, got.Header())
				}
			}
		}
	}
}

// R-REIX-PVU3
func TestSharedStaticHead(t *testing.T) {
	h := staticTestHandler(t)
	for _, file := range sharedFiles(t, h) {
		path := "/_appkit/" + file.name
		get := pageTestResponse(h, pageTestRequest(http.MethodGet, path))
		head := pageTestResponse(h, pageTestRequest(http.MethodHead, path))
		if head.Code != http.StatusOK || head.Body.Len() != 0 || !reflect.DeepEqual(head.Header().Values("Content-Type"), []string{file.contentType}) || head.Header().Get("ETag") != get.Header().Get("ETag") {
			t.Fatalf("HEAD %s: %d %v body=%q", path, head.Code, head.Header(), head.Body.String())
		}
	}
}

// R-YE1L-UCBT
func TestSharedStaticPlainFontAliasesMissing(t *testing.T) {
	h := staticTestHandler(t)
	hashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for _, file := range sharedFiles(t, h) {
		if file.contentType != "font/woff2" || !hashed.MatchString(file.name) {
			continue
		}
		path := page.StaticPrefix + hashed.ReplaceAllString(file.name, ".woff2")
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			got := pageTestResponse(h, pageTestRequest(method, path))
			if got.Code != http.StatusNotFound {
				t.Fatalf("%s %s: status %d", method, path, got.Code)
			}
		}
	}
}
