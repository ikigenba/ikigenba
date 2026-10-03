package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

type appFixture struct {
	t                         *testing.T
	root, work, config, state string
	commands                  []seam.Cmd
	environment               map[string]string
	getenvKeys                []string
	getenvOther               string
}

func newAppFixture(t *testing.T) *appFixture {
	t.Helper()
	r := t.TempDir()
	f := &appFixture{t: t, root: r, work: filepath.Join(r, "wip"), config: filepath.Join(r, "config"), state: filepath.Join(r, "state")}
	for _, p := range []string{f.work, f.config, f.state} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *appFixture) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.work, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *appFixture) manifest(app, body string) { f.write(app+"/etc/manifest.toml", body) }
func (f *appFixture) run() (int, string, string) {
	f.t.Helper()
	f.commands = nil
	f.getenvKeys = nil
	deps := seam.Deps{Dir: f.work, EUID: 1000, Getenv: func(k string) string {
		f.getenvKeys = append(f.getenvKeys, k)
		switch k {
		case "HOME":
			return f.root
		case "XDG_CONFIG_HOME":
			return f.config
		case "XDG_STATE_HOME":
			return f.state
		}
		if v, ok := f.environment[k]; ok {
			return v
		}
		return f.getenvOther
	}, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		f.commands = append(f.commands, c)
		switch c.Path {
		case "git":
			return seam.Result{Stdout: []byte(f.work + "\n")}, nil
		case "go":
			if err := os.WriteFile(c.Args[2], []byte("fixture executable"), 0600); err != nil {
				return seam.Result{}, err
			}
		case "systemctl":
			if len(c.Args) > 1 && c.Args[1] == "show" {
				return seam.Result{Stdout: []byte("inactive\n")}, nil
			}
		}
		return seam.Result{}, nil
	}, Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		f.t.Fatal("unexpected stream")
		return seam.Result{}, nil
	}}
	var out, err bytes.Buffer
	code := runChecked(context.Background(), f.t, []string{"up"}, strings.NewReader(""), &out, &err, deps)
	return code, out.String(), err.String()
}
func (f *appFixture) refuse(want string) {
	f.t.Helper()
	code, out, err := f.run()
	if code != 2 || out != "" || err != "sandbox: "+want+"\n" {
		f.t.Fatalf("got (%d,%q,%q), want refusal %q", code, out, err, want)
	}
	if len(f.commands) != 1 || f.commands[0].Path != "git" {
		f.t.Fatalf("commands after refusal: %#v", f.commands)
	}
}
func (f *appFixture) success() {
	f.t.Helper()
	code, _, err := f.run()
	if code != 0 || err != "" {
		f.t.Fatalf("up: %d %s", code, err)
	}
}
func (f *appFixture) read(rel string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Clean(filepath.Join(f.state, "ikigenba/sandbox/wip", rel)))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}
func appManifest(app string) string { return "app = " + strconv.Quote(app) + "\n" }

// R-AUCL-BZBE R-UE7I-W0Z0 R-UFFF-9SPP R-RKQQ-LM3W R-UP6M-BYN9 R-UNYP-Y6WK R-GH7Y-57L5 R-VDKL-ZDH5
func TestAppsDiscovery(t *testing.T) {
	f := newAppFixture(t)
	for _, a := range []string{"dummy", "auth"} {
		f.manifest(a, appManifest(a))
		f.write(a+"/etc/nginx.conf-extra", "fragment sentinel")
	}
	for _, d := range []string{"docs", "sandbox"} {
		if err := os.MkdirAll(filepath.Join(f.work, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.write("notes", "notes")
	f.manifest("tools/widget", appManifest("widget"))
	f.success()
	services := f.read("services.json")
	var obj struct{ Services []struct{ Name string } }
	if err := json.Unmarshal([]byte(services), &obj); err != nil {
		t.Fatal(err)
	}
	if len(obj.Services) != 2 || obj.Services[0].Name != "auth" || obj.Services[1].Name != "dummy" {
		t.Fatal(services)
	}
	var builds []seam.Cmd
	for _, c := range f.commands {
		if c.Path == "go" {
			builds = append(builds, c)
		}
	}
	if len(builds) != 2 {
		t.Fatal(builds)
	}
	for j, a := range []string{"auth", "dummy"} {
		if builds[j].Dir != filepath.Join(f.work, a) || builds[j].Args[3] != "./cmd/"+a {
			t.Fatal(builds[j])
		}
		if !strings.Contains(f.read("nginx/nginx.conf"), filepath.Join(f.work, a, "etc/nginx.conf*")) {
			t.Fatal("fragment missing")
		}
	}
	beforeEnv, beforeServices := f.read("env/auth.env"), services
	f.manifest("auth", appManifest("auth")+"extra = 1\n[database]\nengine = \"sqlite\"\npath = \"state/auth.db\"\n")
	f.success()
	if f.read("env/auth.env") != beforeEnv || f.read("services.json") != beforeServices {
		t.Fatal("ignored keys changed product")
	}
}

// R-URMF-3I4N R-USUB-H9VC
func TestAppsDiscoveryRefusals(t *testing.T) {
	f := newAppFixture(t)
	f.refuse("no apps in " + f.work + "\n\nan app is a directory holding etc/manifest.toml")
	if os.Geteuid() == 0 {
		t.Fatal("permission evidence requires an ordinary user")
	}
	dir, err := os.Open(filepath.Clean(f.work))
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.Chmod(0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dir.Chmod(0700); err != nil {
			t.Error(err)
		}
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	})
	f.refuse(f.work + ": permission denied")
}

// R-MCLC-9SME R-MBDF-W0VP R-UVA4-8TCQ
func TestAppNames(t *testing.T) {
	for _, name := range []string{"Dummy_2", "nginx", "-dummy", strings.Repeat("a", 64), "a\nb", "a\tb", "a\x7fb", "a\xffb"} {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest(name, appManifest(name))
			printed := strings.NewReplacer("\n", "\\x0a", "\t", "\\x09", "\x7f", "\\x7f").Replace(name)
			f.refuse("'" + printed + "' is not a usable app name")
		})
	}
}

