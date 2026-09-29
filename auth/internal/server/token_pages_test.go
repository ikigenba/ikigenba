package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestTokenElapsedBoundaries(t *testing.T) {
	// R-VQ10-ZQIN: elapsed text floors whole units, including future skew and large day counts.
	cases := []struct {
		d    time.Duration
		text string
	}{{-time.Hour, "just now"}, {0, "just now"}, {time.Minute - time.Nanosecond, "just now"}, {time.Minute, "1 minute ago"}, {2 * time.Minute, "2 minutes ago"}, {time.Hour - time.Nanosecond, "59 minutes ago"}, {time.Hour, "1 hour ago"}, {2 * time.Hour, "2 hours ago"}, {24*time.Hour - time.Nanosecond, "23 hours ago"}, {24 * time.Hour, "1 day ago"}, {48 * time.Hour, "2 days ago"}, {365 * 24 * time.Hour, "365 days ago"}}
	for _, c := range cases {
		if got := tokenElapsed(c.d); got != c.text {
			t.Errorf("elapsed(%s)=%q want %q", c.d, got, c.text)
		}
	}
}

func tokenProfileRequest(session string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Host = "localhost:3001"
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return r
}

func TestTokenRefusalBodiesAreSingleLines(t *testing.T) {
	// R-T01Q-F5J7: all mutation origin refusals and nonexistent/foreign action targets are single plain-text lines.
	st := openTokenTestStore(t)
	_, session := tokenTestIdentity(t, st, "owner")
	other, _ := tokenTestIdentity(t, st, "other")
	token, _, err := st.CreateToken(other.ID, "foreign", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"create", "enable", "disable", "delete"} {
		var r *http.Request
		if action == "create" {
			r = tokenRequest("/tokens", session.ID, url.Values{"name": {"new"}, "expires": {"never"}})
		} else {
			r = tokenActionRequest(session.ID, token.ID, action)
		}
		r.Header.Set("Origin", "wrong")
		tokenAssertPlainLine(t, tokenTestServer(st), r, http.StatusForbidden)
	}
	for _, action := range []string{"enable", "disable", "delete"} {
		for _, id := range []string{token.ID, "missing"} {
			tokenAssertPlainLine(t, tokenTestServer(st), tokenActionRequest(session.ID, id, action), http.StatusNotFound)
		}
	}
}
func tokenAssertPlainLine(t *testing.T, s *Server, r *http.Request, status int) {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != status || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || len(body) < 2 || body[len(body)-1] != '\n' || strings.ContainsAny(body[:len(body)-1], "\r\n") {
		t.Fatalf("plain refusal = %d %q %q", w.Code, w.Header(), body)
	}
}

func tokenElement(t *testing.T, body, name string) string {
	t.Helper()
	matches := regexp.MustCompile(`(?s)<`+name+`(?:\s[^>]*)?>(.*?)</`+name+`>`).FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("%s count=%d in %s", name, len(matches), body)
	}
	return matches[0][1]
}
func tokenElements(body, name string) []string {
	matches := regexp.MustCompile(`(?s)<`+name+`(?:\s[^>]*)?>(.*?)</`+name+`>`).FindAllStringSubmatch(body, -1)
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m[0]
	}
	return out
}
func tokenOpening(element string) string { return element[:strings.IndexByte(element, '>')+1] }
func tokenOrder(t *testing.T, body string, parts ...string) {
	t.Helper()
	last := -1
	for _, p := range parts {
		i := strings.Index(body, p)
		if i <= last {
			t.Fatalf("order %q at %d after %d in %s", p, i, last, body)
		}
		last = i
	}
}
func tokenAttrs(t *testing.T, element string, want map[string]string) {
	t.Helper()
	a := pageAttrs(pageTag{raw: tokenOpening(element)})
	for k, v := range want {
		if !reflect.DeepEqual(a[k], []string{v}) {
			t.Fatalf("attribute %s=%v want %q in %s", k, a[k], v, element)
		}
	}
}
func tokenNoMark(t *testing.T, element, attr string) {
	t.Helper()
	if tokenMarks(tokenOpening(element), attr) {
		t.Fatalf("unexpected %s in %s", attr, element)
	}
}
func tokenMarks(tag, attr string) bool {
	i := 1
	for i < len(tag) && !strings.ContainsRune(" \t\r\n\f>/", rune(tag[i])) {
		i++
	}
	for i < len(tag) {
		for i < len(tag) && strings.ContainsRune(" \t\r\n\f/", rune(tag[i])) {
			i++
		}
		start := i
		for i < len(tag) && !strings.ContainsRune(" \t\r\n\f=>/", rune(tag[i])) {
			i++
		}
		if start == i {
			return false
		}
		if strings.EqualFold(tag[start:i], attr) {
			return true
		}
		for i < len(tag) && strings.ContainsRune(" \t\r\n\f", rune(tag[i])) {
			i++
		}
		if i < len(tag) && tag[i] == '=' {
			i++
			for i < len(tag) && strings.ContainsRune(" \t\r\n\f", rune(tag[i])) {
				i++
			}
			if i < len(tag) && (tag[i] == '"' || tag[i] == '\'') {
				quote := tag[i]
				i++
				for i < len(tag) && tag[i] != quote {
					i++
				}
				if i < len(tag) {
					i++
				}
			} else {
				for i < len(tag) && !strings.ContainsRune(" \t\r\n\f>", rune(tag[i])) {
					i++
				}
			}
		}
	}
	return false
}

