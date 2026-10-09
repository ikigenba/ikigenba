package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func tokenProfileRequest(session string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Host = "localhost:3001"
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return r
}

func TestTokenRefusalBodiesAreSingleLines(t *testing.T) {
	// R-5GDS-WQSP: all mutation origin refusals and nonexistent/foreign action targets are single plain-text lines.
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
		tokenAssertPlainLine(t, tokenTestServer(t, st), r, http.StatusForbidden)
	}
	for _, action := range []string{"enable", "disable", "delete"} {
		for _, id := range []string{token.ID, "missing"} {
			tokenAssertPlainLine(t, tokenTestServer(t, st), tokenActionRequest(session.ID, id, action), http.StatusNotFound)
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

func expectedElapsed(d time.Duration) elapsedData {
	switch {
	case d < time.Minute:
		return elapsedData{"now", 0}
	case d < time.Hour:
		return elapsedData{"minute", int64(d / time.Minute)}
	case d < 24*time.Hour:
		return elapsedData{"hour", int64(d / time.Hour)}
	default:
		return elapsedData{"day", int64(d / (24 * time.Hour))}
	}
}

func TestTokenDataDeclarations(_ *testing.T) {
	// R-7DZ3-U9KX R-7F70-81BM R-7GEW-LT2B R-7HMS-ZKT0 R-7IUP-DCJP R-7K2L-R4AE R-7LAI-4W13 R-7MIE-INRS
	// Assignment to the declared underlying structs checks names, types and order.
	var _ struct {
		ID, Name string
		Created  tokenTimeData
		LastUsed *tokenLastUsedData
		Expires  *tokenTimeData
		Enabled  bool
	} = tokenRowData{}
	var _ struct{ Datetime, Text string } = tokenTimeData{}
	var _ struct {
		Datetime, Title string
		Elapsed         elapsedData
	} = tokenLastUsedData{}
	var _ struct {
		Unit  string
		Count int64
	} = elapsedData{}
	var _ struct {
		Name, Expiry                     string
		Rejected, NameError, ExpiryError bool
	} = tokenCreateData{}
	var _ struct{ Name, Secret string } = tokenCreatedData{}
	var _ struct{ Clients []mcpClientData } = mcpClientsData{}
	var _ struct {
		ID, Name, Approved, ApprovedText, LastUsed, LastUsedTitle string
		LastUsedElapsed                                           elapsedData
		Expires, ExpiresText                                      string
		Expired                                                   bool
	} = mcpClientData{}
}

func TestTokenElapsedBoundaries(t *testing.T) {
	// R-7OY7-A796
	cases := []struct {
		duration time.Duration
		want     elapsedData
	}{
		{-time.Hour, elapsedData{"now", 0}}, {0, elapsedData{"now", 0}},
		{time.Minute - time.Nanosecond, elapsedData{"now", 0}}, {time.Minute, elapsedData{"minute", 1}},
		{2 * time.Minute, elapsedData{"minute", 2}}, {time.Hour - time.Nanosecond, elapsedData{"minute", 59}},
		{time.Hour, elapsedData{"hour", 1}}, {2 * time.Hour, elapsedData{"hour", 2}},
		{24*time.Hour - time.Nanosecond, elapsedData{"hour", 23}}, {24 * time.Hour, elapsedData{"day", 1}},
		{48 * time.Hour, elapsedData{"day", 2}}, {365 * 24 * time.Hour, elapsedData{"day", 365}},
		{time.Duration(1<<63 - 1), elapsedData{"day", 106751}},
	}
	for _, tc := range cases {
		if got := tokenElapsedData(tc.duration); got != tc.want {
			t.Errorf("elapsed(%s)=%#v want %#v", tc.duration, got, tc.want)
		}
	}
}

func TestTokenTimeValues(t *testing.T) {
	// R-7NQA-WFIH
	at := time.Date(2026, 2, 3, 4, 5, 6, 987654321, time.FixedZone("offset", 5400))
	want := tokenTimeData{"2026-02-03T02:35:06Z", "2026-02-03 02:35 UTC"}
	if got := tokenTime(at); got != want {
		t.Fatalf("token time=%#v want %#v", got, want)
	}
}

func expectedTokenRow(token store.Token, draw time.Time) tokenRowData {
	row := tokenRowData{ID: token.ID, Name: token.Name, Created: tokenTimeData{token.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), token.CreatedAt.UTC().Format("2006-01-02 15:04 UTC")}, Enabled: token.Enabled}
	if token.LastUsedAt != nil {
		row.LastUsed = &tokenLastUsedData{token.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z"), token.LastUsedAt.UTC().Format("2006-01-02 15:04 UTC"), expectedElapsed(draw.Sub(*token.LastUsedAt))}
	}
	if token.ExpiresAt != nil {
		row.Expires = &tokenTimeData{token.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), token.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC")}
	}
	return row
}

func TestTokenProfileValuesAndOrder(t *testing.T) {
	// R-7Q63-NYZV R-592E-M4CJ
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "profile-rows")
	used := tokenTestNow.Add(-2 * time.Hour)
	type fixture struct {
		name    string
		created time.Time
		used    *time.Time
	}
	cases := []fixture{
		{"never-old", tokenTestNow.Add(-48 * time.Hour), nil},
		{"never-new", tokenTestNow.Add(-24 * time.Hour), nil},
		{"used-old", tokenTestNow.Add(-48 * time.Hour), &used},
		{"used-new", tokenTestNow.Add(-24 * time.Hour), &used},
		{"latest", tokenTestNow.Add(-time.Hour), timePointer(tokenTestNow.Add(-time.Minute))},
	}
	byName := map[string]store.Token{}
	for _, c := range cases {
		token, secret, err := st.CreateToken(user.ID, c.name, store.Expiry90d, c.created.In(time.FixedZone("offset", 3600)))
		if err != nil {
			t.Fatal(err)
		}
		if c.used != nil {
			if _, err := st.TouchTokenIdentity(secret, "mcp.green.example", *c.used); err != nil {
				t.Fatal(err)
			}
			token.LastUsedAt = c.used
		}
		byName[c.name] = token
	}

	// Include a disabled never-expiring row, preserving the unaltered name.
	extra, _, err := st.CreateToken(user.ID, "never-expiring <&\x00", store.ExpiryNever, tokenTestNow.Add(-72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTokenEnabled(user.ID, extra.ID, false); err != nil {
		t.Fatal(err)
	}
	extra.Enabled = false
	byName[extra.Name] = extra
	mintProfileClient(t, st, user.ID, "other-kind", tokenTestNow)
	other, _ := tokenTestIdentity(t, st, "other-profile")
	if _, _, err := st.CreateToken(other.ID, "other-owner", store.ExpiryNever, tokenTestNow); err != nil {
		t.Fatal(err)
	}
	order := []string{"latest", "used-new", "used-old", "never-new", "never-old", extra.Name}
	wantRows := make([]tokenRowData, 0, len(order))
	for _, name := range order {
		wantRows = append(wantRows, expectedTokenRow(byName[name], tokenTestNow))
	}
	srv := tokenTestServer(t, st)
	rows, err := srv.tokenRows(user.ID, tokenTestNow)
	if err != nil || !reflect.DeepEqual(rows, wantRows) {
		t.Fatalf("rows=%#v err=%v want %#v", rows, err, wantRows)
	}
	w := httptest.NewRecorder()
	tokenTestServer(t, st).ServeHTTP(w, tokenProfileRequest(session.ID))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	body, previous := w.Body.String(), -1
	if !strings.Contains(body, expectedAuthTemplate(t, "tokenList", wantRows)) {
		t.Fatal("profile omits token rows template output")
	}
	for _, name := range order {
		token := byName[name]
		assertPageValues(t, body, token.ID, token.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), token.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"))
		if token.LastUsedAt != nil {
			assertPageValues(t, body, token.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
		at := strings.Index(body, token.ID)
		if at <= previous {
			t.Fatal("profile token order")
		}
		previous = at
	}
}

func TestTokenRejectedPageValues(t *testing.T) {
	// R-VV5F-YR1P R-3U9G-MQVA R-3T1K-8Z4L
	for _, tc := range []struct{ name, expiry string }{
		{"", "never"}, {" \t\n ", "30d"}, {strings.Repeat("界", 65), "90d"},
		{"  retained name  ", "bad"}, {"kept", ""}, {"", "365d"}, {"", "bad"},
	} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "rejected")
		calls := 0
		banner := testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"})
		srv := newTestServer(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }, Banner: func(got page.User) page.Banner {
			calls++
			if got.Email != user.Email || got.ProfileURL != "/" || got.LogoutURL != "/logout" {
				t.Fatalf("banner user=%#v", got)
			}
			return banner
		}})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {tc.name}, "expires": {tc.expiry}}))
		if calls != 1 {
			t.Fatalf("banner calls=%d", calls)
		}
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		expiry := tc.expiry
		valid := expiry == "never" || expiry == "30d" || expiry == "90d" || expiry == "365d"
		if !valid {
			expiry = "90d"
		}
		count := utf8.RuneCountInString(strings.TrimSpace(tc.name))
		data := tokenCreateData{tc.name, expiry, true, count < 1 || count > 64, !valid}
		if got := tokenCreateValues(tc.name, tc.expiry, true); got != data {
			t.Fatalf("rejected data=%#v want %#v", got, data)
		}
		assertAuthTemplate(t, w.Body.String(), "page", authPageData{Banner: banner, Create: &data})
	}
}

