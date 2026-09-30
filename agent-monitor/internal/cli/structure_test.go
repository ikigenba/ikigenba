package cli_test

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func TestRunSignatureAndSystem(_ *testing.T) {
	// R-XK3U-55KN R-XLBQ-IXBC
	run := typed[func([]string, cli.System, io.Writer, io.Writer) cli.ExitCode](cli.Run)
	_ = run
}

// typed returns v as a T; a call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }

type structureWatcher struct{ changes chan struct{} }

func (structureWatcher) Watch([]string)             {}
func (w structureWatcher) Changes() <-chan struct{} { return w.changes }

type structureConsole struct{}

func (structureConsole) Raw() func()              { return func() {} }
func (structureConsole) Keys() <-chan []byte      { return nil }
func (structureConsole) Size() (int, int)         { return 80, 24 }
func (structureConsole) Resized() <-chan struct{} { return nil }

func TestSystemFields(t *testing.T) {
	// R-YLJ8-3XNC
	interrupt := make(chan struct{})
	home := typed[string]("/home/dev")
	root := typed[fs.FS](fstest.MapFS{})
	noColor := typed[string]("1")
	term := typed[string]("xterm")
	terminal := typed[bool](true)
	stdinTerminal := typed[bool](true)
	watcher := typed[cli.Watcher](structureWatcher{})
	interruptCh := typed[<-chan struct{}](interrupt)
	console := typed[cli.Console](structureConsole{})
	sys := cli.System{Home: home, Root: root, NoColor: noColor, Term: term, Terminal: terminal, StdinTerminal: stdinTerminal, Watcher: watcher, Interrupt: interruptCh, Console: console}
	if sys.Home != home || sys.NoColor != noColor || sys.Term != term || !sys.Terminal || !sys.StdinTerminal || sys.Watcher != watcher || sys.Interrupt != interruptCh || sys.Console != console {
		t.Fatalf("System = %+v", sys)
	}
	if _, ok := sys.Root.(fstest.MapFS); !ok {
		t.Fatalf("Root = %T", sys.Root)
	}
}

func TestExitCodes(t *testing.T) {
	// R-2ITW-Y03T R-2K1T-BRUI R-DHGS-WT0N R-JFJF-S38W
	const notFound cli.ExitCode = cli.ExitNotFound
	_ = notFound
	codes := [5]cli.ExitCode{cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage, cli.ExitDataUnreadable, cli.ExitNotFound}
	if codes != [5]cli.ExitCode{0, 1, 2, 3, 4} {
		t.Fatalf("exit codes = %v", codes)
	}
	for _, constant := range []any{cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage, cli.ExitDataUnreadable, cli.ExitNotFound} {
		if _, ok := constant.(cli.ExitCode); !ok {
			t.Fatalf("constant has type %T", constant)
		}
	}
}

func TestCLIExportedNames(t *testing.T) {
	t.Helper()
	// Compile every declared export.
	_ = []any{cli.Run, cli.System{}, cli.ExitCode(0), cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage, cli.ExitDataUnreadable, cli.ExitNotFound, cli.Usage, cli.ListUsage, cli.TreeUsage, cli.ChatUsage, cli.Version}
}

func TestChatUsageDeclaration(_ *testing.T) {
	// R-JGRC-5UZL: this assignment compiles only while ChatUsage is a string constant.
	const actual string = cli.ChatUsage
	_ = actual
}

func TestTreeUsageDeclaration(_ *testing.T) {
	// R-2LNQ-HK71: this assignment compiles only while TreeUsage is a string constant.
	const actual string = cli.TreeUsage
	_ = actual
}

// R-YMR4-HPE1
func TestWatcherMethods(t *testing.T) {
	changes := make(chan struct{})
	w := typed[cli.Watcher](structureWatcher{changes: changes})
	watch := typed[func([]string)](w.Watch)
	changesOf := typed[func() <-chan struct{}](w.Changes)
	watch([]string{"name"})
	if changesOf() != changes {
		t.Fatal("Changes channel lost")
	}
}
