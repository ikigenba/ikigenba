package server

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

type openCheckCase struct {
	name, cause, scheme, authorization string
	cookie                             bool
	guest                              bool
	status                             int
}

func openCheckCases() []openCheckCase {
	cases := []openCheckCase{
		{name: "no-cookie", cause: "absent", guest: true, status: 200},
		{name: "unknown-session", cause: "unknown-session", cookie: true, guest: true, status: 200},
		{name: "idle-session", cause: "idle", cookie: true, guest: true, status: 200},
		{name: "capped-session", cause: "capped", cookie: true, guest: true, status: 200},
		{name: "live-session", cause: "session", cookie: true, status: 200},
		{name: "failed-session-store", cause: "database-failure", cookie: true, status: 500},
	}
	for _, authorization := range []string{"bearer unknown", "basic !", "Digest value", "Bearer", "Basic"} {
		for _, cookie := range []bool{false, true} {
			cases = append(cases, openCheckCase{name: fmt.Sprintf("ignored-%s/cookie=%v", authorization, cookie), cause: "session", authorization: authorization, cookie: cookie, guest: !cookie, status: 200})
		}
	}
	for _, cause := range []string{"honored", "unknown", "disabled", "expired", "stale-owner", "empty-password", "colon-password", "database-failure"} {
		status := 403
		switch cause {
		case "honored", "empty-password", "colon-password":
			status = 200
		case "database-failure":
			status = 500
		}
		for _, scheme := range []string{"Bearer", "Basic"} {
			for _, cookie := range []bool{false, true} {
				cases = append(cases, openCheckCase{name: fmt.Sprintf("%s/%s/cookie=%v", cause, scheme, cookie), cause: cause, scheme: scheme, cookie: cookie, status: status})
			}
		}
	}
	for _, authorization := range malformedBasicValues() {
		for _, cookie := range []bool{false, true} {
			cases = append(cases, openCheckCase{name: fmt.Sprintf("malformed-%q/cookie=%v", authorization, cookie), cause: "malformed", authorization: authorization, cookie: cookie, status: 403})
		}
	}
	return cases
}

func openCheckFixture(t *testing.T, tc openCheckCase, path string, originalHeaders bool) (identityFixture, *http.Request, identitySnapshot) {
	t.Helper()
	f, session, secret := basicOutcomeFixture(t, tc.cause)
	sessionID := session.ID
	switch tc.cause {
	case "unknown-session":
		sessionID = "unknown-session"
	case "idle":
		f.exec(t, `UPDATE sessions SET login_at = ?, last_used_at = ? WHERE id = ?`, identityNow.Add(-3*time.Hour).UnixNano(), identityNow.Add(-20*time.Minute).UnixNano(), session.ID)
	case "capped":
		f.exec(t, `UPDATE sessions SET login_at = ?, last_used_at = ? WHERE id = ?`, identityNow.Add(-18*time.Hour-30*time.Minute).UnixNano(), identityNow.Add(-time.Minute).UnixNano(), session.ID)
	}
	var cookies []*http.Cookie
	if tc.cookie {
		cookies = []*http.Cookie{{Name: SessionCookieName, Value: sessionID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}}
	}
	authorization := tc.authorization
	switch tc.scheme {
	case "Bearer":
		authorization = "Bearer " + secret
	case "Basic":
		authorization = basicAuthorization("ignored-username", secret)
	}
	r := credentialRequest(path, cookies, authorization)
	if originalHeaders {
		r.Header["X-Request-Id"] = []string{"open-request", "ignored-request"}
		r.Header["X-Original-Method"] = []string{"POST", "ignored-method"}
		r.Header["X-Original-Host"] = []string{"app.green.example", "ignored-host"}
		r.Header["X-Original-Uri"] = []string{"/widgets?private=query?more", "ignored-path"}
	}
	before := f.snapshot(t)
	return f, r, before
}

