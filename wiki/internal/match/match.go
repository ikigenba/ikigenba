// Package match judges which standing statements are overruled by corrections.
package match

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"wiki/internal/llm"
	"wiki/internal/model"
)

const defaultMaxTokens = 16384

// Judgment is one correction's coverage, by zero-based rendered-list index.
type Judgment struct {
	Correction  int   `json:"correction"`
	Refutes     []int `json:"refutes"`
	Contradicts []int `json:"contradicts"`
}

// Result is the model's complete set of coverage judgments.
type Result struct {
	Judgments []Judgment `json:"judgments"`
}

// Statement is a durable statement and whether it was introduced by this job.
type Statement struct {
	Claim model.Claim
	New   bool
}

// Unit is the bounded set of statements for one subject.
type Unit struct {
	Subject     model.Subject
	Corrections []Statement
	Claims      []Statement
}

// Edge is a mechanically valid suppression ready to stage.
type Edge struct {
	Correction string
	Suppressed string
}

// Matcher runs the match-stage LLM call.
type Matcher struct {
	c    *llm.Client
	site llm.CallSite
}

// DefaultPromptInstructions is the production match instruction preamble.
//
//go:embed prompt.txt
var DefaultPromptInstructions string

// New builds a Matcher from an injected LLM client and match call site.
func New(c *llm.Client, site llm.CallSite) *Matcher {
	return &Matcher{c: c, site: site}
}

// DefaultCallSite returns the production match-stage generation settings.
func DefaultCallSite() llm.CallSite {
	return llm.CallSite{
		Stage:           "match",
		System:          DefaultPromptInstructions,
		Config:          llm.Config{Provider: "openrouter", Model: "gpt-5.6-luna", Effort: "medium", MaxTokens: defaultMaxTokens},
		MaxParseRetries: 2,
	}
}

// Match judges a unit and returns only mechanically valid new-pair edges.
func (m *Matcher) Match(ctx context.Context, attr llm.Attribution, unit Unit) ([]Edge, error) {
	if m == nil {
		return nil, fmt.Errorf("match: nil matcher")
	}
	result, err := llm.JSON[Result](ctx, m.c, m.site, attr, attr.GroupID+"/"+m.site.Stage, Render(unit), func(result *Result) error {
		if result == nil {
			return fmt.Errorf("response required")
		}
		return Validate(*result, len(unit.Corrections), len(unit.Claims))
	})
	if err != nil {
		return nil, err
	}
	return Filter(unit, result), nil
}

// Render assembles the subject identity and numbered statement lists.
func Render(unit Unit) string {
	var b strings.Builder
	b.WriteString("Subject identity:\n")
	b.WriteString("name: ")
	b.WriteString(unit.Subject.Name)
	b.WriteString("\ntype: ")
	b.WriteString(unit.Subject.Type)
	b.WriteString("\n\nCorrections:\n")
	writeStatements(&b, unit.Corrections)
	b.WriteString("\nClaims:\n")
	writeStatements(&b, unit.Claims)
	return b.String()
}

func writeStatements(b *strings.Builder, statements []Statement) {
	if len(statements) == 0 {
		b.WriteString("(none)\n")
		return
	}
	for i, statement := range statements {
		marker := "existing"
		if statement.New {
			marker = "new"
		}
		fmt.Fprintf(b, "%d. [%s] %s\n", i, marker, statement.Claim.Body)
	}
}

// Validate checks that every judgment index names a rendered statement.
func Validate(result Result, correctionCount, claimCount int) error {
	for i, judgment := range result.Judgments {
		if judgment.Correction < 0 || judgment.Correction >= correctionCount {
			return fmt.Errorf("judgments[%d].correction index %d out of range", i, judgment.Correction)
		}
		for j, index := range judgment.Refutes {
			if index < 0 || index >= claimCount {
				return fmt.Errorf("judgments[%d].refutes[%d] index %d out of range", i, j, index)
			}
		}
		for j, index := range judgment.Contradicts {
			if index < 0 || index >= correctionCount {
				return fmt.Errorf("judgments[%d].contradicts[%d] index %d out of range", i, j, index)
			}
		}
	}
	return nil
}

// Filter converts judgments to suppression edges while enforcing new-pair and
// correction-order invariants. Claim IDs are ULIDs, so lexical order is
// assertion order.
func Filter(unit Unit, result Result) []Edge {
	edges := make([]Edge, 0)
	seen := make(map[Edge]struct{})
	appendEdge := func(source, target Statement) {
		if !source.New && !target.New {
			return
		}
		edge := Edge{Correction: source.Claim.ID, Suppressed: target.Claim.ID}
		if _, ok := seen[edge]; ok {
			return
		}
		seen[edge] = struct{}{}
		edges = append(edges, edge)
	}
	for _, judgment := range result.Judgments {
		if judgment.Correction < 0 || judgment.Correction >= len(unit.Corrections) {
			continue
		}
		source := unit.Corrections[judgment.Correction]
		for _, index := range judgment.Refutes {
			if index >= 0 && index < len(unit.Claims) {
				appendEdge(source, unit.Claims[index])
			}
		}
		for _, index := range judgment.Contradicts {
			if index < 0 || index >= len(unit.Corrections) {
				continue
			}
			target := unit.Corrections[index]
			if source.Claim.JobID == target.Claim.JobID || source.Claim.ID <= target.Claim.ID {
				continue
			}
			appendEdge(source, target)
		}
	}
	return edges
}
