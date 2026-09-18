package seam_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/ikigenba/ikigenba/devctl"

type sourceFile struct {
	path         string
	pkgPath      string
	file         *ast.File
	importPath   map[string]string
	packageFiles []*ast.File
}

var filesystemCalls = map[string]bool{
	"Create": true, "CreateTemp": true, "DirFS": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
	"Open": true, "OpenFile": true, "OpenRoot": true, "ReadDir": true,
	"ReadFile": true, "Readlink": true, "Remove": true, "RemoveAll": true,
	"Rename": true, "Stat": true, "WriteFile": true,
}

func moduleSources(t *testing.T) []sourceFile {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate D01 architecture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	var files []sourceFile
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			relDir, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			imports := make(map[string]string)
			for _, spec := range parsed.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				name := filepath.Base(imported)
				if spec.Name != nil {
					name = spec.Name.Name
				}
				imports[name] = imported
			}
			files = append(files, sourceFile{
				path:       path,
				pkgPath:    modulePath + "/" + filepath.ToSlash(relDir),
				file:       parsed,
				importPath: imports,
			})
			return nil
		})
		if err != nil {
			t.Fatalf("read %s sources: %v", top, err)
		}
	}
	packages := make(map[string][]*ast.File)
	for _, source := range files {
		packages[source.pkgPath] = append(packages[source.pkgPath], source.file)
	}
	for index := range files {
		files[index].packageFiles = packages[files[index].pkgPath]
	}
	return files
}

// R-BU3V-ZE1N
func TestCommandsUseExplicitRootsAndProcessDirectories(t *testing.T) {
	for _, source := range moduleSources(t) {
		if strings.HasSuffix(source.path, "_test.go") {
			continue
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.FuncDecl:
				if value.Type.Results != nil {
					for _, result := range value.Type.Results.List {
						if len(result.Names) != 0 && isCmdType(result.Type, source) {
							t.Errorf("%s: seam.Cmd must not be returned through a zero-valued named result", source.path)
						}
					}
				}
			case *ast.ValueSpec:
				if value.Type != nil && isCmdType(value.Type, source) && len(value.Values) == 0 {
					t.Errorf("%s: seam.Cmd must be created as a checked keyed literal, not a zero value", source.path)
				}
				for _, initializer := range value.Values {
					if aliasesPathOrRunnerCapability(initializer, source) {
						t.Errorf("%s: path helpers and process runners must not be hidden behind aliases", source.path)
					}
				}
			case *ast.AssignStmt:
				for _, right := range value.Rhs {
					if aliasesPathOrRunnerCapability(right, source) {
						t.Errorf("%s: path helpers and process runners must not be hidden behind aliases", source.path)
					}
				}
				for _, left := range value.Lhs {
					selector, ok := unparenthesized(left).(*ast.SelectorExpr)
					if ok && selector.Sel.Name == "Dir" && source.pkgPath != modulePath+"/internal/seam" {
						t.Errorf("%s: command/path Dir fields must not be filled by later assignment", source.path)
					}
				}
			case *ast.ReturnStmt:
				for _, result := range value.Results {
					if aliasesPathOrRunnerCapability(result, source) {
						t.Errorf("%s: path helpers and process runners must not be returned as aliases", source.path)
					}
				}
			case *ast.CompositeLit:
				if !isCmdType(value.Type, source) {
					return true
				}
				foundDir := false
				for _, element := range value.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						t.Errorf("%s: seam.Cmd must use keyed fields and explicitly name Dir", source.path)
						continue
					}
					name, ok := field.Key.(*ast.Ident)
					if ok && name.Name == "Dir" {
						foundDir = true
						if !filesystemPathIsRooted(field.Value, source, enclosingFunction(source.file, value), map[*ast.Ident]bool{}) {
							t.Errorf("%s: seam.Cmd Dir is not provably rooted", source.path)
						}
					}
				}
				if !foundDir {
					t.Errorf("%s: seam.Cmd does not explicitly supply Dir", source.path)
				}
			case *ast.CallExpr:
				function := enclosingFunction(source.file, value)
				callee := unparenthesized(value.Fun)
				if ident, ok := callee.(*ast.Ident); ok && ident.Name == "new" && len(value.Args) == 1 && isCmdType(value.Args[0], source) {
					t.Errorf("%s: seam.Cmd must not be created through new", source.path)
				}
				if ident, ok := callee.(*ast.Ident); ok {
					for index, parameter := range rootedHelperParameters(source.pkgPath, ident.Name) {
						if index >= len(value.Args) || !filesystemPathIsRooted(value.Args[index], source, function, map[*ast.Ident]bool{}) {
							t.Errorf("%s: call to %s does not supply rooted path parameter %s", source.path, ident.Name, parameter)
						}
					}
				}
				for index, argument := range value.Args {
					if aliasesPathOrRunnerCapability(argument, source) && !capabilityArgumentIsSafelyConsumed(value, index, source) {
						t.Errorf("%s: path helpers and process runners must not be passed through unchecked function parameters", source.path)
					}
				}
				for index, parameter := range calledCmdParameters(callee, source) {
					if index >= len(value.Args) || !expressionIsCmd(value.Args[index], source, function, map[string]bool{}) {
						t.Errorf("%s: call does not supply a statically checked seam.Cmd parameter %s", source.path, parameter)
					}
				}
				if index, ok := runnerCallCmdIndex(callee, function, source); ok &&
					(index >= len(value.Args) || !expressionIsCmd(value.Args[index], source, function, map[string]bool{})) {
					t.Errorf("%s: indirect process runner call does not receive a statically checked seam.Cmd", source.path)
				}
				selector, ok := callee.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if (selector.Sel.Name == "Exec" || selector.Sel.Name == "Stream") &&
					(len(value.Args) < 2 || !expressionIsCmd(value.Args[1], source, function, map[string]bool{})) {
					t.Errorf("%s: process runner call does not receive a statically checked seam.Cmd", source.path)
				}
				ident, ok := selector.X.(*ast.Ident)
				if !ok || source.importPath[ident.Name] != "os" {
					return true
				}
				if selector.Sel.Name == "UserHomeDir" {
					t.Errorf("%s: command source discovers the developer home directory", source.path)
				}
				if !filesystemCalls[selector.Sel.Name] || len(value.Args) == 0 {
					return true
				}
				if !filesystemPathIsRooted(value.Args[0], source, enclosingFunction(source.file, value), map[*ast.Ident]bool{}) {
					t.Errorf("%s: os.%s path is not provably rooted under Deps.Dir, checkout root, or an explicit absolute deploy operand", source.path, selector.Sel.Name)
				}
			}
			return true
		})
		for _, imported := range source.importPath {
			if imported == "os/user" {
				t.Errorf("%s: command source imports os/user and can discover the developer home directory", source.path)
			}
		}
	}
}

