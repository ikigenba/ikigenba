package cert_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const wantDeployHook = "if systemctl is-active --quiet nginx; then systemctl reload nginx; fi"

// R-YBCV-JK0W R-YCKR-XBRL
func TestObtainPackageBoundaryAndSignature(t *testing.T) {
	acceptObtainSignature(cert.Obtain)

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	dirFS := os.DirFS(dir)
	var internalImports []string
	foundPackage := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		sourceFile, err := dirFS.Open(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), sourceFile, parser.ImportsOnly)
		closeErr := sourceFile.Close()
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if source.Name.Name != "cert" {
			t.Fatalf("%s package = %s, want cert", entry.Name(), source.Name.Name)
		}
		foundPackage = true
		for _, spec := range source.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(path, "/internal/") {
				internalImports = append(internalImports, path)
			}
		}
	}
	slices.Sort(internalImports)
	want := []string{"github.com/ikigenba/ikigenba/opsctl/internal/host"}
	if !reflect.DeepEqual(internalImports, want) {
		t.Fatalf("internal imports = %v, want %v", internalImports, want)
	}
	if !foundPackage {
		t.Fatal("certificate operations are not owned by package cert")
	}
}

func acceptObtainSignature(func(context.Context, host.Env, string, string) error) {}

// R-FRFQ-VNTM
func TestObtainRejectsMissingConfigurationBeforeExecution(t *testing.T) {
	for _, tc := range []struct {
		name, hostName, email, want string
	}{
		{"host", "", "admin@example.com", "host.name not set"},
		{"email", "example.com", "", "acme.email not set"},
		{"host first", "", "", "host.name not set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			env := host.Env{Root: t.TempDir(), Execute: func(context.Context, host.Command) (host.Result, error) {
				calls++
				return host.Result{}, nil
			}}
			err := cert.Obtain(context.Background(), env, tc.hostName, tc.email)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if calls != 0 {
				t.Fatalf("Execute calls = %d, want 0", calls)
			}
		})
	}
}

