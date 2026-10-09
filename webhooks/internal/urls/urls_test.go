package urls_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/webhooks/internal/urls"
)

func check(t *testing.T, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}

func req(host, proto, target string) *http.Request {
	r := httptest.NewRequest("GET", target, nil)
	r.Host = host
	if proto != "" {
		r.Header.Set("X-Forwarded-Proto", proto)
	}
	return r
}

func TestDeclaredNames(t *testing.T) {
	// R-0DZM-TB8A
	check(t, urls.Service, "webhooks")
	check(t, urls.IngressPrefix, "/in/")
	var (
		scheme  func(*http.Request) string
		base    func(*http.Request, string) string
		hook    func(string, string) string
		auth    func(*http.Request, string) string
		signIn  func(*http.Request, string) string
		newCtx  func(context.Context, string) context.Context
		fromCtx func(context.Context) (string, bool)
	)
	scheme = urls.Scheme
	base = urls.Base
	hook = urls.Hook
	auth = urls.Auth
	signIn = urls.SignIn
	newCtx = urls.NewContext
	fromCtx = urls.FromContext
	_, _, _, _, _, _, _ = scheme, base, hook, auth, signIn, newCtx, fromCtx
}

func TestBehaviour(t *testing.T) {
	// R-0F7J-72YZ
	for _, c := range []struct{ proto, want string }{{"http", "http"}, {"HTTP", "http"}, {"Https", "https"}, {"", "https"}, {"ftp", "https"}, {"http, https", "https"}} {
		check(t, urls.Scheme(req("h", c.proto, "/")), c.want)
	}
	check(t, urls.Hook("https://b.example.test", "gh_push"), "https://b.example.test/in/gh_push")
	dir := t.TempDir()
	path := filepath.Join(dir, "services.json")
	r := req("webhooks.sbx.example.test:8443", "http", "/x?y=z")
	check(t, urls.Base(r, path), "http://webhooks.sbx.example.test:8443")
	check(t, urls.Base(r, ""), "http://webhooks.sbx.example.test:8443")
	check(t, urls.Auth(r, path), "http://auth.sbx.example.test")
	check(t, urls.SignIn(r, path), "http://auth.sbx.example.test/?return="+url.QueryEscape("http://webhooks.sbx.example.test:8443/x?y=z"))
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"webhooks","url":"https://hooks.example.test/","description":"d","socket":"/s","enabled":true,"mcp":true},{"name":"auth","url":"https://accounts.example.test","description":"d","socket":"/s","enabled":true,"mcp":false}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	check(t, urls.Base(r, path), "https://hooks.example.test")
	check(t, urls.Auth(r, path), "https://accounts.example.test")
	ctx := urls.NewContext(context.Background(), "https://base.example.test")
	got, ok := urls.FromContext(ctx)
	check(t, got, "https://base.example.test")
	check(t, ok, true)
	_, ok = urls.FromContext(context.Background())
	check(t, ok, false)
}
