package browse

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

func menuHarnessInjection(t *testing.T) {
	t.Helper()
	old := browserHarnesses
	t.Cleanup(func() { browserHarnesses = old })
}
func menuReadState(t *testing.T) *browserState {
	t.Helper()
	menuHarnessInjection(t)
	return newBrowserState(Config{Home: "/fixture/home"}, fstest.MapFS{})
}

// R-VR6V-5RZ4
func TestHarnessMenuRead(t *testing.T) {
	root := fstest.MapFS{"home/dev/.grok/active_sessions.json": {Data: []byte("broken")}}
	b := newBrowserState(Config{Home: "/home/dev"}, root)
	b.read()
	want := []string{"Claude Code  0", "Codex        0", "Grok         -"}
	for i, row := range b.menus[0].rows {
		if row.line != want[i] || row.harness != i {
			t.Fatalf("row %+v", row)
		}
	}
	menuHarnessInjection(t)
	calls := [3]int{}
	for i := range browserHarnesses {
		browserHarnesses[i].list = func(f fs.FS, home string) ([]session.Session, error) {
			calls[i]++
			if !reflect.DeepEqual(f, root) || home != "/home/dev" {
				t.Fatal("dispatch arguments")
			}
			if i == 2 && calls[i] > 1 {
				return nil, errors.New("failed")
			}
			return make([]session.Session, i+1), nil
		}
	}
	b.read()
	for i, row := range b.menus[0].rows {
		if row.line != browserHarnesses[i].name+browserHarnesses[i].padding+fmt.Sprint(i+1) {
			t.Fatal(row.line)
		}
	}
	b.read()
	if calls != [3]int{2, 2, 2} || b.menus[0].rows[2].line != "Grok         -" {
		t.Fatalf("calls %v rows %+v", calls, b.menus[0].rows)
	}
}

// R-VTMN-XBGI R-VYI9-GEFA
func TestSessionReadAndFirstHighlight(t *testing.T) {
	b := menuReadState(t)
	b.level = 1
	b.harness = 1
	b.menus[1] = newMenu()
	calls := 0
	sessions := []session.Session{{ID: "later", Started: time.Unix(2, 0), HasStarted: true, Title: strings.Repeat("wide", 12)}, {ID: "earlier", Started: time.Unix(1, 0), HasStarted: true}}
	browserHarnesses[1].list = func(f fs.FS, home string) ([]session.Session, error) {
		calls++
		if !reflect.DeepEqual(f, b.root) || home != b.cfg.Home {
			t.Fatal("list arguments")
		}
		return sessions, nil
	}
	b.read()
	ordered := session.OrderByStart(sessions)
	header, lines := session.TableRows(ordered)
	m := b.menus[1]
	if calls != 1 || m.highlighted != 0 || !reflect.DeepEqual(m.topLines, []string{header}) {
		t.Fatalf("session %+v calls %d", m, calls)
	}
	for i, row := range m.rows {
		if row.line != lines[i] || row.session != ordered[i] || row.key.id != ordered[i].ID {
			t.Fatalf("row %+v", row)
		}
	}
	body := b.view(120, 5).body
	if body[1].text != lines[0] {
		t.Fatal("table widths changed")
	}
	sessions = nil
	b.read()
	if m.highlighted != -1 || len(m.rows) != 0 || !reflect.DeepEqual(m.topLines, []string{sessionHeader(), "no live sessions"}) {
		t.Fatalf("empty %+v", m)
	}
	b.menus[1] = newMenu()
	browserHarnesses[1].list = func(fs.FS, string) ([]session.Session, error) { return nil, errors.New("failure") }
	b.read()
	if b.menus[1].highlighted != -1 {
		t.Fatal("failure highlighted")
	}
}
func sessionHeader() string { header, _ := session.TableRows(nil); return header }

