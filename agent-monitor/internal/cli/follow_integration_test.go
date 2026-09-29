package cli

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/codex"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type untouchedWatcher struct{}

func (untouchedWatcher) Watch([]string)           { panic("watcher touched") }
func (untouchedWatcher) Changes() <-chan struct{} { panic("watcher touched") }
func alreadyInterrupted() <-chan struct{}         { c := make(chan struct{}); close(c); return c }

// R-TM4Q-PQUC
func TestFollowOptionAtTopLevel(t *testing.T) {
	for _, arg := range []string{"-f", "--follow", "-f=x", "--follow=x"} {
		assertRun(t, []string{arg, "list", "claude"}, System{Root: panicFS{}, Watcher: untouchedWatcher{}}, ExitUsage, "", "agent-monitor: unknown option '"+arg+"'"+usageHint)
	}
}

// R-SHW2-3BDF R-SKBU-UUUT R-SNZK-062W R-SSV5-J91O
// R-SLJR-8MLI R-SP7G-DXTL R-SQFC-RPKA R-SU31-X0SD R-SVAY-ASJ2
func TestFollowGrammarErrors(t *testing.T) {
	sys := System{Home: "/home/dev", Root: panicFS{}, Watcher: untouchedWatcher{}, Interrupt: alreadyInterrupted()}
	for _, kind := range []string{"list", "tree", "chat"} {
		for _, options := range [][]string{nil, {"-f"}, {"--follow", "-f", "--follow"}} {
			args := append([]string{kind}, options...)
			assertRun(t, args, sys, ExitUsage, "", "agent-monitor: missing harness"+usageHint)
			if kind != "list" {
				args = append(args, "claude")
				assertRun(t, args, sys, ExitUsage, "", "agent-monitor: missing session id"+usageHint)
			}
		}
		for _, option := range []string{"--follow=x", "-f=x", "-fx", "-F", "--Follow", "--follow ", "--version", "-V", "-", "--", "--no-color=x", "--no-colour", "--No-Color"} {
			assertRun(t, []string{kind, "-f", option, "claude"}, sys, ExitUsage, "", "agent-monitor: unknown option '"+option+"'"+usageHint)
		}
		if kind != "tree" {
			assertRun(t, []string{kind, "-f", "--no-color", "claude"}, sys, ExitUsage, "", "agent-monitor: unknown option '--no-color'"+usageHint)
		}
		for _, h := range []string{"", "Claude", "claud"} {
			assertRun(t, []string{kind, "--follow", h, "-f"}, sys, ExitUsage, "", "agent-monitor: unknown harness '"+h+"'"+usageHint)
		}
		args := []string{kind, "--follow", "claude"}
		if kind != "list" {
			args = append(args, "sample")
		}
		if kind == "chat" {
			args = append(args, "agent")
		}
		args = append(args, "-f", "extra", "--follow")
		assertRun(t, args, sys, ExitUsage, "", "agent-monitor: unexpected argument 'extra'"+usageHint)
	}
	for _, args := range [][]string{{"tree", "--follow", "--no-color", "-f"}, {"tree", "--no-color", "--follow", "grok", "--no-color", "-f"}} {
		missing := "harness"
		if len(args) == 6 {
			missing = "session id"
		}
		assertRun(t, args, sys, ExitUsage, "", "agent-monitor: missing "+missing+usageHint)
	}
}

// R-TKWU-BZ3N R-X0PD-PEO4 R-X1XA-36ET R-T06J-TVHU R-AFT8-G88I
func TestNonfollowingCallsIgnoreWatcherAndRoot(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"--version"}, {"list", "--follow", "--help"}, {"tree", "-f", "--help"}, {"chat", "--follow", "--help"}, {"list", "-f", "claud"}, {"tree", "-f"}, {"chat", "--follow", "claude"}, {"list", "claude", "--follow=x", "-f"}} {
		stripped := []string{}
		for i, arg := range args {
			if i > 0 && (arg == "-f" || arg == "--follow") {
				continue
			}
			stripped = append(stripped, arg)
		}
		code, out, diag := runRecorded(stripped, System{}, nil, nil)
		assertRun(t, args, System{Home: "/anywhere", Root: panicFS{}, NoColor: "1", Term: "dumb", Terminal: true, Watcher: untouchedWatcher{}, Interrupt: alreadyInterrupted()}, code, strings.Join(out.writes, ""), strings.Join(diag.writes, ""))
	}
	for _, kind := range []string{"list", "tree", "chat"} {
		for _, h := range []string{"claude", "codex", "grok"} {
			args := []string{kind, "-f", h, "--follow"}
			if kind != "list" {
				args = append(args, "")
			}
			assertRun(t, args, System{Root: panicFS{}, Watcher: untouchedWatcher{}, Interrupt: alreadyInterrupted()}, ExitDataUnreadable, "", "agent-monitor: cannot find the home directory: HOME is not set\n")
		}
	}
}

