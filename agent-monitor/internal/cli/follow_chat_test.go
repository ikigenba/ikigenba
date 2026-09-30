package cli

import (
	"bytes"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
)

const chatFollowPath = "home/dev/.claude/projects/work/sample.jsonl"

func chatFollowUser(text string) string {
	return `{"type":"user","message":{"content":"` + text + `"}}` + "\n"
}

func chatFollowUsage(id string, in int) string {
	return `{"type":"assistant","message":{"id":"` + id + `","model":"model","usage":{"input_tokens":` + strconv.Itoa(in) + `,"output_tokens":1}}}` + "\n"
}

type chatFollowScriptRoot struct {
	*chatFollowFS
	active    bool
	steps     []func()
	pass      int
	interrupt chan struct{}
}

func (f *chatFollowScriptRoot) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(f.root, name)
}

func (f *chatFollowScriptRoot) Open(name string) (fs.File, error) {
	if f.active && name == chatFollowPath {
		f.steps[f.pass]()
		f.pass++
		if f.pass == len(f.steps) {
			close(f.interrupt)
		}
	}
	return f.chatFollowFS.Open(name)
}

type chatFollowScriptWatcher struct {
	root    *chatFollowScriptRoot
	changes chan struct{}
}

func (w *chatFollowScriptWatcher) Watch(_ []string) {
	w.root.active = true
}

func (w *chatFollowScriptWatcher) Changes() <-chan struct{} { return w.changes }

// R-Z51M-89IG R-EP82-A6NK R-EQFY-NYE9 R-ERNV-1Q4Y R-ESVR-FHVN
func TestFollowChatRunPreservesOutputThroughFailureAndReset(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		f := &chatFollowScriptRoot{chatFollowFS: &chatFollowFS{root: fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("one long initial message") + chatFollowUsage("old", 8))}}}, interrupt: make(chan struct{})}
		f.steps = []func(){
			func() {
				f.root[chatFollowPath].Data = append(f.root[chatFollowPath].Data, []byte(chatFollowUser("two"))...)
				f.deny = chatFollowPath
			},
			func() { f.deny = "" },
			func() { f.root[chatFollowPath].Data = []byte(chatFollowUser("new") + chatFollowUsage("new", 1)) },
			func() {},
		}
		changes := make(chan struct{}, len(f.steps))
		for range f.steps {
			changes <- struct{}{}
		}
		w := &chatFollowScriptWatcher{root: f, changes: changes}
		var out, diag bytes.Buffer
		code := Run([]string{"chat", "-f", "claude", "sample"}, System{Home: "/home/dev", Root: f, Terminal: terminal, Watcher: w, Interrupt: f.interrupt}, &out, &diag)
		want := chatFollowEntry("one long initial message") + chatFollowEntry("two") + chatFollowEntry("new")
		if terminal {
			recorded := chat.Recorded{In: true, CacheWrite: true, CacheRead: true, Out: true, Reasoning: true, Calls: true}
			oldFooter := strings.TrimSuffix(chat.TotalsLine(chat.Usage{In: 8, Out: 1, Calls: 1}, recorded), "\n")
			newFooter := strings.TrimSuffix(chat.TotalsLine(chat.Usage{In: 1, Out: 1, Calls: 1}, recorded), "\n")
			want = "\x1b[?25l" + chatFollowEntry("one long initial message") + oldFooter + "\r\x1b[2K" + chatFollowEntry("two") + oldFooter + "\r\x1b[2K" + chatFollowEntry("new") + newFooter + "\x1b[?25h\n"
		}
		if code != ExitSuccess || diag.Len() != 0 || out.String() != want || f.pass != len(f.steps) {
			t.Fatalf("terminal=%v: code=%d out=%q diag=%q passes=%d; want %q", terminal, code, out.String(), diag.String(), f.pass, want)
		}
	}
}

func chatFollowEntry(text string) string {
	return chat.Format(chat.Entry{Kind: chat.KindUser, Text: text})
}

