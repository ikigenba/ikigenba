package grok_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/grok"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

func modified(t *testing.T, path string, stamp time.Time) {
	t.Helper()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}
func sourceTimes(t *testing.T, path string, directory, updates time.Time) {
	t.Helper()
	modified(t, filepath.Join(path, "updates.jsonl"), updates)
	modified(t, path, directory)
}

// R-9NIM-HGED R-PD62-S0HE R-KJBS-9KBX
func TestCutoffReadsDirectoryOrUpdatesChanged(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	old := since.Add(-time.Second)
	for _, test := range []struct {
		name               string
		directory, updates time.Time
	}{{"directory", since, old}, {"updates", old, since}, {"fraction", old, since.Add(time.Millisecond)}, {"old", old, old}} {
		updates := person(1)
		if test.name == "old" {
			updates = "bad\n"
		}
		path := source(t, root, encodedCWD, test.name, `{}`, updates)
		sourceTimes(t, path, test.directory, test.updates)
	}
	// Without updates, directory mtime alone selects the source and its skip.
	missing := source(t, root, encodedCWD, "missing", `{}`, "")
	if err := os.Remove(filepath.Join(missing, "updates.jsonl")); err != nil {
		t.Fatal(err)
	}
	modified(t, missing, since)
	got := grok.Read(root, since, nil)
	var ids []string
	for _, value := range got.Sessions {
		ids = append(ids, value.Ref.ID)
	}
	if got.Skipped != 1 || !reflect.DeepEqual(ids, []string{"directory", "fraction", "updates"}) {
		t.Fatalf("got=%+v", got)
	}
	modified(t, missing, old)
	got = grok.Read(root, since, nil)
	if got.Skipped != 0 || len(got.Sessions) != 3 {
		t.Fatalf("old missing=%+v", got)
	}
}

// R-LWZ5-T1KY
func TestUnknownUpdatesModificationTimeIsReadAndSkipped(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	path := source(t, root, encodedCWD, "denied", `{}`, person(1))
	sourceTimes(t, path, since.Add(-time.Second), since.Add(-time.Second))
	chmod(t, path, 0o400, 0o700)
	got := grok.Read(root, since, nil)
	if got.Skipped != 1 || len(got.Sessions) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

// R-PFLV-JJYS
func TestNamedOldDirectoriesUseSummaryIdentity(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	old := since.Add(-time.Second)
	for _, test := range []struct{ name, summary, updates string }{{"named", `{}`, person(1)}, {"unnamed", `{}`, person(2)}, {"different-name", `{"info":{"id":"actual"}}`, person(3)}, {"directory-only", `{"info":{"id":"not-named"}}`, person(4)}, {"bad", `{}`, "bad\n"}} {
		path := source(t, root, encodedCWD, test.name, test.summary, test.updates)
		sourceTimes(t, path, old, old)
	}
	got := grok.Read(root, since, []string{"named", "actual", "directory-only", "bad", "missing"})
	var ids []string
	for _, value := range got.Sessions {
		ids = append(ids, value.Ref.ID)
	}
	if got.Skipped != 1 || !reflect.DeepEqual(ids, []string{"actual", "named"}) {
		t.Fatalf("got=%+v", got)
	}
}

// R-PGTR-XBPH R-KJBS-9KBX
func TestOldDirectoriesWithoutLearnableIDAreNotRead(t *testing.T) {
	for _, test := range []struct {
		name, summary     string
		denied, readError bool
	}{{name: "missing"}, {name: "malformed", summary: "bad"}, {name: "array", summary: "[]"}, {name: "trailing", summary: "{} {}"}, {name: "null", summary: "null"}, {name: "denied", summary: "{}", denied: true}, {name: "read-error", summary: "{}", readError: true}} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			since := time.Unix(1800000000, 0)
			old := since.Add(-time.Second)
			path := source(t, root, encodedCWD, test.name, test.summary, "bad\n")
			summary := filepath.Join(path, "summary.json")
			if test.denied {
				chmod(t, summary, 0, 0o600)
			}
			if test.readError {
				if err := os.Remove(summary); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(summary, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			sourceTimes(t, path, old, old)
			got := grok.Read(root, since, []string{test.name, ""})
			if got.Skipped != 0 || len(got.Sessions) != 0 {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}

// R-2B5F-3OTZ
func TestReadChildFindsUnreadSpawningParent(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	old := since.Add(-time.Second)
	parent := source(t, root, encodedCWD, "parent", `{}`, "bad\n")
	child := source(t, root, encodedCWD, "child", `{}`, person(1))
	if err := os.MkdirAll(filepath.Join(parent, "subagents", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	sourceTimes(t, parent, old, old)
	sourceTimes(t, child, old, since)
	got := grok.Read(root, since, nil)
	if got.Skipped != 0 || len(got.Sessions) != 1 || got.Sessions[0].Ref.ID != "child" || got.Sessions[0].Parent == nil || *got.Sessions[0].Parent != (session.Ref{Tool: session.ToolGrok, ID: "parent"}) {
		t.Fatalf("got=%+v", got)
	}
}

// R-PEDZ-5S83
func TestUnreadLesserDirectoryDoesNotHideReadDuplicate(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	old := since.Add(-time.Second)
	lesser := source(t, root, "%2Fa", "first", `{"info":{"id":"same"}}`, person(1))
	sourceTimes(t, lesser, old, old)
	greater := source(t, root, "%2Fz", "second", `{"info":{"id":"same"}}`, person(2))
	sourceTimes(t, greater, old, since)
	got := grok.Read(root, since, nil)
	if got.Skipped != 0 || len(got.Sessions) != 1 || got.Sessions[0].Directory != "/z" {
		t.Fatalf("got=%+v", got)
	}
}

// R-KI3V-VSL8 R-KKJO-NC2M
func TestZeroCutoffAndWholeDirectory(t *testing.T) {
	for _, stamp := range []time.Time{time.Unix(-1, 0), time.Unix(2200000000, 0)} {
		root := t.TempDir()
		path := source(t, root, encodedCWD, "sample", `{}`, person(1)+person(2))
		sourceTimes(t, path, stamp, stamp)
		for _, ids := range [][]string{nil, {"unrelated"}} {
			all := grok.Read(root, time.Time{}, ids)
			if all.Skipped != 0 || len(all.Sessions) != 1 || len(all.Sessions[0].Turns) != 2 {
				t.Fatalf("all=%+v", all)
			}
			if stamp.After(time.Unix(0, 0)) {
				got := grok.Read(root, time.Unix(1800000000, 0), ids)
				if !reflect.DeepEqual(all, got) {
					t.Fatalf("all=%+v got=%+v", all, got)
				}
			}
		}
	}
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	path := source(t, root, encodedCWD, "bad", `{}`, "bad\n")
	sourceTimes(t, path, since, since)
	all, got := grok.Read(root, time.Time{}, nil), grok.Read(root, since, nil)
	if all.Skipped != 1 || !reflect.DeepEqual(all, got) {
		t.Fatalf("all=%+v got=%+v", all, got)
	}
}
