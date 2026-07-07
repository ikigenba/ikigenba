package db

import (
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}
