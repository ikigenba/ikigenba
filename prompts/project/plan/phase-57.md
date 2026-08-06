# Phase 57 — Resolve the state and cache roots through `appkit/config`

*Realizes design Decision 48 (state paths from `appkit/config`).*

`cmd/prompts` resolves prompts' durable paths once at startup with
`config.Resolve("prompts", "/srv/prompts/", registry.MustPort("prompts"),
os.Getenv)`. The state directory becomes `filepath.Dir(cfg.DBPath)` and the
cache directory `filepath.Dir(cfg.GenerationPath)`; `runsDir` stays
`<cacheDir>/runs` and the sandbox root stays `<stateDir>/sandboxes`. The
composition root no longer reads `PROMPTS_DB_PATH` or `PROMPTS_GENERATION_PATH`
itself and contains no literal `tmp` fallback, and it no longer defaults the
cache root to the state directory. `config.Resolve` continues to honour both
env overrides, so the env contract is unchanged. The runner, loaders, sandbox
model, `run_delete`, and the calls surface are untouched.

The resolution is a pure function of an injected `getenv`, so its tests drive it
directly with a map environment: no temp dirs, no filesystem, no LLM, no running
suite.

**Done when:**

- R-LBH5-4LO0 is covered by a test asserting that with `IKIGENBA_ROOT=/opt` and
  no explicit overrides, the resolved state directory is exactly
  `/opt/prompts/state` and the sandbox root is `/opt/prompts/state/sandboxes`.
- R-LCP1-IDEP is covered by a test asserting that under the same environment the
  resolved cache directory is exactly `/opt/prompts/cache`, the runs directory
  is `/opt/prompts/cache/runs`, and the cache directory is not equal to the
  state directory.
- R-LDWX-W55E is covered by a test asserting that explicit `PROMPTS_DB_PATH` and
  `PROMPTS_GENERATION_PATH` values are honoured verbatim for the state and cache
  roots even when `IKIGENBA_ROOT=/opt` is also set.
- `cd prompts && grep -rn '"\./tmp' cmd/ internal/` prints nothing.
- The suite is green: `cd prompts && go build ./...`, `gofmt -l .` (no output),
  and `go test ./...` all succeed.