// R-UXPX-0CU4 R-UYXT-E4KT R-V05P-RWBI R-GJNQ-WR2J R-V2LI-JFSW R-AY0A-HAJH
func TestManifestRefusals(t *testing.T) {
	cases := []struct{ body, want string }{{"app = \"dummy\n", "line 1 (last key \"app\"): strings cannot contain newlines"}, {"port = 8080\napp = \"demo\"\n", "'port' is not allowed; the sandbox gives the app its socket"}, {"port = \"8080\"\n", "'port' is not allowed; the sandbox gives the app its socket"}, {"", "'app' is missing"}, {"app = \"demo\"\n", "app 'demo' does not match its directory 'dummy'"}, {"app = \"\"\n", "app '' does not match its directory 'dummy'"}, {"app = \"de\\nmo\"\n", "app 'de\\x0amo' does not match its directory 'dummy'"}}
	for _, c := range []struct{ k, v, kind string }{{"app", "5", "a string"}, {"description", "5", "a string"}, {"default", "1", "a boolean"}, {"mcp", "\"yes\"", "a boolean"}, {"guests", "\"yes\"", "a boolean"}, {"secrets", "\"A\"", "an array of strings"}, {"secrets", "[1]", "an array of strings"}, {"env", "\"x\"", "a table of strings"}, {"env", "{X = 4}", "a table of strings"}, {"resources", "5", "a table"}, {"resources", "\"x\"", "a table"}} {
		body := appManifest("dummy")
		if c.k == "app" {
			body = ""
		}
		cases = append(cases, struct{ body, want string }{body + c.k + " = " + c.v + "\n", "'" + c.k + "' must be " + c.kind})
	}
	for _, c := range cases {
		t.Run(c.want+c.body, func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest("dummy", c.body)
			f.refuse("dummy: etc/manifest.toml: " + c.want)
		})
	}
	for _, kind := range []string{"directory", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest("dummy", appManifest("dummy"))
			p := filepath.Join(f.work, "dummy/etc/manifest.toml")
			reason := "permission denied"
			if kind == "directory" {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
				reason = "is a directory"
			} else {
				if os.Geteuid() == 0 {
					t.Fatal("requires nonroot")
				}
				if err := os.Chmod(p, 0000); err != nil {
					t.Fatal(err)
				}
			}
			f.refuse("dummy: etc/manifest.toml: " + reason)
		})
	}
}

// R-U5K4-3PRI R-NXZ4-ITFR
func TestAppDescriptions(t *testing.T) {
	for _, s := range []string{"Demo widgets\nto list and create", "ends\n", "a\rb", "a\tb", "a\x00b", "a\x1fb", "a\x7fb", "\n"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"description = "+appTOMLString(s)+"\nmcp = true\n")
		f.refuse("dummy: etc/manifest.toml: 'description' must be one line of text")
	}
	for _, s := range []string{"", "   ", "\u0085", "é"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"description = "+appTOMLString(s)+"\n")
		f.success()
	}
	for _, s := range []string{"absent", "", "   ", "\u00a0\u3000"} {
		f := newAppFixture(t)
		body := appManifest("dummy") + "mcp = true\n"
		if s != "absent" {
			body += "description = " + appTOMLString(s) + "\n"
		}
		f.manifest("dummy", body)
		f.refuse("dummy: etc/manifest.toml: 'mcp' is true but 'description' is empty; an MCP service must say what it offers")
	}
	f := newAppFixture(t)
	f.manifest("auth", appManifest("auth"))
	f.manifest("dummy", appManifest("dummy")+"mcp = false\ndescription = \"\"\n")
	f.success()
}

