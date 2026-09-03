package agentkit

import (
	"reflect"
	"slices"
	"strings"
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
	providerType := reflect.TypeOf((*wireProvider)(nil)).Elem()
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

func TestCatalogQuerySignatures(t *testing.T) {
	// R-EIBN-IELI
	assertCatalogQuerySignatures(t, Catalog, CatalogFor, LookupModel, ResolveModel)
}

func assertCatalogQuerySignatures(
	t *testing.T,
	catalog func() []CatalogEntry,
	catalogFor func(ProviderID) []CatalogEntry,
	lookup func(string) (CatalogEntry, bool),
	resolve func(string, ProviderID) (Offering, bool),
) {
	t.Helper()
	if len(catalog()) == 0 || len(catalogFor(ProviderAnthropic)) == 0 {
		t.Fatal("catalog query functions returned no known entries")
	}
	if _, ok := lookup("claude-sonnet-5"); !ok {
		t.Fatal("LookupModel did not find a known model")
	}
	if _, ok := resolve("claude-sonnet-5", ""); !ok {
		t.Fatal("ResolveModel did not resolve a known model")
	}
}

func TestCatalogReturnsFullSortedStructurallyUniqueTable(t *testing.T) {
	// R-ODLV-PL0N
	entries := Catalog()
	wantModels := []string{
		"claude-fable-5",
		"claude-haiku-4-5",
		"claude-opus-4-8",
		"claude-opus-5",
		"claude-sonnet-4-6",
		"claude-sonnet-5",
		"deepseek-v4-flash",
		"deepseek-v4-pro",
		"gemini-2.5-flash",
		"gemini-2.5-pro",
		"gemini-3.1-flash-lite",
		"gemini-3.1-pro-preview",
		"gemini-3.5-flash",
		"gemini-3.7-flash",
		"glm-4.6",
		"glm-4.7",
		"glm-5.1",
		"glm-5.2",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.4-nano",
		"gpt-5.5",
		"gpt-5.5-pro",
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"grok-4.20",
		"grok-4.20-multi-agent",
		"grok-4.3",
		"grok-4.5",
		"grok-4.6",
		"kimi-k2.6",
		"kimi-k2.7-code",
		"kimi-k3",
		"nemotron-3.5-lightning",
		"qwen3.8-27b",
		"qwen3.8-max",
	}
	gotModels := make([]string, len(entries))
	for index, entry := range entries {
		gotModels[index] = entry.Model
	}
	if !reflect.DeepEqual(gotModels, wantModels) {
		t.Fatalf("Catalog models = %q, want full sorted table %q", gotModels, wantModels)
	}
	for index, entry := range entries {
		if index > 0 && entries[index-1].Model >= entry.Model {
			t.Fatalf("catalog models not strictly ascending at %q, %q", entries[index-1].Model, entry.Model)
		}
		if len(entry.Offerings) == 0 {
			t.Fatalf("catalog entry %q has no offerings", entry.Model)
		}
		providers := make(map[ProviderID]bool, len(entry.Offerings))
		for _, offering := range entry.Offerings {
			if providers[offering.Provider] {
				t.Fatalf("catalog entry %q repeats provider %q", entry.Model, offering.Provider)
			}
			providers[offering.Provider] = true
		}
	}
}

func TestCatalogOfferingDataQuality(t *testing.T) {
	// R-OJPD-MFQ4
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.WireModel == "" {
				t.Errorf("%q offering on %q has an empty wire model", entry.Model, offering.Provider)
			}
			if offering.Provider == ProviderOpenRouter && !strings.Contains(offering.WireModel, "/") {
				t.Errorf("%q OpenRouter wire model %q does not contain a slash", entry.Model, offering.WireModel)
			}
			if offering.Context <= 0 {
				t.Errorf("%q offering on %q has context %d, want greater than zero", entry.Model, offering.Provider, offering.Context)
			}

			tiers := offering.Pricing.Tiers
			if len(tiers) == 0 {
				t.Errorf("%q offering on %q has no pricing tiers", entry.Model, offering.Provider)
				continue
			}
			if tiers[0].MinInputTokens != 0 {
				t.Errorf("%q offering on %q first pricing tier starts at %d tokens, want zero", entry.Model, offering.Provider, tiers[0].MinInputTokens)
			}
			for index, tier := range tiers {
				if index > 0 && tier.MinInputTokens <= tiers[index-1].MinInputTokens {
					t.Errorf("%q offering on %q pricing tier %d starts at %d tokens, want greater than prior threshold %d", entry.Model, offering.Provider, index, tier.MinInputTokens, tiers[index-1].MinInputTokens)
				}
				if tier.InputUncached <= 0 {
					t.Errorf("%q offering on %q pricing tier %d has uncached input rate %d, want greater than zero", entry.Model, offering.Provider, index, tier.InputUncached)
				}
				if tier.Output <= 0 {
					t.Errorf("%q offering on %q pricing tier %d has output rate %d, want greater than zero", entry.Model, offering.Provider, index, tier.Output)
				}
			}
		}
	}
}

