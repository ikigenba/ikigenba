package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// R-RKR3-TB47
func TestReposServiceNameIsConfinedToVersionTriggerBoundaryAndCompositionRoot(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "project" || strings.HasPrefix(rel, "project/") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") ||
			strings.HasPrefix(rel, "internal/version/") || strings.HasPrefix(rel, "cmd/prompts/") ||
			rel == "internal/prompt/trigger.go" {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && value == "repos" {
				t.Errorf("%s contains forbidden service-name literal %s", rel, literal.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan non-test Go sources: %v", err)
	}
}
