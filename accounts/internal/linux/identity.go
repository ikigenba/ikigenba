package linux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

const (
	passwdPath = "/etc/passwd"
	groupPath  = "/etc/group"
	shadowPath = "/etc/shadow"
)

var volumeID = regexp.MustCompile(`^vol-[0-9a-f]{8,17}$`)
var backupID = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func validBackupID(id string) bool { return backupID.MatchString(id) }

func validateState(s protocol.DesiredState) error {
	if b := s.Backup; b != nil {
		if (b.Close != "" && !validBackupID(b.Close)) || (b.Restore != nil && !validBackupID(b.Restore.ID)) {
			return machine.ErrInvalidState
		}
	}
	if s.DataVolume != "" && !volumeID.MatchString(s.DataVolume) {
		return machine.ErrInvalidState
	}
	if len(s.Allowlist) > protocol.MaxAllowlist || len(s.Unban) > protocol.MaxUnbans {
		return machine.ErrInvalidState
	}
	for _, entry := range s.Allowlist {
		if canonical, err := valid.AllowEntry(entry); err != nil || canonical != entry {
			return machine.ErrInvalidState
		}
	}
	for _, address := range s.Unban {
		if canonical, err := valid.Address(address); err != nil || canonical != address {
			return machine.ErrInvalidState
		}
	}
	ids := make(map[int64]bool)
	accounts := make(map[string]bool)
	for _, a := range s.Accounts {
		if valid.Slug(a.Slug) != nil || a.GID < 1000 || ids[a.GID] {
			return machine.ErrInvalidState
		}
		ids[a.GID], accounts[a.Slug] = true, true
	}
	for _, u := range s.Users {
		if valid.Name(u.Name) != nil || u.UID < 1000 || ids[u.UID] || (u.State != protocol.UserDeleted && !accounts[u.Account]) {
			return machine.ErrInvalidState
		}
		ids[u.UID] = true
		for _, key := range u.Keys {
			if _, err := valid.ParseKey(key); err != nil || strings.ContainsAny(key, "\r\n") {
				return machine.ErrInvalidState
			}
		}
	}
	return nil
}

const closeLockPath = BackupDir + "/status/locked"

func (h *Host) recordCloseLock(s protocol.DesiredState) error {
	if s.Backup == nil || s.Backup.Close == "" {
		return nil
	}
	prefix := fmt.Sprintf("%s %d ", s.Backup.Close, s.Version)
	current, err := h.readBounded(closeLockPath)
	if err == nil && strings.HasPrefix(string(current), prefix) {
		return nil
	}
	record := prefix + h.env.Now().UTC().Format(time.RFC3339) + "\n"
	_, err = h.writeAtomic(closeLockPath, []byte(record), 0o600)
	return err
}

func (h *Host) groups(visit func(name string, gid int64, members []string)) error {
	return h.scan(groupPath, func(line string) {
		fields := strings.SplitN(line, ":", 4)
		if len(fields) != 4 {
			return
		}
		if gid, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			visit(fields[0], gid, strings.Split(fields[3], ","))
		}
	})
}

func (h *Host) passwd(visit func(name string, uid, gid int64, home string)) error {
	return h.scan(passwdPath, func(line string) {
		fields := strings.SplitN(line, ":", 7)
		if len(fields) != 7 {
			return
		}
		uid, uidErr := strconv.ParseInt(fields[2], 10, 64)
		gid, gidErr := strconv.ParseInt(fields[3], 10, 64)
		if uidErr == nil && gidErr == nil {
			visit(fields[0], uid, gid, fields[5])
		}
	})
}

func (h *Host) expired(visit func(string, bool)) error {
	return h.scan(shadowPath, func(line string) {
		fields := strings.SplitN(line, ":", 9)
		if len(fields) >= 8 {
			visit(fields[0], fields[7] != "")
		}
	})
}

func (h *Host) locks(ctx context.Context, s protocol.DesiredState) error {
	listed := make(map[string]bool, len(s.Accounts)+len(s.Users))
	for _, a := range s.Accounts {
		listed[a.Slug] = true
	}
	targets := make(map[string]struct{})
	for _, u := range s.Users {
		listed[u.Name] = true
		if u.State == protocol.UserLocked || u.State == protocol.UserDeleted {
			targets[u.Name] = struct{}{}
		}
	}
	if err := h.groups(func(name string, _ int64, members []string) {
		if name != ShellGroup {
			return
		}
		for _, member := range members {
			if member != "" && !listed[member] {
				targets[member] = struct{}{}
			}
		}
	}); err != nil {
		return err
	}
	type login struct {
		name string
		uid  int64
	}
	var users []login
	if err := h.passwd(func(name string, uid, _ int64, _ string) {
		if _, found := targets[name]; found {
			users = append(users, login{name: name, uid: uid})
		}
	}); err != nil {
		return err
	}
	expired := make(map[string]bool)
	if err := h.expired(func(name string, ended bool) {
		if _, found := targets[name]; found {
			expired[name] = ended
		}
	}); err != nil {
		return err
	}
	for _, user := range users {
		ended, hasShadow := expired[user.name]
		if hasShadow && !ended {
			if _, err := h.run(ctx, "usermod", "--expiredate", "1", user.name); err != nil {
				return err
			}
		}
		if err := h.unitFiles(func(unit string) error {
			if !isPersonSlice(unit, user.name) {
				return nil
			}
			return h.stopActiveSlice(ctx, unit)
		}); err != nil {
			return err
		}
		if hasShadow {
			if err := h.stopActiveSlice(ctx, "user-"+strconv.FormatInt(user.uid, 10)+".slice"); err != nil {
				return err
			}
		}
	}
	return h.recordCloseLock(s)
}

