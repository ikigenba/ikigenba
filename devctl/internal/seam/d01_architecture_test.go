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
	path       string
	pkgPath    string
	file       *ast.File
	importPath map[string]string
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
	return files
}

// R-BU3V-ZE1N
func TestCommandsUseExplicitRootsAndProcessDirectories(t *testing.T) {
	filesystemCalls := map[string]bool{
		"Create": true, "CreateTemp": true, "DirFS": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
		"Open": true, "OpenFile": true, "OpenRoot": true, "ReadDir": true,
		"ReadFile": true, "Readlink": true, "Remove": true, "RemoveAll": true,
		"Rename": true, "Stat": true, "WriteFile": true,
	}
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
					selector, ok := left.(*ast.SelectorExpr)
					if ok && selector.Sel.Name == "Dir" && source.pkgPath != modulePath+"/internal/seam" {
						t.Errorf("%s: command/path Dir fields must not be filled by later assignment", source.path)
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
				if ident, ok := value.Fun.(*ast.Ident); ok && ident.Name == "new" && len(value.Args) == 1 && isCmdType(value.Args[0], source) {
					t.Errorf("%s: seam.Cmd must not be created through new", source.path)
				}
				if ident, ok := value.Fun.(*ast.Ident); ok {
					for index, parameter := range rootedHelperParameters(source.pkgPath, ident.Name) {
						if index >= len(value.Args) || !filesystemPathIsRooted(value.Args[index], source, enclosingFunction(source.file, value), map[*ast.Ident]bool{}) {
							t.Errorf("%s: call to %s does not supply rooted path parameter %s", source.path, ident.Name, parameter)
						}
					}
				}
				selector, ok := value.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if (selector.Sel.Name == "Exec" || selector.Sel.Name == "Stream") &&
					(len(value.Args) < 2 || !expressionIsCmd(value.Args[1], source, enclosingFunction(source.file, value), map[string]bool{})) {
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

func aliasesPathOrRunnerCapability(expr ast.Expr, source sourceFile) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		return len(rootedHelperParameters(source.pkgPath, value.Name)) != 0
	case *ast.SelectorExpr:
		return value.Sel.Name == "Exec" || value.Sel.Name == "Stream"
	}
	return false
}

func expressionIsCmd(expr ast.Expr, source sourceFile, function *ast.FuncDecl, seen map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.CompositeLit:
		return isCmdType(value.Type, source)
	case *ast.ParenExpr:
		return expressionIsCmd(value.X, source, function, seen)
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
		switch called := value.Fun.(type) {
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
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return false
		}
		path, err := strconv.Unquote(value.Value)
		return err == nil && filepath.IsAbs(path)
	case *ast.ParenExpr:
		return filesystemPathIsRooted(value.X, source, function, seen)
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
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
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
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name == typeName && source.pkgPath == pkgPath
	case *ast.SelectorExpr:
		qualifier, ok := value.X.(*ast.Ident)
		return ok && value.Sel.Name == typeName && source.importPath[qualifier.Name] == pkgPath
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
		switch value := expr.(type) {
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
	if ident, ok := expr.(*ast.Ident); ok {
		if ident.Name == "Cmd" && source.pkgPath == modulePath+"/internal/seam" {
			return true
		}
		for _, declaration := range source.file.Decls {
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
		return false
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Cmd" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && source.importPath[ident.Name] == modulePath+"/internal/seam"
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
