package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/browse"
)

type browserConsole struct {
	cols, rows int
	keys       chan []byte
	resized    chan struct{}
	calls      []string
	events     *[]string
}

func (c *browserConsole) mark(s string) {
	c.calls = append(c.calls, s)
	if c.events != nil {
		*c.events = append(*c.events, s)
	}
}
func (c *browserConsole) Size() (int, int)         { c.mark("size"); return c.cols, c.rows }
func (c *browserConsole) Raw() func()              { c.mark("raw"); return func() { c.mark("restore") } }
func (c *browserConsole) Keys() <-chan []byte      { c.mark("keys"); return c.keys }
func (c *browserConsole) Resized() <-chan struct{} { c.mark("resized"); return c.resized }
func browseFixture() (System, *browserConsole) {
	c := &browserConsole{cols: browse.MinCols, rows: browse.MinRows, keys: make(chan []byte, 8), resized: make(chan struct{}, 4)}
	return System{Home: "/home/dev", Root: fstest.MapFS{}, Terminal: true, StdinTerminal: true, Console: c}, c
}

// R-KOOF-CGMR R-KPWB-Q8DG
func TestConsoleContract(t *testing.T) {
	typ := reflect.TypeOf((*Console)(nil)).Elem()
	if typ.NumMethod() != 4 {
		t.Fatalf("method count %d", typ.NumMethod())
	}
	for name, want := range map[string]reflect.Type{
		"Raw": reflect.TypeOf((func() func())(nil)), "Keys": reflect.TypeOf((func() <-chan []byte)(nil)),
		"Size": reflect.TypeOf((func() (int, int))(nil)), "Resized": reflect.TypeOf((func() <-chan struct{})(nil)),
	} {
		got, ok := typ.MethodByName(name)
		if !ok || got.Type != want {
			t.Errorf("%s = %v", name, got.Type)
		}
	}
	var c Console = &browserConsole{}
	var term browse.Terminal = c
	if term != c {
		t.Fatal("terminal identity lost")
	}
	var w Watcher = &followTestWatcher{}
	var bw browse.Watcher = w
	if bw != w {
		t.Fatal("watcher identity lost")
	}
}

// R-KSC4-HRUU R-KTK0-VJLJ R-XE49-WVTR
func TestBrowseEligibility(t *testing.T) {
	for _, outTerminal := range []bool{false, true} {
		for _, inTerminal := range []bool{false, true} {
			sys, c := browseFixture()
			sys.Terminal = outTerminal
			sys.StdinTerminal = inTerminal
			c.keys <- []byte("q")
			code, out, diag := runRecorded(nil, sys, nil, nil)
			if code != ExitSuccess || len(diag.writes) != 0 {
				t.Fatalf("bare run %d %q", code, diag.writes)
			}
			if outTerminal && inTerminal {
				if len(c.calls) == 0 || out.writes[0] != enterBrowser {
					t.Fatal("browser not opened")
				}
			} else if len(c.calls) != 0 || len(out.writes) != 1 || out.writes[0] != Usage {
				t.Fatal("nonterminal run did not print help")
			}
		}
	}
	for _, args := range [][]string{{"--help"}, {"list", "claude"}, {"tree", "claude", "missing"}, {"chat", "claude", "missing"}, {"list", "-f", "claude"}, {"bogus"}} {
		base, _ := browseFixture()
		base.Console = nil
		base.StdinTerminal = false
		done := make(chan struct{})
		close(done)
		base.Interrupt = done
		a, ao, ae := runRecorded(args, base, nil, nil)
		other, c := browseFixture()
		other.Interrupt = done
		b, bo, be := runRecorded(args, other, nil, nil)
		if a != b || !reflect.DeepEqual(ao.writes, bo.writes) || !reflect.DeepEqual(ae.writes, be.writes) || len(c.calls) != 0 {
			t.Fatalf("console used for %q", args)
		}
	}
}

