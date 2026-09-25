package cli

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// R-69XU-63BD R-6B5Q-JV22 R-6CDM-XMSR R-6DLJ-BEJG R-PQXL-5CDY R-PS5H-J44N R-PULA-ANM1 R-PVT6-OFCQ
func TestTreeGrammar(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"tree"}, "agent-monitor: missing harness" + usageHint},
		{[]string{"tree", "--no-color"}, "agent-monitor: missing harness" + usageHint},
		{[]string{"tree", "--no-color", "--no-color"}, "agent-monitor: missing harness" + usageHint},
		{[]string{"tree", "claude"}, "agent-monitor: missing session id" + usageHint},
		{[]string{"tree", "--no-color", "grok", "--no-color"}, "agent-monitor: missing session id" + usageHint},
		{[]string{"tree", ""}, "agent-monitor: unknown harness ''" + usageHint},
		{[]string{"tree", "Claude"}, "agent-monitor: unknown harness 'Claude'" + usageHint},
		{[]string{"tree", "claud", "--bogus"}, "agent-monitor: unknown harness 'claud'" + usageHint},
		{[]string{"tree", "claud", "id", "extra"}, "agent-monitor: unknown harness 'claud'" + usageHint},
		{[]string{"tree", "--bogus", "claude"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"tree", "claude", "--bogus"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"tree", "claude", "id", "--bogus", "extra"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"tree", "claude", "id", "extra", "--bogus"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
		{[]string{"tree", "claude", "id", "extra", "more"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
		{[]string{"tree", "claude", "id", "--version"}, "agent-monitor: unknown option '--version'" + usageHint},
		{[]string{"tree", "-", "id"}, "agent-monitor: unknown option '-'" + usageHint},
		{[]string{"tree", "--", "id"}, "agent-monitor: unknown option '--'" + usageHint},
		{[]string{"tree", "--no-color=x", "claude", "id"}, "agent-monitor: unknown option '--no-color=x'" + usageHint},
		{[]string{"tree", "--no-colour", "claude", "id"}, "agent-monitor: unknown option '--no-colour'" + usageHint},
		{[]string{"tree", "--No-Color", "claude", "id"}, "agent-monitor: unknown option '--No-Color'" + usageHint},
		{[]string{"tree", "claude", "id", "--no-color", "extra"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
	}
	for _, tc := range cases {
		assertRun(t, tc.args, System{}, ExitUsage, "", tc.want)
	}
	for _, h := range []string{"claude", "codex", "grok"} {
		assertRun(t, []string{"tree", h}, System{}, ExitUsage, "", "agent-monitor: missing session id"+usageHint)
		assertRun(t, []string{"tree", h, ""}, System{Home: "/home/dev", Root: fstest.MapFS{}}, ExitSessionNotFound, "", "agent-monitor: no "+h+" session ''\n")
	}
	assertRun(t, []string{"tree", "claude", "id", "a\nb"}, System{}, ExitUsage, "", "agent-monitor: unexpected argument 'a\\nb'"+usageHint)
}

// R-6ETF-P6A5 R-6G1C-2Y0U R-6H98-GPRJ R-Q806-I4RO
func TestTreeBeforeFilesystem(t *testing.T) {
	for _, args := range [][]string{{"tree"}, {"tree", "--no-color"}, {"tree", "-bad"}, {"tree", "bogus"}, {"tree", "claude"}, {"tree", "claude", "id", "extra"}, {"tree", "claude", "id", "--no-color", "extra"}, {"tree", "--help"}} {
		a, outA, errA := runRecorded(args, System{Root: panicFS{}}, nil, nil)
		b, outB, errB := runRecorded(args, System{Home: "/home/dev", Root: panicFS{}}, nil, nil)
		if a != b || strings.Join(outA.writes, "") != strings.Join(outB.writes, "") || strings.Join(errA.writes, "") != strings.Join(errB.writes, "") {
			t.Errorf("Run(%q) varied with System", args)
		}
	}
	for _, h := range []string{"claude", "codex", "grok"} {
		for _, args := range [][]string{{"tree", h, "id"}, {"tree", "--no-color", h, "--no-color", "id", "--no-color"}} {
			want := "agent-monitor: cannot find the home directory: HOME is not set\n"
			assertRun(t, args, System{Root: panicFS{}}, ExitDataUnreadable, "", want)
			code, out, diag := runRecorded(args, System{Root: panicFS{}}, nil, errors.New("closed"))
			if code != ExitDataUnreadable || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
				t.Errorf("failed diagnostic write: %d %q %q", code, out.writes, diag.writes)
			}
		}
	}
	code, out, diag := runRecorded([]string{"tree", "claude"}, System{}, nil, errors.New("closed"))
	if code != ExitUsage || len(out.writes) != 0 || len(diag.writes) != 1 {
		t.Errorf("failed usage diagnostic: %d %q %q", code, out.writes, diag.writes)
	}
}

// R-6IH4-UHI8 R-6M4T-ZSQB R-Q5KD-QLAA R-Q982-VWID R-QAFZ-9O92
func TestTreeHarnessOutcomes(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		root := &deniedFS{}
		args := []string{"tree", h, "id"}
		code, out, diag := runRecorded(args, System{Home: "/home/dev", Root: root}, nil, nil)
		var location string
		switch h {
		case "claude":
			location = "home/dev/.claude/projects"
		case "codex":
			location = "home/dev/.codex/sessions"
		case "grok":
			location = "home/dev/.grok/sessions"
		}
		want := "agent-monitor: cannot read /" + location + ": permission denied\n"
		if len(root.opens) != 1 || root.opens[0] != location || len(out.writes) != 0 || code != ExitDataUnreadable || len(diag.writes) != 1 || diag.writes[0] != want {
			t.Errorf("tree %s read failure: code %d opens %q out %q diag %q", h, code, root.opens, out.writes, diag.writes)
		}
	}
	for _, h := range []string{"claude", "codex", "grok"} {
		args := []string{"tree", h, "missing"}
		code, out, diag := runRecorded(args, System{Home: "/home/dev", Root: fstest.MapFS{}}, nil, errors.New("closed"))
		want := "agent-monitor: no " + h + " session 'missing'\n"
		if code != ExitSessionNotFound || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
			t.Errorf("tree %s not found: code %d out %q diag %q", h, code, out.writes, diag.writes)
		}
	}
	assertRun(t, []string{"tree", "claude", "a\nb"}, System{Home: "/home/dev", Root: fstest.MapFS{}}, ExitSessionNotFound, "", "agent-monitor: no claude session 'a\\nb'\n")
	assertRun(t, []string{"tree", "--no-color", "claude", "a\nb", "--no-color"}, System{Home: "/home/dev", Root: fstest.MapFS{}}, ExitSessionNotFound, "", "agent-monitor: no claude session 'a\\nb'\n")
}

// R-6JP1-898X R-6KWX-M0ZM R-QBNV-NFZR R-PM1Z-M9F6
func TestTreeSuccessfulProductAndCodes(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")},
	}
	code, out, diag := runRecorded([]string{"tree", "claude", "sample"}, System{Home: "/home/dev", Root: root}, nil, nil)
	want := tree.Draw(tree.Tree{Root: tree.Node{ID: "sample", Status: tree.StatusEnded}}, false)
	if code != ExitSuccess || len(out.writes) != 1 || out.writes[0] != want || len(diag.writes) != 0 {
		t.Errorf("tree success = %d %q %q, want %q", code, out.writes, diag.writes, want)
	}
	for _, args := range [][]string{nil, {"--help"}, {"-bad"}, {"tree"}, {"tree", "claude", "id"}} {
		code, _, _ := runRecorded(args, System{Home: "/home/dev", Root: fstest.MapFS{}}, nil, nil)
		if code < ExitSuccess || code > ExitSessionNotFound {
			t.Errorf("Run(%q) returned invalid exit code %d", args, code)
		}
	}
	code, out, diag = runRecorded([]string{"tree", "claude", "sample"}, System{Home: "/home/dev", Root: root}, errors.New("disk full"), nil)
	if code != ExitWriteFailed || len(out.writes) != 1 || len(diag.writes) != 1 || diag.writes[0] != "agent-monitor: write error: disk full\n" {
		t.Errorf("tree write failure = %d %q %q", code, out.writes, diag.writes)
	}
}