// R-UKB0-SVOH R-ULIX-6NF6 R-NUBF-DI7O R-UMQT-KF5V R-NZ70-WL6G R-AZ86-V2A6 R-V7H4-2IRO R-V8P0-GAID R-O0EX-ACX5
func TestAppVariables(t *testing.T) {
	for _, k := range []string{"A B", "1X", "", "A\nB", "A-B", "é"} {
		for _, secret := range []bool{false, true} {
			f := newAppFixture(t)
			body := appManifest("dummy")
			if secret {
				body += "secrets = [" + appTOMLString(k) + "]\n"
			} else {
				body += "[env]\n" + appTOMLString(k) + " = \"x\"\n"
			}
			f.manifest("dummy", body)
			f.refuse("dummy: etc/manifest.toml: '" + strings.ReplaceAll(k, "\n", "\\x0a") + "' is not a variable name")
		}
	}
	for _, k := range []string{"DRAIN_SECONDS", "LISTEN_FDS", "LISTEN_FDNAMES", "LISTEN_PID", "NOTIFY_SOCKET", "IKIGENBA_X", "IKIGENBA_SERVICES"} {
		for _, secret := range []bool{false, true} {
			f := newAppFixture(t)
			body := appManifest("dummy")
			if secret {
				body += "secrets = [\"" + k + "\"]\n"
			} else {
				body += "[env]\n" + k + " = \"x\"\n"
			}
			f.manifest("dummy", body)
			f.refuse("dummy: etc/manifest.toml: '" + k + "' is set by the sandbox")
		}
	}
	f := newAppFixture(t)
	f.manifest("dummy", appManifest("dummy")+"secrets = [\"X\"]\n[env]\nX = \"x\"\n")
	f.refuse("dummy: etc/manifest.toml: 'X' is both in [env] and in secrets")
	bad := []string{"line one\nline two", "line one\r", "line one\n", "a\x00b", "\ufeff", "\ufdd0", "\U0001ffff"}
	for r := rune(0xfdd0); r <= 0xfdef; r++ {
		bad = append(bad, string(r))
	}
	for plane := rune(0); plane <= 16; plane++ {
		bad = append(bad, string(plane<<16|0xfffe), string(plane<<16|0xffff))
	}
	for _, v := range bad {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"[env]\nBANNER = "+appTOMLString(v)+"\n")
		f.refuse("dummy: etc/manifest.toml: 'BANNER' holds a character an env file cannot hold")
	}
	f = newAppFixture(t)
	f.manifest("dummy", appManifest("dummy")+"[env]\nA1_ = \"\t\u0085é\"\n_ = \"x\"\nport = \"8080\"\nIKIGENBA = \"x\"\n")
	f.success()
	for _, body := range []string{"secrets=[\"Z Z\",\"A A\"]\n", "secrets=[\"A A\"]\n[env]\n\"Z Z\"=\"x\"\n\"B B\"=\"x\"\n", "secrets=[\"IKIGENBA_X\"]\n[env]\nA=\"\\n\"\n"} {
		f = newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+body)
		want := "'Z Z' is not a variable name"
		if strings.Contains(body, "B B") {
			want = "'B B' is not a variable name"
		}
		if strings.Contains(body, "IKIGENBA_X") {
			want = "'A' holds a character an env file cannot hold"
		}
		f.refuse("dummy: etc/manifest.toml: " + want)
	}
}
func appTOMLString(s string) string {
	q := strconv.Quote(s)
	for i := 0; i < 256; i++ {
		q = strings.ReplaceAll(q, fmt.Sprintf("\\x%02x", i), fmt.Sprintf("\\u%04x", i))
	}
	return q
}

