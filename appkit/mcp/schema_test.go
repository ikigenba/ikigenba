package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

type schemaEmpty struct{}

func schemaServer(t *testing.T) *mcp.Server {
	return mcp.NewServer(mcp.ServerConfig{Telemetry: mcpTestWriter(t, nil, nil), Name: "schema"})
}
func schemaRequest(t *testing.T, s *mcp.Server, method, params string) json.RawMessage {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://schema/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(identity.NewContext(r.Context(), identity.Caller{UserID: "user"}))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	var response struct {
		Result json.RawMessage
		Error  json.RawMessage
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Error) != 0 || len(response.Result) == 0 {
		t.Fatalf("response: %s", w.Body.String())
	}
	return response.Result
}
func schemaList(t *testing.T, s *mcp.Server) (json.RawMessage, json.RawMessage) {
	t.Helper()
	var response struct {
		Tools []struct{ InputSchema, OutputSchema json.RawMessage }
	}
	if err := json.Unmarshal(schemaRequest(t, s, "tools/list", `{}`), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Tools) != 1 {
		t.Fatalf("tools: %d", len(response.Tools))
	}
	return response.Tools[0].InputSchema, response.Tools[0].OutputSchema
}
func schemaCall(t *testing.T, s *mcp.Server, args string) (string, json.RawMessage, bool) {
	t.Helper()
	var response struct {
		Content           []struct{ Text string }
		StructuredContent json.RawMessage
		IsError           bool
	}
	if err := json.Unmarshal(schemaRequest(t, s, "tools/call", `{"name":"inspect","arguments":`+args+`}`), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Content) != 1 {
		t.Fatalf("content: %#v", response.Content)
	}
	return response.Content[0].Text, response.StructuredContent, response.IsError
}
func schemaRegister[In, Out any](s *mcp.Server, h func(In) Out) {
	mcp.AddTool(s, mcp.Tool[In, Out]{Name: "inspect", Description: "Inspect values.", Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, in In) (Out, error) { return h(in), nil }})
}
func schemaRawRegister[In any](s *mcp.Server, h func(In)) {
	mcp.AddRawTool(s, mcp.RawTool[In]{Name: "inspect", Description: "Inspect values.", Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, in In) (mcp.Result, error) {
		h(in)
		return mcp.TextResult("ok"), nil
	}})
}
func schemaMustPanic(t *testing.T, fn func()) {
	t.Helper()
	var recovered any
	func() { defer func() { recovered = recover() }(); fn() }()
	if recovered == nil {
		t.Fatal("registration did not panic")
	}
}
func schemaRejectIn[T any](t *testing.T) {
	t.Helper()
	schemaMustPanic(t, func() { schemaRegister(schemaServer(t), func(T) schemaEmpty { return schemaEmpty{} }) })
	schemaMustPanic(t, func() { schemaRawRegister(schemaServer(t), func(T) {}) })
	schemaMustPanic(t, func() { schemaRegister(schemaServer(t), func(schemaEmpty) T { var v T; return v }) })
}

type schemaChoice string

func (schemaChoice) Enum() []string { return []string{"second", "first\nquoted\""} }

type schemaPointerChoice string

func (*schemaPointerChoice) Enum() []string { return []string{"pointer", "value"} }

type schemaLeaf struct {
	Z string `json:"z" mcp:"required"`
	A bool   `json:"a"`
}
type schemaModel struct {
	hidden        string
	Skip          string              `json:"-"`
	Text          string              `json:"text" mcp:"required" description:"verbatim\n description"`
	Flag          bool                `json:"flag"`
	Signed        int8                `json:"signed"`
	Unsigned      uint64              `json:"unsigned"`
	Number        float64             `json:"number"`
	Choice        schemaChoice        `json:"choice"`
	PointerChoice schemaPointerChoice `json:"pointer_choice"`
	List          []schemaLeaf        `json:"list"`
	Nested        schemaLeaf          `json:"nested"`
	Optional      *string             `json:"optional" description:"optional description"`
	Raw           json.RawMessage     `json:"raw"`
}

