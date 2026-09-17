// Package main verifies the module's architecture as well as its entry point.
package main

import (
	"bufio"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/ikigenba/ikigenba/devctl"

type sourceFile struct {
	path       string
	pkgPath    string
	file       *ast.File
	imports    []string
	importPath map[string]string
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func moduleSources(t *testing.T) []sourceFile {
	t.Helper()
	root := moduleRoot(t)
	files := make([]sourceFile, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == filepath.Join(root, "vendor") {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
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
		pkgPath := modulePath
		if relDir != "." {
			pkgPath += "/" + filepath.ToSlash(relDir)
		}
		imports := make([]string, 0, len(parsed.Imports))
		importPaths := make(map[string]string)
		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports = append(imports, imported)
			name := filepath.Base(imported)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			importPaths[name] = imported
		}
		files = append(files, sourceFile{
			path:       path,
			pkgPath:    pkgPath,
			file:       parsed,
			imports:    imports,
			importPath: importPaths,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("read module sources: %v", err)
	}
	return files
}

func isStandardImport(path string) bool {
	pkg, err := build.Default.Import(path, "", build.FindOnly)
	return err == nil && pkg.Goroot
}

// R-DM8X-DRGU
func TestModuleAndApprovedDirectRequirements(t *testing.T) {
	file, err := os.Open(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close go.mod: %v", err)
		}
	}()

	want := map[string]string{
		"github.com/BurntSushi/toml":                   "v1.6.0",
		"github.com/aws/aws-sdk-go-v2":                 "v1.47.0",
		"github.com/aws/aws-sdk-go-v2/config":          "v1.33.5",
		"github.com/aws/aws-sdk-go-v2/service/ec2":     "v1.332.0",
		"github.com/aws/aws-sdk-go-v2/service/iam":     "v1.64.0",
		"github.com/aws/aws-sdk-go-v2/service/route53": "v1.70.0",
		"github.com/aws/aws-sdk-go-v2/service/s3":      "v1.113.1",
		"github.com/aws/aws-sdk-go-v2/service/ssm":     "v1.78.0",
		"github.com/aws/aws-sdk-go-v2/service/sts":     "v1.51.0",
		"github.com/aws/smithy-go":                     "v1.28.1",
	}
	got := make(map[string]string)
	var module, goVersion string
	inRequire := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		switch {
		case len(fields) == 2 && fields[0] == "module":
			module = fields[1]
		case len(fields) == 2 && fields[0] == "go":
			goVersion = fields[1]
		case line == "require (":
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire && len(fields) >= 2 && !strings.Contains(line, "// indirect"):
			got[fields[0]] = fields[1]
		case !inRequire && len(fields) >= 3 && fields[0] == "require" && !strings.Contains(line, "// indirect"):
			got[fields[1]] = fields[2]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if module != modulePath {
		t.Errorf("module = %q, want %q", module, modulePath)
	}
	if goVersion == "" {
		t.Error("go.mod does not specify a Go version")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("direct requirements = %v, want %v", got, want)
	}
}

// R-BLKL-AZUS
func TestModuleContainsExactlyDesignedPackages(t *testing.T) {
	want := []string{
		"cmd/devctl",
		"internal/account",
		"internal/appref",
		"internal/build",
		"internal/checkout",
		"internal/cli",
		"internal/cloud",
		"internal/cloud/awssdk",
		"internal/deploy",
		"internal/host",
		"internal/hostsetup",
		"internal/keyring",
		"internal/remove",
		"internal/restore",
		"internal/seam",
		"internal/secrets",
		"internal/space",
		"internal/spaceapps",
		"internal/spacecreate",
		"internal/spaceinit",
	}
	seen := make(map[string]bool)
	for _, source := range moduleSources(t) {
		seen[strings.TrimPrefix(source.pkgPath, modulePath+"/")] = true
	}
	got := make([]string, 0, len(seen))
	for pkg := range seen {
		got = append(got, pkg)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("module packages = %v, want %v", got, want)
	}
}

// R-A4S9-ZZUZ
func TestPackageImportBoundaries(t *testing.T) {
	allowedMain := map[string]bool{
		modulePath + "/internal/cli":          true,
		modulePath + "/internal/seam":         true,
		modulePath + "/internal/cloud/awssdk": true,
	}
	approvedAWSModules := map[string]bool{
		"github.com/aws/aws-sdk-go-v2":                 true,
		"github.com/aws/aws-sdk-go-v2/config":          true,
		"github.com/aws/aws-sdk-go-v2/service/ec2":     true,
		"github.com/aws/aws-sdk-go-v2/service/iam":     true,
		"github.com/aws/aws-sdk-go-v2/service/route53": true,
		"github.com/aws/aws-sdk-go-v2/service/s3":      true,
		"github.com/aws/aws-sdk-go-v2/service/ssm":     true,
		"github.com/aws/aws-sdk-go-v2/service/sts":     true,
		"github.com/aws/smithy-go":                     true,
	}
	requiredModules := readRequiredModulePaths(t)
	for _, source := range moduleSources(t) {
		for _, imported := range source.imports {
			isModule := imported == modulePath || strings.HasPrefix(imported, modulePath+"/")
			isExternal := !isModule && !isStandardImport(imported)
			switch {
			case source.pkgPath == modulePath+"/cmd/devctl" && isModule && !allowedMain[imported]:
				t.Errorf("%s: cmd/devctl imports disallowed module package %q", source.path, imported)
			case imported == modulePath+"/internal/cli" && source.pkgPath != modulePath+"/cmd/devctl" && source.pkgPath != modulePath+"/internal/cli":
				t.Errorf("%s: internal/cli imported outside cmd/devctl", source.path)
			case source.pkgPath == modulePath+"/internal/cloud" && isModule:
				t.Errorf("%s: internal/cloud imports module package %q", source.path, imported)
			case source.pkgPath == modulePath+"/internal/seam" && isModule && imported != modulePath+"/internal/cloud":
				t.Errorf("%s: internal/seam imports module package %q", source.path, imported)
			}
			if !isExternal {
				continue
			}
			owner := owningModule(imported, requiredModules)
			allowedExternal := source.pkgPath == modulePath+"/internal/cloud/awssdk" && approvedAWSModules[owner]
			allowedExternal = allowedExternal || source.pkgPath == modulePath+"/internal/checkout" && imported == "github.com/BurntSushi/toml"
			if !allowedExternal {
				t.Errorf("%s: package imports disallowed external dependency %q", source.path, imported)
			}
		}
	}
}

func readRequiredModulePaths(t *testing.T) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var modules []string
	inRequire := false
	for _, raw := range strings.Split(string(contents), "\n") {
		line := strings.TrimSpace(raw)
		fields := strings.Fields(line)
		switch {
		case line == "require (":
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire && len(fields) >= 2:
			modules = append(modules, fields[0])
		case !inRequire && len(fields) >= 3 && fields[0] == "require":
			modules = append(modules, fields[1])
		}
	}
	return modules
}

func owningModule(importPath string, modules []string) string {
	owner := ""
	for _, module := range modules {
		if (importPath == module || strings.HasPrefix(importPath, module+"/")) && len(module) > len(owner) {
			owner = module
		}
	}
	return owner
}

// R-BVBS-D5SC
func TestEnvironmentalOperationsStayAtRunSeam(t *testing.T) {
	prohibitedOS := map[string]bool{"Getenv": true, "Getwd": true, "UserHomeDir": true, "Geteuid": true}
	prohibitedTime := map[string]bool{"Now": true, "Since": true, "Sleep": true, "After": true, "Tick": true}
	for _, source := range moduleSources(t) {
		for _, imported := range source.imports {
			if imported == "os/exec" && source.pkgPath != modulePath+"/internal/seam" {
				t.Errorf("%s: imports os/exec outside internal/seam", source.path)
			}
		}
		for _, spec := range source.file.Imports {
			if spec.Name != nil && spec.Name.Name == "." && (spec.Path.Value == `"os"` || spec.Path.Value == `"time"`) {
				t.Errorf("%s: dot-import prevents environmental boundary verification", source.path)
			}
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			imported := source.importPath[ident.Name]
			if imported == "os" {
				allowed := source.pkgPath == modulePath+"/cmd/devctl"
				if selector.Sel.Name == "Environ" {
					allowed = allowed || source.pkgPath == modulePath+"/internal/seam"
				}
				if prohibitedOS[selector.Sel.Name] && !allowed {
					t.Errorf("%s: references os.%s outside cmd/devctl", source.path, selector.Sel.Name)
				}
				if selector.Sel.Name == "Environ" && !allowed {
					t.Errorf("%s: references os.Environ outside cmd/devctl or internal/seam", source.path)
				}
			}
			if imported == "time" && prohibitedTime[selector.Sel.Name] && source.pkgPath != modulePath+"/cmd/devctl" && source.pkgPath != modulePath+"/internal/seam" {
				t.Errorf("%s: references time.%s outside cmd/devctl or internal/seam", source.path, selector.Sel.Name)
			}
			return true
		})
	}
}

// R-BU3V-ZE1N
func TestCommandsUseExplicitProcessDirectoriesAndNoAmbientPaths(t *testing.T) {
	filesystemCalls := map[string]bool{
		"Create": true, "Mkdir": true, "MkdirAll": true, "Open": true,
		"OpenFile": true, "ReadFile": true, "Readlink": true, "Remove": true,
		"RemoveAll": true, "Rename": true, "Stat": true, "WriteFile": true,
	}
	for _, source := range moduleSources(t) {
		if strings.HasSuffix(source.path, "_test.go") {
			continue
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			switch value := node.(type) {
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
						if literal, ok := field.Value.(*ast.BasicLit); ok && literal.Kind == token.STRING && literal.Value == `""` {
							t.Errorf("%s: seam.Cmd Dir is explicitly empty", source.path)
						}
					}
				}
				if !foundDir {
					t.Errorf("%s: seam.Cmd does not explicitly supply Dir", source.path)
				}
			case *ast.CallExpr:
				selector, ok := value.Fun.(*ast.SelectorExpr)
				if !ok || len(value.Args) == 0 {
					return true
				}
				ident, ok := selector.X.(*ast.Ident)
				if !ok || source.importPath[ident.Name] != "os" || !filesystemCalls[selector.Sel.Name] {
					return true
				}
				literal, ok := value.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				path, err := strconv.Unquote(literal.Value)
				if err == nil && path != "" && !filepath.IsAbs(path) {
					t.Errorf("%s: os.%s uses relative literal path %q instead of a dependency root", source.path, selector.Sel.Name, path)
				}
			}
			return true
		})
	}
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
func TestSharedHelpersDoNotDependOnCommands(t *testing.T) {
	commands := map[string]bool{
		modulePath + "/internal/account":     true,
		modulePath + "/internal/build":       true,
		modulePath + "/internal/deploy":      true,
		modulePath + "/internal/host":        true,
		modulePath + "/internal/keyring":     true,
		modulePath + "/internal/remove":      true,
		modulePath + "/internal/restore":     true,
		modulePath + "/internal/secrets":     true,
		modulePath + "/internal/spaceapps":   true,
		modulePath + "/internal/spacecreate": true,
		modulePath + "/internal/spaceinit":   true,
	}
	wantHelpers := map[string]bool{
		modulePath + "/internal/appref":    true,
		modulePath + "/internal/hostsetup": true,
	}
	seen := make(map[string]bool)
	for _, source := range moduleSources(t) {
		if !wantHelpers[source.pkgPath] {
			continue
		}
		seen[source.pkgPath] = true
		for _, imported := range source.importPath {
			if imported == modulePath+"/internal/cli" || commands[imported] {
				t.Errorf("%s: shared helper imports command package %q", source.path, imported)
			}
		}
	}
	for helper := range wantHelpers {
		if !seen[helper] {
			t.Errorf("shared helper package %q does not exist", helper)
		}
	}
}
