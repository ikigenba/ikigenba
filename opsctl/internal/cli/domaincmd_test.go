package cli_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/domain"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
	"github.com/ikigenba/ikigenba/opsctl/internal/services"
)

const wantDomainUsage = `Usage: opsctl domain <subcommand> [arguments]

Manage the host's custom domains: names outside host.name whose A record points
at this host. A primary name is served by the sites app, which decides the site;
a redirect name answers 301 to its primary. Each name has a certificate of its
own, obtained by HTTP-01 from /var/www/acme.

Subcommands:
  add NAME     obtain NAME's certificate, add NAME, and apply nginx
  remove NAME  remove NAME, apply nginx, and delete NAME's certificate; refused
               while a redirect name points at NAME
  list         print each name, and a redirect name's primary

Options (add):
  --redirect PRIMARY  make NAME redirect to PRIMARY, a listed primary name

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at
  host.apex   the app that answers at the parent of host.name; unset means none

NAME is a host name in lower-case ASCII; an internationalized name is given in
its xn-- form. It may not be host.name or a name under it, nor, while host.apex
is set, the parent of host.name. Renewal is certbot's: the timer 'opsctl init'
writes, ikigenba-renew-certificate.timer, renews every name's certificate.
`

type domainCLI struct {
	t       *testing.T
	deps    cli.Deps
	calls   []host.Command
	execute func(host.Command) (host.Result, error)
}

func newDomainCLI(t *testing.T) *domainCLI {
	t.Helper()
	f := &domainCLI{t: t}
	f.deps = cli.Deps{Root: t.TempDir(), EUID: 0, Now: func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) }}
	forbidden := func() { t.Fatal("domain used network dependency") }
	f.deps.LookupHost = func(context.Context, string) ([]string, error) { forbidden(); return nil, nil }
	f.deps.DNS = dns.Env{Open: func(context.Context, string) (dns.Provider, error) { forbidden(); return nil, nil }, LookupNS: func(context.Context, string) ([]string, error) { forbidden(); return nil, nil }}
	f.deps.Cloud = cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { forbidden(); return nil, nil }}
	f.deps.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
		f.calls = append(f.calls, c)
		if f.execute != nil {
			return f.execute(c)
		}
		if result, ok := servicesFixtureCommand(c); ok {
			return result, nil
		}
		return host.Result{}, nil
	}
	return f
}
func (f *domainCLI) config(values map[string]string) {
	f.t.Helper()
	for key, value := range values {
		if err := (config.Store{Root: f.deps.Root}).Set(key, value); err != nil {
			f.t.Fatal(err)
		}
	}
}
func (f *domainCLI) setup() {
	f.t.Helper()
	f.config(map[string]string{"host.name": "SBX.IKIGENBA.DEV.", "acme.email": "operator@example.com"})
	f.write("etc/nginx/conf.d/ikigenba.conf", "previous nginx")
	f.write("run/ikigenba/services.json", "previous services")
}
func (f *domainCLI) write(path, body string) {
	f.t.Helper()
	path = filepath.Join(f.deps.Root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *domainCLI) run(args ...string) (string, string, int) {
	return invoke(append([]string{"domain"}, args...), f.deps)
}
func (f *domainCLI) lineage(name string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Join(f.deps.Root, "etc/letsencrypt/live", name), 0750); err != nil {
		f.t.Fatal(err)
	}
}
func (f *domainCLI) list() []domain.Domain {
	f.t.Helper()
	list, err := domain.List(config.Store{Root: f.deps.Root})
	if err != nil {
		f.t.Fatal(err)
	}
	return list
}
func (f *domainCLI) unchanged(before map[string]treeEntry) {
	f.t.Helper()
	if after := treeState(f.t, f.deps.Root); !reflect.DeepEqual(before, after) {
		f.t.Fatalf("state changed: before %#v after %#v", before, after)
	}
}
func assertDomainResult(t *testing.T, out, diagnostic string, code int, wantOut, wantErr string, wantCode int) {
	t.Helper()
	if out != wantOut || diagnostic != wantErr || code != wantCode {
		t.Fatalf("exit %d stdout %q stderr %q; want %d %q %q", code, out, diagnostic, wantCode, wantOut, wantErr)
	}
}

