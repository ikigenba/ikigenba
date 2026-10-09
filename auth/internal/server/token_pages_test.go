package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth"
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

func expectedElapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return ElapsedJustNow
	case d < 2*time.Minute:
		return ElapsedMinute
	case d < time.Hour:
		return fmt.Sprintf(ElapsedMinutes, int64(d/time.Minute))
	case d < 2*time.Hour:
		return ElapsedHour
	case d < 24*time.Hour:
		return fmt.Sprintf(ElapsedHours, int64(d/time.Hour))
	case d < 48*time.Hour:
		return ElapsedDay
	default:
		return fmt.Sprintf(ElapsedDays, int64(d/(24*time.Hour)))
	}
}

func TestElapsedCopyConstants(t *testing.T) {
	// R-PKOP-QKTD
	for _, text := range []string{ElapsedJustNow, ElapsedMinute, ElapsedHour, ElapsedDay} {
		if text == "" {
			t.Fatal("empty elapsed copy")
		}
	}
	for _, format := range []string{ElapsedMinutes, ElapsedHours, ElapsedDays} {
		if strings.Count(format, "%d") != 1 || strings.Contains(strings.ReplaceAll(format, "%d", ""), "%") {
			t.Fatalf("invalid elapsed format %q", format)
		}
	}
}

func TestTokenElapsedBoundaries(t *testing.T) {
	// R-PDDB-FYD7
	for _, d := range []time.Duration{-time.Hour, 0, time.Minute - time.Nanosecond, time.Minute, 2 * time.Minute, time.Hour - time.Nanosecond, time.Hour, 2 * time.Hour, 24*time.Hour - time.Nanosecond, 24 * time.Hour, 48 * time.Hour, 365 * 24 * time.Hour} {
		if got, want := tokenElapsed(d), expectedElapsed(d); got != want {
			t.Errorf("elapsed(%s)=%q want %q", d, got, want)
		}
	}
}

func TestTokenProfileValuesAndOrder(t *testing.T) {
	// R-VRHQ-TFTM R-T8UC-H064 R-592E-M4CJ R-VSPN-77KB
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
	w := httptest.NewRecorder()
	tokenTestServer(t, st).ServeHTTP(w, tokenProfileRequest(session.ID))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	body, previous := w.Body.String(), -1
	for _, name := range []string{"latest", "used-new", "used-old", "never-new", "never-old"} {
		token := byName[name]
		assertPageValues(t, body, token.ID, token.Name, token.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), token.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"))
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
	// R-VV5F-YR1P R-VWDC-CISE
	for _, tc := range []struct{ name, expiry string }{
		{"", "never"}, {" \t\n ", "30d"}, {strings.Repeat("界", 65), "90d"},
		{"  retained name  ", "bad"}, {"kept", ""},
	} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "rejected")
		w := httptest.NewRecorder()
		tokenTestServer(t, st).ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {tc.name}, "expires": {tc.expiry}}))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		assertPageValues(t, w.Body.String(), tc.name)
		assertPageBanner(t, w.Body.String(), testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}))
	}
}

func TestTokenCreatedPageAndSecretLifetime(t *testing.T) {
	// R-VXL8-QAJ3 R-VYT5-429S R-W011-HU0H
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "created")
	srv := tokenTestServer(t, st)
	name := "  fixture-token-name  "
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {name}, "expires": {"never"}}))
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
	// R-PWVP-KA8B R-PDDB-FYD7
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
	templates, err := page.Templates().ParseFS(auth.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, draw := range readings {
		row := mcpClientData{ID: client.ID, Name: client.Name, Approved: client.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), ApprovedText: client.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), LastUsed: clientUsed.UTC().Format("2006-01-02T15:04:05Z"), LastUsedTitle: clientUsed.UTC().Format("2006-01-02 15:04 UTC"), LastUsedText: expectedElapsed(draw.Sub(clientUsed)), Expires: client.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), ExpiresText: client.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"), Expired: !client.ExpiresAt.After(draw)}
		var expected strings.Builder
		if err := templates.ExecuteTemplate(&expected, "mcp-clients", mcpClientsData{Clients: []mcpClientData{row}}); err != nil {
			t.Fatal(err)
		}
		common := strings.Contains(w.Body.String(), expected.String())
		for _, at := range used {
			common = common && strings.Contains(w.Body.String(), expectedElapsed(draw.Sub(at)))
		}
		if common {
			return
		}
	}
	t.Fatal("profile values do not share any actual injected draw time")
}
