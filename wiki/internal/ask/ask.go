// Package ask will answer questions from retrieved wiki context.
package ask

// Answer is a generated answer and its cited source ids.
type Answer struct {
	Text    string
	Sources []string
}
