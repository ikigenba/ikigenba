package urls_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

// R-BZLI-SBPV R-C0TF-63GK R-C21B-JV79 R-C397-XMXY R-C4H4-BEON
// R-C5P0-P6FC R-C6WX-2Y61 R-C84T-GPWQ R-CAKM-89E4
func TestPublicSignatures(t *testing.T) {
	var (
		scheme  func(*http.Request) string
		sites   func(*http.Request, string) string
		site    func(string, string) string
		apex    func(*http.Request, string) string
		profile func(*http.Request, string) string
		logout  func(*http.Request, string) string
		signin  func(*http.Request) string
		put     func(context.Context, string) context.Context
		get     func(context.Context) (string, bool)
	)
	scheme = urls.Scheme
	sites = urls.SitesURL
	site = urls.SiteURL
	apex = urls.ApexBase
	profile = urls.AuthProfile
	logout = urls.AuthLogout
	signin = urls.SignIn
	put = urls.NewContext
	get = urls.FromContext
	r := request("sites.space", "/", "http")
	if scheme(r) != "http" || sites(r, "") != "http://sites.space" || site("base", "slug") != "base/slug/" || apex(r, "") != "http://sites.sites.space" || profile(r, "") != "http://auth.space/" || logout(r, "") != "http://auth.space/logout" || signin(r) == "" {
		t.Fatal("address declaration use failed")
	}
	if got, ok := get(put(context.Background(), "base")); !ok || got != "base" {
		t.Fatal(got, ok)
	}
}

func request(host, uri, proto string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, uri, nil)
	r.Host = host
	r.Header.Set("X-Forwarded-Proto", proto)
	return r
}

