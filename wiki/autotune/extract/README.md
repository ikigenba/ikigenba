# Extract tuning folder

The 15 development and 8 holdout cases are fictional documents paired with gold subjects, claims, aliases, kinds, and occurrence dates. Run this folder with `autotune autotune/extract` from the repository root.

The deterministic scorer pairs subjects by type and normalized name or alias, then aligns claims with `text-embedding-3-small` embeddings from the `embed` CLI. Its declared weights are 0.35 subject F1, 0.50 claim F1, and 0.15 field accuracy; the alignment threshold is 0.80 with a 0.03 two-way margin. Set `EMBED_BIN` to override the embedding executable. The scorer reads credentials and cache behavior only through that CLI.

`fixtures/perfect` is the offline scorer self-test. Its empty claim lists deliberately exercise arithmetic without an embedding call; `expected.json` records the hand-computed perfect and malformed-output floor scores.
