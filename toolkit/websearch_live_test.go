//go:build live

package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLiveWebSearch(t *testing.T) {
	// R-BGC7-L8FL: the real Brave service accepts the default request and yields
	// a result with a public HTTPS URL. Never print credentials or response data.
	key := os.Getenv("BRAVE_API_KEY")
	if strings.TrimSpace(key) == "" {
		t.Fatal("BRAVE_API_KEY is required")
	}
	tool, err := WebSearch(key)
	if err != nil {
		t.Fatal("WebSearch construction failed")
	}
	answer, err := tool.Call(context.Background(), json.RawMessage(`{"query":"Wikipedia"}`))
	if err != nil {
		t.Fatal("Brave search failed")
	}
	for _, block := range strings.Split(answer, "\n\n") {
		lines := strings.Split(block, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[1], "https://") {
			return
		}
	}
	t.Fatal("Brave returned no HTTPS result block")
}
