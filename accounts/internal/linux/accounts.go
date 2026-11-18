package linux

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const accountMode = fs.ModeSetgid | 0o770

func (h *Host) accounts(ctx context.Context, s protocol.DesiredState) error {
	var knownDirectories map[string]bool
	for _, account := range s.Accounts {
		if account.State != protocol.AccountActive {
			continue
		}
		local, err := h.identity(account.Slug, account.GID, account.Slug)
		if err != nil {
			return err
		}
		id := strconv.FormatInt(account.GID, 10)
		if !local.group {
			if _, err := h.run(ctx, "groupadd", "--system", "--gid", id, account.Slug); err != nil {
				return err
			}
		}
		path := filepath.Join("/srv", account.Slug)
		if !local.account {
			if _, err := h.run(ctx, "useradd", "--system", "--uid", id, "--gid", account.Slug, "--home-dir", path, "--no-create-home", "--shell", "/usr/sbin/nologin", account.Slug); err != nil {
				return err
			}
		}
		if err := h.ensureAccountFolder(ctx, account, path); err != nil {
			return err
		}
		gitConfig := h.path("/etc/gitconfig")
		if knownDirectories == nil {
			res, err := h.query(ctx, Command{Name: "git", Args: []string{"config", "--file", gitConfig, "--get-all", "safe.directory"}}, 0, 1)
			if err != nil {
				return err
			}
			knownDirectories = make(map[string]bool)
			for dir := range strings.SplitSeq(string(res.Stdout), "\n") {
				knownDirectories[dir] = true
			}
		}
		if !knownDirectories[path] {
			if _, err := h.run(ctx, "git", "config", "--file", gitConfig, "--add", "safe.directory", path); err != nil {
				return err
			}
			knownDirectories[path] = true
			h.written["/etc/gitconfig"] = struct{}{}
		}
	}
	return nil
}

func (h *Host) ensureAccountFolder(ctx context.Context, account protocol.Account, path string) error {
	full := h.path(path)
	info, err := os.Lstat(full)
	missing := errors.Is(err, fs.ErrNotExist)
	switch {
	case missing:
		if err := os.Mkdir(full, 0o700); err != nil {
			return hostError(path, err)
		}
	case err != nil:
		return hostError(path, err)
	case !info.IsDir():
		return hostError(path, fs.ErrInvalid)
	}
	if missing || !ownerIs(info, account.GID, account.GID) {
		if _, err := h.run(ctx, "chown", account.Slug+":"+account.Slug, full); err != nil {
			return err
		}
		if err := h.mode(path, accountMode); err != nil {
			return err
		}
		if _, err := h.run(ctx, "setfacl", "-d", "-m", "g::rwx,o::-", full); err != nil {
			return err
		}
	}
	_, err = os.Lstat(filepath.Join(full, ".git"))
	if errors.Is(err, fs.ErrNotExist) {
		_, err = h.run(ctx, "runuser", "-u", account.Slug, "--", "git", "init", "--shared=group", full)
		return err
	}
	if err != nil {
		return hostError(filepath.Join(path, ".git"), err)
	}
	return nil
}

func ownerIs(info fs.FileInfo, uid, gid int64) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == uid && int64(stat.Gid) == gid
}

