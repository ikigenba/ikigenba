package cli_test

import (
	"io"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func TestRunSignatureAndSystem(t *testing.T) {
	// R-DF10-59J9 R-XK3U-55KN R-XLBQ-IXBC
	want := reflect.TypeOf((func([]string, cli.System, io.Writer, io.Writer) cli.ExitCode)(nil))
	if got := reflect.TypeOf(cli.Run); got != want {
		t.Fatalf("Run type = %v, want %v", got, want)
	}
	st := reflect.TypeOf(cli.System{})
	if st.NumField() != 2 || st.Field(0).Name != "Home" || st.Field(0).Type != reflect.TypeOf("") || st.Field(1).Name != "Root" || st.Field(1).Type != reflect.TypeOf((*fs.FS)(nil)).Elem() {
		t.Fatalf("System fields = %v", st)
	}
}

func TestExitCodes(t *testing.T) {
	// R-2ITW-Y03T R-2K1T-BRUI R-DHGS-WT0N R-2KFU-3SGC
	const sessionNotFound cli.ExitCode = cli.ExitSessionNotFound
	_ = sessionNotFound
	codes := [5]cli.ExitCode{cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage, cli.ExitDataUnreadable, cli.ExitSessionNotFound}
	if codes != [5]cli.ExitCode{0, 1, 2, 3, 4} {
		t.Fatalf("exit codes = %v", codes)
	}
	for _, constant := range []any{cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage, cli.ExitDataUnreadable, cli.ExitSessionNotFound} {
		if reflect.TypeOf(constant) != reflect.TypeOf(cli.ExitCode(0)) {
			t.Fatalf("constant has type %T", constant)
		}
	}
}

func TestCLIExportedNames(t *testing.T) {
	t.Helper()
	// Compile every declared export; absence of extras is checked by source review.
	_ = []any{cli.Run, cli.System{}, cli.ExitCode(0), cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage, cli.ExitDataUnreadable, cli.ExitSessionNotFound, cli.Usage, cli.ListUsage, cli.TreeUsage, cli.Version}
}

func TestTreeUsageDeclaration(_ *testing.T) {
	// R-2LNQ-HK71: this assignment compiles only while TreeUsage is a string constant.
	const actual string = cli.TreeUsage
	_ = actual
}

func TestModuleHasNoRequirements(t *testing.T) {
	// R-29D1-RWUN: inspect the module file, not just the resolved package graph.
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "require (" || strings.HasPrefix(line, "require ") {
			t.Fatalf("unexpected require directive: %q", line)
		}
	}
}