// R-KVZT-N32X R-U39R-3JE8 R-ROX5-PC7X R-X1XA-36ET
// R-X4D2-UPW7 R-7CXO-L0W9 R-X356-GY5I R-5CA8-BZUX
func TestBrowsePreflight(t *testing.T) {
	for _, tc := range []struct {
		home       string
		cols, rows int
		nilConsole bool
		want       ExitCode
		diag       string
	}{
		{"", browse.MinCols, browse.MinRows, false, ExitDataUnreadable, "agent-monitor: cannot find the home directory: HOME is not set\n"},
		{"/home/dev", 0, 0, true, ExitUsage, ""},
		{"/home/dev", browse.MinCols - 1, browse.MinRows, false, ExitUsage, ""},
		{"/home/dev", browse.MinCols, browse.MinRows - 1, false, ExitUsage, ""},
	} {
		for _, diagErr := range []error{nil, errors.New("closed")} {
			sys, c := browseFixture()
			sys.Home = tc.home
			c.cols = tc.cols
			c.rows = tc.rows
			sys.Root = panicFS{}
			sys.Watcher = &panicWatcher{}
			if tc.nilConsole {
				sys.Console = nil
			}
			code, out, diag := runRecorded(nil, sys, nil, diagErr)
			want := tc.diag
			if want == "" {
				want = "agent-monitor: terminal too small: need at least " + strconv.Itoa(browse.MinCols) + "x" + strconv.Itoa(browse.MinRows) + ", have " + strconv.Itoa(tc.cols) + "x" + strconv.Itoa(tc.rows) + "\n"
			}
			if code != tc.want || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
				t.Fatalf("preflight %d %q %q", code, out.writes, diag.writes)
			}
			expected := []string(nil)
			if tc.home != "" && !tc.nilConsole {
				expected = []string{"size"}
			}
			if !reflect.DeepEqual(c.calls, expected) {
				t.Fatalf("preflight console calls %q", c.calls)
			}
		}
	}
}

type panicWatcher struct{}

func (*panicWatcher) Watch([]string)           { panic("watcher touched") }
func (*panicWatcher) Changes() <-chan struct{} { panic("watcher touched") }

type browserOutput struct {
	writes   []string
	events   *[]string
	failAt   int
	firstErr error
	onWrite  func(int)
}

func (w *browserOutput) Write(p []byte) (int, error) {
	w.writes = append(w.writes, string(p))
	if w.events != nil {
		*w.events = append(*w.events, "output")
	}
	n := len(w.writes)
	if w.onWrite != nil {
		w.onWrite(n)
	}
	if n == w.failAt {
		return len(p) / 2, w.firstErr
	}
	return len(p), nil
}

type browserDiagnostic struct {
	bytes.Buffer
	events *[]string
}

func (w *browserDiagnostic) Write(p []byte) (int, error) {
	*w.events = append(*w.events, "diagnostic")
	return w.Buffer.Write(p)
}

// R-U4HN-HB4X R-U5PJ-V2VM R-59UF-KGDJ R-53QX-NLO2
func TestBrowseScreenLifecycle(t *testing.T) {
	sys, c := browseFixture()
	c.keys <- []byte("q")
	var events []string
	c.events = &events
	out := &browserOutput{events: &events}
	diag := &browserDiagnostic{events: &events}
	code := Run(nil, sys, out, diag)
	if code != ExitSuccess || diag.Len() != 0 || len(out.writes) != 3 || out.writes[0] != enterBrowser || out.writes[2] != leaveBrowser {
		t.Fatalf("lifecycle %d %q %q", code, out.writes, diag.String())
	}
	if len(events) < 4 || events[0] != "size" || events[1] != "raw" || events[2] != "output" || events[len(events)-1] != "restore" {
		t.Fatalf("order %q", events)
	}
	counts := map[string]int{}
	for _, event := range events {
		counts[event]++
	}
	if counts["raw"] != 1 || counts["restore"] != 1 {
		t.Fatalf("raw/restore %v", counts)
	}
	var direct bytes.Buffer
	keys := make(chan []byte, 1)
	keys <- []byte("q")
	terminal := &browserConsole{cols: c.cols, rows: c.rows, keys: keys}
	if err := browse.Run(context.Background(), browse.Config{Home: sys.Home, Root: sys.Root, Color: true, Terminal: terminal}, &direct); err != nil {
		t.Fatal(err)
	}
	if out.writes[1] != direct.String() {
		t.Fatal("browser bytes changed")
	}
}

