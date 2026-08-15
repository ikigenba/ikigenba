package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"prompts/internal/mcpclient"
	"prompts/internal/suite"
	"reflect"
	"strings"
	"testing"
)

type fakeClient struct {
	instructions     string
	instructionsErr  error
	tools            []mcpclient.Tool
	listErr          error
	callResult       mcpclient.Result
	callErr          error
	instructionCalls int
	listCalls        int
	callCalls        int
	calledName       string
	calledArgs       json.RawMessage
}

func (f *fakeClient) Instructions(context.Context) (string, error) {
	f.instructionCalls++
	return f.instructions, f.instructionsErr
}

func (f *fakeClient) ListTools(context.Context) ([]mcpclient.Tool, error) {
	f.listCalls++
	return f.tools, f.listErr
}

func (f *fakeClient) CallTool(_ context.Context, name string, args json.RawMessage) (mcpclient.Result, error) {
	f.callCalls++
	f.calledName = name
	f.calledArgs = append(f.calledArgs[:0], args...)
	return f.callResult, f.callErr
}

func testTools(peers []suite.Peer, clients map[string]*fakeClient) map[string]toolCaller {
	created := Tools(peers, func(context.Context) []suite.CatalogEntry {
		entries := make([]suite.CatalogEntry, len(peers))
		for i, peer := range peers {
			entries[i] = suite.CatalogEntry{Name: peer.Name}
		}
		return entries
	}, func(peer suite.Peer) Client {
		return clients[peer.Name]
	})
	byName := make(map[string]toolCaller, len(created))
	for _, tool := range created {
		byName[tool.Name()] = tool
	}
	return byName
}

type toolCaller interface {
	Call(context.Context, json.RawMessage) (string, error)
}

func TestToolsExposeFixedGatewayAndOrderedCatalog(t *testing.T) {
	peers := []suite.Peer{{Name: "crm"}, {Name: "wiki"}}
	created := Tools(peers, func(context.Context) []suite.CatalogEntry {
		return []suite.CatalogEntry{{Name: "crm", Summary: "Manage contacts."}, {Name: "wiki", Summary: "Keep notes."}}
	}, func(peer suite.Peer) Client {
		return &fakeClient{}
	})
	if len(created) != 3 {
		t.Fatalf("gateway tool count = %d, want 3", len(created))
	}
	for i, want := range []string{servicesToolName, toolsToolName, callToolName} {
		if created[i].Name() != want {
			t.Errorf("tool %d name = %q, want %q", i, created[i].Name(), want)
		}
	}
	output, err := created[0].Call(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("suite_services: %v", err)
	}
	var entries []suite.CatalogEntry
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if strings.Contains(output, `"Name"`) || strings.Contains(output, `"Summary"`) {
		t.Fatalf("catalog uses Go field names instead of JSON names: %s", output)
	}
	want := []suite.CatalogEntry{{Name: "crm", Summary: "Manage contacts."}, {Name: "wiki", Summary: "Keep notes."}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("catalog = %#v, want %#v", entries, want)
	}
}

func TestSuiteToolsRelaysPublishedInstructionsAndSchemas(t *testing.T) {
	// R-OWUN-9LVP
	schema := json.RawMessage("{\n  \"type\": \"object\", \"properties\": {\"id\": {\"type\": \"string\"}}\n}")
	crm := &fakeClient{
		instructions: "Use CRM identifiers exactly.",
		tools:        []mcpclient.Tool{{Name: "save", Description: "Save a record.", InputSchema: schema}},
	}
	clients := map[string]*fakeClient{"crm": crm, "wiki": {}}
	tools := testTools([]suite.Peer{{Name: "crm"}, {Name: "wiki"}}, clients)

	output, err := tools[toolsToolName].Call(context.Background(), json.RawMessage(`{"service":"crm"}`))
	if err != nil {
		t.Fatalf("suite_tools known service: %v (output %q)", err, output)
	}
	var relayed struct {
		Instructions string `json:"instructions"`
		Tools        []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(output), &relayed); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if relayed.Instructions != crm.instructions || len(relayed.Tools) != 1 || relayed.Tools[0].Name != "save" || relayed.Tools[0].Description != "Save a record." {
		t.Fatalf("relayed output = %#v", relayed)
	}
	if !reflect.DeepEqual(relayed.Tools[0].InputSchema, schema) {
		t.Fatalf("schema bytes = %q, want byte-identical %q", relayed.Tools[0].InputSchema, schema)
	}

	output, err = tools[toolsToolName].Call(context.Background(), json.RawMessage(`{"service":"missing"}`))
	if err == nil {
		t.Fatalf("unknown service returned nil error: %q", output)
	}
	for _, want := range []string{"missing", "crm", "wiki"} {
		if !strings.Contains(output, want) {
			t.Errorf("unknown-service output %q does not contain %q", output, want)
		}
	}
	if crm.instructionCalls != 1 || crm.listCalls != 1 || crm.callCalls != 0 || clients["wiki"].instructionCalls != 0 || clients["wiki"].listCalls != 0 {
		t.Fatalf("unexpected dispatch: crm=%#v wiki=%#v", crm, clients["wiki"])
	}
}

