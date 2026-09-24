package cli

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

type recorder struct {
	writes []string
	err    error
}

func (r *recorder) Write(p []byte) (int, error) {
	r.writes = append(r.writes, string(p))
	if r.err != nil {
		return 0, r.err
	}
	return len(p), nil
}

func runRecorded(args []string, sys System, outErr, diagErr error) (ExitCode, recorder, recorder) {
	out, diag := recorder{err: outErr}, recorder{err: diagErr}
	code := Run(args, sys, &out, &diag)
	return code, out, diag
}

func assertRun(t *testing.T, args []string, sys System, wantCode ExitCode, wantOut, wantErr string) {
	t.Helper()
	code, out, diag := runRecorded(args, sys, nil, nil)
	if code != wantCode || strings.Join(out.writes, "") != wantOut || strings.Join(diag.writes, "") != wantErr {
		t.Errorf("Run(%q) = (%d, %q, %q); want (%d, %q, %q)", args, code, out.writes, diag.writes, wantCode, wantOut, wantErr)
	}
	if len(out.writes) > 1 || len(diag.writes) > 1 {
		t.Errorf("multiple writes: stdout %q stderr %q", out.writes, diag.writes)
	}
}

// R-EZ4D-QPN7 R-XK3U-55KN R-XLBQ-IXBC
func TestBareRunReturnsUsage(t *testing.T) {
	assertRun(t, nil, System{}, ExitSuccess, Usage, "")
}

// R-E0Z7-14VR R-2VB8-NS75 R-EBYA-H2K0 R-ED66-UUAP
func TestTopLevelClassification(t *testing.T) {
	for _, arg := range []string{"-", "--", "-hV", "-Vh", "--help=x", "--version=x", "--HELP", "-H"} {
		assertRun(t, []string{arg}, System{}, ExitUsage, "", "agent-monitor: unknown option '"+arg+"'"+usageHint)
	}
	for _, arg := range []string{"", "List", "status"} {
		assertRun(t, []string{arg}, System{}, ExitUsage, "", "agent-monitor: unknown command '"+arg+"'"+usageHint)
	}
	assertRun(t, []string{"--", "--help"}, System{}, ExitUsage, "", "agent-monitor: unknown option '--'"+usageHint)
}

// R-E273-EWMG
func TestTopLevelFirstArgumentWins(t *testing.T) {
	for _, first := range []string{"--help", "-h", "--version", "-V", "--", "bogus", ""} {
		base, outBase, errBase := runRecorded([]string{first}, System{}, nil, nil)
		for _, tail := range [][]string{{"list", "--help"}, {"\xff", "--version"}} {
			args := append([]string{first}, tail...)
			code, out, diag := runRecorded(args, System{}, nil, nil)
			if code != base || strings.Join(out.writes, "") != strings.Join(outBase.writes, "") || strings.Join(diag.writes, "") != strings.Join(errBase.writes, "") {
				t.Errorf("Run(%q) differed from Run(%q)", args, first)
			}
		}
	}
}

// R-E8AL-BRBX R-E9IH-PJ2M R-EFLZ-MDS3 R-EGTW-05IS R-EI1S-DX9H
func TestListGrammar(t *testing.T) {
	cases := []struct {
		args       []string
		diagnostic string
	}{
		{[]string{"list"}, "agent-monitor: missing harness" + usageHint},
		{[]string{"list", ""}, "agent-monitor: unknown harness ''" + usageHint},
		{[]string{"list", "Claude"}, "agent-monitor: unknown harness 'Claude'" + usageHint},
		{[]string{"list", "claud", "--bogus"}, "agent-monitor: unknown harness 'claud'" + usageHint},
		{[]string{"list", "--bogus", "claud"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"list", "claude", "--bogus"}, "agent-monitor: unknown option '--bogus'" + usageHint},
		{[]string{"list", "claude", "extra", "more"}, "agent-monitor: unexpected argument 'extra'" + usageHint},
		{[]string{"list", "claude", "--version"}, "agent-monitor: unknown option '--version'" + usageHint},
		{[]string{"list", "-", "claude"}, "agent-monitor: unknown option '-'" + usageHint},
	}
	for _, tc := range cases {
		assertRun(t, tc.args, System{}, ExitUsage, "", tc.diagnostic)
	}
}

// R-EJ9O-RP06 R-EKHL-5GQV
func TestNonlistingOutcomesIgnoreSystem(t *testing.T) {
	a := System{}
	b := System{Home: "/elsewhere", Root: panicFS{}}
	for _, args := range [][]string{nil, {"--help"}, {"--version"}, {"list", "--help"}, {"bogus"}, {"list"}, {"list", "claud"}} {
		codeA, outA, diagA := runRecorded(args, a, nil, nil)
		codeB, outB, diagB := runRecorded(args, b, nil, nil)
		if codeA != codeB || strings.Join(outA.writes, "") != strings.Join(outB.writes, "") || strings.Join(diagA.writes, "") != strings.Join(diagB.writes, "") {
			t.Errorf("Run(%q) depends on System", args)
		}
	}
}

type panicFS struct{}

func (panicFS) Open(string) (fs.File, error) { panic("Root accessed") }

