//go:build live

package dropbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func newLiveClient(t *testing.T) *Client {
	t.Helper()
	key := os.Getenv("DROPBOX_APP_KEY")
	secret := os.Getenv("DROPBOX_APP_SECRET")
	refresh := os.Getenv("DROPBOX_REFRESH_TOKEN")
	if key == "" || secret == "" || refresh == "" {
		t.Skip("DROPBOX_* secrets not present; skipping live Dropbox smoke")
	}
	return NewClient(Config{
		AppKey: key, AppSecret: secret, RefreshToken: refresh, AppFolderRoot: os.Getenv("DROPBOX_APP_FOLDER_ROOT"),
	}, nil)
}

func livePath(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("/phase16-%s-%d", name, time.Now().UnixNano())
}

func liveEntries(t *testing.T, c *Client) []DeltaEntry {
	t.Helper()
	ctx := context.Background()
	page, err := c.ListFolder(ctx)
	if err != nil {
		t.Fatalf("list_folder: %v", err)
	}
	entries := page.Entries
	for page.HasMore {
		page, err = c.ListFolderContinue(ctx, page.Cursor)
		if err != nil {
			t.Fatalf("list_folder/continue: %v", err)
		}
		entries = append(entries, page.Entries...)
	}
	return entries
}

func findLiveEntry(entries []DeltaEntry, path string) (DeltaEntry, bool) {
	for _, entry := range entries {
		if strings.EqualFold(entry.PathDisplay, path) {
			return entry, true
		}
	}
	return DeltaEntry{}, false
}

// R-KEIO-B98F
func TestLiveUploadOverwriteReturnsListedRevision(t *testing.T) {
	c := newLiveClient(t)
	path := livePath(t, "overwrite.txt")
	defer c.DeletePath(context.Background(), path)

	rev, err := c.Upload(context.Background(), path, strings.NewReader("phase 16 overwrite"), int64(len("phase 16 overwrite")))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if rev == "" {
		t.Fatal("Upload returned an empty rev")
	}
	entry, ok := findLiveEntry(liveEntries(t, c), path)
	if !ok || entry.Rev != rev {
		t.Fatalf("list_folder entry = %+v, found=%v; want uploaded path with rev %q", entry, ok, rev)
	}
}

// R-KFQK-P0Z4
func TestLiveSessionUploadIsRetrievedWhole(t *testing.T) {
	c := newLiveClient(t)
	path := livePath(t, "session.bin")
	defer c.DeletePath(context.Background(), path)
	const size = uploadSimpleLimit + 1

	rev, err := c.Upload(context.Background(), path, io.LimitReader(&repeatingReader{}, size), size)
	if err != nil {
		t.Fatalf("session Upload: %v", err)
	}
	entry, ok := findLiveEntry(liveEntries(t, c), path)
	if !ok || entry.Rev != rev || entry.Size != uint64(size) {
		t.Fatalf("list_folder entry = %+v, found=%v; want rev %q and size %d", entry, ok, rev, size)
	}
	data, metadata, err := c.Download(context.Background(), path, rev)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if int64(len(data)) != size || metadata.Rev != rev || metadata.ContentHash != entry.ContentHash {
		t.Fatalf("download = len %d metadata %+v; want size %d, rev %q, hash %q", len(data), metadata, size, rev, entry.ContentHash)
	}
	if err := VerifyContentHash(data, entry.ContentHash); err != nil {
		t.Fatalf("retrieved content hash: %v", err)
	}
}

// R-KGYH-2SPT
func TestLiveCreateDeleteAndMoveAreObservableInListFolder(t *testing.T) {
	c := newLiveClient(t)
	dir := livePath(t, "folder")
	from := dir + "/from.txt"
	to := dir + "/to.txt"
	defer c.DeletePath(context.Background(), dir)

	if err := c.CreateFolder(context.Background(), dir); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if entry, ok := findLiveEntry(liveEntries(t, c), dir); !ok || entry.Tag != TagFolder {
		t.Fatalf("list_folder after create = %+v, found=%v; want folder", entry, ok)
	}
	if _, err := c.Upload(context.Background(), from, strings.NewReader("move me"), int64(len("move me"))); err != nil {
		t.Fatalf("Upload before Move: %v", err)
	}
	if err := c.Move(context.Background(), from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}
	entries := liveEntries(t, c)
	if _, ok := findLiveEntry(entries, from); ok {
		t.Fatalf("list_folder retained moved source %q", from)
	}
	if _, ok := findLiveEntry(entries, to); !ok {
		t.Fatalf("list_folder did not contain moved destination %q", to)
	}
	if err := c.DeletePath(context.Background(), dir); err != nil {
		t.Fatalf("DeletePath: %v", err)
	}
	if _, ok := findLiveEntry(liveEntries(t, c), dir); ok {
		t.Fatalf("list_folder retained deleted folder %q", dir)
	}
}
