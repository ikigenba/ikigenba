package agentkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// R-CM76-V0HW
func TestMakeLiveSetsOAuthFiles(t *testing.T) {
	recipe, err := exec.Command("make", "--no-print-directory", "--silent", "--dry-run", "live").Output()
	if err != nil {
		t.Fatalf("expand make live recipe: %v", err)
	}
	// Execute the expanded recipe with a shell function replacing only the
	// test runner, so the actual recipe's environment assignments are exercised.
	script := "go() { printf '%s\\n' \"$AGENTKIT_OPENAI_OAUTH_FILE\" \"$AGENTKIT_XAI_OAUTH_FILE\"; }\n" + string(recipe)
	command := exec.Command("sh")
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live recipe with substituted runner: %v\n%s", err, output)
	}
	home := os.Getenv("HOME")
	want := filepath.Join(home, ".agentkit", "openai-auth.json") + "\n" + filepath.Join(home, ".agentkit", "x-ai-auth.json") + "\n"
	if string(output) != want {
		t.Fatalf("live OAuth file environment = %q, want %q", output, want)
	}
}
