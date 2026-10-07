package server

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func mintProfileClient(t *testing.T, st *store.Store, owner, name string, approved time.Time) (store.Token, string) {
	t.Helper()
	client, err := st.RegisterClient(name, []string{"http://localhost/callback"}, approved)
	if err != nil {
		t.Fatal(err)
	}
	code, err := st.CreateAuthCode(client.ID, owner, client.RedirectURIs[0], "challenge", "https://mcp.green.example/mcp", approved)
	if err != nil {
		t.Fatal(err)
	}
	code, err = st.ConsumeAuthCode(code.Code, approved)
	if err != nil {
		t.Fatal(err)
	}
	token, secret, err := st.CreateClientToken(code, "mcp.green.example", approved)
	if err != nil {
		t.Fatal(err)
	}
	return token, secret
}

func TestAuthAssetTemplateSet(t *testing.T) {
	// R-GA19-E389 R-GB95-RUYY: use the exact published asset and page template seam, with no Funcs.
	expected, err := page.Templates().ParseFS(auth.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"approve", "mcp-clients"} {
		if expected.Lookup(name) == nil || authTemplates.Lookup(name) == nil {
			t.Fatalf("missing template %s", name)
		}
	}
	data := mcpClientsData{Clients: []mcpClientData{{ID: "tok_00000000000000000000000000", Name: "client <&", Approved: "2026-01-01T01:02:03Z", ApprovedText: "2026-01-01 01:02 UTC", Expires: "2026-04-01T01:02:03Z", ExpiresText: "2026-04-01 01:02 UTC"}}}
	var want, got bytes.Buffer
	if err = expected.ExecuteTemplate(&want, "mcp-clients", data); err != nil {
		t.Fatal(err)
	}
	if err = authTemplates.ExecuteTemplate(&got, "mcp-clients", data); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Fatal("auth template differs from asset template")
	}
	// A fresh canonical set has no legacy rendering functions.
	fresh, err := makeAuthTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.Parse(`{{splitNUL "x"}}`); err == nil {
		t.Fatal("canonical set has legacy functions")
	}
}