func TestDomainHelpAndRootPrecedence(t *testing.T) {
	// R-9YZN-A4NT R-A1FG-1O57 R-63B7-WLBZ
	for _, uid := range []int{0, 1000} {
		for _, flag := range []string{"--help", "-h"} {
			f := newDomainCLI(t)
			f.deps.EUID = uid
			writeCorruptCLIConfigFile(t, f.deps.Root)
			noAccess := observeFilesystemAccess(t, f.deps.Root)
			out, diagnostic, code := f.run(flag)
			noAccess()
			assertDomainResult(t, out, diagnostic, code, wantDomainUsage, "", 0)
			if len(f.calls) != 0 {
				t.Fatal(f.calls)
			}
		}
	}
	for _, args := range [][]string{nil, {"wat"}, {"add"}, {"list", "--bogus"}, {"add", "Bad", "--redirect"}, {"--help", "extra"}} {
		f := newDomainCLI(t)
		f.deps.EUID = 1000
		writeCorruptCLIConfigFile(t, f.deps.Root)
		noAccess := observeFilesystemAccess(t, f.deps.Root)
		out, diagnostic, code := f.run(args...)
		noAccess()
		assertDomainResult(t, out, diagnostic, code, "", "opsctl: must run as root\n", 3)
		if len(f.calls) != 0 {
			t.Fatal(f.calls)
		}
	}
}
func TestDomainGrammarIsInert(t *testing.T) {
	// R-9XRQ-WCX4 R-KXVE-C70N R-KZ3A-PYRC R-L0B7-3QI1 R-L1J3-HI8Q
	cases := []struct {
		args    []string
		message string
	}{
		{nil, "no domain subcommand given"}, {[]string{"delete", "example.com"}, "unknown domain subcommand 'delete'"},
		{[]string{"del\nete\r\n\\n\t'\xff", "example.com"}, "unknown domain subcommand 'del\\nete\\r\\n\\n\t'\xff'"},
		{[]string{"add", "example.com", "--bogus"}, "unknown option '--bogus'"}, {[]string{"add", "www.example.com", "--redirect"}, "option '--redirect' needs a value"},
		{[]string{"remove", "www.example.com", "--redirect", "example.com"}, "unknown option '--redirect'"}, {[]string{"list", "bad", "--bogus"}, "unknown option '--bogus'"},
		{[]string{"list", "--bad\nname"}, "unknown option '--bad\\nname'"},
		{[]string{"add", "bad", "--first\r\n\\r\t'\xff", "--second"}, "unknown option '--first\\r\\n\\r\t'\xff'"},
		{[]string{"remove", "--bad\rname", "example.com"}, "unknown option '--bad\\rname'"},
		{[]string{"add", "bad", "extra", "--bogus"}, "unknown option '--bogus'"}, {[]string{"add", "--redirect", "--bogus", "example.com"}, "invalid name: --bogus"},
		{[]string{"add", "--redirect", "--redirect", "example.com"}, "invalid name: --redirect"},
		{[]string{"add", "bad", "--first", "--redirect"}, "unknown option '--first'"},
		{[]string{"add", "--redirect", "example.com"}, "domain add needs NAME"}, {[]string{"add"}, "domain add needs NAME"}, {[]string{"remove"}, "domain remove needs NAME"},
		{[]string{"add", "bad", "second"}, "domain add takes one NAME"}, {[]string{"remove", "bad", "second"}, "domain remove takes one NAME"}, {[]string{"list", "one"}, "domain list takes no arguments"},
		{[]string{"add", "bad", "second", "--redirect", "also-bad"}, "domain add takes one NAME"},
		{[]string{"add", "Example.com"}, "invalid name: Example.com"}, {[]string{"remove", "exa_mple.com"}, "invalid name: exa_mple.com"}, {[]string{"add", "bücher.de"}, "invalid name: bücher.de"},
		{[]string{"add", "bad\nname\r\n\\n\t'\xff"}, "invalid name: bad\\nname\\r\\n\\n\t'\xff"},
		{[]string{"remove", "bad\rname"}, "invalid name: bad\\rname"},
		{[]string{"add", "example.com", "--redirect", "bad\r\nname\\r\t'\xff"}, "invalid name: bad\\r\\nname\\r\t'\xff"},
		{[]string{"add", "Bad", "--redirect", "AlsoBad"}, "invalid name: Bad"}, {[]string{"add", "www.example.com", "--redirect", "Bad"}, "invalid name: Bad"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f := newDomainCLI(t)
			writeCorruptCLIConfigFile(t, f.deps.Root)
			noAccess := observeFilesystemAccess(t, f.deps.Root)
			out, diagnostic, code := f.run(tc.args...)
			noAccess()
			assertDomainResult(t, out, diagnostic, code, "", "opsctl: "+tc.message+"\n\nsee 'opsctl domain --help' for usage\n", 2)
			if len(f.calls) != 0 {
				t.Fatal(f.calls)
			}
		})
	}
	for _, args := range [][]string{{"add", "example.com", "--redirect", "a.com", "--redirect"}, {"add", "--redirect", "a.com", "--redirect", "b.com", "example.com"}} {
		f := newDomainCLI(t)
		noAccess := observeFilesystemAccess(t, f.deps.Root)
		out, diagnostic, code := f.run(args...)
		noAccess()
		if code != 2 || out != "" || !strings.HasPrefix(diagnostic, "opsctl: ") || !strings.HasSuffix(diagnostic, "\n\nsee 'opsctl domain --help' for usage\n") || len(f.calls) != 0 {
			t.Fatal(code, out, diagnostic, f.calls)
		}
	}
}

