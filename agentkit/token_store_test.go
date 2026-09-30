package agentkit

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// R-ZOPK-NW7S
func TestTokenStoreMethodSetIsExact(t *testing.T) {
	// A stub with only Read and Write satisfies TokenStore (no more methods
	// are required), and every TokenStore converts to an interface with Read
	// and Write (at least these methods are present).
	var store TokenStore = stubTokenStore{data: []byte("token")}
	local := interface {
		Read(ctx context.Context) ([]byte, error)
		Write(ctx context.Context, data []byte) error
	}(store)
	if err := local.Write(context.Background(), []byte("token")); err != nil {
		t.Fatalf("Write() = %v, want nil", err)
	}
	if got, err := local.Read(context.Background()); err != nil || string(got) != "token" {
		t.Fatalf("Read() = %q, %v, want %q, nil", got, err, "token")
	}
}

type stubTokenStore struct{ data []byte }

func (s stubTokenStore) Read(context.Context) ([]byte, error) { return s.data, nil }

func (s stubTokenStore) Write(context.Context, []byte) error { return nil }

// R-ZPXH-1NYH
func TestFileTokenStoreConstructor(t *testing.T) {
	constructor := pinned[func(path string) TokenStore](FileTokenStore)
	if store := constructor(filepath.Join(t.TempDir(), "token.json")); store == nil {
		t.Fatal("FileTokenStore returned a nil TokenStore")
	}
}

// R-ZR5D-FFP6
func TestFileTokenStoreRead(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "token.json")
	want := []byte(`{"access_token":"secret"}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := FileTokenStore(path).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Read() = %q, want %q", got, want)
	}

	_, err = FileTokenStore(filepath.Join(directory, "missing.json")).Read(context.Background())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read() missing-file error = %v, want fs.ErrNotExist", err)
	}
}

// R-ZSD9-T7FV
func TestFileTokenStoreWriteAtomicallyReplacesFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "token.json")
	store := FileTokenStore(path)

	for _, want := range [][]byte{[]byte("first token"), []byte("replacement")} {
		if err := store.Write(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		// The path is wholly contained in this test's temporary directory.
		got, err := fs.ReadFile(os.DirFS(directory), "token.json")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("stored bytes = %q, want %q", got, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if gotMode := info.Mode().Perm(); gotMode != 0o600 {
			t.Fatalf("stored file mode = %04o, want 0600", gotMode)
		}
		assertDirectoryEntries(t, directory, "token.json")
	}
}

// R-ZSD9-T7FV
func TestFileTokenStoreWriteRemovesTemporaryFileOnFailure(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	err := FileTokenStore(target).Write(context.Background(), []byte("token"))
	if err == nil {
		t.Fatal("Write() error = nil, want rename failure")
	}
	assertDirectoryEntries(t, directory, "target")
}

func assertDirectoryEntries(t *testing.T, directory string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(entries))
	for index, entry := range entries {
		got[index] = entry.Name()
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directory entries = %v, want %v", got, want)
	}
}
