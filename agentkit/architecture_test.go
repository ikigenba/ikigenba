package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"
)

var (
	_ Event = MessageDone{}
	_ Event = ToolCall{}
	_ Event = ToolReturn{}
	_ Event = OutputDone{}
)

// assertLiveBuildConstraint is the single encoding of the rule that a live
// fixture begins with the live build constraint.
func assertLiveBuildConstraint(t *testing.T, label, text string) {
	t.Helper()
	if !strings.HasPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), "//go:build live") {
		t.Fatalf("%s does not begin with the live build constraint", label)
	}
}

// assertNeverSkips is the single encoding of the rule that a live fixture
// fails, never skips, on a missing credential.
func assertNeverSkips(t *testing.T, label, text string) {
	t.Helper()
	if strings.Contains(text, "t.Skip") {
		t.Fatalf("%s must fail, never skip, on a missing credential", label)
	}
}

// R-L023-R1CS
func TestLiveOAuthRefreshFixturesExistWithLiveTag(t *testing.T) {
	fixtures := []struct {
		name                string
		environmentVariable string
		model               string
		host                string
	}{
		{"oauth_refresh_openai_live_test.go", "AGENTKIT_OPENAI_OAUTH_FILE", "gpt-5.4-mini", "HostOpenAI"},
		{"oauth_refresh_xai_live_test.go", "AGENTKIT_XAI_OAUTH_FILE", "grok-4.3", "HostXAI"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			contents, err := os.ReadFile(fixture.name)
			if err != nil {
				t.Fatalf("read live OAuth fixture: %v", err)
			}
			text := string(contents)
			assertLiveBuildConstraint(t, "live OAuth fixture", text)
			assertNeverSkips(t, "live OAuth fixture", text)
			for _, fragment := range []string{
				"func Test", fixture.environmentVariable, "OAuthRotator", "FileTokenStore",
				".Rotate(", fixture.model, fixture.host, "WireResponses", "access_token",
			} {
				if !strings.Contains(text, fragment) {
					t.Fatalf("live OAuth fixture does not contain %q", fragment)
				}
			}
		})
	}
}

// R-DEM1-KXMP
func TestSerialToolCallsLiveFixtureExists(t *testing.T) {
	contents, err := os.ReadFile("serial_tool_calls_live_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	assertLiveBuildConstraint(t, "serial tool calls live fixture", text)
	assertNeverSkips(t, "serial tool calls live fixture", text)
	for _, fragment := range []string{"func TestLiveSerialToolCalls", "liveMatrixCells", "OfferingGeminiGenerateContent", "SerialToolCalls: true", "ToolUse", "stream.Err()"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("serial tool calls live fixture does not contain %q", fragment)
		}
	}
}

// R-ED2W-P9IU
func TestLiveOAuthReissueFixturesExistWithLiveTag(t *testing.T) {
	fixtures := []struct {
		name                string
		environmentVariable string
		model               string
		host                string
	}{
		{"oauth_reissue_openai_live_test.go", "AGENTKIT_OPENAI_OAUTH_FILE", "gpt-5.4-mini", "HostOpenAI"},
		{"oauth_reissue_xai_live_test.go", "AGENTKIT_XAI_OAUTH_FILE", "grok-4.3", "HostXAI"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			contents, err := os.ReadFile(fixture.name)
			if err != nil {
				t.Fatalf("read live OAuth reissue fixture: %v", err)
			}
			text := string(contents)
			assertLiveBuildConstraint(t, "live OAuth reissue fixture", text)
			assertNeverSkips(t, "live OAuth reissue fixture", text)
			for _, fragment := range []string{
				"func TestLive", fixture.environmentVariable, "OAuthRotator", "NewEndpoint",
				".Send(", fixture.model, fixture.host, "WireResponses", "access_token",
				"MessageDone", "stream.Err()",
			} {
				if !strings.Contains(text, fragment) {
					t.Fatalf("live OAuth reissue fixture does not contain %q", fragment)
				}
			}
			if strings.Contains(text, "WithBaseURL") {
				t.Fatal("live OAuth reissue fixture must use the vendor default base URL")
			}

			parsed, err := parser.ParseFile(token.NewFileSet(), fixture.name, contents, 0)
			if err != nil {
				t.Fatalf("parse live OAuth reissue fixture: %v", err)
			}
			testCount := 0
			for _, declaration := range parsed.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "TestLive") {
					testCount++
				}
			}
			if testCount != 1 {
				t.Fatalf("live OAuth reissue fixture declares %d TestLive functions, want 1", testCount)
			}
		})
	}
}

// R-1ETZ-7OQ0
func TestLiveCacheFixtureExistsWithLiveTagAndSubtests(t *testing.T) {
	contents, err := os.ReadFile("cache_live_test.go")
	if err != nil {
		t.Fatalf("read live cache fixture: %v", err)
	}
	text := string(contents)
	assertLiveBuildConstraint(t, "live cache fixture", text)
	assertNeverSkips(t, "live cache fixture", text)
	for _, fragment := range []string{
		"func TestLiveCache",
		`requireLiveMatrixCredential`,
		`t.Run("anthropic-messages"`,
		`t.Run("openai-responses"`,
		`t.Run("openai-chat"`,
		`t.Run("gemini-generate-content"`,
		`t.Run("xai-responses"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("live cache fixture does not contain %q", fragment)
		}
	}
	if count := strings.Count(text, `t.Run("`); count != 5 {
		t.Fatalf("live cache fixture declares %d subtests, want exactly 5", count)
	}
}

// R-6PME-Y8QW
func TestLiveCacheSubtestsExerciseSavepointRestoreAndAssertCache(t *testing.T) {
	contents, err := os.ReadFile("cache_live_test.go")
	if err != nil {
		t.Fatalf("read live cache fixture: %v", err)
	}
	text := string(contents)
	names := []string{
		"anthropic-messages",
		"openai-responses",
		"openai-chat",
		"gemini-generate-content",
		"xai-responses",
	}
	segments := make(map[string]string, len(names))
	for i, name := range names {
		marker := `t.Run("` + name + `"`
		start := strings.Index(text, marker)
		if start < 0 {
			t.Fatalf("live cache fixture has no %s subtest", name)
		}
		end := len(text)
		if i+1 < len(names) {
			next := `t.Run("` + names[i+1] + `"`
			end = strings.Index(text[start+len(marker):], next)
			if end < 0 {
				t.Fatalf("live cache fixture has no %s subtest after %s", names[i+1], name)
			}
			end += start + len(marker)
		}
		segments[name] = text[start:end]
	}

	for _, name := range names {
		segment := segments[name]
		savepoint := strings.Index(segment, "Savepoint()")
		restore := strings.Index(segment, ".Restore(")
		if savepoint < 0 || restore < savepoint {
			t.Fatalf("%s subtest does not savepoint before restore", name)
		}
		firstSend := strings.Index(segment[savepoint:], ".Send(")
		secondSend := strings.Index(segment[restore:], ".Send(")
		if firstSend < 0 || savepoint+firstSend > restore || secondSend < 0 {
			t.Fatalf("%s subtest does not send before and after restore", name)
		}
		wantErrChecks := 3
		if name == "anthropic-messages" {
			wantErrChecks = 2
		}
		if count := strings.Count(segment, "stream.Err()"); count != wantErrChecks {
			t.Fatalf("%s subtest checks stream.Err() %d times, want %d", name, count, wantErrChecks)
		}
	}

	anthropic := segments["anthropic-messages"]
	savepoint := strings.Index(anthropic, "Savepoint()")
	if !strings.Contains(anthropic, "AddSystem(") {
		t.Fatal("anthropic-messages subtest does not build its prefix with AddSystem")
	}
	if strings.Contains(anthropic[:savepoint], ".Send(") {
		t.Fatal("anthropic-messages subtest sends before its savepoint")
	}

	for _, name := range names[:4] {
		if !strings.Contains(segments[name], "CachedTokens") {
			t.Fatalf("%s subtest does not assert cached tokens", name)
		}
	}
	if strings.Contains(segments["xai-responses"], "CachedTokens") {
		t.Fatal("xai-responses subtest must not inspect cached tokens")
	}
}

// R-L65L-NW29
func TestMakefileDeclaresLiveTargetExclusively(t *testing.T) {
	contents, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(contents)
	if strings.Contains(text, "live-oauth") {
		t.Fatal("Makefile must not declare a live-oauth target")
	}
	if strings.Contains(text, "-tags integration") {
		t.Fatal("Makefile must not pass -tags integration to any target")
	}
	for _, fragment := range []string{
		"live:",
		"AGENTKIT_OPENAI_OAUTH_FILE=$(HOME)/.agentkit/openai-auth.json",
		"AGENTKIT_XAI_OAUTH_FILE=$(HOME)/.agentkit/x-ai-auth.json",
		"go test -tags live -count=1 -run '^TestLive' ./...",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("Makefile does not contain %q", fragment)
		}
	}
	if count := strings.Count(text, "-tags live"); count != 1 {
		t.Fatalf("Makefile live-tag use count = %d, want 1", count)
	}
}

// R-WUQW-COI5
func TestLiveMatrixFixtureNamesOneSubtestPerCell(t *testing.T) {
	contents, err := os.ReadFile("live_matrix_test.go")
	if err != nil {
		t.Fatalf("read live matrix fixture: %v", err)
	}
	text := string(contents)
	assertLiveBuildConstraint(t, "live matrix fixture", text)
	if !strings.Contains(text, "func TestLiveMatrix(") {
		t.Fatal("live matrix fixture does not declare TestLiveMatrix")
	}
	for _, fragment := range []string{
		"for _, cell := range liveMatrixCells",
		`t.Run(string(cell.offering)+"/"+string(cell.authMode)+"/"+cell.model`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("live matrix fixture does not contain %q", fragment)
		}
	}
	if rows := liveMatrixCellRows(t, contents); len(rows) != 12 {
		t.Fatalf("live matrix cell count = %d, want 12", len(rows))
	}
}

// R-WVYS-QG8U
func TestLiveMatrixRunsExactModelHostWireCells(t *testing.T) {
	contents, err := os.ReadFile("live_matrix_test.go")
	if err != nil {
		t.Fatalf("read live matrix fixture: %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, "Lookup(") {
		t.Fatal("live matrix fixture does not resolve cells with Lookup")
	}
	want := []string{
		`{OfferingAnthropicMessages, AuthModeAPIKey, HostAnthropic, WireMessages, "claude-haiku-4-5"}`,
		`{OfferingAnthropicMessages, AuthModeAPIKey, HostAnthropic, WireMessages, "claude-opus-5"}`,
		`{OfferingOpenAIResponses, AuthModeAPIKey, HostOpenAI, WireResponses, "gpt-5.4-nano"}`,
		`{OfferingOpenAIResponses, AuthModeOAuth, HostOpenAI, WireResponses, "gpt-5.4-mini"}`,
		`{OfferingOpenAIChat, AuthModeAPIKey, HostOpenAI, WireChat, "gpt-5.4-nano"}`,
		`{OfferingGeminiGenerateContent, AuthModeAPIKey, HostGemini, WireGenerateContent, "gemini-3.1-flash-lite"}`,
		`{OfferingXAIResponses, AuthModeAPIKey, HostXAI, WireResponses, "grok-4.3"}`,
		`{OfferingXAIResponses, AuthModeOAuth, HostXAI, WireResponses, "grok-4.3"}`,
		`{OfferingXAIChat, AuthModeAPIKey, HostXAI, WireChat, "grok-4.3"}`,
		`{OfferingXAIChat, AuthModeOAuth, HostXAI, WireChat, "grok-4.3"}`,
		`{OfferingOpenRouterChat, AuthModeAPIKey, HostOpenRouter, WireChat, "gpt-5.4-nano"}`,
		`{OfferingOpenRouterResponses, AuthModeAPIKey, HostOpenRouter, WireResponses, "gpt-5.4-nano"}`,
	}
	got := liveMatrixCellRows(t, contents)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("live matrix cells = %#v, want %#v", got, want)
	}
}

// R-WX6P-47ZJ
func TestLiveMatrixRunsTextToolAndSystemSequences(t *testing.T) {
	contents, err := os.ReadFile("live_matrix_test.go")
	if err != nil {
		t.Fatalf("read live matrix fixture: %v", err)
	}
	text := string(contents)
	for _, fragment := range []string{
		"offering.Authenticator(rotator)", "APIKeyRotator(credential)",
		"OAuthRotator(FileTokenStore(credential))", "NewEndpoint(auth)",
		"assertLiveMatrixTextTurn(t, offering, endpoint, cell.model)",
		"assertLiveMatrixToolTurn(t, offering, endpoint, cell.model)",
		"assertLiveMatrixSystemSequence(t, offering, endpoint, cell)",
		"New(offering.WireFormat, endpoint", "Config{Log:", "stream.Err()",
		"MessageDone", "RecordUsage", "InputTokens", "OutputTokens",
		"ToolCall", `event.Use.Name == "echo"`, "ToolReturn",
		"conversation.AddSystem", "first.Err()", "second.Err()",
		"strings.Contains(text.Text, firstToken)", "strings.Contains(text.Text, secondToken)",
		"OfferingAnthropicMessages", "AuthModeAPIKey", `cell.model == "claude-haiku-4-5"`,
		"errors.As(streamErr, &vendorError)", "vendorError.Status != 400",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("live matrix fixture does not contain %q", fragment)
		}
	}
	if count := strings.Count(text, "conversation.AddSystem"); count != 2 {
		t.Fatalf("live matrix AddSystem call count = %d, want 2", count)
	}
	if strings.Contains(text, "WithBaseURL") {
		t.Fatal("live matrix fixture must use vendor default base URLs")
	}
}

