# D21-catalog

The catalog is agentkit's static knowledge of the chat models it is commonly
pointed at: for each model name, which hosts serve it over which wire formats,
and — per offering — the exact model string that goes on the wire, the context
window, the full price schedule, and the reasoning vocabulary the offering
accepts along with its default. It exists so an application can offer a
**shorthand**: a user names a model, optionally a host and a wire format, and
everything else needed to assemble the conversation is looked up.
`agentrepl --help` is the reference consumer — its host sections, reasoning
clauses, and default markers are all queries against this table.

Three words are used precisely:

- **Host** — who serves the request: `anthropic`, `openai`, `gemini`, `xai`,
  `openrouter`. A host is the second thing a user types.
- **Wire format** — the HTTP protocol spoken: `messages`, `generate-content`,
  `chat`, `responses`. Its name is the third thing a user types; the codec
  object that speaks it (D5) is what `New` takes. Both travel on the offering.
- **Offering id** — the catalog's own key for one host/wire-format pair,
  spelled `<host>-<wire format>`: `openrouter-responses`. It is never typed by
  a user. It is what a conversation reports as `Identity.Endpoint` (D1, D7), so
  the conversation prices itself from the offering whose id matches (D3).

The catalog is **authoritative for three things**, and each is a requirement
below, not a convention:

- **The default host.** An entry's offerings are ordered and the first names
  the default host: the model's own vendor when the catalog has that host, else
  OpenRouter. `Lookup(model, "", "")` resolves to it. A user who says only
  `claude-sonnet-5` gets Anthropic; one who says only `deepseek-v4-flash` gets
  OpenRouter.
- **The default wire format.** When a user names no wire format, the newest
  and most capable one the host offers for that model is chosen. The rank is
  fixed in code, not in table order: `responses` outranks `chat`. So
  `Lookup("gpt-5.6-sol", "openrouter", "")` is the responses offering even
  though the table may list chat first.
- **Cost.** It is the *only* rate table — a turn is priced from the wire's own
  figure, else from the catalog offering matching the conversation, else zero
  (D3). That is why the catalog lives in the root package rather than a
  sibling: the conversation prices itself by its own `Identity`, and the root
  cannot import a subpackage that imports it.

What it is **not** is a gate. A model absent from the table still reaches the
vendor verbatim (D1), and a cataloged model may be sent to a host the table
does not list; such a turn simply has no default to offer and prices to zero.

The shape:

```go
package agentkit

// OfferingID is the catalog's key for one host/wire-format pair, spelled
// "<host>-<wire format>". A conversation built from the offering reports it
// as Identity.Endpoint (D1, D7); cost matches on it (D3). Users never type it.
type OfferingID string

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

const (
	WireMessages        WireName = "messages"
	WireGenerateContent WireName = "generate-content"
	WireChat            WireName = "chat"
	WireResponses       WireName = "responses"
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
// model. Term is the vendor's own word for the knob — "effort",
// "thinking_level", "thinking_budget", or "thinking" — which an application
// prints as the option's name and accepts as the key a user types; it is a
// model datum, so a Gemini 3.x model keeps "thinking_level" on its OpenRouter
// offerings too. Levels is read for the effort kind, MinBudget/MaxBudget for
// the budget kind, CanEnable for the toggle kind, CanDisable for any kind.
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

func (s ReasoningSpec) Accepts(r ReasoningConfig) bool

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
// offering, because it can differ by credential kind (OpenAI's platform API
// takes an API key; only the Codex backend honors a ChatGPT OAuth token).
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

func (o Offering) Authenticator(r Rotator) (Authenticator, error)  // D7

// CatalogEntry is everything the catalog knows about one model name. The
// first offering is the default host.
type CatalogEntry struct {
	Model     string
	Offerings []Offering
}

// ErrNotFound is what Lookup wraps when no offering matches.
var ErrNotFound = errors.New("agentkit: catalog entry not found")

func Catalog() []CatalogEntry                                           // every entry, sorted by Model
func Lookup(model string, host Host, wire WireName) (Offering, error)   // "" for host or wire means the default
```

`Lookup` is the shorthand. The model is an exact match. The host, when given,
is an exact match; when empty it is the entry's default host. The wire name,
when given, is an exact match; when empty it is the highest-ranked wire format
the chosen host offers for that model. Anything that fails to match wraps
`ErrNotFound` with a message naming the argument that missed.

