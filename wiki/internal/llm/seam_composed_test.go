package llm

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestComposedPromptsSeamEnsureAndGet(t *testing.T) {
	// R-UF1D-AWZ1
	// R-UG99-OOPQ
	baseURL := startComposedPrompts(t)
	client := New(baseURL, &http.Client{Timeout: 2 * time.Second})
	contextJSON := []byte(`{"page_id":42,"source":"pipeline"}`)
	request := Request{
		Key:     "pipeline:compile:page-42",
		Context: contextJSON,
		Site: CallSite{
			Stage:  "compile",
			System: "Return the compiled page as JSON.",
			Config: Config{Model: "gpt-5.6-luna"},
		},
		Attr: Attribution{
			Origin:  "service:wiki",
			GroupID: "page-42",
		},
		Messages: []Message{{Role: "user", Text: "Compile page 42."}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := client.Ensure(ctx, request)
	if err != nil {
		t.Fatalf("Ensure object-shaped pipeline request against real prompts: %v", err)
	}
	if first.ID == "" {
		t.Fatal("Ensure returned an empty item id")
	}
	if first.Status != "queued" {
		t.Fatalf("Ensure status = %q, want queued", first.Status)
	}

	second, err := client.Ensure(ctx, request)
	if err != nil {
		t.Fatalf("Ensure same key a second time against real prompts: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second Ensure id = %q, want original id %q", second.ID, first.ID)
	}

	got, err := client.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("Get ensured item from real prompts: %v", err)
	}
	if !bytes.Equal(got.Context, contextJSON) {
		t.Fatalf("Get context = %s, want byte-identical %s", got.Context, contextJSON)
	}
}

func startComposedPrompts(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	binary := filepath.Join(tempDir, "prompts")
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	build := exec.Command("go", "build", "-o", binary, "prompts/cmd/prompts")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sibling prompts binary: %v\n%s", err, output)
	}

	dbPath := filepath.Join(tempDir, "prompts.db")
	env := composedPromptsEnv(dbPath)
	promptsRoot := filepath.Join(repoRoot, "prompts")
	migrate := exec.Command(binary, "migrate")
	migrate.Dir = promptsRoot
	migrate.Env = env
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate prompts database: %v\n%s", err, output)
	}

	port := composedFreePort(t)
	var stdout, stderr bytes.Buffer
	serveCtx, stop := context.WithCancel(context.Background())
	serve := exec.CommandContext(serveCtx, binary, "serve", "--port", strconv.Itoa(port))
	serve.Dir = promptsRoot
	serve.Env = env
	serve.Stdout = &stdout
	serve.Stderr = &stderr
	if err := serve.Start(); err != nil {
		stop()
		t.Fatalf("start prompts server: %v", err)
	}
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = serve.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			if serve.Process != nil {
				_ = serve.Process.Kill()
			}
			<-done
		}
	})

	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			stop()
			t.Fatalf("prompts exited before accepting connections: %v\nstdout:\n%s\nstderr:\n%s", serveErr, stdout.String(), stderr.String())
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return "http://" + address
		}
		time.Sleep(50 * time.Millisecond)
	}

	stop()
	<-done
	t.Fatalf("prompts never accepted connections at %s\nstdout:\n%s\nstderr:\n%s", address, stdout.String(), stderr.String())
	return ""
}

func composedPromptsEnv(dbPath string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PROMPTS_DB_PATH" || strings.HasSuffix(key, "_API_KEY") {
			continue
		}
		env = append(env, entry)
	}
	// The completion endpoint validates the selected provider's credential at
	// enqueue time. Supply a non-secret sentinel; serve only queues this item and
	// never attempts inference.
	return append(env, "PROMPTS_DB_PATH="+dbPath, "OPENAI_API_KEY=composed-test-not-a-real-key")
}

func composedFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve prompts port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
