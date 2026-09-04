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
	storeType := reflect.TypeFor[TokenStore]()
	if storeType.Name() != "TokenStore" || storeType.Kind() != reflect.Interface {
		t.Fatalf("TokenStore name/kind = %q/%s, want TokenStore/interface", storeType.Name(), storeType.Kind())
	}

	wantMethods := map[string]reflect.Type{
		"Read":  reflect.TypeOf(func(context.Context) ([]byte, error) { return nil, nil }),
		"Write": reflect.TypeOf(func(context.Context, []byte) error { return nil }),
	}
	if storeType.NumMethod() != len(wantMethods) {
		t.Fatalf("TokenStore method count = %d, want %d", storeType.NumMethod(), len(wantMethods))
	}
	for name, wantType := range wantMethods {
		method, ok := storeType.MethodByName(name)
		if !ok || method.Type != wantType {
			t.Fatalf("TokenStore.%s = %v (present=%t), want %s", name, method.Type, ok, wantType)
		}
	}
}

// R-ZPXH-1NYH
func TestFileTokenStoreConstructor(t *testing.T) {
	wantSignature := reflect.TypeOf(func(string) TokenStore { return nil })
	if got := reflect.TypeOf(FileTokenStore); got != wantSignature {
		t.Fatalf("FileTokenStore signature = %s, want %s", got, wantSignature)
	}
	if store := FileTokenStore(filepath.Join(t.TempDir(), "token.json")); store == nil {
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
