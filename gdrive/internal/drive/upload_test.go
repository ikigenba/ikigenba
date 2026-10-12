package drive_test

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
)

func TestUploadProtocol(t *testing.T) {
	// R-PMVY-BRZN R-RBL2-WPHG R-PPBR-3BH1 R-PRRJ-UUYF R-PSZG-8MP4 R-PU7C-MEFT
	for _, existing := range []bool{false, true} {
		for _, named := range []bool{false, true} {
			for _, size := range []int{0, 7, drive.PushChunk + 3} {
				t.Run(fmt.Sprintf("existing=%t/name=%t/size=%d", existing, named, size), func(t *testing.T) {
					data := bytes.Repeat([]byte("x"), size)
					var pieces atomic.Int32
					var opening atomic.Int32
					session := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						part := int(pieces.Add(1)) - 1
						if r.Method != "PUT" || r.Header.Get("Authorization") != "" || r.URL.RequestURI() != "/secret?credential=private" {
							t.Error("piece request", r.Method, r.URL)
						}
						got, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
						}
						start := part * drive.PushChunk
						end := min(start+drive.PushChunk, size)
						if !bytes.Equal(got, data[start:end]) {
							t.Error("piece bytes")
						}
						rangeValue := "bytes */0"
						if size > 0 {
							rangeValue = fmt.Sprintf("bytes %d-%d/%d", start, end-1, size)
						}
						if r.Header.Get("Content-Range") != rangeValue {
							t.Error("range", r.Header)
						}
						if end < size {
							w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", end-1))
							w.WriteHeader(308)
						} else {
							w.WriteHeader(201)
							reply(w, normal)
						}
					}))
					defer session.Close()
					c := fake(t, func(w http.ResponseWriter, r *http.Request) {
						if root(w, r) {
							return
						}
						opening.Add(1)
						if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Content-Type") != "application/json; charset=UTF-8" || r.Header.Get("X-Upload-Content-Type") != "text/custom" || r.Header.Get("X-Upload-Content-Length") != fmt.Sprint(size) {
							t.Error("opening headers", r.Header)
						}
						q := r.URL.Query()
						if len(q["uploadType"]) != 1 || len(q["supportsAllDrives"]) != 1 || len(q["fields"]) != 1 || q.Get("uploadType") != "resumable" || q.Get("supportsAllDrives") != "true" || q.Get("fields") != "id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink" {
							t.Error("opening query", q)
						}
						var got map[string]any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Error(err)
						}
						want := map[string]any{}
						if existing {
							if r.Method != "PATCH" || r.URL.EscapedPath() != "/upload/drive/v3/files/a%2Fb" {
								t.Error("update path", r.URL)
							}
							if named {
								want["name"] = "new-name"
							}
						} else {
							if r.Method != "POST" || r.URL.Path != "/upload/drive/v3/files" {
								t.Error("create path")
							}
							name := ""
							folder := "my-root"
							if named {
								name = "new-name"
								folder = "shared-folder"
							}
							want["name"] = name
							want["parents"] = []any{folder}
						}
						if !reflect.DeepEqual(got, want) {
							t.Error("metadata", got, want)
						}
						w.Header().Set("Location", session.URL+"/secret?credential=private")
					})
					u := drive.Upload{ContentType: "text/custom", Bytes: int64(size)}
					if existing {
						u.File = "a/b"
						u.Folder = "ignored"
					}
					if named {
						u.Name = "new-name"
						if !existing {
							u.Folder = "shared-folder"
						}
					}
					reader := bytes.NewReader(append(data, 'z'))
					f, err := c.Upload(context.Background(), token, u, reader)
					if err != nil || f.ID != "file-id" {
						t.Fatal(f, err)
					}
					if opening.Load() != 1 || int(pieces.Load()) != max(1, (size+drive.PushChunk-1)/drive.PushChunk) || reader.Len() != 1 {
						t.Fatal("request/read counts", opening.Load(), pieces.Load(), reader.Len())
					}
				})
			}
		}
	}
}
func TestUploadFailures(t *testing.T) {
	// R-PQJN-H37Q R-PVF9-066I R-PWN5-DXX7 R-JCZ3-WZKJ R-PXV1-RPNW
	for _, status := range []int{403, 404, 500, 200} {
		for _, location := range []string{"", "/relative", "%%%", "ftp://127.0.0.1/session?credential=private"} {
			var calls atomic.Int32
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				if root(w, r) {
					return
				}
				calls.Add(1)
				w.Header().Set("Location", location)
				w.WriteHeader(status)
			})
			f, err := c.Upload(context.Background(), token, drive.Upload{}, strings.NewReader(""))
			want := drive.ErrProvider
			if status == 403 {
				want = drive.ErrForbidden
			}
			if status == 404 {
				want = drive.ErrNotFound
			}
			classify(t, err, want)
			if !reflect.DeepEqual(f, drive.File{}) || calls.Load() != 1 {
				t.Fatal(f, calls.Load())
			}
		}
	}
	for _, status := range []int{200, 201, 308, 403, 404, 500} {
		for _, size := range []int64{2, int64(drive.PushChunk + 2)} {
			var calls atomic.Int32
			var address string
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				if root(w, r) {
					return
				}
				if r.Method != "PUT" {
					w.Header().Set("Location", address)
					return
				}
				calls.Add(1)
				w.WriteHeader(status)
				reply(w, token+address)
			}) // obtain session through a separate fake with captured URL
			session := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				reply(w, token+address)
			}))
			address = session.URL + "/session?secret=hidden"
			defer session.Close()
			_, err := c.Upload(context.Background(), token, drive.Upload{Bytes: size}, bytes.NewReader(make([]byte, size)))
			want := drive.ErrProvider
			if status == 403 {
				want = drive.ErrForbidden
			}
			classify(t, err, want)
			if calls.Load() != 1 || strings.Contains(err.Error(), address) || strings.Contains(err.Error(), "secret=hidden") || strings.Contains(err.Error(), token) {
				t.Fatal(err, calls.Load())
			}
		}
	}
	for _, rangeValue := range []string{"", "bytes=0-1", "bytes=0-8388608"} {
		var calls atomic.Int32
		var address string
		session := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Range", rangeValue)
			w.WriteHeader(308)
		}))
		address = session.URL
		defer session.Close()
		c := fake(t, func(w http.ResponseWriter, r *http.Request) {
			if !root(w, r) {
				w.Header().Set("Location", address)
			}
		})
		_, err := c.Upload(context.Background(), token, drive.Upload{Bytes: int64(drive.PushChunk + 1)}, bytes.NewReader(make([]byte, drive.PushChunk+1)))
		classify(t, err, drive.ErrProvider)
		if calls.Load() != 1 {
			t.Fatal(calls.Load())
		}
	}
	// R-PU7C-MEFT
	for _, body := range []string{`[]`, `null`, `broken`, `{"size":"bad"}`, `{"size":null}`, `{"size":1}`, `{"modifiedTime":"bad"}`, `{"modifiedTime":null}`, `{"modifiedTime":""}`} {
		var address string
		session := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reply(w, body) }))
		address = session.URL
		defer session.Close()
		c := fake(t, func(w http.ResponseWriter, r *http.Request) {
			if !root(w, r) {
				w.Header().Set("Location", address)
			}
		})
		_, err := c.Upload(context.Background(), token, drive.Upload{}, strings.NewReader(""))
		classify(t, err, drive.ErrProvider)
	}
	cause := errors.New("artifact-read-error-with-" + token)
	for _, after := range []int{0, drive.PushChunk} {
		for _, readerError := range []error{io.EOF, cause} {
			var pieces atomic.Int32
			var address string
			session := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pieces.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", drive.PushChunk-1))
				w.WriteHeader(308)
			}))
			address = session.URL
			defer session.Close()
			c := fake(t, func(w http.ResponseWriter, r *http.Request) {
				if !root(w, r) {
					w.Header().Set("Location", address)
				}
			})
			body := io.MultiReader(bytes.NewReader(make([]byte, after)), failReader{readerError})
			f, err := c.Upload(context.Background(), token, drive.Upload{Bytes: int64(after + 1)}, body)
			want := readerError
			if errors.Is(readerError, io.EOF) {
				want = io.ErrUnexpectedEOF
			}
			if !errors.Is(err, want) || errors.Is(err, drive.ErrProvider) || !reflect.DeepEqual(f, drive.File{}) || int(pieces.Load()) != after/drive.PushChunk || strings.Contains(err.Error(), token) {
				t.Fatal(f, err, pieces.Load())
			}
		}
	}
}