func tokenConsists(t *testing.T, content string, children ...string) {
	t.Helper()
	rest := content
	for _, child := range children {
		rest = strings.TrimLeft(rest, " \t\r\n\f")
		if !strings.HasPrefix(rest, child) {
			t.Fatalf("content does not consist of expected children: %s", content)
		}
		rest = rest[len(child):]
	}
	if strings.Trim(rest, " \t\r\n\f") != "" {
		t.Fatalf("extra child/text %q", rest)
	}
}
func tokenRead(t *testing.T, element, want string) {
	t.Helper()
	if got := pageText(element); got != want {
		t.Fatalf("reads %q want %q in %s", got, want, element)
	}
}
func tokenCard(t *testing.T, body, title string) string {
	t.Helper()
	for _, c := range tokenElements(body, "section") {
		if strings.Contains(c, "<h2>"+title+"</h2>") {
			return c
		}
	}
	t.Fatalf("missing card %s", title)
	return ""
}
func tokenForm(t *testing.T, body string) string {
	t.Helper()
	for _, f := range tokenElements(body, "form") {
		if tagAttr(tokenOpening(f), "action") == "/tokens" {
			return f
		}
	}
	t.Fatalf("missing create form")
	return ""
}
func assertTokenCreateForm(t *testing.T, body, name, expiry string, rejected, nameErr, expiryErr bool) {
	t.Helper()
	form := tokenForm(t, body)
	// R-TH4B-RXWX: the create form has method post and action /tokens.
	tokenAttrs(t, form, map[string]string{"method": "post", "action": "/tokens"})
	labels := tokenElements(form, "label")
	inputs := regexp.MustCompile(`<input\b[^>]*>`).FindAllString(form, -1)
	selects := tokenElements(form, "select")
	buttons := tokenElements(form, "button")
	// R-TIC8-5PNM: exactly the named labels, single capped text input, and expiry select.
	if len(inputs) != 1 || len(selects) != 1 || len(buttons) != 1 {
		t.Fatalf("form controls=%v %v %v %v", labels, inputs, selects, buttons)
	}
	nameLabel := tokenNamedLabel(t, labels, "token-name")
	expiryLabel := tokenNamedLabel(t, labels, "token-expires")
	tokenRead(t, nameLabel, "Name")
	tokenRead(t, expiryLabel, "Expires")
	tokenAttrs(t, inputs[0], map[string]string{"id": "token-name", "type": "text", "name": "name", "maxlength": "64", "placeholder": "e.g. ci-deploy"})
	tokenAttrs(t, selects[0], map[string]string{"id": "token-expires", "name": "expires"})
	// R-TJK4-JHEB, R-VW7C-EV6R: four ordered options, selected by attribute presence including bare selected.
	opts := tokenElements(selects[0], "option")
	if len(opts) != 4 {
		t.Fatalf("options=%v", opts)
	}
	values := []string{"30d", "90d", "365d", "never"}
	words := []string{"In 30 days", "In 90 days", "In 365 days", "Never"}
	selected := 0
	for i, o := range opts {
		tokenAttrs(t, o, map[string]string{"value": values[i]})
		tokenRead(t, o, words[i])
		if tokenMarks(tokenOpening(o), "selected") {
			selected++
			if values[i] != expiry {
				t.Fatalf("selected %q want %q", values[i], expiry)
			}
		}
	}
	if selected != 1 {
		t.Fatalf("selected=%d", selected)
	}
	tokenConsists(t, tokenElement(t, selects[0], "select"), opts...)
	// R-TKS0-X950, R-T19M-SX9W: single submit button draws the exact plus paths, in order.
	tokenAttrs(t, buttons[0], map[string]string{"type": "submit"})
	tokenRead(t, buttons[0], "Create token")
	tokenAssertIcon(t, buttons[0], []string{"M12 5l0 14", "M5 12l14 0"})
	// R-TLZX-B0VP: labels, controls, and submit follow the required order; spans occur only in field hint/error positions.
	tokenOrder(t, form, nameLabel, inputs[0], expiryLabel, selects[0], buttons[0])
	for _, span := range tokenElements(form, "span") {
		i := strings.Index(form, span)
		if (i <= strings.Index(form, inputs[0]) || i >= strings.Index(form, expiryLabel)) && (i <= strings.Index(form, "</select>") || i >= strings.Index(form, buttons[0])) {
			t.Fatalf("span outside field positions")
		}
	}
	// R-TPNM-GC3S and R-VZV1-K6EU: rejected values remain raw, while fresh input has no value.
	if rejected {
		tokenAttrs(t, inputs[0], map[string]string{"value": name})
	} else {
		tokenNoMark(t, inputs[0], "value")
	}
	// R-TS3F-7VL6, R-W12X-XY5J, R-W2AU-BPW8: each field independently shows exactly its hint or accessible error.
	nameSpans, expirySpans := tokenFieldSpans(form, inputs[0], expiryLabel, buttons[0])
	wantExpiry := 0
	if expiryErr {
		wantExpiry = 1
	}
	if len(nameSpans) != 1 || len(expirySpans) != wantExpiry {
		t.Fatalf("field spans=%v / %v", nameSpans, expirySpans)
	}
	if nameErr {
		tokenAttrs(t, inputs[0], map[string]string{"aria-describedby": "token-name-error"})
		tokenAttrs(t, nameSpans[0], map[string]string{"id": "token-name-error"})
		tokenRead(t, nameSpans[0], "the name must be 1 to 64 characters")
		if strings.Contains(form, `class="hint"`) {
			t.Fatal("hint accompanies error")
		}
	} else {
		tokenNoMark(t, inputs[0], "aria-describedby")
		tokenAttrs(t, nameSpans[0], map[string]string{"class": "hint"})
		tokenRead(t, nameSpans[0], "Something that tells you where it's used. Up to 64 characters.")
		if strings.Contains(form, `id="token-name-error"`) {
			t.Fatal("unexpected name error")
		}
	}
	if expiryErr {
		tokenAttrs(t, selects[0], map[string]string{"aria-describedby": "token-expires-error"})
		tokenAttrs(t, expirySpans[0], map[string]string{"id": "token-expires-error"})
		tokenRead(t, expirySpans[0], "choose one of the listed expiry options")
	} else {
		tokenNoMark(t, selects[0], "aria-describedby")
		if strings.Contains(form, `id="token-expires-error"`) {
			t.Fatal("unexpected expiry error")
		}
	}
	// R-TWZ0-QYJY: rejected forms have one actions div holding button then sole Cancel link.
	if rejected {
		divs := tokenElements(form, "div")
		links := tokenElements(form, "a")
		if len(divs) != 1 || len(links) != 1 {
			t.Fatalf("actions divs/links=%v/%v", divs, links)
		}
		tokenAttrs(t, divs[0], map[string]string{"class": "actions"})
		tokenAttrs(t, links[0], map[string]string{"class": "button ghost", "href": "/"})
		tokenRead(t, links[0], "Cancel")
		tokenConsists(t, tokenElement(t, divs[0], "div"), buttons[0], links[0])
	}
}
func tokenNamedLabel(t *testing.T, labels []string, field string) string {
	t.Helper()
	var found []string
	for _, label := range labels {
		for _, value := range pageAttrs(pageTags(label)[0])["for"] {
			if value == field {
				found = append(found, label)
				break
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("labels for %s=%v", field, found)
	}
	tokenAttrs(t, found[0], map[string]string{"for": field})
	return found[0]
}
func tokenFieldSpans(form, input, expiryLabel, button string) ([]string, []string) {
	nameSlot := form[strings.Index(form, input)+len(input) : strings.Index(form, expiryLabel)]
	expirySlot := form[strings.Index(form, "</select>")+len("</select>") : strings.Index(form, button)]
	return tokenElements(nameSlot, "span"), tokenElements(expirySlot, "span")
}
func tokenAssertIcon(t *testing.T, button string, paths []string) {
	t.Helper()
	content := tokenElement(t, button, "button")
	icons := pageElements(content, "svg")
	if len(icons) != 1 || strings.Trim(content[:icons[0].start], " \t\r\n\f") != "" {
		t.Fatalf("icon not first: %s", button)
	}
	svg := pageContent(content, icons[0])
	ps := pageElements(svg, "path")
	if len(ps) != len(paths) {
		t.Fatalf("paths=%v", ps)
	}
	offset := 0
	var remainder strings.Builder
	for i, p := range ps {
		pageAttr(t, p, "d", paths[i])
		if len(pageAttributeNames(p)) != 1 {
			t.Fatal("extra path attributes")
		}
		remainder.WriteString(svg[offset:p.start])
		offset = p.end
	}
	remainder.WriteString(svg[offset:])
	rest := regexp.MustCompile(`(?i)</path>`).ReplaceAllString(remainder.String(), "")
	if strings.Trim(rest, " \t\r\n\f") != "" {
		t.Fatalf("foreign svg content %q", rest)
	}
}
func tokenCreatedFinalChild(t *testing.T, content string, header, warn, secret, link string) string {
	t.Helper()
	offset := 0
	for _, child := range []string{header, warn, secret} {
		rest := strings.TrimLeft(content[offset:], " \t\r\n\f")
		if !strings.HasPrefix(rest, child) {
			t.Fatal("created card children out of order")
		}
		offset = len(content) - len(rest) + len(child)
	}
	final := strings.Trim(content[offset:], " \t\r\n\f")
	tags := pageTags(final)
	if len(tags) > 0 && tags[0].start == 0 && tags[0].name == "p" {
		paragraphs := tokenElements(final, "p")
		if len(paragraphs) != 1 {
			t.Fatal("invalid account link paragraph")
		}
		tokenConsists(t, tokenElement(t, paragraphs[0], "p"), link)
		tokenConsists(t, final, paragraphs[0])
		return paragraphs[0]
	}
	tokenConsists(t, final, link)
	return link
}

func TestTokenEmptyProfileAndRejectedForms(t *testing.T) {
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "forms")
	srv := tokenTestServer(st)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenProfileRequest(session.ID))
	body := w.Body.String()
	assertAuthPage(t, body, false)
	api := tokenCard(t, body, "API tokens")
	create := tokenCard(t, body, "Create a token")
	// R-T4XB-Y8HZ, R-TLMK-BXKH: flush header and sole empty token panel; no table/scrolling wrapper.
	tokenAttrs(t, api, map[string]string{"class": "card flush"})
	header := tokenElements(api, "header")[0]
	panel := tokenElements(api, "div")[0]
	tokenConsists(t, tokenElement(t, api, "section"), header, panel)
	tokenRead(t, tokenElements(header, "p")[0], "Personal access tokens let scripts and tools act as you. Send one as a bearer token.")
	tokenAttrs(t, panel, map[string]string{"class": "empty"})
	h3 := tokenElements(panel, "h3")[0]
	p := tokenElements(panel, "p")[0]
	tokenRead(t, h3, "No tokens yet")
	tokenRead(t, p, "Create one below when a script or tool needs to act as you.")
	tokenConsists(t, tokenElement(t, panel, "div"), h3, p)
	if strings.Contains(body, "<table") || strings.Contains(body, `class="table-scroll"`) {
		t.Fatal("empty profile has table")
	}
	// R-VZV1-K6EU: fresh create card consists of header and default form.
	tokenAttrs(t, create, map[string]string{"class": "card"})
	tokenConsists(t, tokenElement(t, create, "section"), tokenElements(create, "header")[0], tokenForm(t, create))
	assertTokenCreateForm(t, body, "", "90d", false, false, false)
	cases := []struct {
		name, expiry       string
		nameErr, expiryErr bool
	}{{"", "", true, true}, {" \t ", "30d", true, false}, {strings.Repeat("界", 65), "365d", true, false}, {"  <&\"' kept \n ", "bad", false, true}, {"ok", "", false, true}, {strings.Repeat("界", 64), "bad", false, true}, {"", "never", true, false}, {"", "90d", true, false}}
	for _, c := range cases {
		w = httptest.NewRecorder()
		srv.ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {c.name}, "expires": {c.expiry}}))
		if w.Code != 400 {
			t.Fatalf("reject status=%d", w.Code)
		}
		body = w.Body.String()
		assertAuthPage(t, body, false)
		// R-7PYN-HF6M: rejected page has the user's banner and one Create a token card.
		main := assertChrome(t, body, user.Email)
		cards := tokenElements(main, "section")
		if len(cards) != 1 {
			t.Fatalf("rejected cards=%v", cards)
		}
		tokenConsists(t, main, cards[0])
		card := tokenCard(t, main, "Create a token")
		tokenAttrs(t, card, map[string]string{"class": "card"})
		tokenConsists(t, tokenElement(t, card, "section"), tokenElements(card, "header")[0], tokenForm(t, card))
		// R-TQVI-U3UH: accepted expiry is retained; malformed or missing selects 90d.
		exp := c.expiry
		if _, ok := tokenExpiry(exp); !ok {
			exp = "90d"
		}
		assertTokenCreateForm(t, body, c.name, exp, true, c.nameErr, c.expiryErr)
	}
}

