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
	dir, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := dir.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if err := dir.WriteFile(filepath.Base(name), []byte(content), 0600); err != nil {
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

// R-0AFU-K8LP R-JB5F-WWV6 R-JCDC-AOLV
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
	cf, mentions := claude.Read(p.Claude, dirs, time.Time{})
	xf, gf := codex.Read(p.Codex, time.Time{}, nil), grok.Read(p.Grok, time.Time{}, nil)
	if len(cf.Sessions) != 2 || len(xf.Sessions) != 2 || len(gf.Sessions) != 2 || len(mentions) != 2 {
		t.Fatalf("fixture not read: Claude=%+v Codex=%+v Grok=%+v mentions=%+v", cf, xf, gf, mentions)
	}
	all := cf.Sessions
	all = append(all, xf.Sessions...)
	all = append(all, gf.Sessions...)
	want := local.Found{dispatch.Link(all, jobs, mentions), map[session.Tool]int{session.ToolClaude: 0, session.ToolCodex: 0, session.ToolGrok: 0}}
	reader := struct {
		read func(paths.Paths, time.Time) local.Found
	}{read: local.Read}
	got := reader.read(p, time.Time{})
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

// R-JDL8-OGCK
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
			cf, _ := claude.Read(p.Claude, nil, time.Time{})
			xf, gf := codex.Read(p.Codex, time.Time{}, nil), grok.Read(p.Grok, time.Time{}, nil)
			if cf.Skipped != 1 || xf.Skipped != 2 || gf.Skipped != wantGrok {
				t.Fatalf("fixture skips = %d, %d, %d", cf.Skipped, xf.Skipped, gf.Skipped)
			}
			got := local.Read(p, time.Time{})
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

// R-JET5-2839
func TestCodexDispatchLinksPathSuffixes(t *testing.T) {
	for _, suffix := range []string{"/", "/session-id"} {
		t.Run(suffix, func(t *testing.T) {
			p := home(t)
			addCodex(t, p, "child")
			dir := job(t, p, "dispatch-codex", "child\n")
			addClaude(t, p, "parent", stamp, dir+suffix)
			wantParent(t, byRef(t, local.Read(p, time.Time{}), session.ToolCodex, "child"), &session.Ref{Tool: session.ToolClaude, ID: "parent"})
		})
	}
}

// R-JG11-FZTY
func TestGrokDispatchLinks(t *testing.T) {
	p := home(t)
	addGrok(t, p, "child")
	dir := job(t, p, "dispatch-grok", "child")
	addClaude(t, p, "parent", stamp, dir)
	wantParent(t, byRef(t, local.Read(p, time.Time{}), session.ToolGrok, "child"), &session.Ref{Tool: session.ToolClaude, ID: "parent"})
}

// R-JH8X-TRKN
func TestUnnamedJobDoesNotLinkEitherTool(t *testing.T) {
	p := home(t)
	addCodex(t, p, "child")
	addGrok(t, p, "child")
	job(t, p, "unmentioned", "child\n")
	addClaude(t, p, "parent", stamp, "a different path")
	got := local.Read(p, time.Time{})
	for _, tool := range []session.Tool{session.ToolCodex, session.ToolGrok} {
		wantParent(t, byRef(t, got, tool, "child"), nil)
	}
}

// R-JIGU-7JBC
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
			wantParent(t, byRef(t, local.Read(p, time.Time{}), session.ToolCodex, "child"), &session.Ref{Tool: session.ToolClaude, ID: earlier})
		})
	}
}