// R-4EPJ-0D5Q R-VCCP-LLQG
func TestManifestPrecedence(t *testing.T) {
	cases := []struct {
		body, want string
		icon       bool
	}{{"app=\"demo\"\nport=8080\n", "'port' is not allowed; the sandbox gives the app its socket", false}, {"app=\"dummy\"\nmcp=true\nport=8080\n", "'port' is not allowed; the sandbox gives the app its socket", false}, {"app=\"dummy\"\nmcp=true\ndefault=1\n", "'default' must be a boolean", false}, {"app=\"dummy\"\nmcp=true\ndescription=\"\\n\"\n", "'description' must be one line of text", false}, {"app=\"dummy\"\nmcp=true\ndescription=\"a\\nb\"\n[env]\nIKIGENBA_X=\"x\"\n", "'description' must be one line of text", false}, {"app=\"dummy\"\nmcp=true\n[env]\nIKIGENBA_X=\"x\"\n", "'IKIGENBA_X' is set by the sandbox", false}, {"app=\"dummy\"\nmcp=true\n", "'mcp' is true but 'description' is empty; an MCP service must say what it offers", true}, {"app=5\ndescription=5\ndefault=1\nmcp=1\nsecrets=1\nenv=1\n", "'app' must be a string", false}}
	for _, c := range []struct{ body, want string }{
		{"app=\"dummy\"\ndescription=1\ndefault=1\n", "'description' must be a string"},
		{"app=\"dummy\"\ndefault=1\nmcp=1\n", "'default' must be a boolean"},
		{"app=\"dummy\"\nmcp=1\nsecrets=1\n", "'mcp' must be a boolean"},
		{"app=\"dummy\"\nmcp=\"yes\"\nguests=\"yes\"\n", "'mcp' must be a boolean"},
		{"app=\"dummy\"\nguests=\"yes\"\nsecrets=\"A\"\n", "'guests' must be a boolean"},
		{"app=\"dummy\"\nsecrets=1\nenv=1\n", "'secrets' must be an array of strings"},
		{"app=\"demo\"\nenv=1\n", "'env' must be a table of strings"},
		{"app=\"dummy\"\nenv=1\nresources=1\n", "'env' must be a table of strings"},
		{"app=\"demo\"\nresources=1\n", "'resources' must be a table"},
		{"resources=1\n", "'resources' must be a table"},
		{"app=\"dummy\"\nmcp=true\n[env]\nIKIGENBA_X=\"x\"\n[resources]\ncpu_weight=0\n", "'IKIGENBA_X' is set by the sandbox"},
		{"app=\"dummy\"\nmcp=true\n[resources]\ncpu_weight=0\n", "'resources.cpu_weight' must be a whole number from 1 to 10000"},
	} {
		cases = append(cases, struct {
			body, want string
			icon       bool
		}{c.body, c.want, false})
	}

	for _, c := range cases {
		f := newAppFixture(t)
		f.manifest("dummy", c.body)
		if c.icon {
			if err := os.MkdirAll(filepath.Join(f.work, "dummy/share/icon.svg"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		f.refuse("dummy: etc/manifest.toml: " + c.want)
	}
	f := newAppFixture(t)
	f.manifest("dummy", "app=\"dummy\"\nmcp=true\n[resources]\ncpu_weight=50\n")
	if err := os.MkdirAll(filepath.Join(f.work, "dummy/share/icon.svg"), 0700); err != nil {
		t.Fatal(err)
	}
	f.refuse("dummy: etc/manifest.toml: 'mcp' is true but 'description' is empty; an MCP service must say what it offers")
	f = newAppFixture(t)
	f.manifest("auth", appManifest("auth")+"port=8080\n")
	f.manifest("dummy", appManifest("demo"))
	f.refuse("auth: etc/manifest.toml: 'port' is not allowed; the sandbox gives the app its socket")
	f = newAppFixture(t)
	f.manifest("auth", appManifest("auth")+"default=true\n")
	f.manifest("dummy", appManifest("dummy")+"default=true\n")
	f.refuse("more than one default app: auth, dummy")
	f.manifest("dummy", appManifest("demo")+"default=true\n")
	f.refuse("dummy: etc/manifest.toml: app 'demo' does not match its directory 'dummy'")
}

// R-GH7Y-57L5
func TestManifestReadKeys(t *testing.T) {
	f := newAppFixture(t)
	f.manifest("dummy", appManifest("dummy"))
	f.success()
	beforeEnv, beforeServices := f.read("env/dummy.env"), f.read("services.json")
	f.manifest("dummy", appManifest("dummy")+"description=\"\"\ndefault=false\nmcp=false\nguests=false\nsecrets=[]\nenv={}\nresources={}\n")
	f.success()
	if f.read("env/dummy.env") != beforeEnv || f.read("services.json") != beforeServices {
		t.Fatal("absent keys did not use empty defaults")
	}
	f.environment = map[string]string{"TOKEN": "from-environment"}
	f.manifest("dummy", appManifest("dummy")+"description=\"Demo service\"\ndefault=true\nmcp=true\nsecrets=[\"TOKEN\"]\n[env]\nSETTING=\"configured\"\n[resources]\ncpu_weight=50\n")
	code, out, stderr := f.run()
	if code != 0 || stderr != "" || !strings.Contains(out, "http://wip.localhost:7400\n") {
		t.Fatalf("default app: %d %q %q", code, out, stderr)
	}
	var services struct {
		Services []struct {
			Name, Description string
			MCP               bool
		}
	}
	if err := json.Unmarshal([]byte(f.read("services.json")), &services); err != nil {
		t.Fatal(err)
	}
	if len(services.Services) != 1 || services.Services[0].Name != "dummy" || services.Services[0].Description != "Demo service" || !services.Services[0].MCP {
		t.Fatalf("read keys missing from services: %#v", services)
	}
	env := f.read("env/dummy.env")
	if !strings.Contains(env, "SETTING=\"configured\"\n") || !strings.Contains(env, "TOKEN=\"from-environment\"\n") {
		t.Fatal("manifest env or secret was not read")
	}
}

// R-89VT-FXJS
func TestResourceWeights(t *testing.T) {
	for _, key := range []string{"cpu_weight", "io_weight"} {
		for _, value := range []string{"0", "-1", "10001", "20000", "\"50\"", "50.0", "true", "[]", "{}"} {
			t.Run(key+"="+value, func(t *testing.T) {
				f := newAppFixture(t)
				f.manifest("dummy", appManifest("dummy")+"[resources]\n"+key+"="+value+"\n")
				f.refuse("dummy: etc/manifest.toml: 'resources." + key + "' must be a whole number from 1 to 10000")
			})
		}
		for _, value := range []string{"1", "50", "10000"} {
			f := newAppFixture(t)
			f.manifest("dummy", appManifest("dummy")+"[resources]\n"+key+"="+value+"\n")
			f.success()
		}
	}
}

// R-8B3P-TPAH
func TestResourceMemoryMax(t *testing.T) {
	for _, value := range []string{"\"0\"", "\"512MB\"", "\"512m\"", "\"1.5G\"", "\"50%\"", "\"\"", "\"9999999999G\"", "\"17179869185G\"", "536870912", "\"9223372036854775808\"", "\"8589934592G\"", "\"K\"", "\"+1\"", "\"-1\"", "\" 1\"", "\"1 \"", "\"١\"", "true", "{}"} {
		t.Run(value, func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest("dummy", appManifest("dummy")+"[resources]\nmemory_max="+value+"\n")
			f.refuse("dummy: etc/manifest.toml: 'resources.memory_max' must be a whole number of bytes, optionally followed by K, M, or G")
		})
	}
	for _, value := range []string{"512M", "1G", "1048576", "1", "1K", "0001", "9223372036854775807", "8589934591G", "8796093022207M", "9007199254740991K"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"[resources]\nmemory_max="+appTOMLString(value)+"\n")
		f.success()
	}
}

// R-8CBM-7H16
func TestResourceUnknownKeys(t *testing.T) {
	for _, value := range []string{"50", "\"50\"", "false", "[]", "{}"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"[resources]\ncpu_quota="+value+"\n")
		f.refuse("dummy: etc/manifest.toml: 'resources.cpu_quota' is not allowed; the resources are cpu_weight, memory_max, and io_weight")
	}
	f := newAppFixture(t)
	f.manifest("dummy", appManifest("dummy")+"[resources]\n\"bad\\nkey\\t\\u007f\"=1\n")
	f.refuse("dummy: etc/manifest.toml: 'resources.bad\\x0akey\\x09\\x7f' is not allowed; the resources are cpu_weight, memory_max, and io_weight")
}

