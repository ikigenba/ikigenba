package mcp

import (
	"strings"
	"testing"

	"github.com/ikigenba/agentkit/catalog"
)

func TestDescribeContainsEveryCatalogChatModel(t *testing.T) {
	// R-222I-X6JH
	result, err := toolDescribe()
	if err != nil {
		t.Fatalf("toolDescribe: %v", err)
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("describe content = %#v, want one text item", result["content"])
	}
	text, ok := content[0]["text"].(string)
	if !ok || text == "" {
		t.Fatalf("describe text = %#v, want non-empty string", content[0]["text"])
	}

	seen := make(map[string]bool)
	openRouterModel := ""
	for _, provider := range catalogProviders {
		for _, entry := range catalog.ListByProvider(provider) {
			if entry.Pricing == nil {
				continue
			}
			seen[entry.Model] = true
			if provider == "openrouter" {
				openRouterModel = entry.Model
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("catalog provider lists contained no chat models")
	}
	for model := range seen {
		if !strings.Contains(text, model) {
			t.Errorf("describe text omitted catalog chat model %q", model)
		}
	}
	if openRouterModel == "" {
		t.Fatal("catalog contained no openrouter-routed chat model")
	}
	if !strings.Contains(text, openRouterModel) {
		t.Errorf("describe text omitted openrouter-routed model %q", openRouterModel)
	}
}
