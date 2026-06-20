package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestServeFailsLoudWhenAnthropicKeyMissing(t *testing.T) {
	// R-6RVX-P1IG
	for _, value := range []string{"", "   "} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "go", "run", ".", "serve")
		cmd.Env = withoutAnthropicKey(os.Environ())
		cmd.Env = append(cmd.Env, "ANTHROPIC_API_KEY="+value)
		out, err := cmd.CombinedOutput()
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatal("serve did not fail before startup timeout")
		}
		if err == nil {
			t.Fatalf("serve with ANTHROPIC_API_KEY=%q exited 0; output:\n%s", value, out)
		}
		if !strings.Contains(string(out), "ANTHROPIC_API_KEY is required") {
			t.Fatalf("serve output = %q, want missing-key error", out)
		}
	}
}

func withoutAnthropicKey(env []string) []string {
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
