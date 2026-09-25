package cli

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// R-KEWI-VFR1
func TestChatUsageText(t *testing.T) {
	const want = "Usage: agent-monitor chat <harness> <session-id> [<agent-id>]\n\nPrint one agent's chat and its token totals.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"
	if ChatUsage != want {
		t.Errorf("ChatUsage = %q, want %q", ChatUsage, want)
	}
}

// R-KG4F-97HQ
func TestChatHelpWins(t *testing.T) {
	for _, args := range [][]string{{"chat", "--help"}, {"chat", "-h"}, {"chat", "claude", "session", "--help"}, {"chat", "bogus", "extra", "more", "more", "--bogus", "-h"}} {
		assertRun(t, args, System{Root: panicFS{}}, ExitSuccess, ChatUsage, "")
	}
}

// R-JQIJ-80X5 R-JRQF-LSNU R-JSYB-ZKEJ R-JU68-DC58 R-JVE4-R3VX R-JWM1-4VMM R-JXTX-INDB R-JZ1T-WF40
func TestChatGrammar(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"chat"}, "agent-monitor: missing harness" + usageHint},
		{[]string{"chat", "claude"}, "agent-monitor: missing session id" + usageHint},
		{[]string{"chat", ""}, "agent-monitor: unknown harness ''" + usageHint},
		{[]string{"chat", "Claude"}, "agent-monitor: unknown harness 'Claude'" + usageHint},
		{[]string{"chat", "claud", "--bogus"}, "agent-monitor: unknown harness 'claud'" + usageHint},
		{[]string{"chat", "--bogus", "claud"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"chat", "--no-color", "claude", "id"}, "agent-monitor: unknown option '--no-color'" + usageHint},
		{[]string{"chat", "claude", "id", "--bogus"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"chat", "claude", "id", "agent", "extra", "more"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
		{[]string{"chat", "claude", "id", "agent", "extra", "--bogus"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
		{[]string{"chat", "claude", "id", "a\nb", "extra"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
		{[]string{"chat", "claude", "id", "agent", "a\nb"}, "agent-monitor: unexpected argument 'a\\nb'" + usageHint},
	}
	for _, tc := range cases {
		assertRun(t, tc.args, System{Root: panicFS{}}, ExitUsage, "", tc.want)
	}
}

// R-JLMX-OXYD R-JO2Q-GHFR
func TestChatUsageIgnoresSystem(t *testing.T) {
	a := System{Root: panicFS{}}
	b := System{Home: "/home/dev", Root: panicFS{}, NoColor: "1", Term: "dumb", Terminal: true}
	for _, args := range [][]string{{"chat"}, {"chat", "claude"}, {"chat", "--bad"}, {"chat", "claude", "id", "agent", "extra"}, {"chat", "--help"}} {
		codeA, outA, errA := runRecorded(args, a, nil, nil)
		codeB, outB, errB := runRecorded(args, b, nil, nil)
		if codeA != codeB || strings.Join(outA.writes, "") != strings.Join(outB.writes, "") || strings.Join(errA.writes, "") != strings.Join(errB.writes, "") {
			t.Errorf("Run(%q) depends on System", args)
		}
	}
}

// R-K09Q-A6UP R-JO2Q-GHFR
func TestChatMissingHome(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		for _, args := range [][]string{{"chat", h, "id"}, {"chat", h, "id", "agent"}} {
			assertRun(t, args, System{Root: panicFS{}}, ExitDataUnreadable, "", "agent-monitor: cannot find the home directory: HOME is not set\n")
		}
	}
}

// R-JHZ8-JMQA R-K7L4-KTAV R-KB8T-Q4IY R-KA0X-CCS9
func TestChatSessionNotFound(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		for _, args := range [][]string{{"chat", h, "missing"}, {"chat", h, "missing", "agent"}} {
			code, out, diag := runRecorded(args, System{Home: "/home/dev", Root: fstest.MapFS{}}, nil, errors.New("closed"))
			want := "agent-monitor: no " + h + " session 'missing'\n"
			if code != ExitNotFound || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
				t.Errorf("Run(%q) = %d %q %q", args, code, out.writes, diag.writes)
			}
		}
	}
}

// R-JQIJ-80X5
func TestChatAcceptsEmptyIDs(t *testing.T) {
	assertRun(t, []string{"chat", "codex", "", ""}, System{Home: "/home/dev", Root: fstest.MapFS{}}, ExitNotFound, "", "agent-monitor: no codex session ''\n")
}

// R-K1HM-NYLE R-K2PJ-1QC3 R-K3XF-FI2S R-KCGQ-3W9N R-JQIJ-80X5
func TestChatProduct(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{\"type\":\"user\",\"message\":{\"content\":\"hello\"}}\n{\"type\":\"assistant\",\"message\":{\"id\":\"msg\",\"model\":\"model\",\"content\":[{\"type\":\"text\",\"text\":\"hi\"}],\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n")},
	}
	want := "- user\nhello\n\n- assistant\nhi\n\ntokens: in 2  cache-write 0  cache-read 0  out 3  reasoning 0  calls 1\n"
	settings := []System{
		{},
		{Terminal: true},
		{Terminal: true, NoColor: "1"},
		{Terminal: true, Term: "dumb"},
		{Terminal: true, Term: "xterm"},
	}
	for _, args := range [][]string{{"chat", "claude", "sample"}, {"chat", "claude", "sample", "sample"}} {
		for _, setting := range settings {
			setting.Home = "/home/dev"
			setting.Root = root
			assertRun(t, args, setting, ExitSuccess, want, "")
		}
	}
	code, out, diag := runRecorded([]string{"chat", "claude", "sample"}, System{Home: "/home/dev", Root: root}, errors.New("full"), nil)
	if code != ExitWriteFailed || len(out.writes) != 1 || out.writes[0] != want || len(diag.writes) != 1 || diag.writes[0] != "agent-monitor: write error: full\n" {
		t.Errorf("chat write failure = %d %q %q", code, out.writes, diag.writes)
	}
}

