# D3-usage-and-cost

Every turn a wire reports token counts, and agentkit reports back a `Usage` and
a `Cost`. Both are plain value types with no methods that reach the network;
they are assembled inside the wire adapter as it decodes the stream (D5) and
handed to the consumer through the `Stream` and the event log (D13, D15). This
doc owns two seams: how streamed usage fragments **merge** into one `Usage`, and
how a `Cost` is always **resolved** to a single number that never lies about
being real.

**Usage is a field-wise absolute merge, not a whole-object replace.** A wire
does not emit one final usage object; it emits fragments across the stream, and
different wires distribute the fields differently. One wire emits the input side
early and a *cumulative* output side late, in disjoint fields. Another emits a
complete cumulative snapshot on every chunk. A third emits nothing until a single
final chunk. Each numeric field carried in a fragment is an **absolute** count
(the running or final total for that field), never a delta to be summed. The
merge rule is therefore per-field: for each field, the last fragment that
carried a value for it wins; a field absent from a fragment is left untouched.
Whole-object last-wins is wrong — it would let a late fragment that names only
the output side zero out an input side that an earlier fragment established. A
delta-summing merge is equally wrong — it would double-count a field a wire
repeats cumulatively. Last-non-absent-wins over absolute fields is the one rule
that is correct for all three emit shapes with no per-wire policy knob.

Because merging must distinguish "field present and zero" from "field absent",
the merge operates over an optional-per-field fragment, and `Usage` itself holds
resolved plain integers:

```go
// Usage is the resolved token accounting for one turn. Every field is an
// absolute count for the whole turn, already merged across stream fragments.
// The buckets are non-overlapping by construction: the wire adapter subtracts
// any vendor subset (e.g. a cached count nested inside input, a reasoning count
// nested inside output) during decode, so InputTokens excludes CachedTokens and
// OutputTokens excludes ReasoningTokens. Consumers may sum the four fields to a
// grand total without fear of double counting.
type Usage struct {
    InputTokens     int64 // fresh (uncached) prompt tokens
    CachedTokens    int64 // prompt tokens served from cache
    OutputTokens    int64 // generated tokens excluding reasoning
    ReasoningTokens int64 // reasoning/thinking tokens, when the wire separates them
}

// usageFragment is the per-chunk decode target: every field is optional so the
// merge can tell "reported as zero" from "not reported in this chunk". Wire
// adapters populate only the fields a given chunk carried.
type usageFragment struct {
    InputTokens     *int64
    CachedTokens    *int64
    OutputTokens    *int64
    ReasoningTokens *int64
}
```

The subset topology — that a cached count arrives nested inside the input count
on some wires, or a reasoning count nested inside the output count — is a decode
detail owned by each wire adapter (D5), stated here only as a shape: `Usage`'s
four buckets are disjoint by the time they leave the adapter, and the adapter is
responsible for the subtraction. This doc fixes the *merge* contract; the
subtraction lives with the wire that knows its own nesting.

**Cost is one number, always produced, and honest about whether it is real.**
`Cost` is an `int64` of nano-USD paired with a `Known` flag:

```go
// Cost is the price of one turn in nano-USD (1e-9 USD). Known reports whether
// Amount reflects a real figure; a false Known means the amount is unresolved
// (typically zero) and must not be treated as "this turn was free". Aggregation
// propagates Known: summing any Cost whose Known is false yields a sum whose
// Known is false, so a cumulative total can never silently under-count.
type Cost struct {
    Amount int64 // nano-USD
    Known  bool
}

// Pricing is a consumer-supplied per-model rate, consulted when a wire reports
// no cost of its own. Rates are nano-USD per token so the arithmetic stays in
// integers end to end.
type Pricing struct {
    InputPerToken     int64 // nano-USD per fresh input token
    CachedPerToken    int64 // nano-USD per cached input token
    OutputPerToken    int64 // nano-USD per output token
    ReasoningPerToken int64 // nano-USD per reasoning token
}
```

