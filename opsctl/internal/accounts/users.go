package accounts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// User states reflect account expiration, independently of the password hash.
const (
	StateActive = "active"
	StateLocked = "locked"
)

// User is the host's SSH user and their access details.
type User struct {
	Name       string     `json:"name"`
	Groups     []string   `json:"groups"`
	Keys       []string   `json:"keys"`
	State      string     `json:"state"`
	LastSignIn *time.Time `json:"last_sign_in"`
}

// UserList is the ordered inventory of SSH users.
type UserList struct {
	Users []User `json:"users"`
}

func userDB(env host.Env, names ...string) (accountDB, error) {
	if err := validateNames(names...); err != nil {
		return accountDB{}, err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return db, err
	}
	if !db.user(names[0]) {
		return db, &RefusedError{Reason: "no user '" + names[0] + "'"}
	}
	return db, nil
}

func validKey(key Key) error { _, err := ParseKey(key.Line()); return err }

// AddUser creates an SSH user with one initial key and business membership.
func AddUser(ctx context.Context, env host.Env, name, group string, key Key) error {
	if err := validateNames(name, group); err != nil {
		return err
	}
	if err := validKey(key); err != nil {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	if db.taken(name) {
		return &RefusedError{Reason: "name taken: " + name}
	}
	if !db.businessGroup(group) {
		return &RefusedError{Reason: "no group '" + group + "'"}
	}
	home := rootPath(env.Root, "/home/"+name)
	_, homeErr := os.Lstat(home)
	if homeErr != nil && !errors.Is(homeErr, os.ErrNotExist) {
		return homeErr
	}
	homeExisted := homeErr == nil
	path := keyPath(env.Root, name)
	keyFile, err := os.OpenInRoot(env.Root, strings.TrimPrefix(hardening.KeysDir+"/"+name, "/"))
	var oldKey []byte
	if err == nil {
		oldKey, err = io.ReadAll(keyFile)
		closeErr := keyFile.Close()
		if err == nil {
			err = closeErr
		}
	}
	keyExisted := err == nil
	var oldMode os.FileMode
	if keyExisted {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return statErr
		}
		oldMode = info.Mode()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	skel, err := os.MkdirTemp(env.Root, ".opsctl-skel-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(skel) }()
	_, err = execute(ctx, env, host.Command{Name: "useradd", Args: []string{"--create-home", "--home-dir", "/home/" + name, "--skel", skel, "--key", "HOME_MODE=0700", "--user-group", "--groups", hardening.ShellGroup + "," + group, "--shell", "/bin/bash", name}})
	if err == nil {
		err = writeKeys(env.Root, name, []string{key.Line()})
	}
	if err == nil {
		return nil
	}
	undoAddUser(ctx, env, name, home, homeExisted, path, keyExisted, oldKey, oldMode)
	return err
}

func undoAddUser(ctx context.Context, env host.Env, name, home string, homeExisted bool, path string, keyExisted bool, oldKey []byte, oldMode os.FileMode) {
	if db, err := loadAccounts(env.Root); err == nil {
		if _, exists := db.accounts[name]; exists {
			args := []string{name}
			if !homeExisted {
				args = []string{"--remove", name}
			}
			_, _ = execute(ctx, env, host.Command{Name: "userdel", Args: args})
		}
	}
	if groups, err := loadGroups(env.Root); err == nil {
		if _, exists := groups[name]; exists {
			_, _ = execute(ctx, env, host.Command{Name: "groupdel", Args: []string{name}})
		}
	}
	if !homeExisted {
		_ = os.RemoveAll(home)
	}
	if keyExisted {
		_ = writeKeyBytes(path, oldKey, oldMode)
	} else {
		_ = os.Remove(path)
	}
}

// ListUsers reads the host's SSH users and last-login records.
func ListUsers(ctx context.Context, env host.Env) (UserList, error) {
	result := UserList{Users: []User{}}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return result, err
	}
	states, err := readStates(env)
	if err != nil {
		return result, err
	}
	names := []string{}
	for name := range db.accounts {
		if db.user(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		groups := []string{}
		for group, entry := range db.groups {
			if db.businessGroup(group) && slices.Contains(entry.members, name) {
				groups = append(groups, group)
			}
		}
		sort.Strings(groups)
		keys, keyErr := readKeys(env.Root, name)
		if keyErr != nil {
			return result, keyErr
		}
		command, execErr := execute(ctx, env, host.Command{Name: "lastlog", Args: []string{"-u", name}, Env: []string{"LC_ALL=C"}})
		if execErr != nil {
			return result, execErr
		}
		last, parseErr := parseLastLog(command.Stdout)
		if parseErr != nil {
			return result, parseErr
		}
		result.Users = append(result.Users, User{Name: name, Groups: groups, Keys: keys, State: userState(states, name), LastSignIn: last})
	}
	return result, nil
}

func readStates(env host.Env) (map[string]bool, error) {
	entries, err := readEntries(rootPath(env.Root, "/etc/shadow"), 9)
	if err != nil {
		return nil, err
	}
	states := map[string]bool{}
	today := env.Now().UTC().Unix() / 86400
	for _, entry := range entries {
		day, parseErr := strconv.ParseInt(entry[7], 10, 64)
		states[entry[0]] = parseErr == nil && day >= 1 && day <= today
	}
	return states, nil
}

func userState(states map[string]bool, name string) string {
	if states[name] {
		return StateLocked
	}
	return StateActive
}

func parseLastLog(data []byte) (*time.Time, error) {
	last := ""
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			last = line
		}
	}
	if strings.HasSuffix(last, "**Never logged in**") {
		return nil, nil
	}
	fields := strings.FieldsFunc(last, func(r rune) bool { return r == ' ' })
	if len(fields) < 6 {
		return nil, errors.New("invalid lastlog output")
	}
	parsed, err := time.Parse("Mon Jan 2 15:04:05 -0700 2006", strings.Join(fields[len(fields)-6:], " "))
	if err != nil {
		return nil, fmt.Errorf("invalid lastlog output: %w", err)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

// AddKey gives an SSH user a key, keeping any held label on a matching key.
func AddKey(ctx context.Context, env host.Env, name string, key Key) error {
	return changeKey(ctx, env, name, key, true)
}

// RemoveKey removes every occurrence of a key from an SSH user.
func RemoveKey(ctx context.Context, env host.Env, name string, key Key) error {
	return changeKey(ctx, env, name, key, false)
}

func changeKey(_ context.Context, env host.Env, name string, key Key, add bool) error {
	if err := validateNames(name); err != nil {
		return err
	}
	if err := validKey(key); err != nil {
		return err
	}
	if _, err := userDB(env, name); err != nil {
		return err
	}
	lines, err := readKeys(env.Root, name)
	if err != nil {
		return err
	}
	kept := []string{}
	matched := false
	for _, line := range lines {
		if keyMatches(line, key) {
			matched = true
			if !add {
				continue
			}
		}
		kept = append(kept, line)
	}
	if add && matched || !add && !matched {
		return nil
	}
	if add {
		kept = append(kept, key.Line())
	}
	return writeKeys(env.Root, name, kept)
}

// JoinGroup adds an SSH user to a business group.
func JoinGroup(ctx context.Context, env host.Env, name, group string) error {
	return changeMembership(ctx, env, name, group, true)
}

// LeaveGroup removes an SSH user from a business group.
func LeaveGroup(ctx context.Context, env host.Env, name, group string) error {
	return changeMembership(ctx, env, name, group, false)
}

func changeMembership(ctx context.Context, env host.Env, name, group string, join bool) error {
	db, err := userDB(env, name, group)
	if err != nil {
		return err
	}
	if !db.businessGroup(group) {
		return &RefusedError{Reason: "no group '" + group + "'"}
	}
	if slices.Contains(db.groups[group].members, name) == join {
		return nil
	}
	action := "--delete"
	if join {
		action = "--add"
	}
	_, err = execute(ctx, env, host.Command{Name: "gpasswd", Args: []string{action, name, group}})
	return err
}

// LockUser expires the account and terminates any remaining sessions.
func LockUser(ctx context.Context, env host.Env, name string) error {
	if _, err := userDB(env, name); err != nil {
		return err
	}
	states, err := readStates(env)
	if err != nil {
		return err
	}
	if !states[name] {
		if _, err = execute(ctx, env, host.Command{Name: "usermod", Args: []string{"--lock", "--expiredate", "1", name}}); err != nil {
			return err
		}
	}
	result, err := execute(ctx, env, host.Command{Name: "loginctl", Args: []string{"list-users", "--no-legend"}})
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			_, err = execute(ctx, env, host.Command{Name: "loginctl", Args: []string{"terminate-user", name}})
			return err
		}
	}
	return nil
}

