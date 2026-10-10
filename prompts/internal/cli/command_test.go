package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	prompts "github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/agent"
	"github.com/ikigenba/ikigenba/prompts/internal/cli"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/settings"
)

type countedWriter struct {
	bytes.Buffer
	writes int
}

func (w *countedWriter) Write(p []byte) (int, error) { w.writes++; return w.Buffer.Write(p) }

// Prevent io.WriteString bypassing the counted Write.
func (w *countedWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

type tripReader struct{ t *testing.T }

func (r tripReader) Read([]byte) (int, error) { r.t.Fatal("unexpected reader use"); return 0, io.EOF }

type tripSink struct{ t *testing.T }

func (s tripSink) Deliver(context.Context, telemetry.Event) error {
	s.t.Fatal("unexpected delivery")
	return nil
}

func commandProcess(t *testing.T, args []string) (cli.Process, *bytes.Buffer, *countedWriter) {
	t.Helper()
	out := new(bytes.Buffer)
	errout := new(countedWriter)
	p := cli.Process{Args: args, Stdout: out, Stderr: errout, Dir: t.TempDir(), Cgroup: t.TempDir(), Pid: 321,
		LookupEnv:   func(string) (string, bool) { t.Fatal("unexpected lookup"); return "", false },
		Unsetenv:    func(string) error { t.Fatal("unexpected unset"); return nil },
		Inherit:     func(uintptr) (net.Listener, error) { t.Fatal("unexpected listener inheritance"); return nil, nil },
		Now:         func() time.Time { t.Fatal("unexpected clock"); return time.Time{} },
		Sleep:       func(context.Context, time.Duration) { t.Fatal("unexpected sleep") },
		ScriptAfter: func(time.Duration) <-chan time.Time { t.Fatal("unexpected timer"); return nil },
		Banner:      func(page.User) page.Banner { t.Fatal("unexpected banner"); return page.Banner{} },
		MCP:         func(*telemetry.Writer) *mcp.Server { t.Fatal("unexpected MCP maker"); return nil },
		Stdin:       tripReader{t}, Rand: tripReader{t}, Sink: tripSink{t}, BaseURL: "http://invalid.example"}
	if err := os.WriteFile(filepath.Join(p.Cgroup, "cgroup.procs"), []byte(strconv.Itoa(p.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	return p, out, errout
}
func unchanged(t *testing.T, p cli.Process) {
	t.Helper()
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("command created state: %v", entries)
	}
	entries, err = os.ReadDir(p.Cgroup)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "cgroup.procs" {
		t.Fatalf("changed control group: %v", entries)
	}
	data, err := os.ReadFile(filepath.Join(p.Cgroup, "cgroup.procs"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(p.Pid) {
		t.Fatalf("changed process file: %q", data)
	}
}

// R-NNFJ-6H8J R-X5F4-GDIL
func TestProcessAndExitConstants(t *testing.T) {
	const total = cli.ExitSuccess + cli.ExitServerFailed + cli.ExitUsage
	code := total
	if code != 3 || !reflect.DeepEqual([]int{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}, []int{0, 1, 2}) || !reflect.DeepEqual([]int8{cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage}, []int8{0, 1, 2}) {
		t.Fatal("exit constants")
	}
	p := cli.Process{Args: []string{"--version"}, Version: "injected display", Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer),
		LookupEnv: func(string) (string, bool) { return "", false }, Unsetenv: func(string) error { return nil }, Pid: 2, Stdin: strings.NewReader(""),
		Inherit: func(uintptr) (net.Listener, error) { return nil, nil }, Now: func() time.Time { return time.Time{} },
		Sleep: func(context.Context, time.Duration) {}, ScriptAfter: func(time.Duration) <-chan time.Time { return nil }, Rand: strings.NewReader(""),
		Dir: t.TempDir(), Cgroup: t.TempDir(), Sink: tripSink{t}, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { return nil }, BaseURL: ""}
	if cli.Run(context.Background(), p) != cli.ExitSuccess {
		t.Fatal("process seam")
	}
}

// R-XGE7-WB6U R-XHM4-A2XJ R-XIU0-NUO8 R-XNPM-6XN0 R-XOXI-KPDP R-XQ5E-YH4E R-X6N0-U59A
func TestProductsTouchNothing(t *testing.T) {
	for _, tc := range []struct {
		args          []string
		version, want string
	}{
		{[]string{"--version"}, "sample display", "sample display\n"}, {[]string{"--version"}, "", "\n"},
		{[]string{"manifest"}, "", cli.Manifest}, {[]string{"--help"}, "", cli.Usage}} {
		t.Run(tc.args[0]+tc.version, func(t *testing.T) {
			p, out, errout := commandProcess(t, tc.args)
			p.Version = tc.version
			if got := cli.Run(context.Background(), p); got != cli.ExitSuccess {
				t.Fatalf("exit %d", got)
			}
			if out.String() != tc.want || errout.Len() != 0 || errout.writes != 0 {
				t.Fatalf("output %q stderr %q", out.String(), errout.String())
			}
			unchanged(t, p)
		})
	}
}

// R-XK1X-1MEX R-XL9T-FE5M R-XMHP-T5WB R-XZWM-0N1Y
func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		arg  string
	}{
		{[]string{"bogus"}, "bogus"}, {[]string{"--bogus"}, "--bogus"}, {[]string{"db"}, "db"},
		{[]string{"db", "bogus"}, "bogus"}, {[]string{"db", "status", "extra"}, "extra"},
		{[]string{"manifest", "x"}, "x"}, {[]string{"--help", "x"}, "x"}, {[]string{"--version", "-x"}, "-x"},
		{[]string{agent.Command, "x"}, "x"}, {[]string{agent.Command, "--x"}, "--x"}, {[]string{"bogus", "later"}, "bogus"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			p, out, errout := commandProcess(t, tc.args)
			kind := "command"
			if strings.HasPrefix(tc.arg, "-") {
				kind = "option"
			}
			want := "prompts: unknown " + kind + " '" + tc.arg + "'\n\nsee 'prompts --help' for usage\n"
			if got := cli.Run(context.Background(), p); got != cli.ExitUsage {
				t.Fatalf("exit %d", got)
			}
			if out.Len() != 0 || errout.String() != want || errout.writes != 1 {
				t.Fatalf("stdout %q stderr %q writes %d", out.String(), errout.String(), errout.writes)
			}
			unchanged(t, p)
		})
	}
}

// R-X2ZB-OU17 R-DDR3-LA3F R-XDYF-4RPG R-XBIM-D882 R-XF6B-IJG5 R-X7UX-7WZZ R-X92T-LOQO R-XAAP-ZGHD
func TestContracts(t *testing.T) {
	const usage = cli.Usage + ""
	const manifest = cli.Manifest + ""
	const nginx = cli.NginxConf + ""
	wantUsage := "Usage: prompts [command]\n\nRun prompts as agent sessions over the suite's models, with MCP tools at\n/mcp and pages for prompts and their runs at /, on the socket systemd\npasses in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n  agent       run one prompt run (the app starts it)\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	if usage != wantUsage {
		t.Fatal("usage contract")
	}
	d := settings.Defaults()
	env := "[env]\n"
	for _, v := range []struct {
		name  string
		value int64
	}{{"PROMPT_SECONDS", d.PromptSeconds}, {"OUTPUT_MAX_BYTES", d.OutputMaxBytes}, {"RUN_MAX_TOOL_CALLS", d.RunMaxToolCalls}, {"RUN_MEMORY_MAX_BYTES", d.RunMemoryMaxBytes}, {"RUNS_MEMORY_MAX_BYTES", d.RunsMemoryMaxBytes}, {"RUNS_CPU_PERCENT", d.RunsCPUPercent}, {"RUN_PIDS_MAX", d.RunPidsMax}, {"RUN_MAX_ACTIVE", d.RunMaxActive}, {"RUN_MAX_QUEUED", d.RunMaxQueued}, {"RUN_KEEP_DAYS", d.RunKeepDays}, {"RUN_KEEP_COUNT", d.RunKeepCount}} {
		env += fmt.Sprintf("%s = %q\n", v.name, strconv.FormatInt(v.value, 10))
	}
	keys := []string{}
	for _, host := range []agentkit.Host{agentkit.HostAnthropic, agentkit.HostOpenAI, agentkit.HostGemini, agentkit.HostXAI, agentkit.HostOpenRouter} {
		keys = append(keys, strconv.Quote(agent.KeyVariable(host)))
	}
	wantManifest := "app = \"prompts\"\ndescription = " + strconv.Quote(pages.Description) + "\ndefault = false\nmcp = true\nguests = false\nsecrets = [" + strings.Join(keys, ", ") + "]\n\n" + env + "\n[database]\nengine = \"sqlite\"\npath = \"state/prompts.db\"\n\n[resources]\nslice = \"apps\"\nmemory_max = \"896M\"\ngo_memory_limit = \"128M\"\ndelegate = true\n\n[home]\ngroup = \"core\"\n"
	if manifest != wantManifest {
		t.Fatalf("manifest mismatch: %s", cli.Manifest)
	}
	if nginx != "location = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("nginx contract")
	}
	for name, want := range map[string]string{"etc/manifest.toml": cli.Manifest, "etc/nginx.conf": cli.NginxConf} {
		data, err := fs.ReadFile(prompts.Etc(), name)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("embedded %s differs", name)
		}
	}
}

