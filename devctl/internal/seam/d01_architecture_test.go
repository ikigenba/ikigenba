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
			case *ast.ValueSpec:
				if value.Type != nil && isCmdType(value.Type, source) && len(value.Values) == 0 {
					t.Errorf("%s: seam.Cmd must be created as a checked keyed literal, not a zero value", source.path)
				}
			case *ast.AssignStmt:
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
	}
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
			return true
		}
		if value.Sel.Name == "Dir" {
			if ident, ok := value.X.(*ast.Ident); ok {
				return ident.Name == "deps" || ident.Name == "app"
			}
			if parent, ok := value.X.(*ast.SelectorExpr); ok {
				return parent.Sel.Name == "Deps"
			}
		}
		if source.pkgPath == modulePath+"/internal/deploy" && value.Sel.Name == "path" {
			return true
		}
		return source.pkgPath == modulePath+"/internal/build" && value.Sel.Name == "binary"
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
	calls, _ := functionEvidence(function)
	joinedToDir := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
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
	return calls["IsAbs"] && joinedToDir
}

func isCmdType(expr ast.Expr, source sourceFile) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "Cmd" && source.pkgPath == modulePath+"/internal/seam"
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
	ownedOperations := map[string]map[string]bool{
		modulePath + "/internal/hostsetup": {"release-discovery": false, "install": false, "upgrade": false, "version": false, "configure": false},
		modulePath + "/internal/spaceinit": {"space-init": false},
		modulePath + "/internal/spaceapps": {"restart": false, "logs": false},
		modulePath + "/internal/remove":    {"remove": false},
		modulePath + "/internal/appref":    {"name": false, "version": false, "tag": false, "file": false},
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
			calls, literals := functionEvidence(function)
			switch source.pkgPath + "." + function.Name.Name {
			case modulePath + "/internal/hostsetup.Latest":
				ownedOperations[source.pkgPath]["release-discovery"] = calls["Exec"] && calls["Unmarshal"] && literals["curl"]
			case modulePath + "/internal/hostsetup.InstallLatest":
				ownedOperations[source.pkgPath]["install"] = calls["Latest"] && calls["Run"] && calls["Sudo"]
			case modulePath + "/internal/hostsetup.Upgrade":
				ownedOperations[source.pkgPath]["upgrade"] = calls["Sudo"] && literals["opsctl"]
			case modulePath + "/internal/hostsetup.Version":
				ownedOperations[source.pkgPath]["version"] = calls["Sudo"] && literals["version"] && literals["opsctl"]
			case modulePath + "/internal/hostsetup.Configure":
				ownedOperations[source.pkgPath]["configure"] = calls["Sudo"] && literals["config"] && literals["set"]
			case modulePath + "/internal/spaceinit.Run":
				ownedOperations[source.pkgPath]["space-init"] = calls["Configure"] && calls["Sudo"] && literals["init"] && literals["opsctl"]
			case modulePath + "/internal/spaceapps.Run":
				ownedOperations[source.pkgPath]["restart"] = calls["Sudo"] && literals["restart"] && literals["opsctl"]
				ownedOperations[source.pkgPath]["logs"] = calls["StreamSudo"] && literals["logs"] && literals["journalctl"]
			case modulePath + "/internal/remove.Run":
				ownedOperations[source.pkgPath]["remove"] = calls["Sudo"] && literals["remove"] && literals["uninstall"]
			case modulePath + "/internal/appref.ValidName":
				ownedOperations[source.pkgPath]["name"] = calls["asciiLowerOrDigit"]
			case modulePath + "/internal/appref.ValidVersion":
				ownedOperations[source.pkgPath]["version"] = calls["validDecimal"] && calls["validIdentifiers"]
			case modulePath + "/internal/appref.VersionForTag":
				ownedOperations[source.pkgPath]["tag"] = calls["ValidName"] && calls["ValidVersion"] && calls["CutPrefix"]
			case modulePath + "/internal/appref.ParseFile":
				ownedOperations[source.pkgPath]["file"] = calls["ValidName"] && calls["ValidVersion"] && literals[".tar.xz"]
			}
		}
	}

	for pkg, operations := range ownedOperations {
		for operation, found := range operations {
			if !found {
				t.Errorf("%s does not implement the %s operation in its executable function body", pkg, operation)
			}
		}
	}
}

func functionEvidence(function *ast.FuncDecl) (map[string]bool, map[string]bool) {
	calls := make(map[string]bool)
	literals := make(map[string]bool)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			switch called := value.Fun.(type) {
			case *ast.Ident:
				calls[called.Name] = true
			case *ast.SelectorExpr:
				calls[called.Sel.Name] = true
			}
		case *ast.BasicLit:
			if value.Kind == token.STRING {
				text, err := strconv.Unquote(value.Value)
				if err == nil {
					literals[text] = true
				}
			}
		}
		return true
	})
	return calls, literals
}