func TestArchitectureAnalyzerRecognizesIndirectCapabilities(t *testing.T) {
	t.Run("aliased os access", func(t *testing.T) {
		source := sourceFile{importPath: map[string]string{"os": "os"}}
		for _, name := range []string{"LookupEnv", "ReadFile", "UserHomeDir"} {
			expression := nestedParentheses(&ast.SelectorExpr{X: ast.NewIdent("os"), Sel: ast.NewIdent(name)})
			if !aliasesPathOrRunnerCapability(expression, source) {
				t.Errorf("parenthesized os.%s was not recognized as a path/home capability", name)
			}
		}
	})

	t.Run("cross-file command alias", func(t *testing.T) {
		files := parseArchitectureFixture(t, map[string]string{
			"alias.go": "package sample\nimport \"github.com/ikigenba/ikigenba/devctl/internal/seam\"\ntype hidden = seam.Cmd\n",
			"use.go":   "package sample\nfunc use() { _ = new(hidden) }\n",
		})
		use := files["use.go"]
		if !isCmdType(nestedParentheses(ast.NewIdent("hidden")), use) {
			t.Fatal("parenthesized cross-file seam.Cmd alias was not recognized")
		}
	})

	t.Run("rooted helper passed to unsafe callback", func(t *testing.T) {
		files := parseArchitectureFixture(t, map[string]string{
			"apps.go": `package checkout
func readManifest(string) (int, error) { return 0, nil }
func apps(read func(string) (int, error)) { _, _ = read(".") }
func use() { apps((((readManifest)))) }
`,
		})
		source := files["apps.go"]
		call := findFixtureCall(t, source.file, "apps")
		if !aliasesPathOrRunnerCapability(call.Args[0], source) {
			t.Fatal("parenthesized rooted helper callback was not recognized as a capability")
		}
		if capabilityArgumentIsSafelyConsumed(call, 0, source) {
			t.Fatal("rooted helper passed to a callback invoked with a relative path was accepted")
		}
	})

	t.Run("filesystem capability extracted from indexed composite", func(t *testing.T) {
		files := parseArchitectureFixture(t, map[string]string{
			"leak.go": `package sample
import "os"
var leakedRead = [...]func(string) ([]byte, error){nil, os.ReadFile}[1]
type readFunc func(string) ([]byte, error)
var leakedSlice = [...]readFunc{nil, os.ReadFile}[1:]
var convertedRead = readFunc(os.ReadFile)
var wrappedRead = func() readFunc { return os.ReadFile }()
func use() {
	_, _ = leakedRead(".")
	_, _ = convertedRead(".")
	_ = leakedSlice
	_, _ = wrappedRead(".")
}
`,
		})
		source := files["leak.go"]
		initializers := make(map[string]ast.Expr)
		ast.Inspect(source.file, func(node ast.Node) bool {
			specification, ok := node.(*ast.ValueSpec)
			if ok && len(specification.Names) == 1 && len(specification.Values) == 1 {
				initializers[specification.Names[0].Name] = specification.Values[0]
			}
			return true
		})
		for _, name := range []string{"leakedRead", "leakedSlice", "convertedRead", "wrappedRead"} {
			initializer := initializers[name]
			if initializer == nil {
				t.Fatalf("%s initializer not found", name)
			}
			if !aliasesPathOrRunnerCapability(initializer, source) {
				t.Errorf("%s did not preserve the embedded os.ReadFile capability", name)
			}
		}
		findFixtureCall(t, source.file, "leakedRead")
	})

	t.Run("parenthesized cross-file command alias through new", func(t *testing.T) {
		files := parseArchitectureFixture(t, map[string]string{
			"alias.go": "package sample\nimport \"github.com/ikigenba/ikigenba/devctl/internal/seam\"\ntype hiddenCommand = seam.Cmd\n",
			"use.go":   "package sample\nfunc use() { _ = new((((hiddenCommand)))) }\n",
		})
		use := files["use.go"]
		call := findFixtureCall(t, use.file, "new")
		if len(call.Args) != 1 || !isCmdType(call.Args[0], use) {
			t.Fatal("parenthesized cross-file seam.Cmd alias passed to new was not recognized")
		}
	})

	t.Run("indirect runner", func(t *testing.T) {
		files := parseArchitectureFixture(t, map[string]string{
			"runner.go": `package sample
import (
	"context"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func invoke(run seam.Runner, ctx context.Context, command seam.Cmd) { _, _ = run(ctx, command) }
`,
		})
		source := files["runner.go"]
		call := findFixtureCall(t, source.file, "run")
		function := enclosingFunction(source.file, call)
		index, ok := runnerCallCmdIndex(call.Fun, function, source)
		if !ok || index != 1 {
			t.Fatalf("indirect runner command index = %d, %t; want 1, true", index, ok)
		}
	})
}

