package claude_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/claude"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

const instant = "2025-04-01T09:00:00Z"

func line(kind, stamp string, content any) map[string]any {
	return map[string]any{"type": kind, "timestamp": stamp, "cwd": "/work/project", "message": map[string]any{"content": content}}
}
func assistant(id, stamp, model string, usage any) map[string]any {
	l := line("assistant", stamp, nil)
	l["message"] = map[string]any{"id": id, "model": model, "usage": usage}
	return l
}
func encoded(t *testing.T, lines ...any) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		raw, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}
func write(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func readLines(t *testing.T, lines ...any) session.Session {
	t.Helper()
	root := t.TempDir()
	write(t, root, "projects/p/example.jsonl", encoded(t, lines...))
	found, _ := claude.Read(root, nil, time.Time{})
	if found.Skipped != 0 || len(found.Sessions) != 1 {
		t.Fatalf("found = %+v", found)
	}
	return found.Sessions[0]
}
func wantTurns(t *testing.T, s session.Session, expected ...session.Turn) {
	t.Helper()
	if !reflect.DeepEqual(s.Turns, expected) {
		t.Fatalf("turns\n got: %+v\nwant: %+v", s.Turns, expected)
	}
}
func at(t *testing.T, stamp string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return v.UTC().Truncate(time.Millisecond)
}

// R-MZ1F-9ROK R-VTI0-X5H8 R-VUPX-AX7X R-VVXT-OOYM R-VX5Q-2GPB
// R-VYDM-G8G0 R-WJ3W-YC1T R-ESJC-G4LS R-ETR8-TWCH R-EUZ5-7O36
// R-FGXC-3JFO R-ERBG-2CV3 R-EOVN-ATDP
func TestSources(t *testing.T) {
	reader := struct {
		Read func(string, []string, time.Time) (session.Found, []session.Mention)
	}{Read: claude.Read}
	root := t.TempDir()
	top := line("user", instant, "hello")
	top["isSidechain"] = true
	sub := line("user", instant, "hello")
	sub["isSidechain"] = false
	sub["entrypoint"] = "cli"
	paths := []string{"projects/p/ssh-alpha.jsonl", "projects/p/zeta.jsonl", "projects/p/absent/subagents/agent-a.jsonl", "projects/p/zeta/subagents/workflows/flow/agent-b.jsonl"}
	for _, path := range paths {
		write(t, root, path, encoded(t, sub))
	}
	write(t, root, paths[0], encoded(t, top))
	ignored := []string{"outside.jsonl", "projects/direct.jsonl", "projects/p/zeta/subagents/not-agent.jsonl", "projects/p/zeta/subagents/agent-.jsonl", "projects/p/zeta/subagents/workflows/flow/journal.jsonl", "projects/p/zeta/subagents/workflows/flow/deeper/agent-c.jsonl", "projects/p/zeta/deeper/agent-x.jsonl"}
	for _, path := range ignored {
		write(t, root, path, "not JSON\n")
	}
	write(t, root, "projects/p/absent/subagents/agent-a.meta.json", "broken")
	found, _ := reader.Read(root, nil, time.Time{})
	ids := []string{"absent/agent-a", "ssh-alpha", "zeta", "zeta/agent-b"}
	if found.Skipped != 0 || len(found.Sessions) != len(ids) {
		t.Fatalf("found: %+v", found)
	}
	for i, s := range found.Sessions {
		if s.Ref != (session.Ref{Tool: session.ToolClaude, ID: ids[i]}) || len(s.Turns) != 1 {
			t.Fatalf("session: %+v", s)
		}
		slash := strings.Index(s.Ref.ID, "/")
		if slash < 0 {
			if s.Parent != nil || s.Headless {
				t.Fatalf("top session: %+v", s)
			}
		} else if s.Parent == nil || *s.Parent != (session.Ref{Tool: session.ToolClaude, ID: s.Ref.ID[:slash]}) || !s.Headless {
			t.Fatalf("subagent: %+v", s)
		}
	}
}

// R-VZLI-U06P R-OW6P-XS0K
func TestDuplicatePreferenceAndMentions(t *testing.T) {
	root := t.TempDir()
	job := filepath.Join(root, "jobs", "task")
	tool := line("user", instant, []any{map[string]any{"type": "tool_result", "content": job}})
	write(t, root, "projects/z/parent/subagents/workflows/w/agent-x.jsonl", encoded(t, tool))
	preferred := line("user", instant, "preferred")
	preferred["cwd"] = "/preferred"
	write(t, root, "projects/z/parent/subagents/agent-x.jsonl", encoded(t, preferred))
	write(t, root, "projects/b/same.jsonl", encoded(t, tool))
	preferred["cwd"] = "/lexical"
	write(t, root, "projects/a/same.jsonl", encoded(t, preferred))
	found, mentions := claude.Read(root, []string{job}, time.Time{})
	if found.Skipped != 0 || len(found.Sessions) != 2 || len(mentions) != 0 {
		t.Fatalf("found=%+v mentions=%+v", found, mentions)
	}
	if found.Sessions[0].Directory != "/preferred" || found.Sessions[1].Directory != "/lexical" {
		t.Fatalf("sessions: %+v", found.Sessions)
	}
}

// R-W0TF-7RXE R-W21B-LJO3 R-W397-ZBES R-W4H4-D35H R-W5P0-QUW6
// R-W6WX-4MMV R-F2AJ-IAJC
func TestLineClassification(t *testing.T) {
	cases := []struct {
		name  string
		line  map[string]any
		whose session.Whose
	}{
		{"plain", line("user", instant, "hello"), session.WhosePerson},
		{"tool-result", line("user", instant, []any{map[string]any{"type": "text", "text": "other"}, map[string]any{"type": "tool_result"}}), session.WhoseTool},
		{"text-array", line("user", instant, []any{map[string]any{"type": "text", "text": "other"}}), ""},
		{"null", line("user", instant, nil), ""},
		{"attachment", line("attachment", instant, "hello"), ""},
		{"system", line("system", instant, "hello"), ""},
		{"bookkeeping", line("progress", instant, "hello"), ""},
	}
	for _, origin := range []string{"task-notification", "peer", "coordinator"} {
		l := line("user", instant, nil)
		l["origin"] = map[string]any{"kind": origin}
		l["isMeta"] = true
		cases = append(cases, struct {
			name  string
			line  map[string]any
			whose session.Whose
		}{origin, l, session.WhoseTool})
	}
	meta := line("user", instant, "hello")
	meta["isMeta"] = true
	cases = append(cases, struct {
		name  string
		line  map[string]any
		whose session.Whose
	}{"meta", meta, ""})
	notMeta := line("user", instant, "hello")
	notMeta["isMeta"] = "true"
	notMeta["origin"] = map[string]any{"kind": "other"}
	cases = append(cases, struct {
		name  string
		line  map[string]any
		whose session.Whose
	}{"not-bool-meta", notMeta, session.WhosePerson})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "projects/p/session.jsonl", encoded(t, tc.line))
			found, _ := claude.Read(root, nil, time.Time{})
			if found.Skipped != 0 {
				t.Fatalf("skipped: %+v", found)
			}
			if tc.whose == "" {
				if len(found.Sessions) != 0 {
					t.Fatalf("unexpected sessions: %+v", found)
				}
				return
			}
			if len(found.Sessions) != 1 {
				t.Fatalf("sessions: %+v", found)
			}
			wantTurns(t, found.Sessions[0], session.Turn{Moment: at(t, instant), Whose: tc.whose})
		})
	}
}