func liveMatrixCellRows(t *testing.T, contents []byte) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "live_matrix_test.go", contents, 0)
	if err != nil {
		t.Fatalf("parse live matrix fixture: %v", err)
	}
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "liveMatrixCells" || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatal("liveMatrixCells is not a composite literal")
			}
			rows := make([]string, 0, len(literal.Elts))
			for _, element := range literal.Elts {
				var formatted bytes.Buffer
				if err := format.Node(&formatted, token.NewFileSet(), element); err != nil {
					t.Fatalf("format live matrix cell: %v", err)
				}
				rows = append(rows, formatted.String())
			}
			return rows
		}
	}
	t.Fatal("live matrix fixture does not declare liveMatrixCells")
	return nil
}

// R-L2HW-IKU6
func TestLiveMatrixSubtestsFailNeverSkipOnMissingCredential(t *testing.T) {
	contents, err := os.ReadFile("live_matrix_test.go")
	if err != nil {
		t.Fatalf("read live matrix fixture: %v", err)
	}
	text := string(contents)
	assertNeverSkips(t, "live matrix fixture", text)
	for _, fragment := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "XAI_API_KEY", "OPENROUTER_API_KEY",
		"AGENTKIT_OPENAI_OAUTH_FILE", "AGENTKIT_XAI_OAUTH_FILE",
		"os.Getenv", "t.Fatalf",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("live matrix fixture does not contain %q", fragment)
		}
	}
}

func TestEndpointDeclarationsAreExact(t *testing.T) {
	// R-KBPJ-NMJC
	// R-KFD8-SXRF
	// R-YEPA-QILV
	endpointType := reflect.TypeFor[Endpoint]()
	if endpointType.Name() != "Endpoint" || endpointType.Kind() != reflect.Struct || !token.IsExported(endpointType.Name()) {
		t.Fatalf("Endpoint name/kind = %q/%s, want exported named struct", endpointType.Name(), endpointType.Kind())
	}
	for index := range endpointType.NumField() {
		if endpointType.Field(index).IsExported() {
			t.Fatalf("Endpoint field %q is exported", endpointType.Field(index).Name)
		}
	}
	endpointSpecification := declaredType(t, "endpoint.go", "Endpoint")
	if endpointSpecification.Assign.IsValid() {
		t.Fatal("Endpoint is an alias, want a defined struct")
	}
	if _, ok := endpointSpecification.Type.(*ast.StructType); !ok {
		t.Fatalf("Endpoint declaration is %T, want struct", endpointSpecification.Type)
	}

	assertDefinedEndpointType(t, "Authenticator", reflect.TypeFor[Authenticator](), reflect.Interface)
	authType := reflect.TypeFor[Authenticator]()
	wantAuthenticate := reflect.TypeOf(func(context.Context, *http.Request, []byte) error { return nil })
	if authType.NumMethod() != 1 {
		t.Fatalf("Authenticator method count = %d, want 1", authType.NumMethod())
	}
	authenticate, ok := authType.MethodByName("Authenticate")
	if !ok || authenticate.Type != wantAuthenticate {
		t.Fatalf("Authenticator.Authenticate = %v (present=%t), want %s", authenticate.Type, ok, wantAuthenticate)
	}

}

func TestConfigDeclarationIsExact(t *testing.T) {
	// R-TYGN-9I06
	configType := reflect.TypeFor[Config]()
	if configType.Name() != "Config" || !token.IsExported(configType.Name()) || configType.Kind() != reflect.Struct {
		t.Fatalf("Config name/kind = %q/%s, want exported defined struct", configType.Name(), configType.Kind())
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Tools", typeOf: reflect.TypeFor[[]Tool]()},
		{name: "Deferred", typeOf: reflect.TypeFor[[]DeferredGroup]()},
		{name: "Settings", typeOf: reflect.TypeFor[Settings]()},
		{name: "Output", typeOf: reflect.TypeFor[*OutputContract]()},
		{name: "Log", typeOf: reflect.TypeFor[*Log]()},
		{name: "Limits", typeOf: reflect.TypeFor[Limits]()},
	}
	if configType.NumField() != len(wantFields) {
		t.Fatalf("Config field count = %d, want exactly %d", configType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := configType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf || !field.IsExported() {
			t.Fatalf("Config field %d = %s %s (exported=%t), want %s %s exported", index, field.Name, field.Type, field.IsExported(), want.name, want.typeOf)
		}
	}

	specification := declaredType(t, "conversation.go", "Config")
	if specification.Assign.IsValid() {
		t.Fatal("Config is an alias, want a defined struct")
	}
	structType, ok := specification.Type.(*ast.StructType)
	if !ok {
		t.Fatalf("Config declaration is %T, want struct", specification.Type)
	}
	if got := renderedNode(t, structType); got != "struct {\n\tTools    []Tool\n\tDeferred []DeferredGroup\n\tSettings Settings\n\tOutput   *OutputContract\n\tLog      *Log\n\tLimits   Limits\n}" {
		t.Fatalf("Config declaration = %q, want exact six-field declaration", got)
	}
}

// R-PU3A-GJ3X
func TestModuleContainsOnlyRootAndRetryPackages(t *testing.T) {
	packageDirectories := make(map[string]bool)
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == "." {
				return nil
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "specs" || name == "lint-rules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			packageDirectories[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{".": true, "retry": true}
	if !reflect.DeepEqual(packageDirectories, want) {
		t.Fatalf("Go package directories = %v, want %v", packageDirectories, want)
	}
}

// R-PWJ3-82LB
func TestEndpointOwnsOnlyBaseURLAndAuth(t *testing.T) {
	endpointType := reflect.TypeFor[Endpoint]()
	if endpointType.NumField() != 1 || endpointType.Field(0).Name != "config" {
		t.Fatalf("Endpoint fields = %v, want only config", reflect.VisibleFields(endpointType))
	}
	configType := endpointType.Field(0).Type
	if configType.Kind() != reflect.Struct || configType.NumField() != 4 {
		t.Fatalf("Endpoint config = %s with %d fields, want two durable fields and two option scratch fields", configType, configType.NumField())
	}
	baseURLField := configType.Field(0)
	authField := configType.Field(1)
	if baseURLField.Name != "baseURL" || baseURLField.Type != reflect.TypeFor[*url.URL]() {
		t.Fatalf("Endpoint base URL field = %s %s, want baseURL *url.URL", baseURLField.Name, baseURLField.Type)
	}
	if authField.Name != "auth" || authField.Type != reflect.TypeFor[Authenticator]() {
		t.Fatalf("Endpoint auth field = %s %s, want auth Authenticator", authField.Name, authField.Type)
	}
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint(auth, WithBaseURL("https://example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.config.overrideBaseURL != "" || endpoint.config.overrideSet {
		t.Fatalf("constructed Endpoint retained option scratch state: %+v", endpoint.config)
	}
}

func TestMessageDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0B78-ZYU3
	assertEventWrapper(t, "agentkit.go", "MessageDone", reflect.TypeFor[MessageDone](), "Message", reflect.TypeFor[Message]())
	assertEventSeam(t)
}

func TestOutputDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-TOUQ-SVMB
	assertEventWrapper(t, "output.go", "OutputDone", reflect.TypeFor[OutputDone](), "Value", reflect.TypeFor[json.RawMessage]())
}

func TestEventIsSealedToExactlyFourVariants(t *testing.T) {
	// R-UQNM-NRLU
	assertEventSeam(t)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var implementations []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if ok && general.Tok == token.TYPE {
				for _, rawSpecification := range general.Specs {
					specification := rawSpecification.(*ast.TypeSpec)
					structure, isStruct := specification.Type.(*ast.StructType)
					if !specification.Name.IsExported() || !isStruct {
						continue
					}
					for _, field := range structure.Fields.List {
						if len(field.Names) == 0 || !field.Names[0].IsExported() {
							continue
						}
						var rendered bytes.Buffer
						if formatErr := format.Node(&rendered, token.NewFileSet(), field.Type); formatErr != nil {
							t.Fatal(formatErr)
						}
						for _, codecName := range []string{"wireFormat", "anthropicMessagesWire", "openAIResponsesWire", "responsesWire", "openAIChatWire", "chatWire", "geminiGenerateContentWire", "xaiChatWire", "xaiResponsesWire"} {
							if strings.Contains(rendered.String(), codecName) {
								t.Fatalf("consumer-visible %s.%s exposes assignable wire codec %s", specification.Name, field.Names[0], rendered.String())
							}
						}
					}
				}
			}
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name.Name != "isEvent" {
				continue
			}
			receiver := function.Recv.List[0].Type
			if pointer, ok := receiver.(*ast.StarExpr); ok {
				receiver = pointer.X
			}
			identifier, ok := receiver.(*ast.Ident)
			if !ok {
				t.Fatalf("isEvent receiver = %T, want named Event variant", receiver)
			}
			implementations = append(implementations, identifier.Name)
		}
	}
	want := []string{"MessageDone", "ToolCall", "ToolReturn", "OutputDone"}
	if !reflect.DeepEqual(implementations, want) {
		t.Fatalf("in-package Event implementations = %v, want exactly %v", implementations, want)
	}
}

func TestToolCallDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0CF5-DQKS
	assertEventWrapper(t, "agentkit.go", "ToolCall", reflect.TypeFor[ToolCall](), "Use", reflect.TypeFor[ToolUse]())
}

func TestToolReturnDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0DN1-RIBH
	assertEventWrapper(t, "agentkit.go", "ToolReturn", reflect.TypeFor[ToolReturn](), "Result", reflect.TypeFor[ToolResult]())
}

func TestStreamDeclarationIsOpaqueWithExactMethods(t *testing.T) {
	// R-0G2U-J1SV
	streamType := reflect.TypeFor[Stream]()
	if streamType.Name() != "Stream" || streamType.Kind() != reflect.Struct || !token.IsExported(streamType.Name()) {
		t.Fatalf("Stream name/kind = %q/%s, want exported defined struct", streamType.Name(), streamType.Kind())
	}
	for index := range streamType.NumField() {
		if streamType.Field(index).IsExported() {
			t.Fatalf("Stream field %q is exported", streamType.Field(index).Name)
		}
	}
	streamSpecification := declaredType(t, "agentkit.go", "Stream")
	if streamSpecification.Assign.IsValid() {
		t.Fatal("Stream is an alias, want a defined struct")
	}
	if _, ok := streamSpecification.Type.(*ast.StructType); !ok {
		t.Fatalf("Stream declaration is %T, want struct", streamSpecification.Type)
	}

	pointerType := reflect.TypeFor[*Stream]()
	wantMethods := map[string]reflect.Type{
		"Events": reflect.TypeOf(func(*Stream) iter.Seq[Event] { return nil }),
		"Err":    reflect.TypeOf(func(*Stream) error { return nil }),
	}
	if pointerType.NumMethod() != len(wantMethods) {
		t.Fatalf("*Stream exported method count = %d, want exactly %d", pointerType.NumMethod(), len(wantMethods))
	}
	for name, signature := range wantMethods {
		method, ok := pointerType.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("Stream.%s = %v (present=%t), want %s", name, method.Type, ok, signature)
		}
	}
}

func assertEventWrapper(t *testing.T, filename string, name string, wrapper reflect.Type, fieldName string, fieldType reflect.Type) {
	t.Helper()
	if wrapper.Name() != name || wrapper.Kind() != reflect.Struct || !token.IsExported(wrapper.Name()) {
		t.Fatalf("%s name/kind = %q/%s, want exported defined struct", name, wrapper.Name(), wrapper.Kind())
	}
	if wrapper.NumField() != 1 {
		t.Fatalf("%s field count = %d, want exactly one", name, wrapper.NumField())
	}
	field := wrapper.Field(0)
	if field.Name != fieldName || field.Type != fieldType || !field.IsExported() || field.Anonymous {
		t.Fatalf("%s.%s = %s (exported=%t, anonymous=%t), want %s exported", name, field.Name, field.Type, field.IsExported(), field.Anonymous, fieldType)
	}
	if !wrapper.Implements(reflect.TypeFor[Event]()) {
		t.Fatalf("%s does not implement Event", name)
	}
	specification := declaredType(t, filename, name)
	if specification.Assign.IsValid() {
		t.Fatalf("%s is an alias", name)
	}
	structType, ok := specification.Type.(*ast.StructType)
	if !ok || len(structType.Fields.List) != 1 {
		fieldCount := 0
		if ok {
			fieldCount = len(structType.Fields.List)
		}
		t.Fatalf("%s declaration = %T with %d fields, want one-field struct", name, specification.Type, fieldCount)
	}
}