// R-8DJI-L8RV
func TestResourcesPrecedence(t *testing.T) {
	for _, c := range []struct{ entries, want string }{
		{"memory_max=\"512MB\"\ncpu_weight=0\n", "'resources.cpu_weight' must be a whole number from 1 to 10000"},
		{"io_weight=0\nmemory_max=\"512MB\"\n", "'resources.memory_max' must be a whole number of bytes, optionally followed by K, M, or G"},
		{"cpu_quota=1\nio_weight=0\n", "'resources.io_weight' must be a whole number from 1 to 10000"},
		{"zz=1\ncpu_quota=1\n", "'resources.cpu_quota' is not allowed; the resources are cpu_weight, memory_max, and io_weight"},
	} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"[resources]\n"+c.entries)
		f.refuse("dummy: etc/manifest.toml: " + c.want)
	}
}

// R-8ERE-Z0IK
func TestResourcesDoNotChangeDeployment(t *testing.T) {
	f := newAppFixture(t)
	f.manifest("auth", appManifest("auth"))
	f.manifest("dummy", appManifest("dummy"))
	f.success()
	files := func() map[string]string {
		result := map[string]string{}
		for _, rel := range []string{"env/auth.env", "env/dummy.env", "services.json", "nginx/nginx.conf"} {
			result[rel] = f.read(rel)
		}
		for _, unit := range []string{"auth.socket", "auth.service", "dummy.socket", "dummy.service", "nginx.service"} {
			filename := "sandbox-wip-" + unit
			b, err := os.ReadFile(filepath.Clean(filepath.Join(f.config, "systemd/user", filename)))
			if err != nil {
				t.Fatal(err)
			}
			result[filename] = string(b)
		}
		return result
	}
	code, beforeOut, stderr := f.run()
	if code != 0 || stderr != "" {
		t.Fatalf("baseline: %d %s", code, stderr)
	}
	beforeFiles := files()
	beforeCommands := append([]seam.Cmd(nil), f.commands...)
	f.manifest("dummy", appManifest("dummy")+"[resources]\ncpu_weight=50\nmemory_max=\"512M\"\nio_weight=50\n")
	code, afterOut, stderr := f.run()
	if code != 0 || stderr != "" {
		t.Fatalf("with resources: %d %s", code, stderr)
	}
	afterFiles := files()
	if beforeOut != afterOut || !reflect.DeepEqual(beforeCommands, f.commands) || !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Fatal("resources changed deployment output or commands")
	}
	for _, setting := range []string{"CPUWeight=", "MemoryMax=", "IOWeight="} {
		if strings.Contains(afterFiles["sandbox-wip-dummy.service"], setting) {
			t.Fatalf("resource setting applied: %s", setting)
		}
	}
}

// R-GM3J-OAJX
func TestGuestsDoNotChangeDeployment(t *testing.T) {
	f := newAppFixture(t)
	f.manifest("auth", appManifest("auth"))
	f.manifest("dummy", appManifest("dummy"))
	f.success()
	files := func() map[string]map[string]string {
		data := upSnapshot(t, filepath.Join(f.state, "ikigenba/sandbox/wip"))
		delete(data, "nginx/nginx.conf")
		return map[string]map[string]string{
			"data":     data,
			"units":    upSnapshot(t, filepath.Join(f.config, "systemd/user")),
			"registry": {"content": upRead(t, filepath.Join(f.state, "ikigenba/sandbox/registry.json"))},
		}
	}
	code, beforeOut, stderr := f.run()
	if code != 0 || stderr != "" {
		t.Fatalf("baseline: %d %s", code, stderr)
	}
	beforeFiles := files()
	beforeCommands := append([]seam.Cmd(nil), f.commands...)
	f.manifest("dummy", appManifest("dummy")+"guests = true\n")
	code, afterOut, stderr := f.run()
	if code != 0 || stderr != "" {
		t.Fatalf("with guests: %d %s", code, stderr)
	}
	if beforeOut != afterOut || !reflect.DeepEqual(beforeCommands, f.commands) || !reflect.DeepEqual(beforeFiles, files()) {
		t.Fatal("guests changed deployment output, commands, or files other than nginx configuration")
	}
	var services struct{ Services []map[string]any }
	if err := json.Unmarshal([]byte(f.read("services.json")), &services); err != nil {
		t.Fatal(err)
	}
	for _, service := range services.Services {
		if _, present := service["guests"]; present {
			t.Fatal("service carries guests member")
		}
	}
}

