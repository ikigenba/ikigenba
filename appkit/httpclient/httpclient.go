// Package httpclient provides instrumented outbound HTTP clients and transports.
package httpclient

import (
	"appkit/telemetry"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"eventplane/correlation"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultTimeout = 30 * time.Second

// Options configures an instrumented HTTP client or transport.
type Options struct {
	Recorder *telemetry.Recorder
	Timeout  time.Duration
	Base     http.RoundTripper
}

// New returns an HTTP client that records outbound round trips.
func New(opts Options) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &http.Client{
		Transport: NewTransport(opts),
		Timeout:   timeout,
	}
}

// NewTransport returns a RoundTripper that records outbound round trips.
func NewTransport(opts Options) http.RoundTripper {
	base := opts.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base, recorder: opts.Recorder}
}

type transport struct {
	base     http.RoundTripper
	recorder *telemetry.Recorder
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	started := time.Now()
	outbound := req.Clone(req.Context())
	if isLoopbackLiteral(outbound.URL.Hostname()) {
		outbound.Header.Set(correlation.Header, correlation.FromContext(outbound.Context()))
	} else {
		outbound.Header.Del(correlation.Header)
	}

	requestCounter := newDigestReader(outbound.Body)
	if outbound.Body != nil {
		outbound.Body = requestCounter
	}

	response, err := t.base.RoundTrip(outbound)
	if err != nil {
		t.recorder.Record(telemetry.Record{
			CorrelationID: correlation.FromContext(req.Context()),
			Kind:          telemetry.KindOutbound,
			Op:            operation(req),
			Outcome: &telemetry.Outcome{
				Status:     "error",
				Error:      errorClass(err),
				DurationMS: time.Since(started).Milliseconds(),
			},
			Detail: requestCounter.detail(),
		})
		return response, err
	}

	responseCounter := newDigestReader(response.Body)
	response.Body = &recordingBody{
		ReadCloser: responseCounter,
		once:       sync.Once{},
		record: func() {
			responseBytes, responseSHA := responseCounter.sum()
			t.recorder.Record(telemetry.Record{
				CorrelationID: correlation.FromContext(req.Context()),
				Kind:          telemetry.KindOutbound,
				Op:            operation(req),
				Outcome: &telemetry.Outcome{
					Status:     strconv.Itoa(response.StatusCode),
					DurationMS: time.Since(started).Milliseconds(),
					Bytes:      responseBytes,
					SHA256:     responseSHA,
				},
				Detail: requestCounter.detail(),
			})
		},
	}
	return response, nil
}

func operation(req *http.Request) string {
	return req.Method + " " + req.URL.Host + req.URL.EscapedPath()
}

func isLoopbackLiteral(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func errorClass(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() || os.IsTimeout(err) {
		return "timeout"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_refused"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	return "source_unavailable"
}

type digestReader struct {
	io.ReadCloser
	hash  hash.Hash
	bytes int
}

func newDigestReader(body io.ReadCloser) *digestReader {
	if body == nil {
		body = io.NopCloser(strings.NewReader(""))
	}
	return &digestReader{ReadCloser: body, hash: sha256.New()}
}

func (r *digestReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.bytes += n
		_, _ = r.hash.Write(p[:n])
	}
	return n, err
}

func (r *digestReader) sum() (int, string) {
	return r.bytes, hex.EncodeToString(r.hash.Sum(nil))
}

func (r *digestReader) detail() map[string]any {
	bytes, digest := r.sum()
	return map[string]any{"request_bytes": bytes, "request_sha256": digest}
}

type recordingBody struct {
	io.ReadCloser
	once   sync.Once
	record func()
}

func (b *recordingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.record)
	return err
}
