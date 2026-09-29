package cli

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/browse"
)

const enterBrowser = "\x1b[?1049h\x1b[?25l"
const leaveBrowser = "\x1b[?25h\x1b[?1049l"

type interruptContext struct{ interrupt <-chan struct{} }

func (c interruptContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c interruptContext) Done() <-chan struct{}       { return c.interrupt }
func (c interruptContext) Err() error {
	select {
	case <-c.interrupt:
		return context.Canceled
	default:
		return nil
	}
}
func (interruptContext) Value(any) any { return nil }

type browserWriter struct {
	out io.Writer
	err error
}

func (w *browserWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.out.Write(p)
	w.err = err
	return n, err
}

func runBrowse(sys System, stdout, stderr io.Writer) ExitCode {
	if sys.Home == "" {
		writeDiagnostic(stderr, "agent-monitor: cannot find the home directory: HOME is not set\n")
		return ExitDataUnreadable
	}
	cols, rows := 0, 0
	if sys.Console != nil {
		cols, rows = sys.Console.Size()
	}
	if cols < browse.MinCols || rows < browse.MinRows {
		writeDiagnostic(stderr, "agent-monitor: terminal too small: need at least "+strconv.Itoa(browse.MinCols)+"x"+strconv.Itoa(browse.MinRows)+", have "+strconv.Itoa(cols)+"x"+strconv.Itoa(rows)+"\n")
		return ExitUsage
	}
	restore := sys.Console.Raw()
	_, err := stdout.Write([]byte(enterBrowser))
	if err == nil {
		output := &browserWriter{out: stdout}
		err = browse.Run(interruptContext{sys.Interrupt}, browserConfig(sys), output)
		if output.err != nil {
			err = output.err
		}
	}
	_, leaveErr := stdout.Write([]byte(leaveBrowser))
	if err == nil {
		err = leaveErr
	}
	restore()
	if err != nil {
		writeDiagnostic(stderr, "agent-monitor: write error: "+err.Error()+"\n")
		return ExitWriteFailed
	}
	return ExitSuccess
}

func browseColor(sys System) bool { return sys.NoColor == "" && sys.Term != "dumb" }

func browserConfig(sys System) browse.Config {
	return browse.Config{Home: sys.Home, Root: sys.Root, Color: browseColor(sys), Watcher: sys.Watcher, Terminal: sys.Console}
}