func parseArchitectureFixture(t *testing.T, contents map[string]string) map[string]sourceFile {
	t.Helper()
	parsed := make(map[string]*ast.File)
	for name, content := range contents {
		file, err := parser.ParseFile(token.NewFileSet(), name, content, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed[name] = file
	}
	packageFiles := make([]*ast.File, 0, len(parsed))
	for _, file := range parsed {
		packageFiles = append(packageFiles, file)
	}
	files := make(map[string]sourceFile)
	for name, file := range parsed {
		imports := make(map[string]string)
		for _, specification := range file.Imports {
			path, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				t.Fatalf("unquote %s import: %v", name, err)
			}
			imports[filepath.Base(path)] = path
		}
		files[name] = sourceFile{
			path:         name,
			pkgPath:      modulePath + "/internal/" + file.Name.Name,
			file:         file,
			importPath:   imports,
			packageFiles: packageFiles,
		}
	}
	return files
}

func findFixtureCall(t *testing.T, file *ast.File, name string) *ast.CallExpr {
	t.Helper()
	var found *ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := unparenthesized(call.Fun).(*ast.Ident)
		if ok && ident.Name == name {
			found = call
		}
		return true
	})
	if found == nil {
		t.Fatalf("fixture call %s not found", name)
	}
	return found
}

func nestedParentheses(expr ast.Expr) ast.Expr {
	return &ast.ParenExpr{X: &ast.ParenExpr{X: &ast.ParenExpr{X: expr}}}
}

func unparenthesized(expr ast.Expr) ast.Expr {
	for {
		parenthesized, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = parenthesized.X
	}
}

