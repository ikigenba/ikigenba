package codex

import (
	"errors"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

const (
	home     = "/home/dev"
	base     = "home/dev/.codex/"
	lockDir  = base + "thread-writer-locks"
	sessions = base + "sessions/"
	index    = base + "session_index.jsonl"
)

type observedFS struct {
	fstest.MapFS
	calls []string
	fail  map[string]error
}

func (f *observedFS) record(op, name string) error {
	f.calls = append(f.calls, op+":"+name)
	return f.fail[op+":"+name]
}

func (f *observedFS) Open(name string) (fs.File, error) {
	if err := f.record("open", name); err != nil {
		return nil, err
	}
	return f.MapFS.Open(name)
}

func (f *observedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := f.record("dir", name); err != nil {
		return nil, err
	}
	return f.MapFS.ReadDir(name)
}

func (f *observedFS) ReadFile(name string) ([]byte, error) {
	if err := f.record("read", name); err != nil {
		return nil, err
	}
	return f.MapFS.ReadFile(name)
}

func (f *observedFS) Stat(name string) (fs.FileInfo, error) {
	if err := f.record("stat", name); err != nil {
		return nil, err
	}
	return f.MapFS.Stat(name)
}

func (f *observedFS) ReadLink(name string) (string, error) {
	if err := f.record("link", name); err != nil {
		return "", err
	}
	return f.MapFS.ReadLink(name)
}

func fixture() *observedFS {
	return &observedFS{MapFS: fstest.MapFS{}, fail: map[string]error{}}
}

func (f *observedFS) lock(id string, inode uint64, pid int) {
	f.MapFS[lockDir+"/"+id+".lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: inode}}
	var prior []byte
	if file := f.MapFS["proc/locks"]; file != nil {
		prior = file.Data
	}
	f.MapFS["proc/locks"] = &fstest.MapFile{Data: []byte(string(prior) + "1: POSIX ADVISORY WRITE " + strconv.Itoa(pid) + " 08:01:" + strconv.FormatUint(inode, 10) + " 0 EOF\n")}
	f.MapFS["proc/"+strconv.Itoa(pid)+"/cwd"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("/work/" + id)}
}

func (f *observedFS) rollout(id, date, filename, content string) {
	f.MapFS[sessions+date+"/"+filename+"-"+id+".jsonl"] = &fstest.MapFile{Data: []byte(content)}
}

func find(t *testing.T, got []session.Session, id string) session.Session {
	t.Helper()
	for _, s := range got {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("missing session %q in %#v", id, got)
	return session.Session{}
}

func has(calls []string, wanted string) bool {
	for _, call := range calls {
		if call == wanted {
			return true
		}
	}
	return false
}

func TestListSignature(t *testing.T) {
	// R-UBNS-BLZW: callers can use the contract's exact function shape.
	assertShape := func(list func(fs.FS, string) ([]session.Session, error)) bool {
		return !reflect.ValueOf(list).IsNil()
	}
	if !assertShape(List) {
		t.Fatal("nil List")
	}
}

func TestMissingAndUnreadableLockDirectory(t *testing.T) {
	// R-YIN9-X2GJ R-YJV6-AU78: the absolute home path maps to root and absence touches nothing else.
	f := fixture()
	got, err := List(f, home)
	if err != nil || len(got) != 0 || !reflect.DeepEqual(f.calls, []string{"dir:" + lockDir}) {
		t.Fatalf("missing directory: got=%#v err=%v calls=%v", got, err, f.calls)
	}

	// R-0AI3-54UD R-H0PS-DNFS R-I2KH-XSXP: a path error reports the absolute path and bare cause.
	f = fixture()
	f.fail["dir:"+lockDir] = &fs.PathError{Op: "readdir", Path: lockDir, Err: fs.ErrPermission}
	got, err = List(f, home)
	var read *session.ReadError
	if got != nil || !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(read) || read.Path != "/"+lockDir || !errors.Is(read.Err, fs.ErrPermission) || reflect.TypeOf(read.Err) != reflect.TypeOf(fs.ErrPermission) {
		t.Fatalf("unreadable directory: got=%#v err=%#v", got, err)
	}
}

func TestLockHoldersFailureAndEmptyDirectory(t *testing.T) {
	// R-0BPZ-IWL2: even an empty listed directory must read /proc/locks, and its error is bare.
	f := fixture()
	f.MapFS[lockDir+"/."] = &fstest.MapFile{Mode: fs.ModeDir}
	f.fail["read:proc/locks"] = &fs.PathError{Op: "read", Path: "proc/locks", Err: fs.ErrPermission}
	got, err := List(f, home)
	var read *session.ReadError
	if got != nil || !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(read) || read.Path != "/proc/locks" || !errors.Is(read.Err, fs.ErrPermission) || reflect.TypeOf(read.Err) != reflect.TypeOf(fs.ErrPermission) || !has(f.calls, "read:proc/locks") {
		t.Fatalf("locks failure: got=%#v err=%#v calls=%v", got, err, f.calls)
	}
	delete(f.fail, "read:proc/locks")
	f.MapFS["proc/locks"] = &fstest.MapFile{}
	got, err = List(f, home)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty directory: got=%#v err=%v", got, err)
	}
	// R-YOQR-TX60: an internal coordination lock never triggers rollout or index lookup.
	f.MapFS[lockDir+"/.coordination.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 17}}
	f.calls = nil
	got, err = List(f, home)
	if err != nil || len(got) != 0 || has(f.calls, "read:"+index) || has(f.calls, "stat:"+lockDir+"/.coordination.lock") {
		t.Fatalf("coordination lock: got=%#v err=%v calls=%v", got, err, f.calls)
	}
}

func TestLocksAndAllowedOperations(t *testing.T) {
	// R-YOQR-TX60 R-YR6K-LGNE R-YSEG-Z8E3: only live, well-named locks yield sessions.
	// R-082A-DLCZ R-Z895-Y914 R-I1CL-K170: lock entries are stat-only; all access stays in the allowed paths.
	f := fixture()
	f.lock("live", 17, 321)
	f.MapFS[lockDir+"/.coordination.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 18}}
	f.MapFS[lockDir+"/dead.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 19}}
	f.MapFS[lockDir+"/not-a-lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 20}}
	f.MapFS[lockDir+"/.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 21}}
	f.MapFS[lockDir+"/unknown.lock"] = &fstest.MapFile{}
	f.fail["stat:"+lockDir+"/broken.lock"] = fs.ErrPermission
	f.MapFS[lockDir+"/broken.lock"] = &fstest.MapFile{Sys: &syscall.Stat_t{Dev: 0x801, Ino: 22}}
	got, err := List(f, home)
	if err != nil || len(got) != 1 || got[0].ID != "live" {
		t.Fatalf("live locks: got=%#v err=%v", got, err)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "open:") || strings.HasPrefix(call, "read:"+lockDir+"/") || strings.HasPrefix(call, "stat:"+lockDir+"/.") || strings.Contains(call, "not-a-lock") {
			t.Fatalf("unexpected access %q in %v", call, f.calls)
		}
		if strings.HasPrefix(call, "stat:"+lockDir+"/") {
			continue
		}
		if call == "dir:"+lockDir || call == "read:proc/locks" || call == "link:proc/321/cwd" || call == "dir:"+strings.TrimSuffix(sessions, "/") || call == "read:"+index {
			continue
		}
		t.Fatalf("outside allowed paths: %q", call)
	}
}

func TestRolloutRecordsAndFields(t *testing.T) {
	// R-04EL-8A4W R-05MH-M1VL R-HE4O-L4LF: only complete JSON-object lines count; a final fragment is ignored.
	// R-YYHY-W33K R-P234-64KW R-Z25O-1EBN R-06UD-ZTMA R-HFCK-YWC4 R-HHSD-QFTI: meta, status, and maximum valid timestamp are independent record facts.
	f := fixture()
	f.lock("first", 17, 321)
	f.rollout("first", "2026/09/23", "rollout-a", "\n[]\nnot json\n{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/chosen\"},\"timestamp\":\"2026-09-23T20:00:00Z\"}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"},\"timestamp\":\"2026-09-23T19:00:00Z\"}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"},\"timestamp\":\"9999-99-99T99:99:99Z\"}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"},\"timestamp\":\"2026-09-23T18:00:00Z\"}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n")
	got, err := List(f, home)
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %#v %v", got, err)
	}
	s := got[0]
	maxTime := time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC)
	if s.Status != session.StatusIdle || s.CWD != "/chosen" || !s.HasLastActive || s.LastActive.Compare(maxTime) != 0 {
		t.Fatalf("parsed rollout: %#v", s)
	}
	f.MapFS[sessions+"2026/09/23/rollout-a-first.jsonl"].Data = append(f.MapFS[sessions+"2026/09/23/rollout-a-first.jsonl"].Data, []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n")...)
	got, err = List(f, home)
	if err != nil || len(got) != 1 || got[0].Status != session.StatusWorking {
		t.Fatalf("later task started: %#v %v", got, err)
	}

	// R-05MH-M1VL R-I50A-PCF3: a fragment-only rollout is unusable and falls back to proc cwd.
	f.MapFS[sessions+"2026/09/23/rollout-a-first.jsonl"] = &fstest.MapFile{Data: []byte("{\"type\":\"session_meta\"}")}
	got, err = List(f, home)
	if err != nil || got[0].Status != session.StatusUnknown || got[0].HasLastActive || got[0].CWD != "/work/first" {
		t.Fatalf("fragment only: %#v %v", got, err)
	}
	f.fail["link:proc/321/cwd"] = fs.ErrPermission
	got, err = List(f, home)
	if err != nil || got[0].CWD != "" || got[0].Status != session.StatusUnknown {
		t.Fatalf("unreadable cwd: %#v %v", got, err)
	}
}

func TestRolloutWithoutValidTimestamp(t *testing.T) {
	// R-06UD-ZTMA R-HFCK-YWC4 R-HHSD-QFTI: a usable rollout with only absent, non-string, or malformed timestamps has no latest timestamp.
	for _, tc := range []struct {
		name   string
		member string
	}{
		{name: "absent"},
		{name: "non-string", member: `,"timestamp":42`},
		{name: "malformed", member: `,"timestamp":"9999-99-99T99:99:99Z"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture()
			f.lock("first", 17, 321)
			f.rollout("first", "2026/09/23", "rollout-a", `{"type":"session_meta","payload":{"cwd":"/chosen"}`+tc.member+"}\n")
			got, err := List(f, home)
			if err != nil || len(got) != 1 || got[0].Status != session.StatusIdle || got[0].CWD != "/chosen" || got[0].HasLastActive {
				t.Fatalf("%s timestamp: %#v %v", tc.name, got, err)
			}
		})
	}
}

func TestSubagentsAndMetaPosition(t *testing.T) {
	// R-I2R2-VX3Y R-YYHY-W33K: either parent field excludes a thread, but later meta is not session meta.
	f := fixture()
	f.lock("direct", 17, 321)
	f.lock("nested", 18, 322)
	f.lock("late", 19, 323)
	f.rollout("direct", "2026/09/23", "rollout-a", "{\"type\":\"session_meta\",\"payload\":{\"parent_thread_id\":\"parent\"}}\n")
	f.rollout("nested", "2026/09/23", "rollout-b", "{\"type\":\"session_meta\",\"payload\":{\"source\":{\"subagent\":{\"thread_spawn\":{\"parent_thread_id\":\"parent\"}}}}}\n")
	f.rollout("late", "2026/09/23", "rollout-c", "{\"type\":\"event_msg\"}\n{\"type\":\"session_meta\",\"payload\":{\"parent_thread_id\":\"parent\",\"cwd\":\"/wrong\"}}\n")
	got, err := List(f, home)
	if err != nil || len(got) != 1 || got[0].ID != "late" || got[0].CWD != "/work/late" || got[0].Status != session.StatusIdle {
		t.Fatalf("subagents: %#v %v", got, err)
	}
}

func TestRolloutSearchAndFallbacks(t *testing.T) {
	// R-P4IW-XO2A: the first bytewise year/month/day/file at exactly this depth wins.
	// R-YNIV-G5FB R-I50A-PCF3: missing or unreadable rollout is a per-thread unknown, never a list error.
	// R-I3SE-BKOE: the two sessions are compared by id, with no presentation-order constraint.
	f := fixture()
	f.lock("a", 17, 321)
	f.lock("b", 18, 322)
	f.rollout("a", "2027/01/01", "rollout-a", "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/late-year\"}}\n")
	f.rollout("a", "2026/11/01", "rollout-a", "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/late-month\"}}\n")
	f.rollout("a", "2026/10/03", "rollout-a", "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/late-day\"}}\n")
	f.rollout("a", "2026/10/02", "rollout-z", "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/late-file\"}}\n")
	f.rollout("a", "2026/10/02", "rollout-a", "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/first\"}}\n")
	f.MapFS[sessions+"rollout-early-a.jsonl"] = &fstest.MapFile{Data: []byte("{}\n")}
	f.MapFS[sessions+"2025/rollout-early-a.jsonl"] = &fstest.MapFile{Data: []byte("{}\n")}
	f.fail["read:"+sessions+"2026/10/02/rollout-a-a.jsonl"] = fs.ErrPermission
	got, err := List(f, home)
	if err != nil || len(got) != 2 || find(t, got, "a").Status != session.StatusUnknown || find(t, got, "a").CWD != "/work/a" || find(t, got, "b").Status != session.StatusUnknown {
		t.Fatalf("fallback: %#v %v", got, err)
	}
	delete(f.fail, "read:"+sessions+"2026/10/02/rollout-a-a.jsonl")
	got, err = List(f, home)
	if err != nil || find(t, got, "a").CWD != "/first" {
		t.Fatalf("first rollout: %#v %v", got, err)
	}
}

func TestIndexIndependentOfRollout(t *testing.T) {
	// R-P3B0-JWBL R-Z719-KHAF: last complete index record wins, including apostrophes; rollout facts do not depend on it.
	f := fixture()
	f.lock("one", 17, 321)
	f.lock("two", 18, 322)
	f.rollout("one", "2026/09/23", "rollout-a", "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/meta\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"turn_aborted\"}}\n")
	f.MapFS[index] = &fstest.MapFile{Data: []byte("{\"id\":\"one\",\"thread_name\":\"old\"}\n{\"id\":\"two\",\"thread_name\":\"O'Reilly\"}\n{\"id\":\"one\",\"thread_name\":\"new\"}\n{\"id\":\"two\",\"thread_name\":null}\n{\"id\":\"one\",\"thread_name\":\"fragment\"}")}
	got, err := List(f, home)
	if err != nil || find(t, got, "one").Title != "new" || find(t, got, "two").Title != "" || find(t, got, "one").Status != session.StatusIdle || find(t, got, "one").CWD != "/meta" {
		t.Fatalf("index: %#v %v", got, err)
	}
	f.MapFS[index] = &fstest.MapFile{Data: []byte("{\"id\":\"one\",\"thread_name\":\"new\"}\n{\"id\":\"two\",\"thread_name\":\"O'Reilly\"}\n")}
	got, err = List(f, home)
	if err != nil || find(t, got, "two").Title != "O'Reilly" || find(t, got, "two").Status != session.StatusUnknown {
		t.Fatalf("title despite absent rollout: %#v %v", got, err)
	}
	f.fail["read:"+index] = fs.ErrPermission
	got, err = List(f, home)
	if err != nil || find(t, got, "one").Title != "" || find(t, got, "one").Status != session.StatusIdle || find(t, got, "one").CWD != "/meta" {
		t.Fatalf("unreadable index: %#v %v", got, err)
	}
	delete(f.fail, "read:"+index)
	f.fail["read:"+sessions+"2026/09/23/rollout-a-one.jsonl"] = fs.ErrPermission
	got, err = List(f, home)
	if err != nil || find(t, got, "one").Title != "new" || find(t, got, "one").Status != session.StatusUnknown {
		t.Fatalf("unreadable rollout: %#v %v", got, err)
	}
}
