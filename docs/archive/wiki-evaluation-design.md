# wiki evaluation — design

> **What this fixes.** This is a **corrected** lock. The prior version of this
> document (2026-06-14) carried three assumptions from the research doc straight
> into the build that turned out to be wrong, and they were never surfaced
> sharply enough to be reviewed before P13–P16 were built against them:
>
> 1. **Ten "sites."** It treated the three zero-LLM retrieval lanes
>    (`candidates`, `search`, `sweep`) and the `canonical_name` sub-field as
>    first-class evaluation sites alongside the real call sites. They are not:
>    if no `(model, effort)` can change the result, there is no triple to specify
>    and nothing for *this* harness to test. The eval is **100% about testing the
>    LLM call sites.** Retrieval quality is a real concern, but it is a different
>    measurement with different inputs (recall over retrieval knobs) and does not
>    belong here.
> 2. **The "bundle" coupling.** It fused the dataset and the prompt into a single
>    on-disk "bundle = generation" artifact and made the prompt a committed file
>    rather than a runtime input. The four inputs to an evaluation —
>    **prompt, dataset, model, effort** — are independent and must all be
>    specifiable at run time. The versioning/caching machinery belongs *under*
>    that interface, not in front of it.
> 3. **A single accuracy number, and a "four scorer kinds" taxonomy.** The real
>    per-site unit is a single comparison function whose mechanics are hidden
>    inside it; the only thing that varies between sites is the **schema of what
>    it is handed**. And a single percentage hides the asymmetry that matters
>    most (a contradiction is not a worse recall miss — it is a different,
>    dangerous failure), so the dangerous direction stays a **separate** number.
>
> This document supersedes the prior lock in full. The build under
> `wiki/cmd/wiki-eval` + `wiki/internal/eval` reflects the *old* design and is to
> be reworked to this one (one site is wired today: `match`).
>
> **What this is *not*** (unchanged, and it still governs every decision below):
> the harness is **not a CI test harness, not a build gate, not a deploy gate.**
> Every score is an input to a human decision, never an automated pass/fail.
> Nothing here may fire `go test ./...` red, gate a `/finish` phase, or block a
> deploy. The standing integration tier (`wiki-redesign-plan.md`, *Integration
> testing*) is the only thing that runs real models inside the build, and it too
> is advisory.
>
> **Status.** Locked (corrected, 2026-06-14).

## The sites — exactly the eight LLM call sites

The discriminator is mechanical: **a site belongs in this eval iff changing the
model or the effort could change its result.** That is exactly the set of call
sites that carry a `(prompt, model, effort)` triple — which is exactly the
`config.LLM.*` call-site list. There are eight:

| site | call seam | output schema (the `actual`) |
|---|---|---|
| `extract` | `Structured` | `ExtractSchema` |
| `match` | `Structured` | `MatchSchema` |
| `compile` | `Structured` | `CompileSchema` |
| `merge` | `Structured` | `MergeSchema` |
| `dup_judge` | `Structured` | `JudgeSchema` |
| `fold` | `Structured` | `FoldSchema` |
| `stale` | `Structured` | `StaleSchema` |
| `ask` | `Agent` (tool loop) | `AnswerSchema` |

**Explicitly excluded**, with the reason:

- `candidates`, `search`, `sweep` — pure FTS/BM25/vector retrieval. No model
  call; `(model, effort)` cannot move the number. Retrieval recall is a separate
  evaluation (its inputs are `k`, FTS thresholds, RRF `k`, the embed model/dims,
  and whether the vector lane is on) and is **out of scope for this harness.**
- `canonical_name` — not a call. It is a field of the `dup_judge` output; the
  only model that affects it is `dup_judge`'s, already covered.

Seven of the eight share the identical single-shot seam
`Structured(cfg, schema, msgs) → json`. `ask` is the lone exception: it is an
agent loop (five read tools, a turn/token/wall-clock budget) whose **final**
message is parsed as `AnswerSchema`. It needs its own thin runner; its output is
still scored by the same contract below.

## The four independent inputs

An evaluation run is parameterised by four independent values, **all specifiable
at run time** (e.g. as `wiki-eval` flags):

| input | what it is | why independent |
|---|---|---|
| **prompt** | the system/framing prompt for the site | you iterate the prompt against a fixed dataset |
| **dataset** | the set of `(input, gold)` cases | you harden the data against a fixed prompt |
| **model** | the provider model id | a sweep axis |
| **effort** | the reasoning-effort level | a sweep axis |

It must be valid to vary **any one alone** — most importantly the prompt, against
a fixed dataset, to see whether the change moves the score. "Production" is just
one pinned `(prompt, model, effort)`; the harness runs the *same* call-site
function with a different one.

The prompt is a value, not a buried file. Caching keys on the content hashes of
`(dataset, prompt, model, effort)` so an unchanged combination re-scores for
free — but that cache is an implementation detail beneath the four-input
interface, **not** a "bundle" artifact the user must author to run.

