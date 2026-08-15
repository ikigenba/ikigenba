package server

import (
	"bytes"
	"context"
	"dashboard/internal/audit"
	"dashboard/internal/oauth"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	reposResource = "https://int.ikigenba.com/srv/repos/mcp"
	gitDoorURI    = "/srv/repos/git/code/foo.git/info/refs"
)

func gitAuthnServer(t *testing.T, d serverDeps) http.Handler {
	t.Helper()
	opts := d.opts()
	opts.Resources = []string{testResource, reposResource}
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv.Handler
}

func basicAuthorization(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func authnIdentityHeaders(rec http.Header) map[string]string {
	return map[string]string{
		"X-Owner-Id":      rec.Get("X-Owner-Id"),
		"X-Owner-Email":   rec.Get("X-Owner-Email"),
		"X-Owner-Name":    rec.Get("X-Owner-Name"),
		"X-Owner-Picture": rec.Get("X-Owner-Picture"),
		"X-Client-Id":     rec.Get("X-Client-Id"),
		"X-Token-Id":      rec.Get("X-Token-Id"),
		"X-Chain-Id":      rec.Get("X-Chain-Id"),
	}
}

func lastAuthnAuditDetails(t *testing.T, d serverDeps) map[string]any {
	t.Helper()
	var raw string
	if err := d.db.QueryRow(`SELECT details FROM audit_log WHERE event_type IN (?, ?) ORDER BY rowid DESC LIMIT 1`, audit.EventAuthnAllow, audit.EventAuthnDeny).Scan(&raw); err != nil {
		t.Fatalf("query latest authn audit: %v", err)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		t.Fatalf("decode latest authn audit details: %v", err)
	}
	return details
}

func requireBasicChallenge(t *testing.T, h http.Header) {
	t.Helper()
	if got := h.Get("WWW-Authenticate"); got != `Basic realm="ikigenba"` {
		t.Errorf("WWW-Authenticate = %q, want exact Basic challenge", got)
	}
}

// R-39O6-OFFD
func TestGitDoorBasicPATMatchesBearerIdentity(t *testing.T) {
	d := newServerDeps(t)
	seedIdentity(t, d, "owner-test", "owner@int.ikigenba.com", "Git Owner", "https://images.example/git.png")
	h := gitAuthnServer(t, d)
	tok, p := mintPAT(t, d, "owner@int.ikigenba.com")
	bearer := doAuthn(h, map[string]string{"Authorization": "Bearer " + tok, "X-Original-URI": gitDoorURI})
	basic := doAuthn(h, map[string]string{
		"Authorization":  basicAuthorization("anything", tok),
		"X-Original-URI": gitDoorURI + "?service=git-upload-pack",
	})
	if bearer.Code != http.StatusOK || basic.Code != http.StatusOK {
		t.Fatalf("bearer/basic statuses = %d/%d, want 200/200", bearer.Code, basic.Code)
	}
	if got, want := authnIdentityHeaders(basic.Header()), authnIdentityHeaders(bearer.Header()); !reflect.DeepEqual(got, want) {
		t.Errorf("Basic identity headers = %#v, bearer = %#v", got, want)
	}
	if got := basic.Header().Get("X-Client-Id"); got != "pat:"+p.PublicID {
		t.Errorf("X-Client-Id = %q, want pat:%s", got, p.PublicID)
	}
	if got := basic.Header().Get("X-Token-Id"); got != p.ID {
		t.Errorf("X-Token-Id = %q, want %q", got, p.ID)
	}
	if _, ok := basic.Header()["X-Chain-Id"]; ok {
		t.Errorf("X-Chain-Id = %#v, want absent", basic.Header().Values("X-Chain-Id"))
	}
}

// R-3AW3-2762
func TestGitDoorBasicDiscardsUsername(t *testing.T) {
	d := newServerDeps(t)
	seedIdentity(t, d, "owner-test", "owner@int.ikigenba.com", "Git Owner", "https://images.example/git.png")
	h := gitAuthnServer(t, d)
	tok, _ := mintPAT(t, d, "owner@int.ikigenba.com")
	var want map[string]string
	for _, username := range []string{"", "x", "git"} {
		rec := doAuthn(h, map[string]string{"Authorization": basicAuthorization(username, tok), "X-Original-URI": gitDoorURI})
		if rec.Code != http.StatusOK {
			t.Fatalf("username %q: status = %d, want 200", username, rec.Code)
		}
		got := authnIdentityHeaders(rec.Header())
		if want == nil {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Errorf("username %q headers = %#v, want byte-identical %#v", username, got, want)
		}
	}
}

// R-3C3Z-FYWR
func TestBasicPATIsNotAcceptedOffGitDoor(t *testing.T) {
	d := newServerDeps(t)
	h := gitAuthnServer(t, d)
	tok, _ := mintPAT(t, d, "owner@int.ikigenba.com")
	rec := doAuthn(h, map[string]string{"Authorization": basicAuthorization("git", tok), "X-Original-URI": "/srv/crm/mcp"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Bearer") || !strings.Contains(challenge, "resource_metadata=") {
		t.Errorf("WWW-Authenticate = %q, want Bearer challenge with resource_metadata", challenge)
	}
	if got := lastAuthnAuditDetails(t, d)["reason"]; got != "missing_bearer" {
		t.Errorf("audit reason = %v, want missing_bearer", got)
	}
}

// R-3DBV-TQNG
func TestGitDoorBasicPrefixBoundary(t *testing.T) {
	d := newServerDeps(t)
	h := gitAuthnServer(t, d)
	tok, _ := mintPAT(t, d, "owner@int.ikigenba.com")
	for _, tc := range []struct {
		uri  string
		want int
	}{{"/srv/repos/mcp", 401}, {"/srv/repos/gitolite/x", 401}, {gitDoorURI, 200}} {
		rec := doAuthn(h, map[string]string{"Authorization": basicAuthorization("git", tok), "X-Original-URI": tc.uri})
		if rec.Code != tc.want {
			t.Errorf("URI %q: status = %d, want %d", tc.uri, rec.Code, tc.want)
		}
	}
}

// R-3EJS-7IE5
func TestGitDoorBasicRefusesValidOAuthAccessToken(t *testing.T) {
	d := newServerDeps(t)
	h := gitAuthnServer(t, d)
	tok := mintAccessToken(t, d, "owner@int.ikigenba.com", testResource)
	if rec := doAuthn(h, map[string]string{"Authorization": "Bearer " + tok, "X-Original-URI": "/srv/crm/mcp"}); rec.Code != http.StatusOK {
		t.Fatalf("OAuth bearer control status = %d, want 200", rec.Code)
	}
	rec := doAuthn(h, map[string]string{"Authorization": basicAuthorization("git", tok), "X-Original-URI": gitDoorURI})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Basic OAuth status = %d, want 401", rec.Code)
	}
	requireNoOwnerHeaders(t, rec)
	if got := lastAuthnAuditDetails(t, d)["reason"]; got != "basic_not_pat" {
		t.Errorf("audit reason = %v, want basic_not_pat", got)
	}
}

// R-3FRO-LA4U
func TestGitDoorBasicRejectsRevokedAndExpiredPATs(t *testing.T) {
	for _, state := range []string{"revoked", "expired"} {
		t.Run(state, func(t *testing.T) {
			d := newServerDeps(t)
			h := gitAuthnServer(t, d)
			tok, p := mintPAT(t, d, "owner@int.ikigenba.com")
			if state == "revoked" {
				if err := d.pats.Revoke(context.Background(), p.ID); err != nil {
					t.Fatalf("Revoke: %v", err)
				}
			} else if _, err := d.db.Exec(`UPDATE personal_tokens SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), p.ID); err != nil {
				t.Fatalf("expire PAT: %v", err)
			}
			rec := doAuthn(h, map[string]string{"Authorization": basicAuthorization("git", tok), "X-Original-URI": gitDoorURI})
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			requireBasicChallenge(t, rec.Header())
			if got := lastAuthnAuditDetails(t, d)["reason"]; got != "invalid_pat_basic" {
				t.Errorf("audit reason = %v, want invalid_pat_basic", got)
			}
		})
	}
}

// R-3I7H-CTM8
func TestGitDoorMissingAndMalformedBasicAreMissingCredential(t *testing.T) {
	for _, authorization := range []string{"", "Basic !!!not-base64!!!", "Basic " + base64.StdEncoding.EncodeToString([]byte("no-colon"))} {
		t.Run(authorization, func(t *testing.T) {
			d := newServerDeps(t)
			h := gitAuthnServer(t, d)
			headers := map[string]string{"X-Original-URI": gitDoorURI}
			if authorization != "" {
				headers["Authorization"] = authorization
			}
			rec := doAuthn(h, headers)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			requireBasicChallenge(t, rec.Header())
			if got := lastAuthnAuditDetails(t, d)["reason"]; got != "missing_credential" {
				t.Errorf("audit reason = %v, want missing_credential", got)
			}
		})
	}
}

// R-3JFD-QLCX
func TestGitDoorBearerPathRemainsAuthoritative(t *testing.T) {
	d := newServerDeps(t)
	h := gitAuthnServer(t, d)
	tok, _ := mintPAT(t, d, "owner@int.ikigenba.com")
	valid := doAuthn(h, map[string]string{"Authorization": "Bearer " + tok, "X-Original-URI": gitDoorURI})
	if valid.Code != http.StatusOK {
		t.Fatalf("valid bearer status = %d, want 200", valid.Code)
	}
	invalid := doAuthn(h, map[string]string{"Authorization": "Bearer " + oauth.AccessPrefix + "invalid", "X-Original-URI": gitDoorURI})
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer status = %d, want 401", invalid.Code)
	}
	if got := lastAuthnAuditDetails(t, d)["reason"]; got != "invalid_token" {
		t.Errorf("invalid bearer reason = %v, want invalid_token", got)
	}
}

// R-3KNA-4D3M
func TestGitDoorBasicAllowSeparatesAuditCredentialAndEmitsOneEdge(t *testing.T) {
	d := newServerDeps(t)
	recorder, sink := liveEdgeRecorder(t)
	d.recorder = recorder
	h := gitAuthnServer(t, d)
	tok, _ := mintPAT(t, d, "owner@int.ikigenba.com")
	basic := doAuthn(h, map[string]string{"Authorization": basicAuthorization("git", tok), "X-Original-URI": gitDoorURI})
	if basic.Code != http.StatusOK {
		t.Fatalf("Basic status = %d, want 200", basic.Code)
	}
	bearer := doAuthn(h, map[string]string{"Authorization": "Bearer " + tok, "X-Original-URI": gitDoorURI})
	if bearer.Code != http.StatusOK {
		t.Fatalf("bearer status = %d, want 200", bearer.Code)
	}
	records, _ := finishEdgeRecorder(t, recorder, sink)
	if len(records) != 2 {
		t.Fatalf("edge records = %d, want one per allow", len(records))
	}
	if records[0].Detail["decision"] != "allow" || records[0].CorrelationID != basic.Header().Get("X-Correlation-Id") {
		t.Errorf("Basic edge = decision %v correlation %q, response correlation %q", records[0].Detail["decision"], records[0].CorrelationID, basic.Header().Get("X-Correlation-Id"))
	}
	rows, err := d.db.Query(`SELECT details FROM audit_log WHERE event_type = ? ORDER BY rowid`, audit.EventAuthnAllow)
	if err != nil {
		t.Fatalf("query allow audits: %v", err)
	}
	defer rows.Close()
	var details []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan allow audit: %v", err)
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			t.Fatalf("decode allow audit: %v", err)
		}
		details = append(details, item)
	}
	if len(details) != 2 || details[0]["cred"] != "basic" {
		t.Fatalf("allow audit details = %#v, want first cred=basic", details)
	}
	if _, ok := details[1]["cred"]; ok {
		t.Errorf("bearer allow audit unexpectedly has cred: %#v", details[1])
	}
}

// R-3LV6-I4UB
func TestGitDoorBasicTelemetryNeverContainsCredentials(t *testing.T) {
	d := newServerDeps(t)
	recorder, sink := liveEdgeRecorder(t)
	d.recorder = recorder
	h := gitAuthnServer(t, d)
	tok, _ := mintPAT(t, d, "owner@int.ikigenba.com")
	allowHeader := basicAuthorization("git", tok)
	denyPassword := "not-a-personal-access-token"
	denyHeader := basicAuthorization("git", denyPassword)
	if got := doAuthn(h, map[string]string{"Authorization": allowHeader, "X-Original-URI": gitDoorURI}).Code; got != http.StatusOK {
		t.Fatalf("allow status = %d, want 200", got)
	}
	if got := doAuthn(h, map[string]string{"Authorization": denyHeader, "X-Original-URI": gitDoorURI}).Code; got != http.StatusUnauthorized {
		t.Fatalf("deny status = %d, want 401", got)
	}
	records, raw := finishEdgeRecorder(t, recorder, sink)
	if len(records) != 2 {
		t.Fatalf("edge records = %d, want 2", len(records))
	}
	for _, secret := range []string{
		strings.TrimPrefix(allowHeader, "Basic "), strings.TrimPrefix(denyHeader, "Basic "),
		tok, denyPassword, allowHeader, denyHeader, "Authorization",
	} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("telemetry payload leaked %q", secret)
		}
	}
}
