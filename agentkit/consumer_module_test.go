package agentkit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// runConsumerModule writes a consumer module that requires agentkit through a
// replace directive pointing at this module and runs go test in it, reporting
// every compile error.
func runConsumerModule(t *testing.T, name, source string) (string, error) {
	t.Helper()
	moduleRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	goMod := "module " + name + "\n\ngo 1.26\n\nrequire github.com/ikigenba/ikigenba/agentkit v0.0.0\n\nreplace github.com/ikigenba/ikigenba/agentkit => " + filepath.ToSlash(moduleRoot) + "\n"
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "consumer_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-gcflags=-e", ".")
	command.Dir = temporary
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestRootPackageIsImportableFromConsumerModule(t *testing.T) {
	// R-AZQB-Y0PK
	source := `package consumer

import (
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func TestImport(t *testing.T) {
	if agentkit.AnthropicMessagesWire() == nil {
		t.Fatal("AnthropicMessagesWire() = nil")
	}
	if !errors.Is(agentkit.ErrInvalidConfig, agentkit.ErrInvalidConfig) {
		t.Fatal("ErrInvalidConfig is not comparable")
	}
}
`
	output, err := runConsumerModule(t, "importconsumer", source)
	if err != nil {
		t.Fatalf("consumer module importing github.com/ikigenba/ikigenba/agentkit failed: %v\n%s", err, output)
	}
}

func TestEventIsSealedUnionOfFourVariantsForConsumers(t *testing.T) {
	// R-LKYH-122K
	t.Run("consumer switches the four variants", func(t *testing.T) {
		source := `package consumer

import (
	"encoding/json"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func kind(event agentkit.Event) string {
	switch event := event.(type) {
	case agentkit.MessageDone:
		_ = event.Message
		return "message"
	case agentkit.ToolCall:
		_ = event.Use
		return "call"
	case agentkit.ToolReturn:
		_ = event.Result
		return "return"
	case agentkit.OutputDone:
		_ = event.Value
		return "output"
	}
	return "unknown"
}

func TestSwitch(t *testing.T) {
	events := []agentkit.Event{
		agentkit.MessageDone{Message: agentkit.Message{Role: agentkit.RoleAssistant}},
		agentkit.ToolCall{Use: agentkit.ToolUse{ID: "call"}},
		agentkit.ToolReturn{Result: agentkit.ToolResult{ToolUseID: "call"}},
		agentkit.OutputDone{Value: json.RawMessage("{}")},
	}
	want := []string{"message", "call", "return", "output"}
	for index, event := range events {
		if got := kind(event); got != want[index] {
			t.Fatalf("kind(%T) = %q, want %q", event, got, want[index])
		}
	}
}
`
		output, err := runConsumerModule(t, "eventconsumer", source)
		if err != nil {
			t.Fatalf("consumer switch over Event variants failed: %v\n%s", err, output)
		}
	})
	t.Run("consumer cannot add a variant", func(t *testing.T) {
		source := `package consumer

import "github.com/ikigenba/ikigenba/agentkit"

type outsider struct{}

var _ agentkit.Event = outsider{}
`
		output, err := runConsumerModule(t, "eventsealcheck", source)
		if err == nil {
			t.Fatalf("external Event implementation compiled successfully:\n%s", output)
		}
		if !strings.Contains(output, "does not implement agentkit.Event") {
			t.Fatalf("external Event implementation failed for the wrong reason: %v\n%s", err, output)
		}
	})
}

func TestConsumerCannotReferenceWithheldNames(t *testing.T) {
	// R-1OL8-V3X0
	// R-NZTR-FBVP
	// R-JES9-BTIL
	// R-IXWK-DB4R
	// R-2V52-QGL7
	// R-KBPJ-NMJC
	// R-KE3W-V60A
	groups := []struct {
		requirement string
		names       []string
	}{
		{"R-1OL8-V3X0", []string{"NewConversation", "NewForWire", "Provider", "KnownWire", "RequestState", "RequestMutator", "ErrorClassifier", "WithHeader", "WithFramer", "WithClassifier", "WithMutator", "WithHTTPClient"}},
		{"R-NZTR-FBVP", []string{"ProviderOptions"}},
		{"R-JES9-BTIL", []string{"Vendor", "ProviderID", "ResolveModel", "LookupModel", "CatalogFor"}},
		{"R-IXWK-DB4R", []string{"Credential", "APIKey", "OAuth", "TokenSource"}},
		{"R-2V52-QGL7", []string{"Warning"}},
		{"R-KBPJ-NMJC", []string{"AuthApplier"}},
		{"R-KE3W-V60A", []string{"OAuthClient"}},
	}
	var source strings.Builder
	source.WriteString("package consumer\n\nimport \"github.com/ikigenba/ikigenba/agentkit\"\n\n")
	for _, group := range groups {
		for _, name := range group.names {
			source.WriteString("var _ = agentkit." + name + "\n")
		}
	}
	output, err := runConsumerModule(t, "withheldconsumer", source.String())
	if err == nil {
		t.Fatalf("consumer referencing withheld names compiled successfully:\n%s", output)
	}
	for _, group := range groups {
		t.Run(group.requirement, func(t *testing.T) {
			for _, name := range group.names {
				if !strings.Contains(output, "undefined: agentkit."+name+"\n") {
					t.Errorf("consumer reference to agentkit.%s did not fail as undefined:\n%s", name, output)
				}
			}
		})
	}

	t.Run("R-NZTR-FBVP no raw JSON pass-through", func(t *testing.T) {
		rawJSON := []reflect.Type{reflect.TypeFor[json.RawMessage](), reflect.TypeFor[[]byte](), reflect.TypeFor[map[string]any](), reflect.TypeFor[any]()}
		configType := reflect.TypeFor[Config]()
		for index := range configType.NumField() {
			field := configType.Field(index)
			for _, raw := range rawJSON {
				if field.IsExported() && field.Type == raw {
					t.Errorf("Config.%s accepts raw %s", field.Name, raw)
				}
			}
		}
		if got, want := reflect.TypeOf(New), reflect.TypeOf(func(WireFormat, Endpoint, string, Config) (*Conversation, error) { return nil, nil }); got != want {
			t.Errorf("New = %s, want %s", got, want)
		}
		send, ok := reflect.TypeFor[*Conversation]().MethodByName("Send")
		if want := reflect.TypeOf(func(*Conversation, context.Context, ...Block) *Stream { return nil }); !ok || send.Type != want {
			t.Errorf("Send = %v, want %s", send.Type, want)
		}
	})
}
