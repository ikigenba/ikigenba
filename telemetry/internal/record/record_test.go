package record

import (
	"encoding/json"
	"testing"
)

func TestValidateAcceptsProtocolShapeWithoutRewriting(t *testing.T) {
	item := validRecord()
	wantParams := string(item.Params)
	wantDetail := string(item.Detail)
	if err := item.Validate(); err != nil {
		t.Fatalf("Validate valid record: %v", err)
	}
	if string(item.Params) != wantParams || string(item.Detail) != wantDetail {
		t.Fatal("Validate rewrote opaque JSON")
	}

	lifecycle := item
	lifecycle.Kind = KindLifecycle
	lifecycle.CorrelationID = ""
	if err := lifecycle.Validate(); err != nil {
		t.Fatalf("Validate lifecycle without correlation id: %v", err)
	}
}

func TestValidateRejectsEachInvalidProtocolField(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{"short id", func(item *Record) { item.ID = "short" }},
		{"non Crockford id", func(item *Record) { item.ID = "01H0000000000000000000000I" }},
		{"invalid time", func(item *Record) { item.Time = "yesterday" }},
		{"unknown kind", func(item *Record) { item.Kind = "other" }},
		{"empty service", func(item *Record) { item.Service = "" }},
		{"empty op", func(item *Record) { item.Op = "" }},
		{"missing correlation", func(item *Record) { item.CorrelationID = "" }},
		{"array params", func(item *Record) { item.Params = json.RawMessage(`[]`) }},
		{"scalar detail", func(item *Record) { item.Detail = json.RawMessage(`true`) }},
		{"invalid object", func(item *Record) { item.Params = json.RawMessage(`{"broken"`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := validRecord()
			test.mutate(&item)
			if err := item.Validate(); err == nil {
				t.Fatal("Validate accepted invalid record")
			}
		})
	}
}

func TestNormalizeTimeUsesFixedWidthUTC(t *testing.T) {
	got, err := NormalizeTime("2026-08-02T23:05:06.5-05:00")
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-08-03T04:05:06.500000000Z"; got != want {
		t.Fatalf("NormalizeTime = %q, want %q", got, want)
	}
	if _, err := NormalizeTime("not-a-time"); err == nil {
		t.Fatal("NormalizeTime accepted invalid timestamp")
	}
}

func validRecord() Record {
	return Record{
		ID:            "01H00000000000000000000001",
		Time:          "2026-08-03T04:05:06Z",
		CorrelationID: "chain-a",
		Service:       "service-a",
		Kind:          KindRequest,
		Op:            "widgets.read",
		Params:        json.RawMessage(`{ "preserve" : true }`),
		Detail:        json.RawMessage(`{"answer":42}`),
	}
}
