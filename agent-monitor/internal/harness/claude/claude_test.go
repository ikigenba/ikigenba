package claude_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

const registry = "home/dev/.claude/sessions/"
const projects = "home/dev/.claude/projects/"

// R-U803-6ART: the exported entry point has the declared exact signature.
var _ func(fs.FS, string) ([]session.Session, error) = claude.List

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"proc/stat":         &fstest.MapFile{Data: []byte("btime 1000\n")},
		"proc/42/stat":      &fstest.MapFile{Data: []byte("42 (a) " + strings.Repeat("0 ", 19) + "150")},
		"proc/42/cwd":       &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("/fallback")},
		registry + "a.json": &fstest.MapFile{Data: []byte(`{"sessionId":"alpha","pid":42}`)},
	}
}

func put(m fstest.MapFS, name, content string) {
	m[name] = &fstest.MapFile{Data: []byte(content)}
}

func only(t *testing.T, m fs.FS) session.Session {
	t.Helper()
	got, err := claude.List(m, "/home/dev")
	if err != nil || len(got) != 1 {
		t.Fatalf("List: sessions=%+v error=%v", got, err)
	}
	return got[0]
}

// R-9RAX-ZEP2 R-9SIU-D6FR R-32L6-1AKK R-3B4G-PORF R-H0PS-DNFS
func TestRegistryPathAndErrors(t *testing.T) {
	missing, err := claude.List(fstest.MapFS{}, "/home/dev")
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing registry: %v %v", missing, err)
	}
	root := &observedFS{MapFS: fixture(), failDir: "home/dev/.claude/sessions", failErr: &fs.PathError{Op: "readdir", Path: "other", Err: fs.ErrPermission}}
	got, err := claude.List(root, "/home/dev")
	if got != nil {
		t.Fatalf("error returned sessions: %+v", got)
	}
	var read *session.ReadError
	if !errors.As(err, &read) || reflect.TypeOf(err) != reflect.TypeOf(read) || read.Path != "/home/dev/.claude/sessions" || !errors.Is(read.Err, fs.ErrPermission) || reflect.TypeOf(read.Err) != reflect.TypeOf(fs.ErrPermission) {
		t.Fatalf("read error: %#v", err)
	}
	if !reflect.DeepEqual(root.dirs, []string{"home/dev/.claude/sessions"}) {
		t.Fatalf("registry names: %v", root.dirs)
	}
}

// R-33T2-F2B9 R-9UYN-4PX5 R-A125-1KMM R-AGWU-0L9N
func TestRegistrationsAndSilentFailures(t *testing.T) {
	m := fixture()
	put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,"startedAt":1001500}`)
	put(m, registry+"b.json", `null`)
	put(m, registry+"c.json", `{"sessionId":"","pid":42}`)
	put(m, registry+"d.json", `{"sessionId":"bad","pid":0}`)
	put(m, registry+"e.json", `{"sessionId":"bad","pid":9223372036854775808}`)
	put(m, registry+"f.json", `{"sessionId":"bad","pid":"42"}`)
	put(m, registry+"g.json", `{`)
	put(m, registry+"h.json", `{"sessionId":"beta","pid":42}`)
	put(m, registry+"i.json", `{"sessionId":"lost","pid":99}`)
	put(m, registry+"j.json", `{"sessionId":"fractional","pid":42.1}`)
	put(m, registry+"k.json", `{"sessionId":true,"pid":42}`)
	put(m, registry+"l.json", `["alpha",42]`)
	put(m, registry+"m.json", `{"sessionId":"overflow","pid":1e9999999999999999999}`)
	got, err := claude.List(m, "/home/dev")
	if err != nil || len(got) != 2 {
		t.Fatalf("sessions: %+v, %v", got, err)
	}
	ids := map[string]bool{}
	for _, s := range got {
		ids[s.ID] = true
	}
	if !ids["alpha"] || !ids["beta"] || len(ids) != 2 {
		t.Fatalf("ids: %v", ids)
	}
	// A readable registry still succeeds if both optional locations fail.
	root := &observedFS{MapFS: m, failDir: "home/dev/.claude/projects", failErr: fs.ErrPermission}
	if got, err = claude.List(root, "/home/dev"); err != nil || len(got) != 2 {
		t.Fatalf("optional projects: %+v %v", got, err)
	}
	root.failFile = registry + "h.json"
	if got, err = claude.List(root, "/home/dev"); err != nil || len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("unreadable registration: %+v %v", got, err)
	}
}

// R-UU1R-EQCU R-350Y-SU1Y R-368V-6LSN
func TestProcessLivenessPriorityAndFallback(t *testing.T) {
	cases := []struct {
		name, fields string
		live         bool
	}{
		{"equal ticks", `"procStart":"150","startedAt":0`, true},
		{"earlier ticks", `"procStart":"149","startedAt":9999999`, false},
		{"later ticks", `"procStart":"151","startedAt":0`, true},
		{"start millis equal", `"startedAt":1001500`, true},
		{"start millis earlier", `"startedAt":1001499`, false},
		{"start millis later", `"startedAt":1001501`, true},
		{"decimal integer", `"startedAt":1001500.0`, true},
		{"exponent integer", `"startedAt":1.0015e6`, true},
		{"fractional fallback", `"startedAt":1001499.1`, true},
		{"bad ticks fallback", `"procStart":"-150","startedAt":1001500`, true},
		{"no starts", `"procStart":null`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fixture()
			put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,`+tc.fields+`}`)
			got, err := claude.List(m, "/home/dev")
			if err != nil || (len(got) == 1) != tc.live {
				t.Fatalf("sessions=%+v error=%v; want live=%v", got, err, tc.live)
			}
		})
	}
}

