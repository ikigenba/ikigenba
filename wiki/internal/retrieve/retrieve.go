// Package retrieve will own wiki search and context retrieval.
package retrieve

// Result is one retrieved wiki passage.
type Result struct {
	ID    string
	Title string
	Text  string
}
