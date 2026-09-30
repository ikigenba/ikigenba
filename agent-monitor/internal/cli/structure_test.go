package cli_test

import (
	"io"
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func TestRunSignatureAndSystem(t *testing.T) {
	// R-XK3U-55KN R-XLBQ-IXBC
	want := reflect.TypeOf((func([]string, cli.System, io.Writer, io.Writer) cli.ExitCode)(nil))
	if got := reflect.TypeOf(cli.Run); got != want {
		t.Fatalf("Run type = %v, want %v", got, want)
	}
}

func TestSystemFields(t *testing.T) {
	// R-KNGI-YOW2
	st := reflect.TypeOf(cli.System{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{"Home", reflect.TypeOf("")},
		{"Root", reflect.TypeOf((*fs.FS)(nil)).Elem()},
		{"NoColor", reflect.TypeOf("")},
		{"Term", reflect.TypeOf("")},
		{"Terminal", reflect.TypeOf(false)},
		{"StdinTerminal", reflect.TypeOf(false)},
		{"Watcher", reflect.TypeOf((*cli.Watcher)(nil)).Elem()},
		{"Interrupt", reflect.TypeOf((<-chan struct{})(nil))},
		{"Console", reflect.TypeOf((*cli.Console)(nil)).Elem()},
	}
	if st.NumField() != len(want) {
		t.Fatalf("System field count = %d, want %d", st.NumField(), len(want))
	}
	for i, field := range want {
		got := st.Field(i)
		if got.Name != field.name || got.Type != field.typ || !got.IsExported() {
			t.Fatalf("System field %d = %s %v, want %s %v", i, got.Name, got.Type, field.name, field.typ)
		}
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
		if reflect.TypeOf(constant) != reflect.TypeOf(cli.ExitCode(0)) {
			t.Fatalf("constant has type %T", constant)
		}
	}
}

func TestCLIExportedNames(t *testing.T) {
	t.Helper()
	// Compile every declared export; absence of extras is checked by source review.
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

// R-TS88-MLJT
func TestWatcherMethods(t *testing.T) {
	typ := reflect.TypeOf((*cli.Watcher)(nil)).Elem()
	if typ.NumMethod() != 2 {
		t.Fatalf("Watcher methods = %d", typ.NumMethod())
	}
	for name, want := range map[string]reflect.Type{"Watch": reflect.TypeOf((func([]string))(nil)), "Changes": reflect.TypeOf((func() <-chan struct{})(nil))} {
		method, ok := typ.MethodByName(name)
		if !ok || method.Type != want {
			t.Errorf("Watcher.%s = %v, want %v", name, method.Type, want)
		}
	}
}