func assertEventSeam(t *testing.T) {
	t.Helper()
	eventType := reflect.TypeFor[Event]()
	if eventType.Name() != "Event" || eventType.Kind() != reflect.Interface || eventType.NumMethod() != 1 {
		t.Fatalf("Event = %q/%s with %d methods, want defined one-method interface", eventType.Name(), eventType.Kind(), eventType.NumMethod())
	}
	marker, ok := eventType.MethodByName("isEvent")
	if !ok || marker.Type != reflect.TypeOf(func() {}) || marker.PkgPath == "" || marker.Name != "isEvent" {
		t.Fatalf("Event marker = %#v (present=%t), want unexported isEvent()", marker, ok)
	}
	specification := declaredType(t, "agentkit.go", "Event")
	if specification.Assign.IsValid() {
		t.Fatal("Event is an alias")
	}
	interfaceType, ok := specification.Type.(*ast.InterfaceType)
	if !ok || !reflect.DeepEqual(interfaceMethodNames(interfaceType), []string{"isEvent"}) {
		t.Fatalf("Event declaration = %T with methods %v, want only isEvent", specification.Type, interfaceMethodNames(interfaceType))
	}
	if interfaceType.Methods.List[0].Names[0].IsExported() {
		t.Fatal("Event marker is exported")
	}
}

