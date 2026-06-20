//go:build integration

// The standing integration tier's first slice (extract half) — see "Integration
// testing" in docs/wiki-redesign-plan.md. It runs the REAL pinned (prompt, model,
// effort) triple against a blunt fixture document and asserts the output is
// STRUCTURALLY valid — never whether it is good (quality is Part II's graded
// sweep). It is build-tag gated (`-tags=integration`) so it is always in the tree
// but never in the unit gate, which must stay deterministic, free, and offline.
//
// This is also P6a's phase-owned checkpoint (the first of three): extract is the
// first live LLM site. A red run pauses the march for investigation (advisory,
// not a deploy gate). With no key/network it emits the visible
// `INTEGRATION CHECKPOINT SKIPPED — no keys` line and skips — never passing as if
// it ran.
package integrate

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"agentkit/model"
	"agentkit/provider/anthropic"
	"agentkit/provider/openai"

	"wiki/internal/config"
	"wiki/internal/llm"
)

func liveFactory() llm.ClientFactory {
	return func(r model.Resolved) (llm.Client, error) {
		switch r.Provider {
		case model.ProviderOpenAI:
			return openai.New(os.Getenv("OPENAI_API_KEY"), r.BareID)
		default:
			return anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), r.BareID)
		}
	}
}

func TestExtractIntegration(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("OPENAI_API_KEY") == "" {
		t.Log("INTEGRATION CHECKPOINT SKIPPED — no keys")
		t.Skip("no provider keys present")
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	site := cfg.LLM.Extract
	// Pinned live triple uses the config-default prompt unless overridden.
	if site.Prompt == "" {
		site.Prompt = config.DefaultExtractPrompt
	}

	w := llm.New(liveFactory(), nil)
	ex := NewExtractor(NewWrapperCaller(w), site)

	hdr := DocumentHeader{
		Source:     "test",
		Title:      "Blunt extract fixture",
		ReceivedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	// A blunt fixture: a single obvious entity with one concrete claim. Even a
	// weak model should extract it; the assertion is structural, not quality.
	doc := "Tim Cook is the chief executive officer of Apple Inc."

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	subs, err := ex.Extract(ctx, hdr, doc, "01HXBLUNTFIXTUREINBOXROW001")
	if err != nil {
		t.Fatalf("live extract failed (checkpoint RED — investigate): %v", err)
	}
	if len(subs) == 0 {
		t.Fatal("live extract returned no subjects for a blunt fixture (checkpoint RED)")
	}
	for _, s := range subs {
		switch s.Type {
		case TypeEntity, TypeEvent, TypeConcept:
		default:
			t.Errorf("subject %q has invalid type %q", s.Name, s.Type)
		}
		if strings.TrimSpace(s.Name) == "" {
			t.Error("subject with empty name")
		}
		if len(s.Claims) == 0 {
			t.Errorf("subject %q has no claims", s.Name)
		}
		for _, c := range s.Claims {
			if len(c.Cites) != 1 {
				t.Errorf("claim not cited to the single inbox row: %+v", c)
			}
			// Structural claim hygiene: no obvious document-relative refs / pronouns.
			low := strings.ToLower(c.Text)
			for _, bad := range []string{" he ", " she ", " it ", " they "} {
				if strings.Contains(" "+low+" ", bad) {
					t.Errorf("claim contains a pronoun (%q): %q", strings.TrimSpace(bad), c.Text)
				}
			}
		}
	}
}
