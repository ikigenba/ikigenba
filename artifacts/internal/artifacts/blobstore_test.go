package artifacts

import (
	"bytes"
	"os"
	"testing"
)

// R-3GVI-6VXX
func TestBlobStorePublishesOnlySuccessfullyClosedWrites(t *testing.T) {
	store := &BlobStore{Root: t.TempDir()}
	want := []byte("byte-identical blob contents")
	writer, err := store.Create("complete")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open("complete"); !os.IsNotExist(err) {
		t.Fatalf("partial blob visible before close: %v", err)
	}
	if _, err := writer.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := store.Open("complete")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file.Name())
	_ = file.Close()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read completed blob = %q, %v; want %q", got, err, want)
	}

	abandoned, err := store.Create("abandoned")
	if err != nil {
		t.Fatal(err)
	}
	broken := abandoned.(*blobWriter)
	if err := broken.file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := abandoned.Write([]byte("partial")); err == nil {
		t.Fatal("write to deliberately broken upload unexpectedly succeeded")
	}
	if err := abandoned.Close(); err == nil {
		t.Fatal("closing failed upload unexpectedly succeeded")
	}
	if _, err := store.Open("abandoned"); !os.IsNotExist(err) {
		t.Fatalf("failed upload left readable final blob: %v", err)
	}
}
