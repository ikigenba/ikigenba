package claude_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/claude"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

func commandPair(name string) string {
	return "<command-name>" + name + "</command-name>"
}

func skillItem(skill any, args any) map[string]any {
	return map[string]any{"type": "tool_use", "name": "Skill", "input": map[string]any{"skill": skill, "args": args}}
}

func skillLine(id, stamp string, items ...any) map[string]any {
	l := assistant(id, stamp, "model", nil)
	l["message"].(map[string]any)["content"] = items
	return l
}

func readCommand(t *testing.T, sub bool, lines ...any) *session.Command {
	t.Helper()
	root := t.TempDir()
	path := "projects/p/example.jsonl"
	if sub {
		path = "projects/p/parent/subagents/agent-child.jsonl"
	}
	write(t, root, path, encoded(t, lines...))
	found, _ := claude.Read(root, nil, time.Time{})
	if found.Skipped != 0 || len(found.Sessions) != 1 {
		t.Fatalf("found=%+v", found)
	}
	return found.Sessions[0].Command
}

// R-RJO8-T0H9
func TestCommandSourceOrder(t *testing.T) {
	person := line("user", "2025-04-01T10:00:00Z", commandPair("/deliver"))
	skill := skillLine("skill", instant, skillItem("x", nil))
	for _, sub := range []bool{false, true} {
		for _, tc := range []struct {
			name  string
			lines []any
			want  string
		}{
			{"person-first", []any{person, skill}, "/deliver"},
			{"skill-first", []any{skill, person}, "/x"},
			{"pairs", []any{line("user", instant, commandPair("/first")+commandPair("/second")), skill}, "/first"},
			{"items", []any{skillLine("one", instant, skillItem("first", nil), skillItem("second", nil)), person}, "/first"},
			{"later-block", []any{assistant("one", instant, "model", nil), person, skillLine("one", instant, skillItem("x", nil))}, "/deliver"},
		} {
			t.Run(tc.name+map[bool]string{false: "/top", true: "/sub"}[sub], func(t *testing.T) {
				got := readCommand(t, sub, tc.lines...)
				if got == nil || got.Name != tc.want {
					t.Fatalf("command=%+v, want name %q", got, tc.want)
				}
			})
		}
	}
}

// R-RKW5-6S7Y
func TestCommandPairNames(t *testing.T) {
	for _, name := range []string{"/deliver", "deliver", "//clear", "Clear", " /deliver ", "nested<command-name>/deliver"} {
		t.Run(name, func(t *testing.T) {
			got := readCommand(t, false, line("user", instant, "caveat <command-message>ignored</command-message>"+commandPair(name)))
			if got == nil || got.Name != name {
				t.Fatalf("command=%+v, want name %q", got, name)
			}
		})
	}
}

// R-RM41-KJYN
func TestCommandPairExclusionsContinue(t *testing.T) {
	for _, sub := range []bool{false, true} {
		for _, name := range []string{"clear", "/clear", "exit", "/exit", "", "/"} {
			for _, sameLine := range []bool{false, true} {
				lines := []any{line("user", instant, commandPair(name)), line("user", instant, commandPair("/deliver"))}
				if sameLine {
					lines = []any{line("user", instant, commandPair(name)+commandPair("/deliver"))}
				}
				if got := readCommand(t, sub, lines...); got == nil || got.Name != "/deliver" {
					t.Fatalf("sub=%v name=%q same-line=%v command=%+v", sub, name, sameLine, got)
				}
			}
		}
		for _, content := range []string{"ordinary", "<command-name>unfinished", commandPair("clear") + commandPair("/exit") + commandPair("") + commandPair("/")} {
			if got := readCommand(t, sub, line("user", instant, content), skillLine("one", instant, skillItem("", nil))); got != nil {
				t.Fatalf("sub=%v content=%q command=%+v", sub, content, got)
			}
		}
	}
}

