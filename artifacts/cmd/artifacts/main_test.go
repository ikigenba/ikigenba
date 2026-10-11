package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/version"
	artifacts "github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/cli"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
)

func socketDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "art-bin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

type binaryRun struct {
	cmd      *exec.Cmd
	done     chan error
	out, err bytes.Buffer
	client   *http.Client
	stopped  bool
}

func startBinary(t *testing.T, binary, dir string, env []string) *binaryRun {
	t.Helper()
	sockets := socketDirectory(t)
	path := filepath.Join(sockets, "app")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	defer func() { _ = ln.Close() }()
	file, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sockets, "ready"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notify.Close() }()
	b := &binaryRun{done: make(chan error, 1)}
	b.cmd = exec.Command("/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`)
	b.cmd.Args = append(b.cmd.Args, binary)
	b.cmd.Dir = dir
	b.cmd.Env = append(append([]string{}, env...), "NOTIFY_SOCKET="+notify.LocalAddr().String())
	b.cmd.ExtraFiles = []*os.File{file}
	b.cmd.Stdout, b.cmd.Stderr = &b.out, &b.err
	if err := b.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { b.done <- b.cmd.Wait() }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	b.client = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		if !b.stopped {
			_ = b.cmd.Process.Kill()
			select {
			case <-b.done:
			case <-time.After(5 * time.Second):
				t.Error("child did not stop")
			}
		}
	})
	if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 128)
	n, _, err := notify.ReadFromUnix(data)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if string(data[:n]) != "READY=1" {
		t.Fatalf("readiness %q", data[:n])
	}
	return b
}
func (b *binaryRun) stop(t *testing.T, signal syscall.Signal) {
	t.Helper()
	if err := b.cmd.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-b.done:
		b.stopped = true
		if err != nil || b.out.String() != "" {
			t.Fatalf("stop: %v stdout=%q stderr=%q", err, b.out.String(), b.err.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not stop")
	}
}
func binaryRequest(t *testing.T, b *binaryRun, method, path string, body io.Reader) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, "http://artifacts.sbx.ikigenba.dev"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "binary-owner")
	r.Header.Set("X-User-Email", "mg@example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	res, err := b.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, data
}

type receivedEvent struct {
	Name  string         `json:"event"`
	Time  time.Time      `json:"time"`
	Attrs map[string]any `json:"attrs"`
}