Cost resolves three-deep, first hit wins: (1) **wire-reported** — a wire that
carries its own billed figure decodes it into nano-USD with its own hardcoded
unit and returns `Known=true`; (2) **consumer `Pricing`** — if the consumer
supplied a rate for this model, agentkit prices the merged `Usage` against it;
(3) the **built-in rate table** — a hardcoded chat-model rate map shipped inside
agentkit. A model found at any rung yields `Known=true`. A model absent from all
three still completes the turn and yields a `Cost{Amount: 0, Known: false}` —
never an error, never a fabricated positive number, and never a zero that claims
to be real. "Never zero" means precisely "never a zero whose `Known` is true".

There is **no cost provenance and no labeling**: the consumer gets one `Cost`,
not a tagged union of "billed vs. estimated". The three rungs exist to always
have an answer, not to report which rung answered. Aggregates over many turns
propagate `Known` — one unknown turn makes the running total's `Known` false —
so a dashboard summing costs is told when it is under-counting rather than shown
a confident wrong total.

The **built-in rate table is chat-only** and is the single deliberate exception
to the monorepo rule that concrete values live in sibling projects (D0). Model
rates change often and belong logically in `catalog`, but a cost that always
resolves is core to agentkit's contract, so the chat rate table ships here. The
`embed` sibling ships no rate table of its own; embeddings are out of scope
(D0).

## REQUIREMENTS

- R-26R3-31RB: Usage merge MUST be field-wise over absolute values — for each field the last fragment carrying a value for that field wins and a fragment omitting a field MUST leave that field unchanged; whole-object replacement and delta summation MUST NOT be used.
- R-296V-UL8P: A merge sequence in which one fragment reports only the input-side fields and a later fragment reports only the output-side fields MUST yield a `Usage` retaining both sides (the later fragment MUST NOT zero the input side).
- R-2AES-8CZE: A merge sequence of repeated cumulative snapshots MUST yield a `Usage` equal to the last snapshot, not the sum of the snapshots.
- R-2BMO-M4Q3: The four `Usage` buckets MUST be disjoint as they leave the wire adapter — `InputTokens` excludes `CachedTokens` and `OutputTokens` excludes `ReasoningTokens` — so their sum is a correct grand total.
- R-2E2H-DO7H: Cost resolution MUST try wire-reported, then consumer `Pricing`, then the built-in chat rate table, in that order, and MUST stop at the first rung that yields a value.
- R-2FAD-RFY6: A turn whose model resolves at no rung MUST complete normally and yield `Cost{Known: false}` — never an error and never a `Known=true` zero.
- R-2GIA-57OV: Aggregating any collection of `Cost` values MUST propagate `Known` such that the aggregate's `Known` is false whenever any contributing `Cost` has `Known=false`.
- R-2HQ6-IZFK: agentkit MUST ship a built-in chat-only rate table and MUST resolve cost from it for a model present there when no wire cost and no consumer `Pricing` apply.
- R-2IY2-WR69: A wire that reports its own billed cost MUST decode it to nano-USD with that wire's fixed unit; an absent wire cost MUST fall through to the next rung rather than resolve to zero.
- R-Z5QN-ZRMH: `agentkit` MUST export `type Usage struct { InputTokens int64; CachedTokens int64; OutputTokens int64; ReasoningTokens int64 }` with exactly those four fields.
- R-Z6YK-DJD6: `agentkit` MUST export `type Cost struct { Amount int64; Known bool }` with exactly those two fields.
- R-Z86G-RB3V: `agentkit` MUST export `type Pricing struct { InputPerToken int64; CachedPerToken int64; OutputPerToken int64; ReasoningPerToken int64 }` with exactly those four fields.
- R-0THQ-QIYI: A resolved `Cost` MUST set `Known` true; an unresolved `Cost` MUST set `Known` false with a zero `Amount`, and a `Known`-false `Cost` MUST NOT be read as a real zero-cost turn.