// UnlockUser clears account expiration and the password lock when locked.
func UnlockUser(ctx context.Context, env host.Env, name string) error {
	if _, err := userDB(env, name); err != nil {
		return err
	}
	states, err := readStates(env)
	if err != nil {
		return err
	}
	if !states[name] {
		return nil
	}
	_, err = execute(ctx, env, host.Command{Name: "usermod", Args: []string{"--unlock", "--expiredate", "", name}})
	return err
}

// DeleteUser reassigns shared files, then removes a locked SSH user's account.
func DeleteUser(ctx context.Context, env host.Env, name string) error {
	db, err := userDB(env, name)
	if err != nil {
		return err
	}
	states, err := readStates(env)
	if err != nil {
		return err
	}
	if !states[name] {
		return &RefusedError{Reason: "user not locked: " + name}
	}
	if err = reassignFiles(ctx, env, db, name); err != nil {
		return err
	}
	if err = os.Remove(keyPath(env.Root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = execute(ctx, env, host.Command{Name: "userdel", Args: []string{"--remove", name}}); err != nil {
		return err
	}
	groups, err := loadGroups(env.Root)
	if err != nil {
		return err
	}
	if group, ok := groups[name]; ok && sameID(group.gid, db.accounts[name].gid) {
		_, err = execute(ctx, env, host.Command{Name: "groupdel", Args: []string{name}})
	}
	return err
}

func sameID(a, b string) bool {
	x, e := strconv.ParseUint(a, 10, 32)
	y, f := strconv.ParseUint(b, 10, 32)
	return e == nil && f == nil && x == y
}

func reassignFiles(ctx context.Context, env host.Env, db accountDB, name string) error {
	dir := rootPath(env.Root, SrvDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(db.accounts[name].uid, 10, 32)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		owner := "root"
		if db.businessGroup(entry.Name()) {
			owner = entry.Name()
		}
		paths := []string{}
		err = filepath.WalkDir(filepath.Join(dir, entry.Name()), func(path string, _ fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return errors.New("file owner unavailable")
			}
			if uint64(stat.Uid) == uid {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(paths) != 0 {
			if _, err = execute(ctx, env, host.Command{Name: "chown", Args: append([]string{"--no-dereference", owner, "--"}, paths...)}); err != nil {
				return err
			}
		}
	}
	return nil
}
