# Phase 19 — The `suite` module chassis: embed, materialize, environment, `event()`, `ToolError`

*Realizes design Decision 21 (the runner-injected `suite` module). Depends on
Phase 18.*

Create `internal/runner/suite.py` (stdlib-only, Python ≥ 3.11) holding the
module skeleton: the `ToolError` exception (`.code`/`.message`, message
`"<code>: <message>"`), the lazy `SUITE_*`/`EVENT_JSON` env readers that raise
`RuntimeError` naming the missing variable, the cached `suite.event()`, the
`suite.files` namespace stub, and the shared HTTP posture constants (30 s
socket-op timeout, no retries) that Phases 20–21 flesh out. Embed it in package
`runner` (`//go:embed suite.py`); `Runner.execute` materializes it (0600)
beside `main.py` before exec. `runner.New` grows a `suiteEnv []string`
parameter; `cmd/scripts/main.go` builds it from `registry.Services` (the
`SUITE_SERVICES` JSON map) plus `SUITE_FILES_BASE_URL` (the same resolved
dropbox-base value already wired into `svc.Fetcher`); `execute` appends the
per-run `SUITE_SCRIPT_ID`, `SUITE_RUN_ID`, and `SUITE_OWNER_EMAIL`. This phase
also establishes the probe harness the spine's testing-strategy bullet
describes (temp run dir, real `python3`, `httptest`-backed `SUITE_*` env),
which Phases 20–21 reuse.

**Done when:**

- R-HVKP-FQRD — a named test proves a spawned run's dir contains `suite.py`
  byte-identical to the embedded source and a body of just `import suite`
  succeeds.
- R-HWSL-TII2 — a named test proves the child env carries `SUITE_SERVICES`,
  `SUITE_FILES_BASE_URL`, `SUITE_SCRIPT_ID`, `SUITE_RUN_ID`, and
  `SUITE_OWNER_EMAIL` with exactly the injected/spawning values (probe prints
  them to `stdout.log`).
- R-HY0I-7A8R — a named test proves `suite.event()` returns the trigger
  payload verbatim (deep-equal on a nested payload) and exactly `{}` for the
  manual-run `"{}"` input.
- R-HZ8E-L1ZG — a named test proves that with `EVENT_JSON` (resp.
  `SUITE_SERVICES`) unset, `suite.event()` (resp. `suite.mcp(...)`) raises
  `RuntimeError` naming the missing variable.
- The scripts suite is green per design Conventions.
