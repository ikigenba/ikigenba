package accounts

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// EnsureShellGroup creates the shared SSH group only when it is absent.
func EnsureShellGroup(ctx context.Context, env host.Env) error {
	result, err := execute(ctx, env, host.Command{Name: "getent", Args: []string{"group", hardening.ShellGroup}})
	if err == nil {
		return nil
	}
	var command *host.CommandError
	if !errors.As(err, &command) || command.Err != nil || result.ExitCode != 2 {
		return err
	}
	_, err = execute(ctx, env, host.Command{Name: "groupadd", Args: []string{"--system", hardening.ShellGroup}})
	return err
}

// RestoreGroup recreates one business group using the backup's numeric identity.
func RestoreGroup(ctx context.Context, env host.Env, g ManifestGroup) error {
	if err := validateManifestGroup(g); err != nil {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	if reason := identityCollision(db, g.Slug, g.UID, g.GID, true); reason != "" {
		return &RefusedError{Reason: reason}
	}
	if db.businessGroup(g.Slug) {
		return nil
	}
	dir := rootPath(env.Root, SrvDir+"/"+g.Slug)
	info, err := os.Lstat(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	exists := err == nil
	if exists && !info.IsDir() {
		return &RefusedError{Reason: "not a directory: " + SrvDir + "/" + g.Slug}
	}
	groupArgs := []string{"--system", "--gid", strconv.Itoa(g.GID), g.Slug}
	userArgs := []string{"--system", "--uid", strconv.Itoa(g.UID), "--gid", g.Slug, "--home-dir", SrvDir + "/" + g.Slug, "--no-create-home", "--shell", "/usr/sbin/nologin", g.Slug}
	return createGroup(ctx, env, g.Slug, dir, exists, groupArgs, userArgs)
}

// RestoreUser converges one restored SSH identity, memberships and lock state.
func RestoreUser(ctx context.Context, env host.Env, u ManifestUser) error {
	if err := validateManifestUser(u); err != nil {
		return err
	}
	if _, err := readStates(env); err != nil {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	if reason := identityCollision(db, u.Name, u.UID, u.GID, false); reason != "" {
		return &RefusedError{Reason: reason}
	}
	desired := slices.Clone(u.Groups)
	slices.Sort(desired)
	for _, g := range desired {
		if !db.businessGroup(g) {
			return groupRefusal(g)
		}
	}
	if !db.taken(u.Name) {
		if err = restoreNewUser(ctx, env, u, desired); err != nil {
			return err
		}
	} else {
		if err = restoreMemberships(ctx, env, db, u, desired); err != nil {
			return err
		}
	}
	if u.State == StateLocked {
		return LockUser(ctx, env, u.Name)
	}
	return UnlockUser(ctx, env, u.Name)
}

func restoreNewUser(ctx context.Context, env host.Env, u ManifestUser, groups []string) error {
	if _, err := execute(ctx, env, host.Command{Name: "groupadd", Args: []string{"--gid", strconv.Itoa(u.GID), u.Name}}); err != nil {
		return err
	}
	list := strings.Join(append([]string{hardening.ShellGroup}, groups...), ",")
	args := []string{"--uid", strconv.Itoa(u.UID), "--gid", u.Name, "--groups", list, "--home-dir", "/home/" + u.Name, "--no-create-home", "--shell", "/bin/bash"}
	if u.State == StateLocked {
		args = append(args, "--expiredate", "1970-01-02")
	}
	args = append(args, u.Name)
	_, err := execute(ctx, env, host.Command{Name: "useradd", Args: args})
	if err != nil {
		if db, readErr := loadAccounts(env.Root); readErr == nil {
			g, present := db.groups[u.Name]
			_, accountPresent := db.accounts[u.Name]
			if id, parseErr := manifestID(g.gid); present && parseErr == nil && id == u.GID && !accountPresent {
				_, _ = execute(ctx, env, host.Command{Name: "groupdel", Args: []string{u.Name}})
			}
		}
	}
	return err
}

func restoreMemberships(ctx context.Context, env host.Env, db accountDB, u ManifestUser, desired []string) error {
	removed := []string{}
	for name, g := range db.groups {
		if db.businessGroup(name) && slices.Contains(g.members, u.Name) && !slices.Contains(desired, name) {
			removed = append(removed, name)
		}
	}
	slices.Sort(removed)
	for _, g := range removed {
		if _, err := execute(ctx, env, host.Command{Name: "gpasswd", Args: []string{"--delete", u.Name, g}}); err != nil {
			return err
		}
	}
	for _, g := range desired {
		if !slices.Contains(db.groups[g].members, u.Name) {
			if _, err := execute(ctx, env, host.Command{Name: "gpasswd", Args: []string{"--add", u.Name, g}}); err != nil {
				return err
			}
		}
	}
	return nil
}
