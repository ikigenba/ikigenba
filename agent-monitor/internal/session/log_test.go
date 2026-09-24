package session

import (
	"errors"
	"io"
	"io/fs"
	"reflect"
	"syscall"
	"testing"
	"testing/fstest"
)

type logFS struct {
	files    fstest.MapFS
	openErr  error
	statErr  error
	readErr  error
	closeErr error
	noAt     bool
	eofAfter int64
	eofCall  int
	calls    []string
	ranges   [][2]int64
}

func (f *logFS) Open(name string) (fs.File, error) {
	f.calls = append(f.calls, "Open "+name)
	if f.openErr != nil {
		return nil, f.openErr
	}
	base, err := f.files.Open(name)
	if err != nil {
		return nil, err
	}
	file := &logFile{File: base, owner: f}
	if f.noAt {
		return &logFileWithoutAt{file: file}, nil
	}
	return file, nil
}

type logFile struct {
	fs.File
	owner *logFS
}

func (f *logFile) Stat() (fs.FileInfo, error) {
	f.owner.calls = append(f.owner.calls, "Stat")
	if f.owner.statErr != nil {
		return nil, f.owner.statErr
	}
	return f.File.Stat()
}

func (f *logFile) Read(p []byte) (int, error) {
	f.owner.calls = append(f.owner.calls, "Read")
	return f.File.Read(p)
}

func (f *logFile) ReadAt(p []byte, off int64) (int, error) {
	f.owner.calls = append(f.owner.calls, "ReadAt")
	f.owner.ranges = append(f.owner.ranges, [2]int64{off, off + int64(len(p))})
	if f.owner.readErr != nil {
		return 0, f.owner.readErr
	}
	if f.owner.eofAfter > 0 {
		remaining := f.owner.eofAfter - off
		if remaining <= 0 {
			if f.owner.eofCall == 0 {
				f.owner.eofCall = len(f.owner.ranges)
			}
			return 0, io.EOF
		}
		if int64(len(p)) >= remaining {
			n, _ := f.File.(io.ReaderAt).ReadAt(p[:remaining], off)
			if f.owner.eofCall == 0 {
				f.owner.eofCall = len(f.owner.ranges)
			}
			return n, io.EOF
		}
	}
	return f.File.(io.ReaderAt).ReadAt(p, off)
}

func (f *logFile) Close() error {
	f.owner.calls = append(f.owner.calls, "Close")
	_ = f.File.Close()
	return f.owner.closeErr
}

type logFileWithoutAt struct{ file *logFile }

func (f *logFileWithoutAt) Stat() (fs.FileInfo, error) { return f.file.Stat() }
func (f *logFileWithoutAt) Read(p []byte) (int, error) { return f.file.Read(p) }
func (f *logFileWithoutAt) Close() error               { return f.file.Close() }

func logFixture(content string, inode uint64) *logFS {
	return &logFS{files: fstest.MapFS{"log": {Data: []byte(content), Sys: &syscall.Stat_t{Dev: 1, Ino: inode}}}}
}

func assertPass(t *testing.T, l *Log, f *logFS, want []string, wantReset bool) {
	t.Helper()
	f.calls, f.ranges, f.eofCall = nil, nil, 0
	lines, reset, err := l.Read(f, "log")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := make([]string, len(lines))
	for i, line := range lines {
		got[i] = string(line)
	}
	if !reflect.DeepEqual(got, want) || reset != wantReset {
		t.Fatalf("lines=%q reset=%v; want %q, %v", got, reset, want, wantReset)
	}
	if len(f.calls) < 3 || f.calls[0] != "Open log" || f.calls[1] != "Stat" || f.calls[len(f.calls)-1] != "Close" {
		t.Fatalf("call order: %v", f.calls)
	}
	statCalls := 0
	for _, call := range f.calls {
		if call == "Stat" {
			statCalls++
		}
	}
	if statCalls != 1 {
		t.Fatalf("Stat called %d times: %v", statCalls, f.calls)
	}
	for _, call := range f.calls[2 : len(f.calls)-1] {
		if call != "ReadAt" {
			t.Fatalf("unexpected file method: %v", f.calls)
		}
	}
	for i, interval := range f.ranges {
		if interval[0] < 0 || interval[0] >= interval[1] || interval[1] > int64(len(f.files["log"].Data)) {
			t.Fatalf("invalid ReadAt range: %v", f.ranges)
		}
		for _, earlier := range f.ranges[:i] {
			if interval[0] < earlier[1] && earlier[0] < interval[1] {
				t.Fatalf("overlapping ReadAt ranges: %v", f.ranges)
			}
		}
	}
}

func TestLogFirstPassAndAppend(t *testing.T) {
	// R-2MVM-VBXQ R-2O3J-93OF R-2PBF-MVF4 R-2QJC-0N5T
	// R-2SZ4-S6N7 R-2U71-5YDW R-59S8-SV67 R-BB0C-V2OE
	var log Log
	for i := 0; i < reflect.TypeOf(log).NumField(); i++ {
		if reflect.TypeOf(log).Field(i).IsExported() {
			t.Fatal("Log has an exported field")
		}
	}
	f := logFixture("a\n\nb\nc", 1)
	assertPass(t, &log, f, []string{"a", "", "b"}, false)
	assertPass(t, &log, f, []string{}, false)
	if len(f.ranges) != 0 {
		t.Fatalf("unchanged log was read: %v", f.ranges)
	}
	f.files["log"].Data = []byte("a\n\nb\ncdef\npart")
	assertPass(t, &log, f, []string{"cdef"}, false)
	for _, interval := range f.ranges {
		if interval[0] < 6 {
			t.Fatalf("append reread old bytes: %v", f.ranges)
		}
	}
	f.files["log"].Data = []byte("a\n\nb\ncdef\npartial\n")
	assertPass(t, &log, f, []string{"partial"}, false)
}

