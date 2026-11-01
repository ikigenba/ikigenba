package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

type userTimerStep struct {
	uid       string
	lingering bool
}

func userTimerInstalled(env host.Env) (bool, error) {
	_, err := os.Stat(rootPath(env.Root, filepath.Join(release.UserUnitDir, release.TurnsTimer)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func enableUserTimer(ctx context.Context, env host.Env, name string) (userTimerStep, error) {
	step := userTimerStep{}
	entries, err := readEntries(rootPath(env.Root, "/etc/passwd"), 7)
	if err != nil {
		return step, err
	}
	for _, entry := range entries {
		if entry[0] == name {
			uid, parseErr := strconv.ParseUint(entry[2], 10, 32)
			if parseErr != nil {
				return step, parseErr
			}
			step.uid = strconv.FormatUint(uid, 10)
			break
		}
	}
	if step.uid == "" {
		return step, fmt.Errorf("no passwd entry for %s", name)
	}
	if _, err = execute(ctx, env, host.Command{Name: "loginctl", Args: []string{"enable-linger", name}}); err != nil {
		return step, err
	}
	step.lingering = true
	if _, err = execute(ctx, env, host.Command{Name: "systemctl", Args: []string{"start", "user@" + step.uid + ".service"}}); err != nil {
		return step, err
	}
	_, err = execute(ctx, env, host.Command{Name: "systemctl", Args: []string{"--user", "--machine=" + name + "@.host", "enable", "--now", release.TurnsTimer}})
	return step, err
}
