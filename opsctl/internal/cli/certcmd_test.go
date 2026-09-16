package cli_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantCertUsage = `Usage: opsctl cert <subcommand>

Obtain and inspect the one certificate this host serves: host.name and
*.host.name, proved over DNS-01 through 'opsctl dns acme-auth'.

Subcommands:
  show    print the certificate's names, issuer, and expiry
  obtain  obtain the certificate, or renew it if it is due

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at

Renewal is certbot's: 'certbot renew' re-runs the same hooks and reloads
nginx, with no further configuration. 'opsctl init' writes the timer that
runs it twice a day, ikigenba-renew-certificate.timer.
`

func TestCertHelpIsExactAndInert(t *testing.T) {
	// R-YHGD-GEQD
	for _, uid := range []int{0, 1000} {
		for _, option := range []string{"-h", "--help"} {
			called := false
			deps := cli.Deps{
				Root: filepath.Join(t.TempDir(), "absent"), EUID: uid,
				Execute: func(context.Context, host.Command) (host.Result, error) {
					called = true
					return host.Result{}, errors.New("unexpected execution")
				},
			}
			stdout, stderr, code := invoke([]string{"cert", option}, deps)
			if code != 0 || stdout != wantCertUsage || stderr != "" || called {
				t.Fatalf("uid %d %s: exit %d stdout %q stderr %q executed %t", uid, option, code, stdout, stderr, called)
			}
		}
	}
}

func TestCertGrammarAndRootCheckPrecedeHostAccess(t *testing.T) {
	// R-YIO9-U6H2
	root := t.TempDir()
	before := treeState(t, root)
	called := 0
	deps := cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
		called++
		return host.Result{}, nil
	}}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"cert"}, "opsctl: no cert subcommand given\n\nsee 'opsctl cert --help' for usage\n"},
		{[]string{"cert", "renew"}, "opsctl: unknown cert subcommand 'renew'\n\nsee 'opsctl cert --help' for usage\n"},
		{[]string{"cert", "show", "extra"}, "opsctl: cert show takes no arguments\n\nsee 'opsctl cert --help' for usage\n"},
		{[]string{"cert", "obtain", "--force"}, "opsctl: cert obtain takes no arguments\n\nsee 'opsctl cert --help' for usage\n"},
	}
	for _, tc := range cases {
		stdout, stderr, code := invoke(tc.args, deps)
		if code != 2 || stdout != "" || stderr != tc.want {
			t.Errorf("%v: exit %d stdout %q stderr %q, want %q", tc.args, code, stdout, stderr, tc.want)
		}
	}

	stdout, stderr, code := invoke([]string{"cert", "show"}, cli.Deps{Root: root, EUID: 1000, Execute: deps.Execute})
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("non-root: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if called != 0 || !reflect.DeepEqual(treeState(t, root), before) {
		t.Fatal("invalid or non-root invocation accessed host state")
	}
}

