package wire

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestR_RYQG_4XS2_UTF8Encoding verifies wire output is valid UTF-8 and
// that the unescaped and \uXXXX-escaped forms of the same string parse
// identically on the decode side.
//
// R-RYQG-4XS2: encoding is UTF-8. Strings containing non-ASCII characters
// are emitted as valid UTF-8, not escaped to \uXXXX (escaping is permitted
// but not required; both must parse identically on ralph-loops' side).
func TestR_RYQG_4XS2_UTF8Encoding(t *testing.T) {
	const sample = "café 中文 🎉"

	// Encode a user event carrying the non-ASCII text.
	var buf bytes.Buffer
	if err := Encode(&buf, NewUserEvent(NewTextBlock(sample))); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := buf.Bytes()

	if !utf8.Valid(out) {
		t.Fatalf("encoded line is not valid UTF-8: %q", out)
	}
	if !bytes.Contains(out, []byte(sample)) {
		t.Fatalf("encoded line does not contain raw UTF-8 bytes of %q; got %q", sample, out)
	}
	if bytes.Contains(out, []byte(`\u`)) {
		t.Fatalf("encoded line escaped non-ASCII to \\uXXXX; got %q", out)
	}

	// Both the raw UTF-8 form and the fully \u-escaped form must parse to
	// the same string on the receiving side.
	rawLine := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"` + sample + `"}]}}`)
	escapedLine := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"` + escapeToUnicode(sample) + `"}]}}`)

	rawEvt, err := ParseStdinUserEvent(rawLine)
	if err != nil {
		t.Fatalf("ParseStdinUserEvent(raw): %v", err)
	}
	escEvt, err := ParseStdinUserEvent(escapedLine)
	if err != nil {
		t.Fatalf("ParseStdinUserEvent(escaped): %v", err)
	}

	rawText := rawEvt.Message.Content[0].(TextBlock).Text
	escText := escEvt.Message.Content[0].(TextBlock).Text
	if rawText != escText {
		t.Fatalf("raw vs escaped decoded differently: raw=%q escaped=%q", rawText, escText)
	}
	if rawText != sample {
		t.Fatalf("decoded text = %q, want %q", rawText, sample)
	}
}

// escapeToUnicode rewrites every non-ASCII rune as a JSON \uXXXX escape
// (using surrogate pairs for runes outside the BMP). ASCII is left alone.
func escapeToUnicode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		if r <= 0xFFFF {
			b.WriteString(jsonHex4(uint16(r)))
			continue
		}
		// Surrogate pair for runes > U+FFFF.
		r -= 0x10000
		hi := 0xD800 + uint16(r>>10)
		lo := 0xDC00 + uint16(r&0x3FF)
		b.WriteString(jsonHex4(hi))
		b.WriteString(jsonHex4(lo))
	}
	return b.String()
}

func jsonHex4(v uint16) string {
	const hex = "0123456789abcdef"
	return `\u` + string([]byte{
		hex[(v>>12)&0xF],
		hex[(v>>8)&0xF],
		hex[(v>>4)&0xF],
		hex[v&0xF],
	})
}
