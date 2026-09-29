package browse

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type renderLine struct {
	text        string
	tree        bool
	highlighted bool
}

type screenView struct {
	breadcrumb string
	body       []renderLine
	hint       string
}

func renderScreen(view screenView, cols, rows int) []byte {
	if cols < MinCols || rows < MinRows {
		return []byte("\x1b[H\x1b[2Jterminal too small")
	}
	var out strings.Builder
	for row := 1; row <= rows; row++ {
		fmt.Fprintf(&out, "\x1b[%d;1H\x1b[2K", row)
		line := renderLine{}
		switch {
		case row == 1:
			line.text = view.breadcrumb
		case row == rows:
			line.text = view.hint
		case row >= 3 && row-3 < len(view.body):
			line = view.body[row-3]
		}
		if !line.tree {
			line.text = shownLine(line.text)
		}
		text := clippedLine(line.text, cols)
		if line.highlighted {
			out.WriteString("\x1b[7m")
			for i := 0; i < len(text); {
				if n := sgrLength(text[i:]); n > 0 {
					out.WriteString(text[i : i+n])
					out.WriteString("\x1b[7m")
					i += n
				} else {
					_, n := utf8.DecodeRuneInString(text[i:])
					out.WriteString(text[i : i+n])
					i += n
				}
			}
			out.WriteString("\x1b[0m")
		} else {
			out.WriteString(text)
		}
	}
	return []byte(out.String())
}

func sgrLength(s string) int {
	if len(s) < 3 || s[0] != '\x1b' || s[1] != '[' {
		return 0
	}
	i := 2
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == ';') {
		i++
	}
	if i < len(s) && s[i] == 'm' {
		return i + 1
	}
	return 0
}

func clippedLine(s string, width int) string {
	// Locate the last kept non-space rune; SGR bytes survive even beyond it.
	visible, last := 0, -1
	for i := 0; i < len(s); {
		if n := sgrLength(s[i:]); n > 0 {
			i += n
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if visible < width && r != ' ' {
			last = visible
		}
		visible++
		i += n
	}
	var out strings.Builder
	visible = 0
	for i := 0; i < len(s); {
		if n := sgrLength(s[i:]); n > 0 {
			out.WriteString(s[i : i+n])
			i += n
			continue
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		if visible <= last {
			out.WriteString(s[i : i+n])
		}
		visible++
		i += n
	}
	return out.String()
}

func shownLine(s string) string {
	var out strings.Builder
	columns := 0
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&out, "\\x%02x", s[0])
			columns += 4
		case r == '\t':
			spaces := 8 - columns%8
			out.WriteString(strings.Repeat(" ", spaces))
			columns += spaces
		case unicode.IsControl(r):
			fmt.Fprintf(&out, "\\x%02x", r)
			columns += 4
		default:
			out.WriteString(s[:n])
			columns++
		}
		s = s[n:]
	}
	return out.String()
}

func wrapLine(s string, width int) []string {
	runes := []rune(shownLine(s))
	if len(runes) == 0 {
		return []string{""}
	}
	var rows []string
	for start := 0; start < len(runes); start += width {
		rows = append(rows, string(runes[start:min(start+width, len(runes))]))
	}
	return rows
}
