package accounts_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type groupFixture struct {
	t        *testing.T
	root     string
	commands []host.Command
	hook     func(host.Command) (host.Result, error)
}

func newGroupFixture(t *testing.T, passwd, groups string) *groupFixture {
	t.Helper()
	f := &groupFixture{t: t, root: t.TempDir()}
	f.write("/etc/passwd", passwd)
	f.write("/etc/group", groups)
	return f
}
func (f *groupFixture) write(path, data string) {
	f.t.Helper()
	path = filepath.Join(f.root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	root, err := os.OpenRoot(f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	rel, err := filepath.Rel(f.root, path)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := root.WriteFile(rel, []byte(data), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *groupFixture) path(path string) string { return filepath.Join(f.root, path) }
func (f *groupFixture) env() host.Env {
	return host.Env{Root: f.root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		f.commands = append(f.commands, c)
		if f.hook != nil {
			return f.hook(c)
		}
		return host.Result{}, nil
	}}
}
func (f *groupFixture) accountMutation(c host.Command) {
	f.t.Helper()
	slug := c.Args[len(c.Args)-1]
	switch c.Name {
	case "groupadd":
		data, err := os.ReadFile(f.path("/etc/group"))
		if err != nil {
			f.t.Fatal(err)
		}
		f.write("/etc/group", string(data)+slug+":x:2000:\n")
	case "useradd":
		data, err := os.ReadFile(f.path("/etc/passwd"))
		if err != nil {
			f.t.Fatal(err)
		}
		f.write("/etc/passwd", string(data)+slug+":x:2000:2000::/srv/"+slug+":/usr/sbin/nologin\n")
	case "userdel", "groupdel":
		path := "/etc/passwd"
		if c.Name == "groupdel" {
			path = "/etc/group"
		}
		data, err := os.ReadFile(f.path(path))
		if err != nil {
			f.t.Fatal(err)
		}
		lines := make([]string, 0)
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" && !strings.HasPrefix(line, slug+":") {
				lines = append(lines, line)
			}
		}
		text := strings.Join(lines, "\n")
		if text != "" {
			text += "\n"
		}
		f.write(path, text)
	}
}
func callGroupOperation(ctx context.Context, env host.Env, action, slug string) error {
	switch action {
	case "add":
		return accounts.AddGroup(ctx, env, slug)
	case "remove":
		return accounts.RemoveGroup(ctx, env, slug)
	case "members":
		_, err := accounts.GroupMembers(ctx, env, slug)
		return err
	default:
		_, err := accounts.ListGroups(ctx, env)
		return err
	}
}

func groupTreeState(t *testing.T, rootPath string) map[string]string {
	t.Helper()
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	state := make(map[string]string)
	err = filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}
		state[rel] = info.Mode().String()
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := root.Readlink(rel)
			if err != nil {
				return err
			}
			state[rel] += target
		} else if !entry.IsDir() {
			data, err := root.ReadFile(rel)
			if err != nil {
				return err
			}
			state[rel] += string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
