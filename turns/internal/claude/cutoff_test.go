package claude_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/claude"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

func modified(t *testing.T, path string, stamp time.Time) {
	t.Helper()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// R-N09B-NJF9 R-N1H8-1B5Y R-KJBS-9KBX
func TestCutoffChoosesFilesIndependently(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0).UTC()
	job := filepath.Join(root, "jobs", "old")
	old := write(t, root, "projects/p/parent.jsonl", encoded(t, line("user", instant, []any{map[string]any{"type": "tool_result", "content": job}})))
	modified(t, old, since.Add(-time.Second))
	bad := write(t, root, "projects/p/broken.jsonl", "not JSON\n")
	modified(t, bad, since.Add(-time.Second))
	unreadable := write(t, root, "projects/p/denied.jsonl", "broken\n")
	modified(t, unreadable, since.Add(-time.Second))
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	for name, stamp := range map[string]time.Time{"equal": since, "fraction": since.Add(time.Millisecond), "parent/subagents/agent-child": since} {
		path := write(t, root, "projects/p/"+name+".jsonl", encoded(t, line("user", instant, "hello")))
		modified(t, path, stamp)
	}
	found, mentions := claude.Read(root, []string{job}, since)
	var ids []string
	for _, value := range found.Sessions {
		ids = append(ids, value.Ref.ID)
		if value.Ref.ID == "parent/agent-child" && (value.Parent == nil || *value.Parent != (session.Ref{Tool: session.ToolClaude, ID: "parent"})) {
			t.Fatalf("parent: %+v", value)
		}
	}
	if !reflect.DeepEqual(ids, []string{"equal", "fraction", "parent/agent-child"}) || found.Skipped != 0 || len(mentions) != 0 {
		t.Fatalf("found=%+v mentions=%+v", found, mentions)
	}
}

// R-KI3V-VSL8 R-KKJO-NC2M
func TestZeroCutoffAndWholeSource(t *testing.T) {
	for _, stamp := range []time.Time{time.Unix(-1, 0), time.Unix(2200000000, 0)} {
		root := t.TempDir()
		path := write(t, root, "projects/p/example.jsonl", encoded(t, line("user", instant, "hello"), line("user", "2025-04-01T09:00:01Z", "again")))
		modified(t, path, stamp)
		all, _ := claude.Read(root, nil, time.Time{})
		if len(all.Sessions) != 1 || len(all.Sessions[0].Turns) != 2 || all.Skipped != 0 {
			t.Fatalf("all: %+v", all)
		}
		if stamp.After(time.Unix(0, 0)) {
			changed, _ := claude.Read(root, nil, time.Unix(1800000000, 0))
			if !reflect.DeepEqual(all, changed) {
				t.Fatalf("all=%+v changed=%+v", all, changed)
			}
		}
	}
	root := t.TempDir()
	path := write(t, root, "projects/p/bad.jsonl", "bad\n")
	since := time.Unix(1800000000, 0)
	modified(t, path, since)
	all, _ := claude.Read(root, nil, time.Time{})
	changed, _ := claude.Read(root, nil, since)
	if !reflect.DeepEqual(all, changed) || changed.Skipped != 1 {
		t.Fatalf("all=%+v changed=%+v", all, changed)
	}
}

// R-N54X-6ME1
func TestCutoffIsReevaluatedOnEveryCall(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	path := write(t, root, "projects/p/example.jsonl", "bad\n")
	modified(t, path, since)
	first, _ := claude.Read(root, nil, since)
	if first.Skipped != 1 {
		t.Fatalf("first=%+v", first)
	}
	write(t, root, "projects/p/example.jsonl", encoded(t, line("user", instant, "fixed")))
	modified(t, path, since)
	second, _ := claude.Read(root, nil, since)
	if len(second.Sessions) != 1 || second.Skipped != 0 {
		t.Fatalf("second=%+v", second)
	}
	modified(t, path, since.Add(-time.Second))
	third, _ := claude.Read(root, nil, since)
	if len(third.Sessions) != 0 || third.Skipped != 0 {
		t.Fatalf("third=%+v", third)
	}
	modified(t, path, since)
	fourth, _ := claude.Read(root, nil, since)
	if !reflect.DeepEqual(second, fourth) {
		t.Fatalf("second=%+v fourth=%+v", second, fourth)
	}
}

// R-N6CT-KE4Q R-N7KP-Y5VF R-N8SM-BXM4 R-NB8F-3H3I
func TestCommandPairsAndArgument(t *testing.T) {
	pair := func(name string) string { return "<command-name>" + name + "</command-name>" }
	for _, test := range []struct{ name, content, args string }{
		{"preface", "caveat <command-message>ignored</command-message>" + pair("/deliver") + "<command-args>first\nsecret prompt</command-args>", "first"},
		{"same-turn", pair("clear") + pair("/exit") + pair("") + pair("/") + pair("/deliver") + "<command-args>chosen</command-args>", "chosen"},
		{"earlier-args", "<command-args>before</command-args>" + pair("/deliver"), ""},
		{"first-args", pair("/deliver") + "<command-args>one</command-args><command-args>two</command-args>", "one"},
		{"unterminated", pair("/deliver") + "<command-args>unfinished", ""},
		{"ascii", pair("/deliver") + "<command-args>" + strings.Repeat("a", 300) + "</command-args>", strings.Repeat("a", 256)},
		{"utf8-cut", pair("/deliver") + "<command-args>" + strings.Repeat("a", 255) + "éextra</command-args>", strings.Repeat("a", 255)},
		{"utf8-whole", pair("/deliver") + "<command-args>" + strings.Repeat("é", 128) + "</command-args>", strings.Repeat("é", 128)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := readLines(t, line("user", instant, test.content), line("user", "2025-04-01T09:00:01Z", pair("later")))
			if got.Command == nil || *got.Command != (session.Command{Name: "/deliver", Argument: test.args}) {
				t.Fatalf("command=%+v", got.Command)
			}
		})
	}
	for _, sub := range []bool{false, true} {
		root := t.TempDir()
		path := "projects/p/example.jsonl"
		if sub {
			path = "projects/p/top/subagents/agent-child.jsonl"
		}
		write(t, root, path, encoded(t, line("user", instant, pair("/clear")), line("user", "2025-04-01T09:00:01Z", pair("/deliver"))))
		got, _ := claude.Read(root, nil, time.Time{})
		if len(got.Sessions) != 1 || got.Sessions[0].Command == nil || got.Sessions[0].Command.Name != "/deliver" {
			t.Fatalf("got=%+v", got)
		}
		write(t, root, path, encoded(t, line("user", instant, pair("/exit")+pair("clear"))))
		got, _ = claude.Read(root, nil, time.Time{})
		if len(got.Sessions) != 1 || got.Sessions[0].Command != nil {
			t.Fatalf("got=%+v", got)
		}
	}
}