func TestTokenPopulatedProfile(t *testing.T) {
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "rows")
	other, _ := tokenTestIdentity(t, st, "foreign")
	created := tokenTestNow.Add(-24 * time.Hour).In(time.FixedZone("offset", 3600))
	used := tokenTestNow.Add(-2 * time.Hour)
	type fixture struct {
		name    string
		created time.Time
		used    *time.Time
		expiry  store.Expiry
		enabled bool
	}
	cases := []fixture{{"never old", created, nil, store.ExpiryNever, true}, {"never new", created.Add(time.Hour), nil, store.Expiry30d, false}, {"used old", created, timePointer(used), store.Expiry90d, true}, {"used new", created.Add(time.Hour), timePointer(used), store.Expiry365d, false}, {"latest", created, timePointer(tokenTestNow.Add(time.Minute)), store.ExpiryNever, true}}
	secrets := []string{}
	byName := map[string]store.Token{}
	for _, c := range cases {
		token, secret, err := st.CreateToken(user.ID, c.name, c.expiry, c.created)
		if err != nil {
			t.Fatal(err)
		}
		secrets = append(secrets, secret)
		if c.used != nil {
			if _, err := st.TouchTokenIdentity(secret, *c.used); err != nil {
				t.Fatal(err)
			}
		}
		if !c.enabled {
			if err := st.SetTokenEnabled(user.ID, token.ID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	foreign, foreignSecret, err := st.CreateToken(other.ID, "foreign token", store.ExpiryNever, created)
	if err != nil {
		t.Fatal(err)
	}
	secrets = append(secrets, foreignSecret)
	tokens, err := st.ListTokens(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		byName[token.Name] = token
	}
	calls := 0
	var readings []time.Time
	srv := New(Config{Store: st, Now: func() time.Time {
		calls++
		value := tokenTestNow.Add(time.Duration(calls-1) * time.Hour)
		readings = append(readings, value)
		return value
	}})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenProfileRequest(session.ID))
	if w.Code != 200 {
		t.Fatalf("profile=%d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	assertAuthPage(t, body, false)
	api := tokenCard(t, body, "API tokens")
	panel := tokenElements(api, "div")[0]
	table := tokenElements(panel, "table")
	// R-THYV-6MCE: populated token panel is table-scroll with one table and no empty state.
	tokenAttrs(t, panel, map[string]string{"class": "table-scroll"})
	if len(table) != 1 || strings.Contains(body, `class="empty"`) {
		t.Fatal("populated panel wrong")
	}
	// R-TJ6R-KE33: table consists of head then body, with precisely six ordered column names.
	head := tokenElement(t, table[0], "thead")
	tbody := tokenElement(t, table[0], "tbody")
	tokenConsists(t, tokenElement(t, table[0], "table"), tokenElements(table[0], "thead")[0], tokenElements(table[0], "tbody")[0])
	headRows := tokenElements(head, "tr")
	if len(headRows) != 1 {
		t.Fatalf("head rows=%v", headRows)
	}
	ths := tokenElements(headRows[0], "th")
	if len(ths) != 6 {
		t.Fatalf("ths=%v", ths)
	}
	for i, word := range []string{"Name", "Created", "Last used", "Expires", "Status", ""} {
		tokenRead(t, ths[i], word)
	}
	tokenConsists(t, tokenElement(t, headRows[0], "tr"), ths...)
	tokenConsists(t, head, headRows...)
	// R-VMDB-UFAK, R-VNL8-8719: one row per owned token, sorted used-first, usage desc, creation desc for ties and never-used.
	rows := tokenElements(tbody, "tr")
	if len(rows) != len(cases) {
		t.Fatalf("row count=%d", len(rows))
	}
	tokenConsists(t, tbody, rows...)
	commonReadings := append([]time.Time(nil), readings...)
	names := []string{"latest", "used new", "used old", "never new", "never old"}
	for i, name := range names {
		token := byName[name]
		row := rows[i]
		if strings.Count(row, `action="/tokens/`+token.ID+`/delete"`) != 1 {
			t.Fatalf("row %d does not uniquely identify %s: %s", i, name, row)
		}
		// R-VSGT-RA01: exactly six cells in defined order, with normalized token name.
		cells := tokenElements(row, "td")
		if len(cells) != 6 {
			t.Fatalf("cells=%v", cells)
		}
		tokenConsists(t, tokenElement(t, row, "tr"), cells...)
		tokenRead(t, cells[0], name)
		// R-T9SX-HBGR: UTC datetime drops fractional seconds and displayed time drops seconds.
		tokenAssertTime(t, cells[1], token.CreatedAt, false, "")
		// R-VOT4-LYRY, R-VR8X-DI9C: each last-used cell uses the same injected draw reading; title and datetime describe exact stored instant.
		if token.LastUsedAt == nil {
			tokenAttrs(t, cells[2], map[string]string{"class": "muted"})
			tokenRead(t, cells[2], "Never")
		} else {
			observed := pageText(cells[2])
			remaining := commonReadings[:0]
			for _, reading := range commonReadings {
				if tokenElapsed(reading.Sub(*token.LastUsedAt)) == observed {
					remaining = append(remaining, reading)
				}
			}
			commonReadings = remaining
			tokenAssertTime(t, cells[2], *token.LastUsedAt, true, observed)
		}
		if token.ExpiresAt == nil {
			tokenAttrs(t, cells[3], map[string]string{"class": "muted"})
			tokenRead(t, cells[3], "Never")
		} else {
			tokenAssertTime(t, cells[3], *token.ExpiresAt, false, "")
		}
		// R-VYN5-6EO5: single status badge, ok only when enabled.
		spans := tokenElements(cells[4], "span")
		if len(spans) != 1 {
			t.Fatalf("status=%s", cells[4])
		}
		tokenConsists(t, tokenElement(t, cells[4], "td"), spans[0])
		tokenAttrs(t, spans[0], map[string]string{"class": "badge"})
		word, action := "Disabled", "enable"
		if token.Enabled {
			word, action = "Enabled", "disable"
			tokenAttrs(t, spans[0], map[string]string{"data-kind": "ok"})
		} else {
			tokenNoMark(t, spans[0], "data-kind")
		}
		tokenRead(t, spans[0], word)
		// R-TEOJ-0EFJ: exactly ordered inline toggle/delete forms with small ghost submit buttons.
		tokenAttrs(t, cells[5], map[string]string{"class": "row-actions"})
		forms := tokenElements(cells[5], "form")
		if len(forms) != 2 {
			t.Fatalf("actions=%s", cells[5])
		}
		tokenConsists(t, tokenElement(t, cells[5], "td"), forms...)
		for j, f := range forms {
			a, label := action, "Enable"
			if token.Enabled {
				label = "Disable"
			}
			if j == 1 {
				a, label = "delete", "Delete"
			}
			tokenAttrs(t, f, map[string]string{"class": "inline", "method": "post", "action": "/tokens/" + token.ID + "/" + a})
			buttons := tokenElements(f, "button")
			if len(buttons) != 1 {
				t.Fatalf("buttons=%v", buttons)
			}
			tokenConsists(t, tokenElement(t, f, "form"), buttons[0])
			tokenAttrs(t, buttons[0], map[string]string{"class": "ghost small", "type": "submit"})
			tokenRead(t, buttons[0], label)
		}
	}

	if len(commonReadings) == 0 {
		t.Fatal("row elapsed values do not share any actual injected clock reading")
	}
	// R-W4QN-39DM: profiles do not disclose any stored plaintext secret.
	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Fatal("secret disclosed")
		}
	}
	if strings.Contains(body, foreign.ID) || strings.Contains(body, foreign.Name) {
		t.Fatal("foreign row disclosed")
	}
}
func tokenAssertTime(t *testing.T, cell string, x time.Time, last bool, text string) {
	t.Helper()
	times := tokenElements(cell, "time")
	if len(times) != 1 {
		t.Fatalf("time cell=%s", cell)
	}
	tokenConsists(t, tokenElement(t, cell, "td"), times[0])
	tokenAttrs(t, times[0], map[string]string{"datetime": x.UTC().Format("2006-01-02T15:04:05Z")})
	if last {
		tokenAttrs(t, times[0], map[string]string{"title": x.UTC().Format("2006-01-02 15:04 UTC")})
	} else {
		text = x.UTC().Format("2006-01-02 15:04 UTC")
	}
	tokenRead(t, times[0], text)
}

func TestTokenCreatedPageAndSecretLifetime(t *testing.T) {
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "created")
	srv := tokenTestServer(st)
	name := "  deploy <&\"'\n token  "
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {name}, "expires": {"never"}}))
	if w.Code != 200 {
		t.Fatalf("created status=%d", w.Code)
	}
	body := w.Body.String()
	assertAuthPage(t, body, true)
	tokens, err := st.ListTokens(user.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens=%v %v", tokens, err)
	}
	secret, ok := presentedSecret(body, tokens[0].Hash)
	if !ok {
		t.Fatal("stored secret not presented")
	}
	// R-7R6J-V6XB: created page has the user's banner, one Token created card, then script.
	main := assertChrome(t, body, user.Email)
	card := tokenCard(t, main, "Token created")
	script := tokenElements(main, "script")
	if len(script) != 1 || len(tokenElements(main, "section")) != 1 {
		t.Fatalf("created main=%s", main)
	}
	tokenAttrs(t, card, map[string]string{"class": "card"})
	tokenConsists(t, main, card, script[0])
	// R-W3IQ-PHMX: warning, secret div and sole account link follow the card header exactly.
	divs := tokenElements(card, "div")
	links := tokenElements(card, "a")
	if len(divs) != 2 || len(links) != 1 {
		t.Fatalf("created children=%v %v", divs, links)
	}
	tokenAttrs(t, divs[0], map[string]string{"class": "alert quiet", "data-kind": "warn"})
	strong := tokenElements(divs[0], "strong")[0]
	p := tokenElements(divs[0], "p")[0]
	tokenConsists(t, tokenElement(t, divs[0], "div"), strong, p)
	tokenRead(t, strong, "Copy it now")
	tokenRead(t, p, "This is the only time deploy <&\"' token is shown. Only its hash is stored.")
	tokenAttrs(t, links[0], map[string]string{"href": "/"})
	tokenRead(t, links[0], "Back to your account")
	content := tokenElement(t, card, "section")
	final := tokenCreatedFinalChild(t, content, tokenElements(card, "header")[0], divs[0], divs[1], links[0])
	tokenConsists(t, content, tokenElements(card, "header")[0], divs[0], divs[1], final)
	// R-U0MP-W9S1, R-T2HJ-6P0L: code is exact returned plaintext, followed by secondary Copy button and exact copy paths.
	tokenAttrs(t, divs[1], map[string]string{"class": "secret"})
	code := tokenElements(divs[1], "code")[0]
	button := tokenElements(divs[1], "button")[0]
	tokenConsists(t, tokenElement(t, divs[1], "div"), code, button)
	if tokenElement(t, code, "code") != secret {
		t.Fatal("code not exact plaintext")
	}
	tokenAttrs(t, button, map[string]string{"class": "secondary", "type": "button"})
	tokenRead(t, button, "Copy")
	tokenAssertIcon(t, button, []string{"M7 9.667a2.667 2.667 0 0 1 2.667 -2.667h8.666a2.667 2.667 0 0 1 2.667 2.667v8.666a2.667 2.667 0 0 1 -2.667 2.667h-8.666a2.667 2.667 0 0 1 -2.667 -2.667l0 -8.666", "M4.012 16.737a2.005 2.005 0 0 1 -1.012 -1.737v-10c0 -1.1 .9 -2 2 -2h10c.75 0 1.158 .385 1.5 1"})
	// R-TMUG-PPB6: only the secret div's tag mentions secret; no tag contains a character reference.
	secretTags := 0
	for _, tag := range pageTags(body) {
		if strings.Contains(tag.raw, "&") {
			t.Fatalf("tag contains character reference %s", tag.raw)
		}
		if strings.Contains(strings.ToLower(tag.raw), "secret") {
			secretTags++
			if tag.raw != tokenOpening(divs[1]) {
				t.Fatalf("secret selector collision %s", tag.raw)
			}
		}
	}
	if secretTags != 1 {
		t.Fatalf("secret tags=%d", secretTags)
	}
	// R-W76F-USV0: exact sole script tags and fixed statement, with lexical and quoted-whitespace restrictions.
	if strings.Count(body, "<script>") != 1 || strings.Count(body, "</script>") != 1 || tokenOpening(script[0]) != "<script>" {
		t.Fatal("script tags wrong")
	}
	source := tokenElement(t, script[0], "script")
	compact := strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t\r\n\f", r) {
			return -1
		}
		return r
	}, source)
	want := `document.querySelector('.secret>button').addEventListener('click',function(){navigator.clipboard.writeText(document.querySelector('.secret>code').textContent);});`
	if compact != want {
		t.Fatalf("script=%q", source)
	}
	for _, r := range source {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && !strings.ContainsRune(" \t\r\n\f.(){};,'>", r) {
			t.Fatalf("script invalid char %q", r)
		}
	}
	allowed := map[string]bool{}
	for _, word := range strings.Fields("document querySelector addEventListener click function navigator clipboard writeText textContent secret button code") {
		allowed[word] = true
	}
	for _, word := range regexp.MustCompile(`[A-Za-z]+`).FindAllString(source, -1) {
		if !allowed[word] {
			t.Fatalf("script invalid word %q", word)
		}
	}
	for i, quoted := range strings.Split(source, "'") {
		if i%2 == 1 && strings.ContainsAny(quoted, " \t\r\n\f") {
			t.Fatal("script whitespace inside quote")
		}
	}
	// R-W4QN-39DM: secret appears once in code, then never in profile, mutation or rejection responses.
	if strings.Count(body, secret) != 1 {
		t.Fatal("secret repeated")
	}
	for _, r := range []*http.Request{tokenProfileRequest(session.ID), tokenActionRequest(session.ID, tokens[0].ID, "disable"), tokenRequest("/tokens", session.ID, url.Values{"name": {""}, "expires": {"never"}}), tokenActionRequest(session.ID, tokens[0].ID, "enable"), tokenActionRequest(session.ID, tokens[0].ID, "delete")} {
		later := httptest.NewRecorder()
		srv.ServeHTTP(later, r)
		if strings.Contains(later.Body.String(), secret) {
			t.Fatal("later response leaks secret")
		}
	}
}

