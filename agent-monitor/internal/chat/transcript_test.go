package chat

import (
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
)

type transcriptFS struct {
	files   fstest.MapFS
	openErr error
	opens   []string
	reads   [][2]int64
}

func (f *transcriptFS) Open(name string) (fs.File, error) {
	f.opens = append(f.opens, name)
	if f.openErr != nil {
		return nil, f.openErr
	}
	file, err := f.files.Open(name)
	if err != nil {
		return nil, err
	}
	return &transcriptFile{File: file, owner: f}, nil
}

type transcriptFile struct {
	fs.File
	owner *transcriptFS
}

func (f *transcriptFile) ReadAt(p []byte, off int64) (int, error) {
	f.owner.reads = append(f.owner.reads, [2]int64{off, off + int64(len(p))})
	return f.File.(io.ReaderAt).ReadAt(p, off)
}

func (f *transcriptFile) Read([]byte) (int, error) {
	panic("transcript log called Read")
}

type transcriptDecoder struct {
	fsys    []fs.FS
	records [][]byte
	entries [][]Entry
	usage   []Usage
}

func (d *transcriptDecoder) Decode(fsys fs.FS, record []byte) ([]Entry, Usage) {
	d.fsys = append(d.fsys, fsys)
	d.records = append(d.records, append([]byte(nil), record...))
	i := len(d.records) - 1
	if i >= len(d.entries) {
		return nil, Usage{}
	}
	return d.entries[i], d.usage[i]
}

func transcriptFixture(content string) *transcriptFS {
	return &transcriptFS{files: fstest.MapFS{"log": {Data: []byte(content)}}}
}

func checkReadError(t *testing.T, err error, path string, cause error) {
	t.Helper()
	var readErr *session.ReadError
	if reflect.TypeOf(err) != reflect.TypeOf(readErr) || !errors.As(err, &readErr) || readErr.Path != path || reflect.ValueOf(readErr.Err) != reflect.ValueOf(cause) {
		t.Fatalf("read error=%#v, want path %q and cause %v", err, path, cause)
	}
}

func TestTranscriptAPIAndMetadata(t *testing.T) {
	// R-KR3I-P55Z R-KSBF-2WWO R-KTJB-GOND R-KUR7-UGE2 R-KVZ4-884R R-LD1P-L0IH
	constructor := NewTranscript
	read := (*Transcript).Read
	getUsage := (*Transcript).Usage
	getRecorded := (*Transcript).Recorded
	getPath := (*Transcript).Path
	if reflect.TypeOf(constructor) != reflect.TypeOf((func(string, Recorded, func() Decoder) *Transcript)(nil)) ||
		reflect.TypeOf(read) != reflect.TypeOf((func(*Transcript, fs.FS) ([]Entry, bool, error))(nil)) ||
		reflect.TypeOf(getUsage) != reflect.TypeOf((func(*Transcript) Usage)(nil)) ||
		reflect.TypeOf(getRecorded) != reflect.TypeOf((func(*Transcript) Recorded)(nil)) ||
		reflect.TypeOf(getPath) != reflect.TypeOf((func(*Transcript) string)(nil)) {
		t.Fatal("transcript API signature mismatch")
	}
	recorded := Recorded{In: true, CacheRead: true, Calls: true}
	tr := constructor("/log", recorded, func() Decoder { return &transcriptDecoder{} })
	if tr.Path() != "/log" || tr.Recorded() != recorded || tr.Usage() != (Usage{}) {
		t.Fatalf("initial state: path=%q recorded=%+v usage=%+v", tr.Path(), tr.Recorded(), tr.Usage())
	}
	if _, _, err := tr.Read(transcriptFixture("{}\n")); err != nil {
		t.Fatal(err)
	}
	if tr.Path() != "/log" || tr.Recorded() != recorded {
		t.Fatalf("metadata changed after pass: %q %+v", tr.Path(), tr.Recorded())
	}
}

func TestTranscriptEmptyPath(t *testing.T) {
	// R-LE9L-YS96
	created := 0
	tr := NewTranscript("", Recorded{}, func() Decoder { created++; return &transcriptDecoder{} })
	f := &transcriptFS{}
	for range 2 {
		entries, reset, err := tr.Read(f)
		if entries != nil || reset || err != nil {
			t.Fatalf("empty path: %v %v %v", entries, reset, err)
		}
	}
	if created != 0 || len(f.opens) != 0 {
		t.Fatalf("empty path touched decoder or filesystem: %d %v", created, f.opens)
	}
}

