package panel_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
)

func frameTestRender(t *testing.T, name string, data page.Banner) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := page.Templates().ExecuteTemplate(&rendered, name, data); err != nil {
		t.Fatal(err)
	}
	return rendered.String()
}

// R-Z2EC-BYR0 R-Z4U5-3I8E R-YGG5-G3EI
func frameTestWritten(t *testing.T, raw string, data page.Banner) string {
	t.Helper()
	banner := frameTestRender(t, "banner", data)
	footer := frameTestRender(t, "footer", data)
	bannerStart, bannerEnd, footerStart, footerEnd := -1, -1, -1, -1
	bodies := pageTestTags(raw, "body", false)
	if len(bodies) > 0 {
		at := bodies[0][1]
		for at < len(raw) && strings.ContainsRune(" \t\n\v\f\r", rune(raw[at])) {
			at++
		}
		if strings.HasPrefix(raw[at:], banner) {
			bannerStart, bannerEnd = at, at+len(banner)
		}
	}
	ends := pageTestTags(raw, "body", true)
	if len(ends) > 0 {
		at := ends[len(ends)-1][0]
		for at > 0 && strings.ContainsRune(" \t\n\v\f\r", rune(raw[at-1])) {
			at--
		}
		// Whitespace belonging to the template itself is part of the footer.
		for end := at; end <= ends[len(ends)-1][0]; end++ {
			start := end - len(footer)
			if start >= 0 && (bannerEnd < 0 || start >= bannerEnd) && raw[start:end] == footer {
				footerStart, footerEnd = start, end
				break
			}
		}
	}
	if footerStart >= 0 {
		raw = raw[:footerStart] + raw[footerEnd:]
	}
	if bannerStart >= 0 {
		raw = raw[:bannerStart] + raw[bannerEnd:]
	}
	return raw
}

// R-YGG5-G3EI R-Z2EC-BYR0 R-Z4U5-3I8E
func TestFrameWrittenMarkupOccurrences(t *testing.T) {
	data := page.Banner{Service: "given <service>", Version: "given <version>", Email: "reader@example.test", ProfileURL: "/profile", LogoutURL: "/logout"}
	banner, footer := frameTestRender(t, "banner", data), frameTestRender(t, "footer", data)
	for _, tc := range []struct{ raw, want string }{
		{"<body>\n" + banner + "<main>content</main>" + footer + " \t</body>", "<body>\n<main>content</main> \t</body>"},
		{"<body>" + banner + "<main>" + banner + footer + "</main>" + footer + "</body>", "<body><main>" + banner + footer + "</main></body>"},
		{"<body><main>content</main>" + footer + "</body>", "<body><main>content</main></body>"},
		{"<body>" + banner + "<main>content</main></body>", "<body><main>content</main></body>"},
		{"<body><main>" + banner + footer + "</main></body>", "<body><main>" + banner + footer + "</main></body>"},
		{"<body>" + footer + "<p>after footer</p></body>", "<body>" + footer + "<p>after footer</p></body>"},
		{"<body>" + footer + "</body><body>last</body>", "<body>" + footer + "</body><body>last</body>"},
		{"plain", "plain"},
	} {
		if got := frameTestWritten(t, tc.raw, data); got != tc.want {
			t.Fatalf("written markup = %q, want %q", got, tc.want)
		}
	}
}

