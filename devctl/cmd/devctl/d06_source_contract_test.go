package main

import (
	"errors"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var embeddedInternal = os.DirFS(d06InternalRoot())

func d06InternalRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("locate D06 source contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "internal"))
}

var commandPackages = []string{
	"space",
	"spacecreate",
	"spaceinit",
	"spaceapps",
	"deploy",
	"restore",
	"remove",
	"apex",
}

type parsedSource struct {
	path string
	text string
	file *ast.File
	fset *token.FileSet
}

func TestCompletedStepLinesUseSpaceStep(t *testing.T) {
	// R-VSDZ-20UO
	packages := parseCommandPackages(t)
	for _, packageName := range commandPackages {
		if packageName == "space" {
			continue
		}
		for _, source := range packages[packageName] {
			if strings.Contains(source.text, ": ok (") {
				t.Errorf("%s contains a completed-step rendering; call space.Step instead", source.path)
			}
			assertNoManualCompletionOutput(t, source)
		}
	}
}

func assertNoManualCompletionOutput(t *testing.T, source parsedSource) {
	t.Helper()
	constants := stringConstants(source.file)
	ast.Inspect(source.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isOutputCall(call.Fun) {
			return true
		}
		for _, argument := range call.Args {
			value, known := constantString(argument, constants)
			if known && strings.Contains(value, ": ok") {
				t.Errorf("internal/%s writes a completed-step rendering %q; call space.Step instead", source.path, value)
			}
		}
		return true
	})
}

func stringConstants(file *ast.File) map[string]string {
	values := make(map[string]string)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			for index, name := range value.Names {
				if index >= len(value.Values) {
					continue
				}
				if text, ok := constantString(value.Values[index], values); ok {
					values[name.Name] = text
				}
			}
		}
	}
	return values
}

func constantString(expression ast.Expr, constants map[string]string) (string, bool) {
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if expression.Kind != token.STRING {
			return "", false
		}
		value := constant.MakeFromLiteral(expression.Value, token.STRING, 0)
		if value.Kind() != constant.String {
			return "", false
		}
		return constant.StringVal(value), true
	case *ast.BinaryExpr:
		if expression.Op != token.ADD {
			return "", false
		}
		left, leftOK := constantString(expression.X, constants)
		right, rightOK := constantString(expression.Y, constants)
		return left + right, leftOK && rightOK
	case *ast.ParenExpr:
		return constantString(expression.X, constants)
	case *ast.Ident:
		value, ok := constants[expression.Name]
		return value, ok
	default:
		return "", false
	}
}

func isOutputCall(function ast.Expr) bool {
	switch function := function.(type) {
	case *ast.SelectorExpr:
		qualifier, qualified := function.X.(*ast.Ident)
		if qualified && qualifier.Name == "fmt" {
			switch function.Sel.Name {
			case "Fprint", "Fprintf", "Fprintln":
				return true
			}
		}
		if qualified && qualifier.Name == "io" && function.Sel.Name == "WriteString" {
			return true
		}
		return function.Sel.Name == "Write" || function.Sel.Name == "WriteString"
	default:
		return false
	}
}

func TestPolicyDocumentHasOneArgumentOnlyOwner(t *testing.T) {
	// R-VQYJ-9K7R
	packages := parseCommandPackages(t)
	spaceSources := packages["space"]
	spaceCreateSources := packages["spacecreate"]

	assertNoDirectory(t, "space/templates")
	assertNoDirectory(t, "spacecreate/templates")
	assertNoEmbedDirectives(t, spaceSources)
	assertArgumentOnlyPolicyDocument(t, spaceSources)
	assertNoSpaceCreatePolicyExport(t, spaceCreateSources)
}

