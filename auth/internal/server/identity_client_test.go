package server

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func identityClientToken(t *testing.T, f identityFixture, owner store.User, host string, issued time.Time) (store.Token, string) {
	t.Helper()
	client, err := f.store.RegisterClient("client", []string{"http://localhost/callback"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	code, err := f.store.CreateAuthCode(client.ID, owner.ID, "http://localhost/callback", "challenge", "https://mcp.sbx.ikigenba.dev/mcp", issued)
	if err != nil {
		t.Fatal(err)
	}
	token, secret, err := f.store.CreateClientToken(code, host, issued)
	if err != nil {
		t.Fatal(err)
	}
	return token, secret
}

func identityClientRequest(path, secret, scheme, session string, hosts []string) *http.Request {
	r := identityRequest(path, session, "")
	r.Host = "auth.sbx.ikigenba.dev"
	r.Header.Set("X-Request-Id", "host-check")
	r.Header.Set("Authorization", "Bearer "+secret)
	if scheme == "Basic" {
		r.Header.Set("Authorization", basicAuthorization("ignored-user", secret))
	}
	if hosts != nil {
		r.Header["X-Original-Host"] = append([]string(nil), hosts...)
	}
	return r
}

func normalizedCheckEvent(t *testing.T, events []telemetry.Event) telemetry.Event {
	t.Helper()
	if len(events) != 3 || events[0].Name != "request.started" || events[2].Name != "request.finished" {
		t.Fatalf("unexpected trail: %#v", events)
	}
	e := events[1]
	e.Time = time.Time{}
	return e
}

func TestClientCheckHostHonorsOnlyFirstOriginalHost(t *testing.T) {
	// R-A5Y7-BL86 R-FQFN-71UF: check derives the textual host and touches a matching client token on both endpoints and credential kinds.
	hosts := [][]string{{"mcp.sbx.ikigenba.dev"}, {"mcp.sbx.ikigenba.dev:7400"}, {"mcp.sbx.ikigenba.dev:00007400"}, {"mcp.sbx.ikigenba.dev:99999999999999999999"}, {"MCP.Sbx.Ikigenba.DEV"}, {"mcp.sbx.ikigenba.dev", "repos.sbx.ikigenba.dev"}}
	for _, path := range []string{"/check", "/check/open"} {
		for _, scheme := range []string{"Bearer", "Basic"} {
			for _, host := range hosts {
				t.Run(path+scheme+host[0], func(t *testing.T) {
					f := openIdentityFixture(t)
					user := f.user(t, "client-owner", "client@example.com", identityNow)
					token, secret := identityClientToken(t, f, user, "mcp.sbx.ikigenba.dev", identityNow.Add(-time.Hour))
					before := f.snapshot(t)
					trail := newTrail(t, Config{Store: f.store, Now: func() time.Time { return identityNow }}, nil)
					w, events := trail.request(t, identityClientRequest(path, secret, scheme, "", host))
					if w.Code != 200 || w.Header().Get(HeaderUserID) != user.ID || w.Header().Get(HeaderUserEmail) != user.Email {
						t.Fatalf("answer=%d %#v", w.Code, w.Header())
					}
					assertOnlyTokenTouch(t, before, f.snapshot(t), token.ID, identityNow)
					e := normalizedCheckEvent(t, events)
					if e.Name != "check.allowed" || e.User != user.ID || e.RequestID != "host-check" || e.Attrs["outcome"] != "allowed" || e.Attrs["token"] != token.ID {
						t.Fatalf("allowed event=%#v", e)
					}
				})
			}
		}
	}
}

func TestClientCheckHostRefusalsMatchUnknownTokens(t *testing.T) {
	// R-A5Y7-BL86 R-A9LW-GWG9: mismatches never fall back to a cookie and expose the same refusal event as an unknown credential.
	hosts := [][]string{{"repos.sbx.ikigenba.dev"}, {"auth.sbx.ikigenba.dev"}, {"mcp.sbx.ikigenba.dev.example"}, {"xmcp.sbx.ikigenba.dev"}, {"mcp.sbx.ikigenba.dev:http"}, {"mcp.sbx.ikigenba.dev:"}, {"mcp.sbx.i\u212Aigenba.dev"}, nil, {"repos.sbx.ikigenba.dev", "mcp.sbx.ikigenba.dev"}}
	for _, path := range []string{"/check", "/check/open"} {
		for _, scheme := range []string{"Bearer", "Basic"} {
			for _, host := range hosts {
				for _, cookie := range []bool{false, true} {
					f := openIdentityFixture(t)
					owner := f.user(t, "client-owner", "client@example.com", identityNow)
					_, secret := identityClientToken(t, f, owner, "mcp.sbx.ikigenba.dev", identityNow.Add(-time.Hour))
					cookieOwner := f.user(t, "cookie-owner", "cookie@example.com", identityNow)
					session := f.session(t, cookieOwner.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
					sessionID := ""
					if cookie {
						sessionID = session.ID
					}
					trail := newTrail(t, Config{Store: f.store, Now: func() time.Time { return identityNow }}, nil)
					before := f.snapshot(t)
					r := identityClientRequest(path, secret, scheme, sessionID, host)
					if host == nil {
						r.Host = "mcp.sbx.ikigenba.dev"
					}
					w, events := trail.request(t, r)
					assertCheckRefusal(t, w, 403)
					assertSnapshotEqual(t, f.snapshot(t), before)
					refused := normalizedCheckEvent(t, events)
					control := identityClientRequest(path, "unknown", scheme, sessionID, host)
					control.Host = r.Host
					unknown, unknownEvents := trail.request(t, control)
					assertCheckRefusal(t, unknown, 403)
					assertSnapshotEqual(t, f.snapshot(t), before)
					if !reflect.DeepEqual(refused, normalizedCheckEvent(t, unknownEvents)) {
						t.Fatalf("host %v: refusal differs from unknown: %#v %#v", host, refused, unknownEvents)
					}
				}
			}
		}
	}
}

func TestClientAndPersonalRefusalCausesAreIndistinguishable(t *testing.T) {
	// R-FP7Q-TA3Q R-FRNJ-KTL4 R-FU3C-CD2I: all store refusal causes and malformed Basic expose the same check result and event, with no mutation.
	for _, scheme := range []string{"Bearer", "Basic"} {
		var expected *telemetry.Event
		var expectedBody string
		var expectedHeaders http.Header
		for _, cause := range []string{"unknown", "disabled", "expired", "stale-owner", "revoked-client", "expired-client", "expired-client-wrong-host", "stale-client", "empty-client-host", "wrong-client-host", "malformed"} {
			if cause == "malformed" && scheme == "Bearer" {
				continue
			}
			f := openIdentityFixture(t)
			login := identityNow
			if cause == "stale-owner" || cause == "stale-client" {
				login = identityNow.Add(-store.TokenLoginWindow - time.Nanosecond)
			}
			owner := f.user(t, "owner", "owner@example.com", login)
			issued := identityNow.Add(-time.Hour)
			var token store.Token
			var secret string
			switch cause {
			case "revoked-client", "expired-client", "expired-client-wrong-host", "stale-client", "empty-client-host", "wrong-client-host":
				if cause == "expired-client" || cause == "expired-client-wrong-host" {
					issued = identityNow.Add(-91 * 24 * time.Hour)
				}
				host := "mcp.sbx.ikigenba.dev"
				if cause == "wrong-client-host" || cause == "expired-client-wrong-host" {
					host = "different.sbx.ikigenba.dev"
				}
				if cause == "empty-client-host" {
					host = ""
				}
				token, secret = identityClientToken(t, f, owner, host, issued)
				if cause == "revoked-client" {
					if err := f.store.RevokeToken(owner.ID, token.ID); err != nil {
						t.Fatal(err)
					}
				}
			default:
				expiry := store.ExpiryNever
				if cause == "expired" {
					expiry = store.Expiry30d
					issued = identityNow.Add(-31 * 24 * time.Hour)
				}
				token, secret = f.token(t, owner.ID, "personal", expiry, issued)
				if cause == "disabled" {
					if err := f.store.SetTokenEnabled(owner.ID, token.ID, false); err != nil {
						t.Fatal(err)
					}
				}
				if cause == "unknown" {
					secret = "unknown"
				}
			}
			// Ensure each refusal is checked even when a separate live cookie is available.
			cookieOwner := f.user(t, "cookie", "cookie@example.com", identityNow)
			session := f.session(t, cookieOwner.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
			before := f.snapshot(t)
			trail := newTrail(t, Config{Store: f.store, Now: func() time.Time { return identityNow }}, nil)
			auths := []string{"Bearer " + secret}
			if scheme == "Basic" {
				auths = []string{basicAuthorization("", secret), basicAuthorization("a", secret), basicAuthorization("different\x00", secret)}
				if cause == "malformed" {
					auths = malformedBasicValues()
				}
			}
			for _, auth := range auths {
				r := identityClientRequest("/check", secret, scheme, session.ID, []string{"mcp.sbx.ikigenba.dev"})
				r.Header.Set("Authorization", auth)
				r.Header.Set("X-Original-Method", "POST")
				r.Header.Set("X-Original-URI", "/private?hidden")
				w, events := trail.request(t, r)
				assertCheckRefusal(t, w, 403)
				assertSnapshotEqual(t, f.snapshot(t), before)
				e := normalizedCheckEvent(t, events)
				if expected == nil {
					expected = &e
					expectedBody = w.Body.String()
					expectedHeaders = w.Header().Clone()
				} else if !reflect.DeepEqual(*expected, e) || expectedBody != w.Body.String() || !reflect.DeepEqual(expectedHeaders, w.Header()) {
					t.Fatalf("%s %s: distinguishable refusal %#v want %#v", scheme, cause, e, *expected)
				}
			}
		}
	}
}

func TestMeRejectsClientTokensRegardlessOfHost(t *testing.T) {
	// R-A5Y7-BL86 R-IN8P-M6D3: /me always supplies empty host; live and expired client tokens yield the same plain refusal as unknown/personal failures.
	var expected string
	for _, cause := range []string{"live-client", "expired-client", "unknown", "disabled", "expired", "stale-owner"} {
		for _, hosts := range [][]string{nil, {"mcp.sbx.ikigenba.dev"}} {
			for _, requestHost := range []string{"auth.sbx.ikigenba.dev", "mcp.sbx.ikigenba.dev"} {
				f := openIdentityFixture(t)
				login := identityNow
				if cause == "stale-owner" {
					login = identityNow.Add(-store.TokenLoginWindow - time.Nanosecond)
				}
				owner := f.user(t, "owner", "owner@example.com", login)
				issued := identityNow.Add(-time.Hour)
				var secret string
				if cause == "live-client" || cause == "expired-client" {
					if cause == "expired-client" {
						issued = identityNow.Add(-91 * 24 * time.Hour)
					}
					_, secret = identityClientToken(t, f, owner, "mcp.sbx.ikigenba.dev", issued)
				} else {
					expiry := store.ExpiryNever
					if cause == "expired" {
						expiry = store.Expiry30d
						issued = identityNow.Add(-31 * 24 * time.Hour)
					}
					token, value := f.token(t, owner.ID, "personal", expiry, issued)
					secret = value
					if cause == "unknown" {
						secret = "unknown"
					}
					if cause == "disabled" {
						if err := f.store.SetTokenEnabled(owner.ID, token.ID, false); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := f.snapshot(t)
				trail := newTrail(t, Config{Store: f.store, Now: func() time.Time { return identityNow }}, nil)
				r := identityClientRequest("/me", secret, "Bearer", "", hosts)
				r.Host = requestHost
				w, events := trail.request(t, r)
				if expected == "" {
					expected = w.Body.String()
				}
				assertPlainRefusal(t, w, 403, expected)
				if !singlePlainLine(expected) {
					t.Fatalf("refusal is not one line: %q", expected)
				}
				assertSnapshotEqual(t, f.snapshot(t), before)
				assertTrail(t, events, r, 403)
			}
		}
	}
}

func TestUnknownPersonalCredentialIgnoresOriginalMetadata(t *testing.T) {
	// R-XDIL-E776: original request metadata cannot change unknown personal credential refusal or any persisted state.
	variants := []map[string]string{
		nil,
		{"X-Original-Host": "mcp.sbx.ikigenba.dev"},
		{"X-Original-Host": "repos.sbx.ikigenba.dev"},
		{"X-Original-Host": "mcp.sbx.ikigenba.dev:7400"},
		{"X-Original-Method": "POST", "X-Original-URI": "/different?private"},
		{"X-Original-Method": "", "X-Original-URI": ""},
	}
	f := openIdentityFixture(t)
	owner := f.user(t, "owner", "owner@example.com", identityNow)
	f.token(t, owner.ID, "personal", store.ExpiryNever, identityNow.Add(-time.Hour))
	session := f.session(t, owner.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
	before := f.snapshot(t)
	trail := newTrail(t, Config{Store: f.store, Now: func() time.Time { return identityNow }}, nil)
	var expected identityAnswer
	for i, headers := range variants {
		r := identityRequest("/check", session.ID, "unknown")
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w, _ := trail.request(t, r)
		assertCheckRefusal(t, w, 403)
		assertSnapshotEqual(t, f.snapshot(t), before)
		answer := comparableIdentityAnswer("/check", w)
		if i == 0 {
			expected = answer
		} else if !reflect.DeepEqual(answer, expected) {
			t.Fatalf("metadata altered refusal: %#v want %#v", answer, expected)
		}
	}
}
