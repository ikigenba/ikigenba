package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"iter"
	"net/http"
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
)

func TestEndpointDeclarationsAreExact(t *testing.T) {
	// R-YEPA-QILV
	// R-ZKDG-L0IT
	// R-YFX7-4ACK
	// R-ZMT9-CK07
	// R-ZO15-QBQW
	// R-ZP92-43HL
	// R-YICZ-VTTY
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

	optionType := reflect.TypeFor[EndpointOption]()
	configPointer := reflect.TypeFor[*endpointConfig]()
	errorType := reflect.TypeFor[error]()
	if optionType.Name() != "EndpointOption" || optionType.Kind() != reflect.Func || optionType.NumIn() != 1 || optionType.In(0) != configPointer || optionType.NumOut() != 1 || optionType.Out(0) != errorType {
		t.Fatalf("EndpointOption = %s, want defined func(*endpointConfig) error", optionType)
	}
	if specification := declaredType(t, "endpoint.go", "EndpointOption"); specification.Assign.IsValid() {
		t.Fatal("EndpointOption is an alias")
	}

	assertDefinedEndpointType(t, "AuthApplier", reflect.TypeFor[AuthApplier](), reflect.Interface)
	authType := reflect.TypeFor[AuthApplier]()
	wantApply := reflect.TypeOf(func(context.Context, *http.Request, []byte) error { return nil })
	if authType.NumMethod() != 1 {
		t.Fatalf("AuthApplier method count = %d, want 1", authType.NumMethod())
	}
	apply, ok := authType.MethodByName("Apply")
	if !ok || apply.Type != wantApply {
		t.Fatalf("AuthApplier.Apply = %v (present=%t), want %s", apply.Type, ok, wantApply)
	}

	assertDefinedEndpointType(t, "RequestMutator", reflect.TypeFor[RequestMutator](), reflect.Func)
	assertFunctionSignature(t, reflect.TypeFor[RequestMutator](), reflect.TypeOf(func(*http.Request, *[]byte) error { return nil }))
	assertDefinedEndpointType(t, "ErrorClassifier", reflect.TypeFor[ErrorClassifier](), reflect.Func)
	assertFunctionSignature(t, reflect.TypeFor[ErrorClassifier](), reflect.TypeOf(func(int, http.Header, []byte) error { return nil }))

	constructor := reflect.TypeOf(NewEndpoint)
	wantConstructor := reflect.TypeOf(func(string, AuthApplier, ...EndpointOption) (Endpoint, error) { return Endpoint{}, nil })
	if constructor != wantConstructor || !constructor.IsVariadic() {
		t.Fatalf("NewEndpoint = %s variadic=%t, want %s variadic", constructor, constructor.IsVariadic(), wantConstructor)
	}
	optionFunctions := map[string]reflect.Type{
		"WithHeader":     reflect.TypeOf(func(string, string) EndpointOption { return nil }),
		"WithFramer":     reflect.TypeOf(func(Framer) EndpointOption { return nil }),
		"WithClassifier": reflect.TypeOf(func(ErrorClassifier) EndpointOption { return nil }),
		"WithMutator":    reflect.TypeOf(func(RequestMutator) EndpointOption { return nil }),
		"WithHTTPClient": reflect.TypeOf(func(*http.Client) EndpointOption { return nil }),
	}
	actualFunctions := map[string]reflect.Type{
		"WithHeader": reflect.TypeOf(WithHeader), "WithFramer": reflect.TypeOf(WithFramer),
		"WithClassifier": reflect.TypeOf(WithClassifier), "WithMutator": reflect.TypeOf(WithMutator),
		"WithHTTPClient": reflect.TypeOf(WithHTTPClient),
	}
	for name, want := range optionFunctions {
		if actualFunctions[name] != want {
			t.Fatalf("%s = %s, want %s", name, actualFunctions[name], want)
		}
	}
}