// R-RNBX-YBPC
func TestSkillCommandNames(t *testing.T) {
	for _, name := range []string{"x", "superpowers:brainstorm", "clear", "exit", "/written", " spaced "} {
		got := readCommand(t, false, skillLine("one", instant, skillItem(name, nil)))
		if got == nil || got.Name != "/"+name {
			t.Fatalf("skill=%q command=%+v", name, got)
		}
	}
	bad := []any{
		skillItem(nil, nil), skillItem(42, nil), skillItem(true, nil), skillItem("", nil),
		map[string]any{"type": "tool_use", "name": "Skill", "input": map[string]any{}},
		map[string]any{"type": "tool_use", "name": "Skill"},
		map[string]any{"type": "tool_use", "name": "Skill", "input": "x"},
		map[string]any{"type": "text", "name": "Skill", "input": map[string]any{"skill": "wrong"}},
		map[string]any{"type": "tool_use", "name": "skill", "input": map[string]any{"skill": "wrong"}},
		"not an item",
	}
	for _, item := range bad {
		for _, sameLine := range []bool{false, true} {
			lines := []any{skillLine("one", instant, item), skillLine("two", instant, skillItem("chosen", nil))}
			if sameLine {
				lines = []any{skillLine("one", instant, item, skillItem("chosen", nil))}
			}
			got := readCommand(t, false, lines...)
			if got == nil || got.Name != "/chosen" {
				t.Fatalf("item=%+v same-line=%v command=%+v", item, sameLine, got)
			}
		}
	}
}

// R-RPRQ-PV6Q
func TestCommandPairArgument(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"first-line", commandPair("/deliver") + "<command-args>  first <b>\nprivate prompt</command-args>", "  first <b>"},
		{"carriage-return", commandPair("/deliver") + "<command-args>first\r\nprivate</command-args>", "first\r"},
		{"whole", commandPair("/deliver") + "<command-args>  all as written  </command-args>", "  all as written  "},
		{"earlier", "<command-args>before</command-args>" + commandPair("/deliver"), ""},
		{"chosen-pair", commandPair("/clear") + "<command-args>ignored</command-args>" + commandPair("/deliver") + "<command-args>chosen</command-args>", "chosen"},
		{"first-args", commandPair("/deliver") + "<command-args>one</command-args><command-args>two</command-args>", "one"},
		{"empty", commandPair("/deliver") + "<command-args></command-args>", ""},
		{"unterminated", commandPair("/deliver") + "<command-args>unfinished", ""},
		{"missing", commandPair("/deliver"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readCommand(t, false, line("user", instant, tc.content), line("user", instant, "<command-args>other turn</command-args>"))
			if got == nil || got.Argument != tc.want {
				t.Fatalf("command=%+v, want argument %q", got, tc.want)
			}
		})
	}
}

// R-RQZN-3MXF
func TestSkillArgument(t *testing.T) {
	for _, tc := range []struct {
		name string
		args any
		want string
	}{
		{"first-line", "  first <b>\nprivate prompt", "  first <b>"},
		{"carriage-return", "first\r\nprivate", "first\r"},
		{"whole", "  all as written  ", "  all as written  "},
		{"empty", "", ""}, {"null", nil, ""}, {"number", 42, ""}, {"boolean", true, ""}, {"object", map[string]any{"text": "not args"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readCommand(t, false, skillLine("one", instant, skillItem("chosen", tc.args), skillItem("other", "other item")), skillLine("two", instant, skillItem("later", "later line")))
			if got == nil || got.Argument != tc.want {
				t.Fatalf("command=%+v, want argument %q", got, tc.want)
			}
		})
	}
	missing := map[string]any{"type": "tool_use", "name": "Skill", "input": map[string]any{"skill": "chosen"}}
	got := readCommand(t, false, skillLine("one", instant, missing, skillItem("other", "other item")), skillLine("two", instant, skillItem("later", "later line")))
	if got == nil || got.Argument != "" {
		t.Fatalf("missing args command=%+v", got)
	}
}