type failReader struct{ err error }

func (f failReader) Read([]byte) (int, error) { return 0, f.err }
func TestUploadTelemetry(t *testing.T) {
	// R-JAJB-5G35 R-PXV1-RPNW
	for _, existing := range []bool{false, true} {
		capture := &telemetry.Capture{}
		writer := telemetry.New(telemetry.Config{Service: "gdrive", Sink: capture, Now: func() time.Time { return time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC) }, Rand: bytes.NewReader(make([]byte, 128))})
		var address string
		session := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reply(w, normal) }))
		address = session.URL + "/secret?credential=private"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !root(w, r) {
				w.Header().Set("Location", address)
			}
		}))
		c := drive.NewClient(drive.Config{HTTP: server.Client(), BaseURL: server.URL, Telemetry: writer})
		u := drive.Upload{}
		if existing {
			u.File = "file"
		}
		if _, err := c.Upload(context.Background(), token, u, strings.NewReader("")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := capture.Events()
		want := drive.EndpointCreate
		if existing {
			want = drive.EndpointUpdate
		}
		if len(events) != 3 {
			t.Fatal(events)
		}
		endpoints := []string{drive.EndpointGet, want, drive.EndpointChunk}
		for i, e := range events {
			if e.Attrs["endpoint"] != endpoints[i] || e.Attrs["status"] != int64(200) || len(e.Attrs) != 3 || e.Attrs["duration_us"] != int64(0) {
				t.Fatal(e)
			}
			for _, v := range e.Attrs {
				value := fmt.Sprint(v)
				if strings.Contains(value, token) || strings.Contains(value, address) || strings.Contains(value, "credential=private") {
					t.Fatal(e)
				}
			}
		}
		writer.Shutdown(context.Background(), "test")
		session.Close()
		server.Close()
	}
}

// finalFailure returns its failure alongside all requested bytes.
type finalFailure struct{ err error }

func (f finalFailure) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), f.err
}
func TestUploadBodyErrorWithFinalBytes(t *testing.T) {
	// R-JCZ3-WZKJ R-PWN5-DXX7
	cause := errors.New("artifact failed")
	var address string
	session := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { t.Error("failed piece sent") }))
	defer session.Close()
	address = session.URL
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if !root(w, r) {
			w.Header().Set("Location", address)
		}
	})
	_, err := c.Upload(context.Background(), token, drive.Upload{Bytes: 2}, finalFailure{cause})
	if !errors.Is(err, cause) || errors.Is(err, drive.ErrProvider) {
		t.Fatal(err)
	}
}
