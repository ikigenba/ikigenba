package cli

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func configPaths(t testing.TB, deps seam.Deps) paths.Paths {
	t.Helper()
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func overrideEnv(deps *seam.Deps, key, value string) {
	previous := deps.Getenv
	deps.Getenv = func(k string) string {
		if k == key {
			return value
		}
		return previous(k)
	}
}
func assertFile(t testing.TB, path, want string, mode fs.FileMode) {
	t.Helper()
	confined, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := confined.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	data, err := confined.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	info, err := confined.Stat(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want || info.Mode().Perm() != mode {
		t.Errorf("%s = %q mode %o, want %q mode %o", path, data, info.Mode().Perm(), want, mode)
	}
}
func assertContents(t testing.TB, path, want string) {
	t.Helper()
	confined, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := confined.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	data, err := confined.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s = %q, want %q", path, data, want)
	}
}
func assertAbsent(t testing.TB, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s should be absent, stat = %v", path, err)
	}
}
func assertUnchangedConfigure(t *testing.T, deps seam.Deps, root string, args []string, stdin io.Reader, wantCode int, wantErr string) {
	t.Helper()
	before := snapshotTree(t, root)
	code, out, errText := runCLI(t, deps, args, stdin)
	assertCLI(t, code, out, errText, wantCode, "", wantErr)
	assertTreeEqual(t, before, snapshotTree(t, root))
}

// R-OXYF-9PY9 R-J8AP-FBQ6 R-P0E8-19FN.
func TestConfigureHelp(t *testing.T) {
	for _, args := range [][]string{{"configure", "--help"}, {"configure", "-h"}, {"configure", "not a space", "--token", "-h"}, {"configure", "--bogus", "-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { inertCLI(t, args, 1000, ExitOK, expectedUsage["configure"], "") })
	}
}

// R-J9IL-T3GV R-JKHP-9154 R-JPDA-S43W R-JQL7-5VUL R-JWOP-2QK2 R-P0E8-19FN.
func TestConfigureArgumentTokensAndLocations(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		want       string
		defaultXDG bool
	}{{"repeat", []string{"configure", "--token", "old", "us.ikigenba.dev", "--token=new"}, "new", false}, {"leading hyphen", []string{"configure", "us.ikigenba.dev", "--token=-abc="}, "-abc=", false}, {"space before", []string{"configure", "us.ikigenba.dev", "--token", "stored"}, "stored", true}, {"surrounding blanks", []string{"configure", "us.ikigenba.dev", "--token", " a b "}, " a b ", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			if tc.defaultXDG {
				overrideEnv(&deps, "XDG_CONFIG_HOME", "")
			}
			p := configPaths(t, deps)
			// An unreadable host path cannot matter when an explicit space is supplied.
			if err := os.MkdirAll(p.HostConfig, 0700); err != nil {
				t.Fatal(err)
			}
			stdin := &recordingReader{Reader: strings.NewReader("unused other token\n")}
			code, out, errText := runCLI(t, deps, tc.args, stdin)
			assertCLI(t, code, out, errText, ExitOK, "", "")
			if stdin.Reads != 0 {
				t.Error("read stdin despite --token")
			}
			assertContents(t, filepath.Join(p.Config, paths.ConfigFile), "space=us.ikigenba.dev\n")
			assertFile(t, filepath.Join(p.Config, paths.TokenFile), tc.want, 0600)
		})
	}
}

// R-D8CZ-79O4 R-D9KV-L1ET R-P0E8-19FN.
func TestConfigureValidationOrder(t *testing.T) {
	cases := []struct {
		args    []string
		summary string
		noHome  bool
	}{{[]string{"configure", "us.ikigenba.dev", "--token"}, "option '--token' needs a value", false}, {[]string{"configure", "--token", "--token=x", "us.ikigenba.dev"}, "option '--token' needs a value", false}, {[]string{"configure", "https://us.ikigenba.dev", "--bogus"}, "unknown option '--bogus'", false}, {[]string{"configure", "https://x", "extra"}, "unexpected argument 'extra'", false}, {[]string{"configure", "us.ikigenba.dev", "--token", "--bogus"}, "unknown option '--bogus'", false}, {[]string{"configure", "https://x", "--token"}, "option '--token' needs a value", false}, {[]string{"configure", "us.ikigenba.dev", "--token"}, "option '--token' needs a value", true}}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			deps, root := syntheticDeps(t)
			if tc.noHome {
				overrideEnv(&deps, "HOME", "")
			}
			stdin := &recordingReader{}
			assertUnchangedConfigure(t, deps, root, tc.args, stdin, ExitUsage, usageDiagnostic("configure", tc.summary))
			if stdin.Reads != 0 {
				t.Error("read stdin before validation")
			}
		})
	}
}