// R-4EPJ-0D5Q R-UGNB-NKGE R-O6IF-77MM R-O7QB-KZDB R-O8Y7-YR40 R-OA64-CIUP R-OBE0-QALE R-U4C7-PY0T
func TestAppIcons(t *testing.T) {
	for _, emptyShare := range []bool{false, true} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth"))
		if emptyShare {
			if err := os.MkdirAll(filepath.Join(f.work, "auth/share"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		f.success()
		if strings.Contains(f.read("services.json"), "\"icon\"") {
			t.Fatal("absent icon published")
		}
	}
	t.Run("inaccessible share", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Fatal("permission evidence requires ordinary user")
		}
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth"))
		f.write("auth/share/icon.svg", "<svg/>")
		share := filepath.Join(f.work, "auth/share")
		dir, err := os.Open(filepath.Clean(share))
		if err != nil {
			t.Fatal(err)
		}
		if err := dir.Chmod(0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := dir.Chmod(0700); err != nil {
				t.Error(err)
			}
			if err := dir.Close(); err != nil {
				t.Error(err)
			}
		})
		if _, err := os.Lstat(filepath.Join(share, "icon.svg")); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("need genuine inaccessible icon: %v", err)
		}
		f.success()
		if strings.Contains(f.read("services.json"), "\"icon\"") {
			t.Fatal("failed lstat must mean no icon")
		}
	})

	for _, kind := range []string{"directory", "symlink", "dangling", "fifo", "socket"} {
		t.Run(kind, func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest("dummy", appManifest("dummy"))
			p := filepath.Join(f.work, "dummy/share/icon.svg")
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(p, 0700)
			case "symlink":
				f.write("valid.svg", "<svg/>")
				err = os.Symlink(filepath.Join(f.work, "valid.svg"), p)
			case "dangling":
				err = os.Symlink("missing", p)
			case "fifo":
				err = syscall.Mkfifo(p, 0600)
			case "socket":
				short, makeErr := os.MkdirTemp("", "icon-")
				if makeErr != nil {
					t.Fatal(makeErr)
				}
				t.Cleanup(func() { _ = os.RemoveAll(short) })
				socket := filepath.Join(short, "s")
				listener, listenErr := net.Listen("unix", socket)
				if listenErr != nil {
					t.Fatal(listenErr)
				}
				defer func() {
					if err := listener.Close(); err != nil {
						t.Error(err)
					}
				}()
				if err = os.Rename(socket, p); err != nil {
					t.Fatal(err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			f.refuse("dummy: share/icon.svg is not a regular file")
		})
	}
	for _, size := range []int{6, 65537} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth"))
		f.manifest("dummy", appManifest("demo"))
		f.write("auth/share/icon.svg", svgOfSize(size))
		if os.Geteuid() == 0 {
			t.Fatal("requires nonroot")
		}
		if err := os.Chmod(filepath.Join(f.work, "auth/share/icon.svg"), 0000); err != nil {
			t.Fatal(err)
		}
		f.refuse("auth: share/icon.svg: permission denied")
	}
	for _, content := range []string{svgOfSize(65537), strings.Repeat("x", 65536) + "\xe9"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy"))
		f.write("dummy/share/icon.svg", content)
		f.refuse("dummy: share/icon.svg is larger than 64 KiB")
	}
	for _, content := range []string{"caf\xe9", "<svg><title>Caf\xe9</title></svg>", "<svg><!--\xed\xa0\x80--></svg>"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy"))
		f.write("dummy/share/icon.svg", content)
		f.refuse("dummy: share/icon.svg is not valid UTF-8")
	}
	for _, content := range []string{"", "widget icon", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><svg/>", "<svg>", "<svg>&foo;</svg>", "<svg xmlns=\"http://www.w3.org/2000/svg\"><circle r=\"1\" r=\"2\"/></svg>", "<svg xmlns:p=\"u\" xmlns:q=\"u\" p:a=\"1\" q:a=\"2\"/>", "<html/>", "<svg/><svg/>", "<svg/>text"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy"))
		f.write("dummy/share/icon.svg", content)
		f.refuse("dummy: share/icon.svg is not an SVG image")
	}
	for _, content := range []string{svgOfSize(65536), "<?xml version=\"1.0\" encoding=\"UTF-8\"?><!DOCTYPE svg><!-- icon --><svg xmlns=\"http://www.w3.org/2000/svg\"/>\n", "<s:svg xmlns:s=\"http://www.w3.org/2000/svg\"/>", "<svg a=\"1\" xmlns:p=\"u\" p:a=\"2\"/>", "\xef\xbb\xbf<svg/>"} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy"))
		f.write("dummy/share/icon.svg", content)
		f.success()
		var obj struct{ Services []struct{ Icon string } }
		if err := json.Unmarshal([]byte(f.read("services.json")), &obj); err != nil {
			t.Fatal(err)
		}
		if obj.Services[0].Icon != content {
			t.Fatal("icon bytes changed")
		}
	}
}
func svgOfSize(n int) string {
	if n == 6 {
		return "<svg/>"
	}
	return "<svg>" + strings.Repeat(" ", n-11) + "</svg>"
}

// R-HVN4-BJYL
func TestAppNoSecretsEnvironment(t *testing.T) {
	f := newAppFixture(t)
	f.manifest("dummy", appManifest("dummy"))
	f.getenvOther = "non-empty"
	f.success()
	for _, key := range f.getenvKeys {
		if key != "HOME" && key != "XDG_CONFIG_HOME" && key != "XDG_STATE_HOME" {
			t.Fatalf("unlisted secret consulted: %s", key)
		}
	}
}

// R-A0RL-IX8T R-A1ZH-WOZI R-PKCY-6WJV
func TestAppSecretUsableValues(t *testing.T) {
	for _, value := range []string{"local-secret\n", "local-secret\r\n", "local-\rsecret", "local-secret\x00", "local-secret\ufffe"} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth")+"secrets=[\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_SECRET\"]\n")
		f.environment = map[string]string{"GOOGLE_LOCALHOST_CLIENT_SECRET": value}
		f.refuse("environment variable GOOGLE_LOCALHOST_CLIENT_SECRET, read for auth's GOOGLE_CLIENT_SECRET, holds a character an env file cannot hold")
	}
	f := newAppFixture(t)
	f.manifest("dummy", appManifest("dummy")+"secrets=[\"FOO\"]\n")
	f.manifest("auth", appManifest("auth")+"secrets=[\"GOOGLE_CLIENT_SECRET\",\"GOOGLE_CLIENT_ID\"]\n")
	f.environment = map[string]string{"GOOGLE_LOCALHOST_CLIENT_ID": "x\n", "GOOGLE_LOCALHOST_CLIENT_SECRET": "x\n", "FOO": "x\n"}
	f.refuse("environment variable GOOGLE_LOCALHOST_CLIENT_ID, read for auth's GOOGLE_CLIENT_ID, holds a character an env file cannot hold")
	f = newAppFixture(t)
	f.manifest("dummy", appManifest("dummy")+"secrets=[\"FOO\"]\n")
	f.environment = map[string]string{"FOO": "x\n"}
	f.refuse("environment variable FOO, read for dummy's FOO, holds a character an env file cannot hold")
}

