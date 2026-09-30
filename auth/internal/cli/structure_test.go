package cli

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const authModule = "github.com/ikigenba/ikigenba/auth"
const kitModule = "github.com/ikigenba/ikigenba/appkit"

type treeSource struct {
	dir  string
	file *ast.File
	test bool
}

func moduleSources(t *testing.T, tests bool) []treeSource {
	t.Helper()
	root := filepath.Join("..", "..")
	var sources []treeSource
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || (!tests && strings.HasSuffix(path, "_test.go")) {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		sources = append(sources, treeSource{dir: filepath.ToSlash(rel), file: file, test: strings.HasSuffix(path, "_test.go")})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

func sourceImports(t *testing.T, file *ast.File) map[string]string {
	t.Helper()
	imports := make(map[string]string)
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = path
	}
	return imports
}

func TestModulePackageSet(t *testing.T) {
	// R-SK1K-3IGJ
	want := map[string]string{"cmd/auth": "main", "internal/cli": "cli", "internal/server": "server", "internal/store": "store", "internal/google": "google", "internal/idcodec": "idcodec", "internal/version": "version"}
	found := make(map[string]bool)
	for _, source := range moduleSources(t, true) {
		name, ok := want[source.dir]
		if !ok {
			t.Errorf("Go file in undesigned directory %s", source.dir)
			continue
		}
		// An external test package is a test of the named production package.
		if source.file.Name.Name != name && (!source.test || source.file.Name.Name != name+"_test") {
			t.Errorf("%s: package %s, want %s", source.dir, source.file.Name.Name, name)
		}
		found[source.dir] = true
	}
	for dir := range want {
		if !found[dir] {
			t.Errorf("missing package %s", dir)
		}
	}
}

func TestModuleImportDirection(t *testing.T) {
	// R-SL9G-HA78
	allowed := map[string][]string{
		"cmd/auth":        {"internal/cli"},
		"internal/cli":    {"internal/server", "internal/store", "internal/google", "internal/idcodec", "internal/version"},
		"internal/server": {"internal/store", "internal/google", "internal/idcodec", "internal/version"},
		"internal/store":  {"internal/idcodec", "internal/version"},
		"internal/google": {"internal/idcodec", "internal/version"},
	}
	mainImportsCLI := false
	for _, source := range moduleSources(t, false) {
		for _, path := range sourceImports(t, source.file) {
			if path != authModule && !strings.HasPrefix(path, authModule+"/") {
				continue
			}
			target := strings.TrimPrefix(path, authModule+"/")
			if !slices.Contains(allowed[source.dir], target) {
				t.Errorf("forbidden module import: %s -> %s", source.dir, path)
			}
			if source.dir == "cmd/auth" && target == "internal/cli" {
				mainImportsCLI = true
			}
		}
	}
	if !mainImportsCLI {
		t.Error("cmd/auth does not import internal/cli")
	}
}

func TestAppkitImportBoundary(t *testing.T) {
	// R-SMHC-V1XX R-SSKU-RWNE
	want := map[string]bool{"cmd/auth": true, "internal/cli": true, "internal/server": true}
	found := make(map[string]bool)
	sources := moduleSources(t, false)
	for _, source := range sources {
		imports := sourceImports(t, source.file)
		for _, path := range imports {
			if path == kitModule {
				found[source.dir] = true
				if !want[source.dir] {
					t.Errorf("appkit imported by %s", source.dir)
				}
			}
		}
		if !strings.HasPrefix(source.dir, "internal/") {
			continue
		}
		packageTypes := make(map[string]*ast.TypeSpec)
		for _, peer := range sources {
			if peer.dir == source.dir {
				for name, object := range peer.file.Scope.Objects {
					if declaration, ok := object.Decl.(*ast.TypeSpec); ok {
						packageTypes[name] = declaration
					}
				}
			}
		}
		// Substitute only type-parameter occurrences in the container type
		// syntax. This lets Vec[Record] carry Record into its elided literals.
		var substituteType func(ast.Expr, map[string]ast.Expr) ast.Expr
		substituteType = func(expr ast.Expr, arguments map[string]ast.Expr) ast.Expr {
			switch expr := expr.(type) {
			case *ast.Ident:
				if argument := arguments[expr.Name]; argument != nil {
					return argument
				}
			case *ast.ArrayType:
				cloned := *expr
				cloned.Elt = substituteType(expr.Elt, arguments)
				return &cloned
			case *ast.MapType:
				cloned := *expr
				cloned.Key = substituteType(expr.Key, arguments)
				cloned.Value = substituteType(expr.Value, arguments)
				return &cloned
			case *ast.StarExpr:
				cloned := *expr
				cloned.X = substituteType(expr.X, arguments)
				return &cloned
			case *ast.ParenExpr:
				cloned := *expr
				cloned.X = substituteType(expr.X, arguments)
				return &cloned
			case *ast.IndexExpr:
				cloned := *expr
				cloned.X = substituteType(expr.X, arguments)
				cloned.Index = substituteType(expr.Index, arguments)
				return &cloned
			case *ast.IndexListExpr:
				cloned := *expr
				cloned.X = substituteType(expr.X, arguments)
				cloned.Indices = make([]ast.Expr, len(expr.Indices))
				for i, argument := range expr.Indices {
					cloned.Indices[i] = substituteType(argument, arguments)
				}
				return &cloned
			}
			return expr
		}
		instantiateType := func(base ast.Expr, arguments []ast.Expr) ast.Expr {
			for {
				paren, ok := base.(*ast.ParenExpr)
				if !ok {
					break
				}
				base = paren.X
			}
			name, ok := base.(*ast.Ident)
			if !ok {
				return base
			}
			declaration := packageTypes[name.Name]
			if name.Obj != nil {
				declaration, _ = name.Obj.Decl.(*ast.TypeSpec)
			}
			if declaration == nil || declaration.TypeParams == nil {
				return base
			}
			bindings := make(map[string]ast.Expr)
			index := 0
			for _, field := range declaration.TypeParams.List {
				for _, name := range field.Names {
					if index >= len(arguments) {
						return base
					}
					bindings[name.Name] = arguments[index]
					index++
				}
			}
			return substituteType(declaration.Type, bindings)
		}
		// Selector members, declaration names, and struct field labels are not
		// unqualified references, even when appkit is imported with a dot.
		declaredNames := make(map[*ast.Ident]bool)
		// Go permits element literals to elide their type inside arrays,
		// slices, and maps. Carry only that syntactic enclosing type.
		literalTypes := make(map[*ast.CompositeLit]ast.Expr)
		inheritLiteralType := func(expr ast.Expr, enclosingType ast.Expr) {
			if literal, ok := expr.(*ast.CompositeLit); ok && literal.Type == nil {
				literalTypes[literal] = enclosingType
			}
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				declaredNames[node.Name] = true
			case *ast.SelectorExpr:
				declaredNames[node.Sel] = true
			case *ast.Field:
				for _, name := range node.Names {
					declaredNames[name] = true
				}
			case *ast.CompositeLit:
				typeExpr := node.Type
				if typeExpr == nil {
					typeExpr = literalTypes[node]
				}
				seenTypes := make(map[*ast.TypeSpec]bool)
			resolveType:
				for {
					switch expr := typeExpr.(type) {
					case *ast.ParenExpr:
						typeExpr = expr.X
					case *ast.StarExpr:
						typeExpr = expr.X
					case *ast.IndexExpr:
						typeExpr = instantiateType(expr.X, []ast.Expr{expr.Index})
					case *ast.IndexListExpr:
						typeExpr = instantiateType(expr.X, expr.Indices)
					case *ast.Ident:
						declaration := packageTypes[expr.Name]
						if expr.Obj != nil {
							declaration, _ = expr.Obj.Decl.(*ast.TypeSpec)
						}
						if declaration == nil || seenTypes[declaration] {
							break resolveType
						}
						seenTypes[declaration] = true
						typeExpr = declaration.Type
					default:
						break resolveType
					}
				}
				switch container := typeExpr.(type) {
				case *ast.StructType:
					for _, element := range node.Elts {
						if field, ok := element.(*ast.KeyValueExpr); ok {
							if name, ok := field.Key.(*ast.Ident); ok {
								declaredNames[name] = true
							}
						}
					}
				case *ast.ArrayType:
					for _, element := range node.Elts {
						if field, ok := element.(*ast.KeyValueExpr); ok {
							element = field.Value
						}
						inheritLiteralType(element, container.Elt)
					}
				case *ast.MapType:
					for _, element := range node.Elts {
						if field, ok := element.(*ast.KeyValueExpr); ok {
							inheritLiteralType(field.Key, container.Key)
							inheritLiteralType(field.Value, container.Value)
						}
					}
				}
			}
			return true
		})
		ast.Inspect(source.file, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && imports["."] == kitModule && name.Name == "New" && name.Obj == nil && !declaredNames[name] {
				t.Errorf("%s references appkit.New through a dot import", source.dir)
			}
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "New" {
				return true
			}
			name, ok := selector.X.(*ast.Ident)
			// Imported package names have no local object binding; a
			// shadowing variable with a New field is not appkit.New.
			if ok && name.Obj == nil && imports[name.Name] == kitModule {
				t.Errorf("%s references appkit.New", source.dir)
			}
			return true
		})
	}
	for dir := range want {
		if !found[dir] {
			t.Errorf("%s does not import appkit", dir)
		}
	}
}

