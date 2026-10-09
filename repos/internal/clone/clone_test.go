package clone_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/clone"
)

func servicesFile(t *testing.T, path, name, url string) {
	t.Helper()
	b, e := json.Marshal(map[string]any{"services": []any{map[string]any{"name": name, "url": url, "description": "fixture", "socket": "", "enabled": false, "mcp": false}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func request(host, proto string) *http.Request {
	return &http.Request{Host: host, Header: http.Header{"X-Forwarded-Proto": []string{proto}}}
}

// R-92EH-RPWY
func TestPublishedBaseReadAfresh(t *testing.T) {
	p := filepath.Join(t.TempDir(), "services.json")
	for _, url := range []string{"https://repos.space.example/", "http://repos.dev.localhost:7400", "custom://host/path//"} {
		servicesFile(t, p, "repos", url)
		for _, r := range []*http.Request{nil, request("unrelated:8080", "http"), request("other", "https")} {
			if got := clone.Base(r, p); got != strings.TrimSuffix(url, "/") {
				t.Fatalf("base %q for %q", got, url)
			}
		}
	}
}

// R-93ME-5HNN
func TestFallbackSchemesAndUnusableServices(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed")
	if e := os.WriteFile(malformed, []byte("invalid"), 0600); e != nil {
		t.Fatal(e)
	}
	absent := filepath.Join(dir, "absent")
	other := filepath.Join(dir, "other.json")
	servicesFile(t, other, "telemetry", "https://telemetry.example")
	empty := filepath.Join(dir, "empty.json")
	servicesFile(t, empty, "repos", "")
	for _, p := range []string{"", absent, dir, malformed, other, empty} {
		for _, proto := range []string{"http", "https", "HTTPS", "https, http", "", " http", "http "} {
			wantProto := "https"
			if proto == "http" {
				wantProto = "http"
			}
			r := request("repos.space.example:443", proto)
			r.URL = nil
			if got := clone.Base(r, p); got != wantProto+"://"+r.Host {
				t.Fatalf("Base(%q,%q)=%q", p, proto, got)
			}
		}
	}
}

// R-94UA-J9EC
func TestOtherHeadersDoNotChangeBase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "services.json")
	for _, published := range []bool{false, true} {
		if published {
			servicesFile(t, p, "repos", "http://repos.dev.localhost:7400/")
		}
		plain := request("repos.space.example:443", "https")
		with := request(plain.Host, "https")
		with.Header.Set("Authorization", "Bearer PrivateCredentialValue")
		with.Header.Set("Cookie", "token=PrivateCookieValue")
		with.Header.Set("X-Forwarded-Host", "private.invalid")
		with.Header.Set("Forwarded", "host=private.invalid;proto=http")
		if clone.Base(plain, p) != clone.Base(with, p) {
			t.Fatal("unrelated headers changed base")
		}
	}
}

const helperSuffix = ".helper '!f() { test \"$1\" = get && printf \"username=token\\npassword=%s\\n\" \"$IKIGENBA_TOKEN\"; }; f'"

// R-UWKM-KS3G R-97A3-ASVQ
func TestGuidanceExactTextAndScope(t *testing.T) {
	for _, tt := range []struct{ base, scope string }{
		{"https://repos.sbx.ikigenba.dev", "https://*.sbx.ikigenba.dev"},
		{"http://repos.wip.localhost:7400", "http://*.wip.localhost:7400"},
		{"https://sbx.ikigenba.dev", "https://*.sbx.ikigenba.dev"},
		{"repos.space.example/path", "https://*.space.example"},
		{"", "https://*."}, {"https://repos.", "https://*.repos."},
		{"custom://repos.host:123/path://tail", "custom://*.host:123"},
		{"://host/path", "://*.host"}, {"https://REPOS.host", "https://*.REPOS.host"},
	} {
		g := clone.Guidance(tt.base)
		if g.Intro != clone.Intro || g.Warning != clone.Warning {
			t.Fatalf("guidance text for %q: %+v", tt.base, g)
		}
		if want := "git config --global credential." + tt.scope + helperSuffix; g.Helper != want {
			t.Fatalf("helper %q want %q", g.Helper, want)
		}
	}
}

// R-H4K9-L1MP
func TestDocumentedOriginAndScopeCases(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other.json")
	servicesFile(t, other, "telemetry", "https://telemetry.example")
	dev := filepath.Join(dir, "dev.json")
	servicesFile(t, dev, "repos", "http://repos.wip.localhost:7400")
	space := filepath.Join(dir, "space.json")
	servicesFile(t, space, "repos", "https://repos.sbx.ikigenba.dev/")
	for _, tt := range []struct{ path, host, proto, base, scope string }{
		{other, "repos.sbx.ikigenba.dev:443", "https", "https://repos.sbx.ikigenba.dev:443", "https://*.sbx.ikigenba.dev:443"},
		{other, "repos.sbx.ikigenba.dev", "", "https://repos.sbx.ikigenba.dev", "https://*.sbx.ikigenba.dev"},
		{other, "sbx.ikigenba.dev", "https", "https://sbx.ikigenba.dev", "https://*.sbx.ikigenba.dev"},
		{other, "repos.wip.localhost:7400", "http", "http://repos.wip.localhost:7400", "http://*.wip.localhost:7400"},
		{filepath.Join(dir, "missing"), "repos.sbx.ikigenba.dev", "https", "https://repos.sbx.ikigenba.dev", "https://*.sbx.ikigenba.dev"},
		{dev, "127.0.0.1:8080", "https", "http://repos.wip.localhost:7400", "http://*.wip.localhost:7400"},
		{space, "irrelevant", "http", "https://repos.sbx.ikigenba.dev", "https://*.sbx.ikigenba.dev"},
	} {
		base := clone.Base(request(tt.host, tt.proto), tt.path)
		if base != tt.base {
			t.Fatalf("base %q want %q", base, tt.base)
		}
		h := clone.Guidance(base).Helper
		scope, ok := strings.CutPrefix(h, "git config --global credential.")
		if !ok {
			t.Fatal("missing helper prefix")
		}
		scope, _, ok = strings.Cut(scope, ".helper")
		if !ok || scope != tt.scope {
			t.Fatalf("scope %q want %q", scope, tt.scope)
		}
	}
}

// R-98HZ-OKMF
func TestCredentialsTextPreservesEveryField(t *testing.T) {
	for _, c := range []clone.Credentials{{}, {Intro: "a", Helper: "b", Warning: "c"}, {Intro: "\n", Helper: "h\n\n", Warning: " w "}} {
		if got, want := c.Text(), c.Intro+"\n\n"+c.Helper+"\n\n"+c.Warning; got != want {
			t.Fatalf("text %q want %q", got, want)
		}
	}
}

func TestContextAndURLContracts(t *testing.T) {
	if b, ok := clone.FromContext(t.Context()); b != "" || ok {
		t.Fatal("unrelated context has base")
	}
	ctx := clone.NewContext(t.Context(), "first")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if b, ok := clone.FromContext(ctx); b != "first" || !ok {
		t.Fatal("derived context lost base")
	}
	ctx = clone.NewContext(ctx, "")
	if b, ok := clone.FromContext(ctx); b != "" || !ok {
		t.Fatal("latest empty base not preserved")
	}
	for _, base := range []string{"", "base/", "http://host/path"} {
		for _, name := range []string{"", "notes", "with/slash"} {
			if got := clone.URL(base, name); got != base+"/"+name+".git" {
				t.Fatal("URL altered input")
			}
		}
	}
}

// R-U6YQ-JLIV
func TestGuidanceCopyConstants(t *testing.T) {
	const intro string = clone.Intro
	const warning string = clone.Warning
	for _, value := range []string{intro, warning} {
		if value == "" || strings.Contains(value, "\n") {
			t.Fatalf("invalid guidance constant: %q", value)
		}
	}
}