func TestCertReadsRequiredConfigurationInOrder(t *testing.T) {
	// R-YJW6-7Y7R
	cases := []struct {
		name   string
		args   []string
		values map[string]string
		want   string
	}{
		{"show missing host", []string{"cert", "show"}, nil, "opsctl: host.name not set\n"},
		{"obtain checks host first", []string{"cert", "obtain"}, map[string]string{"host.name": "", "acme.email": "admin@example.com"}, "opsctl: host.name not set\n"},
		{"obtain missing email", []string{"cert", "obtain"}, map[string]string{"host.name": "example.com"}, "opsctl: acme.email not set\n"},
		{"obtain empty email", []string{"cert", "obtain"}, map[string]string{"host.name": "example.com", "acme.email": ""}, "opsctl: acme.email not set\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := config.Store{Root: root}
			for key, value := range tc.values {
				if err := store.Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
			before := treeState(t, root)
			called := false
			stdout, stderr, code := invoke(tc.args, cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
				called = true
				return host.Result{}, nil
			}})
			if code != 1 || stdout != "" || stderr != tc.want || called {
				t.Fatalf("exit %d stdout %q stderr %q executed %t", code, stdout, stderr, called)
			}
			if after := treeState(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("configuration failure changed state: before %#v after %#v", before, after)
			}
		})
	}

	root := t.TempDir()
	writeCLIConfigFile(t, root, "not json\n")
	stdout, stderr, code := invoke([]string{"cert", "show"}, cli.Deps{Root: root, EUID: 0})
	wantPath := filepath.Join(root, "etc", "ikigenba", "config.json")
	if code != 1 || stdout != "" || stderr != "opsctl: "+wantPath+" is corrupt\n" {
		t.Fatalf("corrupt store: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestCertShowPrintsInspectedCertificate(t *testing.T) {
	// R-YTND-A45B
	root := t.TempDir()
	setCertConfig(t, root, map[string]string{"host.name": "example.com"})
	expires := time.Date(2035, time.June, 7, 8, 9, 10, 987654321, time.FixedZone("offset", -6*60*60))
	writeCertificate(t, root, "example.com", []string{"example.com", "*.example.com"}, "Test Issuer", expires)

	stdout, stderr, code := invoke([]string{"cert", "show"}, cli.Deps{Root: root, EUID: 0})
	want := "names: example.com, *.example.com\nissuer: Test Issuer\nexpires: 2035-06-07T14:09:10Z\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q, want stdout %q", code, stdout, stderr, want)
	}
}

func TestCertShowFailureIsDiagnosticAndReadOnly(t *testing.T) {
	// R-YUV9-NVW0
	root := t.TempDir()
	setCertConfig(t, root, map[string]string{"host.name": "ikigenba.dev"})
	before := treeState(t, root)
	stdout, stderr, code := invoke([]string{"cert", "show"}, cli.Deps{Root: root, EUID: 0})
	if code != 1 || stdout != "" || stderr != "opsctl: no certificate for ikigenba.dev\n" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if after := treeState(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("show failure changed state: before %#v after %#v", before, after)
	}
}

func TestCertObtainUsesConfigurationAndFormatsFailure(t *testing.T) {
	// R-YXB2-FFDE
	root := t.TempDir()
	setCertConfig(t, root, map[string]string{"host.name": "example.com", "acme.email": "admin@example.com"})
	var commands []host.Command
	deps := cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		return host.Result{}, nil
	}}
	stdout, stderr, code := invoke([]string{"cert", "obtain"}, deps)
	if code != 0 || stdout != "" || stderr != "" || len(commands) != 1 {
		t.Fatalf("success: exit %d stdout %q stderr %q commands %d", code, stdout, stderr, len(commands))
	}
	wantParts := [][2]string{
		{"--email", "admin@example.com"},
		{"--cert-name", "example.com"},
		{"--config-dir", filepath.Join(root, "etc", "letsencrypt")},
	}
	joined := strings.Join(commands[0].Args, "\x00")
	for _, pair := range wantParts {
		if !strings.Contains(joined, pair[0]+"\x00"+pair[1]) {
			t.Errorf("command args %q do not contain configured pair %q, %q", commands[0].Args, pair[0], pair[1])
		}
	}

	deps.Execute = func(context.Context, host.Command) (host.Result, error) {
		return host.Result{ExitCode: 1, Stdout: []byte("challenge failed\n"), Stderr: []byte("CA refused")}, nil
	}
	stdout, stderr, code = invoke([]string{"cert", "obtain"}, deps)
	want := "opsctl: certbot certonly: exit status 1\n\n> challenge failed\n> CA refused\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("failure: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, want)
	}
}

func setCertConfig(t *testing.T, root string, values map[string]string) {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range values {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
}

func writeCLIConfigFile(t *testing.T, root, contents string) {
	t.Helper()
	dir := filepath.Join(root, "etc", "ikigenba")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCertificate(t *testing.T, root, hostName string, names []string, issuer string, expires time.Time) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: issuer},
		DNSNames:     names,
		NotBefore:    expires.Add(-24 * time.Hour),
		NotAfter:     expires,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "etc", "letsencrypt", "live", hostName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
