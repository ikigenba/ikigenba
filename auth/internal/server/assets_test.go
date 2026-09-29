package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/auth"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func assetRequest(s *Server, method, target string, headers http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	r.Header = headers.Clone()
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func assetNames(t *testing.T) []string {
	t.Helper()
	entries, err := auth.Assets.ReadDir("assets")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestAssetRepresentations(t *testing.T) {
	// R-23D7-LKIT R-25T0-D407 R-270W-QVQW R-288T-4NHL R-29GP-IF8A
	s := New(Config{Now: fixedNow})
	for _, name := range assetNames(t) {
		t.Run(name, func(t *testing.T) {
			body, err := auth.Assets.ReadFile("assets/" + name)
			if err != nil {
				t.Fatal(err)
			}
			w := assetRequest(s, http.MethodGet, "/assets/"+name, nil)
			if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), body) {
				t.Fatalf("status=%d body differs=%v", w.Code, !bytes.Equal(w.Body.Bytes(), body))
			}
			types := map[string]string{".css": "text/css; charset=utf-8", ".woff2": "font/woff2", ".txt": "text/plain; charset=utf-8"}
			wantType := types[filepath.Ext(name)]
			if wantType == "" {
				wantType = "application/octet-stream"
			}
			if w.Header().Get("Content-Type") != wantType {
				t.Fatalf("Content-Type=%q, want %q", w.Header().Get("Content-Type"), wantType)
			}
			tag := fmt.Sprintf("\"%x\"", sha256.Sum256(body))
			if w.Header().Get("ETag") != tag {
				t.Fatalf("ETag=%q, want %q", w.Header().Get("ETag"), tag)
			}
			if !reflect.DeepEqual(w.Header().Values("Cache-Control"), []string{"no-cache"}) {
				t.Fatalf("Cache-Control=%v", w.Header().Values("Cache-Control"))
			}
		})
	}
	// Percent encoding is decoded before matching the file name.
	assertAssetResponseEqual(t, assetRequest(s, "GET", "/assets/%74heme.css", nil), assetRequest(s, "GET", "/assets/theme.css", nil), false)
}

func TestAssetContentTypeExtensionTable(t *testing.T) {
	// R-270W-QVQW: cover extensions not present in the maintained file set.
	for _, tc := range []struct{ name, want string }{
		{"a.css", "text/css; charset=utf-8"}, {"a.woff2", "font/woff2"}, {"a.txt", "text/plain; charset=utf-8"},
		{"a.CSS", "application/octet-stream"}, {"a", "application/octet-stream"}, {"a.css.bin", "application/octet-stream"}, {".txt", "text/plain; charset=utf-8"},
	} {
		if got := assetContentType(tc.name); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAssetConditionalRequests(t *testing.T) {
	// R-2BWI-9YPO R-2D4E-NQGD R-288T-4NHL R-29GP-IF8A
	s := New(Config{Now: fixedNow})
	for _, name := range assetNames(t) {
		target := "/assets/" + name
		plain := assetRequest(s, "GET", target, nil)
		tag := plain.Header().Get("ETag")
		for _, values := range [][]string{{tag}, {"W/" + tag}, {"*"}, {" \t\"other\" , W/" + tag + " \t"}, {"\"other\"", "  " + tag + "  "}, {"\"other\"", " * "}} {
			w := assetRequest(s, "GET", target, http.Header{"If-None-Match": values})
			if w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != tag || !reflect.DeepEqual(w.Header().Values("Cache-Control"), []string{"no-cache"}) {
				t.Fatalf("%s with %v: %d %v %q", name, values, w.Code, w.Header(), w.Body.String())
			}
		}
		for _, values := range [][]string{{""}, {"\"other\""}, {"w/" + tag}, {strings.Trim(tag, "\"")}, {"\"other\"", " W/\"different\" "}, {"\"other\", ,\"more\""}} {
			assertAssetResponseEqual(t, assetRequest(s, "GET", target, http.Header{"If-None-Match": values}), plain, false)
		}
	}
}

func TestAssetMissingPathsAndMethods(t *testing.T) {
	// R-UDBA-BA8O R-2FK7-F9XR R-2I00-6TF5 R-23D7-LKIT
	s := New(Config{Now: fixedNow})
	for _, target := range []string{"/assets/", "/assets/missing", "/assets/THEME.CSS", "/assets/theme.css/child", "/assets//theme.css", "/assets/./theme.css", "/assets/../theme.css", "/assets/%2ftheme.css", "/assets/index.html", "/assets/app.js", "/assets/style.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS", "CUSTOM"} {
			w := assetRequest(s, method, target, http.Header{"If-None-Match": []string{"*"}})
			if w.Code != 404 || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("ETag") != "" {
				t.Fatalf("%s %s: %d %v", method, target, w.Code, w.Header())
			}
			if method == "HEAD" {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
			} else {
				assertAssetPlainLine(t, w.Body.String())
			}
		}
	}
	for _, name := range assetNames(t) {
		for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS", "CUSTOM"} {
			w := assetRequest(s, method, "/assets/"+name, http.Header{"If-None-Match": []string{"*"}})
			if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("ETag") != "" {
				t.Fatalf("%s %s: %d %v", method, name, w.Code, w.Header())
			}
			assertAssetPlainLine(t, w.Body.String())
		}
	}
}

