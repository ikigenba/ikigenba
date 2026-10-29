package accounts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const userKeyBody = "AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B"
const secondKeyBody = "AAAAC3NzaC1lZDI1NTE5AAAAIAZbrkOd+Y6S7A73l8INVj18asxLPNd5ESn8jJF92cOm"
const thirdKeyBody = "AAAAC3NzaC1lZDI1NTE5AAAAIOX98ItbYTqX546sPOUFYsJXWCgEsWBavHBIkCjPHj9P"
const userAccountRows = "root:x:0:0::/root:/bin/bash\nacme:x:301:301::/srv/acme:/usr/sbin/nologin\nbeta:x:302:302::/srv/beta:/usr/sbin/nologin\nalice:x:1001:1001::/home/alice:/bin/bash\nbob:x:1002:1002::/home/bob:/bin/bash\nec2-user:x:1000:1000::/home/ec2-user:/bin/bash\n"
const userGroups = "root:x:0:\nshell:x:400:bob,alice,missing\nacme:x:301:alice,bob\nbeta:x:302:alice\nalice:x:1001:\nbob:x:1002:\nwheel:x:10:ec2-user\n"
const userShadow = "alice:!:1:0:99999:7:::\nbob:!:1:0:99999:7::1:\n"

type userFixture struct {
	t        *testing.T
	env      host.Env
	commands []host.Command
	run      func(host.Command) (host.Result, error)
}

func newUserFixture(t *testing.T) *userFixture {
	t.Helper()
	f := &userFixture{t: t}
	f.env = host.Env{Root: t.TempDir(), Now: func() time.Time { return time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC) }}
	f.env.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
		f.commands = append(f.commands, c)
		if f.run != nil {
			return f.run(c)
		}
		if c.Name == "journalctl" {
			return host.Result{ExitCode: 1}, nil
		}
		return host.Result{}, nil
	}
	f.write("/etc/passwd", userAccountRows, 0600)
	f.write("/etc/group", userGroups, 0600)
	f.write("/etc/shadow", userShadow, 0600)
	return f
}

func (f *userFixture) write(path, data string, mode os.FileMode) {
	f.t.Helper()
	path = rootPath(f.env.Root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *userFixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(rootPath(f.env.Root, path))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *userFixture) key() Key { return Key{Body: userKeyBody, Comment: "alice@laptop"} }

func userCall(name string, args ...string) host.Command { return host.Command{Name: name, Args: args} }