func TestTokenMarkAttributePresence(t *testing.T) {
	// R-VW7C-EV6R: marks reads attribute names case-insensitively and ignores its value form and words inside other values.
	for _, tag := range []string{`<option selected>`, `<option SELECTED="">`, `<option selected='selected'>`, `<option selected=selected>`} {
		if !tokenMarks(tag, "selected") {
			t.Fatalf("selected not marked: %s", tag)
		}
	}
	for _, tag := range []string{`<option data-selected="yes">`, `<option value=" selected ">`, `<option title=' selected '>`} {
		if tokenMarks(tag, "selected") {
			t.Fatalf("selected falsely marked: %s", tag)
		}
	}
}

func TestTokenContractPermittedShapesAndFieldSlots(t *testing.T) {
	// R-TIC8-5PNM: additional labels are allowed; only each named label is unique.
	fresh := tokenTemplateFixture(t, "tokenCreate", tokenCreateValues("", "90d", false))
	extra := strings.Replace(fresh, `<label for="token-name">`, `<label for="other">Other</label><label for="token-name">`, 1)
	assertTokenCreateForm(t, extra, "", "90d", false, false, false)
	// R-W3IQ-PHMX: the final account link may stand alone or inside one paragraph.
	header, warn, displayedValue, link := `<header><h2>Token created</h2></header>`, `<div>warning</div>`, `<div>secret</div>`, `<a href="/">Back to your account</a>`
	for _, final := range []string{link, "<p> \n" + link + " </p>"} {
		content := header + warn + displayedValue + final
		if got := tokenCreatedFinalChild(t, content, header, warn, displayedValue, link); got != final {
			t.Fatalf("final=%s want %s", got, final)
		}
	}
	// R-T19M-SX9W, R-T2HJ-6P0L: leading ASCII whitespace and omitted path end tags are permitted by the shared icon grammar.
	for _, fixture := range []struct {
		name  string
		paths []string
	}{
		{"plusIcon", []string{"M12 5l0 14", "M5 12l14 0"}},
		{"copyIcon", []string{"M7 9.667a2.667 2.667 0 0 1 2.667 -2.667h8.666a2.667 2.667 0 0 1 2.667 2.667v8.666a2.667 2.667 0 0 1 -2.667 2.667h-8.666a2.667 2.667 0 0 1 -2.667 -2.667l0 -8.666", "M4.012 16.737a2.005 2.005 0 0 1 -1.012 -1.737v-10c0 -1.1 .9 -2 2 -2h10c.75 0 1.158 .385 1.5 1"}},
	} {
		paths := fixture.paths
		icon := tokenTemplateFixture(t, fixture.name, nil)
		icon = strings.ReplaceAll(icon, "</path>", "")
		tokenAssertIcon(t, "<button> \t\r\n\f"+icon+" word</button>", paths)
	}
	// R-W12X-XY5J, R-W2AU-BPW8: moving expiry error into the name slot produces wrong counts in both slots.
	form := tokenForm(t, tokenTemplateFixture(t, "tokenCreate", tokenCreateValues("", "bad", true)))
	input := regexp.MustCompile(`<input\b[^>]*>`).FindString(form)
	label := tokenNamedLabel(t, tokenElements(form, "label"), "token-expires")
	button := tokenElements(form, "button")[0]
	expiryError := `<span id="token-expires-error">choose one of the listed expiry options</span>`
	malformed := strings.Replace(form, expiryError, "", 1)
	malformed = strings.Replace(malformed, label, expiryError+label, 1)
	nameSlot, expirySlot := tokenFieldSpans(malformed, input, label, button)
	if len(nameSlot) != 2 || len(expirySlot) != 0 {
		t.Fatalf("misplaced error not distinguished: %v / %v", nameSlot, expirySlot)
	}
}

func tokenTemplateFixture(t *testing.T, name string, data any) string {
	t.Helper()
	w := httptest.NewRecorder()
	if err := authTemplates.ExecuteTemplate(w, name, data); err != nil {
		t.Fatal(err)
	}
	return w.Body.String()
}
