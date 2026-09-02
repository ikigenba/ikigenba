package agentkit

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const compileTimeDefaultOutputAttempts = DefaultOutputAttempts

type phase32Result struct {
	Answer string `json:"answer"`
	Count  int    `json:"count"`
}

var compileTimeOutput = Output[phase32Result]

func TestOutputFunctionDecodesTypedResult(t *testing.T) {
	stream := &Stream{outputDeclared: true, drive: func(yield func(Event) bool) error {
		yield(OutputDone{Value: json.RawMessage(`{"answer":"ready","count":2}`)})
		return nil
	}}
	got, err := compileTimeOutput(stream)

	// R-TQ2N-6ND0
	// R-UGWF-LLOA
	if err != nil || got != (phase32Result{Answer: "ready", Count: 2}) {
		t.Fatalf("Output[phase32Result] = (%#v, %v), want decoded result", got, err)
	}
}

func TestOutputRawMessagePreservesOwnedAcceptedBytes(t *testing.T) {
	accepted := json.RawMessage("  {\n  \"number\": 1.2300, \"escaped\": \"a\\u0062\"\n}  ")
	stream := &Stream{outputDeclared: true, drive: func(yield func(Event) bool) error {
		yield(OutputDone{Value: accepted})
		return nil
	}}
	var eventValue json.RawMessage
	for event := range stream.Events() {
		eventValue = event.(OutputDone).Value
	}
	want := append(json.RawMessage(nil), accepted...)
	eventValue[0] = '\t'

	got, err := Output[json.RawMessage](stream)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Output[json.RawMessage] = (%q, %v), want byte-identical %q", got, err, want)
	}
	got[0] = '\n'
	gotAgain, err := Output[json.RawMessage](stream)
	if err != nil || !bytes.Equal(gotAgain, want) {
		t.Fatalf("repeated Output[json.RawMessage] = (%q, %v), retained value aliased first result", gotAgain, err)
	}
}

func TestOutputReturnsZeroOnDecodeAndMissingResultErrors(t *testing.T) {
	decodeStream := &Stream{outputDeclared: true, drive: func(yield func(Event) bool) error {
		yield(OutputDone{Value: json.RawMessage(`{"answer":3,"count":2}`)})
		return nil
	}}
	decoded, decodeErr := Output[phase32Result](decodeStream)
	if decodeErr == nil || decoded != (phase32Result{}) {
		t.Fatalf("incompatible decode = (%#v, %v), want zero value and json error", decoded, decodeErr)
	}

	nilValueStream := &Stream{outputDeclared: true, drive: func(yield func(Event) bool) error {
		yield(OutputDone{})
		return nil
	}}
	nilValue, nilValueErr := Output[phase32Result](nilValueStream)
	if nilValueErr == nil || nilValue != (phase32Result{}) {
		t.Fatalf("nil output value = (%#v, %v), want zero value and decode error", nilValue, nilValueErr)
	}

	missingStream := &Stream{outputDeclared: true, drive: func(func(Event) bool) error { return nil }}
	missing, missingErr := Output[phase32Result](missingStream)
	if missingErr == nil || missing != (phase32Result{}) || !strings.Contains(missingErr.Error(), "without completed output") {
		t.Fatalf("missing output = (%#v, %v), want zero value and meaningful error", missing, missingErr)
	}
}

func TestOutputRejectsNilAndNoContractStreams(t *testing.T) {
	nilResult, nilErr := Output[phase32Result](nil)
	if nilErr == nil || nilResult != (phase32Result{}) {
		t.Fatalf("nil stream output = (%#v, %v), want zero value and error", nilResult, nilErr)
	}

	drives := 0
	stream := &Stream{drive: func(func(Event) bool) error {
		drives++
		return nil
	}}
	got, err := Output[phase32Result](stream)
	// R-UGWF-LLOA
	if err == nil || got != (phase32Result{}) || drives != 1 {
		t.Fatalf("no-contract output = (%#v, %v), drives=%d; want zero, error, and one normal drive", got, err, drives)
	}
}

func TestOutputContractExactShape(t *testing.T) {
	// R-TIR8-W0WU
	contractType := reflect.TypeOf(OutputContract{})
	want := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Schema", typeOf: reflect.TypeFor[json.RawMessage]()},
		{name: "MaxAttempts", typeOf: reflect.TypeFor[int]()},
	}
	if contractType.NumField() != len(want) {
		t.Fatalf("OutputContract has %d fields, want exactly %d", contractType.NumField(), len(want))
	}
	for index, expected := range want {
		actual := contractType.Field(index)
		if actual.Name != expected.name || actual.Type != expected.typeOf || !actual.IsExported() {
			t.Fatalf("OutputContract field %d = (%s, %v, exported=%t), want (%s, %v, exported=true)", index, actual.Name, actual.Type, actual.IsExported(), expected.name, expected.typeOf)
		}
	}
}

func TestDefaultOutputAttempts(t *testing.T) {
	// R-TJZ5-9SNJ
	var constantSizedArray [compileTimeDefaultOutputAttempts]int
	if got := len(constantSizedArray); got != 3 {
		t.Fatalf("DefaultOutputAttempts = %d, want 3", got)
	}
}