func TestAppkitModuleRequirement(t *testing.T) {
	// R-SOX5-MLFB
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	inRequire, required := false, false
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "replace" {
			t.Error("go.mod contains a replace directive")
		}
		if fields[0] == ")" {
			inRequire = false
			continue
		}
		if fields[0] == "require" {
			fields = fields[1:]
			if len(fields) > 0 && fields[0] == "(" {
				inRequire = true
				continue
			}
		} else if !inRequire {
			continue
		}
		if len(fields) >= 2 && strings.Trim(fields[0], "\"") == kitModule {
			required = true
		}
	}
	if !required {
		t.Error("go.mod does not require appkit")
	}
}

func TestPackagedInputEntries(t *testing.T) {
	// R-SV0N-JG4S
	for dir, name := range map[string]string{"etc": "manifest.toml", "share": "icon.svg"} {
		entries, err := os.ReadDir(filepath.Join("..", "..", dir))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != name {
			t.Errorf("%s entries = %v; want only %s", dir, entries, name)
			continue
		}
		if dir == "share" && !entries[0].Type().IsRegular() {
			t.Error("share/icon.svg is not a regular file")
		}
	}
}

// normalizeMainWiringParens removes syntax-only parentheses at the expression
// positions used by main's direct wiring. It preserves the underlying nodes,
// including their bindings and source positions, for the wiring checks below.
func normalizeMainWiringParens(node ast.Node) {
	unparen := func(expr ast.Expr) ast.Expr {
		for {
			paren, ok := expr.(*ast.ParenExpr)
			if !ok {
				return expr
			}
			expr = paren.X
		}
	}
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for i, expr := range node.Lhs {
				node.Lhs[i] = unparen(expr)
			}
			for i, expr := range node.Rhs {
				node.Rhs[i] = unparen(expr)
			}
		case *ast.ValueSpec:
			for i, expr := range node.Values {
				node.Values[i] = unparen(expr)
			}
		case *ast.ExprStmt:
			node.X = unparen(node.X)
		case *ast.CallExpr:
			node.Fun = unparen(node.Fun)
			for i, expr := range node.Args {
				node.Args[i] = unparen(expr)
			}
		case *ast.SelectorExpr:
			node.X = unparen(node.X)
		case *ast.SliceExpr:
			node.X = unparen(node.X)
			node.Low = unparen(node.Low)
			node.High = unparen(node.High)
			node.Max = unparen(node.Max)
		case *ast.CompositeLit:
			node.Type = unparen(node.Type)
			for i, expr := range node.Elts {
				node.Elts[i] = unparen(expr)
			}
		case *ast.KeyValueExpr:
			node.Key = unparen(node.Key)
			node.Value = unparen(node.Value)
		}
		return true
	})
}