func TestClientProfileTemplateAndCells(t *testing.T) {
	// R-5RCW-COGY R-FVB8-Q4T7 R-5TSP-47YC R-5V0L-HZP1 R-5W8H-VRFQ
	// R-5XGE-9J6F R-5YOA-NAX4 R-5ZW7-12NT R-6143-EUEI R-63JW-6DVW
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "client-profile")
	other, _ := tokenTestIdentity(t, st, "foreign-client")
	personal, personalSecret, err := st.CreateToken(user.ID, "personal", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		name     string
		approved time.Time
		used     *time.Time
	}
	used := tokenTestNow.Add(-2 * time.Hour)
	cases := []fixture{
		{"never old", tokenTestNow.Add(-100 * 24 * time.Hour), nil},
		{"never new", tokenTestNow.Add(-24 * time.Hour), nil},
		{"used old", tokenTestNow.Add(-48 * time.Hour), &used},
		{"used new", tokenTestNow.Add(-24 * time.Hour), &used},
		{"latest <&\"\t client", tokenTestNow.Add(-time.Hour), timePointer(tokenTestNow.Add(time.Minute))},
		{"expires now", tokenTestNow.Add(-90 * 24 * time.Hour), nil},
	}
	var secrets []string
	byName := map[string]store.Token{}
	for _, c := range cases {
		token, secret := mintProfileClient(t, st, user.ID, c.name, c.approved)
		if c.used != nil {
			if _, err = st.TouchTokenIdentity(secret, "mcp.green.example", *c.used); err != nil {
				t.Fatal(err)
			}
		}
		byName[c.name] = token
		secrets = append(secrets, secret)
	}
	foreign, _ := mintProfileClient(t, st, other.ID, "foreign", tokenTestNow)
	srv := tokenTestServer(t, st)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenProfileRequest(session.ID))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	assertAuthPage(t, w.Body.String())
	main := assertChrome(t, w.Body.String(), user.Email)
	card := tokenCard(t, main, "MCP clients")
	header := tokenElements(card, "header")[0]
	panel := tokenElements(card, "div")[0]
	tokenAttrs(t, card, map[string]string{"class": "card flush"})
	tokenConsists(t, tokenElement(t, card, "section"), header, panel)
	hs := tokenElements(header, "h2")
	ps := tokenElements(header, "p")
	tokenRead(t, hs[0], "MCP clients")
	tokenRead(t, ps[0], "MCP clients you approve act as you at the MCP gateway. Revoke one to sign it out.")
	tokenConsists(t, tokenElement(t, header, "header"), hs[0], ps[0])
	tokenAttrs(t, panel, map[string]string{"class": "table-scroll"})
	tables := tokenElements(panel, "table")
	if len(tables) != 1 {
		t.Fatal("table count")
	}
	tokenConsists(t, tokenElement(t, panel, "div"), tables[0])
	table := tables[0]
	head := tokenElements(table, "thead")[0]
	body := tokenElements(table, "tbody")[0]
	tokenConsists(t, tokenElement(t, table, "table"), head, body)
	heads := tokenElements(head, "tr")
	if len(heads) != 1 {
		t.Fatal("head row count")
	}
	columns := tokenElements(heads[0], "th")
	if len(columns) != 5 {
		t.Fatal("column count")
	}
	for i, title := range []string{"Client", "Approved", "Last used", "Expires", ""} {
		tokenRead(t, columns[i], title)
	}
	tokenConsists(t, tokenElement(t, head, "thead"), heads[0])
	tokenConsists(t, tokenElement(t, heads[0], "tr"), columns...)
	rows := tokenElements(body, "tr")
	if len(rows) != len(cases) {
		t.Fatalf("rows=%d", len(rows))
	}
	tokenConsists(t, tokenElement(t, body, "tbody"), rows...)
	order := []string{"latest <&\"\t client", "used new", "used old", "never new", "expires now", "never old"}
	expected := mcpClientsData{}
	for i, name := range order {
		token := byName[name]
		cells := tokenElements(rows[i], "td")
		if len(cells) != 5 {
			t.Fatal("cell count")
		}
		tokenConsists(t, tokenElement(t, rows[i], "tr"), cells...)
		tokenRead(t, cells[0], pageText(name))
		tokenAssertTimeCell(t, cells[1], token.CreatedAt, false, tokenTestNow)
		row := mcpClientData{ID: token.ID, Name: token.Name, Approved: tokenDatetime(token.CreatedAt), ApprovedText: tokenMinute(token.CreatedAt), Expires: tokenDatetime(*token.ExpiresAt), ExpiresText: tokenMinute(*token.ExpiresAt), Expired: !token.ExpiresAt.After(tokenTestNow)}
		for _, c := range cases {
			if c.name == name && c.used != nil {
				token.LastUsedAt = c.used
			}
		}
		if token.LastUsedAt == nil {
			tokenAttrs(t, cells[2], map[string]string{"class": "muted"})
			tokenRead(t, cells[2], "Never")
		} else {
			tokenAssertTimeCell(t, cells[2], *token.LastUsedAt, true, tokenTestNow)
			row.LastUsed = tokenDatetime(*token.LastUsedAt)
			row.LastUsedTitle = tokenMinute(*token.LastUsedAt)
			row.LastUsedText = tokenElapsed(tokenTestNow.Sub(*token.LastUsedAt))
		}
		if row.Expired {
			times := tokenElements(cells[3], "time")
			if len(times) != 1 {
				t.Fatal("expired time count")
			}
			tokenConsists(t, tokenElement(t, cells[3], "td"), times[0])
			tokenAttrs(t, times[0], map[string]string{"datetime": row.Expires, "title": row.ExpiresText})
			tokenRead(t, times[0], "Expired")
		} else {
			tokenAssertTimeCell(t, cells[3], *token.ExpiresAt, false, tokenTestNow)
		}
		tokenAttrs(t, cells[4], map[string]string{"class": "row-actions"})
		forms := tokenElements(cells[4], "form")
		if len(forms) != 1 {
			t.Fatal("revoke forms")
		}
		tokenConsists(t, tokenElement(t, cells[4], "td"), forms[0])
		tokenAttrs(t, forms[0], map[string]string{"class": "inline", "method": "post", "action": "/tokens/" + token.ID + "/revoke"})
		buttons := tokenElements(forms[0], "button")
		if len(buttons) != 1 {
			t.Fatal("revoke buttons")
		}
		tokenConsists(t, tokenElement(t, forms[0], "form"), buttons[0])
		tokenAttrs(t, buttons[0], map[string]string{"class": "ghost small", "type": "submit"})
		tokenRead(t, buttons[0], "Revoke")
		expected.Clients = append(expected.Clients, row)
	}
	data, err := srv.profileClients(user.ID, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, expected) {
		t.Fatalf("client data=%#v want %#v", data, expected)
	}
	var render bytes.Buffer
	templates, err := page.Templates().ParseFS(auth.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if err = templates.ExecuteTemplate(&render, "mcp-clients", expected); err != nil {
		t.Fatal(err)
	}
	_, suffix, found := strings.Cut(main, card)
	if !found {
		t.Fatal("client card is absent from profile")
	}
	if strings.Trim(card+suffix, " \t\r\n\f") != strings.Trim(render.String(), " \t\r\n\f") {
		t.Fatal("profile suffix differs from template")
	}
	for _, tag := range pageTags(card) {
		if tag.name == "input" || tag.name == "select" || tag.name == "a" {
			t.Fatalf("unexpected control %s", tag.name)
		}
	}
	if len(tokenElements(card, "form")) != len(cases) || len(tokenElements(card, "button")) != len(cases) {
		t.Fatal("extra controls")
	}
	for _, secret := range append(secrets, personalSecret) {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret on profile")
		}
	}
	if strings.Contains(card, personal.ID) || strings.Contains(card, foreign.ID) {
		t.Fatal("wrong kind/owner client rows")
	}
	api := tokenCard(t, main, "API tokens")
	if !strings.Contains(api, personal.ID) {
		t.Fatal("missing personal row")
	}
	for _, token := range byName {
		if strings.Contains(api, token.ID) {
			t.Fatal("client in personal card")
		}
	}
}