func TestDomainConfigAndListErrors(t *testing.T) {
	// R-L2QZ-V9ZF R-L3YW-91Q4 R-A9YQ-Q2C2
	for _, sub := range []string{"add", "remove"} {
		for _, tc := range []struct {
			values map[string]string
			want   string
		}{
			{nil, "host.name not set"}, {map[string]string{"host.name": "."}, "host.name not set"},
			{map[string]string{"host.name": "", "acme.email": "", "host.domains": "Bad"}, "host.name not set"},
			{map[string]string{"host.name": "SBX.IKIGENBA.DEV.", "host.domains": "Bad"}, "acme.email not set"},
			{map[string]string{"host.name": "EXAMPLE.COM.", "acme.email": "a@b.com", "host.apex": "sites", "host.domains": "Bad"}, "host.apex is set but host.name 'example.com' has no parent domain"},
			{map[string]string{"host.name": "sbx.ikigenba.dev", "acme.email": "", "host.apex": "sites"}, "acme.email not set"},
		} {
			if sub == "remove" && tc.want == "acme.email not set" {
				continue
			}
			f := newDomainCLI(t)
			f.config(tc.values)
			before := treeState(t, f.deps.Root)
			out, diagnostic, code := f.run(sub, "sbx.ikigenba.dev")
			assertDomainResult(t, out, diagnostic, code, "", "opsctl: "+tc.want+"\n", 1)
			f.unchanged(before)
			if len(f.calls) != 0 {
				t.Fatal(f.calls)
			}
		}
	}
	for _, sub := range []string{"add", "remove", "list"} {
		f := newDomainCLI(t)
		f.config(map[string]string{"host.name": "sbx.ikigenba.dev", "acme.email": "a@b.com", "host.domains": "example.com,Example.org"})
		before := treeState(t, f.deps.Root)
		args := []string{sub}
		if sub != "list" {
			args = append(args, "example.com")
		}
		out, diagnostic, code := f.run(args...)
		assertDomainResult(t, out, diagnostic, code, "", "opsctl: host.domains malformed: \"Example.org\"\n", 1)
		f.unchanged(before)
		if len(f.calls) != 0 {
			t.Fatal(f.calls)
		}
	}
	for _, sub := range []string{"add", "remove", "list"} {
		f := newDomainCLI(t)
		writeCorruptCLIConfigFile(t, f.deps.Root)
		before := treeState(t, f.deps.Root)
		args := []string{sub}
		if sub != "list" {
			args = append(args, "example.com")
		}
		out, diagnostic, code := f.run(args...)
		want := "opsctl: " + filepath.Join(f.deps.Root, "etc/ikigenba/config.json") + ": " + config.ErrCorrupt.Error() + "\n"
		assertDomainResult(t, out, diagnostic, code, "", want, 1)
		f.unchanged(before)
		if len(f.calls) != 0 {
			t.Fatal(f.calls)
		}
	}
}

