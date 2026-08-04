package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// R-1I5J-PHAE
func TestRecordJSONContainsOnlyAllowlistedFieldsAndOmitsEmptyOptionalFields(t *testing.T) {
	stamp := time.Date(2026, time.August, 3, 12, 13, 14, 123456789, time.UTC)
	full := Record{
		ID:            "record-1",
		Time:          stamp,
		CorrelationID: "correlation-1",
		Service:       "crm",
		Kind:          KindRequest,
		Actor:         &Actor{OwnerEmail: "owner@example.com", ClientID: "client-1"},
		Op:            "contacts.list",
		Params:        map[string]any{"limit": float64(10)},
		Outcome:       &Outcome{Status: "ok", DurationMS: 4, Bytes: 12, SHA256: strings.Repeat("a", 64)},
		Detail:        map[string]any{"transport": "mcp"},
	}

	var got map[string]any
	encoded, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal populated record: %v", err)
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode populated record: %v", err)
	}
	wantKeys := []string{"actor", "correlation_id", "detail", "id", "kind", "op", "outcome", "params", "service", "time"}
	gotKeys := make([]string, 0, len(got))
	for key := range got {
		gotKeys = append(gotKeys, key)
	}
	sortStrings(gotKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("populated record keys = %v, want exactly %v", gotKeys, wantKeys)
	}
	if got["time"] != "2026-08-03T12:13:14.123456789Z" {
		t.Fatalf("time = %q, want RFC3339Nano UTC", got["time"])
	}

	empty := Record{ID: "record-2", Time: stamp, Service: "crm", Kind: KindLifecycle, Op: "start"}
	encoded, err = json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal sparse record: %v", err)
	}
	got = nil
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode sparse record: %v", err)
	}
	for _, key := range []string{"correlation_id", "actor", "params", "outcome", "detail"} {
		if _, exists := got[key]; exists {
			t.Errorf("sparse record unexpectedly contains %q", key)
		}
	}
}

// R-1JDG-3913
func TestEncodeParamsElidesOnlyValuesExceedingPerValueLimit(t *testing.T) {
	value2000 := strings.Repeat("x", 1998)
	value1024 := strings.Repeat("y", 1022)
	raw, err := json.Marshal(map[string]any{"large": value2000, "at_limit": value1024})
	if err != nil {
		t.Fatal(err)
	}

	got := EncodeParams(raw, nil)
	largeEncoded, _ := json.Marshal(value2000)
	assertElision(t, got["large"], largeEncoded)
	if got["at_limit"] != value1024 {
		t.Fatalf("1024-byte encoded value was elided or changed")
	}
}

// R-1LT8-USIH
func TestEncodeParamsKeepsSmallValuesWhenLargeValuesMakeRecordExceedBudget(t *testing.T) {
	values := map[string]any{
		"largest":  strings.Repeat("a", 4998),
		"second":   strings.Repeat("b", 3998),
		"small":    strings.Repeat("c", 198),
		"smallest": strings.Repeat("d", 98),
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}

	got := EncodeParams(raw, nil)
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxParamsBytes {
		t.Fatalf("encoded params size = %d, want <= %d", len(encoded), maxParamsBytes)
	}
	for _, key := range []string{"largest", "second"} {
		valueEncoded, _ := json.Marshal(values[key])
		assertElision(t, got[key], valueEncoded)
	}
	for _, key := range []string{"small", "smallest"} {
		if got[key] != values[key] {
			t.Errorf("%s value was elided or changed", key)
		}
	}
}

// R-1N15-8K96
func TestEncodeParamsAlwaysElidesSensitiveNames(t *testing.T) {
	raw := json.RawMessage(`{"secret":"x","public":"y"}`)
	got := EncodeParams(raw, []string{"secret"})
	secretEncoded, _ := json.Marshal("x")
	assertElision(t, got["secret"], secretEncoded)
	if got["public"] != "y" {
		t.Fatalf("undeclared sibling = %#v, want literal y", got["public"])
	}
}

// R-1O91-MBZV
func TestDigestAndElisionDigestsUseFullLowercaseSHA256(t *testing.T) {
	input := []byte("known telemetry input")
	length, got := Digest(input)
	wantSum := sha256.Sum256(input)
	want := hex.EncodeToString(wantSum[:])
	if length != len(input) || got != want {
		t.Fatalf("Digest = (%d, %q), want (%d, %q)", length, got, len(input), want)
	}
	if matched := regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got); !matched {
		t.Fatalf("digest %q is not 64-character lowercase hex", got)
	}

	params := EncodeParams(json.RawMessage(`{"secret":"x"}`), []string{"secret"})
	marker := elisionDetails(t, params["secret"])
	digest, ok := marker["sha256"].(string)
	if !ok || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) {
		t.Fatalf("elision sha256 = %#v, want 64-character lowercase hex", marker["sha256"])
	}
}

func assertElision(t *testing.T, got any, encoded []byte) {
	t.Helper()
	details := elisionDetails(t, got)
	wantSum := sha256.Sum256(encoded)
	wantDigest := hex.EncodeToString(wantSum[:])
	if details["bytes"] != len(encoded) {
		t.Errorf("elision bytes = %#v, want %d", details["bytes"], len(encoded))
	}
	if details["sha256"] != wantDigest {
		t.Errorf("elision sha256 = %#v, want %s", details["sha256"], wantDigest)
	}
}

func elisionDetails(t *testing.T, got any) map[string]any {
	t.Helper()
	marker, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("value = %#v, want elision marker", got)
	}
	details, ok := marker["$elided"].(map[string]any)
	if !ok {
		t.Fatalf("value = %#v, want $elided details", got)
	}
	return details
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
