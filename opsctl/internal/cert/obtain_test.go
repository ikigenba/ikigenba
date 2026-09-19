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
	"io/fs"
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

const wantDeployHook = "systemctl try-reload-or-restart nginx"

// R-YBCV-JK0W R-2X8T-NIF1
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

func acceptObtainSignature(func(context.Context, host.Env, string, string, bool) error) {}

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
			root := t.TempDir()
			statePath := filepath.Join(root, "var/lib/opsctl/state")
			if err := os.MkdirAll(filepath.Dir(statePath), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotTree(t, root)
			env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				calls++
				return host.Result{}, nil
			}}
			err := cert.Obtain(context.Background(), env, tc.hostName, tc.email, false)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if calls != 0 {
				t.Fatalf("Execute calls = %d, want 0", calls)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("host state changed: before %#v, after %#v", before, after)
			}
		})
	}
}

// R-324F-6LDT
func TestObtainValidatesApexBeforeExecution(t *testing.T) {
	for _, tc := range []struct {
		name, hostName string
		apex           bool
		wantError      string
		wantCalls      int
	}{
		{"apex without parent", "example.com", true, "host.apex is set but host.name 'example.com' has no parent domain", 0},
		{"non-apex without parent", "example.com", false, "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "var/lib/opsctl/state")
			if err := os.MkdirAll(filepath.Dir(statePath), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotTree(t, root)
			calls := 0
			env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				calls++
				return host.Result{}, nil
			}}
			err := cert.Obtain(context.Background(), env, tc.hostName, "admin@example.com", tc.apex)
			if tc.wantError == "" && err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if tc.wantError != "" && (err == nil || err.Error() != tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if calls != tc.wantCalls {
				t.Fatalf("Execute calls = %d, want %d", calls, tc.wantCalls)
			}
			if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("host state changed: before %#v, after %#v", before, after)
			}
		})
	}
}

// R-AMEV-4J2G
func TestObtainRejectsMissingExecutionDependency(t *testing.T) {
	err := cert.Obtain(context.Background(), host.Env{Root: t.TempDir()}, "example.com", "admin@example.com", false)
	if err == nil || err.Error() != "obtain certificate: host execution is not configured" {
		t.Fatalf("error = %v", err)
	}
}

