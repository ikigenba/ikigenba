package daemon_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// R-JRLF-28PP
var _ struct {
	Args       []string
	LookupEnv  func(string) (string, bool)
	Stderr     io.Writer
	Executable func() (string, error)
	RootCAs    *x509.CertPool
	Socket     string
} = daemon.Process{}

var _ func(context.Context, daemon.Process, func(context.Context) (cloud.Presigner, cloud.Metadata, error), machine.Machine) int = daemon.Main

func boxSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "accountsd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return filepath.Join(dir, "box.sock")
}

// R-JV94-7JXS R-KDJL-Y427
var _ func(net.Conn) (int64, error) = daemon.KernelUID

func TestKernelUIDGetsUnixPeerCredential(t *testing.T) {
	path := boxSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	dial := net.Dialer{}
	client, err := dial.DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if uid, err := daemon.KernelUID(conn); err != nil || uid != int64(os.Getuid()) {
		t.Errorf("KernelUID = %d, %v, want %d", uid, err, os.Getuid())
	}
	piped, other := net.Pipe()
	defer func() { _ = piped.Close(); _ = other.Close() }()
	if _, err := daemon.KernelUID(piped); err == nil {
		t.Error("KernelUID accepted a non-Unix connection")
	}
}

// R-KERI-BVSW R-KH7B-3FAA R-KIF7-H70Z
func TestMainReplacesStaleSocketPathAndUsesKernelUID(t *testing.T) {
	for _, stale := range []string{"file", "symlink"} {
		t.Run(stale, func(t *testing.T) {
			path := boxSocketPath(t)
			file := filepath.Join(t.TempDir(), "daemon")
			const executable = "daemon bytes"
			if err := os.WriteFile(file, []byte(executable), 0o600); err != nil {
				t.Fatal(err)
			}
			if stale == "file" {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(file, path); err != nil {
				t.Fatal(err)
			}
			st := activeBoxState(1, "")
			st.Users[0].UID = int64(os.Getuid())
			const message = `{"result":{"clause":"<&>"}}`
			forwarded := make(chan string, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case protocol.StatePath:
					if req.URL.Query().Get(protocol.QueryAfter) != "0" {
						<-req.Context().Done()
						return
					}
					if err := json.NewEncoder(w).Encode(st); err != nil {
						t.Error(err)
					}
				case protocol.ReportPath:
					w.WriteHeader(http.StatusNoContent)
				case protocol.BoxPath:
					forwarded <- req.URL.Query().Get(protocol.QueryUID)
					_, _ = io.WriteString(w, message)
				default:
					t.Errorf("unexpected path %s", req.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			ctx, cancel := context.WithCancel(t.Context())
			ready := make(chan struct{})
			now := func() time.Time { return time.Time{} }
			pause := func(context.Context, time.Duration) {}
			memory := machine.NewMemory(machine.MemoryConfig{Now: now, Sleep: pause})
			m := machineProbe{Machine: memory, apply: func(ctx context.Context, s protocol.DesiredState) error {
				close(ready)
				return memory.Apply(ctx, s)
			}}
			var stderr bytes.Buffer
			var lookups []string
			p := daemon.Process{
				LookupEnv: func(key string) (string, bool) {
					lookups = append(lookups, key)
					return server.URL, true
				},
				Stderr: &stderr, Executable: func() (string, error) { return file, nil },
				RootCAs: roots, Socket: path,
			}
			result := make(chan int, 1)
			go func() {
				result <- daemon.Main(ctx, p, func(context.Context) (cloud.Presigner, cloud.Metadata, error) {
					info, err := os.Lstat(path)
					want := os.FileMode(0)
					if stale == "symlink" {
						want = os.ModeSymlink
					}
					if err != nil || info.Mode().Type() != want {
						t.Errorf("socket path changed before open succeeded: %v, %v", info, err)
					}
					presigner := presignerFunc(func(context.Context, string, time.Duration) (string, error) {
						return "https://identity.example", nil
					})
					metadata := metadataFunc(func(context.Context) (cloud.InstanceFacts, error) { return cloud.InstanceFacts{}, nil })
					return presigner, metadata, nil
				}, m)
				close(result)
			}()
			t.Cleanup(func() {
				cancel()
				waitBox(t, result)
			})
			waitBox(t, ready)
			if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != daemon.SocketMode {
				t.Fatalf("socket at %s = %v, %v", path, info, err)
			}
			dial := net.Dialer{}
			client, err := dial.DialContext(ctx, "unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			socketLine(t, client, boxRequest(protocol.MethodToolsCall, "1")+"\n")
			expectBoxAnswer(t, client, boxmcp.Answer{Message: json.RawMessage(message)})
			if got := waitBox(t, forwarded); got != strconv.Itoa(os.Getuid()) {
				t.Errorf("forward uid = %q, want %d", got, os.Getuid())
			}
			cancel()
			if code := waitBox(t, result); code != daemon.ExitSuccess || stderr.Len() != 0 ||
				len(lookups) != 1 || lookups[0] != daemon.EnvURL {
				t.Errorf("Main = %d, stderr %q, lookups %v", code, &stderr, lookups)
			}
			if info, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("socket path after Main = %v, %v", info, err)
			}
			if data, err := os.ReadFile(filepath.Clean(file)); err != nil || string(data) != executable {
				t.Errorf("symlink destination changed: %q, %v", data, err)
			}
		})
	}
}

// R-KFZE-PNJL
func TestMainKeepsReconcilingWhenTheSocketCannotBeSetUp(t *testing.T) {
	for _, kind := range []string{"none", "directory", "missing parent"} {
		t.Run(kind, func(t *testing.T) {
			path := boxSocketPath(t)
			switch kind {
			case "none":
				path = ""
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "missing parent":
				path = filepath.Join(path, "socket")
			}
			file := filepath.Join(t.TempDir(), "daemon")
			if err := os.WriteFile(file, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fetched := make(chan string, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				fetched <- req.URL.Path
				cancel()
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			var stderr bytes.Buffer
			cfg := daemon.Process{
				LookupEnv: func(string) (string, bool) { return server.URL, true },
				Stderr:    &stderr, Executable: func() (string, error) { return file, nil },
				RootCAs: roots, Socket: path,
			}
			code := daemon.Main(ctx, cfg, func(context.Context) (cloud.Presigner, cloud.Metadata, error) {
				return presignerFunc(func(context.Context, string, time.Duration) (string, error) {
					return "https://identity.example", nil
				}), nil, nil
			}, nil)
			if code != daemon.ExitSuccess || waitBox(t, fetched) != protocol.StatePath {
				t.Errorf("Main = %d, expected it to fetch state", code)
			}
			out := stderr.String()
			if kind == "none" && out != "" || kind != "none" &&
				(!strings.HasPrefix(out, "accountsd: "+daemon.DiagSocket+": ") || strings.Count(out, "\n") != 1) {
				t.Errorf("stderr = %q", out)
			}
			if kind == "directory" {
				if info, err := os.Stat(path); err != nil || !info.IsDir() {
					t.Errorf("socket directory after Main = %v, %v", info, err)
				}
			}
		})
	}
}
