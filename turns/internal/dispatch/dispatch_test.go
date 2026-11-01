package dispatch_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/dispatch"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// R-ZJM2-5AAF R-ZKTY-J214 R-ZN9R-ALII R-ZOHN-OD97 R-ZPPK-24ZW R-ZS5C-TOHA
func TestJobsReadableEntries(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"z": "child-z\n", "a": "child-a", "m": "  child-m \r\n", "empty": "", "space": " \t\r\n"} {
		write(t, filepath.Join(root, name, "session-id"), content)
	}
	if err := os.MkdirAll(filepath.Join(root, "missing"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "directory", "session-id"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "regular-job"), "ignored")
	unreadable := filepath.Join(root, "unreadable", "session-id")
	write(t, unreadable, "hidden")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(unreadable, 0o600); err != nil {
			t.Error(err)
		}
	})
	external := t.TempDir()
	write(t, filepath.Join(external, "session-id"), "linked-dir\n")
	symlink(t, external, filepath.Join(root, "b-link"))
	if err := os.MkdirAll(filepath.Join(root, "c-link"), 0o700); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(root, "a", "session-id"), filepath.Join(root, "c-link", "session-id"))
	symlink(t, filepath.Join(root, "absent"), filepath.Join(root, "broken"))
	reader := struct {
		Jobs func(string) []dispatch.Job
	}{Jobs: dispatch.Jobs}
	got := reader.Jobs(root)
	want := []dispatch.Job{
		{filepath.Join(root, "a"), "child-a"},
		{Dir: filepath.Join(root, "b-link"), SessionID: "linked-dir"},
		{Dir: filepath.Join(root, "c-link"), SessionID: "child-a"},
		{Dir: filepath.Join(root, "m"), SessionID: "child-m"},
		{Dir: filepath.Join(root, "z"), SessionID: "child-z"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Jobs = %#v, want %#v", got, want)
	}
}

// R-ZQXG-FWQL
func TestJobsUnavailableRoot(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "absent")
	file := filepath.Join(root, "file")
	write(t, file, "data")
	locked := filepath.Join(root, "locked")
	write(t, filepath.Join(locked, "job", "session-id"), "child")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		chmod(t, locked, 0o700)
	})
	for _, dir := range []string{missing, file, locked} {
		if got := dispatch.Jobs(dir); len(got) != 0 {
			t.Fatalf("Jobs(%q) = %#v", dir, got)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing root created: %v", err)
	}
}

type fileState struct {
	Mode    os.FileMode
	ModTime time.Time
	Data    string
}

func snapshot(t *testing.T, root string) map[string]fileState {
	t.Helper()
	directory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	states := make(map[string]fileState)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		state := fileState{Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode().IsRegular() {
			name, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := directory.ReadFile(name)
			if readErr != nil {
				return readErr
			}
			state.Data = string(data)
		}
		states[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return states
}

// R-ZTD9-7G7Z
func TestJobsLeavesSourcesUnchanged(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "valid", "session-id"), "child\n")
	write(t, filepath.Join(root, "old", "notes"), "other bytes")
	before := snapshot(t, root)
	if got := dispatch.Jobs(root); len(got) != 1 {
		t.Fatalf("Jobs = %#v", got)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("source changed: before %#v, after %#v", before, after)
	}
}

// R-ZM1U-WTRT R-ZUL5-L7YO R-ZVT1-YZPD R-ZY8U-QJ6R R-0801-SP4B
func TestLinkChoosesEarliestAcrossAllJobsAndTools(t *testing.T) {
	later := time.Unix(20, 0).UTC()
	earlier := time.Unix(10, 0).In(time.FixedZone("offset", 7*60*60))
	command := &session.Command{Name: "/task", Argument: "arg"}
	turns := []session.Turn{{Moment: later, Whose: session.WhoseAgent, Model: "model", Tokens: session.Tokens{Input: 4}}}
	sessions := []session.Session{
		{Ref: session.Ref{Tool: session.ToolCodex, ID: "shared"}, Directory: "/work", Headless: true, Command: command, Turns: turns},
		{Ref: session.Ref{Tool: session.ToolGrok, ID: "shared"}, Directory: "/other", Turns: turns},
	}
	jobs := []dispatch.Job{{Dir: "/jobs/first", SessionID: "shared"}, {Dir: "/jobs/second", SessionID: "shared"}}
	parent := session.Ref{Tool: session.ToolClaude, ID: "earliest"}
	mentions := []session.Mention{{Path: jobs[0].Dir, By: session.Ref{Tool: session.ToolClaude, ID: "late"}, Moment: later}, {Path: jobs[1].Dir, By: parent, Moment: earlier}}
	linker := struct {
		Link func([]session.Session, []dispatch.Job, []session.Mention) []session.Session
	}{Link: dispatch.Link}
	got := linker.Link(sessions, jobs, mentions)
	want := append([]session.Session(nil), sessions...)
	for i := range want {
		want[i].Parent = &parent
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Link = %#v, want %#v", got, want)
	}
}