func aliasesPathOrRunnerCapability(expr ast.Expr, source sourceFile) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.Ident:
		return len(rootedHelperParameters(source.pkgPath, value.Name)) != 0 ||
			source.pkgPath != modulePath+"/internal/seam" && functionIsRunner(value.Name, source)
	case *ast.SelectorExpr:
		if value.Sel.Name == "Exec" || value.Sel.Name == "Stream" {
			return true
		}
		qualifier, ok := value.X.(*ast.Ident)
		if ok && source.importPath[qualifier.Name] == "os" &&
			(filesystemCalls[value.Sel.Name] || value.Sel.Name == "Getenv" || value.Sel.Name == "LookupEnv" || value.Sel.Name == "UserHomeDir") {
			return true
		}
		return aliasesPathOrRunnerCapability(value.X, source)
	case *ast.IndexExpr:
		return aliasesPathOrRunnerCapability(value.X, source) || aliasesPathOrRunnerCapability(value.Index, source)
	case *ast.IndexListExpr:
		if aliasesPathOrRunnerCapability(value.X, source) {
			return true
		}
		for _, index := range value.Indices {
			if aliasesPathOrRunnerCapability(index, source) {
				return true
			}
		}
	case *ast.SliceExpr:
		return aliasesPathOrRunnerCapability(value.X, source) ||
			expressionListAliasesCapability([]ast.Expr{value.Low, value.High, value.Max}, source)
	case *ast.CompositeLit:
		// cmd/devctl is required to assemble the real dependency capabilities in
		// one explicit seam.Deps literal. That wiring is not an alias escape.
		if isNamedType(value.Type, modulePath+"/internal/seam", "Deps", source) {
			return false
		}
		for _, element := range value.Elts {
			switch element := element.(type) {
			case *ast.KeyValueExpr:
				if aliasesPathOrRunnerCapability(element.Key, source) || aliasesPathOrRunnerCapability(element.Value, source) {
					return true
				}
			case ast.Expr:
				if aliasesPathOrRunnerCapability(element, source) {
					return true
				}
			}
		}
	case *ast.KeyValueExpr:
		return aliasesPathOrRunnerCapability(value.Key, source) || aliasesPathOrRunnerCapability(value.Value, source)
	case *ast.CallExpr:
		// A direct capability call consumes the function value; its path is checked
		// separately. Function-type conversions preserve their argument as a
		// callable value; ordinary call arguments are checked at the call site.
		if expressionIsFunctionType(value.Fun, source) &&
			expressionListAliasesCapability(value.Args, source) {
			return true
		}
		if !directPathOrRunnerCapability(value.Fun, source) {
			return aliasesPathOrRunnerCapability(value.Fun, source)
		}
	case *ast.FuncLit:
		found := false
		ast.Inspect(value.Body, func(node ast.Node) bool {
			returned, ok := node.(*ast.ReturnStmt)
			if ok && expressionListAliasesCapability(returned.Results, source) {
				found = true
			}
			return !found
		})
		return found
	case *ast.TypeAssertExpr:
		return aliasesPathOrRunnerCapability(value.X, source)
	case *ast.StarExpr:
		return aliasesPathOrRunnerCapability(value.X, source)
	case *ast.UnaryExpr:
		return aliasesPathOrRunnerCapability(value.X, source)
	case *ast.BinaryExpr:
		return aliasesPathOrRunnerCapability(value.X, source) || aliasesPathOrRunnerCapability(value.Y, source)
	}
	return false
}

func expressionListAliasesCapability(expressions []ast.Expr, source sourceFile) bool {
	for _, expression := range expressions {
		if expression != nil && aliasesPathOrRunnerCapability(expression, source) {
			return true
		}
	}
	return false
}

func expressionIsFunctionType(expr ast.Expr, source sourceFile) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.FuncType:
		return true
	case *ast.Ident:
		for _, file := range source.packageFiles {
			for _, declaration := range file.Decls {
				generic, ok := declaration.(*ast.GenDecl)
				if !ok || generic.Tok != token.TYPE {
					continue
				}
				for _, specification := range generic.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if ok && typeSpec.Name.Name == value.Name && typeSpec.Type != value {
						return expressionIsFunctionType(typeSpec.Type, source)
					}
				}
			}
		}
	case *ast.SelectorExpr:
		return isRunnerType(value, source)
	}
	return false
}

func directPathOrRunnerCapability(expr ast.Expr, source sourceFile) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.Ident:
		return len(rootedHelperParameters(source.pkgPath, value.Name)) != 0 ||
			source.pkgPath != modulePath+"/internal/seam" && functionIsRunner(value.Name, source)
	case *ast.SelectorExpr:
		if value.Sel.Name == "Exec" || value.Sel.Name == "Stream" {
			return true
		}
		qualifier, ok := value.X.(*ast.Ident)
		return ok && source.importPath[qualifier.Name] == "os" &&
			(filesystemCalls[value.Sel.Name] || value.Sel.Name == "Getenv" || value.Sel.Name == "LookupEnv" || value.Sel.Name == "UserHomeDir")
	}
	return false
}

