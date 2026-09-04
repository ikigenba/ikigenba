package agentkit

import (
	"errors"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestOfferingIDVocabulary(t *testing.T) {
	// R-JB4K-6IAI
	want := []OfferingID{"anthropic-messages", "openai-responses", "openai-chat", "gemini-generate-content", "xai-responses", "xai-chat", "openrouter-chat", "openrouter-responses"}
	got := []OfferingID{OfferingAnthropicMessages, OfferingOpenAIResponses, OfferingOpenAIChat, OfferingGeminiGenerateContent, OfferingXAIResponses, OfferingXAIChat, OfferingOpenRouterChat, OfferingOpenRouterResponses}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("offering IDs = %q, want %q", got, want)
	}
	if typ := reflect.TypeOf(OfferingID("")); typ.Name() != "OfferingID" || typ.Kind() != reflect.String {
		t.Fatalf("OfferingID type = %v (kind %v), want named string", typ, typ.Kind())
	}
}

func TestHostVocabulary(t *testing.T) {
	// R-JCCG-KA17
	want := []Host{"anthropic", "openai", "gemini", "xai", "openrouter"}
	got := []Host{HostAnthropic, HostOpenAI, HostGemini, HostXAI, HostOpenRouter}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %q, want %q", got, want)
	}
	if typ := reflect.TypeOf(Host("")); typ.Name() != "Host" || typ.Kind() != reflect.String {
		t.Fatalf("Host type = %v (kind %v), want named string", typ, typ.Kind())
	}
}

func TestWireNameVocabulary(t *testing.T) {
	// R-JDKC-Y1RW
	want := []WireName{"messages", "generate-content", "chat", "responses"}
	got := []WireName{WireMessages, WireGenerateContent, WireChat, WireResponses}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire names = %q, want %q", got, want)
	}
	if typ := reflect.TypeOf(WireName("")); typ.Name() != "WireName" || typ.Kind() != reflect.String {
		t.Fatalf("WireName type = %v (kind %v), want named string", typ, typ.Kind())
	}
}

func TestOfferingShape(t *testing.T) {
	// R-JG05-PL9A
	assertStructShape(t, Offering{}, []fieldShape{
		{"ID", reflect.TypeOf(OfferingID(""))},
		{"Host", reflect.TypeOf(Host(""))},
		{"WireName", reflect.TypeOf(WireName(""))},
		{"WireFormat", reflect.TypeOf((*WireFormat)(nil)).Elem()},
		{"BaseURL", reflect.TypeOf("")},
		{"AuthModes", reflect.TypeOf([]AuthMode(nil))},
		{"OAuth", reflect.TypeOf(OAuthClient{})},
		{"WireModel", reflect.TypeOf("")},
		{"Context", reflect.TypeOf(int64(0))},
		{"Pricing", reflect.TypeOf(Pricing{})},
		{"Reasoning", reflect.TypeOf(ReasoningSpec{})},
	})
}

func TestCatalogEntryShape(t *testing.T) {
	// R-JKVR-8O82
	assertStructShape(t, CatalogEntry{}, []fieldShape{
		{"Model", reflect.TypeOf("")},
		{"Offerings", reflect.TypeOf([]Offering(nil))},
	})
}

func TestCatalogLookupSurface(t *testing.T) {
	// R-JM3N-MFYR
	assertCatalogLookupSurface(t, Catalog, Lookup, ErrNotFound)
}

func assertCatalogLookupSurface(t *testing.T, catalog func() []CatalogEntry, lookup func(string, Host, WireName) (Offering, error), notFound error) {
	t.Helper()
	if catalog == nil || lookup == nil || notFound == nil {
		t.Fatal("Catalog, Lookup, and ErrNotFound must all be exported and non-nil")
	}
}

func TestLookupClaudeSonnet5(t *testing.T) {
	// R-JUMY-AU5M
	defaultOffering, err := Lookup("claude-sonnet-5", "", "")
	if err != nil || defaultOffering.ID != OfferingAnthropicMessages || defaultOffering.WireModel != "claude-sonnet-5" {
		t.Fatalf("default Lookup = (%+v, %v), want Anthropic messages offering", defaultOffering, err)
	}
	openRouter, err := Lookup("claude-sonnet-5", HostOpenRouter, WireChat)
	if err != nil || openRouter.ID != OfferingOpenRouterChat || openRouter.WireModel != "anthropic/claude-sonnet-5" {
		t.Fatalf("OpenRouter chat Lookup = (%+v, %v), want OpenRouter chat offering", openRouter, err)
	}
	if _, err := Lookup("claude-sonnet-5", HostGemini, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Gemini Lookup error = %v, want ErrNotFound", err)
	}
}