// R-PMSQ-YG19 R-A5N7-207L
func TestAppMissingSecrets(t *testing.T) {
	for _, values := range []map[string]string{nil, {"GOOGLE_LOCALHOST_CLIENT_ID": "", "GOOGLE_LOCALHOST_CLIENT_SECRET": ""}, {"GOOGLE_CLIENT_ID": "9999-web.apps.googleusercontent.com", "GOOGLE_CLIENT_SECRET": "production-secret"}} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth")+"secrets=[\"GOOGLE_CLIENT_SECRET\",\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_ID\"]\n")
		f.environment = values
		f.refuse("secrets missing from the environment\n\nauth GOOGLE_CLIENT_ID from GOOGLE_LOCALHOST_CLIENT_ID\nauth GOOGLE_CLIENT_SECRET from GOOGLE_LOCALHOST_CLIENT_SECRET")
	}
	f := newAppFixture(t)
	f.manifest("auth", appManifest("auth")+"secrets=[\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_SECRET\"]\n")
	f.environment = map[string]string{"GOOGLE_LOCALHOST_CLIENT_ID": "valid"}
	f.refuse("secrets missing from the environment\n\nauth GOOGLE_CLIENT_SECRET from GOOGLE_LOCALHOST_CLIENT_SECRET")
	f.manifest("dummy", appManifest("dummy")+"secrets=[\"GOOGLE_CLIENT_ID\",\"FOO\"]\n")
	f.environment = nil
	f.refuse("secrets missing from the environment\n\nauth GOOGLE_CLIENT_ID from GOOGLE_LOCALHOST_CLIENT_ID\nauth GOOGLE_CLIENT_SECRET from GOOGLE_LOCALHOST_CLIENT_SECRET\ndummy FOO from FOO\ndummy GOOGLE_CLIENT_ID from GOOGLE_LOCALHOST_CLIENT_ID")
	f.environment = map[string]string{"GOOGLE_LOCALHOST_CLIENT_ID": "valid", "GOOGLE_LOCALHOST_CLIENT_SECRET": "valid", "FOO": "bar", "GOOGLE_CLIENT_SECRET": "x\n", "SIGNING_KEY": "x\n"}
	f.success()
}

// R-9X3W-DM0Q R-S32R-VW8K
func TestAppSecretEnvironment(t *testing.T) {
	f := newAppFixture(t)
	f.manifest("auth", appManifest("auth")+"secrets=[\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_SECRET\"]\n")
	f.manifest("dummy", appManifest("dummy")+"secrets=[\"FOO\",\"GOOGLE_CLIENT_ID\"]\n")
	f.environment = map[string]string{"GOOGLE_LOCALHOST_CLIENT_ID": "1234-abc.apps.googleusercontent.com", "GOOGLE_LOCALHOST_CLIENT_SECRET": "local-secret", "GOOGLE_CLIENT_ID": "9999-web.apps.googleusercontent.com", "GOOGLE_CLIENT_SECRET": "production-secret", "FOO": "bar"}
	f.success()
	for _, app := range []string{"auth", "dummy"} {
		if !strings.Contains(f.read("env/"+app+".env"), "GOOGLE_CLIENT_ID=\"1234-abc.apps.googleusercontent.com\"\n") {
			t.Fatal("wrong Google source")
		}
	}
	if !strings.Contains(f.read("env/auth.env"), "GOOGLE_CLIENT_SECRET=\"local-secret\"\n") || !strings.Contains(f.read("env/dummy.env"), "FOO=\"bar\"\n") {
		t.Fatal("secret not delivered")
	}
	f.environment["GOOGLE_LOCALHOST_CLIENT_SECRET"] = "rotated-secret"
	f.success()
	if !strings.Contains(f.read("env/auth.env"), "GOOGLE_CLIENT_SECRET=\"rotated-secret\"\n") {
		t.Fatal("secret not refreshed")
	}
}

