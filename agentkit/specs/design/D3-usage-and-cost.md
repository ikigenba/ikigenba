# D3-usage-and-cost

Every turn a wire reports token counts, and agentkit reports back a `Usage` and
a `Cost`. Both are plain value types with no methods that reach the network;
they are assembled inside the wire adapter as it decodes the stream (D5) and
handed to the consumer through the `Stream` and the event log (D13, D15). This
doc owns two seams: how streamed usage fragments **merge** into one `Usage`, and
how a `Cost` is always **resolved** to a single number.

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
resolved plain integers. The buckets follow the billing lines vendors actually
draw: fresh input, cache reads, cache writes (split by the 5-minute and 1-hour
cache lifetimes Anthropic bills separately), output, and reasoning.

```go
// Usage is the resolved token accounting for one turn. Every field is an
// absolute count for the whole turn, already merged across stream fragments.
// The buckets are non-overlapping by construction: the wire adapter subtracts
// any vendor subset (e.g. a cached count nested inside input, a reasoning count
// nested inside output) during decode. Consumers may sum the six fields to a
// grand total without fear of double counting. A wire that does not report a
// bucket leaves it zero.
type Usage struct {
    InputTokens        int64 // fresh (uncached) prompt tokens
    CachedTokens       int64 // prompt tokens served from cache
    CacheWrite5mTokens int64 // prompt tokens written to a 5-minute cache
    CacheWrite1hTokens int64 // prompt tokens written to a 1-hour cache
    OutputTokens       int64 // generated tokens excluding reasoning
    ReasoningTokens    int64 // reasoning/thinking tokens, when the wire separates them
}

// usageFragment is the per-chunk decode target: every field is optional so the
// merge can tell "reported as zero" from "not reported in this chunk". Wire
// adapters populate only the fields a given chunk carried.
type usageFragment struct {
    InputTokens        *int64
    CachedTokens       *int64
    CacheWrite5mTokens *int64
    CacheWrite1hTokens *int64
    OutputTokens       *int64
    ReasoningTokens    *int64
}
```

The subset topology — that a cached count arrives nested inside the input count
on some wires, or a reasoning count nested inside the output count — is a decode
detail owned by each wire adapter (D5), stated here only as a shape: `Usage`'s
six buckets are disjoint by the time they leave the adapter, and the adapter is
responsible for the subtraction. The Anthropic wire is today the only one that
reports cache writes, and it reports them split by lifetime; the other wires
leave both cache-write buckets at zero.

**Cost is one number.** `Cost` is nano-USD as a bare integer. The consumer is
handed the amount and nothing beside it: no flag, no label, no record of how
the number was produced. A turn's cost is, in order of preference, the figure
the wire itself reported, the figure computed from the catalog's static rate
for this conversation's provider and model (D21), or zero. Zero is the answer
for a turn whose provider/model pair is not in the catalog — the consumer
chose an off-catalog combination and gets an unpriced turn — and never an
error.

```go
// Cost is the price of one turn in nano-USD (1e-9 USD). It is a bare amount;
// aggregation is ordinary addition.
type Cost int64

// RateTier is one row of a model's price schedule. MinInputTokens is the
// input-token floor at which the row starts applying; the first row's floor is
// always zero. Rates are nano-USD per token so the arithmetic stays in integers
// end to end. A rate the vendor does not charge is zero.
type RateTier struct {
    MinInputTokens int64 // tier floor on total input tokens (inclusive)
    InputUncached  int64 // nano-USD per fresh input token
    CacheReadInput int64 // nano-USD per cached input token
    CacheWrite5m   int64 // nano-USD per 5-minute cache-write token
    CacheWrite1h   int64 // nano-USD per 1-hour cache-write token
    Output         int64 // nano-USD per output token; reasoning bills here too
}

// Pricing is a model's full price schedule on one provider: one or more tiers
// ordered by ascending floor. The tier applied to a turn is the last one whose
// floor the turn's total input tokens reach.
type Pricing struct {
    Tiers []RateTier
}

// Cost prices a Usage against this schedule. An empty schedule prices to zero.
func (p Pricing) Cost(u Usage) Cost
```