func TestLookupGPT56Sol(t *testing.T) {
	// R-JVUU-OLWB
	defaultOffering, err := Lookup("gpt-5.6-sol", "", "")
	wantReasoning := ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium}
	if err != nil || defaultOffering.ID != OfferingOpenAIResponses || defaultOffering.WireModel != "gpt-5.6-sol" || defaultOffering.Reasoning.Default != wantReasoning {
		t.Fatalf("default Lookup = (%+v, %v), want OpenAI responses with medium reasoning", defaultOffering, err)
	}
	openRouter, err := Lookup("gpt-5.6-sol", HostOpenRouter, "")
	if err != nil || openRouter.ID != OfferingOpenRouterResponses {
		t.Fatalf("OpenRouter Lookup = (%+v, %v), want OpenRouter responses offering", openRouter, err)
	}
	if _, err := Lookup("gpt-5.6-sol", HostOpenAI, WireMessages); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OpenAI messages Lookup error = %v, want ErrNotFound", err)
	}
}

func TestLookupDeepSeekAndUnknownModel(t *testing.T) {
	// R-JX2R-2DN0
	offering, err := Lookup("deepseek-v4-flash", "", "")
	if err != nil || offering.Host != HostOpenRouter {
		t.Fatalf("DeepSeek Lookup = (%+v, %v), want OpenRouter offering", offering, err)
	}
	if _, err := Lookup("no-such-model", "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown model Lookup error = %v, want ErrNotFound", err)
	}
}

func TestLookupSelectionIsExactAndOrdered(t *testing.T) {
	// R-JOJG-DZG5
	if _, err := Lookup("GPT-5.6-sol", "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("case-mismatched model error = %v, want ErrNotFound", err)
	}
	defaultChat, err := Lookup("gpt-5.6-sol", "", WireChat)
	if err != nil || defaultChat.Host != HostOpenAI || defaultChat.ID != OfferingOpenAIChat {
		t.Fatalf("default-host chat Lookup = (%+v, %v), want exact OpenAI chat offering", defaultChat, err)
	}
	openRouterChat, err := Lookup("gpt-5.6-sol", HostOpenRouter, WireChat)
	if err != nil || openRouterChat.ID != OfferingOpenRouterChat {
		t.Fatalf("exact host/wire Lookup = (%+v, %v), want OpenRouter chat offering", openRouterChat, err)
	}
}

func TestLookupRanksResponsesAboveChat(t *testing.T) {
	// R-JQZ9-5IXJ
	entries := Catalog()
	entryIndex := slices.IndexFunc(entries, func(entry CatalogEntry) bool { return entry.Model == "gpt-5.6-sol" })
	if entryIndex < 0 {
		t.Fatal("Catalog omitted gpt-5.6-sol")
	}
	chatIndex := slices.IndexFunc(entries[entryIndex].Offerings, func(offering Offering) bool {
		return offering.ID == OfferingOpenRouterChat
	})
	responsesIndex := slices.IndexFunc(entries[entryIndex].Offerings, func(offering Offering) bool {
		return offering.ID == OfferingOpenRouterResponses
	})
	if chatIndex < 0 || responsesIndex < 0 || chatIndex >= responsesIndex {
		t.Fatalf("fixture order chat=%d responses=%d, want chat before responses", chatIndex, responsesIndex)
	}
	offering, err := Lookup("gpt-5.6-sol", HostOpenRouter, "")
	if err != nil || offering.ID != OfferingOpenRouterResponses {
		t.Fatalf("empty-wire Lookup = (%+v, %v), want responses despite table order", offering, err)
	}
}

