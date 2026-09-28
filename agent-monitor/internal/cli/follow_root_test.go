package cli

import (
	"embed"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"
	"time"
)

// Embedding makes the concrete harness routing available without reading the
// machine or adding an injectable replacement for the production harnesses.
//
//go:embed run.go follow.go follow_chat.go
var rootRoutingSources embed.FS

func rootRoutingExpression(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return rootRoutingExpression(expr.X) + "." + expr.Sel.Name
	case *ast.StarExpr:
		return rootRoutingExpression(expr.X)
	}
	return ""
}

func rootRoutingKeepsSystem(expr ast.Expr, system string, roots ...string) bool {
	if rootRoutingExpression(expr) == system {
		return true
	}
	literal, ok := expr.(*ast.CompositeLit)
	if !ok || rootRoutingExpression(literal.Type) != "System" {
		return false
	}
	for _, element := range literal.Elts {
		if pair, ok := element.(*ast.KeyValueExpr); ok && rootRoutingExpression(pair.Key) == "Root" {
			return slices.Contains(roots, rootRoutingExpression(pair.Value))
		}
	}
	return false
}

// R-IBKA-V9RV
func TestFollowRootSharedRouting(t *testing.T) {
	functions := make(map[string]*ast.FuncDecl)
	structs := make(map[string]*ast.StructType)
	for _, name := range []string{"run.go", "follow.go", "follow_chat.go"} {
		data, err := rootRoutingSources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.TypeSpec:
				if typ, ok := n.Type.(*ast.StructType); ok {
					structs[n.Name.Name] = typ
				}
			case *ast.FuncDecl:
				key := n.Name.Name
				if n.Recv != nil {
					key = rootRoutingExpression(n.Recv.List[0].Type) + "." + key
				}
				functions[key] = n
			}
			return true
		})
	}
	systemParameter := func(fn *ast.FuncDecl) string {
		for _, p := range fn.Type.Params.List {
			if rootRoutingExpression(p.Type) == "System" {
				return p.Names[0].Name
			}
		}
		return ""
	}
	follow := functions["followCommand"]
	if follow == nil {
		t.Fatal("missing follow entry point")
	}
	systemName := systemParameter(follow)
	rootField := systemName + ".Root"
	rootName := ""
	var rootAssignment *ast.AssignStmt
	for _, stmt := range follow.Body.List {
		if a, ok := stmt.(*ast.AssignStmt); ok && len(a.Lhs) == 1 && len(a.Rhs) == 1 && rootRoutingExpression(a.Lhs[0]) == rootField && rootRoutingExpression(a.Rhs[0]) != rootField {
			rootName = rootRoutingExpression(a.Rhs[0])
			rootAssignment = a
			break
		}
	}
	if rootName == "" {
		t.Fatal("render system does not retain a follow root")
	}
	var construction *ast.AssignStmt
	for _, stmt := range follow.Body.List {
		a, ok := stmt.(*ast.AssignStmt)
		if !ok || a.Pos() >= rootAssignment.Pos() || len(a.Lhs) != 1 || len(a.Rhs) != 1 || rootRoutingExpression(a.Lhs[0]) != rootName {
			continue
		}
		call, ok := a.Rhs[0].(*ast.CallExpr)
		if ok && rootRoutingExpression(call.Fun) == "newFollowRoot" && len(call.Args) == 1 && rootRoutingExpression(call.Args[0]) == rootField {
			construction = a
		}
	}
	if construction == nil {
		t.Fatal("follow root is not constructed from the invocation root before rendering")
	}
	lastRootAssignment := rootAssignment.Pos()
	ast.Inspect(follow.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range a.Lhs {
				if rootRoutingExpression(lhs) == rootField && len(a.Rhs) == len(a.Lhs) && rootRoutingExpression(a.Rhs[i]) == rootName && a.Pos() > lastRootAssignment {
					lastRootAssignment = a.Pos()
				}
			}
		}
		return true
	})
	ast.Inspect(follow.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok && a != construction {
			for i, lhs := range a.Lhs {
				name := rootRoutingExpression(lhs)
				if name == systemName && (len(a.Rhs) != len(a.Lhs) || !rootRoutingKeepsSystem(a.Rhs[i], systemName, rootName, rootField)) {
					t.Error("following replaces its root System")
				}
				if name == rootName && a.Pos() < lastRootAssignment && (len(a.Rhs) != len(a.Lhs) || rootRoutingExpression(a.Rhs[i]) != rootName) {
					t.Error("following replaces its shared root binding")
				}
				if name == rootField && (len(a.Rhs) != len(a.Lhs) || (rootRoutingExpression(a.Rhs[i]) != rootName && rootRoutingExpression(a.Rhs[i]) != rootField)) {
					t.Error("following replaces its render root")
				}
			}
		}
		return true
	})
	// Discover the retained System field from the actual renderer literals,
	// including constructors called with the invocation's render System.
	retained := make(map[string]string)
	for typ, structure := range structs {
		for _, field := range structure.Fields.List {
			if rootRoutingExpression(field.Type) == "System" {
				for _, name := range field.Names {
					retained[typ] = name.Name
				}
			}
		}
	}
	retain := func(fn *ast.FuncDecl, system string) {
		roots := []string{system + ".Root"}
		if fn == follow {
			roots = append(roots, rootName)
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok && fn != follow {
				for i, lhs := range a.Lhs {
					name := rootRoutingExpression(lhs)
					if name == system && (len(a.Rhs) != len(a.Lhs) || !rootRoutingKeepsSystem(a.Rhs[i], system, system+".Root")) {
						t.Error("renderer constructor replaces the root System")
					}
					if name == system+".Root" && (len(a.Rhs) != len(a.Lhs) || rootRoutingExpression(a.Rhs[i]) != name) {
						t.Error("renderer constructor replaces the shared root")
					}
				}
			}
			literal, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			field, hasSystem := retained[rootRoutingExpression(literal.Type)]
			if !hasSystem {
				return true
			}
			if fn == follow && literal.Pos() < rootAssignment.Pos() {
				t.Error("renderer retains the original root before replacement")
			}
			found := false
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if ok && rootRoutingExpression(pair.Key) == field {
					found = true
					if !rootRoutingKeepsSystem(pair.Value, system, roots...) {
						t.Error("renderer retains a different root System")
					}
				}
			}
			if !found {
				t.Error("renderer does not retain the root System")
			}
			return true
		})
	}
	retain(follow, systemName)
	ast.Inspect(follow.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn := functions[rootRoutingExpression(call.Fun)]
		if fn == nil {
			return true
		}
		constructsRenderer := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if literal, ok := n.(*ast.CompositeLit); ok {
				if _, ok := retained[rootRoutingExpression(literal.Type)]; ok {
					constructsRenderer = true
				}
			}
			return true
		})
		if !constructsRenderer {
			return true
		}
		index := 0
		for _, parameter := range fn.Type.Params.List {
			for range parameter.Names {
				if rootRoutingExpression(parameter.Type) == "System" {
					if len(call.Args) <= index || !rootRoutingKeepsSystem(call.Args[index], systemName, rootName, rootField) {
						t.Error("renderer constructor receives a different root System")
					}
					if call.Pos() < rootAssignment.Pos() {
						t.Error("renderer receives the original root before replacement")
					}
					retain(fn, systemParameter(fn))
				}
				index++
			}
		}
		return true
	})
	if len(retained) == 0 {
		t.Fatal("no renderer retains the follow System")
	}
	renderCalls, passes := 0, 0
	for key, fn := range functions {
		if fn.Recv == nil {
			continue
		}
		typ := rootRoutingExpression(fn.Recv.List[0].Type)
		field, ok := retained[typ]
		if !ok {
			continue
		}
		receiver := fn.Recv.List[0].Names[0].Name
		retainedSystem := receiver + "." + field
		transcriptFields := make(map[string]bool)
		for _, f := range structs[typ].Fields.List {
			if rootRoutingExpression(f.Type) == "chat.Transcript" {
				for _, name := range f.Names {
					transcriptFields[receiver+"."+name.Name+".Read"] = true
				}
			}
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok {
				for i, lhs := range a.Lhs {
					if name := rootRoutingExpression(lhs); name == retainedSystem && (len(a.Rhs) != len(a.Lhs) || !rootRoutingKeepsSystem(a.Rhs[i], retainedSystem, retainedSystem+".Root")) {
						t.Errorf("%s replaces the retained root System", key)
					}
					if name := rootRoutingExpression(lhs); name == retainedSystem+".Root" && (len(a.Rhs) != len(a.Lhs) || rootRoutingExpression(a.Rhs[i]) != name) {
						t.Errorf("%s replaces the retained root", key)
					}
				}
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := rootRoutingExpression(call.Fun)
			if name == "renderCommand" {
				renderCalls++
				if len(call.Args) != 2 || !rootRoutingKeepsSystem(call.Args[1], retainedSystem, retainedSystem+".Root") {
					t.Errorf("%s changes the harness root", key)
				}
			}
			if transcriptFields[name] {
				passes++
				if len(call.Args) != 1 || rootRoutingExpression(call.Args[0]) != retainedSystem+".Root" {
					t.Error("transcript pass uses a different root")
				}
			}
			return true
		})
	}
	harnessCalls := 0
	render := functions["renderCommand"]
	harnessRoot := systemParameter(render) + ".Root"
	ast.Inspect(render.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range a.Lhs {
				name := rootRoutingExpression(lhs)
				if name == systemParameter(render) && (len(a.Rhs) != len(a.Lhs) || !rootRoutingKeepsSystem(a.Rhs[i], systemParameter(render), harnessRoot)) {
					t.Error("harness dispatch replaces its root System")
				}
				if name == harnessRoot && (len(a.Rhs) != len(a.Lhs) || rootRoutingExpression(a.Rhs[i]) != name) {
					t.Error("harness dispatch replaces its shared root")
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !slices.Contains([]string{"claude", "codex", "grok"}, rootRoutingExpression(selector.X)) || !slices.Contains([]string{"List", "Tree", "Chat"}, selector.Sel.Name) {
			return true
		}
		harnessCalls++
		if len(call.Args) == 0 || rootRoutingExpression(call.Args[0]) != harnessRoot {
			t.Errorf("%s uses a different harness root", rootRoutingExpression(call.Fun))
		}
		return true
	})
	if renderCalls == 0 || harnessCalls == 0 || passes == 0 {
		t.Fatalf("unverified routing: render calls=%d harness calls=%d transcript passes=%d", renderCalls, harnessCalls, passes)
	}
}