// visitChildren keeps just one directory batch at each depth and does not follow links.
func (h *Host) visitChildren(path string, visit func(fs.DirEntry) error) error {
	d, err := os.Open(h.path(path))
	if err != nil {
		return hostError(path, err)
	}
	defer func() { _ = d.Close() }()
	for {
		entries, err := d.ReadDir(32)
		for _, entry := range entries {
			if visitErr := visit(entry); visitErr != nil {
				return visitErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return hostError(path, err)
		}
	}
}

func (h *Host) reassignFiles(ctx context.Context, uid int64, recipients map[string]bool) error {
	owners := make(map[string]bool, len(recipients))
	if err := h.passwd(func(user string, _, _ int64, home string) {
		if recipients[user] && home == filepath.Join("/srv", user) {
			owners[user] = true
		}
	}); err != nil {
		return err
	}
	return h.visitChildren("/srv", func(top fs.DirEntry) error {
		name := top.Name()
		base := filepath.Join("/srv", name)
		owner := "root"
		if top.IsDir() && owners[name] {
			owner = name
		}
		batch := make([]string, 0, ChownBatch)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			args := append([]string{"--no-dereference", owner, "--"}, batch...)
			_, err := h.run(ctx, "chown", args...)
			batch = batch[:0]
			return err
		}
		var walk func(string, fs.DirEntry) error
		walk = func(path string, entry fs.DirEntry) error {
			info, err := entry.Info()
			if err != nil {
				return hostError(path, err)
			}
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && int64(stat.Uid) == uid {
				batch = append(batch, h.path(path))
				if len(batch) == ChownBatch {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			if entry.IsDir() {
				return h.visitChildren(path, func(child fs.DirEntry) error {
					return walk(filepath.Join(path, child.Name()), child)
				})
			}
			return nil
		}
		if err := walk(base, top); err != nil {
			return err
		}
		return flush()
	})
}

func (h *Host) secure(ctx context.Context, path string) error {
	info, err := os.Lstat(h.path(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return hostError(path, err)
	}
	if !ownerIs(info, 0, 0) {
		if _, err := h.run(ctx, "chown", "--no-dereference", "root:root", h.path(path)); err != nil {
			return err
		}
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	mask := fs.FileMode(0o022)
	if path == HostKeyDir || path == filepath.Join(HostKeyDir, "ssh_host_ed25519_key") {
		mask = 0o077
	}
	return h.mode(path, (info.Mode()&permissionBits)&^mask)
}

func (h *Host) isolation(ctx context.Context, s protocol.DesiredState) error {
	for _, path := range []string{DaemonPath, "/etc/accountsd.env", SessionHelper, HostKeyDir, filepath.Join(HostKeyDir, "ssh_host_ed25519_key"), filepath.Join(HostKeyDir, "ssh_host_ed25519_key.pub"), KeysDir} {
		if err := h.secure(ctx, path); err != nil {
			return err
		}
	}
	for _, path := range slices.Sorted(maps.Keys(h.written)) {
		if err := h.secure(ctx, path); err != nil {
			return err
		}
	}
	if err := h.visitChildren(KeysDir, func(entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		path := filepath.Join(KeysDir, entry.Name())
		if _, tracked := h.written[path]; tracked || slices.Contains(h.backups, h.path(path)) {
			return nil
		}
		return h.secure(ctx, path)
	}); err != nil {
		return err
	}
	for _, a := range s.Accounts {
		if a.State == protocol.AccountActive {
			if err := h.mode(filepath.Join("/srv", a.Slug), accountMode); err != nil {
				return err
			}
		}
	}
	for _, u := range s.Users {
		if u.State == protocol.UserActive || u.State == protocol.UserLocked {
			exists, err := h.hasAccount(u.Name)
			if err != nil {
				return err
			}
			if exists {
				if err := h.optionalMode(filepath.Join("/home", u.Name), 0o700); err != nil {
					return err
				}
			}
		}
	}
	return h.removeOtherGroups(ctx, s.Users)
}

func (h *Host) removeOtherGroups(ctx context.Context, users []protocol.User) error {
	var commandErr error
	err := h.groups(func(group string, _ int64, members []string) {
		if group == ShellGroup || commandErr != nil {
			return
		}
		for _, user := range users {
			if group == user.Account || group == user.Name {
				continue
			}
			for _, member := range members {
				if member == user.Name {
					_, commandErr = h.run(ctx, "gpasswd", "--delete", user.Name, group)
					if commandErr != nil {
						return
					}
					break
				}
			}
		}
	})
	if commandErr != nil {
		return commandErr
	}
	return err
}
