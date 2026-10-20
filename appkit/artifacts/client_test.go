package artifacts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

var fixedTime = time.Date(2028, 2, 3, 4, 5, 6, 0, time.UTC)

func writer(t *testing.T) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	var diagnostics bytes.Buffer
	w := telemetry.New(telemetry.Config{Service: "connector", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return fixedTime }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 64)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() {
		w.Shutdown(context.Background(), "done")
		if diagnostics.Len() != 0 {
			t.Error(diagnostics.String())
		}
	})
	return w, capture
}
func serve(t *testing.T, h http.HandlerFunc) services.Entry {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: h, ReadHeaderTimeout: time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	file := filepath.Join(dir, "services.json")
	data, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": "artifacts-2", "url": "https://example.invalid///", "description": "test", "socket": socket, "enabled": true, "mcp": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(services.Variable, file)
	list, err := services.Read(os.Getenv(services.Variable))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := list.Find("artifacts-2")
	if !ok {
		t.Fatal("missing entry")
	}
	return entry
}
func headers(w http.ResponseWriter, n int) {
	w.Header().Set("Content-Length", fmt.Sprint(n))
	w.Header().Set("ETag", `"digest"`)
	w.Header().Set("Last-Modified", fixedTime.Format(http.TimeFormat))
	w.Header().Set("Content-Type", "application/test")
}
func metadataJSON() string {
	return `{"id":"artifact-id","path":"saved/path","bytes":7,"content_type":"binary/test","sha256":"sum","owner":"owner","created":"2028-02-03T06:05:06+02:00","modified":"2028-02-03T04:05:06Z","url":"https://file.invalid","future":{"nested":true}}`
}
func invoke(ctx context.Context, c *artifacts.Client, method, path string) (artifacts.Metadata, error) {
	switch method {
	case "PUT":
		return c.Put(ctx, path, artifacts.PutOptions{}, nil)
	case "HEAD":
		return c.Head(ctx, path)
	default:
		m, b, err := c.Get(ctx, path)
		if b != nil {
			_ = b.Close()
		}
		return m, err
	}
}
func sentinels() []error {
	return []error{artifacts.ErrBadPath, artifacts.ErrForbidden, artifacts.ErrNotFound, artifacts.ErrTooLarge, artifacts.ErrUnavailable}
}
func plainError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("missing error")
	}
	var status *artifacts.HTTPError
	if errors.As(err, &status) {
		t.Fatal(err)
	}
	for _, sentinel := range sentinels() {
		if errors.Is(err, sentinel) {
			t.Fatal(err)
		}
	}
}

// R-ZXVA-I2PY R-YSEP-HVIC R-YTML-VN91 R-P8K8-8XKZ
func TestConstructor(t *testing.T) {
	func() {
		defer func() {
			if !strings.Contains(fmt.Sprint(recover()), "telemetry writer is nil") {
				t.Error("wrong panic")
			}
		}()
		artifacts.NewClient(artifacts.ClientConfig{})
		t.Error("no panic")
	}()
	w, _ := writer(t)
	for _, entry := range []services.Entry{{}, {Name: "?", URL: "%%%", Socket: "\x00"}, {Socket: filepath.Join(t.TempDir(), "missing")}} {
		cfg := artifacts.ClientConfig{entry, w}
		c := artifacts.NewClient(cfg)
		if c == nil {
			t.Fatal("nil client")
		}
	}
}

// R-PB01-0H2D R-Z25W-K1FW R-ZSZO-YZR6
func TestErrorTypes(t *testing.T) {
	for i, a := range sentinels() {
		if a == nil || !strings.HasPrefix(a.Error(), "artifacts: ") {
			t.Fatal(a)
		}
		for j, b := range sentinels() {
			if errors.Is(a, b) != (i == j) {
				t.Fatal("sentinels overlap")
			}
		}
	}
	e := &artifacts.HTTPError{418, "body"}
	if e.StatusCode != 418 || e.Body != "body" || e.Error() != "artifacts: unexpected HTTP response (status 418)" {
		t.Fatal(e)
	}
}