func TestConversationConstructionFixesOrchestrationConfiguration(t *testing.T) {
	// R-OJ6F-H3XD
	// R-NW62-A0NM
	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpointA, err := NewEndpoint(auth, WithBaseURL("https://one.invalid/messages"))
	if err != nil {
		t.Fatal(err)
	}
	endpointB, err := NewEndpoint(auth, WithBaseURL("https://two.invalid/responses"))
	if err != nil {
		t.Fatal(err)
	}
	tool := fixtureTool{name: "fixed_tool", schema: json.RawMessage(`{"type":"object"}`)}
	cfg := Config{
		Tools:    []Tool{tool},
		Settings: Settings{Options: Options{"temperature": "0.2", "stop": `["fixed"]`}},
	}
	first, err := New(AnthropicMessagesWire(), endpointA, "model-a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(OpenAIResponsesWire(), endpointB, "model-b", cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools[0] = fixtureTool{name: "mutated", schema: json.RawMessage(`{"type":"object"}`)}
	cfg.Settings.Options["stop"] = `["mutated"]`

	firstProvider := first.provider.(*composedProvider)
	secondProvider := second.provider.(*composedProvider)
	if _, ok := firstProvider.wire.(*anthropicMessagesWire); !ok {
		t.Fatalf("first construction wire = %T, want Anthropic", firstProvider.wire)
	}
	if _, ok := secondProvider.wire.(*openAIResponsesWire); !ok {
		t.Fatalf("second construction wire = %T, want OpenAI Responses", secondProvider.wire)
	}
	if first.identity.Model != "model-a" || second.identity.Model != "model-b" ||
		firstProvider.endpoint.config.baseURL.String() != "https://one.invalid/messages" ||
		secondProvider.endpoint.config.baseURL.String() != "https://two.invalid/responses" {
		t.Fatalf("construction identities/endpoints changed: %#v/%#v", first.identity, second.identity)
	}
	for index, conversation := range []*Conversation{first, second} {
		if len(conversation.tools) != 1 || conversation.tools[0].Name() != "fixed_tool" ||
			conversation.settings.Options["stop"] != `["fixed"]` {
			t.Fatalf("conversation %d construction config changed: tools=%v settings=%#v", index, toolNames(conversation.tools), conversation.settings)
		}
	}
	assertConversationExportsExactly(t)
}

// R-KAG7-PUS7
func TestConversationIdentityMatchesOfferingAndRotatorWithAndWithoutBaseURLOverride(t *testing.T) {
	var offering Offering
	found := false
	for _, entry := range Catalog() {
		for _, candidate := range entry.Offerings {
			for _, spec := range candidate.Endpoints {
				if spec.AuthMode == AuthModeAPIKey {
					offering, found = candidate, true
				}
			}
			if found {
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no cataloged offering accepts AuthModeAPIKey")
	}
	const wantEndpoint = "anthropic-messages"
	if got := string(offering.ID); got != wantEndpoint {
		t.Fatalf("selected Offering.ID = %q, want fixture %q", got, wantEndpoint)
	}

	rotator := APIKeyRotator("test-key")
	const wantAuthMode = "api_key"
	if got := string(rotator.AuthMode()); got != wantAuthMode {
		t.Fatalf("rotator.AuthMode = %q, want fixture %q", got, wantAuthMode)
	}
	auth, err := offering.Authenticator(rotator)
	if err != nil {
		t.Fatalf("Authenticator: %v", err)
	}

	for _, tc := range []struct {
		name string
		opts []EndpointOption
	}{
		{name: "offering default base URL", opts: nil},
		{name: "WithBaseURL override", opts: []EndpointOption{WithBaseURL("https://override.invalid/v1")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, err := NewEndpoint(auth, tc.opts...)
			if err != nil {
				t.Fatalf("NewEndpoint: %v", err)
			}
			conversation, err := New(offering.WireFormat, endpoint, offering.WireModel, Config{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := conversation.identity.Endpoint; got != wantEndpoint {
				t.Fatalf("Identity.Endpoint = %q, want offering identity %q", got, wantEndpoint)
			}
			if got := conversation.identity.AuthMode; got != wantAuthMode {
				t.Fatalf("Identity.AuthMode = %q, want rotator mode %q", got, wantAuthMode)
			}
		})
	}
}

// R-IJ9R-S28F
func TestConstructionSeamIsExactAndSufficientForEveryOffering(t *testing.T) {
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			t.Run(entry.Model+"/"+string(offering.ID), func(t *testing.T) {
				var rotator Rotator
				var endpointSpec EndpointSpec
				for _, spec := range offering.Endpoints {
					if spec.AuthMode == AuthModeAPIKey {
						rotator = APIKeyRotator("test-key")
						endpointSpec = spec
						break
					}
				}
				if rotator == nil {
					rotator = &tokenSourceStub{}
					for _, spec := range offering.Endpoints {
						if spec.AuthMode == AuthModeOAuth {
							endpointSpec = spec
							break
						}
					}
				}
				authenticator, err := offering.Authenticator(rotator)
				if err != nil {
					t.Fatalf("Authenticator: %v", err)
				}
				baseURL := endpointSpec.BaseURL
				if baseURL == "" {
					baseURL = "https://example.test"
				}
				endpoint, err := NewEndpoint(authenticator, WithBaseURL(baseURL))
				if err != nil {
					t.Fatalf("NewEndpoint: %v", err)
				}
				conversation, err := New(offering.WireFormat, endpoint, offering.WireModel, Config{})
				if err != nil || conversation == nil {
					t.Fatalf("New = (%v, %v), want non-nil conversation and nil error", conversation, err)
				}
			})
		}
	}

	type symbolCheck struct {
		name string
		got  reflect.Type
		want reflect.Type
		kind reflect.Kind
	}
	symbols := []symbolCheck{
		{name: "New", got: reflect.TypeOf(New), want: reflect.TypeOf(func(WireFormat, Endpoint, string, Config) (*Conversation, error) { return nil, nil }), kind: reflect.Func},
		{name: "NewEndpoint", got: reflect.TypeOf(NewEndpoint), want: reflect.TypeOf(func(Authenticator, ...EndpointOption) (Endpoint, error) { return Endpoint{}, nil }), kind: reflect.Func},
		{name: "EndpointOption", got: reflect.TypeFor[EndpointOption](), kind: reflect.Func},
		{name: "WithBaseURL", got: reflect.TypeOf(WithBaseURL), want: reflect.TypeOf(func(string) EndpointOption { return nil }), kind: reflect.Func},
		{name: "Endpoint", got: reflect.TypeFor[Endpoint](), kind: reflect.Struct},
		{name: "Authenticator", got: reflect.TypeFor[Authenticator](), kind: reflect.Interface},
		{name: "WireFormat", got: reflect.TypeFor[WireFormat](), kind: reflect.Interface},
		{name: "AnthropicMessagesWire", got: reflect.TypeOf(AnthropicMessagesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "GeminiGenerateContentWire", got: reflect.TypeOf(GeminiGenerateContentWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "ChatWire", got: reflect.TypeOf(ChatWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "ResponsesWire", got: reflect.TypeOf(ResponsesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "OpenAIChatWire", got: reflect.TypeOf(OpenAIChatWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "OpenAIResponsesWire", got: reflect.TypeOf(OpenAIResponsesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "XAIChatWire", got: reflect.TypeOf(XAIChatWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "XAIResponsesWire", got: reflect.TypeOf(XAIResponsesWire), want: reflect.TypeOf(func() WireFormat { return nil }), kind: reflect.Func},
		{name: "Rotator", got: reflect.TypeFor[Rotator](), kind: reflect.Interface},
		{name: "APIKeyRotator", got: reflect.TypeOf(APIKeyRotator), want: reflect.TypeOf(func(string) Rotator { return nil }), kind: reflect.Func},
		{name: "OAuthRotator", got: reflect.TypeOf(OAuthRotator), want: reflect.TypeOf(func(TokenStore) Rotator { return nil }), kind: reflect.Func},
		{name: "Token", got: reflect.TypeFor[Token](), kind: reflect.Struct},
		{name: "TokenStore", got: reflect.TypeFor[TokenStore](), kind: reflect.Interface},
		{name: "FileTokenStore", got: reflect.TypeOf(FileTokenStore), want: reflect.TypeOf(func(string) TokenStore { return nil }), kind: reflect.Func},
		{name: "AuthMode", got: reflect.TypeFor[AuthMode](), kind: reflect.String},
		{name: "Rotation", got: reflect.TypeFor[Rotation](), kind: reflect.Struct},
		{name: "EndpointSpec", got: reflect.TypeFor[EndpointSpec](), kind: reflect.Struct},
		{name: "Offering.Authenticator", got: reflect.TypeOf(Offering.Authenticator), want: reflect.TypeOf(func(Offering, Rotator) (Authenticator, error) { return nil, nil }), kind: reflect.Func},
	}
	wantNames := []string{
		"New", "NewEndpoint", "EndpointOption", "WithBaseURL", "Endpoint", "Authenticator", "WireFormat",
		"AnthropicMessagesWire", "GeminiGenerateContentWire", "ChatWire", "ResponsesWire",
		"OpenAIChatWire", "OpenAIResponsesWire", "XAIChatWire", "XAIResponsesWire",
		"Rotator", "APIKeyRotator", "OAuthRotator", "Token", "TokenStore", "FileTokenStore",
		"AuthMode", "Rotation", "EndpointSpec", "Offering.Authenticator",
	}
	gotNames := make([]string, len(symbols))
	for index, symbol := range symbols {
		gotNames[index] = symbol.name
		if symbol.got == nil || symbol.got.Kind() != symbol.kind {
			t.Fatalf("%s type/kind = %v, want present %s", symbol.name, symbol.got, symbol.kind)
		}
		if symbol.want != nil && symbol.got != symbol.want {
			t.Fatalf("%s type = %s, want %s", symbol.name, symbol.got, symbol.want)
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("construction seam = %v, want exactly %v", gotNames, wantNames)
	}
}

// R-W1KR-P3S7
// R-VT1H-0PLC
// R-IKHO-5TZ4
func TestNewDeclarationTakesWireFormatAndRejectsNilWire(t *testing.T) {
	declarations := parsePackageDeclarations(t, ".")
	assertASTFunction(t, declarations, "New", []string{"WireFormat", "Endpoint", "string", "Config"}, []string{"*Conversation", "error"}, false)

	auth := authFunc(func(context.Context, *http.Request, []byte) error { return nil })
	endpoint, err := NewEndpoint(auth, WithBaseURL("https://example.invalid/wire"))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := New(nil, endpoint, "model", Config{})
	if conversation != nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(nil, ...) = (%v, %v), want nil ErrInvalidConfig", conversation, err)
	}

	for _, test := range []struct {
		name string
		wire WireFormat
		want reflect.Type
	}{
		{"anthropic_messages", AnthropicMessagesWire(), reflect.TypeFor[*anthropicMessagesWire]()},
		{"openai_responses", OpenAIResponsesWire(), reflect.TypeFor[*openAIResponsesWire]()},
		{"responses", ResponsesWire(), reflect.TypeFor[*responsesWire]()},
		{"openai_chat_completions", OpenAIChatWire(), reflect.TypeFor[*openAIChatWire]()},
		{"chat", ChatWire(), reflect.TypeFor[*chatWire]()},
		{"gemini_generate_content", GeminiGenerateContentWire(), reflect.TypeFor[*geminiGenerateContentWire]()},
		{"xai_chat", XAIChatWire(), reflect.TypeFor[*xaiChatWire]()},
		{"xai_responses", XAIResponsesWire(), reflect.TypeFor[*xaiResponsesWire]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			conversation, err := New(test.wire, endpoint, "model", Config{})
			if err != nil {
				t.Fatal(err)
			}
			provider, ok := conversation.provider.(*composedProvider)
			if !ok {
				t.Fatalf("New provider = %T, want *composedProvider", conversation.provider)
			}
			if reflect.TypeOf(provider.wire) != test.want {
				t.Fatalf("New wire = %T, want %s", provider.wire, test.want)
			}
		})
	}
}

func TestWireFormatDeclarationIsExactAndSealed(t *testing.T) {
	// R-OXYY-4WN5
	// R-OWR1-R4WG
	wireType := reflect.TypeFor[WireFormat]()
	if wireType.Name() != "WireFormat" || !token.IsExported(wireType.Name()) || wireType.Kind() != reflect.Interface {
		t.Fatalf("WireFormat name/kind = %q/%s, want exported named interface", wireType.Name(), wireType.Kind())
	}
	wantMethods := map[string]reflect.Type{
		"EncodeRequest": reflect.TypeOf(func(requestState) ([]byte, error) { return nil, nil }),
		"DecodeStream":  reflect.TypeOf(func(iter.Seq2[[]byte, error]) iter.Seq2[Event, error] { return nil }),
		"RenderTools":   reflect.TypeOf(func([]Tool) (json.RawMessage, error) { return nil, nil }),
		"OptionSpecs":   reflect.TypeOf(func() []OptionSpec { return nil }),
	}
	if wireType.NumMethod() != len(wantMethods) {
		t.Fatalf("WireFormat method count = %d, want %d", wireType.NumMethod(), len(wantMethods))
	}
	for name, signature := range wantMethods {
		method, ok := wireType.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("WireFormat.%s = %v (present=%t), want %s", name, method.Type, ok, signature)
		}
	}

	declarations := parsePackageDeclarations(t, ".")
	interfaceSpecification, ok := declarations.types["WireFormat"]
	if !ok {
		t.Fatal("WireFormat declaration is missing")
	}
	interfaceType, ok := interfaceSpecification.Type.(*ast.InterfaceType)
	if !ok {
		t.Fatalf("WireFormat declaration is %T, want interface", interfaceSpecification.Type)
	}
	methodNames := []string{"EncodeRequest", "DecodeStream", "RenderTools", "OptionSpecs"}
	if got := interfaceMethodNames(interfaceType); !reflect.DeepEqual(got, methodNames) {
		t.Fatalf("WireFormat methods = %v, want exactly %v in order", got, methodNames)
	}
	assertASTMethod(t, interfaceType, "EncodeRequest", []string{"requestState"}, []string{"[]byte", "error"})
	assertASTMethod(t, interfaceType, "DecodeStream", []string{"iter.Seq2[[]byte, error]"}, []string{"iter.Seq2[Event, error]"})
	assertASTMethod(t, interfaceType, "RenderTools", []string{"[]Tool"}, []string{"json.RawMessage", "error"})
	assertASTMethod(t, interfaceType, "OptionSpecs", nil, []string{"[]OptionSpec"})

	tests := []struct {
		name     string
		exported func() WireFormat
		wantType reflect.Type
	}{
		{"AnthropicMessagesWire", AnthropicMessagesWire, reflect.TypeFor[*anthropicMessagesWire]()},
		{"GeminiGenerateContentWire", GeminiGenerateContentWire, reflect.TypeFor[*geminiGenerateContentWire]()},
		{"ChatWire", ChatWire, reflect.TypeFor[*chatWire]()},
		{"ResponsesWire", ResponsesWire, reflect.TypeFor[*responsesWire]()},
		{"OpenAIChatWire", OpenAIChatWire, reflect.TypeFor[*openAIChatWire]()},
		{"OpenAIResponsesWire", OpenAIResponsesWire, reflect.TypeFor[*openAIResponsesWire]()},
		{"XAIChatWire", XAIChatWire, reflect.TypeFor[*xaiChatWire]()},
		{"XAIResponsesWire", XAIResponsesWire, reflect.TypeFor[*xaiResponsesWire]()},
	}
	wantConstructors := make(map[string]bool, len(tests))
	for _, test := range tests {
		wantConstructors[test.name] = true
		assertASTFunction(t, declarations, test.name, nil, []string{"WireFormat"}, false)
		got := test.exported()
		if got == nil {
			t.Fatalf("%s returned nil", test.name)
		}
		if reflect.TypeOf(got) != test.wantType {
			t.Fatalf("%s returned %T, want the built-in codec %v", test.name, got, test.wantType)
		}
	}
	var gotConstructors []string
	for name := range declarations.functions {
		if token.IsExported(name) && strings.HasSuffix(name, "Wire") {
			gotConstructors = append(gotConstructors, name)
			if !wantConstructors[name] {
				t.Errorf("unexpected exported wire constructor %s", name)
			}
		}
	}
	if len(gotConstructors) != len(wantConstructors) {
		t.Fatalf("exported wire constructors = %v, want exactly eight", gotConstructors)
	}
}

// R-HC0C-ZEW7
func TestRootWireConstructorsHaveDistinctSoleCodecTypes(t *testing.T) {
	wires := []struct {
		filename    string
		typeName    string
		constructor func() WireFormat
	}{
		{"wire_anthropic_messages.go", "anthropicMessagesWire", AnthropicMessagesWire},
		{"wire_gemini_generate_content.go", "geminiGenerateContentWire", GeminiGenerateContentWire},
		{"wire_chat.go", "chatWire", ChatWire},
		{"wire_responses.go", "responsesWire", ResponsesWire},
		{"wire_openai_chat.go", "openAIChatWire", OpenAIChatWire},
		{"wire_openai_responses.go", "openAIResponsesWire", OpenAIResponsesWire},
		{"wire_xai_chat.go", "xaiChatWire", XAIChatWire},
		{"wire_xai_responses.go", "xaiResponsesWire", XAIResponsesWire},
	}

	allowedTypes := make(map[string]bool, len(wires))
	dynamicTypes := make(map[reflect.Type]string, len(wires))
	for _, wire := range wires {
		allowedTypes[wire.typeName] = true
		structure, ok := declaredType(t, wire.filename, wire.typeName).Type.(*ast.StructType)
		if !ok {
			t.Errorf("%s in %s is not a struct", wire.typeName, wire.filename)
			continue
		}
		wantFields := 1
		if wire.typeName == "geminiGenerateContentWire" {
			wantFields = 4
		}
		if len(structure.Fields.List) != wantFields {
			t.Errorf("%s fields = %d, want embedded wireCodec plus required private state", wire.typeName, len(structure.Fields.List))
			continue
		}
		field := structure.Fields.List[0]
		codec, isIdentifier := field.Type.(*ast.Ident)
		if len(field.Names) != 0 || !isIdentifier || codec.Name != "wireCodec" {
			t.Errorf("%s first field = %#v, want embedded wireCodec", wire.typeName, field)
		}
		if wire.typeName == "geminiGenerateContentWire" {
			cacheMark := structure.Fields.List[1]
			cacheName := structure.Fields.List[2]
			requestUsedCache := structure.Fields.List[3]
			markType, markIsIdentifier := cacheMark.Type.(*ast.Ident)
			nameType, nameIsIdentifier := cacheName.Type.(*ast.Ident)
			requestUsedCacheType, requestUsedCacheIsIdentifier := requestUsedCache.Type.(*ast.Ident)
			if len(cacheMark.Names) != 1 || cacheMark.Names[0].Name != "cacheMark" || !markIsIdentifier || markType.Name != "int" {
				t.Errorf("gemini cache mark field = %#v, want cacheMark int", cacheMark)
			}
			if len(cacheName.Names) != 1 || cacheName.Names[0].Name != "cacheName" || !nameIsIdentifier || nameType.Name != "string" {
				t.Errorf("gemini cache name field = %#v, want cacheName string", cacheName)
			}
			if len(requestUsedCache.Names) != 1 || requestUsedCache.Names[0].Name != "requestUsedCache" || !requestUsedCacheIsIdentifier || requestUsedCacheType.Name != "bool" {
				t.Errorf("gemini request cache-use field = %#v, want requestUsedCache bool", requestUsedCache)
			}
		}

		dynamicType := reflect.TypeOf(wire.constructor())
		wantType := "*agentkit." + wire.typeName
		if dynamicType == nil || dynamicType.String() != wantType {
			t.Errorf("%s() dynamic type = %v, want %s", strings.TrimSuffix(strings.TrimPrefix(wire.filename, "wire_"), ".go"), dynamicType, wantType)
		}
		if prior, duplicate := dynamicTypes[dynamicType]; duplicate {
			t.Errorf("%s and %s constructors share dynamic type %v", prior, wire.typeName, dynamicType)
		}
		dynamicTypes[dynamicType] = wire.typeName
	}
	if len(dynamicTypes) != len(wires) {
		t.Errorf("root constructors have %d distinct dynamic types, want %d", len(dynamicTypes), len(wires))
	}

	filenames, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	embeddingTypes := 0
	for _, filename := range filenames {
		if filename == "wire.go" || strings.HasSuffix(filename, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, rawSpecification := range general.Specs {
				specification := rawSpecification.(*ast.TypeSpec)
				structure, ok := specification.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structure.Fields.List {
					codec, isIdentifier := field.Type.(*ast.Ident)
					if len(field.Names) == 0 && isIdentifier && codec.Name == "wireCodec" {
						embeddingTypes++
						if !allowedTypes[specification.Name.Name] {
							t.Errorf("production type %s in %s also embeds wireCodec", specification.Name.Name, filename)
						}
					}
				}
			}
		}
	}
	if embeddingTypes != len(wires) {
		t.Errorf("production types embedding wireCodec = %d, want exactly %d", embeddingTypes, len(wires))
	}
}

// R-II1V-EAHQ
func TestExternalPackageCannotImplementWireFormat(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	module := "module externalwiretest\n\ngo 1.26\n\nrequire github.com/ikigenba/ikigenba/agentkit v0.0.0\n\nreplace github.com/ikigenba/ikigenba/agentkit => " + workingDirectory + "\n"
	source := `package externalwiretest

import (
	"encoding/json"
	"iter"

	"github.com/ikigenba/ikigenba/agentkit"
)

type requestState struct{}
type outsider struct{}

func (outsider) EncodeRequest(requestState) ([]byte, error) { return nil, nil }
func (outsider) DecodeStream(iter.Seq2[[]byte, error]) iter.Seq2[agentkit.Event, error] { return nil }
func (outsider) RenderTools([]agentkit.Tool) (json.RawMessage, error) { return nil, nil }
func (outsider) OptionSpecs() []agentkit.OptionSpec { return nil }

var _ agentkit.WireFormat = outsider{}
`
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "outside_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", ".")
	command.Dir = temporary
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("external WireFormat implementation compiled successfully:\n%s", output)
	}
	if !bytes.Contains(output, []byte("want EncodeRequest(agentkit.requestState)")) {
		t.Fatalf("external implementation failed for the wrong reason: %v\n%s", err, output)
	}
}

// R-IMXG-XDGI
func TestWireConstructorsMatchFileNames(t *testing.T) {
	constructors := []struct {
		name     string
		filename string
	}{
		{"AnthropicMessagesWire", "wire_anthropic_messages.go"},
		{"GeminiGenerateContentWire", "wire_gemini_generate_content.go"},
		{"ChatWire", "wire_chat.go"},
		{"ResponsesWire", "wire_responses.go"},
		{"OpenAIChatWire", "wire_openai_chat.go"},
		{"OpenAIResponsesWire", "wire_openai_responses.go"},
		{"XAIChatWire", "wire_xai_chat.go"},
		{"XAIResponsesWire", "wire_xai_responses.go"},
	}
	for _, constructor := range constructors {
		for _, candidate := range constructors {
			parsed, err := parser.ParseFile(token.NewFileSet(), candidate.filename, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, declaration := range parsed.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				found = found || ok && function.Recv == nil && function.Name.Name == constructor.name
			}
			if found != (candidate.filename == constructor.filename) {
				t.Fatalf("%s presence in %s = %t, want %t", constructor.name, candidate.filename, found, candidate.filename == constructor.filename)
			}
		}
	}
}

func assertDefinedEndpointType(t *testing.T, name string, typeOf reflect.Type, kind reflect.Kind) {
	t.Helper()
	if typeOf.Name() != name || typeOf.Kind() != kind || !token.IsExported(typeOf.Name()) {
		t.Fatalf("%s = %q/%s, want exported defined %s", name, typeOf.Name(), typeOf.Kind(), kind)
	}
	if specification := declaredType(t, "endpoint.go", name); specification.Assign.IsValid() {
		t.Fatalf("%s is an alias", name)
	}
}

func assertFunctionSignature(t *testing.T, got, want reflect.Type) {
	t.Helper()
	if got.NumIn() != want.NumIn() || got.NumOut() != want.NumOut() {
		t.Fatalf("signature %s does not match %s", got, want)
	}
	for index := range got.NumIn() {
		if got.In(index) != want.In(index) {
			t.Fatalf("parameter %d = %s, want %s", index, got.In(index), want.In(index))
		}
	}
	for index := range got.NumOut() {
		if got.Out(index) != want.Out(index) {
			t.Fatalf("result %d = %s, want %s", index, got.Out(index), want.Out(index))
		}
	}
}

func TestToolDeclarationIsExactAndSealed(t *testing.T) {
	// R-DH1U-CH43
	toolType := reflect.TypeFor[Tool]()
	if toolType.Name() != "Tool" || !token.IsExported(toolType.Name()) || toolType.Kind() != reflect.Interface {
		t.Fatalf("Tool name/kind = %q/%s, want exported named interface", toolType.Name(), toolType.Kind())
	}
	wantExported := map[string]reflect.Type{
		"Name":        reflect.TypeOf(func() string { return "" }),
		"Description": reflect.TypeOf(func() string { return "" }),
		"Schema":      reflect.TypeOf(func() json.RawMessage { return nil }),
		"Call":        reflect.TypeOf(func(context.Context, json.RawMessage) (string, error) { return "", nil }),
		"Access":      reflect.TypeOf(func(json.RawMessage) Access { panic("type only") }),
		"isTool":      reflect.TypeOf(func() {}),
	}
	if toolType.NumMethod() != len(wantExported) {
		t.Fatalf("Tool exported method count = %d, want %d", toolType.NumMethod(), len(wantExported))
	}
	for name, signature := range wantExported {
		method, ok := toolType.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("Tool.%s = %v (present=%t), want %s", name, method.Type, ok, signature)
		}
	}
	marker, _ := toolType.MethodByName("isTool")
	if marker.PkgPath == "" {
		t.Fatal("Tool marker has no package path, want unexported sealing method")
	}

	specification := declaredType(t, "tool.go", "Tool")
	if specification.Assign.IsValid() {
		t.Fatal("Tool is an alias, want a defined interface")
	}
	interfaceType, ok := specification.Type.(*ast.InterfaceType)
	if !ok {
		t.Fatalf("Tool declaration = %T, want interface", specification.Type)
	}
	wantMethods := []string{"Name", "Description", "Schema", "Call", "Access", "isTool"}
	if got := interfaceMethodNames(interfaceType); !reflect.DeepEqual(got, wantMethods) {
		t.Fatalf("Tool methods = %v, want exactly %v in order", got, wantMethods)
	}
	assertASTMethod(t, interfaceType, "Name", nil, []string{"string"})
	assertASTMethod(t, interfaceType, "Description", nil, []string{"string"})
	assertASTMethod(t, interfaceType, "Schema", nil, []string{"json.RawMessage"})
	assertASTMethod(t, interfaceType, "Call", []string{"context.Context", "json.RawMessage"}, []string{"string", "error"})
	assertASTMethod(t, interfaceType, "Access", []string{"json.RawMessage"}, []string{"Access"})
	assertASTMethod(t, interfaceType, "isTool", nil, nil)
	if interfaceType.Methods.List[5].Names[0].IsExported() {
		t.Fatal("Tool marker is exported, want package-sealing lowercase marker")
	}
}

func TestSiblingToolConstructionSurfaceIsExactAndSealed(t *testing.T) {
	// R-5ZBT-XCT3
	toolType := reflect.TypeFor[Tool]()
	if toolType.Kind() != reflect.Interface || toolType.NumMethod() != 6 {
		t.Fatalf("Tool = %s with %d methods, want sealed six-method interface", toolType, toolType.NumMethod())
	}
	marker, ok := toolType.MethodByName("isTool")
	if !ok || marker.PkgPath == "" {
		t.Fatal("Tool lacks its unexported package-sealing marker")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "tool.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var constructors []string
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !function.Name.IsExported() || function.Type.Results == nil {
			continue
		}
		results := function.Type.Results.List
		returnsTool := len(results) == 1 && renderedNode(t, results[0].Type) == "Tool"
		returnsToolAndError := len(results) == 2 && renderedNode(t, results[0].Type) == "Tool" && renderedNode(t, results[1].Type) == "error"
		if returnsTool || returnsToolAndError {
			constructors = append(constructors, function.Name.Name)
		}
	}
	want := []string{"NewTool", "MustTool", "NewToolFromSchema"}
	if !reflect.DeepEqual(constructors, want) {
		t.Fatalf("exported Tool constructors = %v, want exactly %v", constructors, want)
	}
	assertExactToolFunctionDeclaration(t, "NewTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error), access func(in In) Access) (Tool, error)")
	assertExactToolFunctionDeclaration(t, "MustTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error), access func(in In) Access) Tool")
	assertExactToolFunctionDeclaration(t, "NewToolFromSchema", "func(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error), access func(args json.RawMessage) Access) (Tool, error)")
}

func TestExternalPackageCannotImplementSealedTool(t *testing.T) {
	// R-3Y5U-Z4BF
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	module := "module externaltooltest\n\ngo 1.26\n\nrequire github.com/ikigenba/ikigenba/agentkit v0.0.0\n\nreplace github.com/ikigenba/ikigenba/agentkit => " + workingDirectory + "\n"
	source := `package externaltooltest

import (
	"context"
	"encoding/json"

	"github.com/ikigenba/ikigenba/agentkit"
)

type outsider struct{}

func (outsider) Name() string { return "outside" }
func (outsider) Description() string { return "outside" }
func (outsider) Schema() json.RawMessage { return json.RawMessage(` + "`{\"type\":\"object\"}`" + `) }
func (outsider) Call(context.Context, json.RawMessage) (string, error) { return "", nil }
func (outsider) Access(json.RawMessage) agentkit.Access { panic("not called") }
func (outsider) isTool() {}

var _ agentkit.Tool = outsider{}
`
	if err := os.WriteFile(filepath.Join(temporary, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "outside_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", ".")
	command.Dir = temporary
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("external Tool implementation compiled successfully:\n%s", output)
	}
	if !bytes.Contains(output, []byte("unexported method isTool")) {
		t.Fatalf("external implementation failed for the wrong reason: %v\n%s", err, output)
	}
}

func TestJSONSchemaVocabularyIsDocumentedStringGrammarNotExportedConstants(t *testing.T) {
	// R-431G-I7A7
	// R-61RM-OWAH
	parsed, err := parser.ParseFile(token.NewFileSet(), "tool.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var newToolDocumentation string
	for _, declaration := range parsed.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Name.Name == "NewTool" && declaration.Doc != nil {
				newToolDocumentation = declaration.Doc.Text()
			}
		case *ast.GenDecl:
			if declaration.Tok != token.CONST {
				continue
			}
			for _, specification := range declaration.Specs {
				for _, name := range specification.(*ast.ValueSpec).Names {
					if name.IsExported() {
						t.Fatalf("tool.go exports tag-vocabulary constant %s", name.Name)
					}
				}
			}
		}
	}
	if !strings.Contains(newToolDocumentation, "jsonschema string tag") {
		t.Errorf("NewTool documentation does not describe the jsonschema string tag grammar:\n%s", newToolDocumentation)
	}
	documentedTokens := make(map[string]bool)
	for _, token := range strings.FieldsFunc(newToolDocumentation, func(character rune) bool {
		return unicode.IsSpace(character) || strings.ContainsRune(`\",.`, character)
	}) {
		documentedTokens[token] = true
	}
	for _, fragment := range []string{
		"required", "enum=a|b", "description=text", "minimum=n", "maximum=n",
		"exclusiveMinimum=n", "exclusiveMaximum=n", "multipleOf=n", "minLength=n", "maxLength=n",
		"pattern=expr", "format=name", "minItems=n", "maxItems=n", "uniqueItems=true|false",
	} {
		if !documentedTokens[fragment] {
			t.Errorf("NewTool documentation does not describe string grammar fragment %q:\n%s", fragment, newToolDocumentation)
		}
	}
}

func TestEveryToolConstructionAndWireRenderingUsesExportedSchemaChecker(t *testing.T) {
	// R-45H9-9QRL
	wantCalls := map[string]string{
		"NewTool":           "newTool",
		"MustTool":          "NewTool",
		"NewToolFromSchema": "newTool",
		"newTool":           "ValidateToolSchema",
	}
	for functionName, calledName := range wantCalls {
		function := declaredFunction(t, "tool.go", functionName)
		if !functionCallsIdentifier(function, calledName) {
			t.Errorf("%s does not route through %s", functionName, calledName)
		}
	}
	canonicalValidation := declaredFunction(t, "wire.go", "validateCanonicalTools")
	if !functionCallsIdentifier(canonicalValidation, "ValidateToolSchema") {
		t.Fatal("common canonical tool validation does not defensively call exported ValidateToolSchema")
	}
	for _, filename := range []string{"wire_responses.go", "wire_chat.go", "wire_openai_responses.go", "wire_openai_chat.go", "wire_anthropic_messages.go", "wire_gemini_generate_content.go", "wire_xai_chat.go", "wire_xai_responses.go"} {
		wireRender := declaredMethod(t, filename, "RenderTools")
		if !functionCallsIdentifier(wireRender, "validateCanonicalTools") {
			t.Errorf("%s RenderTools does not call the one common canonical validator", filename)
		}
	}
	wireCodecType := declaredType(t, "wire.go", "wireCodec").Type.(*ast.StructType)
	for _, field := range wireCodecType.Fields.List {
		for _, name := range field.Names {
			lower := strings.ToLower(name.Name)
			if strings.Contains(lower, "render") || strings.Contains(lower, "dialect") || strings.Contains(lower, "mode") {
				t.Errorf("wireCodec retains shared declaration-shaping field %q", name.Name)
			}
		}
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "tool.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.IsExported() && strings.Contains(function.Name.Name, "Schema") && strings.Contains(function.Name.Name, "Valid") && function.Name.Name != "ValidateToolSchema" {
			t.Fatalf("competing exported schema checker %s exists", function.Name.Name)
		}
	}
}

func TestConcreteWiresOwnToolDeclarationShaping(t *testing.T) {
	// R-47X2-1A8Z
	// R-494Y-F1ZO
	wires := []struct {
		filename       string
		receiver       string
		renderer       string
		wrapperMarkers []string
		forbidden      []string
	}{
		{
			filename:       "wire_openai_responses.go",
			receiver:       "openAIResponsesWire",
			renderer:       "renderOpenAIResponsesTools",
			wrapperMarkers: []string{"json:\"type\"", "json:\"name\"", "json:\"description\"", "json:\"parameters\""},
			forbidden:      []string{"json:\"function\"", "json:\"input_schema\"", "json:\"functionDeclarations\""},
		},
		{
			filename:       "wire_openai_chat.go",
			receiver:       "openAIChatWire",
			renderer:       "renderOpenAIChatTools",
			wrapperMarkers: []string{"json:\"type\"", "json:\"function\"", "json:\"name\"", "json:\"description\"", "json:\"parameters\""},
			forbidden:      []string{"json:\"input_schema\"", "json:\"functionDeclarations\""},
		},
		{
			filename:       "wire_anthropic_messages.go",
			receiver:       "anthropicMessagesWire",
			renderer:       "renderAnthropicTools",
			wrapperMarkers: []string{"json:\"name\"", "json:\"description\"", "json:\"input_schema\""},
			forbidden:      []string{"json:\"type\"", "json:\"parameters\"", "json:\"functionDeclarations\""},
		},
		{
			filename:       "wire_gemini_generate_content.go",
			receiver:       "geminiGenerateContentWire",
			renderer:       "renderGeminiTools",
			wrapperMarkers: []string{"json:\"tools\"", "json:\"functionDeclarations\"", "json:\"name\"", "json:\"description\"", "json:\"parameters\""},
			forbidden:      []string{"json:\"type\"", "json:\"input_schema\""},
		},
	}

	for _, wire := range wires {
		t.Run(wire.receiver, func(t *testing.T) {
			method := declaredMethod(t, wire.filename, "RenderTools")
			if got := methodReceiverName(method); got != wire.receiver {
				t.Fatalf("%s RenderTools receiver = %q, want %q", wire.filename, got, wire.receiver)
			}
			if !functionCallsIdentifier(method, "validateCanonicalTools") {
				t.Errorf("%s RenderTools bypasses canonical validation", wire.filename)
			}
			if !functionCallsIdentifier(method, wire.renderer) {
				t.Errorf("%s RenderTools does not delegate declaration shaping to its local %s", wire.filename, wire.renderer)
			}

			renderer := declaredFunction(t, wire.filename, wire.renderer)
			shape := renderedNode(t, renderer)
			for _, marker := range wire.wrapperMarkers {
				if !strings.Contains(shape, marker) {
					t.Errorf("%s lacks wire-owned declaration marker %q", wire.renderer, marker)
				}
			}
			for _, marker := range wire.forbidden {
				if strings.Contains(shape, marker) {
					t.Errorf("%s contains another wire's declaration marker %q", wire.renderer, marker)
				}
			}
		})
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "wire.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		signature := renderedNode(t, function.Type)
		if strings.Contains(signature, "[]Tool") && strings.Contains(signature, "json.RawMessage") {
			t.Errorf("shared wire layer declares tool-shaping function %s with signature %s", function.Name, signature)
		}
		if function.Name.Name != "validateCanonicalTools" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.SwitchStmt, *ast.TypeSwitchStmt:
				t.Error("shared canonical validation contains wire-mode/dialect dispatch")
			}
			return true
		})
	}
}

