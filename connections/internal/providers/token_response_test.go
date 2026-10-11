package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/connections/internal/providers"
)

func tokenClient(t *testing.T, body *string, status *int) providers.Client {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(*status)
		_, _ = w.Write([]byte(*body))
	}))
	t.Cleanup(fake.Close)
	p := providers.Table()[0]
	p.AuthorizeURL = fake.URL + "/authorize"
	p.TokenURL = fake.URL + "/token"
	p.RevokeURL = fake.URL + "/revoke"
	return providers.Client{Provider: p, ID: "client", Secret: "secret", HTTP: fake.Client(), Now: func() time.Time {
		return time.Date(2032, 3, 4, 5, 6, 7, 987654321, time.FixedZone("test", -7200))
	}}
}

// R-P4HO-2G5U R-X9ZZ-8RD5
func TestUsableTokenResponse(t *testing.T) {
	body, status := "", 200
	c := tokenClient(t, &body, &status)
	id := jwt(`{"email":"email@example.test","sub":"subject"}`)
	base := func() map[string]any {
		return map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": json.RawMessage("3600"), "id_token": id}
	}
	calls := []struct {
		name string
		call func() (providers.Grant, error)
	}{
		{"exchange", func() (providers.Grant, error) {
			return c.Exchange(context.Background(), "code", "verifier", "https://callback.test")
		}},
		{"refresh", func() (providers.Grant, error) { return c.Refresh(context.Background(), "stored") }},
	}
	for _, operation := range calls {
		t.Run(operation.name, func(t *testing.T) {
			for _, st := range []int{200, 201, 299} {
				status = st
				for _, number := range []string{"1", "1.0", "1e0", "10e-1", "3600", "3600.0", "3.6e3", "31536000", "31536000.000", "3.1536e7"} {
					m := base()
					m["expires_in"] = json.RawMessage(number)
					encoded, err := json.Marshal(m)
					if err != nil {
						t.Fatal(err)
					}
					body = string(encoded)
					if _, err = operation.call(); err != nil {
						t.Fatalf("status %d expires_in %s: %v", st, number, err)
					}
				}
			}
			status = 200
			badBodies := []string{"[]", "null", "{}", "not json"}
			for _, field := range []string{"access_token", "expires_in", "refresh_token", "scope", "id_token"} {
				values := []any{nil, true, []string{"string"}, map[string]string{"key": "string"}}
				if field != "expires_in" {
					values = append(values, 3)
				}
				for _, value := range values {
					m := base()
					m[field] = value
					encoded, err := json.Marshal(m)
					if err != nil {
						t.Fatal(err)
					}
					badBodies = append(badBodies, string(encoded))
				}
			}
			for _, number := range []string{"null", `"3600"`, "0", "-1", "1.5", "3600.0000000000001", "31536001", "3.1536001e7", "31536000.0000000001", "1e100", "1e-100", "10000000000"} {
				m := base()
				m["expires_in"] = json.RawMessage(number)
				encoded, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				badBodies = append(badBodies, string(encoded))
			}
			for _, field := range []string{"access_token", "expires_in", "refresh_token"} {
				if field == "refresh_token" && operation.name == "refresh" {
					continue
				}
				for _, absent := range []bool{true, false} {
					if field == "expires_in" && !absent {
						continue
					}
					m := base()
					if absent {
						delete(m, field)
					} else {
						m[field] = ""
					}
					encoded, err := json.Marshal(m)
					if err != nil {
						t.Fatal(err)
					}
					badBodies = append(badBodies, string(encoded))
				}
			}
			for _, st := range []int{200, 201, 299} {
				status = st
				for _, invalid := range badBodies {
					body = invalid
					g, err := operation.call()
					if !errors.Is(err, providers.ErrFailed) || providers.HTTPStatus(err) != status {
						t.Fatalf("body %s: grant %#v, error %v, status %d", body, g, err, providers.HTTPStatus(err))
					}
					equal(t, g, providers.Grant{})
				}
			}
		})
	}
}

// R-XDNO-E2L8
func TestExchangeGrantFields(t *testing.T) {
	body, status := "", 200
	c := tokenClient(t, &body, &status)
	p := &c.Provider
	defaultScopes := []string{}
	for _, scope := range p.Scopes {
		defaultScopes = append(defaultScopes, p.GrantedName(scope))
	}
	orderedScope := " extra-first  " + p.Scopes[4] + " " + p.GrantedName("email") + " extra-second openid " + p.Scopes[2] + " " + p.Scopes[3] + " extra-first "
	for _, custom := range []bool{false, true} {
		p.Account = nil
		account := providers.Account{Email: "email@example.test", Subject: "subject"}
		id := jwt(`{"email":"email@example.test","sub":"subject"}`)
		if custom {
			id = "custom-account-token"
			account = providers.Account{Email: "custom@example.test", Subject: "custom-subject"}
			p.Account = func(received string) (providers.Account, error) {
				if received != id {
					t.Errorf("account got %q want %q", received, id)
				}
				return account, nil
			}
		}
		for _, scope := range []string{"absent", "", "   ", orderedScope} {
			m := map[string]any{"access_token": "issued-access", "refresh_token": "issued-refresh", "expires_in": json.RawMessage("3.6e3"), "id_token": id}
			if scope != "absent" {
				m["scope"] = scope
			}
			encoded, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			body = string(encoded)
			g, err := c.Exchange(context.Background(), "code", "verifier", "https://callback.test")
			if err != nil {
				t.Fatal(err)
			}
			wantScopes := append([]string{}, defaultScopes...)
			if scope == orderedScope {
				wantScopes = append(wantScopes, "extra-first", "extra-second")
			}
			equal(t, g, providers.Grant{AccessToken: "issued-access", RefreshToken: "issued-refresh", Expires: c.Now().UTC().Truncate(time.Second).Add(time.Hour), Scopes: wantScopes, Account: account})
		}
	}
}

// R-XEVK-RUBX
func TestRefreshGrantFields(t *testing.T) {
	body, status := "", 200
	c := tokenClient(t, &body, &status)
	c.Provider.Account = func(string) (providers.Account, error) {
		t.Error("refresh called account extractor")
		return providers.Account{}, providers.ErrAccount
	}
	for _, refresh := range []string{"absent", "", "rotated"} {
		for _, scope := range []string{"absent", "", "   ", " extra-second openid extra-first extra-second "} {
			for _, id := range []string{"absent", "", "not-a-jwt", jwt(`{"email":"ignored@example.test","sub":"ignored"}`)} {
				m := map[string]any{"access_token": "new-access", "expires_in": json.RawMessage("31536000.0")}
				for key, value := range map[string]string{"refresh_token": refresh, "scope": scope, "id_token": id} {
					if value != "absent" {
						m[key] = value
					}
				}
				encoded, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				body = string(encoded)
				g, err := c.Refresh(context.Background(), "stored-refresh")
				if err != nil {
					t.Fatalf("refresh=%q scope=%q id=%q: %v", refresh, scope, id, err)
				}
				want := providers.Grant{AccessToken: "new-access", Expires: c.Now().UTC().Truncate(time.Second).Add(31536000 * time.Second)}
				if refresh != "absent" {
					want.RefreshToken = refresh
				}
				if scope != "absent" && scope != "" && scope != "   " {
					want.Scopes = []string{"openid", "extra-second", "extra-first"}
				}
				equal(t, g, want)
			}
		}
	}
}
