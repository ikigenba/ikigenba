package agentkit

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoWarningOrCategorySpecificErrorTypesAreExported(t *testing.T) {
	// R-2K5Z-AIWY
	// R-2V52-QGL7
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	exportedErrorTypes := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				name := typeSpecification.Name.Name
				if name == "Warning" {
					t.Fatal("exported Warning type exists; invalid configuration must fail loudly")
				}
				if ast.IsExported(name) && strings.HasSuffix(name, "Error") {
					exportedErrorTypes = append(exportedErrorTypes, name)
				}
			}
		}
	}
	if len(exportedErrorTypes) != 1 || exportedErrorTypes[0] != "Error" {
		t.Fatalf("exported error types = %v, want only Error", exportedErrorTypes)
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func TestDependencyDirection(t *testing.T) {
	// R-1WZW-0VTR
	command := exec.Command("go", "list", "-json", "./...")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	const module = "github.com/ikigenba/ikigenba/agentkit"
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		if pkg.ImportPath == module {
			for _, imported := range pkg.Imports {
				if strings.HasPrefix(imported, module+"/") {
					t.Fatalf("root Conversation package imports lower layer %q", imported)
				}
			}
			continue
		}
		if !strings.HasPrefix(pkg.ImportPath, module+"/") {
			continue
		}
		vendorConstructorPackage := pkg.ImportPath == module+"/anthropic" || pkg.ImportPath == module+"/openai"
		for _, imported := range pkg.Imports {
			if imported == module && !vendorConstructorPackage {
				t.Fatalf("lower package %q imports back up to Conversation", pkg.ImportPath)
			}
			if imported != pkg.ImportPath && (strings.Contains(imported, "/anthropic") || strings.Contains(imported, "/openai") || strings.Contains(imported, "/gemini")) {
				t.Fatalf("lower package %q imports vendor package %q", pkg.ImportPath, imported)
			}
		}
	}
}

func TestCredentialWorldsRemainVendorLocal(t *testing.T) {
	// R-3IB6-03OE
	// R-3JJ2-DVF3
	// R-3OEN-WYDV
	fileSet := token.NewFileSet()
	rootEntries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range rootEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				name := specification.(*ast.TypeSpec).Name.Name
				if name == "Credential" || name == "TokenSource" {
					t.Fatalf("root package declares forbidden shared %s", name)
				}
			}
		}
	}

	// #nosec G101 -- these are Go method identifiers, not credential values.
	wantMarkers := map[string]string{
		"anthropic/credential.go": "isAnthropicCredential",
		"openai/credential.go":    "isOpenAICredential",
	}
	tokenResults := make(map[string]int)
	for name, marker := range wantMarkers {
		parsed, parseErr := parser.ParseFile(fileSet, name, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		foundMarker := false
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				interfaceType, ok := typeSpecification.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				if typeSpecification.Name.Name == "Credential" {
					for _, method := range interfaceType.Methods.List {
						if len(method.Names) == 1 && method.Names[0].Name == marker && !ast.IsExported(marker) {
							foundMarker = true
						}
					}
				}
				if typeSpecification.Name.Name == "TokenSource" {
					method := interfaceType.Methods.List[0].Type.(*ast.FuncType)
					for _, result := range method.Results.List {
						arity := len(result.Names)
						if arity == 0 {
							arity = 1
						}
						tokenResults[name] += arity
					}
				}
			}
		}
		if !foundMarker {
			t.Fatalf("%s Credential lacks exact private marker %s", name, marker)
		}
	}
	if tokenResults["anthropic/credential.go"] == tokenResults["openai/credential.go"] {
		t.Fatalf("vendor TokenSource result shapes were unified: %v", tokenResults)
	}
}