func packageFunctions(source sourceFile, name string) []*ast.FuncDecl {
	var functions []*ast.FuncDecl
	for _, file := range source.packageFiles {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == name {
				functions = append(functions, function)
			}
		}
	}
	return functions
}

func calledFunction(expr ast.Expr, source sourceFile) []*ast.FuncDecl {
	name := ""
	switch value := unparenthesized(expr).(type) {
	case *ast.Ident:
		name = value.Name
	case *ast.SelectorExpr:
		if qualifier, ok := value.X.(*ast.Ident); ok && source.importPath[qualifier.Name] != "" {
			return nil
		}
		name = value.Sel.Name
	}
	return packageFunctions(source, name)
}

type namedParameter struct {
	name     string
	typeExpr ast.Expr
}

func fieldParameters(fields *ast.FieldList) []namedParameter {
	var parameters []namedParameter
	if fields == nil {
		return parameters
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			parameters = append(parameters, namedParameter{name: name.Name, typeExpr: field.Type})
		}
	}
	return parameters
}

func calledCmdParameters(expr ast.Expr, source sourceFile) map[int]string {
	parameters := make(map[int]string)
	for _, function := range calledFunction(expr, source) {
		for index, parameter := range fieldParameters(function.Type.Params) {
			if isCmdType(parameter.typeExpr, source) {
				parameters[index] = parameter.name
			}
		}
	}
	return parameters
}

func functionIsRunner(name string, source sourceFile) bool {
	for _, function := range packageFunctions(source, name) {
		parameters := fieldParameters(function.Type.Params)
		if len(parameters) >= 2 && isCmdType(parameters[1].typeExpr, source) {
			return true
		}
	}
	return false
}

func runnerCallCmdIndex(expr ast.Expr, function *ast.FuncDecl, source sourceFile) (int, bool) {
	ident, ok := unparenthesized(expr).(*ast.Ident)
	if !ok || function == nil {
		return 0, false
	}
	for _, parameter := range fieldParameters(function.Type.Params) {
		if parameter.name == ident.Name && isRunnerType(parameter.typeExpr, source) {
			return 1, true
		}
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		specification, ok := node.(*ast.ValueSpec)
		if !ok || specification.Type == nil || !isRunnerType(specification.Type, source) {
			return true
		}
		for _, name := range specification.Names {
			found = found || name.Name == ident.Name
		}
		return true
	})
	return 1, found
}

func capabilityArgumentIsSafelyConsumed(call *ast.CallExpr, argumentIndex int, source sourceFile) bool {
	for _, function := range calledFunction(call.Fun, source) {
		parameters := fieldParameters(function.Type.Params)
		if argumentIndex >= len(parameters) || function.Body == nil {
			continue
		}
		name := parameters[argumentIndex].name
		called := false
		safe := true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			invocation, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := unparenthesized(invocation.Fun).(*ast.Ident)
			if !ok || ident.Name != name {
				return true
			}
			called = true
			safe = safe && len(invocation.Args) != 0 && filesystemPathIsRooted(invocation.Args[0], source, function, map[*ast.Ident]bool{})
			return true
		})
		if called && safe {
			return true
		}
	}
	return false
}

func expressionIsCmd(expr ast.Expr, source sourceFile, function *ast.FuncDecl, seen map[string]bool) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.CompositeLit:
		return isCmdType(value.Type, source)
	case *ast.Ident:
		if function == nil || seen[value.Name] {
			return false
		}
		seen[value.Name] = true
		defer delete(seen, value.Name)
		if function.Type.Params != nil {
			for _, field := range function.Type.Params.List {
				for _, parameter := range field.Names {
					if parameter.Name == value.Name {
						return isCmdType(field.Type, source)
					}
				}
			}
		}
		checked := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for index, left := range statement.Lhs {
					ident, ok := left.(*ast.Ident)
					if ok && ident.Name == value.Name && index < len(statement.Rhs) {
						checked = checked || expressionIsCmd(statement.Rhs[index], source, function, seen)
					}
				}
			case *ast.ValueSpec:
				for index, name := range statement.Names {
					if name.Name == value.Name {
						checked = checked || statement.Type != nil && isCmdType(statement.Type, source) ||
							index < len(statement.Values) && expressionIsCmd(statement.Values[index], source, function, seen)
					}
				}
			}
			return true
		})
		return checked
	case *ast.CallExpr:
		name := ""
		switch called := unparenthesized(value.Fun).(type) {
		case *ast.Ident:
			name = called.Name
		case *ast.SelectorExpr:
			name = called.Sel.Name
		}
		for _, declaration := range source.file.Decls {
			candidate, ok := declaration.(*ast.FuncDecl)
			if !ok || candidate.Name.Name != name || candidate.Type.Results == nil || len(candidate.Type.Results.List) == 0 {
				continue
			}
			return isCmdType(candidate.Type.Results.List[0].Type, source)
		}
	}
	return false
}

