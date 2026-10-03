package smarthttp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/ikigenba/ikigenba/repos/internal/git"
	"golang.org/x/sys/unix"
)

// Serialize the brief owner permission changes used to read or start the
// selected inode. Each change is restored before the helper returns.
var executableMu sync.Mutex

// retainExecutable keeps the selected real git independent of pathname changes.
// A readable image is copied to an anonymous, sealed executable rather than a
// file in the repository tree, whose mount and permissions are unrelated to git.
func retainExecutable(path string) (*os.File, bool, error) {
	executableMu.Lock()
	defer executableMu.Unlock()
	fd, err := unix.Open(filepath.Clean(path), unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, false, err
	}
	selected := os.NewFile(uintptr(fd), path)
	keep := false
	defer func() {
		if !keep {
			_ = selected.Close()
		}
	}()
	info, err := selected.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, false, fmt.Errorf("selected git is not executable")
	}
	reader, pathOnly, err := executableReader(selected, info)
	if pathOnly {
		// An execute-only inode owned by another user, or on an immutable
		// filesystem, can still be executed through its retained reference.
		keep = true
		return selected, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = reader.Close() }()
	image, imageErr := executableImage(reader)
	if imageErr == nil {
		return image, false, nil
	}
	// A snapshot is optional: an execution policy can allow the selected
	// git while refusing anonymous executable images. Keep that original
	// inode and, if its owner removed execute permission during Emit, borrow
	// it only across Start, restoring the post-Emit mode immediately.
	keep = true
	return selected, true, nil
}

func startExecutable(cmd *exec.Cmd, selected *os.File, original bool) error {
	if !original {
		return cmd.Start()
	}
	executableMu.Lock()
	defer executableMu.Unlock()
	info, err := selected.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int64(stat.Uid) != int64(os.Geteuid()) || stat.Mode&0100 != 0 {
		return cmd.Start()
	}
	mode := stat.Mode & 07777
	if err = chmodExecutable(selected, mode|0100); err != nil {
		return err
	}
	startErr := cmd.Start()
	restoreErr := chmodExecutable(selected, mode)
	if restoreErr != nil {
		restoreErr = fmt.Errorf("restore selected git mode after start: %w", restoreErr)
	}
	return errors.Join(startErr, restoreErr)
}

func executableReader(selected *os.File, info os.FileInfo) (*os.File, bool, error) {
	name := "/proc/self/fd/" + strconv.FormatUint(uint64(selected.Fd()), 10)
	reader, err := os.Open(filepath.Clean(name))
	if !errors.Is(err, os.ErrPermission) {
		return reader, false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int64(stat.Uid) != int64(os.Geteuid()) {
		return nil, true, nil
	}
	// Opening a read descriptor needs read permission, executing does not.
	// Borrow only the owner's read bit on this exact retained inode; once
	// open, the descriptor stays readable after the original mode is restored.
	mode := stat.Mode & 07777
	if err = chmodExecutable(selected, mode|0400); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, unix.EROFS) {
			return nil, true, nil
		}
		return nil, false, err
	}
	reader, openErr := os.Open(filepath.Clean(name))
	restoreErr := chmodExecutable(selected, mode)
	if restoreErr != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, false, fmt.Errorf("restore selected git mode: %w", restoreErr)
	}
	return reader, false, openErr
}

func chmodExecutable(selected *os.File, mode uint32) error {
	err := unix.Fchmodat(int(selected.Fd()), "", mode, unix.AT_EMPTY_PATH)
	if errors.Is(err, unix.EOPNOTSUPP) {
		// Older kernels have no fchmodat2. This procfs reference still names
		// the retained inode, including when its original name was unlinked.
		err = unix.Chmod("/proc/self/fd/"+strconv.FormatUint(uint64(selected.Fd()), 10), mode)
	}
	return err
}

func executableImage(reader *os.File) (*os.File, error) {
	fd, err := unix.MemfdCreate("git-executable", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING|unix.MFD_EXEC)
	if errors.Is(err, unix.EINVAL) {
		// MFD_EXEC was introduced after executable memfds themselves.
		fd, err = unix.MemfdCreate("git-executable", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	}
	if err != nil {
		return nil, err
	}
	image := os.NewFile(uintptr(fd), "git-executable")
	defer func() { _ = image.Close() }()
	if _, err = io.CopyBuffer(image, reader, make([]byte, git.CopyBufferSize)); err != nil {
		return nil, err
	}
	if err = image.Chmod(0500); err != nil {
		return nil, err
	}
	seals := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_EXEC | unix.F_SEAL_SEAL
	if _, err = unix.FcntlInt(image.Fd(), unix.F_ADD_SEALS, seals); errors.Is(err, unix.EINVAL) {
		_, err = unix.FcntlInt(image.Fd(), unix.F_ADD_SEALS, seals & ^unix.F_SEAL_EXEC)
	}
	if err != nil {
		return nil, err
	}
	// Exec requires that no writable description of the image remain open.
	return os.Open("/proc/self/fd/" + strconv.FormatUint(uint64(image.Fd()), 10))
}