// R-RS7J-HEO4
func TestCommandArgumentByteLimit(t *testing.T) {
	for _, tc := range []struct{ name, arg, want string }{
		{"ascii", strings.Repeat("a", 300), strings.Repeat("a", 256)},
		{"ascii-exact", strings.Repeat("a", 256), strings.Repeat("a", 256)},
		{"short", "éshort", "éshort"},
		{"two-byte-cut", strings.Repeat("a", 255) + "éextra", strings.Repeat("a", 255)},
		{"three-byte-cut", strings.Repeat("a", 254) + "€extra", strings.Repeat("a", 254)},
		{"four-byte-cut", strings.Repeat("a", 253) + "😀extra", strings.Repeat("a", 253)},
		{"utf8-exact", strings.Repeat("é", 128), strings.Repeat("é", 128)},
		{"utf8-boundary", strings.Repeat("é", 129), strings.Repeat("é", 128)},
	} {
		for _, skill := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/pair", true: "/skill"}[skill], func(t *testing.T) {
				l := line("user", instant, commandPair("/deliver")+"<command-args>"+tc.arg+"</command-args>")
				if skill {
					l = skillLine("one", instant, skillItem("deliver", tc.arg))
				}
				got := readCommand(t, false, l)
				if got == nil || got.Argument != tc.want {
					t.Fatalf("command=%+v, want argument %q", got, tc.want)
				}
			})
		}
	}
}

// R-RTFF-V6ET
func TestCommandRequiresReturnedTurn(t *testing.T) {
	pair := commandPair("/excluded")
	meta := line("user", instant, pair)
	meta["isMeta"] = true
	tool := line("user", instant, pair)
	tool["origin"] = map[string]any{"kind": "peer"}
	nonStringID := skillLine("one", instant, skillItem("excluded", nil))
	nonStringID["message"].(map[string]any)["id"] = 42
	missingID := skillLine("one", instant, skillItem("excluded", nil))
	delete(missingID["message"].(map[string]any), "id")
	for _, tc := range []struct {
		name  string
		lines []any
	}{
		{"tool", []any{tool}},
		{"meta", []any{meta}},
		{"system", []any{line("system", instant, pair)}},
		{"assistant-pair", []any{skillLine("one", instant, map[string]any{"type": "text", "text": pair})}},
		{"bad-person-time", []any{line("user", "broken", pair)}},
		{"pre-epoch-person", []any{line("user", "1969-12-31T23:59:59Z", pair)}},
		{"user-skill", []any{line("user", instant, []any{skillItem("excluded", nil)})}},
		{"tool-result-skill", []any{line("user", instant, []any{map[string]any{"type": "tool_result", "content": []any{skillItem("excluded", nil)}}})}},
		{"empty-id", []any{skillLine("", instant, skillItem("excluded", nil))}},
		{"missing-id", []any{missingID}},
		{"non-string-id", []any{nonStringID}},
		{"bad-agent-time", []any{skillLine("one", "broken", skillItem("excluded", nil))}},
		{"pre-epoch-agent", []any{skillLine("one", "1969-12-31T23:59:59Z", skillItem("excluded", nil))}},
		{"first-line-invalid", []any{assistant("one", "broken", "model", nil), skillLine("one", instant, skillItem("excluded", nil))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := append([]any{line("user", instant, "baseline")}, tc.lines...)
			if got := readCommand(t, false, lines...); got != nil {
				t.Fatalf("command=%+v", got)
			}
		})
	}
	// A later block's timestamp does not drop an agent turn whose first line is usable.
	got := readCommand(t, false, assistant("one", instant, "model", nil), skillLine("one", "broken", skillItem("admitted", nil)))
	if got == nil || got.Name != "/admitted" {
		t.Fatalf("command=%+v", got)
	}
}
