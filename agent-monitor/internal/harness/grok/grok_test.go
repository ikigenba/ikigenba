package grok_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

// R-UGJD-UOYO: the exported entry point has the declared exact signature.
var _ func(fs.FS, string) ([]session.Session, error) = grok.List

const base = "home/dev/.grok/"
const stat = "42 (grok) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 150 0 0"

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
func root(index string) fstest.MapFS {
	return fstest.MapFS{
		base + "active_sessions.json": file(index),
		"proc/stat":                   file("btime 1000\n"),
		"proc/42/stat":                file(stat),
		"proc/43/stat":                file(stat),
	}
}

const entry = `{"session_id":"a","pid":42,"cwd":"/work","opened_at":"1970-01-01T00:16:42Z"}`

func mustList(t *testing.T, root fs.FS) []session.Session {
	t.Helper()
	got, err := grok.List(root, "/home/dev")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

type observed struct {
	fstest.MapFS
	opened []string
	listed []string
	writes []string
	fail   map[string]error
}

func (o *observed) forbidden(name string) error {
	o.writes = append(o.writes, name)
	return fs.ErrPermission
}

func (o *observed) Write([]byte) (int, error) { return 0, o.forbidden("Write") }
func (o *observed) WriteFile(string, []byte, fs.FileMode) error {
	return o.forbidden("WriteFile")
}
func (o *observed) Create(string) (fs.File, error) { return nil, o.forbidden("Create") }
func (o *observed) OpenFile(string, int, fs.FileMode) (fs.File, error) {
	return nil, o.forbidden("OpenFile")
}
func (o *observed) Mkdir(string, fs.FileMode) error    { return o.forbidden("Mkdir") }
func (o *observed) MkdirAll(string, fs.FileMode) error { return o.forbidden("MkdirAll") }
func (o *observed) Remove(string) error                { return o.forbidden("Remove") }
func (o *observed) RemoveAll(string) error             { return o.forbidden("RemoveAll") }
func (o *observed) Rename(string, string) error        { return o.forbidden("Rename") }
func (o *observed) Chmod(string, fs.FileMode) error    { return o.forbidden("Chmod") }
func (o *observed) Chtimes(string, time.Time, time.Time) error {
	return o.forbidden("Chtimes")
}
func (o *observed) Symlink(string, string) error { return o.forbidden("Symlink") }
func (o *observed) Lock() error                  { return o.forbidden("Lock") }
func (o *observed) Unlock() error                { return o.forbidden("Unlock") }

func (o *observed) Open(name string) (fs.File, error) {
	o.opened = append(o.opened, name)
	if err := o.fail[name]; err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return o.MapFS.Open(name)
}
func (o *observed) ReadFile(name string) ([]byte, error) {
	o.opened = append(o.opened, name)
	if err := o.fail[name]; err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return fs.ReadFile(o.MapFS, name)
}
func (o *observed) ReadDir(name string) ([]fs.DirEntry, error) {
	o.listed = append(o.listed, name)
	if err := o.fail[name]; err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	return fs.ReadDir(o.MapFS, name)
}

// R-AN0B-XFZ4 R-AO88-B7PT R-3CCD-3GI4 R-H0PS-DNFS R-3KVN-RUOZ
func TestIndexPathsAndErrors(t *testing.T) {
	if got := mustList(t, fstest.MapFS{}); len(got) != 0 {
		t.Fatalf("missing index = %+v", got)
	}
	o := &observed{MapFS: root("[]"), fail: map[string]error{base + "active_sessions.json": fs.ErrPermission}}
	got, err := grok.List(o, "/home/dev")
	if got != nil {
		t.Fatalf("error returned sessions: %+v", got)
	}
	var re *session.ReadError
	if !errors.As(err, &re) || reflect.TypeOf(err) != reflect.TypeOf(&session.ReadError{}) || re.Path != "/home/dev/.grok/active_sessions.json" || reflect.ValueOf(re.Err).Pointer() != reflect.ValueOf(fs.ErrPermission).Pointer() {
		t.Fatalf("read error = %#v", err)
	}
	if !reflect.DeepEqual(o.opened, []string{base + "active_sessions.json"}) {
		t.Fatalf("opened %v", o.opened)
	}
}

// R-AQO1-2R77 R-3KVN-RUOZ
func TestInvalidIndex(t *testing.T) {
	for _, value := range []string{"", "[", "null", "{}", `[{} , 5]`, `[null]`, `"x"`} {
		got, err := grok.List(root(value), "/home/dev")
		var re *session.ReadError
		if got != nil || !errors.As(err, &re) || re.Path != "/home/dev/.grok/active_sessions.json" || reflect.ValueOf(re.Err).Pointer() != reflect.ValueOf(session.ErrNotJSON).Pointer() {
			t.Errorf("index %q: got %+v, %v", value, got, err)
		}
	}
}

// R-3DK9-H88T R-3ES5-UZZI R-AVJM-LU5Z R-AWRI-ZLWO R-BDU4-CEAE
func TestLiveEntries(t *testing.T) {
	entries := []string{
		`{}`, `{"session_id":"","pid":42}`, `{"session_id":5,"pid":42}`,
		`{"session_id":"str","pid":"42"}`, `{"session_id":"zero","pid":0}`,
		`{"session_id":"negative","pid":-1}`, `{"session_id":"fraction","pid":42.5}`,
		`{"session_id":"overflow","pid":9223372036854775808}`,
		`{"session_id":"dead","pid":99}`, `{"session_id":"reused","pid":42,"opened_at":"1970-01-01T00:16:41Z"}`,
		`{"session_id":"equal","pid":42,"opened_at":"1970-01-01T00:16:41.5Z"}`,
		`{"session_id":"later","pid":42,"opened_at":"1970-01-01T00:16:42Z"}`,
		`{"session_id":"absent","pid":42}`, `{"session_id":"wrongtype","pid":42,"opened_at":5}`,
		`{"session_id":"badtime","pid":42,"opened_at":"no"}`,
		`{"session_id":"decimal","pid":42.0}`, `{"session_id":"exponent","pid":4.2e1}`, `{"session_id":"trailing-zero","pid":4200e-2}`, `{"session_id":"leading-fraction","pid":0.00042e5}`,
		`{"session_id":"long-zero-exponent","pid":42e0000000000000000000000000000000000000}`,
	}
	got := mustList(t, root("["+strings.Join(entries, ",")+"]"))
	ids := make(map[string]int)
	for _, s := range got {
		ids[s.ID]++
	}
	want := map[string]int{"equal": 1, "later": 1, "absent": 1, "wrongtype": 1, "badtime": 1, "decimal": 1, "exponent": 1, "trailing-zero": 1, "leading-fraction": 1, "long-zero-exponent": 1}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

// R-AXZF-DDND R-AWRI-ZLWO
func TestCWDAndDuplicateEntries(t *testing.T) {
	index := `[{"session_id":"a","pid":42,"cwd":"/from-index"},{"session_id":"a","pid":42,"cwd":""},{"session_id":"b","pid":43,"cwd":5}]`
	r := root(index)
	r["proc/42/cwd"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("/from-proc")}
	got := mustList(t, r)
	counts := make(map[string]int)
	for _, s := range got {
		counts[s.ID+"|"+s.CWD]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"a|/from-index": 1, "a|/from-proc": 1, "b|": 1}) {
		t.Fatalf("sessions = %+v", got)
	}
}

// R-3H7Y-MJGW R-B0F8-4X4R R-B1N4-IOVG R-AN0B-XFZ4
func TestDirectorySearchAndForbiddenIDs(t *testing.T) {
	index := `[{"session_id":"a","pid":42},{"session_id":"b/c","pid":42},{"session_id":"..","pid":42},{"session_id":".","pid":42}]`
	r := root(index)
	r[base+"sessions/z/a/summary.json"] = file(`{"generated_title":"later"}`)
	r[base+"sessions/a/a/summary.json"] = file(`{"generated_title":"first"}`)
	r[base+"sessions/a/a/events.jsonl"] = file("{\"type\":\"turn_started\"}\n")
	r[base+"sessions/link"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("a")}
	r[base+"sessions/a/a/secret.lock"] = file("do not read")
	o := &observed{MapFS: r, fail: map[string]error{base + "sessions/blocked": fs.ErrPermission}}
	got := mustList(t, o)
	byID := make(map[string]session.Session)
	for _, s := range got {
		byID[s.ID] = s
	}
	if len(got) != 4 || len(byID) != 4 || byID["a"].Title != "first" || byID["a"].Status != session.StatusWorking || byID["b/c"].Title != "" || byID[".."].Title != "" || byID["."].Title != "" {
		t.Fatalf("sessions = %+v", got)
	}
	for _, name := range append(o.opened, o.listed...) {
		if strings.HasSuffix(name, ".lock") || strings.Contains(name, "b/c") || strings.Contains(name, "sessions/..") || strings.Contains(name, "sessions/./") || strings.Contains(name, "sessions/link/") {
			t.Errorf("forbidden access %q", name)
		}
	}
	if !contains(o.listed, base+"sessions") || !contains(o.listed, base+"sessions/a") {
		t.Fatalf("listed = %v", o.listed)
	}
}
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// R-3H7Y-MJGW R-ARVX-GIXW
func TestUnreadableEncodedDirectory(t *testing.T) {
	r := root("[" + entry + "]")
	r[base+"sessions/a/a/summary.json"] = file(`{"generated_title":"blocked"}`)
	r[base+"sessions/b/a/summary.json"] = file(`{"generated_title":"found"}`)
	o := &observed{MapFS: r, fail: map[string]error{base + "sessions/a": fs.ErrPermission}}
	got := mustList(t, o)
	if len(got) != 1 || got[0].Title != "found" {
		t.Fatalf("sessions = %+v", got)
	}
	if !contains(o.listed, base+"sessions/a") || !contains(o.listed, base+"sessions/b") {
		t.Fatalf("listed = %v", o.listed)
	}
}

// R-3IFV-0B7L R-B42X-A8CU R-BBEB-KUT0 R-ARVX-GIXW
func TestIndependentFallbacks(t *testing.T) {
	for _, tc := range []struct {
		summary, events       string
		summaryErr, eventsErr error
		title                 string
		status                session.Status
		when                  string
	}{
		{`{"generated_title":"Hello \u263a"}`, "{\"type\":\"turn_started\",\"ts\":\"2026-01-01T00:00:00Z\"}\n", nil, nil, "Hello ☺", session.StatusWorking, "2026-01-01T00:00:00Z"},
		{`{"generated_title":7}`, "{\"type\":\"turn_ended\"}\n", nil, nil, "", session.StatusIdle, ""},
		{`[]`, "{\"type\":\"turn_started\"}\n", nil, nil, "", session.StatusWorking, ""},
		{`{"generated_title":"still here"}`, "{\"type\":\"turn_started\",\"ts\":\"2026-01-01T00:00:00Z\"}\n", nil, fs.ErrPermission, "still here", session.StatusUnknown, ""},
		{`{"generated_title":"lost"}`, "{\"type\":\"turn_started\",\"ts\":\"2026-01-01T00:00:00Z\"}\n", fs.ErrPermission, nil, "", session.StatusWorking, "2026-01-01T00:00:00Z"},
	} {
		r := root("[" + entry + "]")
		r[base+"sessions/x/a/summary.json"] = file(tc.summary)
		r[base+"sessions/x/a/events.jsonl"] = file(tc.events)
		o := &observed{MapFS: r, fail: map[string]error{base + "sessions/x/a/summary.json": tc.summaryErr, base + "sessions/x/a/events.jsonl": tc.eventsErr}}
		got := mustList(t, o)
		if len(got) != 1 || got[0].Title != tc.title || got[0].Status != tc.status || got[0].CWD != "/work" || got[0].HasLastActive != (tc.when != "") {
			t.Errorf("got %+v, want title %q status %s", got, tc.title, tc.status)
		}
		if tc.when != "" {
			want, err := time.Parse(time.RFC3339Nano, tc.when)
			if err != nil || got[0].LastActive.Compare(want) != 0 {
				t.Errorf("got LastActive %v, want %v", got[0].LastActive, want)
			}
		}
	}
}

// R-UYXC-XTBM R-3JNR-E2YA R-HE4O-L4LF R-V059-BL2B R-HFCK-YWC4 R-HHSD-QFTI
func TestEventsRecordsAndTimes(t *testing.T) {
	cases := []struct {
		events string
		status session.Status
		when   string
		has    bool
	}{
		{"", session.StatusIdle, "", false},
		{"{\"type\":\"turn_started\",\"ts\":\"2026-01-02T03:04:05.1Z\"}\n{\"type\":\"turn_ended\",\"ts\":\"2026-01-01T03:04:05Z\"}\n", session.StatusIdle, "2026-01-02T03:04:05.1Z", true},
		{"{\"type\":\"turn_started\"}\n{\"type\":\"waiting\",\"ts\":\"2026-01-01T00:00:00Z\"}\n", session.StatusWorking, "2026-01-01T00:00:00Z", true},
		{"[1]\nnull\n{\"type\":\"turn_started\"", session.StatusIdle, "", false},
		{"{\"type\":\"turn_started\",\"ts\":5}\n{\"type\":\"other\",\"ts\":\"bad\"}\n", session.StatusWorking, "", false},
		{"{\"type\":\"turn_started\",\"ts\":\"2026-01-01T00:00:00+01:00\"}\n{\"type\":\"turn_ended\"", session.StatusWorking, "2026-01-01T00:00:00+01:00", true},
	}
	for _, tc := range cases {
		r := root("[" + entry + "]")
		r[base+"sessions/x/a/events.jsonl"] = file(tc.events)
		got := mustList(t, r)
		if len(got) != 1 {
			t.Fatalf("got %+v", got)
		}
		s := got[0]
		if s.Status != tc.status || s.HasLastActive != tc.has {
			t.Errorf("events %q: got %+v", tc.events, s)
		}
		if tc.has {
			want, _ := time.Parse(time.RFC3339Nano, tc.when)
			if s.LastActive.Compare(want) != 0 {
				t.Errorf("time = %v, want %v", s.LastActive, want)
			}
		}
	}
}

// R-BCM7-YMJP R-B1N4-IOVG R-BDU4-CEAE
func TestOnlyReadsAllowedFiles(t *testing.T) {
	r := root("[" + entry + "]")
	r[base+"sessions/x/a/summary.json"] = file(`{"generated_title":"title"}`)
	r[base+"sessions/x/a/events.jsonl"] = file("{\"type\":\"turn_ended\"}\n")
	r[base+"sessions/x/a/state.lock"] = file("secret")
	o := &observed{MapFS: r, fail: map[string]error{}}
	got := mustList(t, o)
	if len(got) != 1 || got[0].Title != "title" || got[0].Status != session.StatusIdle {
		t.Fatalf("got %+v", got)
	}
	for _, name := range o.opened {
		if strings.HasPrefix(name, base+"sessions/") && !strings.HasSuffix(name, "summary.json") && !strings.HasSuffix(name, "events.jsonl") {
			t.Errorf("opened unexpected file %q", name)
		}
	}
	if len(o.writes) != 0 {
		t.Fatalf("forbidden write or lock methods called: %v", o.writes)
	}
}