func TestTranscriptRecordSelectionAndLogPasses(t *testing.T) {
	// R-GUKJ-NF66 R-6TBZ-GZ3V
	d := &transcriptDecoder{}
	tr := NewTranscript("/log", Recorded{}, func() Decoder { return d })
	f := transcriptFixture("not json\n[]\n  {\"a\":1}  \ntrue\n{bad}\n{\"b\":2}\n{\"partial\":")
	if _, _, err := tr.Read(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.opens, []string{"log"}) || len(f.reads) != 1 || f.reads[0] != ([2]int64{0, int64(len(f.files["log"].Data))}) {
		t.Fatalf("first log pass: opens=%v reads=%v", f.opens, f.reads)
	}
	if got := strings.Join([]string{string(d.records[0]), string(d.records[1])}, "|"); got != "  {\"a\":1}  |{\"b\":2}" || len(d.records) != 2 {
		t.Fatalf("records: %q", d.records)
	}
	for _, got := range d.fsys {
		if got != f {
			t.Fatal("decoder received another filesystem")
		}
	}
	f.opens, f.reads = nil, nil
	f.files["log"].Data = []byte("not json\n[]\n  {\"a\":1}  \ntrue\n{bad}\n{\"b\":2}\n{\"partial\":3}\n")
	if _, _, err := tr.Read(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.opens, []string{"log"}) || len(f.reads) != 1 || f.reads[0][0] != int64(len("not json\n[]\n  {\"a\":1}  \ntrue\n{bad}\n{\"b\":2}\n{\"partial\":")) {
		t.Fatalf("append pass: opens=%v reads=%v", f.opens, f.reads)
	}
	if len(d.records) != 3 || string(d.records[2]) != "{\"partial\":3}" {
		t.Fatalf("appended record: %q", d.records)
	}
}

func TestTranscriptDecoderLifetimeAndReset(t *testing.T) {
	// R-LHXB-43H9 R-LMSW-N6G1
	var decoders []*transcriptDecoder
	tr := NewTranscript("/log", Recorded{}, func() Decoder {
		d := &transcriptDecoder{}
		decoders = append(decoders, d)
		return d
	})
	if len(decoders) != 1 {
		t.Fatalf("constructor calls: %d", len(decoders))
	}
	f := transcriptFixture("{\"first\":1}\n{\"second\":2}\n")
	if _, reset, err := tr.Read(f); err != nil || reset || len(decoders[0].records) != 2 || len(decoders) != 1 {
		t.Fatalf("first pass: reset=%v err=%v decoders=%d records=%d", reset, err, len(decoders), len(decoders[0].records))
	}
	f.files["log"].Data = []byte("{\"first\":1}\n{\"second\":2}\n{\"third\":3}\n")
	if _, reset, err := tr.Read(f); err != nil || reset || len(decoders[0].records) != 3 || len(decoders) != 1 {
		t.Fatalf("append pass: reset=%v err=%v decoders=%d records=%d", reset, err, len(decoders), len(decoders[0].records))
	}
	f.files["log"].Data = []byte("{}\n")
	if _, reset, err := tr.Read(f); err != nil || !reset || len(decoders) != 2 || len(decoders[1].records) != 1 {
		t.Fatalf("reset pass: reset=%v err=%v decoders=%d", reset, err, len(decoders))
	}
	f.files["log"].Data = []byte("{}\n{\"later\":true}\n")
	if _, reset, err := tr.Read(f); err != nil || reset || len(decoders) != 2 || len(decoders[1].records) != 2 {
		t.Fatalf("after reset: reset=%v err=%v decoders=%d", reset, err, len(decoders))
	}
}

