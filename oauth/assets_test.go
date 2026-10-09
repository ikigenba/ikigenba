package oauth_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/oauth"
)

// R-GQNY-95SV
func TestAssetsExportsExactlyTheCallbackFiles(t *testing.T) {
	t.Parallel()

	requireSignature := func(assets func() fs.FS) func() fs.FS { return assets }
	assets := requireSignature(oauth.Assets)
	entries, err := fs.ReadDir(assets(), ".")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	wantNames := []string{"failure.html", "success.html"}
	if len(entries) != len(wantNames) {
		t.Fatalf("Assets root has %d entries, want %d", len(entries), len(wantNames))
	}
	for index, entry := range entries {
		if entry.Name() != wantNames[index] {
			t.Errorf("root entry %d = %q, want %q", index, entry.Name(), wantNames[index])
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info(%q) error = %v", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("root entry %q mode = %v, want regular file", entry.Name(), info.Mode())
		}
	}
}

// R-GRVU-MXJK
func TestAssetsRemainIdenticalAcrossCallsAndWorkingDirectories(t *testing.T) {
	before := readAssets(t, oauth.Assets())
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	for _, directory := range []string{firstDirectory, secondDirectory} {
		t.Chdir(directory)
		for range 2 {
			got := readAssets(t, oauth.Assets())
			if len(got) != len(before) {
				t.Fatalf("Assets returned %d files in %q, want %d", len(got), directory, len(before))
			}
			for name, want := range before {
				contents, found := got[name]
				if !found || !bytes.Equal(contents, want) {
					t.Errorf("Assets contents of %q changed in %q", name, directory)
				}
			}
		}
	}
}

func readAssets(t *testing.T, assets fs.FS) map[string][]byte {
	t.Helper()
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	contents := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		data, err := fs.ReadFile(assets, entry.Name())
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", entry.Name(), err)
		}
		contents[entry.Name()] = data
	}
	return contents
}