```go
offering, _ := agentkit.Lookup("claude-sonnet-5", "", "")
auth, _     := offering.Authenticator(agentkit.APIKeyRotator(key))
ep, _       := agentkit.NewEndpoint(auth)
conv, _     := agentkit.New(offering.WireFormat, ep, offering.WireModel, cfg)
```

`Lookup("claude-sonnet-5", "openrouter", "chat")` yields a different offering
entirely: the generic chat wire, the OpenRouter URL, send
`anthropic/claude-sonnet-5`. The host changes the wire format, the endpoint
specs, the wire model, the price schedule, and the reasoning vocabulary; the
application reads all of them from the one `Offering` and hands two of them
straight to `New`.

Every offering id's transport, host, wire name, codec, and the `api_key`
endpoint URL, is fixed per id, and a host's alternate protocol is simply
another id with its own offerings: `openai-chat` beside `openai-responses`,
`xai-chat` beside `xai-responses`, `openrouter-responses` beside
`openrouter-chat`. Where a host serves the same model on both of its
protocols, the table carries both offerings with identical wire model,
pricing, and reasoning, so nothing is lost by choosing the alternate. OpenAI's
own host uses the OpenAI-specific codecs (D5) because its credential placement
differs; xAI and OpenRouter use the generic ones.

The `oauth` endpoint spec is per offering, not per id, because it was proven
live to be so. A ChatGPT OAuth token is honored only by the Codex backend,
`https://chatgpt.com/backend-api/codex/responses`, never by `api.openai.com`
on either protocol, and the Codex backend serves only a subset of the models
the platform API serves. So `openai-responses` lists an `oauth` spec at the
Codex URL only on the models Codex serves, `openai-chat` never lists one, and
the xAI offerings list an `oauth` spec at the same URL as their `api_key`
spec, since `api.x.ai` accepts an xAI OAuth bearer on both protocols. Every
such fact is an external dependency and is re-proven by the live matrix (D23)
whenever the matrix is built.

`MaxOutputTokens` is the vendor's cap on one response's output, the value the
Anthropic wire sends as `max_tokens` when the caller sets no
`max_output_tokens` option (D8), because Anthropic rejects a request without
one. The caps were read from Anthropic's own rejection of an oversize request:
64000 for Claude Haiku 4.5, 128000 for every other cataloged Claude model. It
is zero on offerings whose cap has not been proven; zero means unknown, and no
wire currently needs it there.

A round-trip test proves the identity/pricing link by asserting the logged
`Identity.Endpoint` against the literal string (`"openrouter-chat"`), not
against the `OfferingID` constant; vocabulary tests pin the constants to the
same literals separately.

Which credential to use, environment-variable names, and token files are the
application's business and are deliberately not here: the catalog knows
offerings and their endpoint specs, and the wire format knows where a
credential goes (D5, D7); the application knows which credentials it holds.
Grouping for display is a walk over `Catalog()` by `Offering.Host`; there is
no separate vendor label.

The table itself is data, not contract: entries are added, repriced, and
retired without touching this document. What the contract fixes is the shape
above and a set of invariants every entry must satisfy — the table is complete
on cost, every default is inside its own vocabulary, wire names are non-empty,
every reasoning term matches its kind — so the table can grow freely while
staying trustworthy. A few pinned entries anchor resolution with real fixtures.

One invariant reaches across to the wire seam: **everything the catalog says an
offering accepts, the offering's wire must be able to send.** The vocabulary a
`ReasoningSpec` describes — `off` when `CanDisable`, `on` for an enableable
toggle, each level in `Levels`, the budget range, and the `Default` itself — is
what an application will put in front of a user, and a value the user can pick
that then fails at `Send` as "wire cannot express" is a catalog lie. `Term` is
also the key every wire accepts for that value (D8), so the help row and the
typed option agree by construction. The
invariant test walks every offering, writes each request in its vocabulary
as the option `Term=value` a user would type, and asserts the offering's
`WireFormat` validates it (D8). It is what forces
the generic `chat` and `responses` wires to render the toggle and budget forms
that OpenRouter's models need (D8), rather than the table being trimmed to
what the wires happened to express.

The reasoning term follows the kind, and its spelling is fixed so an
application's help text and a user's typed key agree across every model:

| `Kind` | `Term` | Models |
|---|---|---|
| effort | `effort` | Anthropic, OpenAI, xAI, GLM 5.2, Qwen |
| effort | `thinking_level` | Gemini 3.x |
| budget | `thinking_budget` | Gemini 2.5, Claude Haiku 4.5 |
| toggle | `thinking` | DeepSeek, Kimi, GLM 4.x/5.1, Nemotron, Grok toggles |
| none | `""` | — |

