package cli

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

// R-PPPO-RKN9 R-PQXL-5CDY R-PS5H-J44N R-PULA-ANM1 R-PVT6-OFCQ R-PX13-273F R-PY8Z-FYU4
func TestTreeGrammar(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"tree"}, "agent-monitor: missing harness" + usageHint},
		{[]string{"tree", "claude"}, "agent-monitor: missing session id" + usageHint},
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

// R-PZGV-TQKT R-Q0OS-7IBI R-Q1WO-LA27 R-Q806-I4RO
func TestTreeBeforeFilesystem(t *testing.T) {
	for _, args := range [][]string{{"tree"}, {"tree", "-bad"}, {"tree", "bogus"}, {"tree", "claude"}, {"tree", "claude", "id", "extra"}, {"tree", "--help"}} {
		a, outA, errA := runRecorded(args, System{Root: panicFS{}}, nil, nil)
		b, outB, errB := runRecorded(args, System{Home: "/home/dev", Root: panicFS{}}, nil, nil)
		if a != b || strings.Join(outA.writes, "") != strings.Join(outB.writes, "") || strings.Join(errA.writes, "") != strings.Join(errB.writes, "") {
			t.Errorf("Run(%q) varied with System", args)
		}
	}
	for _, h := range []string{"claude", "codex", "grok"} {
		args := []string{"tree", h, "id"}
		want := "agent-monitor: cannot find the home directory: HOME is not set\n"
		assertRun(t, args, System{Root: panicFS{}}, ExitDataUnreadable, "", want)
		code, out, diag := runRecorded(args, System{Root: panicFS{}}, nil, errors.New("closed"))
		if code != ExitDataUnreadable || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
			t.Errorf("failed diagnostic write: %d %q %q", code, out.writes, diag.writes)
		}
	}
	code, out, diag := runRecorded([]string{"tree", "claude"}, System{}, nil, errors.New("closed"))
	if code != ExitUsage || len(out.writes) != 0 || len(diag.writes) != 1 {
		t.Errorf("failed usage diagnostic: %d %q %q", code, out.writes, diag.writes)
	}
}

// R-Q34K-Z1SW R-Q5KD-QLAA R-Q6SA-4D0Z R-Q982-VWID R-QAFZ-9O92
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
}

// R-Q4CH-CTJL R-QBNV-NFZR R-PM1Z-M9F6
func TestTreeSuccessfulProductAndCodes(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")},
	}
	code, out, diag := runRecorded([]string{"tree", "claude", "sample"}, System{Home: "/home/dev", Root: root}, nil, nil)
	want := tree.Draw(tree.Tree{Root: tree.Node{ID: "sample", Status: tree.StatusEnded}})
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