func TestSpaceDispatchShape(t *testing.T) {
	// R-V0CA-9ASP
	source := parseEmbeddedSource(t, "cli/run.go")
	var runSpace *ast.FuncDecl
	for _, declaration := range source.file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == "runSpace" {
			if runSpace != nil {
				t.Fatal("internal/cli/run.go declares runSpace more than once")
			}
			runSpace = function
		}
	}
	if runSpace == nil || runSpace.Body == nil {
		t.Fatal("internal/cli/run.go does not declare runSpace")
	}

	wantCases := map[string]struct {
		qualifier string
		tail      bool
	}{
		"create":  {qualifier: "spacecreate", tail: true},
		"init":    {qualifier: "spaceinit", tail: true},
		"restart": {qualifier: "spaceapps"},
		"logs":    {qualifier: "spaceapps"},
	}
	seen := make(map[string]bool, len(wantCases))
	for _, statement := range runSpace.Body.List {
		dispatch, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		for _, guarded := range dispatch.Body.List {
			switchStatement, ok := guarded.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			for _, item := range switchStatement.Body.List {
				clause, ok := item.(*ast.CaseClause)
				if !ok || len(clause.List) != 1 || len(clause.Body) != 1 {
					t.Fatalf("runSpace dispatch clause = %#v, want one name and one return", item)
				}
				literal, ok := clause.List[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("runSpace dispatch case = %#v, want string literal", clause.List[0])
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				want, ok := wantCases[name]
				if !ok || seen[name] {
					t.Fatalf("runSpace has unexpected or duplicate dispatch %q", name)
				}
				seen[name] = true
				assertRunDispatchCall(t, clause.Body[0], want.qualifier, want.tail)
			}
		}
	}
	if len(seen) != len(wantCases) {
		t.Fatalf("runSpace dispatches = %v, want create, init, restart, and logs", seen)
	}
	if len(runSpace.Body.List) == 0 {
		t.Fatal("runSpace has no fallback")
	}
	assertRunDispatchCall(t, runSpace.Body.List[len(runSpace.Body.List)-1], "space", false)
}

// R-30S9-ZDKI
func TestCloudAccessUsesConnectBoundary(t *testing.T) {
	for packagePath, sources := range parseAllInternalPackages(t) {
		for _, violation := range cloudAccessViolations(sources) {
			t.Errorf("internal/%s: %s", packagePath, violation)
		}
	}
}

