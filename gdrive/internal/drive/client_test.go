package drive_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
)

const token = "fake-access-token-over-sixteen"
const normal = `{"id":"file-id","name":"sample","mimeType":"text/plain","size":"0","modifiedTime":"2032-01-02T03:04:05+02:00","parents":["folder-id"],"webViewLink":"link"}`

func fake(t *testing.T, handler http.HandlerFunc) *drive.Client {
	t.Helper()
	var requests atomic.Int32
	var sawRoot atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if r.URL.Path == "/drive/v3/files/root" {
			if n != 1 {
				t.Error("root request was duplicated or was not first")
			}
			sawRoot.Store(true)
		} else if (strings.HasPrefix(r.URL.Path, "/upload/drive/v3/files") || (strings.HasPrefix(r.URL.Path, "/drive/v3/files") && r.URL.Query().Get("alt") != "media" && !strings.HasSuffix(r.URL.Path, "/export"))) && !sawRoot.Load() {
			t.Error("operation omitted initial root lookup")
		}
		if strings.HasPrefix(r.URL.Path, "/drive/v3/") && r.Method != http.MethodGet {
			t.Error("Drive read did not use GET", r.Method)
		}
		for _, key := range []string{"supportsAllDrives", "fields", "includeItemsFromAllDrives", "q", "pageSize", "pageToken", "corpora", "driveId", "orderBy", "alt", "mimeType", "uploadType"} {
			if values, present := r.URL.Query()[key]; present && len(values) != 1 {
				t.Error("duplicate query parameter", key)
			}
		}
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("supportsAllDrives") != "true" {
			t.Error("missing authorization or shared-drive support", r.URL)
		}
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	client := s.Client()
	client.Timeout = 5 * time.Second
	return drive.NewClient(drive.Config{client, s.URL, nil})
}
func reply(w http.ResponseWriter, s string) { _, _ = io.WriteString(w, s) }
func root(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/drive/v3/files/root" {
		if r.Method != "GET" || r.URL.Query().Get("fields") != "id" || len(r.URL.Query()["fields"]) != 1 || len(r.URL.Query()["supportsAllDrives"]) != 1 {
			panic("incorrect root request")
		}
		reply(w, `{"id":"my-root"}`)
		return true
	}
	return false
}
func classify(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error %v want %v", err, want)
	}
	matches := 0
	for _, candidate := range []error{drive.ErrNotFound, drive.ErrForbidden, drive.ErrTooLarge, drive.ErrProvider} {
		if errors.Is(err, candidate) {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("error classes %d", matches)
	}
}
func TestListSearchAndReduction(t *testing.T) {
	// R-I6AM-J0M8 R-IETX-7ET3 R-JVE8-LKXH R-JRQJ-G9PE R-IR0X-1481 R-IS8T-EVYQ R-ITGP-SNPF R-JQIN-2HYP R-IVWI-K76T
	for _, search := range []bool{false, true} {
		for _, optional := range []bool{false, true} {
			t.Run(fmt.Sprintf("search=%t/optional=%t", search, optional), func(t *testing.T) {
				var calls atomic.Int32
				folder := ""
				page := ""
				text := ""
				shared := ""
				if optional {
					folder = "it's a\\b"
					page = "opaque-page"
					text = "it's a\\b"
					shared = "shared-id"
				}
				c := fake(t, func(w http.ResponseWriter, r *http.Request) {
					n := calls.Add(1)
					if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("supportsAllDrives") != "true" {
						t.Error("authorization or shared support")
					}
					q := r.URL.Query()
					if n == 1 {
						if r.URL.Path != "/drive/v3/files/root" || q.Get("fields") != "id" || len(q["fields"]) != 1 || len(q["supportsAllDrives"]) != 1 {
							t.Error("first root request")
						}
						reply(w, `{"id":"my-root"}`)
						return
					}
					if n != 2 || r.URL.Path != "/drive/v3/files" {
						t.Error("unexpected request")
					}
					quoted := "'it\\'s a\\\\b'"
					want := "'root' in parents and trashed = false"
					if optional {
						want = quoted + " in parents and trashed = false"
					}
					if search {
						want = "trashed = false"
						if optional {
							want = "fullText contains " + quoted + " and trashed = false and " + quoted + " in parents"
						}
						if q.Has("orderBy") {
							t.Error("search sorted")
						}
					} else if q.Get("orderBy") != "folder,name" {
						t.Error("list order")
					}
					if q.Get("q") != want || q.Get("pageSize") != "50" || q.Get("pageToken") != page || q.Has("pageToken") != optional || q.Get("includeItemsFromAllDrives") != "true" || q.Get("fields") != "nextPageToken,files(id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink)" {
						t.Errorf("query %v", q)
					}
					corpora := "allDrives"
					if search && optional {
						corpora = "drive"
					}
					if q.Get("corpora") != corpora || q.Get("driveId") != shared && search {
						t.Error("scope")
					}

					reply(w, `{"nextPageToken":"next-token","files":[`+normal+`,{"id":"native","mimeType":"application/vnd.google-apps.document","size":"1024","parents":["my-root"]},{"id":"shared","driveId":"shared-id","parents":["shared-id"]},{"id":"orphan"},{"id":"shortcut","mimeType":"application/vnd.google-apps.shortcut","shortcutDetails":{"targetId":"target"}}]}`)
				})
				var p drive.Page
				var err error
				if search {
					p, err = c.Search(context.Background(), token, drive.Query{Text: text, Folder: folder, Drive: shared, Page: page})
				} else {
					p, err = c.List(context.Background(), token, folder, page)
				}
				if err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 2 || p.Next != "next-token" || len(p.Files) != 5 {
					t.Fatal(p)
				}
				f := p.Files[0]
				if f.ID != "file-id" || f.Name != "sample" || f.Kind != drive.KindFile || f.MimeType != "text/plain" || f.Bytes == nil || *f.Bytes != 0 || f.Folder != "folder-id" || f.Link != "link" || !f.Modified.Equal(time.Date(2032, 1, 2, 1, 4, 5, 0, time.UTC)) || f.Modified.Location() != time.UTC {
					t.Fatal(f)
				}
				for _, f := range p.Files[1:] {
					if f.Bytes != nil || f.Folder != "" || f.Target != nil {
						t.Fatal(f)
					}
				}
			})
		}
	}
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if !root(w, r) {
			reply(w, `{"files":[]}`)
		}
	})
	p, err := c.List(context.Background(), token, "", "")
	if err != nil || p.Files == nil || len(p.Files) != 0 || p.Next != "" {
		t.Fatal(p, err)
	}
}
func TestShow(t *testing.T) {
	// R-IYCB-BQO7 R-LBTX-1PTC R-K2PM-W7DN
	for _, details := range []string{`{"targetId":"target-id","targetMimeType":"application/vnd.google-apps.document"}`, `{}`, `null`, `"wrong"`} {
		c := fake(t, func(w http.ResponseWriter, r *http.Request) {
			if root(w, r) {
				return
			}
			if r.URL.EscapedPath() != "/drive/v3/files/a%2Fb" || r.URL.Query().Get("fields") != "id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink,trashed,shortcutDetails" {
				t.Error("show request", r.URL)
			}
			reply(w, `{"id":"shortcut-id","mimeType":"application/vnd.google-apps.shortcut","shortcutDetails":`+details+`}`)
		})
		f, err := c.Show(context.Background(), token, "a/b")
		if err != nil {
			t.Fatal(err)
		}
		if details[0] == '{' && strings.Contains(details, "target-id") {
			if f.Target == nil || *f.Target != (drive.Target{ID: "target-id", Kind: drive.KindDocument}) {
				t.Fatal(f)
			}
		} else if f.Target != nil {
			t.Fatal(f)
		}
	}
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if !root(w, r) {
			reply(w, `{"trashed":true}`)
		}
	})
	f, err := c.Show(context.Background(), token, "file")
	classify(t, err, drive.ErrNotFound)
	if !reflect.DeepEqual(f, drive.File{}) {
		t.Fatal(f)
	}
	c = fake(t, func(w http.ResponseWriter, r *http.Request) {
		if !root(w, r) {
			reply(w, normal)
		}
	})
	f, err = c.Show(context.Background(), token, "file")
	if err != nil || f.ID != "file-id" || f.Target != nil {
		t.Fatal(f, err)
	}
}
func TestDrives(t *testing.T) {
	// R-IZK7-PIEW
	var calls atomic.Int32
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		q := r.URL.Query()
		if r.URL.Path != "/drive/v3/drives" || q.Get("pageSize") != "100" || q.Has("pageToken") != (n == 2) {
			t.Error(r.URL)
		}
		if n == 1 {
			reply(w, `{"drives":[{"id":"first","name":"First"}],"nextPageToken":"second-page"}`)
		} else {
			if q.Get("pageToken") != "second-page" {
				t.Error(q)
			}
			reply(w, `{"drives":[{"id":"second","name":"Second"}]}`)
		}
	})
	got, err := c.Drives(context.Background(), token)
	if err != nil || !reflect.DeepEqual(got, []drive.Drive{{ID: "first", Name: "First"}, {ID: "second", Name: "Second"}}) {
		t.Fatal(got, err)
	}
	c = fake(t, func(w http.ResponseWriter, _ *http.Request) { reply(w, `{}`) })
	got, err = c.Drives(context.Background(), token)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