func TestTranscriptEntryFilterAndUsage(t *testing.T) {
	// R-LJ57-HV7Y R-76XN-M9BN
	all := []Entry{
		{Kind: KindUser, Text: ""}, {Kind: KindAssistant, Text: ""},
		{Kind: KindReasoning, Text: ""}, {Kind: KindAgent, Text: ""},
		{Kind: Kind("alien"), Text: "present"},
		{Kind: KindUser, Text: "user"}, {Kind: KindAssistant, Text: "assistant"},
		{Kind: KindReasoning, Text: "thinking"}, {Kind: KindAgent, Text: "agent"},
		{Kind: KindTool, Text: ""}, {Kind: KindResultOK, Text: ""}, {Kind: KindResultError, Text: ""},
	}
	d := &transcriptDecoder{entries: [][]Entry{all, {{Kind: KindTool, Tool: "Bash"}}}, usage: []Usage{
		{In: 5, CacheWrite: -3, CacheRead: 7, Out: 11, Reasoning: -2, Calls: 1},
		{In: -3, CacheWrite: 4, CacheRead: -9, Out: -8, Reasoning: 2, Calls: -1},
	}}
	tr := NewTranscript("/log", Recorded{}, func() Decoder { return d })
	if tr.Usage() != (Usage{}) {
		t.Fatalf("initial usage: %+v", tr.Usage())
	}
	f := transcriptFixture("{}\n{}\n")
	entries, _, err := tr.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]Entry(nil), all[5:]...), Entry{Kind: KindTool, Tool: "Bash"})
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries=%+v want=%+v", entries, want)
	}
	if wantUsage := (Usage{In: 5, CacheWrite: 4, CacheRead: 7, Out: 11, Reasoning: 2, Calls: 1}); tr.Usage() != wantUsage {
		t.Fatalf("usage=%+v want=%+v", tr.Usage(), wantUsage)
	}
	f.files["log"].Data = []byte("{}\n{}\n{}\n")
	entries, _, err = tr.Read(f)
	if err != nil || len(entries) != 0 || tr.Usage() != (Usage{In: 5, CacheWrite: 4, CacheRead: 7, Out: 11, Reasoning: 2, Calls: 1}) {
		t.Fatalf("empty decoded pass: %v %v %+v", entries, err, tr.Usage())
	}
	f.files["log"].Data = []byte("{}\n")
	_, reset, err := tr.Read(f)
	if err != nil || !reset || tr.Usage() != (Usage{}) {
		t.Fatalf("reset usage: %v %v %+v", reset, err, tr.Usage())
	}
}

func TestTranscriptMissingAndReadErrors(t *testing.T) {
	// R-LO0T-0Y6Q R-LP8P-EPXF
	created := 0
	d := &transcriptDecoder{usage: []Usage{{In: 2}}, entries: [][]Entry{{{Kind: KindUser, Text: "one"}}}}
	tr := NewTranscript("/log", Recorded{}, func() Decoder { created++; return d })
	f := transcriptFixture("{}\n")
	if _, _, err := tr.Read(f); err != nil || tr.Usage().In != 2 {
		t.Fatalf("setup: %v %+v", err, tr.Usage())
	}
	delete(f.files, "log")
	entries, reset, err := tr.Read(f)
	if entries != nil || reset || err != nil || tr.Usage().In != 2 || created != 1 || len(d.records) != 1 {
		t.Fatalf("missing: %v %v %v usage=%+v created=%d", entries, reset, err, tr.Usage(), created)
	}
	cause := errors.New("denied")
	f.openErr = &fs.PathError{Op: "open", Path: "log", Err: cause}
	entries, reset, err = tr.Read(f)
	if entries != nil || reset || err == nil {
		t.Fatalf("read failure: %v %v %v", entries, reset, err)
	}
	checkReadError(t, err, "/log", cause)
	if tr.Usage().In != 2 || created != 1 || len(d.records) != 1 {
		t.Fatalf("failure changed state: %+v %d %d", tr.Usage(), created, len(d.records))
	}
	f.openErr = cause
	_, _, err = tr.Read(f)
	checkReadError(t, err, "/log", cause)
	f.openErr = nil
	f.files["log"] = &fstest.MapFile{Data: []byte("{}\n{}\n")}
	_, reset, err = tr.Read(f)
	if err != nil || reset || created != 1 || len(d.records) != 2 {
		t.Fatalf("pass after errors: reset=%v err=%v created=%d records=%d", reset, err, created, len(d.records))
	}
}