func commandFixture() fstest.MapFS {
	return fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl":                                  &fstest.MapFile{Data: []byte("{\"type\":\"user\",\"message\":{\"content\":\"hello\"}}\n")},
		"home/dev/.codex/sessions/2026/01/01/rollout-2026-01-01T00-00-00-sample.jsonl": &fstest.MapFile{Data: []byte("{}\n")},
		"home/dev/.grok/sessions/work/sample/summary.json":                             &fstest.MapFile{Data: []byte("{}")},
	}
}

// R-SJ3Y-H344 R-SMRN-MEC7 R-SRN9-5HAZ R-TJOX-Y7CY
func TestFollowGrammarPositionsAndMode(t *testing.T) {
	for _, kind := range []string{"list", "tree", "chat"} {
		for _, h := range []string{"claude", "codex", "grok"} {
			base := []string{kind, h}
			if kind != "list" {
				base = append(base, "sample")
			}
			root := commandFixture()
			code, out, diag := runRecorded(base, System{Home: "/home/dev", Root: root}, nil, nil)
			if code != ExitSuccess {
				t.Fatalf("snapshot %q: %d %q", base, code, diag.writes)
			}
			expected := strings.Join(out.writes, "")
			if kind == "chat" {
				expected = expected[:strings.LastIndex(expected, "tokens:")]
			}
			for position := 1; position <= len(base); position++ {
				args := append([]string{}, base[:position]...)
				args = append(args, "-f", "--follow", "-f")
				args = append(args, base[position:]...)
				tracked := &dispatchRoot{root: root}
				sys := System{Home: "/home/dev", Root: tracked, Interrupt: alreadyInterrupted()}
				assertRun(t, args, sys, ExitSuccess, expected, "")
				for _, name := range tracked.names {
					for _, other := range []string{"claude", "codex", "grok"} {
						if other != h && strings.Contains(name, "."+other) {
							t.Errorf("%q dispatched to %s through %q", args, other, name)
						}
					}
				}
			}
			if kind == "tree" {
				assertRun(t, []string{kind, "--no-color", "-f", h, "--follow", "sample", "--no-color"}, System{Home: "/home/dev", Root: root, Interrupt: alreadyInterrupted()}, ExitSuccess, expected, "")
			}
			if kind == "chat" {
				assertRun(t, []string{kind, h, "-f", "sample", "--follow", "sample", "-f"}, System{Home: "/home/dev", Root: root, Interrupt: alreadyInterrupted()}, ExitSuccess, expected, "")
			}
		}
	}
	assertRun(t, []string{"chat", "--follow", "codex", "", "", "-f"}, System{Home: "/home/dev", Root: fstest.MapFS{}, Watcher: untouchedWatcher{}}, ExitNotFound, "", "agent-monitor: no codex session ''\n")
}

type dispatchRoot struct {
	root  fs.FS
	names []string
}

func (r *dispatchRoot) Open(name string) (fs.File, error) {
	r.names = append(r.names, name)
	return r.root.Open(name)
}

func directProduct(kind, h string, root fs.FS) (string, error) {
	if kind == "list" {
		var s []session.Session
		var err error
		switch h {
		case "claude":
			s, err = claude.List(root, "/home/dev")
		case "codex":
			s, err = codex.List(root, "/home/dev")
		case "grok":
			s, err = grok.List(root, "/home/dev")
		}
		return session.Table(s), err
	}
	if kind == "tree" {
		var value tree.Tree
		var err error
		switch h {
		case "claude":
			value, err = claude.Tree(root, "/home/dev", "sample")
		case "codex":
			value, err = codex.Tree(root, "/home/dev", "sample")
		case "grok":
			value, err = grok.Tree(root, "/home/dev", "sample")
		}
		return tree.Draw(value, false), err
	}
	var tr *chat.Transcript
	var entries []chat.Entry
	var err error
	switch h {
	case "claude":
		tr, entries, err = claude.Chat(root, "/home/dev", "sample", "sample")
	case "codex":
		tr, entries, err = codex.Chat(root, "/home/dev", "sample", "sample")
	case "grok":
		tr, entries, err = grok.Chat(root, "/home/dev", "sample", "sample")
	}
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for _, entry := range entries {
		text.WriteString(chat.Format(entry))
	}
	text.WriteString(chat.TotalsLine(tr.Usage(), tr.Recorded()))
	return text.String(), nil
}