// R-K2PJ-1QC3
func TestChatEmptyEntriesPrintTotals(t *testing.T) {
	root := fstest.MapFS{"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")}}
	assertRun(t, []string{"chat", "claude", "sample"}, System{Home: "/home/dev", Root: root}, ExitSuccess, "tokens: in 0  cache-write 0  cache-read 0  out 0  reasoning 0  calls 0\n", "")
}

// R-K8T0-YL1K R-KA0X-CCS9 R-KB8T-Q4IY
func TestChatAgentNotFound(t *testing.T) {
	root := fstest.MapFS{"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")}}
	args := []string{"chat", "claude", "sample", "a\nb"}
	code, out, diag := runRecorded(args, System{Home: "/home/dev", Root: root}, nil, errors.New("closed"))
	want := "agent-monitor: no claude agent 'a\\nb' in session 'sample'\n"
	if code != ExitNotFound || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
		t.Errorf("chat agent missing = %d %q %q", code, out.writes, diag.writes)
	}
}

// R-K6D8-71K6 R-KCGQ-3W9N
func TestChatReadError(t *testing.T) {
	root := fstest.MapFS{"home/dev/.claude/projects/work/sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")}}
	blocked := "home/dev/.claude/projects/work/sample.jsonl"
	sys := System{Home: "/home/dev", Root: &denyFileFS{root: root, name: blocked}}
	assertRun(t, []string{"chat", "claude", "sample"}, sys, ExitDataUnreadable, "", "agent-monitor: cannot read /"+blocked+": permission denied\n")
}

type denyFileFS struct {
	root fs.FS
	name string
	seen int
}

func (d *denyFileFS) Open(name string) (fs.File, error) {
	if name == d.name {
		d.seen++
		if d.seen == 2 {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
	}
	return d.root.Open(name)
}