func TestDomainListNeedsNoHostConfig(t *testing.T) {
	// R-APTF-P2Z3 R-L3YW-91Q4
	for _, value := range []string{"", "www.example.com:example.com,example.com"} {
		f := newDomainCLI(t)
		want := ""
		if value != "" {
			f.config(map[string]string{domain.Key: value})
			want = "example.com\nwww.example.com example.com\n"
		}
		before := treeState(t, f.deps.Root)
		out, diagnostic, code := f.run("list")
		assertDomainResult(t, out, diagnostic, code, want, "", 0)
		f.unchanged(before)
		if len(f.calls) != 0 {
			t.Fatal(f.calls)
		}
	}
}

func TestDomainRefusalsAreOrderedAndInert(t *testing.T) {
	// R-L56S-MTGT R-LBAA-JO6A R-CS9P-PFDI
	for _, tc := range []struct {
		args               []string
		list, apex, reason string
	}{
		{[]string{"add", "blog.sbx.ikigenba.dev", "--redirect", "absent.com"}, "blog.sbx.ikigenba.dev", "sites", "name belongs to the host: blog.sbx.ikigenba.dev"},
		{[]string{"add", "sbx.ikigenba.dev"}, "sbx.ikigenba.dev", "", "name belongs to the host: sbx.ikigenba.dev"},
		{[]string{"add", "ikigenba.dev"}, "ikigenba.dev", "sites", "name is the apex: ikigenba.dev"},
		{[]string{"add", "www.example.com", "--redirect", "absent.com"}, "example.com,www.example.com:example.com", "", "domain exists: www.example.com"},
		{[]string{"add", "example.com"}, "example.com", "", "domain exists: example.com"},
		{[]string{"add", "example.com", "--redirect", "absent.com"}, "example.com", "", "domain exists: example.com"},
		{[]string{"add", "new.example.com", "--redirect", "absent.com"}, "example.com", "", "no domain 'absent.com'"},
		{[]string{"add", "new.example.com", "--redirect", "www.example.com"}, "example.com,www.example.com:example.com", "", "not a primary name: www.example.com"},
		{[]string{"remove", "sbx.ikigenba.dev"}, "sbx.ikigenba.dev,www.example.com:sbx.ikigenba.dev", "", "name belongs to the host: sbx.ikigenba.dev"},
		{[]string{"remove", "blog.sbx.ikigenba.dev"}, "", "", "name belongs to the host: blog.sbx.ikigenba.dev"},
		{[]string{"remove", "example.com"}, "example.com,www.example.com:example.com", "", "domain has redirects: example.com"},
		{[]string{"remove", "absent.com"}, "example.com", "", "no domain 'absent.com'"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f := newDomainCLI(t)
			f.setup()
			f.config(map[string]string{"host.apex": tc.apex})
			if tc.list != "" {
				f.config(map[string]string{domain.Key: tc.list})
			}
			if strings.Contains(tc.reason, "belongs") {
				f.lineage(tc.args[1])
			}
			before := treeState(t, f.deps.Root)
			out, diagnostic, code := f.run(tc.args...)
			assertDomainResult(t, out, diagnostic, code, "", "opsctl: "+tc.reason+"\n", 4)
			f.unchanged(before)
			if len(f.calls) != 0 {
				t.Fatal(f.calls)
			}
		})
	}
}