func astText(t *testing.T, node ast.Node) string {
	t.Helper()
	var out bytes.Buffer
	if err := format.Node(&out, token.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestMainStructuralWiring(t *testing.T) {
	// R-SHLR-BYZ5 R-SQ52-0D60
	path := filepath.Join("..", "..", "cmd", "auth", "main.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if file.Name.Name != "main" {
		t.Fatalf("main.go package = %s", file.Name.Name)
	}
	normalizeMainWiringParens(file)
	imports := sourceImports(t, file)
	// Canonicalize imported identifiers so import aliases do not affect the checks.
	canonical := map[string]string{kitModule: "appkit", authModule + "/internal/cli": "cli", "context": "context", "crypto/rand": "rand", "os": "os", "os/signal": "signal", "syscall": "syscall", "time": "time"}
	ast.Inspect(file, func(node ast.Node) bool {
		name, ok := node.(*ast.Ident)
		if ok && name.Obj == nil {
			if replacement := canonical[imports[name.Name]]; replacement != "" {
				name.Name = replacement
			}
		}
		return true
	})
	var main *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "main" && fn.Recv == nil {
			main = fn
		}
	}
	if main == nil || main.Body == nil {
		t.Fatal("main.go does not own func main()")
	}
	if main.Type.Params.NumFields() != 0 || main.Type.Results.NumFields() != 0 {
		t.Fatal("main has parameters or results")
	}
	calls := make(map[string][]*ast.CallExpr)
	ast.Inspect(main.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			calls[astText(t, call.Fun)] = append(calls[astText(t, call.Fun)], call)
		}
		return true
	})
	oneCall := func(name string) *ast.CallExpr {
		t.Helper()
		if len(calls[name]) != 1 {
			t.Fatalf("main has %d calls to %s, want one", len(calls[name]), name)
		}
		return calls[name][0]
	}
	binding := func(call *ast.CallExpr) []string {
		t.Helper()
		var names []string
		ast.Inspect(main.Body, func(node ast.Node) bool {
			switch declaration := node.(type) {
			case *ast.AssignStmt:
				if len(declaration.Rhs) == 1 && declaration.Rhs[0] == call {
					for _, lhs := range declaration.Lhs {
						name, ok := lhs.(*ast.Ident)
						if !ok {
							t.Fatal("wiring result is not bound to an identifier")
						}
						names = append(names, name.Name)
					}
				}
			case *ast.ValueSpec:
				if len(declaration.Values) == 1 && declaration.Values[0] == call {
					for _, name := range declaration.Names {
						names = append(names, name.Name)
					}
				}
			}
			return true
		})
		return names
	}
	kitCall, runCall, contextCall, exitCall := oneCall("appkit.New"), oneCall("cli.Run"), oneCall("signal.NotifyContext"), oneCall("os.Exit")
	if len(kitCall.Args) != 1 || astText(t, kitCall.Args[0]) != `"auth"` {
		t.Fatal("appkit.New must receive auth")
	}
	if kitCall.Pos() >= runCall.Pos() {
		t.Fatal("appkit.New is not before cli.Run")
	}
	kit, ctx, code := binding(kitCall), binding(contextCall), binding(runCall)
	var cleanupBinding any
	ast.Inspect(main.Body, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			if len(declaration.Rhs) == 1 && declaration.Rhs[0] == contextCall && len(declaration.Lhs) == 2 {
				if name, ok := declaration.Lhs[1].(*ast.Ident); ok && name.Obj != nil {
					cleanupBinding = name.Obj
				}
			}
		case *ast.ValueSpec:
			if len(declaration.Values) == 1 && declaration.Values[0] == contextCall && len(declaration.Names) == 2 && declaration.Names[1].Obj != nil {
				cleanupBinding = declaration.Names[1].Obj
			}
		}
		return true
	})
	inlineRun := len(exitCall.Args) == 1 && exitCall.Args[0] == runCall
	if len(kit) != 1 || len(ctx) != 2 || (!inlineRun && len(code) != 1) {
		t.Fatal("invalid wiring result bindings")
	}
	if len(contextCall.Args) != 3 || astText(t, contextCall.Args[0]) != "context.Background()" {
		t.Fatal("signal context does not start with the process background context")
	}
	signalName := func(value ast.Expr) string {
		name := astText(t, value)
		// os.Interrupt denotes SIGINT, including when os is imported by an alias.
		if name == "os.Interrupt" {
			return "syscall.SIGINT"
		}
		return name
	}
	signals := []string{signalName(contextCall.Args[1]), signalName(contextCall.Args[2])}
	slices.Sort(signals)
	if !slices.Equal(signals, []string{"syscall.SIGINT", "syscall.SIGTERM"}) {
		t.Fatalf("context signals = %v", signals)
	}
	if len(runCall.Args) != 2 || astText(t, runCall.Args[0]) != ctx[0] {
		t.Fatal("cli.Run is not given the signal context")
	}
	if contextCall.Pos() >= runCall.Pos() {
		t.Fatal("signal context is not initialized before cli.Run")
	}
	processExpr := runCall.Args[1]
	if name, ok := processExpr.(*ast.Ident); ok && name.Obj != nil {
		// The Process may be initialized in its declaration or assigned to
		// a predeclared variable. In both cases Run must receive that value
		// only after the initialization, rather than the zero-value Process.
		ast.Inspect(main.Body, func(node ast.Node) bool {
			switch declaration := node.(type) {
			case *ast.AssignStmt:
				if len(declaration.Lhs) == 1 && len(declaration.Rhs) == 1 && declaration.Pos() < runCall.Pos() {
					if target, ok := declaration.Lhs[0].(*ast.Ident); ok && target.Obj == name.Obj {
						processExpr = declaration.Rhs[0]
					}
				}
			case *ast.ValueSpec:
				if len(declaration.Names) == 1 && len(declaration.Values) == 1 && declaration.Pos() < runCall.Pos() && declaration.Names[0].Obj == name.Obj {
					processExpr = declaration.Values[0]
				}
			}
			return true
		})
	}
	process, ok := processExpr.(*ast.CompositeLit)
	if !ok || astText(t, process.Type) != "cli.Process" {
		t.Fatal("cli.Run is not given a cli.Process")
	}
	if kitCall.Pos() >= process.Pos() {
		t.Fatal("appkit kit is not initialized before Process captures its Banner")
	}
	want := map[string]string{
		"Args": "os.Args[1:]", "LookupEnv": "os.LookupEnv", "Unsetenv": "os.Unsetenv", "Pid": "os.Getpid()", "Stdout": "os.Stdout", "Stderr": "os.Stderr", "Inherit": "nil", "Now": "time.Now", "Rand": "rand.Reader", "OIDCIssuer": `"https://accounts.google.com"`, "DBSource": `"state/auth.db"`, "Banner": kit[0] + ".Banner",
	}
	found := make(map[string]string)
	for _, element := range process.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			t.Fatal("Process wiring needs named fields")
		}
		found[astText(t, field.Key)] = astText(t, field.Value)
	}
	for field, value := range want {
		actual, exists := found[field]
		if field == "Inherit" && !exists {
			continue
		} // The omitted field is nil.
		if actual != value {
			t.Errorf("Process.%s = %s, want %s", field, actual, value)
		}
	}
	if len(exitCall.Args) != 1 || (!inlineRun && astText(t, exitCall.Args[0]) != code[0]) {
		t.Fatal("os.Exit is not given cli.Run's result")
	}
	if !inlineRun && exitCall.Pos() <= runCall.Pos() {
		t.Fatal("os.Exit precedes cli.Run")
	}
	// Every call and statement must be part of constructing the seam, releasing
	// its signal context, or handing the result to os.Exit. No handler, command,
	// condition, loop, goroutine, or other application logic belongs in main.
	allowedCalls := map[string]bool{"appkit.New": true, "cli.Run": true, "signal.NotifyContext": true, "context.Background": true, "os.Getpid": true, "os.Exit": true, ctx[1]: true}
	for name := range calls {
		if !allowedCalls[name] {
			t.Errorf("main contains non-wiring call %s", name)
		}
	}
	// Accept both short declarations and var declarations, plus an inline
	// cli.Run argument to os.Exit. Optional signal cleanup is wiring too;
	// it need not have a binding or be called when os.Exit ends the process.
	isWiringValue := func(value ast.Expr) bool {
		return value == kitCall || value == contextCall || value == runCall || value == process
	}
	// Explicitly discarding the resolved signal cleanup callback is the same
	// harmless wiring choice as binding that result to the blank identifier.
	isCleanupDiscard := func(assignment *ast.AssignStmt) bool {
		if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 || cleanupBinding == nil {
			return false
		}
		blank, blankOK := assignment.Lhs[0].(*ast.Ident)
		cleanup, cleanupOK := assignment.Rhs[0].(*ast.Ident)
		return blankOK && blank.Name == "_" && cleanupOK && cleanup.Obj == cleanupBinding
	}
	// A separately declared variable remains wiring when the corresponding
	// assignment supplies one of the required seam values.
	assignedWiring := make(map[any]bool)
	deferredCleanup := make(map[*ast.CallExpr]bool)
	ast.Inspect(main.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if len(node.Rhs) == 1 && isWiringValue(node.Rhs[0]) {
				for _, lhs := range node.Lhs {
					if name, ok := lhs.(*ast.Ident); ok && name.Obj != nil {
						assignedWiring[name.Obj] = true
					}
				}
			}
		case *ast.DeferStmt:
			deferredCleanup[node.Call] = true
		}
		return true
	})
	ast.Inspect(main.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if (len(node.Rhs) != 1 || !isWiringValue(node.Rhs[0])) && !isCleanupDiscard(node) {
				t.Error("main contains an assignment outside its wiring")
			}
		case *ast.ValueSpec:
			if len(node.Values) == 0 {
				for _, name := range node.Names {
					if !assignedWiring[name.Obj] {
						t.Error("main declares a variable outside its wiring")
					}
				}
			} else if len(node.Values) != 1 || !isWiringValue(node.Values[0]) {
				t.Error("main contains a declaration outside its wiring")
			}
		case *ast.ExprStmt:
			call, ok := node.X.(*ast.CallExpr)
			if !ok || (call != exitCall && astText(t, call.Fun) != ctx[1]) {
				t.Error("main contains a statement outside its wiring")
			}
		case *ast.DeferStmt:
			if astText(t, node.Call.Fun) != ctx[1] {
				t.Error("main defers an action outside its signal cleanup")
			}
		case *ast.CallExpr:
			if astText(t, node.Fun) == ctx[1] {
				if len(node.Args) != 0 {
					t.Error("signal cleanup has arguments")
				}
				// R-SQ52-0D60 requires Run's context to be cancelled on
				// SIGTERM or SIGINT. Calling stop first cancels it and
				// unregisters those signals before Run can receive them.
				if node.Pos() < runCall.Pos() && !deferredCleanup[node] {
					t.Error("signal context is cancelled before cli.Run")
				}
			}
		}
		return true
	})
	// Inspect all non-test declarations in package main as well: an init
	// function or package initializer must not hide unrelated application work.
	for _, source := range moduleSources(t, false) {
		if source.dir != "cmd/auth" {
			continue
		}
		normalizeMainWiringParens(source.file)
		packageImports := sourceImports(t, source.file)
		ast.Inspect(source.file, func(node ast.Node) bool {
			name, ok := node.(*ast.Ident)
			if ok && name.Obj == nil {
				if replacement := canonical[packageImports[name.Name]]; replacement != "" {
					name.Name = replacement
				}
			}
			return true
		})
		for _, declaration := range source.file.Decls {
			fn, isFunction := declaration.(*ast.FuncDecl)
			isMain := isFunction && fn.Name.Name == "main" && fn.Recv == nil
			ast.Inspect(declaration, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.CallExpr:
					name := astText(t, node.Fun)
					if !allowedCalls[name] {
						t.Errorf("package main contains non-wiring call %s", name)
					}
					// Calls belong to main's checked wiring, including the
					// process adapters and optional signal cleanup.
					if !isMain {
						t.Errorf("entry-point wiring call %s outside main", name)
					}
				case *ast.AssignStmt, *ast.ValueSpec:
					if !isMain {
						t.Errorf("package main contains a binding outside its entry-point wiring: %T", node)
					}
				case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.GoStmt, *ast.SendStmt, *ast.IncDecStmt, *ast.BranchStmt, *ast.ReturnStmt, *ast.FuncLit, *ast.BinaryExpr, *ast.UnaryExpr:
					t.Errorf("package main contains non-wiring logic %T", node)
				}
				return true
			})
		}
	}
}
