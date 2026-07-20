package githubidp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAuthorizeURLCarriesOnlyGitHubAppParameters(t *testing.T) {
	// R-I4AP-U132
	provider := New(Credentials{ClientID: "github-client"})
	raw := provider.AuthorizeURL("state-123", "https://dashboard.example/auth/github/callback")

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q): %v", raw, err)
	}
	if got, want := parsed.Scheme+"://"+parsed.Host+parsed.Path, "https://github.com/login/oauth/authorize"; got != want {
		t.Fatalf("authorization endpoint = %q, want %q", got, want)
	}
	want := url.Values{
		"client_id":    {"github-client"},
		"redirect_uri": {"https://dashboard.example/auth/github/callback"},
		"state":        {"state-123"},
	}
	if got := parsed.Query(); !reflect.DeepEqual(got, want) {
		t.Errorf("query = %#v, want exactly %#v", got, want)
	}
	if _, present := parsed.Query()["scope"]; present {
		t.Error("scope parameter is present")
	}
}

func TestExchangeCodeBuildsIdentityFromProfilePrimaryEmailAndMembership(t *testing.T) {
	// R-I5IM-7STR
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearerToken(t, r)
		switch r.URL.Path {
		case "/login/oauth/access_token":
			writeJSON(t, w, map[string]any{"access_token": "short-lived-token"})
		case "/user":
			writeJSON(t, w, map[string]any{
				"id": 583231, "login": "octocat", "name": "The Octocat",
				"avatar_url": "https://avatars.example/octocat", "email": nil,
			})
		case "/user/emails":
			writeJSON(t, w, []map[string]any{
				{"email": "secondary@example.com", "primary": false, "verified": true},
				{"email": "primary@example.com", "primary": true, "verified": true},
			})
		case "/user/memberships/orgs/ikigenba":
			writeJSON(t, w, map[string]any{"state": "active"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := fakeBackedProvider(server.URL)
	identity, err := provider.ExchangeCode(context.Background(), "one-time-code", "https://dashboard.example/callback")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	want := Identity{
		Iss:           "https://github.com",
		Sub:           "583231",
		Login:         "octocat",
		Email:         "primary@example.com",
		EmailVerified: true,
		Name:          "The Octocat",
		Picture:       "https://avatars.example/octocat",
		OrgMembership: "active",
	}
	if identity != want {
		t.Errorf("identity = %#v, want %#v", identity, want)
	}
}

func TestExchangeCodeRejectsOAuthErrorAndSendsExchangeCredentials(t *testing.T) {
	// R-I6QI-LKKG
	var requestChecked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		want := url.Values{
			"client_id":     {"client-id"},
			"client_secret": {"client-secret"},
			"code":          {"bad-code"},
			"redirect_uri":  {"https://dashboard.example/callback"},
		}
		if !reflect.DeepEqual(r.PostForm, want) {
			t.Errorf("exchange form = %#v, want %#v", r.PostForm, want)
		}
		requestChecked = true
		writeJSON(t, w, map[string]any{
			"error":             "bad_verification_code",
			"error_description": "The code passed is incorrect or expired.",
		})
	}))
	defer server.Close()

	provider := fakeBackedProvider(server.URL)
	_, err := provider.ExchangeCode(context.Background(), "bad-code", "https://dashboard.example/callback")
	if err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Fatalf("ExchangeCode error = %v, want bad_verification_code error", err)
	}
	if !requestChecked {
		t.Error("token exchange request was not checked")
	}
}

func TestExchangeCodeHandlesNullNameAndMembershipResponses(t *testing.T) {
	// R-I7YE-ZCB5
	t.Run("null name and missing membership are facts", func(t *testing.T) {
		server := newIdentityServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		defer server.Close()

		identity, err := fakeBackedProvider(server.URL).ExchangeCode(context.Background(), "code", "https://dashboard.example/callback")
		if err != nil {
			t.Fatalf("ExchangeCode: %v", err)
		}
		if identity.Name != "" {
			t.Errorf("Name = %q, want empty for JSON null", identity.Name)
		}
		if identity.OrgMembership != "" {
			t.Errorf("OrgMembership = %q, want empty for 404", identity.OrgMembership)
		}
	})

	t.Run("membership server error is retried once then returned", func(t *testing.T) {
		var calls atomic.Int32
		server := newIdentityServer(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			http.Error(w, "unavailable", http.StatusInternalServerError)
		})
		defer server.Close()

		_, err := fakeBackedProvider(server.URL).ExchangeCode(context.Background(), "code", "https://dashboard.example/callback")
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("ExchangeCode error = %v, want membership 500 error", err)
		}
		if got := calls.Load(); got != 2 {
			t.Errorf("membership calls = %d, want 2", got)
		}
	})
}

func TestExchangeCodeRejectsEmailResponseWithoutPrimaryAddress(t *testing.T) {
	// R-I96B-D41U
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			writeJSON(t, w, map[string]any{"access_token": "token"})
		case "/user":
			writeJSON(t, w, map[string]any{"id": 583231, "login": "octocat", "name": "Octocat"})
		case "/user/emails":
			writeJSON(t, w, []map[string]any{{"email": "secondary@example.com", "primary": false, "verified": true}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := fakeBackedProvider(server.URL).ExchangeCode(context.Background(), "code", "https://dashboard.example/callback")
	if err == nil || !strings.Contains(err.Error(), "no primary") {
		t.Fatalf("ExchangeCode error = %v, want no-primary-address error", err)
	}
}

func fakeBackedProvider(baseURL string) *github {
	provider := New(Credentials{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Org:          "ikigenba",
	}).(*github)
	provider.webBase = baseURL
	provider.apiBase = baseURL
	provider.httpClient = http.DefaultClient
	return provider
}

func newIdentityServer(t *testing.T, membership http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			writeJSON(t, w, map[string]any{"access_token": "token"})
		case "/user":
			_, _ = w.Write([]byte(`{"id":583231,"login":"octocat","name":null,"avatar_url":"https://avatars.example/octocat"}`))
		case "/user/emails":
			writeJSON(t, w, []map[string]any{{"email": "primary@example.com", "primary": true, "verified": true}})
		case "/user/memberships/orgs/ikigenba":
			membership(w, r)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
}

func assertBearerToken(t *testing.T, r *http.Request) {
	t.Helper()
	if r.URL.Path == "/login/oauth/access_token" {
		return
	}
	if got := r.Header.Get("Authorization"); got != "Bearer short-lived-token" {
		t.Errorf("Authorization = %q, want bearer user token", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
