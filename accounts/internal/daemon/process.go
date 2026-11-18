package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Process carries the daemon's process inputs and optional HTTPS trust roots.
type Process struct {
	Args       []string
	LookupEnv  func(string) (string, bool)
	Stderr     io.Writer
	Executable func() (string, error)
	RootCAs    *x509.CertPool
}

// The daemon's exit statuses and endpoint environment variable.
const (
	ExitSuccess = 0
	ExitFailed  = 1
	ExitUsage   = 2
	EnvURL      = "ACCOUNTS_URL"
)

// NewClient uses roots to verify HTTPS peers and permits no redirects.
func NewClient(roots *x509.CertPool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Main validates startup inputs, identifies the executable and runs the daemon.
func Main(ctx context.Context, p Process, open func(context.Context) (cloud.Presigner, cloud.Metadata, error), m machine.Machine) int {
	if len(p.Args) != 0 {
		diagnose(p.Stderr, DiagArguments, nil)
		return ExitUsage
	}
	raw, ok := p.LookupEnv(EnvURL)
	u, err := parseBaseURL(raw)
	if !ok || err != nil || u.Scheme != "https" {
		diagnose(p.Stderr, DiagURL, nil)
		return ExitUsage
	}
	digest, err := ownDigest(p.Executable)
	if err != nil {
		diagnose(p.Stderr, DiagSetup, err)
		return ExitFailed
	}
	presigner, metadata, err := open(ctx)
	if err != nil {
		diagnose(p.Stderr, DiagSetup, err)
		return ExitFailed
	}
	client := NewClient(p.RootCAs)
	defer client.CloseIdleConnections()
	err = Run(ctx, Config{
		BaseURL:   strings.TrimRight(raw, "/"),
		HTTP:      client,
		Presigner: presigner,
		Metadata:  metadata,
		Machine:   m,
		Digest:    digest,
		Now:       time.Now,
		Sleep:     sleep,
		Stderr:    p.Stderr,
	})
	if err == nil || errors.Is(err, context.Canceled) {
		return ExitSuccess
	}
	return ExitFailed
}

func ownDigest(executable func() (string, error)) (string, error) {
	path, err := executable()
	if err != nil {
		return "", err
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return protocol.Digest(f)
}

func sleep(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
