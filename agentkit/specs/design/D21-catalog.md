# D21-catalog

The catalog is agentkit's static knowledge of the chat models it is commonly
pointed at: for each model name, who made it, which providers serve it, and —
per provider — the exact model string that goes on the wire, the context
window, the full price schedule, and the reasoning vocabulary the offering
accepts along with its default. It exists so an application can offer a
**shorthand**: a user names a model, and everything else needed to assemble the
conversation is looked up. `agentrepl --help` is the reference consumer — its
provider sections, reasoning clauses, and default markers are all queries
against this table.

The catalog is **authoritative for three things**, and each is a requirement
below, not a convention:

- **The default provider.** An entry's offerings are ordered, and the first is
  the provider a bare model name resolves to: `ResolveModel(model, "")` returns
  it. A user who says only `model=claude-sonnet-5` gets Anthropic.
- **The default reasoning.** Every offering carries `Reasoning.Default`, the
  request to make when the user has not chosen a level, and it is guaranteed to
  be inside that offering's own vocabulary. `claude-sonnet-5` on Anthropic
  defaults to effort medium; `gpt-5.4` defaults to effort none;
  `gemini-2.5-flash` defaults to the vendor's dynamic budget.
- **Cost.** It is the *only* rate table — a turn is priced from the wire's own
  figure, else from the catalog offering matching the conversation, else zero
  (D3). There is no consumer-supplied price and no other table. That is why the
  catalog lives in the root package rather than a sibling: the conversation
  prices itself by its own `Identity`, and the root cannot import a subpackage
  that imports it.

What it is **not** is a gate. A model absent from the table still reaches the
vendor verbatim (D1), and a cataloged model may be sent to a provider the table
does not list; such a turn simply has no default to offer and prices to zero.

The shape:

```go
package agentkit

// ProviderID names the vendor constructor package that serves an offering.
// It is a value type, distinct from the Provider SPI interface (D6). Values
// are the package names, and are also what a vendor package's New names its
// endpoint with (D6 WithName), so Identity.Endpoint equals the ProviderID of
// the offering that prices the conversation.
type ProviderID string

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

func (s ReasoningSpec) Accepts(r ReasoningConfig) bool

// Offering is one model as served by one provider.
type Offering struct {
	Provider  ProviderID
	WireModel string        // the exact model string sent on the wire
	Context   int64         // context window in tokens
	Pricing   Pricing       // full price schedule (D3); never empty
	Reasoning ReasoningSpec
}

// CatalogEntry is everything the catalog knows about one model name. The
// first offering is the default provider.
type CatalogEntry struct {
	Model     string
	Vendor    Vendor
	Offerings []Offering
}

func Catalog() []CatalogEntry                                    // every entry, sorted by Model
func CatalogFor(p ProviderID) []CatalogEntry                       // entries with an offering on p, sorted
func LookupModel(model string) (CatalogEntry, bool)              // one entry by model name
func ResolveModel(model string, p ProviderID) (Offering, bool)     // p == "" means the default offering
```

`ResolveModel` is the shorthand. `ResolveModel("claude-sonnet-5", "")` yields
the Anthropic offering: build with `anthropic.New`, send `claude-sonnet-5`,
default effort medium. `ResolveModel("claude-sonnet-5", ProviderOpenRouter)`
yields a different offering entirely: build with `openrouter.New`, send
`anthropic/claude-sonnet-5`. The provider override changes which constructor is
called, which wire model is sent, which price schedule applies, and which
reasoning vocabulary is checked — the application reads all four from the one
`Offering`. A `false` result is not an error; the application decides whether
to refuse or to pass the pair through unvalidated and unpriced.

Auth methods, environment-variable names, and auth files are the application's
business and are deliberately not here: the catalog knows models and
offerings, and the vendor packages (D7) know credentials.

The table itself is data, not contract: entries are added, repriced, and
retired without touching this document. What the contract fixes is the shape
above and a set of invariants every entry must satisfy — the table is complete
on cost, every default is inside its own vocabulary, wire names are non-empty
— so the table can grow freely while staying trustworthy. A pair of pinned
entries anchors resolution with real fixtures.

The seed table is authored by hand, not by the build loop: it lives at
`specs/_data/catalog_table.go` (a directory Go tooling ignores) and is
installed verbatim as the root package's `catalog_table.go`. Repricing or
adding a model is an edit to that file and nothing else.

## REQUIREMENTS