func TestRootAndEmptyID(t *testing.T) {
	// R-JZ1X-QW5K R-K09U-4NW9 R-IDM0-TN2E
	for _, body := range []string{`{}`, `{"id":""}`, `[]`, `{"id":3}`} {
		for _, method := range []string{"list", "search", "show", "upload"} {
			var calls atomic.Int32
			c := fake(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); reply(w, body) })
			var err error
			switch method {
			case "list":
				_, err = c.List(context.Background(), token, "", "")
			case "search":
				_, err = c.Search(context.Background(), token, drive.Query{})
			case "show":
				_, err = c.Show(context.Background(), token, "file")
			case "upload":
				_, err = c.Upload(context.Background(), token, drive.Upload{}, strings.NewReader(""))
			}
			classify(t, err, drive.ErrProvider)
			if calls.Load() != 1 {
				t.Fatal("requests", calls.Load())
			}
		}
	}
	for _, status := range []int{403, 404, 500} {
		c := fake(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		_, err := c.List(context.Background(), token, "", "")
		classify(t, err, drive.ErrProvider)
	}
	c := fake(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("empty id made request") })
	_, e := c.Show(context.Background(), token, "")
	classify(t, e, drive.ErrNotFound)
	_, e = c.Media(context.Background(), token, "")
	classify(t, e, drive.ErrNotFound)
	_, e = c.Export(context.Background(), token, "", "type")
	classify(t, e, drive.ErrNotFound)
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFailures(t *testing.T) {
	// R-JU6C-7T6S R-J5NP-MD4D R-J6VM-04V2 R-J83I-DWLR R-JBR7-J7TU
	methods := []string{"drives", "list", "search", "show", "media", "export", "upload"}
	call := func(c *drive.Client, ctx context.Context, method string) error {
		switch method {
		case "drives":
			v, e := c.Drives(ctx, token)
			if e != nil && v != nil {
				t.Fatal(v)
			}
			return e
		case "list":
			v, e := c.List(ctx, token, "", "")
			if e != nil && !reflect.DeepEqual(v, drive.Page{}) {
				t.Fatal(v)
			}
			return e
		case "search":
			v, e := c.Search(ctx, token, drive.Query{})
			if e != nil && !reflect.DeepEqual(v, drive.Page{}) {
				t.Fatal(v)
			}
			return e
		case "show":
			v, e := c.Show(ctx, token, "file")
			if e != nil && !reflect.DeepEqual(v, drive.File{}) {
				t.Fatal(v)
			}
			return e
		case "media":
			v, e := c.Media(ctx, token, "file")
			if e != nil && v != nil {
				t.Fatal(v)
			}
			if v != nil {
				_ = v.Close()
			}
			return e
		case "export":
			v, e := c.Export(ctx, token, "file", "mime")
			if e != nil && v != nil {
				t.Fatal(v)
			}
			if v != nil {
				_ = v.Close()
			}
			return e
		default:
			v, e := c.Upload(ctx, token, drive.Upload{}, strings.NewReader(""))
			if e != nil && !reflect.DeepEqual(v, drive.File{}) {
				t.Fatal(v)
			}
			return e
		}
	}
	for _, method := range methods {
		for _, status := range []int{302, 400, 401, 403, 404, 429, 500} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				c := fake(t, func(w http.ResponseWriter, r *http.Request) {
					if root(w, r) {
						return
					}
					w.WriteHeader(status)
					reply(w, token)
				})
				err := call(c, context.Background(), method)
				want := drive.ErrProvider
				if status == 403 {
					want = drive.ErrForbidden
				}
				if status == 404 {
					want = drive.ErrNotFound
				}
				classify(t, err, want)
				if strings.Contains(err.Error(), token) {
					t.Fatal("token leaked")
				}
			})
		}
		c := drive.NewClient(drive.Config{HTTP: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(token) })}})
		err := call(c, context.Background(), method)
		classify(t, err, drive.ErrProvider)
		if strings.Contains(err.Error(), token) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err = call(c, ctx, method)
		if !errors.Is(err, context.Canceled) || !errors.Is(err, drive.ErrProvider) {
			t.Fatal(err)
		}
	}
	for _, method := range []string{"drives", "list", "search", "show"} {
		for _, body := range []string{`[]`, `null`, `bad`, `{"files":{},"drives":{}}`, `{"files":[{"size":"wrong"}],"drives":[],"size":"wrong"}`, `{"files":[{"modifiedTime":"wrong"}],"modifiedTime":"wrong","drives":{}}`} {
			if method == "drives" && strings.Contains(body, `"drives":[]`) {
				continue
			}
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				if !root(w, r) {
					reply(w, body)
				}
			})
			if method == "show" && strings.Contains(body, `"files":{}`) {
				continue
			}
			classify(t, call(c, context.Background(), method), drive.ErrProvider)
		}
	}
	for _, method := range methods {
		var closed atomic.Int32
		c := drive.NewClient(drive.Config{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			body := "error"
			status := 500
			if r.URL.Path == "/drive/v3/files/root" {
				body = `{"id":"root"}`
				status = 200
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: &trackedBody{Reader: strings.NewReader(body), closed: &closed}}, nil
		})}})
		_ = call(c, context.Background(), method)
		want := int32(1)
		if method == "list" || method == "search" || method == "show" || method == "upload" {
			want = 2
		}
		if closed.Load() != want {
			t.Fatalf("%s closed %d want %d", method, closed.Load(), want)
		}
	}
}

