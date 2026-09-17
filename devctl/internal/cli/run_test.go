package cli

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

var _ func(context.Context, []string, io.Reader, io.Writer, io.Writer, seam.Deps) int = Run

func TestRunReturnsWithoutTerminatingCaller(t *testing.T) {
	// R-U72C-BSYD
	file, err := parser.ParseFile(token.NewFileSet(), "run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, isIdentifier := selector.X.(*ast.Ident)
		if isIdentifier && identifier.Name == "os" && selector.Sel.Name == "Exit" {
			t.Error("Run calls os.Exit instead of returning")
		}
		return true
	})

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr, seam.Deps{EUID: 1})
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
