# autotune research — external facts for the eval workbench (D64–D67)

Free-form working note (not the research spine). Ground truth gathered while
designing the extract-prompt workbench, so the build loop need not re-derive it.

## agentkit (the dev workbench's provider client)

- Module `github.com/ikigenba/agentkit`; source checked out at
  `~/projects/agentkit`. Versions are annotated git tags only; latest at
  design time: **`v0.7.0`** — the pin the go.mod require should use.
- Provider subpackages: `anthropic/`, `openai/`, `google/`, `zai/`. Chat-style
  one-shot calls go through the root package's orchestration over a provider
  client (read the provider package's docs at build time for the exact
  constructor; keys come from the standard env vars `ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`).
- Embeddings: root package `embedding.go` defines the seam —
  `EmbeddingProvider` (`Embed(ctx, *EmbedRequest) *EmbedRoundTrip`, `Name()`),
  `EmbedRequest{Model string; Inputs []string; Role InputType; Dimensions int;
  Retry RetryPolicy}`, `EmbedRoundTrip.Vectors() [][]float32`. The **openai**
  and **google** provider packages implement it; anthropic does not (Anthropic
  has no embedding endpoint). Hence the D64 embedding pin rides the openai
  provider.

## The pinned embedding model

- `text-embedding-3-small` (OpenAI) — the same model wiki production already
  defaults to for page/query embeddings (`internal/wiki/config.go`,
  `defaultEmbedModel`), default 1536 dimensions. Sentence-pair similarity at
  gold-claim length (6–15 word self-contained sentences) is the model class's
  benchmark sweet spot; the known weakness for our use is elevated baseline
  similarity between same-subject claims, mitigated in D65 by the margin rule
  and the digit-token rule, plus threshold calibration on real pairs.
- Determinism caveat: embedding output is stable for a pinned model, but the
  provider could retrain under the same name; the D65 disk cache (keyed
  model + text hash) makes historical scores immune to that drift.

## The retired harness (what to salvage, what not to repeat)

- Deleted in commit `7b99268c` ("wiki Phase 94"); recoverable via
  `git show 7b99268c^:<path>`. Salvage: the two gold cases
  `wiki/testdata/eval/extract/meridian-freshcrate-acquisition/` and
  `wiki/testdata/eval/extract/tulsa-lab-opening/` (document.txt + gold.json —
  the D64 seed corpus; the new gold shape adds `aliases` per subject). Do not
  repeat: the LLM judge (`internal/eval/judge.go`) — non-deterministic scoring
  is the core defect the new design removes.

## ralph (the improvement-loop executor)

- Binary documented at `~/projects/ralph/README.md`. Facts the loop design
  leans on: prompt files are passed by path and re-run in fresh contexts until
  the prompt declares `DONE`; `--harness` picks the agent backend and
  `-c key=value` passes per-agent config (model choice etc.) — which is how the
  improver model stays a launch-time operator choice, independent of the eval
  model pinned in `config.json`; all loop state must live in the workspace
  because nothing carries across turns.