// R-XRDB-C8V3 R-XSL7-Q0LS R-XTT4-3SCH R-XV10-HK36 R-XXGT-93KK R-XYOP-MVB9
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"absent", "catalog", "newer", "invalid", "cwd"} {
		t.Run(mode, func(t *testing.T) {
			p, out, errout := commandProcess(t, []string{"db", "status"})
			path := filepath.Join(p.Dir, "state", "prompts.db")
			if mode == "catalog" || mode == "newer" || mode == "cwd" {
				handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: prompts.Migrations(), Now: func() time.Time { return time.Unix(100, 0) }})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "newer" {
					err = handle.Write(context.Background(), func(tx *sql.Tx) error {
						_, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES ('9999','2000-01-01T00:00:00Z')")
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := handle.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid" {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("invalid database fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := db.Config{Path: path, Migrations: prompts.Migrations()}
			var before, after bytes.Buffer
			statusErr := db.Status(context.Background(), cfg, &before)
			if mode == "cwd" {
				t.Chdir(p.Dir)
				p.Dir = ""
			}
			got := cli.Run(context.Background(), p)
			afterErr := db.Status(context.Background(), cfg, &after)
			if out.String() != before.String() || before.String() != after.String() || fmt.Sprint(statusErr) != fmt.Sprint(afterErr) {
				t.Fatalf("status changed: %q %q %q", before.String(), out.String(), after.String())
			}
			if statusErr == nil {
				if got != cli.ExitSuccess || errout.Len() != 0 {
					t.Fatalf("exit %d error %q", got, errout.String())
				}
			} else {
				want := "prompts: " + strings.ReplaceAll(statusErr.Error(), "\n", " ") + "\n"
				if got != cli.ExitServerFailed || errout.String() != want || errout.writes != 1 {
					t.Fatalf("exit %d diagnostic %q", got, errout.String())
				}
			}
			if mode == "absent" {
				unchanged(t, p)
			}
		})
	}
}

type closingReader struct {
	io.Reader
	closed int
}

func (r *closingReader) Close() error { r.closed++; return nil }

// R-I1AW-4ESB R-Y2CE-S6JC R-Y3KB-5YA1 R-M8MC-20BC
func TestAgentDispatch(t *testing.T) {
	for _, input := range []string{"", "not json", "{}"} {
		t.Run(input, func(t *testing.T) {
			p, out, errout := commandProcess(t, []string{agent.Command})
			inputReader := &closingReader{Reader: strings.NewReader(input)}
			p.Stdin = inputReader
			var looked []string
			p.LookupEnv = func(key string) (string, bool) {
				looked = append(looked, key)
				if key == "PROMPT_SECONDS" {
					return "refused", true
				}
				return "", false
			}
			p.Now = func() time.Time { return time.Unix(100, 0) }
			directInput := &closingReader{Reader: strings.NewReader(input)}
			var directOut, directErr bytes.Buffer
			expected := agent.Run(context.Background(), agent.Process{Stdin: directInput, Stdout: &directOut, Stderr: &directErr, LookupEnv: func(string) (string, bool) { return "", false }, Now: p.Now})
			got := cli.Run(context.Background(), p)
			if got != expected || out.String() != directOut.String() || errout.String() != directErr.String() || inputReader.closed != directInput.closed {
				t.Fatalf("dispatch differs: exit %d/%d output %q/%q stderr %q/%q closes %d/%d", got, expected, out.String(), directOut.String(), errout.String(), directErr.String(), inputReader.closed, directInput.closed)
			}
			for _, key := range looked {
				switch key {
				case "IKIGENBA_RUN_DIR", "IKIGENBA_RUN_ID", "IKIGENBA_WORK_DIR", "IKIGENBA_SERVICES":
				default:
					t.Fatalf("unexpected agent lookup %q", key)
				}
			}
			unchanged(t, p)
		})
	}
}

// A plain reader and nil reader have the same empty-byte semantics as a closer.
func TestAgentPlainAndNilInput(t *testing.T) {
	var results []int
	for _, input := range []io.Reader{nil, strings.NewReader("")} {
		var out, errout bytes.Buffer
		results = append(results, cli.Run(context.Background(), cli.Process{Args: []string{agent.Command}, Stdin: input, Stdout: &out, Stderr: &errout, LookupEnv: func(string) (string, bool) { return "", false }}))
	}
	if !reflect.DeepEqual(results, []int{results[0], results[0]}) {
		t.Fatal(results)
	}
}

// R-I1AW-4ESB R-Y2CE-S6JC R-Y3KB-5YA1 R-M8MC-20BC
func TestSuccessfulAgentDispatch(t *testing.T) {
	model := serveChatModel(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serveAnswer(w)
	}))
	defer server.Close()
	encoded, err := json.Marshal(agent.Spec{Model: model, Key: "test-provider-key", Prompt: "test prompt", BaseURL: server.URL, MaxToolCalls: 4})
	if err != nil {
		t.Fatal(err)
	}
	p, out, errout := commandProcess(t, []string{agent.Command})
	runDir := t.TempDir()
	directDir := t.TempDir()
	for _, dir := range []string{runDir, directDir} {
		if err := os.Mkdir(filepath.Join(dir, "work"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	lookup := func(dir string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			switch key {
			case "IKIGENBA_RUN_DIR":
				return dir, true
			case "IKIGENBA_WORK_DIR":
				return filepath.Join(dir, "work"), true
			case "IKIGENBA_RUN_ID":
				return "prr_0102030405060708", true
			case "IKIGENBA_SERVICES":
				return "", true
			default:
				t.Fatalf("agent read forbidden variable %q", key)
				return "", false
			}
		}
	}
	p.LookupEnv = lookup(runDir)
	p.Now = func() time.Time { return time.Unix(100, 0) }
	p.BaseURL = "http://irrelevant.example"
	input := &closingReader{Reader: bytes.NewReader(encoded)}
	p.Stdin = input
	directInput := &closingReader{Reader: bytes.NewReader(encoded)}
	var directOut, directErr bytes.Buffer
	expected := agent.Run(context.Background(), agent.Process{Stdin: directInput, Stdout: &directOut, Stderr: &directErr, LookupEnv: lookup(directDir), Now: p.Now})
	got := cli.Run(context.Background(), p)
	if got != agent.ExitAnswered || got != expected || out.String() != directOut.String() || errout.String() != directErr.String() || input.closed != 1 || directInput.closed != 1 {
		t.Fatalf("successful dispatch differs: %d/%d stdout %q/%q stderr %q/%q", got, expected, out.String(), directOut.String(), errout.String(), directErr.String())
	}
	if !reflect.DeepEqual(readTree(t, runDir), readTree(t, directDir)) {
		t.Fatal("agent produced different file trees")
	}
	unchanged(t, p)
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	directory, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := directory.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = fs.WalkDir(directory.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			tree[name] = "directory"
			return nil
		}
		data, err := directory.ReadFile(name)
		tree[name] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
