package server

import (
	"net/http"
	"strings"
	"testing"

	"dashboard/internal/googleidp"
)

// TestIndexLoggedIn: a request carrying a live session cookie renders the
// identity-aware page — the owner's email and a POST /logout control, and not
// the logged-out sign-in link.
func TestIndexLoggedIn(t *testing.T) {
	srv := testServer(t)
	sess := liveSession(t, srv)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/", map[string]string{"Cookie": sess.Name + "=" + sess.Value})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, googleidp.StubIdentity.Email) {
		t.Errorf("logged-in index missing owner email %q:\n%s", googleidp.StubIdentity.Email, body)
	}
	if !strings.Contains(body, `action="/logout"`) {
		t.Errorf("logged-in index missing logout control:\n%s", body)
	}
	if strings.Contains(body, `href="/login"`) {
		t.Errorf("logged-in index still shows sign-in link:\n%s", body)
	}
}

// TestIndexLoggedOut: no cookie renders the logged-out landing — the sign-in
// link, no owner.
func TestIndexLoggedOut(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/login/google"`) || !strings.Contains(body, `href="/login/github"`) {
		t.Errorf("logged-out index missing sign-in link:\n%s", body)
	}
	if strings.Contains(body, `action="/logout"`) {
		t.Errorf("logged-out index shows a logout control:\n%s", body)
	}
}

func TestIndexLoggedOutShowsBrandTitle(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	wall := signinWallMain(t, rec.Body.String())

	// R-JA3I-IY1F
	if got := strings.Count(wall, `<h1 class="signin-title">Ikigenba</h1>`); got != 1 {
		t.Errorf("brand title count = %d, want 1:\n%s", got, wall)
	}
	if strings.Contains(wall, `<h1>Sign in to access your services.</h1>`) {
		t.Errorf("sign-in lede remains the heading:\n%s", wall)
	}

	cssRec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	rule := cssRule(t, cssRec.Body.String(), `.signin-wall .signin-title`)
	for _, want := range []string{`font-family: var(--font-display);`, `font-size: var(--text-h1-size);`, `line-height: var(--text-h1-lh);`, `font-weight: var(--text-h1-weight);`} {
		if !strings.Contains(rule, want) {
			t.Errorf("brand title CSS missing %q:\n%s", want, rule)
		}
	}
}

func TestIndexLoggedOutShowsTaglineThenLede(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	wall := signinWallMain(t, rec.Body.String())
	wantOrder := `<h1 class="signin-title">Ikigenba</h1>
    <p class="signin-tagline">The place life actually happens.</p>
    <p class="signin-lede">Sign in to access your services.</p>
    <hr class="signin-rule">`
	if !strings.Contains(wall, wantOrder) {
		t.Errorf("title, tagline, lede, and rule are not consecutive:\n%s", wall)
	}
	cssRec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	taglineRule := cssRule(t, cssRec.Body.String(), `.signin-wall .signin-tagline`)
	// R-JCJB-AHIT
	for _, want := range []string{`font-family: var(--font-display);`, `font-size: var(--text-h3-size);`, `font-weight: var(--text-h3-weight);`} {
		if !strings.Contains(taglineRule, want) {
			t.Errorf("tagline CSS missing %q:\n%s", want, taglineRule)
		}
	}
	ledeRule := cssRule(t, cssRec.Body.String(), `.signin-wall .signin-lede`)
	// R-JDR7-O99I
	for _, want := range []string{`font-size: var(--text-small-size);`, `line-height: var(--text-small-lh);`} {
		if !strings.Contains(ledeRule, want) {
			t.Errorf("lede CSS missing %q:\n%s", want, ledeRule)
		}
	}
	if strings.Contains(ledeRule, `color:`) {
		t.Errorf("lede CSS declares its own color:\n%s", ledeRule)
	}
}

