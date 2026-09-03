package agentkit

import (
	"reflect"
	"testing"
)

func TestProviderIDVocabulary(t *testing.T) {
	// R-EFVU-QV44
	want := []ProviderID{"anthropic", "openai", "gemini", "xai", "openrouter"}
	got := []ProviderID{ProviderAnthropic, ProviderOpenAI, ProviderGemini, ProviderXAI, ProviderOpenRouter}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider IDs = %q, want %q", got, want)
	}

	providerIDType := reflect.TypeOf(ProviderID(""))
	providerType := reflect.TypeOf((*Provider)(nil)).Elem()
	if providerIDType.Name() != "ProviderID" || providerIDType.Kind() != reflect.String {
		t.Fatalf("ProviderID type = %v (kind %v), want named string", providerIDType, providerIDType.Kind())
	}
	if providerIDType == providerType {
		t.Fatal("ProviderID is not distinct from the Provider SPI interface")
	}
}

func TestVendorVocabulary(t *testing.T) {
	// R-O52L-16TS
	want := []Vendor{"anthropic", "openai", "google", "x-ai", "z-ai", "deepseek", "moonshotai", "nvidia", "qwen"}
	got := []Vendor{VendorAnthropic, VendorOpenAI, VendorGoogle, VendorXAI, VendorZAI, VendorDeepSeek, VendorMoonshot, VendorNVIDIA, VendorQwen}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vendors = %q, want %q", got, want)
	}
	if typ := reflect.TypeOf(Vendor("")); typ.Name() != "Vendor" || typ.Kind() != reflect.String {
		t.Fatalf("Vendor type = %v (kind %v), want named string", typ, typ.Kind())
	}
}

func TestReasoningKindValuesAndOrder(t *testing.T) {
	// R-O6AH-EYKH
	want := []ReasoningKind{0, 1, 2, 3}
	got := []ReasoningKind{ReasoningKindNone, ReasoningKindEffort, ReasoningKindBudget, ReasoningKindToggle}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reasoning kinds = %v, want iota order %v", got, want)
	}
	if typ := reflect.TypeOf(ReasoningKind(0)); typ.Name() != "ReasoningKind" || typ.Kind() != reflect.Int {
		t.Fatalf("ReasoningKind type = %v (kind %v), want named int", typ, typ.Kind())
	}
}

func TestReasoningSpecShape(t *testing.T) {
	// R-O7ID-SQB6
	assertStructShape(t, ReasoningSpec{}, []fieldShape{
		{"Kind", reflect.TypeOf(ReasoningKind(0))},
		{"Levels", reflect.TypeOf([]Effort(nil))},
		{"MinBudget", reflect.TypeOf(int(0))},
		{"MaxBudget", reflect.TypeOf(int(0))},
		{"CanEnable", reflect.TypeOf(false)},
		{"CanDisable", reflect.TypeOf(false)},
		{"Default", reflect.TypeOf(ReasoningConfig{})},
	})
}

func TestReasoningSpecAcceptsSignature(t *testing.T) {
	// R-O8QA-6I1V
	assertReasoningSpecAcceptsSignature(t, ReasoningSpec.Accepts)
}

func assertReasoningSpecAcceptsSignature(t *testing.T, accepts func(ReasoningSpec, ReasoningConfig) bool) {
	t.Helper()
	if !accepts(ReasoningSpec{}, ReasoningConfig{Mode: ReasoningDefault}) {
		t.Fatal("ReasoningSpec.Accepts rejected ReasoningDefault")
	}
}