type rootForwardProbe struct {
	root  fstest.MapFS
	calls []string
	err   error
	file  fs.File
}

func (p *rootForwardProbe) Open(n string) (fs.File, error) {
	p.calls = append(p.calls, "Open:"+n)
	if p.err != nil {
		return nil, p.err
	}
	if p.file != nil {
		return p.file, nil
	}
	return p.root.Open(n)
}
func (p *rootForwardProbe) ReadDir(n string) ([]fs.DirEntry, error) {
	p.calls = append(p.calls, "ReadDir:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.ReadDir(p.root, n)
}
func (p *rootForwardProbe) ReadFile(n string) ([]byte, error) {
	p.calls = append(p.calls, "ReadFile:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.ReadFile(p.root, n)
}
func (p *rootForwardProbe) Stat(n string) (fs.FileInfo, error) {
	p.calls = append(p.calls, "Stat:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.Stat(p.root, n)
}
func (p *rootForwardProbe) ReadLink(n string) (string, error) {
	p.calls = append(p.calls, "ReadLink:"+n)
	if p.err != nil {
		return "", p.err
	}
	return "raw/../target", nil
}
func (p *rootForwardProbe) Lstat(n string) (fs.FileInfo, error) {
	p.calls = append(p.calls, "Lstat:"+n)
	if p.err != nil {
		return nil, p.err
	}
	return fs.Stat(p.root, n)
}

// R-IBKA-V9RV
func TestFollowRootMethodSetAndForwarding(t *testing.T) {
	r := newFollowRoot(fstest.MapFS{})
	typ := reflect.TypeOf(r)
	var methods []string
	for i := range typ.NumMethod() {
		methods = append(methods, typ.Method(i).Name)
	}
	wantMethods := []string{"Lstat", "Open", "ReadDir", "ReadFile", "ReadLink", "Stat"}
	if !slices.Equal(methods, wantMethods) {
		t.Fatalf("methods = %v, want %v", methods, wantMethods)
	}
	f, err := (fstest.MapFS{"file": &fstest.MapFile{Data: []byte("text")}}).Open("file")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	p := &rootForwardProbe{file: f}
	if got, err := newFollowRoot(p).Open("unchanged/../name"); got != f || err != nil {
		t.Fatalf("Open did not preserve file: %v, %v", got, err)
	}
	operations := []struct {
		name string
		call func(fs.FS, string) (any, error)
	}{
		{"Open", func(r fs.FS, n string) (any, error) {
			f, err := r.Open(n)
			if err != nil {
				return nil, err
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			return io.ReadAll(f)
		}},
		{"ReadDir", func(r fs.FS, n string) (any, error) { return fs.ReadDir(r, n) }},
		{"ReadFile", func(r fs.FS, n string) (any, error) { return fs.ReadFile(r, n) }},
		{"Stat", func(r fs.FS, n string) (any, error) { return fs.Stat(r, n) }},
		{"ReadLink", func(r fs.FS, n string) (any, error) { return fs.ReadLink(r, n) }},
		{"Lstat", func(r fs.FS, n string) (any, error) { return fs.Lstat(r, n) }},
	}
	for _, op := range operations {
		for _, n := range []string{"dir", "dir/file", "missing", "dir/../file", "/absolute", ""} {
			for _, injected := range []error{nil, errors.New("read denied")} {
				p := &rootForwardProbe{root: fstest.MapFS{"dir/file": &fstest.MapFile{Data: []byte("text")}}, err: injected}
				want, wantErr := op.call(p, n)
				p.calls = nil
				got, gotErr := op.call(newFollowRoot(p), n)
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
					t.Errorf("%s(%q) = (%v,%v), want (%v,%v)", op.name, n, got, gotErr, want, wantErr)
				}
				if p.calls[len(p.calls)-1] != op.name+":"+n {
					t.Errorf("%s(%q) calls %v", op.name, n, p.calls)
				}
			}
		}
	}
}

// R-IBKA-V9RV R-IE03-MT99
func TestFollowRootGlobAndSubAccountNames(t *testing.T) {
	r := newFollowRoot(fstest.MapFS{"home/dev/logs/a/file": &fstest.MapFile{Data: []byte("log")}})
	matches, err := fs.Glob(r, "home/dev/logs/*/file")
	if err != nil || !slices.Equal(matches, []string{"home/dev/logs/a/file"}) {
		t.Fatalf("Glob = %v, %v", matches, err)
	}
	sub, err := fs.Sub(r, "home/dev/logs/a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(sub, "file"); err != nil {
		t.Fatal(err)
	}
	if got := watchedFollowNames(r); !slices.Equal(got, []string{"home/dev/logs", "home/dev/logs/a"}) {
		t.Errorf("watched = %v", got)
	}
}

// R-IE03-MT99
func TestFollowWatchedNames(t *testing.T) {
	r := newFollowRoot(fstest.MapFS{
		"home/dev/.claude/sessions/41822.json": &fstest.MapFile{Data: []byte("{}")},
		"proc/41822/stat":                      &fstest.MapFile{Data: []byte("stat")},
		"process/log":                          &fstest.MapFile{Data: []byte("log")},
	})
	_, _ = r.ReadDir("home/dev/.claude/sessions")
	f, err := r.Open("home/dev/.claude/sessions/41822.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_, _ = r.ReadFile("proc/41822/stat")
	_, _ = r.Stat("proc")
	_, _ = r.Stat("home/dev/.grok")
	_, _ = r.ReadFile("home/dev/.claude/sessions/41822.json")
	_, _ = r.Lstat("home/dev/.claude/sessions")
	_, _ = r.ReadLink("home/dev/.grok/link")
	want := []string{"home/dev", "home/dev/.claude/sessions", "home/dev/.grok"}
	if got := watchedFollowNames(r); !slices.Equal(got, want) {
		t.Errorf("watched = %v, want %v", got, want)
	}
	beginFollowRender(r)
	if got := watchedFollowNames(r); len(got) != 0 {
		t.Errorf("new render watched = %v", got)
	}
	_, _ = r.Stat("process/log")
	if got := watchedFollowNames(r); !slices.Equal(got, []string{"process"}) {
		t.Errorf("proc prefix exclusion = %v", got)
	}
}

// R-IE03-MT99
func TestFollowWatchedNamesForEachMethod(t *testing.T) {
	methods := []struct {
		name string
		call func(*followRoot, string)
	}{
		{"Open", func(r *followRoot, n string) {
			f, err := r.Open(n)
			if err == nil {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}
		}},
		{"ReadDir", func(r *followRoot, n string) { _, _ = r.ReadDir(n) }},
		{"ReadFile", func(r *followRoot, n string) { _, _ = r.ReadFile(n) }},
		{"Stat", func(r *followRoot, n string) { _, _ = r.Stat(n) }},
		{"ReadLink", func(r *followRoot, n string) { _, _ = r.ReadLink(n) }},
		{"Lstat", func(r *followRoot, n string) { _, _ = r.Lstat(n) }},
	}
	cases := []struct {
		name string
		want []string
	}{
		{"home/dev/data", []string{"home/dev/data"}},
		{"home/dev/data/file", []string{"home/dev/data"}},
		{"home/dev/absent/file", []string{"home/dev/absent"}},
		{"proc", []string{}},
		{"proc/123/stat", []string{}},
		{"proc/absent", []string{}},
		{"process/file", []string{"process"}},
	}
	for _, method := range methods {
		for _, tc := range cases {
			t.Run(method.name+"/"+tc.name, func(t *testing.T) {
				r := newFollowRoot(fstest.MapFS{"home/dev/data/file": &fstest.MapFile{Data: []byte("data")}, "proc/123/stat": &fstest.MapFile{Data: []byte("stat")}})
				method.call(r, tc.name)
				if got := watchedFollowNames(r); !slices.Equal(got, tc.want) {
					t.Fatalf("watched = %v, want %v", got, tc.want)
				}
				beginFollowRender(r)
				if got := watchedFollowNames(r); len(got) != 0 {
					t.Fatalf("next render watched = %v", got)
				}
			})
		}
	}
}

// The mutating method signatures remain available on both the root and files.
// Any call fails the test immediately rather than altering the fixture.
type rootMutationTrap struct{ t *testing.T }

func (p rootMutationTrap) fail() {
	p.t.Helper()
	p.t.Fatal("following called a mutating filesystem method")
}
func (p rootMutationTrap) Write([]byte) (int, error)                   { p.fail(); return 0, nil }
func (p rootMutationTrap) WriteAt([]byte, int64) (int, error)          { p.fail(); return 0, nil }
func (p rootMutationTrap) WriteString(string) (int, error)             { p.fail(); return 0, nil }
func (p rootMutationTrap) WriteFile(string, []byte, fs.FileMode) error { p.fail(); return nil }
func (p rootMutationTrap) Create(string) (fs.File, error)              { p.fail(); return nil, nil }
func (p rootMutationTrap) OpenFile(string, int, fs.FileMode) (fs.File, error) {
	p.fail()
	return nil, nil
}
func (p rootMutationTrap) Mkdir(string, fs.FileMode) error            { p.fail(); return nil }
func (p rootMutationTrap) MkdirAll(string, fs.FileMode) error         { p.fail(); return nil }
func (p rootMutationTrap) Remove(string) error                        { p.fail(); return nil }
func (p rootMutationTrap) RemoveAll(string) error                     { p.fail(); return nil }
func (p rootMutationTrap) Rename(string, string) error                { p.fail(); return nil }
func (p rootMutationTrap) Truncate(int64) error                       { p.fail(); return nil }
func (p rootMutationTrap) Chmod(fs.FileMode) error                    { p.fail(); return nil }
func (p rootMutationTrap) Chown(int, int) error                       { p.fail(); return nil }
func (p rootMutationTrap) Chtimes(string, time.Time, time.Time) error { p.fail(); return nil }
func (p rootMutationTrap) Symlink(string, string) error               { p.fail(); return nil }
func (p rootMutationTrap) Link(string, string) error                  { p.fail(); return nil }
func (p rootMutationTrap) Sync() error                                { p.fail(); return nil }

type rootReadOnlyProbe struct {
	rootMutationTrap
	root fstest.MapFS
}
type rootReadOnlyFile struct {
	rootMutationTrap
	fs.File
}

type rootReadOnlyInfo struct {
	rootMutationTrap
	fs.FileInfo
}
type rootReadOnlyEntry struct {
	rootMutationTrap
	fs.DirEntry
}

func (f rootReadOnlyFile) ReadAt(p []byte, offset int64) (int, error) {
	return f.File.(io.ReaderAt).ReadAt(p, offset)
}

func (f rootReadOnlyFile) Stat() (fs.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return rootReadOnlyInfo{rootMutationTrap: f.rootMutationTrap, FileInfo: info}, nil
}
func (e rootReadOnlyEntry) Info() (fs.FileInfo, error) {
	info, err := e.DirEntry.Info()
	if err != nil {
		return nil, err
	}
	return rootReadOnlyInfo{rootMutationTrap: e.rootMutationTrap, FileInfo: info}, nil
}

func (p rootReadOnlyProbe) Open(n string) (fs.File, error) {
	f, err := p.root.Open(n)
	if err != nil {
		return nil, err
	}
	return rootReadOnlyFile{rootMutationTrap: p.rootMutationTrap, File: f}, nil
}
func (f rootReadOnlyFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.File.(fs.ReadDirFile).ReadDir(n)
	for i, e := range entries {
		entries[i] = rootReadOnlyEntry{rootMutationTrap: f.rootMutationTrap, DirEntry: e}
	}
	return entries, err
}

type rootSafetyWatcher struct{ changes chan struct{} }

func (rootSafetyWatcher) Watch([]string)             {}
func (w rootSafetyWatcher) Changes() <-chan struct{} { return w.changes }

// R-F075-Q4BT
func TestFollowDoesNotMutateRootOrFiles(t *testing.T) {
	root := fstest.MapFS{
		"home/dev/.claude/projects/work/sample.jsonl":                                  &fstest.MapFile{Data: []byte("{}\n")},
		"home/dev/.claude/sessions/sample.json":                                        &fstest.MapFile{Data: []byte(`{"sessionId":"sample","pid":42}`)},
		"home/dev/.codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-sample.jsonl": &fstest.MapFile{Data: []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"sample\"}}\n")},
		"home/dev/.grok/sessions/x/sample/summary.json":                                &fstest.MapFile{Data: []byte(`{"session_kind":"headless"}`)},
		"home/dev/.grok/sessions/x/sample/updates.jsonl":                               &fstest.MapFile{Data: []byte("{}\n")},
	}
	for _, terminal := range []bool{false, true} {
		for _, harness := range []string{"claude", "codex", "grok"} {
			for _, command := range []string{"list", "tree", "chat"} {
				args := []string{command, harness}
				if command != "list" {
					args = append(args, "sample")
				}
				args = append(args, "-f")
				interrupt := make(chan struct{})
				changes := make(chan struct{})
				done := make(chan struct{})
				go func() {
					defer close(done)
					for range 2 {
						select {
						case changes <- struct{}{}:
						case <-interrupt:
							return
						}
					}
					close(interrupt)
				}()
				sys := System{Home: "/home/dev", Root: rootReadOnlyProbe{rootMutationTrap: rootMutationTrap{t: t}, root: root}, Terminal: terminal, Interrupt: interrupt, Watcher: rootSafetyWatcher{changes: changes}}
				if code := Run(args, sys, io.Discard, io.Discard); code != ExitSuccess {
					close(interrupt)
					<-done
					t.Fatalf("fixture failed before following %v: %d", args, code)
				}
				<-done
			}
		}
	}
}