func TestIndexLoggedOutShowsRuleImmediatelyBeforeCTA(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	wall := signinWallMain(t, rec.Body.String())
	adjacent := `<hr class="signin-rule">
    <a href="/login/google" class="btn btn-primary btn-lg btn-accent-link">Sign in with Google</a>`
	if !strings.Contains(wall, adjacent) {
		t.Errorf("sign-in rule is not immediately before unchanged CTA:\n%s", wall)
	}
	google := `<a href="/login/google" class="btn btn-primary btn-lg btn-accent-link">Sign in with Google</a>`
	github := `<a href="/login/github" class="btn btn-primary btn-lg btn-accent-link">Sign in with GitHub</a>`
	if got := strings.Count(wall, `<a `); got != 2 {
		t.Errorf("sign-in anchor count = %d, want exactly 2:\n%s", got, wall)
	}
	if strings.Count(wall, google) != 1 || strings.Count(wall, github) != 1 || !strings.Contains(wall, google+"\n    "+github) {
		t.Errorf("provider anchors are not adjacent equal peers in Google/GitHub order:\n%s", wall)
	}
	if strings.Contains(wall, `href="/login"`) {
		t.Errorf("retired chooser-less login anchor remains:\n%s", wall)
	}
	cssRec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	// R-ILDB-6TGS
	rule := cssRule(t, cssRec.Body.String(), `.signin-wall .signin-rule`)
	if !strings.Contains(rule, `border-top: var(--border-width) solid var(--color-border);`) {
		t.Errorf("sign-in rule lacks the required hairline:\n%s", rule)
	}
}

func TestIndexLoggedOutRulesBracketCTAWithoutMargins(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	wall := signinWallMain(t, rec.Body.String())
	bracketed := `<hr class="signin-rule">
    <a href="/login/google" class="btn btn-primary btn-lg btn-accent-link">Sign in with Google</a>
    <a href="/login/github" class="btn btn-primary btn-lg btn-accent-link">Sign in with GitHub</a>
    <hr class="signin-rule">
    <aside class="name-origin" aria-label="What ikigenba means">`

	// R-IML7-KL7H
	if got := strings.Count(wall, `class="signin-rule"`); got != 2 {
		t.Errorf("sign-in rule count = %d, want 2:\n%s", got, wall)
	}
	if got := strings.Count(wall, `<hr class="signin-rule">`); got != 2 {
		t.Errorf("margin-free sign-in rule markup count = %d, want 2:\n%s", got, wall)
	}
	if !strings.Contains(wall, bracketed) {
		t.Errorf("two sign-in rules do not immediately bracket the CTA before the name-origin aside:\n%s", wall)
	}

	cssRec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	rule := cssRule(t, cssRec.Body.String(), `.signin-wall .signin-rule`)
	if got := strings.Count(rule, `margin:`); got != 1 || !strings.Contains(rule, `margin: 0;`) {
		t.Errorf("sign-in rule CSS margin must be exactly zero:\n%s", rule)
	}
	wallRule := cssRule(t, cssRec.Body.String(), `.signin-wall`)
	if !strings.Contains(wallRule, `gap: var(--space-6);`) {
		t.Errorf("sign-in wall does not provide the rules' spacing with the standard gap:\n%s", wallRule)
	}
}

func TestIndexLoggedOutShowsBorderlessEtymologyTable(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	aside := nameOriginAside(t, rec.Body.String())
	wantTable := `<table class="name-origin-table">
        <tbody>
          <tr>
            <td class="name-origin-word"><b class="seam">iki</b></td>
            <td class="name-origin-kanji" lang="ja">生き</td>
            <td class="name-origin-gloss">to live</td>
          </tr>
          <tr>
            <td class="name-origin-word"><b class="seam">genba</b></td>
            <td class="name-origin-kanji" lang="ja">現場</td>
            <td class="name-origin-gloss">the place</td>
          </tr>`
	// R-JG70-FSQW
	if !strings.Contains(aside, wantTable) || strings.Count(aside, `<tr>`) != 2 || strings.Count(aside, `<td class=`) != 6 {
		t.Errorf("name-origin table does not contain exactly the required two-by-three content:\n%s", aside)
	}
	for _, retired := range []string{"<dt", "<dd", "name-origin-parts"} {
		if strings.Contains(aside, retired) {
			t.Errorf("name-origin contains retired %q markup:\n%s", retired, aside)
		}
	}

	cssRec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	tableRule := cssRule(t, cssRec.Body.String(), `.name-origin-table`)
	cellRule := cssRule(t, cssRec.Body.String(), `.name-origin-table td`)
	if !strings.Contains(tableRule, `margin: 0 auto var(--space-3);`) || !strings.Contains(cellRule, `text-align: left;`) {
		t.Errorf("table is not block-centered with left-aligned cells:\n%s\n%s", tableRule, cellRule)
	}
	// R-JHEW-TKHL
	for _, rule := range []string{tableRule, cellRule} {
		for _, forbidden := range []string{"\n  border:", "border-color:", "border-style:", "border-width:"} {
			if strings.Contains(rule, forbidden) {
				t.Errorf("name-origin table CSS contains forbidden border property %q:\n%s", forbidden, rule)
			}
		}
	}
}