func TestDomainAddRemoveComposePublishedOperations(t *testing.T) {
	// R-9XRQ-WCX4 R-L6EP-0L7I R-LCI6-XFWZ R-AR1C-2UPS R-63B7-WLBZ
	for _, tc := range []struct {
		args []string
		apex string
	}{
		{[]string{"add", "xsbx.ikigenba.dev"}, "sites"},
		{[]string{"add", "ikigenba.dev"}, ""},
		{[]string{"add", "www.example.com", "--redirect", "example.com"}, "sites"},
		{[]string{"add", "--redirect", "example.com", "www.example.com"}, ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f := newDomainCLI(t)
			f.setup()
			f.config(map[string]string{domain.Key: "example.com"})
			if tc.apex != "" {
				f.config(map[string]string{"host.apex": tc.apex})
			}
			out, diagnostic, code := f.run(tc.args...)
			assertDomainResult(t, out, diagnostic, code, "", "", 0)
			name := tc.args[1]
			primary := ""
			if len(tc.args) > 2 {
				name = "www.example.com"
				primary = "example.com"
			}
			want := []domain.Domain{{Name: "example.com"}, {Name: name, Primary: primary}}
			if !reflect.DeepEqual(f.list(), want) {
				t.Fatal(f.list(), want)
			}
			f.assertPublished()
			f.assertCommands(name, true, false)
			out, diagnostic, code = f.run("list")
			line := name
			if primary != "" {
				line += " " + primary
			}
			assertDomainResult(t, out, diagnostic, code, "example.com\n"+line+"\n", "", 0)
		})
	}
	for _, lineage := range []bool{false, true} {
		for _, value := range []string{"example.com", "example.com,www.example.com:example.com"} {
			t.Run(fmt.Sprint(lineage, value), func(t *testing.T) {
				f := newDomainCLI(t)
				f.setup()
				f.config(map[string]string{domain.Key: value})
				if lineage {
					f.config(map[string]string{"host.apex": "sites"})
				}
				name := "example.com"
				if strings.Contains(value, ",") {
					name = "www.example.com"
				}
				if lineage {
					f.lineage(name)
				}
				if err := (config.Store{Root: f.deps.Root}).Del("acme.email"); err != nil {
					t.Fatal(err)
				}
				out, diagnostic, code := f.run("remove", name)
				assertDomainResult(t, out, diagnostic, code, "", "", 0)
				want := []domain.Domain{}
				if name != "example.com" {
					want = []domain.Domain{{Name: "example.com"}}
				}
				if !reflect.DeepEqual(f.list(), want) {
					t.Fatal(f.list(), want)
				}
				if len(want) == 0 {
					if _, err := (config.Store{Root: f.deps.Root}).Get(domain.Key); !errors.Is(err, config.ErrNotSet) {
						t.Fatal(err)
					}
				}
				f.assertPublished()
				f.assertCommands(name, false, lineage)
			})
		}
	}
}
func (f *domainCLI) assertPublished() {
	f.t.Helper()
	env := host.Env{Root: f.deps.Root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		if result, ok := servicesFixtureCommand(c); ok {
			return result, nil
		}
		return host.Result{}, nil
	}}
	want, err := nginx.Render(context.Background(), env, "sbx.ikigenba.dev", f.apex())
	if err != nil {
		f.t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(f.deps.Root, "etc/nginx/conf.d/ikigenba.conf"))
	if err != nil || string(got) != string(want) {
		f.t.Fatal("published nginx differs", err)
	}
	published, err := os.ReadFile(filepath.Join(f.deps.Root, "run/ikigenba/services.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := services.Write(context.Background(), env, "sbx.ikigenba.dev"); err != nil {
		f.t.Fatal(err)
	}
	wantServices, err := os.ReadFile(filepath.Join(f.deps.Root, "run/ikigenba/services.json"))
	if err != nil || string(published) != string(wantServices) {
		f.t.Fatal("published services differs", err)
	}
}
func (f *domainCLI) apex() string {
	value, _ := (config.Store{Root: f.deps.Root}).Get("host.apex")
	return value
}
func (f *domainCLI) assertCommands(name string, obtain, deleteCert bool) {
	f.t.Helper()
	var got []string
	for _, c := range f.calls {
		got = append(got, c.Name+" "+strings.Join(c.Args, " "))
	}
	want := []string{"nginx -t", "systemctl reload-or-restart nginx", "id --user ikigenba", "id --group --name ikigenba"}
	offset := 0
	if obtain {
		var expected host.Command
		env := host.Env{Root: f.deps.Root, Execute: func(_ context.Context, c host.Command) (host.Result, error) { expected = c; return host.Result{}, nil }}
		if err := cert.ObtainDomain(context.Background(), env, name, "operator@example.com"); err != nil {
			f.t.Fatal(err)
		}
		if len(f.calls) == 0 || !reflect.DeepEqual(f.calls[0], expected) {
			f.t.Fatal(got)
		}
		offset = 1
	}
	if len(got) != offset+len(want)+2+btoi(deleteCert) {
		f.t.Fatal(got)
	}
	if !reflect.DeepEqual(got[offset:offset+len(want)], want) {
		f.t.Fatal(got, want)
	}
	if !strings.HasPrefix(got[offset+len(want)], "chown root:ikigenba "+f.deps.Root+"/run/ikigenba "+f.deps.Root+"/run/ikigenba/.services-") {
		f.t.Fatal(got)
	}
	if got[offset+len(want)+1] != "setfacl -m u:nginx:x "+f.deps.Root+"/run/ikigenba" {
		f.t.Fatal(got)
	}
	if deleteCert {
		var expected host.Command
		env := host.Env{Root: f.deps.Root, Execute: func(_ context.Context, c host.Command) (host.Result, error) { expected = c; return host.Result{}, nil }}
		if err := cert.DeleteDomain(context.Background(), env, name); err != nil {
			f.t.Fatal(err)
		}
		if !reflect.DeepEqual(f.calls[len(f.calls)-1], expected) {
			f.t.Fatal(f.calls[len(f.calls)-1], expected)
		}
	}
}
func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestDomainObtainFailureLeavesPublishedState(t *testing.T) {
	// R-ADMF-VDK5
	f := newDomainCLI(t)
	f.setup()
	before := treeState(t, f.deps.Root)
	f.execute = func(c host.Command) (host.Result, error) {
		if c.Name != "certbot" || c.Args[0] != "certonly" {
			t.Fatal(c)
		}
		return host.Result{ExitCode: 1, Stderr: []byte("Some challenges have failed.")}, nil
	}
	out, diagnostic, code := f.run("add", "example.com")
	assertDomainResult(t, out, diagnostic, code, "", "opsctl: certbot certonly: exit status 1\n\n> Some challenges have failed.\n", 1)
	if len(f.calls) != 1 {
		t.Fatal(f.calls)
	}
	f.unchanged(before)
}