func TestLookupFailuresNameMissedArgument(t *testing.T) {
	// R-JS75-JAO8
	tests := []struct {
		name string
		err  error
		hint string
	}{
		{"model", lookupError("no-such-model", "", ""), "lookup model"},
		{"host", lookupError("claude-sonnet-5", HostGemini, ""), "lookup host"},
		{"wire", lookupError("gpt-5.6-sol", HostOpenAI, WireMessages), "lookup wire"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, ErrNotFound) || !strings.Contains(test.err.Error(), test.hint) {
				t.Fatalf("Lookup error = %v, want wrapped ErrNotFound naming %q", test.err, test.hint)
			}
		})
	}
}

func lookupError(model string, host Host, wire WireName) error {
	_, err := Lookup(model, host, wire)
	return err
}

func TestCatalogExportsNoRetiredQuerySurface(t *testing.T) {
	// R-JES9-BTIL
	assertRootPackageDeclaresNone(t, map[string]bool{
		"Vendor":       true,
		"ProviderID":   true,
		"ResolveModel": true,
		"LookupModel":  true,
		"CatalogFor":   true,
	})
}

// R-JH82-3CZZ
func TestCatalogOfferingsCarryFixedTransport(t *testing.T) {
	wantByID := map[OfferingID]struct {
		host     Host
		name     WireName
		wireType string
		url      string
		auth     []AuthMode
		oauth    OAuthClient
	}{
		OfferingAnthropicMessages:     {HostAnthropic, WireMessages, "*agentkit.anthropicWire", "https://api.anthropic.com/v1/messages", []AuthMode{AuthModeAPIKey}, OAuthClient{}},
		OfferingOpenAIResponses:       {HostOpenAI, WireResponses, "*agentkit.openAIResponsesWire", "https://api.openai.com/v1/responses", []AuthMode{AuthModeAPIKey, AuthModeOAuth}, OAuthClient{"https://auth.openai.com/oauth/token", "app_EMoamEEZ73f0CkXaXp7hrann"}},
		OfferingOpenAIChat:            {HostOpenAI, WireChat, "*agentkit.openAIChatWire", "https://api.openai.com/v1/chat/completions", []AuthMode{AuthModeAPIKey, AuthModeOAuth}, OAuthClient{"https://auth.openai.com/oauth/token", "app_EMoamEEZ73f0CkXaXp7hrann"}},
		OfferingGeminiGenerateContent: {HostGemini, WireGenerateContent, "*agentkit.geminiWire", "", []AuthMode{AuthModeAPIKey}, OAuthClient{}},
		OfferingXAIResponses:          {HostXAI, WireResponses, "*agentkit.responsesWire", "https://api.x.ai/v1/responses", []AuthMode{AuthModeAPIKey, AuthModeOAuth}, OAuthClient{"https://auth.x.ai/oauth2/token", "b1a00492-073a-47ea-816f-4c329264a828"}},
		OfferingXAIChat:               {HostXAI, WireChat, "*agentkit.chatWire", "https://api.x.ai/v1/chat/completions", []AuthMode{AuthModeAPIKey, AuthModeOAuth}, OAuthClient{"https://auth.x.ai/oauth2/token", "b1a00492-073a-47ea-816f-4c329264a828"}},
		OfferingOpenRouterChat:        {HostOpenRouter, WireChat, "*agentkit.chatWire", "https://openrouter.ai/api/v1/chat/completions", []AuthMode{AuthModeAPIKey}, OAuthClient{}},
		OfferingOpenRouterResponses:   {HostOpenRouter, WireResponses, "*agentkit.responsesWire", "https://openrouter.ai/api/v1/responses", []AuthMode{AuthModeAPIKey}, OAuthClient{}},
	}
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			want, ok := wantByID[offering.ID]
			if !ok {
				t.Errorf("%q offering has unknown id %q", entry.Model, offering.ID)
				continue
			}
			wantURL := want.url
			if offering.ID == OfferingGeminiGenerateContent {
				wantURL = "https://generativelanguage.googleapis.com/v1beta/models/" + url.PathEscape(offering.WireModel) + ":streamGenerateContent?alt=sse"
			}
			if offering.Host != want.host || offering.WireName != want.name || reflect.TypeOf(offering.WireFormat).String() != want.wireType || offering.BaseURL != wantURL || !slices.Equal(offering.AuthModes, want.auth) || offering.OAuth != want.oauth {
				t.Errorf("%q offering %q transport = {%q %q %T %q %v %+v}, want {%q %q %s %q %v %+v}", entry.Model, offering.ID, offering.Host, offering.WireName, offering.WireFormat, offering.BaseURL, offering.AuthModes, offering.OAuth, want.host, want.name, want.wireType, wantURL, want.auth, want.oauth)
			}
		}
	}
}