// R-GFIN-FMDJ
func TestObtainExecutesExactCertbotCommand(t *testing.T) {
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	var gotCtx context.Context
	var got host.Command
	env := host.Env{Root: root, Execute: func(callCtx context.Context, command host.Command) (host.Result, error) {
		gotCtx, got = callCtx, command
		return host.Result{}, nil
	}}
	if err := cert.Obtain(ctx, env, "example.com", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	want := host.Command{Name: "certbot", Args: []string{
		"certonly", "--non-interactive", "--agree-tos", "--email", "admin@example.com",
		"--manual", "--preferred-challenges", "dns",
		"--manual-auth-hook", "opsctl dns acme-auth",
		"--manual-cleanup-hook", "opsctl dns acme-cleanup",
		"--deploy-hook", wantDeployHook,
		"--cert-name", "example.com", "-d", "example.com", "-d", "*.example.com",
		"--keep-until-expiring",
		"--config-dir", filepath.Join(root, "etc/letsencrypt"),
		"--work-dir", filepath.Join(root, "var/lib/letsencrypt"),
		"--logs-dir", filepath.Join(root, "var/log/letsencrypt"),
	}}
	if gotCtx != ctx {
		t.Fatal("Obtain did not pass the supplied context")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

// R-YMBY-ZHP5
func TestObtainEstablishesLineageAndRetainsHooks(t *testing.T) {
	fixture := newCertbotFixture(t, issueCertificate, nil)
	if err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	lineage := filepath.Join(fixture.root, "etc/letsencrypt/live/example.com")
	lineageRoot, err := os.OpenRoot(lineage)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lineageRoot.Close(); err != nil {
			t.Errorf("close lineage: %v", err)
		}
	})
	certificatePEM, err := lineageRoot.ReadFile("fullchain.pem")
	if err != nil {
		t.Fatal(err)
	}
	certificate := parseCertificate(t, certificatePEM)
	if want := []string{"example.com", "*.example.com"}; !reflect.DeepEqual(certificate.DNSNames, want) {
		t.Fatalf("certificate names = %v, want %v", certificate.DNSNames, want)
	}
	keyPEM, err := lineageRoot.ReadFile("privkey.pem")
	if err != nil {
		t.Fatal(err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		t.Fatalf("private key is not an RSA PEM block")
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok || publicKey.N.Cmp(privateKey.N) != 0 || publicKey.E != privateKey.E {
		t.Fatal("certificate and private key do not match")
	}
	if challenges := fixture.challengeValues(); len(challenges) != 0 {
		t.Fatalf("challenge values left behind: %v", challenges)
	}
	fixture.resetHookLogs()
	if err := fixture.renew(context.Background()); err != nil {
		t.Fatalf("renew with retained hooks: %v", err)
	}
	if challenges := fixture.challengeValues(); len(challenges) != 0 {
		t.Fatalf("renewal challenge values left behind: %v", challenges)
	}
	wantDNSCalls := []string{
		"dns acme-auth example.com renewal-apex-token",
		"dns acme-auth *.example.com renewal-wildcard-token",
		"dns acme-cleanup example.com renewal-apex-token",
		"dns acme-cleanup *.example.com renewal-wildcard-token",
	}
	if calls := fixture.dnsCalls(); !reflect.DeepEqual(calls, wantDNSCalls) {
		t.Fatalf("renewal DNS hook calls = %v, want %v", calls, wantDNSCalls)
	}
	if calls := fixture.systemctlCalls(); !reflect.DeepEqual(calls, []string{"is-active --quiet nginx", "reload nginx"}) {
		t.Fatalf("renewal systemctl calls = %v", calls)
	}
}

// R-YNJV-D9FU
func TestObtainLetsCertbotKeepNotDueCertificate(t *testing.T) {
	original := []byte("existing certificate")
	fixture := newCertbotFixture(t, keepCertificate, original)
	if err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(fixture.fullchain())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, original) || len(fixture.challengeValues()) != 0 || len(fixture.systemctlCalls()) != 0 {
		t.Fatalf("not-due result: certificate %q, challenges %v, systemctl %v", got, fixture.challengeValues(), fixture.systemctlCalls())
	}
	if fixture.calls != 1 {
		t.Fatalf("certbot calls = %d, want 1", fixture.calls)
	}
}

// R-YORR-R16J
func TestObtainPreservesCertbotFailure(t *testing.T) {
	cause := errors.New("cannot start")
	wantResult := host.Result{Stdout: []byte("out"), Stderr: []byte("detail"), ExitCode: 17}
	for _, tc := range []struct {
		name   string
		result host.Result
		err    error
	}{
		{"exit", wantResult, nil},
		{"execution", wantResult, cause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := host.Env{Execute: func(context.Context, host.Command) (host.Result, error) {
				return tc.result, tc.err
			}}
			var err error
			stdout, stderr := captureOutput(t, func() {
				err = cert.Obtain(context.Background(), env, "example.com", "admin@example.com")
			})
			if len(stdout) != 0 || len(stderr) != 0 {
				t.Fatalf("Obtain printed stdout %q, stderr %q", stdout, stderr)
			}
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) {
				t.Fatalf("error %T = %v, want *host.CommandError", err, err)
			}
			if commandErr.Label != "certbot certonly" || !reflect.DeepEqual(commandErr.Result, tc.result) || !errors.Is(commandErr.Err, tc.err) {
				t.Fatalf("CommandError = %#v", commandErr)
			}
		})
	}
	env := host.Env{Execute: func(context.Context, host.Command) (host.Result, error) { return host.Result{}, nil }}
	if err := cert.Obtain(context.Background(), env, "example.com", "admin@example.com"); err != nil {
		t.Fatalf("successful execution returned %v", err)
	}
}

// R-YPZO-4SX8
func TestObtainRefusalPreservesCertificateAndCleansChallenges(t *testing.T) {
	original := []byte("known good certificate")
	fixture := newCertbotFixture(t, refuseCertificate, original)
	err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com")
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error = %v, want certbot failure", err)
	}
	got, readErr := os.ReadFile(fixture.fullchain())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("certificate = %q, want unchanged %q", got, original)
	}
	if challenges := fixture.challengeValues(); len(challenges) != 0 {
		t.Fatalf("refusal left challenges %v", challenges)
	}
	if calls := fixture.systemctlCalls(); len(calls) != 0 {
		t.Fatalf("refusal invoked systemctl: %v", calls)
	}
}

func TestObtainDeployHookHonorsNginxStateAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		active      bool
		reloadFails bool
		wantCalls   []string
		wantError   bool
	}{
		{"active", true, false, []string{"is-active --quiet nginx", "reload nginx"}, false},
		{"inactive", false, false, []string{"is-active --quiet nginx"}, false},
		{"reload failure", true, true, []string{"is-active --quiet nginx", "reload nginx"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCertbotFixture(t, issueCertificate, nil)
			fixture.nginxActive = tc.active
			fixture.reloadFails = tc.reloadFails
			err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com")
			if (err != nil) != tc.wantError {
				t.Fatalf("Obtain error = %v, want error %v", err, tc.wantError)
			}
			if tc.wantError {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) || commandErr.Result.ExitCode == 0 {
					t.Fatalf("reload failure = %#v, want failed certbot CommandError", err)
				}
			}
			if calls := fixture.systemctlCalls(); !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("systemctl calls = %v, want %v", calls, tc.wantCalls)
			}
		})
	}
}