// R-CBSI-M14T
func TestScheme(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"https", "https"}, {"HTTPS", "https"}, {"HtTp", "http"}, {"http", "http"}, {"", "https"}, {"ftp", "https"}, {"https, http", "https"}, {" http", "https"}, {"HTTP ", "https"}} {
		r := request("host", "/", tc.input)
		r.Header.Add("X-Forwarded-Proto", "http")
		if got := urls.Scheme(r); got != tc.want {
			t.Errorf("Scheme(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
	r := request("host", "/", "")
	r.Header.Del("X-Forwarded-Proto")
	if urls.Scheme(r) != "https" {
		t.Fatal("missing header")
	}
}

func servicesFile(t *testing.T, path, sites, auth string) {
	t.Helper()
	var entries []map[string]any
	for name, addr := range map[string]string{"sites": sites, "auth": auth} {
		entries = append(entries, map[string]any{"name": name, "url": addr, "description": "", "socket": "", "enabled": false, "mcp": false})
	}
	b, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

// R-CD0E-ZSVI R-CE8B-DKM7 R-CGO4-543L R-CJ3W-WNKZ
func TestListedServicesAreReadAfresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	r := request("sites.space:7443", "/", "http")
	for _, tc := range []struct{ sites, auth, wantSites, wantAuth string }{
		{"https://published/sites/", "https://accounts", "https://published/sites", "https://accounts"},
		{"https://changed//", "https://accounts/", "https://changed/", "https://accounts/"},
		{"", "", "http://sites.space:7443", "http://auth.space"},
	} {
		servicesFile(t, path, tc.sites, tc.auth)
		if got := urls.SitesURL(r, path); got != tc.wantSites {
			t.Errorf("SitesURL=%q want %q", got, tc.wantSites)
		}
		wantApex := tc.wantSites
		if tc.sites == "" {
			wantApex = "http://sites.sites.space:7443"
		}
		if got := urls.ApexBase(r, path); got != wantApex {
			t.Errorf("ApexBase=%q want %q", got, wantApex)
		}
		if got := urls.AuthProfile(r, path); got != tc.wantAuth+"/" {
			t.Errorf("AuthProfile=%q", got)
		}
		if got := urls.AuthLogout(r, path); got != tc.wantAuth+"/logout" {
			t.Errorf("AuthLogout=%q", got)
		}
	}
}

// R-CD0E-ZSVI R-CE8B-DKM7 R-CGO4-543L R-CJ3W-WNKZ
func TestServicesFailuresAndMissingEntries(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"services":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", filepath.Join(dir, "missing"), invalid, empty, dir} {
		r := request("space:80", "/", "http")
		if got := urls.SitesURL(r, path); got != "http://space:80" {
			t.Errorf("SitesURL=%q", got)
		}
		if got := urls.ApexBase(r, path); got != "http://sites.space:80" {
			t.Errorf("ApexBase=%q", got)
		}
		if got := urls.AuthProfile(r, path); got != "http://auth.space/" {
			t.Errorf("AuthProfile=%q", got)
		}
		if got := urls.AuthLogout(r, path); got != "http://auth.space/logout" {
			t.Errorf("AuthLogout=%q", got)
		}
	}
}

// R-CFG7-RCCW
func TestSiteURLExactConcatenation(t *testing.T) {
	for _, tc := range []struct{ base, slug, want string }{{"https://sites.space", "blog", "https://sites.space/blog/"}, {"base/", "a/b", "base//a/b/"}, {"", "", "//"}, {"base?x=1", "a b", "base?x=1/a b/"}} {
		if got := urls.SiteURL(tc.base, tc.slug); got != tc.want {
			t.Errorf("SiteURL=%q want %q", got, tc.want)
		}
	}
}

// R-CHW0-IVUA R-CJ3W-WNKZ R-CKBT-AFBO
func TestAuthSpaceAndSignIn(t *testing.T) {
	for _, tc := range []struct{ host, authSpace, signinSpace string }{
		{"sites.sbx.ikigenba.dev:443", "sbx.ikigenba.dev", "sbx.ikigenba.dev:443"},
		{"SiTeS.Example:7400", "Example", "Example:7400"},
		{"sites.", "sites.", "sites."}, {"sites.:80", "sites.", ":80"},
		{"sites.example:", "example", "example:"},
		{"sites.example:abc", "example:abc", "example:abc"},
		{"other.example:80", "other.example", "other.example:80"},
		{"sites.example:12:34", "example:12", "example:12:34"},
	} {
		for _, proto := range []string{"https", "HTTPS", "http", "", "ftp"} {
			r := request(tc.host, "/about?from=launcher&space=a+b", proto)
			scheme := "https"
			if proto == "http" {
				scheme = "http"
			}
			if got := urls.AuthProfile(r, ""); got != scheme+"://auth."+tc.authSpace+"/" {
				t.Errorf("host %q profile=%q", tc.host, got)
			}
			if got := urls.AuthLogout(r, ""); got != scheme+"://auth."+tc.authSpace+"/logout" {
				t.Errorf("host %q logout=%q", tc.host, got)
			}
			want := scheme + "://auth." + tc.signinSpace + "/?return=" + url.QueryEscape(scheme+"://"+tc.host+r.RequestURI)
			if got := urls.SignIn(r); got != want {
				t.Errorf("host %q SignIn=%q want %q", tc.host, got, want)
			}
		}
	}
}

// R-CLJP-O72D
func TestContextAncestry(t *testing.T) {
	root := context.Background()
	if got, ok := urls.FromContext(root); got != "" || ok {
		t.Fatal(got, ok)
	}
	for _, s := range []string{"", "https://sites.space/", "arbitrary bytes \x00"} {
		ctx := urls.NewContext(root, s)
		derived, cancel := context.WithCancel(ctx)
		cancel()
		if got, ok := urls.FromContext(derived); got != s || !ok {
			t.Fatal(got, ok)
		}
		if got, ok := urls.FromContext(urls.NewContext(derived, "inner")); got != "inner" || !ok {
			t.Fatal(got, ok)
		}
	}
}