Tier selection uses the turn's *total* input — fresh, cached, and both
cache-write buckets summed — because that is the number vendors compare
against their long-context threshold. Reasoning tokens bill at the tier's
output rate; no vendor prices them separately.

The catalog match is by the conversation's `Identity` (D1): the offering whose
`ID` equals `Identity.Endpoint` and whose `WireModel` equals
`Identity.Model` supplies the `Pricing`. The authenticator `Offering.Authenticator` returns
names its endpoint with the offering's `ID` value (D7, D21), so a
conversation built from a cataloged offering prices itself with no help from
the app. An unlisted model finds no offering and prices to zero.

## REQUIREMENTS

- R-26R3-31RB: Usage merge MUST be field-wise over absolute values — for each field the last fragment carrying a value for that field wins and a fragment omitting a field MUST leave that field unchanged; whole-object replacement and delta summation MUST NOT be used.
- R-296V-UL8P: A merge sequence in which one fragment reports only the input-side fields and a later fragment reports only the output-side fields MUST yield a `Usage` retaining both sides (the later fragment MUST NOT zero the input side).
- R-2AES-8CZE: A merge sequence of repeated cumulative snapshots MUST yield a `Usage` equal to the last snapshot, not the sum of the snapshots.
- R-ND0W-8GRT: `agentkit` MUST export `type Usage struct { InputTokens int64; CachedTokens int64; CacheWrite5mTokens int64; CacheWrite1hTokens int64; OutputTokens int64; ReasoningTokens int64 }` with exactly those six fields.
- R-NFGP-0097: The six `Usage` buckets MUST be disjoint as they leave the wire adapter — `InputTokens` excludes `CachedTokens` and both cache-write buckets, and `OutputTokens` excludes `ReasoningTokens` — so their sum is a correct grand total.
- R-NGOL-DRZW: The Anthropic Messages wire MUST decode the 5-minute and 1-hour cache-creation token counts into `CacheWrite5mTokens` and `CacheWrite1hTokens` respectively, and every other built-in wire MUST leave both cache-write buckets at zero.
- R-NHWH-RJQL: `agentkit` MUST export `type Cost int64` as a bare nano-USD amount with no companion flag, label, or provenance field.
- R-NJ4E-5BHA: `agentkit` MUST export `type RateTier struct { MinInputTokens int64; InputUncached int64; CacheReadInput int64; CacheWrite5m int64; CacheWrite1h int64; Output int64 }` with exactly those six fields.
- R-NKCA-J37Z: `agentkit` MUST export `type Pricing struct { Tiers []RateTier }` with exactly that field.
- R-NLK6-WUYO: `agentkit` MUST export `func (p Pricing) Cost(u Usage) Cost`, and an empty `Tiers` MUST price every `Usage` to zero.
- R-NMS3-AMPD: `Pricing.Cost` MUST select the last tier whose `MinInputTokens` is less than or equal to the sum of `InputTokens`, `CachedTokens`, `CacheWrite5mTokens`, and `CacheWrite1hTokens`, falling back to the first tier when no later tier's floor is reached.
- R-NNZZ-OEG2: `Pricing.Cost` MUST equal `InputTokens×InputUncached + CachedTokens×CacheReadInput + CacheWrite5mTokens×CacheWrite5m + CacheWrite1hTokens×CacheWrite1h + (OutputTokens+ReasoningTokens)×Output` using the selected tier's rates.
- R-NP7W-266R: A turn's `Cost` MUST be the wire-reported figure when the wire carries one, otherwise the catalog offering's `Pricing.Cost` of the merged `Usage` when an offering matches the conversation, otherwise zero; and nothing but the amount MUST be surfaced to the consumer.
- R-KLGQ-PSGW: The catalog offering used to price a conversation MUST be the one whose `ID` equals `Identity.Endpoint` and whose `WireModel` equals `Identity.Model`; any other pair MUST match no offering.
- R-NRNO-TPO5: A turn whose conversation matches no catalog offering and whose wire reports no cost MUST complete normally with a `Cost` of zero, never an error.
- R-NSVL-7HEU: Aggregating `Cost` values over turns MUST be plain integer addition of the amounts.
- R-2IY2-WR69: A wire that reports its own billed cost MUST decode it to nano-USD with that wire's fixed unit; an absent wire cost MUST fall through to the next rung rather than resolve to zero.
