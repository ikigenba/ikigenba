package browse

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// R-249K-G086
func TestTooSmallScreens(t *testing.T) {
	for _, size := range [][2]int{{39, 40}, {120, 4}, {0, 0}, {-1, 10}, {40, 4}} {
		view := screenView{breadcrumb: "breadcrumb", body: []renderLine{{text: "body", highlighted: true}}, hint: "hint"}
		want := "\x1b[H\x1b[2Jterminal too small"
		if got := string(renderScreen(view, size[0], size[1])); got != want {
			t.Errorf("size %v = %q, want %q", size, got, want)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		writer := &renderScreenRecorder{}
		if err := Run(ctx, Config{Home: "/home/dev", Root: fstest.MapFS{}, Terminal: &renderRunTerminal{cols: size[0], rows: size[1]}}, writer); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(writer.screens, []string{want}) {
			t.Errorf("Run size %v screens = %q", size, writer.screens)
		}
	}
}

// R-WUJ1-10CK
// R-X0MI-XV21
func TestNormalScreenRows(t *testing.T) {
	for _, height := range []int{5, 6, 9} {
		for _, count := range []int{0, 1, height - 3} {
			view := screenView{breadcrumb: "path", hint: "keys"}
			want := make([]string, height)
			want[0], want[height-1] = "path", "keys"
			for i := range count {
				text := fmt.Sprintf("body %d", i)
				view.body = append(view.body, renderLine{text: text})
				want[i+2] = text
			}
			if got := string(renderScreen(view, 40, height)); got != renderExpectedRows(want) {
				t.Errorf("height %d count %d = %q, want %q", height, count, got, renderExpectedRows(want))
			}
		}
	}
}

// R-WVQX-ES39
func TestHighlightedRowBytes(t *testing.T) {
	for _, tt := range []struct {
		text, want string
		tree       bool
	}{
		{"Claude Code  2", "\x1b[7mClaude Code  2\x1b[0m", false},
		{"\x1b[36m●\x1b[0m [a] x", "\x1b[7m\x1b[36m\x1b[7m●\x1b[0m\x1b[7m [a] x\x1b[0m", true},
		{"\x1b[mx\x1b[1;32m y  ", "\x1b[7m\x1b[m\x1b[7mx\x1b[1;32m\x1b[7m y\x1b[0m", true},
	} {
		view := screenView{body: []renderLine{{text: tt.text, tree: tt.tree, highlighted: true}}}
		want := renderExpectedRows([]string{"", "", tt.want, "", ""})
		if got := string(renderScreen(view, 40, 5)); got != want {
			t.Errorf("highlight %q = %q, want %q", tt.text, got, want)
		}
		view.body[0].highlighted = false
		text := tt.text
		if !tt.tree {
			text = shownLine(text)
		}
		want = renderExpectedRows([]string{"", "", clippedLine(text, 40), "", ""})
		if got := string(renderScreen(view, 40, 5)); got != want {
			t.Errorf("unhighlighted %q = %q", tt.text, got)
		}
	}
}

// R-WY6Q-6BKN
func TestRowClippingPreservesSGR(t *testing.T) {
	for _, tt := range []struct {
		text  string
		width int
		want  string
	}{
		{strings.Repeat("界", 50), 40, strings.Repeat("界", 40)},
		{"ab  ", 40, "ab"}, {"\x1b[36m●\x1b[0m [abc]", 4, "\x1b[36m●\x1b[0m [a"},
		{"x\x1b[36m●\x1b[0m", 1, "x\x1b[36m\x1b[0m"},
		{"a b", 2, "a"}, {"unchanged", 40, "unchanged"},
		{"a \x1b[m \x1b[0m", 40, "a\x1b[m\x1b[0m"},
		{" \x1b[;m ", 1, "\x1b[;m"}, {"a\x1b[31mb\x1b[0mc", 1, "a\x1b[31m\x1b[0m"},
	} {
		if got := clippedLine(tt.text, tt.width); got != tt.want {
			t.Errorf("clip(%q,%d) = %q, want %q", tt.text, tt.width, got, tt.want)
		}
	}
	text := strings.Repeat("x", 45) + "\x1b[0m"
	want := renderExpectedRows([]string{strings.Repeat("b", 40), "", strings.Repeat("x", 40) + "\x1b[0m", "", strings.Repeat("h", 40)})
	view := screenView{breadcrumb: strings.Repeat("b", 45), body: []renderLine{{text: text, tree: true}}, hint: strings.Repeat("h", 45)}
	if got := string(renderScreen(view, 40, 5)); got != want {
		t.Errorf("screen clipping = %q, want %q", got, want)
	}
}

// R-WZEM-K3BC
func TestShownForm(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{"a\tb", "a       b"}, {"\x1b[31m", "\\x1b[31m"}, {"\u009b", "\\x9b"}, {"\xff", "\\xff"},
		{"\xff\tb", "\\xff    b"}, {"é\tb", "é       b"}, {"\x00\x7f\r\n", "\\x00\\x7f\\x0d\\x0a"},
		{"\xc3(\xed\xa0\x80", "\\xc3(\\xed\\xa0\\x80"}, {"\ufffd", "\ufffd"}, {"a\t\tb", "a               b"},
	} {
		if got := shownLine(tt.raw); got != tt.want {
			t.Errorf("shown(%q) = %q, want %q", tt.raw, got, tt.want)
		}
		if got := shownLine(tt.want); got != tt.want {
			t.Errorf("shown output not idempotent: %q", got)
		}
		view := screenView{breadcrumb: tt.raw, body: []renderLine{{text: tt.raw}}, hint: tt.raw}
		want := renderExpectedRows([]string{tt.want, "", tt.want, "", tt.want})
		if got := string(renderScreen(view, 120, 5)); got != want {
			t.Errorf("shown screen = %q, want %q", got, want)
		}
	}
	view := screenView{body: []renderLine{{text: "\x1b[36m●\x1b[0m", tree: true}}}
	if got := string(renderScreen(view, 40, 5)); got != renderExpectedRows([]string{"", "", "\x1b[36m●\x1b[0m", "", ""}) {
		t.Errorf("tree shown form = %q", got)
	}
}

