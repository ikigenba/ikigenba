// Package compile builds durable wiki pages from canonical subjects and claims.
package compile

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"wiki/internal/llm"
	"wiki/internal/model"
)

// PageCharCap is the maximum generated page body length in Unicode code points.
const PageCharCap = 12000

const defaultMaxTokens = 16384

var (
	bracketedULID          = regexp.MustCompile(`\[[0-9A-HJKMNP-TV-Z]{26}\]`)
	repeatedSpace          = regexp.MustCompile(`[ \t]{2,}`)
	spaceBeforePunctuation = regexp.MustCompile(`[ \t]+([,.;:!?])`)
)

// DefaultPromptInstructions is the production compile instruction preamble.
//
//go:embed prompt.txt
var DefaultPromptInstructions string

// Compiler rebuilds wiki pages from subject identity and complete claim sets.
type Compiler struct {
	c          *llm.Client
	site       llm.CallSite
	maxTighten int
	log        *slog.Logger
}

// New builds a Compiler from an injected LLM client and compile call site.
func New(c *llm.Client, site llm.CallSite, log *slog.Logger) *Compiler {
	return &Compiler{c: c, site: site, maxTighten: 2, log: log}
}

// DefaultCallSite returns the production compile-stage generation settings.
func DefaultCallSite() llm.CallSite {
	return llm.CallSite{
		Stage:  "compile",
		System: DefaultPromptInstructions,
		Config: llm.Config{
			Provider:  "openai",
			Model:     "gpt-5.6-luna",
			Effort:    "low",
			MaxTokens: defaultMaxTokens,
		},
	}
}

// Compile rebuilds one subject's page from its complete claim set.
func (c *Compiler) Compile(ctx context.Context, attr llm.Attribution, s model.Subject, claims []model.Claim) (title, body string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("compile: nil compiler")
	}

	maxTighten := c.maxTighten
	if maxTighten < 0 {
		maxTighten = 0
	}

	prompt := renderPrompt(s, claims, PageCharCap, "")
	var last compileResponse
	for attempt := 0; attempt <= maxTighten; attempt++ {
		out, err := llm.JSON[compileResponse](ctx, c.c, c.site, attr, prompt, validateResponse)
		if err != nil {
			return "", "", err
		}

		out.Title = strings.TrimSpace(out.Title)
		out.Body = sanitizeBody(out.Body)
		last = out
		if utf8.RuneCountInString(out.Body) <= PageCharCap {
			return out.Title, out.Body, nil
		}

		prompt = renderPrompt(s, claims, PageCharCap, fmt.Sprintf(
			"The previous page is %d chars; hard limit %d — compress lower-salience claims and keep the lead.",
			utf8.RuneCountInString(out.Body), PageCharCap,
		))
	}

	if c.log != nil {
		c.log.WarnContext(ctx, "compile body exceeded page character cap after tightening",
			"subject_id", s.ID,
			"body_chars", utf8.RuneCountInString(last.Body),
			"cap", PageCharCap,
		)
	}
	return last.Title, truncateRunes(last.Body, PageCharCap), nil
}

type compileResponse struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func validateResponse(r *compileResponse) error {
	if r == nil {
		return fmt.Errorf("response required")
	}
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("title required")
	}
	if strings.TrimSpace(r.Body) == "" {
		return fmt.Errorf("body required")
	}
	return nil
}

func renderPrompt(s model.Subject, claims []model.Claim, cap int, tighten string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hard body limit: %d characters.\n", cap)
	if strings.TrimSpace(tighten) != "" {
		b.WriteString(tighten)
		b.WriteByte('\n')
	}

	b.WriteString("\nSubject identity:\n")
	writePromptLine(&b, "name", s.Name)
	writePromptLine(&b, "type", s.Type)

	b.WriteString("\nComplete claim texts:\n")
	if len(claims) == 0 {
		b.WriteString("- none\n")
		return b.String()
	}
	for i, claim := range claims {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(claim.Body))
	}
	return b.String()
}

func sanitizeBody(body string) string {
	body = bracketedULID.ReplaceAllString(body, "")
	body = repeatedSpace.ReplaceAllString(body, " ")
	body = spaceBeforePunctuation.ReplaceAllString(body, "$1")
	return strings.TrimSpace(body)
}

func writePromptLine(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteByte('\n')
}

func truncateRunes(s string, cap int) string {
	if cap < 0 {
		cap = 0
	}
	if utf8.RuneCountInString(s) <= cap {
		return s
	}
	runes := []rune(s)
	return string(runes[:cap])
}