func methodReceiverName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return ""
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	identifier, _ := receiver.(*ast.Ident)
	if identifier == nil {
		return ""
	}
	return identifier.Name
}

func functionCallsIdentifier(function *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == name {
			found = true
		}
		return true
	})
	return found
}

func declaredMethod(t *testing.T, filename, name string) *ast.FuncDecl {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv != nil && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("method %s is not declared in %s", name, filename)
	return nil
}

type architectureToolInput struct {
	Query string `json:"query" jsonschema:"required"`
}

func TestNewToolDeclarationIsExact(t *testing.T) {
	// R-DI9Q-Q8US
	got := reflect.TypeOf(NewTool[architectureToolInput])
	want := reflect.TypeOf(func(string, string, func(context.Context, architectureToolInput) (string, error), func(architectureToolInput) Access) (Tool, error) {
		return nil, nil
	})
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "NewTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error), access func(in In) Access) (Tool, error)")
}

func TestMustToolDeclarationIsExact(t *testing.T) {
	// R-DJHN-40LH
	got := reflect.TypeOf(MustTool[architectureToolInput])
	want := reflect.TypeOf(func(string, string, func(context.Context, architectureToolInput) (string, error), func(architectureToolInput) Access) Tool {
		return nil
	})
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "MustTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error), access func(in In) Access) Tool")
}