func TestTokenCreatedPageAndSecretLifetime(t *testing.T) {
	// R-VXL8-QAJ3 R-3VHD-0ILZ R-W011-HU0H
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "created")
	calls := 0
	banner := testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"})
	srv := newTestServer(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }, Banner: func(got page.User) page.Banner {
		calls++
		if got.Email != user.Email || got.ProfileURL != "/" || got.LogoutURL != "/logout" {
			t.Fatalf("banner user=%#v", got)
		}
		return banner
	}})
	name := "  fixture-token-name  "
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {name}, "expires": {"never"}}))
	if calls != 1 {
		t.Fatalf("banner calls=%d", calls)
	}
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	assertPageBanner(t, w.Body.String(), testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}))
	assertPageValues(t, w.Body.String(), strings.TrimSpace(name))
	tokens, err := st.ListTokens(user.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens=%v %v", tokens, err)
	}
	secret, ok := presentedSecret(w.Body.String(), tokens[0].Hash)
	if !ok || strings.Count(w.Body.String(), secret) != 1 {
		t.Fatal("secret missing or repeated")
	}
	assertAuthTemplate(t, w.Body.String(), "page", authPageData{Banner: banner, Created: &tokenCreatedData{Name: strings.TrimSpace(name), Secret: secret}})
	for _, r := range []*http.Request{
		tokenProfileRequest(session.ID), tokenActionRequest(session.ID, tokens[0].ID, "disable"),
		tokenRequest("/tokens", session.ID, url.Values{"name": {""}, "expires": {"never"}}),
		tokenActionRequest(session.ID, tokens[0].ID, "enable"), tokenActionRequest(session.ID, tokens[0].ID, "delete"),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
	} {
		later := httptest.NewRecorder()
		srv.ServeHTTP(later, r)
		if strings.Contains(later.Body.String(), secret) {
			t.Fatal("later response leaks secret")
		}
	}
	// Request values and stored names may themselves contain an earlier secret.
	later := httptest.NewRecorder()
	srv.ServeHTTP(later, tokenRequest("/tokens", session.ID, url.Values{"name": {secret}, "expires": {"bad"}}))
	assertPageValues(t, later.Body.String(), secret)
	if _, _, err := st.CreateToken(user.ID, secret, store.ExpiryNever, tokenTestNow); err != nil {
		t.Fatal(err)
	}
	later = httptest.NewRecorder()
	srv.ServeHTTP(later, tokenProfileRequest(session.ID))
	if strings.Count(later.Body.String(), secret) != 1 {
		t.Fatal("secret-shaped name omitted or repeated")
	}
}