// R-WXSV-4EK2 R-W9CP-W649 R-WAKM-9XUY R-WBSI-NPLN R-WE8B-F931
// R-WFG7-T0TQ R-WHW0-KKB4 R-F3IF-W2A1
func TestAssistantAssembly(t *testing.T) {
	first := assistant("one", instant, "old", map[string]any{"input_tokens": -1, "output_tokens": "invalid"})
	invalidID := assistant("", instant, "ignored", map[string]any{"input_tokens": -1})
	last := assistant("one", "2025-04-01T12:00:00Z", "<synthetic>", map[string]any{"input_tokens": 2, "cache_creation_input_tokens": 3, "cache_read_input_tokens": 7, "output_tokens": 11, "thinking_tokens": 13, "output_tokens_details": map[string]any{"thinking_tokens": -1}})
	s := readLines(t, first, assistant("two", "2025-04-01T10:00:00Z", "second", map[string]any{"output_tokens_details": map[string]any{"thinking_tokens": 17}}), invalidID, last, assistant("three", "2025-04-01T11:00:00Z", "", nil))
	wantTurns(t, s,
		session.Turn{Moment: at(t, instant), Whose: session.WhoseAgent, Model: "<synthetic>", Tokens: session.Tokens{Input: 5, Cached: 7, Output: 11, Reasoning: 13}},
		session.Turn{Moment: at(t, "2025-04-01T10:00:00Z"), Whose: session.WhoseAgent, Model: "second", Tokens: session.Tokens{Reasoning: 17}},
		session.Turn{Moment: at(t, "2025-04-01T11:00:00Z"), Whose: session.WhoseAgent})
	// A later missing/non-string model replaces an earlier model.
	final := assistant("model", instant, "prior", nil)
	missing := assistant("model", instant, "", nil)
	missing["message"].(map[string]any)["model"] = 42
	s = readLines(t, final, missing)
	if s.Turns[0].Model != "" {
		t.Fatalf("model: %q", s.Turns[0].Model)
	}
}

