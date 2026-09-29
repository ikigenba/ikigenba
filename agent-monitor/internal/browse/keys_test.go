package browse

import (
	"context"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// R-D81S-JCKP
func TestDecodeKeyTokens(t *testing.T) {
	tests := []struct {
		value string
		want  []key
	}{
		{"", nil}, {"jj", []key{keyDown, keyDown}},
		{"\x1b[A\x1b[A", []key{keyUp, keyUp}},
		{"\x1b[5~q", []key{keyPageUp, keyQuit}},
		{"\x1bj", []key{keyIgnore}}, {"\x1b\x1b", []key{keyIgnore}},
		{"\x1b\x1b[A", []key{keyIgnore}},
		{"j\x1b", []key{keyDown, keyIgnore}},
		{"\x1b[\x03", []key{keyIgnore, keyQuit}},
		{"\r\n", []key{keyOpen, keyIgnore}},
		{"\x1b[1;5Aj", []key{keyIgnore, keyDown}},
		{"\x1b[5", []key{keyIgnore}}, {"\x1bO", []key{keyIgnore}},
		{"\x1bO\x03q", []key{keyIgnore, keyQuit}},
		{"é界j", []key{keyIgnore, keyIgnore, keyDown}},
		{"\xffj\xc3q", []key{keyIgnore, keyDown, keyIgnore, keyQuit}},
		{"\x1béj", []key{keyIgnore, keyDown}},
		{"\x1b\x1b\x1b[Aq", []key{keyIgnore, keyQuit}},
		{"\x1b", []key{keyQuit}},
	}
	for _, tt := range tests {
		if got := decodeKeys([]byte(tt.value)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decodeKeys(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

// R-D99O-X4BE
func TestIgnoredKeysPreserveRunState(t *testing.T) {
	original := browserHarnesses
	t.Cleanup(func() { browserHarnesses = original })
	ignored := []string{"?", "Q", "J", "H", "\n", "\x1bj", "\x1b[1;5A", "\x1b[5", "\x1bO", "\xff", "é", "\x1b\x1b[A", "?\x1b"}
	states := []struct {
		name, setup string
		failure     bool
		tail        []string
	}{
		{"harness scrolled", "G", false, []string{"k", "l", "h"}},
		{"session scrolled", "GlG", false, []string{"k", "l", "h", "h"}},
		{"agent scrolled", "GlGlG", false, []string{"k", "l", "h", "h", "h"}},
		{"chat following", "GlGlGl", false, []string{"k", "j", "h", "h", "h"}},
		{"chat paused", "GlGlGlgj", false, []string{"j", "k", "G", "h", "h", "h"}},
		{"chat failure", "GlGlGl", true, []string{"k", "G", "h", "h", "h"}},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			run := func(extra *string) ([]string, []string) {
				browserHarnesses = original
				var calls []string
				for h := range browserHarnesses {
					browserHarnesses[h].list = func(fs.FS, string) ([]session.Session, error) {
						calls = append(calls, fmt.Sprintf("list %d", h))
						return []session.Session{
							{ID: "session-a", HasStarted: true, Started: time.Unix(1, 0)},
							{ID: "session-b", HasStarted: true, Started: time.Unix(2, 0)},
							{ID: "session-c", HasStarted: true, Started: time.Unix(3, 0)},
						}, nil
					}
					browserHarnesses[h].tree = func(_ fs.FS, _, sid string) (tree.Tree, error) {
						calls = append(calls, fmt.Sprintf("tree %d %s", h, sid))
						return tree.Tree{Root: tree.Node{ID: sid}, Subagents: []tree.Node{{ID: "child-a", Parent: sid}, {ID: "child-b", Parent: sid}}}, nil
					}
					browserHarnesses[h].chat = func(_ fs.FS, _, sid, aid string) (*chat.Transcript, []chat.Entry, error) {
						calls = append(calls, fmt.Sprintf("chat %d %s %s", h, sid, aid))
						if state.failure {
							return nil, nil, &session.ReadError{Path: "/fixture/chat", Err: fs.ErrPermission}
						}
						var lines []string
						for i := range 20 {
							lines = append(lines, fmt.Sprintf("%d %s %s line %02d", h, sid, aid, i))
						}
						return chat.NewTranscript("", chat.Recorded{}, nil), []chat.Entry{{Kind: chat.KindUser, Text: strings.Join(lines, "\n")}}, nil
					}
				}
				keys := make(chan []byte, len(state.tail)+3)
				keys <- []byte(state.setup)
				if extra != nil {
					keys <- []byte(*extra)
				}
				for _, value := range state.tail {
					keys <- []byte(value)
				}
				keys <- []byte("q")
				writer := &renderScreenRecorder{}
				if err := Run(context.Background(), Config{Home: "/fixture", Root: fstest.MapFS{}, Terminal: &renderRunTerminal{cols: 120, rows: 5, keys: keys}}, writer); err != nil {
					t.Fatal(err)
				}
				return writer.screens, calls
			}
			baseline, baselineCalls := run(nil)
			// Establish that the compared state actually has the requested scroll,
			// selection or chat following state, rather than an empty default view.
			before := baseline[1]
			switch state.name {
			case "harness scrolled":
				if strings.Contains(before, "Claude Code") || !strings.Contains(before, "\x1b[7mGrok") {
					t.Fatal("harness fixture did not scroll/select last")
				}
			case "session scrolled":
				if strings.Contains(before, "session-a") || !strings.Contains(before, "\x1b[7msession-c") {
					t.Fatal("session fixture did not scroll/select last")
				}
			case "agent scrolled":
				if !strings.Contains(before, "child-b") || strings.Contains(before, "child-a") || !strings.Contains(before, "\x1b[7m") {
					t.Fatal("agent fixture did not scroll/select child")
				}
			case "chat following":
				if !strings.Contains(before, "[following]") || strings.Contains(before, "line 00") {
					t.Fatal("chat fixture not following end")
				}
			case "chat paused":
				if !strings.Contains(before, "[paused]") || !strings.Contains(before, "line 00") {
					t.Fatal("chat fixture not paused after first row")
				}
			case "chat failure":
				if !strings.Contains(before, "cannot read") || !strings.Contains(before, "[following]") {
					t.Fatal("chat fixture did not fail following")
				}
			}
			for _, token := range ignored {
				t.Run(fmt.Sprintf("%x", []byte(token)), func(t *testing.T) {
					screens, calls := run(&token)
					if len(screens) != len(baseline)+1 {
						t.Fatalf("ignored %q changed screen count: %d", token, len(screens))
					}
					if screens[2] != before {
						t.Fatalf("ignored %q changed current screen\nbefore %q\nafter %q", token, before, screens[2])
					}
					if !reflect.DeepEqual(screens[3:], baseline[2:]) {
						t.Fatalf("ignored %q changed subsequent navigation/view/following", token)
					}
					if !reflect.DeepEqual(calls, baselineCalls) {
						t.Fatalf("ignored %q changed harness/session/agent identities: %v want %v", token, calls, baselineCalls)
					}
				})
			}
		})
	}
}

// R-D99O-X4BE
func TestExactKeyMapping(t *testing.T) {
	groups := []struct {
		tokens []string
		want   key
	}{
		{[]string{"q", "\x03", "\x1b"}, keyQuit},
		{[]string{"\x1b[A", "\x1bOA", "k"}, keyUp},
		{[]string{"\x1b[B", "\x1bOB", "j"}, keyDown},
		{[]string{"\x1b[C", "\x1bOC", "\r", "\x1bOM", "l"}, keyOpen},
		{[]string{"\x1b[D", "\x1bOD", "h"}, keyBack},
		{[]string{"\x1b[5~", "\x02"}, keyPageUp},
		{[]string{"\x1b[6~", "\x06"}, keyPageDown},
		{[]string{"g"}, keyFirst}, {[]string{"G"}, keyLast},
		{[]string{"Q", "J", "H", "\n", "\x1bj", "\x1b[1;5A", "\x1b[5", "\x1bO", "\x1b[m", "\x1b[7~", "é"}, keyIgnore},
	}
	for _, group := range groups {
		for _, token := range group.tokens {
			got := decodeKeys([]byte(token))
			if !reflect.DeepEqual(got, []key{group.want}) {
				t.Errorf("decodeKeys(%q) = %v, want [%v]", token, got, group.want)
			}
		}
	}
	if got := decodeKeys([]byte("j\x1b")); !reflect.DeepEqual(got, []key{keyDown, keyIgnore}) {
		t.Errorf("trailing escape = %v", got)
	}
}