// R-X1UF-BMSQ
func TestWrappedRows(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		width int
		want  []string
	}{
		{"", 4, []string{""}}, {"abcd", 4, []string{"abcd"}}, {"abcde", 4, []string{"abcd", "e"}},
		{"ab cd", 3, []string{"ab ", "cd"}}, {"é界●z", 2, []string{"é界", "●z"}},
		{"\x1b[31m", 4, []string{"\\x1b", "[31m"}}, {"a\tb", 4, []string{"a   ", "    ", "b"}},
		{"\xff", 3, []string{"\\xf", "f"}},
	} {
		if got := wrapLine(tt.raw, tt.width); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("wrap(%q,%d) = %q, want %q", tt.raw, tt.width, got, tt.want)
		}
	}
}

// R-2MK2-6KCL
func TestRunWritesWholeScreensOnly(t *testing.T) {
	for _, size := range [][2]int{{40, 5}, {120, 40}, {39, 40}, {120, 4}} {
		keys := make(chan []byte, 3)
		keys <- []byte("j")
		keys <- []byte("?")
		keys <- []byte("q")
		writer := &renderScreenRecorder{}
		if err := Run(context.Background(), Config{Home: "/home/dev", Root: fstest.MapFS{}, Terminal: &renderRunTerminal{cols: size[0], rows: size[1], keys: keys}}, writer); err != nil {
			t.Fatal(err)
		}
		if len(writer.screens) != 3 {
			t.Fatalf("size %v: %d writes, want three whole screens", size, len(writer.screens))
		}
		for _, screen := range writer.screens {
			if size[0] < 40 || size[1] < 5 {
				if screen != "\x1b[H\x1b[2Jterminal too small" {
					t.Errorf("non-screen write %q", screen)
				}
				continue
			}
			rest := screen
			for row := 1; row <= size[1]; row++ {
				prefix := fmt.Sprintf("\x1b[%d;1H\x1b[2K", row)
				if !strings.HasPrefix(rest, prefix) {
					t.Fatalf("row %d missing prefix in %q", row, rest)
				}
				rest = strings.TrimPrefix(rest, prefix)
				if row < size[1] {
					next := fmt.Sprintf("\x1b[%d;1H\x1b[2K", row+1)
					pos := strings.Index(rest, next)
					if pos < 0 {
						t.Fatalf("screen missing row %d", row+1)
					}
					rowText := rest[:pos]
					if strings.Contains(rowText, "\x1b") && !strings.Contains(rowText, "\x1b[7m") {
						t.Errorf("unexpected escape in row: %q", rowText)
					}
					rest = rest[pos:]
				}
			}
			if strings.Contains(rest, "\x1b") {
				t.Errorf("extra bytes after final row: %q", rest)
			}
		}
	}
}