// R-YUUI-9EZQ R-YW2E-N6QF R-YXAB-0YH4 R-P9S4-MPBO R-Z5TL-PCNZ R-Z9HA-UNW2 R-ZBX3-M7DG R-ZGSP-5AC8 R-6IIT-3FUX
func TestWireAndTrail(t *testing.T) {
	type request struct {
		method, target, host string
		header               http.Header
		body                 string
	}
	observed := make(chan request, 1)
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		observed <- request{r.Method, r.RequestURI, r.Host, r.Header.Clone(), string(body)}
		if r.Method == "PUT" {
			w.WriteHeader(201)
			_, _ = io.WriteString(w, metadataJSON())
		} else {
			headers(w, 0)
		}
	})
	w, capture := writer(t)
	client := artifacts.NewClient(artifacts.ClientConfig{Service: entry, Telemetry: w})
	paths := []string{"a%2Fb", "a%41", "a//b", "../x", "./x", "a/", "/a", "", "AZ09-._~!$&'()*+,;=:@/%aF"}
	count := 0
	for _, caller := range []*identity.Caller{nil, {UserID: "user", Email: "mail", RequestID: "request"}, {UserID: "user", RequestID: "request"}} {
		ctx := events.NewContext(context.Background(), events.Cause{ID: "cause", Depth: 9})
		if caller != nil {
			ctx = identity.NewContext(ctx, *caller)
		}
		for _, path := range paths {
			for _, method := range []string{"PUT", "GET", "HEAD"} {
				if _, err := invoke(ctx, client, method, path); err != nil {
					t.Fatal(err)
				}
				got := <-observed
				if got.method != method || got.target != "/files/"+path || got.host != entry.Name || got.body != "" {
					t.Fatal(got)
				}
				expected := make(http.Header)
				if caller != nil {
					r := &http.Request{Header: expected}
					identity.Forward(*caller, r)
				}
				for _, key := range []string{"X-User-Id", "X-User-Email", "X-Request-Id"} {
					if strings.Join(got.header.Values(key), "|") != strings.Join(expected.Values(key), "|") {
						t.Fatal(got.header)
					}
				}
				for _, key := range []string{"X-Event-Cause", "X-Event-Depth", "Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
					if _, present := got.header[key]; present {
						t.Fatal(got.header)
					}
				}
				if err := w.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				trail := capture.Events()
				count++
				if len(trail) != count {
					t.Fatal("wrong event count")
				}
				e := trail[count-1]
				if !strings.Contains(path, "%") && e.Attrs["path"] != "/files/"+path {
					t.Fatal(e)
				}
				if e.Name != "sibling.called" || e.Service != "connector" || e.Time != fixedTime || len(e.Attrs) != 5 || e.Attrs["method"] != method || e.Attrs["target"] != entry.Name || e.Attrs["duration_us"] != int64(0) {
					t.Fatal(e)
				}
				status := int64(200)
				if method == "PUT" {
					status = 201
				}
				if e.Attrs["status"] != status {
					t.Fatal(e)
				}
				user, id := "", ""
				if caller != nil {
					user, id = caller.UserID, caller.RequestID
				}
				if e.User != user || e.RequestID != id {
					t.Fatal(e)
				}
			}
		}
	}
}

type trackingBody struct {
	io.Reader
	reads, closes int
}

func (b *trackingBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *trackingBody) Close() error               { b.closes++; return nil }

