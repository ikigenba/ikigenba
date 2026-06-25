package wiki

// QueryAnalysis is the prepared form of a question.
type QueryAnalysis struct {
	SubQueries []string `json:"sub_queries"`
	Keywords   []string `json:"keywords"`
	Aliases    []string `json:"aliases"`
}