func TestSuiteCallRejectsInvalidOrNonObjectArgsWithoutDispatch(t *testing.T) {
	// R-OY2J-NDME
	crm := &fakeClient{}
	tool := testTools([]suite.Peer{{Name: "crm"}}, map[string]*fakeClient{"crm": crm})[callToolName]
	for _, test := range []struct {
		name string
		args string
		want string
	}{
		{name: "invalid JSON", args: `{"id":`, want: "not valid JSON"},
		{name: "JSON array", args: `[1,2]`, want: "JSON object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, err := json.Marshal(suiteCallInput{Service: "crm", Tool: "save", Args: test.args})
			if err != nil {
				t.Fatal(err)
			}
			output, err := tool.Call(context.Background(), input)
			if err == nil {
				t.Fatalf("Call returned nil error: %q", output)
			}
			if !strings.Contains(output, test.want) || !strings.Contains(output, toolsToolName) {
				t.Fatalf("error output = %q, want defect %q and %q", output, test.want, toolsToolName)
			}
		})
	}
	if crm.callCalls != 0 {
		t.Fatalf("tools/call requests = %d, want zero", crm.callCalls)
	}
}

func TestSuiteCallDispatchesBareNameAndParsedObject(t *testing.T) {
	// R-OZAG-15D3
	crm := &fakeClient{callResult: mcpclient.Result{Text: `{"saved":"X"}`}}
	tool := testTools([]suite.Peer{{Name: "crm"}}, map[string]*fakeClient{"crm": crm})[callToolName]
	input, err := json.Marshal(suiteCallInput{Service: "crm", Tool: "save", Args: ` { "id" : "X", "count": 2 } `})
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("Call: %v (output %q)", err, output)
	}
	if output != crm.callResult.Text {
		t.Fatalf("output = %q, want %q", output, crm.callResult.Text)
	}
	if crm.callCalls != 1 || crm.calledName != "save" {
		t.Fatalf("dispatch count/name = %d/%q, want 1/save", crm.callCalls, crm.calledName)
	}
	var got, want map[string]any
	if err := json.Unmarshal(crm.calledArgs, &got); err != nil {
		t.Fatalf("called args are not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"id":"X","count":2}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("called args = %#v, want %#v", got, want)
	}
}

func TestSuiteCallFramesPeerToolError(t *testing.T) {
	// R-P0IC-EX3S
	crm := &fakeClient{callResult: mcpclient.Result{Text: "record X does not exist", IsError: true}}
	tool := testTools([]suite.Peer{{Name: "crm"}}, map[string]*fakeClient{"crm": crm})[callToolName]
	input, err := json.Marshal(suiteCallInput{Service: "crm", Tool: "save", Args: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.Call(context.Background(), input)
	if err == nil {
		t.Fatalf("peer isError returned nil error: %q", output)
	}
	for _, want := range []string{"crm", "save", `suite_tools("crm")`, "record X does not exist"} {
		if !strings.Contains(output, want) {
			t.Errorf("error output %q does not contain %q", output, want)
		}
	}
	if crm.callCalls != 1 {
		t.Fatalf("tools/call requests = %d, want one", crm.callCalls)
	}
}

func TestUnreachablePeerIsContainedAsGatewayError(t *testing.T) {
	// R-P1Q8-SOUH
	refused := errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	crm := &fakeClient{instructionsErr: refused, callErr: refused}
	tools := testTools([]suite.Peer{{Name: "crm"}}, map[string]*fakeClient{"crm": crm})

	toolsOutput, toolsErr := tools[toolsToolName].Call(context.Background(), json.RawMessage(`{"service":"crm"}`))
	if toolsErr == nil || !strings.Contains(toolsOutput, "crm") || !strings.Contains(toolsOutput, "connection refused") {
		t.Fatalf("suite_tools unreachable = %q, %v", toolsOutput, toolsErr)
	}
	callInput, err := json.Marshal(suiteCallInput{Service: "crm", Tool: "save", Args: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	callOutput, callErr := tools[callToolName].Call(context.Background(), callInput)
	if callErr == nil || !strings.Contains(callOutput, "crm") || !strings.Contains(callOutput, "save") || !strings.Contains(callOutput, toolsToolName) || !strings.Contains(callOutput, "connection refused") {
		t.Fatalf("suite_call unreachable = %q, %v", callOutput, callErr)
	}
}
