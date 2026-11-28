package linux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// SeedMarker is the marker path relative to an account's folder.
const SeedMarker = ".git/ikigenba-seed"

func pendingSeed(gid int64) string { return fmt.Sprintf("pending %d\n", gid) }

func (h *Host) stageAccountFolder(ctx context.Context, account protocol.Account, staging, path string) error {
	full := h.path(staging)
	if err := os.Mkdir(full, 0o700); err != nil {
		return hostError(staging, err)
	}
	if err := h.accountFolderPermissions(ctx, account.Slug, staging); err != nil {
		return err
	}
	if _, err := h.run(ctx, "runuser", "-u", account.Slug, "--", "git", "init", "--shared=group", full); err != nil {
		return err
	}
	marker := filepath.Join(full, SeedMarker)
	if err := h.teeAs(ctx, account.Slug, marker, strings.NewReader(pendingSeed(account.GID))); err != nil {
		return err
	}
	if err := os.Rename(full, h.path(path)); err != nil {
		return hostError(path, err)
	}
	return nil
}

func (h *Host) seedAccountFolder(ctx context.Context, s protocol.DesiredState, account protocol.Account, path string) error {
	full := h.path(path)
	pending, err := hasPendingSeed(full, pendingSeed(account.GID))
	if err != nil {
		return hostError(filepath.Join(path, SeedMarker), err)
	}
	if !pending {
		return nil
	}
	res, err := h.query(ctx, Command{
		Name: "runuser",
		Args: []string{"-u", account.Slug, "--", "git", "-C", full, "rev-parse", "--verify", "--quiet", "HEAD"},
	}, 0, 1)
	if err != nil {
		return err
	}
	if res.ExitCode == 1 {
		if err := h.commitSeed(ctx, account.Slug, accountHostName(s), path); err != nil {
			return err
		}
	}
	return h.teeAs(ctx, account.Slug, filepath.Join(full, SeedMarker), strings.NewReader("done\n"))
}

func accountHostName(s protocol.DesiredState) string {
	if s.Domain == "" {
		return s.Host
	}
	return s.Host + "." + s.Domain
}

func hasPendingSeed(full, pending string) (bool, error) {
	folder, err := os.OpenFile(filepath.Clean(full), os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer func() { _ = folder.Close() }()

	dir, name := filepath.Split(SeedMarker)
	git, err := openSeedAt(folder, filepath.Clean(dir), syscall.O_DIRECTORY)
	if git == nil {
		return false, err
	}
	defer func() { _ = git.Close() }()
	return pendingSeedIn(git, name, pending)
}

func pendingSeedIn(git *os.File, name, pending string) (bool, error) {
	marker, err := openSeedAt(git, name, syscall.O_NONBLOCK)
	if marker == nil {
		return false, err
	}
	defer func() { _ = marker.Close() }()
	info, err := marker.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(pending)) {
		return false, nil
	}
	// One extra byte distinguishes the exact value from a concurrent extension.
	data, err := io.ReadAll(io.LimitReader(marker, int64(min(MaxCapture, len(pending)+1))))
	return string(data) == pending, err
}

func openSeedAt(dir *os.File, name string, flags int) (*os.File, error) {
	fd, err := syscall.Openat(int(dir.Fd()), name, flags|syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ELOOP),
		errors.Is(err, syscall.ENXIO), errors.Is(err, syscall.ENODEV):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (h *Host) commitSeed(ctx context.Context, slug, hostname, path string) error {
	source := accounts.Seed()
	if err := h.copySeed(ctx, slug, path, source); err != nil {
		return err
	}
	full := h.path(path)
	if err := h.addSeedFiles(ctx, slug, full, source); err != nil {
		return err
	}
	_, err := h.run(ctx, "runuser", "-u", slug, "--", "git", "-C", full,
		"-c", "user.name="+slug, "-c", "user.email="+slug+"@"+hostname, "commit", "--quiet", "-m", machine.SeedMessage)
	return err
}

func (h *Host) copySeed(ctx context.Context, slug, path string, source fs.FS) error {
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		target := filepath.Join(path, name)
		if err != nil {
			return hostError(target, err)
		}
		switch {
		case name == ".":
			return nil
		case entry.IsDir():
			_, err := h.run(ctx, "runuser", "-u", slug, "--", "mkdir", "-p", h.path(target))
			return err
		case !entry.Type().IsRegular():
			return nil
		default:
			body, err := source.Open(name)
			if err != nil {
				return hostError(target, err)
			}
			defer func() { _ = body.Close() }()
			return h.teeAs(ctx, slug, h.path(target), body)
		}
	})
}

func (h *Host) addSeedFiles(ctx context.Context, slug, full string, source fs.FS) error {
	base := []string{"-u", slug, "--", "git", "-C", full, "add", "--force", "--"}
	args := slices.Grow(base, ChownBatch)
	prefix := len(base)
	flush := func() error {
		if len(args) == prefix {
			return nil
		}
		_, err := h.run(ctx, "runuser", args...)
		args = args[:prefix]
		return err
	}
	if err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			args = append(args, name)
			if len(args)-prefix == ChownBatch {
				return flush()
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return flush()
}
