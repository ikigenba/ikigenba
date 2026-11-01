package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
)

func TestPublicShapes(t *testing.T) {
	// R-ISG0-GB35
	type shape struct{ Space, Token, HostSpace string }
	c := config.Config(shape{"space", "token", "host"})
	if c.Space != "space" || c.Token != "token" || c.HostSpace != "host" {
		t.Fatal(c)
	}
	// R-ITNW-U2TU
	requireType[func(paths.Paths) (config.Config, error)](config.Load)
	// R-IW3P-LMB8
	requireType[func(string) bool](config.ValidSpace)
	// R-IXBL-ZE1X
	requireType[func(config.Config) bool](config.Config.Configured)
}

func requireType[T any](_ T) {}

func TestValidSpace(t *testing.T) {
	// R-IYJI-D5SM
	label := strings.Repeat("a", 63)
	maxName := label + "." + label + "." + label + "." + strings.Repeat("b", 61)
	for _, name := range []string{"us.ikigenba.dev", "sbx.ikigenba.dev", "a.b", "x-1.y2", "9.example", label + ".b", maxName} {
		if !config.ValidSpace(name) {
			t.Errorf("ValidSpace(%q) = false", name)
		}
	}
	for _, name := range []string{
		"", "ikigenba", "https://us.ikigenba.dev", "us.ikigenba.dev:443", "us.ikigenba.dev/mcp",
		"US.ikigenba.dev", "us.ikigenba.dev.", ".us.ikigenba.dev", "us..ikigenba.dev",
		"-us.ikigenba.dev", "us-.ikigenba.dev", "us_x.ikigenba.dev", " us.ikigenba.dev",
		strings.Repeat("a", 64) + ".b", maxName + "b", "a.b ", "é.b", "a.\x00b", "a.b\n",
	} {
		if config.ValidSpace(name) {
			t.Errorf("ValidSpace(%q) = true", name)
		}
	}
}

func TestSpaceFileParsing(t *testing.T) {
	// R-IZRE-QXJB
	for _, tt := range []struct{ name, contents, want string }{
		{"no final newline", "space=us.ikigenba.dev", "us.ikigenba.dev"},
		{"ignored and padded lines", "# note\n  space = x\nspace=us.ikigenba.dev \r\n", "us.ikigenba.dev"},
		{"first invalid", "space=https://us.ikigenba.dev\nspace=us.ikigenba.dev\n", ""},
		{"first valid wins", "space=a.b\nspace=x.y\n", "a.b"},
		{"tabs", "\tspace=\tx-1.y2 \t\r\n", "x-1.y2"},
		{"empty first value", "space=\t\nspace=a.b", ""},
		{"case sensitive key", "Space=x.y\nspace=a.b", "a.b"},
		{"no key", "# space=a.b\nother=a.b\n", ""},
		{"one carriage return", "space=a.b\r\r\nspace=x.y", ""},
		{"only space and tab trim", "\u00a0space=x.y\nspace=a.b", "a.b"},
		{"non-ascii value whitespace", "space=a.b\u00a0", ""},
		{"long ignored line", strings.Repeat("x", 70000) + "\nspace=a.b", "a.b"},
	} {
		for _, host := range []bool{false, true} {
			name := "user/" + tt.name
			if host {
				name = "host/" + tt.name
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				p := paths.Paths{Config: root}
				file := filepath.Join(root, paths.ConfigFile)
				if host {
					p = paths.Paths{HostConfig: filepath.Join(root, "host-config")}
					file = p.HostConfig
				}
				write(t, file, tt.contents)
				c, err := config.Load(p)
				if err != nil || c.Space != tt.want {
					t.Fatalf("Load = %+v, %v, want space %q", c, err, tt.want)
				}
				if host && c.HostSpace != tt.want {
					t.Fatalf("HostSpace = %q, want %q", c.HostSpace, tt.want)
				}
			})
		}
	}
}

