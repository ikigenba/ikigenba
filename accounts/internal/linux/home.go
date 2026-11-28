package linux

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func (h *Host) populateHome(ctx context.Context, user protocol.User, home string) error {
	source := accounts.Home()
	return fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		target := filepath.Join(home, name)
		if err != nil {
			return hostError(target, err)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		full := h.path(target)
		if _, err := os.Lstat(full); !errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				return hostError(target, err)
			}
			return nil
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
