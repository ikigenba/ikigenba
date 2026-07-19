package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"appkit/server"
)

func newIdentityGateServer(t *testing.T, inner http.Handler) http.Handler {
	t.Helper()
	srv, err := server.New(server.Options{
		Addr:       "127.0.0.1:0",
		Logger:     discardLogger(),
		ResourceID: testResourceID,
		AuthServer: testAuthServer,
		Version:    testVersion,
		Service:    testService,
		Register: func(rt *server.Router) error {
			rt.Handle("POST /identity-test", rt.RequireIdentity(inner))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv.Handler
}

func TestIdentityGateRejectsEmailWithoutOwnerID(t *testing.T) {
	calls := 0
	h := newIdentityGateServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/identity-test", nil)
	req.Header.Set("X-Owner-Email", "owner@example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// R-DDVL-DPVB
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if calls != 0 {
		t.Fatalf("inner handler calls = %d, want 0", calls)
	}
	wantMetadata := testResourceID + "/.well-known/oauth-protected-resource"
	if challenge := rr.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, wantMetadata) {
		t.Errorf("WWW-Authenticate = %q, want resource_metadata %q", challenge, wantMetadata)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	wantBody := map[string]any{
		"error":             "unauthorized",
		"error_description": "missing authenticated identity",
	}
	if !reflect.DeepEqual(body, wantBody) {
		t.Errorf("body = %#v, want %#v", body, wantBody)
	}
}

func TestIdentityGateAllowsOwnerIDWithoutEmail(t *testing.T) {
	calls := 0
	h := newIdentityGateServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/identity-test", nil)
	req.Header.Set("X-Owner-Id", "owner_opaque_123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// R-DF3H-RHM0
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if calls != 1 {
		t.Fatalf("inner handler calls = %d, want 1", calls)
	}
}

func TestIdentityGateCopiesAllIdentityHeaders(t *testing.T) {
	var got server.Identity
	h := newIdentityGateServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		got, ok = server.IdentityFrom(r.Context())
		if !ok {
			t.Error("IdentityFrom reported no identity")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/identity-test", nil)
	req.Header.Set("X-Owner-Id", " owner:id/123 ")
	req.Header.Set("X-Owner-Email", "Owner+display@example.com")
	req.Header.Set("X-Owner-Name", "Owner Name")
	req.Header.Set("X-Owner-Picture", "https://images.example/owner.png?size=large")
	req.Header.Set("X-Client-Id", "client:desktop/456")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// R-DGBE-59CP
	want := server.Identity{
		OwnerID:      " owner:id/123 ",
		OwnerEmail:   "Owner+display@example.com",
		OwnerName:    "Owner Name",
		OwnerPicture: "https://images.example/owner.png?size=large",
		ClientID:     "client:desktop/456",
	}
	if got != want {
		t.Errorf("identity = %#v, want headers verbatim as %#v", got, want)
	}
}
