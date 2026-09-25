// Package chat defines transcript entries, usage, and their printed form.
package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func textEscape(s string) string {
	return escape(s, false)
}

func nameEscape(s string) string {
	return escape(s, true)
}

func escape(s string, name bool) string {
	var b strings.Builder
	for len(s) > 0 {
		r, width := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && width == 1:
			fmt.Fprintf(&b, "\\x%02x", s[0])
		case (r == '\n' || r == '\t') && !name:
			b.WriteString(s[:width])
		case unicode.IsControl(r):
			fmt.Fprintf(&b, "\\x%02x", r)
		default:
			b.WriteString(s[:width])
		}
		s = s[width:]
	}
	return b.String()
}

func argumentLine(s string) string {
	var b bytes.Buffer
	if json.Valid([]byte(s)) {
		_ = json.Compact(&b, []byte(s))
		return b.String()
	}
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func cutArgumentLine(s string) string {
	a := argumentLine(s)
	if utf8.RuneCountInString(a) <= 200 {
		return a
	}
	runes := []rune(a)
	return string(runes[:200]) + "…"
}

func header(e Entry) string {
	timePart := "-"
	if e.HasTime {
		timePart = e.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	kindPart := string(e.Kind)
	if e.Kind == KindTool {
		kindPart += " " + nameEscape(e.Tool)
	}
	return timePart + " " + kindPart
}

func body(e Entry) string {
	if e.Text == "" {
		return ""
	}
	switch e.Kind {
	case KindUser, KindAssistant, KindReasoning, KindAgent:
		return textEscape(e.Text) + "\n"
	case KindTool:
		return textEscape(cutArgumentLine(e.Text)) + "\n"
	default:
		return ""
	}
}

// Format prints one entry as a header, body, and blank line.
func Format(e Entry) string {
	switch e.Kind {
	case KindUser, KindAssistant, KindReasoning, KindAgent, KindTool, KindResultOK, KindResultError:
		return header(e) + "\n" + body(e) + "\n"
	default:
		return ""
	}
}

// TotalsLine prints the available usage counts on one line.
func TotalsLine(u Usage, r Recorded) string {
	v := func(n int64, rec bool) string {
		if !rec {
			return "-"
		}
		return strconv.FormatInt(n, 10)
	}
	return "tokens: in " + v(u.In, r.In) +
		"  cache-write " + v(u.CacheWrite, r.CacheWrite) +
		"  cache-read " + v(u.CacheRead, r.CacheRead) +
		"  out " + v(u.Out, r.Out) +
		"  reasoning " + v(u.Reasoning, r.Reasoning) +
		"  calls " + v(u.Calls, r.Calls) + "\n"
}
