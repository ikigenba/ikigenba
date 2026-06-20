// Package extract will turn ingested sources into normalized wiki text.
package extract

// Result is extracted source text plus a stable content type.
type Result struct {
	Text        string
	ContentType string
}