type trackedBody struct {
	io.Reader
	closed *atomic.Int32
}

func (b *trackedBody) Close() error { b.closed.Add(1); return nil }

func TestStreams(t *testing.T) {
	// R-LHXE-YKIT R-J200-H1WA R-PZ2Y-5HEL R-Q0AU-J95A R-Q1IQ-X0VZ
	for _, export := range []bool{false, true} {
		t.Run(fmt.Sprint(export), func(t *testing.T) {
			release := make(chan struct{})
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/drive/v3/files/a%2Fb"
				if export {
					wantPath += "/export"
				}
				if r.URL.EscapedPath() != wantPath {
					t.Error("stream path", r.URL)
				}
				if export {
					if r.URL.Query().Get("mimeType") != "text/plain" {
						t.Error(r.URL)
					}
				} else if r.URL.Query().Get("alt") != "media" {
					t.Error(r.URL)
				}
				reply(w, "first")
				w.(http.Flusher).Flush()
				select {
				case <-release:
					reply(w, "second")
				case <-r.Context().Done():
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var body io.ReadCloser
			var err error
			if export {
				body, err = c.Export(ctx, token, "a/b", "text/plain")
			} else {
				body, err = c.Media(ctx, token, "a/b")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = body.Close() }()
			part := make([]byte, 5)
			if _, err = io.ReadFull(body, part); err != nil || string(part) != "first" {
				t.Fatal(string(part), err)
			}
			close(release)
			rest, err := io.ReadAll(body)
			if err != nil || string(rest) != "second" {
				t.Fatal(string(rest), err)
			}
			c = fake(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			ctx, cancel2 := context.WithCancel(context.Background())
			defer cancel2()
			if export {
				body, err = c.Export(ctx, token, "a/b", "text/plain")
			} else {
				body, err = c.Media(ctx, token, "a/b")
			}
			if err != nil {
				t.Fatal(err)
			}
			cancel2()
			_, err = body.Read(part)
			_ = body.Close()
			if err == nil || errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		})
	}
	for _, reason := range []string{"exportSizeLimitExceeded", "other"} {
		c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(403)
			reply(w, `{"error":{"errors":[{"reason":"`+reason+`"}]}}`)
		})
		body, err := c.Export(context.Background(), token, "file", "mime")
		if body != nil {
			t.Fatal(body)
		}
		want := drive.ErrForbidden
		if reason == "exportSizeLimitExceeded" {
			want = drive.ErrTooLarge
		}
		classify(t, err, want)
	}
	for _, export := range []bool{false, true} {
		for _, size := range []int{10, drive.ExportCap} {
			c := fake(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprint(size+100))
				_, _ = w.Write(bytes.Repeat([]byte{'x'}, size))
			})
			var body io.ReadCloser
			var err error
			if export {
				body, err = c.Export(context.Background(), token, "file", "mime")
			} else {
				body, err = c.Media(context.Background(), token, "file")
			}
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 4096)
			total := 0
			for {
				n, e := body.Read(buf)
				total += n
				if e != nil {
					err = e
					break
				}
			}
			_ = body.Close()
			want := drive.ErrProvider
			if export && size >= drive.ExportCap {
				want = drive.ErrTooLarge
			}
			if total != size {
				t.Fatal(total)
			}
			classify(t, err, want)
		}
	}
}