// R-Z89E-GW5D R-PENQ-5SAG
func TestUnsendablePaths(t *testing.T) {
	entry := serve(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") })
	w, capture := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	for _, path := range []string{"a b", "a?b", "a#b", "a%zz", "é", "a%", "a%1", "a\n", "a\x00", "a\\b", "a[b]"} {
		for _, method := range []string{"PUT", "GET", "HEAD"} {
			body := &trackingBody{Reader: strings.NewReader("data")}
			var m artifacts.Metadata
			var err error
			if method == "PUT" {
				m, err = c.Put(context.Background(), path, artifacts.PutOptions{}, body)
			} else {
				m, err = invoke(context.Background(), c, method, path)
			}
			if !errors.Is(err, artifacts.ErrBadPath) || m != (artifacts.Metadata{}) || body.reads != 0 || body.closes != 0 {
				t.Fatal(path, m, err)
			}
		}
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(capture.Events()) != 0 {
		t.Fatal("unsent request recorded")
	}
}

// R-YZQ3-SHYI R-YYI7-EQ7T R-ZD4Z-ZZ45 R-ZECW-DQUU R-ZI0L-J22X
func TestPutBodyOptionsMetadata(t *testing.T) {
	type received struct {
		headers http.Header
		body    string
	}
	seen := make(chan received, 1)
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		seen <- received{r.Header.Clone(), string(data)}
		w.Header().Set("Content-Type", "unrelated/type")
		status := 200
		if r.Header.Get("Content-Type") != "" {
			status = 201
		}
		w.WriteHeader(status)
		answer := metadataJSON()
		if r.Header.Get("X-Artifact-Expires") != "" {
			answer = strings.TrimSuffix(answer, "}") + `,"expires":"2028-03-04T05:06:07+01:00"}`
		}
		_, _ = io.WriteString(w, answer)
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	expires := time.Date(2028, 3, 4, 5, 6, 7, 123456789, time.FixedZone("offset", 3600))
	for _, opts := range []artifacts.PutOptions{{}, {"content/custom", expires}} {
		for _, data := range []string{"", "\x00\xffstream bytes"} {
			b := &trackingBody{Reader: strings.NewReader(data)}
			m, err := c.Put(context.Background(), "input", opts, b)
			if err != nil {
				t.Fatal(err)
			}
			got := <-seen
			if got.body != data || b.closes != 0 || b.reads == 0 {
				t.Fatal(got, b)
			}
			if opts.ContentType == "" {
				if len(got.headers.Values("Content-Type")) != 0 || len(got.headers.Values("X-Artifact-Expires")) != 0 {
					t.Fatal(got)
				}
			} else {
				if len(got.headers.Values("Content-Type")) != 1 || got.headers.Get("Content-Type") != opts.ContentType || len(got.headers.Values("X-Artifact-Expires")) != 1 || got.headers.Get("X-Artifact-Expires") != expires.UTC().Format(time.RFC3339) {
					t.Fatal(got)
				}
			}
			expected := artifacts.Metadata{"artifact-id", "saved/path", 7, "binary/test", "sum", "owner", fixedTime, fixedTime, time.Time{}, "https://file.invalid"}
			if opts.ContentType != "" {
				expected.Expires = time.Date(2028, 3, 4, 4, 6, 7, 0, time.UTC)
			}
			if m != expected || m.Created.Location() != time.UTC || m.Modified.Location() != time.UTC || !m.Expires.IsZero() && m.Expires.Location() != time.UTC {
				t.Fatal(m)
			}
		}
	}
	if _, err := c.Put(context.Background(), "nil", artifacts.PutOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if (<-seen).body != "" {
		t.Fatal("nil not empty")
	}
}

// R-ZJ8H-WTTM R-PENQ-5SAG
func TestPutInvalidMetadata(t *testing.T) {
	answers := make(chan string, 1)
	entry := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); _, _ = io.WriteString(w, <-answers) })
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	invalid := []string{"", "null", "[]", `{}`, metadataJSON() + ` {}`, metadataJSON() + ` garbage`}
	for _, field := range []string{"id", "path", "content_type", "sha256", "owner", "url", "bytes", "created", "modified"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(metadataJSON()), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, field)
		b, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		invalid = append(invalid, string(b))
		badValues := []string{`null`, `true`, `[]`, `{}`}
		if field == "created" || field == "modified" || field == "bytes" {
			badValues = append(badValues, `"not-a-time"`)
		}
		for _, bad := range badValues {
			fields[field] = json.RawMessage(bad)
			b, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			invalid = append(invalid, string(b))
		}
	}
	invalid = append(invalid, strings.Replace(metadataJSON(), `"bytes":7`, `"bytes":1.5`, 1), strings.Replace(metadataJSON(), `"bytes":7`, `"bytes":9223372036854775808`, 1), strings.TrimSuffix(metadataJSON(), "}")+`,"expires":null}`)
	for _, value := range invalid {
		answers <- value
		m, err := c.Put(context.Background(), "p", artifacts.PutOptions{}, nil)
		plainError(t, err)
		if m != (artifacts.Metadata{}) {
			t.Fatal(m)
		}
	}
}