func TestIndexLoggedOutShowsPronunciationAfterTable(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	aside := nameOriginAside(t, rec.Body.String())
	say := `<p class="name-origin-say">pronounced <b>EE-kee-GEN-buh</b></p>`

	// R-O7K1-XEN7
	if strings.Count(aside, `<p class="name-origin-say">`) != 1 || !strings.HasSuffix(aside, say+"\n    </aside>") {
		t.Errorf("pronunciation is not the single final element after the table:\n%s", aside)
	}
	cssRec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	wantRule := `.name-origin .name-origin-say {
  max-width: none;
  margin: var(--space-3) 0 0;
  font-size: var(--text-small-size);
  line-height: var(--text-small-lh);
  color: var(--color-text-subtle);
  text-align: center;
}`
	if got := cssRule(t, cssRec.Body.String(), `.name-origin .name-origin-say`); got != wantRule {
		t.Errorf("pronunciation CSS changed:\ngot:\n%s\nwant:\n%s", got, wantRule)
	}
}

func TestIndexLoggedInOmitsLoginComposition(t *testing.T) {
	srv := testServer(t)
	sess := liveSession(t, srv)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/", map[string]string{"Cookie": sess.Name + "=" + sess.Value})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// R-DB19-LAND
	for _, forbidden := range []string{"name-origin-table", "name-origin-say", "signin-title", "signin-tagline", "signin-lede", "signin-rule"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("logged-in index includes logged-out class %q:\n%s", forbidden, body)
		}
	}
	for _, want := range []string{
		`<main class="page">`,
		`<h2>Connect your agent</h2>`,
		`/install/claude`,
		`/install/codex`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("logged-in index missing dashboard landing fragment %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, googleidp.StubIdentity.Email) {
		t.Errorf("logged-in index missing owner email %q:\n%s", googleidp.StubIdentity.Email, body)
	}
}

func nameOriginAside(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `<aside class="name-origin"`)
	if start < 0 {
		t.Fatalf("body missing name-origin aside:\n%s", body)
	}
	end := strings.Index(body[start:], `</aside>`)
	if end < 0 {
		t.Fatalf("name-origin aside is not closed:\n%s", body[start:])
	}
	return body[start : start+end+len(`</aside>`)]
}

func signinWallMain(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `<main class="signin-wall">`)
	if start < 0 {
		t.Fatalf("body missing sign-in wall:\n%s", body)
	}
	end := strings.Index(body[start:], `</main>`)
	if end < 0 {
		t.Fatalf("sign-in wall is not closed:\n%s", body[start:])
	}
	return body[start : start+end+len(`</main>`)]
}

func TestIndexLoggedOutKeepsLandingOnly(t *testing.T) {
	srv := testServer(t)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// R-DB02-LND7
	if !strings.Contains(body, `href="/login/google"`) || !strings.Contains(body, `href="/login/github"`) {
		t.Errorf("logged-out landing missing sign-in link:\n%s", body)
	}
	if strings.Contains(body, googleidp.StubIdentity.Email) {
		t.Errorf("logged-out landing leaked owner email:\n%s", body)
	}
	if strings.Contains(body, `action="/pat"`) {
		t.Errorf("logged-out landing exposed PAT form:\n%s", body)
	}
	if strings.Contains(body, `id="grants-block"`) {
		t.Errorf("logged-out landing exposed grants block:\n%s", body)
	}
	if strings.Contains(body, `href="/profile"`) {
		t.Errorf("logged-out landing exposed profile control:\n%s", body)
	}
}

// TestIndexDeadCookie: a present-but-invalid cookie (here, unknown) renders
// logged-out and clears the cookie so the browser stops resending a value that
// can never redeem.
func TestIndexDeadCookie(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/",
		map[string]string{"Cookie": sessionCookieName + "=bogus-value"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `href="/login/google"`) || !strings.Contains(rec.Body.String(), `href="/login/github"`) {
		t.Errorf("dead-cookie index not logged-out:\n%s", rec.Body.String())
	}

	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatalf("dead cookie was not cleared (no %s Set-Cookie)", sessionCookieName)
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("cleared cookie MaxAge = %d, want negative (delete)", cleared.MaxAge)
	}
}