func TestLogResetAndLineLifetime(t *testing.T) {
	// R-2RR8-EEWI R-2XUQ-B9LZ R-32QB-UCKR
	var log Log
	f := logFixture("long\ncarried", 1)
	lines, reset, err := log.Read(f, "log")
	if err != nil || reset || len(lines) != 1 || string(lines[0]) != "long" {
		t.Fatalf("first pass: %q %v %v", lines, reset, err)
	}
	f.files["log"].Data = []byte("new\n")
	assertPass(t, &log, f, []string{"new"}, true)
	if string(lines[0]) != "long" {
		t.Fatalf("returned line changed after shrink: %q", lines[0])
	}
	f.files["log"].Data = []byte("same size but new inode\n")
	f.files["log"].Sys = &syscall.Stat_t{Dev: 1, Ino: 2}
	assertPass(t, &log, f, []string{"same size but new inode"}, true)
	f.files["log"].Sys = nil
	f.files["log"].Data = []byte("same size but new inode\nnext\n")
	assertPass(t, &log, f, []string{"next"}, false)
	f.files["log"].Sys = &syscall.Stat_t{Dev: 1, Ino: 3}
	f.files["log"].Data = []byte("same size but new inode\nnext\nagain\n")
	assertPass(t, &log, f, []string{"again"}, false)
	var other Log
	otherFile := logFixture("other\n", 4)
	assertPass(t, &other, otherFile, []string{"other"}, false)
	if string(lines[0]) != "long" {
		t.Fatalf("returned line changed after another Log: %q", lines[0])
	}
}

func TestLogErrorsLeaveStateAlone(t *testing.T) {
	// R-2Z2M-P1CO R-BDG5-MM5S R-BEO2-0DWH
	var log Log
	f := logFixture("head\nfrag", 1)
	assertPass(t, &log, f, []string{"head"}, false)
	f.files["log"].Data = []byte("head\nfragment\n")
	for _, failure := range []struct {
		name string
		set  func()
		clr  func()
	}{
		{"open", func() { f.openErr = fs.ErrPermission }, func() { f.openErr = nil }},
		{"stat", func() { f.statErr = fs.ErrPermission }, func() { f.statErr = nil }},
		{"read", func() { f.readErr = fs.ErrPermission }, func() { f.readErr = nil }},
	} {
		t.Run(failure.name, func(t *testing.T) {
			failure.set()
			lines, reset, err := log.Read(f, "log")
			if lines != nil || reset || reflect.ValueOf(err) != reflect.ValueOf(fs.ErrPermission) {
				t.Fatalf("failure result: %q %v %v", lines, reset, err)
			}
			failure.clr()
		})
	}
	f.noAt = true
	f.statErr = fs.ErrPermission
	f.calls = nil
	lines, reset, err := log.Read(f, "log")
	if lines != nil || reset || reflect.ValueOf(err) != reflect.ValueOf(fs.ErrPermission) {
		t.Fatalf("Stat before ReaderAt check: %q %v %v", lines, reset, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"Open log", "Stat", "Close"}) {
		t.Fatalf("Stat failure on file without ReaderAt: %v", f.calls)
	}
	f.statErr = nil
	f.calls = nil
	lines, reset, err = log.Read(f, "log")
	if lines != nil || reset || err == nil {
		t.Fatalf("missing ReaderAt: %q %v %v", lines, reset, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"Open log", "Stat", "Close"}) {
		t.Fatalf("missing ReaderAt call order: %v", f.calls)
	}
	f.noAt = false
	f.closeErr = errors.New("close failure")
	assertPass(t, &log, f, []string{"fragment"}, false)
	for _, interval := range f.ranges {
		if interval[0] < 9 {
			t.Fatalf("failed passes changed offset: %v", f.ranges)
		}
	}
	f.statErr = fs.ErrPermission
	lines, reset, err = log.Read(f, "log")
	if lines != nil || reset || reflect.ValueOf(err) != reflect.ValueOf(fs.ErrPermission) {
		t.Fatalf("Close error changed failing pass: %q %v %v", lines, reset, err)
	}
	f.statErr = nil
	f.files["log"].Data = []byte("rotated\n")
	assertPass(t, &log, f, []string{"rotated"}, true)
	missing := &logFS{files: fstest.MapFS{}}
	lines, reset, err = log.Read(missing, "log")
	if lines != nil || reset || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %q %v %v", lines, reset, err)
	}
}

func TestLogLargeFileAndEOF(t *testing.T) {
	// R-2U71-5YDW R-2SZ4-S6N7 R-2PBF-MVF4
	content := make([]byte, 70000)
	for i := range content {
		content[i] = 'x'
	}
	content[len(content)-1] = '\n'
	f := logFixture(string(content), 1)
	var log Log
	assertPass(t, &log, f, []string{string(content[:len(content)-1])}, false)
	var eofLog Log
	f.eofAfter = 5
	assertPass(t, &eofLog, f, []string{}, false)
	if f.eofCall == 0 || len(f.ranges) != f.eofCall {
		t.Fatalf("ReadAt called after EOF: %v", f.ranges)
	}
	f.eofAfter = 0
	assertPass(t, &eofLog, f, []string{string(content[:len(content)-1])}, false)
	for _, interval := range f.ranges {
		if interval[0] < 5 {
			t.Fatalf("EOF pass offset not retained: %v", f.ranges)
		}
	}
}