// R-P0E8-19FN.
func TestConfigureInvalidSpace(t *testing.T) {
	for _, space := range []string{"https://us.ikigenba.dev", "us.ikigenba.dev:443", "us.ikigenba.dev/mcp", "US.ikigenba.dev", "ikigenba", ""} {
		for _, noHome := range []bool{false, true} {
			t.Run(space+stringBool(noHome), func(t *testing.T) {
				deps, root := syntheticDeps(t)
				if noHome {
					overrideEnv(&deps, "HOME", "")
				}
				stdin := &recordingReader{}
				assertUnchangedConfigure(t, deps, root, []string{"configure", space, "--token=distinct-secret"}, stdin, ExitUsage, "turns: '"+space+"' is not a space name\n")
				if stdin.Reads != 0 {
					t.Error("invalid space read stdin")
				}
			})
		}
	}
}
func stringBool(value bool) string {
	if value {
		return " no HOME"
	}
	return " with HOME"
}

// R-JEE7-C6FN R-P0E8-19FN.
func TestConfigureBadHome(t *testing.T) {
	for _, home := range []string{"", "home/me"} {
		for _, withSpace := range []bool{false, true} {
			t.Run(home+stringBool(withSpace), func(t *testing.T) {
				deps, root := syntheticDeps(t)
				overrideEnv(&deps, "HOME", home)
				args := []string{"configure"}
				if withSpace {
					args = append(args, "a.b")
				}
				stdin := &recordingReader{}
				message := "HOME is not set"
				if home != "" {
					message = "HOME 'home/me' is not an absolute path"
				}
				assertUnchangedConfigure(t, deps, root, args, stdin, ExitFailed, "turns: "+message+"\n")
				if stdin.Reads != 0 {
					t.Error("bad HOME read stdin")
				}
			})
		}
	}
}

// R-JGU0-3PX1 R-P0E8-19FN.
func TestConfigureNeedsHostSpaceEvenWithUserConfig(t *testing.T) {
	for _, host := range []string{"empty etc", "absent", "space=bad\n"} {
		for _, tokenArg := range []bool{false, true} {
			t.Run(host+stringBool(tokenArg), func(t *testing.T) {
				deps, root := syntheticDeps(t)
				p := configPaths(t, deps)
				writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=a.b\n", 0640)
				if host == "empty etc" {
					deps.Etc = ""
				} else if host != "absent" {
					writeFixture(t, p.HostConfig, host, 0600)
				}
				args := []string{"configure"}
				if tokenArg {
					args = append(args, "--token=secret")
				}
				stdin := &recordingReader{}
				assertUnchangedConfigure(t, deps, root, args, stdin, ExitUsage, usageDiagnostic("configure", "no space given"))
				if stdin.Reads != 0 {
					t.Error("no host space read stdin")
				}
			})
		}
	}
}

// R-RHGK-OQS9 R-P0E8-19FN.
func TestConfigureHostReadFailure(t *testing.T) {
	deps, root := syntheticDeps(t)
	p := configPaths(t, deps)
	if err := os.MkdirAll(p.HostConfig, 0700); err != nil {
		t.Fatal(err)
	}
	_, wantErr := config.Load(paths.Paths{HostConfig: p.HostConfig})
	if wantErr == nil {
		t.Fatal("host directory should fail config.Load")
	}
	stdin := &recordingReader{}
	assertUnchangedConfigure(t, deps, root, []string{"configure", "--token=secret"}, stdin, ExitFailed, "turns: could not read the configuration\n\n"+wantErr.Error()+"\n")
	if stdin.Reads != 0 {
		t.Error("host error read stdin")
	}
	code, out, errText := runCLI(t, deps, []string{"configure", "a.b", "--token=secret"}, stdin)
	assertCLI(t, code, out, errText, ExitOK, "", "")
}

