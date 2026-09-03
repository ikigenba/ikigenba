package toolkit

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

type toolConstructor struct {
	name      string
	construct func(string) (agentkit.Tool, error)
}

func allToolConstructors() []toolConstructor {
	return []toolConstructor{
		{name: "Bash", construct: Bash},
		{name: "Read", construct: Read},
		{name: "Write", construct: Write},
		{name: "Edit", construct: Edit},
		{name: "Glob", construct: func(root string) (agentkit.Tool, error) { return Glob(root) }},
		{name: "Grep", construct: func(root string) (agentkit.Tool, error) { return Grep(root) }},
	}
}

func TestConstructorsValidateRoot(t *testing.T) {
	// R-CF25-6WJ3: every constructor rejects empty, missing, and non-directory roots.
	temp := t.TempDir()
	missing := filepath.Join(temp, "missing")
	file, err := os.CreateTemp(temp, "root-file-")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, constructor := range allToolConstructors() {
		for _, root := range []string{"", missing, file.Name()} {
			t.Run(constructor.name+"/"+fmt.Sprintf("%q", root), func(t *testing.T) {
				tool, err := constructor.construct(root)
				if tool != nil || err == nil {
					t.Errorf("constructor(%q) = (%v, %v), want (nil, error)", root, tool, err)
				}
				if err != nil && (!strings.Contains(err.Error(), "root") || !strings.Contains(err.Error(), fmt.Sprintf("%q", root))) {
					t.Errorf("constructor(%q) error = %q, want root and supplied value", root, err)
				}
			})
		}
	}
}

func TestRelativeRootIsResolvedAtConstruction(t *testing.T) {
	// R-CGA1-KO9S: a later process chdir does not move a tool's relative root.
	base := t.TempDir()
	root := filepath.Join(base, "root")
	other := filepath.Join(base, "other")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "message.txt"), []byte("fixed root"), 0o600); err != nil {
		t.Fatal(err)
	}

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	tool, err := Read("root")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"file_path":"message.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "     1\tfixed root"; got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}

func TestFileToolsConfinePaths(t *testing.T) {
	// R-CHHX-YG0H: file_path cannot traverse an in-root symlink to an outside directory.
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaping")); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join("escaping", "secret.txt")

	tests := []struct {
		name  string
		make  func(string) (agentkit.Tool, error)
		input string
	}{
		{name: "Read", make: Read, input: fmt.Sprintf(`{"file_path":%q}`, filePath)},
		{name: "Write", make: Write, input: fmt.Sprintf(`{"file_path":%q,"content":"changed"}`, filePath)},
		{name: "Edit", make: Edit, input: fmt.Sprintf(`{"file_path":%q,"old_string":"secret","new_string":"changed"}`, filePath)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool, err := test.make(root)
			if err != nil {
				t.Fatal(err)
			}
			text, err := tool.Call(context.Background(), json.RawMessage(test.input))
			if err == nil || !strings.Contains(err.Error(), "file_path") || !strings.Contains(err.Error(), filePath) {
				t.Errorf("Call() = (%q, %v), want error naming file_path %q", text, err, filePath)
			}
		})
	}
}

func TestToolOutputCap(t *testing.T) {
	// R-EUUW-QDX3: successful output is capped by Unicode character count with the exact marker.
	tool, err := Bash(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	longInput := strings.Repeat("é", 30001)
	command := fmt.Sprintf("printf %s", longInput)
	got, err := tool.Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"command":%q}`, command)))
	if err != nil {
		t.Fatal(err)
	}
	want := string([]rune(longInput)[:30000]) +
		"\n[output truncated: showing 30000 of 30001 characters]"
	if got != want {
		t.Errorf("long Call() rune count = %d, want exact capped output", len([]rune(got)))
	}

	short, err := tool.Call(context.Background(), json.RawMessage(`{"command":"printf short"}`))
	if err != nil {
		t.Fatal(err)
	}
	if short != "short" {
		t.Errorf("short Call() = %q, want unchanged output", short)
	}
}

func TestToolCallResultInvariant(t *testing.T) {
	// R-CL5N-3R8K: calls return either successful text with nil error or empty text with an error.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "read.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "edit.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		make    func(string) (agentkit.Tool, error)
		success string
		failure string
	}{
		{name: "Bash", make: Bash, success: `{"command":"printf ok"}`, failure: `{`},
		{name: "Read", make: Read, success: `{"file_path":"read.txt"}`, failure: `{"file_path":"missing.txt"}`},
		{name: "Write", make: Write, success: `{"file_path":"write.txt","content":"ok"}`, failure: `{"file_path":".","content":"no"}`},
		{name: "Edit", make: Edit, success: `{"file_path":"edit.txt","old_string":"old","new_string":"new"}`, failure: `{"file_path":"missing.txt","old_string":"old","new_string":"new"}`},
		{name: "Glob", make: func(root string) (agentkit.Tool, error) { return Glob(root) }, success: `{"pattern":"*"}`, failure: `{"pattern":"["}`},
		{name: "Grep", make: func(root string) (agentkit.Tool, error) { return Grep(root) }, success: `{"pattern":"absent"}`, failure: `{"pattern":"["}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool, err := test.make(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tool.Call(context.Background(), json.RawMessage(test.success)); err != nil {
				t.Errorf("successful Call() error = %v", err)
			}
			text, err := tool.Call(context.Background(), json.RawMessage(test.failure))
			if err == nil || text != "" {
				t.Errorf("failed Call() = (%q, %v), want (empty, error)", text, err)
			}
		})
	}
}

func TestToolNamesAndSchemaPropertyNames(t *testing.T) {
	// R-CDU8-T4SE: exported tool names are exact and schema properties use the allowed casing.
	validName := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	grepFlags := map[string]bool{"-i": true, "-n": true, "-A": true, "-B": true, "-C": true}
	root := t.TempDir()
	for _, constructor := range allToolConstructors() {
		tool, err := constructor.construct(root)
		if err != nil {
			t.Fatalf("%s(): %v", constructor.name, err)
		}
		if got := tool.Name(); got != constructor.name {
			t.Errorf("%s Name() = %q", constructor.name, got)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			t.Fatalf("%s schema: %v", constructor.name, err)
		}
		for name := range schema.Properties {
			if !validName.MatchString(name) && (constructor.name != "Grep" || !grepFlags[name]) {
				t.Errorf("%s schema property %q has disallowed casing", constructor.name, name)
			}
		}
	}
}

func TestExportedIdentifierClosure(t *testing.T) {
	// R-CCMC-FD1P: non-test package files expose exactly the contracted identifier set.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var exported []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv == nil && ast.IsExported(declaration.Name.Name) {
					exported = append(exported, declaration.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(spec.Name.Name) {
							exported = append(exported, spec.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if ast.IsExported(name.Name) {
								exported = append(exported, name.Name)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(exported)
	want := []string{"Bash", "Edit", "Glob", "GlobOption", "Grep", "GrepOption", "Read", "SkipOption", "WithSkip", "Write"}
	if !reflect.DeepEqual(exported, want) {
		t.Errorf("exported identifiers = %v, want %v", exported, want)
	}
}
