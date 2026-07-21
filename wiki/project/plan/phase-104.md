# Phase 104 — The autotune driver core: CLI, resolved config, workspace lifecycle

*Realizes design Decision 68 (autotune driver), slice R-EYQR-OABI, R-EZYO-2227, R-F2EG-TLJL, R-F16K-FTSW. Depends on Phase 103.*

`cmd/autotune` exists with its CLI contract and everything that happens before ralph starts: step dispatch (extract only; anything else errors naming the supported set), agentrepl-style `-c` parsing that overrides only the `eval` block into a resolved `tmp/autotune/<step>/config.json` (unknown or `embedding.*`/`weights.*` keys rejected by name), and the workspace lifecycle — fresh-run wipe + prompt seeding (`--from` variant) + runner build + 3× dev baseline through an injected executor seam, and the `--resume` config-stamp guard (mismatch is a hard error naming both configs; `--resume`+`--from` is a flag error). No agentkit import anywhere in `cmd/autotune` (D63's confinement grep must stay true).

**Done when:**
- R-EYQR-OABI — step dispatch and flag-combination errors — covered by a tagged test.
- R-EZYO-2227 — fresh-run provisioning order and contents via the scripted executor — covered by a tagged test.
- R-F2EG-TLJL — override resolution and rejection-by-name — covered by a tagged test.
- R-F16K-FTSW — resume guard: matching stamp skips baseline; mismatch errors naming both — covered by a tagged test.
- The suite is green per design Conventions.