// R-ETSY-BIID R-JQL7-5VUL.
func TestConfigureStdinFirstLine(t *testing.T) {
	for _, text := range []string{"tok\n", "tok\r\n", "tok", "tok\nsecond\n", " tok \n"} {
		t.Run(text, func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			p := configPaths(t, deps)
			code, out, errText := runCLI(t, deps, []string{"configure", "a.b"}, strings.NewReader(text))
			assertCLI(t, code, out, errText, ExitOK, "", "")
			want := "tok"
			if strings.HasPrefix(text, " ") {
				want = " tok "
			}
			assertFile(t, filepath.Join(p.Config, paths.TokenFile), want, 0600)
		})
	}
}

type lineThenError struct {
	remaining  string
	laterReads int
}

func (r *lineThenError) Read(p []byte) (int, error) {
	if r.remaining == "" {
		r.laterReads++
		return 0, errors.New("unneeded second read")
	}
	n := copy(p, r.remaining)
	r.remaining = r.remaining[n:]
	return n, nil
}

// R-OVIM-I6GV.
func TestConfigureStopsAtFirstNewline(t *testing.T) {
	deps, _ := syntheticDeps(t)
	reader := &lineThenError{remaining: "tok\n"}
	code, out, errText := runCLI(t, deps, []string{"configure", "a.b"}, reader)
	assertCLI(t, code, out, errText, ExitOK, "", "")
	if reader.laterReads != 0 {
		t.Error("read after first LF")
	}
	p := configPaths(t, deps)
	assertFile(t, filepath.Join(p.Config, paths.TokenFile), "tok", 0600)
}

// R-89VY-KJJO R-8B3U-YBAD R-JMXI-0KMI R-P0E8-19FN.
func TestConfigureBadTokensLeaveFilesAndModes(t *testing.T) {
	cases := []struct {
		value   string
		flag    bool
		summary string
	}{{"", false, "no token given"}, {"\n", false, "no token given"}, {"\r\n", false, "no token given"}, {" \t\n", false, "no token given"}, {"", true, "no token given"}, {" ", true, "no token given"}, {"\u2003", true, "no token given"}, {"to\tk\n", false, "the token holds a control character"}, {"to\rk\r\n", false, "the token holds a control character"}, {"tok\x7f", false, "the token holds a control character"}, {"a\x01b", true, "the token holds a control character"}, {"tok\r\r\n", false, "the token holds a control character"}}
	for _, tc := range cases {
		for _, existing := range []bool{false, true} {
			t.Run(tc.value+stringBool(tc.flag)+stringBool(existing), func(t *testing.T) {
				deps, root := syntheticDeps(t)
				p := configPaths(t, deps)
				if existing {
					writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=old.example\n", 0640)
					writeFixture(t, filepath.Join(p.Config, paths.TokenFile), "old-token", 0400)
				}
				args := []string{"configure", "a.b"}
				var stdin io.Reader = strings.NewReader(tc.value)
				if tc.flag {
					args = append(args, "--token="+tc.value)
					stdin = &recordingReader{}
				}
				assertUnchangedConfigure(t, deps, root, args, stdin, ExitUsage, "turns: "+tc.summary+"\n")
			})
		}
	}
	deps, root := syntheticDeps(t)
	assertUnchangedConfigure(t, deps, root, []string{"configure", "a.b"}, nil, ExitUsage, "turns: no token given\n")
	deps, root = syntheticDeps(t)
	assertUnchangedConfigure(t, deps, root, []string{"configure", "a.b", "--token", ""}, nil, ExitUsage, "turns: no token given\n")
}

type tokenErrorReader struct{}

func (tokenErrorReader) Read([]byte) (int, error) { return 0, errors.New("synthetic stdin error") }

// R-JO5E-ECD7 R-P0E8-19FN.
func TestConfigureStdinError(t *testing.T) {
	deps, root := syntheticDeps(t)
	assertUnchangedConfigure(t, deps, root, []string{"configure", "a.b"}, tokenErrorReader{}, ExitFailed, "turns: could not read the token from stdin\n\nsynthetic stdin error\n")
}

