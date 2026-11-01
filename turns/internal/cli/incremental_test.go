package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

func setModified(t testing.TB, path string, moment time.Time) {
	t.Helper()
	if err := os.Chtimes(path, moment, moment); err != nil {
		t.Fatal(err)
	}
}
func claudeSource(p paths.Paths, id string) string {
	return filepath.Join(p.Claude, "projects", "work", id+".jsonl")
}
func latestRefs(fake *pushFake) []session.Ref {
	var refs []session.Ref
	for i, r := range fake.requests {
		if r.Params.Arguments.Tool != space.LatestTool {
			continue
		}
		for _, s := range fake.sessions[i] {
			refs = append(refs, session.Ref{Tool: s.Tool, ID: s.ID})
		}
	}
	return refs
}
func resetPushRequests(fake *pushFake) { fake.requests = nil; fake.bodies = nil; fake.sessions = nil }
func assertPushWriteDiagnostic(t testing.TB, text, path string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "turns: could not write '"+strings.ReplaceAll(path, "\n", "\\n")+"'" || lines[1] != "" || lines[2] == "" || strings.HasPrefix(lines[2], "> ") || !strings.HasSuffix(text, "\n") {
		t.Fatalf("write diagnostic %q", text)
	}
}

// R-D31W-4175 R-D49S-HSXU: strict file reading is observed in the refs sent to the fake space.
func TestPushStrictLastPushCutoff(t *testing.T) {
	const valid = "2026-11-01T14:05:00Z"
	cutoff := time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC)
	for _, content := range []string{"absent", "directory", "unreadable", "", valid, valid + "\n", valid + "\n\n", valid + "\r\n", " " + valid, valid + " \n", "2026-11-01T14:05:00.5Z\n", "2026-11-01T14:05:00+00:00\n", "2026-11-01T4:05:00Z\n", "2026-13-01T14:05:00Z\n", "2026-11-01t14:05:00z\n"} {
		t.Run(fmt.Sprintf("%q", content), func(t *testing.T) {
			deps, p, fake := pushSetup(t)
			pushClaude(t, p, "old", 1)
			pushClaude(t, p, "boundary", 1)
			setModified(t, claudeSource(p, "old"), time.Date(2026, 11, 1, 4, 4, 59, 0, time.UTC))
			setModified(t, claudeSource(p, "boundary"), cutoff)
			path := filepath.Join(p.State, paths.LastPushFile)
			switch content {
			case "absent":
			case "directory":
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			default:
				text := content
				if content == "unreadable" {
					text = valid + "\n"
				}
				pushWrite(t, path, text)
				if content == "unreadable" {
					if err := os.Chmod(path, 0); err != nil {
						t.Fatal(err)
					}
				}
			}
			code, _, errText := pushRun(t, deps, "--dry-run")
			want := []session.Ref{{Tool: session.ToolClaude, ID: "boundary"}, {Tool: session.ToolClaude, ID: "old"}}
			if content == valid || content == valid+"\n" {
				want = want[:1]
			}
			if code != ExitOK || errText != "" || !reflect.DeepEqual(latestRefs(fake), want) {
				t.Fatalf("code=%d stderr=%q refs=%v want=%v", code, errText, latestRefs(fake), want)
			}
		})
	}
}

