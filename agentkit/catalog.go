package agentkit

import (
	"cmp"
	"slices"
)

// ProviderID names the vendor constructor package that serves an offering.
// It is a value type, distinct from the Provider SPI interface (D6). Values
// are the package names, and are also what a vendor package's New names its
// endpoint with its vendor auth adapter, so Identity.Endpoint equals the ProviderID of
// the offering that prices the conversation.
type ProviderID string

// Provider identifiers for the built-in vendor constructor packages.
const (
	ProviderAnthropic  ProviderID = "anthropic"
	ProviderOpenAI     ProviderID = "openai"
	ProviderGemini     ProviderID = "gemini"
	ProviderXAI        ProviderID = "xai"
	ProviderOpenRouter ProviderID = "openrouter"
)

// Vendor names a model's creator, in OpenRouter namespace spelling, so an
// OpenRouter wire model is conventionally Vendor + "/" + Model.
type Vendor string

// Vendor names in OpenRouter namespace spelling.
const (
	VendorAnthropic Vendor = "anthropic"
	VendorOpenAI    Vendor = "openai"
	VendorGoogle    Vendor = "google"
	VendorXAI       Vendor = "x-ai"
	VendorZAI       Vendor = "z-ai"
	VendorDeepSeek  Vendor = "deepseek"
	VendorMoonshot  Vendor = "moonshotai"
	VendorNVIDIA    Vendor = "nvidia"
	VendorQwen      Vendor = "qwen"
)

// ReasoningKind is the shape of an offering's native reasoning control.
type ReasoningKind int

// Native reasoning-control shapes.
const (
	ReasoningKindNone   ReasoningKind = iota // no reasoning control at all
	ReasoningKindEffort                      // an enumerated effort level
	ReasoningKindBudget                      // an integer token budget
	ReasoningKindToggle                      // bare on/off
)

// ReasoningSpec is one offering's reasoning vocabulary in the neutral D8
// model. Levels is read for the effort kind, MinBudget/MaxBudget for the
// budget kind, CanEnable for the toggle kind, CanDisable for any kind.
// Default is the request an application should make when the user has not
// chosen; ReasoningDefault as the Default means the vendor's own dynamic
// behavior. Accepts reports whether a request is inside the vocabulary.
type ReasoningSpec struct {
	Kind       ReasoningKind
	Levels     []Effort
	MinBudget  int
	MaxBudget  int
	CanEnable  bool
	CanDisable bool
	Default    ReasoningConfig
}

// Accepts reports whether r is inside the offering's reasoning vocabulary.
func (s ReasoningSpec) Accepts(r ReasoningConfig) bool {
	switch r.Mode {
	case ReasoningDefault:
		return true
	case ReasoningOff:
		return s.CanDisable
	case ReasoningOn:
		return s.Kind == ReasoningKindToggle && s.CanEnable
	case ReasoningEffort:
		if s.Kind != ReasoningKindEffort {
			return false
		}
		for _, level := range s.Levels {
			if r.Effort == level {
				return true
			}
		}
		return false
	case ReasoningBudget:
		return s.Kind == ReasoningKindBudget &&
			s.MinBudget <= r.Budget && r.Budget <= s.MaxBudget
	default:
		return false
	}
}

// Offering is one model as served by one provider.
type Offering struct {
	Provider  ProviderID
	WireModel string  // the exact model string sent on the wire
	Context   int64   // context window in tokens
	Pricing   Pricing // full price schedule (D3); never empty
	Reasoning ReasoningSpec
}

// CatalogEntry is everything the catalog knows about one model name. The
// first offering is the default provider.
type CatalogEntry struct {
	Model     string
	Vendor    Vendor
	Offerings []Offering
}

// Catalog returns every known model, sorted by model name.
func Catalog() []CatalogEntry {
	entries := make([]CatalogEntry, len(catalogTable))
	for index, entry := range catalogTable {
		entries[index] = cloneCatalogEntry(entry)
	}
	sortCatalogEntries(entries)
	return entries
}

// CatalogFor returns the known models offered by p, sorted by model name.
func CatalogFor(p ProviderID) []CatalogEntry {
	entries := make([]CatalogEntry, 0)
	for _, entry := range catalogTable {
		if _, ok := findOffering(entry, p); ok {
			entries = append(entries, cloneCatalogEntry(entry))
		}
	}
	sortCatalogEntries(entries)
	return entries
}

// LookupModel returns the catalog entry whose model name exactly matches model.
func LookupModel(model string) (CatalogEntry, bool) {
	for _, entry := range catalogTable {
		if entry.Model == model {
			return cloneCatalogEntry(entry), true
		}
	}
	return CatalogEntry{}, false
}

// ResolveModel returns the default offering for model, or its offering from p.
func ResolveModel(model string, p ProviderID) (Offering, bool) {
	for _, entry := range catalogTable {
		if entry.Model != model {
			continue
		}
		if p == "" {
			if len(entry.Offerings) == 0 {
				return Offering{}, false
			}
			return cloneOffering(entry.Offerings[0]), true
		}
		offering, ok := findOffering(entry, p)
		if !ok {
			return Offering{}, false
		}
		return cloneOffering(offering), true
	}
	return Offering{}, false
}

func findOffering(entry CatalogEntry, p ProviderID) (Offering, bool) {
	for _, offering := range entry.Offerings {
		if offering.Provider == p {
			return offering, true
		}
	}
	return Offering{}, false
}

func cloneCatalogEntry(entry CatalogEntry) CatalogEntry {
	offerings := entry.Offerings
	entry.Offerings = make([]Offering, len(offerings))
	for index, offering := range offerings {
		entry.Offerings[index] = cloneOffering(offering)
	}
	return entry
}

func cloneOffering(offering Offering) Offering {
	offering.Pricing.Tiers = slices.Clone(offering.Pricing.Tiers)
	offering.Reasoning = cloneReasoningSpec(offering.Reasoning)
	return offering
}

func cloneReasoningSpec(spec ReasoningSpec) ReasoningSpec {
	if spec.Kind != ReasoningKindToggle {
		spec.Levels = slices.Clone(spec.Levels)
		return spec
	}

	cloned := toggleSpec(spec.CanEnable, spec.CanDisable, spec.Default)
	cloned.Levels = slices.Clone(spec.Levels)
	cloned.MinBudget = spec.MinBudget
	cloned.MaxBudget = spec.MaxBudget
	return cloned
}

func sortCatalogEntries(entries []CatalogEntry) {
	slices.SortFunc(entries, func(left, right CatalogEntry) int {
		return cmp.Compare(left.Model, right.Model)
	})
}