func TestCatalogReasoningInvariants(t *testing.T) {
	// R-OM56-DZ7I
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			spec := offering.Reasoning
			if !spec.Accepts(spec.Default) {
				t.Errorf("%q offering on %q does not accept its reasoning default %+v", entry.Model, offering.Provider, spec.Default)
			}

			switch spec.Kind {
			case ReasoningKindEffort:
				if len(spec.Levels) == 0 {
					t.Errorf("%q effort offering on %q has no reasoning levels", entry.Model, offering.Provider)
				}
				seen := make(map[Effort]bool, len(spec.Levels))
				for _, level := range spec.Levels {
					if seen[level] {
						t.Errorf("%q effort offering on %q repeats reasoning level %v", entry.Model, offering.Provider, level)
					}
					seen[level] = true
				}
			case ReasoningKindBudget:
				if spec.MinBudget >= spec.MaxBudget {
					t.Errorf("%q budget offering on %q has range [%d, %d], want minimum less than maximum", entry.Model, offering.Provider, spec.MinBudget, spec.MaxBudget)
				}
			case ReasoningKindNone:
				if spec.CanEnable || spec.CanDisable {
					t.Errorf("%q none-kind offering on %q has enable flags (%t, %t), want both false", entry.Model, offering.Provider, spec.CanEnable, spec.CanDisable)
				}
				if len(spec.Levels) != 0 {
					t.Errorf("%q none-kind offering on %q has reasoning levels %v, want none", entry.Model, offering.Provider, spec.Levels)
				}
			}
		}
	}
}

func TestCatalogCoversEveryBuiltInProvider(t *testing.T) {
	// R-ELZC-NPTL
	providers := []ProviderID{
		ProviderAnthropic,
		ProviderOpenAI,
		ProviderGemini,
		ProviderXAI,
		ProviderOpenRouter,
	}
	for _, provider := range providers {
		if entries := CatalogFor(provider); len(entries) == 0 {
			t.Errorf("CatalogFor(%q) returned no entries", provider)
		}
	}
}

func TestCatalogForAndLookupModelSelection(t *testing.T) {
	// R-OG1O-H4I1
	const provider = ProviderOpenAI
	wantModels := []string{
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.4-nano",
		"gpt-5.5",
		"gpt-5.5-pro",
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
	}

	entries := CatalogFor(provider)
	gotModels := make([]string, len(entries))
	for index, entry := range entries {
		gotModels[index] = entry.Model
	}
	if !reflect.DeepEqual(gotModels, wantModels) {
		t.Fatalf("CatalogFor(%q) models = %q, want exactly %q", provider, gotModels, wantModels)
	}

	want := expectedClaudeSonnet5()
	got, ok := LookupModel(want.Model)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("LookupModel(%q) = (%+v, %t), want (%+v, true)", want.Model, got, ok, want)
	}
	if got, ok := LookupModel("not-a-catalog-model"); ok || !reflect.DeepEqual(got, CatalogEntry{}) {
		t.Fatalf("unknown LookupModel = (%+v, %t), want zero value and false", got, ok)
	}
}

func TestResolveModelSelectionAndFailures(t *testing.T) {
	// R-OH9K-UW8Q
	entry := expectedClaudeSonnet5()

	got, ok := ResolveModel(entry.Model, "")
	if !ok || !reflect.DeepEqual(got, entry.Offerings[0]) {
		t.Fatalf("default ResolveModel = (%+v, %t), want first offering (%+v, true)", got, ok, entry.Offerings[0])
	}
	want := entry.Offerings[1]
	got, ok = ResolveModel(entry.Model, want.Provider)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit ResolveModel = (%+v, %t), want (%+v, true)", got, ok, want)
	}
	if _, ok := ResolveModel("not-a-catalog-model", ""); ok {
		t.Fatal("ResolveModel accepted an unknown model")
	}
	if _, ok := ResolveModel(entry.Model, ProviderGemini); ok {
		t.Fatalf("ResolveModel accepted unavailable provider %q for %q", ProviderGemini, entry.Model)
	}
}

func TestResolveModelPinnedClaudeSonnet5(t *testing.T) {
	// R-OND2-RQY7
	got, ok := ResolveModel("claude-sonnet-5", "")
	if !ok {
		t.Fatal("ResolveModel did not resolve claude-sonnet-5 with the default provider")
	}
	if got.Provider != ProviderAnthropic {
		t.Errorf("default provider = %q, want %q", got.Provider, ProviderAnthropic)
	}
	if got.WireModel != "claude-sonnet-5" {
		t.Errorf("default wire model = %q, want %q", got.WireModel, "claude-sonnet-5")
	}

	got, ok = ResolveModel("claude-sonnet-5", ProviderOpenRouter)
	if !ok {
		t.Fatal("ResolveModel did not resolve claude-sonnet-5 for OpenRouter")
	}
	if got.WireModel != "anthropic/claude-sonnet-5" {
		t.Errorf("OpenRouter wire model = %q, want %q", got.WireModel, "anthropic/claude-sonnet-5")
	}

	if _, ok := ResolveModel("claude-sonnet-5", ProviderGemini); ok {
		t.Fatal("ResolveModel resolved unavailable claude-sonnet-5 Gemini pairing")
	}
}