// R-69XU-63BD R-6IH4-UHI8 R-6JP1-898X R-6KWX-M0ZM
func TestTreeDrawingArgumentsAndColor(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")},
	}
	treeValue := tree.Tree{Root: tree.Node{ID: "sample", Status: tree.StatusEnded}}
	cases := []struct {
		name  string
		args  []string
		sys   System
		color bool
	}{
		{"default", []string{"tree", "claude", "sample"}, System{}, false},
		{"terminal-unset-term", []string{"tree", "claude", "sample"}, System{Terminal: true}, true},
		{"terminal-xterm", []string{"tree", "claude", "sample"}, System{Terminal: true, Term: "xterm-256color"}, true},
		{"terminal-mixed-case", []string{"tree", "claude", "sample"}, System{Terminal: true, Term: "Dumb"}, true},
		{"terminal-trailing-space", []string{"tree", "claude", "sample"}, System{Terminal: true, Term: "dumb "}, true},
		{"no-terminal", []string{"tree", "claude", "sample"}, System{Terminal: false, Term: "xterm"}, false},
		{"no-color-one", []string{"tree", "claude", "sample"}, System{Terminal: true, NoColor: "1"}, false},
		{"no-color-zero", []string{"tree", "claude", "sample"}, System{Terminal: true, NoColor: "0"}, false},
		{"dumb-terminal", []string{"tree", "claude", "sample"}, System{Terminal: true, Term: "dumb"}, false},
		{"option-before-harness", []string{"tree", "--no-color", "claude", "sample"}, System{Terminal: true}, false},
		{"option-between-places", []string{"tree", "claude", "--no-color", "sample", "--no-color"}, System{Terminal: true}, false},
		{"option-after-id", []string{"tree", "claude", "sample", "--no-color"}, System{Terminal: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sys := tc.sys
			sys.Home = "/home/dev"
			sys.Root = root
			assertRun(t, tc.args, sys, ExitSuccess, tree.Draw(treeValue, tc.color), "")
		})
	}
}
