package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/telemetry/internal/cli"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

type writes struct{ calls [][]byte }

func (w *writes) Write(b []byte) (int, error) {
	w.calls = append(w.calls, bytes.Clone(b))
	return len(b), nil
}
func (w *writes) text() string { return string(bytes.Join(w.calls, nil)) }

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Error("randomness consulted")
	return 0, io.EOF
}

func refusedProcess(t *testing.T) (cli.Process, *bytes.Buffer, *writes) {
	t.Helper()
	stdout := new(bytes.Buffer)
	stderr := new(writes)
	p := cli.Process{Stdout: stdout, Stderr: stderr, Pid: 42, Version: "injected display", Dir: t.TempDir(), Rand: forbiddenReader{t}, Now: func() time.Time { t.Error("clock consulted"); return time.Time{} }, Sleep: func(context.Context, time.Duration) { t.Error("sleep consulted") }, Inherit: func(uintptr) (net.Listener, error) {
		t.Error("descriptor inherited")
		return nil, errors.New("unexpected")
	}, Unsetenv: func(string) error { t.Error("environment changed"); return nil }}
	return p, stdout, stderr
}
func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("directory changed: %v %v", entries, err)
	}
}

// R-0QPC-VU08
func TestManifest(t *testing.T) {
	const manifest = cli.Manifest
	if want := "app = \"telemetry\"\ndescription = \"" + web.Description + "\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nRETENTION_DAYS = \"15\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/telemetry.db\"\n\n[resources]\nslice = \"core\"\nmemory_max = \"256M\"\n\n[home]\ngroup = \"core\"\n"; manifest != want {
		t.Fatalf("manifest: got %q, want %q", manifest, want)
	}
}

// R-U76E-S4XV R-U8EB-5WOK R-UAU3-XG5Y R-QXCY-WVJI
func TestDeclarations(t *testing.T) {
	const nginx = cli.NginxConf
	if nginx != "location = /ingest { return 404; }\n" {
		t.Fatal("nginx")
	}
	const usage = cli.Usage
	if usage != "Usage: telemetry [command]\n\nServe the suite's trail of events: ingest at /ingest, MCP tools at /mcp, and\na landing page at /, on the socket systemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n" {
		t.Fatal("usage")
	}
	const success, failed, usageExit = cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage
	if success != 0 || failed != 1 || usageExit != 2 {
		t.Fatal("exit constants")
	}
}

// R-TX82-Z3CB R-THDE-02PA R-OPT6-7ZI9 R-OR12-LR8Y R-R5W9-L9QD R-R745-Z1H2 R-OUOR-R2H1 R-R4OD-7HZO R-R8C2-CT7R R-UC20-B7WN R-R9JY-QKYG
func TestCommands(t *testing.T) {
	cases := []struct {
		args                []string
		product, diagnostic string
		code                int
	}{
		{[]string{"--version"}, "injected display" + "\n", "", cli.ExitSuccess},
		{[]string{"manifest"}, cli.Manifest, "", cli.ExitSuccess},
		{[]string{"--help"}, cli.Usage, "", cli.ExitSuccess},
	}
	for _, args := range [][]string{{"bogus"}, {"--bad"}, {""}, {"--help", "--version"}, {"manifest", "tail", "more"}, {"--version", "-extra"}, {"bogus", "--help"}, {"db"}, {"db", "bogus"}, {"db", "status", "tail"}, {"db", "--bad"}} {
		arg := args[0]
		if arg == "db" && len(args) > 1 {
			if args[1] == "status" {
				arg = args[2]
			} else {
				arg = args[1]
			}
		} else if arg == "--help" || arg == "manifest" || arg == "--version" {
			arg = args[1]
		}
		kind := "command"
		if strings.HasPrefix(arg, "-") {
			kind = "option"
		}
		cases = append(cases, struct {
			args                []string
			product, diagnostic string
			code                int
		}{args, "", "telemetry: unknown " + kind + " '" + arg + "'\n\nsee 'telemetry --help' for usage\n", cli.ExitUsage})
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			p, out, errout := refusedProcess(t)
			p.Args = tc.args
			p.LookupEnv = func(string) (string, bool) { t.Error("environment consulted"); return "", false }
			code := cli.Run(context.Background(), p)
			if code != tc.code || out.String() != tc.product || errout.text() != tc.diagnostic {
				t.Fatalf("got %d %q %q", code, out.String(), errout.text())
			}
			if tc.diagnostic != "" && len(errout.calls) != 1 {
				t.Fatal("fragmented diagnostic")
			}
			assertEmpty(t, p.Dir)
		})
	}
}