type contextKey struct{}

func argumentValue(t *testing.T, args []string, name string) string {
	t.Helper()
	for i := range len(args) - 1 {
		if args[i] == name {
			return args[i+1]
		}
	}
	t.Fatalf("argument %s not found in %v", name, args)
	return ""
}

type certbotFixture struct {
	t            *testing.T
	root         string
	scenario     certbotScenario
	nginxActive  bool
	reloadFails  bool
	calls        int
	binDir       string
	challengeDir string
	dnsLog       string
	systemctlLog string
}

type certbotScenario int

const (
	issueCertificate certbotScenario = iota
	keepCertificate
	refuseCertificate
)

func newCertbotFixture(t *testing.T, scenario certbotScenario, certificate []byte) *certbotFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &certbotFixture{
		t: t, root: root, scenario: scenario, nginxActive: true,
		binDir: filepath.Join(root, "bin"), challengeDir: filepath.Join(root, "challenges"),
		dnsLog:       filepath.Join(root, "dns.log"),
		systemctlLog: filepath.Join(root, "systemctl.log"),
	}
	if err := os.MkdirAll(fixture.binDir, 0o750); err != nil {
		t.Fatal(err)
	}
	fixture.writeExecutable("opsctl", `#!/bin/sh
set -eu
mkdir -p "$FIXTURE_CHALLENGES"
printf '%s %s %s %s\n' "$1" "$2" "$CERTBOT_DOMAIN" "$CERTBOT_VALIDATION" >> "$FIXTURE_DNS_LOG"
case "$1 $2" in
  "dns acme-auth") printf '%s' "$CERTBOT_DOMAIN" > "$FIXTURE_CHALLENGES/$CERTBOT_VALIDATION" ;;
  "dns acme-cleanup") rm -f "$FIXTURE_CHALLENGES/$CERTBOT_VALIDATION" ;;
  *) exit 64 ;;
esac
`)
	fixture.writeExecutable("systemctl", `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FIXTURE_SYSTEMCTL_LOG"
case "$1" in
  is-active) test "$FIXTURE_NGINX_ACTIVE" = 1 ;;
  reload) test "$FIXTURE_RELOAD_FAIL" = 0 ;;
  *) exit 64 ;;
esac
`)
	if certificate != nil {
		if err := os.MkdirAll(filepath.Dir(fixture.fullchain()), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture.fullchain(), certificate, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (f *certbotFixture) fullchain() string {
	return filepath.Join(f.root, "etc/letsencrypt/live/example.com/fullchain.pem")
}

func (f *certbotFixture) env() host.Env {
	return host.Env{Root: f.root, Execute: func(ctx context.Context, command host.Command) (host.Result, error) {
		f.calls++
		if command.Name != "certbot" || len(command.Args) == 0 || command.Args[0] != "certonly" {
			f.t.Fatalf("unexpected command: %#v", command)
		}
		if f.scenario == keepCertificate && fileExists(f.fullchain()) {
			return host.Result{}, nil
		}
		authHook := argumentValue(f.t, command.Args, "--manual-auth-hook")
		cleanupHook := argumentValue(f.t, command.Args, "--manual-cleanup-hook")
		for _, challenge := range []struct{ domain, token string }{{"example.com", "apex-token"}, {"*.example.com", "wildcard-token"}} {
			if result, err := f.runHook(ctx, authHook, challenge.domain, challenge.token); err != nil || result.ExitCode != 0 {
				return result, err
			}
		}
		for _, challenge := range []struct{ domain, token string }{{"example.com", "apex-token"}, {"*.example.com", "wildcard-token"}} {
			if result, err := f.runHook(ctx, cleanupHook, challenge.domain, challenge.token); err != nil || result.ExitCode != 0 {
				return result, err
			}
		}
		if f.scenario == refuseCertificate {
			return host.Result{Stderr: []byte("CA refused issuance"), ExitCode: 1}, nil
		}
		lineage := filepath.Dir(f.fullchain())
		if err := os.MkdirAll(lineage, 0o750); err != nil {
			f.t.Fatal(err)
		}
		certificatePEM, privateKeyPEM := makeCertificate(f.t)
		if err := os.WriteFile(f.fullchain(), certificatePEM, 0o600); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lineage, "privkey.pem"), privateKeyPEM, 0o600); err != nil {
			f.t.Fatal(err)
		}
		renewalPath := filepath.Join(f.root, "etc/letsencrypt/renewal/example.com.conf")
		if err := os.MkdirAll(filepath.Dir(renewalPath), 0o750); err != nil {
			f.t.Fatal(err)
		}
		renewal := strings.Join([]string{
			argumentValue(f.t, command.Args, "--manual-auth-hook"),
			argumentValue(f.t, command.Args, "--manual-cleanup-hook"),
			argumentValue(f.t, command.Args, "--deploy-hook"),
		}, "\n")
		if err := os.WriteFile(renewalPath, []byte(renewal), 0o600); err != nil {
			f.t.Fatal(err)
		}
		return f.runHook(ctx, argumentValue(f.t, command.Args, "--deploy-hook"), "", "")
	}}
}

