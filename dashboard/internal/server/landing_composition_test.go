package server

import (
	"net/http"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"dashboard/internal/googleidp"
)

func landingServerWithCRM(t *testing.T) *http.Server {
	t.Helper()
	root := t.TempDir()
	writeManifest(t, root, "dashboard", "APP=dashboard\nMOUNT=/\nDEFAULT=true\n")
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nMCP=true\n")

	opts := newServerDeps(t).opts()
	opts.ManifestRoot = root
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func signedInLanding(t *testing.T, srv *http.Server) string {
	t.Helper()
	sess := liveSession(t, srv)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", map[string]string{
		"Cookie":            sess.Name + "=" + sess.Value,
		"X-Forwarded-Proto": "https",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestLandingServiceNameLinksToMount(t *testing.T) {
	body := signedInLanding(t, landingServerWithCRM(t))

	// R-DB12-LINK
	if !strings.Contains(body, `<a href="/srv/crm/" class="name">ikigenba_crm</a>`) {
		t.Errorf("service name is not linked to the service mount:\n%s", body)
	}
	if !strings.Contains(body, `https://int.ikigenba.com/srv/crm/mcp`) {
		t.Errorf("service row no longer shows raw MCP URL:\n%s", body)
	}
}

func TestLandingServiceListChrome(t *testing.T) {
	body := signedInLanding(t, landingServerWithCRM(t))

	// R-OF1Q-VEDC
	for _, want := range []string{
		`<ul class="rows">`,
		`<li class="service-row">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("service list missing shared list chrome %q:\n%s", want, body)
		}
	}
	for _, stale := range []string{
		`services-table`,
		`<thead`,
		`<th>`,
	} {
		if strings.Contains(body, stale) {
			t.Errorf("service list still contains table chrome %q:\n%s", stale, body)
		}
	}

	// R-OG9N-9641
	for _, want := range []string{
		`<code class="meta service-url">https://int.ikigenba.com/srv/crm/mcp</code>`,
		`aria-label="Copy ikigenba_crm MCP URL"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("service row missing copyable MCP URL affordance %q:\n%s", want, body)
		}
	}

	// R-OHHJ-MXUQ
	if strings.Contains(body, `class="section-intro"`) {
		t.Errorf("landing page still renders section intro copy:\n%s", body)
	}
}

func TestHomeIsOnlyMCPServiceDirectory(t *testing.T) {
	body := signedInLanding(t, landingServerWithCRM(t))
	main := pageMain(t, body)

	// R-VJH9-9GQY
	if strings.Count(main, `<section `) != 1 || strings.Count(main, `<h2>MCP Services</h2>`) != 1 {
		t.Errorf("home body is not the single MCP Services section:\n%s", main)
	}
	for _, forbidden := range []string{"/install/claude", "/install/codex", "Connect your agent"} {
		if strings.Contains(main, forbidden) {
			t.Errorf("home body contains install content %q:\n%s", forbidden, main)
		}
	}
}

func TestLandingProfileAvatarLinksToProfile(t *testing.T) {
	body := signedInLanding(t, landingServerWithCRM(t))

	email := googleidp.StubIdentity.Email
	r, _ := utf8.DecodeRuneInString(email)
	initial := string(unicode.ToUpper(r))
	avatar := `<a href="/profile" class="avatar"`
	signOut := `<form method="POST" action="/logout">`

	// R-XO4W-LKAI
	if !strings.Contains(body, avatar) {
		t.Errorf("profile avatar link is missing; want %q in:\n%s", avatar, body)
	}
	if !strings.Contains(body, `>`+initial+`</a>`) {
		t.Errorf("profile avatar does not render owner initial %q:\n%s", initial, body)
	}
	if !strings.Contains(body, `title="`+email+`"`) {
		t.Errorf("profile avatar title does not carry owner email %q:\n%s", email, body)
	}
	if !strings.Contains(body, `aria-label="Profile — `+email+`"`) {
		t.Errorf("profile avatar aria-label does not carry owner email %q:\n%s", email, body)
	}
	if old := `<a href="/profile">` + email + `</a>`; strings.Contains(body, old) {
		t.Errorf("owner email is still rendered as visible profile link %q:\n%s", old, body)
	}

	signOutAt := strings.Index(body, signOut)
	avatarAt := strings.Index(body, avatar)
	if signOutAt < 0 || avatarAt < 0 || signOutAt >= avatarAt {
		t.Errorf("sign-out should precede the profile avatar; signOutAt=%d avatarAt=%d:\n%s", signOutAt, avatarAt, body)
	}
}

func TestLoggedOutLandingHidesProfileLink(t *testing.T) {
	srv := landingServerWithCRM(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", map[string]string{
		"X-Forwarded-Proto": "https",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if strings.Contains(rec.Body.String(), `href="/profile"`) {
		t.Errorf("logged-out landing exposes profile link:\n%s", rec.Body.String())
	}
}

func TestLandingSignOutRemainsPostLogout(t *testing.T) {
	body := signedInLanding(t, landingServerWithCRM(t))

	// R-DB14-SOUT
	if !strings.Contains(body, `<form method="POST" action="/logout">`) {
		t.Errorf("logged-in landing missing POST /logout form:\n%s", body)
	}
	if !strings.Contains(body, `<button type="submit"`) || !strings.Contains(body, `Sign out`) {
		t.Errorf("logged-in landing missing sign-out submit button:\n%s", body)
	}
}

func TestLoggedOutLandingOmitsMetricsAndRetiredTelemetryLinks(t *testing.T) {
	srv := landingServerWithCRM(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// R-X682-UBSK
	body := rec.Body.String()
	if strings.Contains(body, `href="/metrics"`) || strings.Contains(body, `href="/telemetry"`) {
		t.Errorf("logged-out landing exposes owner-only metrics link:\n%s", body)
	}
}

func pageMain(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<main class="page">`)
	if start < 0 {
		t.Fatalf("body missing page main:\n%s", body)
	}
	end := strings.Index(body[start:], `</main>`)
	if end < 0 {
		t.Fatalf("page main is not closed:\n%s", body[start:])
	}
	return body[start : start+end+len(`</main>`)]
}