// R-A3HX-T440 R-37GR-KDJC R-A5XQ-KNLE R-AEH1-91S9
func TestIndependentRegistrationFields(t *testing.T) {
	cases := []struct {
		fields     string
		status     session.Status
		cwd, title string
	}{
		{`"status":"busy","cwd":"/chosen","name":"O'Reilly"`, session.StatusWorking, "/chosen", "O'Reilly"},
		{`"status":"idle","cwd":"","name":4`, session.StatusIdle, "/fallback", ""},
		{`"status":"other","cwd":4,"name":null`, session.StatusUnknown, "/fallback", ""},
	}
	for _, tc := range cases {
		m := fixture()
		put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,`+tc.fields+`}`)
		got := only(t, m)
		if got.Status != tc.status || got.CWD != tc.cwd || got.Title != tc.title || got.HasLastActive {
			t.Fatalf("fields: %+v", got)
		}
	}
	// A transcript changes only the last-active fields.
	m := fixture()
	put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,"status":"busy","cwd":"/chosen","name":"O'Reilly"}`)
	put(m, projects+"arbitrary/alpha.jsonl", "{\"timestamp\":\"2026-01-02T03:04:05Z\"}\n")
	got := only(t, m)
	if got.Status != session.StatusWorking || got.CWD != "/chosen" || got.Title != "O'Reilly" || !got.HasLastActive {
		t.Fatalf("transcript independence: %+v", got)
	}
	wantTime := got.LastActive
	put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,"status":"idle","cwd":"/changed","name":"Changed"}`)
	changed := only(t, m)
	if changed.Status != session.StatusIdle || changed.CWD != "/changed" || changed.Title != "Changed" || !changed.HasLastActive || changed.LastActive.Compare(wantTime) != 0 {
		t.Fatalf("registration fields changed transcript time: %+v", changed)
	}
}

// R-38ON-Y5A1 R-TVR7-PUBW R-A9LF-PYTH R-9W6J-IHNU R-AFOX-MTIY
func TestTranscriptSearchAndAccessBoundary(t *testing.T) {
	m := fixture()
	put(m, registry+"a.json", `{"sessionId":"alpha","pid":42,"cwd":"/unrelated"}`)
	put(m, registry+"secret.key", "secret")
	put(m, projects+"z/alpha.jsonl", "{\"timestamp\":\"2030-01-01T00:00:00Z\"}\n")
	put(m, projects+"a/alpha.jsonl", "{\"timestamp\":\"2020-01-01T00:00:00Z\"}\n")
	put(m, projects+"a/other.jsonl", "secret")
	put(m, projects+"a/deep/alpha.jsonl", "secret")
	put(m, projects+"file", "secret")
	m[projects+"link"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("a")}
	root := &observedFS{MapFS: m}
	s := only(t, root)
	if !s.HasLastActive || !s.LastActive.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first project transcript: %+v", s)
	}
	for _, name := range root.accesses {
		if strings.HasSuffix(name, ".key") || strings.Contains(name, "other.jsonl") || strings.Contains(name, "deep") || name == projects+"file" || strings.HasPrefix(name, projects+"z/") {
			t.Fatalf("unexpected access: %s; accesses=%v", name, root.accesses)
		}
	}
	for _, id := range []string{"../secret", ".", "..", "a/b"} {
		m := fixture()
		put(m, registry+"a.json", `{"sessionId":"`+id+`","pid":42}`)
		root := &observedFS{MapFS: m}
		got := only(t, root)
		if got.HasLastActive || len(root.dirs) != 1 {
			t.Fatalf("invalid id %q searched transcript: %+v %v", id, got, root.dirs)
		}
	}
}

// R-38ON-Y5A1 R-UXPG-K1KX: an unreadable matching file does not occupy its directory.
func TestUnreadableFirstMatchingTranscript(t *testing.T) {
	m := fixture()
	put(m, projects+"a/alpha.jsonl", "unreadable")
	put(m, projects+"z/alpha.jsonl", "{\"timestamp\":\"2027-02-03T04:05:06Z\"}\n")
	root := &observedFS{MapFS: m, failFile: projects + "a/alpha.jsonl"}
	got := only(t, root)
	want := time.Date(2027, 2, 3, 4, 5, 6, 0, time.UTC)
	if !got.HasLastActive || got.LastActive.Compare(want) != 0 {
		t.Fatalf("later readable transcript: %+v; reads=%v", got, root.files)
	}
	if !reflect.DeepEqual(root.files[len(root.files)-2:], []string{projects + "a/alpha.jsonl", projects + "z/alpha.jsonl"}) {
		t.Fatalf("transcript search order: %v", root.files)
	}
}

// R-UXPG-K1KX R-39WK-BX0Q R-HE4O-L4LF R-HFCK-YWC4 R-HHSD-QFTI
func TestLatestCompleteRecord(t *testing.T) {
	m := fixture()
	put(m, projects+"p/alpha.jsonl", strings.Join([]string{
		`{"timestamp":"2025-01-01T00:00:00Z"}`,
		`["2029-01-01T00:00:00Z"]`,
		`{"timestamp":9}`,
		`{"timestamp":"invalid"}`,
		"\v" + `{"timestamp":"2031-01-01T00:00:00Z"}`,
		` {"timestamp":"2026-01-01T01:01:01.456+01:00"} `,
		`{"timestamp":"2024-01-01T00:00:00Z"}`,
		`{"timestamp":"2030-01-01T00:00:00Z"`,
	}, "\n"))
	got := only(t, m)
	want := time.Date(2026, 1, 1, 0, 1, 1, 456000000, time.UTC)
	if !got.HasLastActive || got.LastActive.Compare(want) != 0 {
		t.Fatalf("latest: %+v want %v", got, want)
	}
	put(m, projects+"p/alpha.jsonl", `{"timestamp":"2030-01-01T00:00:00Z"}`)
	if got := only(t, m); got.HasLastActive {
		t.Fatalf("fragment counted: %+v", got)
	}
}

type observedFS struct {
	fstest.MapFS
	failDir     string
	failFile    string
	failErr     error
	dirs, files []string
	accesses    []string
}

func (f *observedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	f.dirs = append(f.dirs, name)
	f.accesses = append(f.accesses, name)
	if name == f.failDir {
		return nil, f.failErr
	}
	return f.MapFS.ReadDir(name)
}

func (f *observedFS) ReadFile(name string) ([]byte, error) {
	f.files = append(f.files, name)
	f.accesses = append(f.accesses, name)
	if name == f.failFile {
		return nil, fs.ErrPermission
	}
	return f.MapFS.ReadFile(name)
}

func (f *observedFS) Open(name string) (fs.File, error) {
	f.accesses = append(f.accesses, name)
	return f.MapFS.Open(name)
}

func (f *observedFS) Stat(name string) (fs.FileInfo, error) {
	f.accesses = append(f.accesses, name)
	return fs.Stat(f.MapFS, name)
}

func (f *observedFS) ReadLink(name string) (string, error) {
	f.accesses = append(f.accesses, name)
	return fs.ReadLink(f.MapFS, name)
}