func TestNewToolFromSchemaDeclarationIsExact(t *testing.T) {
	// R-DKPJ-HSC6
	got := reflect.TypeOf(NewToolFromSchema)
	want := reflect.TypeOf(func(string, string, json.RawMessage, func(context.Context, json.RawMessage) (string, error), func(json.RawMessage) Access) (Tool, error) {
		return nil, nil
	})
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "NewToolFromSchema", "func(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error), access func(args json.RawMessage) Access) (Tool, error)")
}

func TestValidateToolSchemaDeclarationIsExact(t *testing.T) {
	// R-07JJ-UNM0
	got := reflect.TypeOf(ValidateToolSchema)
	want := reflect.TypeOf(func(json.RawMessage) error { return nil })
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "ValidateToolSchema", "func(schema json.RawMessage) error")
}

func assertExactToolFunctionDeclaration(t *testing.T, name, want string) {
	t.Helper()
	declaration := declaredFunction(t, "tool.go", name)
	if declaration.Recv != nil || !declaration.Name.IsExported() {
		t.Fatalf("%s is not an exported package function", name)
	}
	var rendered bytes.Buffer
	if err := format.Node(&rendered, token.NewFileSet(), declaration.Type); err != nil {
		t.Fatal(err)
	}
	if got := rendered.String(); got != want {
		t.Fatalf("%s declaration = %q, want exactly %q", name, got, want)
	}
}

func renderedNode(t *testing.T, node ast.Node) string {
	t.Helper()
	var rendered bytes.Buffer
	if err := format.Node(&rendered, token.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	return rendered.String()
}

func TestFramerAndSSEFramesDeclarationsAreExact(t *testing.T) {
	// R-ZGPR-FPAQ
	// R-ZHXN-TH1F
	wantSignature := reflect.TypeOf(func(io.Reader) iter.Seq2[[]byte, error] { return nil })
	framerType := reflect.TypeFor[Framer]()
	if framerType.Name() != "Framer" || !token.IsExported(framerType.Name()) || framerType.Kind() != reflect.Func {
		t.Fatalf("Framer name/kind = %q/%s, want exported defined function type", framerType.Name(), framerType.Kind())
	}
	if framerType.NumIn() != 1 || framerType.In(0) != wantSignature.In(0) || framerType.NumOut() != 1 || framerType.Out(0) != wantSignature.Out(0) {
		t.Fatalf("Framer signature = %s, want func%s", framerType, strings.TrimPrefix(wantSignature.String(), "func"))
	}
	framerSpecification := declaredType(t, "wire.go", "Framer")
	if framerSpecification.Assign.IsValid() {
		t.Fatal("Framer is an alias, want a defined function type")
	}
	if _, ok := framerSpecification.Type.(*ast.FuncType); !ok {
		t.Fatalf("Framer declaration is %T, want function type", framerSpecification.Type)
	}

	sseType := reflect.TypeOf(SSEFrames)
	if sseType != wantSignature {
		t.Fatalf("SSEFrames signature = %s, want exactly %s", sseType, wantSignature)
	}
	if !sseType.AssignableTo(framerType) {
		t.Fatalf("SSEFrames type %s is not assignable to Framer %s", sseType, framerType)
	}
	var assigned Framer = SSEFrames
	if assigned == nil {
		t.Fatal("SSEFrames assignment unexpectedly produced a nil Framer")
	}
	sseDeclaration := declaredFunction(t, "sse.go", "SSEFrames")
	if sseDeclaration.Recv != nil || !sseDeclaration.Name.IsExported() {
		t.Fatal("SSEFrames is not an exported package function")
	}
}

func declaredType(t *testing.T, filename, name string) *ast.TypeSpec {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
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
			if typeSpecification.Name.Name == name {
				return typeSpecification
			}
		}
	}
	t.Fatalf("type %s is not declared in %s", name, filename)
	return nil
}

func declaredFunction(t *testing.T, filename, name string) *ast.FuncDecl {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("function %s is not declared in %s", name, filename)
	return nil
}

func TestConversationPublicShape(t *testing.T) {
	// R-YURK-JTY8
	// R-WEW7-DNV4
	// R-7KE4-A3OZ
	// R-VT1H-0PLC
	// R-IKHO-5TZ4
	conversationType := reflect.TypeOf(Conversation{})
	if conversationType.Name() != "Conversation" || !token.IsExported(conversationType.Name()) || conversationType.Kind() != reflect.Struct {
		t.Fatalf("Conversation name/kind = %q/%s, want exported Conversation struct", conversationType.Name(), conversationType.Kind())
	}
	for index := range conversationType.NumField() {
		field := conversationType.Field(index)
		if field.IsExported() {
			t.Fatalf("Conversation field %q is exported", field.Name)
		}
	}

	pointerType := reflect.TypeFor[*Conversation]()
	send, ok := pointerType.MethodByName("Send")
	if !ok {
		t.Fatal("*Conversation has no exported Send method")
	}
	wantSend := reflect.TypeOf(func(*Conversation, context.Context, ...Block) *Stream { return nil })
	if send.Type != wantSend || !send.Type.IsVariadic() {
		t.Fatalf("Send type = %s (variadic=%t), want %s (variadic=true)", send.Type, send.Type.IsVariadic(), wantSend)
	}
	addSystem, ok := pointerType.MethodByName("AddSystem")
	if !ok {
		t.Fatal("*Conversation has no exported AddSystem method")
	}
	wantAddSystem := reflect.TypeOf(func(*Conversation, string) error { return nil })
	if addSystem.Type != wantAddSystem {
		t.Fatalf("AddSystem type = %s, want %s", addSystem.Type, wantAddSystem)
	}
	wantMethods := map[string]reflect.Type{
		"Close":     reflect.TypeOf(func(*Conversation) error { return nil }),
		"Release":   reflect.TypeOf(func(*Conversation, Savepoint) error { return nil }),
		"Restore":   reflect.TypeOf(func(*Conversation, Savepoint) error { return nil }),
		"Savepoint": reflect.TypeOf(func(*Conversation) (Savepoint, error) { return Savepoint{}, nil }),
	}
	for name, want := range wantMethods {
		method, exists := pointerType.MethodByName(name)
		if !exists {
			t.Fatalf("*Conversation has no exported %s method", name)
		}
		if method.Type != want {
			t.Fatalf("%s type = %s, want %s", name, method.Type, want)
		}
	}
	assertConversationExportsExactly(t)
}

func assertConversationExportsExactly(t *testing.T) {
	// R-8A00-BA9K
	t.Helper()
	pointerType := reflect.TypeFor[*Conversation]()
	want := []string{"AddSystem", "Close", "Release", "Restore", "Savepoint", "Send"}
	if pointerType.NumMethod() != len(want) {
		t.Fatalf("*Conversation has %d exported methods, want exactly %v", pointerType.NumMethod(), want)
	}
	for index, name := range want {
		if got := pointerType.Method(index).Name; got != name {
			t.Fatalf("*Conversation exported method %d = %s, want exact method set %v", index, got, want)
		}
	}
}

func TestSavepointIsOpaque(t *testing.T) {
	// R-7J67-WBYA
	savepointType := reflect.TypeFor[Savepoint]()
	if savepointType.Name() != "Savepoint" || !token.IsExported(savepointType.Name()) {
		t.Fatalf("Savepoint name = %q, want exported Savepoint type", savepointType.Name())
	}
	for index := range savepointType.NumField() {
		if field := savepointType.Field(index); field.IsExported() {
			t.Fatalf("Savepoint field %q is exported", field.Name)
		}
	}
	if savepointType.NumMethod() != 0 || reflect.PointerTo(savepointType).NumMethod() != 0 {
		t.Fatalf("Savepoint value/pointer exported methods = %d/%d, want 0/0", savepointType.NumMethod(), reflect.PointerTo(savepointType).NumMethod())
	}
}

func TestDeferredGroupDeclarationIsExact(t *testing.T) {
	// R-0PU1-L7QF
	groupType := reflect.TypeFor[DeferredGroup]()
	if groupType.Name() != "DeferredGroup" || !token.IsExported(groupType.Name()) || groupType.Kind() != reflect.Struct {
		t.Fatalf("DeferredGroup name/kind = %q/%s, want exported DeferredGroup struct", groupType.Name(), groupType.Kind())
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Name", typeOf: reflect.TypeFor[string]()},
		{name: "Blurb", typeOf: reflect.TypeFor[string]()},
		{name: "Tools", typeOf: reflect.TypeFor[[]Tool]()},
	}
	if groupType.NumField() != len(wantFields) {
		t.Fatalf("DeferredGroup field count = %d, want exactly %d", groupType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := groupType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf || !field.IsExported() {
			t.Fatalf("DeferredGroup field %d = %s %s (exported=%t), want %s %s (exported=true)", index, field.Name, field.Type, field.IsExported(), want.name, want.typeOf)
		}
	}
}

func TestIdentityPublicShape(t *testing.T) {
	// R-YVZG-XLOX
	identityType := reflect.TypeOf(Identity{})
	wantNames := []string{"Endpoint", "AuthMode", "Model"}
	if identityType.Kind() != reflect.Struct || identityType.NumField() != len(wantNames) {
		t.Fatalf("Identity kind/field count = %s/%d, want struct/%d", identityType.Kind(), identityType.NumField(), len(wantNames))
	}
	for index, wantName := range wantNames {
		field := identityType.Field(index)
		if field.Name != wantName || field.Type != reflect.TypeOf("") || !field.IsExported() {
			t.Fatalf("Identity field %d = %s %s (exported=%t), want %s string (exported=true)", index, field.Name, field.Type, field.IsExported(), wantName)
		}
	}
}

func TestCategoryDeclaration(t *testing.T) {
	// R-ZAM9-IUL9
	categoryType := reflect.TypeFor[Category]()
	if categoryType.Name() != "Category" || !token.IsExported(categoryType.Name()) || categoryType.Kind() != reflect.Int {
		t.Fatalf("Category name/kind = %q/%s, want exported defined Category with underlying int", categoryType.Name(), categoryType.Kind())
	}

	wantNames := []string{
		"CategoryUnknown",
		"CategoryAuth",
		"CategoryInvalidRequest",
		"CategoryRateLimit",
		"CategoryOverloaded",
		"CategoryInsufficientQuota",
		"CategoryTimeout",
		"CategoryTransport",
	}
	wantValues := []Category{0, 1, 2, 3, 4, 5, 6, 7}
	gotValues := []Category{
		CategoryUnknown,
		CategoryAuth,
		CategoryInvalidRequest,
		CategoryRateLimit,
		CategoryOverloaded,
		CategoryInsufficientQuota,
		CategoryTimeout,
		CategoryTransport,
	}
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("Category values = %v, want %v", gotValues, wantValues)
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundDefinedIntType := false
	var categoryConstantNames []string
	var iotaSequenceNames []string
	iotaDeclarations := 0
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		if general.Tok == token.TYPE {
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				underlying, isIdentifier := typeSpecification.Type.(*ast.Ident)
				if typeSpecification.Name.Name == "Category" && typeSpecification.Assign == token.NoPos && isIdentifier && underlying.Name == "int" {
					foundDefinedIntType = true
				}
			}
			continue
		}
		if general.Tok != token.CONST {
			continue
		}
		declarationNames := make([]string, 0)
		hasIota := false
		for _, specification := range general.Specs {
			valueSpecification := specification.(*ast.ValueSpec)
			for _, name := range valueSpecification.Names {
				if strings.HasPrefix(name.Name, "Category") {
					categoryConstantNames = append(categoryConstantNames, name.Name)
					declarationNames = append(declarationNames, name.Name)
				}
			}
			for _, value := range valueSpecification.Values {
				identifier, isIdentifier := value.(*ast.Ident)
				if isIdentifier && identifier.Name == "iota" {
					hasIota = true
				}
			}
		}
		if len(declarationNames) > 0 && hasIota {
			iotaDeclarations++
			iotaSequenceNames = declarationNames
		}
	}
	if !foundDefinedIntType {
		t.Fatal("Category is not declared as the defined type Category int")
	}
	if iotaDeclarations != 1 || !reflect.DeepEqual(iotaSequenceNames, wantNames) {
		t.Fatalf("Category iota declarations/names = %d/%v, want one declaration containing %v", iotaDeclarations, iotaSequenceNames, wantNames)
	}
	if !reflect.DeepEqual(categoryConstantNames, wantNames) {
		t.Fatalf("Category constants = %v, want exactly %v", categoryConstantNames, wantNames)
	}
}