func TestTelemetryAndDefaultHTTPRouting(t *testing.T) {
	// R-JWM4-ZCO6 R-JAJB-5G35
	var ticks atomic.Int64
	now := func() time.Time {
		return time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(ticks.Add(1)) * time.Microsecond)
	}
	capture := &telemetry.Capture{}
	writer := telemetry.New(telemetry.Config{Service: "gdrive", Sink: capture, Now: now, Rand: bytes.NewReader(make([]byte, 256))})
	defer writer.Shutdown(context.Background(), "test")
	caller := identity.Caller{UserID: "user", RequestID: "request"}
	ctx := identity.NewContext(context.Background(), caller)
	var endpoints []string
	c := drive.NewClient(drive.Config{HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.String(), drive.DefaultBaseURL) {
			t.Error("default base", r.URL)
		}
		body := ""
		switch r.URL.Path {
		case "/drive/v3/files/root":
			body = `{"id":"root"}`
			endpoints = append(endpoints, drive.EndpointGet)
		case "/drive/v3/drives":
			body = `{"drives":[]}`
			endpoints = append(endpoints, drive.EndpointDrives)
		case "/drive/v3/files":
			body = `{"files":[]}`
			endpoints = append(endpoints, drive.EndpointList)
		case "/drive/v3/files/file/export":
			body = "bytes"
			endpoints = append(endpoints, drive.EndpointExport)
		default:
			if r.URL.Query().Get("alt") == "media" {
				body = "bytes"
				endpoints = append(endpoints, drive.EndpointMedia)
			} else {
				body = normal
				endpoints = append(endpoints, drive.EndpointGet)
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, Telemetry: writer})
	if _, err := c.List(ctx, token, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Search(ctx, token, drive.Query{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Show(ctx, token, "file"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Drives(ctx, token); err != nil {
		t.Fatal(err)
	}
	b, err := c.Media(ctx, token, "file")
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	b, err = c.Export(ctx, token, "file", "mime")
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	if err = writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != len(endpoints) {
		t.Fatal(len(events), endpoints)
	}
	for i, e := range events {
		if e.Name != drive.Called || e.User != caller.UserID || e.RequestID != caller.RequestID || len(e.Attrs) != 3 || e.Attrs["endpoint"] != endpoints[i] || e.Attrs["status"] != int64(200) || e.Attrs["duration_us"] != int64(1) {
			t.Fatal(e)
		}
	}
}
func TestMalformedResources(t *testing.T) {
	// R-J6VM-04V2
	for _, body := range []string{`null`, `[]`, `{"size":null}`, `{"size":1}`, `{"modifiedTime":""}`, `{"modifiedTime":null}`} {
		for _, method := range []string{"show", "list", "search"} {
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				if root(w, r) {
					return
				}
				if method == "show" {
					reply(w, body)
				} else {
					reply(w, `{"files":[`+body+`]}`)
				}
			})
			var err error
			switch method {
			case "show":
				_, err = c.Show(context.Background(), token, "file")
			case "list":
				_, err = c.List(context.Background(), token, "", "")
			case "search":
				_, err = c.Search(context.Background(), token, drive.Query{})
			}
			classify(t, err, drive.ErrProvider)
		}
	}
	c := fake(t, func(w http.ResponseWriter, _ *http.Request) { reply(w, `{"drives":[null]}`) })
	_, err := c.Drives(context.Background(), token)
	classify(t, err, drive.ErrProvider)
}
func TestTelemetryFailureAndNegativeDuration(t *testing.T) {
	// R-JWM4-ZCO6 R-JBR7-J7TU
	for _, failure := range []bool{false, true} {
		var ticks atomic.Int64
		capture := &telemetry.Capture{}
		writer := telemetry.New(telemetry.Config{Service: "gdrive", Sink: capture, Now: func() time.Time {
			return time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(ticks.Add(1)) * time.Microsecond)
		}, Rand: bytes.NewReader(make([]byte, 64))})
		c := drive.NewClient(drive.Config{HTTP: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			if failure {
				return nil, errors.New(token)
			}
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(token))}, nil
		})}, Telemetry: writer})
		_, err := c.Drives(context.Background(), token)
		if err == nil {
			t.Fatal("missing failure")
		}
		if err = writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := capture.Events()
		status := int64(403)
		if failure {
			status = 0
		}
		if len(events) != 1 || events[0].User != "" || events[0].RequestID != "" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"endpoint": drive.EndpointDrives, "status": status, "duration_us": int64(0)}) {
			t.Fatal(events)
		}
		writer.Shutdown(context.Background(), "test")
	}
}
