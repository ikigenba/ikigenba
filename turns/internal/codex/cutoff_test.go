package codex_test

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/codex"
	"github.com/ikigenba/ikigenba/turns/internal/session"
)

func modified(t *testing.T, path string, stamp time.Time) {
	t.Helper()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// R-MREY-JWTK R-KJBS-9KBX
func TestCutoffUsesEachRolloutTime(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	for name, stamp := range map[string]time.Time{"equal": since, "fraction": since.Add(time.Millisecond), "parent": since.Add(-time.Second), "child": since} {
		meta := metadata(name)
		if name == "child" {
			meta["payload"].(obj)["thread_source"] = "subagent"
			meta["payload"].(obj)["parent_thread_id"] = "parent"
		}
		path := put(t, root, live(name), lines(t, meta, person(start)))
		modified(t, path, stamp)
	}
	path := put(t, root, live("bad"), []byte("bad\n"))
	modified(t, path, since.Add(-time.Second))
	got := codex.Read(root, since, nil)
	var ids []string
	for _, value := range got.Sessions {
		ids = append(ids, value.Ref.ID)
		if value.Ref.ID == "child" && (value.Parent == nil || *value.Parent != (session.Ref{Tool: session.ToolCodex, ID: "parent"})) {
			t.Fatalf("parent=%+v", value)
		}
	}
	if got.Skipped != 0 || !reflect.DeepEqual(ids, []string{"child", "equal", "fraction"}) {
		t.Fatalf("got=%+v", got)
	}
}

// R-MSMU-XOK9
func TestNamedOldRolloutsAreRead(t *testing.T) {
	for _, name := range []string{live("job"), "archived_sessions/rollout-job.jsonl", live("job") + ".zst", "archived_sessions/rollout-job.jsonl.zst"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			since := time.Unix(1800000000, 0)
			meta := metadata("job")
			meta["payload"].(obj)["originator"] = "codex_exec"
			data := lines(t, meta, person(start))
			if name[len(name)-4:] == ".zst" {
				data = compress(t, data)
			}
			path := put(t, root, name, data)
			modified(t, path, since.Add(-time.Second))
			got := codex.Read(root, since, []string{"missing", "job"})
			if got.Skipped != 0 || len(got.Sessions) != 1 || got.Sessions[0].Ref.ID != "job" || !got.Sessions[0].Headless || got.Sessions[0].Parent != nil {
				t.Fatalf("got=%+v", got)
			}
		})
	}
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	path := put(t, root, live("bad"), append(lines(t, metadata("bad")), []byte("not JSON\n")...))
	modified(t, path, since.Add(-time.Second))
	got := codex.Read(root, since, []string{"bad", "missing"})
	if got.Skipped != 1 || len(got.Sessions) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

// R-MTUR-BGAY R-KJBS-9KBX
func TestOldRolloutsWithoutLearnableIDAreNotRead(t *testing.T) {
	blank := metadata("")
	absent := metadata("ignored")
	delete(absent["payload"].(obj), "id")
	number := metadata("ignored")
	number["payload"].(obj)["id"] = 17
	for _, test := range []struct {
		name           string
		data           []byte
		zipped, denied bool
	}{
		{name: "malformed-before-meta", data: append([]byte("not JSON\n"), lines(t, metadata("named"), person(start))...)},
		{name: "array-before-meta", data: append([]byte("[]\n"), lines(t, metadata("named"), person(start))...)},
		{name: "no-meta", data: lines(t, person(start))},
		{name: "missing-id", data: lines(t, absent, person(start))},
		{name: "number-id", data: lines(t, number, person(start))},
		{name: "empty-first-id", data: lines(t, blank, metadata("named"), person(start))},
		{name: "bad-zstd", data: []byte("not zstd"), zipped: true},
		{name: "unreadable", data: lines(t, metadata("named"), person(start)), denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			since := time.Unix(1800000000, 0)
			name := live("sample")
			if test.zipped {
				name += ".zst"
			}
			path := put(t, root, name, test.data)
			modified(t, path, since.Add(-time.Second))
			if test.denied {
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
			}
			got := codex.Read(root, since, []string{"", "named", "ignored"})
			if got.Skipped != 0 || len(got.Sessions) != 0 {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}

// R-I3AJ-YVL1
func TestUnreadLiveRolloutDoesNotHideReadArchive(t *testing.T) {
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	for _, test := range []struct {
		name, cwd string
		stamp     time.Time
	}{{live("same"), "/live", since.Add(-time.Second)}, {"archived_sessions/rollout-same.jsonl", "/archive", since}} {
		meta := metadata("same")
		meta["payload"].(obj)["cwd"] = test.cwd
		path := put(t, root, test.name, lines(t, meta, person(start)))
		modified(t, path, test.stamp)
	}
	got := codex.Read(root, since, nil)
	if got.Skipped != 0 || len(got.Sessions) != 1 || got.Sessions[0].Directory != "/archive" {
		t.Fatalf("got=%+v", got)
	}
}

// R-KI3V-VSL8 R-KKJO-NC2M
func TestZeroCutoffAndReadWholeRollout(t *testing.T) {
	for _, stamp := range []time.Time{time.Unix(-1, 0), time.Unix(2200000000, 0)} {
		root := t.TempDir()
		path := put(t, root, live("sample"), lines(t, metadata("sample"), person(start), person("2026-10-21T09:00:01Z")))
		modified(t, path, stamp)
		for _, ids := range [][]string{nil, {"unrelated"}} {
			all := codex.Read(root, time.Time{}, ids)
			if len(all.Sessions) != 1 || len(all.Sessions[0].Turns) != 2 || all.Skipped != 0 {
				t.Fatalf("all=%+v", all)
			}
			if stamp.After(time.Unix(0, 0)) {
				got := codex.Read(root, time.Unix(1800000000, 0), ids)
				if !reflect.DeepEqual(all, got) {
					t.Fatalf("all=%+v got=%+v", all, got)
				}
			}
		}
	}
	root := t.TempDir()
	since := time.Unix(1800000000, 0)
	path := put(t, root, live("bad"), []byte("bad\n"))
	modified(t, path, since)
	all, got := codex.Read(root, time.Time{}, nil), codex.Read(root, since, nil)
	if all.Skipped != 1 || !reflect.DeepEqual(all, got) {
		t.Fatalf("all=%+v got=%+v", all, got)
	}
}
