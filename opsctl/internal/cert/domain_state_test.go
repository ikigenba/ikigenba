package cert_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// The fixture implements the external webroot protocol: certbot writes a token,
// its CA reads that token over HTTP, and only a successful challenge publishes
// a lineage. It owns both the due decision and the persisted renewal options.
// No real certificate tools, DNS records or services are used.
type webrootCertbotFixture struct {
	t          *testing.T
	root       string
	server     *httptest.Server
	due        bool
	refuse     bool
	requests   int
	challenges int
	calls      int
}

func domainCertificatePair(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	// Fixed test-only key material and signing input keep the fixture fully
	// deterministic without changing the existing host-certificate tests.
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Unix(1_700_000_000, 0),
		NotAfter:     time.Unix(1_800_000_000, 0),
		DNSNames:     []string{name},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(bytes.NewReader(make([]byte, 4096)), template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func newWebrootCertbotFixture(t *testing.T) *webrootCertbotFixture {
	t.Helper()
	f := &webrootCertbotFixture{t: t, root: t.TempDir()}
	f.write("bin/systemctl", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOOK_LOG\"\n", 0o750)
	f.write("etc/letsencrypt/live/host.example/fullchain.pem", "host certificate", 0o600)
	f.write("etc/letsencrypt/renewal/host.example.conf", "host DNS authenticator", 0o600)
	f.write("etc/letsencrypt/live/other.example/fullchain.pem", "unrelated certificate", 0o600)
	f.write("etc/letsencrypt/renewal/other.example.conf", "unrelated renewal", 0o600)
	f.write("etc/systemd/system/renew.timer", "unchanged timer", 0o600)
	f.write("etc/nginx/nginx.conf", "unchanged servers", 0o600)
	f.write("dns/records", "unchanged TXT records", 0o600)
	if err := os.MkdirAll(filepath.Join(f.root, "var/www/acme"), 0o750); err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(http.FileServer(http.Dir(filepath.Join(f.root, "var/www/acme"))))
	t.Cleanup(f.server.Close)
	return f
}

func (f *webrootCertbotFixture) write(path, value string, mode os.FileMode) {
	f.t.Helper()
	fullPath := filepath.Join(f.root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(value), mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *webrootCertbotFixture) env() host.Env {
	return host.Env{Root: f.root, Execute: f.execute}
}

func (f *webrootCertbotFixture) read(path string) ([]byte, error) {
	f.t.Helper()
	root, err := os.OpenRoot(f.root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := root.Close(); err != nil {
			f.t.Error(err)
		}
	}()
	return root.ReadFile(path)
}

func (f *webrootCertbotFixture) execute(ctx context.Context, c host.Command) (host.Result, error) {
	f.t.Helper()
	f.calls++
	if c.Name != "certbot" || len(c.Args) == 0 || c.Dir != "" || c.Env != nil || c.Stdin != nil {
		f.t.Fatalf("unexpected host execution: %#v", c)
	}
	name := argumentValue(f.t, c.Args, "--cert-name")
	configDir := argumentValue(f.t, c.Args, "--config-dir")
	if configDir != filepath.Join(f.root, "etc/letsencrypt") || strings.ContainsAny(name, "/\\") {
		f.t.Fatalf("unrooted or invalid lineage: %#v", c)
	}
	switch c.Args[0] {
	case "delete":
		if err := os.RemoveAll(filepath.Join(configDir, "live", name)); err != nil {
			return host.Result{}, err
		}
		err := os.Remove(filepath.Join(configDir, "renewal", name+".conf"))
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return host.Result{}, err
	case "certonly":
		webroot := argumentValue(f.t, c.Args, "--webroot-path")
		hook := argumentValue(f.t, c.Args, "--deploy-hook")
		if !reflect.DeepEqual(argumentValues(c.Args, "-d"), []string{name}) || !containsArgument(c.Args, "--webroot") || !containsArgument(c.Args, "--keep-until-expiring") || containsArgument(c.Args, "--force-renewal") || containsArgument(c.Args, "--manual") {
			f.t.Fatalf("not an unforced single-name HTTP-01 request: %#v", c)
		}
		if webroot != filepath.Join(f.root, "var/www/acme") || hook != wantDeployHook {
			f.t.Fatalf("incorrect webroot or hook: %#v", c)
		}
		if _, err := os.Stat(filepath.Join(configDir, "live", name, "fullchain.pem")); err == nil && !f.due {
			return host.Result{}, nil
		}
		return f.issue(ctx, name, webroot, hook)
	default:
		f.t.Fatalf("unexpected certbot operation: %#v", c)
		return host.Result{}, nil
	}
}

func containsArgument(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func (f *webrootCertbotFixture) issue(ctx context.Context, name, webroot, hook string) (host.Result, error) {
	f.t.Helper()
	f.requests++
	challengePath := filepath.Join(webroot, ".well-known/acme-challenge/fixture-token")
	if err := os.MkdirAll(filepath.Dir(challengePath), 0o750); err != nil {
		return host.Result{}, err
	}
	if err := os.WriteFile(challengePath, []byte("fixture-proof-for-"+name), 0o600); err != nil {
		return host.Result{}, err
	}
	defer func() {
		if err := os.Remove(challengePath); err != nil {
			f.t.Error(err)
		}
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.server.URL+"/.well-known/acme-challenge/fixture-token", nil)
	if err != nil {
		return host.Result{}, err
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		return host.Result{}, err
	}
	proof, readErr := io.ReadAll(io.LimitReader(response.Body, 1024))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || string(proof) != "fixture-proof-for-"+name {
		return host.Result{ExitCode: 1, Stderr: []byte("HTTP-01 challenge failed")}, nil
	}
	f.challenges++
	if f.refuse {
		return host.Result{ExitCode: 1, Stderr: []byte("CA refused issuance")}, nil
	}
	certificate, key := domainCertificatePair(f.t, name)
	f.write("etc/letsencrypt/live/"+name+"/fullchain.pem", string(certificate), 0o600)
	f.write("etc/letsencrypt/live/"+name+"/privkey.pem", string(key), 0o600)
	config := fmt.Sprintf("[renewalparams]\nauthenticator = webroot\nwebroot_path = %s\ndeploy_hook = %s\n[[webroot_map]]\n%s = %s\n", webroot, hook, name, webroot)
	f.write("etc/letsencrypt/renewal/"+name+".conf", config, 0o600)
	f.due = false
	return host.Exec(ctx, host.Command{Name: "/bin/sh", Args: []string{"-c", hook}, Env: []string{"PATH=" + filepath.Join(f.root, "bin"), "HOOK_LOG=" + filepath.Join(f.root, "hook.log")}})
}

func (f *webrootCertbotFixture) renew(ctx context.Context, name string) error {
	f.t.Helper()
	content, err := f.read(filepath.Join("etc/letsencrypt/renewal", name+".conf"))
	if err != nil {
		return err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		key, value, found := strings.Cut(line, " = ")
		if found {
			values[key] = value
		}
	}
	if values["authenticator"] != "webroot" || values[name] != values["webroot_path"] {
		return errors.New("invalid saved webroot authenticator")
	}
	result, err := f.issue(ctx, name, values[name], values["deploy_hook"])
	if err != nil || result.ExitCode != 0 {
		return fmt.Errorf("fixture renewal failed: result %v, error %w", result, err)
	}
	return nil
}

func TestDomainCertificateHTTP01IssuanceAndSavedRenewal(t *testing.T) {
	// R-SAB8-5DKY
	for _, due := range []bool{false, true} {
		t.Run(fmt.Sprint(due), func(t *testing.T) {
			f := newWebrootCertbotFixture(t)
			if due {
				f.write("etc/letsencrypt/live/custom.example/fullchain.pem", "due certificate", 0o600)
				f.due = true
			}
			if err := cert.ObtainDomain(context.Background(), f.env(), "custom.example", "admin@example.com"); err != nil {
				t.Fatal(err)
			}
			info, err := cert.Inspect(f.env(), "custom.example")
			if err != nil || !reflect.DeepEqual(info.Names, []string{"custom.example"}) {
				t.Fatal(info, err)
			}
			key, err := os.ReadFile(filepath.Join(f.root, "etc/letsencrypt/live/custom.example/privkey.pem"))
			if err != nil || !bytes.Contains(key, []byte("PRIVATE KEY")) {
				t.Fatal("private key missing", err)
			}
			certificate, err := f.read("etc/letsencrypt/live/custom.example/fullchain.pem")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tls.X509KeyPair(certificate, key); err != nil {
				t.Fatal("certificate and private key do not match", err)
			}
			if f.requests != 1 || f.challenges != 1 || f.calls != 1 {
				t.Fatal("issuance did not use HTTP-01", f.requests, f.challenges, f.calls)
			}
			if err := f.renew(context.Background(), "custom.example"); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(filepath.Join(f.root, "hook.log"))
			if err != nil || string(calls) != "try-reload-or-restart nginx\ntry-reload-or-restart nginx\n" || f.challenges != 2 {
				t.Fatal("renewal did not repeat challenge and reload", string(calls), f.challenges, err)
			}
		})
	}
}

func TestDomainCertificateKeepsNotDueLineage(t *testing.T) {
	// R-HBEO-D5QL
	f := newWebrootCertbotFixture(t)
	certificate, key := domainCertificatePair(t, "custom.example")
	f.write("etc/letsencrypt/live/custom.example/fullchain.pem", string(certificate), 0o600)
	f.write("etc/letsencrypt/live/custom.example/privkey.pem", string(key), 0o600)
	f.write("etc/letsencrypt/renewal/custom.example.conf", "saved renewal", 0o600)
	before := snapshotTree(t, f.root)
	if err := cert.ObtainDomain(context.Background(), f.env(), "custom.example", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	if f.requests != 0 || f.challenges != 0 || f.calls != 1 || !reflect.DeepEqual(snapshotTree(t, f.root), before) {
		t.Fatal("not-due lineage was changed or issuance requested")
	}
}

func TestDomainCertificateCARefusalPreservesEarlierLineage(t *testing.T) {
	// R-HCMK-QXHA
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			f := newWebrootCertbotFixture(t)
			if existing {
				f.write("etc/letsencrypt/live/custom.example/fullchain.pem", "earlier fullchain", 0o600)
				f.write("etc/letsencrypt/live/custom.example/privkey.pem", "earlier key", 0o600)
				f.write("etc/letsencrypt/renewal/custom.example.conf", "earlier renewal", 0o600)
			}
			before := snapshotTree(t, filepath.Join(f.root, "etc/letsencrypt"))
			f.due, f.refuse = true, true
			err := cert.ObtainDomain(context.Background(), f.env(), "custom.example", "admin@example.com")
			var commandError *host.CommandError
			if !errors.As(err, &commandError) || commandError.Result.ExitCode != 1 || f.requests != 1 || f.challenges != 1 {
				t.Fatal("CA refusal was not reached or retained", err)
			}
			if !reflect.DeepEqual(snapshotTree(t, filepath.Join(f.root, "etc/letsencrypt")), before) {
				t.Fatal("CA refusal published or changed lineage")
			}
		})
	}
}

func TestDomainCertificateDeleteAndIsolation(t *testing.T) {
	// R-25DS-GPIV R-S93B-RLU9
	f := newWebrootCertbotFixture(t)
	protected := []string{"etc/letsencrypt/live/host.example", "etc/letsencrypt/live/other.example", "etc/systemd", "etc/nginx", "dns"}
	before := make(map[string]map[string]treeEntry)
	for _, path := range protected {
		before[path] = snapshotTree(t, filepath.Join(f.root, path))
	}
	renewalsBefore := make(map[string][]byte)
	for _, name := range []string{"host.example", "other.example"} {
		content, err := f.read(filepath.Join("etc/letsencrypt/renewal", name+".conf"))
		if err != nil {
			t.Fatal(err)
		}
		renewalsBefore[name] = content
	}
	for _, operation := range []func() error{
		func() error {
			return cert.ObtainDomain(context.Background(), f.env(), "custom.example", "admin@example.com")
		},
		func() error { return cert.DeleteDomain(context.Background(), f.env(), "custom.example") },
	} {
		if err := operation(); err != nil {
			t.Fatal(err)
		}
		for _, path := range protected {
			if !reflect.DeepEqual(snapshotTree(t, filepath.Join(f.root, path)), before[path]) {
				t.Fatalf("domain operation changed %s", path)
			}
		}
		for name, want := range renewalsBefore {
			got, err := f.read(filepath.Join("etc/letsencrypt/renewal", name+".conf"))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("domain operation changed other renewal", name, err)
			}
		}
	}
	for _, path := range []string{"etc/letsencrypt/live/custom.example", "etc/letsencrypt/renewal/custom.example.conf"} {
		if _, err := os.Lstat(filepath.Join(f.root, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("delete retained lineage or renewal", path, err)
		}
	}
	if err := f.renew(context.Background(), "custom.example"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted lineage could still renew", err)
	}
	calls, err := os.ReadFile(filepath.Join(f.root, "hook.log"))
	if err != nil || string(calls) != "try-reload-or-restart nginx\n" || f.calls != 2 {
		t.Fatal("unexpected service or process action", string(calls), f.calls, err)
	}
}