func TestCloudAccessAnalyzerRejectsForbiddenForms(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{
			name: "connect opener",
			files: map[string]string{"use.go": `package sample
import (
	"context"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func use(ctx context.Context, deps seam.Deps) { _, _ = cloud.Connect(ctx, (((deps.Cloud))), "profile", "region") }
`},
		},
		{
			name: "parenthesized connect function",
			files: map[string]string{"use.go": `package sample
import (
	"context"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func use(ctx context.Context, deps seam.Deps) { _, _ = (cloud.Connect)(ctx, deps.Cloud, "profile", "region") }
`},
		},
		{
			name: "named cloud import",
			files: map[string]string{"use.go": `package sample
import (
	"context"
	platformcloud "github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func use(ctx context.Context, deps seam.Deps) { _, _ = platformcloud.Connect(ctx, deps.Cloud, "profile", "region") }
`},
		},
		{
			name: "unrelated cloud field",
			files: map[string]string{"use.go": `package sample
type options struct { Cloud string }
func use(value options) { _ = value.Cloud }
`},
		},
		{
			name: "direct reference",
			files: map[string]string{"use.go": `package sample
import "github.com/ikigenba/ikigenba/devctl/internal/seam"
func use(deps seam.Deps) { _ = deps.Cloud }
`},
			want: 1,
		},
		{
			name: "different call",
			files: map[string]string{"use.go": `package sample
import "github.com/ikigenba/ikigenba/devctl/internal/seam"
func consume(any) {}
func use(deps seam.Deps) { consume(deps.Cloud) }
`},
			want: 1,
		},
		{
			name: "wrong connect argument",
			files: map[string]string{"use.go": `package sample
import (
	"context"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func use(ctx context.Context, deps seam.Deps) { _, _ = cloud.Connect(ctx, nil, string(deps.Cloud), "region") }
`},
			want: 1,
		},
		{
			name: "field initialization",
			files: map[string]string{"use.go": `package sample
import (
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func use(open cloud.Opener) { _ = seam.Deps{Cloud: open} }
`},
			want: 1,
		},
		{
			name: "aliased field initialization",
			files: map[string]string{"use.go": `package sample
import (
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
type dependencies = seam.Deps
func use(open cloud.Opener) { _ = dependencies{Cloud: open} }
`},
			want: 1,
		},
		{
			name: "cross-file alias",
			files: map[string]string{
				"alias.go": `package sample
import "github.com/ikigenba/ikigenba/devctl/internal/seam"
type dependencies = seam.Deps
`,
				"use.go": `package sample
func use(deps dependencies) { _ = deps.Cloud }
`,
			},
			want: 1,
		},
		{
			name: "positional initialization",
			files: map[string]string{"use.go": `package sample
import "github.com/ikigenba/ikigenba/devctl/internal/seam"
func use() { _ = seam.Deps{"", 0, nil, nil, nil, nil, nil, nil} }
`},
			want: 1,
		},
		{
			name: "aliased positional initialization",
			files: map[string]string{"use.go": `package sample
import "github.com/ikigenba/ikigenba/devctl/internal/seam"
type dependencies = seam.Deps
func use() { _ = dependencies{"", 0, nil, nil, nil, nil, nil, nil} }
`},
			want: 1,
		},
		{
			name: "shadowed cloud import",
			files: map[string]string{"use.go": `package sample
import (
	"context"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)
func use(ctx context.Context, deps seam.Deps) {
	cloud := struct { Connect func(context.Context, cloud.Opener, string, string) (any, error) }{}
	_, _ = cloud.Connect(ctx, deps.Cloud, "profile", "region")
}
`},
			want: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fset := token.NewFileSet()
			sources := make([]parsedSource, 0, len(test.files))
			for fileName, body := range test.files {
				file, err := parser.ParseFile(fset, fileName, body, 0)
				if err != nil {
					t.Fatal(err)
				}
				sources = append(sources, parsedSource{path: path.Join("sample", fileName), text: body, file: file, fset: fset})
			}
			violations := cloudAccessViolations(sources)
			if got := len(violations); got != test.want {
				t.Fatalf("violations = %d, want %d: %v", got, test.want, violations)
			}
		})
	}
}

func cloudAccessViolations(sources []parsedSource) []string {
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	config := types.Config{
		GoVersion: "go1.26",
		Importer:  newContractImporter(),
	}
	var typeErrors []error
	config.Error = func(err error) {
		typeErrors = append(typeErrors, err)
	}
	files := make([]*ast.File, 0, len(sources))
	for _, source := range sources {
		files = append(files, source.file)
	}
	packageDirectory := path.Dir(sources[0].path)
	packagePath := "github.com/ikigenba/ikigenba/devctl/internal/" + packageDirectory
	_, _ = config.Check(packagePath, sources[0].fset, files, info)
	typeErrorDetail := ""
	if len(typeErrors) != 0 {
		typeErrorDetail = ": " + typeErrors[0].Error()
	}

	var violations []string
	for _, source := range sources {
		parents := parentNodes(source.file)
		ast.Inspect(source.file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.SelectorExpr:
				selection := info.Selections[node]
				if selection != nil && selection.Obj().Name() == "Cloud" && isSeamDepsType(selection.Recv()) &&
					!isConnectOpenArgument(node, parents, info) {
					violations = append(violations, source.path+": seam.Deps.Cloud is used outside cloud.Connect's open argument")
				}
				if node.Sel.Name == "Cloud" && selection == nil && !isPackageSelector(node, info) {
					violations = append(violations, source.path+": cannot type-check a Cloud field reference safely"+typeErrorDetail)
				}
			case *ast.CompositeLit:
				literalType := info.TypeOf(node.Type)
				if literalType == nil {
					if compositeLiteralCouldInitializeCloud(node, source, sources) {
						violations = append(violations, source.path+": cannot type-check a composite literal that could initialize Cloud"+typeErrorDetail)
					}
					return true
				}
				if !isSeamDepsType(literalType) {
					return true
				}
				for _, element := range node.Elts {
					field, keyed := element.(*ast.KeyValueExpr)
					if !keyed {
						violations = append(violations, source.path+": seam.Deps uses positional initialization, which initializes Cloud")
						break
					}
					name, ok := field.Key.(*ast.Ident)
					if ok && name.Name == "Cloud" {
						violations = append(violations, source.path+": seam.Deps.Cloud is initialized")
					}
				}
			}
			return true
		})
	}
	// Synthetic imports deliberately model only the two packages this contract
	// needs, so unrelated selectors can produce type errors. Every Cloud selector
	// and every literal that could initialize Cloud is handled conservatively
	// above when type information is missing; those errors therefore cannot turn
	// a forbidden form into a false negative.
	return violations
}

