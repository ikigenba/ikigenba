package apps_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

func TestSQLiteEngineExactErrorAndPrecedence(t *testing.T) {
	// R-YYOW-R2F7
	//
	const engineError = "'database.engine' must be \"sqlite\""
	for _, database := range []string{
		"[database]\npath = \"state/db\"\n",
		"[database]\nengine = \"postgres\"\npath = \"state/db\"\n",
		"[database]\nengine = \"SQLite\"\npath = \"state/db\"\n",
		"[database]\nengine = 1\npath = \"state/db\"\n",
		"[database]\nengine = true\npath = \"state/db\"\n",
		"database = {engine = [], path = \"state/db\"}\n",
		"database = {engine = {}, path = \"state/db\"}\n",
		"database = {}\n",
	} {
		for _, faults := range []string{"", "mcp = true\n", "[resources]\ncpu_weight = 0\n", "[resources]\nmemory_max = \"bad\"\n", "[resources]\nslice = \"edge\"\n", "[resources]\ngo_memory_limit = \"bad\"\n", "[resources]\nmemory_max = \"256M\"\ngo_memory_limit = \"512M\"\n", "[resources]\ndelegate = 1\n", "[resources]\noom_policy = \"stop\"\n", "[resources]\nunknown = 0\n"} {
			// Put root fields ahead of table headers without changing the faults' scopes.
			document := database + faults
			if faults == "mcp = true\n" {
				document = faults + database
			}
			_, err := apps.ParseManifest([]byte(document))
			if err == nil || err.Error() != engineError {
				t.Fatalf("ParseManifest(%q) = %v, want %q", document, err, engineError)
			}
		}
	}
	for _, path := range []string{"\"../db\"", "42", "[]"} {
		_, err := apps.ParseManifest([]byte("[database]\nengine = false\npath = " + path + "\n"))
		if err == nil || err.Error() != engineError {
			t.Fatalf("path %s: %v", path, err)
		}
	}
	for _, fault := range []struct{ document, want string }{
		{"port = true\n", "'port' is not allowed; the host gives the app its socket"},
		{"description = \"\\n\"\n", "'description' must be one line of text"},
	} {
		_, err := apps.ParseManifest([]byte(fault.document + "[database]\nengine = false\n"))
		if err == nil || err.Error() != fault.want {
			t.Fatalf("higher precedence %q: %v", fault.document, err)
		}
	}
	for _, fault := range []string{"app = \"seed\"\n", "guests = 1\n", "description = 1\n", "env = {X = 1}\n", "broken TOML\n"} {
		_, err := apps.ParseManifest([]byte(fault + "[database]\nengine = false\n"))
		if err == nil || err.Error() == engineError {
			t.Fatalf("higher precedence %q: %v", fault, err)
		}
	}
	model, err := apps.ParseManifest([]byte("[database]\nengine = \"sqlite\"\npath = \"state/new.db\"\n"))
	if err != nil || model.Database.Engine != "sqlite" || model.Database.Path != "state/new.db" {
		t.Fatalf("sqlite = %#v, %v", model, err)
	}
}
