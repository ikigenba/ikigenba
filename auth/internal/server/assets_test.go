package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func sharedAssetFiles(t *testing.T, s *Server) map[string]string {
	t.Helper()
	files := map[string]string{
		"theme.css":          "text/css; charset=utf-8",
		"launcher.js":        "text/javascript; charset=utf-8",
		"feedback.js":        "text/javascript; charset=utf-8",
		"favicon.svg":        "image/svg+xml",
		"OFL.txt":            "text/plain; charset=utf-8",
		"TABLER-LICENSE.txt": "text/plain; charset=utf-8",
	}
	css := assetRequest(s, "GET", page.StaticPrefix+"theme.css", nil)
	if css.Code != http.StatusOK {
		t.Fatalf("theme CSS status = %d", css.Code)
	}
	for _, match := range regexp.MustCompile(`url\("([^"]*\.woff2)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		files[match[1]] = "font/woff2"
	}
	return files
}

func TestSharedAssetFontNames(t *testing.T) {
	// R-SD2F-ARHO: CSS names exactly three hashed fonts, with upright Inter preloaded.
	s := newTestServer(t, Config{})
	fonts := map[string]bool{}
	pattern := regexp.MustCompile(`^(InterVariable|InterVariable-Italic|JetBrainsMono)\.[0-9a-f]{16}\.woff2$`)
	for name, contentType := range sharedAssetFiles(t, s) {
		if contentType != "font/woff2" {
			continue
		}
		match := pattern.FindStringSubmatch(name)
		if len(match) != 2 || fonts[match[1]] {
			t.Fatalf("unexpected or duplicate font name: %q", name)
		}
		fonts[match[1]] = true
		if match[1] == "InterVariable" && page.PreloadURL() != page.StaticPrefix+name {
			t.Fatalf("preload URL = %q, upright font path = %q", page.PreloadURL(), page.StaticPrefix+name)
		}
	}
	if len(fonts) != 3 {
		t.Fatalf("font names = %v, want all three families", fonts)
	}
}

func sharedAssetCache(name string) string {
	if strings.HasSuffix(name, ".woff2") {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

func assetRequest(h http.Handler, method, target string, headers http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	r.Header = headers.Clone()
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func assertAssetResponseEqual(t *testing.T, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
		t.Fatalf("response %d %v body length %d, want %d %v body length %d", got.Code, got.Header(), got.Body.Len(), want.Code, want.Header(), want.Body.Len())
	}
}

func assertAssetHeader(t *testing.T, w *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if !reflect.DeepEqual(w.Header().Values(name), []string{want}) {
		t.Fatalf("%s=%v, want one %q", name, w.Header().Values(name), want)
	}
}

func assertStrongAssetTag(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	tags := w.Header().Values("ETag")
	if len(tags) != 1 {
		t.Fatalf("ETag=%v", tags)
	}
	tag := tags[0]
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		t.Fatalf("not strong entity tag: %q", tag)
	}
	for i := 1; i < len(tag)-1; i++ {
		c := tag[i]
		if c != 0x21 && (c < 0x23 || c > 0x7e) && c < 0x80 {
			t.Fatalf("invalid entity-tag byte in %q", tag)
		}
	}
	return tag
}

func TestSharedAssetRepresentations(t *testing.T) {
	// R-SFI8-2AZ2: the nine shared files are nonempty and have their specified types.
	// R-SGQ4-G2PR: 200 and 304 carry one strong tag and file-specific cache policy.
	// R-1GDL-HS81: the body and tag are stable within and across servers.
	// R-1HLH-VJYQ: HEAD has the GET representation headers and no body.
	s, other := newTestServer(t, Config{}), newTestServer(t, Config{})
	for name, contentType := range sharedAssetFiles(t, s) {
		t.Run(name, func(t *testing.T) {
			target := page.StaticPrefix + name
			get := assetRequest(s, "GET", target, nil)
			if get.Code != 200 || get.Body.Len() == 0 {
				t.Fatalf("GET = %d body length %d", get.Code, get.Body.Len())
			}
			assertAssetHeader(t, get, "Content-Type", contentType)
			tag := assertStrongAssetTag(t, get)
			assertAssetHeader(t, get, "Cache-Control", sharedAssetCache(name))
			for _, server := range []*Server{s, other} {
				again := assetRequest(server, "GET", target, nil)
				if !bytes.Equal(again.Body.Bytes(), get.Body.Bytes()) || again.Header().Get("ETag") != tag {
					t.Fatal("representation changed")
				}
			}
			head := assetRequest(s, "HEAD", target, nil)
			if head.Code != 200 || head.Body.Len() != 0 {
				t.Fatalf("HEAD = %d body length %d", head.Code, head.Body.Len())
			}
			for _, header := range []string{"Content-Type", "ETag", "Cache-Control"} {
				assertAssetHeader(t, head, header, get.Header().Get(header))
			}
			cached := assetRequest(s, "GET", target, http.Header{"If-None-Match": {tag}})
			if cached.Code != 304 {
				t.Fatalf("conditional status %d", cached.Code)
			}
			assertStrongAssetTag(t, cached)
			assertAssetHeader(t, cached, "Cache-Control", sharedAssetCache(name))
		})
	}
}

func TestSharedAssetConditionalRequests(t *testing.T) {
	// R-SHY0-TUGG: matching well-formed lists or * yield empty 304 for GET and HEAD.
	// R-SJ5X-7M75: nonmatching well-formed lists yield the GET representation.
	s := newTestServer(t, Config{})
	for name, contentType := range sharedAssetFiles(t, s) {
		target := page.StaticPrefix + name
		get := assetRequest(s, "GET", target, nil)
		tag := get.Header().Get("ETag")
		for _, modified := range []string{"", "Thu, 01 Jan 1970 00:00:00 GMT", "Fri, 31 Dec 9999 23:59:59 GMT", "invalid"} {
			for _, match := range []string{"*", tag, "W/" + tag, " , \t\"other\", W/" + tag + "\t , ,", "\"other\", " + tag} {
				for _, method := range []string{"GET", "HEAD"} {
					w := assetRequest(s, method, target, http.Header{"If-None-Match": {match}, "If-Modified-Since": {modified}})
					if w.Code != 304 || w.Body.Len() != 0 {
						t.Fatalf("%s %s match %q modified %q = %d body %q", method, name, match, modified, w.Code, w.Body.String())
					}
					assertAssetHeader(t, w, "ETag", tag)
					assertStrongAssetTag(t, w)
					assertAssetHeader(t, w, "Cache-Control", sharedAssetCache(name))
				}
			}
			for _, match := range []string{"\"other\"", "W/\"other\"", " ,\t\"other\" , W/\"different\", ,"} {
				w := assetRequest(s, "GET", target, http.Header{"If-None-Match": {match}, "If-Modified-Since": {modified}})
				if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), get.Body.Bytes()) {
					t.Fatalf("nonmatch %q = %d different body", match, w.Code)
				}
				assertAssetHeader(t, w, "Content-Type", contentType)
				assertAssetHeader(t, w, "ETag", tag)
				assertStrongAssetTag(t, w)
				assertAssetHeader(t, w, "Cache-Control", sharedAssetCache(name))
			}
		}
	}
}

func TestSharedAssetPathsMethodsAndDelegation(t *testing.T) {
	// R-SBUI-WZQZ: decoded byte-exact prefix, fixed names and CSS font names define the paths.
	// R-55ES-LBDV: every appkit path delegates unchanged, including unspecified header behavior.
	// R-2U35-R9SL: unsupported methods on files yield 405 and Allow.
	// R-SKDT-LDXU: non-file appkit paths yield 404 for any method or condition.
	s := newTestServer(t, Config{})
	static := page.Static()
	if page.StaticPrefix != "/_appkit/" {
		t.Fatalf("shared asset prefix = %q, want /_appkit/", page.StaticPrefix)
	}
	missing := []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/InterVariable.woff2", "/_appkit/InterVariable-Italic.woff2", "/_appkit/JetBrainsMono.woff2", "/_appkit/missing", "/_appkit/THEME.CSS", "/_appkit/theme.cssX", "/_appkit/theme.css/child", "/_appkit//theme.css", "/_appkit/./theme.css", "/_appkit/../theme.css", "/_appkit/%2ftheme.css"}
	for name := range sharedAssetFiles(t, s) {
		missing = append(missing, page.StaticPrefix+strings.ToUpper(name), page.StaticPrefix+name+"extra", page.StaticPrefix+name+"/child")
	}
	targets := append([]string{}, missing...)
	for name := range sharedAssetFiles(t, s) {
		targets = append(targets, page.StaticPrefix+name, fmt.Sprintf("%s%%%02X%s", page.StaticPrefix, name[0], name[1:]))
	}
	targets = append(targets, "/_appkit/%74heme.css", "/_appkit/%66eedback.js", "/_appkit/%66avicon.svg")
	conditions := []http.Header{
		nil,
		{"If-None-Match": {"*"}},
		{"If-None-Match": {"\"other\""}},
		{"If-None-Match": {"malformed"}},
		{"If-None-Match": {"malformed", "*"}},
		{"Range": {"bytes=0-1"}},
		{"Range": {"bytes=999999999-"}},
		{"If-Match": {"\"other\""}},
		{"If-Unmodified-Since": {"Thu, 01 Jan 1970 00:00:00 GMT"}},
		{"If-Range": {"\"other\""}, "Range": {"bytes=0-1"}},
		{"If-Modified-Since": {"Fri, 31 Dec 9999 23:59:59 GMT"}},
		{"Cookie": {"arbitrary=value"}, "Authorization": {"Bearer arbitrary"}, "X-Test": {"one", "two"}},
		{"If-None-Match": {"malformed", "*"}, "Range": {"bytes=0-1"}, "If-Match": {"*"}, "If-Unmodified-Since": {"invalid"}, "If-Range": {"invalid"}, "If-Modified-Since": {"invalid"}, "X-Test": {"one", "two"}},
	}
	for _, target := range targets {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CONNECT", "CUSTOM"} {
			for _, header := range conditions {
				w := assetRequest(s, method, target, header)
				assertAssetResponseEqual(t, w, assetRequest(static, method, target, header))
				isMissing := false
				for _, path := range missing {
					if target == path {
						isMissing = true
					}
				}
				if isMissing {
					if w.Code != 404 {
						t.Fatalf("%s %s = %d %v", method, target, w.Code, w.Header())
					}
				} else if method != "GET" && method != "HEAD" {
					if w.Code != 405 {
						t.Fatalf("%s %s = %d %v", method, target, w.Code, w.Header())
					}
					assertAssetHeader(t, w, "Allow", "GET, HEAD")
				}
			}
		}
	}
	// URL.Path is decoded before delegation.
	assertAssetResponseEqual(t, assetRequest(s, "GET", "/_appkit/%74heme.css", nil), assetRequest(s, "GET", "/_appkit/theme.css", nil))
	assertAssetResponseEqual(t, assetRequest(s, "GET", "/_appkit/%66eedback.js", nil), assetRequest(s, "GET", "/_appkit/feedback.js", nil))
	assertAssetResponseEqual(t, assetRequest(s, "GET", "/_appkit/%66avicon.svg", nil), assetRequest(s, "GET", "/_appkit/favicon.svg", nil))
}

func TestSharedAssetCredentialAndStoreIndependence(t *testing.T) {
	// R-4Y9U-JYP8: live and invalid credentials leave the entire response unchanged.
	// R-4ZHQ-XQFX: no appkit response sets a cookie.
	// R-50PN-BI6M: closing the store's database handle leaves the entire response unchanged.
	st := openServerStore(t, filepath.Join(t.TempDir(), "auth.db"), &tokenTestRand{}, fixedNow)
	user, _, err := st.UpsertUserOnLogin("issuer", "asset-user", "asset@example.test", fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := st.CreateToken(user.ID, "asset-token", store.ExpiryNever, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, Config{Store: st, Now: fixedNow})
	targets := []string{"/_appkit/", "/_appkit/missing", "/_appkit//theme.css"}
	for name := range sharedAssetFiles(t, s) {
		targets = append(targets, page.StaticPrefix+name)
	}
	type testCase struct {
		method, target string
		header         http.Header
		response       *httptest.ResponseRecorder
	}
	var cases []testCase
	for _, target := range targets {
		tag := assetRequest(s, "GET", target, nil).Header().Get("ETag")
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, condition := range []string{"", "*", tag, "\"other\""} {
				header := http.Header{}
				if condition != "" {
					header.Set("If-None-Match", condition)
				}
				plain := assetRequest(s, method, target, header)
				if len(plain.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("Set-Cookie on appkit response")
				}
				cases = append(cases, testCase{method, target, header, plain})
				for _, credentials := range []http.Header{{"Cookie": {SessionCookieName + "=" + session.ID}}, {"Authorization": {"Bearer " + secret}}, {"Cookie": {SessionCookieName + "=" + session.ID}, "Authorization": {"Bearer " + secret}}, {"Cookie": {SessionCookieName + "=invalid; other=value"}, "Authorization": {"arbitrary invalid credentials"}}} {
					combined := header.Clone()
					for key, values := range credentials {
						combined[key] = values
					}
					got := assetRequest(s, method, target, combined)
					assertAssetResponseEqual(t, got, plain)
					if len(got.Header().Values("Set-Cookie")) != 0 {
						t.Fatal("Set-Cookie on credentialed response")
					}
					cases = append(cases, testCase{method, target, combined, got})
				}
			}
		}
	}
	if err := serverStoreDB(t, st).Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		assertAssetResponseEqual(t, assetRequest(s, tc.method, tc.target, tc.header), tc.response)
	}
}
