package cert_test

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

	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInspectionAPI(t *testing.T) {
	// R-YDSO-B3IA
	infoType := reflect.TypeFor[cert.Info]()
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{"Names", reflect.TypeFor[[]string]()},
		{"Issuer", reflect.TypeFor[string]()},
		{"Expires", reflect.TypeFor[time.Time]()},
	}
	if infoType.NumField() != len(wantFields) {
		t.Fatalf("Info has %d fields, want exactly %d", infoType.NumField(), len(wantFields))
	}
	for i, want := range wantFields {
		field := infoType.Field(i)
		if field.Name != want.name || field.Type != want.typeOf {
			t.Errorf("Info field %d = %s %v, want %s %v", i, field.Name, field.Type, want.name, want.typeOf)
		}
	}

	// R-YF0K-OV8Z
	requireInspectSignature(cert.Inspect)

	// R-YG8H-2MZO
	if cert.ErrNotFound == nil {
		t.Fatal("ErrNotFound is nil")
	}
}

func TestInspectReturnsFirstCertificateDataWithoutSideEffects(t *testing.T) {
	// R-YR7K-IKNX
	root := t.TempDir()
	hostName := "ikigenba.dev"
	expires := time.Date(2020, time.March, 4, 5, 6, 7, 0, time.UTC)
	first := certificatePEM(t, x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostName},
		Issuer:                pkix.Name{CommonName: "Test Issuer"},
		DNSNames:              []string{"*.ikigenba.dev", "ikigenba.dev", "www.ikigenba.dev"},
		NotBefore:             expires.Add(-48 * time.Hour),
		NotAfter:              expires,
		BasicConstraintsValid: true,
	})
	second := certificatePEM(t, x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "ignored"},
		Issuer:                pkix.Name{CommonName: "Ignored Issuer"},
		DNSNames:              []string{"ignored.example"},
		NotBefore:             expires.Add(-time.Hour),
		NotAfter:              expires.Add(time.Hour),
		BasicConstraintsValid: true,
	})
	writeFullchain(t, root, hostName, append(first, second...))
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rootFS.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	})
	relativePath := filepath.Join("etc", "letsencrypt", "live", hostName, "fullchain.pem")
	before, err := rootFS.ReadFile(relativePath)
	if err != nil {
		t.Fatal(err)
	}
	executed := false
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		executed = true
		return host.Result{}, errors.New("unexpected execution")
	}}

	got, err := cert.Inspect(env, hostName)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if executed {
		t.Fatal("Inspect executed an external command")
	}
	wantNames := []string{"*.ikigenba.dev", "ikigenba.dev", "www.ikigenba.dev"}
	if !reflect.DeepEqual(got.Names, wantNames) {
		t.Errorf("Names = %v, want encoded order %v", got.Names, wantNames)
	}
	if got.Issuer != "Test Issuer" {
		t.Errorf("Issuer = %q, want %q", got.Issuer, "Test Issuer")
	}
	if !got.Expires.Equal(expires) {
		t.Errorf("Expires = %v, want %v", got.Expires, expires)
	}
	after, err := rootFS.ReadFile(relativePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Error("Inspect changed fullchain.pem")
	}
}

func TestInspectUsesIssuerDistinguishedNameFallback(t *testing.T) {
	root := t.TempDir()
	hostName := "ikigenba.dev"
	issuer := pkix.Name{Organization: []string{"Ikigenba CA"}, Country: []string{"US"}}
	writeFullchain(t, root, hostName, certificatePEM(t, x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostName},
		Issuer:                issuer,
		DNSNames:              []string{hostName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
	}))

	got, err := cert.Inspect(host.Env{Root: root}, hostName)
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer != issuer.String() {
		t.Errorf("Issuer = %q, want distinguished name %q", got.Issuer, issuer.String())
	}
}

func TestInspectNotFound(t *testing.T) {
	// R-YSFG-WCEM
	for _, test := range []struct {
		name    string
		makeDir bool
	}{
		{name: "lineage absent"},
		{name: "fullchain absent", makeDir: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			hostName := "ikigenba.dev"
			if test.makeDir {
				makeLineage(t, root, hostName)
			}
			got, err := cert.Inspect(host.Env{Root: root}, hostName)
			if !errors.Is(err, cert.ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if err.Error() != "no certificate for "+hostName {
				t.Errorf("error = %q", err)
			}
			if !reflect.DeepEqual(got, cert.Info{}) {
				t.Errorf("Info = %#v, want zero value", got)
			}
		})
	}
}

func TestInspectOperationalErrors(t *testing.T) {
	hostName := "ikigenba.dev"
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "no certificate PEM", data: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("key")}), want: "no PEM certificate block"},
		{name: "invalid X.509", data: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid")}), want: "parse certificate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFullchain(t, root, hostName, test.data)
			got, err := cert.Inspect(host.Env{Root: root}, hostName)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want operational error containing %q", err, test.want)
			}
			if errors.Is(err, cert.ErrNotFound) {
				t.Fatalf("operational error matched ErrNotFound: %v", err)
			}
			if !reflect.DeepEqual(got, cert.Info{}) {
				t.Errorf("Info = %#v, want zero value", got)
			}
		})
	}
}

func TestInspectUnreadableFullchainIsOperationalError(t *testing.T) {
	root := t.TempDir()
	hostName := "ikigenba.dev"
	path := writeFullchain(t, root, hostName, []byte("unreadable"))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Errorf("restore fullchain permissions: %v", err)
		}
	})

	got, err := cert.Inspect(host.Env{Root: root}, hostName)
	if err == nil || !strings.Contains(err.Error(), "read certificate") {
		t.Fatalf("error = %v, want unreadable-file operational error", err)
	}
	if errors.Is(err, cert.ErrNotFound) {
		t.Fatalf("unreadable-file error matched ErrNotFound: %v", err)
	}
	if !reflect.DeepEqual(got, cert.Info{}) {
		t.Errorf("Info = %#v, want zero value", got)
	}
}

func writeFullchain(t *testing.T, root, hostName string, contents []byte) string {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rootFS.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	}()
	dir := filepath.Join("etc", "letsencrypt", "live", hostName)
	if err := rootFS.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := rootFS.WriteFile(filepath.Join(dir, "fullchain.pem"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, dir, "fullchain.pem")
}

func makeLineage(t *testing.T, root, hostName string) {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rootFS.Close(); err != nil {
			t.Errorf("close root: %v", err)
		}
	}()
	if err := rootFS.MkdirAll(filepath.Join("etc", "letsencrypt", "live", hostName), 0o700); err != nil {
		t.Fatal(err)
	}
}

func requireInspectSignature(func(host.Env, string) (cert.Info, error)) {}

func certificatePEM(t *testing.T, template x509.Certificate) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := template
	issuer.Subject = template.Issuer
	issuer.IsCA = true
	issuer.KeyUsage = x509.KeyUsageCertSign
	der, err := x509.CreateCertificate(rand.Reader, &template, &issuer, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
