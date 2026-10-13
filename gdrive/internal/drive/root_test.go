package drive_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
)

func callDrive(c *drive.Client, accessToken, method string) error {
	ctx := context.Background()
	switch method {
	case "list":
		_, err := c.List(ctx, accessToken, "explicit-folder", "page", 31)
		return err
	case "search":
		_, err := c.Search(ctx, accessToken, drive.Query{Folder: "explicit-folder", Drive: "shared-drive", Page: "page"})
		return err
	case "show", "show-root":
		id := "file"
		if method == "show-root" {
			id = "root"
		}
		_, err := c.Show(ctx, accessToken, id)
		return err
	case "upload", "upload-folder", "upload-existing":
		u := drive.Upload{}
		if method == "upload-folder" {
			u.Folder = "explicit-folder"
		}
		if method == "upload-existing" {
			u.File = "file"
		}
		_, err := c.Upload(ctx, accessToken, u, strings.NewReader(""))
		return err
	case "drives":
		_, err := c.Drives(ctx, accessToken)
		return err
	default:
		var body io.ReadCloser
		var err error
		if method == "media" {
			body, err = c.Media(ctx, accessToken, "root")
		} else {
			body, err = c.Export(ctx, accessToken, "root", "text/plain")
		}
		if body != nil {
			_ = body.Close()
		}
		return err
	}
}

func TestRootLookupPerCall(t *testing.T) {
	// R-R6PH-DMIO
	for _, method := range []string{"list", "search", "show", "show-root", "upload", "upload-folder", "upload-existing", "drives", "media", "export"} {
		t.Run(method, func(t *testing.T) {
			needsRoot := method != "drives" && method != "media" && method != "export"
			var requests []string
			var accessToken string
			c := drive.NewClient(drive.Config{BaseURL: "https://fake.invalid", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				lookup := r.URL.Path == "/drive/v3/files/root" && r.URL.Query().Get("fields") == "id"
				body := normal
				header := make(http.Header)
				if lookup {
					q := r.URL.Query()
					if !needsRoot || len(requests) != 0 || r.Method != http.MethodGet || !reflect.DeepEqual(q["fields"], []string{"id"}) || !reflect.DeepEqual(q["supportsAllDrives"], []string{"true"}) || r.Header.Get("Authorization") != "Bearer "+accessToken {
						t.Error("incorrect initial root lookup", r.URL, requests)
					}
					requests = append(requests, "lookup")
					body = `{"id":"caller-root"}`
				} else {
					if needsRoot && (len(requests) == 0 || requests[0] != "lookup") {
						t.Error("request preceded root lookup", r.URL)
					}
					requests = append(requests, r.Method+" "+r.URL.Path)
					switch r.URL.Path {
					case "/drive/v3/files":
						body = `{"files":[]}`
					case "/drive/v3/drives":
						body = `{"drives":[]}`
					case "/upload/drive/v3/files", "/upload/drive/v3/files/file":
						header.Set("Location", "https://session.invalid/piece")
					}
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}})
			for _, value := range []string{"first-caller-token", "second-caller-token"} {
				accessToken = value
				requests = nil
				if err := callDrive(c, value, method); err != nil {
					t.Fatal(err)
				}
				want := 1
				if needsRoot {
					want = 2
				}
				if strings.HasPrefix(method, "upload") {
					want = 3
				}
				if len(requests) != want {
					t.Fatalf("requests %v want %d", requests, want)
				}
			}
		})
	}
}

func TestReductionUsesRootOfSameCall(t *testing.T) {
	// R-RAD6-IXQR R-LGPI-KSS4 R-PJ89-6GRK
	for _, method := range []string{"list", "search", "show", "upload"} {
		for _, parents := range []string{"", `,"parents":[]`, `,"parents":["caller-root","ignored"]`, `,"parents":["shared-drive","ignored"]`, `,"parents":["nested-folder","caller-root"]`, `,"parents":["previous-root"]`} {
			for _, native := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/native=%t", method, parents, native), func(t *testing.T) {
					mime := "text/plain"
					if native {
						mime = "application/vnd.google-apps.document"
					}
					resource := `{"id":"chosen-file","name":"chosen-name","mimeType":"` + mime + `","size":"1024","modifiedTime":"2032-01-02T03:04:05+02:00","driveId":"shared-drive","webViewLink":"chosen-link"` + parents + `}`
					rootID := "previous-root"
					c := drive.NewClient(drive.Config{BaseURL: "https://fake.invalid", HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
						body := resource
						header := make(http.Header)
						switch {
						case r.URL.Path == "/drive/v3/files/root" && r.URL.Query().Get("fields") == "id":
							body = `{"id":"` + rootID + `"}`
						case r.URL.Path == "/drive/v3/files":
							if !reflect.DeepEqual(r.URL.Query()["fields"], []string{"nextPageToken,files(id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink)"}) {
								t.Error("list fields", r.URL)
							}
							body = `{"files":[` + resource + `]}`
						case r.URL.Path == "/drive/v3/files/file":
							if !reflect.DeepEqual(r.URL.Query()["fields"], []string{"id,name,mimeType,size,modifiedTime,parents,driveId,webViewLink,trashed,shortcutDetails"}) {
								t.Error("show fields", r.URL)
							}
						case r.URL.Path == "/upload/drive/v3/files":
							header.Set("Location", "https://session.invalid/piece")
						}
						return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
					})}})
					// Prime the same client with another caller's root; no root can be cached across calls.
					if err := callDrive(c, "previous-caller-token", method); err != nil {
						t.Fatal(err)
					}
					rootID = "caller-root"
					var f drive.File
					var err error
					switch method {
					case "list", "search":
						var page drive.Page
						if method == "list" {
							page, err = c.List(context.Background(), token, "", "", 31)
						} else {
							page, err = c.Search(context.Background(), token, drive.Query{})
						}
						if err == nil && len(page.Files) == 1 {
							f = page.Files[0]
						} else {
							t.Fatal(page, err)
						}
					case "show":
						f, err = c.Show(context.Background(), token, "file")
					case "upload":
						f, err = c.Upload(context.Background(), token, drive.Upload{}, strings.NewReader(""))
					}
					folder := ""
					if strings.Contains(parents, `"nested-folder"`) {
						folder = "nested-folder"
					} else if strings.Contains(parents, `"previous-root"`) {
						folder = "previous-root"
					}
					want := drive.File{ID: "chosen-file", Name: "chosen-name", MimeType: mime, Kind: drive.KindFile, Modified: time.Date(2032, 1, 2, 1, 4, 5, 0, time.UTC), Drive: "shared-drive", Link: "chosen-link", Folder: folder}
					if native {
						want.Kind = drive.KindDocument
					} else {
						size := int64(1024)
						want.Bytes = &size
					}
					if err != nil || !reflect.DeepEqual(f, want) {
						t.Fatalf("file %#v want %#v, error %v", f, want, err)
					}
				})
			}
		}
	}
}
