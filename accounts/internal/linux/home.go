package linux

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func (h *Host) populateHome(ctx context.Context, user protocol.User, home string) error {
	source := templateFiles(h.path(filepath.Join(CloneDir, "home")))
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		target := filepath.Join(home, name)
		if err != nil {
			return hostError(target, err)
		}
		if name == "." {
			return nil
		}
		full := h.path(target)
		if _, err := os.Lstat(full); !errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				return hostError(target, err)
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			_, err := h.run(ctx, "runuser", "-u", user.Name, "--", "mkdir", "-m", "0755", full)
			return err
		}
		body, err := source.Open(name)
		if err != nil {
			return hostError(target, err)
		}
		defer func() { _ = body.Close() }()
		if err := h.teeAs(ctx, user.Name, full, machine.FillHome(body, user.Name, user.Account)); err != nil {
			return err
		}
		_, err = h.run(ctx, "runuser", "-u", user.Name, "--", "chmod", "0644", full)
		return err
	})
}