func TestDomainNginxFailureRestoresList(t *testing.T) {
	// R-L8UH-S4OW R-LDQ3-B7NO
	for _, tc := range []struct{ sub, prior string }{
		{"add", ""}, {"add", "example.com"}, {"remove", "example.com,www.example.com:example.com"},
	} {
		sub := tc.sub
		for _, failure := range []string{"prepare", "test", "reload"} {
			for _, previous := range []bool{false, true} {
				t.Run(fmt.Sprint(sub, tc.prior, failure, previous), func(t *testing.T) {
					f := newDomainCLI(t)
					f.setup()
					if tc.prior != "" {
						f.config(map[string]string{domain.Key: tc.prior})
					}
					name := "new.example.com"
					if sub == "remove" {
						name = "www.example.com"
					}
					prior := f.list()
					servicesBefore, err := os.ReadFile(filepath.Join(f.deps.Root, "run/ikigenba/services.json"))
					if err != nil {
						t.Fatal(err)
					}
					nginxPath := filepath.Join(f.deps.Root, "etc/nginx/conf.d/ikigenba.conf")
					if !previous {
						if err := os.Remove(nginxPath); err != nil {
							t.Fatal(err)
						}
					}
					if failure == "prepare" {
						if err := os.RemoveAll(filepath.Join(f.deps.Root, "etc/nginx")); err != nil {
							t.Fatal(err)
						}
					}
					f.lineage(name)
					lineageBefore := treeState(t, filepath.Join(f.deps.Root, "etc/letsencrypt"))
					var candidate []byte
					f.execute = func(c host.Command) (host.Result, error) {
						if c.Name == "certbot" {
							f.write("etc/letsencrypt/live/"+name+"/fullchain.pem", "issued by fixture")
							lineageBefore = treeState(t, filepath.Join(f.deps.Root, "etc/letsencrypt"))
						}
						if failure == "test" && c.Name == "nginx" {
							return host.Result{ExitCode: 1, Stderr: []byte("nginx: configuration file /etc/nginx/nginx.conf test failed")}, nil
						}
						if failure == "reload" && c.Name == "systemctl" {
							var renderErr error
							candidate, renderErr = nginx.Render(context.Background(), host.Env{Root: f.deps.Root}, "sbx.ikigenba.dev", "")
							if renderErr != nil {
								t.Fatal(renderErr)
							}
							return host.Result{ExitCode: 7}, nil
						}
						return host.Result{}, nil
					}
					out, diagnostic, code := f.run(sub, name)
					if code != 1 || out != "" {
						t.Fatal(code, out, diagnostic)
					}
					if failure == "test" && diagnostic != "opsctl: nginx -t: exit status 1\n\n> nginx: configuration file /etc/nginx/nginx.conf test failed\n" {
						t.Fatal(diagnostic)
					}
					if failure == "reload" && diagnostic != "opsctl: systemctl reload-or-restart nginx: exit status 7\n" {
						t.Fatal(diagnostic)
					}
					if failure == "prepare" && !strings.HasPrefix(diagnostic, "opsctl: publish nginx configuration: ") {
						t.Fatal(diagnostic)
					}
					if !reflect.DeepEqual(f.list(), prior) {
						t.Fatal(f.list(), prior)
					}
					if len(prior) == 0 {
						if _, err := (config.Store{Root: f.deps.Root}).Get(domain.Key); !errors.Is(err, config.ErrNotSet) {
							t.Fatal(err)
						}
					}
					content, readErr := os.ReadFile(filepath.Join(f.deps.Root, "etc/nginx/conf.d/ikigenba.conf"))
					switch {
					case failure == "reload":
						if readErr != nil || string(content) != string(candidate) || len(candidate) == 0 {
							t.Fatal(string(content), readErr)
						}
					case previous && failure != "prepare":
						if string(content) != "previous nginx" || readErr != nil {
							t.Fatal(string(content), readErr)
						}
						info, err := os.Stat(nginxPath)
						if err != nil || info.Mode().Perm() != 0600 {
							t.Fatal(info, err)
						}
					default:
						if !errors.Is(readErr, os.ErrNotExist) {
							t.Fatal(readErr)
						}
					}
					gotServices, err := os.ReadFile(filepath.Join(f.deps.Root, "run/ikigenba/services.json"))
					if err != nil || string(gotServices) != string(servicesBefore) {
						t.Fatal(string(gotServices), err)
					}
					if !reflect.DeepEqual(lineageBefore, treeState(t, filepath.Join(f.deps.Root, "etc/letsencrypt"))) {
						t.Fatal("certificate state changed")
					}
					wantCount := 0
					if sub == "add" {
						wantCount++
					}
					if failure != "prepare" {
						wantCount++
					}
					if failure == "reload" {
						wantCount++
					}
					if len(f.calls) != wantCount {
						t.Fatal(f.calls, wantCount)
					}
				})
			}
		}
	}
}

