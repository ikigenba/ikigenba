package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	artifacts "github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

type repeatReader struct{ next byte }

func (r *repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
		r.next++
	}
	return len(p), nil
}

type bodyReader struct {
	reader io.Reader
	read   int
	hook   func()
}

func (b *bodyReader) Read(p []byte) (int, error) {
	if b.hook != nil {
		b.hook()
		b.hook = nil
	}
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("client stopped") }

type fixture struct {
	st     *store.Store
	d      *db.DB
	blobs  string
	now    time.Time
	w      *telemetry.Writer
	em     *events.Emitter
	tc     *telemetry.Capture
	ec     *events.Capture
	cfg    files.Config
	stderr bytes.Buffer
}

func setup(t *testing.T, limit int64) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{now: time.Date(2028, 2, 3, 4, 5, 6, 0, time.UTC), tc: &telemetry.Capture{}, ec: &events.Capture{}}
	dir := t.TempDir()
	f.blobs = filepath.Join(dir, "blobs")
	if err := os.Mkdir(f.blobs, 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "artifacts.db"), Migrations: artifacts.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.st = store.New(f.d, f.blobs, store.Config{Now: func() time.Time { return f.now }, Rand: &repeatReader{}})
	f.w = telemetry.New(telemetry.Config{Service: urls.Service, Sink: f.tc, Now: func() time.Time { return f.now }, Rand: &repeatReader{}, Stderr: &f.stderr})
	f.em = events.New(events.Config{Service: urls.Service, Sink: f.ec, Emits: trail.Emits(), Now: func() time.Time { return f.now }, Rand: &repeatReader{}, Stderr: &f.stderr})
	f.cfg = files.Config{Store: f.st, Max: limit, Telemetry: f.w, Events: f.em}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		f.em.Shutdown(ctx)
		f.w.Shutdown(ctx, "test")
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *fixture) request(t *testing.T, method, target, user string, body io.Reader, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, body)
	r.Host = "artifacts.example:7401"
	r.Header = headers.Clone()
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	if value := r.Header.Get("Content-Length"); value != "" {
		length, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		r.ContentLength = length
	}
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", user+"@example.test")
	r.Header.Set("X-Request-Id", "request-example")
	r.Header.Set("X-Forwarded-Proto", "http")
	r = r.WithContext(events.NewContext(r.Context(), events.Cause{ID: "evt_0123456789abcdef", Depth: 2}))
	w := httptest.NewRecorder()
	identity.Require(files.Handler(f.cfg)).ServeHTTP(w, r)
	return w
}
func (f *fixture) snapshot(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(f.blobs)
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[string]string)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(f.blobs, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		m[e.Name()] = string(b)
	}
	return m
}
func (f *fixture) get(t *testing.T, p string) store.Artifact {
	t.Helper()
	a, err := f.st.Get(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func refusal(t *testing.T, w *httptest.ResponseRecorder, method string, status int, line string) {
	t.Helper()
	want := line + "\n"
	if method == "HEAD" {
		want = ""
	}
	if w.Code != status || w.Body.String() != want || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
		t.Fatalf("refusal: %d %v %q want %d %q", w.Code, w.Header(), w.Body.String(), status, want)
	}
}
func metadata(t *testing.T, w *httptest.ResponseRecorder, a store.Artifact, status int) {
	t.Helper()
	want, err := json.Marshal(files.Describe(a, "http://artifacts.example:7401"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != status || w.Body.String() != string(want)+"\n" || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
		t.Fatalf("metadata %d %v %s", w.Code, w.Header(), w.Body.String())
	}
}

// R-CBAF-T7WW R-CEY4-YJ4Z R-CDQ8-KREA R-CCIC-6ZNL
func TestMetadata(t *testing.T) {
	if files.ExpiresHeader != "X-Artifact-Expires" {
		t.Fatal(files.ExpiresHeader)
	}
	stamp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", 7200))
	a := store.Artifact{ID: "artifact-example", Path: "reports/q.pdf", Bytes: 7, ContentType: "type/example", SHA256: "hash-example", OwnerID: "private-id", OwnerEmail: "owner@example.test", Created: stamp, Modified: stamp, Expires: stamp}
	m := files.Describe(a, "https://published.example")
	want := files.Metadata{ID: a.ID, Path: a.Path, Bytes: a.Bytes, ContentType: a.ContentType, SHA256: a.SHA256, Owner: a.OwnerEmail, Created: stamp.UTC().Format(time.RFC3339), Modified: stamp.UTC().Format(time.RFC3339), URL: urls.File("https://published.example", a.Path)}
	exp := stamp.UTC().Format(time.RFC3339)
	want.Expires = &exp
	if !reflect.DeepEqual(m, want) {
		t.Fatal(m, want)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"id":"artifact-example","path":"reports/q.pdf","bytes":7,"content_type":"type/example","sha256":"hash-example","owner":"owner@example.test","created":"2030-01-02T01:04:05Z","modified":"2030-01-02T01:04:05Z","expires":"2030-01-02T01:04:05Z","url":"https://published.example/files/reports/q.pdf"}`
	if string(b) != expected {
		t.Fatal(string(b))
	}
	a.Expires = time.Time{}
	m = files.Describe(a, "https://published.example")
	if m.Expires != nil {
		t.Fatal(m)
	}
	b, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != strings.Replace(expected, `,"expires":"2030-01-02T01:04:05Z"`, "", 1) {
		t.Fatal(string(b))
	}
	for _, line := range []string{files.InvalidPath, files.InvalidExpires, files.PastExpiry, files.NotFound, files.Forbidden, files.TooLarge, files.BodyFailed} {
		if line == "" || strings.Contains(line, "\n") {
			t.Fatal(line)
		}
	}
}

// R-CCIC-6ZNL R-CL1M-VDUG R-CNHF-MXBU R-COPC-0P2J R-CPX8-EGT8 R-D0WB-UEHH
func TestLifecycle(t *testing.T) {
	f := setup(t, 100)
	for i, ct := range []string{"", "type/example; parameter=Verbatim", ""} {
		p := []string{"one.bin", "two.bin", "three.bin"}[i]
		header := http.Header{}
		if i != 0 {
			header["Content-Type"] = []string{ct, "ignored/second"}
		}
		var input io.Reader = &bodyReader{reader: strings.NewReader("payload-example")}
		if i == 1 {
			input = strings.NewReader("payload-example")
		}
		w := f.request(t, "PUT", "/files/"+p+"?ignored=1", "owner", input, header)
		a := f.get(t, p)
		metadata(t, w, a, 201)
		expected := ct
		if expected == "" {
			expected = store.DefaultContentType
		}
		if a.OwnerID != "owner" || a.OwnerEmail != "owner@example.test" || !a.Expires.IsZero() || a.ContentType != expected || a.Bytes != 15 {
			t.Fatal(a)
		}
		if get := f.request(t, "GET", "/files/"+p, "other", nil, nil); get.Code != 200 || get.Body.String() != "payload-example" {
			t.Fatal(get.Code, get.Body.String())
		}
		if len(f.snapshot(t)) != i+1 {
			t.Fatal("blob count")
		}
	}
	before := f.get(t, "one.bin")
	h := http.Header{files.ExpiresHeader: {"2030-01-02T03:04:05.9+02:00"}, "Content-Type": {"new/example"}}
	w := f.request(t, "PUT", "/files/one.bin", "owner", strings.NewReader("replacement"), h)
	a := f.get(t, "one.bin")
	metadata(t, w, a, 200)
	assertStoredPayload(t, f, a, "replacement")
	if a.ID != before.ID || a.OwnerEmail != before.OwnerEmail || a.Created != before.Created || a.ContentType != "new/example" || a.Expires.Format(time.RFC3339) != "2030-01-02T01:04:05Z" {
		t.Fatal(a)
	}
	w = f.request(t, "PUT", "/files/one.bin", "owner", strings.NewReader("final"), nil)
	a = f.get(t, "one.bin")
	metadata(t, w, a, 200)
	assertStoredPayload(t, f, a, "final")
	if !a.Expires.IsZero() {
		t.Fatal(a)
	}
	other := f.get(t, "two.bin")
	w = f.request(t, "DELETE", "/files/one.bin", "owner", nil, nil)
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatal(w)
	}
	refusal(t, f.request(t, "GET", "/files/one.bin", "owner", nil, nil), "GET", 404, files.NotFound)
	if f.get(t, "two.bin") != other || len(f.snapshot(t)) != 2 {
		t.Fatal("delete disturbed other artifacts")
	}
}

// R-CG61-CAVO R-CHDX-Q2MD R-CILU-3UD2 R-CJTQ-HM3R R-CR54-S8JX R-CSD1-60AM R-CTKX-JS1B R-CYGJ-2V03 R-R8FJ-KDNG
func TestPreflightRefusals(t *testing.T) {
	f := setup(t, 4)
	metadata(t, f.request(t, "PUT", "/files/owned", "owner", strings.NewReader("old"), nil), f.get(t, "owned"), 201)
	original := f.get(t, "owned")
	snapshot := f.snapshot(t)
	f.flush(t)
	eventCount := len(f.tc.Events())
	cases := []struct {
		method, target, user string
		headers              http.Header
		status               int
		line                 string
		failing              bool
	}{
		{method: "POST", target: "/files/../x", status: 405, failing: true},
		{method: "PATCH", target: "/files/owned", status: 405},
		{method: "PUT", target: "/files/a//b", status: 400, line: files.InvalidPath, headers: http.Header{files.ExpiresHeader: {"broken"}}},
		{method: "PUT", target: "/files/../x", status: 400, line: files.InvalidPath},
		{method: "PUT", target: "/files/./x", status: 400, line: files.InvalidPath},
		{method: "GET", target: "/files/a%2Fb", status: 400, line: files.InvalidPath},
		{method: "GET", target: "/files/a%41", status: 400, line: files.InvalidPath},
		{method: "DELETE", target: "/files/", status: 400, line: files.InvalidPath},
		{method: "HEAD", target: "/files/a/", status: 400, line: files.InvalidPath},
		{method: "PUT", target: "/files/new", status: 400, line: files.InvalidExpires, headers: http.Header{files.ExpiresHeader: {""}}},
		{method: "PUT", target: "/files/new", status: 400, line: files.InvalidExpires, headers: http.Header{files.ExpiresHeader: {"not-a-time"}}},
		{method: "PUT", target: "/files/new", status: 400, line: files.InvalidExpires, headers: http.Header{files.ExpiresHeader: {"2030-01-01T00:00:00Z", "2031-01-01T00:00:00Z"}}, failing: true},
		{method: "PUT", target: "/files/owned", user: "other", status: 400, line: files.PastExpiry, headers: http.Header{files.ExpiresHeader: {"2028-02-03T04:05:06.9Z"}}},
		{method: "PUT", target: "/files/owned", user: "other", status: 403, line: files.Forbidden},
		{method: "DELETE", target: "/files/owned", user: "other", status: 403, line: files.Forbidden},
		{method: "GET", target: "/files/missing", status: 404, line: files.NotFound},
		{method: "HEAD", target: "/files/missing", status: 404, line: files.NotFound},
		{method: "DELETE", target: "/files/missing", status: 404, line: files.NotFound},
		{method: "PUT", target: "/files/owned", user: "other", status: 503, line: store.Unreachable, headers: http.Header{files.ExpiresHeader: {"2020-01-01T00:00:00Z"}}, failing: true},
	}
	for _, c := range cases {
		t.Run(c.method+c.target+c.line, func(t *testing.T) {
			user := c.user
			if user == "" {
				user = "owner"
			}
			body := &bodyReader{reader: strings.NewReader("too-large-secret")}
			f.d.SetFailing(c.failing)
			w := f.request(t, c.method, c.target, user, body, c.headers)
			f.d.SetFailing(false)
			if c.status == 405 {
				if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD, PUT, DELETE" || w.Body.Len() != 0 {
					t.Fatal(w)
				}
			} else {
				refusal(t, w, c.method, c.status, c.line)
			}
			if body.read != 0 || f.get(t, "owned") != original || !reflect.DeepEqual(f.snapshot(t), snapshot) {
				t.Fatal("preflight changed state or read body")
			}
			f.flush(t)
			if len(f.tc.Events()) != eventCount || len(f.ec.Events()) != eventCount {
				t.Fatal("refusal emitted")
			}
		})
	}
	// A nil store proves method rejection makes no store call.
	w := httptest.NewRecorder()
	files.Handler(files.Config{}).ServeHTTP(w, httptest.NewRequest("POST", "/files/bad//path", nil))
	if w.Code != 405 {
		t.Fatal(w)
	}
}

// R-CUST-XJS0 R-P164-T44K R-CW0Q-BBIP R-R77N-6LWR R-R8FJ-KDNG R-D248-8686
func TestBodyAndStoreFailures(t *testing.T) {
	f := setup(t, 4)
	f.request(t, "PUT", "/files/owned", "owner", strings.NewReader("old"), nil)
	original := f.get(t, "owned")
	snapshot := f.snapshot(t)
	f.flush(t)
	eventCount := len(f.tc.Events())
	for _, p := range []string{"new", "owned"} {
		for _, knownLength := range []bool{false, true} {
			body := &bodyReader{reader: strings.NewReader("01234567890123456789")}
			h := http.Header{}
			if knownLength {
				h.Set("Content-Length", "20")
			}
			w := f.request(t, "PUT", "/files/"+p, "owner", body, h)
			refusal(t, w, "PUT", 413, files.TooLarge)
			f.noEvents(t, eventCount)
			if w.Header().Get("Connection") != "close" || body.read > 5 {
				t.Fatal(w.Header(), body.read)
			}
			if f.get(t, "owned") != original || !reflect.DeepEqual(f.snapshot(t), snapshot) {
				t.Fatal("large body changed state")
			}
		}
	}
	for _, p := range []string{"new", "owned"} {
		w := f.request(t, "PUT", "/files/"+p, "owner", io.MultiReader(strings.NewReader("ab"), failedReader{}), nil)
		refusal(t, w, "PUT", 400, files.BodyFailed)
		f.noEvents(t, eventCount)
		if f.get(t, "owned") != original || !reflect.DeepEqual(f.snapshot(t), snapshot) {
			t.Fatal("failed body changed state")
		}
	}
	for _, method := range []string{"GET", "HEAD", "PUT", "DELETE"} {
		f.d.SetFailing(true)
		b := &bodyReader{reader: strings.NewReader("body")}
		w := f.request(t, method, "/files/owned", "owner", b, nil)
		refusal(t, w, method, 503, store.Unreachable)
		f.noEvents(t, eventCount)
		f.d.SetFailing(false)
		if b.read != 0 || !reflect.DeepEqual(f.snapshot(t), snapshot) {
			t.Fatal("failing handle read or changed body")
		}
	}
	b := &bodyReader{reader: strings.NewReader("abc"), hook: func() { f.d.SetFailing(true) }}
	w := f.request(t, "PUT", "/files/owned", "owner", b, nil)
	refusal(t, w, "PUT", 503, store.Unreachable)
	f.noEvents(t, eventCount)
	f.d.SetFailing(false)
	if f.get(t, "owned") != original || !reflect.DeepEqual(f.snapshot(t), snapshot) {
		t.Fatal("late db failure changed state")
	}
	b = &bodyReader{reader: strings.NewReader("oversized"), hook: func() { f.d.SetFailing(true) }}
	w = f.request(t, "PUT", "/files/owned", "owner", b, nil)
	refusal(t, w, "PUT", 413, files.TooLarge)
	f.noEvents(t, eventCount)
	f.d.SetFailing(false)
	if f.get(t, "owned") != original || !reflect.DeepEqual(f.snapshot(t), snapshot) {
		t.Fatal("late db large body changed state")
	}
	w = f.request(t, "PUT", "/files/exact", "owner", strings.NewReader("1234"), nil)
	metadata(t, w, f.get(t, "exact"), 201)
	assertStoredPayload(t, f, f.get(t, "exact"), "1234")
	f.cfg.Max = math.MaxInt64
	payload := "nonempty unlimited body"
	w = f.request(t, "PUT", "/files/unlimited", "owner", strings.NewReader(payload), nil)
	a := f.get(t, "unlimited")
	metadata(t, w, a, 201)
	sum := sha256.Sum256([]byte(payload))
	if a.Bytes != int64(len(payload)) || a.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal(a)
	}
	if len(f.snapshot(t)) != 3 {
		t.Fatal("unexpected blobs")
	}
	f.flush(t)
	eventCount = len(f.tc.Events())
	missing := filepath.Join(t.TempDir(), "not-there")
	f.cfg.Store = store.New(f.d, missing, store.Config{Now: func() time.Time { return f.now }, Rand: &repeatReader{}})
	w = f.request(t, "PUT", "/files/new", "owner", strings.NewReader("abc"), nil)
	refusal(t, w, "PUT", 503, store.Unreachable)
	f.noEvents(t, eventCount)
	w = f.request(t, "PUT", "/files/new", "owner", strings.NewReader("abc"), http.Header{files.ExpiresHeader: {"2020-01-01T00:00:00Z"}})
	refusal(t, w, "PUT", 400, files.PastExpiry)
	f.noEvents(t, eventCount)
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// R-CZOF-GMQS
func TestServeContent(t *testing.T) {
	f := setup(t, 100)
	payload := "0123456789abcdefghij"
	f.request(t, "PUT", "/files/reports/content.bin", "owner", strings.NewReader(payload), http.Header{"Content-Type": {"custom/example"}})
	a := f.get(t, "reports/content.bin")
	snapshot := f.snapshot(t)
	for _, tc := range []struct {
		method  string
		headers http.Header
	}{
		{"GET", nil}, {"HEAD", nil}, {"GET", http.Header{"Range": {"bytes=0-9"}}}, {"GET", http.Header{"Range": {"bytes=999-1000"}}},
		{"GET", http.Header{"If-None-Match": {`"` + a.SHA256 + `"`}}}, {"GET", http.Header{"If-Match": {`"wrong"`}}},
		{"GET", http.Header{"If-Modified-Since": {a.Modified.Format(http.TimeFormat)}}}, {"GET", http.Header{"If-Unmodified-Since": {a.Modified.Add(-time.Hour).Format(http.TimeFormat)}}},
		{"GET", http.Header{"Range": {"bytes=2-4"}, "If-Range": {`"wrong"`}}}, {"GET", http.Header{"Range": {"bytes=2-4"}, "If-Range": {`"` + a.SHA256 + `"`}}},
	} {
		r := httptest.NewRequest(tc.method, "/files/"+a.Path, nil)
		r.Header = tc.headers.Clone()
		if r.Header == nil {
			r.Header = make(http.Header)
		}
		want := httptest.NewRecorder()
		want.Header().Set("Content-Type", a.ContentType)
		want.Header().Set("Content-Disposition", `attachment; filename="content.bin"`)
		want.Header().Set("X-Content-Type-Options", "nosniff")
		want.Header().Set("ETag", `"`+a.SHA256+`"`)
		http.ServeContent(want, r, "content.bin", a.Modified, strings.NewReader(payload))
		got := f.request(t, tc.method, "/files/"+a.Path, "reader", nil, tc.headers)
		if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
			t.Fatalf("ServeContent %s %v: %d %v %q want %d %v %q", tc.method, tc.headers, got.Code, got.Header(), got.Body.String(), want.Code, want.Header(), want.Body.String())
		}
		if !reflect.DeepEqual(f.snapshot(t), snapshot) {
			t.Fatal("read changed blobs")
		}
	}
}

// R-D248-8686 R-D3C4-LXYV
func TestEventsAndBodyPrivacy(t *testing.T) {
	f := setup(t, 100)
	mark := "body-only-marker-unique"
	names := []string{trail.Created, trail.Replaced, trail.Deleted}
	artifactsSeen := make([]store.Artifact, 0, 3)
	w := f.request(t, "PUT", "/files/shared.bin", "owner", strings.NewReader(mark), nil)
	if w.Code != 201 {
		t.Fatal(w)
	}
	artifactsSeen = append(artifactsSeen, f.get(t, "shared.bin"))
	w = f.request(t, "PUT", "/files/shared.bin", "owner", strings.NewReader(mark+"again"), nil)
	if w.Code != 200 {
		t.Fatal(w)
	}
	artifactsSeen = append(artifactsSeen, f.get(t, "shared.bin"))
	refusal(t, f.request(t, "PUT", "/files/shared.bin", "other", strings.NewReader(mark), nil), "PUT", 403, files.Forbidden)
	f.cfg.Max = 2
	refusal(t, f.request(t, "PUT", "/files/shared.bin", "owner", strings.NewReader(mark), nil), "PUT", 413, files.TooLarge)
	f.cfg.Max = 100
	refusal(t, f.request(t, "PUT", "/files/shared.bin", "owner", io.MultiReader(strings.NewReader(mark), failedReader{}), nil), "PUT", 400, files.BodyFailed)
	for _, method := range []string{"GET", "HEAD"} {
		if w = f.request(t, method, "/files/shared.bin", "reader", nil, nil); w.Code != 200 {
			t.Fatal(w)
		}
	}
	artifactsSeen = append(artifactsSeen, f.get(t, "shared.bin"))
	w = f.request(t, "DELETE", "/files/shared.bin", "owner", nil, nil)
	if w.Code != 204 {
		t.Fatal(w)
	}
	f.flush(t)
	te, be := f.tc.Events(), f.ec.Events()
	if len(te) != 3 || len(be) != 3 {
		t.Fatal(te, be)
	}
	for i, a := range artifactsSeen {
		attrs := map[string]any{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
		if te[i].Name != names[i] || be[i].Name != names[i] || !reflect.DeepEqual(map[string]any(te[i].Attrs), attrs) || !reflect.DeepEqual(map[string]any(be[i].Attrs), attrs) || te[i].User != "owner" || be[i].User != "owner" || te[i].RequestID != "request-example" || be[i].RequestID != "request-example" || be[i].Cause != "evt_0123456789abcdef" || be[i].Depth != 3 {
			t.Fatal(te[i], be[i])
		}
	}
	encoded, err := json.Marshal([]any{te, be})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), mark) || strings.Contains(f.stderr.String(), mark) {
		t.Fatal("body reached events")
	}
	if len(f.snapshot(t)) != 0 {
		t.Fatal("deleted blobs remain")
	}
}

// R-CL1M-VDUG R-CPX8-EGT8
func TestCreateExpiryAndPublishedURL(t *testing.T) {
	f := setup(t, 100)
	p := filepath.Join(t.TempDir(), "services.json")
	content := `{"services":[{"name":"artifacts","url":"https://published.example/","description":"","socket":"","enabled":true,"mcp":true}]}`
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	f.cfg.ServicesPath = p
	for _, known := range []bool{false, true} {
		name := "unknown"
		var body io.Reader = &bodyReader{reader: strings.NewReader("body")}
		if known {
			name = "known"
			body = strings.NewReader("body")
		}
		w := f.request(t, "PUT", "/files/"+name, "owner", body, http.Header{files.ExpiresHeader: {"2030-01-02T03:04:05.9+02:00"}})
		a := f.get(t, name)
		want, err := json.Marshal(files.Describe(a, "https://published.example"))
		if err != nil {
			t.Fatal(err)
		}
		if w.Code != 201 || w.Body.String() != string(want)+"\n" || a.Expires.Format(time.RFC3339) != "2030-01-02T01:04:05Z" {
			t.Fatal(w, a)
		}
		if got := f.request(t, "GET", "/files/"+name, "reader", nil, nil); got.Body.String() != "body" {
			t.Fatal(got)
		}
		if len(f.snapshot(t)) != map[bool]int{false: 1, true: 2}[known] {
			t.Fatal("blob count")
		}
	}
}

type writeHook struct {
	*httptest.ResponseRecorder
	hook   func()
	writes int
}

func (w *writeHook) Write(p []byte) (int, error) {
	w.writes++
	if w.hook != nil {
		w.hook()
		w.hook = nil
	}
	return w.ResponseRecorder.Write(p)
}

// R-CZOF-GMQS
func TestStreamingSnapshot(t *testing.T) {
	for _, change := range []string{"replace", "delete"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t, 1<<20)
			payload := strings.Repeat("old-byte-stream", 10000)
			f.request(t, "PUT", "/files/stream.bin", "owner", strings.NewReader(payload), nil)
			a := f.get(t, "stream.bin")
			r := httptest.NewRequest("GET", "/files/stream.bin", nil)
			r.Header.Set("X-User-Id", "reader")
			w := &writeHook{ResponseRecorder: httptest.NewRecorder(), hook: func() {
				if change == "replace" {
					got := f.request(t, "PUT", "/files/stream.bin", "owner", strings.NewReader(strings.Repeat("new-byte-stream", 10000)), nil)
					if got.Code != 200 {
						t.Fatal(got)
					}
				} else {
					got := f.request(t, "DELETE", "/files/stream.bin", "owner", nil, nil)
					if got.Code != 204 {
						t.Fatal(got)
					}
				}
			}}
			identity.Require(files.Handler(f.cfg)).ServeHTTP(w, r)
			expected := httptest.NewRecorder()
			expected.Header().Set("Content-Type", a.ContentType)
			expected.Header().Set("Content-Disposition", `attachment; filename="stream.bin"`)
			expected.Header().Set("X-Content-Type-Options", "nosniff")
			expected.Header().Set("ETag", `"`+a.SHA256+`"`)
			http.ServeContent(expected, r, "stream.bin", a.Modified, strings.NewReader(payload))
			t.Logf("streaming response used %d writes", w.writes)
			if w.Code != expected.Code || w.Body.String() != payload || !reflect.DeepEqual(w.Header(), expected.Header()) {
				t.Fatal(w)
			}
			want := 0
			if change == "replace" {
				want = 1
			}
			if len(f.snapshot(t)) != want {
				t.Fatal("blob count")
			}
		})
	}
}

func assertStoredPayload(t *testing.T, f *fixture, a store.Artifact, payload string) {
	t.Helper()
	sum := sha256.Sum256([]byte(payload))
	if a.Bytes != int64(len(payload)) || a.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal(a)
	}
	got := f.request(t, "GET", "/files/"+a.Path, "reader", nil, nil)
	if got.Code != 200 || got.Body.String() != payload {
		t.Fatal(got.Code, got.Body.String())
	}
}
func (f *fixture) noEvents(t *testing.T, count int) {
	t.Helper()
	f.flush(t)
	if len(f.tc.Events()) != count || len(f.ec.Events()) != count {
		t.Fatal("failed operation emitted", f.tc.Events(), f.ec.Events())
	}
}