// R-JIFY-H4QO
func TestCatalogAlternateAPIOfferingsArePaired(t *testing.T) {
	for _, entry := range Catalog() {
		assertOfferingPair(t, entry, OfferingOpenAIResponses, OfferingOpenAIChat)
		assertOfferingPair(t, entry, OfferingXAIResponses, OfferingXAIChat)
		assertOfferingPair(t, entry, OfferingOpenRouterChat, OfferingOpenRouterResponses)
	}
}

func assertOfferingPair(t *testing.T, entry CatalogEntry, first, second OfferingID) {
	t.Helper()
	firstIndex := slices.IndexFunc(entry.Offerings, func(offering Offering) bool { return offering.ID == first })
	secondIndex := slices.IndexFunc(entry.Offerings, func(offering Offering) bool { return offering.ID == second })
	if firstIndex < 0 && secondIndex < 0 {
		return
	}
	if firstIndex < 0 || secondIndex < 0 {
		t.Errorf("%q has unpaired alternate APIs %q at index %d and %q at index %d", entry.Model, first, firstIndex, second, secondIndex)
		return
	}
	if firstIndex >= secondIndex {
		t.Errorf("%q has %q at index %d, want before %q at index %d", entry.Model, first, firstIndex, second, secondIndex)
	}
	a, b := entry.Offerings[firstIndex], entry.Offerings[secondIndex]
	if a.WireModel != b.WireModel || a.Context != b.Context || !reflect.DeepEqual(a.Pricing, b.Pricing) || !reflect.DeepEqual(a.Reasoning, b.Reasoning) {
		t.Errorf("%q alternate API offerings on %q and %q differ: first=%+v second=%+v", entry.Model, first, second, a, b)
	}
}

// R-JJNU-UWHD
func TestCatalogEntryFirstOfferingHostPreference(t *testing.T) {
	for _, entry := range Catalog() {
		hasNonOpenRouter := slices.ContainsFunc(entry.Offerings, func(offering Offering) bool { return offering.Host != HostOpenRouter })
		if hasNonOpenRouter && entry.Offerings[0].Host == HostOpenRouter {
			t.Errorf("%q has a non-OpenRouter offering but its first offering's Host is %q", entry.Model, entry.Offerings[0].Host)
		}
	}
}

func TestCatalogReturnsFullSortedStructurallyUniqueTable(t *testing.T) {
	// R-JNBK-07PG
	entries := Catalog()
	wantModels := []string{
		"claude-fable-5", "claude-haiku-4-5", "claude-opus-4-8", "claude-opus-5", "claude-sonnet-4-6", "claude-sonnet-5",
		"deepseek-v4-flash", "deepseek-v4-pro", "gemini-2.5-flash", "gemini-2.5-pro", "gemini-3.1-flash-lite", "gemini-3.1-pro-preview", "gemini-3.5-flash", "gemini-3.7-flash",
		"glm-4.6", "glm-4.7", "glm-5.1", "glm-5.2", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.5", "gpt-5.5-pro", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra",
		"grok-4.20", "grok-4.20-multi-agent", "grok-4.3", "grok-4.5", "grok-4.6", "kimi-k2.6", "kimi-k2.7-code", "kimi-k3", "nemotron-3.5-lightning", "qwen3.8-27b", "qwen3.8-max",
	}
	gotModels := make([]string, len(entries))
	for i, entry := range entries {
		gotModels[i] = entry.Model
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
		ids := make(map[OfferingID]bool, len(entry.Offerings))
		for _, offering := range entry.Offerings {
			if ids[offering.ID] {
				t.Fatalf("catalog entry %q repeats offering id %q", entry.Model, offering.ID)
			}
			ids[offering.ID] = true
		}
	}
}

