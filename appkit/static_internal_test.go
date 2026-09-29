package appkit

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestStaticEntityTagsDependOnlyOnContent(t *testing.T) {
	// R-EI8V-2AZM
	var contents [][]byte
	for _, name := range []string{"theme.css", "launcher.js", "InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2", "OFL.txt", "TABLER-LICENSE.txt"} {
		data, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents, data)
	}
	contents = append(contents, nil, []byte("same content"), []byte("Same content"), []byte{0, 1, 0xff})
	type observation struct {
		content []byte
		etag    string
	}
	var observations []observation
	for _, content := range contents {
		// Vary paths, filesystems and handlers while keeping the bytes equal.
		for _, name := range []string{"theme.css", "launcher.js", "OFL.txt"} {
			for range 2 {
				handler := staticHandler{files: fstest.MapFS{"assets/" + name: &fstest.MapFile{Data: bytes.Clone(content)}}}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					for _, headers := range []http.Header{
						{},
						{"If-None-Match": {"*"}},
						{"If-None-Match": {`"different"`}},
						{"If-None-Match": {"*"}, "If-Modified-Since": {"Tue, 01 Jan 2030 00:00:00 GMT"}},
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
							observations = append(observations, observation{content, response.Header().Get("ETag")})
						}
					}
				}
			}
		}
	}
	// Include responses from the public factory using the embedded assets.
	for _, name := range []string{"theme.css", "launcher.js", "InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2", "OFL.txt", "TABLER-LICENSE.txt"} {
		content, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			handler := Static()
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(method, StaticPrefix+name, nil))
				observations = append(observations, observation{content, response.Header().Get("ETag")})
			}
		}
	}
	for index, left := range observations {
		for _, right := range observations[:index] {
			if bytes.Equal(left.content, right.content) != (left.etag == right.etag) {
				t.Fatalf("content equality = %t, but ETags are %q and %q", bytes.Equal(left.content, right.content), left.etag, right.etag)
			}
		}
	}
}
