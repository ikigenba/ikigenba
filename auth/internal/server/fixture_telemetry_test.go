package server

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func testServerWriter(t *testing.T, stderr io.Writer) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	writer := telemetry.New(telemetry.Config{Service: "auth", Sink: capture, Stderr: stderr, Now: fixedNow, Rand: bytes.NewReader(bytes.Repeat([]byte{23}, 1<<20))})
	t.Cleanup(func() {
		if err := writer.Flush(context.Background()); err != nil {
			t.Error(err)
		}
		for _, event := range capture.Events() {
			if event.Service != "auth" {
				t.Errorf("event service=%q, want auth", event.Service)
			}
		}
		writer.Shutdown(context.Background(), "test complete")
	})
	return writer, capture
}

func newTestServer(t *testing.T, cfg Config, stderr ...io.Writer) *Server {
	t.Helper()
	if cfg.Now == nil {
		cfg.Now = fixedNow
	}
	if cfg.Rand == nil {
		cfg.Rand = &signInRand{next: 1}
	}
	output := io.Writer(&bytes.Buffer{})
	if len(stderr) != 0 {
		output = stderr[0]
	}
	writer, _ := testServerWriter(t, output)
	cfg.Telemetry = writer
	return New(cfg)
}

func newStatusTestServer(t *testing.T, status int, cfg Config, stderr ...io.Writer) *Server {
	t.Helper()
	if cfg.Now == nil {
		cfg.Now = fixedNow
	}
	if cfg.Rand == nil {
		cfg.Rand = &signInRand{next: 1}
	}
	output := io.Writer(&bytes.Buffer{})
	if len(stderr) != 0 {
		output = stderr[0]
	}
	writer, capture := testServerWriter(t, output)
	cfg.Telemetry = writer
	t.Cleanup(func() {
		if err := writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		finished := 0
		for _, event := range capture.Events() {
			if event.Name == "request.finished" {
				finished++
				if event.Attrs["status"] != int64(status) {
					t.Errorf("request.finished status=%v, want %d", event.Attrs["status"], status)
				}
			}
		}
		if finished == 0 {
			t.Error("no request.finished event recorded")
		}
	})
	return New(cfg)
}

func singlePlainLine(body string) bool {
	line := strings.TrimSuffix(strings.TrimSuffix(body, "\n"), "\r")
	return line != "" && !strings.ContainsAny(line, "\r\n") && strings.HasSuffix(body, "\n")
}