func TestSpacePrecedence(t *testing.T) {
	// R-J0ZB-4PA0
	for _, tt := range []struct{ name, user, host, space, hostSpace string }{
		{"user wins", "space=sbx.ikigenba.dev", "space=us.ikigenba.dev", "sbx.ikigenba.dev", "us.ikigenba.dev"},
		{"invalid user", "space=bad", "space=us.ikigenba.dev", "us.ikigenba.dev", "us.ikigenba.dev"},
		{"missing user", "", "space=us.ikigenba.dev", "us.ikigenba.dev", "us.ikigenba.dev"},
		{"invalid host", "space=a.b", "space=bad", "a.b", ""},
		{"missing host", "space=a.b", "", "a.b", ""},
		{"both invalid", "space=bad", "space=bad", "", ""},
		{"both absent", "", "", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			p := paths.Paths{Config: root, HostConfig: filepath.Join(root, "host-config")}
			if tt.user != "" {
				write(t, filepath.Join(root, paths.ConfigFile), tt.user)
			}
			if tt.host != "" {
				write(t, p.HostConfig, tt.host)
			}
			c, err := config.Load(p)
			if err != nil || c.Space != tt.space || c.HostSpace != tt.hostSpace {
				t.Fatalf("Load = %+v, %v, want space %q, host %q", c, err, tt.space, tt.hostSpace)
			}
		})
	}
}

func TestTokenBytes(t *testing.T) {
	// R-OT2T-QMZH
	for _, tt := range []struct{ input, want string }{
		{"tok", "tok"}, {"tok\n", "tok"}, {"tok\r\n", "tok"}, {" tok \t\n", " tok \t"},
		{"tok\n\n", "tok\n"}, {"\n", ""}, {"tok\r", "tok"}, {"tok\r\r\n", "tok\r"},
		{"\r\n", ""}, {"", ""}, {"tok\x00bytes\n", "tok\x00bytes"}, {"\xff\n", "\xff"},
	} {
		root := t.TempDir()
		write(t, filepath.Join(root, paths.TokenFile), tt.input)
		c, err := config.Load(paths.Paths{Config: root})
		if err != nil || c.Token != tt.want {
			t.Errorf("token input %q: %+v, %v, want %q", tt.input, c, err, tt.want)
		}
	}
	root := t.TempDir()
	c, err := config.Load(paths.Paths{Config: root})
	if err != nil || c.Token != "" {
		t.Fatalf("missing token: %+v, %v", c, err)
	}
}

func TestMissingFilesAndEmptyPaths(t *testing.T) {
	// R-J3F3-W8RE
	root := t.TempDir()
	t.Chdir(root)
	// These files would be read incorrectly if empty Config were treated as '.'.
	write(t, filepath.Join(root, paths.ConfigFile), "space=wrong.example")
	write(t, filepath.Join(root, paths.TokenFile), "wrong-token")
	host := filepath.Join(root, "host-config")
	write(t, host, "space=us.ikigenba.dev")
	before := snapshot(t, root)
	for _, tt := range []struct {
		p    paths.Paths
		want config.Config
	}{
		{paths.Paths{}, config.Config{}},
		{paths.Paths{Config: filepath.Join(root, "missing")}, config.Config{}},
		{paths.Paths{Config: filepath.Join(root, "missing"), HostConfig: filepath.Join(root, "absent-host")}, config.Config{}},
		{paths.Paths{HostConfig: host}, config.Config{Space: "us.ikigenba.dev", HostSpace: "us.ikigenba.dev"}},
	} {
		c, err := config.Load(tt.p)
		if err != nil || c != tt.want {
			t.Fatalf("Load(%+v) = %+v, %v, want %+v", tt.p, c, err, tt.want)
		}
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("Load changed files: before %v, after %v", before, after)
	}
}