// R-PAJG-Q342 R-RBZR-I4FU R-PCZ9-HMLG R-PE75-VEC5 R-RD7N-VW6J R-PGMY-MXTJ R-PJ2R-EHAX R-REFK-9NX8 R-RFNG-NFNX
func TestRefusedEnvironment(t *testing.T) {
	invalid := []string{"0", "-1", "+1", "2.5", "5s", "05", " 5", "5 ", "abc", "５", "1\n"}
	for _, key := range []string{"DRAIN_SECONDS", "RETENTION_DAYS"} {
		for _, value := range invalid {
			t.Run(key+"/"+value, func(t *testing.T) {
				p, out, errout := refusedProcess(t)
				var looked []string
				p.LookupEnv = func(k string) (string, bool) {
					looked = append(looked, k)
					if k == key {
						return value, true
					}
					return "", false
				}
				code := cli.Run(context.Background(), p)
				unit := "seconds"
				expectedKeys := []string{"DRAIN_SECONDS"}
				if key == "RETENTION_DAYS" {
					unit = "days"
					expectedKeys = append(expectedKeys, key)
				}
				want := "telemetry: " + key + " is '" + value + "', not a positive whole number of " + unit + "\n"
				if code != cli.ExitUsage || out.Len() != 0 || errout.text() != want || len(errout.calls) != 1 || !reflect.DeepEqual(looked, expectedKeys) {
					t.Fatalf("got %d %q keys %v", code, errout.text(), looked)
				}
				assertEmpty(t, p.Dir)
			})
		}
	}
	for _, env := range []map[string]string{{}, {"LISTEN_PID": "41", "LISTEN_FDS": "1"}, {"LISTEN_PID": "042", "LISTEN_FDS": "1"}, {"LISTEN_PID": "42"}, {"LISTEN_PID": "42", "LISTEN_FDS": "0"}, {"LISTEN_PID": "42", "LISTEN_FDS": "-1"}, {"LISTEN_PID": "42", "LISTEN_FDS": "1x"}, {"LISTEN_PID": "42", "LISTEN_FDS": "2"}, {"LISTEN_PID": "42", "LISTEN_FDS": "0002"}, {"LISTEN_PID": "42", "LISTEN_FDS": "999999999999999999999999999999999"}} {
		p, out, errout := refusedProcess(t)
		p.LookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
		want := "telemetry: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
		if env["LISTEN_PID"] == "42" && (env["LISTEN_FDS"] == "2" || env["LISTEN_FDS"] == "0002" || len(env["LISTEN_FDS"]) > 20) {
			want = "telemetry: " + env["LISTEN_FDS"] + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"
		}
		if cli.Run(context.Background(), p) != cli.ExitUsage || out.Len() != 0 || errout.text() != want || len(errout.calls) != 1 {
			t.Fatalf("env %v: %q", env, errout.text())
		}
		assertEmpty(t, p.Dir)
	}
}

// R-PMQG-JSJ0 R-PNYC-XK9P R-RGVD-17EM R-TX82-Z3CB
func TestInheritedFailure(t *testing.T) {
	for _, fds := range []string{"1", "001"} {
		p, out, errout := refusedProcess(t)
		env := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": fds, "LISTEN_FDNAMES": "trail"}
		p.LookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
		var unset []string
		p.Unsetenv = func(k string) error { unset = append(unset, k); delete(env, k); return errors.New("ignored") }
		calls := 0
		p.Inherit = func(fd uintptr) (net.Listener, error) {
			calls++
			if fd != 3 {
				t.Errorf("descriptor %d", fd)
			}
			return nil, errors.New("descriptor unavailable")
		}
		if cli.Run(context.Background(), p) != cli.ExitServerFailed || out.Len() != 0 || errout.text() != "telemetry: descriptor unavailable\n" || len(errout.calls) != 1 || calls != 1 || !reflect.DeepEqual(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) || len(env) != 0 {
			t.Fatalf("inherit failure %q %v", errout.text(), unset)
		}
		assertEmpty(t, p.Dir)
	}
}

// R-THDE-02PA R-TG5H-MAYL
func TestInjectedVersion(t *testing.T) {
	for _, value := range []string{"", "display from caller", "line one\nline two"} {
		p, out, errout := refusedProcess(t)
		p.Version = value
		p.Args = []string{"--version"}
		p.LookupEnv = func(string) (string, bool) { t.Error("environment consulted"); return "", false }
		if code := cli.Run(context.Background(), p); code != cli.ExitSuccess || out.String() != value+"\n" || errout.text() != "" {
			t.Fatalf("version %q: %d %q %q", value, code, out.String(), errout.text())
		}
		assertEmpty(t, p.Dir)
	}
}