func chatFollowNew(root fs.FS, terminal bool) *chatFollowRenderer {
	return newChatFollowRenderer(parsedCommand{kind: "chat", harness: "claude", id: "sample", agent: "sample"}, System{Home: "/home/dev", Root: root, Terminal: terminal}).(*chatFollowRenderer)
}

func chatFollowRender(t *testing.T, r *chatFollowRenderer, first bool) string {
	t.Helper()
	got, err := r.render(first)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

type chatFollowFS struct {
	root  fstest.MapFS
	opens []string
	deny  string
}

func (f *chatFollowFS) Open(name string) (fs.File, error) {
	f.opens = append(f.opens, name)
	if name == f.deny {
		return nil, fs.ErrPermission
	}
	return f.root.Open(name)
}

// R-Z51M-89IG
func TestFollowChatRetainsTranscriptAndReadsOnePass(t *testing.T) {
	f := &chatFollowFS{root: fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("first"))}}}
	r := chatFollowNew(f, false)
	if got := chatFollowRender(t, r, true); got != chatFollowEntry("first") {
		t.Fatalf("first entries = %q", got)
	}
	tr := r.transcript
	f.opens = nil
	f.root[chatFollowPath].Data = append(f.root[chatFollowPath].Data, []byte(chatFollowUser("second"))...)
	if got := chatFollowRender(t, r, false); got != chatFollowEntry("second") || r.transcript != tr {
		t.Fatalf("later entries = %q, retained = %v", got, r.transcript == tr)
	}
	if len(f.opens) != 1 || f.opens[0] != chatFollowPath {
		t.Fatalf("later reads = %q; want one transcript pass", f.opens)
	}
	f.opens = nil
	if got := chatFollowRender(t, r, false); got != "" || len(f.opens) != 1 || f.opens[0] != chatFollowPath {
		t.Fatalf("unchanged pass = %q, reads %q", got, f.opens)
	}
}

// R-EP82-A6NK
func TestFollowChatPipeOutputsOnlyNewEntries(t *testing.T) {
	m := fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("one") + chatFollowUser("two") + chatFollowUsage("message", 2))}}
	r := chatFollowNew(m, false)
	if got := chatFollowRender(t, r, true); got != chatFollowEntry("one")+chatFollowEntry("two") {
		t.Fatalf("first output = %q", got)
	}
	if got := chatFollowRender(t, r, false); got != "" {
		t.Fatalf("unchanged output = %q", got)
	}
	m[chatFollowPath].Data = append(m[chatFollowPath].Data, []byte(chatFollowUsage("other", 3))...)
	if got := chatFollowRender(t, r, false); got != "" {
		t.Fatalf("usage-only output = %q", got)
	}
	m[chatFollowPath].Data = append(m[chatFollowPath].Data, []byte(chatFollowUser("three"))...)
	if got := chatFollowRender(t, r, false); got != chatFollowEntry("three") {
		t.Fatalf("append output = %q", got)
	}
	if got := r.interrupt(); got != "" {
		t.Fatalf("interrupt = %q", got)
	}
}

// R-EQFY-NYE9
func TestFollowChatTerminalFooter(t *testing.T) {
	m := fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("one") + chatFollowUsage("message", 2))}}
	r := chatFollowNew(m, true)
	footer := func() string {
		return strings.TrimSuffix(chat.TotalsLine(r.transcript.Usage(), r.transcript.Recorded()), "\n")
	}
	got := chatFollowRender(t, r, true)
	if want := "\x1b[?25l" + chatFollowEntry("one") + footer(); got != want {
		t.Fatalf("first output = %q, want %q", got, want)
	}
	if got := chatFollowRender(t, r, false); got != "" {
		t.Fatalf("unchanged output = %q", got)
	}
	m[chatFollowPath].Data = append(m[chatFollowPath].Data, []byte(chatFollowUsage("other", 3))...)
	got = chatFollowRender(t, r, false)
	if want := "\r\x1b[2K" + footer(); got != want {
		t.Fatalf("usage-only output = %q, want %q", got, want)
	}
	m[chatFollowPath].Data = append(m[chatFollowPath].Data, []byte(chatFollowUser("two"))...)
	got = chatFollowRender(t, r, false)
	if want := "\r\x1b[2K" + chatFollowEntry("two") + footer(); got != want {
		t.Fatalf("entry output = %q, want %q", got, want)
	}
	if got := r.interrupt(); got != "\x1b[?25h\n" {
		t.Fatalf("interrupt = %q", got)
	}
}