- R-EFVU-QV44: `agentkit` MUST export `type ProviderID string` with the constants `ProviderAnthropic = "anthropic"`, `ProviderOpenAI = "openai"`, `ProviderGemini = "gemini"`, `ProviderXAI = "xai"`, and `ProviderOpenRouter = "openrouter"`, distinct from the `Provider` SPI interface (D6).
- R-O52L-16TS: `agentkit` MUST export `type Vendor string` with the constants `VendorAnthropic = "anthropic"`, `VendorOpenAI = "openai"`, `VendorGoogle = "google"`, `VendorXAI = "x-ai"`, `VendorZAI = "z-ai"`, `VendorDeepSeek = "deepseek"`, `VendorMoonshot = "moonshotai"`, `VendorNVIDIA = "nvidia"`, and `VendorQwen = "qwen"`.
- R-O6AH-EYKH: `agentkit` MUST export `type ReasoningKind int` with the constants `ReasoningKindNone`, `ReasoningKindEffort`, `ReasoningKindBudget`, `ReasoningKindToggle` declared in that `iota` order starting at 0.
- R-O7ID-SQB6: `agentkit` MUST export `type ReasoningSpec struct { Kind ReasoningKind; Levels []Effort; MinBudget int; MaxBudget int; CanEnable bool; CanDisable bool; Default ReasoningConfig }` with exactly those fields.
- R-O8QA-6I1V: `agentkit` MUST export `func (s ReasoningSpec) Accepts(r ReasoningConfig) bool`.
- R-EH3R-4MUT: `agentkit` MUST export `type Offering struct { Provider ProviderID; WireModel string; Context int64; Pricing Pricing; Reasoning ReasoningSpec }` with exactly those fields.
- R-OB62-Y1J9: `agentkit` MUST export `type CatalogEntry struct { Model string; Vendor Vendor; Offerings []Offering }` with exactly those fields.
- R-EIBN-IELI: `agentkit` MUST export `func Catalog() []CatalogEntry`, `func CatalogFor(p ProviderID) []CatalogEntry`, `func LookupModel(model string) (CatalogEntry, bool)`, and `func ResolveModel(model string, p ProviderID) (Offering, bool)`.
- R-ODLV-PL0N: `Catalog()` MUST return every entry sorted ascending by `Model`, with no two entries sharing a `Model`, every entry holding at least one offering, and no two offerings of one entry sharing a `Provider`.
- R-OG1O-H4I1: `CatalogFor(p)` MUST return exactly the entries that hold an offering whose `Provider` is `p`, sorted ascending by `Model`, and `LookupModel` MUST return the entry whose `Model` equals its argument or `false`.
- R-OH9K-UW8Q: `ResolveModel(model, "")` MUST return the entry's first offering; `ResolveModel(model, p)` with a non-empty `p` MUST return the entry's offering whose `Provider` is `p`; an unknown model or an entry with no offering on `p` MUST return `false`.
- R-OIHH-8NZF: Every `CatalogEntry` and `Offering` returned by the catalog functions MUST be a copy, such that mutating a returned value's `Offerings`, `Pricing.Tiers`, or `Reasoning.Levels` has no effect on any later call.
- R-OJPD-MFQ4: Every offering in the table MUST have a non-empty `WireModel` (containing a `/` when `Provider` is `ProviderOpenRouter`), a `Context` greater than zero, and a `Pricing` with at least one tier whose first tier's `MinInputTokens` is zero, whose tiers have strictly increasing `MinInputTokens`, and whose every tier has `InputUncached` and `Output` greater than zero.
- R-OKXA-07GT: `ReasoningSpec.Accepts` MUST return true for `ReasoningDefault` always; for `ReasoningOff` iff `CanDisable`; for `ReasoningOn` iff `Kind` is `ReasoningKindToggle` and `CanEnable`; for `ReasoningEffort` iff `Kind` is `ReasoningKindEffort` and the level is in `Levels`; for `ReasoningBudget` iff `Kind` is `ReasoningKindBudget` and `MinBudget <= Budget <= MaxBudget`; and false otherwise.
- R-OM56-DZ7I: Every offering's `Reasoning.Default` MUST be accepted by its own `Reasoning`; an effort-kind spec MUST have non-empty `Levels` with no duplicates; a budget-kind spec MUST have `MinBudget` less than `MaxBudget`; a none-kind spec MUST have `CanEnable` and `CanDisable` false and empty `Levels`.
- R-OND2-RQY7: `ResolveModel("claude-sonnet-5", "")` MUST return an offering with `Provider` `ProviderAnthropic` and `WireModel` `"claude-sonnet-5"`; `ResolveModel("claude-sonnet-5", ProviderOpenRouter)` MUST return an offering with `WireModel` `"anthropic/claude-sonnet-5"`; and `ResolveModel("claude-sonnet-5", ProviderGemini)` MUST return `false`.
- R-OOKZ-5IOW: `ResolveModel("gpt-5.6-sol", "")` MUST return an offering with `Provider` `ProviderOpenAI`, `WireModel` `"gpt-5.6-sol"`, and a `Reasoning.Default` of `ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium}`.
- R-OPSV-JAFL: The catalog MUST NOT gate construction or `Send`: a conversation for a model or provider/model pair that `ResolveModel` reports `false` for MUST construct and send exactly as a cataloged one does, differing only in pricing to zero (D3).
- R-EKRG-9Y2W: Each vendor package's `New` (`anthropic`, `openai`, `gemini`, `xai`, `openrouter`) MUST construct its endpoint with `WithName` set to its own `ProviderID` value, so that the resulting conversation's `Identity.Endpoint` equals that `ProviderID` and a cataloged wire model prices from its offering.
- R-ELZC-NPTL: For each of the five `ProviderID` constants, `CatalogFor` MUST return a non-empty result.