func TestSchemaObjectsAndOrdering(t *testing.T) {
	// R-6MRV-S1RK R-TRVR-1U87 R-TT3N-FLYW R-TVJG-75GA R-TWRC-KX6Z
	// R-TPFY-AAQT R-PI8U-UZFZ R-TZ75-CGOD R-TQNU-O2HI R-U0F1-Q8F2 R-U1MY-405R
	s := schemaServer(t)
	schemaRegister(s, func(in schemaModel) schemaModel { in.hidden = "ignored"; return in })
	in, out := schemaList(t, s)
	want := `{"type":"object","properties":{"text":{"type":"string","description":"verbatim\n description"},"flag":{"type":"boolean"},"signed":{"type":"integer"},"unsigned":{"type":"integer","minimum":0},"number":{"type":"number"},"choice":{"type":"string","enum":["second","first\nquoted\""]},"pointer_choice":{"type":"string","enum":["pointer","value"]},"list":{"type":"array","items":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"boolean"}},"required":["z"],"additionalProperties":false}},"nested":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"boolean"}},"required":["z"],"additionalProperties":false},"optional":{"type":"string","description":"optional description"},"raw":{"type":"object"}},"required":["text"],"additionalProperties":false}`
	if string(in) != want || string(out) != want {
		t.Fatalf("schemas:\n%s\n%s\nwant %s", in, out, want)
	}
	empty := schemaServer(t)
	schemaRegister(empty, func(in schemaEmpty) schemaEmpty { return in })
	a, b := schemaList(t, empty)
	if string(a) != `{"type":"object","additionalProperties":false}` || !bytes.Equal(a, b) {
		t.Fatalf("empty: %s %s", a, b)
	}
}
func TestSchemaIntegerKinds(t *testing.T) {
	// R-TVJG-75GA
	type all struct {
		A int    `json:"a"`
		B int16  `json:"b"`
		C int32  `json:"c"`
		D int64  `json:"d"`
		E uint   `json:"e"`
		F uint8  `json:"f"`
		G uint16 `json:"g"`
		H uint32 `json:"h"`
	}
	s := schemaServer(t)
	schemaRegister(s, func(in all) all { return in })
	in, _ := schemaList(t, s)
	if string(in) != `{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"},"c":{"type":"integer"},"d":{"type":"integer"},"e":{"type":"integer","minimum":0},"f":{"type":"integer","minimum":0},"g":{"type":"integer","minimum":0},"h":{"type":"integer","minimum":0}},"additionalProperties":false}` {
		t.Fatal(string(in))
	}
}

type schemaMissingTag struct{ Value string }
type schemaEmptyTag struct {
	Value string `json:"" mcp:"required"`
}
type schemaBadName struct {
	Value string `json:"bad-name"`
}
type schemaOption struct {
	Value string `json:"value,omitempty"`
}
type schemaStringOption struct {
	Value int `json:"value,string"`
}
type schemaMarkerEmpty struct {
	Value string `json:"value" mcp:""`
}
type schemaMarkerOther struct {
	Value string `json:"value" mcp:"optional"`
}
type schemaMarkerOptions struct {
	Value string `json:"value" mcp:"required,omitempty"`
}
type schemaMarkerSkipped struct {
	Value string `json:"-" mcp:"required"`
}
type schemaMarkerHidden struct {
	value string `mcp:"required"`
}
type schemaByteSlice struct {
	Value []byte `json:"value"`
}
type schemaNamedByte uint8
type schemaNamedBytes struct {
	Value []schemaNamedByte `json:"value"`
}
type schemaPointerSlice struct {
	Value []*string `json:"value"`
}
type schemaDoublePointer struct {
	Value **string `json:"value"`
}
type schemaRawSlice struct {
	Value []json.RawMessage `json:"value"`
}
type schemaRawPointer struct {
	Value *json.RawMessage `json:"value"`
}
type SchemaEmbeddedEmpty struct{}
type schemaEmbedded struct {
	SchemaEmbeddedEmpty `json:"ignored"`
}
type schemaRecursive struct {
	Next *schemaRecursive `json:"next"`
}
type schemaCycleA struct {
	B []schemaCycleB `json:"b"`
}
type schemaCycleB struct {
	A *schemaCycleA `json:"a"`
}
type schemaEmptyEnum string

