package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestVersionUsesExecutableRelease(t *testing.T) {
	// R-8PCC-RYAO R-8QK9-5Q1D
	for _, label := range []string{"", "candidate/test"} {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			sha := strings.Repeat("a", 40)
			dir := filepath.Join(root, "opt/ikigenba/releases", sha)
			exe := filepath.Join(dir, "opsctl/bin/opsctl")
			writeCLIInstallFile(t, exe, "fixture")
			writeCLIInstallFile(t, filepath.Join(dir, "release.json"), `{"sha":"`+sha+`"}`)
			if label != "" {
				writeCLIInstallFile(t, filepath.Join(dir, "label"), label+"\n")
			}
			writeInitConfig(t, root, `{"opsctl.version":"override","future.key":"ignored"}`)
			deps := cli.Deps{Root: root, EUID: 17, Executable: func() (string, error) { return exe, nil }, Getenv: func(string) string { return "override" }}
			want := sha[:7]
			if label != "" {
				want = label + " (" + want + ")"
			}
			for _, args := range [][]string{{"version"}, {"-V"}, {"--version"}} {
				out, err, code := invoke(args, deps)
				if out != want+"\n" || err != "" || code != 0 {
					t.Fatalf("%v = %q %q %d", args, out, err, code)
				}
			}
			deps.Executable = func() (string, error) { return "", errors.New("fixture failure") }
			out, err, code := invoke([]string{"version"}, deps)
			if out != "\n" || err != "" || code != 0 {
				t.Fatal(out, err, code)
			}
			before, e := readFixtureFile(root, "etc/ikigenba/config.json")
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(string(before), "future.key") {
				t.Fatal("configuration changed")
			}
		})
	}
}

func TestUnknownConfigurationKeysSurviveCommands(t *testing.T) {
	// R-VHJD-YDWI
	deps := depsAt(t, 0)
	for _, args := range [][]string{{"config", "set", "future.key=opaque"}, {"config", "set", "known=value"}, {"config", "del", "known"}} {
		out, err, code := invoke(args, deps)
		if out != "" || err != "" || code != 0 {
			t.Fatal(args, out, err, code)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"config", "get", "future.key"}, "opaque\n"}, {[]string{"config", "list"}, "future.key=opaque\n"}} {
		out, err, code := invoke(tc.args, deps)
		if out != tc.want || err != "" || code != 0 {
			t.Fatal(tc.args, out, err, code)
		}
	}
	for _, args := range [][]string{{"--help"}, {"version"}, {"init", "--help"}, {"nginx", "--help"}} {
		out, err, code := invoke(args, deps)
		clean := depsAt(t, 0)
		wantOut, wantErr, wantCode := invoke(args, clean)
		if out != wantOut || err != wantErr || code != wantCode {
			t.Fatal(args, "future key changed command result")
		}
	}
	out, err, code := invoke([]string{"config", "get", "future.key"}, deps)
	if out != "opaque\n" || err != "" || code != 0 {
		t.Fatal("future key changed")
	}
}

func TestUnknownConfigurationKeyDoesNotChangeSetupAction(t *testing.T) {
	// R-VHJD-YDWI
	baseline := readyStateGuardDeps(t)
	future := readyStateGuardDeps(t)
	for _, deps := range []cli.Deps{baseline, future} {
		store := config.Store{Root: deps.Root}
		for k, v := range map[string]string{"aws.region": "region", "backup.s3_uri": "s3://bucket/host/"} {
			if err := store.Set(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	futureStore := config.Store{Root: future.Root}
	if err := futureStore.Set("future.key", "opaque"); err != nil {
		t.Fatal(err)
	}
	run := func(deps cli.Deps) (string, string, int, []string, map[string]treeEntry) {
		var commands []string
		deps.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
			line := strings.ReplaceAll(c.Name+" "+strings.Join(c.Args, " "), deps.Root, "ROOT")
			if prefix, _, found := strings.Cut(line, "/.services-"); found {
				line = prefix + "/.services-TEMP"
			}
			commands = append(commands, line)
			if result, handled := servicesFixtureCommand(c); handled {
				return result, nil
			}
			return host.Result{}, nil
		}
		out, diagnostic, code := invoke([]string{"init"}, deps)
		return out, diagnostic, code, commands, treeState(t, deps.Root)
	}
	wantOut, wantErr, wantCode, wantCommands, wantTree := run(baseline)
	out, diagnostic, code, commands, _ := run(future)
	if wantCode != 0 || wantErr != "" {
		t.Fatalf("baseline setup failed: %d %q", wantCode, wantErr)
	}
	if out != wantOut || diagnostic != wantErr || code != wantCode || !reflect.DeepEqual(commands, wantCommands) {
		t.Fatal("unknown key changed configured setup result", code, diagnostic, commands, wantCommands)
	}
	if value, err := futureStore.Get("future.key"); err != nil || value != "opaque" {
		t.Fatal("setup did not preserve future key", value, err)
	}
	if err := futureStore.Del("future.key"); err != nil {
		t.Fatal(err)
	}
	gotTree := treeState(t, future.Root)
	for path, e := range wantTree {
		e.Content = strings.ReplaceAll(e.Content, baseline.Root, "ROOT")
		wantTree[path] = e
	}
	for path, e := range gotTree {
		e.Content = strings.ReplaceAll(e.Content, future.Root, "ROOT")
		gotTree[path] = e
	}
	if !reflect.DeepEqual(wantTree, gotTree) {
		t.Fatal("future key changed setup filesystem effects")
	}
}

func readFixtureFile(root, name string) ([]byte, error) {
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fs.Close() }()
	return fs.ReadFile(name)
}