// R-T1EG-7N8J R-T2MC-LEZ8 R-X5KZ-8HMW R-T6A1-QQ7B R-T7HY-4HY0
// R-X80S-014A R-J88L-KMGB R-TDLG-1CNH R-XAGK-RKLO R-TH95-6NVK R-AFT8-G88I
func TestSnapshotDispatchIsExactlyOneSelectedHarnessCall(t *testing.T) {
	for _, kind := range []string{"list", "tree", "chat"} {
		for _, h := range []string{"claude", "codex", "grok"} {
			expectedRoot := &dispatchRoot{root: commandFixture()}
			expected, err := directProduct(kind, h, expectedRoot)
			if err != nil {
				t.Fatal(err)
			}
			for _, terminal := range []bool{false, true} {
				root := &dispatchRoot{root: commandFixture()}
				args := []string{kind, h}
				if kind != "list" {
					args = append(args, "sample")
				}
				if kind == "tree" {
					args = append(args, "--no-color")
				}
				assertRun(t, args, System{Home: "/home/dev", Root: root, Terminal: terminal, NoColor: "1", Term: "dumb", Watcher: untouchedWatcher{}, Interrupt: alreadyInterrupted()}, ExitSuccess, expected, "")
				if !reflect.DeepEqual(root.names, expectedRoot.names) {
					t.Errorf("%s %s calls = %q, want one selected call %q", kind, h, root.names, expectedRoot.names)
				}
			}
		}
	}
}

// R-X6SV-M9DL R-X98O-DSUZ R-XBOH-5CCD R-TB5N-9T63 R-NC7A-KHTL R-NDF6-Y9KA
// R-5CA8-BZUX R-7CXO-L0W9
func TestFirstFollowFailureMatchesSnapshot(t *testing.T) {
	for _, kind := range []string{"list", "tree", "chat"} {
		for _, h := range []string{"claude", "codex", "grok"} {
			args := []string{kind, h}
			if kind != "list" {
				args = append(args, "missing")
			}
			for _, root := range []fs.FS{&deniedFS{}, fstest.MapFS{}} {
				if kind == "list" {
					if _, empty := root.(fstest.MapFS); empty {
						continue
					}
				}
				code, out, diag := runRecorded(args, System{Home: "/home/dev", Root: root}, nil, nil)
				argsFollow := append(append([]string{}, args...), "-f")
				assertRun(t, argsFollow, System{Home: "/home/dev", Root: root, Watcher: untouchedWatcher{}}, code, strings.Join(out.writes, ""), strings.Join(diag.writes, ""))
			}
		}
	}
	root := commandFixture()
	for _, args := range [][]string{{"chat", "claude", "sample", "missing"}, {"chat", "claude", "sample"}} {
		sys := System{Home: "/home/dev", Root: root, Watcher: untouchedWatcher{}}
		if len(args) == 3 {
			sys.Root = &integrationDenyFileFS{denyFileFS: &denyFileFS{root: root, name: "home/dev/.claude/projects/work/sample.jsonl"}}
		}
		code, out, diag := runRecorded(args, sys, nil, nil)
		if len(args) == 3 {
			sys.Root = &integrationDenyFileFS{denyFileFS: &denyFileFS{root: root, name: "home/dev/.claude/projects/work/sample.jsonl"}}
		}
		assertRun(t, append(args, "--follow"), sys, code, strings.Join(out.writes, ""), strings.Join(diag.writes, ""))
	}
	for _, args := range [][]string{nil, {"list", "claude"}, {"tree", "claude", "sample"}, {"chat", "claude", "sample"}} {
		code, out, diag := runRecorded(args, System{Home: "/home/dev", Root: root}, errors.New("full"), nil)
		if code != ExitWriteFailed || len(out.writes) != 1 || len(diag.writes) != 1 {
			t.Errorf("snapshot write failure: %d %q %q", code, out.writes, diag.writes)
		}
	}
}

// Stat and ReadDir do not consume the transcript-open fault counter.
type integrationDenyFileFS struct{ *denyFileFS }

func (d *integrationDenyFileFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(d.root, name) }
func (d *integrationDenyFileFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(d.root, name)
}

func (d *integrationDenyFileFS) Open(name string) (fs.File, error) {
	if name == d.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return d.root.Open(name)
}