// R-33CB-KD4I R-AMEV-4J2G R-YYIY-T743
func TestObtainExecutesExactCertbotCommand(t *testing.T) {
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	var gotCtx context.Context
	var got host.Command
	calls := 0
	dnsStatePath := filepath.Join(root, "var/lib/dns/records")
	if err := os.MkdirAll(filepath.Dir(dnsStatePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsStatePath, []byte("existing TXT records"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)
	env := host.Env{Root: root, Execute: func(callCtx context.Context, command host.Command) (host.Result, error) {
		calls++
		if atExecution := snapshotTree(t, root); !reflect.DeepEqual(atExecution, before) {
			t.Fatalf("Obtain wrote host or DNS state before Execute: before %#v, at Execute %#v", before, atExecution)
		}
		gotCtx, got = callCtx, command
		return host.Result{}, nil
	}}
	if err := cert.Obtain(ctx, env, "example.com", "admin@example.com", false); err != nil {
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
	if calls != 1 {
		t.Fatalf("Execute calls = %d, want exactly 1", calls)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("Obtain wrote host or DNS state directly: before %#v, after %#v", before, after)
	}
}

// R-33CB-KD4I
func TestObtainExecutesExactCertbotCommandWithApex(t *testing.T) {
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), contextKey{}, "apex-marker")
	var gotCtx context.Context
	var got host.Command
	calls := 0
	env := host.Env{Root: root, Execute: func(callCtx context.Context, command host.Command) (host.Result, error) {
		calls++
		gotCtx, got = callCtx, command
		return host.Result{}, nil
	}}
	if err := cert.Obtain(ctx, env, "app.example.com", "admin@example.com", true); err != nil {
		t.Fatal(err)
	}
	want := host.Command{Name: "certbot", Args: []string{
		"certonly", "--non-interactive", "--agree-tos", "--email", "admin@example.com",
		"--manual", "--preferred-challenges", "dns",
		"--manual-auth-hook", "opsctl dns acme-auth",
		"--manual-cleanup-hook", "opsctl dns acme-cleanup",
		"--deploy-hook", wantDeployHook,
		"--cert-name", "app.example.com", "-d", "app.example.com", "-d", "*.app.example.com", "-d", "example.com",
		"--keep-until-expiring",
		"--config-dir", filepath.Join(root, "etc/letsencrypt"),
		"--work-dir", filepath.Join(root, "var/lib/letsencrypt"),
		"--logs-dir", filepath.Join(root, "var/log/letsencrypt"),
	}}
	if gotCtx != ctx {
		t.Fatal("Obtain did not pass the supplied context")
	}
	if calls != 1 {
		t.Fatalf("Execute calls = %d, want exactly 1", calls)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

// R-KAY4-F25I
func TestObtainHooksBeginWithPATHExecutables(t *testing.T) {
	binDir := t.TempDir()
	binRoot, err := os.OpenRoot(binDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := binRoot.Close(); err != nil {
			t.Errorf("close fixture bin: %v", err)
		}
	})
	for _, name := range []string{"opsctl", "systemctl"} {
		if err := binRoot.WriteFile(name, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := binRoot.Chmod(name, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	var got host.Command
	env := host.Env{Root: t.TempDir(), Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		got = command
		return host.Result{}, nil
	}}
	if err := cert.Obtain(context.Background(), env, "example.com", "admin@example.com", false); err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"--manual-auth-hook", "--manual-cleanup-hook", "--deploy-hook"} {
		hook := argumentValue(t, got.Args, option)
		words := strings.Fields(hook)
		if len(words) == 0 {
			t.Fatalf("%s hook is empty", option)
		}
		first := words[0]
		if first != filepath.Base(first) || strings.ContainsAny(first, ";|&(){}<>") {
			t.Fatalf("%s hook first word %q is not a bare executable name", option, first)
		}
		info, err := os.Stat(filepath.Join(binDir, first))
		if err != nil || info.Mode()&0o111 == 0 {
			t.Fatalf("%s hook executable %q does not resolve on fixture PATH", option, first)
		}
	}
}

// R-34K7-Y4V7
func TestObtainEstablishesLineageAndRetainsHooks(t *testing.T) {
	fixture := newCertbotFixtureForHost(t, issueCertificate, nil, "app.example.com")
	if err := cert.Obtain(context.Background(), fixture.env(), "app.example.com", "admin@example.com", true); err != nil {
		t.Fatal(err)
	}
	lineage := filepath.Join(fixture.root, "etc/letsencrypt/live/app.example.com")
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
	if want := []string{"app.example.com", "*.app.example.com", "example.com"}; !reflect.DeepEqual(certificate.DNSNames, want) {
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
		"dns acme-auth app.example.com renewal-apex-token",
		"dns acme-auth *.app.example.com renewal-wildcard-token",
		"dns acme-auth example.com renewal-parent-token",
		"dns acme-cleanup app.example.com renewal-apex-token",
		"dns acme-cleanup *.app.example.com renewal-wildcard-token",
		"dns acme-cleanup example.com renewal-parent-token",
	}
	if calls := fixture.dnsCalls(); !reflect.DeepEqual(calls, wantDNSCalls) {
		t.Fatalf("renewal DNS hook calls = %v, want %v", calls, wantDNSCalls)
	}
	if calls := fixture.systemctlCalls(); !reflect.DeepEqual(calls, []string{"try-reload-or-restart nginx"}) {
		t.Fatalf("renewal systemctl calls = %v", calls)
	}
}

// R-35S4-BWLW
func TestObtainLetsCertbotKeepNotDueCertificate(t *testing.T) {
	original, _ := makeCertificateForNames(t, []string{"example.com", "*.example.com"})
	fixture := newCertbotFixture(t, keepCertificate, original)
	if err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com", false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(fixture.fullchain())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, original) || len(fixture.challengeValues()) != 0 || len(fixture.systemctlCalls()) != 0 {
		t.Fatalf("not-due result: certificate %q, challenges %v, systemctl %v", got, fixture.challengeValues(), fixture.systemctlCalls())
	}
	if calls := fixture.dnsCalls(); len(calls) != 0 {
		t.Fatalf("not-due certificate invoked DNS hooks: %v", calls)
	}
	if fixture.caRequests != 0 {
		t.Fatalf("not-due certificate requested CA issuance %d times", fixture.caRequests)
	}
	if fixture.calls != 1 {
		t.Fatalf("certbot calls = %d, want 1", fixture.calls)
	}
}

// R-3700-POCL
func TestObtainReissuesMismatchedLineageAndThenKeepsIt(t *testing.T) {
	for _, tc := range []struct {
		name          string
		apex          bool
		existingNames []string
		wantNames     []string
	}{
		{
			name:          "add parent",
			apex:          true,
			existingNames: []string{"app.example.com", "*.app.example.com"},
			wantNames:     []string{"app.example.com", "*.app.example.com", "example.com"},
		},
		{
			name:          "remove parent",
			apex:          false,
			existingNames: []string{"app.example.com", "*.app.example.com", "example.com"},
			wantNames:     []string{"app.example.com", "*.app.example.com"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existingCertificate, _ := makeCertificateForNames(t, tc.existingNames)
			fixture := newCertbotFixtureForHost(t, reconcileCertificate, existingCertificate, "app.example.com")
			if err := cert.Obtain(context.Background(), fixture.env(), "app.example.com", "admin@example.com", tc.apex); err != nil {
				t.Fatal(err)
			}
			certificateContent, err := os.ReadFile(fixture.fullchain())
			if err != nil {
				t.Fatal(err)
			}
			if got := parseCertificate(t, certificateContent).DNSNames; !reflect.DeepEqual(got, tc.wantNames) {
				t.Fatalf("certificate names = %v, want %v", got, tc.wantNames)
			}
			lineagePath := filepath.Join(fixture.root, "etc/letsencrypt")
			settled := snapshotTree(t, lineagePath)
			if err := cert.Obtain(context.Background(), fixture.env(), "app.example.com", "admin@example.com", tc.apex); err != nil {
				t.Fatal(err)
			}
			if got := snapshotTree(t, lineagePath); !reflect.DeepEqual(got, settled) {
				t.Fatalf("matching second obtain changed lineage: before %#v, after %#v", settled, got)
			}
			if fixture.caRequests != 1 {
				t.Fatalf("CA issuance requests = %d, want 1", fixture.caRequests)
			}
			if challenges := fixture.challengeValues(); len(challenges) != 0 {
				t.Fatalf("challenge values left behind: %v", challenges)
			}
			if fixture.calls != 2 {
				t.Fatalf("certbot calls = %d, want 2", fixture.calls)
			}
			for _, command := range fixture.commands {
				if slices.Contains(command.Args, "--force-renewal") || slices.Contains(command.Args, "--force-renew") {
					t.Fatalf("Obtain requested forced renewal: %v", command.Args)
				}
			}
			renewal, err := os.ReadFile(filepath.Join(fixture.root, "etc/letsencrypt/renewal/app.example.com.conf"))
			if err != nil {
				t.Fatal(err)
			}
			wantHooks := "opsctl dns acme-auth\nopsctl dns acme-cleanup\n" + wantDeployHook
			if got := strings.TrimSpace(string(renewal)); got != wantHooks {
				t.Fatalf("recorded hooks = %q, want %q", got, wantHooks)
			}
		})
	}
}

// R-YORR-R16J R-GWME-QK2L
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
				err = cert.Obtain(context.Background(), env, "example.com", "admin@example.com", false)
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
	if err := cert.Obtain(context.Background(), env, "example.com", "admin@example.com", false); err != nil {
		t.Fatalf("successful execution returned %v", err)
	}
}

// R-GWME-QK2L
func TestObtainPreservesExistingCommandErrorIdentity(t *testing.T) {
	cause := errors.New("remote certbot failed")
	existing := &host.CommandError{
		Label:  "remote certbot",
		Result: host.Result{Stdout: []byte("captured output"), Stderr: []byte("captured detail"), ExitCode: 73},
		Err:    cause,
	}
	for _, returned := range []host.Result{
		{},
		{Stdout: []byte("different output"), Stderr: []byte("different detail"), ExitCode: 19},
	} {
		t.Run(strconv.Itoa(returned.ExitCode), func(t *testing.T) {
			env := host.Env{Execute: func(context.Context, host.Command) (host.Result, error) {
				return returned, existing
			}}
			err := cert.Obtain(context.Background(), env, "example.com", "admin@example.com", false)
			var got *host.CommandError
			if !errors.As(err, &got) || got != existing {
				t.Fatalf("error = %#v, want original CommandError %#v", err, existing)
			}
		})
	}
}

// R-3Y2Q-E7IX
func TestObtainRefusalPreservesCertificateAndCleansChallenges(t *testing.T) {
	fixture := newCertbotFixture(t, refuseCertificate, nil)
	originalCertificate, originalKey := makeCertificate(t)
	fixture.seedServingLineage(originalCertificate, originalKey)
	lineageBefore := snapshotTree(t, filepath.Join(fixture.root, "etc/letsencrypt"))
	nginxBefore := snapshotTree(t, filepath.Join(fixture.root, "etc/nginx"))
	fixture.assertNginxServes(t, originalCertificate, originalKey)
	err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com", false)
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error = %v, want certbot failure", err)
	}
	if lineageAfter := snapshotTree(t, filepath.Join(fixture.root, "etc/letsencrypt")); !reflect.DeepEqual(lineageAfter, lineageBefore) {
		t.Fatalf("certificate lineage or replacement artifacts changed: before %#v, after %#v", lineageBefore, lineageAfter)
	}
	if nginxAfter := snapshotTree(t, filepath.Join(fixture.root, "etc/nginx")); !reflect.DeepEqual(nginxAfter, nginxBefore) {
		t.Fatalf("nginx serving state changed: before %#v, after %#v", nginxBefore, nginxAfter)
	}
	fixture.assertNginxServes(t, originalCertificate, originalKey)
	if challenges := fixture.challengeValues(); len(challenges) != 0 {
		t.Fatalf("refusal left challenges %v", challenges)
	}
	wantDNSCalls := []string{
		"dns acme-auth example.com apex-token",
		"dns acme-auth *.example.com wildcard-token",
		"dns acme-cleanup example.com apex-token",
		"dns acme-cleanup *.example.com wildcard-token",
	}
	if calls := fixture.dnsCalls(); !reflect.DeepEqual(calls, wantDNSCalls) {
		t.Fatalf("refusal DNS hook calls = %v, want %v", calls, wantDNSCalls)
	}
	if fixture.caRequests != 1 {
		t.Fatalf("refusal CA issuance requests = %d, want 1", fixture.caRequests)
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
		{"active", true, false, []string{"try-reload-or-restart nginx"}, false},
		{"inactive", false, false, []string{"try-reload-or-restart nginx"}, false},
		{"reload failure", true, true, []string{"try-reload-or-restart nginx"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCertbotFixture(t, issueCertificate, nil)
			fixture.nginxActive = tc.active
			fixture.reloadFails = tc.reloadFails
			err := cert.Obtain(context.Background(), fixture.env(), "example.com", "admin@example.com", false)
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

func argumentValues(args []string, name string) []string {
	var values []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			values = append(values, args[i+1])
			i++
		}
	}
	return values
}

func fixtureChallenges(hostName string, domains []string, renewal bool) []struct{ domain, token string } {
	prefix := ""
	if renewal {
		prefix = "renewal-"
	}
	challenges := make([]struct{ domain, token string }, 0, len(domains))
	for _, domain := range domains {
		token := "parent-token"
		switch domain {
		case hostName:
			token = "apex-token"
		case "*." + hostName:
			token = "wildcard-token"
		}
		challenges = append(challenges, struct{ domain, token string }{domain, prefix + token})
	}
	return challenges
}

type certbotFixture struct {
	t            *testing.T
	root         string
	hostName     string
	scenario     certbotScenario
	nginxActive  bool
	reloadFails  bool
	calls        int
	caRequests   int
	commands     []host.Command
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
	reconcileCertificate
)

func newCertbotFixture(t *testing.T, scenario certbotScenario, certificate []byte) *certbotFixture {
	return newCertbotFixtureForHost(t, scenario, certificate, "example.com")
}

func newCertbotFixtureForHost(t *testing.T, scenario certbotScenario, certificate []byte, hostName string) *certbotFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &certbotFixture{
		t: t, root: root, hostName: hostName, scenario: scenario, nginxActive: true,
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
  try-reload-or-restart)
    if test "$FIXTURE_NGINX_ACTIVE" = 1; then
      test "$FIXTURE_RELOAD_FAIL" = 0
    fi
    ;;
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
	return filepath.Join(f.root, "etc/letsencrypt/live", f.hostName, "fullchain.pem")
}

func (f *certbotFixture) env() host.Env {
	return host.Env{Root: f.root, Execute: func(ctx context.Context, command host.Command) (host.Result, error) {
		f.calls++
		f.commands = append(f.commands, command)
		if command.Name != "certbot" || len(command.Args) == 0 || command.Args[0] != "certonly" {
			f.t.Fatalf("unexpected command: %#v", command)
		}
		if got := argumentValue(f.t, command.Args, "--cert-name"); got != f.hostName {
			f.t.Fatalf("certificate name = %q, want %q", got, f.hostName)
		}
		domains := argumentValues(command.Args, "-d")
		if f.scenario == keepCertificate && fileExists(f.fullchain()) {
			return host.Result{}, nil
		}
		if f.scenario == reconcileCertificate && fileExists(f.fullchain()) {
			content, err := os.ReadFile(f.fullchain())
			if err != nil {
				f.t.Fatal(err)
			}
			if reflect.DeepEqual(parseCertificate(f.t, content).DNSNames, domains) {
				return host.Result{}, nil
			}
		}
		authHook := argumentValue(f.t, command.Args, "--manual-auth-hook")
		cleanupHook := argumentValue(f.t, command.Args, "--manual-cleanup-hook")
		for _, challenge := range fixtureChallenges(f.hostName, domains, false) {
			if result, err := f.runHook(ctx, authHook, challenge.domain, challenge.token); err != nil || result.ExitCode != 0 {
				return result, err
			}
		}
		f.caRequests++
		for _, challenge := range fixtureChallenges(f.hostName, domains, false) {
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
		certificatePEM, privateKeyPEM := makeCertificateForNames(f.t, domains)
		if err := os.WriteFile(f.fullchain(), certificatePEM, 0o600); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lineage, "privkey.pem"), privateKeyPEM, 0o600); err != nil {
			f.t.Fatal(err)
		}
		renewalPath := filepath.Join(f.root, "etc/letsencrypt/renewal", f.hostName+".conf")
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

func (f *certbotFixture) seedServingLineage(certificate, privateKey []byte) {
	f.t.Helper()
	archive := filepath.Join(f.root, "etc/letsencrypt/archive", f.hostName)
	live := filepath.Dir(f.fullchain())
	if err := os.MkdirAll(archive, 0o750); err != nil {
		f.t.Fatal(err)
	}
	if err := os.MkdirAll(live, 0o750); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "fullchain1.pem"), certificate, 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "privkey1.pem"), privateKey, 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink("../../archive/"+f.hostName+"/fullchain1.pem", f.fullchain()); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink("../../archive/"+f.hostName+"/privkey1.pem", filepath.Join(live, "privkey.pem")); err != nil {
		f.t.Fatal(err)
	}
	renewalPath := filepath.Join(f.root, "etc/letsencrypt/renewal", f.hostName+".conf")
	if err := os.MkdirAll(filepath.Dir(renewalPath), 0o750); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(renewalPath, []byte("prior renewal configuration\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	nginxPath := filepath.Join(f.root, "etc/nginx/conf.d/certificate.conf")
	if err := os.MkdirAll(filepath.Dir(nginxPath), 0o750); err != nil {
		f.t.Fatal(err)
	}
	configuration := "ssl_certificate " + f.fullchain() + ";\nssl_certificate_key " + filepath.Join(live, "privkey.pem") + ";\n"
	if err := os.WriteFile(nginxPath, []byte(configuration), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *certbotFixture) assertNginxServes(t *testing.T, wantCertificate, wantPrivateKey []byte) {
	t.Helper()
	if !f.nginxActive {
		t.Fatal("nginx is not active")
	}
	root, err := os.OpenRoot(f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close fixture root: %v", err)
		}
	})
	configuration, err := root.ReadFile("etc/nginx/conf.d/certificate.conf")
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPath := filepath.Join(filepath.Dir(f.fullchain()), "privkey.pem")
	for _, path := range []string{f.fullchain(), privateKeyPath} {
		if !strings.Contains(string(configuration), path) {
			t.Fatalf("nginx configuration does not serve %s", path)
		}
	}
	certificate, err := root.ReadFile("etc/letsencrypt/live/example.com/fullchain.pem")
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := root.ReadFile("etc/letsencrypt/live/example.com/privkey.pem")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(certificate, wantCertificate) || !bytes.Equal(privateKey, wantPrivateKey) {
		t.Fatal("nginx lineage no longer resolves to the prior certificate and private key")
	}
	parsedCertificate := parseCertificate(t, certificate)
	keyBlock, _ := pem.Decode(privateKey)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		t.Fatal("served private key is not an RSA PEM block")
	}
	parsedKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, ok := parsedCertificate.PublicKey.(*rsa.PublicKey)
	if !ok || publicKey.N.Cmp(parsedKey.N) != 0 || publicKey.E != parsedKey.E {
		t.Fatal("served certificate and private key do not match")
	}
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
	content, err := root.ReadFile(filepath.Join("etc/letsencrypt/renewal", f.hostName+".conf"))
	if err != nil {
		return err
	}
	hooks := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(hooks) != 3 {
		return errors.New("invalid renewal hook configuration")
	}
	certificateContent, err := root.ReadFile(filepath.Join("etc/letsencrypt/live", f.hostName, "fullchain.pem"))
	if err != nil {
		return err
	}
	challenges := fixtureChallenges(f.hostName, parseCertificate(f.t, certificateContent).DNSNames, true)
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
	return makeCertificateForNames(t, []string{"example.com", "*.example.com"})
}

func makeCertificateForNames(t *testing.T, names []string) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[0]},
		NotBefore: time.Unix(1_700_000_000, 0), NotAfter: time.Unix(1_800_000_000, 0),
		DNSNames: names,
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

type treeEntry struct {
	Mode   fs.FileMode
	Data   string
	Target string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snapshot := make(map[string]treeEntry)
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rootFS.Close(); err != nil {
			t.Errorf("close snapshot root: %v", err)
		}
	}()
	err = filepath.WalkDir(root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := treeEntry{Mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			item.Target, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var content []byte
			content, err = rootFS.ReadFile(relative)
			item.Data = string(content)
		}
		if err != nil {
			return err
		}
		snapshot[relative] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