func enclosingFunction(file *ast.File, target ast.Node) *ast.FuncDecl {
	var enclosing *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Pos() <= target.Pos() && target.End() <= function.End() {
			enclosing = function
		}
	}
	return enclosing
}

func filesystemPathIsRooted(expr ast.Expr, source sourceFile, function *ast.FuncDecl, seen map[*ast.Ident]bool) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return false
		}
		path, err := strconv.Unquote(value.Value)
		return err == nil && filepath.IsAbs(path)
	case *ast.SelectorExpr:
		if value.Sel.Name == "Root" {
			ident, ok := value.X.(*ast.Ident)
			return ok && ident.Name == "checkout" && receiverHasNamedType(function, "checkout", modulePath+"/internal/checkout", "Checkout", source)
		}
		if value.Sel.Name == "Dir" {
			if ident, ok := value.X.(*ast.Ident); ok {
				return ident.Name == "deps" && functionHasParameterType(function, "deps", modulePath+"/internal/seam", "Deps", source) ||
					ident.Name == "app" && appVariableComesFromCheckout(function, ident, source)
			}
			if parent, ok := value.X.(*ast.SelectorExpr); ok {
				base, baseOK := parent.X.(*ast.Ident)
				return parent.Sel.Name == "Deps" && baseOK &&
					(base.Name == "checkout" && receiverHasNamedType(function, "checkout", modulePath+"/internal/checkout", "Checkout", source) ||
						base.Name == "h" && receiverHasNamedType(function, "h", modulePath+"/internal/host", "Host", source))
			}
		}
		if source.pkgPath == modulePath+"/internal/deploy" && value.Sel.Name == "path" {
			ident, ok := value.X.(*ast.Ident)
			return ok && ident.Name == "invocation"
		}
		if source.pkgPath == modulePath+"/internal/build" && value.Sel.Name == "binary" {
			ident, ok := value.X.(*ast.Ident)
			return ok && ident.Name == "staged"
		}
		return false
	case *ast.CallExpr:
		if selector, ok := unparenthesized(value.Fun).(*ast.SelectorExpr); ok {
			if ident, ok := selector.X.(*ast.Ident); ok && source.importPath[ident.Name] == "os" {
				switch selector.Sel.Name {
				case "CreateTemp", "MkdirTemp":
					return len(value.Args) != 0 && filesystemPathIsRooted(value.Args[0], source, function, seen)
				}
			}
			if ident, ok := selector.X.(*ast.Ident); ok && source.importPath[ident.Name] == "path/filepath" {
				switch selector.Sel.Name {
				case "Join", "Dir":
					return len(value.Args) != 0 && filesystemPathIsRooted(value.Args[0], source, function, seen)
				}
			}
			if selector.Sel.Name == "Path" {
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "checkout" {
					return true
				}
				return filesystemPathIsRooted(selector.X, source, function, seen)
			}
			if selector.Sel.Name == "Name" {
				return filesystemPathIsRooted(selector.X, source, function, seen)
			}
		}
	case *ast.Ident:
		if seen[value] || function == nil {
			return false
		}
		if source.pkgPath == modulePath+"/internal/deploy" && function.Name.Name == "validateFile" && value.Name == "path" {
			return deployPathResolutionIsExplicit(function)
		}
		seen[value] = true
		defer delete(seen, value)
		// These parameters are path capabilities: their callers are checked at the
		// point where the capability is introduced (Deps.Dir or checkout.App.Dir).
		if parameterIsRooted(source.pkgPath, function.Name.Name, value.Name) ||
			source.pkgPath == modulePath+"/internal/build" && function.Name.Name == "copyArchiveDirectory" && value.Name == "source" {
			return true
		}
		found := false
		rooted := true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for index, left := range statement.Lhs {
					ident, ok := left.(*ast.Ident)
					if !ok || ident.Name != value.Name || index >= len(statement.Rhs) {
						continue
					}
					found = true
					rooted = rooted && filesystemPathIsRooted(statement.Rhs[index], source, function, seen)
				}
			case *ast.ValueSpec:
				for index, name := range statement.Names {
					if name.Name != value.Name || index >= len(statement.Values) {
						continue
					}
					found = true
					rooted = rooted && filesystemPathIsRooted(statement.Values[index], source, function, seen)
				}
			}
			return true
		})
		return found && rooted
	}
	return false
}