// R-ADDF-OOR4 R-7BPS-795K R-7CXO-L0W9 R-5CA8-BZUX
func TestBrowseWriteFailures(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		sys, c := browseFixture()
		c.keys <- []byte("q")
		var events []string
		c.events = &events
		first := errors.New("first failure")
		out := &browserOutput{events: &events, failAt: failAt, firstErr: first}
		diag := &browserDiagnostic{events: &events}
		code := Run(nil, sys, out, diag)
		calls := failAt
		if failAt < 3 {
			calls++
		}
		if code != ExitWriteFailed || len(out.writes) != calls || out.writes[len(out.writes)-1] != leaveBrowser || diag.String() != "agent-monitor: write error: first failure\n" {
			t.Fatalf("failure %d: %d %q %q", failAt, code, out.writes, diag.String())
		}
		if events[len(events)-2] != "restore" || events[len(events)-1] != "diagnostic" {
			t.Fatalf("restore order %q", events)
		}
		if failAt == 1 && !reflect.DeepEqual(c.calls, []string{"size", "raw", "restore"}) {
			t.Fatalf("browser called after entry failure %q", c.calls)
		}
	}
}

// R-53QX-NLO2
func TestBrowseWriterLatchesFirstError(t *testing.T) {
	first := errors.New("failed")
	out := &browserOutput{failAt: 1, firstErr: first}
	w := &browserWriter{out: out}
	p := []byte("screen")
	n, err := w.Write(p)
	if n != len(p)/2 || reflect.ValueOf(err) != reflect.ValueOf(first) || len(out.writes) != 1 || out.writes[0] != string(p) {
		t.Fatal("first result changed")
	}
	n, err = w.Write([]byte("next"))
	if n != 0 || reflect.ValueOf(err) != reflect.ValueOf(first) || len(out.writes) != 1 {
		t.Fatal("writer passed output after error")
	}
}

// R-5B2B-Y848 R-AH14-TZZ7
func TestBrowseContextAndColor(t *testing.T) {
	sys, c := browseFixture()
	sys.Root = &dispatchRoot{root: fstest.MapFS{}}
	sys.Watcher = &followTestWatcher{}
	cfg := browserConfig(sys)
	if cfg.Home != sys.Home || cfg.Root != sys.Root || cfg.Watcher != sys.Watcher || cfg.Terminal != c {
		t.Fatal("browser config does not preserve system values")
	}
	sys.Watcher = nil
	if browserConfig(sys).Watcher != nil {
		t.Fatal("nil watcher changed")
	}
	for _, done := range []chan struct{}{nil, make(chan struct{})} {
		ctx := interruptContext{done}
		if ctx.Done() != done || ctx.Err() != nil {
			t.Fatal("interrupt channel changed")
		}
		if done != nil {
			close(done)
			if reflect.ValueOf(ctx.Err()) != reflect.ValueOf(context.Canceled) {
				t.Fatal("closed context not canceled")
			}
		}
	}
	for _, term := range []string{"", "xterm-256color", "Dumb", "dumb ", "dumb"} {
		for _, noColor := range []string{"", "1", "0"} {
			if got := browseColor(System{Term: term, NoColor: noColor}); got != (noColor == "" && term != "dumb") {
				t.Errorf("color %q %q = %v", term, noColor, got)
			}
		}
	}
}

// R-1AU7-1JQS R-54YU-1DER R-57EM-SWW5
func TestBrowseNilWatcherInterruptAndClosedKeys(t *testing.T) {
	for _, keys := range [][][]byte{{[]byte("q")}, {}, {[]byte("j"), []byte("j"), []byte("j")}} {
		var baseline []string
		for _, withWatcher := range []bool{false, true} {
			sys, c := browseFixture()
			for _, key := range keys {
				c.keys <- key
			}
			close(c.keys)
			if withWatcher {
				sys.Watcher = &followTestWatcher{changes: make(chan struct{})}
			}
			code, out, diag := runRecorded(nil, sys, nil, nil)
			if code != ExitSuccess || len(diag.writes) != 0 || out.writes[len(out.writes)-1] != leaveBrowser {
				t.Fatalf("nil lifecycle %d %q %q", code, out.writes, diag.writes)
			}
			if !withWatcher {
				baseline = out.writes
			} else if !reflect.DeepEqual(baseline, out.writes) {
				t.Fatal("nil watcher changed output")
			}
			if len(keys) == 3 && len(out.writes) != 3+len(keys) {
				t.Fatalf("queued keys not processed %q", out.writes)
			}
		}
	}
}