func TestAssetHeadMirrorsGet(t *testing.T) {
	// R-2ECB-1I72
	s := New(Config{Now: fixedNow})
	targets := []string{"/assets/", "/assets/missing", "/assets//theme.css", "/assets/./theme.css"}
	for _, name := range assetNames(t) {
		targets = append(targets, "/assets/"+name)
	}
	for _, target := range targets {
		tag := assetRequest(s, "GET", target, nil).Header().Get("ETag")
		for _, header := range []http.Header{nil, {"If-None-Match": []string{"*"}}, {"If-None-Match": []string{tag}}, {"If-None-Match": []string{"\"other\""}}} {
			assertAssetResponseEqual(t, assetRequest(s, "HEAD", target, header), assetRequest(s, "GET", target, header), true)
		}
	}
}

func TestAssetCredentialAndStoreIndependence(t *testing.T) {
	// R-2J7W-KL5U R-2LNP-C4N8 R-2MVL-PWDX
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"), &tokenTestRand{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	user, err := st.UpsertUserOnLogin("issuer", "asset-user", "asset@example.test", fixedNow())
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
	s := New(Config{Store: st, Now: fixedNow})
	targets := []string{"/assets/", "/assets/missing", "/assets//theme.css"}
	for _, name := range assetNames(t) {
		targets = append(targets, "/assets/"+name)
	}
	type response struct {
		method, target string
		header         http.Header
		plain          *httptest.ResponseRecorder
	}
	var cases []response
	for _, target := range targets {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, condition := range []string{"", "*", "\"other\""} {
				header := http.Header{}
				if condition != "" {
					header.Set("If-None-Match", condition)
				}
				plain := assetRequest(s, method, target, header)
				if len(plain.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("Set-Cookie on unauthenticated request")
				}
				for _, credentials := range []http.Header{{"Cookie": []string{SessionCookieName + "=" + session.ID}}, {"Authorization": []string{"Bearer " + secret}}, {"Cookie": []string{SessionCookieName + "=invalid; other=value"}, "Authorization": []string{"arbitrary invalid credentials"}}} {
					combined := header.Clone()
					for key, values := range credentials {
						combined[key] = values
					}
					got := assetRequest(s, method, target, combined)
					assertAssetResponseEqual(t, got, plain, false)
					if len(got.Header().Values("Set-Cookie")) != 0 {
						t.Fatal("Set-Cookie on credentialed request")
					}
				}
				cases = append(cases, response{method, target, header, plain})
			}
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		assertAssetResponseEqual(t, assetRequest(s, tc.method, tc.target, tc.header), tc.plain, false)
	}
}

func TestAssetWorkingDirectoryIndependence(t *testing.T) {
	// R-2O3I-3O4M
	s := New(Config{Now: fixedNow})
	targets := []string{"/assets/", "/assets/missing"}
	for _, name := range assetNames(t) {
		targets = append(targets, "/assets/"+name)
	}
	baseline := map[string]*httptest.ResponseRecorder{}
	for _, target := range targets {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			baseline[method+target] = assetRequest(s, method, target, nil)
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("assets", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range assetNames(t) {
		if err := os.WriteFile(filepath.Join("assets", name), []byte("different content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for key, want := range baseline {
		method, target, _ := strings.Cut(key, "/")
		assertAssetResponseEqual(t, assetRequest(s, method, "/"+target, nil), want, false)
	}
	t.Chdir(t.TempDir())
	for key, want := range baseline {
		method, target, _ := strings.Cut(key, "/")
		assertAssetResponseEqual(t, assetRequest(s, method, "/"+target, nil), want, false)
	}
}

func assertAssetResponseEqual(t *testing.T, got, want *httptest.ResponseRecorder, head bool) {
	t.Helper()
	gh, wh := got.Header().Clone(), want.Header().Clone()
	gh.Del("Date")
	wh.Del("Date")
	if got.Code != want.Code || !reflect.DeepEqual(gh, wh) {
		t.Fatalf("response %d %v, want %d %v", got.Code, gh, want.Code, wh)
	}
	if head {
		if got.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	} else if !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
		t.Fatal("response body differs")
	}
}

func assertAssetPlainLine(t *testing.T, body string) {
	t.Helper()
	if !strings.HasSuffix(body, "\n") || len(body) < 2 || strings.ContainsAny(strings.TrimSuffix(body, "\n"), "\r\n") {
		t.Fatalf("not one plain line: %q", body)
	}
}
