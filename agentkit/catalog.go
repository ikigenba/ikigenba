package agentkit

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// OfferingID is the catalog's key for one host/wire-format pair, spelled
// "<host>-<wire format>". A conversation built from the offering reports it
// as Identity.Endpoint (D1, D7); cost matches on it (D3). Users never type it.
type OfferingID string

// Offering ids for the built-in transports.
const (
	OfferingAnthropicMessages     OfferingID = "anthropic-messages"
	OfferingOpenAIResponses       OfferingID = "openai-responses"
	OfferingOpenAIChat            OfferingID = "openai-chat"
	OfferingGeminiGenerateContent OfferingID = "gemini-generate-content"
	OfferingXAIResponses          OfferingID = "xai-responses"
	OfferingXAIChat               OfferingID = "xai-chat"
	OfferingOpenRouterChat        OfferingID = "openrouter-chat"
	OfferingOpenRouterResponses   OfferingID = "openrouter-responses"
)

// Host names who serves an offering. It is the second Lookup argument.
type Host string

// Host names.
const (
	HostAnthropic  Host = "anthropic"
	HostOpenAI     Host = "openai"
	HostGemini     Host = "gemini"
	HostXAI        Host = "xai"
	HostOpenRouter Host = "openrouter"
)

// WireName names the wire format an offering speaks. It is the third Lookup
// argument. Two constructors may share a name (D5): ChatWire and
// OpenAIChatWire are both "chat"; the offering says which codec applies.
type WireName string

// Wire format names.
const (
	WireMessages        WireName = "messages"
	WireGenerateContent WireName = "generate-content"
	WireChat            WireName = "chat"
	WireResponses       WireName = "responses"
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
	Term       string
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

// Rotation is what an OAuth rotation needs beyond the stored token: where
// the refresh request is sent and the app id it must present (D22). It is
// zero on every EndpointSpec whose AuthMode is api_key.
type Rotation struct {
	RefreshURL string
	ClientID   string
}

// EndpointSpec is one way to reach an offering: the credential kind, where
// requests go under it, and how that credential is rotated. An offering has
// one spec per credential kind it accepts; the URL belongs here, not on the
// offering, because it can differ by credential kind.
type EndpointSpec struct {
	AuthMode AuthMode
	BaseURL  string   // where chat requests are sent, model-in-path baked in
	Rotation Rotation // how to get a fresh token; zero for api_key
}

// Offering is one model as served by one host over one wire format:
// everything New needs.
type Offering struct {
	ID              OfferingID
	Host            Host
	WireName        WireName
	WireFormat      WireFormat     // the codec to pass to New
	Endpoints       []EndpointSpec // one per credential kind, api_key first
	WireModel       string         // the exact model string sent on the wire
	Context         int64          // context window in tokens
	MaxOutputTokens int64          // the vendor's output cap; zero when unknown
	Pricing         Pricing        // full price schedule (D3); never empty
	Reasoning       ReasoningSpec
}

// CatalogEntry is everything the catalog knows about one model name. The
// first offering is the default host.
type CatalogEntry struct {
	Model     string
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

// ErrNotFound is what Lookup wraps when no offering matches.
var ErrNotFound = errors.New("agentkit: catalog entry not found")

// offeringMaxOutputTokens returns the MaxOutputTokens of the catalog
// offering whose ID equals identity.Endpoint and whose WireModel equals
// identity.Model, and whether such an offering exists. It does not require
// the offering's MaxOutputTokens to be non-zero; callers that need a
// sendable value check that themselves.
func offeringMaxOutputTokens(identity Identity) (int64, bool) {
	for _, entry := range catalogTable {
		for _, candidate := range entry.Offerings {
			if string(candidate.ID) == identity.Endpoint && candidate.WireModel == identity.Model {
				return candidate.MaxOutputTokens, true
			}
		}
	}
	return 0, false
}

// Lookup is the catalog's shorthand: given a model name and optionally a
// host and wire format, it resolves the one matching Offering. An empty
// host means the entry's default host (its first offering's Host); an empty
// wire means the highest-ranked wire format the chosen host offers for that
// model, ranked responses above chat. Anything that fails to match wraps
// ErrNotFound naming the argument that missed.
func Lookup(model string, host Host, wire WireName) (Offering, error) {
	matchedEntry := findEntry(model)
	if matchedEntry == nil {
		return Offering{}, fmt.Errorf("agentkit: lookup model %q: %w", model, ErrNotFound)
	}

	effectiveHost := host
	if effectiveHost == "" && len(matchedEntry.Offerings) != 0 {
		effectiveHost = matchedEntry.Offerings[0].Host
	}

	chosen := findOffering(*matchedEntry, effectiveHost, wire)
	if chosen != nil {
		return cloneOffering(*chosen), nil
	}
	if !hostOffered(*matchedEntry, effectiveHost) {
		return Offering{}, fmt.Errorf("agentkit: lookup host %q: %w", host, ErrNotFound)
	}
	return Offering{}, fmt.Errorf("agentkit: lookup wire %q: %w", wire, ErrNotFound)
}

func findEntry(model string) *CatalogEntry {
	for index := range catalogTable {
		if catalogTable[index].Model == model {
			return &catalogTable[index]
		}
	}
	return nil
}

func findOffering(entry CatalogEntry, host Host, wire WireName) *Offering {
	var chosen *Offering
	for index := range entry.Offerings {
		offering := &entry.Offerings[index]
		if offering.Host != host {
			continue
		}
		if wire != "" {
			if offering.WireName == wire {
				return offering
			}
			continue
		}
		if chosen == nil || wireRank(offering.WireName) > wireRank(chosen.WireName) {
			chosen = offering
		}
	}
	return chosen
}

func hostOffered(entry CatalogEntry, host Host) bool {
	return slices.ContainsFunc(entry.Offerings, func(offering Offering) bool {
		return offering.Host == host
	})
}

func wireRank(wire WireName) int {
	switch wire {
	case WireResponses:
		return 2
	case WireChat:
		return 1
	default:
		return 0
	}
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
	offering.Endpoints = slices.Clone(offering.Endpoints)
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