`generation` survives as a *labelling* dimension on a dataset (cases carry a
1-based generation; a later, harder generation stands beside the first, never
replacing it — saturated generations are kept, never deleted). It is not a
coupling of prompt and data.

## The one eval-function shape

Per site there is exactly one function. Its mechanics (exact compare vs.
LLM-graded claim verification) are **hidden inside it**; from the harness's view
every site is the same signature:

```go
type Verdict struct {
    Percentage float64            // [0,1] headline: recall / accuracy
    Dangerous  map[string]float64 // named dangerous-direction rates, NEVER folded into Percentage
}

func eval_<site>(ctx context.Context, judge Judge, actual, gold json.RawMessage) Verdict
```

- `actual` — the call site's real output, in its existing output schema.
- `gold` — the authored reference. **`gold` is a correctness *spec*, not a sample
  output:** for the claim sites it is a flat list of facts that must be present,
  not a second copy of the output shape. Its schema is the only thing that
  differs between sites, and it is defined per site below.
- `judge` — the held-out LLM judge (below). Always passed; discrete sites ignore
  it.
- The return separates the headline from the dangerous axis. A single percentage
  is forbidden, because "95% with one false-merge" must read as worse than "90%
  clean," and a blended number inverts that.

### Two internal mechanics

**Discrete compare (no judge).** When the output is a discrete decision (an id,
an enum, a set of ids) there is one right answer; the function parses both sides
and compares directly. Deterministic, free, key-free.

**Grounded claim verification (judge required).** When the output asserts facts
in text, there is no string equality — a correct answer is phrased differently
every time, and similarity (lexical or embedding) cannot tell agreement from
contradiction (negation barely moves a vector; at the atomic-claim level that is
*maximal*, not minimal). So we do **not** compare two texts. We keep the gold as
an authored, 100%-accurate claim list and probe the output with each claim:

- **Recall (ternary, per gold claim):** the judge answers **affirms /
  contradicts / silent** for "does the output assert this fact?"
  - affirms → recall hit
  - silent → recall miss (counts against `Percentage`; recoverable)
  - contradicts → the dangerous case (`Dangerous{contradiction}`)
  Ternary, not binary, so a contradiction is never scored as a mere miss. This is
  also what makes the metric immune to negation: you are verifying a fixed claim
  against a passage, the one question whose answer flips on "not."
