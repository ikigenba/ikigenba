package panel

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
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
	h := coreHandler(t, widget.NewStore(), source, io.Discard)
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

// R-0OXI-OYCC
func TestFrameEmailHasNoVisibleText(t *testing.T) {
	for _, services := range [][]page.Service{nil, {}, {{Name: "alpha", URL: "https://alpha.test/", Enabled: true, Icon: `<svg><path d="M0 0"/></svg>`}}} {
		source := func(u page.User) page.Banner {
			data := pageTestBanner(u)
			data.Services = services
			return data
		}
		for _, email := range []string{"reader@example.test", " \treader+<&\"@example.test \n"} {
			for _, r := range pageTestDocuments() {
				r.Header.Set("X-User-Email", email)
				body := pageTestResponse(coreHandler(t, widget.NewStore(), source, io.Discard), r).Body.String()
				if strings.Contains(pageTestVisible(body), strings.Join(strings.Fields(email), " ")) {
					t.Fatal("email appears in visible text")
				}
			}
		}
	}
}
