package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"dashboard/internal/ratelimit"
)

var correlationIDPattern = regexp.MustCompile(`^[0123456789ABCDEFGHJKMNPQRSTVWXYZ]{26}$`)

func allowedAuthn(t *testing.T, h http.Handler, d serverDeps, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	tok := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	headers := map[string]string{
		"Authorization":  "Bearer " + tok,
		"X-Original-URI": "/srv/crm/mcp",
	}
	for name, value := range extra {
		headers[name] = value
	}
	return doAuthn(h, headers)
}

// R-X7FZ-83J9
func TestAuthnAllowCorrelationIDHasCrockfordULIDShape(t *testing.T) {
	d := newServerDeps(t)
	rec := allowedAuthn(t, authnServer(t, d, nil), d, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Correlation-Id"); !correlationIDPattern.MatchString(got) {
		t.Fatalf("X-Correlation-Id = %q, want 26 Crockford-base32 characters", got)
	}
}

// R-X8NV-LV9Y
func TestAuthnSuccessiveAllowsMintFreshCorrelationIDs(t *testing.T) {
	d := newServerDeps(t)
	h := authnServer(t, d, nil)
	tok := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	headers := map[string]string{"Authorization": "Bearer " + tok, "X-Original-URI": "/srv/crm/mcp"}
	first := doAuthn(h, headers)
	second := doAuthn(h, headers)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d, want 200, 200", first.Code, second.Code)
	}
	firstID, secondID := first.Header().Get("X-Correlation-Id"), second.Header().Get("X-Correlation-Id")
	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("successive X-Correlation-Id values = %q, %q, want distinct non-empty ids", firstID, secondID)
	}
}

// R-X9VR-ZN0N
func TestAuthnAllowNeverEchoesInboundCorrelationID(t *testing.T) {
	const attackerID = "ATTACKER-CHOSEN-CORRELATION"
	d := newServerDeps(t)
	rec := allowedAuthn(t, authnServer(t, d, nil), d, map[string]string{"X-Correlation-Id": attackerID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Correlation-Id"); got == attackerID || !correlationIDPattern.MatchString(got) {
		t.Fatalf("X-Correlation-Id = %q, want fresh Crockford ULID distinct from inbound value", got)
	}
	for name, values := range rec.Header() {
		for _, value := range values {
			if strings.Contains(value, attackerID) {
				t.Errorf("response header %s echoes attacker value %q", name, value)
			}
		}
	}
}

// R-XB3O-DERC
func TestSessionAuthnAllowsMintFreshCorrelationIDsAndIgnoreInboundValue(t *testing.T) {
	const attackerID = "ATTACKER-SESSION-CORRELATION"
	d := newServerDeps(t)
	h := authnServer(t, d, nil)
	cookie := liveSessionCookie(t, d, "owner@int.ikigenba.com")
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://127.0.0.1/internal/session-authn", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("Cookie", sessionCookieName+"="+cookie)
		req.Header.Set("X-Correlation-Id", attackerID)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	first, second := request(), request()
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d, want 200, 200", first.Code, second.Code)
	}
	firstID, secondID := first.Header().Get("X-Correlation-Id"), second.Header().Get("X-Correlation-Id")
	if !correlationIDPattern.MatchString(firstID) || !correlationIDPattern.MatchString(secondID) {
		t.Fatalf("session correlation ids = %q, %q, want Crockford ULIDs", firstID, secondID)
	}
	if firstID == secondID || firstID == attackerID || secondID == attackerID {
		t.Fatalf("session correlation ids = %q, %q, want fresh values that ignore %q", firstID, secondID, attackerID)
	}
}

// R-XCBK-R6I1
func TestIntrospectionDenialsNeverReturnCorrelationID(t *testing.T) {
	t.Run("bearer 401", func(t *testing.T) {
		d := newServerDeps(t)
		rec := doAuthn(authnServer(t, d, nil), map[string]string{"X-Original-URI": "/srv/crm/mcp"})
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Correlation-Id") != "" {
			t.Fatalf("status = %d, X-Correlation-Id = %q; want 401 and absent", rec.Code, rec.Header().Get("X-Correlation-Id"))
		}
	})
	t.Run("bearer 429", func(t *testing.T) {
		d := newServerDeps(t)
		h := authnServer(t, d, ratelimit.New(1, time.Minute))
		tok := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
		headers := map[string]string{"Authorization": "Bearer " + tok, "X-Original-URI": "/srv/crm/mcp"}
		if first := doAuthn(h, headers); first.Code != http.StatusOK {
			t.Fatalf("priming request status = %d, want 200", first.Code)
		}
		rec := doAuthn(h, headers)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("X-Correlation-Id") != "" {
			t.Fatalf("status = %d, X-Correlation-Id = %q; want 429 and absent", rec.Code, rec.Header().Get("X-Correlation-Id"))
		}
	})
	t.Run("session 401", func(t *testing.T) {
		d := newServerDeps(t)
		rec := doSessionAuthn(authnServer(t, d, nil), "")
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Correlation-Id") != "" {
			t.Fatalf("status = %d, X-Correlation-Id = %q; want 401 and absent", rec.Code, rec.Header().Get("X-Correlation-Id"))
		}
	})
}
