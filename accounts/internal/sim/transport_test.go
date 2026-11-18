package sim

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case result := <-ch:
		return result
	case <-timer.C:
		t.Fatal("timed out waiting for simulator")
		var zero T
		return zero
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamingTransport(t *testing.T) {
	// R-VCHK-CE82
	const host = "own.test:4302"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	payload := bytes.Repeat([]byte("sample-bytes"), 8192)
	written := make(chan struct{})
	exited := make(chan error, 1)
	client := &http.Client{Transport: transport{
		binding: host,
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != host || r.Method != http.MethodPost || r.URL.RequestURI() != "/endpoint?part=3" {
				t.Errorf("request = %s %s %s", r.Host, r.Method, r.URL)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != "request bytes" {
				t.Errorf("request body = %q, %v", body, err)
			}
			w.Header().Add("X-Test", "one")
			w.Header().Add("X-Test", "two")
			w.WriteHeader(http.StatusAccepted)
			_, err = w.Write(payload)
			if err != nil {
				t.Error(err)
			}
			close(written)
			<-r.Context().Done()
			exited <- r.Context().Err()
		}),
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unreachable.test/endpoint?part=3", strings.NewReader("request bytes"))
	must(t, err)
	responses := make(chan *http.Response, 1)
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			t.Error(err)
		}
		responses <- resp
	}()
	resp := await(t, responses)
	if resp == nil {
		t.Fatal("missing response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted || strings.Join(resp.Header.Values("X-Test"), " ") != "one two" {
		t.Fatalf("response = %d %v", resp.StatusCode, resp.Header)
	}
	select {
	case <-written:
		t.Fatal("write completed before the client read any response bytes")
	default:
	}
	got := make([]byte, len(payload))
	_, err = io.ReadFull(resp.Body, got)
	must(t, err)
	if !bytes.Equal(got, payload) {
		t.Fatal("response body changed")
	}
	await(t, written)
	cancel()
	if err := await(t, exited); !errors.Is(err, context.Canceled) {
		t.Fatalf("handler context = %v", err)
	}
}
