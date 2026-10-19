//go:build live

package toolkit

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestLiveWebFetch(t *testing.T) {
	// R-B6L0-J2I1
	tool, err := WebFetch()
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Call(context.Background(), json.RawMessage(`{"url":"https://example.com/"}`))
	if err != nil {
		t.Fatalf("fetch public example: %v", err)
	}
	header := "URL: https://example.com/\nStatus: 200\nType: text/html\n\n"
	if !strings.HasPrefix(result, header) {
		t.Fatal("missing expected response header")
	}
	content := strings.TrimPrefix(result, header)
	if strings.TrimSpace(content) == "" || regexp.MustCompile(`<\/?[a-zA-Z][^>]*>`).MatchString(content) {
		t.Fatal("expected nonempty Markdown without HTML tags")
	}
}
