// Package quote escapes untrusted text for diagnostics and table cells.
package quote

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Arg escapes an argument for display inside single quotes.
func Arg(s string) string { return escape(s, true) }

// Field escapes a field without surrounding quotes.
func Field(s string) string { return escape(s, false) }

func escape(s string, escapeQuote bool) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && width == 1 {
			out.WriteString("\\x")
			writeHex(&out, rune(s[i]), 2)
			i++
			continue
		}
		switch r {
		case '\\':
			out.WriteString("\\\\")
		case '\'':
			if escapeQuote {
				out.WriteString("\\'")
			} else {
				out.WriteByte('\'')
			}
		case '\n':
			out.WriteString("\\n")
		case '\t':
			out.WriteString("\\t")
		case '\r':
			out.WriteString("\\r")
		default:
			switch {
			case r < 0x20 || r == 0x7f:
				out.WriteString("\\x")
				writeHex(&out, r, 2)
			case r >= 0x80 && !unicode.IsPrint(r):
				if r <= 0xffff {
					out.WriteString("\\u")
					writeHex(&out, r, 4)
				} else {
					out.WriteString("\\U")
					writeHex(&out, r, 8)
				}
			default:
				out.WriteString(s[i : i+width])
			}
		}
		i += width
	}
	return out.String()
}

func writeHex(out *strings.Builder, n rune, digits int) {
	const hex = "0123456789abcdef"
	for shift := (digits - 1) * 4; shift >= 0; shift -= 4 {
		out.WriteByte(hex[(n>>shift)&0xf])
	}
}