func TestErrorDeclaration(t *testing.T) {
	// R-ZBU5-WMBY
	errorType := reflect.TypeFor[Error]()
	if errorType.Name() != "Error" || !token.IsExported(errorType.Name()) || errorType.Kind() != reflect.Struct {
		t.Fatalf("Error name/kind = %q/%s, want exported named Error struct", errorType.Name(), errorType.Kind())
	}
	wantFields := []struct {
		name     string
		typeOf   reflect.Type
		exported bool
	}{
		{name: "Category", typeOf: reflect.TypeFor[Category](), exported: true},
		{name: "Status", typeOf: reflect.TypeFor[int](), exported: true},
		{name: "Code", typeOf: reflect.TypeFor[string](), exported: true},
		{name: "Message", typeOf: reflect.TypeFor[string](), exported: true},
		{name: "RetryAfter", typeOf: reflect.TypeFor[time.Duration](), exported: true},
		{name: "Endpoint", typeOf: reflect.TypeFor[Identity](), exported: true},
		{name: "err", typeOf: reflect.TypeFor[error](), exported: false},
	}
	if errorType.NumField() != len(wantFields) {
		t.Fatalf("Error field count = %d, want exactly %d", errorType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := errorType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf || field.IsExported() != want.exported || field.Anonymous {
			t.Fatalf("Error field %d = %s %s (exported=%t, anonymous=%t), want %s %s (exported=%t, anonymous=false)", index, field.Name, field.Type, field.IsExported(), field.Anonymous, want.name, want.typeOf, want.exported)
		}
	}

	pointerType := reflect.TypeFor[*Error]()
	if !pointerType.Implements(reflect.TypeFor[error]()) {
		t.Fatal("*Error does not implement error")
	}
	if !pointerType.Implements(reflect.TypeFor[interface{ Unwrap() error }]()) {
		t.Fatal("*Error does not implement interface { Unwrap() error }")
	}
}

func TestRetryableDeclaration(t *testing.T) {
	// R-ZD22-AE2N
	got := reflect.TypeOf(Retryable)
	want := reflect.TypeOf(func(error) bool { return false })
	if got != want {
		t.Fatalf("Retryable type = %s, want exactly %s", got, want)
	}
}

func TestRoleDeclaration(t *testing.T) {
	// R-YX7D-BDFM
	roleType := reflect.TypeFor[Role]()
	if roleType.Name() != "Role" || roleType.Kind() != reflect.Int {
		t.Fatalf("Role name/kind = %q/%s, want defined Role with underlying int", roleType.Name(), roleType.Kind())
	}
	wantNames := []string{"RoleSystem", "RoleUser", "RoleAssistant", "RoleTool"}
	wantValues := []Role{0, 1, 2, 3}
	gotValues := []Role{RoleSystem, RoleUser, RoleAssistant, RoleTool}
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("Role values = %v, want %v", gotValues, wantValues)
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "message.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundDefinedIntType := false
	var roleConstantNames []string
	var iotaSequenceNames []string
	iotaDeclarations := 0
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		if general.Tok == token.TYPE {
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				underlying, isIdentifier := typeSpecification.Type.(*ast.Ident)
				if typeSpecification.Name.Name == "Role" && typeSpecification.Assign == token.NoPos && isIdentifier && underlying.Name == "int" {
					foundDefinedIntType = true
				}
			}
			continue
		}
		if general.Tok != token.CONST {
			continue
		}
		declarationNames := make([]string, 0)
		hasIota := false
		for _, specification := range general.Specs {
			valueSpecification := specification.(*ast.ValueSpec)
			for _, name := range valueSpecification.Names {
				if strings.HasPrefix(name.Name, "Role") {
					roleConstantNames = append(roleConstantNames, name.Name)
					declarationNames = append(declarationNames, name.Name)
				}
			}
			for _, value := range valueSpecification.Values {
				identifier, isIdentifier := value.(*ast.Ident)
				if isIdentifier && identifier.Name == "iota" {
					hasIota = true
				}
			}
		}
		if len(declarationNames) > 0 && hasIota {
			iotaDeclarations++
			iotaSequenceNames = declarationNames
		}
	}
	if !foundDefinedIntType {
		t.Fatal("Role is not declared as the defined type Role int")
	}
	if iotaDeclarations != 1 || !reflect.DeepEqual(iotaSequenceNames, wantNames) {
		t.Fatalf("Role iota declarations/names = %d/%v, want one declaration containing %v", iotaDeclarations, iotaSequenceNames, wantNames)
	}
	if !reflect.DeepEqual(roleConstantNames, wantNames) {
		t.Fatalf("Role constants = %v, want exactly %v", roleConstantNames, wantNames)
	}
}

func TestMessageDeclaration(t *testing.T) {
	// R-YYF9-P56B
	assertExactStructFields(t, reflect.TypeFor[Message](), []exactStructField{
		{name: "Role", typeOf: reflect.TypeFor[Role]()},
		{name: "Blocks", typeOf: reflect.TypeFor[[]Block]()},
	})
}

func TestHistoryDeclaration(t *testing.T) {
	// R-YZN6-2WX0
	historyType := reflect.TypeFor[History]()
	if historyType.Name() != "History" || historyType.Kind() != reflect.Slice || historyType.Elem() != reflect.TypeFor[Message]() {
		t.Fatalf("History name/kind/element = %q/%s/%s, want defined History slice of Message", historyType.Name(), historyType.Kind(), historyType.Elem())
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "history.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundDefinedSlice := false
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpecification := specification.(*ast.TypeSpec)
			arrayType, isArray := typeSpecification.Type.(*ast.ArrayType)
			element, isIdentifier := func() (*ast.Ident, bool) {
				if !isArray {
					return nil, false
				}
				identifier, ok := arrayType.Elt.(*ast.Ident)
				return identifier, ok
			}()
			if typeSpecification.Name.Name == "History" && typeSpecification.Assign == token.NoPos && isArray && arrayType.Len == nil && isIdentifier && element.Name == "Message" {
				foundDefinedSlice = true
			}
		}
	}
	if !foundDefinedSlice {
		t.Fatal("History is not declared as the defined slice type History []Message")
	}
}

func TestUsageDeclaration(t *testing.T) {
	// R-ND0W-8GRT
	assertExactStructFields(t, reflect.TypeFor[Usage](), []exactStructField{
		{name: "InputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CachedTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite5mTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite1hTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "OutputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "ReasoningTokens", typeOf: reflect.TypeFor[int64]()},
	})
}

func TestCostDeclarationIsExact(t *testing.T) {
	// R-NHWH-RJQL
	costType := reflect.TypeFor[Cost]()
	if costType.Name() != "Cost" || !token.IsExported(costType.Name()) || costType.Kind() != reflect.Int64 {
		t.Fatalf("Cost name/kind = %q/%s, want exported defined int64", costType.Name(), costType.Kind())
	}
	specification := declaredType(t, "cost.go", "Cost")
	if specification.Assign.IsValid() {
		t.Fatal("Cost is an alias, want a defined type")
	}
	identifier, ok := specification.Type.(*ast.Ident)
	if !ok || identifier.Name != "int64" || renderedNode(t, specification.Type) != "int64" {
		t.Fatalf("Cost declaration = %T %q, want exactly type Cost int64", specification.Type, renderedNode(t, specification.Type))
	}
}