// R-ZQSB-ZDKW R-Z2EC-BYR0 R-Z4U5-3I8E R-0ZWM-4W0L
func TestFrameDocumentsDrawSameFetchedBannerAndFooter(t *testing.T) {
	var drawn page.Banner
	calls := 0
	source := func(u page.User) page.Banner {
		calls++
		drawn = page.Banner{Service: fmt.Sprintf("source <%d>", calls), Version: fmt.Sprintf("revision <%d>", calls), Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL,
			Services: []page.Service{{Name: "other", URL: "/other", Enabled: true}}}
		return drawn
	}
	h := coreHandler(t, panelTestStore(t), source, io.Discard)
	for _, r := range pageTestDocuments() {
		body := pageTestResponse(h, r).Body.String()
		banner := frameTestRender(t, "banner", drawn)
		footer := frameTestRender(t, "footer", drawn)
		bodies, ends := pageTestTags(body, "body", false), pageTestTags(body, "body", true)
		if len(bodies) == 0 || len(ends) == 0 {
			t.Fatal("document body absent")
		}
		start := bodies[0][1]
		for start < len(body) && strings.ContainsRune(" \t\n\v\f\r", rune(body[start])) {
			start++
		}
		if !strings.HasPrefix(body[start:], banner) {
			t.Fatal("not drawn with fetched banner")
		}
		last := ends[len(ends)-1][0]
		found := false
		for end := last; end >= start+len(banner)+len(footer); end-- {
			if !formASCIIWhitespace(body[end:last]) {
				break
			}
			if body[end-len(footer):end] == footer {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("not drawn with same fetched footer before final body end")
		}
	}
}

// R-GSWJ-U944
func TestFrameEmailHasNoVisibleText(t *testing.T) {
	for _, services := range [][]page.Service{nil, {}, {{Name: "alpha", URL: "https://alpha.test/", Enabled: true, Icon: `<svg><path d="M0 0"/></svg>`}}} {
		source := pageTestEchoingBanner(services)
		for _, email := range []string{"reader@example.test", " \treader+<&\"@example.test \n"} {
			for _, r := range pageTestDocuments() {
				r.Header.Set("X-User-Email", email)
				store := panelTestStore(t)
				for _, w := range panelStoreAll(t, store) {
					if strings.Contains(w.Name, "@") {
						t.Fatal("email test precondition violated")
					}
				}
				if r.Body != nil {
					data, err := io.ReadAll(r.Body)
					if err != nil || strings.Contains(string(data), "@") {
						t.Fatal("email body precondition violated")
					}
					r.Body = io.NopCloser(bytes.NewReader(data))
				}
				body := pageTestResponse(coreHandler(t, store, source, io.Discard), r).Body.String()
				data := pageTestBannerData(r)
				data.Services = services
				if strings.Contains(pageTestVisible(frameTestWritten(t, body, data)), strings.Join(strings.Fields(email), " ")) {
					t.Fatal("email appears in visible text")
				}
			}
		}
	}
}

// R-Y4AE-S6E9
func TestFrameDocumentsPreloadSharedFont(t *testing.T) {
	for _, services := range [][]page.Service{nil, {{Name: "other", URL: "/other", Enabled: true}}} {
		for _, r := range pageTestDocuments() {
			h := coreHandler(t, panelTestStore(t), pageTestEchoingBanner(services), io.Discard)
			out := pageTestResponse(h, r)
			if out.Body.Len() == 0 || out.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatal("expected HTML document")
			}
			data := pageTestBannerData(r)
			data.Services = services
			body := pageTestStrip(frameTestWritten(t, out.Body.String(), data))
			bodies := pageTestTags(body, "body", false)
			if len(bodies) == 0 {
				t.Fatal("document body absent")
			}
			hasValue := func(tag, name, value string) bool {
				for _, attr := range coreAttributes(tag) {
					if attr.valued && strings.EqualFold(attr.name, name) && attr.value == value {
						return true
					}
				}
				return false
			}
			count := 0
			for _, span := range pageTestTags(body, "link", false) {
				tag := body[span[0]:span[1]]
				if !hasValue(tag, "rel", "preload") {
					continue
				}
				count++
				if span[0] >= bodies[0][0] || !hasValue(tag, "as", "font") || !hasValue(tag, "type", "font/woff2") || !hasValue(tag, "href", page.PreloadURL()) || !pageTestBareAttribute(tag, "crossorigin") && !hasValue(tag, "crossorigin", "") {
					t.Fatalf("font preload attributes or position: %s", tag)
				}
			}
			if count != 1 {
				t.Fatalf("font preload count = %d", count)
			}
		}
	}
}