// R-JTF1-X2EX
func TestCatalogOfferingDataInvariants(t *testing.T) {
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.WireModel == "" {
				t.Errorf("%q offering %q has empty WireModel", entry.Model, offering.ID)
			}
			if offering.Host == HostOpenRouter && !strings.Contains(offering.WireModel, "/") {
				t.Errorf("%q OpenRouter offering has unqualified WireModel %q", entry.Model, offering.WireModel)
			}
			if offering.Context <= 0 {
				t.Errorf("%q offering %q has Context %d, want greater than zero", entry.Model, offering.ID, offering.Context)
			}
			tiers := offering.Pricing.Tiers
			if len(tiers) == 0 {
				t.Errorf("%q offering %q has no pricing tiers", entry.Model, offering.ID)
				continue
			}
			if tiers[0].MinInputTokens != 0 {
				t.Errorf("%q offering %q first pricing tier starts at %d, want zero", entry.Model, offering.ID, tiers[0].MinInputTokens)
			}
			for i, tier := range tiers {
				if i > 0 && tiers[i-1].MinInputTokens >= tier.MinInputTokens {
					t.Errorf("%q offering %q pricing tier %d thresholds not strictly increasing", entry.Model, offering.ID, i)
				}
				if tier.InputUncached <= 0 || tier.Output <= 0 {
					t.Errorf("%q offering %q pricing tier %d has non-positive rates", entry.Model, offering.ID, i)
				}
			}
		}
	}
}

// R-JZIJ-TX4E
func TestCatalogEveryHostIsRepresented(t *testing.T) {
	hosts := []Host{HostAnthropic, HostOpenAI, HostGemini, HostXAI, HostOpenRouter}
	seen := make(map[Host]bool, len(hosts))
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			seen[offering.Host] = true
		}
	}
	for _, host := range hosts {
		if !seen[host] {
			t.Errorf("Catalog() has no offering with Host %q", host)
		}
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

func TestReasoningSpecAcceptsSignature(t *testing.T) {
	// R-O8QA-6I1V
	assertReasoningSpecAcceptsSignature(t, ReasoningSpec.Accepts)
}

func TestReasoningSpecShape(t *testing.T) {
	// R-O11N-T3ME
	assertStructShape(t, ReasoningSpec{}, []fieldShape{
		{"Kind", reflect.TypeOf(ReasoningKind(0))},
		{"Term", reflect.TypeOf("")},
		{"Levels", reflect.TypeOf([]Effort(nil))},
		{"MinBudget", reflect.TypeOf(int(0))},
		{"MaxBudget", reflect.TypeOf(int(0))},
		{"CanEnable", reflect.TypeOf(false)},
		{"CanDisable", reflect.TypeOf(false)},
		{"Default", reflect.TypeOf(ReasoningConfig{})},
	})
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

func TestCatalogReasoningTermVocabulary(t *testing.T) {
	// R-O29K-6VD3
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			term := offering.Reasoning.Term
			switch offering.Reasoning.Kind {
			case ReasoningKindEffort:
				if term != "effort" && term != "thinking_level" {
					t.Errorf("%q offering on %q has effort-kind Term %q, want \"effort\" or \"thinking_level\"", entry.Model, offering.ID, term)
				}
			case ReasoningKindBudget:
				if term != "thinking_budget" {
					t.Errorf("%q offering on %q has budget-kind Term %q, want \"thinking_budget\"", entry.Model, offering.ID, term)
				}
			case ReasoningKindToggle:
				if term != "thinking" {
					t.Errorf("%q offering on %q has toggle-kind Term %q, want \"thinking\"", entry.Model, offering.ID, term)
				}
			case ReasoningKindNone:
				if term != "" {
					t.Errorf("%q offering on %q has none-kind Term %q, want empty", entry.Model, offering.ID, term)
				}
			}
		}
	}
}

func TestCatalogReasoningVocabularySendable(t *testing.T) {
	// R-W8QS-PJR3
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			validator, ok := offering.WireFormat.(interface{ validateSettings(Settings) error })
			if !ok {
				t.Fatalf("%q offering %q: WireFormat has no validateSettings", entry.Model, offering.ID)
			}

			var vocabulary []ReasoningConfig
			spec := offering.Reasoning
			if spec.CanDisable {
				vocabulary = append(vocabulary, ReasoningConfig{Mode: ReasoningOff})
			}
			if spec.Kind == ReasoningKindToggle && spec.CanEnable {
				vocabulary = append(vocabulary, ReasoningConfig{Mode: ReasoningOn})
			}
			for _, level := range spec.Levels {
				vocabulary = append(vocabulary, ReasoningConfig{Mode: ReasoningEffort, Effort: level})
			}
			if spec.Kind == ReasoningKindBudget {
				vocabulary = append(vocabulary,
					ReasoningConfig{Mode: ReasoningBudget, Budget: spec.MinBudget},
					ReasoningConfig{Mode: ReasoningBudget, Budget: spec.MaxBudget},
				)
			}
			vocabulary = append(vocabulary, spec.Default)

			for _, c := range vocabulary {
				settings := Settings{Options: Options{spec.Term: c.String()}}
				if err := validator.validateSettings(settings); err != nil {
					t.Errorf("%q offering %q: %s=%q rejected: %v", entry.Model, offering.ID, spec.Term, settings.Options[spec.Term], err)
				}
			}
		}
	}
}

