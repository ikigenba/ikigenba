package sim

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

type transport struct {
	handler http.Handler
	binding string
}

// RoundTrip joins the client and handler with a synchronous pipe. The pipe
// provides backpressure without accumulating a response in memory.
func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	request := req.Clone(ctx)
	request.Host = t.binding
	request.RequestURI = req.URL.RequestURI()
	reader, writer := io.Pipe()
	ready := make(chan *http.Response, 1)
	response := &responseWriter{
		header:  make(http.Header),
		ready:   ready,
		pipe:    writer,
		request: req,
		body:    responseBody{reader: reader, cancel: cancel},
	}
	stop := context.AfterFunc(ctx, func() { _ = reader.CloseWithError(ctx.Err()) })
	go func() {
		defer stop()
		defer func() { _ = writer.Close() }()
		if request.Body != nil {
			defer func() { _ = request.Body.Close() }()
		}
		t.handler.ServeHTTP(response, request)
		response.WriteHeader(http.StatusOK)
	}()
	select {
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	case result := <-ready:
		return result, nil
	}
}

type responseBody struct {
	reader *io.PipeReader
	cancel context.CancelFunc
}

func (b responseBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b responseBody) Close() error {
	b.cancel()
	return b.reader.Close()
}

type responseWriter struct {
	header  http.Header
	ready   chan<- *http.Response
	pipe    *io.PipeWriter
	request *http.Request
	body    io.ReadCloser
	started bool
}

func (w *responseWriter) Header() http.Header { return w.header }

func (w *responseWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	w.started = true
	w.ready <- &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Header:        w.header.Clone(),
		Body:          w.body,
		ContentLength: -1,
		Request:       w.request,
	}
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.pipe.Write(p)
}

func (w *responseWriter) Flush() { w.WriteHeader(http.StatusOK) }
