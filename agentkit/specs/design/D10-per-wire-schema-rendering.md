# D10-per-wire-schema-rendering

A tool set is defined once, against the canonical subset (D9), and rendered into
each wire's own tool-declaration grammar by that wire's `RenderTools` (D5). The
canonical subset is the *input* to rendering; the differences between wires are all
in the envelope around the schema, and in one wire's need to trim the schema
further. Rendering is a pure function of `[]Tool` and lives entirely inside the
wire — there is **no shared dialect hook** for schema shaping, because the shapes
do not factor: what varies is each wire's declaration wrapper, not a set of knobs.

The four declaration shapes differ along two axes — how the function envelope
nests, and whether the schema field is named `parameters` or `input_schema`:

- **Flat function form.** The declaration is one object with `type: "function"`
  and the tool's `name`, `description`, and `parameters` (the schema) as sibling
  fields. Used by the Responses-style wire.

  ```
  {"type":"function","name":T,"description":D,"parameters":<schema>}
  ```

- **Nested function form.** The declaration wraps the same fields inside a
  `function` object under `type: "function"`. Used by the Chat-Completions-style
  wire. The nesting is the only difference from the flat form, and it is exactly
  the kind of gratuitous divergence that makes a shared "render a function" helper
  leak — so each wire owns its wrapper.

  ```
  {"type":"function","function":{"name":T,"description":D,"parameters":<schema>}}
  ```

- **Top-level schema form.** The declaration is `name`, `description`, and
  `input_schema` (the schema under a differently named field) at top level, with no
  `type` wrapper. Used by the Messages-style wire.

  ```
  {"name":T,"description":D,"input_schema":<schema>}
  ```

- **Grouped-declarations form.** Declarations are collected under a single
  `functionDeclarations` array inside a `tools` wrapper, each entry carrying
  `name`, `description`, and `parameters`. Used by the generateContent-style wire.
  This wire is also the **hardest trimmer**: its accepted schema is a strict subset
  of the canonical subset, so its `RenderTools` performs an additional narrowing
  pass (dropping or rewriting keywords it rejects) *inside the wire*, before
  emitting the declaration.

  ```
  {"tools":[{"functionDeclarations":[{"name":T,"description":D,"parameters":<schema>}]}]}
  ```

The hardest-trimmer's extra narrowing is deliberately **not** a canonical-subset
concern and **not** a shared hook. The canonical subset (D9) is the intersection
every wire can render; this one wire needs slightly less than it renders, and that
gap is a private detail of the wire's own `RenderTools`. Pushing the narrowing into
a shared dialect layer would leak one vendor's quirk into the common path and force
every other wire to carry a no-op hook. Keeping it inside the wire means the
canonical subset stays the single cross-wire contract (a tool set moves untouched
or fails identically, D9) while each wire privately reconciles the subset to its
own grammar.

Rendering never *widens* a schema: a wire that happens to accept richer schemas
than the canonical subset still receives only canonical schemas from the
orchestrator, and its `RenderTools` emits exactly what it was given (plus, for the
hardest trimmer, less). The tool-result and tool-call *wire shapes* — string
versus object arguments, the four distinct result envelopes — are the wire's
concern too (D5) and are pinned by the round-trip property test, not restated here.

## REQUIREMENTS

- R-47X2-1A8Z: Each wire's `RenderTools` MUST render a canonical-subset tool set (D9) into that wire's own tool-declaration grammar, and schema rendering MUST NOT be factored into a shared cross-wire dialect hook.
- R-494Y-F1ZO: The flat and nested function forms MUST be produced by their respective wires' own renderers — the declaration envelope (sibling fields vs. a nested `function` object; `parameters` vs. `input_schema`; grouped `functionDeclarations`) is owned per wire.
- R-4ACU-STQD: A wire whose accepted schema is narrower than the canonical subset MUST perform its additional narrowing inside its own `RenderTools`, and that narrowing MUST NOT appear in the canonical subset definition or in any other wire.
- R-4BKR-6LH2: `RenderTools` MUST NOT widen a schema beyond what the orchestrator supplied; a wire that accepts richer schemas MUST still emit only what the canonical schema expressed.
- R-4E0J-Y4YG: Each wire's rendered tool declaration MUST be pinned by a golden-fixture test asserting the exact declaration bytes for a representative canonical tool set.
