# Open design gaps — structural-requirement compliance

Surfaced during the "requirements are the design" compliance pass. These are
exported seams that are **named in prose but whose shape was never actually
designed**, so no structural requirement could be minted for them yet. Addressing
them means deciding the shape, then minting structural requirements (and, where a
new subsystem is warranted, opening a design doc) in a follow-up pass.

## G1 — `ReplayEncoding` has no declared shape
- Referenced by `WireFormat.DefaultReplayEncoding() ReplayEncoding` (D5) and
  `WithReplayEncoding(e ReplayEncoding) EndpointOption` (D6).
- Both cite a design doc "D-K" that does not exist (docs are D0–D17).
- No type shape is given anywhere. The two structural requirements that mention
  it (R-ZFHV-1XK1 in D5, R-ZLLC-YS9I in D6) name the type but cannot pin it.
- Decide: owner (proposed D6) + shape (an enum of replay encodings? opaque?),
  then mint a structural requirement declaring it.

## G2 — generic wire constructor + per-package `Option` shape
- D1 (R-1UK3-9CCD) requires a public, first-class generic
  `(WireFormat, Endpoint, Model)` constructor, but its exact exported name and
  signature are never written in any doc.
- The per-package `Option` type used by vendor `New(cred, opts ...Option)` (D7:
  R-ZROU-VMYZ, R-ZSWR-9EPO) has no declared shape.
- Decide: the constructor signature and the `Option` type shape, then mint
  structural requirements.

## G3 — vendor packages beyond anthropic/openai
- D0 promises 5 day-one endpoints (OpenAI, Anthropic, Gemini, xAI, OpenRouter).
- Only `anthropic` and `openai` credential surfaces are designed (D7); `gemini`,
  `xai`, `openrouter`, and the per-vendor `TokenSource` shapes are undeclared.
- Decide (recommended): mint one structural PATTERN requirement — every vendor
  package MUST export a sealed `Credential` (own unexported marker) + credential
  constructors + a `New` returning `*agentkit.Conversation` — and defer each
  concrete vendor package's exact constructors to its own design.

## Note
`openai.New`'s signature (R-ZSWR-9EPO) was inferred by symmetry with
`anthropic.New`; D7 shows only openai's `APIKey`/`Subscription`. Confirm when
addressing G2/G3.