// R-566Q-F55G
func TestBrowseInterruptPriority(t *testing.T) {
	for _, at := range []int{0, 1, 2} {
		sys, c := browseFixture()
		done := make(chan struct{})
		sys.Interrupt = done
		c.keys <- []byte("j")
		c.resized <- struct{}{}
		changes := make(chan struct{}, 1)
		changes <- struct{}{}
		sys.Watcher = &followTestWatcher{changes: changes}
		if at == 0 {
			close(done)
		}
		out := &browserOutput{onWrite: func(n int) {
			if n == at {
				close(done)
			}
		}}
		diag := &recorder{}
		code := Run(nil, sys, out, diag)
		if code != ExitSuccess || len(out.writes) != 3 || len(diag.writes) != 0 || len(c.keys) != 1 || len(c.resized) != 1 || len(changes) != 1 {
			t.Fatalf("interrupt %d: %d %q queues %d/%d/%d", at, code, out.writes, len(c.keys), len(c.resized), len(changes))
		}
	}
}

// R-L5R0-P90H
func TestBrowseEscapeReadBoundary(t *testing.T) {
	for _, value := range []string{"\x1b", "\x1b[A", "\x1bj", "\x1b\x1b"} {
		sys, c := browseFixture()
		c.keys <- []byte(value)
		c.keys <- []byte("j")
		close(c.keys)
		code, out, diag := runRecorded(nil, sys, nil, nil)
		if code != ExitSuccess || len(diag.writes) != 0 {
			t.Fatal("escape lifecycle")
		}
		if value == "\x1b" {
			if len(out.writes) != 3 || len(c.keys) != 1 {
				t.Fatal("bare escape did not quit")
			}
		} else if len(c.keys) != 0 || len(out.writes) < 4 {
			t.Fatal("multi-byte escape quit browser")
		}
	}
}

// R-54YU-1DER
func TestBrowseNilInterruptWriteFailure(t *testing.T) {
	sys, c := browseFixture()
	out := &browserOutput{failAt: 2, firstErr: io.ErrClosedPipe}
	diag := &recorder{}
	code := Run(nil, sys, out, diag)
	if code != ExitWriteFailed || len(out.writes) != 3 || len(c.keys) != 0 || !strings.Contains(diag.writes[0], io.ErrClosedPipe.Error()) {
		t.Fatal("failed output waited for input")
	}
}

// R-ADDF-OOR4 R-7BPS-795K
func TestBrowseEntryFailureIgnoresFailedLeaveAndDiagnostic(t *testing.T) {
	sys, c := browseFixture()
	out := &recorder{err: errors.New("broken output")}
	diag := &recorder{err: errors.New("broken diagnostic")}
	code := Run(nil, sys, out, diag)
	if code != ExitWriteFailed || !reflect.DeepEqual(out.writes, []string{enterBrowser, leaveBrowser}) || !reflect.DeepEqual(diag.writes, []string{"agent-monitor: write error: broken output\n"}) || !reflect.DeepEqual(c.calls, []string{"size", "raw", "restore"}) {
		t.Fatalf("failed cleanup %d %q %q %q", code, out.writes, diag.writes, c.calls)
	}
}

// R-566Q-F55G
func TestBrowseInterruptCompletesLaterScreen(t *testing.T) {
	sys, c := browseFixture()
	done := make(chan struct{})
	sys.Interrupt = done
	c.keys <- []byte("j")
	c.keys <- []byte("j")
	out := &browserOutput{onWrite: func(n int) {
		if n == 3 {
			close(done)
		}
	}}
	diag := &recorder{}
	code := Run(nil, sys, out, diag)
	if code != ExitSuccess || len(out.writes) != 4 || out.writes[3] != leaveBrowser || len(c.keys) != 1 || len(diag.writes) != 0 {
		t.Fatalf("later interruption %d %q remaining %d", code, out.writes, len(c.keys))
	}
}

// R-ADDF-OOR4 R-7BPS-795K
func TestBrowseLaterScreenFailureStopsImmediately(t *testing.T) {
	sys, c := browseFixture()
	c.keys <- []byte("j")
	c.keys <- []byte("j")
	out := &browserOutput{failAt: 3, firstErr: errors.New("later failure")}
	diag := &recorder{}
	code := Run(nil, sys, out, diag)
	if code != ExitWriteFailed || len(out.writes) != 4 || out.writes[3] != leaveBrowser || len(c.keys) != 1 || !reflect.DeepEqual(diag.writes, []string{"agent-monitor: write error: later failure\n"}) {
		t.Fatalf("later failure %d %q %q remaining %d", code, out.writes, diag.writes, len(c.keys))
	}
}