// R-ZKGE-ALKB R-ZLOA-ODB0 R-ZO43-FWSE
func TestGetHeadMetadata(t *testing.T) {
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		headers(w, 4)
		w.Header()["Etag"] = []string{`"first"`, `"second"`}
		w.Header()["Content-Type"] = []string{"type/first", "type/second"}
		if r.Method == "GET" {
			_, _ = io.WriteString(w, "data")
		}
	})
	w, _ := writer(t)
	for _, base := range []string{entry.URL, ""} {
		entry.URL = base
		c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
		for _, method := range []string{"GET", "HEAD"} {
			var m artifacts.Metadata
			var err error
			if method == "GET" {
				var body io.ReadCloser
				m, body, err = c.Get(context.Background(), "a%2Fb")
				if err == nil {
					if body == nil {
						t.Fatal("nil body")
					}
					data, readErr := io.ReadAll(body)
					closeErr := body.Close()
					if readErr != nil || closeErr != nil || string(data) != "data" {
						t.Fatal("body changed", readErr, closeErr)
					}
				}
			} else {
				m, err = c.Head(context.Background(), "a%2Fb")
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := artifacts.Metadata{Path: "a%2Fb", Bytes: 4, ContentType: "type/first", SHA256: "first", Modified: fixedTime, URL: strings.TrimRight(base, "/") + "/files/a%2Fb"}
			if m != expected || m.Modified.Location() != time.UTC {
				t.Fatal(m)
			}
		}
	}
}

// R-ZMW7-251P R-PENQ-5SAG
func TestInvalidHeaders(t *testing.T) {
	changes := make(chan http.Header, 1)
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		headers(w, 1)
		for k, v := range <-changes {
			if v == nil {
				w.Header().Del(k)
			} else {
				w.Header()[k] = v
			}
		}
		w.WriteHeader(200)
		if r.Method == "GET" {
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "x")
		}
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	for _, change := range []http.Header{{"Content-Length": nil}, {"Etag": nil}, {"Last-Modified": nil}, {"Etag": {`W/"x"`}}, {"Etag": {`""`}}, {"Etag": {`"a"b"`}}, {"Etag": {"unquoted"}}, {"Last-Modified": {"invalid"}}} {
		for _, method := range []string{"GET", "HEAD"} {
			changes <- change
			if method == "GET" {
				m, b, err := c.Get(context.Background(), "p")
				plainError(t, err)
				if m != (artifacts.Metadata{}) || b != nil {
					t.Fatal(m, b)
				}
			} else {
				m, err := c.Head(context.Background(), "p")
				plainError(t, err)
				if m != (artifacts.Metadata{}) {
					t.Fatal(m)
				}
			}
		}
	}
}

