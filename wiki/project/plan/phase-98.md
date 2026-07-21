# Phase 98 — The eval workspace: prompt-as-data under `eval/extract/`

*Realizes design Decision 64 (slice: R-KICZ-E6WA — the workspace files and the embed identity; the loaders' ids ride Phase 99). Depends on Phase 97.*

The `eval/extract/` workspace comes into existence and the production extract
prompt becomes data:

- `eval/extract/prompt.txt` — created with the current value of
  `extract.DefaultPromptInstructions`, byte-for-byte.
- `eval/extract/prompt.go` — package `extractprompt`, embedding `prompt.txt`
  and exporting `Instructions`.
- `internal/extract` — `DefaultPromptInstructions` becomes a `var` initialized
  from `extractprompt.Instructions` (same exported name; production call path
  and prompt content unchanged).
- `eval/extract/config.json` — the D64 pinned config, verbatim.
- The seed gold corpus, recovered from git history
  (`git show 7b99268c^:wiki/testdata/eval/extract/<case>/...`), reshaped to the
  D64 gold schema (add `"aliases": []` per subject):
  `eval/extract/gold/dev/meridian-freshcrate-acquisition/` and
  `eval/extract/gold/holdout/tulsa-lab-opening/`, each with `document.txt` +
  `gold.json`.
- `wiki/.gitignore` gains `/tmp/` (the disposable loop workspace, D64).

**Done when:**
- R-KICZ-E6WA — `extract.DefaultPromptInstructions` is byte-identical to the contents of `eval/extract/prompt.txt` — covered by a tagged test that reads the file and compares.
- `eval/extract/prompt.txt`, `eval/extract/prompt.go`, `eval/extract/config.json`, `eval/extract/gold/dev/meridian-freshcrate-acquisition/{document.txt,gold.json}`, and `eval/extract/gold/holdout/tulsa-lab-opening/{document.txt,gold.json}` all exist.
- `grep -c '^/tmp/$' .gitignore` from `wiki/` prints `1`.
- The suite is green per design Conventions.
