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

// R-5C8P-YLY9
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
		".":               "dummy",
		"cmd/dummy":       "main",
		"internal/cli":    "cli",
		"internal/server": "server",
		"internal/panel":  "panel",
		"internal/widget": "widget",
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

	const moduleRoot = "github.com/ikigenba/ikigenba/dummy"
	const module = moduleRoot + "/"
	wantImports := map[string]map[string]bool{
		".":               {},
		"cmd/dummy":       {module + "internal/cli": true},
		"internal/cli":    {module + "internal/server": true, module + "internal/panel": true, module + "internal/widget": true},
		"internal/panel":  {moduleRoot: true, module + "internal/widget": true},
		"internal/server": {},
		"internal/widget": {},
	}
	for directory, packageImports := range imports {
		local := make(map[string]bool)
		for imported := range packageImports {
			if imported == moduleRoot || strings.HasPrefix(imported, module) {
				local[imported] = true
			}
		}
		if !reflect.DeepEqual(local, wantImports[directory]) {
			t.Errorf("%s module imports = %v, want %v", directory, local, wantImports[directory])
		}
		if directory == "." && !reflect.DeepEqual(packageImports, map[string]bool{"embed": true}) {
			t.Errorf("root package imports = %v, want embed only", packageImports)
		}
	}
}

// R-AK3S-P0DU
func TestModuleHasNoRequirements(t *testing.T) {
	t.Parallel()

	contents, err := projectFS(t).ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	requireDirective := regexp.MustCompile(`(?m)^[[:blank:]]*require(?:[[:space:]]|\()`)
	if requireDirective.Match(contents) {
		t.Errorf("go.mod contains a require directive:\n%s", contents)
	}
}

// R-AMJL-GJV8
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

// R-AOZE-83CM
func TestVersionComesFromDeclarationInitializer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(projectRoot(t), "internal", "cli", "release.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse release source: %v", err)
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
				if index >= len(value.Values) {
					t.Fatal("Version has no declaration initializer")
				}
				literal, ok := value.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("Version initializer is %T, want a string literal", value.Values[index])
				}
				declared, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil {
					t.Fatalf("unquote Version initializer: %v", unquoteErr)
				}
				if Version != declared {
					t.Errorf("Version = %q, declaration initializes %q", Version, declared)
				}
				return
			}
		}
	}
	t.Fatal("Version declaration not found")
}

// R-ANRH-UBLX
func TestVersionIsSemanticVersion(t *testing.T) {
	t.Parallel()

	semver := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	if !semver.MatchString(Version) {
		t.Errorf("Version %q is not v-prefixed Semantic Versioning", Version)
	}
}

// R-LI0D-VJTO
func TestManifestConstant(t *testing.T) {
	t.Parallel()

	const compiledAsConstant = Manifest
	const want = "app = \"dummy\"\ndefault = false\nsecrets = []\n"
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