func TestProviderDeclarationIsExact(t *testing.T) {
	// R-ZQGY-HV8A
	providerType := reflect.TypeFor[Provider]()
	if providerType.Name() != "Provider" || providerType.Kind() != reflect.Interface || providerType.NumMethod() != 4 {
		t.Fatalf("Provider = %q/%s with %d methods", providerType.Name(), providerType.Kind(), providerType.NumMethod())
	}
	want := map[string]reflect.Type{
		"BuildRequest": reflect.TypeOf(func(context.Context, RequestState) (*http.Request, error) { return nil, nil }),
		"Decode":       reflect.TypeOf(func(context.Context, *http.Response) iter.Seq2[Event, error] { return nil }),
		"Classify":     reflect.TypeOf(func(int, http.Header, []byte) error { return nil }),
		"Identity":     reflect.TypeOf(func() Identity { return Identity{} }),
	}
	for name, signature := range want {
		method, ok := providerType.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("Provider.%s = %v (present=%t), want %s", name, method.Type, ok, signature)
		}
	}
	specification := declaredType(t, "provider.go", "Provider")
	if specification.Assign.IsValid() {
		t.Fatal("Provider is an alias")
	}
	interfaceType, ok := specification.Type.(*ast.InterfaceType)
	if !ok || len(interfaceType.Methods.List) != 4 {
		t.Fatalf("Provider declaration = %T with %d fields", specification.Type, interfaceFieldCount(interfaceType))
	}
}

func TestProviderOptionsDeclarationIsExact(t *testing.T) {
	// R-08RG-8FCP
	optionsType := reflect.TypeFor[ProviderOptions]()
	if optionsType.Name() != "ProviderOptions" || !token.IsExported(optionsType.Name()) || optionsType.Kind() != reflect.Map {
		t.Fatalf("ProviderOptions name/kind = %q/%s, want exported defined map", optionsType.Name(), optionsType.Kind())
	}
	if optionsType.Key() != reflect.TypeFor[string]() || optionsType.Elem() != reflect.TypeFor[json.RawMessage]() {
		t.Fatalf("ProviderOptions = map[%s]%s, want exactly map[string]json.RawMessage", optionsType.Key(), optionsType.Elem())
	}
	specification := declaredType(t, "agentkit.go", "ProviderOptions")
	if specification.Assign.IsValid() {
		t.Fatal("ProviderOptions is an alias, want a defined map type")
	}
	mapType, ok := specification.Type.(*ast.MapType)
	if !ok {
		t.Fatalf("ProviderOptions declaration is %T, want map type", specification.Type)
	}
	if got := renderedNode(t, mapType); got != "map[string]json.RawMessage" {
		t.Fatalf("ProviderOptions declaration = %q, want exactly %q", got, "map[string]json.RawMessage")
	}
}

func TestConfigDeclarationIsExact(t *testing.T) {
	// R-SKM2-6G5E
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
		{name: "Options", typeOf: reflect.TypeFor[ProviderOptions]()},
		{name: "Output", typeOf: reflect.TypeFor[*OutputContract]()},
		{name: "Log", typeOf: reflect.TypeFor[*Log]()},
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
	if got := renderedNode(t, structType); got != "struct {\n\tTools    []Tool\n\tDeferred []DeferredGroup\n\tSettings Settings\n\tOptions  ProviderOptions\n\tOutput   *OutputContract\n\tLog      *Log\n}" {
		t.Fatalf("Config declaration = %q, want exact six-field declaration", got)
	}
}

func TestRequestStateDeclarationIsExact(t *testing.T) {
	// R-09ZC-M73E
	stateType := reflect.TypeFor[RequestState]()
	if stateType.Name() != "RequestState" || !token.IsExported(stateType.Name()) || stateType.Kind() != reflect.Struct {
		t.Fatalf("RequestState name/kind = %q/%s, want exported defined struct", stateType.Name(), stateType.Kind())
	}
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Model", typeOf: reflect.TypeFor[string]()},
		{name: "History", typeOf: reflect.TypeFor[[]Message]()},
		{name: "Settings", typeOf: reflect.TypeFor[Settings]()},
		{name: "Options", typeOf: reflect.TypeFor[ProviderOptions]()},
		{name: "Tools", typeOf: reflect.TypeFor[[]Tool]()},
	}
	if stateType.NumField() != len(wantFields) {
		t.Fatalf("RequestState field count = %d, want exactly %d", stateType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		field := stateType.Field(index)
		if field.Name != want.name || field.Type != want.typeOf || !field.IsExported() {
			t.Fatalf("RequestState field %d = %s %s (exported=%t), want %s %s exported", index, field.Name, field.Type, field.IsExported(), want.name, want.typeOf)
		}
	}
	historyType := stateType.Field(1).Type
	if historyType.Name() != "" || historyType == reflect.TypeFor[History]() {
		t.Fatalf("RequestState.History = %s (name %q), want unnamed []Message and not defined History", historyType, historyType.Name())
	}

	specification := declaredType(t, "agentkit.go", "RequestState")
	if specification.Assign.IsValid() {
		t.Fatal("RequestState is an alias, want a defined struct")
	}
	structType, ok := specification.Type.(*ast.StructType)
	if !ok {
		t.Fatalf("RequestState declaration is %T, want struct", specification.Type)
	}
	if got := renderedNode(t, structType); got != "struct {\n\tModel    string\n\tHistory  []Message\n\tSettings Settings\n\tOptions  ProviderOptions\n\tTools    []Tool\n}" {
		t.Fatalf("RequestState declaration = %q, want exact five-field declaration", got)
	}
}

func TestMessageDoneDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0B78-ZYU3
	assertEventWrapper(t, "MessageDone", reflect.TypeFor[MessageDone](), "Message", reflect.TypeFor[Message]())
	assertEventSeam(t)
}

func TestEventIsSealedToExactlyThreeVariants(t *testing.T) {
	// R-4YQU-G8K9
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
	want := []string{"MessageDone", "ToolCall", "ToolReturn"}
	if !reflect.DeepEqual(implementations, want) {
		t.Fatalf("in-package Event implementations = %v, want exactly %v", implementations, want)
	}
}

func TestToolCallDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0CF5-DQKS
	assertEventWrapper(t, "ToolCall", reflect.TypeFor[ToolCall](), "Use", reflect.TypeFor[ToolUse]())
}

func TestToolReturnDeclarationIsExactAndImplementsEvent(t *testing.T) {
	// R-0DN1-RIBH
	assertEventWrapper(t, "ToolReturn", reflect.TypeFor[ToolReturn](), "Result", reflect.TypeFor[ToolResult]())
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
		"Events": reflect.TypeOf((*Stream).Events),
		"Err":    reflect.TypeOf((*Stream).Err),
	}
	if pointerType.NumMethod() != len(wantMethods) {
		t.Fatalf("*Stream exported method count = %d, want exactly %d", pointerType.NumMethod(), len(wantMethods))
	}
	wantEvents := reflect.TypeOf(func(*Stream) iter.Seq[Event] { return nil })
	wantErr := reflect.TypeOf(func(*Stream) error { return nil })
	if wantMethods["Events"] != wantEvents || wantMethods["Err"] != wantErr {
		t.Fatalf("Stream methods = Events %s, Err %s; want %s and %s", wantMethods["Events"], wantMethods["Err"], wantEvents, wantErr)
	}
	for name, signature := range wantMethods {
		method, ok := pointerType.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("Stream.%s = %v (present=%t), want %s", name, method.Type, ok, signature)
		}
	}
}

