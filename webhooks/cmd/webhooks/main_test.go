package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// buildBinary compiles the real cmd/webhooks binary to a temp path and returns it.
// Tests exercise the genuine composition root through this artifact — not a
// re-implemented Spec — so a wrong port/flag or wiring slip in main.go is caught.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "webhooks.bin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/webhooks: %v\n%s", err, out)
	}
	return bin
}

// freePort returns an OS-assigned free TCP port on loopback.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// R-IC14-FKIK — the real binary's `manifest` verb byte-equals the committed
// etc/manifest.env, and that file declares the exact webhooks identity (a wrong
// port/flag/extra or key reordering must fail here).
func TestManifestVerbByteEqualsCommittedFile(t *testing.T) {
	bin := buildBinary(t)

	got, err := exec.Command(bin, "manifest").Output()
	if err != nil {
		t.Fatalf("run manifest verb: %v", err)
	}

	committed, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.env"))
	if err != nil {
		t.Fatalf("read committed manifest.env: %v", err)
	}

	if string(got) != string(committed) {
		t.Fatalf("manifest verb stdout != committed etc/manifest.env\n--- verb ---\n%s\n--- committed ---\n%s", got, committed)
	}

	// The committed manifest must declare webhooks's identity in the sibling field
	// order (APP, MOUNT, DEFAULT=false, PORT, MCP, FEED, then the ordered extras).
	want := "APP=webhooks\n" +
		"MOUNT=/srv/webhooks/\n" +
		"DEFAULT=false\n" +
		"PORT=3011\n" +
		"MCP=true\n" +
		"FEED=/feed\n" +
		"OUTBOX_RETENTION_DAYS=7\n" +
		"OUTBOX_RETENTION_MAX_ROWS=1000000\n"
	if string(committed) != want {
		t.Fatalf("committed manifest.env:\n%s\nwant:\n%s", committed, want)
	}
}

// R-ID90-TC99 — `serve` against a clean empty temp-file SQLite applies all
// migrations and brings up a real loopback server answering /health with HTTP 200
// and a health envelope whose service field is "webhooks". No mocks: a real
// temp-file DB and a real listening server reached over the network.
func TestServeMigratesAndServesHealth(t *testing.T) {
	bin := buildBinary(t)

	dbPath := filepath.Join(t.TempDir(), "webhooks.db")
	port := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = append(os.Environ(),
		"WEBHOOKS_DB_PATH="+dbPath,
		"WEBHOOKS_PORT="+strconv.Itoa(port),
		// Provide explicit dev URLs so config.Resolve doesn't need IKIGENBA_DOMAIN.
		"WEBHOOKS_RESOURCE_ID=http://127.0.0.1/srv/webhooks/mcp",
		"WEBHOOKS_AUTH_SERVER=http://127.0.0.1",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/health"
	var resp *http.Response
	deadline := time.Now().Add(15 * time.Second)
	for {
		r, err := http.Get(url)
		if err == nil {
			resp = r
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never answered /health: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want 200", resp.StatusCode)
	}

	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode health envelope: %v", err)
	}
	if env["service"] != "webhooks" {
		t.Fatalf("health service = %v, want webhooks (envelope: %v)", env["service"], env)
	}

	// The DB file the chassis migrated must exist on disk (real temp-file SQLite,
	// not :memory:).
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("migrated DB %s missing: %v", dbPath, err)
	}
}