func (f *certbotFixture) runHook(ctx context.Context, hook, domain, validation string) (host.Result, error) {
	active, reloadFails := "0", "0"
	if f.nginxActive {
		active = "1"
	}
	if f.reloadFails {
		reloadFails = "1"
	}
	return host.Exec(ctx, host.Command{Name: "sh", Args: []string{"-c", hook}, Env: []string{
		"PATH=" + f.binDir + ":" + os.Getenv("PATH"),
		"FIXTURE_CHALLENGES=" + f.challengeDir,
		"FIXTURE_DNS_LOG=" + f.dnsLog,
		"FIXTURE_SYSTEMCTL_LOG=" + f.systemctlLog,
		"FIXTURE_NGINX_ACTIVE=" + active,
		"FIXTURE_RELOAD_FAIL=" + reloadFails,
		"CERTBOT_DOMAIN=" + domain,
		"CERTBOT_VALIDATION=" + validation,
	}})
}

func (f *certbotFixture) renew(ctx context.Context) error {
	f.t.Helper()
	root, err := os.OpenRoot(f.root)
	if err != nil {
		return err
	}
	defer func() {
		if err := root.Close(); err != nil {
			f.t.Errorf("close fixture root: %v", err)
		}
	}()
	content, err := root.ReadFile("etc/letsencrypt/renewal/example.com.conf")
	if err != nil {
		return err
	}
	hooks := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(hooks) != 3 {
		return errors.New("invalid renewal hook configuration")
	}
	challenges := []struct{ domain, token string }{
		{"example.com", "renewal-apex-token"},
		{"*.example.com", "renewal-wildcard-token"},
	}
	for _, challenge := range challenges {
		if result, err := f.runHook(ctx, hooks[0], challenge.domain, challenge.token); err != nil || result.ExitCode != 0 {
			return errors.New("renewal authentication hook failed")
		}
	}
	for _, challenge := range challenges {
		if result, err := f.runHook(ctx, hooks[1], challenge.domain, challenge.token); err != nil || result.ExitCode != 0 {
			return errors.New("renewal cleanup hook failed")
		}
	}
	if result, err := f.runHook(ctx, hooks[2], "", ""); err != nil || result.ExitCode != 0 {
		return errors.New("renewal deploy hook failed")
	}
	return nil
}

func (f *certbotFixture) resetHookLogs() {
	f.t.Helper()
	for _, path := range []string{f.dnsLog, f.systemctlLog} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			f.t.Fatal(err)
		}
	}
}

func (f *certbotFixture) writeExecutable(name, content string) {
	f.t.Helper()
	binRoot, err := os.OpenRoot(f.binDir)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() {
		if err := binRoot.Close(); err != nil {
			f.t.Errorf("close fixture bin: %v", err)
		}
	}()
	if err := binRoot.WriteFile(name, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := binRoot.Chmod(name, 0o750); err != nil {
		f.t.Fatal(err)
	}
}

func (f *certbotFixture) challengeValues() []string {
	f.t.Helper()
	entries, err := os.ReadDir(f.challengeDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		values = append(values, entry.Name())
	}
	return values
}

func (f *certbotFixture) systemctlCalls() []string {
	f.t.Helper()
	content, err := os.ReadFile(f.systemctlLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func (f *certbotFixture) dnsCalls() []string {
	f.t.Helper()
	content, err := os.ReadFile(f.dnsLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func makeCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.com"},
		NotBefore: time.Unix(1_700_000_000, 0), NotAfter: time.Unix(1_800_000_000, 0),
		DNSNames: []string{"example.com", "*.example.com"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func parseCertificate(t *testing.T, content []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("fullchain does not begin with a certificate PEM block")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return certificate
}

func captureOutput(t *testing.T, fn func()) ([]byte, []byte) {
	t.Helper()
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	fn()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := stdoutReader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrReader.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.Clone(stdout), bytes.Clone(stderr)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