// R-WKBT-C3SI R-WLJP-PVJ7 R-EW71-LFTV
func TestDirectoryAndEntrypoint(t *testing.T) {
	for _, entrypoint := range []string{"sdk-cli", "cli", "claude-desktop", "anything", ""} {
		t.Run(entrypoint, func(t *testing.T) {
			book := line("bookkeeping", instant, nil)
			book["cwd"] = ""
			book["entrypoint"] = ""
			first := line("system", instant, nil)
			first["cwd"] = "/work/a/../b/./"
			first["entrypoint"] = entrypoint
			second := line("user", instant, "hi")
			second["entrypoint"] = "cli"
			s := readLines(t, book, first, second)
			if s.Directory != "/work/b" || s.Headless != (entrypoint == "sdk-cli") {
				t.Fatalf("session: %+v", s)
			}
		})
	}
	s := readLines(t, line("user", instant, "hi"))
	if s.Headless {
		t.Fatal("missing entrypoint is headless")
	}
}

// R-WQFB-8YHZ
func TestFirstCommand(t *testing.T) {
	valid := func(name, args string) string {
		return "<command-name>" + name + "</command-name><command-args>" + args + "</command-args>"
	}
	var lines []any
	for _, name := range []string{"clear", "/clear", "exit", "/exit", "", "/"} {
		lines = append(lines, line("user", instant, valid(name, "ignored")))
	}
	lines = append(lines, line("user", instant, "<command-name>unfinished"))
	meta := line("user", instant, valid("meta", "ignored"))
	meta["isMeta"] = true
	tool := line("user", instant, valid("tool", "ignored"))
	tool["origin"] = map[string]any{"kind": "peer"}
	lines = append(lines, meta, tool, line("system", instant, valid("system", "ignored")), line("user", "broken", valid("bad-time", "ignored")), line("user", instant, valid("/plan", "  a <b>\n c ")), line("user", instant, valid("later", "ignored")))
	s := readLines(t, lines...)
	if s.Command == nil || *s.Command != (session.Command{Name: "/plan", Argument: "  a <b>"}) {
		t.Fatalf("command: %+v", s.Command)
	}
	s = readLines(t, line("user", instant, "<command-name>plain</command-name> <command-args>unfinished"))
	if s.Command == nil || *s.Command != (session.Command{Name: "plain"}) {
		t.Fatalf("command: %+v", s.Command)
	}
	for _, sub := range []bool{false, true} {
		root := t.TempDir()
		path := "projects/p/no-command.jsonl"
		if sub {
			path = "projects/p/parent/subagents/agent-a.jsonl"
		}
		write(t, root, path, encoded(t, line("user", instant, valid("clear", "")), line("user", instant, "ordinary")))
		f, _ := claude.Read(root, nil, time.Time{})
		if len(f.Sessions) != 1 || f.Sessions[0].Command != nil {
			t.Fatalf("session: %+v", f)
		}
	}
}

