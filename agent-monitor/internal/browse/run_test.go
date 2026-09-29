package browse

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

type runtimeTerminal struct {
	keys        chan []byte
	resized     chan struct{}
	cols, rows  int
	sizeCalls   int
	methodCalls int
}

func runtimeNewTerminal(cols, rows int) *runtimeTerminal {
	return &runtimeTerminal{keys: make(chan []byte, 8), resized: make(chan struct{}, 8), cols: cols, rows: rows}
}
func (t *runtimeTerminal) Keys() <-chan []byte      { t.methodCalls++; return t.keys }
func (t *runtimeTerminal) Resized() <-chan struct{} { t.methodCalls++; return t.resized }
func (t *runtimeTerminal) Size() (int, int)         { t.sizeCalls++; t.methodCalls++; return t.cols, t.rows }

type runtimeWatcher struct {
	changes     chan struct{}
	watched     [][]string
	methodCalls int
	onWatch     func([]string)
}

func runtimeNewWatcher() *runtimeWatcher           { return &runtimeWatcher{changes: make(chan struct{}, 8)} }
func (w *runtimeWatcher) Changes() <-chan struct{} { w.methodCalls++; return w.changes }
func (w *runtimeWatcher) Watch(names []string) {
	w.methodCalls++
	w.watched = append(w.watched, append([]string{}, names...))
	if w.onWatch != nil {
		w.onWatch(names)
	}
}

type runtimeWriter struct {
	screens []string
	onWrite func(int)
	err     error
	failAt  int
}

func (w *runtimeWriter) Write(data []byte) (int, error) {
	w.screens = append(w.screens, string(data))
	if w.onWrite != nil {
		w.onWrite(len(w.screens))
	}
	if w.err != nil && (w.failAt == 0 || w.failAt == len(w.screens)) {
		return 0, w.err
	}
	return len(data), nil
}

func runtimeFixture() fstest.MapFS {
	stat := "42 (fixture) S " + strings.Repeat("0 ", 18) + "1\n"
	return fstest.MapFS{
		"home/dev/.claude/sessions/a.json":           &fstest.MapFile{Data: []byte(`{"sessionId":"alpha","pid":42,"cwd":"/work","status":"busy"}`)},
		"home/dev/.claude/sessions/b.json":           &fstest.MapFile{Data: []byte(`{"sessionId":"beta","pid":42,"cwd":"/work","status":"idle"}`)},
		"home/dev/.claude/projects/work/alpha.jsonl": &fstest.MapFile{Data: []byte("{\"type\":\"user\",\"message\":{\"content\":\"hello\"}}\n")},
		"home/dev/.claude/projects/work/beta.jsonl":  &fstest.MapFile{},
		"proc/42/stat": &fstest.MapFile{Data: []byte(stat)},
	}
}
func runtimeConfig(root fs.FS, term Terminal, watcher Watcher) Config {
	return Config{Home: "/home/dev", Root: root, Terminal: term, Watcher: watcher}
}
func runtimeCountCalls(root *runtimeRootSpy, method, name string) int {
	count := 0
	for _, call := range root.calls {
		if call.method == method && call.name == name {
			count++
		}
	}
	return count
}
func runtimeHighlighted(screen string) string {
	start := strings.Index(screen, "\x1b[7m")
	if start < 0 {
		return ""
	}
	s := screen[start+4:]
	end := strings.Index(s, "\x1b[0m")
	if end < 0 {
		return ""
	}
	return s[:end]
}