// R-5QBZ-LGLF: all four modes select by file modification time; full reads also bypass invalid cutoffs.
func TestPushModesAndChangedSession(t *testing.T) {
	cutoff := time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC)
	for _, args := range [][]string{nil, {"--dry-run"}, {"--all"}, {"--dry-run", "--all"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			deps, p, fake := pushSetup(t)
			for i, id := range []string{"old", "same-second", "new"} {
				pushClaude(t, p, id, 2)
				modified := []time.Time{cutoff.Add(-time.Second), cutoff.Add(500 * time.Millisecond), cutoff.Add(time.Second)}[i]
				setModified(t, claudeSource(p, id), modified)
			}
			pushWrite(t, filepath.Join(p.State, paths.LastPushFile), cutoff.Format(lastPushLayout)+"\n")
			code, out, errText := pushRun(t, deps, args...)
			want := []session.Ref{{Tool: session.ToolClaude, ID: "new"}, {Tool: session.ToolClaude, ID: "same-second"}}
			count := "2 sessions, 4 turns"
			for _, arg := range args {
				if arg == "--all" {
					want = []session.Ref{{Tool: session.ToolClaude, ID: "new"}, {Tool: session.ToolClaude, ID: "old"}, {Tool: session.ToolClaude, ID: "same-second"}}
					count = "3 sessions, 6 turns"
				}
			}
			if code != ExitOK || errText != "" || !reflect.DeepEqual(latestRefs(fake), want) || !strings.Contains(out, "claude: "+count) {
				t.Fatalf("%d %q %q refs=%v", code, out, errText, latestRefs(fake))
			}
		})
	}
	for _, kind := range []string{"absent", "directory", "yesterday"} {
		t.Run(kind, func(t *testing.T) {
			deps, p, fake := pushSetup(t)
			for i, id := range []string{"old", "same-second", "new"} {
				pushClaude(t, p, id, 1)
				setModified(t, claudeSource(p, id), cutoff.Add(time.Duration(i-1)*time.Second))
			}
			path := filepath.Join(p.State, paths.LastPushFile)
			switch kind {
			case "directory":
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "yesterday":
				pushWrite(t, path, kind)
			}
			code, _, _ := pushRun(t, deps)
			if len(latestRefs(fake)) != 3 {
				t.Fatalf("selected %v", latestRefs(fake))
			}
			if kind != "directory" && code != ExitOK {
				t.Fatal(code)
			}
		})
	}
	t.Run("second-success", func(t *testing.T) {
		deps, p, fake := pushSetup(t)
		for _, id := range []string{"a", "b", "c"} {
			pushClaude(t, p, id, 1)
			setModified(t, claudeSource(p, id), cutoff.Add(-time.Second))
		}
		deps.Now = func() time.Time { return cutoff }
		fake.latest = map[session.Ref]time.Time{}
		code, _, errText := pushRun(t, deps)
		if code != ExitOK || errText != "" {
			t.Fatalf("%d %q", code, errText)
		}
		pushClaude(t, p, "b", 3)
		setModified(t, claudeSource(p, "b"), cutoff.Add(2*time.Minute))
		deps.Now = func() time.Time { return cutoff.Add(5 * time.Minute) }
		resetPushRequests(fake)
		code, out, errText := pushRun(t, deps)
		want := []session.Ref{{Tool: session.ToolClaude, ID: "b"}}
		if code != ExitOK || errText != "" || !reflect.DeepEqual(latestRefs(fake), want) || out != "claude: 1 session, 2 turns pushed\ncodex: 0 sessions, 0 turns pushed\ngrok: 0 sessions, 0 turns pushed\n" {
			t.Fatalf("%d %q %q refs=%v", code, out, errText, latestRefs(fake))
		}
	})
}

// R-5XND-W31L: old malformed sessions do not contribute skipped lines until requested.
func TestPushSkippedRespectsCutoff(t *testing.T) {
	deps, p, fake := pushSetup(t)
	cutoff := time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC)
	path := claudeSource(p, "bad")
	pushWrite(t, path, "null\n")
	setModified(t, path, cutoff.Add(-time.Second))
	last := filepath.Join(p.State, paths.LastPushFile)
	for _, args := range [][]string{nil, {"--all"}} {
		pushWrite(t, last, cutoff.Format(lastPushLayout)+"\n")
		if len(args) == 0 && len(fake.bodies) > 0 {
			t.Fatal("malformed session made a request")
		}
		code, out, errText := pushRun(t, deps, args...)
		want := pushZero(false)
		if len(args) > 0 {
			want = "claude: 0 sessions, 0 turns pushed\nclaude: 1 session skipped\ncodex: 0 sessions, 0 turns pushed\ngrok: 0 sessions, 0 turns pushed\n"
		}
		if code != ExitOK || out != want || errText != "" {
			t.Fatalf("%d %q %q", code, out, errText)
		}
	}
	setModified(t, path, cutoff)
	pushWrite(t, last, cutoff.Format(lastPushLayout)+"\n")
	code, out, errText := pushRun(t, deps)
	if code != ExitOK || errText != "" || !strings.Contains(out, "claude: 1 session skipped\n") {
		t.Fatalf("%d %q %q", code, out, errText)
	}
}