// R-VUUK-B377
func TestAgentReadDrawnNodeIdentity(t *testing.T) {
	b := menuReadState(t)
	b.level = 2
	b.harness = 2
	b.session = session.Session{ID: "root"}
	b.menus[2] = newMenu()
	calls := 0
	tr := tree.Tree{Root: tree.Node{ID: "root"}, Subagents: []tree.Node{{ID: "z"}, {ID: "kid", Parent: "a"}, {ID: "a", HasStarted: true, Started: time.Unix(1, 0)}, {ID: "a", Label: "duplicate"}, {ID: "root"}, {ID: "cycle-a", Parent: "cycle-b"}, {ID: "cycle-b", Parent: "cycle-a"}, {ID: "orphan", Parent: "missing"}}}
	browserHarnesses[2].tree = func(root fs.FS, home, id string) (tree.Tree, error) {
		calls++
		if !reflect.DeepEqual(root, b.root) || home != b.cfg.Home || id != "root" {
			t.Fatal("tree arguments")
		}
		return tr, nil
	}
	b.read()
	lines := strings.Split(strings.TrimSuffix(tree.Draw(tr, false), "\n"), "\n")
	want := []string{"root", "a", "kid", "cycle-a", "cycle-b", "orphan", "z"}
	m := b.menus[2]
	if calls != 1 || !reflect.DeepEqual(menuIDs(m), want) || m.highlighted != 0 {
		t.Fatalf("nodes %v calls %d", menuIDs(m), calls)
	}
	for i, row := range m.rows {
		if row.line != lines[i] || row.node.ID != want[i] {
			t.Fatalf("row %+v", row)
		}
	}
	if !reflect.DeepEqual(m.bottomLines, lines[len(want):]) {
		t.Fatalf("bottom %v", m.bottomLines)
	}
}

// R-W0Y2-7XWO
func TestLevelFailureLines(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		for _, c := range []struct {
			err  error
			want string
		}{
			{fmt.Errorf("wrapped: %w", &session.ReadError{Path: "/some path", Err: errors.New("not valid JSON")}), "cannot read " + quote.Field("/some path") + ": not valid JSON"},
			{fmt.Errorf("wrapped: %w", tree.ErrNotFound), "no " + h + " session '" + quote.Arg("session'\n") + "'"},
			{fmt.Errorf("wrapped: %w", chat.ErrAgentNotFound), "no " + h + " agent '" + quote.Arg("agent'\n") + "' in session '" + quote.Arg("session'\n") + "'"},
		} {
			if got := failureLine(c.err, h, "session'\n", "agent'\n"); got != c.want {
				t.Fatalf("%q want %q", got, c.want)
			}
		}
	}
}

// R-4T7R-F6JV R-0B8T-C4QU
func TestMenuReadFailureAndBackRetention(t *testing.T) {
	b := menuReadState(t)
	b.read()
	b.apply(keyOpen, 120, 5)
	fail := false
	browserHarnesses[0].list = func(fs.FS, string) ([]session.Session, error) {
		if fail {
			return nil, &session.ReadError{Path: "/registry", Err: fs.ErrPermission}
		}
		return []session.Session{{ID: "one"}, {ID: "two"}, {ID: "three"}}, nil
	}
	b.read()
	b.apply(keyLast, 120, 5)
	b.view(120, 5)
	saved := *b.menus[1]
	saved.rows = append([]menuRow(nil), saved.rows...)
	b.apply(keyOpen, 120, 5)
	fail = true
	b.apply(keyBack, 120, 5)
	b.read()
	if !reflect.DeepEqual(*b.menus[1], saved) {
		t.Fatalf("back/read changed retained menu: %+v want %+v", b.menus[1], saved)
	}
	if got := b.view(120, 5); got.body[1].text != saved.rows[2].line {
		t.Fatalf("failure replaced rows: %+v", got.body)
	}
	b.level = 2
	b.menus[2] = menuFixture("root", "child")
	b.menus[2].bottomLines = []string{"", "key"}
	before := *b.menus[2]
	browserHarnesses[0].tree = func(fs.FS, string, string) (tree.Tree, error) { return tree.Tree{}, tree.ErrNotFound }
	b.read()
	if !reflect.DeepEqual(*b.menus[2], before) {
		t.Fatal("agent failed read changed content")
	}
}