func (h *Host) stopActiveSlice(ctx context.Context, unit string) error {
	active, err := h.unitStatus(ctx, "is-active", unit)
	if err == nil && active {
		_, err = h.run(ctx, "systemctl", "stop", unit)
	}
	return err
}

type identity struct {
	group, account          bool
	groupOfAccount, inShell bool
}

func (h *Host) identity(name string, id int64, account string) (identity, error) {
	var local identity
	var conflict bool
	if err := h.groups(func(group string, gid int64, members []string) {
		if group == name {
			local.group = true
			conflict = conflict || gid != id
		} else {
			conflict = conflict || gid == id
		}
		if group == ShellGroup && slices.Contains(members, name) {
			local.inShell = true
		}
		if group == account && slices.Contains(members, name) {
			local.groupOfAccount = true
		}
	}); err != nil {
		return identity{}, err
	}
	if err := h.passwd(func(user string, uid, gid int64, _ string) {
		if user == name {
			local.account = true
			conflict = conflict || uid != id || gid != id
		} else {
			conflict = conflict || uid == id
		}
	}); err != nil {
		return identity{}, err
	}
	if conflict {
		return identity{}, fmt.Errorf("%s: %w", name, machine.ErrIdentity)
	}
	return local, nil
}

func (h *Host) hasAccount(name string) (bool, error) {
	var found bool
	err := h.passwd(func(user string, _, _ int64, _ string) {
		found = found || user == name
	})
	return found, err
}

func (h *Host) dropGroup(ctx context.Context, name string) error {
	var found bool
	if err := h.groups(func(group string, _ int64, _ []string) {
		found = found || group == name
	}); err != nil {
		return err
	}
	if !found {
		return nil
	}
	_, err := h.run(ctx, "groupdel", name)
	return err
}

func (h *Host) removedAccounts(ctx context.Context, s protocol.DesiredState) error {
	for _, a := range s.Accounts {
		if a.State != protocol.AccountRemoved {
			continue
		}
		exists, err := h.hasAccount(a.Slug)
		if err != nil {
			return err
		}
		if exists {
			if _, err := h.run(ctx, "userdel", a.Slug); err != nil {
				return err
			}
		}
		if err := h.dropGroup(ctx, a.Slug); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) users(ctx context.Context, s protocol.DesiredState) error {
	var recipients map[string]bool
	for _, u := range s.Users {
		switch u.State {
		case protocol.UserActive, protocol.UserLocked:
			if err := h.ensureUser(ctx, u); err != nil {
				return err
			}
		case protocol.UserDeleted:
			if recipients == nil {
				recipients = make(map[string]bool, len(s.Accounts))
				for _, a := range s.Accounts {
					recipients[a.Slug] = true
				}
			}
			if err := h.deleteUser(ctx, u, recipients); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Host) ensureUser(ctx context.Context, u protocol.User) error {
	local, err := h.identity(u.Name, u.UID, u.Account)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(u.UID, 10)
	groups := ShellGroup + "," + u.Account
	if !local.group {
		if _, err := h.run(ctx, "groupadd", "--gid", id, u.Name); err != nil {
			return err
		}
	}
	if !local.account {
		args := []string{"--uid", id, "--gid", u.Name, "--groups", groups, "--create-home",
			"--home-dir", h.path(filepath.Join("/home", u.Name)), "--skel", h.path(SkelDir), "--key", "HOME_MODE=0700", "--shell", "/bin/bash"}
		if u.State == protocol.UserLocked {
			args = append(args, "--expiredate", "1")
		}
		if _, err := h.run(ctx, "useradd", append(args, u.Name)...); err != nil {
			return err
		}
	} else if !local.inShell || !local.groupOfAccount {
		if _, err := h.run(ctx, "usermod", "--append", "--groups", groups, u.Name); err != nil {
			return err
		}
	}
	var keys strings.Builder
	for _, key := range u.Keys {
		keys.WriteString(key)
		keys.WriteByte('\n')
	}
	if _, err := h.write(filepath.Join(KeysDir, u.Name), []byte(keys.String()), 0o644); err != nil {
		return err
	}
	if u.State == protocol.UserActive {
		var expires bool
		if err := h.expired(func(name string, ended bool) {
			if name == u.Name {
				expires = ended
			}
		}); err != nil {
			return err
		}
		if expires {
			_, err := h.run(ctx, "usermod", "--expiredate", "", u.Name)
			return err
		}
	}
	return nil
}

func (h *Host) deleteUser(ctx context.Context, u protocol.User, recipients map[string]bool) error {
	exists, err := h.hasAccount(u.Name)
	if err != nil {
		return err
	}
	if exists {
		if err := h.reassignFiles(ctx, u.UID, recipients); err != nil {
			return err
		}
	}
	if _, err := h.remove(filepath.Join(KeysDir, u.Name)); err != nil || !exists {
		return err
	}
	if _, err := h.run(ctx, "userdel", "--remove", u.Name); err != nil {
		return err
	}
	return h.dropGroup(ctx, u.Name)
}

func (h *Host) optionalMode(path string, mode fs.FileMode) error {
	err := h.mode(path, mode)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
