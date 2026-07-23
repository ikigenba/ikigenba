package ask

import (
	_ "embed"
)

// DefaultAnalysisInstructions is the production question-analysis instruction preamble.
//
//go:embed analysis-prompt.txt
var DefaultAnalysisInstructions string

// DefaultSynthesisInstructions is the production answer-synthesis instruction preamble.
//
//go:embed synthesis-prompt.txt
var DefaultSynthesisInstructions string

// RenderAnalysis returns the question-only user turn used by production analysis.
func RenderAnalysis(_ string, question string) string {
	return question
}
