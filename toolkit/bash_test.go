package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBashConstructor(t *testing.T) {
	tool, err := Bash(t.TempDir())
	// R-C2V5-D745: Bash constructs an exported tool for a valid root.
	if err != nil {
		t.Fatalf("Bash() error = %v", err)
	}
	if tool == nil {
		t.Fatal("Bash() returned a nil tool")
	}
}

func TestBashSchema(t *testing.T) {
	tool, err := Bash(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want an object", schema["properties"])
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required = %#v, want an array", schema["required"])
	}

	// R-E1LB-JW4F: command is the sole required property and timeout has
	// the declared millisecond range.
	if got, want := sortedKeys(properties), []string{"command", "timeout"}; !reflect.DeepEqual(got, want) {
		t.Errorf("property names = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(required, []any{"command"}) {
		t.Errorf("required = %#v, want only command", required)
	}
	command := properties["command"].(map[string]any)
	if got := command["type"]; got != "string" {
		t.Errorf("command type = %#v, want string", got)
	}
	if got := command["minLength"]; got != float64(1) {
		t.Errorf("command minLength = %#v, want 1", got)
	}
	timeout := properties["timeout"].(map[string]any)
	if got := timeout["type"]; got != "integer" {
		t.Errorf("timeout type = %#v, want integer", got)
	}
	if got := timeout["minimum"]; got != float64(1) {
		t.Errorf("timeout minimum = %#v, want 1", got)
	}
	if got := timeout["maximum"]; got != float64(600000) {
		t.Errorf("timeout maximum = %#v, want 600000", got)
	}
}

func TestBashRequiresExecutableOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tool, err := Bash(t.TempDir())
	// R-E2T7-XNV4: a missing bash executable is a construction-time error.
	if err == nil {
		t.Fatal("Bash() error = nil, want an error")
	}
	if tool != nil {
		t.Errorf("Bash() tool = %#v, want nil", tool)
	}
}

func TestBashStartsEveryCallInRootWithProcessEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "root-marker"), []byte("from-root"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOOLKIT_BASH_TEST_VALUE", "from-environment")
	tool, err := Bash(root)
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"command":"cat root-marker; printf ':%s' \"$TOOLKIT_BASH_TEST_VALUE\"; cd /"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "from-root:from-environment"; got != want {
		t.Errorf("first Call() = %q, want %q", got, want)
	}

	got, err = tool.Call(context.Background(), json.RawMessage(`{"command":"cat root-marker"}`))
	if err != nil {
		t.Fatal(err)
	}
	// R-E414-BFLT: commands inherit the process environment and each fresh
	// shell starts in root even after an earlier shell changes directory.
	if want := "from-root"; got != want {
		t.Errorf("second Call() = %q, want %q", got, want)
	}
}

func TestBashInterleavesStandardOutputAndError(t *testing.T) {
	tool, err := Bash(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"command":"echo out1; echo err1 >&2; echo out2"}`))
	if err != nil {
		t.Fatal(err)
	}
	// R-E590-P7CI: stdout and stderr are captured through one shared writer
	// and retain the order in which the shell writes them.
	if want := "out1\nerr1\nout2\n"; got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}

func TestBashReturnsSuccessfulOutputUnchanged(t *testing.T) {
	tool, err := Bash(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"command":"printf 'known output'"}`))
	// R-E6GX-2Z37: status zero returns the exact captured output and no error.
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if want := "known output"; got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}

func TestBashReturnsNonzeroExitAsResult(t *testing.T) {
	tool, err := Bash(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := tool.Call(context.Background(), json.RawMessage(`{"command":"printf failure; exit 3"}`))
	// R-E7OT-GQTW: a nonzero status appends its exit-code marker to output
	// and remains a normal, nil-error result.
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if want := strings.Join([]string{"failure", "[exit code 3]"}, "\n"); got != want {
		t.Errorf("Call() = %q, want %q", got, want)
	}
}