func TestRateTierDeclarationIsExact(t *testing.T) {
	// R-NJ4E-5BHA
	assertExactStructFields(t, reflect.TypeFor[RateTier](), []exactStructField{
		{name: "MinInputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "InputUncached", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheReadInput", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite5m", typeOf: reflect.TypeFor[int64]()},
		{name: "CacheWrite1h", typeOf: reflect.TypeFor[int64]()},
		{name: "Output", typeOf: reflect.TypeFor[int64]()},
	})
}

func TestPricingDeclarationAndCostMethodAreExact(t *testing.T) {
	// R-NKCA-J37Z
	// R-NLK6-WUYO
	assertExactStructFields(t, reflect.TypeFor[Pricing](), []exactStructField{
		{name: "Tiers", typeOf: reflect.TypeFor[[]RateTier]()},
	})

	pricingType := reflect.TypeFor[Pricing]()
	method, ok := pricingType.MethodByName("Cost")
	wantMethod := reflect.TypeOf(func(Pricing, Usage) Cost { return 0 })
	if !ok || method.Type != wantMethod {
		t.Fatalf("Pricing.Cost = %v (present=%t), want exact value-receiver signature %s", method.Type, ok, wantMethod)
	}
}

func TestTextDeclaration(t *testing.T) {
	// R-Z0V2-GONP
	assertExactBlockStruct(t, reflect.TypeFor[Text](), []exactStructField{
		{name: "Text", typeOf: reflect.TypeFor[string]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestReasoningDeclaration(t *testing.T) {
	// R-Z22Y-UGEE
	assertExactBlockStruct(t, reflect.TypeFor[Reasoning](), []exactStructField{
		{name: "Text", typeOf: reflect.TypeFor[string]()},
		{name: "Redacted", typeOf: reflect.TypeFor[bool]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestToolUseDeclaration(t *testing.T) {
	// R-Z3AV-8853
	assertExactBlockStruct(t, reflect.TypeFor[ToolUse](), []exactStructField{
		{name: "ID", typeOf: reflect.TypeFor[string]()},
		{name: "Name", typeOf: reflect.TypeFor[string]()},
		{name: "Input", typeOf: reflect.TypeFor[json.RawMessage]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestRuntimeToolValidationIsOwnedOnlyByTheUnexportedOrchestrator(t *testing.T) {
	// R-4MJU-MJ5B
	// R-4F8G-BWP5
	gate := declaredFunction(t, "orchestrator.go", "validateToolSet")
	if gate.Name.IsExported() || gate.Recv != nil || renderedNode(t, gate.Type) != "func(tools []Tool) error" {
		t.Fatalf("validateToolSet declaration = %s receiver=%v exported=%t", renderedNode(t, gate.Type), gate.Recv != nil, gate.Name.IsExported())
	}
	gateCallsSchemaValidator := false
	ast.Inspect(gate.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		identifier, direct := func() (*ast.Ident, bool) {
			if !ok {
				return nil, false
			}
			identifier, direct := call.Fun.(*ast.Ident)
			return identifier, direct
		}()
		if direct && identifier.Name == "ValidateToolSchema" {
			gateCallsSchemaValidator = true
		}
		return true
	})
	if !gateCallsSchemaValidator {
		t.Fatal("validateToolSet does not call ValidateToolSchema")
	}

	dispatch := declaredFunction(t, "orchestrator.go", "dispatch")
	if dispatch.Name.IsExported() || dispatch.Recv == nil || renderedNode(t, dispatch.Type) != "func(ctx context.Context, call ToolUse, savepointLive bool) ToolResult" || renderedNode(t, dispatch.Recv.List[0].Type) != "*orchestrator" {
		t.Fatalf("dispatch declaration = receiver %s type %s", renderedNode(t, dispatch.Recv.List[0].Type), renderedNode(t, dispatch.Type))
	}

	validatorCalls := 0
	toolCalls := 0
	validatorDeclarations := 0
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == "validateToolArguments" {
				validatorDeclarations++
				if function.Name.IsExported() {
					t.Errorf("argument validator %s is exported", function.Name.Name)
				}
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch called := call.Fun.(type) {
			case *ast.Ident:
				if called.Name == "validateToolArguments" {
					validatorCalls++
					if filepath.Base(path) != "orchestrator.go" {
						t.Errorf("argument validator called outside orchestrator.go: %s", path)
					}
				}
			case *ast.SelectorExpr:
				if called.Sel.Name == "Call" {
					toolCalls++
					if filepath.Base(path) != "orchestrator.go" {
						t.Errorf("runtime Tool.Call outside orchestrator.go: %s", path)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if validatorDeclarations != 1 || validatorCalls != 1 || toolCalls != 1 {
		t.Fatalf("runtime seam counts: validator declarations=%d calls=%d Tool.Call=%d, want 1/1/1", validatorDeclarations, validatorCalls, toolCalls)
	}
}

func TestToolResultDeclaration(t *testing.T) {
	// R-Z4IR-LZVS
	assertExactBlockStruct(t, reflect.TypeFor[ToolResult](), []exactStructField{
		{name: "ToolUseID", typeOf: reflect.TypeFor[string]()},
		{name: "Content", typeOf: reflect.TypeFor[string]()},
		{name: "IsError", typeOf: reflect.TypeFor[bool]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
}

func TestReasoningModeDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-ZWKG-EPXR
	want := []ReasoningMode{0, 1, 2, 3, 4}
	got := []ReasoningMode{ReasoningDefault, ReasoningOff, ReasoningOn, ReasoningEffort, ReasoningBudget}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReasoningMode values = %v, want %v", got, want)
	}
	assertDefinedIntTypedIota(t, "settings.go", "ReasoningMode", []string{
		"ReasoningDefault", "ReasoningOff", "ReasoningOn", "ReasoningEffort", "ReasoningBudget",
	})
}

func TestEffortDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-NU3H-L95J
	want := []Effort{0, 1, 2, 3, 4, 5, 6}
	got := []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effort values = %v, want %v", got, want)
	}
	assertDefinedIntTypedIota(t, "settings.go", "Effort", []string{
		"EffortNone", "EffortMinimal", "EffortLow", "EffortMedium", "EffortHigh", "EffortXHigh", "EffortMax",
	})
}

func TestReasoningConfigDeclarationHasExactNeutralReasoningFields(t *testing.T) {
	// R-ZXSC-SHOG
	assertExactStructFields(t, reflect.TypeFor[ReasoningConfig](), []exactStructField{
		{name: "Mode", typeOf: reflect.TypeFor[ReasoningMode]()},
		{name: "Effort", typeOf: reflect.TypeFor[Effort]()},
		{name: "Budget", typeOf: reflect.TypeFor[int]()},
	})
}

func TestToolChoiceDeclarationHasExactNeutralSelectionFields(t *testing.T) {
	// R-0085-K15U
	assertExactStructFields(t, reflect.TypeFor[ToolChoice](), []exactStructField{
		{name: "Mode", typeOf: reflect.TypeFor[ToolChoiceMode]()},
		{name: "Name", typeOf: reflect.TypeFor[string]()},
	})
}

func TestToolChoiceModeDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-01G1-XSWJ
	want := []ToolChoiceMode{0, 1, 2, 3}
	got := []ToolChoiceMode{ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceTool}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolChoiceMode values = %v, want %v", got, want)
	}
	assertDefinedIntTypedIota(t, "settings.go", "ToolChoiceMode", []string{
		"ToolChoiceAuto", "ToolChoiceNone", "ToolChoiceRequired", "ToolChoiceTool",
	})
}

func assertDefinedIntTypedIota(t *testing.T, filename, typeName string, wantNames []string) {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	foundDefinedInt := false
	typedGroups := make([][]string, 0)
	typedSpecifications := 0
	typedSpecificationUsesIota := false
	targetSpecificationsAreImplicitAfterFirst := true
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		if general.Tok == token.TYPE {
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				underlying, isIdentifier := typeSpecification.Type.(*ast.Ident)
				if typeSpecification.Name.Name == typeName && typeSpecification.Assign == token.NoPos && isIdentifier && underlying.Name == "int" {
					foundDefinedInt = true
				}
			}
			continue
		}
		if general.Tok != token.CONST {
			continue
		}

		groupType := ""
		groupNames := make([]string, 0)
		for _, specification := range general.Specs {
			valueSpecification := specification.(*ast.ValueSpec)
			if explicitType, ok := valueSpecification.Type.(*ast.Ident); ok {
				groupType = explicitType.Name
				if groupType == typeName {
					typedSpecifications++
					typedSpecificationUsesIota = len(valueSpecification.Values) == 1 && expressionName(valueSpecification.Values[0]) == "iota"
				}
			}
			if groupType != typeName {
				continue
			}
			if len(groupNames) > 0 && (valueSpecification.Type != nil || len(valueSpecification.Values) != 0) {
				targetSpecificationsAreImplicitAfterFirst = false
			}
			for _, name := range valueSpecification.Names {
				groupNames = append(groupNames, name.Name)
			}
		}
		if len(groupNames) > 0 {
			typedGroups = append(typedGroups, groupNames)
		}
	}

	if !foundDefinedInt {
		t.Fatalf("%s is not declared as the defined type %s int", typeName, typeName)
	}
	if len(typedGroups) != 1 || typedSpecifications != 1 || !typedSpecificationUsesIota || !targetSpecificationsAreImplicitAfterFirst || !reflect.DeepEqual(typedGroups[0], wantNames) {
		t.Fatalf("%s typed iota declaration = groups %v, typed specs %d, starts with iota %t, implicit continuation %t; want exactly one typed iota group %v", typeName, typedGroups, typedSpecifications, typedSpecificationUsesIota, targetSpecificationsAreImplicitAfterFirst, wantNames)
	}
}

type exactStructField struct {
	name   string
	typeOf reflect.Type
}

func assertExactBlockStruct(t *testing.T, got reflect.Type, want []exactStructField) {
	t.Helper()
	assertExactStructFields(t, got, want)
	if !got.Implements(reflect.TypeFor[Block]()) {
		t.Fatalf("%s does not implement Block as a value", got)
	}
}

func assertExactStructFields(t *testing.T, got reflect.Type, want []exactStructField) {
	t.Helper()
	if got.Name() == "" || !token.IsExported(got.Name()) {
		t.Fatalf("%s name = %q, want exported named type", got, got.Name())
	}
	if got.Kind() != reflect.Struct || got.NumField() != len(want) {
		t.Fatalf("%s kind/field count = %s/%d, want struct/%d", got, got.Kind(), got.NumField(), len(want))
	}
	for index, wantField := range want {
		field := got.Field(index)
		if field.Name != wantField.name || field.Type != wantField.typeOf || !field.IsExported() || field.Anonymous {
			t.Fatalf("%s field %d = %s %s (exported=%t, anonymous=%t), want %s %s (exported=true, anonymous=false)", got, index, field.Name, field.Type, field.IsExported(), field.Anonymous, wantField.name, wantField.typeOf)
		}
	}
}

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

func TestSiblingContractWithholdsRootOwnedMechanismsAndKeepsOptionsLocal(t *testing.T) {
	// R-65FB-U7IK
	for _, filename := range []string{"tool.go", "orchestrator.go", "sse.go", "errors.go", "cost.go", "usage.go", "identity.go", "endpoint.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Name.IsExported() && declaration.Name.Name == "ValidateToolArguments" {
					t.Fatalf("root exports prohibited function %s", declaration.Name.Name)
				}
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					var names []*ast.Ident
					switch specification := specification.(type) {
					case *ast.TypeSpec:
						names = []*ast.Ident{specification.Name}
					case *ast.ValueSpec:
						names = specification.Names
					}
					for _, name := range names {
						if !name.IsExported() {
							continue
						}
						lower := strings.ToLower(name.Name)
						if name.Name == "Warning" || strings.Contains(lower, "outputcap") || strings.Contains(lower, "tooloutputlimit") {
							t.Fatalf("root exports prohibited policy symbol %s", name.Name)
						}
					}
				}
			}
		}
	}

}

// R-NZTR-FBVP
// R-1OL8-V3X0
func TestRootPackageExportsNoRetiredProviderMachinery(t *testing.T) {
	assertRootPackageDeclaresNone(t, map[string]bool{
		"NewConversation": true,
		"NewForWire":      true,
		"Provider":        true,
		"Known" + "Wire":  true,
		"RequestState":    true,
		"RequestMutator":  true,
		"ErrorClassifier": true,
		"WithHeader":      true,
		"WithFramer":      true,
		"WithClassifier":  true,
		"WithMutator":     true,
		"WithHTTPClient":  true,
		"ProviderOptions": true,
	})
}

func assertRootPackageDeclaresNone(t *testing.T, forbidden map[string]bool) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range parsed.Decls {
			var names []*ast.Ident
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv == nil {
					names = []*ast.Ident{declaration.Name}
				}
			case *ast.GenDecl:
				for _, raw := range declaration.Specs {
					switch specification := raw.(type) {
					case *ast.TypeSpec:
						names = append(names, specification.Name)
					case *ast.ValueSpec:
						names = append(names, specification.Names...)
					}
				}
			}
			for _, name := range names {
				if forbidden[name.Name] {
					t.Fatalf("forbidden exported identifier %s found in %s", name.Name, entry.Name())
				}
			}
		}
	}
}

type packageDeclarations struct {
	types        map[string]*ast.TypeSpec
	functions    map[string]*ast.FuncDecl
	apiConstants []string
	apiUsesIota  bool
}

func parsePackageDeclarations(t *testing.T, directory string) packageDeclarations {
	t.Helper()
	declarations := packageDeclarations{types: make(map[string]*ast.TypeSpec), functions: make(map[string]*ast.FuncDecl)}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range parsed.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv == nil {
					declarations.functions[declaration.Name.Name] = declaration
				}
			case *ast.GenDecl:
				apiConstantDeclaration := false
				for _, raw := range declaration.Specs {
					switch specification := raw.(type) {
					case *ast.TypeSpec:
						declarations.types[specification.Name.Name] = specification
					case *ast.ValueSpec:
						if declaration.Tok != token.CONST {
							continue
						}
						apiConstantDeclaration = apiConstantDeclaration || expressionName(specification.Type) == "API"
						for _, name := range specification.Names {
							if apiConstantDeclaration {
								declarations.apiConstants = append(declarations.apiConstants, name.Name)
							}
						}
						for _, value := range specification.Values {
							declarations.apiUsesIota = declarations.apiUsesIota || expressionName(value) == "iota"
						}
					}
				}
			}
		}
	}
	return declarations
}

func interfaceMethodNames(interfaceType *ast.InterfaceType) []string {
	names := make([]string, 0, len(interfaceType.Methods.List))
	for _, field := range interfaceType.Methods.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func assertASTMethod(t *testing.T, interfaceType *ast.InterfaceType, name string, inputs, outputs []string) {
	t.Helper()
	for _, field := range interfaceType.Methods.List {
		if len(field.Names) == 1 && field.Names[0].Name == name {
			function, ok := field.Type.(*ast.FuncType)
			if !ok {
				t.Fatalf("%s is not a method", name)
			}
			assertASTSignature(t, name, function, inputs, outputs, false)
			return
		}
	}
	t.Fatalf("method %s is missing", name)
}

func assertASTFunction(t *testing.T, declarations packageDeclarations, name string, inputs, outputs []string, variadic bool) {
	t.Helper()
	function, exists := declarations.functions[name]
	if !exists {
		t.Fatalf("function %s is missing", name)
	}
	assertASTSignature(t, name, function.Type, inputs, outputs, variadic)
}

func assertASTSignature(t *testing.T, name string, function *ast.FuncType, inputs, outputs []string, variadic bool) {
	t.Helper()
	gotInputs := fieldTypeNames(function.Params)
	gotOutputs := fieldTypeNames(function.Results)
	gotVariadic := len(function.Params.List) > 0 && strings.HasPrefix(expressionName(function.Params.List[len(function.Params.List)-1].Type), "...")
	if !reflect.DeepEqual(gotInputs, inputs) || !reflect.DeepEqual(gotOutputs, outputs) || gotVariadic != variadic {
		t.Fatalf("%s signature = (%v) (%v), variadic=%v; want (%v) (%v), variadic=%v", name, gotInputs, gotOutputs, gotVariadic, inputs, outputs, variadic)
	}
}

func fieldTypeNames(fields *ast.FieldList) []string {
	if fields == nil || len(fields.List) == 0 {
		return nil
	}
	result := make([]string, 0)
	for _, field := range fields.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			result = append(result, expressionName(field.Type))
		}
	}
	return result
}

func expressionName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		return expressionName(expression.X) + "." + expression.Sel.Name
	case *ast.StarExpr:
		return "*" + expressionName(expression.X)
	case *ast.ArrayType:
		return "[]" + expressionName(expression.Elt)
	case *ast.Ellipsis:
		return "..." + expressionName(expression.Elt)
	case *ast.IndexExpr:
		return expressionName(expression.X) + "[" + expressionName(expression.Index) + "]"
	case *ast.IndexListExpr:
		indices := make([]string, len(expression.Indices))
		for index, argument := range expression.Indices {
			indices[index] = expressionName(argument)
		}
		return expressionName(expression.X) + "[" + strings.Join(indices, ", ") + "]"
	default:
		return ""
	}
}