func compositeLiteralCouldInitializeCloud(literal *ast.CompositeLit, source parsedSource, sources []parsedSource) bool {
	for _, element := range literal.Elts {
		field, keyed := element.(*ast.KeyValueExpr)
		if !keyed {
			return syntaxNamesSeamDeps(literal.Type, source, sources, make(map[string]bool))
		}
		name, ok := field.Key.(*ast.Ident)
		if ok && name.Name == "Cloud" {
			return true
		}
	}
	return false
}

func syntaxNamesSeamDeps(expression ast.Expr, source parsedSource, sources []parsedSource, visiting map[string]bool) bool {
	switch expression := unparenthesized(expression).(type) {
	case *ast.SelectorExpr:
		qualifier, ok := expression.X.(*ast.Ident)
		return ok && expression.Sel.Name == "Deps" &&
			importAliases(source.file)[qualifier.Name] == "github.com/ikigenba/ikigenba/devctl/internal/seam"
	case *ast.Ident:
		if visiting[expression.Name] {
			return false
		}
		visiting[expression.Name] = true
		for _, candidate := range sources {
			for _, declaration := range candidate.file.Decls {
				general, ok := declaration.(*ast.GenDecl)
				if !ok || general.Tok != token.TYPE {
					continue
				}
				for _, item := range general.Specs {
					typeSpec := item.(*ast.TypeSpec)
					if typeSpec.Name.Name == expression.Name && typeSpec.Assign.IsValid() {
						return syntaxNamesSeamDeps(typeSpec.Type, candidate, sources, visiting)
					}
				}
			}
		}
	}
	return false
}

func parentNodes(file *ast.File) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	stack := make([]ast.Node, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) != 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func isPackageSelector(selector *ast.SelectorExpr, info *types.Info) bool {
	identifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = info.Uses[identifier].(*types.PkgName)
	return ok
}

func isConnectOpenArgument(expression ast.Expr, parents map[ast.Node]ast.Node, info *types.Info) bool {
	var node ast.Node = expression
	for {
		parent, ok := parents[node]
		if !ok {
			return false
		}
		if parentheses, ok := parent.(*ast.ParenExpr); ok && parentheses.X == node {
			node = parent
			continue
		}
		call, ok := parent.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 || call.Args[1] != node {
			return false
		}
		function, ok := unparenthesized(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return false
		}
		object := info.Uses[function.Sel]
		return object != nil && object.Name() == "Connect" && object.Pkg() != nil &&
			object.Pkg().Path() == "github.com/ikigenba/ikigenba/devctl/internal/cloud"
	}
}

func unparenthesized(expression ast.Expr) ast.Expr {
	for {
		parentheses, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}
		expression = parentheses.X
	}
}

func isSeamDepsType(value types.Type) bool {
	if value == nil {
		return false
	}
	if pointer, ok := types.Unalias(value).(*types.Pointer); ok {
		value = pointer.Elem()
	}
	named, ok := types.Unalias(value).(*types.Named)
	return ok && named.Obj().Name() == "Deps" && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "github.com/ikigenba/ikigenba/devctl/internal/seam"
}

type contractImporter struct {
	packages map[string]*types.Package
}