- **Fabrication (text → list):** a separate pass asks which assertions in the
  output are *not* supported by the gold list → `Dangerous{fabrication}`. This is
  the inherently harder, lower-confidence direction (it requires enumerating the
  output's assertions and has a granularity question), so it runs on a judge
  panel and its number is treated as softer than recall.

The only irreducible LLM step is **per-claim verification** — one fact vs. one
passage — which is the most reliable, most stable judge task there is (run a
panel, take majority). Embeddings have one safe job: a **shortlist/blocking**
step (find which gold claim a candidate assertion is nearest to, to cut the
judge cross-product) — never the agreement/contradiction decision.

## Per-site `gold` schemas

`actual` is fixed (the eight output schemas). Only `gold` is authored. Citation
validity is **folded into `gold`** (the case's resolvable ids), so the signature
needs no third "source" argument.

### Discrete sites

**match** — actual `{verdict:{same?,no_match?}, dup_pairs:[{a,b}]}`
```json
gold: { "verdict": {"same":"<subject_id>"} | {"no_match": true},
        "dup_pairs": [ {"a":"<id>","b":"<id>"} ] }
```
exact verdict compare; `Dangerous{false_merge}` when actual names a `same` where
gold is `no_match` or names the wrong id; dup_pairs scored as a set.

**dup_judge** — actual `{verdict: merge|dismiss|cant_tell, canonical_name?}`
```json
gold: { "verdict": "merge"|"dismiss"|"cant_tell" }
```
exact verdict; `Dangerous{false_merge}` when actual=merge, gold=dismiss.

### Claim-verification sites

**extract** — actual `{subjects:[{type,kind,name,aliases,claims:[{text}]}]}`
```json
gold: { "subjects": [
          { "name":"...", "type":"entity|event|concept",
            "claims_required":[ "<atomic fact that must be extracted>", ... ] } ] }
```
match gold subject → actual subject by name/alias; per `claims_required` →
affirm/contradict/silent (recall); unsupported actual claims → fabrication.
`Dangerous{over_extract, wrong_subject, contradiction}`.

**compile** — actual like extract, claims carry `cites`
```json
gold: { "subjects": [
          { "name":"...", "type":"...",
            "claims_required":[ {"text":"<fact>", "cites_required":["<inbox_id>", ...]} ] } ],
        "valid_cites": [ "<inbox_id>", ... ] }
```
extract's checks + each required claim's cites present; every actual cite ∈
`valid_cites`. `Dangerous{fabrication, cite_loss, bogus_citation, contradiction}`.

**merge** — actual `{pages:[{subject,title,body,superseded?}], stale_notes?}`
```json
gold: { "subject":"...",
        "claims_required":[ "<fact the merged body must still assert>", ... ],
        "superseded_expected":[ "<id>", ... ] }
```
per `claims_required` → affirm/contradict/silent over `body` (recall = no fact
lost); compare `superseded`. `Dangerous{fact_loss, contradiction}`.

**fold** (lint) — actual `{title, body, superseded}`
```json
gold: { "title_expected":"...",
        "claims_required":[ "<fact from EITHER source page that must survive>", ... ],
        "superseded_expected":[ "<loser id>" ] }
```
required-claim recall over `body`. `Dangerous{fact_loss}`.

**stale** (lint, mixed) — actual `{title, body, superseded, dispositions:[{note_id,status}]}`
```json
gold: { "dispositions_expected":[ {"note_id":"...","status":"repaired"|"dismissed"} ],
        "claims_required":[ "<fact the repaired body must reflect>", ... ] }
```
exact per-`note_id` disposition compare (discrete) **+** required-claim recall
over `body` (judged). `Dangerous{wrong_disposition, fact_loss}`.

**ask** — actual `{answer, citations:[{subject,title}], found}`
```json
gold (answerable):   { "answerable": true,
                       "claims_required":[ "<fact the answer must state>", ... ],
                       "cites_required":[ "<subject_id>", ... ],
                       "valid_cites":  [ "<subject_id>", ... ] }
gold (unanswerable): { "answerable": false }
```
if `answerable:false` → require `actual.found==false`, else
`Dangerous{fabrication}=1`. If answerable → required-claim recall over `answer`
+ `cites_required` ⊆ actual citations + every actual citation ∈ `valid_cites` +
unsupported claims = fabrication.
`Dangerous{fabrication, contradiction, missing_citation, bogus_citation}`.

## The dataset record

A dataset is a list of cases:

| field | type | meaning |
|---|---|---|
| `case_id` | string | stable id within the dataset (e.g. `match-0007`); survives across generations |
| `site` | string | one of the **eight** site names; ties the case to its `eval_<site>` |
| `generation` | int | 1-based label; later = harder; saturated generations kept, never deleted |
| `failure_tag` | string | the dangerous behaviour stressed (`false_merge`, `over_extract`, `fabrication`, `contradiction`, …), from a per-site vocabulary; slices the dangerous axis. **Not** a difficulty label |
| `input` | object | the exact, byte-identical input the real call site consumes (site-shaped, `json.RawMessage`) |
| `gold` | object | the correctness spec above (site-shaped, `json.RawMessage`) |

The loader returns `input`/`gold` as `json.RawMessage`; each `eval_<site>`
unmarshals its own shapes, so adding a site never reshapes the record.

## Judge-model independence (unchanged)

The judge used inside claim-verification is a **single fixed model, held out of
the run's sweep** — it is never one of the models being scored, so a model can't
grade itself. Subjective criteria use a **panel of N samples**, majority
aggregated. The judge is **injected** (`Judge` interface), so the mechanical
(discrete) scorer surface stays unit-testable offline with a stub that
deterministically abstains — no key, no network in `go test`.

## Saturation (unchanged)

A generation is **saturated** — a signal to mint the next, harder one, never an
auto-action — when the headline ceiling is high (≥ 0.95) **and** the top
configurations are indistinguishable (headline spread ≤ 0.02) **with the
dangerous axes also indistinguishable**. Reported as an advisory line beside the
table; thresholds configurable; the human decides.

## Decision presentation (unchanged in spirit)

The report is one row per configuration — **never** a single composite rank —
with the headline, the **dangerous axis beside it (never folded in)**, cost
(total + per-case), and latency (mean + p95), always captioned with the
generation. Sorted by headline with the tie band grouped and the
safer-of-the-band (lower dangerous total, then lower cost) floated up. The
report is the input to a human's pick of the per-site `(prompt, model, effort)`
default; nothing auto-promotes.

## What the build must change

The current `wiki/internal/eval` + `wiki/cmd/wiki-eval` implement the superseded
design (bundle abstraction, ten-site registry, `match`-only adapter, single
table). To this design:

1. Replace the bundle/`-bundle` interface with four independent inputs
   (`-prompt`, `-dataset`, `-model`, `-effort`); keep content-hash caching
   beneath it.
2. Drop `candidates`/`search`/`sweep`/`canonical_name` as sites; reduce the
   registry to the eight call sites.
3. Replace the scorer-kinds taxonomy with one `eval_<site>(ctx, judge, actual,
   gold) → Verdict` per site, mechanics internal.
4. Author the per-site `gold` schemas above; golds are `claims_required` lists
   (+ folded `valid_cites`) for the claim sites, discrete specs otherwise.
5. Build the seven `Structured`-seam sites on one generic runner; give `ask` its
   own agent-loop runner. Today only `match` runs end-to-end.