func functionHasParameterType(function *ast.FuncDecl, name, pkgPath, typeName string, source sourceFile) bool {
	if function == nil || function.Type.Params == nil {
		return false
	}
	for _, field := range function.Type.Params.List {
		for _, parameter := range field.Names {
			if parameter.Name == name && isNamedType(field.Type, pkgPath, typeName, source) {
				return true
			}
		}
	}
	return false
}

func receiverHasNamedType(function *ast.FuncDecl, name, pkgPath, typeName string, source sourceFile) bool {
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	field := function.Recv.List[0]
	return len(field.Names) == 1 && field.Names[0].Name == name && isNamedType(field.Type, pkgPath, typeName, source)
}

func isNamedType(expr ast.Expr, pkgPath, typeName string, source sourceFile) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.Ident:
		return value.Name == typeName && source.pkgPath == pkgPath
	case *ast.SelectorExpr:
		qualifier, ok := value.X.(*ast.Ident)
		return ok && value.Sel.Name == typeName && importedPath(source, qualifier.Name) == pkgPath
	case *ast.StarExpr:
		return isNamedType(value.X, pkgPath, typeName, source)
	}
	return false
}

func appVariableComesFromCheckout(function *ast.FuncDecl, app *ast.Ident, source sourceFile) bool {
	if function != nil && function.Type.Params != nil {
		for _, field := range function.Type.Params.List {
			for _, parameter := range field.Names {
				if parameter.Name == app.Name && isNamedType(field.Type, modulePath+"/internal/checkout", "App", source) {
					return true
				}
			}
		}
	}
	if function == nil {
		return false
	}
	// Locals named app are accepted only when assigned from the prepared/staged
	// checkout-app fields whose construction is checked at their declaration.
	if functionHasLocalCheckoutApp(function, app.Name) {
		return true
	}
	return false
}

func functionHasLocalCheckoutApp(function *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, left := range assignment.Lhs {
			ident, ok := left.(*ast.Ident)
			if !ok || ident.Name != name || index >= len(assignment.Rhs) {
				continue
			}
			selector, ok := assignment.Rhs[index].(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "app" {
				found = found || selectorRootName(selector.X) == "prepared" || selectorRootName(selector.X) == "staged"
			}
		}
		return true
	})
	return found
}

func selectorRootName(expr ast.Expr) string {
	for {
		switch value := unparenthesized(expr).(type) {
		case *ast.Ident:
			return value.Name
		case *ast.SelectorExpr:
			expr = value.X
		default:
			return ""
		}
	}
}

func rootedHelperParameters(pkgPath, function string) map[int]string {
	contracts := map[string]map[int]string{
		modulePath + "/internal/build.copyArchiveDirectory": {0: "appDir", 1: "archiveRoot"},
		modulePath + "/internal/build.copyArchiveFile":      {0: "source", 1: "destination"},
		modulePath + "/internal/build.writeArchiveFile":     {0: "path"},
		modulePath + "/internal/checkout.hasMainPackage":    {0: "dir"},
		modulePath + "/internal/checkout.readManifest":      {0: "dir"},
	}
	return contracts[pkgPath+"."+function]
}

func parameterIsRooted(pkgPath, function, parameter string) bool {
	for _, name := range rootedHelperParameters(pkgPath, function) {
		if name == parameter {
			return true
		}
	}
	return pkgPath == modulePath+"/internal/build" && function == "executeStaged" && parameter == "dir"
}

func deployPathResolutionIsExplicit(function *ast.FuncDecl) bool {
	checksAbsolute := false
	joinedToDir := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			selector, selectorOK := call.Fun.(*ast.SelectorExpr)
			if selectorOK && selector.Sel.Name == "IsAbs" {
				checksAbsolute = true
			}
		}
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, left := range assignment.Lhs {
			ident, ok := left.(*ast.Ident)
			if !ok || ident.Name != "path" || index >= len(assignment.Rhs) {
				continue
			}
			call, ok := assignment.Rhs[index].(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				continue
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			root, rootOK := call.Args[0].(*ast.Ident)
			if ok && selector.Sel.Name == "Join" && rootOK && root.Name == "dir" {
				joinedToDir = true
			}
		}
		return true
	})
	return checksAbsolute && joinedToDir
}

