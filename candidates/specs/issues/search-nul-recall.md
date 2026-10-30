# Search recall loses an exact name containing NUL

Filed during the user-authorized `build-spec` run for `candidates`, starting at `dbf8c41ffe62332107245e599e93d13511a53f99` on `wip-search` (thread `01a18e29-3a1c-76e2-a164-985978c386f1`). The store implementer reproduced the failure and a fresh independent blocker validator confirmed it through the store API. Design and AGENTS.md were not changed.

Requirements involved: R-7GUS-5A5Q and its dependent store guarantees R-4KDM-VQPC and R-7KIH-ALDT; normalization R-78BH-GVYV, tier/score R-7ARA-8FG9 and R-7BZ6-M76Y, fixed FTS migration R-4J5Q-HYYN, and accepted candidate names R-F1IL-EZ5P and R-FA1W-3DCK.

## Friction

An active candidate whose sole name is `A\u0000B` must be returned with tier 1 and score 1 when searched for that exact name. It satisfies R-7GUS-5A5Q's small-store premises: one active name, a three-rune ASCII query, no tier-3 name and no tier-5 name. That requirement calls for "exactly the first `limit` entries of the ranking."

The prescribed two recall passes cannot recall this candidate. Normalization produces `a\u0000b` with words `a` and `b`. Pass 1 says "a query with no word of three runes skips the pass." Pass 2 says "a trigram the index lacks has no vocab row and drops out." Its only three-rune window contains NUL; FTS5's prescribed trigram tokenizer has no vocabulary row for it. Both passes therefore return nothing, despite the required exact match.

## Evidence

The implementer and a fresh validator separately exercised the current store using an external temporary Go test overlay, without creating a probe in the repository. The fresh validator ran `go test -overlay <temporary-overlay> -run '^TestFreshNULBlockerProbe$' -count=1 -v ./internal/store`, opening the database through appkit with the embedded migrations and calling `store.New`, `Attach`, `GetCandidate` and `Search`.

```text
Attach: Rows=1 New=1 Created=1, nil error
GetCandidate: Status="active" Name="A\x00B" Names=["A\x00B"]
Search("A\x00B", 10): []store.Match{}, nil error
```

The independent probe exited 1 because it expected one match with the original name and score 1. Its temporary files were removed. Ordinary search, migration and timing checks pass; this counterexample is outside their ordinary fixtures, not a failing toolchain or speed issue.

## Why the build cannot resolve it

Adding an exact-name lookup or a small-store scan introduces a recall path beyond the design's settled two passes. Encoding or replacing NUL changes the raw-name/trigram contract and fixed migration, while rejecting or sanitizing an accepted candidate name changes the ingest contract. The build treats those contracts as read-only and cannot choose a different one. The three affected guarantee ids remain open rather than carrying inadequate coverage tags.

## Suggested resolution

Revise the design to permit a recall fallback for names or queries the prescribed tokenizer cannot represent, or explicitly revise the accepted-input and exact-ranking guarantees together. Re-mint every changed requirement id, then start a new build run. Add the exact NUL case to the revised contract's by-use regression coverage.