func newContractImporter() *contractImporter {
	result := &contractImporter{packages: make(map[string]*types.Package)}
	result.packages["github.com/ikigenba/ikigenba/devctl/internal/cloud"] = syntheticCloudPackage()
	result.packages["github.com/ikigenba/ikigenba/devctl/internal/seam"] = syntheticSeamPackage()
	return result
}

func (importer *contractImporter) Import(importPath string) (*types.Package, error) {
	if imported, ok := importer.packages[importPath]; ok {
		return imported, nil
	}
	name := path.Base(importPath)
	imported := types.NewPackage(importPath, name)
	imported.MarkComplete()
	importer.packages[importPath] = imported
	return imported, nil
}

func syntheticCloudPackage() *types.Package {
	const importPath = "github.com/ikigenba/ikigenba/devctl/internal/cloud"
	cloudPackage := types.NewPackage(importPath, "cloud")
	opener := types.NewNamed(types.NewTypeName(token.NoPos, cloudPackage, "Opener", nil),
		types.NewSignatureType(nil, nil, nil, nil, nil, false), nil)
	cloudPackage.Scope().Insert(opener.Obj())
	connect := types.NewFunc(token.NoPos, cloudPackage, "Connect", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	cloudPackage.Scope().Insert(connect)
	cloudPackage.MarkComplete()
	return cloudPackage
}

func syntheticSeamPackage() *types.Package {
	const importPath = "github.com/ikigenba/ikigenba/devctl/internal/seam"
	seamPackage := types.NewPackage(importPath, "seam")
	fields := []*types.Var{
		types.NewField(token.NoPos, seamPackage, "Dir", types.Typ[types.String], false),
		types.NewField(token.NoPos, seamPackage, "EUID", types.Typ[types.Int], false),
		types.NewField(token.NoPos, seamPackage, "Getenv", types.NewInterfaceType(nil, nil).Complete(), false),
		types.NewField(token.NoPos, seamPackage, "Cloud", types.NewInterfaceType(nil, nil).Complete(), false),
		types.NewField(token.NoPos, seamPackage, "Exec", types.NewInterfaceType(nil, nil).Complete(), false),
		types.NewField(token.NoPos, seamPackage, "Stream", types.NewInterfaceType(nil, nil).Complete(), false),
		types.NewField(token.NoPos, seamPackage, "Now", types.NewInterfaceType(nil, nil).Complete(), false),
		types.NewField(token.NoPos, seamPackage, "After", types.NewInterfaceType(nil, nil).Complete(), false),
	}
	deps := types.NewNamed(types.NewTypeName(token.NoPos, seamPackage, "Deps", nil),
		types.NewStruct(fields, nil), nil)
	seamPackage.Scope().Insert(deps.Obj())
	seamPackage.MarkComplete()
	return seamPackage
}

func assertRunDispatchCall(t *testing.T, statement ast.Stmt, qualifier string, tail bool) {
	t.Helper()
	result, ok := statement.(*ast.ReturnStmt)
	if !ok || len(result.Results) != 1 {
		t.Fatalf("%s dispatch body = %#v, want one returned call", qualifier, statement)
	}
	call, ok := result.Results[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 4 || call.Ellipsis.IsValid() {
		t.Fatalf("%s dispatch return = %#v, want Run(ctx, args, stdout, deps)", qualifier, result.Results[0])
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("%s dispatch call = %#v, want %s.Run", qualifier, call.Fun, qualifier)
	}
	packageName, packageOK := selector.X.(*ast.Ident)
	if !packageOK || packageName.Name != qualifier || selector.Sel.Name != "Run" {
		t.Fatalf("%s dispatch call = %#v, want %s.Run", qualifier, call.Fun, qualifier)
	}
	for index, name := range map[int]string{0: "ctx", 2: "stdout", 3: "deps"} {
		identifier, ok := call.Args[index].(*ast.Ident)
		if !ok || identifier.Name != name {
			t.Fatalf("%s.Run argument %d = %#v, want %s", qualifier, index, call.Args[index], name)
		}
	}
	if !tail {
		identifier, ok := call.Args[1].(*ast.Ident)
		if !ok || identifier.Name != "args" {
			t.Fatalf("%s.Run arguments = %#v, want args", qualifier, call.Args[1])
		}
		return
	}
	slice, ok := call.Args[1].(*ast.SliceExpr)
	if !ok {
		t.Fatalf("%s.Run arguments = %#v, want args[1:]", qualifier, call.Args[1])
	}
	low, lowOK := slice.Low.(*ast.BasicLit)
	if !lowOK || low.Kind != token.INT || low.Value != "1" || slice.High != nil || slice.Max != nil || slice.Slice3 {
		t.Fatalf("%s.Run arguments = %#v, want args[1:]", qualifier, call.Args[1])
	}
}

func parseEmbeddedSource(t *testing.T, filePath string) parsedSource {
	t.Helper()
	return parseEmbeddedSourceWithFileSet(t, filePath, token.NewFileSet())
}

func parseEmbeddedSourceWithFileSet(t *testing.T, filePath string, fset *token.FileSet) parsedSource {
	t.Helper()
	contents, err := fs.ReadFile(embeddedInternal, filePath)
	if err != nil {
		t.Fatalf("read embedded internal/%s: %v", filePath, err)
	}
	parsed, err := parser.ParseFile(fset, filePath, contents, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse embedded internal/%s: %v", filePath, err)
	}
	return parsedSource{path: filePath, text: string(contents), file: parsed, fset: fset}
}

func parseAllInternalPackages(t *testing.T) map[string][]parsedSource {
	t.Helper()
	packages := make(map[string][]parsedSource)
	fileSets := make(map[string]*token.FileSet)
	err := fs.WalkDir(embeddedInternal, ".", func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(filePath, ".go") || strings.HasSuffix(filePath, "_test.go") {
			return nil
		}
		packagePath := path.Dir(filePath)
		fset := fileSets[packagePath]
		if fset == nil {
			fset = token.NewFileSet()
			fileSets[packagePath] = fset
		}
		packages[packagePath] = append(packages[packagePath], parseEmbeddedSourceWithFileSet(t, filePath, fset))
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded internal sources: %v", err)
	}
	return packages
}

func parseCommandPackages(t *testing.T) map[string][]parsedSource {
	t.Helper()
	packages := make(map[string][]parsedSource, len(commandPackages))
	for _, packageName := range commandPackages {
		entries, err := fs.ReadDir(embeddedInternal, packageName)
		if errors.Is(err, fs.ErrNotExist) && packageName == "apex" {
			continue
		}
		if err != nil {
			t.Fatalf("read embedded internal/%s: %v", packageName, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			filePath := path.Join(packageName, entry.Name())
			source := parseEmbeddedSource(t, filePath)
			if source.file.Name.Name != packageName {
				t.Errorf("internal/%s declares package %s", filePath, source.file.Name.Name)
			}
			packages[packageName] = append(packages[packageName], source)
		}
	}
	return packages
}

func assertNoDirectory(t *testing.T, directory string) {
	t.Helper()
	_, err := fs.ReadDir(embeddedInternal, directory)
	if err == nil {
		t.Errorf("internal/%s must not exist", directory)
		return
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("inspect embedded internal/%s: %v", directory, err)
	}
}

func assertNoEmbedDirectives(t *testing.T, sources []parsedSource) {
	t.Helper()
	for _, source := range sources {
		for _, group := range source.file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(comment.Text, "//go:embed") {
					t.Errorf("internal/%s embeds data used by the space package", source.path)
				}
			}
		}
	}
}

func assertArgumentOnlyPolicyDocument(t *testing.T, sources []parsedSource) {
	t.Helper()
	functions := make(map[string]*ast.FuncDecl)
	functionFiles := make(map[*ast.FuncDecl]parsedSource)
	packageVariables := make(map[string]struct{})
	for _, source := range sources {
		imports := importAliases(source.file)
		for _, importPath := range imports {
			if isRuntimeSourcePackage(importPath) {
				t.Errorf("internal/%s imports runtime source package %q", source.path, importPath)
			}
		}
		for _, declaration := range source.file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv == nil {
					functions[declaration.Name.Name] = declaration
					functionFiles[declaration] = source
				}
			case *ast.GenDecl:
				if declaration.Tok != token.VAR {
					continue
				}
				for _, specification := range declaration.Specs {
					for _, name := range specification.(*ast.ValueSpec).Names {
						packageVariables[name.Name] = struct{}{}
					}
				}
			}
		}
	}

	policyDocument := functions["PolicyDocument"]
	if policyDocument == nil || !policyDocument.Name.IsExported() {
		t.Fatal("internal/space must export PolicyDocument")
	}

	visited := make(map[string]bool)
	var inspect func(string)
	inspect = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		declaration := functions[name]
		if declaration == nil || declaration.Body == nil {
			return
		}
		source := functionFiles[declaration]
		imports := importAliases(source.file)
		for alias, importPath := range imports {
			if isFileSourcePackage(importPath) {
				t.Errorf("internal/%s imports %q in code reachable from PolicyDocument", source.path, importPath)
				delete(imports, alias)
			}
		}
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.Ident:
				if _, ok := packageVariables[node.Name]; ok {
					t.Errorf("PolicyDocument depends on mutable package name %s in internal/%s", node.Name, source.path)
				}
			case *ast.CallExpr:
				switch called := node.Fun.(type) {
				case *ast.Ident:
					if isFileReadFunction(called.Name) {
						t.Errorf("PolicyDocument reaches file-reading call %s in internal/%s", called.Name, source.path)
					}
					if _, ok := functions[called.Name]; ok {
						inspect(called.Name)
					}
				case *ast.SelectorExpr:
					qualifier, ok := called.X.(*ast.Ident)
					if ok && isFileSourcePackage(imports[qualifier.Name]) && isFileReadFunction(called.Sel.Name) {
						t.Errorf("PolicyDocument reaches file-reading call %s.%s in internal/%s", qualifier.Name, called.Sel.Name, source.path)
					}
				}
			}
			return true
		})
	}
	inspect("PolicyDocument")
}

