// Package asksite holds dependency-neutral defaults shared by ask and the
// service configuration package.
package asksite

import "wiki/internal/llm"

const (
	ModelID               = "gpt-5.6-luna"
	AnalysisInstructions  = "Prepare the wiki question for retrieval. Return only JSON with sub_queries, keywords, and aliases arrays. Split sub_queries by subject and return at most four. Use keywords for salient terms and aliases for alternate names.\n"
	SynthesisInstructions = "Answer only from the supplied wiki pages and return only JSON with found, text, and citations. Each citation must use the exact path and title of a supplied page. Return found=false when the pages do not answer the question. Never fabricate a fact, page, or citation. If supplied pages contradict each other, surface both sides and cite both pages. Do not modify the wiki.\n\nSchema: {\"found\":true|false,\"text\":\"answer\",\"citations\":[{\"path\":\"type/slug\",\"title\":\"Page title\"}]}\n\nExample: {\"found\":true,\"text\":\"Ada wrote the note.\",\"citations\":[{\"path\":\"entity/ada\",\"title\":\"Ada\"}]}\n"
	defaultMaxTokens      = 16384
)

// Subject returns the production analysis call-site defaults.
func Subject() llm.CallSite {
	return llm.CallSite{
		Stage:  "ask-subject",
		System: AnalysisInstructions,
		Config: llm.Config{Provider: "openai", Model: ModelID, Effort: "low", MaxTokens: defaultMaxTokens},
	}
}

// Synthesis returns the production answer-synthesis call-site defaults.
func Synthesis() llm.CallSite {
	return llm.CallSite{
		Stage:  "ask-synthesis",
		System: SynthesisInstructions,
		Config: llm.Config{Provider: "openai", Model: ModelID, Effort: "low", MaxTokens: defaultMaxTokens},
	}
}
