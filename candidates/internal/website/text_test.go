package website_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func TestUnreadableReasonVocabulary(t *testing.T) {
	// R-DX20-V7LF
	type code string
	got := []code{website.ReasonScripted, website.ReasonRedirect, website.ReasonEmpty}
	want := []code{"scripted", "redirect", "empty"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatal(got)
		}
	}
}

func TestTextLayout(t *testing.T) {
	// R-2H4K-EFIU R-E1XM-EAK7 R-R2JM-7CZP
	for _, tt := range []struct{ body, want string }{
		{`<html><head><title>Acme</title><style>p{color:red}</style></head><body><header><a href="/">Home</a></header><nav><a href="/about">About</a></nav><h1>Acme  Hauling</h1><p>We haul <b>fr</b>eight
 since&nbsp;1990.</p><ul><li>Logs</li><li>Chips</li></ul><footer>Call us</footer></body></html>`, "Acme Hauling\n\nWe haul freight since 1990.\n\nLogs\nChips"},
		{`<div>One</div><div>Two<br>Three</div><p>Four</p>`, "One\nTwo\nThree\n\nFour"},
		{`<article><header><h2>News</h2></header><p>Body</p></article><form><p>Order</p><button>Send</button></form>`, "News\n\nBody\n\nOrder"},
		{`<p>Before</p><h1>One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6><blockquote>Quote</blockquote><pre> A  B
 C </pre><dl><dt>Term</dt><dd>Meaning</dd></dl><figure>Image<figcaption>Caption</figcaption></figure><hr><p>After</p>`, "Before\n\nOne\n\nTwo\n\nThree\n\nFour\n\nFive\n\nSix\n\nQuote\n\nA B C\n\nTerm\nMeaning\n\nImage\nCaption\n\nAfter"},
		{`<p>Before</p><table><caption>Data</caption><tr><th>One</th><th>Two</th></tr><tr><td>A</td><td>B</td></tr></table><p>After</p>`, "Before\n\nData\nOne\nTwo\nA\nB\n\nAfter"},
		{`<address>Place</address><form>Form<fieldset><legend>Legend</legend>Fields</fieldset></form><section><header>Heading</header><footer>Footing</footer></section>`, "Place\nForm\nLegend\nFields\nHeading\nFooting"},
		{`<p>one <b>two</b>three<!-- comment -->four &amp; five</p><p> A` + "\u2003\t" + `B </p>`, "one twothreefour & five\n\nA B"},
		{`<noscript><iframe src="https://x.test/"></iframe></noscript><p>Hi</p>`, "Hi"},
		{`<html><head><noscript><link rel="stylesheet" href="a.css"></noscript></head><body><p>Hi</p></body></html>`, "Hi"},
		{"<p>caf\xe9</p>", "caf\uFFFD"}, {"", ""}, {"<script>x</script>", ""}, {"<p>   </p>", ""},
	} {
		if got := website.Text([]byte(tt.body)); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func TestTextContentRootsAndDrops(t *testing.T) {
	// R-E0PQ-0ITI R-E1XM-EAK7 R-R2JM-7CZP
	for _, tt := range []struct{ body, want string }{
		{`<div role="Navigation">Menu</div><main><p>Inside</p></main><p>Outside</p>`, "Inside"},
		{`<p>Outside</p><div role=" MAIN presentation "><p>First</p><div role="main">Nested</div></div><main>Second</main>`, "First\n\nNested\n\nSecond"},
		{`<aside><main>Hidden</main></aside><p>Visible</p>`, "Visible"},
		{`<div role="navigation main"><main>Hidden</main></div><p>Visible</p>`, "Visible"},
		{`<div role="" role="main">Before</div><main>Inside</main>`, "Inside"},
		{`<div role="presentation main">Before</div><p>After</p>`, "Before\n\nAfter"},
		{`<header>Hidden</header><footer>Hidden</footer><section><header>Section</header><footer>Foot</footer></section><article><header>Article</header></article><main><header>Main</header><p>Body</p></main>`, "Main\n\nBody"},
		{`<header>Hidden</header><section><header>Section</header><footer>Foot</footer></section><article><header>Article</header></article>`, "Section\nFoot\nArticle"},
		{`<svg><text>Hidden</text></svg><math><mi>Hidden</mi></math><p>Visible</p>`, "Visible"},
	} {
		if got := website.Text([]byte(tt.body)); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
	for _, tag := range []string{"script", "style", "noscript", "template", "iframe", "noembed", "noframes", "textarea", "select", "button", "nav", "aside", "dialog"} {
		body := "<" + tag + ">Hidden</" + tag + "><p>Visible</p>"
		if got := website.Text([]byte(body)); got != "Visible" {
			t.Errorf("%s: %q", tag, got)
		}
	}
	for _, role := range []string{"navigation", "banner", "contentinfo", "dialog", "alertdialog"} {
		body := `<div role=" ` + strings.ToUpper(role) + ` presentation ">Hidden</div><p>Visible</p>`
		if got := website.Text([]byte(body)); got != "Visible" {
			t.Errorf("%s: %q", role, got)
		}
	}
}

func TestTextFlatFallback(t *testing.T) {
	// R-DZHT-MR2T R-R1BP-TL90 R-R2JM-7CZP
	a, b := strings.Repeat("a", 199), strings.Repeat("b", 300)
	for _, tt := range []struct{ body, want string }{
		{"<title>T</title><p>" + a + "</p><nav>" + b + "</nav>", "T " + a + " " + b},
		{"<p>" + a + "a</p><nav>" + b + "</nav>", a + "a"},
		{"<nav>" + strings.Repeat("b", 150) + "</nav>", ""},
		{"<title>Acme\n  Hauling</title><main>Tiny</main><nav>" + b + "</nav>", "Acme Hauling Tiny " + b},
		{"<title>" + b + "</title><main>Tiny</main>", "Tiny"},
		{"<main>Short</main><nav>A<b>c</b>me <!-- comment -->caf\xe9<script>hidden</script><style>hidden</style><noscript>hidden</noscript>\u2003" + b + "</nav>", "Short A c me caf\uFFFD " + b},
		{"<svg><text>" + b + "</text><script>hidden</script><style>hidden</style></svg><p>Short</p>", b + " Short"},
	} {
		if got := website.Text([]byte(tt.body)); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func TestTextByteBound(t *testing.T) {
	// R-R3RI-L4QE
	for _, content := range []string{strings.Repeat("a", website.TextBytes-1), strings.Repeat("a", website.TextBytes), strings.Repeat("a", 70000), strings.Repeat("a", website.TextBytes-1) + "é", strings.Repeat("a", website.TextBytes-2) + "界", strings.Repeat("a", website.TextBytes-3) + "🙂", strings.Repeat("界", 30000)} {
		want := content
		if len(want) > website.TextBytes {
			n := website.TextBytes
			for !utf8.RuneStart(want[n]) {
				n--
			}
			want = want[:n]
		}
		if got := website.Text([]byte("<p>" + content + "</p>")); got != want || !utf8.ValidString(got) {
			t.Fatalf("bounded text: got %d bytes, want %d", len(got), len(want))
		}
	}
}

func TestGetReadabilityAndReasons(t *testing.T) {
	// R-R4ZE-YWH3 R-R7F7-QFYH R-EAGX-2OR2 R-R1BP-TL90
	for _, tt := range []struct{ name, body, reason string }{
		{"scripted", `<title>Acme</title><div id="app"></div><script src="/app.js"></script>`, website.ReasonScripted},
		{"refresh", `<meta http-equiv=" Refresh " content="0; url=https://x.test/"><script src="a.js"></script>`, website.ReasonRedirect},
		{"frameset", `<html><head><title>X</title></head><frameset><frame src="https://x.test/"></frameset></html>`, website.ReasonRedirect},
		{"iframe", `<iframe src="https://x.test/"></iframe>`, website.ReasonRedirect},
		{"priority", `<script>x</script><iframe></iframe>`, website.ReasonRedirect},
		{"foreign script", `<svg><script>x</script></svg>`, website.ReasonScripted},
		{"foreign iframe", `<svg><iframe></iframe></svg>`, website.ReasonEmpty},
		{"noscript", `<noscript><iframe src="https://x.test/"></iframe></noscript><p>Hi</p>`, website.ReasonEmpty},
		{"soon", `<p>Coming soon</p>`, website.ReasonEmpty},
		{"below", "<p>" + strings.Repeat("x", website.MinChars-1) + "</p>", website.ReasonEmpty},
		{"threshold", "<p>" + strings.Repeat("x", website.MinChars) + "</p>", ""},
		{"rune below", "<p>" + strings.Repeat("界", website.MinChars-1) + "</p>", website.ReasonEmpty},
		{"rune threshold", "<p>" + strings.Repeat("界", website.MinChars) + "</p>", ""},
		{"only title", "<title>" + strings.Repeat("x", 300) + "</title>", website.ReasonEmpty},
		{"leading title", "<title>Acme</title><h1>Acme</h1><p>" + strings.Repeat("x", 194) + "</p>", website.ReasonEmpty},
		{"title word boundary", "<title>Acme</title><p>Acmeville" + strings.Repeat("x", 191) + "</p>", ""},
		{"flat title whitespace", "<title>Acme\n Hauling</title><nav>" + strings.Repeat("x", 199) + "</nav>", website.ReasonEmpty},
		{"flat title threshold", "<title>Acme\n Hauling</title><nav>" + strings.Repeat("x", 200) + "</nav>", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const address = "https://acme.test/"
			k := checker(func(*http.Request) (*http.Response, error) { return response(200, "text/html", tt.body), nil })
			pg := k.Get(context.Background(), address)
			if pg.URL != address || pg.StatusCode != 200 || pg.Size != int64(len(tt.body)) || pg.Error != "" || pg.Reason != tt.reason {
				t.Fatal(pg)
			}
			if tt.reason != "" && pg.Text != "" {
				t.Fatal(pg)
			}
			if tt.reason == "" && pg.Text != website.Text([]byte(tt.body)) {
				t.Fatal(pg)
			}
		})
	}
}

func TestGetReadsBeyondCheckCap(t *testing.T) {
	// R-E814-B59O R-R67B-CO7S
	body := "<title>Acme</title><style>" + strings.Repeat(" ", 300000) + "</style><p>" + strings.Repeat("x", 300) + "</p>"
	k := checker(func(*http.Request) (*http.Response, error) { return response(200, "text/html", body), nil })
	pg := k.Get(context.Background(), "https://acme.test/about")
	if pg != (website.Page{URL: "https://acme.test/about", Title: "Acme", Text: strings.Repeat("x", 300), StatusCode: 200, Size: int64(len(body))}) {
		t.Fatal(pg)
	}
}

func TestGetUnreadablePageFields(t *testing.T) {
	// R-R7F7-QFYH
	const body = `<title>Acme</title><div id="app"></div><script src="/app.js"></script>`
	k := checker(func(*http.Request) (*http.Response, error) { return response(200, "text/html", body), nil })
	pg := k.Get(context.Background(), "https://acme.test/")
	if pg != (website.Page{URL: "https://acme.test/", Title: "Acme", Reason: website.ReasonScripted, StatusCode: 200, Size: int64(len(body))}) {
		t.Fatal(pg)
	}
}
