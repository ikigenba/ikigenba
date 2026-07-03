# agentkit — cleanup findings

## High-priority (named migrations)
- none

## Other stale info
- none

## Notes
- agentkit is a pure Go library: no markdown docs, no `project/` dir, no
  `.envrc`/`CLAUDE.md`, no deploy scripts. Nothing describes deploy format or
  service naming/inventory, so neither named migration applies here.
- Every "registry" reference in the code (`model/registry.go`, `tools/tools.go`,
  `job/job.go`) is a model-pricing or tool-descriptor registry — unrelated to
  the top-level service-name `registry/` migration. Not stale.
- Every "symlink"/"deploy"-adjacent reference (`tools/confine.go`,
  `tools/write/write.go`) is filesystem path-safety / atomic-rename code, not
  deploy tooling. Not stale.
- Comments flagged by a legacy/deprecated grep are all benign and current:
  bugfix notes (`provider/anthropic/anthropic.go:209`), wire-compat notes
  (`wire/event.go:59`, `wire/tool_result_block.go:6`), and a genuinely supported
  "legacy/unconfined" confine mode (`tools/confine.go:14,54`).
- Model IDs in `model/registry.go` (`claude-haiku-4-5`, `claude-sonnet-4-6`,
  `gpt-5.5`) are live operative code data, not documentation; not evaluated as
  stale docs and out of scope for a no-source-edit review.