func (schemaEmptyEnum) Enum() []string { return nil }

type schemaDuplicateEnum string

func (*schemaDuplicateEnum) Enum() []string { return []string{"a", "a"} }

type schemaNonStringEnum int

func (schemaNonStringEnum) Enum() []string { return []string{"a"} }
func TestSchemaRegistrationRules(t *testing.T) {
	// R-U2UU-HRWG
	schemaRejectIn[string](t)
	schemaRejectIn[*schemaEmpty](t)
	schemaRejectIn[json.RawMessage](t)
	// R-6NZS-5TI9
	schemaRejectIn[schemaMissingTag](t)
	schemaRejectIn[schemaEmptyTag](t)
	schemaRejectIn[schemaBadName](t)
	// R-6P7O-JL8Y
	schemaRejectIn[schemaOption](t)
	schemaRejectIn[schemaStringOption](t)
	schemaRejectIn[schemaMarkerEmpty](t)
	schemaRejectIn[schemaMarkerOther](t)
	schemaRejectIn[schemaMarkerOptions](t)
	schemaRejectIn[schemaMarkerSkipped](t)
	hidden := schemaMarkerHidden{value: "unexported"}
	t.Run(hidden.value, func(t *testing.T) { schemaRejectIn[schemaMarkerHidden](t) })
	// R-TPFY-AAQT
	schemaRejectIn[schemaByteSlice](t)
	schemaRejectIn[schemaNamedBytes](t)
	// R-TZ75-CGOD
	schemaRejectIn[schemaPointerSlice](t)
	schemaRejectIn[schemaDoublePointer](t)
	// R-TQNU-O2HI
	schemaRejectIn[schemaRawSlice](t)
	schemaRejectIn[schemaRawPointer](t)
	// R-U5AN-9BDU
	schemaRejectIn[schemaEmbedded](t)
	// R-U6IJ-N34J
	schemaRejectIn[schemaRecursive](t)
	schemaRejectIn[schemaCycleA](t)
	// R-U42Q-VJN5
	schemaRejectIn[struct {
		V schemaEmptyEnum `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V schemaDuplicateEnum `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V schemaNonStringEnum `json:"v"`
	}](t)
}
func TestSchemaRequiredOutputPointer(t *testing.T) {
	// R-6QFK-XCZN
	type nested struct {
		Value *string `json:"value" mcp:"required"`
	}
	type nestedOutput struct {
		N []nested `json:"n"`
	}
	schemaMustPanic(t, func() { schemaRegister(schemaServer(t), func(schemaEmpty) nested { return nested{} }) })
	schemaMustPanic(t, func() { schemaRegister(schemaServer(t), func(schemaEmpty) nestedOutput { return nestedOutput{} }) })
}
func TestSchemaUnsupportedKinds(t *testing.T) {
	// R-MJHL-D11C
	schemaRejectIn[struct {
		V map[string]string `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V any `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V [1]int `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V float32 `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V complex64 `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V complex128 `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V chan int `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V func() `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V uintptr `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V unsafe.Pointer `json:"v"`
	}](t)
}

type schemaMarshal string

func (schemaMarshal) MarshalJSON() ([]byte, error) { return []byte(`"x"`), nil }

type schemaUnmarshal string

func (*schemaUnmarshal) UnmarshalJSON([]byte) error { return nil }

type schemaTextMarshal string

func (*schemaTextMarshal) MarshalText() ([]byte, error) { return []byte("x"), nil }

type schemaTextUnmarshal string

func (schemaTextUnmarshal) UnmarshalText([]byte) error { return nil }
func TestSchemaCustomEncodingRejected(t *testing.T) {
	// R-GT7Q-O5ZL
	schemaRejectIn[struct {
		V schemaMarshal `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V schemaUnmarshal `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V schemaTextMarshal `json:"v"`
	}](t)
	schemaRejectIn[struct {
		V schemaTextUnmarshal `json:"v"`
	}](t)
}

func schemaInvalid[In any](t *testing.T, args, want string) {
	t.Helper()
	s := schemaServer(t)
	called := false
	schemaRawRegister(s, func(In) { called = true })
	text, _, failure := schemaCall(t, s, args)
	if !failure || text != want || called {
		t.Fatalf("got %q, error=%t, called=%t; want %q", text, failure, called, want)
	}
}
func TestSchemaOffenceAggregation(t *testing.T) {
	// R-BG4E-PHZG R-BHCB-39Q5 R-BIK7-H1GU R-BJS3-UT7J
	// R-BL00-8KY8 R-BM7W-MCOX R-BNFT-04FM R-BONP-DW6B
	type input struct {
		First string       `json:"first" mcp:"required"`
		Rows  []schemaLeaf `json:"rows"`
		Dup   int          `json:"dup"`
		Last  bool         `json:"last" mcp:"required"`
	}
	schemaInvalid[input](t, `{"last":4,"rows":[{"a":"no","z":8,"odd.name":0},{"a":null}],"dup":"bad","dup":{"nested":null},"unknown":1,"unknown":2,"quote\"key":3,"":4}`, "invalid arguments:\nfirst: missing required field\nrows[0].z: expected string, got number\nrows[0].a: expected boolean, got string\nrows[0][\"odd.name\"]: unknown field\nrows[1].z: missing required field\nrows[1].a: expected boolean, got null\ndup: duplicate field\nlast: expected boolean, got number\nunknown: unknown field\nunknown: unknown field\n[\"quote\\\"key\"]: unknown field\n[\"\"]: unknown field")
}
func TestSchemaTypeMismatches(t *testing.T) {
	// R-PFT2-3FYL R-PH0Y-H7PA
	type input struct {
		S string      `json:"s"`
		B bool        `json:"b"`
		I int         `json:"i"`
		N float64     `json:"n"`
		A []string    `json:"a"`
		O schemaEmpty `json:"o"`
	}
	cases := []struct{ args, want string }{
		{`null`, `arguments: expected object, got null`}, {`[]`, `arguments: expected object, got array`}, {`"x"`, `arguments: expected object, got string`}, {`true`, `arguments: expected object, got boolean`}, {`2`, `arguments: expected object, got number`},
		{`{"s":null,"b":{},"i":true,"n":[],"a":4,"o":"x"}`, "s: expected string, got null\nb: expected boolean, got object\ni: expected integer, got boolean\nn: expected number, got array\na: expected array, got number\no: expected object, got string"},
	}
	for _, c := range cases {
		schemaInvalid[input](t, c.args, "invalid arguments:\n"+c.want)
	}
}
func TestSchemaIntegerValidation(t *testing.T) {
	// R-MVOL-6QGA R-LADB-7ILN R-MWWH-KI6Z R-LCT3-Z231
	type input struct {
		I int8   `json:"i"`
		U uint64 `json:"u"`
	}
	for _, c := range []struct{ args, want string }{
		{`{"i":3.25e0}`, `i: expected integer, got 3.25e0`},
		{`{"i":-128.1,"u":1e-1000000}`, "i: expected integer, got -128.1\nu: expected integer, got 1e-1000000"},
		{`{"i":128.0,"u":18446744073709551616}`, "i: must be between -128 and 127, got 128.0\nu: must be between 0 and 18446744073709551615, got 18446744073709551616"},
		{`{"i":-129e0,"u":-1}`, "i: must be between -128 and 127, got -129e0\nu: must be between 0 and 18446744073709551615, got -1"},
		{`{"i":1e1000000000}`, `i: must be between -128 and 127, got 1e1000000000`},
	} {
		schemaInvalid[input](t, c.args, "invalid arguments:\n"+c.want)
	}
	for _, number := range []string{"3", "3.0", "3e0", "30e-1", "0.03e2", "300.000e-2"} {
		s := schemaServer(t)
		var got input
		schemaRawRegister(s, func(in input) { got = in })
		text, _, bad := schemaCall(t, s, `{"i":`+number+`,"u":18446744073709551615}`)
		if bad || text != "ok" || got.I != 3 || got.U != math.MaxUint64 {
			t.Fatalf("%s: %#v %s", number, got, text)
		}
	}
}
func TestSchemaFloatAndEnumValidation(t *testing.T) {
	// R-MY4D-Y9XO R-LE10-CTTQ R-N0K6-PTF2 R-LF8W-QLKF
	type input struct {
		N float64      `json:"n"`
		C schemaChoice `json:"c"`
	}
	schemaInvalid[input](t, `{"n":1.7976931348623159e308,"c":"absent\n\""}`, "invalid arguments:\nn: out of range for a 64-bit float, got 1.7976931348623159e308\nc: must be one of \"second\", \"first\\nquoted\\\"\", got \"absent\\n\\\"\"")
	for _, number := range []string{"1.7976931348623158e308", "-1.7976931348623158e308"} {
		schemaInvalid[input](t, `{"n":`+number+`}`, "invalid arguments:\nn: out of range for a 64-bit float, got "+number)
	}
	for _, number := range []string{"1.7976931348623157e308", "0.10000000000000001", "1e-4000", "-0"} {
		s := schemaServer(t)
		var got input
		schemaRawRegister(s, func(in input) { got = in })
		text, _, bad := schemaCall(t, s, `{"n":`+number+`,"c":"second"}`)
		if bad || text != "ok" {
			t.Fatalf("%s: %s", number, text)
		}
		var want float64
		if err := json.Unmarshal([]byte(number), &want); err != nil {
			t.Fatal(err)
		}
		if math.Float64bits(got.N) != math.Float64bits(want) {
			t.Fatalf("%s: %v != %v", number, got.N, want)
		}
	}
}
func TestSchemaRawAndPointerInput(t *testing.T) {
	// R-BPVL-RNX0 R-VBQP-WXUR R-BR3I-5FNP
	type input struct {
		Raw json.RawMessage `json:"raw"`
		P   *int            `json:"p" mcp:"required"`
		O   *schemaLeaf     `json:"o"`
		S   string          `json:"s"`
		A   []int           `json:"a"`
		N   schemaLeaf      `json:"n"`
	}
	for _, raw := range []string{"null", "[]", "1", "true", `"x"`} {
		schemaInvalid[input](t, `{"p":null,"raw":`+raw+`}`, "invalid arguments:\nraw: expected object, got "+map[string]string{"null": "null", "[]": "array", "1": "number", "true": "boolean", `"x"`: "string"}[raw])
	}
	s := schemaServer(t)
	var got input
	schemaRawRegister(s, func(in input) { got = in })
	raw := `{ "anything" : [1, {"duplicate":0,"duplicate":1}], "x": null }`
	text, _, bad := schemaCall(t, s, `{"raw":`+raw+`,"p":null,"o":{"z":"nested","a":true},"s":"unaltered\ntext","a":[3.0,2e0],"n":{"z":"leaf"}}`)
	if bad || text != "ok" || string(got.Raw) != raw || got.P != nil || got.O == nil || *got.O != (schemaLeaf{Z: "nested", A: true}) || got.S != "unaltered\ntext" || !reflect.DeepEqual(got.A, []int{3, 2}) || got.N != (schemaLeaf{Z: "leaf"}) {
		t.Fatalf("decoded: %#v %s", got, text)
	}
	absent := schemaServer(t)
	schemaRawRegister(absent, func(in input) { got = in })
	schemaCall(t, absent, `{"p":null}`)
	if got.Raw != nil || got.O != nil || got.A != nil || got.S != "" || got.N != (schemaLeaf{}) {
		t.Fatalf("absent: %#v", got)
	}
	present := schemaServer(t)
	schemaRawRegister(present, func(in input) { got = in })
	schemaCall(t, present, `{"p":3e0}`)
	if got.P == nil || *got.P != 3 {
		t.Fatalf("pointer: %#v", got)
	}
}
func TestSchemaOutputEncoding(t *testing.T) {
	// R-6RNH-B4QC R-U7QG-0UV8
	type output struct {
		S        string          `json:"s"`
		B        bool            `json:"b"`
		I        int64           `json:"i"`
		U        uint64          `json:"u"`
		F        float64         `json:"f"`
		Nil      []string        `json:"nil"`
		Values   []int           `json:"values"`
		Omitted  *string         `json:"omitted"`
		Present  *string         `json:"present"`
		Raw      json.RawMessage `json:"raw"`
		Absent   json.RawMessage `json:"absent"`
		Required json.RawMessage `json:"required" mcp:"required"`
		N        schemaLeaf      `json:"n"`
	}
	str := "present"
	raw := json.RawMessage(`{ "kept": [1, 2] }`)
	value := output{S: string([]byte{'a', 0xff, 0xfe, 'b'}), B: true, I: math.MinInt64, U: math.MaxUint64, F: math.SmallestNonzeroFloat64, Values: []int{2, 1}, Present: &str, Raw: raw, Required: json.RawMessage(`{}`), N: schemaLeaf{Z: "nested"}}
	s := schemaServer(t)
	schemaRegister(s, func(schemaEmpty) output { return value })
	text, structured, bad := schemaCall(t, s, `{}`)
	var members map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &members); err != nil {
		t.Fatal(err)
	}
	var gotString string
	if err := json.Unmarshal(members["s"], &gotString); err != nil {
		t.Fatal(err)
	}
	var gotFloat float64
	if err := json.Unmarshal(members["f"], &gotFloat); err != nil {
		t.Fatal(err)
	}
	if gotString != "a\ufffd\ufffdb" || math.Float64bits(gotFloat) != math.Float64bits(value.F) {
		t.Fatalf("scalar encoding: string=%q float=%v", gotString, gotFloat)
	}
	want := `{"s":` + string(members["s"]) + `,"b":true,"i":-9223372036854775808,"u":18446744073709551615,"f":` + string(members["f"]) + `,"nil":[],"values":[2,1],"present":"present","raw":{ "kept": [1, 2] },"required":{},"n":{"z":"nested","a":false}}`
	if bad || text != want || string(structured) != want {
		t.Fatalf("got %q %s error=%t; want %s", text, structured, bad, want)
	}
}
func TestSchemaUnencodableValues(t *testing.T) {
	// R-6SVD-OWH1
	type output struct {
		F float64         `json:"f"`
		C schemaChoice    `json:"c"`
		R json.RawMessage `json:"r" mcp:"required"`
	}
	valid := output{C: "second", R: json.RawMessage(`{}`)}
	values := []output{valid, valid, valid, valid, valid}
	values[0].F = math.NaN()
	values[1].F = math.Inf(1)
	values[2].F = math.Inf(-1)
	values[3].C = "outside"
	values[4].R = nil
	for _, raw := range []string{"null", "[]", `{"x":1} {}`, "", `{"broken":`} {
		values = append(values, output{C: "second", R: json.RawMessage(raw)})
	}
	for _, value := range values {
		s := schemaServer(t)
		schemaRegister(s, func(schemaEmpty) output { return value })
		text, _, bad := schemaCall(t, s, `{}`)
		if !bad || text != mcp.PanicText {
			t.Fatalf("%#v: %s error=%t", value, text, bad)
		}
	}
}

type schemaDefinedNullable mcp.Nullable[string]
type schemaNullableAlias = mcp.Nullable[string]

func TestNullableShapeAndSchema(t *testing.T) {
	// R-P61V-1A11 R-P79R-F1RQ R-PI8U-UZFZ
	value := mcp.Nullable[int]{true, false, 7}
	if !value.Present || value.Null || value.Value != 7 {
		t.Fatalf("fields: %#v", value)
	}
	type input struct {
		Text   schemaNullableAlias           `json:"text" description:"kept description"`
		Choice mcp.Nullable[schemaChoice]    `json:"choice"`
		Count  mcp.Nullable[uint64]          `json:"count" mcp:"required"`
		List   mcp.Nullable[[]schemaLeaf]    `json:"list"`
		Object mcp.Nullable[schemaLeaf]      `json:"object"`
		Raw    mcp.Nullable[json.RawMessage] `json:"raw"`
	}
	for _, raw := range []bool{false, true} {
		s := schemaServer(t)
		if raw {
			schemaRawRegister(s, func(input) {})
		} else {
			schemaRegister(s, func(input) schemaEmpty { return schemaEmpty{} })
		}
		in, _ := schemaList(t, s)
		want := `{"type":"object","properties":{"text":{"type":["string","null"],"description":"kept description"},"choice":{"type":["string","null"],"enum":["second","first\nquoted\"",null]},"count":{"type":["integer","null"],"minimum":0},"list":{"type":["array","null"],"items":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"boolean"}},"required":["z"],"additionalProperties":false}},"object":{"type":["object","null"],"properties":{"z":{"type":"string"},"a":{"type":"boolean"}},"required":["z"],"additionalProperties":false},"raw":{"type":["object","null"]}},"required":["count"],"additionalProperties":false}`
		if string(in) != want {
			t.Fatalf("schema: %s; want %s", in, want)
		}
	}
	// A defined type follows the ordinary struct rules, including JSON tags.
	schemaRejectIn[struct {
		Value schemaDefinedNullable `json:"value"`
	}](t)
}

func nullableRejectInput[In any](t *testing.T) {
	t.Helper()
	schemaMustPanic(t, func() { schemaRegister(schemaServer(t), func(In) schemaEmpty { return schemaEmpty{} }) })
	schemaMustPanic(t, func() { schemaRawRegister(schemaServer(t), func(In) {}) })
}

func TestNullableRegistrationRestrictions(t *testing.T) {
	// R-P8HN-STIF
	nullableRejectInput[mcp.Nullable[string]](t)
	nullableRejectInput[struct {
		V []mcp.Nullable[string] `json:"v"`
	}](t)
	nullableRejectInput[struct {
		V *mcp.Nullable[string] `json:"v"`
	}](t)
	nullableRejectInput[struct {
		V mcp.Nullable[*string] `json:"v"`
	}](t)
	nullableRejectInput[struct {
		V mcp.Nullable[mcp.Nullable[string]] `json:"v"`
	}](t)
	// R-P9PK-6L94
	schemaMustPanic(t, func() {
		schemaRegister(schemaServer(t), func(schemaEmpty) mcp.Nullable[string] { return mcp.Nullable[string]{} })
	})
	schemaMustPanic(t, func() {
		schemaRegister(schemaServer(t), func(schemaEmpty) struct {
			V mcp.Nullable[string] `json:"v"`
		} {
			return struct {
				V mcp.Nullable[string] `json:"v"`
			}{}
		})
	})
	schemaMustPanic(t, func() {
		schemaRegister(schemaServer(t), func(schemaEmpty) struct {
			V []struct {
				N mcp.Nullable[int] `json:"n"`
			} `json:"v"`
		} {
			return struct {
				V []struct {
					N mcp.Nullable[int] `json:"n"`
				} `json:"v"`
			}{}
		})
	})
	// R-PI8U-UZFZ: the wrapped type is checked as a struct field.
	nullableRejectInput[struct {
		V mcp.Nullable[map[string]int] `json:"v"`
	}](t)
	nullableRejectInput[struct {
		V mcp.Nullable[schemaMarshal] `json:"v"`
	}](t)
	nullableRejectInput[struct {
		V mcp.Nullable[schemaMissingTag] `json:"v"`
	}](t)
	nullableRejectInput[struct {
		V mcp.Nullable[schemaEmptyEnum] `json:"v"`
	}](t)
}

type schemaNullableInput struct {
	Text   mcp.Nullable[string]          `json:"text"`
	Count  mcp.Nullable[int8]            `json:"count" mcp:"required"`
	Choice mcp.Nullable[schemaChoice]    `json:"choice"`
	Number mcp.Nullable[float64]         `json:"number"`
	Flag   mcp.Nullable[bool]            `json:"flag"`
	List   mcp.Nullable[[]schemaLeaf]    `json:"list"`
	Object mcp.Nullable[schemaLeaf]      `json:"object"`
	Raw    mcp.Nullable[json.RawMessage] `json:"raw"`
}

func TestNullableInputStates(t *testing.T) {
	// R-PAXG-KCZT R-PC5C-Y4QI R-PEL5-PO7W
	raw := `{ "nested" : [1, null], "duplicate":0,"duplicate":1 }`
	cases := []struct {
		args string
		want schemaNullableInput
	}{
		{`{"count":null}`, schemaNullableInput{Count: mcp.Nullable[int8]{Present: true, Null: true}}},
		{`{"text":null,"count":null,"choice":null,"number":null,"flag":null,"list":null,"object":null,"raw":null}`, schemaNullableInput{Text: mcp.Nullable[string]{Present: true, Null: true}, Count: mcp.Nullable[int8]{Present: true, Null: true}, Choice: mcp.Nullable[schemaChoice]{Present: true, Null: true}, Number: mcp.Nullable[float64]{Present: true, Null: true}, Flag: mcp.Nullable[bool]{Present: true, Null: true}, List: mcp.Nullable[[]schemaLeaf]{Present: true, Null: true}, Object: mcp.Nullable[schemaLeaf]{Present: true, Null: true}, Raw: mcp.Nullable[json.RawMessage]{Present: true, Null: true}}},
		{`{"text":"kept\ntext","count":3e0,"choice":"second","number":2.5,"flag":true,"list":[{"z":"first"},{"z":"second","a":true}],"object":{"z":"object"},"raw":` + raw + `}`, schemaNullableInput{Text: mcp.Nullable[string]{Present: true, Value: "kept\ntext"}, Count: mcp.Nullable[int8]{Present: true, Value: 3}, Choice: mcp.Nullable[schemaChoice]{Present: true, Value: "second"}, Number: mcp.Nullable[float64]{Present: true, Value: 2.5}, Flag: mcp.Nullable[bool]{Present: true, Value: true}, List: mcp.Nullable[[]schemaLeaf]{Present: true, Value: []schemaLeaf{{Z: "first"}, {Z: "second", A: true}}}, Object: mcp.Nullable[schemaLeaf]{Present: true, Value: schemaLeaf{Z: "object"}}, Raw: mcp.Nullable[json.RawMessage]{Present: true, Value: json.RawMessage(raw)}}},
	}
	for _, c := range cases {
		for _, typed := range []bool{false, true} {
			s := schemaServer(t)
			var got schemaNullableInput
			called := false
			h := func(in schemaNullableInput) { got = in; called = true }
			if typed {
				schemaRegister(s, func(in schemaNullableInput) schemaEmpty { h(in); return schemaEmpty{} })
			} else {
				schemaRawRegister(s, h)
			}
			text, _, bad := schemaCall(t, s, c.args)
			if bad || !called || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("%s: got %#v, text=%s, want %#v", c.args, got, text, c.want)
			}
		}
	}
	// All fields, including Count, are optional in this input.
	type optional struct {
		Value mcp.Nullable[schemaLeaf] `json:"value"`
	}
	s := schemaServer(t)
	got := optional{Value: mcp.Nullable[schemaLeaf]{Present: true, Null: true}}
	schemaRawRegister(s, func(in optional) { got = in })
	_, _, bad := schemaCall(t, s, `{}`)
	if bad || got != (optional{}) {
		t.Fatalf("absent: %#v", got)
	}
}

func TestNullableOffences(t *testing.T) {
	// R-PC5C-Y4QI R-PFT2-3FYL R-PH0Y-H7PA
	cases := []struct{ args, want string }{
		{`{}`, `count: missing required field`},
		{`{"text":1,"count":false,"choice":{},"number":[],"flag":"yes","list":true,"object":2,"raw":"x"}`, "text: expected string or null, got number\ncount: expected integer or null, got boolean\nchoice: expected string or null, got object\nnumber: expected number or null, got array\nflag: expected boolean or null, got string\nlist: expected array or null, got boolean\nobject: expected object or null, got number\nraw: expected object or null, got string"},
		{`{"count":128,"choice":"other","number":1e309,"list":[{"z":1},{"a":null}],"object":{"a":"x","unknown":null}}`, "count: must be between -128 and 127, got 128\nchoice: must be one of \"second\", \"first\\nquoted\\\"\", got \"other\"\nnumber: out of range for a 64-bit float, got 1e309\nlist[0].z: expected string, got number\nlist[1].z: missing required field\nlist[1].a: expected boolean, got null\nobject.z: missing required field\nobject.a: expected boolean, got string\nobject.unknown: unknown field"},
		{`{"count":1.5,"text":null,"text":{},"object":{"z":"first","z":4}}`, "text: duplicate field\ncount: expected integer, got 1.5\nobject.z: duplicate field"},
	}
	for _, c := range cases {
		schemaInvalid[schemaNullableInput](t, c.args, "invalid arguments:\n"+c.want)
	}
}
