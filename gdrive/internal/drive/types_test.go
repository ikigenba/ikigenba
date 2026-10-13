package drive_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
)

func TestKindsAndFormats(t *testing.T) {
	// R-I9YB-OBUB R-IIHM-CQ16 R-PI0C-SP0V R-IG1T-L6JS R-IJPI-QHRV R-IKXF-49IK
	gotKinds := []drive.Kind{drive.KindFile, drive.KindFolder, drive.KindShortcut, drive.KindDocument, drive.KindSpreadsheet, drive.KindPresentation, drive.KindDrawing, drive.KindOther}
	wantKinds := []drive.Kind{"file", "folder", "shortcut", "document", "spreadsheet", "presentation", "drawing", "other"}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatal("kind constants", gotKinds)
	}
	kinds := []struct {
		mime string
		kind drive.Kind
	}{
		{"application/vnd.google-apps.folder", drive.KindFolder}, {"application/vnd.google-apps.shortcut", drive.KindShortcut}, {"application/vnd.google-apps.document", drive.KindDocument}, {"application/vnd.google-apps.spreadsheet", drive.KindSpreadsheet}, {"application/vnd.google-apps.presentation", drive.KindPresentation}, {"application/vnd.google-apps.drawing", drive.KindDrawing}, {"application/vnd.google-apps.form", drive.KindOther}, {"", drive.KindFile}, {"image/jpeg", drive.KindFile}, {"application/octet-stream", drive.KindFile},
	}
	for _, item := range kinds {
		if got := drive.KindOf(item.mime); got != item.kind {
			t.Errorf("kind %q: %q", item.mime, got)
		}
	}
	table := map[drive.Kind][]string{
		drive.KindDocument:     {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.oasis.opendocument.text", "application/rtf", "application/pdf", "text/plain", "text/html", "application/zip", "application/epub+zip", "text/markdown"},
		drive.KindSpreadsheet:  {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.oasis.opendocument.spreadsheet", "application/pdf", "application/zip", "text/csv", "text/tab-separated-values"},
		drive.KindPresentation: {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.oasis.opendocument.presentation", "application/pdf", "text/plain"},
		drive.KindDrawing:      {"application/pdf", "image/jpeg", "image/png", "image/svg+xml"},
		drive.KindFile:         {}, drive.KindFolder: {}, drive.KindShortcut: {}, drive.KindOther: {}, drive.Kind("unknown"): {},
	}
	for kind, want := range table {
		got := drive.Formats(kind)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("formats %s: %v", kind, got)
		}
		def := ""
		if len(want) > 0 {
			def = want[0]
			got[0] = "changed"
			if drive.Formats(kind)[0] != want[0] {
				t.Fatal("formats shares storage")
			}
		}
		if kind == drive.KindDrawing {
			def = "image/png"
		}
		if drive.DefaultFormat(kind) != def {
			t.Errorf("default %s", kind)
		}
	}
}
func TestObjectJSON(t *testing.T) {
	// R-IB68-23L0 R-Y86C-24YC R-IM5B-I199 R-IND7-VSZY
	zero := int64(0)
	stamp := time.Date(2032, 1, 2, 3, 4, 5, 0, time.UTC)
	f := drive.File{"file-id", "name", drive.KindFile, "text/plain", &zero, stamp, "folder-id", "drive-id", "link", &drive.Target{"target-id", drive.KindDocument}}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"file-id","name":"name","kind":"file","mime_type":"text/plain","bytes":0,"modified":"2032-01-02T03:04:05Z","folder":"folder-id","drive":"drive-id","link":"link","target":{"id":"target-id","kind":"document"}}`
	if string(b) != want {
		t.Fatalf("json %s", b)
	}
	f.Bytes = nil
	f.Folder = ""
	f.Drive = ""
	f.Target = nil
	b, err = json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"id":"file-id","name":"name","kind":"file","mime_type":"text/plain","modified":"2032-01-02T03:04:05Z","link":"link"}`
	if string(b) != want {
		t.Fatalf("json %s", b)
	}
	d := drive.Drive{"shared-id", "Shared"}
	b, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"id":"shared-id","name":"Shared"}` {
		t.Fatalf("drive json %s", b)
	}
	p := drive.Page{[]drive.File{f}, "next"}
	q := drive.Query{"text", "folder", "drive", "page", 31}
	u := drive.Upload{"file", "name", "folder", "type", 3}
	if !reflect.DeepEqual(p.Files, []drive.File{f}) || p.Next != "next" || q.Text != "text" || q.Folder != "folder" || q.Drive != "drive" || q.Page != "page" || q.Size != 31 || u.File != "file" || u.Name != "name" || u.Folder != "folder" || u.ContentType != "type" || u.Bytes != 3 {
		t.Fatal("public types")
	}
}
func TestConstants(t *testing.T) {
	// R-Y6YF-OD7N R-I8QF-AK3M R-PGSG-EXA6
	const defaultBaseURL = drive.DefaultBaseURL
	const pushChunk = drive.PushChunk
	const called = drive.Called
	if defaultBaseURL != "https://www.googleapis.com" || pushChunk != 8<<20 || drive.ExportCap != 10_000_000 || called != "drive.called" {
		t.Fatal("constants")
	}
	got := []string{drive.EndpointList, drive.EndpointGet, drive.EndpointMedia, drive.EndpointExport, drive.EndpointDrives, drive.EndpointCreate, drive.EndpointUpdate, drive.EndpointChunk}
	want := []string{"files.list", "files.get", "files.get.media", "files.export", "drives.list", "files.create", "files.update", "upload.chunk"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
