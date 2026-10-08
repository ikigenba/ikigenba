package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestPerAppCommandsRefuseReleasedLayoutBeforeConfiguration(t *testing.T) {
	// R-GLNY-UNLX R-GMVV-8FCM
	for _, command := range []string{"install", "uninstall"} {
		for _, target := range []string{"releases/missing", ""} {
			t.Run(command+"/"+target, func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, "opt/ikigenba")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				current := filepath.Join(dir, "current")
				if target == "" {
					if err := os.Mkdir(current, 0700); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(target, current); err != nil {
					t.Fatal(err)
				}
				// An unreadable store shape proves layout refusal precedes store reads.
				if err := os.MkdirAll(filepath.Join(root, "etc/ikigenba/config.json"), 0700); err != nil {
					t.Fatal(err)
				}
				called := false
				deps := cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { called = true; return nil, nil }}}
				arg := "bad/name"
				if command == "install" {
					arg = "s3://bucket/key"
				}
				stdout, stderr, code := invoke([]string{command, arg}, deps)
				if code != 1 || stdout != "" || stderr != "opsctl: this host runs releases; deploy with 'opsctl activate'\n" || called {
					t.Fatalf("exit %d stdout %q stderr %q called %v", code, stdout, stderr, called)
				}
				info, err := os.Lstat(current)
				if err != nil || (target != "" && info.Mode()&os.ModeSymlink == 0) {
					t.Fatalf("current changed: %v %v", info, err)
				}
				for _, path := range []string{"var/opt", "etc/systemd", "etc/nginx", "run/ikigenba", "usr/local/bin/opsctl"} {
					if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
						t.Fatalf("unexpected mutation %s: %v", path, err)
					}
				}
			})
		}
	}
}

func TestPerAppCommandsReportLayoutFailureBeforeWorkflow(t *testing.T) {
	// R-GLNY-UNLX R-GMVV-8FCM
	for _, command := range []string{"install", "uninstall"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "opt"), []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{command, "bad/name"}
		if command == "install" {
			args[1] = "s3://bucket/key"
		}
		deps := cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("process called")
			return host.Result{}, nil
		}, Cloud: cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { t.Fatal("cloud called"); return nil, nil }}}
		_, layoutErr := apps.ReadLayout(root)
		if layoutErr == nil {
			t.Fatal("fixture did not fail layout detection")
		}
		before := treeState(t, root)
		stdout, stderr, code := invoke(args, deps)
		if code != 1 || stdout != "" || stderr != "opsctl: "+layoutErr.Error()+"\n" {
			t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
		}
		if !reflect.DeepEqual(before, treeState(t, root)) {
			t.Fatal("layout failure changed host state")
		}
	}
}