// R-PLZ4-GEQM R-6JQP-H7LM R-6KYL-UZCB R-PENQ-5SAG
func TestResponseStatuses(t *testing.T) {
	statuses := make(chan int, 1)
	calls := make(chan struct{}, 100)
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls <- struct{}{}
		w.Header().Set("Location", "/redirect-target")
		w.WriteHeader(<-statuses)
		if r.Method != "HEAD" {
			_, _ = io.WriteString(w, strings.Repeat("x", 5000))
		}
	})
	w, _ := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	mapping := map[int]error{400: artifacts.ErrBadPath, 403: artifacts.ErrForbidden, 404: artifacts.ErrNotFound, 413: artifacts.ErrTooLarge, 503: artifacts.ErrUnavailable}
	for _, code := range []int{400, 403, 404, 413, 503, 300, 301, 302, 303, 307, 308, 204, 418, 500} {
		for _, method := range []string{"PUT", "GET", "HEAD"} {
			statuses <- code
			m, err := invoke(context.Background(), c, method, "p")
			if err == nil || m != (artifacts.Metadata{}) || len(calls) != 1 {
				t.Fatal(code, method, m, err, len(calls))
			}
			<-calls
			var he *artifacts.HTTPError
			if sentinel := mapping[code]; sentinel != nil {
				if errors.As(err, &he) {
					t.Fatal(err)
				}
				for _, other := range sentinels() {
					if errors.Is(err, other) != errors.Is(sentinel, other) {
						t.Fatal(err)
					}
				}
			} else {
				if !errors.As(err, &he) || he.StatusCode != code {
					t.Fatal(err)
				}
				expected := strings.Repeat("x", 4096)
				if method == "HEAD" || code == 204 {
					expected = ""
				}
				if he.Body != expected {
					t.Fatal(code, len(he.Body))
				}
			}
		}
	}
}

// R-PC7X-E8T2 R-6IIT-3FUX R-PENQ-5SAG
func TestTransportFailures(t *testing.T) {
	w, capture := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{services.Entry{Name: "artifacts", Socket: filepath.Join(t.TempDir(), "missing")}, w})
	count := 0
	for _, method := range []string{"PUT", "GET", "HEAD"} {
		for _, kind := range []string{"dial", "cancel", "deadline"} {
			ctx := context.Background()
			var want error
			if kind == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			}
			if kind == "deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
				want = context.DeadlineExceeded
			}
			m, err := invoke(ctx, c, method, "p")
			plainError(t, err)
			if m != (artifacts.Metadata{}) {
				t.Fatal(m)
			}
			if want != nil {
				if !errors.Is(err, want) {
					t.Fatal(err)
				}
			} else {
				var op *net.OpError
				if !errors.As(err, &op) || op.Op != "dial" {
					t.Fatal(err)
				}
			}
			count++
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			trail := capture.Events()
			if len(trail) != count || trail[count-1].Attrs["status"] != int64(0) {
				t.Fatal(trail)
			}
		}
	}
}

// R-6M6I-8R30
func TestConcurrentCalls(t *testing.T) {
	entry := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/files/")
		if r.Method == "POST" {
			root := showRequest(t, r)
			var params struct{ Arguments map[string]string }
			if err := json.Unmarshal(root["params"], &params); err != nil {
				t.Error(err)
			}
			path = params.Arguments["path"]
			if r.Header.Get("X-User-Id") != path {
				t.Error("caller mixed")
			}
			w.Header().Set("Content-Type", "application/json")
			showRespond(w, root, `{"structuredContent":`+strings.Replace(metadataJSON(), `"artifact-id"`, fmt.Sprintf("%q", path), 1)+`}`)
			return
		}
		if r.Header.Get("X-User-Id") != path {
			t.Error("caller mixed")
		}
		if r.Method == "PUT" {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, metadataJSON())
		} else {
			headers(w, 0)
		}
	})
	w, capture := writer(t)
	c := artifacts.NewClient(artifacts.ClientConfig{entry, w})
	var wg sync.WaitGroup
	for n := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := fmt.Sprint(n)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: path, RequestID: "request" + path})
			for _, method := range []string{"PUT", "GET", "HEAD", "SHOW"} {
				if method == "SHOW" {
					m, err := c.Show(ctx, path)
					if err != nil || m.ID != path {
						t.Error(m, err)
					}
					continue
				}
				if _, err := invoke(ctx, c, method, path); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 96 {
		t.Fatal(len(trail))
	}
	for _, e := range trail {
		wantPath := "/files/" + e.User
		if e.Attrs["method"] == "POST" {
			wantPath = "/mcp"
		}
		if e.RequestID != "request"+e.User || e.Attrs["path"] != wantPath {
			t.Fatal(e)
		}
	}
}
