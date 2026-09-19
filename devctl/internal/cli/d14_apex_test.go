package cli

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestApexDispatchAndHelpAreExactAndDependencyFree(t *testing.T) {
	// R-48BK-GLAQ R-49JG-UD1F
	assertApexDispatchForwardsArgumentsBeforeHelpRewrite(t)

	for _, args := range [][]string{
		{"apex", "--help"},
		{"apex", "-h"},
		{"apex", "set", "--help"},
		{"apex", "show", "-h"},
		{"apex", "clear", "ignored", "--help"},
	} {
		t.Run(args[1], func(t *testing.T) {
			calls := 0
			deps := noExternalDeps(&calls)
			deps.Dir = t.TempDir()
			assertResult(t, invokeWithDeps(deps, args...), 0, expectedApexUsage, "")
			if calls != 0 {
				t.Fatalf("Run(%q) made %d external calls, want none", args, calls)
			}
		})
	}

	calls := 0
	deps := noExternalDeps(&calls)
	deps.EUID = 0
	assertResult(t, invokeWithDeps(deps, "apex", "--help"), 3, "", "devctl: must not run as root\n")
	if calls != 0 {
		t.Fatalf("superuser refusal made %d external calls, want none", calls)
	}
}

func assertApexDispatchForwardsArgumentsBeforeHelpRewrite(t *testing.T) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var runBody *ast.BlockStmt
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "Run" {
			runBody = function.Body
			break
		}
	}
	if runBody == nil {
		t.Fatal("cli.Run declaration not found")
	}

	var apexCall, helpRewrite *ast.CallExpr
	ast.Inspect(runBody, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch function := call.Fun.(type) {
		case *ast.Ident:
			if function.Name == "helpArguments" {
				helpRewrite = call
			}
		case *ast.SelectorExpr:
			packageName, ok := function.X.(*ast.Ident)
			if ok && packageName.Name == "apex" && function.Sel.Name == "Run" {
				apexCall = call
			}
		}
		return true
	})

	if apexCall == nil {
		t.Fatal("cli.Run does not call apex.Run")
	}
	if helpRewrite == nil {
		t.Fatal("cli.Run has no help argument normalization")
	}
	if apexCall.Pos() > helpRewrite.Pos() {
		t.Fatal("cli.Run normalizes help arguments before dispatching apex")
	}
	if len(apexCall.Args) != 4 || !identifierIs(apexCall.Args[0], "ctx") ||
		!selectorIs(apexCall.Args[1], "invocation", "arguments") ||
		!identifierIs(apexCall.Args[2], "stdout") || !identifierIs(apexCall.Args[3], "deps") {
		t.Fatal("cli.Run does not forward apex arguments, stdout, and deps directly")
	}
}

func selectorIs(expression ast.Expr, receiver, field string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != field {
		return false
	}
	return identifierIs(selector.X, receiver)
}

func identifierIs(expression ast.Expr, name string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == name
}

func TestApexArgumentFailuresAreExactAndDependencyFree(t *testing.T) {
	// R-4BZ9-LWIT
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing subcommand", args: []string{"apex"}, want: "apex needs <subcommand>"},
		{name: "unknown subcommand", args: []string{"apex", "frobnicate"}, want: "unknown subcommand 'frobnicate'"},
		{name: "set missing operand", args: []string{"apex", "set"}, want: "apex set needs <app>.<space>"},
		{name: "set extra operand", args: []string{"apex", "set", "crm.sbx1", "extra"}, want: "apex set takes only <app>.<space>"},
		{name: "show operand", args: []string{"apex", "show", "sbx1"}, want: "apex show takes no arguments"},
		{name: "clear operand", args: []string{"apex", "clear", "sbx1"}, want: "apex clear takes no arguments"},
		{name: "unknown option", args: []string{"apex", "show", "--verbose"}, want: "unknown option '--verbose'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			deps := noExternalDeps(&calls)
			deps.Dir = t.TempDir()
			wantStderr := "devctl: " + test.want + "\n\nsee 'devctl apex --help' for usage\n"
			assertResult(t, invokeWithDeps(deps, test.args...), 2, "", wantStderr)
			if calls != 0 {
				t.Fatalf("Run(%q) made %d external calls, want none", test.args, calls)
			}
		})
	}
}

func TestApexOperandParsingPrecedesCloud(t *testing.T) {
	// R-QW0J-KUFF
	for _, operand := range []string{"sbx1", "crm.sbx1.example.com"} {
		deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
		cloudCalls := 0
		deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		}
		result := invokeWithDeps(deps, "apex", "set", operand)
		if result.code != 2 || result.stdout != "" || cloudCalls != 0 {
			t.Fatalf("apex set %q = %#v, cloud calls %d", operand, result, cloudCalls)
		}
	}
}