// R-DJ0V-ZA8Y R-UXR4-CZI9
func TestOpeningAndBackLevels(t *testing.T) {
	b := menuReadState(t)
	b.read()
	b.apply(keyDown, 120, 40)
	if !b.apply(keyOpen, 120, 40) || b.level != 1 || b.harness != 1 {
		t.Fatal("harness open")
	}
	b.menus[1] = menuFixture("one", "two")
	b.menus[1].rows[1].session = session.Session{ID: "two"}
	b.menus[1].highlighted = 1
	if !b.apply(keyOpen, 120, 40) || b.level != 2 || b.session.ID != "two" {
		t.Fatal("session open")
	}
	b.menus[2] = menuFixture("root", "child")
	b.menus[2].rows[1].node = tree.Node{ID: "child"}
	b.menus[2].highlighted = 1
	if !b.apply(keyOpen, 120, 40) || b.level != 3 || b.node.ID != "child" || b.chat == nil {
		t.Fatal("agent open")
	}
	for _, level := range []int{2, 1, 0} {
		if !b.apply(keyBack, 120, 40) || b.level != level || b.menus[level].highlighted != 1 {
			t.Fatalf("back level %d", level)
		}
	}
	if b.apply(keyBack, 120, 40) || b.level != 0 {
		t.Fatal("harness back")
	}
}