// R-JRT3-JNLA R-JQL7-5VUL R-JVGS-OYTD.
func TestConfigureReplacesExistingFiles(t *testing.T) {
	for _, mode := range []fs.FileMode{0644, 0400, 0000} {
		t.Run(mode.String(), func(t *testing.T) {
			deps, _ := syntheticDeps(t)
			p := configPaths(t, deps)
			args := []string{"configure", "a-much-longer-space.ikigenba.dev", "--token=a-much-longer-token"}
			code, out, errText := runCLI(t, deps, args, nil)
			assertCLI(t, code, out, errText, ExitOK, "", "")
			for _, name := range []string{paths.ConfigFile, paths.TokenFile} {
				if err := os.Chmod(filepath.Join(p.Config, name), mode); err != nil {
					t.Fatal(err)
				}
			}
			code, out, errText = runCLI(t, deps, []string{"configure", "sbx.ikigenba.dev", "--token=t2"}, nil)
			assertCLI(t, code, out, errText, ExitOK, "", "")
			assertContents(t, filepath.Join(p.Config, paths.ConfigFile), "space=sbx.ikigenba.dev\n")
			assertFile(t, filepath.Join(p.Config, paths.TokenFile), "t2", 0600)
		})
	}
}

// R-JT0Z-XFBZ R-OZ6B-NHOY.
func TestConfigureOnlyCreatesPrivateRequiredPaths(t *testing.T) {
	for _, useDefault := range []bool{false, true} {
		t.Run(stringBool(useDefault), func(t *testing.T) {
			deps, root := syntheticDeps(t)
			if useDefault {
				overrideEnv(&deps, "XDG_CONFIG_HOME", "")
			} else {
				overrideEnv(&deps, "XDG_CONFIG_HOME", filepath.Join(root, "missing", "parent", "config"))
			}
			p := configPaths(t, deps)
			before := snapshotTree(t, root)
			code, out, errText := runCLI(t, deps, []string{"configure", "a.b", "--token=private-token"}, nil)
			assertCLI(t, code, out, errText, ExitOK, "", "")
			after := snapshotTree(t, root)
			for name, entry := range after {
				if old, ok := before[name]; ok {
					if !reflect.DeepEqual(old, entry) {
						t.Errorf("changed preexisting path %s", name)
					}
					continue
				}
				absolute := root + name
				if entry.Mode.IsDir() {
					if !strings.HasPrefix(p.Config, absolute+string(filepath.Separator)) && absolute != p.Config {
						t.Errorf("unexpected directory %s", absolute)
					}
					if entry.Mode.Perm() & ^fs.FileMode(0700) != 0 {
						t.Errorf("created nonprivate %s: %o", absolute, entry.Mode.Perm())
					}
				} else if absolute != filepath.Join(p.Config, paths.ConfigFile) && absolute != filepath.Join(p.Config, paths.TokenFile) {
					t.Errorf("unexpected file %s", absolute)
				}
			}
		})
	}
}

// R-JU8W-B72O R-P0E8-19FN R-OZ6B-NHOY.
func TestConfigureHostWritesTokenOnly(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, tokenArg := range []bool{false, true} {
			t.Run(stringBool(existing)+stringBool(tokenArg), func(t *testing.T) {
				deps, root := syntheticDeps(t)
				p := configPaths(t, deps)
				writeFixture(t, p.HostConfig, "space=us.ikigenba.dev\n", 0600)
				if existing {
					writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=override.example\n", 0640)
				}
				before := snapshotTree(t, root)
				args := []string{"configure"}
				var stdin io.Reader = strings.NewReader("new-token\n")
				if tokenArg {
					args = append(args, "--token=new-token")
					stdin = &recordingReader{}
				}
				code, out, errText := runCLI(t, deps, args, stdin)
				assertCLI(t, code, out, errText, ExitOK, "", "")
				if existing {
					assertFile(t, filepath.Join(p.Config, paths.ConfigFile), "space=override.example\n", 0640)
				} else {
					assertAbsent(t, filepath.Join(p.Config, paths.ConfigFile))
				}
				assertFile(t, filepath.Join(p.Config, paths.TokenFile), "new-token", 0600)
				after := snapshotTree(t, root)
				for name, entry := range after {
					absolute := root + name
					if absolute == p.Config || absolute == filepath.Join(p.Config, paths.TokenFile) {
						continue
					}
					if old, ok := before[name]; !ok || !reflect.DeepEqual(old, entry) {
						t.Errorf("unexpected changed path %s", absolute)
					}
				}
			})
		}
	}
}