func TestResolveModelPinnedGPT56Sol(t *testing.T) {
	// R-OOKZ-5IOW
	got, ok := ResolveModel("gpt-5.6-sol", "")
	if !ok {
		t.Fatal("ResolveModel did not resolve gpt-5.6-sol with the default provider")
	}
	if got.Provider != ProviderOpenAI {
		t.Errorf("default provider = %q, want %q", got.Provider, ProviderOpenAI)
	}
	if got.WireModel != "gpt-5.6-sol" {
		t.Errorf("default wire model = %q, want %q", got.WireModel, "gpt-5.6-sol")
	}
	wantDefault := ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium}
	if !reflect.DeepEqual(got.Reasoning.Default, wantDefault) {
		t.Errorf("default reasoning = %+v, want %+v", got.Reasoning.Default, wantDefault)
	}
}

func TestCatalogQueriesReturnDefensiveCopies(t *testing.T) {
	// R-OIHH-8NZF
	const model = "claude-sonnet-5"
	want := expectedClaudeSonnet5()

	entries := Catalog()
	index := slices.IndexFunc(entries, func(entry CatalogEntry) bool { return entry.Model == model })
	if index < 0 {
		t.Fatalf("Catalog omitted %q", model)
	}
	mutateCatalogEntry(&entries[index])
	assertLookupUnchanged(t, model, want)

	providerEntries := CatalogFor(ProviderAnthropic)
	index = slices.IndexFunc(providerEntries, func(entry CatalogEntry) bool { return entry.Model == model })
	if index < 0 {
		t.Fatalf("CatalogFor omitted %q", model)
	}
	mutateCatalogEntry(&providerEntries[index])
	assertLookupUnchanged(t, model, want)

	lookedUp, ok := LookupModel(model)
	if !ok {
		t.Fatalf("LookupModel omitted %q", model)
	}
	mutateCatalogEntry(&lookedUp)
	entries = Catalog()
	index = slices.IndexFunc(entries, func(entry CatalogEntry) bool { return entry.Model == model })
	if index < 0 || !reflect.DeepEqual(entries[index], want) {
		t.Fatalf("Catalog changed after mutating LookupModel result: %+v", entries)
	}

	offering, ok := ResolveModel(model, ProviderAnthropic)
	if !ok {
		t.Fatalf("ResolveModel omitted %q on %q", model, ProviderAnthropic)
	}
	offering.Pricing.Tiers[0].Output = -1
	offering.Reasoning.Levels[0] = Effort(99)
	got, ok := ResolveModel(model, ProviderAnthropic)
	if !ok || !reflect.DeepEqual(got, want.Offerings[0]) {
		t.Fatalf("ResolveModel changed after mutating prior result: (%+v, %t)", got, ok)
	}
}

func expectedClaudeSonnet5() CatalogEntry {
	levels := []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	reasoning := ReasoningSpec{
		Kind:       ReasoningKindEffort,
		Levels:     levels,
		CanDisable: true,
		Default:    ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium},
	}
	pricing := Pricing{Tiers: []RateTier{{
		MinInputTokens: 0,
		InputUncached:  3000,
		CacheReadInput: 300,
		CacheWrite5m:   3750,
		CacheWrite1h:   6000,
		Output:         15000,
	}}}
	return CatalogEntry{
		Model:  "claude-sonnet-5",
		Vendor: VendorAnthropic,
		Offerings: []Offering{
			{
				Provider:  ProviderAnthropic,
				WireModel: "claude-sonnet-5",
				Context:   1_000_000,
				Pricing:   pricing,
				Reasoning: reasoning,
			},
			{
				Provider:  ProviderOpenRouter,
				WireModel: "anthropic/claude-sonnet-5",
				Context:   1_000_000,
				Pricing:   pricing,
				Reasoning: reasoning,
			},
		},
	}
}

func mutateCatalogEntry(entry *CatalogEntry) {
	entry.Model = "mutated"
	entry.Offerings[0].WireModel = "mutated"
	entry.Offerings[0].Pricing.Tiers[0].Output = -1
	entry.Offerings[0].Reasoning.Levels[0] = Effort(99)
}

func assertLookupUnchanged(t *testing.T, model string, want CatalogEntry) {
	t.Helper()
	got, ok := LookupModel(model)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("LookupModel changed after mutating another query result: (%+v, %t)", got, ok)
	}
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
