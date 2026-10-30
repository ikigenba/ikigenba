package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
)

// R-DD0R-79BA R-DE8N-L11Z R-DGOG-CKJD R-DHWC-QCA2 R-DJ49-440R R-PHW4-C3M0 R-PJ40-PVCP R-DLK1-VNI5 R-SGIE-LJRV R-V8IV-TOG4 R-SIY7-D399 R-RDKJ-WH74 R-RESG-A8XT
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	const commit = "abcdeff01234567890-dirty"
	const release = "binary-fixture"
	t.Setenv(version.CommitVariable, commit)
	t.Setenv(version.ReleaseVariable, release)
	id := version.Read()
	display := id.String()
	bin := filepath.Join(t.TempDir(), "accounts")
	build := exec.Command("go", "build", "-o")
	build.Args = append(build.Args, bin, ".")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %s: %v", b, e)
	}
	env := []string{version.CommitVariable + "=" + commit, version.ReleaseVariable + "=" + release}
	for _, c := range []struct {
		args, env   []string
		out, errout string
		code        int
	}{
		{[]string{"--version"}, env, display + "\n", "", 0}, {[]string{"--version"}, nil, "\n", "", 0}, {[]string{"manifest"}, env, cli.Manifest, "", 0},
		{[]string{"bogus"}, env, "", "accounts: unknown command 'bogus'\n\nsee 'accounts --help' for usage\n", cli.ExitUsage},
		{nil, env, "", "accounts: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
	} {
		cmd := exec.Command("/bin/sh", "-c", `exec "$0" "$@"`)
		cmd.Args = append(cmd.Args, bin)
		cmd.Args = append(cmd.Args, c.args...)
		cmd.Env = append([]string{}, c.env...)
		cmd.Dir = t.TempDir()
		var out, errout bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errout
		err := cmd.Run()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatal(err)
			}
			code = ee.ExitCode()
		}
		if code != c.code || out.String() != c.out || errout.String() != c.errout {
			t.Fatalf("%v: %d %q %q", c.args, code, out.String(), errout.String())
		}
		entries, e := os.ReadDir(cmd.Dir)
		if e != nil || len(entries) != 0 {
			t.Fatal(entries, e)
		}
	}
	socketDir, e := os.MkdirTemp("", "accounts-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	captureSocket := filepath.Join(socketDir, "capture")
	captureLn, e := net.Listen("unix", captureSocket)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	var captured []telemetry.Event
	captureServer := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/ingest" {
			t.Error("unexpected telemetry request", r.Method, r.URL.Path)
		}
		var wire struct {
			Event string          `json:"event"`
			Attrs telemetry.Attrs `json:"attrs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		mu.Lock()
		captured = append(captured, telemetry.Event{Name: wire.Event, Attrs: wire.Attrs})
		mu.Unlock()
		w.WriteHeader(204)
	})}
	captureDone := make(chan struct{})
	go func() { defer close(captureDone); _ = captureServer.Serve(captureLn) }()
	t.Cleanup(func() { _ = captureServer.Close(); <-captureDone })
	path := filepath.Join(t.TempDir(), "services")
	serviceBytes, e := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "telemetry", "url": "https://telemetry.test", "socket": captureSocket, "enabled": true, "mcp": false, "description": "capture"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, serviceBytes, 0600); e != nil {
		t.Fatal(e)
	}
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "host")
	record := filepath.Join(stubDir, "record")
	groupsPath := filepath.Join(stubDir, "groups")
	if e = os.WriteFile(groupsPath, []byte(`{"groups":[]}`+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	script := "#!/bin/sh\nprintf '%s|' \"$@\" >> '" + record + "'\nprintf '\\n' >> '" + record + "'\ncase \"$1 $2\" in\n'group list') IFS= read -r doc < '" + groupsPath + "'; printf '%s\\n' \"$doc\";;\n'user list') printf '{\"users\":[]}\\n';;\n'group members') printf '{\"group\":\"%s\",\"members\":[]}\\n' \"$3\";;\nesac\n"
	if e = writeExecutable(stub, []byte(script)); e != nil {
		t.Fatal(e)
	}
	sudo := filepath.Join(stubDir, "sudo")
	sudoScript := strings.Replace(script, "case \"$1 $2\" in", "shift 2\ncase \"$1 $2\" in", 1)
	if e = writeExecutable(sudo, []byte(sudoScript)); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	var created string
	for i := range 4 {
		signal := os.Signal(syscall.SIGTERM)
		if i == 1 {
			signal = syscall.SIGINT
		}
		socket := filepath.Join(socketDir, "serve"+string(rune('0'+i)))
		ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		ln.SetUnlinkOnClose(false)
		file, err := ln.File()
		if err != nil {
			t.Fatal(err)
		}
		notify := filepath.Join(socketDir, "notify"+string(rune('0'+i)))
		nc, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notify, Net: "unixgram"})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`)
		cmd.Args = append(cmd.Args, bin)
		cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify)
		if i != 3 {
			cmd.Env = append(cmd.Env, services.Variable+"="+path)
		}
		if i == 2 {
			cmd.Env = append(cmd.Env, "PATH="+stubDir)
		} else {
			cmd.Env = append(cmd.Env, "ACCOUNTS_HOST_COMMAND="+stub)
		}
		cmd.Dir = dir
		cmd.ExtraFiles = []*os.File{file}
		var out, errout bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errout
		mu.Lock()
		offset := len(captured)
		mu.Unlock()
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		_ = ln.Close()
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		if err = nc.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 64)
		n, _, err := nc.ReadFromUnix(b)
		_ = nc.Close()
		if err != nil || string(b[:n]) != "READY=1" {
			_ = cmd.Process.Kill()
			childErr := <-exited
			t.Fatalf("ready %q %v child %v stderr %s", b[:n], err, childErr, errout.String())
		}
		info, err := os.Stat(filepath.Join(dir, "state", "accounts.db"))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal(info, err)
		}
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}
		hc := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://accounts/mcp", HTTPClient: hc})
		caller := identity.Caller{UserID: "owner", Email: "mg@example.com", RequestID: "binary-request"}
		result, err := client.CallTool(context.Background(), caller, "groups", nil)
		if err != nil || result.IsError() {
			t.Fatal(result, err)
		}
		rawBytes, err := result.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Meta    map[string]json.RawMessage `json:"_meta"`
			Content tools.GroupList            `json:"structuredContent"`
		}
		if err = json.Unmarshal(rawBytes, &raw); err != nil {
			t.Fatal(err)
		}
		var serverInfo map[string]string
		if err = json.Unmarshal(raw.Meta["io.modelcontextprotocol/serverInfo"], &serverInfo); err != nil || !reflect.DeepEqual(serverInfo, map[string]string{"name": pages.ServiceName, "version": display}) {
			t.Fatal(serverInfo, err)
		}
		if i == 0 && len(raw.Content.Groups) != 0 {
			t.Fatal(raw.Content)
		}
		if i == 1 && (len(raw.Content.Groups) != 1 || raw.Content.Groups[0].Created == "" || raw.Content.Groups[0].Created != created) {
			t.Fatal("persistence", raw.Content, created)
		}
		if i == 0 || i == 2 {
			result, err = client.CallTool(context.Background(), caller, "create_group", json.RawMessage(`{"group":"acme"}`))
			if err != nil || result.IsError() {
				t.Fatal(result, err)
			}
			if i == 0 {
				if err = os.WriteFile(groupsPath, []byte(`{"groups":[{"slug":"acme","members":0}]}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				result, err = client.CallTool(context.Background(), caller, "groups", nil)
				if err != nil || result.IsError() {
					t.Fatal(result, err)
				}
				b, _ := result.MarshalJSON()
				if err = json.Unmarshal(b, &raw); err != nil || len(raw.Content.Groups) != 1 || raw.Content.Groups[0].Created == "" {
					t.Fatal(string(b), err)
				}
				created = raw.Content.Groups[0].Created
			}
		}
		if i == 3 {
			req, err := http.NewRequestWithContext(context.Background(), "GET", "http://accounts/about", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Host = "accounts.example.test"
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-User-Id", caller.UserID)
			req.Header.Set("X-User-Email", caller.Email)
			response, err := hc.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			set, err := pages.Load()
			if err != nil {
				t.Fatal(err)
			}
			banner := page.New(pages.ServiceName, id).Banner(page.User{Email: caller.Email, ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"})
			banner.Trail = []page.Level{pages.AboutLevel}
			want := httptest.NewRecorder()
			set.Write(want, req, 200, "about", pages.AboutData{Banner: banner, Description: pages.Description})
			if response.StatusCode != 200 || !bytes.Equal(body, want.Body.Bytes()) {
				t.Fatal("about identity differs")
			}
		}
		transport.CloseIdleConnections()
		if err = cmd.Process.Signal(signal); err != nil {
			t.Fatal(err)
		}
		select {
		case err = <-exited:
			if err != nil {
				t.Fatal(err, errout.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("child did not stop")
		}
		if out.Len() != 0 {
			t.Fatal(out.String())
		}
		if i != 3 {
			if errout.Len() != 0 {
				t.Fatal(errout.String())
			}
			mu.Lock()
			events := append([]telemetry.Event(nil), captured[offset:]...)
			mu.Unlock()
			if len(events) < 2 || events[0].Name != "service.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"version": display}) {
				t.Fatal(events)
			}
			reason := "SIGTERM"
			if i == 1 {
				reason = "SIGINT"
			}
			last := events[len(events)-1]
			if last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": reason}) {
				t.Fatal(last)
			}
		}
	}
	records, e := os.ReadFile(filepath.Clean(record))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(records), "group|add|acme|\n") || !strings.Contains(string(records), "-n|/usr/local/bin/opsctl|group|add|acme|\n") {
		t.Fatal(string(records))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(records)), "\n") {
		if strings.HasPrefix(line, "-n|") && !strings.HasPrefix(line, "-n|/usr/local/bin/opsctl|") {
			t.Fatal(line)
		}
	}
}

func writeExecutable(path string, data []byte) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	if err = root.WriteFile(name, data, 0600); err != nil {
		return err
	}
	return root.Chmod(name, 0700)
}
