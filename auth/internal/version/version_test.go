package version

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestVersionIsSourceLiteral(t *testing.T) {
	// R-3O47-G1QT: this package owns the release version and exports Version.
	if Version == "" {
		t.Fatal("Version is empty")
	}

	// R-3SZS-Z4PL: Version is a string literal of shape v<semver>, read from
	// the source that declares it, and a plain build yields that literal.
	src, err := os.ReadFile(filepath.Join(dir(t), "version.go"))
	if err != nil {
		t.Fatalf("read version.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "version.go", src, 0)
	if err != nil {
		t.Fatalf("parse version.go: %v", err)
	}
	var literal string
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "Version" || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Fatal("Version is not initialized to a string literal")
			return false
		}
		literal = lit.Value[1 : len(lit.Value)-1]
		return false
	})
	if literal == "" {
		t.Fatal("Version declaration not found")
	}
	if !regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`).MatchString(literal) {
		t.Fatalf("Version literal %q is not v<semver>", literal)
	}
	if Version != literal {
		t.Fatalf("Version = %q, source literal %q; a plain build must yield the literal", Version, literal)
	}
}

func dir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return wd
}
