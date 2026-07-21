// Package extract turns ingested source text into subjects and claims.
package extract

import (
	"context"
	"fmt"
	"strings"
	"time"

	extractprompt "wiki/eval/extract"
	"wiki/internal/llm"
)

// ExtractedSubject is one subject with short, self-contained claims from source text.
type ExtractedSubject struct {
	Type       string   `json:"type"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	OccurredAt string   `json:"occurred_at"`
	Claims     []string `json:"claims"`
}

// DocumentHeader anchors model extraction to explicit source metadata.
type DocumentHeader struct {
	Source     string
	Title      string
	Tags       []string
	ReceivedAt time.Time
}

// Extractor runs the extract-stage LLM call.
type Extractor struct {
	c    *llm.Client
	site llm.CallSite
}

const defaultMaxTokens = 16384

// DefaultPromptInstructions is the baked-in production extract instruction preamble.
var DefaultPromptInstructions = extractprompt.Instructions

// New builds an Extractor from an injected LLM client and extract call site.
func New(c *llm.Client, site llm.CallSite) *Extractor {
	return &Extractor{c: c, site: site}
}

// DefaultCallSite returns the production extract-stage generation settings.
func DefaultCallSite() llm.CallSite {
	temp := 0.0
	return llm.CallSite{
		Stage:           "extract",
		Config:          llm.Config{Temperature: &temp, Thinking: boolPtr(false), MaxTokens: defaultMaxTokens},
		Temperature:     &temp,
		Reasoning:       llm.DisableReasoning(),
		MaxTokens:       defaultMaxTokens,
		MaxParseRetries: 2,
	}
}

// Extract extracts subjects and claims from source text.
func (e *Extractor) Extract(ctx context.Context, attr llm.Attribution, h DocumentHeader, text string) ([]ExtractedSubject, error) {
	if e == nil {
		return nil, fmt.Errorf("extract: nil extractor")
	}
	out, err := llm.JSON[extractResponse](ctx, e.c, e.site, attr, Render(DefaultPromptInstructions, h, text), func(response *extractResponse) error {
		if response == nil {
			return validateResponse(nil)
		}
		return Validate(response.Subjects)
	})
	if err != nil {
		return nil, err
	}
	return out.Subjects, nil
}

func boolPtr(value bool) *bool { return &value }

type extractResponse struct {
	Subjects []ExtractedSubject `json:"subjects"`
}

func renderPrompt(instructions string, h DocumentHeader, text string) string {
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n\n")
	b.WriteString("Document header:\n")
	writeHeaderLine(&b, "source", h.Source)
	writeHeaderLine(&b, "title", h.Title)
	writeHeaderLine(&b, "tags", strings.Join(h.Tags, ", "))
	writeHeaderLine(&b, "received on", h.ReceivedAt.Format("2006-01-02"))
	b.WriteString("\nSource text:\n")
	b.WriteString(text)
	return b.String()
}

// Render assembles the exact prompt used by the production extraction path.
func Render(instructions string, h DocumentHeader, text string) string {
	return renderPrompt(instructions, h, text)
}

// Validate applies the production extraction response rules to subjects.
func Validate(subjects []ExtractedSubject) error {
	return validateResponse(&extractResponse{Subjects: subjects})
}

func writeHeaderLine(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteByte('\n')
}

func validateResponse(r *extractResponse) error {
	if r == nil {
		return fmt.Errorf("response required")
	}
	for i := range r.Subjects {
		if err := validateSubject(i, r.Subjects[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateSubject(i int, s ExtractedSubject) error {
	switch s.Type {
	case "entity", "event", "concept":
	default:
		return fmt.Errorf("subjects[%d].type must be entity, event, or concept", i)
	}
	if strings.TrimSpace(s.Kind) == "" {
		return fmt.Errorf("subjects[%d].kind required", i)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("subjects[%d].name required", i)
	}
	if s.OccurredAt == "" {
		if s.Type == "event" {
			return fmt.Errorf("subjects[%d].occurred_at required for events", i)
		}
	} else if !isISOPrefix(s.OccurredAt) {
		return fmt.Errorf("subjects[%d].occurred_at must be an ISO-8601 prefix", i)
	}
	if len(s.Claims) == 0 {
		return fmt.Errorf("subjects[%d].claims required", i)
	}
	for j, claim := range s.Claims {
		if strings.TrimSpace(claim) == "" {
			return fmt.Errorf("subjects[%d].claims[%d] required", i, j)
		}
	}
	return nil
}

func isISOPrefix(v string) bool {
	switch len(v) {
	case len("2006"):
		_, err := time.Parse("2006", v)
		return err == nil
	case len("2006-01"):
		_, err := time.Parse("2006-01", v)
		return err == nil
	case len("2006-01-02"):
		_, err := time.Parse("2006-01-02", v)
		return err == nil
	default:
		return false
	}
}
