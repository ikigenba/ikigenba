package chat

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFormatSignature(t *testing.T) {
	// R-KYEW-ZRM5
	if reflect.TypeOf(Format) != reflect.TypeOf((func(Entry) string)(nil)) {
		t.Fatal("Format signature differs")
	}
}

func TestTotalsLineSignature(t *testing.T) {
	// R-KZMT-DJCU
	if reflect.TypeOf(TotalsLine) != reflect.TypeOf((func(Usage, Recorded) string)(nil)) {
		t.Fatal("TotalsLine signature differs")
	}
}

func TestTextEscape(t *testing.T) {
	// R-L22M-52U8
	cases := []struct{ in, want string }{
		{"a\n\tb", "a\n\tb"},
		{"a\rb", "a\\x0db"},
		{"\x1b[31m\x7f\u009b", "\\x1b[31m\\x7f\\x9b"},
		{"C:\\dir it's é", "C:\\dir it's é"},
		{string([]byte{0xff}), "\\xff"},
	}
	for _, tc := range cases {
		if got := textEscape(tc.in); got != tc.want {
			t.Errorf("textEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNameEscape(t *testing.T) {
	// R-L3AI-IUKX
	cases := []struct{ in, want string }{{"Bash", "Bash"}, {"a\nb\t", "a\\x0ab\\x09"}, {"\x1b", "\\x1b"}}
	for _, tc := range cases {
		got := nameEscape(tc.in)
		if got != tc.want || strings.ContainsRune(got, '\n') {
			t.Errorf("nameEscape(%q) = %q", tc.in, got)
		}
	}
}

func TestArgumentLine(t *testing.T) {
	// R-L5QB-AE2B
	cases := []struct{ in, want string }{
		{"{ \"cmd\": \"ls\",\n  \"n\": 1 }", `{"cmd":"ls","n":1}`},
		{`{"content":"a\nb"}`, `{"content":"a\nb"}`},
		{`{"q":"<a&b>"}`, `{"q":"<a&b>"}`},
		{"not\njson", `"not\njson"`},
	}
	for _, tc := range cases {
		if got := argumentLine(tc.in); got != tc.want {
			t.Errorf("argumentLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCutArgumentLine(t *testing.T) {
	// R-L6Y7-O5T0
	for _, tc := range []struct {
		n       int
		wantCut bool
	}{{198, false}, {199, true}} {
		input := strings.Repeat("é", tc.n)
		got := cutArgumentLine(input) // Invalid JSON becomes a quoted JSON string.
		want := `"` + strings.Repeat("é", tc.n)
		if tc.wantCut {
			want = `"` + strings.Repeat("é", 199) + "…"
		} else {
			want += `"`
		}
		if got != want {
			t.Errorf("cutArgumentLine length %d = %q", tc.n, got)
		}
	}
}

func TestHeader(t *testing.T) {
	// R-L864-1XJP
	moment := time.Date(2026, 9, 24, 14, 58, 3, 900000000, time.FixedZone("west", -5*3600))
	if got := header(Entry{Time: moment, HasTime: true, Kind: KindTool, Tool: "a\nb"}); got != "2026-09-24T19:58:03Z tool a\\x0ab" {
		t.Errorf("timed tool header = %q", got)
	}
	if got := header(Entry{Time: moment, Kind: KindUser}); got != "- user" {
		t.Errorf("untimed header = %q", got)
	}
	for _, kind := range []Kind{KindAssistant, KindReasoning, KindAgent, KindResultOK, KindResultError} {
		if got := header(Entry{Kind: kind}); got != "- "+string(kind) {
			t.Errorf("header kind %q = %q", kind, got)
		}
	}
}

func TestBody(t *testing.T) {
	// R-L9E0-FPAE
	for _, kind := range []Kind{KindUser, KindAssistant, KindReasoning, KindAgent} {
		if got := body(Entry{Kind: kind, Text: "a\rb"}); got != "a\\x0db\n" {
			t.Errorf("body kind %q = %q", kind, got)
		}
	}
	if got := body(Entry{Kind: KindTool, Text: `{ "cmd": "ls" }`}); got != `{"cmd":"ls"}`+"\n" {
		t.Errorf("tool body = %q", got)
	}
	for _, kind := range []Kind{KindResultOK, KindResultError} {
		if got := body(Entry{Kind: kind, Text: "ignored"}); got != "" {
			t.Errorf("result body = %q", got)
		}
	}
	if got := body(Entry{Kind: KindUser}); got != "" {
		t.Errorf("empty body = %q", got)
	}
}

func TestFormat(t *testing.T) {
	// R-LALW-TH13
	moment := time.Date(2026, 9, 24, 19, 58, 3, 412000000, time.UTC)
	cases := []struct {
		entry Entry
		want  string
	}{
		{Entry{Time: moment, HasTime: true, Kind: KindUser, Text: "commit all files"}, "2026-09-24T19:58:03Z user\ncommit all files\n\n"},
		{Entry{Time: moment.Add(3 * time.Second), HasTime: true, Kind: KindTool, Tool: "Bash", Text: `{"command":"git status --short"}`}, "2026-09-24T19:58:06Z tool Bash\n{\"command\":\"git status --short\"}\n\n"},
		{Entry{Time: moment.Add(4 * time.Second), HasTime: true, Kind: KindResultOK, Text: "ignored"}, "2026-09-24T19:58:07Z result ok\n\n"},
		{Entry{Kind: Kind("unknown")}, ""},
	}
	for _, tc := range cases {
		if got := Format(tc.entry); got != tc.want {
			t.Errorf("Format(%+v) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}

func TestTotalsLine(t *testing.T) {
	// R-LBTT-78RS
	u := Usage{In: 5, CacheWrite: 5391, CacheRead: 46131, Out: 201, Reasoning: 30, Calls: 3}
	r := Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}
	if got := TotalsLine(u, r); got != "tokens: in 5  cache-write 5391  cache-read 46131  out 201  reasoning 30  calls 3\n" {
		t.Errorf("totals = %q", got)
	}
	if got := TotalsLine(Usage{}, r); got != "tokens: in 0  cache-write 0  cache-read 0  out 0  reasoning 0  calls 0\n" {
		t.Errorf("zero totals = %q", got)
	}
	r.Reasoning = false
	if got := TotalsLine(Usage{}, r); got != "tokens: in 0  cache-write 0  cache-read 0  out 0  reasoning -  calls 0\n" {
		t.Errorf("unrecorded reasoning = %q", got)
	}
}