func TestDomainRestoreFailureIncludesBothDiagnostics(t *testing.T) {
	// R-LA2E-5WFL
	for _, sub := range []string{"add", "remove"} {
		for _, capture := range []string{"", "bad"} {
			t.Run(sub+capture, func(t *testing.T) {
				f := newDomainCLI(t)
				f.setup()
				if sub == "remove" {
					f.config(map[string]string{domain.Key: "example.com"})
				}
				f.execute = func(c host.Command) (host.Result, error) {
					if c.Name == "nginx" {
						writeCorruptCLIConfigFile(t, f.deps.Root)
						return host.Result{ExitCode: 1, Stderr: []byte(capture)}, nil
					}
					return host.Result{}, nil
				}
				out, diagnostic, code := f.run(sub, "example.com")
				want := "opsctl: nginx -t: exit status 1\n"
				if capture != "" {
					want += "\n> bad\n"
				}
				want += "\n" + filepath.Join(f.deps.Root, "etc/ikigenba/config.json") + ": " + config.ErrCorrupt.Error() + "\n"
				assertDomainResult(t, out, diagnostic, code, "", want, 1)
				wantCalls := 1
				if sub == "add" {
					wantCalls = 2
				}
				if len(f.calls) != wantCalls {
					t.Fatal(f.calls)
				}
			})
		}
	}
}

func TestDomainSaveFailureStopsBeforeApply(t *testing.T) {
	// R-54UF-WJ6A
	for _, sub := range []string{"add", "remove"} {
		t.Run(sub, func(t *testing.T) {
			f := newDomainCLI(t)
			f.setup()
			if sub == "remove" {
				f.config(map[string]string{domain.Key: "example.com"})
			}
			lock := filepath.Join(f.deps.Root, "etc/ikigenba/config.lock")
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(lock, 0700); err != nil {
				t.Fatal(err)
			}
			before := treeState(t, f.deps.Root)
			out, diagnostic, code := f.run(sub, "example.com")
			if out != "" || code != 1 || !strings.HasPrefix(diagnostic, "opsctl: open "+lock+": ") {
				t.Fatal(code, out, diagnostic)
			}
			wantCalls := 0
			if sub == "add" {
				wantCalls = 1
			}
			if len(f.calls) != wantCalls {
				t.Fatal(f.calls)
			}
			f.unchanged(before)
		})
	}
}

