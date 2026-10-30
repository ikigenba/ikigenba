// Package accounts manages the host's business groups and SSH users.
package accounts

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// SrvDir holds every business group's shared directory.
const SrvDir = "/srv"

// RefusedError describes a request the host's state forbids.
type RefusedError struct{ Reason string }

func (e *RefusedError) Error() string { return e.Reason }

// ValidName reports whether name is a supported Linux account name.
func ValidName(name string) bool {
	if len(name) < 1 || len(name) > 32 {
		return false
	}
	for i, c := range []byte(name) {
		if c >= 'a' && c <= 'z' || c == '_' || i > 0 && (c >= '0' && c <= '9' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func validateNames(names ...string) error {
	for _, name := range names {
		if !ValidName(name) {
			return fmt.Errorf("invalid name: %s", name)
		}
	}
	return nil
}

type account struct{ name, password, uid, gid, comment, home, shell string }
type group struct {
	name, password, gid string
	members             []string
}
type accountDB struct {
	accounts map[string]account
	groups   map[string]group
}

func rootPath(root, path string) string { return filepath.Join(root, path) }

func readEntries(path string, fields int) ([][]string, error) {
	file, err := os.OpenInRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	entries := make([][]string, 0)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != fields {
			return nil, fmt.Errorf("invalid account file: %s", path)
		}
		entries = append(entries, parts)
	}
	return entries, nil
}

func loadGroups(root string) (map[string]group, error) {
	entries, err := readEntries(rootPath(root, "/etc/group"), 4)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]group)
	for _, f := range entries {
		members := make([]string, 0)
		if f[3] != "" {
			members = strings.Split(f[3], ",")
		}
		groups[f[0]] = group{name: f[0], password: f[1], gid: f[2], members: members}
	}
	return groups, nil
}

func loadAccounts(root string) (accountDB, error) {
	db := accountDB{accounts: make(map[string]account)}
	entries, err := readEntries(rootPath(root, "/etc/passwd"), 7)
	if err != nil {
		return db, err
	}
	db.groups, err = loadGroups(root)
	if err != nil {
		return db, err
	}
	for _, f := range entries {
		db.accounts[f[0]] = account{f[0], f[1], f[2], f[3], f[4], f[5], f[6]}
	}
	return db, nil
}
func (db accountDB) taken(name string) bool {
	_, a := db.accounts[name]
	_, g := db.groups[name]
	return a || g
}
func (db accountDB) businessGroup(name string) bool {
	a, ok := db.accounts[name]
	_, g := db.groups[name]
	return ok && g && a.home == SrvDir+"/"+name
}
func (db accountDB) user(name string) bool {
	_, ok := db.accounts[name]
	return ok && slices.Contains(db.groups[hardening.ShellGroup].members, name)
}
func (db accountDB) members(name string) []string {
	members := make([]string, 0)
	for _, name := range db.groups[name].members {
		if db.user(name) && !slices.Contains(members, name) {
			members = append(members, name)
		}
	}
	slices.Sort(members)
	return members
}
func execute(ctx context.Context, env host.Env, command host.Command) (host.Result, error) {
	var result host.Result
	var err error
	if env.Execute == nil {
		err = fmt.Errorf("host executor is not configured")
	} else {
		result, err = env.Execute(ctx, command)
	}
	if err != nil || result.ExitCode != 0 {
		label := command.Name
		if label == "runuser" {
			label = "git"
		}
		return result, &host.CommandError{Label: label, Result: result, Err: err}
	}
	return result, nil
}