func tokenAssertTimeCell(t *testing.T, cell string, x time.Time, last bool, draw time.Time) {
	t.Helper()
	times := tokenElements(cell, "time")
	if len(times) != 1 {
		t.Fatal("time cell count")
	}
	tokenConsists(t, tokenElement(t, cell, "td"), times[0])
	tokenAttrs(t, times[0], map[string]string{"datetime": tokenDatetime(x)})
	if last {
		tokenAttrs(t, times[0], map[string]string{"title": tokenMinute(x)})
		tokenRead(t, times[0], tokenElapsed(draw.Sub(x)))
	} else {
		tokenRead(t, times[0], tokenMinute(x))
	}
}

func TestProfileKindsHaveIndependentEmptyStates(t *testing.T) {
	// R-5AAA-ZW38 R-62BZ-SM57: each empty panel depends only on its own kind.
	for _, kind := range []string{"neither", "personal", "client"} {
		t.Run(kind, func(t *testing.T) {
			st := openTokenTestStore(t)
			user, session := tokenTestIdentity(t, st, "empty")
			if kind == "personal" {
				if _, _, err := st.CreateToken(user.ID, "personal", store.ExpiryNever, tokenTestNow); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "client" {
				mintProfileClient(t, st, user.ID, "client", tokenTestNow)
			}
			w := httptest.NewRecorder()
			tokenTestServer(t, st).ServeHTTP(w, tokenProfileRequest(session.ID))
			title, heading, message := "MCP clients", "No MCP clients yet", "A client appears here once you approve it from its login."
			if kind == "client" {
				title, heading, message = "API tokens", "No tokens yet", "Create one below when a script or tool needs to act as you."
			}
			card := tokenCard(t, w.Body.String(), title)
			panel := tokenElements(card, "div")[0]
			tokenAttrs(t, panel, map[string]string{"class": "empty"})
			hs := tokenElements(panel, "h3")
			ps := tokenElements(panel, "p")
			if len(hs) != 1 || len(ps) != 1 {
				t.Fatal("empty children")
			}
			tokenRead(t, hs[0], heading)
			tokenRead(t, ps[0], message)
			tokenConsists(t, tokenElement(t, panel, "div"), hs[0], ps[0])
			tokenAssertNoPanel(t, card, "table", "table-scroll")
		})
	}
}

func TestClientTokenRevokeEffectsAndTrail(t *testing.T) {
	// R-5MHA-TLI6 R-5OX3-L4ZK: revoke removes only that approval and records one id-only event.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "revoke")
	target, secret := mintProfileClient(t, st, user.ID, "same client", tokenTestNow)
	mintProfileClient(t, st, user.ID, "same client", tokenTestNow)
	if _, _, err := st.CreateToken(user.ID, "personal", store.ExpiryNever, tokenTestNow); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListTokens(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	req := tokenActionRequest(session.ID, target.ID, "revoke")
	req.Header.Set("X-Request-Id", "0123456789abcdef0123456789abcdef")
	w, events := f.request(t, req)
	if w.Code != 302 || w.Header().Get("Location") != "/" {
		t.Fatalf("revoke=%d %v", w.Code, w.Header())
	}
	assertTokenDomainTrail(t, events, user.ID, "token.revoked", target.ID)
	after, err := st.ListTokens(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var want []store.Token
	for _, token := range before {
		if token.ID != target.ID {
			want = append(want, token)
		}
	}
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("remaining tokens changed: %#v want %#v", after, want)
	}
	if _, err = st.LookupTokenIdentity(secret, "mcp.green.example", tokenTestNow); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked identity=%v", err)
	}
}

func TestTokenActionsEnforceKindsOwnersAndOrigin(t *testing.T) {
	// R-5DY0-57BB R-5NP7-7D8V R-5F5W-IZ20 R-5GDS-WQSP R-5K1I-220S
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "actor")
	other, _ := tokenTestIdentity(t, st, "other-actor")
	client, _ := mintProfileClient(t, st, user.ID, "client", tokenTestNow)
	foreign, _ := mintProfileClient(t, st, other.ID, "foreign", tokenTestNow)
	personal, _, err := st.CreateToken(user.ID, "personal", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	foreignPersonal, _, err := st.CreateToken(other.ID, "foreign personal", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListTokens(user.ID)
	otherBefore, _ := st.ListTokens(other.ID)
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	for _, action := range []string{"enable", "disable", "delete", "revoke"} {
		ids := []string{"tok_00000000000000000000000000", foreignPersonal.ID, client.ID}
		if action == "revoke" {
			ids = []string{"tok_00000000000000000000000000", foreign.ID, personal.ID}
		}
		for _, id := range ids {
			r := tokenActionRequest(session.ID, id, action)
			w, events := f.request(t, r)
			assertTokenPlainResult(t, w, 404)
			assertTokenDomainTrail(t, events, user.ID, "", "")
		}
		for _, origin := range []string{"", "https://wrong.example"} {
			r := tokenActionRequest(session.ID, client.ID, action)
			r.Header.Set("Origin", origin)
			w, events := f.request(t, r)
			assertTokenPlainResult(t, w, 403)
			assertTokenDomainTrail(t, events, user.ID, "", "")
		}
	}
	after, _ := st.ListTokens(user.ID)
	otherAfter, _ := st.ListTokens(other.ID)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(otherBefore, otherAfter) {
		t.Fatal("refusal changed token")
	}
}

func assertTokenPlainResult(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !singlePlainLine(w.Body.String()) {
		t.Fatalf("plain response=%d %v %q", w.Code, w.Header(), w.Body.String())
	}
}
func assertTokenDomainTrail(t *testing.T, events []telemetry.Event, user, name, id string) {
	t.Helper()
	want := []string{"request.started", "request.finished"}
	if name != "" {
		want = []string{"request.started", name, "request.finished"}
	}
	var got []string
	for _, event := range events {
		got = append(got, event.Name)
		if event.RequestID != events[0].RequestID {
			t.Fatal("request ids differ")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event names=%v want %v", got, want)
	}
	if name != "" {
		e := events[1]
		if e.User != user || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"token": id}) {
			t.Fatalf("domain event=%#v", e)
		}
	}
}

func TestPersonalTokenActionsTrailChangesOnly(t *testing.T) {
	// R-5HLP-AIJE R-5ITL-OAA3 R-5K1I-220S: personal toggles record only state changes; delete one event.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "personal-trail")
	token, _, err := st.CreateToken(user.ID, "personal", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	for _, tc := range []struct{ action, event string }{{"enable", ""}, {"disable", "token.disabled"}, {"disable", ""}, {"enable", "token.enabled"}, {"enable", ""}, {"delete", "token.deleted"}} {
		r := tokenActionRequest(session.ID, token.ID, tc.action)
		w, events := f.request(t, r)
		if w.Code != 302 || w.Header().Get("Location") != "/" {
			t.Fatal("personal action")
		}
		assertTokenDomainTrail(t, events, user.ID, tc.event, token.ID)
	}
}

func TestClientRevokeFailingStoreHasNoTokenTrail(t *testing.T) {
	// R-5K1I-220S: a failed store changes no token and adds no domain event.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "failure")
	client, _ := mintProfileClient(t, st, user.ID, "client", tokenTestNow)
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	failServerStore(t, st)
	r := tokenActionRequest(session.ID, client.ID, "revoke")
	w, events := f.request(t, r)
	assertTokenPlainResult(t, w, 500)
	assertTokenDomainTrail(t, events, user.ID, "", "")
}

func TestRejectedTokenCreationHasNoTokenTrail(t *testing.T) {
	// R-5K1I-220S: rejected create submissions, origin refusals and failed-store requests have only request events.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "create-refusal")
	f := newTrail(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }}, nil)
	invalid := tokenRequest("/tokens", session.ID, url.Values{"name": {""}, "expires": {"wrong"}})
	w, events := f.request(t, invalid)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	assertTokenDomainTrail(t, events, user.ID, "", "")
	for _, origin := range []string{"", "https://wrong.example"} {
		r := tokenRequest("/tokens", session.ID, url.Values{"name": {"accepted"}, "expires": {"90d"}})
		r.Header.Set("Origin", origin)
		w, events = f.request(t, r)
		assertTokenPlainResult(t, w, 403)
		assertTokenDomainTrail(t, events, user.ID, "", "")
	}
	tokens, err := st.ListTokens(user.ID)
	if err != nil || len(tokens) != 0 {
		t.Fatalf("refused creates=%#v %v", tokens, err)
	}
	failServerStore(t, st)
	r := tokenRequest("/tokens", session.ID, url.Values{"name": {"accepted"}, "expires": {"90d"}})
	w, events = f.request(t, r)
	assertTokenPlainResult(t, w, 500)
	assertTokenDomainTrail(t, events, user.ID, "", "")
}
