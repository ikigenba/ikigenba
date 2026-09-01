package agentkit_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

type signingAuth struct {
	t              *testing.T
	wantContext    context.Context
	applyCallCount int
}

type authContextKey struct{}

func (auth *signingAuth) Apply(ctx context.Context, request *http.Request, body []byte) error {
	auth.applyCallCount++
	if ctx != auth.wantContext {
		auth.t.Error("auth did not receive final request context")
	}
	if !strings.Contains(string(body), `"messages"`) {
		auth.t.Errorf("auth body = %q, want encoded Anthropic body", body)
	}
	request.Header.Set("X-Body-Signature", fmt.Sprint(len(body)))
	return nil
}

func TestGenericKnownWireAcceptsBareAuthApplier(t *testing.T) {
	// R-3KQY-RN5S
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.Header.Get("X-Body-Signature") == "" {
			t.Error("body-signing AuthApplier was not called")
		}
		writer.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), authContextKey{}, "final")
	auth := &signingAuth{t: t, wantContext: ctx}
	conversation, err := agentkit.NewKnownWireConversation(agentkit.KnownWireAnthropic, server.URL, auth)
	if err != nil {
		t.Fatal(err)
	}
	conversation.Send(ctx, agentkit.Text{Text: "hello"})
	if auth.applyCallCount != 1 || requestCount != 1 {
		t.Fatalf("calls: auth=%d request=%d", auth.applyCallCount, requestCount)
	}
}

func TestVendorCredentialsAndOptionsAreCompileTimeIsolated(t *testing.T) {
	// R-3IB6-03OE
	temporary := t.TempDir()
	writeCompileFixture(t, temporary)
	output, err := runCompileFixture(temporary)
	if err == nil {
		t.Fatal("cross-vendor credentials and options unexpectedly compiled")
	}
	assertCompileIsolation(t, output)
}

func writeCompileFixture(t *testing.T, directory string) {
	t.Helper()
	module := `module compilefixture

go 1.26

require github.com/ikigenba/ikigenba/agentkit v0.0.0

replace github.com/ikigenba/ikigenba/agentkit => ` + moduleRoot(t) + "\n"
	source := `package compilefixture

import (
	"github.com/ikigenba/ikigenba/agentkit/anthropic"
	"github.com/ikigenba/ikigenba/agentkit/openai"
)

func invalid() {
	_, _ = anthropic.New(openai.APIKey("wrong"))
	_, _ = openai.New(anthropic.APIKey("wrong"))
	_, _ = anthropic.New(anthropic.APIKey("key"), openai.WithBaseURL("https://example.test"))
}
`
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "compile_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runCompileFixture(directory string) ([]byte, error) {
	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	output, err := command.CombinedOutput()
	return output, err
}

func assertCompileIsolation(t *testing.T, output []byte) {
	t.Helper()
	for _, want := range []string{"missing method apply", "openai.Option"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("compile failure missing %q:\n%s", want, output)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
