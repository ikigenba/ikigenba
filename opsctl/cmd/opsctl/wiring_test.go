package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"testing"
)

func TestProcessWiring(t *testing.T) {
	// R-5TYS-68EN
	expected := `package main
 func main() {
 os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.Deps{
 Root: "/", EUID: os.Geteuid(), Getenv: os.Getenv,
 DNS: dns.Env{Open: route53.Open}, LookPath: exec.LookPath,
 LookupHost: net.DefaultResolver.LookupHost, Execute: host.Exec, Now: time.Now,
 Cloud: cloud.Env{Open: awscloud.Open},
 }))
 }`
	actual, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want, err := parser.ParseFile(token.NewFileSet(), "expected.go", expected, 0)
	if err != nil {
		t.Fatal(err)
	}
	var gotMain *ast.FuncDecl
	for _, decl := range actual.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Name.Name != "main" || gotMain != nil {
				t.Fatal("process package contains additional functions")
			}
			gotMain = fn
		} else if _, ok := decl.(*ast.GenDecl); !ok || decl.(*ast.GenDecl).Tok != token.IMPORT {
			t.Fatal("process package contains non-import declarations")
		}
	}
	if gotMain == nil {
		t.Fatal("main missing")
	}
	render := func(node ast.Node) string {
		var buf bytes.Buffer
		// Reparse without source positions so layout reflects syntax alone.
		if err := format.Node(&buf, token.NewFileSet(), node); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	if got, w := render(gotMain), render(want.Decls[0]); got != w {
		t.Fatalf("process wiring =\n%s\nwant\n%s", got, w)
	}
}