func importAliases(file *ast.File) map[string]string {
	aliases := make(map[string]string)
	for _, specification := range file.Imports {
		importPath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(importPath)
		if specification.Name != nil {
			alias = specification.Name.Name
		}
		aliases[alias] = importPath
	}
	return aliases
}

func isFileSourcePackage(importPath string) bool {
	switch importPath {
	case "embed", "io/fs", "io/ioutil", "os", "path/filepath":
		return true
	default:
		return false
	}
}

func isRuntimeSourcePackage(importPath string) bool {
	return isFileSourcePackage(importPath) || importPath == "runtime"
}

func isFileReadFunction(name string) bool {
	switch name {
	case "Glob", "Lstat", "Open", "OpenFile", "ReadDir", "ReadFile", "Stat", "Sub", "Walk", "WalkDir":
		return true
	default:
		return false
	}
}

func assertNoSpaceCreatePolicyExport(t *testing.T, sources []parsedSource) {
	t.Helper()
	for _, source := range sources {
		for _, group := range source.file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(comment.Text, "//go:embed") &&
					(strings.Contains(strings.ToLower(comment.Text), "policy") || strings.Contains(strings.ToLower(comment.Text), "template")) {
					t.Errorf("internal/%s embeds a policy template", source.path)
				}
			}
		}
		for _, declaration := range source.file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Name.Name == "PolicyDocument" && declaration.Name.IsExported() {
					t.Errorf("internal/%s exports PolicyDocument", source.path)
				}
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					value, ok := specification.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range value.Names {
						if name.IsExported() && strings.Contains(name.Name, "PolicyTemplate") {
							t.Errorf("internal/%s exports embedded policy template %s", source.path, name.Name)
						}
					}
				}
			}
		}
	}
}