func TestLoadDoesNotChangeExistingFiles(t *testing.T) {
	// R-J3F3-W8RE
	root := t.TempDir()
	p := paths.Paths{Config: root, HostConfig: filepath.Join(root, "host-config")}
	write(t, p.HostConfig, "space=host.example\n")
	write(t, filepath.Join(root, paths.ConfigFile), "space=user.example\r\n")
	write(t, filepath.Join(root, paths.TokenFile), " token \n")
	before := snapshot(t, root)
	if _, err := config.Load(p); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("Load changed files: before %v, after %v", before, after)
	}
}

// R-VHZU-7NBM.
func TestReadFailures(t *testing.T) {
	for _, file := range []string{"host", paths.ConfigFile, paths.TokenFile} {
		for _, failure := range []string{"directory", "not a directory", "permission"} {
			t.Run(file+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				p := paths.Paths{Config: filepath.Join(root, "user")}
				if err := os.Mkdir(p.Config, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(p.Config, file)
				if file == "host" {
					p.HostConfig = filepath.Join(root, "host")
					path = p.HostConfig
				}
				switch failure {
				case "directory":
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				case "not a directory":
					if file == "host" {
						write(t, path, "regular-file")
						p.HostConfig = filepath.Join(path, "child")
						path = p.HostConfig
					} else {
						p.Config = filepath.Join(root, "regular-file")
						write(t, p.Config, "regular-file")
						path = filepath.Join(p.Config, paths.ConfigFile)
						if file == paths.TokenFile {
							// A regular Config directory with a token symlink isolates its ENOTDIR error.
							p.Config = filepath.Join(root, "user")
							path = filepath.Join(p.Config, paths.TokenFile)
							if err := os.Symlink(filepath.Join(root, "regular-file", "child"), path); err != nil {
								t.Fatal(err)
							}
						}
					}
				case "permission":
					write(t, path, "unreadable")
					if err := os.Chmod(path, 0000); err != nil {
						t.Fatal(err)
					}
				}
				_, err := config.Load(p)
				prefix := "could not read '" + path + "': "
				if err == nil || !strings.HasPrefix(err.Error(), prefix) || len(err.Error()) == len(prefix) || strings.ContainsAny(err.Error(), "\r\n") {
					t.Fatalf("error = %v, want one line with prefix %q and nonempty detail", err, prefix)
				}
			})
		}
	}
}

func TestConfigured(t *testing.T) {
	// R-J5UW-NS8S
	for _, space := range []string{"", "space"} {
		for _, token := range []string{"", "token"} {
			for _, host := range []string{"", "host"} {
				c := config.Config{Space: space, Token: token, HostSpace: host}
				if got, want := c.Configured(), space != "" && token != ""; got != want {
					t.Errorf("%+v.Configured() = %v, want %v", c, got, want)
				}
			}
		}
	}
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

type fileState struct {
	mode     fs.FileMode
	contents string
}

func snapshot(t *testing.T, root string) map[string]fileState {
	t.Helper()
	files := map[string]fileState{}
	filesystem := os.DirFS(root)
	err := fs.WalkDir(filesystem, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := fileState{mode: info.Mode()}
		if !entry.IsDir() {
			contents, readErr := fs.ReadFile(filesystem, path)
			if readErr != nil {
				return readErr
			}
			state.contents = string(contents)
		}
		files[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// R-VHZU-7NBM.
func TestHostReadFailureRendersPathAndCause(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "host\nroot")
	p, err := paths.Resolve(func(key string) string {
		if key == "HOME" {
			return filepath.Join(root, "home")
		}
		return ""
	}, etc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.HostConfig, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = config.Load(p)
	prefix := "could not read '" + strings.ReplaceAll(p.HostConfig, "\n", `\n`) + "': "
	if err == nil || !strings.HasPrefix(err.Error(), prefix) || len(err.Error()) == len(prefix) {
		t.Fatalf("error = %v; want prefix %q and cause", err, prefix)
	}
	for _, b := range []byte(err.Error()) {
		if b < 0x20 || b == 0x7f {
			t.Errorf("control byte in error %q", err.Error())
		}
	}
}
