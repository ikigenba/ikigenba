package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

// EnsureTimers gives active SSH users any lingering and timer setup they lack.
func EnsureTimers(ctx context.Context, env host.Env) error {
	installed, err := userTimerInstalled(env)
	if err != nil || !installed {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	states, err := readStates(env)
	if err != nil {
		return err
	}
	names := []string{}
	for name, a := range db.accounts {
		if !db.user(name) || states[name] {
			continue
		}
		_, lingerErr := os.Lstat(rootPath(env.Root, "/var/lib/systemd/linger/"+name))
		if lingerErr != nil && !errors.Is(lingerErr, os.ErrNotExist) {
			return lingerErr
		}
		timer, timerErr := os.Lstat(rootPath(env.Root, filepath.Join(a.home, ".config/systemd/user/timers.target.wants", release.TurnsTimer)))
		if timerErr != nil && !errors.Is(timerErr, os.ErrNotExist) {
			return timerErr
		}
		if lingerErr != nil || timerErr != nil || timer.Mode()&os.ModeSymlink == 0 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if _, err := enableUserTimer(ctx, env, name); err != nil {
			return err
		}
	}
	return nil
}