func isCmdType(expr ast.Expr, source sourceFile) bool {
	expr = unparenthesized(expr)
	if ident, ok := expr.(*ast.Ident); ok {
		if ident.Name == "Cmd" && source.pkgPath == modulePath+"/internal/seam" {
			return true
		}
		for _, file := range source.packageFiles {
			for _, declaration := range file.Decls {
				generic, ok := declaration.(*ast.GenDecl)
				if !ok || generic.Tok != token.TYPE {
					continue
				}
				for _, specification := range generic.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if ok && typeSpec.Name.Name == ident.Name && typeSpec.Type != ident && isCmdType(typeSpec.Type, source) {
						return true
					}
				}
			}
		}
		return false
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Cmd" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && importedPath(source, ident.Name) == modulePath+"/internal/seam"
}

func isRunnerType(expr ast.Expr, source sourceFile) bool {
	switch value := unparenthesized(expr).(type) {
	case *ast.Ident:
		if (value.Name == "Runner" || value.Name == "StreamRunner") && source.pkgPath == modulePath+"/internal/seam" {
			return true
		}
		for _, file := range source.packageFiles {
			for _, declaration := range file.Decls {
				generic, ok := declaration.(*ast.GenDecl)
				if !ok || generic.Tok != token.TYPE {
					continue
				}
				for _, specification := range generic.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if ok && typeSpec.Name.Name == value.Name && typeSpec.Type != value && isRunnerType(typeSpec.Type, source) {
						return true
					}
				}
			}
		}
	case *ast.SelectorExpr:
		qualifier, ok := value.X.(*ast.Ident)
		return ok && (value.Sel.Name == "Runner" || value.Sel.Name == "StreamRunner") &&
			importedPath(source, qualifier.Name) == modulePath+"/internal/seam"
	}
	return false
}

func importedPath(source sourceFile, qualifier string) string {
	if path := source.importPath[qualifier]; path != "" {
		return path
	}
	for _, file := range source.packageFiles {
		for _, specification := range file.Imports {
			path, err := strconv.Unquote(specification.Path.Value)
			if err != nil {
				continue
			}
			name := filepath.Base(path)
			if specification.Name != nil {
				name = specification.Name.Name
			}
			if name == qualifier {
				return path
			}
		}
	}
	return ""
}

// R-YHTO-8IAI
func TestCommandAndSharedHelperOwnership(t *testing.T) {
	commandPackages := map[string]bool{
		modulePath + "/internal/build":       true,
		modulePath + "/internal/deploy":      true,
		modulePath + "/internal/remove":      true,
		modulePath + "/internal/restore":     true,
		modulePath + "/internal/secrets":     true,
		modulePath + "/internal/spaceapps":   true,
		modulePath + "/internal/spacecreate": true,
		modulePath + "/internal/spaceinit":   true,
	}
	helperPackages := map[string]bool{
		modulePath + "/internal/appref":    true,
		modulePath + "/internal/hostsetup": true,
	}
	ownedFunctions := map[string]map[string]bool{
		modulePath + "/internal/hostsetup": {"Latest": false, "InstallLatest": false, "Upgrade": false, "Version": false, "Configure": false},
		modulePath + "/internal/spaceinit": {"Run": false},
		modulePath + "/internal/spaceapps": {"Run": false},
		modulePath + "/internal/remove":    {"Run": false},
		modulePath + "/internal/appref":    {"ValidName": false, "ValidVersion": false, "VersionForTag": false, "ParseFile": false},
	}

	for _, source := range moduleSources(t) {
		if strings.HasSuffix(source.path, "_test.go") {
			continue
		}
		if helperPackages[source.pkgPath] {
			for _, imported := range source.importPath {
				if imported == modulePath+"/internal/cli" || commandPackages[imported] {
					t.Errorf("%s: shared helper imports command package %q", source.path, imported)
				}
			}
		}
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Body == nil {
				continue
			}
			if functions := ownedFunctions[source.pkgPath]; functions != nil {
				if _, required := functions[function.Name.Name]; required {
					functions[function.Name.Name] = true
				}
			}
		}
	}

	for pkg, functions := range ownedFunctions {
		for function, found := range functions {
			if !found {
				t.Errorf("%s does not declare required operation function %s", pkg, function)
			}
		}
	}
}