The seed table is authored by hand, not by the build loop: it lives at
`specs/_data/catalog_table.go` (a directory Go tooling ignores) and is
installed verbatim as the root package's `catalog_table.go`. Repricing or
adding a model is an edit to that file and nothing else.

## REQUIREMENTS

- R-JB4K-6IAI: `agentkit` MUST export `type OfferingID string` with exactly the constants `OfferingAnthropicMessages = "anthropic-messages"`, `OfferingOpenAIResponses = "openai-responses"`, `OfferingOpenAIChat = "openai-chat"`, `OfferingGeminiGenerateContent = "gemini-generate-content"`, `OfferingXAIResponses = "xai-responses"`, `OfferingXAIChat = "xai-chat"`, `OfferingOpenRouterChat = "openrouter-chat"`, and `OfferingOpenRouterResponses = "openrouter-responses"`.
- R-JCCG-KA17: `agentkit` MUST export `type Host string` with exactly the constants `HostAnthropic = "anthropic"`, `HostOpenAI = "openai"`, `HostGemini = "gemini"`, `HostXAI = "xai"`, and `HostOpenRouter = "openrouter"`.
- R-JDKC-Y1RW: `agentkit` MUST export `type WireName string` with exactly the constants `WireMessages = "messages"`, `WireGenerateContent = "generate-content"`, `WireChat = "chat"`, and `WireResponses = "responses"`.
- R-JES9-BTIL: `agentkit` MUST NOT export any of `Vendor`, `ProviderID`, `ResolveModel`, `LookupModel`, or `CatalogFor`.
- R-O6AH-EYKH: `agentkit` MUST export `type ReasoningKind int` with the constants `ReasoningKindNone`, `ReasoningKindEffort`, `ReasoningKindBudget`, `ReasoningKindToggle` declared in that `iota` order starting at 0.
- R-O11N-T3ME: `agentkit` MUST export `type ReasoningSpec struct { Kind ReasoningKind; Term string; Levels []Effort; MinBudget int; MaxBudget int; CanEnable bool; CanDisable bool; Default ReasoningConfig }` with exactly those fields.
- R-O8QA-6I1V: `agentkit` MUST export `func (s ReasoningSpec) Accepts(r ReasoningConfig) bool`.
- R-KE3W-V60A: `agentkit` MUST export `type Rotation struct { RefreshURL string; ClientID string }` with exactly those fields, and MUST NOT export `OAuthClient`.
- R-KFBT-8XQZ: `agentkit` MUST export `type EndpointSpec struct { AuthMode AuthMode; BaseURL string; Rotation Rotation }` with exactly those fields.
- R-KGJP-MPHO: `agentkit` MUST export `type Offering struct { ID OfferingID; Host Host; WireName WireName; WireFormat WireFormat; Endpoints []EndpointSpec; WireModel string; Context int64; MaxOutputTokens int64; Pricing Pricing; Reasoning ReasoningSpec }` with exactly those fields.
- R-KHRM-0H8D: Every offering in the table MUST carry the `Host`, `WireName`, and `WireFormat` fixed for its `ID`, and its `Endpoints` MUST begin with an `EndpointSpec` whose `AuthMode` is `AuthModeAPIKey` and whose `BaseURL` is fixed for its `ID`: `anthropic-messages` → `anthropic`, `messages`, `AnthropicMessagesWire()`, `https://api.anthropic.com/v1/messages`; `openai-responses` → `openai`, `responses`, `OpenAIResponsesWire()`, `https://api.openai.com/v1/responses`; `openai-chat` → `openai`, `chat`, `OpenAIChatWire()`, `https://api.openai.com/v1/chat/completions`; `gemini-generate-content` → `gemini`, `generate-content`, `GeminiGenerateContentWire()`, `https://generativelanguage.googleapis.com/v1beta/models/<WireModel>:streamGenerateContent?alt=sse` with `<WireModel>` path-escaped; `xai-responses` → `xai`, `responses`, `ResponsesWire()`, `https://api.x.ai/v1/responses`; `xai-chat` → `xai`, `chat`, `ChatWire()`, `https://api.x.ai/v1/chat/completions`; `openrouter-chat` → `openrouter`, `chat`, `ChatWire()`, `https://openrouter.ai/api/v1/chat/completions`; `openrouter-responses` → `openrouter`, `responses`, `ResponsesWire()`, `https://openrouter.ai/api/v1/responses`.
- R-KIZI-E8Z2: Every `EndpointSpec` in the table whose `AuthMode` is `AuthModeOAuth` MUST be, by its offering's `ID`: for `openai-responses`, `BaseURL` `https://chatgpt.com/backend-api/codex/responses` and `Rotation` `{RefreshURL: "https://auth.openai.com/oauth/token", ClientID: "app_EMoamEEZ73f0CkXaXp7hrann"}`; for `xai-responses`, `BaseURL` `https://api.x.ai/v1/responses` and `Rotation` `{RefreshURL: "https://auth.x.ai/oauth2/token", ClientID: "b1a00492-073a-47ea-816f-4c329264a828"}`; for `xai-chat`, `BaseURL` `https://api.x.ai/v1/chat/completions` and that same xAI `Rotation`; and no offering with any other `ID` MUST carry an `oauth` spec.
- R-KK7E-S0PR: For every offering in the table, `Endpoints` MUST be non-empty, MUST hold at most one spec per `AuthMode`, every spec's `BaseURL` MUST be an absolute HTTP(S) URL, and a spec's `Rotation` MUST be non-zero in both fields if and only if its `AuthMode` is `AuthModeOAuth`.
- R-KLFB-5SGG: The `openai-responses` offerings of exactly the models `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, `gpt-5.5`, and `gpt-5.4-mini` MUST carry an `oauth` `EndpointSpec`, and the `openai-responses` offering of every other model MUST NOT; every `xai-responses` and `xai-chat` offering MUST carry one.
- R-KMN7-JK75: `MaxOutputTokens` MUST be `64000` on the `anthropic-messages` offering of `claude-haiku-4-5`, `128000` on every other `anthropic-messages` offering, and `0` on every offering whose `Host` is not `HostAnthropic`.
- R-KNV3-XBXU: Mutating a returned `Offering`'s `Endpoints` slice or any element of it MUST have no effect on any later catalog call.
- R-JIFY-H4QO: For every entry, an offering with `ID` `OfferingOpenAIResponses` MUST be paired with one with `OfferingOpenAIChat`, one with `OfferingXAIResponses` with one with `OfferingXAIChat`, and one with `OfferingOpenRouterChat` with one with `OfferingOpenRouterResponses`, each pair sharing `WireModel`, `Context`, `Pricing`, and `Reasoning`.
- R-JJNU-UWHD: For every entry holding an offering whose `Host` is not `HostOpenRouter`, the entry's first offering MUST NOT have `Host` `HostOpenRouter`.
- R-JKVR-8O82: `agentkit` MUST export `type CatalogEntry struct { Model string; Offerings []Offering }` with exactly those fields.
- R-JM3N-MFYR: `agentkit` MUST export `var ErrNotFound error`, `func Catalog() []CatalogEntry`, and `func Lookup(model string, host Host, wire WireName) (Offering, error)`.
- R-JNBK-07PG: `Catalog()` MUST return every entry sorted ascending by `Model`, with no two entries sharing a `Model`, every entry holding at least one offering, and no two offerings of one entry sharing an `ID`.
- R-JOJG-DZG5: `Lookup(model, host, wire)` MUST consider only the entry whose `Model` equals `model` exactly; among its offerings only those whose `Host` equals `host` when `host` is non-empty, and only those whose `Host` equals the entry's first offering's `Host` when `host` is empty; and among those only the one whose `WireName` equals `wire` when `wire` is non-empty.
- R-JQZ9-5IXJ: When `wire` is empty, `Lookup` MUST choose, among the offerings that survive the model and host selection, the one whose `WireName` ranks highest under the fixed order `responses` above `chat`, independent of the offerings' order in the table.
- R-JS75-JAO8: When no offering survives `Lookup`'s selection, `Lookup` MUST return a non-nil error for which `errors.Is(err, ErrNotFound)` holds and whose message names the argument that failed to match.
- R-OIHH-8NZF: Every `CatalogEntry` and `Offering` returned by the catalog functions MUST be a copy, such that mutating a returned value's `Offerings`, `Pricing.Tiers`, or `Reasoning.Levels` has no effect on any later call.
- R-JTF1-X2EX: Every offering in the table MUST have a non-empty `WireModel` (containing a `/` when `Host` is `HostOpenRouter`), a `Context` greater than zero, and a `Pricing` with at least one tier whose first tier's `MinInputTokens` is zero, whose tiers have strictly increasing `MinInputTokens`, and whose every tier has `InputUncached` and `Output` greater than zero.
- R-OKXA-07GT: `ReasoningSpec.Accepts` MUST return true for `ReasoningDefault` always; for `ReasoningOff` iff `CanDisable`; for `ReasoningOn` iff `Kind` is `ReasoningKindToggle` and `CanEnable`; for `ReasoningEffort` iff `Kind` is `ReasoningKindEffort` and the level is in `Levels`; for `ReasoningBudget` iff `Kind` is `ReasoningKindBudget` and `MinBudget <= Budget <= MaxBudget`; and false otherwise.
- R-OM56-DZ7I: Every offering's `Reasoning.Default` MUST be accepted by its own `Reasoning`; an effort-kind spec MUST have non-empty `Levels` with no duplicates; a budget-kind spec MUST have `MinBudget` less than `MaxBudget`; a none-kind spec MUST have `CanEnable` and `CanDisable` false and empty `Levels`.
- R-O29K-6VD3: Every offering's `Reasoning.Term` MUST be `"effort"` or `"thinking_level"` when `Kind` is `ReasoningKindEffort`, `"thinking_budget"` when `Kind` is `ReasoningKindBudget`, `"thinking"` when `Kind` is `ReasoningKindToggle`, and `""` when `Kind` is `ReasoningKindNone`.
- R-O3HG-KN3S: Every offering of `gemini-3.5-flash`, `gemini-3.7-flash`, `gemini-3.1-flash-lite`, and `gemini-3.1-pro-preview` MUST have `Reasoning.Term` `"thinking_level"`; every offering of `claude-opus-5` MUST have `"effort"`; every offering of `gemini-2.5-flash` and `claude-haiku-4-5` MUST have `"thinking_budget"`; and every offering of `deepseek-v4-pro` and `grok-4.20` MUST have `"thinking"`.
- R-W8QS-PJR3: For every offering, each request `c` in its reasoning vocabulary — `ReasoningConfig{Mode: ReasoningOff}` when `CanDisable`; `ReasoningConfig{Mode: ReasoningOn}` when `Kind` is `ReasoningKindToggle` and `CanEnable`; `ReasoningConfig{Mode: ReasoningEffort, Effort: level}` for each level in `Levels`; `ReasoningConfig{Mode: ReasoningBudget, Budget: b}` for `b` equal to `MinBudget` and to `MaxBudget`; and `Reasoning.Default` — MUST, as `Settings{Options: Options{Term: c.String()}}`, pass the `Send`-time settings validation of the offering's `WireFormat` (D8), so that no vocabulary value can fail `Send` with `ErrInvalidConfig`.
- R-JUMY-AU5M: `Lookup("claude-sonnet-5", "", "")` MUST return an offering with `ID` `OfferingAnthropicMessages` and `WireModel` `"claude-sonnet-5"`; `Lookup("claude-sonnet-5", "openrouter", "chat")` MUST return an offering with `ID` `OfferingOpenRouterChat` and `WireModel` `"anthropic/claude-sonnet-5"`; and `Lookup("claude-sonnet-5", "gemini", "")` MUST return an error wrapping `ErrNotFound`.
- R-JVUU-OLWB: `Lookup("gpt-5.6-sol", "", "")` MUST return an offering with `ID` `OfferingOpenAIResponses`, `WireModel` `"gpt-5.6-sol"`, and a `Reasoning.Default` of `ReasoningConfig{Mode: ReasoningEffort, Effort: EffortMedium}`; `Lookup("gpt-5.6-sol", "openrouter", "")` MUST return an offering with `ID` `OfferingOpenRouterResponses`; and `Lookup("gpt-5.6-sol", "openai", "messages")` MUST return an error wrapping `ErrNotFound`.
- R-JX2R-2DN0: `Lookup("deepseek-v4-flash", "", "")` MUST return an offering with `Host` `HostOpenRouter`, and `Lookup("no-such-model", "", "")` MUST return an error wrapping `ErrNotFound`.
- R-JYAN-G5DP: The catalog MUST NOT gate construction or `Send`: a conversation for a model or host/model pair that `Lookup` reports `ErrNotFound` for MUST construct and send exactly as a cataloged one does, differing only in pricing to zero (D3).
- R-JZIJ-TX4E: For each of the five `Host` constants, `Catalog()` MUST contain at least one offering whose `Host` is that constant.