// R-VESI-D57U R-O2UQ-1WEJ R-VUN7-C5UV R-B43S-E58Y R-S6QH-17GN R-W0QP-90KC R-O5AI-TFVX
func TestAppDeploymentFiles(t *testing.T) {
	for _, def := range []bool{false, true} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth")+"secrets=[\"GOOGLE_CLIENT_ID\",\"GOOGLE_CLIENT_SECRET\"]\n[env]\nWORKSPACE_DOMAIN=\"michaelgreenly.dev\"\n")
		f.manifest("dummy", appManifest("dummy")+fmt.Sprintf("default=%t\nmcp=true\ndescription=\"Demo widgets to list and create\"\n", def))
		icon := "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 24\"><circle cx=\"12\" cy=\"12\" r=\"10\"/></svg>\n"
		f.write("dummy/share/icon.svg", icon)
		f.environment = map[string]string{"GOOGLE_LOCALHOST_CLIENT_ID": "1234-abc.apps.googleusercontent.com", "GOOGLE_LOCALHOST_CLIENT_SECRET": "local-secret", "GOOGLE_CLIENT_ID": "9999-web.apps.googleusercontent.com", "GOOGLE_CLIENT_SECRET": "production-secret", "SIGNING_KEY": "ignored", "STRIPE_KEY": "ignored"}
		f.success()
		servicesPath := filepath.Join(f.state, "ikigenba/sandbox/wip/services.json")
		base := func(app string) string {
			return "IKIGENBA_CALLBACK_URL=\"http://localhost:7400\"\nIKIGENBA_PUBLIC_URL=\"http://" + app + ".wip.localhost:7400\"\nIKIGENBA_SANDBOX=\"wip\"\nIKIGENBA_SERVICES=\"" + servicesPath + "\"\n"
		}
		authEnv := "DRAIN_SECONDS=\"5\"\nGOOGLE_CLIENT_ID=\"1234-abc.apps.googleusercontent.com\"\nGOOGLE_CLIENT_SECRET=\"local-secret\"\n" + base("auth") + "WORKSPACE_DOMAIN=\"michaelgreenly.dev\"\n"
		if got := f.read("env/auth.env"); got != authEnv {
			t.Fatalf("auth env %q", got)
		}
		if got := f.read("env/dummy.env"); got != "DRAIN_SECONDS=\"5\"\n"+base("dummy") {
			t.Fatalf("dummy env %q", got)
		}
		want := "{\n  \"services\": [\n    { \"name\": \"auth\", \"url\": \"http://auth.wip.localhost:7400\", \"description\": \"\", \"socket\": \"/run/user/1000/sandbox/7400/auth.sock\", \"enabled\": true, \"mcp\": false },\n    { \"name\": \"dummy\", \"url\": \"http://dummy.wip.localhost:7400\", \"description\": \"Demo widgets to list and create\", \"socket\": \"/run/user/1000/sandbox/7400/dummy.sock\", \"enabled\": true, \"mcp\": true, \"icon\": \"<svg xmlns=\\\"http://www.w3.org/2000/svg\\\" viewBox=\\\"0 0 24 24\\\"><circle cx=\\\"12\\\" cy=\\\"12\\\" r=\\\"10\\\"/></svg>\\n\" }\n  ]\n}\n"
		if got := f.read("services.json"); got != want {
			t.Fatalf("services differs\ngot %s\nwant %s", got, want)
		}
		b, err := os.ReadFile(filepath.Clean(filepath.Join(f.state, "ikigenba/sandbox/registry.json")))
		if err != nil {
			t.Fatal(err)
		}
		var reg struct {
			Sandboxes []struct {
				Apps []struct {
					Name    string
					Default bool
				}
			}
		}
		if err := json.Unmarshal(b, &reg); err != nil {
			t.Fatal(err)
		}
		if len(reg.Sandboxes) != 1 || len(reg.Sandboxes[0].Apps) != 2 {
			t.Fatalf("registry %s", b)
		}
		for _, a := range reg.Sandboxes[0].Apps {
			if a.Default != (a.Name == "dummy" && def) {
				t.Fatalf("registry default %s", b)
			}
		}
	}
	f := newAppFixture(t)
	v := "a\"b\\c$d`e\t  "
	f.manifest("dummy", appManifest("dummy")+"description=\"<widget>& é\"\n[env]\nX="+appTOMLString(v)+"\n")
	f.success()
	if !strings.HasSuffix(f.read("env/dummy.env"), "X=\"a\\\"b\\\\c\\$d\\`e\t  \"\n") {
		t.Fatal(f.read("env/dummy.env"))
	}
	if !strings.Contains(f.read("services.json"), "\"description\": \"<widget>& é\"") {
		t.Fatal(f.read("services.json"))
	}
}

// R-A6V3-FRYA
func TestAppSecretNonLeakage(t *testing.T) {
	sentinel := "SENTINEL-secret-token"
	for _, kind := range []string{"success", "invalidValue", "missing"} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth")+"secrets=[\"TOKEN\",\"Z\"]\n")
		f.environment = map[string]string{"TOKEN": sentinel, "Z": "good"}
		if kind == "invalidValue" {
			f.environment["Z"] = "bad\n"
		}
		if kind == "missing" {
			delete(f.environment, "Z")
		}

		code, out, stderr := f.run()
		if kind == "success" && code != 0 || kind != "success" && code != 2 {
			t.Fatalf("%s: %d %s", kind, code, stderr)
		}
		if strings.Contains(out+stderr, sentinel) {
			t.Fatalf("secret in output %s", kind)
		}
		for _, c := range f.commands {
			if strings.Contains(c.Path+strings.Join(c.Args, " ")+c.Dir, sentinel) {
				t.Fatalf("secret in command %#v", c)
			}
		}
		for _, root := range []string{f.state, filepath.Join(f.config, "systemd")} {
			if _, err := os.Stat(root); os.IsNotExist(err) {
				continue
			}
			scoped, openErr := os.OpenRoot(root)
			if openErr != nil {
				f.t.Fatal(openErr)
			}
			err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.Type().IsRegular() {
					return nil
				}
				relative, relErr := filepath.Rel(root, p)
				if relErr != nil {
					return relErr
				}
				b, readErr := scoped.ReadFile(relative)
				if readErr != nil {
					return readErr
				}
				if strings.Contains(string(b), sentinel) && p != filepath.Join(f.state, "ikigenba/sandbox/wip/env/auth.env") {
					t.Fatalf("secret leaked into %s", p)
				}
				return nil
			})
			if closeErr := scoped.Close(); closeErr != nil {
				t.Error(closeErr)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
