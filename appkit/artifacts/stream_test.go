package artifacts_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/services"
)

// R-PIBF-B3IJ R-ZECW-DQUU
func TestPutStreamsBeforeEOF(t *testing.T) {
	received := make(chan string, 1)
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 5)
		_, err := io.ReadFull(r.Body, buf)
		if err != nil {
			t.Error(err)
		}
		received <- string(buf)
		tail, err := io.ReadAll(r.Body)
		if err != nil || string(tail) != "tail" {
			t.Error("tail changed", err)
		}
		_, _ = io.WriteString(w, metadataJSON())
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	reader, pipe := io.Pipe()
	t.Cleanup(func() { _ = reader.Close(); _ = pipe.Close() })
	result := make(chan error, 1)
	go func() { _, err := c.Put(context.Background(), "stream", artifacts.PutOptions{}, reader); result <- err }()
	if _, err := io.WriteString(pipe, "first"); err != nil {
		t.Fatal(err)
	}
	if got := <-received; got != "first" {
		t.Fatal(got)
	}
	if _, err := io.WriteString(pipe, "tail"); err != nil {
		t.Fatal(err)
	}
	if err := pipe.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }

// R-ZFKS-RILJ R-PENQ-5SAG R-ZECW-DQUU
func TestPutReadFailure(t *testing.T) {
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, metadataJSON())
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	failure := errors.New("source failed")
	body := &trackingBody{Reader: failedReader{failure}}
	m, err := c.Put(context.Background(), "p", artifacts.PutOptions{}, body)
	if err == nil || m != (artifacts.Metadata{}) || body.closes != 0 {
		t.Fatal(m, err, body)
	}
}

// R-PJJB-OV98 R-ZO43-FWSE
func TestGetStreamsAndCloseReleasesConnection(t *testing.T) {
	observedClose := make(chan struct{})
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		headers(w, 100)
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(observedClose)
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	m, body, err := c.Get(context.Background(), "stream")
	if err != nil {
		t.Fatal(err)
	}
	if m.Bytes != 100 || body == nil {
		t.Fatal(m)
	}
	part := make([]byte, 5)
	if _, err := io.ReadFull(body, part); err != nil || string(part) != "first" {
		t.Fatal(string(part), err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	<-observedClose
}

// R-7B3B-TN32 R-PN70-U6HB R-ZMW7-251P R-PENQ-5SAG
func TestUnreturnedBodiesAreClosed(t *testing.T) {
	for _, method := range []string{"PUT", "GET"} {
		t.Run(method, func(t *testing.T) {
			failed := make(chan error, 1)
			entry := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(500)
				_, err := io.CopyN(w, strings.NewReader(strings.Repeat("x", 64<<20)), 64<<20)
				failed <- err
			})
			w, _ := writer(t)
			c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
			_, err := invoke(context.Background(), c, method, "p")
			var status *artifacts.HTTPError
			if !errors.As(err, &status) || len(status.Body) != 4096 {
				t.Fatal(err)
			}
			if err := <-failed; err == nil {
				t.Fatal("server finished oversized body")
			}
		})
	}
	// Invalid successful headers also surrender their unread stream.
	done := make(chan struct{})
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(done)
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	_, body, err := c.Get(context.Background(), "p")
	plainError(t, err)
	if body != nil {
		t.Fatal("body returned on error")
	}
	<-done
}

// R-00B3-9M7C R-01IZ-NDY1
func TestProcessIsolation(t *testing.T) {
	dir := t.TempDir()
	stdout, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdout.Close() }()
	stderr, err := os.CreateTemp(dir, "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stderr.Close() }()
	oldOut, oldErr, oldLog := os.Stdout, os.Stderr, log.Writer()
	var logs bytes.Buffer
	os.Stdout, os.Stderr = stdout, stderr
	log.SetOutput(&logs)
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; log.SetOutput(oldLog) }()
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, metadataJSON())
		} else {
			headers(w, 0)
		}
	})
	w, _ := writer(t)
	// Absolute service sockets work before and after a change of directory.
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	check := func(c *artifacts.Client) {
		for _, method := range []string{"PUT", "GET", "HEAD"} {
			m, err := invoke(context.Background(), c, method, "p")
			if err != nil {
				t.Fatal(err)
			}
			if method == "PUT" {
				if m.ID != "artifact-id" {
					t.Fatal(m)
				}
			} else if m.Path != "p" {
				t.Fatal(m)
			}
		}
		for _, e := range sentinels() {
			_ = e.Error()
		}
		_ = (&artifacts.HTTPError{500, ""}).Error()
		for _, method := range []string{"PUT", "GET", "HEAD"} {
			_, err := invoke(context.Background(), c, method, "bad path")
			if !errors.Is(err, artifacts.ErrBadPath) {
				t.Fatal(err)
			}
		}
	}
	check(c)
	t.Chdir(t.TempDir())
	check(c)
	// A relative socket resolves from the working directory at connection time.
	t.Chdir(filepath.Dir(entry.Socket))
	entry.Socket = filepath.Base(entry.Socket)
	relative := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	check(relative)
	missing := artifacts.NewClient(artifacts.ClientConfig{services.Entry{Name: "artifacts", Socket: filepath.Join(dir, "missing")}, w})
	for _, method := range []string{"PUT", "GET", "HEAD"} {
		_, err := invoke(context.Background(), missing, method, "p")
		plainError(t, err)
	}
	for _, f := range []*os.File{stdout, stderr} {
		stat, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if stat.Size() != 0 {
			t.Fatal("process stream written", f.Name())
		}
	}
	if logs.Len() != 0 {
		t.Fatal("default logger written")
	}
}
