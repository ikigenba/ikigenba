package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// GroupCount is one business group's member count.
type GroupCount struct {
	Slug    string `json:"slug"`
	Members int    `json:"members"`
}

// GroupList holds the business groups in name order.
type GroupList struct {
	Groups []GroupCount `json:"groups"`
}

// MemberList holds the users in one business group.
type MemberList struct {
	Group   string   `json:"group"`
	Members []string `json:"members"`
}

func groupRefusal(slug string) error { return &RefusedError{Reason: "no group '" + slug + "'"} }

// ListGroups reads the business groups and their member counts.
func ListGroups(_ context.Context, env host.Env) (GroupList, error) {
	result := GroupList{Groups: make([]GroupCount, 0)}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return result, err
	}
	for name := range db.groups {
		if db.businessGroup(name) {
			result.Groups = append(result.Groups, GroupCount{Slug: name, Members: len(db.members(name))})
		}
	}
	slices.SortFunc(result.Groups, func(a, b GroupCount) int { return strings.Compare(a.Slug, b.Slug) })
	return result, nil
}

// GroupMembers reads the distinct users belonging to a business group.
func GroupMembers(_ context.Context, env host.Env, slug string) (MemberList, error) {
	result := MemberList{Group: slug, Members: make([]string, 0)}
	if err := validateNames(slug); err != nil {
		return result, err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return result, err
	}
	if !db.businessGroup(slug) {
		return result, groupRefusal(slug)
	}
	result.Members = db.members(slug)
	return result, nil
}

// AddGroup creates a group and its service account, adopting any kept folder.
func AddGroup(ctx context.Context, env host.Env, slug string) error {
	if err := validateNames(slug); err != nil {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	if db.taken(slug) {
		return &RefusedError{Reason: "name taken: " + slug}
	}
	dir := rootPath(env.Root, SrvDir+"/"+slug)
	info, err := os.Lstat(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	exists := err == nil
	if exists && !info.IsDir() {
		return &RefusedError{Reason: "not a directory: " + SrvDir + "/" + slug}
	}
	groupArgs := []string{"--system"}
	userArgs := []string{"--system"}
	if exists {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot read ownership: %s", dir)
		}
		gidUsed, uidUsed := false, false
		for _, g := range db.groups {
			if numericID(g.gid) == uint64(stat.Gid) {
				gidUsed = true
			}
		}
		for _, a := range db.accounts {
			if numericID(a.uid) == uint64(stat.Uid) {
				uidUsed = true
			}
		}
		if !gidUsed {
			groupArgs = append(groupArgs, "--gid", strconv.FormatUint(uint64(stat.Gid), 10))
		}
		if !uidUsed {
			userArgs = append(userArgs, "--uid", strconv.FormatUint(uint64(stat.Uid), 10))
		}
	}
	groupArgs = append(groupArgs, slug)
	userArgs = append(userArgs, "--gid", slug, "--home-dir", SrvDir+"/"+slug, "--no-create-home", "--shell", "/usr/sbin/nologin", slug)
	if _, err = execute(ctx, env, host.Command{Name: "groupadd", Args: groupArgs}); err != nil {
		return err
	}
	userMade := false
	undo := func() {
		if userMade {
			_, _ = execute(ctx, env, host.Command{Name: "userdel", Args: []string{slug}})
		}
		groups, readErr := loadGroups(env.Root)
		if readErr == nil {
			if _, ok := groups[slug]; ok {
				_, _ = execute(ctx, env, host.Command{Name: "groupdel", Args: []string{slug}})
			}
		}
	}
	if _, err = execute(ctx, env, host.Command{Name: "useradd", Args: userArgs}); err != nil {
		undo()
		return err
	}
	userMade = true
	if !exists {
		if err = os.MkdirAll(dir, 0700); err != nil {
			undo()
			return err
		}
	}
	commands := []host.Command{
		{Name: "chown", Args: []string{slug + ":" + slug, dir}},
		{Name: "chmod", Args: []string{"2770", dir}},
		{Name: "setfacl", Args: []string{"-d", "-m", "g::rwx,o::-", dir}},
	}
	for _, command := range commands {
		if _, err = execute(ctx, env, command); err != nil {
			undo()
			return err
		}
	}
	if _, statErr := os.Lstat(dir + "/.git"); os.IsNotExist(statErr) {
		if _, err = execute(ctx, env, host.Command{Name: "runuser", Args: []string{"-u", slug, "--", "git", "init", "--shared=group", dir}, Dir: dir}); err != nil {
			undo()
			return err
		}
	} else if statErr != nil {
		undo()
		return statErr
	}
	if err = trustDirectory(ctx, env, slug); err != nil {
		undo()
		return err
	}
	return nil
}

func numericID(value string) uint64 {
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return ^uint64(0)
	}
	return id
}

func trustDirectory(ctx context.Context, env host.Env, slug string) error {
	args := []string{"config", "--file", rootPath(env.Root, "/etc/gitconfig")}
	result, err := execute(ctx, env, host.Command{Name: "git", Args: append(slices.Clone(args), "--get-all", "safe.directory")})
	if err != nil {
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) || commandErr.Err != nil || result.ExitCode != 1 {
			return err
		}
	}
	value := SrvDir + "/" + slug
	if result.ExitCode == 0 && slices.Contains(strings.Split(string(result.Stdout), "\n"), value) {
		return nil
	}
	_, err = execute(ctx, env, host.Command{Name: "git", Args: append(args, "--add", "safe.directory", value)})
	return err
}

func leftover(db accountDB, env host.Env, slug string) bool {
	if slug == hardening.ShellGroup {
		return false
	}
	group, ok := db.groups[slug]
	if !ok || len(group.members) != 0 {
		return false
	}
	if _, ok = db.accounts[slug]; ok {
		return false
	}
	info, err := os.Lstat(rootPath(env.Root, SrvDir+"/"+slug))
	if err != nil || !info.IsDir() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && numericID(group.gid) == uint64(stat.Gid)
}

// RemoveGroup removes an empty business group or finishes its partial removal.
func RemoveGroup(ctx context.Context, env host.Env, slug string) error {
	if err := validateNames(slug); err != nil {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	if !db.businessGroup(slug) {
		if !leftover(db, env, slug) {
			return groupRefusal(slug)
		}
		_, err = execute(ctx, env, host.Command{Name: "groupdel", Args: []string{slug}})
		return err
	}
	if len(db.members(slug)) != 0 {
		return &RefusedError{Reason: "group not empty: " + slug}
	}
	if _, err = execute(ctx, env, host.Command{Name: "userdel", Args: []string{slug}}); err != nil {
		return err
	}
	groups, err := loadGroups(env.Root)
	if err != nil {
		return err
	}
	if _, ok := groups[slug]; ok {
		_, err = execute(ctx, env, host.Command{Name: "groupdel", Args: []string{slug}})
	}
	return err
}