// R-OX0X-GWCI R-EZUQ-QR1Y R-F12N-4ISN R-OY8T-UO37
func TestMomentsOrderAndMerge(t *testing.T) {
	s := readLines(t,
		assistant("first", "2025-04-01T11:00:00.0009+02:00", "first", map[string]any{"input_tokens": 2, "cache_read_input_tokens": 3, "output_tokens": 5, "thinking_tokens": 7}),
		line("user", instant, "person"),
		assistant("second", instant, "last", map[string]any{"input_tokens": 11, "cache_read_input_tokens": 13, "output_tokens": 17, "thinking_tokens": 19}),
		line("user", "2025-04-01T08:59:59.9999Z", "earlier"),
		line("user", instant, "same person"),
		line("user", instant, []any{map[string]any{"type": "tool_result"}}),
		assistant("bad", "broken", "bad", nil), assistant("bad", instant, "later", nil),
		line("user", "", "missing"), line("user", "1969-12-31T23:59:59.9999Z", "pre-epoch"))
	wantTurns(t, s,
		session.Turn{Moment: at(t, "2025-04-01T08:59:59.999Z"), Whose: session.WhosePerson},
		session.Turn{Moment: at(t, instant), Whose: session.WhoseAgent, Model: "last", Tokens: session.Tokens{Input: 13, Cached: 16, Output: 22, Reasoning: 26}},
		session.Turn{Moment: at(t, instant), Whose: session.WhosePerson},
		session.Turn{Moment: at(t, instant), Whose: session.WhoseTool})
	root := t.TempDir()
	missing := line("user", instant, "hi")
	delete(missing, "timestamp")
	write(t, root, "projects/p/empty.jsonl", encoded(t, missing, line("user", "bad", "x"), line("user", "1969-12-31T23:59:59Z", "x")))
	f, _ := claude.Read(root, nil, time.Time{})
	if len(f.Sessions) != 0 || f.Skipped != 0 {
		t.Fatalf("bad moments: %+v", f)
	}
}

// R-WGO4-6SKF R-OOVB-N5KE R-OZGQ-8FTW R-FC1Q-KGGW R-FD9M-Y87L
func TestInvalidCountsSkipWholeSource(t *testing.T) {
	for _, key := range []string{"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "output_tokens", "thinking_tokens", "nested"} {
		for _, bad := range []any{1.5, "1", -1, json.Number("1000000000001"), json.Number("9223372036854775808"), nil} {
			t.Run(fmt.Sprintf("%s/%v", key, bad), func(t *testing.T) {
				usage := map[string]any{key: bad}
				if key == "nested" {
					usage = map[string]any{"output_tokens_details": map[string]any{"thinking_tokens": bad}}
				}
				root := t.TempDir()
				tool := line("user", instant, "/jobs/x")
				tool["origin"] = map[string]any{"kind": "peer"}
				write(t, root, "projects/p/bad.jsonl", encoded(t, tool, assistant("one", instant, "m", usage), assistant("two", "2025-04-01T10:00:00Z", "m", usage)))
				write(t, root, "projects/p/good.jsonl", encoded(t, line("user", instant, "ok")))
				f, m := claude.Read(root, []string{"/jobs/x"}, time.Time{})
				if f.Skipped != 1 || len(f.Sessions) != 1 || f.Sessions[0].Ref.ID != "good" || len(m) != 0 {
					t.Fatalf("found=%+v mentions=%+v", f, m)
				}
			})
		}
	}
	for _, field := range []string{"input_tokens", "cache_read_input_tokens", "output_tokens", "thinking_tokens"} {
		root := t.TempDir()
		write(t, root, "projects/p/sum.jsonl", encoded(t, assistant("one", instant, "m", map[string]any{field: 1000000000000}), assistant("two", instant, "m", map[string]any{field: 1})))
		f, _ := claude.Read(root, nil, time.Time{})
		if f.Skipped != 1 || len(f.Sessions) != 0 {
			t.Fatalf("merge %s: %+v", field, f)
		}
	}
	root := t.TempDir()
	write(t, root, "projects/p/sum.jsonl", encoded(t, assistant("one", instant, "m", map[string]any{"input_tokens": 1000000000000, "cache_creation_input_tokens": 1})))
	f, _ := claude.Read(root, nil, time.Time{})
	if f.Skipped != 1 || len(f.Sessions) != 0 {
		t.Fatalf("input sum: %+v", f)
	}
	// Both inclusive limits remain valid.
	s := readLines(t, assistant("one", instant, "m", map[string]any{"input_tokens": 1000000000000, "cache_read_input_tokens": 1000000000000, "output_tokens": 1000000000000, "thinking_tokens": 1000000000000}))
	if s.Turns[0].Tokens != (session.Tokens{Input: 1000000000000, Cached: 1000000000000, Output: 1000000000000, Reasoning: 1000000000000}) {
		t.Fatalf("limits: %+v", s)
	}
}

