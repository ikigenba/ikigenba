package auth_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/auth"
)

var _ func() fs.FS = auth.Assets

func TestEmbeddedAssetsIgnoreWorkingDirectory(t *testing.T) {
	// R-3BYY-W6QV R-G8TD-0BHK
	read := func() map[string]string {
		t.Helper()
		files := auth.Assets()
		entries, err := fs.ReadDir(files, ".")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		contents := map[string]string{}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("asset is not regular: %s", entry.Name())
			}
			names = append(names, entry.Name())
			data, err := fs.ReadFile(files, entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			contents[entry.Name()] = string(data)
		}
		if !reflect.DeepEqual(names, []string{"approve.html", "mcp-clients.html", "page.html", "profile.html", "sign-in.html", "token-create.html", "token-created.html"}) {
			t.Fatalf("assets: %v", names)
		}
		return contents
	}
	before := read()
	t.Chdir(t.TempDir())
	for range 2 {
		if after := read(); !reflect.DeepEqual(after, before) {
			t.Fatal("embedded assets changed with working directory")
		}
	}
}