func TestDomainServicesFailureDoesNotUndoPublishedState(t *testing.T) {
	// R-AHA5-0OS8
	for _, sub := range []string{"add", "remove"} {
		t.Run(sub, func(t *testing.T) {
			f := newDomainCLI(t)
			f.setup()
			if sub == "remove" {
				f.config(map[string]string{domain.Key: "example.com"})
				f.lineage("example.com")
			}
			f.execute = func(c host.Command) (host.Result, error) {
				if c.Name == "id" {
					return host.Result{ExitCode: 8, Stderr: []byte("account unavailable")}, nil
				}
				return host.Result{}, nil
			}
			out, diagnostic, code := f.run(sub, "example.com")
			assertDomainResult(t, out, diagnostic, code, "", "opsctl: inspect ikigenba account: exit status 8\n\n> account unavailable\n", 1)
			want := []domain.Domain{}
			if sub == "add" {
				want = []domain.Domain{{Name: "example.com"}}
			}
			if !reflect.DeepEqual(f.list(), want) {
				t.Fatal(f.list(), want)
			}
			rendered, err := nginx.Render(context.Background(), host.Env{Root: f.deps.Root}, "sbx.ikigenba.dev", "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(f.deps.Root, "etc/nginx/conf.d/ikigenba.conf"))
			if err != nil || string(got) != string(rendered) {
				t.Fatal("nginx changed", err)
			}
			expected := []string{"nginx", "systemctl", "id"}
			if sub == "add" {
				expected = append([]string{"certbot"}, expected...)
			}
			var names []string
			for _, c := range f.calls {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, expected) {
				t.Fatal(names, expected)
			}
		})
	}
}

func TestDomainOrphanRemovalFollowsRootedLineageSymlink(t *testing.T) {
	// R-LEXZ-OZED R-AR1C-2UPS
	f := newDomainCLI(t)
	f.setup()
	f.lineage("stored")
	if err := os.Symlink("stored", filepath.Join(f.deps.Root, "etc/letsencrypt/live/example.com")); err != nil {
		t.Fatal(err)
	}
	before := treeState(t, f.deps.Root)
	out, diagnostic, code := f.run("remove", "example.com")
	assertDomainResult(t, out, diagnostic, code, "", "", 0)
	if len(f.calls) != 1 || f.calls[0].Name != "certbot" || !reflect.DeepEqual(f.calls[0].Args, []string{"delete", "--non-interactive", "--cert-name", "example.com", "--config-dir", filepath.Join(f.deps.Root, "etc/letsencrypt"), "--work-dir", filepath.Join(f.deps.Root, "var/lib/letsencrypt"), "--logs-dir", filepath.Join(f.deps.Root, "var/log/letsencrypt")}) {
		t.Fatal(f.calls)
	}
	f.unchanged(before)
}

func TestDomainDeleteFailureRetainsPublishedStateAndRetryHint(t *testing.T) {
	// R-LG5W-2R52
	for _, listed := range []bool{false, true} {
		for _, capture := range []string{"", "An unexpected error occurred:"} {
			t.Run(fmt.Sprint(listed, capture), func(t *testing.T) {
				f := newDomainCLI(t)
				f.setup()
				f.lineage("example.com")
				if listed {
					f.config(map[string]string{domain.Key: "example.com"})
				}
				before := treeState(t, f.deps.Root)
				f.execute = func(c host.Command) (host.Result, error) {
					if c.Name == "certbot" {
						return host.Result{ExitCode: 1, Stderr: []byte(capture)}, nil
					}
					if result, ok := servicesFixtureCommand(c); ok {
						return result, nil
					}
					return host.Result{}, nil
				}
				out, diagnostic, code := f.run("remove", "example.com")
				want := "opsctl: certbot delete: exit status 1\n"
				if capture != "" {
					want += "\n> " + capture + "\n"
				}
				want += "\nthe certificate for example.com remains; run 'opsctl domain remove example.com' again to delete it\n"
				assertDomainResult(t, out, diagnostic, code, "", want, 1)
				if len(f.list()) != 0 {
					t.Fatal(f.list())
				}
				if listed {
					f.assertPublished()
					f.assertCommands("example.com", false, true)
				} else {
					f.unchanged(before)
					if len(f.calls) != 1 {
						t.Fatal(f.calls)
					}
				}
			})
		}
	}
}