// R-EQ3J-OL4E R-OQ38-0XB3 R-ORB4-EP1S R-OSJ0-SGSH
func TestJSONLines(t *testing.T) {
	good := encoded(t, line("user", instant, "hi"))
	for _, bad := range []string{"{", "[]", `"text"`, "42", "true", "null", "{} {}"} {
		t.Run(bad, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "projects/p/broken.jsonl", good+bad+"\n")
			f, _ := claude.Read(root, nil, time.Time{})
			if f.Skipped != 1 || len(f.Sessions) != 0 {
				t.Fatalf("complete line: %+v", f)
			}
			write(t, root, "projects/p/broken.jsonl", good+bad)
			f, _ = claude.Read(root, nil, time.Time{})
			if f.Skipped != 0 || len(f.Sessions) != 1 {
				t.Fatalf("unterminated line: %+v", f)
			}
		})
	}
	root := t.TempDir()
	write(t, root, "projects/p/blank.jsonl", "\n \t\r\n"+good+"\n")
	write(t, root, "projects/p/empty.jsonl", "")
	write(t, root, "projects/p/bookkeeping.jsonl", "{}\n")
	write(t, root, "projects/p/incomplete.jsonl", strings.TrimSuffix(good, "\n"))
	f, _ := claude.Read(root, nil, time.Time{})
	if f.Skipped != 0 || len(f.Sessions) != 1 {
		t.Fatalf("blanks: %+v", f)
	}
}

// R-F9LX-SWZI
func TestMissingIdentityOrDirectory(t *testing.T) {
	for _, tc := range []struct{ name, path, cwd string }{{"no-id", "projects/p/.jsonl", "/work"}, {"no-cwd", "projects/p/id.jsonl", ""}, {"relative", "projects/p/id.jsonl", "relative/path"}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			l := line("user", instant, "hi")
			l["cwd"] = tc.cwd
			write(t, root, tc.path, encoded(t, l))
			f, _ := claude.Read(root, nil, time.Time{})
			if f.Skipped != 1 || len(f.Sessions) != 0 {
				t.Fatalf("found: %+v", f)
			}
		})
	}
}

// R-FATU-6OQ7
func TestUnreadableFile(t *testing.T) {
	root := t.TempDir()
	path := write(t, root, "projects/p/unreadable.jsonl", encoded(t, line("user", instant, "hi")))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0600); err != nil {
			t.Error(err)
		}
	})
	f, _ := claude.Read(root, nil, time.Time{})
	if f.Skipped != 1 || len(f.Sessions) != 0 {
		t.Fatalf("unreadable: %+v", f)
	}
}

