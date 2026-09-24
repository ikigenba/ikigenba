package proc_test

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
)

const sampleStat = "42 (a) b) c) S 1 42 42 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 150 0 0"

func mapFile(data string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(data)} }

// R-HLG2-VR1L R-8ZKD-MKNC R-F8QR-G78N R-9206-E44Q
func TestStartAndTicks(t *testing.T) {
	root := fstest.MapFS{
		"proc/stat":    mapFile("cpu 1 2\nbtime 1787539631\n"),
		"proc/42/stat": mapFile(sampleStat),
	}
	got, err := proc.Start(root, 42)
	if err != nil || got.Compare(time.Unix(1787539632, 500000000)) != 0 {
		t.Fatalf("Start = %v, %v", got, err)
	}
	ticks, err := proc.StartTicks(root, 42)
	if err != nil || ticks != 150 {
		t.Fatalf("StartTicks = %d, %v", ticks, err)
	}
	delete(root, "proc/stat")
	ticks, err = proc.StartTicks(root, 42)
	if err != nil || ticks != 150 {
		t.Fatalf("StartTicks without boot time = %d, %v", ticks, err)
	}
}

// R-A4BE-4UUD R-A6R6-WEBR R-HTZD-K58G
func TestStartErrors(t *testing.T) {
	good := fstest.MapFS{"proc/stat": mapFile("btime 123"), "proc/42/stat": mapFile(sampleStat)}
	for _, pid := range []int{0, -1} {
		if _, err := proc.Start(good, pid); err == nil {
			t.Errorf("Start(%d) accepted invalid pid", pid)
		}
		if _, err := proc.StartTicks(good, pid); err == nil {
			t.Errorf("StartTicks(%d) accepted invalid pid", pid)
		}
	}
	missing := fstest.MapFS{"proc/stat": mapFile("btime 123")}
	if _, err := proc.Start(missing, 42); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Start missing stat: %v", err)
	}
	if _, err := proc.StartTicks(missing, 42); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("StartTicks missing stat: %v", err)
	}
	if _, err := proc.Start(fstest.MapFS{"proc/42/stat": mapFile(sampleStat)}, 42); err == nil {
		t.Error("Start accepted missing boot file")
	}
	for _, boot := range []string{"", "btime -5", "btime +5", "btime 12x", "btime 9223372036854775808", "notbtime 1"} {
		root := fstest.MapFS{"proc/stat": mapFile(boot), "proc/42/stat": mapFile(sampleStat)}
		if _, err := proc.Start(root, 42); err == nil {
			t.Errorf("Start accepted boot %q", boot)
		}
	}
	for _, stat := range []string{"42 (name", "42 (name) S", strings.Replace(sampleStat, "150", "-5", 1), strings.Replace(sampleStat, "150", "+5", 1), strings.Replace(sampleStat, "150", "12x", 1), strings.Replace(sampleStat, "150", "18446744073709551616", 1)} {
		root := fstest.MapFS{"proc/stat": mapFile("btime 123"), "proc/42/stat": mapFile(stat)}
		if _, err := proc.StartTicks(root, 42); err == nil {
			t.Errorf("StartTicks accepted %q", stat)
		}
		if _, err := proc.Start(root, 42); err == nil {
			t.Errorf("Start accepted %q", stat)
		}
	}
	root := fstest.MapFS{"proc/stat": mapFile("btime 9223372036854775807"), "proc/42/stat": mapFile(strings.Replace(sampleStat, "150", "100", 1))}
	if _, err := proc.Start(root, 42); err == nil {
		t.Error("Start accepted overflowing time")
	}
}

// R-HMNZ-9ISA R-HWF6-BOPU
func TestCwd(t *testing.T) {
	root := fstest.MapFS{"proc/42/cwd": {Mode: fs.ModeSymlink, Data: []byte("/home/dev/src/tools")}}
	got, err := proc.Cwd(root, 42)
	if err != nil || got != "/home/dev/src/tools" {
		t.Fatalf("Cwd = %q, %v", got, err)
	}
	want, wantErr := fs.ReadLink(root, "proc/41/cwd")
	got, err = proc.Cwd(root, 41)
	if got != want || reflect.TypeOf(err) != reflect.TypeOf(wantErr) || err.Error() != wantErr.Error() || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing Cwd = %q, %v; want %q, %v", got, err, want, wantErr)
	}
}

// R-HNVV-NAIZ R-HP3S-129O R-I8M6-5E4S R-I02V-GZXX
func TestFileIDOf(t *testing.T) {
	for _, tc := range []struct {
		dev  uint64
		want proc.FileID
	}{
		{0x10305, proc.FileID{Major: 259, Minor: 5, Inode: 77}},
		{0x0000123456789abc, proc.FileID{Major: 0x189a, Minor: 0x234567bc, Inode: 77}},
	} {
		root := fstest.MapFS{"file": {Sys: &syscall.Stat_t{Dev: tc.dev, Ino: 77}}}
		fi, err := fs.Stat(root, "file")
		if err != nil {
			t.Fatal(err)
		}
		got, ok := proc.FileIDOf(fi)
		if !ok || got != tc.want {
			t.Errorf("FileIDOf(%x) = %+v, %v; want %+v", tc.dev, got, ok, tc.want)
		}
	}
	fi, err := fs.Stat(fstest.MapFS{"file": {}}, "file")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := proc.FileIDOf(fi); ok || got != (proc.FileID{}) {
		t.Errorf("FileIDOf nil Sys = %+v, %v", got, ok)
	}
	fi, err = fs.Stat(fstest.MapFS{"file": {Sys: "other"}}, "file")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := proc.FileIDOf(fi); ok || got != (proc.FileID{}) {
		t.Errorf("FileIDOf other Sys = %+v, %v", got, ok)
	}
}