// R-ERNV-1Q4Y
func TestFollowChatFailedPassRetriesSameTranscript(t *testing.T) {
	f := &chatFollowFS{root: fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("one") + chatFollowUsage("message", 2))}}}
	r := chatFollowNew(f, true)
	chatFollowRender(t, r, true)
	tr, usage, footer := r.transcript, r.transcript.Usage(), r.footer
	f.root[chatFollowPath].Data = append(f.root[chatFollowPath].Data, []byte(chatFollowUser("two"))...)
	f.deny = chatFollowPath
	if got, err := r.render(false); err == nil || got != "" || r.transcript != tr || tr.Usage() != usage || r.footer != footer {
		t.Fatalf("failed pass = %q, %v; state changed", got, err)
	}
	f.deny = ""
	if got := chatFollowRender(t, r, false); got != "\r\x1b[2K"+chatFollowEntry("two")+footer || r.transcript != tr {
		t.Fatalf("recovery = %q, retained = %v", got, r.transcript == tr)
	}
}

// R-ESVR-FHVN
func TestFollowChatResetAppendsEntriesAndReplacesTotals(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		m := fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("old text longer than replacement") + chatFollowUsage("old", 8))}}
		r := chatFollowNew(m, terminal)
		before := chatFollowRender(t, r, true)
		m[chatFollowPath].Data = []byte(chatFollowUser("new") + chatFollowUsage("new", 1))
		got := chatFollowRender(t, r, false)
		want := chatFollowEntry("new")
		if terminal {
			want = "\r\x1b[2K" + want + strings.TrimSuffix(chat.TotalsLine(chat.Usage{In: 1, Out: 1, Calls: 1}, r.transcript.Recorded()), "\n")
		}
		if got != want || !strings.Contains(before+got, chatFollowEntry("old text longer than replacement")) {
			t.Fatalf("reset terminal=%v = %q, want %q", terminal, got, want)
		}
	}
}

// R-Z69I-M195
func TestFollowChatRediscoversEmptyPath(t *testing.T) {
	f := &chatFollowFS{root: fstest.MapFS{
		"home/dev/.claude/projects":           &fstest.MapFile{Mode: fs.ModeDir},
		"home/dev/.claude/sessions/live.json": &fstest.MapFile{Data: []byte(`{"sessionId":"sample","pid":42,"procStart":"150"}`)},
		"proc/42/stat":                        &fstest.MapFile{Data: []byte("42 (claude) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 150 0 0")},
	}}
	r := chatFollowNew(f, false)
	if got := chatFollowRender(t, r, true); got != "" || r.transcript.Path() != "" {
		t.Fatalf("initial empty transcript = %q, %q", got, r.transcript.Path())
	}
	tr := r.transcript
	f.deny = "home/dev/.claude/projects"
	if got, err := r.render(false); got != "" || err == nil || r.transcript != tr {
		t.Fatalf("failed discovery = %q %v, retained=%v", got, err, r.transcript == tr)
	}
	f.deny = ""
	if got := chatFollowRender(t, r, false); got != "" || r.transcript.Path() != "" || r.transcript == tr {
		t.Fatalf("empty rediscovery = %q, path=%q", got, r.transcript.Path())
	}
	f.root[chatFollowPath] = &fstest.MapFile{Data: []byte(chatFollowUser("created"))}
	if got := chatFollowRender(t, r, false); got != chatFollowEntry("created") || r.transcript.Path() != "/"+chatFollowPath {
		t.Fatalf("discovery = %q, path=%q", got, r.transcript.Path())
	}
	f.opens = nil
	if got := chatFollowRender(t, r, false); got != "" || len(f.opens) != 1 || f.opens[0] != chatFollowPath {
		t.Fatalf("post-discovery pass = %q, reads=%q", got, f.opens)
	}
}