func TestRunFirstScreenBeforeInputAndCancellation(t *testing.T) {
	// R-2GGK-9PN4 R-WD52-1NBM
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	term.keys <- []byte("j")
	term.resized <- struct{}{}
	watcher.changes <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := &runtimeWriter{onWrite: func(int) {
		if len(term.keys) != 1 || len(term.resized) != 1 || len(watcher.changes) != 1 {
			t.Fatal("event received before first screen")
		}
	}}
	if err := Run(ctx, runtimeConfig(fstest.MapFS{}, term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 1 || len(term.keys) != 1 || len(term.resized) != 1 || len(watcher.changes) != 1 {
		t.Fatalf("screens=%d events=%d/%d/%d", len(writer.screens), len(term.keys), len(term.resized), len(watcher.changes))
	}
}

func TestRunInitialLevelRead(t *testing.T) {
	// R-PKKF-1154
	for _, size := range [][2]int{{120, 40}, {1, 1}} {
		term := runtimeNewTerminal(size[0], size[1])
		root := &runtimeRootSpy{source: runtimeFixture()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		writer := &runtimeWriter{}
		if err := Run(ctx, runtimeConfig(root, term, nil), writer); err != nil {
			t.Fatal(err)
		}
		if runtimeCountCalls(root, "ReadDir", "home/dev/.claude/sessions") != 1 || runtimeCountCalls(root, "ReadDir", "home/dev/.codex/thread-writer-locks") != 1 || runtimeCountCalls(root, "ReadFile", "home/dev/.grok/active_sessions.json") != 1 {
			t.Fatalf("initial harness read: %v", root.calls)
		}
		if size[0] >= MinCols && !strings.Contains(runtimeHighlighted(writer.screens[0]), "Claude Code  2") {
			t.Fatalf("first highlight: %q", runtimeHighlighted(writer.screens[0]))
		}
	}
}

func TestRunSizesEachScreen(t *testing.T) {
	// R-231O-28HH
	term := runtimeNewTerminal(120, 40)
	writer := &runtimeWriter{}
	previousSizeCalls := 0
	writer.onWrite = func(n int) {
		if term.sizeCalls <= previousSizeCalls {
			t.Fatalf("screen %d had no intervening Size call", n)
		}
		previousSizeCalls = term.sizeCalls
		if n == 1 {
			term.cols, term.rows = 60, 5
			term.resized <- struct{}{}
		} else {
			term.keys <- []byte("q")
		}
	}
	if err := Run(context.Background(), runtimeConfig(fstest.MapFS{}, term, nil), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 2 || strings.Count(writer.screens[1], ";1H\x1b[2K") != 5 {
		t.Fatalf("resize screens: %q", writer.screens)
	}
}

func TestRunEachEventWritesExactlyOnce(t *testing.T) {
	// R-H2CC-8AIE
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	writer := &runtimeWriter{}
	writer.onWrite = func(n int) {
		switch n {
		case 1:
			term.keys <- []byte("!")
		case 2:
			term.resized <- struct{}{}
		case 3:
			watcher.changes <- struct{}{}
		case 4:
			term.keys <- []byte("q")
		default:
			t.Fatal("extra screen")
		}
	}
	if err := Run(context.Background(), runtimeConfig(fstest.MapFS{}, term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 4 {
		t.Fatalf("screens=%d", len(writer.screens))
	}
	for _, screen := range writer.screens[1:] {
		if screen != writer.screens[0] {
			t.Fatal("unchanged state screen differs")
		}
	}
}

func TestRunKeyValuesApplyInOrder(t *testing.T) {
	// R-DAHL-AW23
	for _, value := range []string{"jj", "\x1b[B\x1b[B"} {
		term := runtimeNewTerminal(120, 40)
		writer := &runtimeWriter{onWrite: func(n int) {
			if n == 1 {
				term.keys <- []byte(value)
			} else {
				term.keys <- []byte("q")
			}
		}}
		if err := Run(context.Background(), runtimeConfig(fstest.MapFS{}, term, nil), writer); err != nil {
			t.Fatal(err)
		}
		if len(writer.screens) != 2 || !strings.HasPrefix(runtimeHighlighted(writer.screens[1]), "Grok") {
			t.Fatalf("%q screens: %q", value, writer.screens)
		}
	}
	term := runtimeNewTerminal(120, 40)
	root := &runtimeRootSpy{source: runtimeFixture()}
	writer := &runtimeWriter{onWrite: func(int) { term.keys <- []byte("jql") }}
	if err := Run(context.Background(), runtimeConfig(root, term, nil), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 1 || runtimeCountCalls(root, "ReadDir", "home/dev/.codex/thread-writer-locks") != 1 {
		t.Fatal("key after quit acted")
	}
}

func TestRunQuitFromEveryLevel(t *testing.T) {
	// R-DBPH-ONSS
	for _, prefix := range []string{"", "l", "ll", "lll", "jjl", "jl"} {
		for _, quit := range []string{"q", "\x1b", "\x03"} {
			for _, small := range []bool{false, true} {
				term := runtimeNewTerminal(120, 40)
				writer := &runtimeWriter{}
				writer.onWrite = func(n int) {
					switch {
					case n == 1:
						if prefix == "" {
							if small {
								term.cols, term.rows = 1, 1
								term.resized <- struct{}{}
							} else {
								term.keys <- []byte(quit)
							}
						} else {
							term.keys <- []byte(prefix)
						}
					case n == 2 && small && prefix != "":
						term.cols, term.rows = 1, 1
						term.resized <- struct{}{}
					default:
						term.keys <- []byte(quit)
					}
				}
				if err := Run(context.Background(), runtimeConfig(runtimeFixture(), term, nil), writer); err != nil {
					t.Fatal(err)
				}
				expected := 1
				if prefix != "" {
					expected++
				}
				if small {
					expected++
				}
				if len(writer.screens) != expected {
					t.Fatalf("prefix=%q quit=%q small=%v screens=%d want=%d", prefix, quit, small, len(writer.screens), expected)
				}
			}
		}
	}
}

func TestRunTooSmallKeysDoNotAct(t *testing.T) {
	// R-DCXE-2FJH
	term := runtimeNewTerminal(120, 40)
	root := &runtimeRootSpy{source: runtimeFixture()}
	writer := &runtimeWriter{}
	writer.onWrite = func(n int) {
		switch n {
		case 1:
			term.keys <- []byte("j")
		case 2:
			term.cols, term.rows = 1, 1
			term.resized <- struct{}{}
		case 3:
			term.keys <- []byte("jGlh")
		case 4:
			term.cols, term.rows = 120, 40
			term.resized <- struct{}{}
		case 5:
			term.keys <- []byte("q")
		}
	}
	if err := Run(context.Background(), runtimeConfig(root, term, nil), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 5 || writer.screens[1] != writer.screens[4] {
		t.Fatal("too-small keys changed state")
	}
	if runtimeCountCalls(root, "ReadDir", "home/dev/.claude/sessions") != 1 || runtimeCountCalls(root, "ReadDir", "home/dev/.codex/thread-writer-locks") != 1 {
		t.Fatal("too-small navigation caused a read")
	}
}

func TestRunReadTiming(t *testing.T) {
	// R-W4LR-D94R
	root := &runtimeRootSpy{source: runtimeFixture()}
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	writer := &runtimeWriter{}
	writer.onWrite = func(n int) {
		count := runtimeCountCalls(root, "ReadDir", "home/dev/.claude/sessions")
		switch n {
		case 1:
			if count != 1 {
				t.Fatal(count)
			}
			term.keys <- []byte("lj")
		case 2:
			if count != 2 || !strings.Contains(runtimeHighlighted(writer.screens[n-1]), "beta") {
				t.Fatalf("open/read/key order count=%d highlight=%q", count, runtimeHighlighted(writer.screens[n-1]))
			}
			term.resized <- struct{}{}
		case 3:
			if count != 2 {
				t.Fatal("resize read")
			}
			term.keys <- []byte("!")
		case 4:
			if count != 2 {
				t.Fatal("ignored key read")
			}
			watcher.changes <- struct{}{}
		case 5:
			if count != 3 {
				t.Fatal("watcher did not read")
			}
			term.keys <- []byte("h")
		case 6:
			if count != 4 {
				t.Fatal("back did not read")
			}
			term.keys <- []byte("h")
		case 7:
			if count != 4 {
				t.Fatal("harness back read")
			}
			term.keys <- []byte("q")
		}
	}
	if err := Run(context.Background(), runtimeConfig(root, term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 7 {
		t.Fatalf("screens %d", len(writer.screens))
	}
}

func TestRunWatchAfterSuccessfulScreenAndLastRead(t *testing.T) {
	// R-W89G-IKCU
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	writer := &runtimeWriter{}
	watcher.onWatch = func(_ []string) {
		if len(writer.screens) != len(watcher.watched) {
			t.Fatalf("watch occurred before screen: %d/%d", len(writer.screens), len(watcher.watched))
		}
	}
	writer.onWrite = func(n int) {
		switch n {
		case 1:
			term.keys <- []byte("ll")
		case 2:
			term.keys <- []byte("hhl")
		case 3:
			term.keys <- []byte("!")
		case 4:
			term.keys <- []byte("q")
		}
	}
	if err := Run(context.Background(), runtimeConfig(runtimeFixture(), term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"home/dev/.claude/projects", "home/dev/.claude/projects/work", "home/dev/.claude/sessions", "home/dev/.codex", "home/dev/.grok"},
		{"home/dev/.claude/projects", "home/dev/.claude/projects/work", "home/dev/.claude/projects/work/alpha", "home/dev/.claude/sessions"},
		{"home/dev/.claude/projects", "home/dev/.claude/projects/work", "home/dev/.claude/sessions"},
	}
	if len(watcher.watched) != len(want) {
		t.Fatalf("watches: %v", watcher.watched)
	}
	for i, names := range want {
		if !slices.Equal(watcher.watched[i], names) {
			t.Fatalf("watch %d=%v want=%v", i, watcher.watched[i], names)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	emptyWatcher := runtimeNewWatcher()
	emptyWatcher.onWatch = func(names []string) {
		if len(names) != 0 {
			t.Fatalf("empty set: %#v", names)
		}
	}
	if err := Run(ctx, Config{Home: "/proc", Root: fstest.MapFS{}, Watcher: emptyWatcher}, &runtimeWriter{}); err != nil {
		t.Fatal(err)
	}
	if len(emptyWatcher.watched) != 1 {
		t.Fatal("initial empty watch missing")
	}
}

func TestRunNilWatcherEquivalent(t *testing.T) {
	// R-W9HC-WC3J
	var screens [][]string
	for _, watcher := range []Watcher{nil, runtimeNewWatcher()} {
		term := runtimeNewTerminal(120, 40)
		writer := &runtimeWriter{onWrite: func(n int) {
			if n == 1 {
				term.keys <- []byte("jl")
			} else {
				term.keys <- []byte("q")
			}
		}}
		if err := Run(context.Background(), runtimeConfig(fstest.MapFS{}, term, watcher), writer); err != nil {
			t.Fatal(err)
		}
		screens = append(screens, writer.screens)
	}
	if !slices.Equal(screens[0], screens[1]) {
		t.Fatal("nil watcher differs")
	}
}

func TestRunNilTerminalEquivalent(t *testing.T) {
	// R-WAP9-A3U8
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, b := &runtimeWriter{}, &runtimeWriter{}
	if err := Run(ctx, runtimeConfig(fstest.MapFS{}, nil, nil), a); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, runtimeConfig(fstest.MapFS{}, runtimeNewTerminal(0, 0), nil), b); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.screens, b.screens) || len(a.screens) != 1 {
		t.Fatal("nil terminal differs")
	}
}

func TestRunCancellationDuringEventFinishesScreenAndWatch(t *testing.T) {
	// R-WECY-FF2B
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	ctx, cancel := context.WithCancel(context.Background())
	writer := &runtimeWriter{}
	writer.onWrite = func(n int) {
		if n == 1 {
			term.keys <- []byte("ll")
		} else {
			cancel()
		}
	}
	if err := Run(ctx, runtimeConfig(runtimeFixture(), term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 2 || !strings.Contains(writer.screens[1], "agent-monitor › Claude Code › alpha") || len(watcher.watched) != 2 {
		t.Fatalf("screens %d watches %v", len(writer.screens), watcher.watched)
	}
}

func TestRunClosedKeysEndsImmediately(t *testing.T) {
	// R-WFKU-T6T0
	term := runtimeNewTerminal(120, 40)
	root := &runtimeRootSpy{source: runtimeFixture()}
	watcher := runtimeNewWatcher()
	sizeCallsAtClose, rootCallsAtClose := 0, 0
	writer := &runtimeWriter{onWrite: func(int) {
		sizeCallsAtClose = term.sizeCalls
		rootCallsAtClose = len(root.calls)
		close(term.keys)
	}}
	if err := Run(context.Background(), runtimeConfig(root, term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 1 || term.sizeCalls != sizeCallsAtClose || len(watcher.watched) != 1 || len(root.calls) != rootCallsAtClose {
		t.Fatal("work after keys close")
	}
}

func TestRunClosedAuxiliaryChannelsRemoved(t *testing.T) {
	// R-WGSR-6YJP
	// Each channel first supplies a real event and is then closed. Surviving
	// input must continue producing screens; closures produce neither reads
	// nor screens and cannot end browsing.
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	root := &runtimeRootSpy{source: runtimeFixture()}
	writer := &runtimeWriter{}
	writer.onWrite = func(n int) {
		switch n {
		case 1:
			term.resized <- struct{}{}
			close(term.resized)
		case 2:
			watcher.changes <- struct{}{}
			close(watcher.changes)
		case 3:
			term.keys <- []byte("j")
		case 4:
			term.keys <- []byte("q")
		default:
			t.Fatal("closed channel caused a screen")
		}
	}
	if err := Run(context.Background(), runtimeConfig(root, term, watcher), writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.screens) != 4 || runtimeCountCalls(root, "ReadDir", "home/dev/.claude/sessions") != 2 {
		t.Fatalf("screens=%d reads=%v", len(writer.screens), root.calls)
	}
	if writer.screens[0] != writer.screens[1] || writer.screens[1] != writer.screens[2] || !strings.HasPrefix(runtimeHighlighted(writer.screens[3]), "Codex") {
		t.Fatal("closed auxiliary channels disrupted surviving input")
	}
}

func TestRunWriteFailureReturnsIdenticalErrorAndStops(t *testing.T) {
	// R-WI0N-KQAE
	for _, failAt := range []int{1, 2} {
		root := &runtimeRootSpy{source: runtimeFixture()}
		term := runtimeNewTerminal(120, 40)
		watcher := runtimeNewWatcher()
		sentinel := errors.New("writer failed")
		writer := &runtimeWriter{err: sentinel, failAt: failAt}
		var rootCalls, terminalCalls, watcherCalls int
		writer.onWrite = func(n int) {
			if n == failAt {
				rootCalls = len(root.calls)
				terminalCalls = term.methodCalls
				watcherCalls = watcher.methodCalls
				term.keys <- []byte("lll")
			} else {
				term.keys <- []byte("ll")
			}
		}
		if err := Run(context.Background(), runtimeConfig(root, term, watcher), writer); reflect.ValueOf(err) != reflect.ValueOf(sentinel) {
			t.Fatalf("error=%v", err)
		}
		if len(writer.screens) != failAt || len(root.calls) != rootCalls || term.methodCalls != terminalCalls || watcher.methodCalls != watcherCalls || len(term.keys) != 1 {
			t.Fatal("action after failed write")
		}
	}
}

func TestRunReturnPathsLeaveNoActivity(t *testing.T) {
	// R-WJ8J-YI13
	for _, ending := range []string{"quit", "closed keys", "cancel"} {
		root := &runtimeRootSpy{source: runtimeFixture()}
		term := runtimeNewTerminal(120, 40)
		watcher := runtimeNewWatcher()
		ctx, cancel := context.WithCancel(context.Background())
		writer := &runtimeWriter{onWrite: func(int) {
			switch ending {
			case "quit":
				term.keys <- []byte("q")
			case "closed keys":
				close(term.keys)
			case "cancel":
				cancel()
			}
		}}
		if err := Run(ctx, runtimeConfig(root, term, watcher), writer); err != nil {
			t.Fatalf("%s: %v", ending, err)
		}
		before := [4]int{len(root.calls), term.methodCalls, watcher.methodCalls, len(writer.screens)}
		term.resized <- struct{}{}
		watcher.changes <- struct{}{}
		if ending != "closed keys" {
			term.keys <- []byte("l")
		}
		after := [4]int{len(root.calls), term.methodCalls, watcher.methodCalls, len(writer.screens)}
		if before != after || len(term.resized) != 1 || len(watcher.changes) != 1 {
			t.Fatal("post-return activity")
		}
		cancel()
	}
}

func TestRunFilesystemReadOnly(t *testing.T) {
	// R-WKGG-C9RS
	root := &runtimeReadOnlyRoot{runtimeRootSpy: &runtimeRootSpy{source: runtimeFixture()}}
	term := runtimeNewTerminal(120, 40)
	watcher := runtimeNewWatcher()
	writer := &runtimeWriter{}
	writer.onWrite = func(n int) {
		switch n {
		case 1:
			term.keys <- []byte("lll")
		case 2:
			watcher.changes <- struct{}{}
		case 3:
			term.keys <- []byte("hhhq")
		}
	}
	if err := Run(context.Background(), runtimeConfig(root, term, watcher), writer); err != nil {
		t.Fatal(err)
	}
}