// R-ELPH-J8HK R-ET0V-TUXQ R-ERSZ-G371
func TestMissingHome(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		args := []string{"list", h}
		want := "agent-monitor: cannot find the home directory: HOME is not set\n"
		assertRun(t, args, System{Root: panicFS{}}, ExitDataUnreadable, "", want)
		code, out, diag := runRecorded(args, System{Root: panicFS{}}, nil, errors.New("closed"))
		if code != ExitDataUnreadable || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
			t.Errorf("failed diagnostic write: %d %q %q", code, out.writes, diag.writes)
		}
	}
}

// R-EMXD-X089 R-EO5A-ARYY R-DZRA-ND52
func TestListEmptyHarnessData(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		code, out, diag := runRecorded([]string{"list", h}, System{Home: "/home/dev", Root: fstest.MapFS{}}, nil, nil)
		if code != ExitSuccess || len(out.writes) != 1 || out.writes[0] != session.Table(nil) || len(diag.writes) != 0 {
			t.Errorf("list %s: code %d, out %q, err %q", h, code, out.writes, diag.writes)
		}
	}
}

// R-EO5A-ARYY R-EMXD-X089
func TestListNonemptyHarnessData(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/sessions/a.json": &fstest.MapFile{Data: []byte(`{"sessionId":"alpha","pid":42,"status":"busy","name":"Implement feature","cwd":"/work"}`)},
		"proc/42/stat":                     &fstest.MapFile{Data: []byte("42 (a) " + strings.Repeat("0 ", 19) + "150")},
	}
	want := session.Table([]session.Session{{ID: "alpha", Status: session.StatusWorking, CWD: "/work", Title: "Implement feature"}})
	assertRun(t, []string{"list", "claude"}, System{Home: "/home/dev", Root: root}, ExitSuccess, want, "")
}

// R-2WJ5-1JXU R-2XR1-FBOJ R-2YYX-T3F8 R-31EQ-KMWM R-EQL3-2BGC R-EU8S-7MOF
func TestWriteFailuresAndOneWrite(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"--version"}, {"list", "--help"}} {
		code, out, diag := runRecorded(args, System{}, errors.New("disk full"), errors.New("closed"))
		if code != ExitWriteFailed || len(out.writes) != 1 || len(diag.writes) != 1 || diag.writes[0] != "agent-monitor: write error: disk full\n" {
			t.Errorf("Run(%q): code %d out %q err %q", args, code, out.writes, diag.writes)
		}
	}
	code, out, diag := runRecorded([]string{"list", "claude"}, System{Home: "/home/dev", Root: fstest.MapFS{}}, errors.New("disk full"), nil)
	if code != ExitWriteFailed || len(out.writes) != 1 || out.writes[0] != session.Table(nil) || len(diag.writes) != 1 || diag.writes[0] != "agent-monitor: write error: disk full\n" {
		t.Errorf("list write failure: code %d out %q err %q", code, out.writes, diag.writes)
	}
	for _, args := range [][]string{{"-bad"}, {"bad"}, {"list"}, {"list", "bad"}, {"list", "claude", "extra"}} {
		code, out, diag := runRecorded(args, System{}, nil, errors.New("closed"))
		if code != ExitUsage || len(out.writes) != 0 || len(diag.writes) != 1 {
			t.Errorf("Run(%q): code %d out %q err %q", args, code, out.writes, diag.writes)
		}
	}
}

type deniedFS struct{ opens []string }

func (d *deniedFS) Open(name string) (fs.File, error) {
	d.opens = append(d.opens, name)
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

// R-EPD6-OJPN R-EMXD-X089 R-ET0V-TUXQ R-ERSZ-G371
func TestHarnessReadErrors(t *testing.T) {
	for _, h := range []string{"claude", "codex", "grok"} {
		root := &deniedFS{}
		code, out, diag := runRecorded([]string{"list", h}, System{Home: "/home/it's", Root: root}, nil, errors.New("closed"))
		var wantPath string
		switch h {
		case "claude":
			wantPath = "home/it's/.claude/sessions"
		case "codex":
			wantPath = "home/it's/.codex/thread-writer-locks"
		case "grok":
			wantPath = "home/it's/.grok/active_sessions.json"
		}
		if len(root.opens) != 1 || root.opens[0] != wantPath {
			t.Errorf("%s opened %q, want exactly %q", h, root.opens, wantPath)
		}
		want := "agent-monitor: cannot read /" + wantPath + ": permission denied\n"
		if code != ExitDataUnreadable || len(out.writes) != 0 || len(diag.writes) != 1 || diag.writes[0] != want {
			t.Errorf("list %s: code %d out %q err %q, want %q", h, code, out.writes, diag.writes, want)
		}
	}
}

// R-EBYA-H2K0 R-ED66-UUAP R-EFLZ-MDS3 R-EGTW-05IS
func TestDiagnosticsEscapeArguments(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-a\nb"}, "agent-monitor: unknown option '-a\\nb'" + usageHint},
		{[]string{"it's"}, "agent-monitor: unknown command 'it\\'s'" + usageHint},
		{[]string{"list", "a\tb"}, "agent-monitor: unknown harness 'a\\tb'" + usageHint},
		{[]string{"list", "claude", "\x1b[31m"}, "agent-monitor: unexpected argument '\\x1b[31m'" + usageHint},
	}
	for _, tc := range cases {
		assertRun(t, tc.args, System{}, ExitUsage, "", tc.want)
	}
}
