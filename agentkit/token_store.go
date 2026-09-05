package agentkit

import (
	"context"
	"os"
	"path/filepath"
)

// TokenStore is where an OAuth rotator keeps its bytes. The bytes are opaque to
// the store. This is the sole intended extension point of the OAuth path.
// R-ZOPK-NW7S
type TokenStore interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
}

type fileTokenStore struct {
	path string
}

// FileTokenStore is a TokenStore over one file. Read passes the OS error
// through unchanged, so a consumer can detect "not logged in yet" with
// errors.Is(err, fs.ErrNotExist). Write is atomic and creates the file 0600.
// R-ZPXH-1NYH
func FileTokenStore(path string) TokenStore {
	return fileTokenStore{path: path}
}

// R-ZR5D-FFP6
func (s fileTokenStore) Read(_ context.Context) ([]byte, error) {
	return os.ReadFile(s.path)
}

// R-ZSD9-T7FV
func (s fileTokenStore) Write(_ context.Context, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".agentkit-token-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, s.path)
}
