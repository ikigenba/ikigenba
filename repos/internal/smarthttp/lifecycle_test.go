package smarthttp_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Hold the response writer after bytes have reached its client. The final
// return from Write is deliberately independent of Git's exit and timer.
type lifecycleWriter struct {
	http.ResponseWriter
	ctx             context.Context
	limit, accepted int
	ready, resume   chan struct{}
	once            sync.Once
	writeErr        error
	flushErr        error
}

func (w *lifecycleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *lifecycleWriter) FlushError() error {
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil {
		return err
	}
	if w.accepted == w.limit {
		return w.flushErr
	}
	return nil
}

func (w *lifecycleWriter) Write(p []byte) (int, error) {
	if left := w.limit - w.accepted; len(p) > left {
		p = p[:left]
	}
	n, err := w.ResponseWriter.Write(p)
	w.accepted += n
	if err != nil {
		return n, err
	}
	if err = http.NewResponseController(w.ResponseWriter).Flush(); err != nil {
		return n, err
	}
	if w.accepted == w.limit {
		w.once.Do(func() {
			close(w.ready)
			select {
			case <-w.resume:
			case <-w.ctx.Done():
			}
		})
	}
	if w.accepted == w.limit {
		return n, w.writeErr
	}
	return n, nil
}

func TestFetchEventUsesExitAndActualOutput(t *testing.T) {
	// R-0H17-6FS7
	for _, test := range []struct {
		name               string
		full               bool
		writeErr, flushErr error
	}{
		{name: "complete-output-late-timer", full: true},
		{name: "partial-output-natural-exit"},
		{name: "complete-output-write-error", full: true, writeErr: io.ErrClosedPipe},
		{name: "partial-output-write-error", writeErr: io.ErrClosedPipe},
		{name: "complete-output-flush-error", full: true, flushErr: io.ErrClosedPipe},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			trace := filepath.Join(f.root, "lifecycle-trace.json")
			f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
			ready, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var release sync.Once
			ctx := deadline(t)
			limit := 4
			if !test.full {
				limit = 2
			}
			handler := f.handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				handler.ServeHTTP(&lifecycleWriter{ResponseWriter: w, ctx: ctx, limit: limit, ready: ready, resume: resume, writeErr: test.writeErr, flushErr: test.flushErr}, r)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(func() { release.Do(func() { close(resume) }) })
			body := packet("command=ls-refs\n") + "0001" + packet("peel\n") + packet("symrefs\n") + "0000"
			request, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/notes.git/git-upload-pack", strings.NewReader(body))
			must(t, err)
			request.Header.Set("X-User-Id", "alice")
			request.Header.Set("X-Request-Id", "lifecycle")
			request.Header.Set("Git-Protocol", "version=2")
			request.Header.Set("Content-Type", "application/x-git-upload-pack-request")
			response, err := server.Client().Do(request)
			must(t, err)
			t.Cleanup(func() { _ = response.Body.Close() })
			same(t, response.StatusCode, http.StatusOK)
			answer := make([]byte, limit)
			_, err = io.ReadFull(response.Body, answer)
			must(t, err)
			same(t, string(answer), "0000"[:limit])
			streamTake(t, ready)
			timer := f.clock.take(t)
			sid := streamBackend(t, streamTrace(t, trace))
			streamAwaitNaturalExit(t, trace, sid)
			if test.full {
				// All four body bytes reached the client before delivery, while
				// the response writer still holds the handler's final Write.
				timer.fire <- epoch
			}
			release.Do(func() { close(resume) })
			streamTake(t, done)
			_, _ = io.ReadAll(response.Body)
			var fetched []telemetry.Event
			for _, event := range f.events() {
				if event.RequestID == "lifecycle" && event.Name == "repo.fetched" {
					fetched = append(fetched, event)
				}
			}
			if test.full {
				same(t, len(fetched), 1)
				same(t, fetched[0].Attrs, telemetry.Attrs{"repo": repo.ID, "bytes": int64(0)})
			} else {
				same(t, len(fetched), 0)
			}
			assertIdle(t, f, repo.ID)
		})
	}
}
