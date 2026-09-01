package agentkit

import (
	"context"
	"encoding/json"
	"go/ast"
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

	send, ok := reflect.TypeOf((*Conversation)(nil)).MethodByName("Send")
	if !ok {
		t.Fatal("*Conversation has no exported Send method")
	}
	wantSend := reflect.TypeOf(func(*Conversation, context.Context, ...Block) *Stream { return nil })
	if send.Type != wantSend || !send.Type.IsVariadic() {
		t.Fatalf("Send type = %s (variadic=%t), want %s (variadic=true)", send.Type, send.Type.IsVariadic(), wantSend)
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

func TestToolResultDeclaration(t *testing.T) {
	// R-Z4IR-LZVS
	assertExactBlockStruct(t, reflect.TypeFor[ToolResult](), []exactStructField{
		{name: "ToolUseID", typeOf: reflect.TypeFor[string]()},
		{name: "Content", typeOf: reflect.TypeFor[string]()},
		{name: "IsError", typeOf: reflect.TypeFor[bool]()},
		{name: "Provider", typeOf: reflect.TypeFor[json.RawMessage]()},
	})
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
