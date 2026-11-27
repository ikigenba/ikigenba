package linux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const restoreWorkPrefix = ".accountsd-restore-"

var errRestoreTargetOccupied = errors.New("restore target is occupied")

// Open each component without following links. Rename and unlink then use
// these directory descriptors so a changed pathname cannot redirect the write.
func (h *Host) openRestoreDir(path string, create bool) (*os.File, error) {
	relative := strings.TrimPrefix(path, "/")
	anchor, rest, _ := strings.Cut(relative, "/")
	if anchor != "srv" && anchor != "home" {
		return nil, hostError(path, fs.ErrInvalid)
	}
	const flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Open(h.path("/"+anchor), flags, 0)
	if err != nil {
		return nil, hostError(path, err)
	}
	if rest != "" {
		for name := range strings.SplitSeq(rest, "/") {
			next, err := unix.Openat(fd, name, flags, 0)
			if create && errors.Is(err, fs.ErrNotExist) {
				err = unix.Mkdirat(fd, name, 0o700)
				if err == nil || errors.Is(err, fs.ErrExist) {
					next, err = unix.Openat(fd, name, flags, 0)
				}
			}
			_ = unix.Close(fd)
			if err != nil {
				return nil, hostError(path, err)
			}
			fd = next
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (h *Host) restoreEntry(path string) (unix.Stat_t, error) {
	var stat unix.Stat_t
	parent, err := h.openRestoreDir(filepath.Dir(path), false)
	if err != nil {
		return stat, err
	}
	defer func() { _ = parent.Close() }()
	if err := unix.Fstatat(int(parent.Fd()), filepath.Base(path), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return stat, hostError(path, err)
	}
	return stat, nil
}

func (h *Host) checkRestoreTarget(path string) error {
	stat, err := h.restoreEntry(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case stat.Mode&unix.S_IFMT != unix.S_IFDIR:
		return errRestoreTargetOccupied
	}
	dir, err := h.openRestoreDir(path, false)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	if _, err := dir.Readdirnames(1); err == io.EOF {
		return nil
	} else if err != nil {
		return hostError(path, err)
	}
	return errRestoreTargetOccupied
}

func (h *Host) moveRestoreEntry(source, target string, replaceEmptyDir bool) error {
	from, err := h.openRestoreDir(filepath.Dir(source), false)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()
	to, err := h.openRestoreDir(filepath.Dir(target), false)
	if err != nil {
		return err
	}
	defer func() { _ = to.Close() }()

	name := filepath.Base(target)
	if replaceEmptyDir {
		if err := unix.Unlinkat(int(to.Fd()), name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return hostError(target, err)
		}
	}
	if err := unix.Renameat2(int(from.Fd()), filepath.Base(source), int(to.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return hostError(target, err)
	}
	return nil
}

type restorePermissions struct {
	uid, gid int64
	mode     fs.FileMode
}

func modeFromUnixRestore(mode uint32) fs.FileMode {
	out := fs.FileMode(mode & 0o777)
	if mode&unix.S_ISUID != 0 {
		out |= fs.ModeSetuid
	}
	if mode&unix.S_ISGID != 0 {
		out |= fs.ModeSetgid
	}
	if mode&unix.S_ISVTX != 0 {
		out |= fs.ModeSticky
	}
	return out
}

func (h *Host) setRestorePermissions(ctx context.Context, path, owner string, mode fs.FileMode) error {
	if _, err := h.run(ctx, "chown", "--no-dereference", owner, h.path(path)); err != nil {
		return err
	}
	return h.mode(path, mode)
}

func (h *Host) restoreDatabaseOwner(ctx context.Context, path string, permissions restorePermissions) error {
	return h.setRestorePermissions(ctx, path, fmt.Sprintf("%d:%d", permissions.uid, permissions.gid), permissions.mode)
}

func (h *Host) rootRestoreWork(ctx context.Context, path string) error {
	stat, err := h.restoreEntry(path)
	if err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return hostError(path, fs.ErrInvalid)
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return h.setRestorePermissions(ctx, path, "root:root", 0o700)
	}
	return h.mode(path, 0o700)
}

// Both anchors are root-owned bind mounts. The temporary work directory is
// created immediately beneath its anchor, on the same mount as the copy.
func (h *Host) temporaryRestoreWork(ctx context.Context, anchor string) (string, error) {
	dir, err := h.openRestoreDir(anchor, false)
	if err != nil {
		return "", err
	}
	_ = dir.Close()
	full, err := os.MkdirTemp(h.path(anchor), restoreWorkPrefix)
	if err != nil {
		return "", hostError(anchor, err)
	}
	work := filepath.Join(anchor, filepath.Base(full))
	if err := h.rootRestoreWork(ctx, work); err != nil {
		return "", errors.Join(err, h.removeRestoreWork(work))
	}
	return work, nil
}

func (h *Host) prepareRestoreWork(ctx context.Context, work string) error {
	stat, err := h.restoreEntry(work)
	if err == nil && stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		if err = os.Remove(h.path(work)); err != nil {
			return hostError(work, err)
		}
		err = fs.ErrNotExist
	}
	if errors.Is(err, fs.ErrNotExist) {
		err = os.Mkdir(h.path(work), 0o700)
	}
	if err != nil {
		return hostError(work, err)
	}
	return h.rootRestoreWork(ctx, work)
}

func (h *Host) removeRestoreWork(path string) error {
	if err := os.RemoveAll(h.path(path)); err != nil {
		return hostError(path, err)
	}
	return nil
}