// R-5V7L-4JK7: a quiet incremental or full run reports zero and only a real push advances state.
func TestPushQuietSelection(t *testing.T) {
	cutoff := time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC)
	for _, args := range [][]string{nil, {"--dry-run"}, {"--all"}, {"--dry-run", "--all"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			deps, p, fake := pushSetup(t)
			dry, all := false, false
			for _, arg := range args {
				dry = dry || arg == "--dry-run"
				all = all || arg == "--all"
			}
			if !all {
				pushClaude(t, p, "old", 1)
				setModified(t, claudeSource(p, "old"), cutoff.Add(-time.Second))
			}
			path := filepath.Join(p.State, paths.LastPushFile)
			before := cutoff.Format(lastPushLayout) + "\n"
			pushWrite(t, path, before)
			started := cutoff.Add(time.Hour)
			deps.Now = func() time.Time { return started }
			code, out, errText := pushRun(t, deps, args...)
			data, err := pushRead(path)
			want := started.Format(lastPushLayout) + "\n"
			if dry {
				want = before
			}
			if code != ExitOK || errText != "" || out != pushZero(dry) || len(fake.requests) != 0 || err != nil || string(data) != want {
				t.Fatalf("%d %q %q requests=%d state=%q err=%v", code, out, errText, len(fake.requests), data, err)
			}
		})
	}
}

// R-5TZO-QRTI R-5SRS-D02T R-D1TZ-Q9GG: first injected clock result is written in UTC with fractions dropped.
func TestPushStartMoment(t *testing.T) {
	for _, args := range [][]string{nil, {"--all"}} {
		for _, previous := range []string{"", "longer old contents that must be replaced\n"} {
			t.Run(fmt.Sprint(args, previous), func(t *testing.T) {
				deps, p, _ := pushSetup(t)
				path := filepath.Join(p.State, paths.LastPushFile)
				if previous != "" {
					pushWrite(t, path, previous)
				}
				started := time.Date(2026, 11, 1, 9, 5, 0, 999000000, time.FixedZone("west", -5*3600))
				calls := 0
				deps.Now = func() time.Time { answer := started.Add(time.Duration(calls) * time.Second); calls++; return answer }
				code, _, errText := pushRun(t, deps, args...)
				data, err := pushRead(path)
				if code != ExitOK || errText != "" || err != nil || string(data) != "2026-11-01T14:05:00Z\n" {
					t.Fatalf("%d %q state=%q %v", code, errText, data, err)
				}
			})
		}
	}
}

// R-5NW6-TX41 R-5LGE-2DMN: repeated flags and their order preserve the full observable run.
func TestPushOptionEquivalence(t *testing.T) {
	for _, pair := range [][][]string{{{"--dry-run"}, {"--dry-run", "--dry-run"}}, {{"--all"}, {"--all", "--all"}}, {{"--dry-run", "--all"}, {"--all", "--dry-run", "--all"}}, {{"--dry-run", "--all"}, {"--all", "--dry-run"}}} {
		t.Run(fmt.Sprint(pair), func(t *testing.T) {
			deps, p, fake := pushSetup(t)
			pushClaude(t, p, "a", 3)
			cutoff := time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC)
			setModified(t, claudeSource(p, "a"), cutoff.Add(-time.Second))
			path := filepath.Join(p.State, paths.LastPushFile)
			initial := cutoff.Format(lastPushLayout) + "\n"
			pushWrite(t, path, initial)
			code, out, errText := pushRun(t, deps, pair[0]...)
			requests := append([][]byte(nil), fake.bodies...)
			after, err := pushRead(path)
			if err != nil {
				t.Fatal(err)
			}
			pushWrite(t, path, initial)
			resetPushRequests(fake)
			code2, out2, errText2 := pushRun(t, deps, pair[1]...)
			after2, err := pushRead(path)
			if code != ExitOK || code2 != code || out2 != out || errText2 != errText || !reflect.DeepEqual(fake.bodies, requests) || err != nil || !bytes.Equal(after, after2) {
				t.Fatalf("different equivalent runs: %d/%d %q/%q %q/%q", code, code2, out, out2, errText, errText2)
			}
		})
	}
}

// R-D5HO-VKOJ: configure preserves existing state including permission bits and modification time, or absence.
func TestConfigurePreservesLastPush(t *testing.T) {
	for _, present := range []bool{false, true} {
		for _, kind := range []string{"success", "refused", "failure"} {
			t.Run(fmt.Sprint(present, kind), func(t *testing.T) {
				deps, p, _ := pushSetup(t)
				pushWrite(t, filepath.Join(p.Config, paths.ConfigFile), "space=us.ikigenba.dev\n")
				path := filepath.Join(p.State, paths.LastPushFile)
				var before os.FileInfo
				if present {
					writeFixture(t, path, "2026-11-01T14:05:00Z\n", 0640)
					setModified(t, path, time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
					var err error
					before, err = os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"configure", "sbx.ikigenba.dev", "--token", "t"}
				want := ExitOK
				if kind == "refused" {
					args[1] = "not-a-space-name"
					want = ExitUsage
				}
				if kind == "failure" {
					if err := os.Remove(filepath.Join(p.Config, paths.ConfigFile)); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(p.Config, paths.ConfigFile), 0700); err != nil {
						t.Fatal(err)
					}
					want = ExitFailed
				}
				code, _, _ := runCLI(t, deps, args, nil)
				if code != want {
					t.Fatalf("code=%d want=%d", code, want)
				}
				data, err := pushRead(path)
				if !present {
					if !os.IsNotExist(err) {
						t.Fatalf("state created: %v", err)
					}
					return
				}
				after, statErr := os.Stat(path)
				if err != nil || statErr != nil || string(data) != "2026-11-01T14:05:00Z\n" || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("state changed: %q %v %v", data, err, statErr)
				}
			})
		}
	}
}

