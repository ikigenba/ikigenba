package artifacts

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BlobStore stores blobs in the flat blobs directory below Root.
type BlobStore struct {
	Root string
}

// Create returns a writer that atomically publishes the blob when closed.
func (b *BlobStore) Create(id string) (io.WriteCloser, error) {
	final, err := b.path(id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return nil, fmt.Errorf("create blob directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(final), ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary blob: %w", err)
	}
	return &blobWriter{file: temp, temp: temp.Name(), final: final}, nil
}

// Open opens a completed blob.
func (b *BlobStore) Open(id string) (*os.File, error) {
	path, err := b.path(id)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Remove deletes a completed blob.
func (b *BlobStore) Remove(id string) error {
	path, err := b.path(id)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func (b *BlobStore) path(id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("invalid blob id")
	}
	return filepath.Join(b.Root, "blobs", id), nil
}

type blobWriter struct {
	file       *os.File
	temp       string
	final      string
	writeError bool
	closed     bool
}

func (w *blobWriter) abort() {
	if w.closed {
		return
	}
	w.closed = true
	_ = w.file.Close()
	_ = os.Remove(w.temp)
}

func (w *blobWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if err != nil {
		w.writeError = true
	}
	return n, err
}

func (w *blobWriter) Close() error {
	if w.closed {
		return os.ErrClosed
	}
	w.closed = true
	closeErr := w.file.Close()
	if closeErr != nil || w.writeError {
		_ = os.Remove(w.temp)
		if closeErr != nil {
			return closeErr
		}
		return fmt.Errorf("blob write failed")
	}
	if err := os.Rename(w.temp, w.final); err != nil {
		_ = os.Remove(w.temp)
		return fmt.Errorf("publish blob: %w", err)
	}
	return nil
}