// R-OUYT-K09V R-FFPF-PROZ
func TestReadOnlyAndMissingDirectories(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	for _, path := range []string{missing, root} {
		f, m := claude.Read(path, []string{filepath.Join(root, "jobs", "absent")}, time.Time{})
		if len(f.Sessions) != 0 || f.Skipped != 0 || len(m) != 0 {
			t.Fatalf("missing: %+v %+v", f, m)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing root created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects")); !os.IsNotExist(err) {
		t.Fatalf("projects created: %v", err)
	}
	job := filepath.Join(root, "jobs", "task")
	write(t, root, "jobs/task/output", "unchanged")
	write(t, root, "projects/p/s.jsonl", encoded(t, line("user", instant, "hi")))
	type saved struct {
		contents string
		mode     os.FileMode
		modified time.Time
	}
	snapshot := func() map[string]saved {
		result := make(map[string]saved)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			content := ""
			if !entry.IsDir() {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				data, err := fs.ReadFile(os.DirFS(root), relative)
				if err != nil {
					return err
				}
				content = string(data)
			}
			result[path] = saved{content, info.Mode(), info.ModTime()}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	claude.Read(root, []string{job}, time.Time{})
	after := snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reader changed tree")
	}
}

// R-X2OG-NHIU R-WSV4-0HZD R-X08N-VY1G R-X3WD-199J R-WWIT-5T7G
// R-WYYL-XCOU R-X06I-B4FJ R-X1EE-OW68
func TestMentions(t *testing.T) {
	root := t.TempDir()
	job := filepath.Join(root, ".local/state/dispatch/jobs/x")
	other := filepath.Join(root, "jobs/alpha")
	tool := func(stamp string, content any) map[string]any {
		l := line("user", stamp, content)
		l["origin"] = map[string]any{"kind": "peer"}
		return l
	}
	write(t, root, "projects/p/b.jsonl", encoded(t,
		tool("2025-04-01T10:00:00Z", job+"/output"),
		tool(instant, []any{map[string]any{"text": job + "2 then " + job + "/last.md"}, map[string]any{"content": other}}),
		tool("2025-04-01T08:00:00Z", []any{map[string]any{"content": []any{map[string]any{"text": "~/.local/state/dispatch/jobs/x/last.md"}}}})))
	write(t, root, "projects/p/a.jsonl", encoded(t, tool(instant, job)))
	found, mentions := claude.Read(root, []string{other, job, job, ""}, time.Time{})
	expected := []session.Mention{
		{Path: job, By: session.Ref{Tool: session.ToolClaude, ID: "a"}, Moment: at(t, instant)},
		{Path: job, By: session.Ref{Tool: session.ToolClaude, ID: "b"}, Moment: at(t, "2025-04-01T08:00:00Z")},
		{Path: other, By: session.Ref{Tool: session.ToolClaude, ID: "b"}, Moment: at(t, instant)},
	}
	// The jobs paths above sort with the hidden-state path before jobs/alpha.
	if !reflect.DeepEqual(mentions, expected) {
		t.Fatalf("mentions: %+v", mentions)
	}
	for _, jobs := range [][]string{nil, {}} {
		f, m := claude.Read(root, jobs, time.Time{})
		if !reflect.DeepEqual(f, found) || len(m) != 0 {
			t.Fatalf("jobs=%v found=%+v mentions=%+v", jobs, f, m)
		}
	}
	exclusions := []map[string]any{
		line("user", instant, job), assistant("id", instant, "m", nil), line("attachment", instant, job), line("system", instant, job), line("progress", instant, job), tool("invalid", job), tool("1969-12-31T23:59:59Z", job), tool(instant, job+"2"), tool(instant, "prefix"+job), tool(instant, job+".suffix"), tool(instant, job+"_suffix"), tool(instant, job+"-suffix"), tool(instant, nil),
	}
	exclusions[1]["message"].(map[string]any)["content"] = []any{map[string]any{"type": "tool_use", "input": job}}
	exclusions[len(exclusions)-1]["toolUseResult"] = job
	for i, l := range exclusions {
		isolated := t.TempDir()
		write(t, isolated, "projects/p/excluded.jsonl", encoded(t, line("user", instant, "baseline"), l))
		_, m := claude.Read(isolated, []string{job}, time.Time{})
		if len(m) != 0 {
			t.Fatalf("exclusion %d: %+v", i, m)
		}
	}
	for _, text := range []string{job, job + "/", job + "/last.md", "(" + job + ")", job + "2 " + job + "/last.md"} {
		isolated := t.TempDir()
		write(t, isolated, "projects/p/included.jsonl", encoded(t, tool(instant, text)))
		_, m := claude.Read(isolated, []string{job}, time.Time{})
		if len(m) != 1 {
			t.Fatalf("text %q: %+v", text, m)
		}
	}
}