// R-K1KA-LTIU R-P2U0-SSX1 R-P0E8-19FN.
func TestConfigureWriteFailures(t *testing.T) {
	for _, name := range []string{paths.TokenFile, paths.ConfigFile} {
		t.Run(name, func(t *testing.T) {
			deps, root := syntheticDeps(t)
			p := configPaths(t, deps)
			badPath := filepath.Join(p.Config, name)
			writeFixture(t, filepath.Join(badPath, "child"), "directory contents", 0640)
			if name == paths.TokenFile {
				writeFixture(t, filepath.Join(p.Config, paths.ConfigFile), "space=old.example\n", 0640)
			} else {
				writeFixture(t, filepath.Join(p.Config, paths.TokenFile), "old-token", 0644)
			}
			before := snapshotTree(t, root)
			code, out, errText := runCLI(t, deps, []string{"configure", "a.b", "--token=new-token"}, nil)
			if code != ExitFailed || out != "" {
				t.Errorf("unexpected result %d %q", code, out)
			}
			lines := strings.Split(strings.TrimSuffix(errText, "\n"), "\n")
			if len(lines) != 3 || lines[0] != "turns: could not write '"+badPath+"'" || lines[1] != "" || lines[2] == "" || strings.HasPrefix(lines[2], "> ") {
				t.Errorf("bad diagnostic %q", errText)
			}
			assertFile(t, filepath.Join(badPath, "child"), "directory contents", 0640)
			if name == paths.TokenFile {
				assertTreeEqual(t, before, snapshotTree(t, root))
			} else {
				assertFile(t, filepath.Join(p.Config, paths.TokenFile), "new-token", 0600)
				after := snapshotTree(t, root)
				delete(before, strings.TrimPrefix(filepath.Join(p.Config, paths.TokenFile), root))
				delete(after, strings.TrimPrefix(filepath.Join(p.Config, paths.TokenFile), root))
				assertTreeEqual(t, before, after)
			}
		})
	}
}

// R-P0E8-19FN.
func TestConfigureCannotCreateDirectory(t *testing.T) {
	deps, root := syntheticDeps(t)
	blocked := filepath.Join(root, "blocked")
	writeFixture(t, blocked, "file", 0600)
	overrideEnv(&deps, "XDG_CONFIG_HOME", blocked)
	p := configPaths(t, deps)
	before := snapshotTree(t, root)
	code, out, errText := runCLI(t, deps, []string{"configure", "a.b", "--token=secret"}, nil)
	if code != ExitFailed || out != "" {
		t.Errorf("unexpected result %d %q", code, out)
	}
	lines := strings.Split(strings.TrimSuffix(errText, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "turns: could not create '"+p.Config+"'" || lines[1] != "" || lines[2] == "" || strings.HasPrefix(lines[2], "> ") {
		t.Errorf("bad diagnostic %q", errText)
	}
	assertTreeEqual(t, before, snapshotTree(t, root))
}

// R-P1M4-F16C R-P0E8-19FN.
func TestConfigureRefusalsNeverEchoToken(t *testing.T) {
	for _, kind := range []string{"no space", "invalid space", "no HOME", "write error"} {
		for _, flag := range []bool{false, true} {
			t.Run(kind+stringBool(flag), func(t *testing.T) {
				deps, _ := syntheticDeps(t)
				token := "distinctive-sensitive-fixture-token"
				args := []string{"configure", "a.b"}
				switch kind {
				case "no space":
					args = []string{"configure"}
				case "invalid space":
					args = []string{"configure", "https://bad.example"}
				case "no HOME":
					overrideEnv(&deps, "HOME", "")
				case "write error":
					p := configPaths(t, deps)
					writeFixture(t, filepath.Join(p.Config, paths.TokenFile, "child"), "blocked", 0600)
				}
				var stdin io.Reader = strings.NewReader(token + "\n")
				if flag {
					args = append(args, "--token", token)
				}
				code, out, errText := runCLI(t, deps, args, stdin)
				if code == ExitOK {
					t.Error("expected refusal")
				}
				if strings.Contains(out, token) || strings.Contains(errText, token) {
					t.Error("token leaked")
				}
			})
		}
	}
}