// R-2CSV-4EF1 R-2E0R-I65Q R-DK8S-D1ZN R-W25Y-LPND
func TestLevelBreadcrumbHintFailureAndChatKeys(t *testing.T) {
	b := menuReadState(t)
	b.read()
	b.harness = 2
	b.session = session.Session{ID: "session id"}
	b.node = tree.Node{ID: "session id"}
	for level := 0; level < 4; level++ {
		b.level = level
		if level > 0 && level < 3 {
			b.menus[level] = newMenu()
			b.menus[level].failure = "failure"
		}
		if level == 3 {
			b.chat = newChatState()
			b.failure = strings.Repeat("f", 85)
		}
		view := b.view(40, 5)
		crumb := "agent-monitor"
		if level > 0 {
			crumb += " › Grok"
		}
		if level > 1 {
			crumb += " › " + quote.Field(b.session.ID)
		}
		if level > 2 {
			crumb += " › " + quote.Field(b.node.ID)
		}
		if view.breadcrumb != crumb {
			t.Fatal(view.breadcrumb)
		}
		hint := "↑↓ move  → open  q quit"
		if level == 1 || level == 2 {
			hint = "↑↓ move  → open  ← back  q quit"
		}
		if level == 3 {
			hint = "↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [following]"
		}
		if view.hint != hint {
			t.Fatal(view.hint)
		}
		if level == 1 || level == 2 {
			if len(view.body) != 1 || view.body[0].text != "failure" || view.body[0].highlighted {
				t.Fatal(view.body)
			}
		}
		if level == 3 {
			if len(view.body) != 2 || view.body[0].text != strings.Repeat("f", 40) || view.body[1].text != strings.Repeat("f", 40) {
				t.Fatal(view.body)
			}
			before := b.view(40, 5)
			for _, k := range []key{keyOpen, keyUp, keyDown, keyPageUp, keyPageDown, keyFirst, keyLast} {
				if b.apply(k, 40, 5) || !reflect.DeepEqual(b.view(40, 5), before) {
					t.Fatalf("failed chat key %d", k)
				}
			}
		}
	}
	// A successful empty transcript is content and uses the chat hint's state.
	b.chat = newChatState()
	if err := b.chat.read(b.root, func(fs.FS) (*chat.Transcript, []chat.Entry, error) {
		return chat.NewTranscript("", chat.Recorded{}, nil), []chat.Entry{{Kind: chat.KindUser, Text: strings.Repeat("a", 400)}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	b.view(40, 5)
	b.apply(keyFirst, 40, 5)
	paused := b.view(40, 5)
	if !strings.HasSuffix(paused.hint, "[paused]") {
		t.Fatal(paused.hint)
	}
	if b.apply(keyOpen, 40, 5) || !reflect.DeepEqual(b.view(40, 5), paused) {
		t.Fatal("chat open changed state")
	}
	b.apply(keyLast, 40, 5)
	if !strings.HasSuffix(b.view(40, 5).hint, "[following]") {
		t.Fatal("chat last not dispatched")
	}
}

// R-2K49-F0V7 R-4RZV-1ET6
func TestColorAndResizeState(t *testing.T) {
	b := menuReadState(t)
	b.read()
	first := b.view(120, 5)
	b.cfg.Color = true
	if !reflect.DeepEqual(first, b.view(120, 5)) {
		t.Fatal("harness color")
	}
	b.apply(keyDown, 120, 5)
	level, harness := b.level, b.harness
	highlight := b.menus[0].highlighted
	renderScreen(screenView{}, 10, 2)
	b.view(120, 40)
	if b.level != level || b.harness != harness || b.menus[0].highlighted != highlight {
		t.Fatal("size changed selection")
	}
	b.level = 1
	b.menus[1] = menuFixture("session")
	one := b.view(120, 40)
	b.cfg.Color = false
	if !reflect.DeepEqual(one, b.view(120, 40)) {
		t.Fatal("session color")
	}
	b.level = 2
	b.session = session.Session{ID: "root"}
	b.menus[2] = newMenu()
	tr := tree.Tree{Root: tree.Node{ID: "root", Status: tree.StatusWorking}}
	browserHarnesses[0].tree = func(fs.FS, string, string) (tree.Tree, error) { return tr, nil }
	b.harness = 0
	for _, color := range []bool{false, true} {
		b.cfg.Color = color
		b.read()
		lines := strings.Split(strings.TrimSuffix(tree.Draw(tr, color), "\n"), "\n")
		if b.menus[2].rows[0].line != lines[0] || !reflect.DeepEqual(b.menus[2].bottomLines, lines[1:]) {
			t.Fatal("draw color not passed")
		}
	}
	b.level = 3
	b.node = tree.Node{ID: "root"}
	b.chat = newChatState()
	b.failure = "failed"
	b.cfg.Color = false
	before := b.view(120, 40)
	b.cfg.Color = true
	if !reflect.DeepEqual(before, b.view(120, 40)) {
		t.Fatal("chat color")
	}
	b.apply(keyOpen, 120, 40)
	renderScreen(screenView{}, 1, 1)
	if b.level != 3 || b.harness != 0 || b.session.ID != "root" || b.node.ID != "root" || !b.chat.following() {
		t.Fatal("resize chat changed state")
	}
}

// R-4RZV-1ET6
func TestResizedAndTooSmallKeepMenuSelection(t *testing.T) {
	term := &menuResizeTerminal{keys: make(chan []byte, 1), resized: make(chan struct{}, 1), sizes: [][2]int{{120, 5}, {120, 5}, {10, 2}, {120, 40}}}
	writer := &menuResizeWriter{term: term}
	if err := Run(context.Background(), Config{Root: fstest.MapFS{}, Home: "/fixture/home", Terminal: term}, writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 4 || writer.screens[2] != "\x1b[H\x1b[2Jterminal too small" {
		t.Fatalf("screens %v", writer.screens)
	}
	for _, i := range []int{1, 3} {
		if !strings.Contains(writer.screens[i], "\x1b[7mCodex        0\x1b[0m") || strings.Contains(writer.screens[i], "\x1b[7mClaude Code") {
			t.Fatalf("screen %d: %q", i, writer.screens[i])
		}
	}
}

type menuResizeTerminal struct {
	keys    chan []byte
	resized chan struct{}
	sizes   [][2]int
	index   int
}

func (t *menuResizeTerminal) Keys() <-chan []byte      { return t.keys }
func (t *menuResizeTerminal) Resized() <-chan struct{} { return t.resized }
func (t *menuResizeTerminal) Size() (int, int) {
	size := t.sizes[t.index]
	t.index++
	return size[0], size[1]
}

type menuResizeWriter struct {
	term    *menuResizeTerminal
	screens []string
}

func (w *menuResizeWriter) Write(p []byte) (int, error) {
	w.screens = append(w.screens, string(p))
	switch len(w.screens) {
	case 1:
		w.term.keys <- []byte("j")
	case 2, 3:
		w.term.resized <- struct{}{}
	case 4:
		w.term.keys <- []byte("q")
	}
	return len(p), nil
}

// R-4T7R-F6JV R-W25Y-LPND
func TestChatReadFailurePreservesContent(t *testing.T) {
	b := menuReadState(t)
	b.level = 3
	b.chat = newChatState()
	b.session = session.Session{ID: "session"}
	b.node = tree.Node{ID: "agent"}
	calls := 0
	browserHarnesses[0].chat = func(root fs.FS, home, sid, aid string) (*chat.Transcript, []chat.Entry, error) {
		calls++
		if !reflect.DeepEqual(root, b.root) || home != b.cfg.Home || sid != "session" || aid != "agent" {
			t.Fatal("chat dispatch")
		}
		if calls != 2 {
			return nil, nil, &session.ReadError{Path: fmt.Sprint("/failure", calls), Err: fs.ErrPermission}
		}
		return chat.NewTranscript("", chat.Recorded{}, nil), []chat.Entry{{Kind: chat.KindUser, Text: strings.Repeat("text", 100)}}, nil
	}
	b.read()
	if b.chat.hasContent() || !b.chat.following() || b.failure != "cannot read /failure1: permission denied" {
		t.Fatalf("first failure %q", b.failure)
	}
	b.read()
	if !b.chat.hasContent() {
		t.Fatal("success not content")
	}
	b.view(40, 5)
	b.apply(keyFirst, 40, 5)
	before := b.view(40, 5)
	b.read()
	after := b.view(40, 5)
	if calls != 3 || !reflect.DeepEqual(before, after) || b.chat.following() {
		t.Fatalf("failed read changed paused chat: %+v %+v", before, after)
	}
}

// R-UWJ7-Z7RK R-UXR4-CZI9
func TestBackReadRestoresFromOpeningSnapshot(t *testing.T) {
	for _, c := range []struct {
		before, after []string
		opened        int
		want          string
	}{
		{[]string{"a", "x", "b"}, []string{"a", "b"}, 1, "a"},
		{[]string{"a", "y", "x"}, []string{"a", "b"}, 2, "a"},
		{[]string{"x", "b"}, []string{"b"}, 0, "b"},
		{[]string{"x", "b"}, nil, 0, ""},
		{[]string{"a", "x", "b"}, []string{"b", "x", "a"}, 1, "x"},
	} {
		t.Run(strings.Join(c.before, ",")+"->"+strings.Join(c.after, ","), func(t *testing.T) {
			menuHarnessInjection(t)
			makeSessions := func(ids []string) []session.Session {
				items := make([]session.Session, len(ids))
				for i, id := range ids {
					items[i] = session.Session{ID: id, HasStarted: true, Started: time.Unix(int64(i+1), 0)}
				}
				return items
			}
			items := makeSessions(c.before)
			browserHarnesses[0].list = func(fs.FS, string) ([]session.Session, error) { return items, nil }
			browserHarnesses[0].tree = func(fs.FS, string, string) (tree.Tree, error) {
				items = makeSessions(c.after)
				return tree.Tree{Root: tree.Node{ID: c.before[c.opened]}}, nil
			}
			keys := make(chan []byte, 4)
			keys <- []byte("l")
			keys <- []byte(strings.Repeat("j", c.opened) + "l")
			keys <- []byte("h")
			keys <- []byte("q")
			writer := &renderScreenRecorder{}
			if err := Run(context.Background(), Config{Home: "/fixture/home", Root: fstest.MapFS{}, Terminal: &renderRunTerminal{cols: 120, rows: 40, keys: keys}}, writer); err != nil {
				t.Fatal(err)
			}
			screen := writer.screens[len(writer.screens)-1]
			if c.want == "" {
				if strings.Contains(screen, "\x1b[7m") {
					t.Fatalf("empty back menu highlighted: %q", screen)
				}
			} else {
				highlighted := "\x1b[7m" + c.want + " "
				if !strings.Contains(screen, highlighted) {
					t.Fatalf("back should highlight %q: %q", c.want, screen)
				}
			}
		})
	}
}
