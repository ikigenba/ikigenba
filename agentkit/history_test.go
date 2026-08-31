package agentkit

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestHistoryJSONRoundTripPreservesAllVariants(t *testing.T) {
	// R-21VH-JYSJ
	original := History{
		{Role: RoleSystem, Blocks: []Block{Text{Text: "", Provider: json.RawMessage(`{"text_signature":"s"}`)}}},
		{Role: RoleAssistant, Blocks: []Block{
			Reasoning{Text: "think", Redacted: true, Provider: json.RawMessage(`{"encrypted":"cipher"}`)},
			ToolUse{ID: "call-1", Name: "lookup", Input: json.RawMessage(`{"q":"value"}`), Provider: json.RawMessage(`{"call_signature":7}`)},
		}},
		{Role: RoleTool, Blocks: []Block{ToolResult{ToolUseID: "call-1", Content: "failed", IsError: true, Provider: json.RawMessage(`{"result_signature":false}`)}}},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var shape []struct {
		Blocks []map[string]json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	if text, present := shape[0].Blocks[0]["text"]; !present || string(text) != `""` {
		t.Fatalf("empty Text.Text must remain present in stable JSON: %s", encoded)
	}
	var decoded History
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round trip changed history:\n got: %#v\nwant: %#v\nJSON: %s", decoded, original, encoded)
	}
	wantTypes := []reflect.Type{reflect.TypeFor[Text](), reflect.TypeFor[Reasoning](), reflect.TypeFor[ToolUse](), reflect.TypeFor[ToolResult]()}
	gotTypes := []reflect.Type{reflect.TypeOf(decoded[0].Blocks[0]), reflect.TypeOf(decoded[1].Blocks[0]), reflect.TypeOf(decoded[1].Blocks[1]), reflect.TypeOf(decoded[2].Blocks[0])}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("concrete block types = %v, want %v", gotTypes, wantTypes)
	}
}

func TestHistoryJSONRejectsUnknownBlockType(t *testing.T) {
	var history History
	err := json.Unmarshal([]byte(`[{"role":2,"blocks":[{"type":"future"}]}]`), &history)
	if err == nil || err.Error() != `agentkit: history message 0 block 0: unknown block type "future"` {
		t.Fatalf("Unmarshal error = %v, want unknown discriminator rejection", err)
	}
}
