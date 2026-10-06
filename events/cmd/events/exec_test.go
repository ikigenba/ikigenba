package main_test

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

	"github.com/ikigenba/ikigenba/events/internal/cli"
)

// R-Q0YV-7I42 R-06XG-JBD4 R-6KA0-OOQW R-6LHX-2GHL R-6MPT-G88A
// R-6NXP-TZYZ R-6P5M-7RPO R-6QDI-LJGD R-C0NJ-9HL4
func TestBinaryWiring(t *testing.T) {
	t.Setenv("IKIGENBA_SERVICES", "")
	binary := filepath.Join(t.TempDir(), "events")
	build := exec.Command("go", "build")
	build.Args = append(build.Args, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, tc := range []struct {
		args     []string
		out, err string
		code     int
	}{
		{[]string{"--version"}, cli.Version + "\n", "", cli.ExitSuccess},
		{[]string{"manifest"}, cli.Manifest, "", cli.ExitSuccess},
		{[]string{"bogus"}, "", "events: unknown command 'bogus'\n\nsee 'events --help' for usage\n", cli.ExitUsage},
		{nil, "", "events: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
	} {
		command := &exec.Cmd{Path: binary, Args: append([]string{binary}, tc.args...)}
		command.Env = []string{}
		command.Dir = t.TempDir()
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != tc.code || stdout.String() != tc.out || stderr.String() != tc.err {
			t.Fatalf("args %v: code %d stdout %q stderr %q", tc.args, code, stdout.String(), stderr.String())
		}
	}
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			short, err := os.MkdirTemp("", "binary-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(short) })
			socket := filepath.Join(short, "events.sock")
			notifyPath := filepath.Join(short, "notify.sock")
			telemetryPath := filepath.Join(short, "trail.sock")
			for _, path := range []string{socket, notifyPath, telemetryPath} {
				if len(path) >= 107 {
					t.Fatalf("socket path too long: %s", path)
				}
			}
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = notify.Close() })
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			inherited, err := listener.File()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = inherited.Close() })
			trailListener, err := net.Listen("unix", telemetryPath)
			if err != nil {
				t.Fatal(err)
			}
			type event struct {
				Service   string         `json:"service"`
				Name      string         `json:"event"`
				RequestID string         `json:"request_id"`
				User      string         `json:"user"`
				Attrs     map[string]any `json:"attrs"`
			}
			var mutex sync.Mutex
			var trail []event
			trailServer := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/ingest" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("trail body: %v", err)
					w.WriteHeader(500)
					return
				}
				var record event
				if err := json.Unmarshal(body, &record); err != nil {
					t.Errorf("trail decode: %v", err)
					w.WriteHeader(500)
					return
				}
				mutex.Lock()
				trail = append(trail, record)
				mutex.Unlock()
				w.WriteHeader(http.StatusNoContent)
			})}
			go func() { _ = trailServer.Serve(trailListener) }()
			t.Cleanup(func() { _ = trailServer.Close() })
			servicesPath := filepath.Join(short, "services.json")
			services, err := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "telemetry", "enabled": true, "socket": telemetryPath, "url": "", "description": "", "mcp": false}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(servicesPath, services, 0600); err != nil {
				t.Fatal(err)
			}
			working := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "/bin/sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"`)
			command.Args = append(command.Args, binary)
			command.Env = []string{"IKIGENBA_SERVICES=" + servicesPath, "NOTIFY_SOCKET=" + notifyPath}
			command.Dir = working
			command.ExtraFiles = []*os.File{inherited}
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var processErr error
			go func() { processErr = command.Wait(); close(done) }()
			t.Cleanup(func() {
				cancel()
				<-done
			})
			if err := notify.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var notification [1024]byte
			size, _, err := notify.ReadFromUnix(notification[:])
			if err != nil {
				t.Fatalf("readiness: %v", err)
			}
			if string(notification[:size]) != "READY=1" {
				t.Fatalf("notification %q", notification[:size])
			}
			info, err := os.Stat(filepath.Join(working, "state", "events.db"))
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("database not created: %v", err)
			}
			if err := command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if processErr != nil {
					t.Fatalf("signal %s: %v", signal, processErr)
				}
			case <-ctx.Done():
				t.Fatal("child failed to exit")
			}
			if _, err := os.Stat(socket); err != nil {
				t.Fatalf("inherited socket removed: %v", err)
			}
			queued, err := net.DialTimeout("unix", socket, time.Second)
			if err != nil {
				t.Fatalf("inherited socket no longer queues: %v", err)
			}
			if err := listener.SetDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			accepted, err := listener.AcceptUnix()
			if err != nil {
				t.Fatalf("parent duplicate stopped accepting: %v", err)
			}
			_ = accepted.Close()
			_ = queued.Close()
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("streams stdout %q stderr %q", stdout.String(), stderr.String())
			}
			mutex.Lock()
			records := append([]event(nil), trail...)
			mutex.Unlock()
			started := -1
			for index, record := range records {
				if record.Service != "events" {
					t.Fatalf("service %#v", record)
				}
				if record.Name == "service.started" {
					started = index
					break
				}
				if record.Name != "sibling.called" {
					t.Fatalf("event before start %#v", record)
				}
			}
			if started < 0 {
				t.Fatalf("no started event: %#v", records)
			}
			first := records[started]
			if first.RequestID != "" || first.User != "" || !reflect.DeepEqual(first.Attrs, map[string]any{"version": cli.Version}) {
				t.Fatalf("started %#v", first)
			}
			last := records[len(records)-1]
			reason := "SIGTERM"
			if signal == syscall.SIGINT {
				reason = "SIGINT"
			}
			if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, map[string]any{"reason": reason}) {
				t.Fatalf("last %#v", last)
			}
		})
	}
}