func TestOpenCheckMatchesCheckExceptGuests(t *testing.T) {
	for _, tc := range openCheckCases() {
		for _, originalHeaders := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/original-headers=%v", tc.name, originalHeaders), func(t *testing.T) {
				var controlAnswer identityAnswer
				var controlState identitySnapshot
				var controlAttrs telemetry.Attrs
				for _, path := range []string{"/check", "/check/open"} {
					fixture, req, before := openCheckFixture(t, tc, path, originalHeaders)
					trail := newTrail(t, Config{Store: fixture.store, Now: func() time.Time { return identityNow }}, nil)
					if tc.cause == "database-failure" {
						// Fail the actual store after server.New has received it.
						failServerStore(t, fixture.store)
						if tc.scheme == "" {
							_, err := fixture.store.TouchSession(req.Cookies()[0].Value, identityNow)
							if err == nil || errors.Is(err, store.ErrNotFound) {
								t.Fatalf("failing-store error = %v, want failure distinct from ErrNotFound", err)
							}
						}
					}
					response, events := trail.request(t, req)
					if tc.cause == "database-failure" {
						serverStoreDB(t, fixture.store).SetFailing(false)
					}
					after := fixture.snapshot(t)
					if tc.cause == "database-failure" {
						serverStoreDB(t, fixture.store).SetFailing(true)
					}
					if path == "/check" {
						controlAnswer, controlState = comparableIdentityAnswer(path, response), after
						if len(events) != 3 {
							t.Fatalf("control events = %#v", events)
						}
						controlAttrs = events[1].Attrs
						continue
					}

					if response.Code != tc.status {
						t.Fatalf("status = %d, want %d", response.Code, tc.status)
					}
					if tc.guest {
						// R-390Q-CH5D: absent, unknown, idle, and capped sessions allow a guest without any stored mutation.
						if _, present := response.Header()[HeaderUserID]; present {
							t.Fatal("guest has user id header")
						}
						if _, present := response.Header()[HeaderUserEmail]; present {
							t.Fatal("guest has email header")
						}
						assertSnapshotEqual(t, after, before)
					} else {
						// R-3BGJ-40MR: every non-guest outcome matches /check's status, presence and values of contracted headers, and complete stored state.
						if got := comparableIdentityAnswer(path, response); !reflect.DeepEqual(got, controlAnswer) {
							t.Fatalf("open answer = %#v, check answer = %#v", got, controlAnswer)
						}
						assertSnapshotEqual(t, after, controlState)
					}
					// R-8KY6-1HXB: a cookie's genuine store failure yields 500, never a guest success, without identity.
					if tc.cause == "database-failure" && tc.scheme == "" {
						assertCheckRefusal(t, response, http.StatusInternalServerError)
					}
					// R-3IRX-EN2X: identity appears only in its two response headers; no successful response body is inspected.
					for _, user := range before.users {
						assertHeaderOmits(t, response.Header(), user.id, HeaderUserID)
						assertHeaderOmits(t, response.Header(), user.email, HeaderUserEmail)
					}
					// R-3L7Q-66KB: successes, guests, refusals, and store failures issue no challenge header.
					for name := range response.Header() {
						if strings.EqualFold(name, "WWW-Authenticate") {
							t.Fatalf("challenge header = %#v", response.Header())
						}
					}

					// R-3OVF-BHSE: exactly the two request events and one status-selected check event, with the request id and only an allowed identity's user.
					checkName := "check.allowed"
					switch response.Code {
					case http.StatusForbidden, http.StatusUnauthorized:
						checkName = "check.refused"
					case http.StatusInternalServerError:
						checkName = "check.failed"
					}
					if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != checkName || events[2].Name != "request.finished" {
						t.Fatalf("event sequence = %#v", events)
					}
					check := events[1]
					wantID := req.Header.Get("X-Request-Id")
					if wantID == "" {
						wantID = events[0].RequestID
					}
					if wantID == "" || check.RequestID != wantID || events[2].RequestID != wantID || check.User != response.Header().Get(HeaderUserID) {
						t.Fatalf("check envelope = %#v, want request %q and user %q", check, wantID, response.Header().Get(HeaderUserID))
					}
					if events[2].Attrs["status"] != int64(response.Code) {
						t.Fatalf("finished status = %v", events[2].Attrs["status"])
					}
					// R-3RB8-319S: exact string-valued attributes match /check except the guest outcome, retaining its presented credential kind.
					wantAttrs := telemetry.Attrs{}
					for key, value := range controlAttrs {
						wantAttrs[key] = value
					}
					if tc.guest {
						wantAttrs["outcome"] = "guest"
					}
					if !reflect.DeepEqual(check.Attrs, wantAttrs) {
						t.Fatalf("open attrs = %#v, want %#v", check.Attrs, wantAttrs)
					}
					for key, value := range check.Attrs {
						if _, ok := value.(string); !ok {
							t.Fatalf("attribute %s = %T, want string", key, value)
						}
					}
				}
			})
		}
	}
}

func TestOpenCheckHeadAndMethodRefusalNeverChallenge(t *testing.T) {
	// R-3L7Q-66KB: HEAD answers and method refusals also omit any authentication challenge.
	for _, method := range []string{http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			fixture := openIdentityFixture(t)
			trail := newTrail(t, Config{Store: fixture.store, Now: func() time.Time { return identityNow }}, nil)
			req := trailRequest(method, "/check/open")
			response, _ := trail.request(t, req)
			for name := range response.Header() {
				if strings.EqualFold(name, "WWW-Authenticate") {
					t.Fatalf("challenge header = %#v", response.Header())
				}
			}
		})
	}
}