func TestReasoningSpecAcceptsVocabulary(t *testing.T) {
	// R-OKXA-07GT
	tests := []struct {
		name string
		spec ReasoningSpec
		got  ReasoningConfig
		want bool
	}{
		{
			name: "default regardless of kind and fields",
			spec: ReasoningSpec{Kind: ReasoningKindNone, MinBudget: 10, MaxBudget: 1},
			got:  ReasoningConfig{Mode: ReasoningDefault, Effort: EffortMax, Budget: -1},
			want: true,
		},
		{
			name: "off when disabling is allowed",
			spec: ReasoningSpec{Kind: ReasoningKindBudget, CanDisable: true},
			got:  ReasoningConfig{Mode: ReasoningOff},
			want: true,
		},
		{
			name: "off when disabling is not allowed",
			spec: ReasoningSpec{Kind: ReasoningKindToggle, CanDisable: false},
			got:  ReasoningConfig{Mode: ReasoningOff},
			want: false,
		},
		{
			name: "on for enabled toggle",
			spec: ReasoningSpec{Kind: ReasoningKindToggle, CanEnable: true},
			got:  ReasoningConfig{Mode: ReasoningOn},
			want: true,
		},
		{
			name: "on rejected for disabled toggle",
			spec: ReasoningSpec{Kind: ReasoningKindToggle, CanEnable: false},
			got:  ReasoningConfig{Mode: ReasoningOn},
			want: false,
		},
		{
			name: "on rejected for wrong kind even when enabled",
			spec: ReasoningSpec{Kind: ReasoningKindEffort, CanEnable: true},
			got:  ReasoningConfig{Mode: ReasoningOn},
			want: false,
		},
		{
			name: "effort exact membership",
			spec: ReasoningSpec{Kind: ReasoningKindEffort, Levels: []Effort{EffortLow, EffortHigh}},
			got:  ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh},
			want: true,
		},
		{
			name: "effort non-membership",
			spec: ReasoningSpec{Kind: ReasoningKindEffort, Levels: []Effort{EffortLow, EffortHigh}},
			got:  ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium},
			want: false,
		},
		{
			name: "effort rejected for wrong kind",
			spec: ReasoningSpec{Kind: ReasoningKindToggle, Levels: []Effort{EffortHigh}},
			got:  ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh},
			want: false,
		},
		{
			name: "budget inclusive minimum",
			spec: ReasoningSpec{Kind: ReasoningKindBudget, MinBudget: 100, MaxBudget: 200},
			got:  ReasoningConfig{Mode: ReasoningBudget, Budget: 100},
			want: true,
		},
		{
			name: "budget inclusive maximum",
			spec: ReasoningSpec{Kind: ReasoningKindBudget, MinBudget: 100, MaxBudget: 200},
			got:  ReasoningConfig{Mode: ReasoningBudget, Budget: 200},
			want: true,
		},
		{
			name: "budget below minimum",
			spec: ReasoningSpec{Kind: ReasoningKindBudget, MinBudget: 100, MaxBudget: 200},
			got:  ReasoningConfig{Mode: ReasoningBudget, Budget: 99},
			want: false,
		},
		{
			name: "budget above maximum",
			spec: ReasoningSpec{Kind: ReasoningKindBudget, MinBudget: 100, MaxBudget: 200},
			got:  ReasoningConfig{Mode: ReasoningBudget, Budget: 201},
			want: false,
		},
		{
			name: "budget rejected for wrong kind",
			spec: ReasoningSpec{Kind: ReasoningKindToggle, MinBudget: 100, MaxBudget: 200},
			got:  ReasoningConfig{Mode: ReasoningBudget, Budget: 150},
			want: false,
		},
		{
			name: "unknown mode",
			spec: ReasoningSpec{Kind: ReasoningKindToggle, CanEnable: true, CanDisable: true},
			got:  ReasoningConfig{Mode: ReasoningMode(99)},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.spec.Accepts(test.got); got != test.want {
				t.Errorf("ReasoningSpec.Accepts(%+v) = %t, want %t for spec %+v", test.got, got, test.want, test.spec)
			}
		})
	}
}

func TestOfferingShape(t *testing.T) {
	// R-EH3R-4MUT
	assertStructShape(t, Offering{}, []fieldShape{
		{"Provider", reflect.TypeOf(ProviderID(""))},
		{"WireModel", reflect.TypeOf("")},
		{"Context", reflect.TypeOf(int64(0))},
		{"Pricing", reflect.TypeOf(Pricing{})},
		{"Reasoning", reflect.TypeOf(ReasoningSpec{})},
	})
}

func TestCatalogEntryShape(t *testing.T) {
	// R-OB62-Y1J9
	assertStructShape(t, CatalogEntry{}, []fieldShape{
		{"Model", reflect.TypeOf("")},
		{"Vendor", reflect.TypeOf(Vendor(""))},
		{"Offerings", reflect.TypeOf([]Offering(nil))},
	})
}

type fieldShape struct {
	name   string
	typeOf reflect.Type
}

func assertStructShape(t *testing.T, value any, want []fieldShape) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want exactly %d", typ.Name(), typ.NumField(), len(want))
	}
	for index, expected := range want {
		field := typ.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf {
			t.Errorf("%s field %d = %s %v, want %s %v", typ.Name(), index, field.Name, field.Type, expected.name, expected.typeOf)
		}
		if !field.IsExported() {
			t.Errorf("%s.%s is not exported", typ.Name(), field.Name)
		}
	}
}