// R-XAG4-BP5J
func TestFollowChatAppendEqualsSnapshotAtEveryPass(t *testing.T) {
	m := fstest.MapFS{chatFollowPath: &fstest.MapFile{}}
	sys := System{Home: "/home/dev", Root: m}
	r := chatFollowNew(m, false)
	var output strings.Builder
	parts := []string{chatFollowUser("one"), `{"type":"user","message":{"content":"par`, `tial"}}`, "\n" + chatFollowUsage("message", 2), chatFollowUser("three")}
	for i, part := range parts {
		m[chatFollowPath].Data = append(m[chatFollowPath].Data, []byte(part)...)
		output.WriteString(chatFollowRender(t, r, i == 0))
		var snapshot, diagnostics bytes.Buffer
		if code := Run([]string{"chat", "claude", "sample"}, sys, &snapshot, &diagnostics); code != ExitSuccess || diagnostics.Len() != 0 {
			t.Fatalf("snapshot pass %d: %d %q", i, code, diagnostics.String())
		}
		at := strings.LastIndex(snapshot.String(), "tokens:")
		if at < 0 || output.String() != snapshot.String()[:at] {
			t.Fatalf("pass %d: follow=%q snapshot=%q", i, output.String(), snapshot.String())
		}
		if (i == 1 || i == 2) && strings.Contains(output.String(), "partial") {
			t.Fatalf("incomplete record written at pass %d", i)
		}
	}
}

// R-XAG4-BP5J
func TestFollowChatRunAppendEqualsSnapshot(t *testing.T) {
	f := &chatFollowScriptRoot{chatFollowFS: &chatFollowFS{root: fstest.MapFS{chatFollowPath: &fstest.MapFile{Data: []byte(chatFollowUser("one"))}}}, interrupt: make(chan struct{})}
	var out, diag bytes.Buffer
	check := func() {
		var snapshot, diagnostics bytes.Buffer
		code := Run([]string{"chat", "claude", "sample"}, System{Home: "/home/dev", Root: f.root}, &snapshot, &diagnostics)
		at := strings.LastIndex(snapshot.String(), "tokens:")
		if code != ExitSuccess || diagnostics.Len() != 0 || at < 0 || out.String() != snapshot.String()[:at] {
			t.Fatalf("pass %d: code=%d follow=%q snapshot=%q diagnostics=%q", f.pass, code, out.String(), snapshot.String(), diagnostics.String())
		}
	}
	parts := []string{`{"type":"user","message":{"content":"par`, `tial"}}`, "\n" + chatFollowUsage("message", 2), chatFollowUser("three")}
	for _, part := range parts {
		f.steps = append(f.steps, func() {
			check()
			f.root[chatFollowPath].Data = append(f.root[chatFollowPath].Data, []byte(part)...)
		})
	}
	changes := make(chan struct{}, len(f.steps))
	for range f.steps {
		changes <- struct{}{}
	}
	w := &chatFollowScriptWatcher{root: f, changes: changes}
	code := Run([]string{"chat", "claude", "sample", "--follow"}, System{Home: "/home/dev", Root: f, Watcher: w, Interrupt: f.interrupt}, &out, &diag)
	if code != ExitSuccess || diag.Len() != 0 || f.pass != len(f.steps) {
		t.Fatalf("follow: code=%d diagnostics=%q passes=%d", code, diag.String(), f.pass)
	}
	check()
}