// R-ZX0Y-CRG2
func TestLinkTieBreakIsIndependentOfOrder(t *testing.T) {
	child := []session.Session{{Ref: session.Ref{Tool: session.ToolGrok, ID: "child"}}}
	jobs := []dispatch.Job{{Dir: "/job", SessionID: "child"}}
	moment := time.Unix(10, 0)
	want := session.Ref{Tool: session.ToolClaude, ID: "a"}
	mentions := []session.Mention{
		{Path: "/job", By: session.Ref{Tool: session.ToolClaude, ID: "b"}, Moment: moment},
		{Path: "/job", By: session.Ref{Tool: session.ToolGrok, ID: "a"}, Moment: moment.In(time.FixedZone("west", -3600))},
		{Path: "/job", By: want, Moment: moment},
	}
	for i := 0; i < len(mentions); i++ {
		for j := 0; j < len(mentions); j++ {
			mentions[i], mentions[j] = mentions[j], mentions[i]
			got := dispatch.Link(child, jobs, mentions)
			if got[0].Parent == nil || *got[0].Parent != want {
				t.Fatalf("parent = %v, want %v", got[0].Parent, want)
			}
		}
	}
}

// R-ZZGR-4AXG R-04CC-NDW8
func TestLinkRequiresExactJobAndPath(t *testing.T) {
	child := []session.Session{{Ref: session.Ref{Tool: session.ToolCodex, ID: "child"}}}
	job := dispatch.Job{Dir: "/Jobs/task", SessionID: "child"}
	for _, path := range []string{job.Dir + "/", strings.ToLower(job.Dir), job.Dir + "/session-id", "/unrelated"} {
		mentions := []session.Mention{{Path: path, By: session.Ref{Tool: session.ToolClaude, ID: "parent"}}}
		if got := dispatch.Link(child, []dispatch.Job{job}, mentions); got[0].Parent != nil {
			t.Fatalf("path %q linked: %v", path, got[0].Parent)
		}
	}
	for _, jobs := range [][]dispatch.Job{nil, {{Dir: job.Dir, SessionID: "different"}}} {
		got := dispatch.Link(child, jobs, []session.Mention{{Path: job.Dir, By: session.Ref{Tool: session.ToolClaude, ID: "parent"}}})
		if got[0].Parent != nil {
			t.Fatal("unmatched job linked")
		}
	}
	if got := dispatch.Link(child, []dispatch.Job{job}, nil); got[0].Parent != nil {
		t.Fatal("unmentioned job linked")
	}
}

// R-00ON-I2O5 R-034G-9M5J
func TestLinkPreservesExistingAndIneligibleParents(t *testing.T) {
	parent := session.Ref{Tool: session.ToolCodex, ID: "native"}
	sessions := []session.Session{
		{Ref: session.Ref{Tool: session.ToolCodex, ID: "child"}, Parent: &parent},
		{Ref: session.Ref{Tool: session.ToolGrok, ID: "child"}, Parent: &parent},
		{Ref: session.Ref{Tool: session.ToolClaude, ID: "child"}},
		{Ref: session.Ref{Tool: "other", ID: "child"}, Parent: &parent},
	}
	got := dispatch.Link(sessions, []dispatch.Job{{Dir: "/job", SessionID: "child"}}, []session.Mention{{Path: "/job", By: session.Ref{Tool: session.ToolClaude, ID: "dispatch"}}})
	if !reflect.DeepEqual(got, sessions) {
		t.Fatalf("ineligible sessions changed: %#v", got)
	}
}

// R-05K9-15MX R-06S5-EXDM
func TestLinkDisregardsSelfAndEmptyIdentity(t *testing.T) {
	child := session.Session{Ref: session.Ref{Tool: session.ToolGrok, ID: "child"}}
	job := []dispatch.Job{{Dir: "/job", SessionID: "child"}}
	invalid := []session.Mention{{Path: "/job", By: child.Ref, Moment: time.Unix(1, 0)}, {Path: "/job", By: session.Ref{Tool: session.ToolClaude}, Moment: time.Unix(2, 0)}}
	if got := dispatch.Link([]session.Session{child}, job, invalid); got[0].Parent != nil {
		t.Fatal("invalid mention supplied parent")
	}
	parent := session.Ref{Tool: session.ToolClaude, ID: "valid"}
	invalid = append(invalid, session.Mention{Path: "/job", By: parent, Moment: time.Unix(3, 0)})
	got := dispatch.Link([]session.Session{child}, job, invalid)
	if got[0].Parent == nil || *got[0].Parent != parent {
		t.Fatalf("next eligible mention ignored: %#v", got)
	}
}

// R-N6Q1-C2OI
func TestLinkDoesNotAliasInputs(t *testing.T) {
	parent := session.Ref{Tool: session.ToolGrok, ID: "native"}
	sessions := []session.Session{{Ref: session.Ref{Tool: session.ToolCodex, ID: "child"}}, {Ref: session.Ref{Tool: session.ToolGrok, ID: "other"}, Parent: &parent}}
	jobs := []dispatch.Job{{Dir: "/job", SessionID: "child"}}
	mentions := []session.Mention{{Path: "/job", By: session.Ref{Tool: session.ToolClaude, ID: "dispatch"}, Moment: time.Unix(4, 0)}}
	beforeSessions := append([]session.Session(nil), sessions...)
	beforeJobs := append([]dispatch.Job(nil), jobs...)
	beforeMentions := append([]session.Mention(nil), mentions...)
	got := dispatch.Link(sessions, jobs, mentions)
	if !reflect.DeepEqual(sessions, beforeSessions) || !reflect.DeepEqual(jobs, beforeJobs) || !reflect.DeepEqual(mentions, beforeMentions) {
		t.Fatal("Link modified arguments")
	}
	got[0].Directory = "/changed"
	got[0].Parent.ID = "changed-parent"
	got[1].Parent.ID = "changed-native"
	if sessions[0].Directory != "" || sessions[0].Parent != nil || parent.ID != "native" || mentions[0].By.ID != "dispatch" {
		t.Fatal("returned value aliases input")
	}
}