func TestCatalogReasoningTermPinnedModels(t *testing.T) {
	// R-O3HG-KN3S
	wantTerm := map[string]string{
		"gemini-3.5-flash":       "thinking_level",
		"gemini-3.7-flash":       "thinking_level",
		"gemini-3.1-flash-lite":  "thinking_level",
		"gemini-3.1-pro-preview": "thinking_level",
		"claude-opus-5":          "effort",
		"gemini-2.5-flash":       "thinking_budget",
		"claude-haiku-4-5":       "thinking_budget",
		"deepseek-v4-pro":        "thinking",
		"grok-4.20":              "thinking",
	}
	for _, entry := range Catalog() {
		want, ok := wantTerm[entry.Model]
		if !ok {
			continue
		}
		for _, offering := range entry.Offerings {
			if offering.Reasoning.Term != want {
				t.Errorf("%q offering on %q has Term %q, want %q", entry.Model, offering.ID, offering.Reasoning.Term, want)
			}
		}
	}
}

func TestOAuthClientShape(t *testing.T) {
	// R-0702-EGC7
	assertStructShape(t, OAuthClient{}, []fieldShape{
		{"TokenURL", reflect.TypeOf("")},
		{"ClientID", reflect.TypeOf("")},
	})
}

// TestCatalogReasoningInvariants is the reasoning half of the same table-wide
// sweep: the requirement fixes the relationship (default accepted, minimum
// below maximum), and the pinned fixture tests carry the exact defaults.
func TestCatalogReasoningInvariants(t *testing.T) {
	// R-OM56-DZ7I
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			spec := offering.Reasoning
			if !spec.Accepts(spec.Default) {
				t.Errorf("%q offering on %q does not accept its reasoning default %+v", entry.Model, offering.ID, spec.Default)
			}

			switch spec.Kind {
			case ReasoningKindEffort:
				if len(spec.Levels) == 0 {
					t.Errorf("%q effort offering on %q has no reasoning levels", entry.Model, offering.ID)
				}
				seen := make(map[Effort]bool, len(spec.Levels))
				for _, level := range spec.Levels {
					if seen[level] {
						t.Errorf("%q effort offering on %q repeats reasoning level %v", entry.Model, offering.ID, level)
					}
					seen[level] = true
				}
			case ReasoningKindBudget:
				if spec.MinBudget >= spec.MaxBudget {
					t.Errorf("%q budget offering on %q has range [%d, %d], want minimum less than maximum", entry.Model, offering.ID, spec.MinBudget, spec.MaxBudget)
				}
			case ReasoningKindNone:
				if spec.CanEnable || spec.CanDisable {
					t.Errorf("%q none-kind offering on %q has enable flags (%t, %t), want both false", entry.Model, offering.ID, spec.CanEnable, spec.CanDisable)
				}
				if len(spec.Levels) != 0 {
					t.Errorf("%q none-kind offering on %q has reasoning levels %v, want none", entry.Model, offering.ID, spec.Levels)
				}
			}
		}
	}
}

// TestCatalogOAuthAuthModeInvariant verifies that OAuth is advertised exactly
// when the offering carries both fields required to refresh credentials.
// R-0ANR-JRKA
func TestCatalogOAuthAuthModeInvariant(t *testing.T) {
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			hasOAuthMode := slices.Contains(offering.AuthModes, AuthModeOAuth)
			hasOAuthClient := offering.OAuth.TokenURL != "" && offering.OAuth.ClientID != ""
			if hasOAuthMode != hasOAuthClient {
				t.Errorf("%q offering on %q has AuthModes %v and OAuth %+v, want OAuth mode iff both OAuth fields are non-empty", entry.Model, offering.ID, offering.AuthModes, offering.OAuth)
			}
		}
	}
}

