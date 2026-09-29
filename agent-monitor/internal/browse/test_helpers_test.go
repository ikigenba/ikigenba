package browse

import (
	"fmt"
	"strings"
)

type renderRunTerminal struct {
	cols, rows int
	keys       <-chan []byte
}

func (t *renderRunTerminal) Keys() <-chan []byte    { return t.keys }
func (t *renderRunTerminal) Size() (int, int)       { return t.cols, t.rows }
func (*renderRunTerminal) Resized() <-chan struct{} { return nil }

type renderScreenRecorder struct{ screens []string }

func (w *renderScreenRecorder) Write(p []byte) (int, error) {
	w.screens = append(w.screens, string(p))
	return len(p), nil
}

func renderExpectedRows(rows []string) string {
	var out strings.Builder
	for i, row := range rows {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[2K%s", i+1, row)
	}
	return out.String()
}