func TestProfileUsesOneActualDrawTime(t *testing.T) {
	// R-PWVP-KA8B
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "common-clock")
	used := []time.Time{tokenTestNow.Add(-3 * time.Minute), tokenTestNow.Add(-7 * time.Minute)}
	for i, at := range used {
		_, secret, err := st.CreateToken(user.ID, fmt.Sprintf("clock-%d", i), "never", tokenTestNow.Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.TouchTokenIdentity(secret, "mcp.green.example", at); err != nil {
			t.Fatal(err)
		}
	}
	client, secret := mintProfileClient(t, st, user.ID, "clock-client", tokenTestNow.Add(-store.ClientTokenTTL+time.Minute))
	clientUsed := tokenTestNow.Add(-11 * time.Minute)
	if _, err := st.TouchTokenIdentity(secret, "mcp.green.example", clientUsed); err != nil {
		t.Fatal(err)
	}
	var readings []time.Time
	srv := newTestServer(t, Config{Store: st, Banner: testPageBanner, Now: func() time.Time {
		reading := tokenTestNow.Add(time.Duration(len(readings)) * 2 * time.Minute)
		readings = append(readings, reading)
		return reading
	}})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenProfileRequest(session.ID))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, draw := range readings {
		row := mcpClientData{ID: client.ID, Name: client.Name, Approved: client.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), ApprovedText: client.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), LastUsed: clientUsed.UTC().Format("2006-01-02T15:04:05Z"), LastUsedTitle: clientUsed.UTC().Format("2006-01-02 15:04 UTC"), LastUsedElapsed: expectedElapsed(draw.Sub(clientUsed)), Expires: client.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), ExpiresText: client.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"), Expired: !client.ExpiresAt.After(draw)}
		common := strings.Contains(w.Body.String(), expectedAuthTemplate(t, "mcp-clients", mcpClientsData{Clients: []mcpClientData{row}}))
		for _, at := range used {
			expected := tokenLastUsedData{at.UTC().Format("2006-01-02T15:04:05Z"), at.UTC().Format("2006-01-02 15:04 UTC"), expectedElapsed(draw.Sub(at))}
			common = common && strings.Contains(w.Body.String(), expectedAuthTemplate(t, "tokenLastUsed", expected))
		}
		if common {
			return
		}
	}
	t.Fatal("profile values do not share any actual injected draw time")
}

