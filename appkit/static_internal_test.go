package appkit

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticEntityTagsAreQuotedSHA256Digests(t *testing.T) {
	// R-UIW2-AEPG
	for _, name := range []string{"theme.css", "launcher.js", "InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2", "OFL.txt", "TABLER-LICENSE.txt"} {
		content, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf(`"%x"`, sha256.Sum256(content))
		for range 2 {
			handler := Static()
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, headers := range []http.Header{
					{},
					{"If-None-Match": {""}},
					{"If-None-Match": {"*"}},
					{"If-None-Match": {want}},
					{"If-None-Match": {"W/" + want}},
					{"If-None-Match": {`"different"`}},
					{"If-None-Match": {want}, "If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
					{"If-None-Match": {`"different"`}, "If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
					{"Range": {"bytes=0-3"}},
					{"If-Match": {`"different"`}},
					{"If-Unmodified-Since": {"Tue, 01 Jan 1980 00:00:00 GMT"}},
					{"If-Range": {`"different"`}},
					{"If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
				} {
					request := httptest.NewRequest(method, StaticPrefix+name, nil)
					request.Header = headers.Clone()
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					if response.Code == http.StatusOK || response.Code == http.StatusNotModified {
						if got := response.Header().Get("ETag"); got != want {
							t.Errorf("%s %s with %v: status %d, ETag %q; want %q", method, name, headers, response.Code, got, want)
						}
					}
				}
			}
		}
	}
}
