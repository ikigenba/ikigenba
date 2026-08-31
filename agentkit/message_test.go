package agentkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBlockVariantsAndDiscriminators(t *testing.T) {
	// R-20NL-671U
	variants := []struct {
		block Block
		want  string
	}{
		{block: Text{}, want: "text"},
		{block: Reasoning{}, want: "reasoning"},
		{block: ToolUse{}, want: "tool_use"},
		{block: ToolResult{}, want: "tool_result"},
	}
	for _, variant := range variants {
		if got := variant.block.BlockType(); got != variant.want {
			t.Errorf("%T.BlockType() = %q, want %q", variant.block, got, variant.want)
		}
	}
}

func TestBlockIsSealedOutsidePackage(t *testing.T) {
	// R-1Y7S-ENKG
	moduleRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	goMod := "module sealcheck\n\ngo 1.26\n\nrequire github.com/ikigenba/ikigenba/agentkit v0.0.0\nreplace github.com/ikigenba/ikigenba/agentkit => " + filepath.ToSlash(moduleRoot) + "\n"
	source := `package sealcheck
import "github.com/ikigenba/ikigenba/agentkit"
type outside struct{}
func (outside) BlockType() string { return "outside" }
func (outside) isBlock() {}
var _ agentkit.Block = outside{}
`
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "seal_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = temporary
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("outside type unexpectedly implemented Block:\n%s", output)
	}
	if !strings.Contains(string(output), "unexported method isBlock") {
		t.Fatalf("compile failure did not prove the sealed marker:\n%s", output)
	}
}

func TestEveryVariantHasBareProviderPayload(t *testing.T) {
	// R-1ZFO-SFB5
	for _, value := range []any{Text{}, Reasoning{}, ToolUse{}, ToolResult{}} {
		typeOf := reflect.TypeOf(value)
		field, ok := typeOf.FieldByName("Provider")
		if !ok || field.Type != reflect.TypeFor[json.RawMessage]() {
			t.Errorf("%s Provider field = %#v, want json.RawMessage", typeOf, field)
		}
		for _, forbidden := range []string{"Endpoint", "Kind"} {
			if _, exists := typeOf.FieldByName(forbidden); exists {
				t.Errorf("%s unexpectedly has payload metadata field %s", typeOf, forbidden)
			}
		}
	}
}

func TestReasoningKindIsIndependentOfProviderPayload(t *testing.T) {
	// R-233D-XQJ8
	blocks := []Block{
		Text{Text: "visible", Provider: json.RawMessage(`{"signature":true}`)},
		Reasoning{Text: "thought", Provider: nil},
	}
	if _, ok := blocks[0].(Text); !ok || blocks[0].BlockType() != "text" {
		t.Fatalf("text with provider payload changed kind: %T", blocks[0])
	}
	if reasoning, ok := blocks[1].(Reasoning); !ok || reasoning.Provider != nil || blocks[1].BlockType() != "reasoning" {
		t.Fatalf("reasoning without provider payload changed kind: %#v", blocks[1])
	}
}

func TestToolResultUsesVendorCallIDVerbatim(t *testing.T) {
	// R-24BA-BI9X
	vendorID := " vendor/call:id beta "
	use := ToolUse{ID: vendorID, Name: "lookup"}
	result := ToolResult{ToolUseID: use.ID, Content: "done"}
	if result.ToolUseID != vendorID {
		t.Fatalf("ToolUseID = %q, want vendor ID verbatim %q", result.ToolUseID, vendorID)
	}
}
