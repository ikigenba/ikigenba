package panel_test

import (
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
)

// R-MIBZ-GU1J
func TestPageFaviconLink(t *testing.T) {
	for _, services := range [][]page.Service{nil, {{Name: "other", URL: "/other", Enabled: true}}} {
		for _, request := range pageTestDocuments() {
			h := coreHandler(t, panelTestStore(t), pageTestEchoingBanner(services), io.Discard)
			data := pageTestBannerData(request)
			data.Services = services
			response := pageTestResponse(h, request)
			body := pageTestStrip(frameTestWritten(t, response.Body.String(), data))
			bodies := pageTestTags(body, "body", false)
			if len(bodies) == 0 {
				t.Fatalf("%s %s: document has no body start tag", request.Method, request.URL.Path)
			}
			count := 0
			for _, span := range pageTestTags(body, "link", false) {
				tag := body[span[0]:span[1]]
				if rel, ok := pageTestAttribute(tag, "rel"); !ok || rel != "icon" {
					continue
				}
				count++
				href, hrefPresent := pageTestAttribute(tag, "href")
				mediaType, typePresent := pageTestAttribute(tag, "type")
				if span[1] > bodies[0][0] || !hrefPresent || href != "/_appkit/favicon.svg" || !typePresent || mediaType != "image/svg+xml" {
					t.Fatalf("%s %s: favicon link must precede body and declare its SVG URL and type: %q", request.Method, request.URL.Path, tag)
				}
			}
			if count != 1 {
				t.Fatalf("%s %s: favicon link count = %d", request.Method, request.URL.Path, count)
			}
		}
	}
}
