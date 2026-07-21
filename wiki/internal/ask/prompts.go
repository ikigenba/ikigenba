package ask

import (
	"strings"

	analysisprompt "wiki/eval/analysis"
)

// DefaultAnalysisInstructions is the production question-analysis instruction preamble.
var DefaultAnalysisInstructions = analysisprompt.Instructions

// RenderAnalysis assembles the exact prompt sent by the production analysis call.
func RenderAnalysis(instructions, question string) string {
	return instructions + "\n\nQuestion: " + strings.TrimSpace(question)
}
