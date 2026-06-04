// Package wire encodes and decodes the newline-delimited JSON event
// stream that ikigai-cli exchanges with ralph-loops over stdin/stdout.
package wire

import (
	"bytes"
	"encoding/json"
	"io"
)

// Encode writes a single event to w as one NDJSON line: a compact JSON
// object followed by a single '\n'. The encoded object contains no
// embedded newlines (any '\n' inside a string value is escaped as
// "\\n" by encoding/json), and no leading or trailing whitespace
// inside the line.
//
// R-RCS9-92FK: each line is exactly one complete JSON object,
// newline-terminated, with no pretty-printing.
func Encode(w io.Writer, event any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(event); err != nil {
		return err
	}
	// json.Encoder.Encode already appends a single '\n'. Write it
	// to w in one call so events aren't split across writes.
	_, err := w.Write(buf.Bytes())
	return err
}