func TestTokenCreateDataInputs(t *testing.T) {
	// R-3T1K-8Z4L R-3Z52-5TU2
	empty := tokenCreateData{Expiry: "90d"}
	if got := tokenCreateValues("", "", false); got != empty {
		t.Fatalf("empty create=%#v", got)
	}
	cases := []struct {
		name, expiry string
		want         tokenCreateData
	}{
		{" \u2003\u00a0 ", "never", tokenCreateData{" \u2003\u00a0 ", "never", true, true, false}},
		{strings.Repeat("界", 64), "30d", tokenCreateData{strings.Repeat("界", 64), "30d", true, false, false}},
		{strings.Repeat("界", 65), "365d", tokenCreateData{strings.Repeat("界", 65), "365d", true, true, false}},
		{" \xff ", "invalid", tokenCreateData{" \xff ", "90d", true, false, true}},
		{strings.Repeat("\xff", 65), "90d", tokenCreateData{strings.Repeat("\xff", 65), "90d", true, true, false}},
	}
	for _, tc := range cases {
		if got := tokenCreateValues(tc.name, tc.expiry, true); got != tc.want {
			t.Errorf("data=%#v want %#v", got, tc.want)
		}
	}
	// ParseForm chooses the first body value ahead of the query's value.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "form-precedence")
	req := tokenRequest("/tokens?name=query-name&expires=never", session.ID, url.Values{"name": {"  preserved first  ", "second"}, "expires": {"bad", "30d"}})
	w := httptest.NewRecorder()
	tokenTestServer(t, st).ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	data := tokenCreateData{"  preserved first  ", "90d", true, false, true}
	assertAuthTemplate(t, w.Body.String(), "page", authPageData{Banner: testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}), Create: &data})
}

func TestTokenUnicodeAndInvalidUTF8Trimming(t *testing.T) {
	// R-3Z52-5TU2
	for _, tc := range []struct{ name, want string }{
		{"\u2003\u00a0" + strings.Repeat("界", 64) + "\u202f", strings.Repeat("界", 64)},
		{" \xff ", "\xff"},
		{" \xff\u2003x\u00a0 ", "\xff\u2003x"},
	} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "unicode-name")
		w := httptest.NewRecorder()
		tokenTestServer(t, st).ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {tc.name}, "expires": {"never"}}))
		tokens, err := st.ListTokens(user.ID)
		if w.Code != 200 || err != nil || len(tokens) != 1 || tokens[0].Name != tc.want {
			t.Fatalf("status=%d tokens=%#v err=%v want name %q", w.Code, tokens, err, tc.want)
		}
	}
}

func TestProfileDataTokenActionRoutes(t *testing.T) {
	// R-3WP9-EACO
	for _, enabled := range []bool{true, false} {
		for _, action := range []string{"toggle", "delete", "revoke"} {
			st := openTokenTestStore(t)
			user, session := tokenTestIdentity(t, st, "data-routes")
			personal, _, err := st.CreateToken(user.ID, "data-personal", store.ExpiryNever, tokenTestNow)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetTokenEnabled(user.ID, personal.ID, enabled); err != nil {
				t.Fatal(err)
			}
			client, _ := mintProfileClient(t, st, user.ID, "data-client", tokenTestNow)
			srv := tokenTestServer(t, st)
			rows, err := srv.tokenRows(user.ID, tokenTestNow)
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows=%#v %v", rows, err)
			}
			clients, err := srv.profileClients(user.ID, tokenTestNow)
			if err != nil || len(clients.Clients) != 1 {
				t.Fatalf("clients=%#v %v", clients, err)
			}
			id, verb := rows[0].ID, action
			if action == "toggle" {
				verb = "enable"
				if rows[0].Enabled {
					verb = "disable"
				}
			}
			if action == "revoke" {
				id = clients.Clients[0].ID
			}
			before, err := st.ListTokens(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, tokenRequest("/tokens/"+id+"/"+verb, session.ID, nil))
			if w.Code != 302 || w.Header().Get("Location") != "/" {
				t.Fatalf("route=%d %v", w.Code, w.Header())
			}
			after, err := st.ListTokens(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			var want []store.Token
			for _, token := range before {
				if token.ID == id {
					if action != "toggle" {
						continue
					}
					token.Enabled = !enabled
				}
				want = append(want, token)
			}
			if !reflect.DeepEqual(after, want) {
				t.Fatalf("action=%s after=%#v want %#v", action, after, want)
			}
			if personal.ID != rows[0].ID || client.ID != clients.Clients[0].ID {
				t.Fatal("data IDs do not name supplied tokens")
			}
		}
	}
}
