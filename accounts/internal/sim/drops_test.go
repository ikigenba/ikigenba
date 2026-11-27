package sim

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestReadDropStreamsOneLineAtATime(t *testing.T) {
	// R-D78B-07KZ R-D9O3-RR2D
	long := strings.Repeat("x", 20<<10)
	path := filepath.Join(t.TempDir(), "drop")
	input := "first\n" + long + "\nlast"
	must(t, os.WriteFile(path, []byte(input), 0o600))
	f, err := os.Open(filepath.Clean(path))
	must(t, err)
	t.Cleanup(func() { must(t, f.Close()) })
	var got []string
	readDrop(f, MaxBackupFileBytes, func(line []byte) {
		if len(got) == 0 {
			offset, err := f.Seek(0, io.SeekCurrent)
			must(t, err)
			if offset >= int64(len("first\n")+len(long)+1) {
				t.Fatalf("first line reached the consumer after reading the next complete line: offset %d", offset)
			}
		}
		got = append(got, string(line))
	})
	if !slices.Equal(got, []string{"first", long, "last"}) {
		t.Fatalf("reader returned %d lines; expected all three intact", len(got))
	}
}

func TestLookProcessesBanAndBackupDropsBeforeBoxDirectory(t *testing.T) {
	// R-D78B-07KZ R-D9O3-RR2D R-DEJP-AU15 R-DGZI-2DIJ
	dir := shortTempDir(t)
	cfg := Config{
		BanFile: filepath.Join(dir, "bans"), BackupFile: filepath.Join(dir, "backups"), BoxDir: filepath.Join(dir, "box"),
	}
	const blocked = "192.0.2.77"
	const database = "data/app.db"
	const blocker = "keep"
	must(t, os.WriteFile(cfg.BoxDir, []byte(blocker), 0o600))
	b := runSandbox(t, cfg, 1, nil)
	alice := activeBoxUser("alice", 4242)
	b.apply(1, 1, alice)
	writeDrops := func() {
		t.Helper()
		must(t, os.WriteFile(cfg.BanFile, []byte("host01 "+blocked+"\n"), 0o600))
		must(t, os.WriteFile(cfg.BackupFile, []byte("host01 team database "+database+"\n"), 0o600))
	}
	writeDrops()
	b.look()
	expectAbsent(t, cfg.BanFile)
	expectAbsent(t, cfg.BackupFile)
	got, err := os.ReadFile(filepath.Clean(cfg.BoxDir))
	must(t, err)
	if string(got) != blocker {
		t.Fatalf("look changed the box path to %q", got)
	}
	r := b.apply(1, 2, alice)
	if len(r.Bans) != 1 || r.Bans[0].Address != blocked {
		t.Fatalf("invalid box path prevented applying the ban: %+v", r.Bans)
	}
	if dbs := teamBackup(t, r).Databases; len(dbs) != 1 || dbs[0].Path != database {
		t.Fatalf("invalid box path prevented adding the database: %+v", dbs)
	}
	must(t, os.Remove(cfg.BoxDir))
	writeDrops()
	watch := watchDirectory(t, dir)
	b.look()
	var actions []string
	readDirectoryEvents(t, watch, func(event syscall.InotifyEvent, name string) {
		if event.Mask == syscall.IN_DELETE || event.Mask == syscall.IN_CREATE|syscall.IN_ISDIR {
			actions = append(actions, name)
		}
	})
	want := []string{filepath.Base(cfg.BanFile), filepath.Base(cfg.BackupFile), filepath.Base(cfg.BoxDir)}
	if !slices.Equal(actions, want) {
		t.Fatalf("look consumed drops and created the box directory in order %v, want %v", actions, want)
	}
	expectSocket(t, filepath.Join(cfg.BoxDir, alice.Name))
}
