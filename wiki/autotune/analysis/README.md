# Analysis tuning folder

The 15 development and 8 holdout cases contain production-rendered question-only user turns and gold `sub_queries`, `keywords`, and `aliases`. Their split is aligned with the extract corpus. Run this folder with `autotune autotune/analysis` from the repository root.

The deterministic scorer ports the retired Go workbench math. Each list uses greedy embedding-cosine alignment from the `embed` CLI with `text-embedding-3-small`, threshold 0.80, two-way margin 0.03, and digit compatibility. The composite weights are 0.50 sub-query F1, 0.30 keyword F1, and 0.20 alias F1. Set `EMBED_BIN` to override the embedding executable.

`fixtures/partial` supplies fixed vectors and a hand-computed partial match: sub-query F1 is 2/3, keyword F1 is 1, alias F1 is 0, and the composite is 0.633333.