// R-1K4U-QXGK R-1IWY-D5PV: status counts and asks about all tools regardless of state cutoff.
func TestStatusAlwaysReadsEverySession(t *testing.T) {
	t.Run("absent-roots", func(t *testing.T) {
		deps, _ := syntheticDeps(t)
		code, out, errText := statusRun(t, deps)
		if code != ExitOK || errText != "" || out != statusZero("none", "none", "never", "") {
			t.Fatalf("%d %q %q", code, out, errText)
		}
	})

	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			deps, p, fake, _ := statusSetup(t)
			cutoff := time.Date(2026, 11, 1, 14, 5, 0, 0, time.UTC)
			for _, tool := range session.Tools() {
				for _, id := range []string{"old", "new"} {
					statusSession(t, p, tool, id, 3)
					modified := cutoff.Add(-time.Second)
					if mixed && id == "new" {
						modified = cutoff.Add(time.Second)
					}
					switch tool {
					case session.ToolClaude:
						setModified(t, claudeSource(p, id), modified)
					case session.ToolCodex:
						setModified(t, filepath.Join(p.Codex, "sessions", "a", "b", "c", "rollout-"+id+".jsonl"), modified)
					case session.ToolGrok:
						dir := filepath.Join(p.Grok, "sessions", "%2Fwork", id)
						setModified(t, filepath.Join(dir, "summary.json"), modified)
						setModified(t, filepath.Join(dir, "updates.jsonl"), modified)
						setModified(t, dir, modified)
					}
					if id == "old" {
						fake.latest[session.Ref{Tool: tool, ID: id}] = time.Unix(101, 0)
					}
				}
			}
			code, without, errText := statusRun(t, deps)
			requests := append([][]session.Ref(nil), fake.requests...)
			if code != ExitOK || errText != "" || len(requests) != 1 || len(requests[0]) != 6 {
				t.Fatalf("%d %q refs=%v", code, errText, requests)
			}
			for _, tool := range session.Tools() {
				want := fmt.Sprintf("%s: 2 sessions, 6 turns here; 1 session, 2 turns on the space", tool)
				if !strings.Contains(without, want) {
					t.Fatalf("report %q missing %q", without, want)
				}
			}
			pushWrite(t, filepath.Join(p.State, paths.LastPushFile), cutoff.Format(lastPushLayout)+"\n")
			fake.requests = nil
			code, with, errText := statusRun(t, deps)
			tools := func(report string) string { return strings.Join(strings.Split(report, "\n")[3:], "\n") }
			if code != ExitOK || errText != "" || tools(with) != tools(without) || !reflect.DeepEqual(fake.requests, requests) {
				t.Fatalf("status depends on cutoff: %d %q %q refs=%v", code, with, errText, fake.requests)
			}
		})
	}
}

// R-83NP-N7NB: the real clock is sampled before the first fake request even outside UTC.
func TestNilClockStartPrecedesFirstRequest(t *testing.T) {
	deps, p, fake := pushSetup(t)
	pushClaude(t, p, "a", 1)
	deps.Now = nil
	oldLocal := time.Local
	time.Local = time.FixedZone("fixture offset", 5*3600)
	t.Cleanup(func() { time.Local = oldLocal })
	var first time.Time
	fake.hook = func(*http.Request) {
		if first.IsZero() {
			first = time.Now()
		}
	}
	before := time.Now().Truncate(time.Second)
	code, _, errText := pushRun(t, deps)
	data, err := os.ReadFile(filepath.Join(p.State, paths.LastPushFile))
	moment, parseErr := time.Parse("2006-01-02T15:04:05Z\n", string(data))
	if code != ExitOK || errText != "" || err != nil || parseErr != nil || moment.Before(before) || first.IsZero() || moment.After(first) {
		t.Fatalf("%d %q moment=%v before=%v first=%v errs=%v/%v", code, errText, moment, before, first, err, parseErr)
	}
}