// R-ZD7R-ACRX R-ZEFN-O4IM R-ZFNK-1W9B R-ZGVG-FO00
// R-ZI3C-TFQP R-ZJB9-77HE R-ZKJ5-KZ83
func TestBinary(t *testing.T) {
	t.Setenv(services.Variable, "")
	t.Setenv(version.CommitVariable, "abcdef1234567890")
	t.Setenv(version.ReleaseVariable, "binary-test-release")
	id := version.Read()
	display := id.String()
	env := []string{version.CommitVariable + "=abcdef1234567890", version.ReleaseVariable + "=binary-test-release"}
	binary := filepath.Join(t.TempDir(), "artifacts")
	build := exec.Command("go", "build")
	build.Args = append(build.Args, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, tc := range []struct{ args, env []string }{
		{[]string{"--version"}, env}, {[]string{"--version"}, nil}, {[]string{"manifest"}, env}, {[]string{"bogus"}, env}, {nil, env},
	} {
		var gotOut, gotErr, wantOut, wantErr bytes.Buffer
		// The executable is the absolute path just built in the test-owned directory.
		command := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		command.Env = tc.env
		// A non-nil empty environment never inherits the parent's variables.
		if command.Env == nil {
			command.Env = []string{}
		}
		command.Dir = t.TempDir()
		command.Stdout, command.Stderr = &gotOut, &gotErr
		err := command.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		v := display
		if tc.env == nil {
			v = ""
		}
		wantCode := cli.Run(context.Background(), cli.Process{Args: tc.args, LookupEnv: func(string) (string, bool) { return "", false }, Stdout: &wantOut, Stderr: &wantErr, Version: v, Dir: command.Dir, Inherit: func(uintptr) (net.Listener, error) {
			t.Fatal("unexpected listener")
			return nil, errors.New("unexpected listener")
		}})
		if code != wantCode || gotOut.String() != wantOut.String() || gotErr.String() != wantErr.String() {
			t.Fatalf("args=%v exit=%d/%d stdout=%q/%q stderr=%q/%q", tc.args, code, wantCode, gotOut.String(), wantOut.String(), gotErr.String(), wantErr.String())
		}
		if tc.env == nil && gotOut.String() != "\n" {
			t.Fatalf("empty identity output %q", gotOut.String())
		}
	}
	sockets := socketDirectory(t)
	sinkPath := filepath.Join(sockets, "sink")
	ln, err := net.Listen("unix", sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var trail []receivedEvent
	var bus []events.Event
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		if r.URL.Path == "/source" {
			_, _ = w.Write([]byte("import bytes"))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/ingest":
			var e receivedEvent
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				t.Error(err)
			}
			trail = append(trail, e)
		case events.EmitPath:
			var e events.Event
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				t.Error(err)
			}
			bus = append(bus, e)
		default:
			t.Errorf("unexpected sink path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case err := <-serverDone:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("sink did not stop")
		}
	})
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	entries := []map[string]any{}
	for _, name := range []string{"telemetry", "events", "scripts"} {
		entries = append(entries, map[string]any{"name": name, "url": "https://" + name + ".sbx.ikigenba.dev", "socket": sinkPath, "description": "test sink", "enabled": true, "mcp": false})
	}
	document, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicesPath, document, 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// R-XO1A-6QM3: the binary leaves Inherit, Now and Rand nil; serving
	// through its passed descriptor and observing its ids and timestamps
	// exercises those defaults without using the test process's resources.
	before := time.Now()
	first := startBinary(t, binary, dir, append(append([]string{}, env...), services.Variable+"="+servicesPath))
	for _, path := range []string{"state/artifacts.db", "state/blobs"} {
		info, err := os.Stat(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if path == "state/blobs" && !info.IsDir() || path == "state/artifacts.db" && !info.Mode().IsRegular() {
			t.Fatalf("unexpected state %s %v", path, info)
		}
	}
	payload := []byte{0, 1, 2, 255, 10, 13}
	status, data := binaryRequest(t, first, "PUT", "/files/persist.bin", bytes.NewReader(payload))
	if status != 201 {
		t.Fatalf("PUT %d %s", status, data)
	}
	var meta files.Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if !store.ValidID(meta.ID) {
		t.Fatalf("default random artifact id %q", meta.ID)
	}
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://artifacts/mcp", HTTPClient: first.client})
	caller := identity.Caller{UserID: "binary-owner", Email: "mg@example.com"}
	result, err := client.CallTool(context.Background(), caller, "list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Meta map[string]map[string]any `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(answer.Meta["io.modelcontextprotocol/serverInfo"], map[string]any{"name": pages.ServiceName, "version": display}) {
		t.Fatalf("serverInfo %s", raw)
	}
	result, err = client.CallTool(context.Background(), caller, "import", json.RawMessage(`{"path":"import.bin","url":"https://scripts.sbx.ikigenba.dev/source"}`))
	if err != nil || result.IsError() {
		t.Fatalf("default import timer: %v %v", result, err)
	}
	imported, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var importAnswer struct {
		Metadata files.Metadata `json:"structuredContent"`
	}
	if err := json.Unmarshal(imported, &importAnswer); err != nil {
		t.Fatal(err)
	}
	if !store.ValidID(importAnswer.Metadata.ID) || importAnswer.Metadata.ID == meta.ID {
		t.Fatalf("default random ids %q and %q", meta.ID, importAnswer.Metadata.ID)
	}
	first.stop(t, syscall.SIGTERM)
	if first.err.String() != "" {
		t.Fatalf("stderr %q", first.err.String())
	}
	after := time.Now()
	created, err := time.Parse(time.RFC3339, meta.Created)
	if err != nil || created.Before(before.Truncate(time.Second)) || created.After(after) {
		t.Fatalf("default clock created=%q %v", meta.Created, err)
	}
	mu.Lock()
	if len(trail) < 2 || trail[0].Name != "service.started" || !reflect.DeepEqual(trail[0].Attrs, map[string]any{"version": display}) {
		t.Errorf("started %#v", trail)
	}
	if len(trail) > 0 {
		last := trail[len(trail)-1]
		if last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, map[string]any{"reason": "SIGTERM"}) {
			t.Errorf("stopping %#v", last)
		}
	}
	if len(bus) != 2 {
		t.Errorf("default event sink got %d events", len(bus))
	}
	for _, event := range trail {
		if event.Time.Before(before) || event.Time.After(after) {
			t.Errorf("default telemetry clock %v", event.Time)
		}
	}
	for _, event := range bus {
		if event.Time.Before(before) || event.Time.After(after) {
			t.Errorf("default bus clock %v", event.Time)
		}
	}
	mu.Unlock()
	second := startBinary(t, binary, dir, env)
	status, data = binaryRequest(t, second, "GET", "/files/persist.bin", nil)
	if status != 200 || !bytes.Equal(data, payload) {
		t.Fatalf("persisted GET %d %q", status, data)
	}
	status, data = binaryRequest(t, second, "GET", "/about", nil)
	templates, err := page.Templates().ParseFS(artifacts.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	banner := page.New(pages.ServiceName, id).Banner(page.User{Email: "mg@example.com", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
	banner.Trail = []page.Level{pages.AboutLevel}
	var about bytes.Buffer
	if err := templates.ExecuteTemplate(&about, "about", pages.AboutData{Description: pages.Description, Banner: banner}); err != nil {
		t.Fatal(err)
	}
	if status != 200 || string(data) != about.String() {
		t.Fatalf("about status=%d body differs from template", status)
	}
	second.stop(t, syscall.SIGINT)
	third := startBinary(t, binary, dir, append(append([]string{}, env...), services.Variable+"="+servicesPath))
	third.stop(t, syscall.SIGINT)
	if third.err.String() != "" {
		t.Fatalf("stderr %q", third.err.String())
	}
	mu.Lock()
	defer mu.Unlock()
	last := trail[len(trail)-1]
	if last.Name != "service.stopping" || !reflect.DeepEqual(last.Attrs, map[string]any{"reason": "SIGINT"}) {
		t.Fatalf("interrupt stopping %#v", last)
	}
}
