package gateway_test

import (
	"bytes"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

// R-S0LT-03HX R-YTQ0-53GB
func TestAppkitPathsDelegateUnchanged(t *testing.T) {
	h := gateway.Handler(pageConfig(t, "", basicBanner))
	for _, path := range []string{
		page.StaticPrefix, "/_appkit/theme.css", "/_appkit/InterVariable.woff2", "/_appkit/InterVariable-Italic.woff2", "/_appkit/JetBrainsMono.woff2", "/_appkit/OFL.txt", "/_appkit/TABLER-LICENSE.txt", "/_appkit/launcher.js", "/_appkit/unknown", "/_appkit/connect.html", "/_appkit/../theme.css", "/_appkit/./theme.css", "/_appkit//theme.css", "/_appkit/%2e%2e/theme.css", "/_appkit/theme.css/",
	} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "OPTIONS", "CUSTOM"} {
			for _, conditional := range []string{"", "Thu, 01 Jan 2099 00:00:00 GMT"} {
				for _, identity := range []string{"signed", "absent", "empty"} {
					r := pageRequest(method, path+"?test=ignored")
					setPageIdentity(r, identity)
					r.Header.Set("If-Modified-Since", conditional)
					r.Header.Set("Range", "bytes=0-9")
					actual := answer(h, r)
					expected := httptest.NewRecorder()
					page.Static().ServeHTTP(expected, r.Clone(r.Context()))
					if actual.Code != expected.Code || !reflect.DeepEqual(actual.Header(), expected.Header()) || !bytes.Equal(actual.Body.Bytes(), expected.Body.Bytes()) {
						t.Fatalf("%s %s: gateway %d %v %q appkit %d %v %q", method, path, actual.Code, actual.Header(), actual.Body.String(), expected.Code, expected.Header(), expected.Body.String())
					}
				}
			}
		}
	}
}
