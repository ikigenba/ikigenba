package apps_test

import (
	"os"
	"testing"
)

type commandCall struct {
	name string
	args []string
}

func assertMode(t *testing.T, name string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatalf("stat %s: %v", name, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %v, want %v", name, info.Mode().Perm(), want)
	}
}