func assertEventWrapper(t *testing.T, name string, wrapper reflect.Type, fieldName string, fieldType reflect.Type) {
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
	specification := declaredType(t, "agentkit.go", name)
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

func TestConversationConstructorDeclarationsAreExact(t *testing.T) {
	// R-SLTY-K7W3
	// R-SN1U-XZMS
	newForWire := reflect.TypeOf(NewForWire)
	wantNewForWire := reflect.TypeOf(func(KnownWire, Endpoint, string, Config) (*Conversation, error) { return nil, nil })
	if newForWire != wantNewForWire || newForWire.IsVariadic() {
		t.Fatalf("NewForWire = %s variadic=%t, want exactly %s non-variadic", newForWire, newForWire.IsVariadic(), wantNewForWire)
	}
	newConversation := reflect.TypeOf(NewConversation)
	wantNewConversation := reflect.TypeOf(func(Provider, *http.Client, Config) *Conversation { return nil })
	if newConversation != wantNewConversation || newConversation.IsVariadic() {
		t.Fatalf("NewConversation = %s variadic=%t, want exactly %s non-variadic", newConversation, newConversation.IsVariadic(), wantNewConversation)
	}
	for _, declaration := range []struct {
		filename string
		name     string
		params   int
		results  int
	}{
		{filename: "provider.go", name: "NewForWire", params: 4, results: 2},
		{filename: "conversation.go", name: "NewConversation", params: 3, results: 1},
	} {
		function := declaredFunction(t, declaration.filename, declaration.name)
		if function.Recv != nil || function.Type.Params.NumFields() != declaration.params || function.Type.Results.NumFields() != declaration.results {
			t.Fatalf("%s AST has receiver=%v params=%d results=%d, want package function with %d params and %d results", declaration.name, function.Recv != nil, function.Type.Params.NumFields(), function.Type.Results.NumFields(), declaration.params, declaration.results)
		}
		for _, parameter := range function.Type.Params.List {
			if _, variadic := parameter.Type.(*ast.Ellipsis); variadic {
				t.Fatalf("%s AST contains a variadic parameter", declaration.name)
			}
		}
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "provider.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	exportedFunctions := make(map[string]bool)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.IsExported() {
			exportedFunctions[function.Name.Name] = true
		}
	}
	if !exportedFunctions["NewForWire"] {
		t.Fatal("NewForWire is not an exported package function")
	}
	for _, obsolete := range []string{"NewKnownWireConversation", "NewKnownWireModelConversation"} {
		if exportedFunctions[obsolete] {
			t.Fatalf("obsolete exported constructor %s remains declared", obsolete)
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

func TestWireFormatDeclarationIsExact(t *testing.T) {
	// R-YC9H-YZ4H
	wireType := reflect.TypeFor[WireFormat]()
	if wireType.Name() != "WireFormat" || !token.IsExported(wireType.Name()) || wireType.Kind() != reflect.Interface {
		t.Fatalf("WireFormat name/kind = %q/%s, want exported named interface", wireType.Name(), wireType.Kind())
	}
	wantMethods := []struct {
		name   string
		typeOf reflect.Type
	}{
		{"EncodeRequest", reflect.TypeOf(func(RequestState) ([]byte, error) { return nil, nil })},
		{"DecodeStream", reflect.TypeOf(func(iter.Seq2[[]byte, error]) iter.Seq2[Event, error] { return nil })},
		{"RenderTools", reflect.TypeOf(func([]Tool) (json.RawMessage, error) { return nil, nil })},
		{"ReservedKeys", reflect.TypeOf(func() []string { return nil })},
	}
	if wireType.NumMethod() != len(wantMethods) {
		t.Fatalf("WireFormat method count = %d, want exactly %d", wireType.NumMethod(), len(wantMethods))
	}
	for _, want := range wantMethods {
		method, ok := wireType.MethodByName(want.name)
		if !ok || method.Type != want.typeOf {
			t.Fatalf("WireFormat.%s type = %v (present=%t), want exactly %s", want.name, method.Type, ok, want.typeOf)
		}
	}

	typeSpecification := declaredType(t, "wire.go", "WireFormat")
	if typeSpecification.Assign.IsValid() {
		t.Fatal("WireFormat is an alias, want a defined interface type")
	}
	interfaceType, ok := typeSpecification.Type.(*ast.InterfaceType)
	if !ok || len(interfaceType.Methods.List) != len(wantMethods) {
		t.Fatalf("WireFormat declaration = %T with %d fields, want interface with four explicit methods", typeSpecification.Type, interfaceFieldCount(interfaceType))
	}
	for index, field := range interfaceType.Methods.List {
		if len(field.Names) != 1 || field.Names[0].Name != wantMethods[index].name {
			t.Fatalf("WireFormat declaration field %d names = %v, want explicit method %s in order", index, field.Names, wantMethods[index].name)
		}
		if _, ok := field.Type.(*ast.FuncType); !ok {
			t.Fatalf("WireFormat.%s declaration is %T, want method function", wantMethods[index].name, field.Type)
		}
	}
}

func TestToolDeclarationIsExactAndSealed(t *testing.T) {
	// R-02NY-BKN8
	toolType := reflect.TypeFor[Tool]()
	if toolType.Name() != "Tool" || !token.IsExported(toolType.Name()) || toolType.Kind() != reflect.Interface {
		t.Fatalf("Tool name/kind = %q/%s, want exported named interface", toolType.Name(), toolType.Kind())
	}
	wantExported := map[string]reflect.Type{
		"Name":        reflect.TypeOf(func() string { return "" }),
		"Description": reflect.TypeOf(func() string { return "" }),
		"Schema":      reflect.TypeOf(func() json.RawMessage { return nil }),
		"Call":        reflect.TypeOf(func(context.Context, json.RawMessage) (string, error) { return "", nil }),
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
	wantMethods := []string{"Name", "Description", "Schema", "Call", "isTool"}
	if got := interfaceMethodNames(interfaceType); !reflect.DeepEqual(got, wantMethods) {
		t.Fatalf("Tool methods = %v, want exactly %v in order", got, wantMethods)
	}
	assertASTMethod(t, interfaceType, "Name", nil, []string{"string"})
	assertASTMethod(t, interfaceType, "Description", nil, []string{"string"})
	assertASTMethod(t, interfaceType, "Schema", nil, []string{"json.RawMessage"})
	assertASTMethod(t, interfaceType, "Call", []string{"context.Context", "json.RawMessage"}, []string{"string", "error"})
	assertASTMethod(t, interfaceType, "isTool", nil, nil)
	if interfaceType.Methods.List[4].Names[0].IsExported() {
		t.Fatal("Tool marker is exported, want package-sealing lowercase marker")
	}
}

func TestSiblingToolConstructionSurfaceIsExactAndSealed(t *testing.T) {
	// R-5ZBT-XCT3
	toolType := reflect.TypeFor[Tool]()
	if toolType.Kind() != reflect.Interface || toolType.NumMethod() != 5 {
		t.Fatalf("Tool = %s with %d methods, want sealed five-method interface", toolType, toolType.NumMethod())
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
	assertExactToolFunctionDeclaration(t, "NewTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) (Tool, error)")
	assertExactToolFunctionDeclaration(t, "MustTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) Tool")
	assertExactToolFunctionDeclaration(t, "NewToolFromSchema", "func(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error)) (Tool, error)")
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
	for _, filename := range []string{"wire_openai_responses.go", "wire_openai_chat.go", "wire_anthropic.go", "wire_gemini.go"} {
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
			filename:       "wire_anthropic.go",
			receiver:       "anthropicWire",
			renderer:       "renderAnthropicTools",
			wrapperMarkers: []string{"json:\"name\"", "json:\"description\"", "json:\"input_schema\""},
			forbidden:      []string{"json:\"type\"", "json:\"parameters\"", "json:\"functionDeclarations\""},
		},
		{
			filename:       "wire_gemini.go",
			receiver:       "geminiWire",
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
	// R-03VU-PCDX
	got := reflect.TypeOf(NewTool[architectureToolInput])
	want := reflect.TypeOf(func(string, string, func(context.Context, architectureToolInput) (string, error)) (Tool, error) {
		return nil, nil
	})
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "NewTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) (Tool, error)")
}

func TestMustToolDeclarationIsExact(t *testing.T) {
	// R-053R-344M
	got := reflect.TypeOf(MustTool[architectureToolInput])
	want := reflect.TypeOf(func(string, string, func(context.Context, architectureToolInput) (string, error)) Tool { return nil })
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "MustTool", "func[In any](name, description string, fn func(ctx context.Context, in In) (string, error)) Tool")
}

func TestNewToolFromSchemaDeclarationIsExact(t *testing.T) {
	// R-06BN-GVVB
	got := reflect.TypeOf(NewToolFromSchema)
	want := reflect.TypeOf(func(string, string, json.RawMessage, func(context.Context, json.RawMessage) (string, error)) (Tool, error) {
		return nil, nil
	})
	assertFunctionSignature(t, got, want)
	assertExactToolFunctionDeclaration(t, "NewToolFromSchema", "func(name, description string, schema json.RawMessage, fn func(ctx context.Context, args json.RawMessage) (string, error)) (Tool, error)")
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

func interfaceFieldCount(interfaceType *ast.InterfaceType) int {
	if interfaceType == nil {
		return 0
	}
	return len(interfaceType.Methods.List)
}

func TestConversationPublicShape(t *testing.T) {
	// R-YURK-JTY8
	// R-SPHN-PJ46
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
	wantMethods := []string{"Send"}
	if pointerType.NumMethod() != len(wantMethods) {
		t.Fatalf("*Conversation exported method count = %d, want exactly %d", pointerType.NumMethod(), len(wantMethods))
	}
	for index, wantName := range wantMethods {
		if got := pointerType.Method(index).Name; got != wantName {
			t.Fatalf("*Conversation method %d = %q, want %q", index, got, wantName)
		}
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

func TestKnownWireDeclaration(t *testing.T) {
	// R-Y7DW-FW5P
	wireType := reflect.TypeOf(KnownWire(0))
	if wireType.Kind() != reflect.Int {
		t.Fatalf("KnownWire underlying kind = %s, want int", wireType.Kind())
	}
	wantNames := []string{
		"KnownWireAnthropicMessages",
		"KnownWireOpenAIResponses",
		"KnownWireOpenAIChat",
		"KnownWireGemini",
	}
	wantValues := []KnownWire{0, 1, 2, 3}
	gotValues := []KnownWire{
		KnownWireAnthropicMessages,
		KnownWireOpenAIResponses,
		KnownWireOpenAIChat,
		KnownWireGemini,
	}
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("KnownWire values = %v, want %v", gotValues, wantValues)
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "provider.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var knownWireConstants []string
	var sequenceDeclarations int
	foundDefinedIntType := false
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		if general.Tok == token.TYPE {
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				underlying, isIdentifier := typeSpecification.Type.(*ast.Ident)
				if typeSpecification.Name.Name == "KnownWire" && typeSpecification.Assign == token.NoPos && isIdentifier && underlying.Name == "int" {
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
				if strings.HasPrefix(name.Name, "KnownWire") {
					knownWireConstants = append(knownWireConstants, name.Name)
					declarationNames = append(declarationNames, name.Name)
				}
			}
			for _, value := range valueSpecification.Values {
				identifier, ok := value.(*ast.Ident)
				if ok && identifier.Name == "iota" {
					hasIota = true
				}
			}
		}
		if len(declarationNames) > 0 && hasIota {
			sequenceDeclarations++
			if !reflect.DeepEqual(declarationNames, wantNames) {
				t.Fatalf("KnownWire iota declaration names = %v, want %v", declarationNames, wantNames)
			}
		}
	}
	if sequenceDeclarations != 1 {
		t.Fatalf("KnownWire iota declarations = %d, want 1", sequenceDeclarations)
	}
	if !foundDefinedIntType {
		t.Fatal("KnownWire is not defined as type KnownWire int")
	}
	if !reflect.DeepEqual(knownWireConstants, wantNames) {
		t.Fatalf("KnownWire constants = %v, want exactly %v", knownWireConstants, wantNames)
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
	// R-Z5QN-ZRMH
	assertExactStructFields(t, reflect.TypeFor[Usage](), []exactStructField{
		{name: "InputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "CachedTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "OutputTokens", typeOf: reflect.TypeFor[int64]()},
		{name: "ReasoningTokens", typeOf: reflect.TypeFor[int64]()},
	})
}

func TestCostDeclaration(t *testing.T) {
	// R-Z6YK-DJD6
	assertExactStructFields(t, reflect.TypeFor[Cost](), []exactStructField{
		{name: "Amount", typeOf: reflect.TypeFor[int64]()},
		{name: "Known", typeOf: reflect.TypeFor[bool]()},
	})
}

func TestPricingDeclaration(t *testing.T) {
	// R-Z86G-RB3V
	assertExactStructFields(t, reflect.TypeFor[Pricing](), []exactStructField{
		{name: "InputPerToken", typeOf: reflect.TypeFor[int64]()},
		{name: "CachedPerToken", typeOf: reflect.TypeFor[int64]()},
		{name: "OutputPerToken", typeOf: reflect.TypeFor[int64]()},
		{name: "ReasoningPerToken", typeOf: reflect.TypeFor[int64]()},
	})
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
	if dispatch.Name.IsExported() || dispatch.Recv == nil || renderedNode(t, dispatch.Type) != "func(ctx context.Context, call ToolUse) ToolResult" || renderedNode(t, dispatch.Recv.List[0].Type) != "*orchestrator" {
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

func TestSettingsDeclarationHasExactGenerationControlFields(t *testing.T) {
	// R-ZU4N-N6GD
	assertExactStructFields(t, reflect.TypeFor[Settings](), []exactStructField{
		{name: "Temperature", typeOf: reflect.TypeFor[*float64]()},
		{name: "TopP", typeOf: reflect.TypeFor[*float64]()},
		{name: "MaxOutputTokens", typeOf: reflect.TypeFor[*int]()},
		{name: "StopSequences", typeOf: reflect.TypeFor[[]string]()},
		{name: "ToolChoice", typeOf: reflect.TypeFor[ToolChoice]()},
		{name: "Reasoning", typeOf: reflect.TypeFor[ReasoningConfig]()},
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

func TestReasoningConfigDeclarationHasExactNeutralReasoningFields(t *testing.T) {
	// R-ZXSC-SHOG
	assertExactStructFields(t, reflect.TypeFor[ReasoningConfig](), []exactStructField{
		{name: "Mode", typeOf: reflect.TypeFor[ReasoningMode]()},
		{name: "Effort", typeOf: reflect.TypeFor[Effort]()},
		{name: "Budget", typeOf: reflect.TypeFor[int]()},
	})
}

func TestEffortDeclarationIsDefinedIntWithExactTypedIotaSequence(t *testing.T) {
	// R-ZZ09-69F5
	want := []Effort{0, 1, 2, 3}
	got := []Effort{EffortNone, EffortLow, EffortMedium, EffortHigh}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Effort values = %v, want %v", got, want)
	}
	assertDefinedIntTypedIota(t, "settings.go", "Effort", []string{
		"EffortNone", "EffortLow", "EffortMedium", "EffortHigh",
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
				if declaration.Name.IsExported() && (declaration.Name.Name == "ValidateToolArguments" || declaration.Name.Name == "WithBaseURL") {
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

	rootOption := declaredType(t, "endpoint.go", "EndpointOption")
	if rootOption.Assign.IsValid() || renderedNode(t, rootOption.Type) != "func(*endpointConfig) error" {
		t.Fatalf("EndpointOption = %s alias=%t", renderedNode(t, rootOption.Type), rootOption.Assign.IsValid())
	}
	if got := renderedNode(t, declaredFunction(t, "endpoint.go", "WithHTTPClient").Type); got != "func(client *http.Client) EndpointOption" {
		t.Fatalf("root WithHTTPClient declaration = %s", got)
	}
	for _, filename := range []string{"anthropic/anthropic.go", "openai/openai.go", "openrouter/openrouter.go", "gemini/gemini.go", "xai/xai.go"} {
		option := declaredType(t, filename, "Option")
		if option.Assign.IsValid() || renderedNode(t, option.Type) != "func(*config) error" {
			t.Fatalf("%s Option = %s alias=%t", filename, renderedNode(t, option.Type), option.Assign.IsValid())
		}
		if got := renderedNode(t, declaredFunction(t, filename, "WithBaseURL").Type); got != "func(raw string) Option" {
			t.Fatalf("%s WithBaseURL declaration = %s", filename, got)
		}
	}
}

func TestVendorWithConfigDeclarationsAndForwardingAreExact(t *testing.T) {
	// R-SO9R-BRDH
	for _, filename := range []string{"anthropic/anthropic.go", "openai/openai.go", "gemini/gemini.go", "xai/xai.go", "openrouter/openrouter.go"} {
		t.Run(filename, func(t *testing.T) {
			configuration := declaredType(t, filename, "config")
			structure, ok := configuration.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("config = %T, want struct", configuration.Type)
			}
			configFields := 0
			for _, field := range structure.Fields.List {
				if renderedNode(t, field.Type) != "agentkit.Config" {
					continue
				}
				configFields++
				if len(field.Names) != 1 || field.Names[0].Name != "conversation" || field.Names[0].IsExported() {
					t.Fatalf("agentkit.Config field = %v, want private conversation", field.Names)
				}
			}
			if configFields != 1 {
				t.Fatalf("agentkit.Config field count = %d, want one", configFields)
			}

			withConfig := declaredFunction(t, filename, "WithConfig")
			if got := renderedNode(t, withConfig.Type); got != "func(cfg agentkit.Config) Option" {
				t.Fatalf("WithConfig declaration = %s", got)
			}
			storesArgument := false
			ast.Inspect(withConfig.Body, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
					return true
				}
				storesArgument = renderedNode(t, assignment.Lhs[0]) == "configuration.conversation" && renderedNode(t, assignment.Rhs[0]) == "cfg"
				return !storesArgument
			})
			if !storesArgument {
				t.Fatal("WithConfig does not store cfg in configuration.conversation")
			}

			constructor := declaredFunction(t, filename, "New")
			assertedCalls := 0
			ast.Inspect(constructor.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || renderedNode(t, call.Fun) != "agentkit.NewForWire" {
					return true
				}
				assertedCalls++
				if len(call.Args) != 4 || renderedNode(t, call.Args[3]) != "configuration.conversation" {
					t.Fatalf("NewForWire arguments = %s, want stored conversation config fourth", renderedNode(t, call))
				}
				return true
			})
			if assertedCalls != 1 {
				t.Fatalf("NewForWire call count = %d, want one", assertedCalls)
			}
		})
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
		vendorConstructorPackage := pkg.ImportPath == module+"/anthropic" || pkg.ImportPath == module+"/openai" || pkg.ImportPath == module+"/xai" || pkg.ImportPath == module+"/openrouter" || pkg.ImportPath == module+"/gemini"
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
		"anthropic/credential.go":  "isAnthropicCredential",
		"openai/credential.go":     "isOpenAICredential",
		"xai/credential.go":        "isXAICredential",
		"openrouter/credential.go": "isOpenRouterCredential",
		"gemini/credential.go":     "isGeminiCredential",
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

func TestPhaseEightVendorDeclarationsAreExact(t *testing.T) {
	// R-YM0P-1521
	// R-YN8L-EWSQ
	// R-YPOE-6GA4
	// R-YQWA-K80T
	// R-YS46-XZRI
	// R-YTC3-BRI7
	// R-YUJZ-PJ8W
	// R-YVRW-3AZL
	// R-YWZS-H2QA
	// R-YY7O-UUGZ
	// R-YZFL-8M7O
	// R-Z0NH-MDYD
	// R-Z1VE-05P2
	type vendorSpec struct {
		directory    string
		marker       string
		tokenResults []string
		apiNames     []string
		hasOAuth     bool
	}
	specifications := []vendorSpec{
		{"anthropic", "isAnthropicCredential", []string{"string", "error"}, []string{"Messages", "TextCompletions"}, true},
		{"openai", "isOpenAICredential", []string{"string", "string", "error"}, []string{"Responses", "ChatCompletions"}, true},
		{"xai", "isXAICredential", []string{"string", "error"}, []string{"Responses", "ChatCompletions"}, true},
		{"openrouter", "isOpenRouterCredential", nil, []string{"ChatCompletions", "Responses"}, false},
		{"gemini", "isGeminiCredential", nil, nil, false},
	}
	for _, specification := range specifications {
		t.Run(specification.directory, func(t *testing.T) {
			declarations := parsePackageDeclarations(t, specification.directory)
			credential := requireInterfaceDeclaration(t, declarations, "Credential")
			if got := interfaceMethodNames(credential); !reflect.DeepEqual(got, []string{"apply", specification.marker}) {
				t.Fatalf("Credential methods = %v, want apply and %s", got, specification.marker)
			}
			assertASTMethod(t, credential, "apply", []string{"context.Context", "*http.Request", "[]byte"}, []string{"error"})
			assertASTMethod(t, credential, specification.marker, nil, nil)
			assertASTFunction(t, declarations, "APIKey", []string{"string"}, []string{"Credential"}, false)
			assertASTFunction(t, declarations, "New", []string{"Credential", "string", "...Option"}, []string{"*agentkit.Conversation", "error"}, true)

			_, oauthExists := declarations.functions["OAuth"]
			if oauthExists != specification.hasOAuth {
				t.Fatalf("OAuth presence = %v, want %v", oauthExists, specification.hasOAuth)
			}
			if specification.hasOAuth {
				assertASTFunction(t, declarations, "OAuth", []string{"TokenSource"}, []string{"Credential"}, false)
			}

			tokenSource, tokenExists := declarations.types["TokenSource"]
			if tokenExists != (specification.tokenResults != nil) {
				t.Fatalf("TokenSource presence = %v, want %v", tokenExists, specification.tokenResults != nil)
			}
			if tokenExists {
				interfaceType, ok := tokenSource.Type.(*ast.InterfaceType)
				if !ok {
					t.Fatalf("TokenSource is %T, want defined interface", tokenSource.Type)
				}
				assertASTMethod(t, interfaceType, "Token", []string{"context.Context"}, specification.tokenResults)
			}

			api, apiExists := declarations.types["API"]
			if apiExists != (specification.apiNames != nil) {
				t.Fatalf("API presence = %v, want %v", apiExists, specification.apiNames != nil)
			}
			if apiExists {
				identifier, ok := api.Type.(*ast.Ident)
				if !ok || identifier.Name != "int" || api.Assign.IsValid() {
					t.Fatal("API is not a defined type with underlying int")
				}
				if !reflect.DeepEqual(declarations.apiConstants, specification.apiNames) || !declarations.apiUsesIota {
					t.Fatalf("API constants/iota = %v/%v, want %v/true", declarations.apiConstants, declarations.apiUsesIota, specification.apiNames)
				}
				assertASTFunction(t, declarations, "WithAPI", []string{"API"}, []string{"Option"}, false)
			} else if _, exists := declarations.functions["WithAPI"]; exists {
				t.Fatal("WithAPI exists without the contracted API enum")
			}
		})
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

func requireInterfaceDeclaration(t *testing.T, declarations packageDeclarations, name string) *ast.InterfaceType {
	t.Helper()
	specification, exists := declarations.types[name]
	if !exists || specification.Assign.IsValid() {
		t.Fatalf("%s is missing or is an alias", name)
	}
	interfaceType, ok := specification.Type.(*ast.InterfaceType)
	if !ok {
		t.Fatalf("%s is %T, want interface", name, specification.Type)
	}
	return interfaceType
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
	default:
		return ""
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
			if len(wireInterface.Methods.List) != 4 {
				t.Fatalf("WireFormat has %d methods, want exact four-method seam", len(wireInterface.Methods.List))
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
	wantFunctions := []string{"NewEndpoint", "WithHeader", "WithFramer", "WithClassifier", "WithMutator", "WithHTTPClient"}
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
