# Match tuning folder

The development and holdout cases are fictional subject-level correction sets from the shared tuning universe. Each input reproduces the production `match.Render` shape, including zero-based correction and claim lists and existing/new provenance markers. Run this folder with `autotune autotune/match` from the repository root.

The scorer is offline and deterministic. It expands candidate judgments into `(correction, relation, target)` index edges, validates every index against the case counts, and computes exact set F1 against `gold.json`. Malformed output and any out-of-range index score the declared zero floor. Duplicate candidate edges do not inflate or penalize the set.

`fixtures/edges` records hand-computed agreeing, partial, disjoint, malformed, and out-of-range results used by the ordinary Go suite.
