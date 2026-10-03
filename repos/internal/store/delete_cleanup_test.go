package store_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func TestDeleteLateOwnedRootPermissionFaultLeavesExactState(t *testing.T) {
	// R-NTBU-K7EG R-WY0N-16WU
	for _, memory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "memory"}[memory], func(t *testing.T) {
			f := setup(t)
			if memory {
				f.cfg.Source = ":memory:"
			}
			s := f.open(t)
			r := create(t, s, "owner", "notes")
			other := create(t, s, "other", "keep")
			// Many tiny owned files make the staged directory observable while
			// deletion proceeds; the watcher proves the actual phase it faulted.
			payload := filepath.Join(s.Dir(r.ID), "payload")
			must(t, os.Mkdir(payload, 0700))
			for i := 0; i < 8192; i++ {
				write(t, filepath.Join(payload, fmt.Sprintf("file-%05d", i)), "x")
			}
			must(t, syscall.Mkfifo(filepath.Join(payload, "pipe"), 0600))
			must(t, syscall.Mknod(filepath.Join(payload, "socket"), syscall.S_IFSOCK|0600, 0))
			empty := filepath.Join(payload, "readonly-empty")
			must(t, os.Mkdir(empty, 0500))
			before := snapshot(t, f.cfg.Root)
			beforeRows := all(t, s)
			rootMode := before["."].Mode.Perm()
			t.Cleanup(func() { must(t, os.Chmod(f.cfg.Root, rootMode)) })
			fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
			must(t, err)
			watcher := os.NewFile(uintptr(fd), "owned-root-watch")
			defer func() { must(t, watcher.Close()) }()
			_, err = syscall.InotifyAddWatch(fd, f.cfg.Root, syscall.IN_MOVED_FROM)
			must(t, err)
			ctx := testContext(t)
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("missing watcher deadline")
			}
			must(t, watcher.SetReadDeadline(deadline))
			faultDone := make(chan error, 1)
			go func() {
				faultDone <- faultAfterObservedStaging(watcher, f.cfg.Root, r.ID+".git", rootMode&^0222)
			}()
			deleteErr := s.Delete(ctx, r.ID)
			select {
			case err := <-faultDone:
				must(t, err)
			case <-ctx.Done():
				t.Fatal("watcher did not observe and fault staged deletion")
			}
			// The fault remains in place through the reply, rather than being
			// restored by the fixture before final cleanup could encounter it.
			info, err := os.Stat(f.cfg.Root)
			must(t, err)
			same(t, info.Mode().Perm(), rootMode&^0222)
			must(t, os.Chmod(f.cfg.Root, rootMode))
			var wantRows []store.Repo
			wantTree := before
			if deleteErr != nil {
				if errors.Is(deleteErr, store.ErrNotFound) {
					t.Fatal(deleteErr)
				}
				wantRows = beforeRows
			} else {
				wantRows = []store.Repo{other}
				wantTree = make(map[string]entry)
				for name, value := range before {
					if name != r.ID+".git" && !strings.HasPrefix(name, r.ID+".git/") {
						wantTree[name] = value
					}
				}
			}
			same(t, all(t, s), wantRows)
			same(t, snapshot(t, f.cfg.Root), wantTree)
			if !memory {
				must(t, s.Close())
				s = f.open(t)
				same(t, all(t, s), wantRows)
				same(t, snapshot(t, f.cfg.Root), wantTree)
			}
		})
	}
}

func faultAfterObservedStaging(watcher *os.File, root, original string, mode os.FileMode) error {
	buffer := make([]byte, 4096)
	for {
		n, err := watcher.Read(buffer)
		if err != nil {
			return err
		}
		for offset := 0; offset+syscall.SizeofInotifyEvent <= n; {
			header := buffer[offset : offset+syscall.SizeofInotifyEvent]
			length := int(binary.NativeEndian.Uint32(header[12:16]))
			end := offset + syscall.SizeofInotifyEvent + length
			if end > n {
				return errors.New("truncated owned directory event")
			}
			name := string(bytes.TrimRight(buffer[offset+syscall.SizeofInotifyEvent:end], "\x00"))
			mask := binary.NativeEndian.Uint32(header[4:8])
			offset = end
			if name != original || mask&syscall.IN_MOVED_FROM == 0 {
				continue
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), ".delete-") {
					continue
				}
				staged := filepath.Join(root, e.Name(), "entry")
				if _, err = os.Stat(staged); err != nil {
					continue
				}
				if _, err = os.Lstat(filepath.Join(root, original)); !errors.Is(err, os.ErrNotExist) {
					return errors.New("original directory still present at fault phase")
				}
				if err = os.Chmod(root, mode); err != nil {
					return err
				}
				// Observe the fault and staged entry together after chmod, so
				// a chmod racing after Delete's completion cannot pass.
				info, err := os.Stat(root)
				if err != nil {
					return err
				}
				if info.Mode().Perm() != mode {
					return errors.New("root permission fault was not observed")
				}
				if _, err = os.Stat(staged); err != nil {
					return fmt.Errorf("permission fault did not overlap active staged entry: %w", err)
				}
				if _, err = os.Lstat(filepath.Join(root, original)); !errors.Is(err, os.ErrNotExist) {
					return errors.New("original entry restored before fault observation")
				}
				return nil
			}
			return errors.New("staged entry was not observed before cleanup")
		}
	}
}
