package cli_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

type recordedWrites struct {
	bytes.Buffer
	calls int
}

func (w *recordedWrites) Write(p []byte) (int, error) {
	w.calls++
	return w.Buffer.Write(p)
}

// R-KMH0-K217 R-U7V3-Z3MZ R-U930-CVDO R-XYXH-B0FH R-Y05D-OS66
// R-UCQP-I6LR R-Y2L6-GBNK R-SCSM-XPUS R-Y3T2-U3E9
func TestCommandDispatchAndDiagnostics(t *testing.T) {
	cases := []struct {
		args []string
		out  string
		arg  string
		kind string
	}{
		{args: []string{"--version"}, out: "fixture-display" + "\n"},
		{args: []string{"manifest"}, out: cli.Manifest},
		{args: []string{"--help"}, out: cli.Usage},
		{args: []string{"db"}, arg: "db", kind: "command"},
		{args: []string{"db", "bogus"}, arg: "bogus", kind: "command"},
		{args: []string{"db", "status", "extra"}, arg: "extra", kind: "command"},
		{args: []string{"db", "status", "-x"}, arg: "-x", kind: "option"},
		{args: []string{"db", "-x"}, arg: "-x", kind: "option"},
		{args: []string{"bogus"}, arg: "bogus", kind: "command"},
		{args: []string{"-x"}, arg: "-x", kind: "option"},
		{args: []string{""}, arg: "", kind: "command"},
		{args: []string{"version"}, arg: "version", kind: "command"},
		{args: []string{"--"}, arg: "--", kind: "option"},
		{args: []string{"-"}, arg: "-", kind: "option"},
		{args: []string{"help"}, arg: "help", kind: "command"},
		{args: []string{"serve"}, arg: "serve", kind: "command"},
		{args: []string{"Manifest"}, arg: "Manifest", kind: "command"},
		{args: []string{"bogus", "--help"}, arg: "bogus", kind: "command"},
		{args: []string{"-wrong", "manifest"}, arg: "-wrong", kind: "option"},
		{args: []string{"manifest", "manifest"}, arg: "manifest", kind: "command"},
		{args: []string{"--help", "--version"}, arg: "--version", kind: "option"},
		{args: []string{"--version", ""}, arg: "", kind: "command"},
		{args: []string{"--version", "manifest", "ignored"}, arg: "manifest", kind: "command"},
		{args: []string{"--help", "--help", "ignored"}, arg: "--help", kind: "option"},
		{args: []string{"manifest", "a'b"}, arg: "a'b", kind: "command"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "/"), func(t *testing.T) {
			var out bytes.Buffer
			var diagnostic recordedWrites
			p := untouchedProcess(tc.args, &out, &diagnostic, t.TempDir())
			code := cli.Run(context.Background(), p)
			wantCode, wantDiagnostic, wantWrites := cli.ExitSuccess, "", 0
			if tc.kind != "" {
				wantCode, wantWrites = cli.ExitUsage, 1
				wantDiagnostic = "repos: unknown " + tc.kind + " '" + tc.arg + "'\n\nsee 'repos --help' for usage\n"
			}
			if code != wantCode || out.String() != tc.out || diagnostic.String() != wantDiagnostic || diagnostic.calls != wantWrites {
				t.Fatalf("Run(%q) = %d, stdout %q, stderr %q in %d writes; want %d, %q, %q in %d writes", tc.args, code, out.String(), diagnostic.String(), diagnostic.calls, wantCode, tc.out, wantDiagnostic, wantWrites)
			}
			assertEmptyDirectory(t, p.Dir)
		})
	}
}

// R-DC1H-ESUT
func TestCommandsNeedOnlyArgumentsAndStreams(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"--help"}, {"bogus"}, {"manifest", "--version"}} {
		var out, diagnostic bytes.Buffer
		p := cli.Process{Args: args, Stdout: &out, Stderr: &diagnostic, Dir: t.TempDir()}
		code := cli.Run(context.Background(), p)
		if code != cli.ExitSuccess && code != cli.ExitUsage {
			t.Fatalf("command Run(%q) = %d", args, code)
		}
		assertEmptyDirectory(t, p.Dir)
	}
}

func assertEmptyDirectory(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("command left %d entries in Dir", len(entries))
	}
}
