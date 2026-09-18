package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func projectRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func projectFS(t *testing.T) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(projectRoot(t))
	if err != nil {
		t.Fatalf("open project root: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Errorf("close project root: %v", closeErr)
		}
	})
	return root
}

// R-DJO3-6OQP
func TestModulePackageLayoutAndImports(t *testing.T) {
	t.Parallel()

	root := projectRoot(t)
	moduleFile, err := projectFS(t).ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if firstLine := strings.SplitN(string(moduleFile), "\n", 2)[0]; firstLine != "module github.com/ikigenba/ikigenba/dummy" {
		t.Errorf("module directive = %q", firstLine)
	}

	wantPackages := map[string]string{
		"cmd/dummy":       "main",
		"internal/cli":    "cli",
		"internal/server": "server",
	}
	gotPackages := make(map[string]string)
	imports := make(map[string]map[string]bool)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		relDir, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		relDir = filepath.ToSlash(relDir)
		if previous, exists := gotPackages[relDir]; exists && previous != parsed.Name.Name {
			t.Errorf("directory %s has packages %q and %q", relDir, previous, parsed.Name.Name)
		}
		gotPackages[relDir] = parsed.Name.Name
		if imports[relDir] == nil {
			imports[relDir] = make(map[string]bool)
		}
		for _, imported := range parsed.Imports {
			pathValue, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			imports[relDir][pathValue] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect Go packages: %v", err)
	}
	if !reflect.DeepEqual(gotPackages, wantPackages) {
		t.Errorf("packages = %#v, want %#v", gotPackages, wantPackages)
	}

	const module = "github.com/ikigenba/ikigenba/dummy/"
	if !imports["cmd/dummy"][module+"internal/cli"] {
		t.Error("cmd/dummy does not import internal/cli")
	}
	if !imports["internal/cli"][module+"internal/server"] {
		t.Error("internal/cli does not import internal/server")
	}
	if imports["internal/server"][module+"internal/cli"] {
		t.Error("internal/server imports internal/cli")
	}
}

// R-AK3S-P0DU
func TestModuleHasNoRequirements(t *testing.T) {
	t.Parallel()

	contents, err := projectFS(t).ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	requireDirective := regexp.MustCompile(`(?m)^require(?:[[:space:]]|\()`)
	if requireDirective.Match(contents) {
		t.Errorf("go.mod contains a require directive:\n%s", contents)
	}
}

// R-AMJL-GJV8 R-AOZE-83CM
func TestVersionIsAnInitializedStringVariable(t *testing.T) {
	t.Parallel()

	requireString := func(string) {}
	requireString(Version)
	if Version == "" {
		t.Fatal("Version is empty")
	}

	files, err := filepath.Glob(filepath.Join(projectRoot(t), "internal", "cli", "*.go"))
	if err != nil {
		t.Fatalf("list cli source: %v", err)
	}
	found := false
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range value.Names {
					if name.Name != "Version" {
						continue
					}
					if found {
						t.Fatal("Version declared more than once")
					}
					found = true
					if index >= len(value.Values) {
						t.Error("Version has no declaration initializer")
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("Version variable declaration not found")
	}
}

// R-ANRH-UBLX
func TestVersionIsSemanticVersion(t *testing.T) {
	t.Parallel()

	semver := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	if !semver.MatchString(Version) {
		t.Errorf("Version %q is not v-prefixed Semantic Versioning", Version)
	}
}

// R-ARF6-ZMU0
func TestManifestConstant(t *testing.T) {
	t.Parallel()

	const compiledAsConstant = Manifest
	const want = "app = \"dummy\"\nport = 3000\ndefault = false\nsecrets = []\n"
	if compiledAsConstant != want {
		t.Errorf("Manifest = %q, want %q", compiledAsConstant, want)
	}
}

// R-ASN3-DEKP
func TestCommittedManifestMatchesConstant(t *testing.T) {
	t.Parallel()

	contents, err := projectFS(t).ReadFile("etc/manifest.toml")
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}
	if string(contents) != Manifest {
		t.Errorf("etc/manifest.toml = %q, want Manifest %q", contents, Manifest)
	}
}

// R-ATUZ-R6BE
func TestPackageCheckoutEntries(t *testing.T) {
	t.Parallel()

	root := projectRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "etc"))
	if err != nil {
		t.Fatalf("read etc directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "manifest.toml" {
		t.Errorf("etc entries = %v, want only manifest.toml", entryNames(entries))
	}
	if _, err = os.Stat(filepath.Join(root, "share")); !os.IsNotExist(err) {
		t.Errorf("share directory must not exist; stat error = %v", err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
