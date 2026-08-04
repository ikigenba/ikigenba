package gh

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestNewClientUsesInjectedHTTPClientForTokenExchangeAndREST_R_05B3_MHSB(t *testing.T) {
	// R-05B3-MHSB
	key := mustRSAKey(t)
	keyPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	var paths []string
	hc := stubClient(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/orgs/acme/installation":
			return jsonResponse(http.StatusOK, `{"id":42}`), nil
		case "/app/installations/42/access_tokens":
			return jsonResponse(http.StatusCreated, `{"token":"injected-token","expires_at":"2026-07-04T12:10:00Z"}`), nil
		case "/repos/acme/widgets":
			return jsonResponse(http.StatusOK, `{"name":"widgets"}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})
	withAPIBase(t, "https://offline.github.test")

	c, err := NewClient(Config{AppID: "12345", Org: "acme", PrivateKey: keyPEM}, hc)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	c.ts.now = func() time.Time { return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC) }
	if _, err := c.RepoGet(context.Background(), "widgets"); err != nil {
		t.Fatalf("RepoGet() error = %v", err)
	}

	want := []string{
		"/orgs/acme/installation",
		"/app/installations/42/access_tokens",
		"/repos/acme/widgets",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("injected client paths = %v, want token exchange and REST paths %v", paths, want)
	}
}

func TestNilClientFallbacksUseThirtySecondInstrumentedClients(t *testing.T) {
	c := &Client{}
	if got := c.client(); got.Timeout != 30*time.Second || got.Transport == nil {
		t.Fatalf("Client fallback = {Timeout:%v Transport:%T}, want 30s instrumented client", got.Timeout, got.Transport)
	}
	src := &tokenSource{}
	if got := src.client(); got.Timeout != 30*time.Second || got.Transport == nil {
		t.Fatalf("tokenSource fallback = {Timeout:%v Transport:%T}, want 30s instrumented client", got.Timeout, got.Transport)
	}
}
