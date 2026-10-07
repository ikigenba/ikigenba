package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

var metadataPaths = []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource/mcp/dummy,notes", "/.well-known/oauth-protected-resource/anything/else", "/.well-known/oauth-protected-resource/mcp/a,,b", "/.well-known/oauth-protected-resource/x/../"}

func checkMetadata(t *testing.T, body string, resource, auth string) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(body))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		t.Fatalf("object: %v %v", token, err)
	}
	got := make(map[string]any)
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		name, ok := key.(string)
		if !ok {
			t.Fatal(key)
		}
		if _, duplicate := got[name]; duplicate {
			t.Fatalf("duplicate key %s", name)
		}
		var value any
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		got[name] = value
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('}') {
		t.Fatal(token, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("extra JSON: %v %v", extra, err)
	}
	want := map[string]any{"resource": resource, "authorization_servers": []any{auth}, "bearer_methods_supported": []any{"header"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("document %v want %v", got, want)
	}
}

// R-RTW5-3SJ9 R-RV41-HK9Y R-RWBX-VC0N
func TestMetadataDocumentAndAuthorizationServer(t *testing.T) {
	for _, tc := range []struct{ url, auth string }{
		{"", "https://auth.space.test"}, {"https://auth.sbx.ikigenba.dev", "https://auth.sbx.ikigenba.dev"},
		{"https://auth.sbx.ikigenba.dev/", "https://auth.sbx.ikigenba.dev"}, {"https://auth.sbx.ikigenba.dev/x?y=1#z", "https://auth.sbx.ikigenba.dev"},
		{"http://auth.wip-mcp.localhost:7402", "http://auth.wip-mcp.localhost:7402"}, {"https://user:pass@accounts.test:81/base", "https://accounts.test:81"},
		{"auth.sbx.ikigenba.dev", "https://auth.space.test"}, {"//accounts.test/base", "https://auth.space.test"}, {"https:/base", "https://auth.space.test"}, {"https://%zz", "https://auth.space.test"},
	} {
		entry := service("auth", "", false, false)
		entry["url"] = tc.url
		h := gateway.Handler(pageConfig(t, servicesFile(t, []map[string]any{entry}), func(page.User) page.Banner { t.Fatal("metadata called banner"); return page.Banner{} }))
		for _, path := range metadataPaths {
			for _, identity := range []string{"signed", "absent", "empty"} {
				r := pageRequest("GET", path+"?ignored=yes")
				setPageIdentity(r, identity)
				w := answer(h, r)
				if w.Code != 200 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
					t.Fatal(w.Code, w.Header())
				}
				checkMetadata(t, w.Body.String(), "https://mcp.space.test:8443/mcp", tc.auth)
			}
		}
	}
}

// R-RV41-HK9Y R-RWBX-VC0N
func TestMetadataMissingAndBrokenServicesFallback(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(malformed, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing"), malformed, servicesFile(t, []map[string]any{})} {
		h := gateway.Handler(pageConfig(t, path, basicBanner))
		for _, tc := range []struct{ host, proto, resource, auth string }{
			{"mcp.sbx.ikigenba.dev:443", "https", "https://mcp.sbx.ikigenba.dev:443/mcp", "https://auth.sbx.ikigenba.dev"},
			{"mcp.sbx.ikigenba.dev", "", "https://mcp.sbx.ikigenba.dev/mcp", "https://auth.sbx.ikigenba.dev"},
			{"sbx.ikigenba.dev", "HTTPS", "https://sbx.ikigenba.dev/mcp", "https://auth.sbx.ikigenba.dev"},
			{"mcp.sbx.ikigenba.dev", "http", "http://mcp.sbx.ikigenba.dev/mcp", "http://auth.sbx.ikigenba.dev"},
		} {
			r := pageRequest("GET", metadataPaths[0])
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			w := answer(h, r)
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			checkMetadata(t, w.Body.String(), tc.resource, tc.auth)
		}
	}
}

// R-RXJU-93RC R-RYRQ-MVI1 R-RZZN-0N8Q
func TestMetadataIdentityMethodsAndHEAD(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, path := range metadataPaths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "CUSTOM"} {
			guest := pageRequest(method, path)
			guest.Header.Del("X-User-Id")
			guest.Header.Del("X-User-Email")
			reference := answer(h, guest)
			for _, mode := range []string{"signed", "absent", "empty"} {
				r := pageRequest(method, path)
				setPageIdentity(r, mode)
				w := answer(h, r)
				if w.Code != reference.Code || !reflect.DeepEqual(w.Header(), reference.Header()) || w.Body.String() != reference.Body.String() {
					t.Fatalf("identity affected %s %s", method, path)
				}
			}
			if method == "HEAD" {
				get := guest.Clone(guest.Context())
				get.Method = "GET"
				w := answer(h, get)
				if reference.Code != w.Code || !reflect.DeepEqual(reference.Header(), w.Header()) || reference.Body.Len() != 0 {
					t.Fatal("HEAD differs")
				}
			} else if method != "GET" {
				if reference.Code != http.StatusMethodNotAllowed || !reflect.DeepEqual(reference.Header().Values("Allow"), []string{"GET, HEAD"}) || reference.Body.Len() != 0 {
					t.Fatal(reference.Code, reference.Header(), reference.Body.String())
				}
			}
		}
	}
}

// R-RWBX-VC0N
func TestMetadataReadsAuthAfresh(t *testing.T) {
	path := servicesFile(t, []map[string]any{})
	h := gateway.Handler(pageConfig(t, path, basicBanner))
	for _, auth := range []string{"https://first.test/path", "http://second.test:81/", ""} {
		entry := service("auth", "", true, false)
		entry["url"] = auth
		writeServices(t, path, []map[string]any{entry})
		want := strings.TrimSuffix(auth, "/")
		if auth == "https://first.test/path" {
			want = "https://first.test"
		}
		if auth == "" {
			want = "https://auth.space.test"
		}
		w := answer(h, pageRequest("GET", metadataPaths[0]))
		checkMetadata(t, w.Body.String(), "https://mcp.space.test:8443/mcp", want)
	}
}