// R-JJOQ-LB21
func TestAbsentRootsStayAbsent(t *testing.T) {
	for _, since := range []time.Time{{}, cutoff(t)} {
		p := home(t)
		got := local.Read(p, since)
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

// R-JKWM-Z2SQ
func TestReadLeavesAllFixtureTreesUnchanged(t *testing.T) {
	p := home(t)
	since := cutoff(t)
	for _, id := range []string{"forced-child", "cold-child", "changed-child"} {
		addCodex(t, p, id)
		addGrok(t, p, id)
	}
	dir := job(t, p, "dispatch", "forced-child\n")
	write(t, filepath.Join(p.Jobs, "not-started", "notes"), "no session id yet")
	addClaude(t, p, "parent", stamp, dir+"/session-id")
	addClaude(t, p, "cold-parent", stamp, "no job")
	write(t, claudeFile(p, "broken"), "not json\n")
	write(t, filepath.Join(grokDir(p, "forced-child"), "ignored"), "unrelated bytes")
	setTreeTime(t, p.Home, since.Add(-time.Hour))
	setTime(t, claudeFile(p, "parent"), since)
	setTime(t, claudeFile(p, "broken"), since)
	setTime(t, codexFile(p, "changed-child"), since)
	setTime(t, filepath.Join(grokDir(p, "changed-child"), "updates.jsonl"), since)
	before := snapshot(t, p.Home)
	for _, when := range []time.Time{{}, since} {
		got := local.Read(p, when)
		wantSessions := 8
		if !when.IsZero() {
			wantSessions = 5
		}
		if len(got.Sessions) != wantSessions || got.Skipped[session.ToolClaude] != 1 {
			t.Fatalf("fixture not read as expected: %+v", got)
		}
		if after := snapshot(t, p.Home); !reflect.DeepEqual(after, before) {
			t.Fatalf("fixture trees changed\nbefore: %+v\nafter: %+v", before, after)
		}
	}
}

func claudeFile(p paths.Paths, id string) string {
	return filepath.Join(p.Claude, "projects", "workspace", id+".jsonl")
}

func codexFile(p paths.Paths, id string) string {
	return filepath.Join(p.Codex, "sessions", "year", "month", "day", "rollout-"+id+".jsonl")
}

func grokDir(p paths.Paths, id string) string {
	return filepath.Join(p.Grok, "sessions", "encoded", id)
}

func cutoff(t *testing.T) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return value.Add(24 * time.Hour)
}

func setTime(t *testing.T, name string, value time.Time) {
	t.Helper()
	if err := os.Chtimes(name, value, value); err != nil {
		t.Fatal(err)
	}
}

func setTreeTime(t *testing.T, root string, value time.Time) {
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
	if err := fs.WalkDir(tree.FS(), ".", func(name string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return tree.Chtimes(name, value, value)
	}); err != nil {
		t.Fatal(err)
	}
}

func wantAbsent(t *testing.T, found local.Found, refs ...session.Ref) {
	t.Helper()
	for _, value := range found.Sessions {
		for _, ref := range refs {
			if value.Ref == ref {
				t.Fatalf("unexpected session %+v in %+v", ref, found)
			}
		}
	}
}

// R-JCDC-AOLV
func TestReadComposesSelectedSourcesInToolOrder(t *testing.T) {
	p := home(t)
	since := cutoff(t)
	dir := job(t, p, "named", "a-child")
	job(t, p, "unnamed", "z-child")
	for _, id := range []string{"z-parent", "a-parent", "cold-parent"} {
		addClaude(t, p, id, stamp, dir)
	}
	for _, id := range []string{"a-child", "fresh-child", "z-child"} {
		addCodex(t, p, id)
		addGrok(t, p, id)
	}
	setTreeTime(t, p.Home, since.Add(-time.Hour))
	for _, id := range []string{"z-parent", "a-parent"} {
		setTime(t, claudeFile(p, id), since)
	}
	setTime(t, codexFile(p, "fresh-child"), since)
	setTime(t, filepath.Join(grokDir(p, "fresh-child"), "updates.jsonl"), since)
	jobs := dispatch.Jobs(p.Jobs)
	var dirs []string
	for _, value := range jobs {
		dirs = append(dirs, value.Dir)
	}
	cf, mentions := claude.Read(p.Claude, dirs, since)
	xf := codex.Read(p.Codex, since, []string{"a-child"})
	gf := grok.Read(p.Grok, since, []string{"a-child"})
	all := append(append(cf.Sessions, xf.Sessions...), gf.Sessions...)
	want := dispatch.Link(all, jobs, mentions)
	got := local.Read(p, since)
	if !reflect.DeepEqual(got.Sessions, want) {
		t.Fatalf("selected composition = %+v, want %+v", got.Sessions, want)
	}
	wantRefs := []session.Ref{
		{Tool: session.ToolClaude, ID: "a-parent"}, {Tool: session.ToolClaude, ID: "z-parent"},
		{Tool: session.ToolCodex, ID: "a-child"}, {Tool: session.ToolCodex, ID: "fresh-child"},
		{Tool: session.ToolGrok, ID: "a-child"}, {Tool: session.ToolGrok, ID: "fresh-child"},
	}
	var refs []session.Ref
	for _, value := range got.Sessions {
		refs = append(refs, value.Ref)
	}
	if !reflect.DeepEqual(refs, wantRefs) {
		t.Fatalf("selected refs = %+v, want %+v", refs, wantRefs)
	}
}

// R-JDL8-OGCK
func TestSkipCountsOnlyIncludeSelectedBrokenSources(t *testing.T) {
	p := home(t)
	since := cutoff(t)
	for _, id := range []string{"old", "changed"} {
		write(t, claudeFile(p, id), "not json\n")
		write(t, codexFile(p, id), "not json\n")
		write(t, filepath.Join(grokDir(p, id), "updates.jsonl"), "not json\n")
	}
	setTreeTime(t, p.Home, since.Add(-time.Hour))
	setTime(t, claudeFile(p, "changed"), since.Add(time.Second))
	setTime(t, codexFile(p, "changed"), since.Add(time.Second))
	setTime(t, filepath.Join(grokDir(p, "changed"), "updates.jsonl"), since.Add(time.Second))
	cf, _ := claude.Read(p.Claude, nil, since)
	xf, gf := codex.Read(p.Codex, since, nil), grok.Read(p.Grok, since, nil)
	want := map[session.Tool]int{session.ToolClaude: cf.Skipped, session.ToolCodex: xf.Skipped, session.ToolGrok: gf.Skipped}
	got := local.Read(p, since)
	if !reflect.DeepEqual(got.Skipped, want) || len(got.Skipped) != len(session.Tools()) {
		t.Fatalf("skip counts = %+v, want %+v", got.Skipped, want)
	}
	for _, tool := range session.Tools() {
		if got.Skipped[tool] != 1 {
			t.Fatalf("%s skip count = %d, want 1", tool, got.Skipped[tool])
		}
	}
}

// R-JM4J-CUJF R-JNCF-QMA4
func TestReadClaudeMentionForcesUnchangedJobChildren(t *testing.T) {
	p := home(t)
	since := cutoff(t)
	addCodex(t, p, "child")
	addGrok(t, p, "child")
	codexData, err := os.ReadFile(codexFile(p, "child"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, codexFile(p, "child"), string(codexData)+jsonLines(t, map[string]any{
		"type": "token_usage_record", "timestamp": "2026-02-03T11:00:00Z",
		"payload": map[string]any{"usage": map[string]any{"input_tokens": 21, "output_tokens": 9}},
	}))
	updates := filepath.Join(grokDir(p, "child"), "updates.jsonl")
	grokData, err := os.ReadFile(filepath.Clean(updates))
	if err != nil {
		t.Fatal(err)
	}
	write(t, updates, string(grokData)+jsonLines(t, map[string]any{
		"params": map[string]any{
			"_meta":  map[string]any{"agentTimestampMs": 1770116400000},
			"update": map[string]any{"sessionUpdate": "user_message_chunk"},
		},
	}))
	dir := job(t, p, "named", "child")
	addClaude(t, p, "parent", stamp, dir)
	setTreeTime(t, p.Home, since.Add(-time.Hour))
	setTime(t, claudeFile(p, "parent"), since)
	uncut := local.Read(p, time.Time{})
	got := local.Read(p, since)
	for _, tool := range []session.Tool{session.ToolCodex, session.ToolGrok} {
		child := byRef(t, got, tool, "child")
		wantParent(t, child, &session.Ref{Tool: session.ToolClaude, ID: "parent"})
		wantTurns := byRef(t, uncut, tool, "child").Turns
		if len(wantTurns) != 2 || !reflect.DeepEqual(child.Turns, wantTurns) {
			t.Fatalf("%s forced turns = %+v, want %+v", tool, child.Turns, wantTurns)
		}
	}
}

// R-JOKC-4E0T
func TestLinkUsesOnlyMentionsReadInThisCall(t *testing.T) {
	p := home(t)
	since := cutoff(t)
	addCodex(t, p, "child")
	addGrok(t, p, "child")
	dir := job(t, p, "named", "child")
	addClaude(t, p, "old-parent", stamp, dir)
	setTreeTime(t, p.Home, since.Add(-time.Hour))
	setTime(t, codexFile(p, "child"), since)
	setTime(t, filepath.Join(grokDir(p, "child"), "updates.jsonl"), since)
	uncut := local.Read(p, time.Time{})
	got := local.Read(p, since)
	wantAbsent(t, got, session.Ref{Tool: session.ToolClaude, ID: "old-parent"})
	for _, tool := range []session.Tool{session.ToolCodex, session.ToolGrok} {
		wantParent(t, byRef(t, uncut, tool, "child"), &session.Ref{Tool: session.ToolClaude, ID: "old-parent"})
		wantParent(t, byRef(t, got, tool, "child"), nil)
	}
	addClaude(t, p, "new-parent", "2026-02-03T11:00:00Z", dir)
	setTime(t, claudeFile(p, "new-parent"), since)
	got = local.Read(p, since)
	wantAbsent(t, got, session.Ref{Tool: session.ToolClaude, ID: "old-parent"})
	for _, tool := range []session.Tool{session.ToolCodex, session.ToolGrok} {
		wantParent(t, byRef(t, got, tool, "child"), &session.Ref{Tool: session.ToolClaude, ID: "new-parent"})
	}
}

// R-JPS8-I5RI
func TestUnchangedChildrenRequireMentionFromSelectedClaudeSource(t *testing.T) {
	for _, scenario := range []string{"no-mention", "unchanged-parent", "different-job"} {
		t.Run(scenario, func(t *testing.T) {
			p := home(t)
			since := cutoff(t)
			addCodex(t, p, "child")
			addGrok(t, p, "child")
			dir := job(t, p, "named", "child")
			if scenario == "unchanged-parent" {
				addClaude(t, p, "parent", stamp, dir)
			}
			if scenario == "different-job" {
				other := job(t, p, "different", "absent-child")
				addClaude(t, p, "parent", stamp, other)
			}
			setTreeTime(t, p.Home, since.Add(-time.Hour))
			if scenario == "different-job" {
				setTime(t, claudeFile(p, "parent"), since)
			}
			got := local.Read(p, since)
			wantAbsent(t, got, session.Ref{Tool: session.ToolCodex, ID: "child"}, session.Ref{Tool: session.ToolGrok, ID: "child"})
		})
	}
}

// R-JR04-VXI7
func TestEveryReadReloadsJobSessionIDsRegardlessOfTimes(t *testing.T) {
	p := home(t)
	since := cutoff(t)
	addCodex(t, p, "old-child")
	addCodex(t, p, "new-child")
	dir := job(t, p, "named", "old-child")
	addClaude(t, p, "parent", stamp, dir)
	old := since.Add(-time.Hour)
	setTreeTime(t, p.Home, old)
	setTime(t, claudeFile(p, "parent"), since)
	first := local.Read(p, since)
	wantParent(t, byRef(t, first, session.ToolCodex, "old-child"), &session.Ref{Tool: session.ToolClaude, ID: "parent"})
	wantAbsent(t, first, session.Ref{Tool: session.ToolCodex, ID: "new-child"})
	write(t, filepath.Join(dir, "session-id"), "new-child\n")
	setTime(t, filepath.Join(dir, "session-id"), old)
	setTime(t, dir, old)
	second := local.Read(p, since)
	wantParent(t, byRef(t, second, session.ToolCodex, "new-child"), &session.Ref{Tool: session.ToolClaude, ID: "parent"})
	wantAbsent(t, second, session.Ref{Tool: session.ToolCodex, ID: "old-child"})
}
