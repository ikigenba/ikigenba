package local_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/claude"
	"github.com/ikigenba/ikigenba/turns/internal/codex"
	"github.com/ikigenba/ikigenba/turns/internal/dispatch"
	"github.com/ikigenba/ikigenba/turns/internal/grok"
	"github.com/ikigenba/ikigenba/turns/internal/local"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

const stamp = "2026-02-03T10:00:00Z"

func home(t *testing.T) paths.Paths {
	t.Helper()
	root := t.TempDir()
	p, err := paths.Resolve(func(key string) string {
		if key == "HOME" {
			return root
		}
		return ""
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func jsonLines(t *testing.T, values ...any) string {
	t.Helper()
	var data []byte
	for _, value := range values {
		line, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	return string(data)
}

func addClaude(t *testing.T, p paths.Paths, id, moment, mention string) {
	t.Helper()
	write(t, filepath.Join(p.Claude, "projects", "workspace", id+".jsonl"), jsonLines(t, map[string]any{
		"type": "user", "timestamp": moment, "cwd": filepath.Join(p.Home, "workspace"),
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": mention}}},
	}))
}

func addCodex(t *testing.T, p paths.Paths, id string) {
	t.Helper()
	write(t, filepath.Join(p.Codex, "sessions", "year", "month", "day", "rollout-"+id+".jsonl"), jsonLines(t,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": filepath.Join(p.Home, "workspace"), "originator": "codex_exec"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"model": "fixture-model"}},
		map[string]any{"type": "token_usage_record", "timestamp": stamp, "payload": map[string]any{"usage": map[string]any{"input_tokens": 12, "output_tokens": 5}}},
	))
}

func addGrok(t *testing.T, p paths.Paths, id string) {
	t.Helper()
	dir := filepath.Join(p.Grok, "sessions", "encoded", id)
	write(t, filepath.Join(dir, "summary.json"), jsonLines(t, map[string]any{
		"info": map[string]any{"id": id, "cwd": filepath.Join(p.Home, "workspace")}, "session_kind": "headless",
	}))
	write(t, filepath.Join(dir, "updates.jsonl"), jsonLines(t, map[string]any{
		"params": map[string]any{
			"_meta":  map[string]any{"agentTimestampMs": 1770112800000},
			"update": map[string]any{"sessionUpdate": "turn_completed", "usage": map[string]any{"inputTokens": 7, "outputTokens": 3, "modelUsage": map[string]any{"fixture-model-build": map[string]any{}}}},
		},
	}))
}

func job(t *testing.T, p paths.Paths, name, id string) string {
	t.Helper()
	dir := filepath.Join(p.Jobs, name)
	write(t, filepath.Join(dir, "session-id"), id)
	return dir
}

func byRef(t *testing.T, found local.Found, tool session.Tool, id string) session.Session {
	t.Helper()
	for _, value := range found.Sessions {
		if value.Ref == (session.Ref{Tool: tool, ID: id}) {
			return value
		}
	}
	t.Fatalf("missing %s session %q in %+v", tool, id, found)
	return session.Session{}
}

func wantParent(t *testing.T, value session.Session, parent *session.Ref) {
	t.Helper()
	if !reflect.DeepEqual(value.Parent, parent) {
		t.Fatalf("%+v parent = %+v, want %+v", value.Ref, value.Parent, parent)
	}
}

// R-0AFU-K8LP R-0BNQ-Y0CE R-0CVN-BS33
func TestReadComposesReaderResultsInToolOrder(t *testing.T) {
	p := home(t)
	dir := job(t, p, "dispatch-codex", "a-child\n")
	for _, id := range []string{"z-parent", "a-parent"} {
		addClaude(t, p, id, stamp, dir)
	}
	for _, id := range []string{"z-child", "a-child"} {
		addCodex(t, p, id)
		addGrok(t, p, id)
	}
	jobs := dispatch.Jobs(p.Jobs)
	dirs := make([]string, len(jobs))
	for i, value := range jobs {
		dirs[i] = value.Dir
	}
	cf, mentions := claude.Read(p.Claude, dirs)
	xf, gf := codex.Read(p.Codex), grok.Read(p.Grok)
	if len(cf.Sessions) != 2 || len(xf.Sessions) != 2 || len(gf.Sessions) != 2 || len(mentions) != 2 {
		t.Fatalf("fixture not read: Claude=%+v Codex=%+v Grok=%+v mentions=%+v", cf, xf, gf, mentions)
	}
	all := cf.Sessions
	all = append(all, xf.Sessions...)
	all = append(all, gf.Sessions...)
	want := local.Found{dispatch.Link(all, jobs, mentions), map[session.Tool]int{session.ToolClaude: 0, session.ToolCodex: 0, session.ToolGrok: 0}}
	reader := struct {
		read func(paths.Paths) local.Found
	}{read: local.Read}
	got := reader.read(p)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gathered result = %+v, want %+v", got, want)
	}
	wantRefs := []session.Ref{
		{Tool: session.ToolClaude, ID: "a-parent"}, {Tool: session.ToolClaude, ID: "z-parent"},
		{Tool: session.ToolCodex, ID: "a-child"}, {Tool: session.ToolCodex, ID: "z-child"},
		{Tool: session.ToolGrok, ID: "a-child"}, {Tool: session.ToolGrok, ID: "z-child"},
	}
	for i, value := range got.Sessions {
		if value.Ref != wantRefs[i] {
			t.Fatalf("session %d = %+v, want %+v", i, value.Ref, wantRefs[i])
		}
	}
	wantParent(t, byRef(t, got, session.ToolCodex, "a-child"), &session.Ref{Tool: session.ToolClaude, ID: "a-parent"})
}

// R-0E3J-PJTS
func TestSkipCountsIncludeEveryTool(t *testing.T) {
	for _, grokBroken := range []bool{false, true} {
		t.Run(map[bool]string{false: "zero-grok", true: "broken-grok"}[grokBroken], func(t *testing.T) {
			p := home(t)
			addClaude(t, p, "parent", stamp, "no job")
			addCodex(t, p, "child")
			addGrok(t, p, "child")
			write(t, filepath.Join(p.Claude, "projects", "workspace", "broken.jsonl"), "not json\n")
			for _, name := range []string{"one", "two"} {
				write(t, filepath.Join(p.Codex, "archived_sessions", "rollout-"+name+".jsonl"), "not json\n")
			}
			wantGrok := 0
			if grokBroken {
				wantGrok = 1
				write(t, filepath.Join(p.Grok, "sessions", "encoded", "broken", "summary.json"), "[]")
			}
			cf, _ := claude.Read(p.Claude, nil)
			xf, gf := codex.Read(p.Codex), grok.Read(p.Grok)
			if cf.Skipped != 1 || xf.Skipped != 2 || gf.Skipped != wantGrok {
				t.Fatalf("fixture skips = %d, %d, %d", cf.Skipped, xf.Skipped, gf.Skipped)
			}
			got := local.Read(p)
			if len(got.Skipped) != len(session.Tools()) {
				t.Fatalf("skip keys = %+v", got.Skipped)
			}
			want := map[session.Tool]int{session.ToolClaude: cf.Skipped, session.ToolCodex: xf.Skipped, session.ToolGrok: gf.Skipped}
			for _, tool := range session.Tools() {
				count, ok := got.Skipped[tool]
				if !ok || count != want[tool] {
					t.Fatalf("%s skips = %d, present %v; want %d", tool, count, ok, want[tool])
				}
			}
		})
	}
}

// R-N7XX-PUF7
func TestCodexDispatchLinksPathSuffixes(t *testing.T) {
	for _, suffix := range []string{"/", "/session-id"} {
		t.Run(suffix, func(t *testing.T) {
			p := home(t)
			addCodex(t, p, "child")
			dir := job(t, p, "dispatch-codex", "child\n")
			addClaude(t, p, "parent", stamp, dir+suffix)
			wantParent(t, byRef(t, local.Read(p), session.ToolCodex, "child"), &session.Ref{Tool: session.ToolClaude, ID: "parent"})
		})
	}
}

// R-0GJC-H3B6
func TestGrokDispatchLinks(t *testing.T) {
	p := home(t)
	addGrok(t, p, "child")
	dir := job(t, p, "dispatch-grok", "child")
	addClaude(t, p, "parent", stamp, dir)
	wantParent(t, byRef(t, local.Read(p), session.ToolGrok, "child"), &session.Ref{Tool: session.ToolClaude, ID: "parent"})
}

// R-0HR8-UV1V
func TestUnnamedJobDoesNotLinkEitherTool(t *testing.T) {
	p := home(t)
	addCodex(t, p, "child")
	addGrok(t, p, "child")
	job(t, p, "unmentioned", "child\n")
	addClaude(t, p, "parent", stamp, "a different path")
	got := local.Read(p)
	for _, tool := range []session.Tool{session.ToolCodex, session.ToolGrok} {
		wantParent(t, byRef(t, got, tool, "child"), nil)
	}
}

// R-0IZ5-8MSK
func TestEarliestMentionWinsRegardlessOfClaudeID(t *testing.T) {
	for _, earlier := range []string{"a-parent", "z-parent"} {
		t.Run(earlier, func(t *testing.T) {
			p := home(t)
			addCodex(t, p, "child")
			dir := job(t, p, "dispatch-codex", "child\n")
			for _, id := range []string{"a-parent", "z-parent"} {
				moment := "2026-02-03T11:00:00Z"
				if id == earlier {
					moment = stamp
				}
				addClaude(t, p, id, moment, dir)
			}
			wantParent(t, byRef(t, local.Read(p), session.ToolCodex, "child"), &session.Ref{Tool: session.ToolClaude, ID: earlier})
		})
	}
}

// R-0K71-MEJ9
func TestAbsentRootsStayAbsent(t *testing.T) {
	p := home(t)
	got := local.Read(p)
	if len(got.Sessions) != 0 || len(got.Skipped) != len(session.Tools()) {
		t.Fatalf("empty home = %+v", got)
	}
	for _, tool := range session.Tools() {
		if count, ok := got.Skipped[tool]; !ok || count != 0 {
			t.Fatalf("empty home skips = %+v", got.Skipped)
		}
	}
	for _, dir := range []string{p.Claude, p.Codex, p.Grok, p.Jobs} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("root %q exists or stat failed: %v", dir, err)
		}
	}
}

type entrySnapshot struct {
	Mode    fs.FileMode
	ModTime time.Time
	Content string
}

func snapshot(t *testing.T, root string) map[string]entrySnapshot {
	t.Helper()
	tree, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := tree.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	entries := make(map[string]entrySnapshot)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := entrySnapshot{Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode().IsRegular() {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := tree.ReadFile(relative)
			if readErr != nil {
				return readErr
			}
			value.Content = string(data)
		}
		entries[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// R-0LEY-069Y
func TestReadLeavesAllFixtureTreesUnchanged(t *testing.T) {
	p := home(t)
	addCodex(t, p, "codex-child")
	addGrok(t, p, "grok-child")
	dir := job(t, p, "dispatch-codex", "codex-child\n")
	job(t, p, "dispatch-grok", "grok-child")
	write(t, filepath.Join(p.Jobs, "not-started", "notes"), "no session id yet")
	addClaude(t, p, "parent", stamp, dir+"/session-id")
	write(t, filepath.Join(p.Claude, "projects", "workspace", "broken.jsonl"), "not json\n")
	write(t, filepath.Join(p.Grok, "sessions", "encoded", "grok-child", "ignored"), "unrelated bytes")
	before := snapshot(t, p.Home)
	got := local.Read(p)
	if len(got.Sessions) != 3 || got.Skipped[session.ToolClaude] != 1 {
		t.Fatalf("fixture not fully read: %+v", got)
	}
	if after := snapshot(t, p.Home); !reflect.DeepEqual(after, before) {
		t.Fatalf("fixture trees changed\nbefore: %+v\nafter: %+v", before, after)
	}
}