func TestAuthenticationHasOneRuntimeInterface(t *testing.T) {
	// R-3KQY-RN5S
	fileSet := token.NewFileSet()
	authInterfaces := make(map[string][]string)
	productionFiles := []string{"endpoint.go", "anthropic/credential.go", "openai/credential.go"}
	for _, name := range productionFiles {
		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				interfaceType, ok := typeSpecification.Type.(*ast.InterfaceType)
				if !ok || !strings.Contains(typeSpecification.Name.Name, "Auth") {
					continue
				}
				methods := make([]string, 0, len(interfaceType.Methods.List))
				for _, method := range interfaceType.Methods.List {
					if len(method.Names) == 1 {
						methods = append(methods, method.Names[0].Name)
					}
				}
				authInterfaces[typeSpecification.Name.Name] = methods
			}
		}
	}
	if len(authInterfaces) != 1 || len(authInterfaces["AuthApplier"]) != 1 || authInterfaces["AuthApplier"][0] != "Apply" {
		t.Fatalf("authentication runtime interfaces = %v, want only AuthApplier.Apply", authInterfaces)
	}
}

func TestWireArchitectureStaysBelowTransportSeam(t *testing.T) {
	// R-2WCZ-48BW
	// R-2XKV-I02L
	// R-32GH-131D
	fileSet := token.NewFileSet()
	for _, name := range []string{"wire_anthropic.go", "wire_openai_responses.go", "wire_openai_chat.go", "wire_gemini.go"} {
		// #nosec G304 -- names are fixed test fixtures, not external input.
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"http://", "https://", "Authorization", "http.Client", "http.Request"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s owns endpoint transport concern %q", name, forbidden)
			}
		}
		parsed, err := parser.ParseFile(fileSet, name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					typeSpecification, ok := specification.(*ast.TypeSpec)
					if ok && (strings.Contains(typeSpecification.Name.Name, "Wire") || strings.Contains(typeSpecification.Name.Name, "wire")) && ast.IsExported(typeSpecification.Name.Name) {
						t.Fatalf("concrete codec %q is exported", typeSpecification.Name.Name)
					}
				}
			case *ast.FuncDecl:
				if declaration.Recv == nil && strings.HasPrefix(declaration.Name.Name, "New") && strings.Contains(declaration.Name.Name, "Wire") {
					t.Fatalf("wire selection helper %q is exported", declaration.Name.Name)
				}
			}
		}
	}

	wireSource, err := os.ReadFile("wire.go")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(fileSet, "wire.go", wireSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range general.Specs {
			typeSpecification, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpecification.Name.Name != "WireFormat" {
				continue
			}
			wireInterface := typeSpecification.Type.(*ast.InterfaceType)
			if len(wireInterface.Methods.List) != 5 {
				t.Fatalf("WireFormat has %d methods, want exact five-method seam", len(wireInterface.Methods.List))
			}
		}
	}

	conversationSource, err := os.ReadFile("conversation.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(conversationSource), "WireFormat") || strings.Contains(string(conversationSource), "SetWire") {
		t.Fatal("Conversation exposes assignable wire selection")
	}
}

func TestEndpointAndProviderArchitecture(t *testing.T) {
	// R-37C2-K605
	// R-38JY-XXQU
	// R-3ENG-USGB
	// R-3H39-MBXP
	fileSet := token.NewFileSet()
	source, err := os.ReadFile("endpoint.go")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(fileSet, "endpoint.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	publicFunctions := map[string]bool{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && ast.IsExported(function.Name.Name) {
			publicFunctions[function.Name.Name] = true
		}
	}
	wantFunctions := []string{"WithBaseURL", "WithHeader", "WithFramer", "WithClassifier", "WithMutator", "WithReplayEncoding"}
	if len(publicFunctions) != len(wantFunctions) {
		t.Fatalf("endpoint public functions = %v, want exact option vocabulary", publicFunctions)
	}
	for _, name := range wantFunctions {
		if !publicFunctions[name] {
			t.Fatalf("missing endpoint option %s", name)
		}
	}
	if strings.Contains(string(source), "PathTemplate") || strings.Contains(string(source), "WithPath") {
		t.Fatal("endpoint introduced a separate path-template concept")
	}

	providerSource, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatal(err)
	}
	providerFile, err := parser.ParseFile(fileSet, "provider.go", providerSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range providerFile.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range general.Specs {
			typeSpecification, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpecification.Name.Name != "Provider" {
				continue
			}
			providerInterface := typeSpecification.Type.(*ast.InterfaceType)
			if len(providerInterface.Methods.List) != 4 {
				t.Fatalf("Provider has %d methods, want four", len(providerInterface.Methods.List))
			}
		}
	}
}