// R-HQBO-EU0D R-IC9V-APCV R-DVNY-TNQ6 R-DWVV-7FGV R-IB1Y-WXM6
func TestLockHolders(t *testing.T) {
	lines := []string{
		"93: -> FLOCK ADVISORY WRITE 1961333 00:30:5192906 0 EOF",
		"2: FLOCK ADVISORY READ 12 00:02:3 0 EOF",
		"3: OFDLCK ADVISORY WRITE -1 00:02:3 0 EOF",
		"", "4: FLOCK WRITE", "5: FLOCK ADVISORY WRITE 12 broken 0 EOF",
		"6: FLOCK ADVISORY WRITE 1761980 103:05:42618649 0 EOF",
		"7: POSIX ADVISORY WRITE 99 103:05:42618649 0 EOF",
		"8: FLOCK ADVISORY WRITE 33 aB:CD:1 0 EOF",
		"9: FLOCK ADVISORY WRITE 0 00:01:2 0 EOF",
		"10: FLOCK ADVISORY WRITE +7 00:01:2 0 EOF",
		"11: FLOCK ADVISORY WRITE 7 100000000:01:2 0 EOF",
		"12: FLOCK ADVISORY WRITE 7 01:01:18446744073709551616 0 EOF",
	}
	root := fstest.MapFS{"proc/locks": mapFile(strings.Join(lines, "\n"))}
	got, err := proc.LockHolders(root)
	want := map[proc.FileID]int{{Major: 259, Minor: 5, Inode: 42618649}: 1761980, {Major: 0xab, Minor: 0xcd, Inode: 1}: 33}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LockHolders = %+v, %v; want %+v", got, err, want)
	}
	for _, content := range []string{"", "1: FLOCK ADVISORY READ 2 00:01:2"} {
		got, err := proc.LockHolders(fstest.MapFS{"proc/locks": mapFile(content)})
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("empty LockHolders = %+v, %v", got, err)
		}
	}
}

type recordFS struct {
	fs.FS
	names []string
}

func (r *recordFS) Open(name string) (fs.File, error) {
	r.names = append(r.names, name)
	return r.FS.Open(name)
}
func (r *recordFS) Lstat(name string) (fs.FileInfo, error) { return fs.Lstat(r.FS, name) }
func (r *recordFS) ReadLink(name string) (string, error) {
	r.names = append(r.names, name)
	return fs.ReadLink(r.FS, name)
}

// R-FB6K-7QQ1
func TestReadScope(t *testing.T) {
	root := &recordFS{FS: fstest.MapFS{"proc/stat": mapFile("btime 100"), "proc/42/stat": mapFile(sampleStat), "proc/42/cwd": {Mode: fs.ModeSymlink, Data: []byte("/tmp")}, "proc/locks": mapFile("")}}
	if _, err := proc.Start(root, 42); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root.names, []string{"proc/42/stat", "proc/stat"}) {
		t.Errorf("Start names: %q", root.names)
	}
	root.names = nil
	if _, err := proc.StartTicks(root, 42); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root.names, []string{"proc/42/stat"}) {
		t.Errorf("StartTicks names: %q", root.names)
	}
	root.names = nil
	if _, err := proc.Cwd(root, 42); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root.names, []string{"proc/42/cwd"}) {
		t.Errorf("Cwd names: %q", root.names)
	}
	root.names = nil
	if _, err := proc.LockHolders(root); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(root.names, []string{"proc/locks"}) {
		t.Errorf("LockHolders names: %q", root.names)
	}
}

type errorFS struct{ err error }

func (e errorFS) Open(string) (fs.File, error) { return nil, e.err }

// R-I66D-DUNE
func TestLockHoldersReadError(t *testing.T) {
	cause := fs.ErrPermission
	pathErr := &fs.PathError{Op: "open", Path: "proc/locks", Err: cause}
	for _, tc := range []struct {
		name  string
		input error
		want  error
	}{
		{"plain", cause, cause},
		{"path", pathErr, cause},
		{"wrapped path", wrappedError{pathErr}, wrappedError{pathErr}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := proc.LockHolders(errorFS{tc.input})
			if got != nil || reflect.TypeOf(err) != reflect.TypeOf(tc.want) || err.Error() != tc.want.Error() {
				t.Errorf("LockHolders read failure = %+v, %v; want nil, %v", got, err, tc.want)
			}
		})
	}
}

type wrappedError struct{ error }

func (w wrappedError) Unwrap() error { return w.error }