// R-PSVE-2RD8
func TestResolveModelAuthModesAreDefensiveCopies(t *testing.T) {
	first, err := Lookup("claude-sonnet-5", "", "")
	if err != nil || len(first.AuthModes) == 0 {
		t.Fatalf("Lookup(claude-sonnet-5) = (%+v, %v), want offering with authentication modes", first, err)
	}
	want := []AuthMode{AuthModeAPIKey}
	first.AuthModes[0] = AuthMode("mutated")

	second, err := Lookup("claude-sonnet-5", "", "")
	if err != nil || !slices.Equal(second.AuthModes, want) {
		t.Fatalf("AuthModes after mutating prior Lookup result = (%v, %v), want (%v, nil)", second.AuthModes, err, want)
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
	assertCatalogEntryUnchanged(t, model, want)

	offering, err := Lookup(model, "", "")
	if err != nil {
		t.Fatalf("Lookup omitted %q: %v", model, err)
	}
	offering.Pricing.Tiers[0].Output = -1
	offering.Reasoning.Levels[0] = Effort(99)
	got, err := Lookup(model, "", "")
	if err != nil || !offeringEqual(got, want.Offerings[0]) {
		t.Fatalf("Lookup changed after mutating prior result: (%+v, %v)", got, err)
	}
}

func expectedClaudeSonnet5() CatalogEntry {
	levels := []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	reasoning := ReasoningSpec{
		Kind:       ReasoningKindEffort,
		Term:       "effort",
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
		Model: "claude-sonnet-5",
		Offerings: []Offering{
			{
				ID:         OfferingAnthropicMessages,
				Host:       HostAnthropic,
				WireName:   WireMessages,
				WireFormat: nil,
				BaseURL:    "https://api.anthropic.com/v1/messages",
				AuthModes:  []AuthMode{AuthModeAPIKey},
				WireModel:  "claude-sonnet-5",
				Context:    1_000_000,
				Pricing:    pricing,
				Reasoning:  reasoning,
			},
			{
				ID:         OfferingOpenRouterChat,
				Host:       HostOpenRouter,
				WireName:   WireChat,
				WireFormat: nil,
				BaseURL:    "https://openrouter.ai/api/v1/chat/completions",
				AuthModes:  []AuthMode{AuthModeAPIKey},
				WireModel:  "anthropic/claude-sonnet-5",
				Context:    1_000_000,
				Pricing:    pricing,
				Reasoning:  reasoning,
			},
			{
				ID:         OfferingOpenRouterResponses,
				Host:       HostOpenRouter,
				WireName:   WireResponses,
				WireFormat: nil,
				BaseURL:    "https://openrouter.ai/api/v1/responses",
				AuthModes:  []AuthMode{AuthModeAPIKey},
				WireModel:  "anthropic/claude-sonnet-5",
				Context:    1_000_000,
				Pricing:    pricing,
				Reasoning:  reasoning,
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

func assertCatalogEntryUnchanged(t *testing.T, model string, want CatalogEntry) {
	t.Helper()
	entries := Catalog()
	index := slices.IndexFunc(entries, func(entry CatalogEntry) bool { return entry.Model == model })
	if index < 0 || !catalogEntryEqual(entries[index], want) {
		t.Fatalf("Catalog changed after mutating another query result: %+v", entries)
	}
}

func catalogEntryEqual(got, want CatalogEntry) bool {
	if got.Model != want.Model || len(got.Offerings) != len(want.Offerings) {
		return false
	}
	for index := range got.Offerings {
		if !offeringEqual(got.Offerings[index], want.Offerings[index]) {
			return false
		}
	}
	return true
}

func offeringEqual(got, want Offering) bool {
	wireTypes := map[OfferingID]string{
		OfferingAnthropicMessages:   "*agentkit.anthropicWire",
		OfferingOpenRouterChat:      "*agentkit.chatWire",
		OfferingOpenRouterResponses: "*agentkit.responsesWire",
	}
	if reflect.TypeOf(got.WireFormat).String() != wireTypes[want.ID] {
		return false
	}
	got.WireFormat = nil
	want.WireFormat = nil
	return reflect.DeepEqual(got, want)
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
