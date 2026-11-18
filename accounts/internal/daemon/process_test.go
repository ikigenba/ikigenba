package daemon_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// R-KYWP-YMV1 R-6C3Y-HFL4
func TestProcessConstants(t *testing.T) {
	if daemon.ExitSuccess != 0 || daemon.ExitFailed != 1 || daemon.ExitUsage != 2 ||
		daemon.MinBackoff != 5*time.Second || daemon.MaxBackoff != time.Minute ||
		daemon.RequestTimeout != protocol.LongPoll+15*time.Second || daemon.DownloadTimeout != 5*time.Minute ||
		daemon.EnvURL != "ACCOUNTS_URL" || daemon.UserAgent != "accountsd" {
		t.Error("daemon constants differ from the process contract")
	}
	const copyText = daemon.DiagArguments + daemon.DiagURL + daemon.DiagSetup + daemon.DiagFetch +
		daemon.DiagUpdate + daemon.DiagApply + daemon.DiagReport
	if copyText == "" {
		t.Error("empty daemon diagnostics")
	}
}

// R-167Y-31IB R-17FU-GT90
func TestNewClientTrustAndRedirects(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Error("client followed a redirect")
		}
		w.Header().Set("Location", "/redirect-target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	root := x509.NewCertPool()
	root.AddCert(server.Certificate())
	for _, tc := range []struct {
		label string
		pool  *x509.CertPool
		trust bool
	}{
		{label: "trusted", pool: root, trust: true},
		{label: "empty", pool: x509.NewCertPool()},
		{label: "system"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			client := daemon.NewClient(tc.pool)
			defer client.CloseIdleConnections()
			tr, ok := client.Transport.(*http.Transport)
			if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs != tc.pool || client.Timeout != 0 {
				t.Fatalf("client does not carry the injected roots and request-only timeouts: %+v", client)
			}
			if tc.pool == nil {
				return
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if !tc.trust {
				if err == nil {
					_ = resp.Body.Close()
					t.Errorf("client without the fixture certificate unexpectedly connected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusTemporaryRedirect {
				t.Errorf("status = %d, want 307", resp.StatusCode)
			}
		})
	}
}

// R-1501-P9RM R-6WU8-ZJ6X R-6Y25-DAXM
func TestMainValidatesBeforeExecutableAndOpen(t *testing.T) {
	var tests = []struct {
		name string
		args []string
		url  string
		ok   bool
		diag string
	}{
		{name: "arguments", args: []string{"anything"}, url: "https://accounts.example", ok: true, diag: daemon.DiagArguments},
		{name: "absent", diag: daemon.DiagURL},
		{name: "empty", ok: true, diag: daemon.DiagURL},
		{name: "relative", url: "/accounts", ok: true, diag: daemon.DiagURL},
		{name: "insecure", url: "http://accounts.example", ok: true, diag: daemon.DiagURL},
		{name: "no host", url: "https:/accounts", ok: true, diag: daemon.DiagURL},
		{name: "user info", url: "https://user@accounts.example", ok: true, diag: daemon.DiagURL},
		{name: "query", url: "https://accounts.example/prefix?param=1", ok: true, diag: daemon.DiagURL},
		{name: "fragment", url: "https://accounts.example/prefix#fragment", ok: true, diag: daemon.DiagURL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			lookups := 0
			p := daemon.Process{
				Args: tc.args,
				LookupEnv: func(key string) (string, bool) {
					lookups++
					if key != daemon.EnvURL {
						t.Errorf("lookup for %q", key)
					}
					return tc.url, tc.ok
				},
				Stderr: &stderr,
				Executable: func() (string, error) {
					t.Fatal("executable called after invalid input")
					return "", nil
				},
			}
			code := daemon.Main(t.Context(), p, func(context.Context) (cloud.Presigner, cloud.Metadata, error) {
				t.Fatal("open called after invalid input")
				return nil, nil, nil
			}, nil)
			wantLookups := 1
			if len(tc.args) != 0 {
				wantLookups = 0
			}
			if code != daemon.ExitUsage || lookups != wantLookups || stderr.String() != "accountsd: "+tc.diag+"\n" {
				t.Errorf("Main = %d, looked up %d times, stderr %q", code, lookups, stderr.String())
			}
		})
	}
}

// R-9A7O-IWEP R-6VMC-LRG8
func TestMainSetupFailure(t *testing.T) {
	for _, step := range []string{"executable", "digest", "open"} {
		t.Run(step, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "daemon")
			if step == "open" {
				if err := os.WriteFile(file, []byte("daemon"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var stderr bytes.Buffer
			var order []string
			p := daemon.Process{
				LookupEnv: func(string) (string, bool) { return "https://accounts.example", true },
				Stderr:    &stderr,
				Executable: func() (string, error) {
					order = append(order, "executable")
					if step == "executable" {
						return "", errors.New("executable\r\nfailed")
					}
					return file, nil
				},
			}
			got := daemon.Main(t.Context(), p, func(context.Context) (cloud.Presigner, cloud.Metadata, error) {
				order = append(order, "open")
				return nil, nil, errors.New("open\nfailed")
			}, nil)
			want := []string{"executable"}
			if step == "open" {
				want = append(want, "open")
			}
			if got != daemon.ExitFailed || !slices.Equal(order, want) {
				t.Errorf("Main = %d, calls %v, want %v", got, order, want)
			}
			out := stderr.String()
			if !strings.HasPrefix(out, "accountsd: "+daemon.DiagSetup+": ") || strings.Count(out, "\n") != 1 ||
				!strings.HasSuffix(out, "\n") || strings.ContainsRune(out, '\r') {
				t.Errorf("stderr = %q", out)
			}
		})
	}
}

type cancelWriter struct {
	io.Writer
	cancel context.CancelFunc
}

func (w cancelWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.cancel()
	return n, err
}

// R-9A7O-IWEP R-18NQ-UKZP
func TestMainDigestsThenFetchesThroughItsHTTPSClient(t *testing.T) {
	const original = "\x7fELForiginal"
	const update = "\x7fELFupdate"
	file := filepath.Join(t.TempDir(), "daemon")
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := protocol.Digest(strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	updateDigest, err := protocol.Digest(strings.NewReader(update))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		redirect  bool
		expired   bool
		code      int
		wantPaths []string
	}{
		{name: "redirect stops on cancellation", redirect: true, code: daemon.ExitSuccess, wantPaths: []string{"/prefix" + protocol.StatePath}},
		{name: "self-update", code: daemon.ExitSuccess, wantPaths: []string{"/prefix" + protocol.StatePath, "/prefix" + protocol.BinaryPath}},
		{name: "deadline exceeded", expired: true, code: daemon.ExitFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.expired {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Time{})
				defer stop()
			}
			var mu sync.Mutex
			var paths []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				paths = append(paths, req.URL.Path)
				mu.Unlock()
				switch req.URL.Path {
				case "/prefix" + protocol.StatePath:
					query := req.URL.Query()
					if query.Get(protocol.QueryAfter) != "0" || query.Get(protocol.QueryDigest) != digest {
						t.Errorf("first state query = %v", query)
					}
					if tc.redirect {
						http.Redirect(w, req, "/redirect-target", http.StatusTemporaryRedirect)
						return
					}
					_, _ = fmt.Fprint(w, state(8, updateDigest).body)
				case "/prefix" + protocol.BinaryPath:
					_, _ = io.WriteString(w, update)
				default:
					t.Errorf("unexpected request to %s", req.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			root := x509.NewCertPool()
			root.AddCert(server.Certificate())
			var stderr bytes.Buffer
			p := daemon.Process{
				LookupEnv:  func(string) (string, bool) { return server.URL + "/prefix///", true },
				Stderr:     cancelWriter{Writer: &stderr, cancel: cancel},
				Executable: func() (string, error) { return file, nil },
				RootCAs:    root,
			}
			m := machine.NewMemory(machine.MemoryConfig{Now: func() time.Time { return time.Time{} }, Sleep: func(context.Context, time.Duration) {}})
			host := machineProbe{Machine: m, install: func(ctx context.Context, body io.Reader, digest string) error {
				if err := m.InstallDaemon(ctx, body, digest); err != nil {
					return err
				}
				cancel()
				return ctx.Err()
			}}
			if code := daemon.Main(ctx, p, func(got context.Context) (cloud.Presigner, cloud.Metadata, error) {
				if got != ctx {
					t.Error("open received a different context")
				}
				p := presignerFunc(func(context.Context, string, time.Duration) (string, error) {
					return "https://identity.example", nil
				})
				return p, nil, nil
			}, host); code != tc.code {
				t.Errorf("Main = %d, want %d, stderr %q", code, tc.code, &stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(paths, tc.wantPaths) {
				t.Errorf("paths = %v, want %v", paths, tc.wantPaths)
			}
			if tc.redirect && !strings.HasPrefix(stderr.String(), "accountsd: "+daemon.DiagFetch+": ") {
				t.Errorf("redirect was not a failed fetch: %q", &stderr)
			}
			if !tc.redirect && !tc.expired && m.State().Daemon != updateDigest {
				t.Errorf("machine did not install daemon: %+v", m.State())
			}
		})
	}
}
