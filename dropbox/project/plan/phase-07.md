# Phase 7 — Adopt `registry` at the composition root

*Realizes design Decision 9 (adopt `registry`; resolve dropbox's own loopback
address by name at startup). Depends on the existing `cmd/dropbox/main.go`
composition root and `internal/dropbox/events.go`. Covers `R-QJ8F-AXWP`,
`R-QKGB-OPNE`. **Read D9 for the exact call sites and rationale.***

dropbox stops hardcoding loopback port literals and references itself **by name**
through the shared `registry` library, resolving **once at the composition root**.
This is behavior-preserving: `registry` already carries dropbox's current value
(`dropbox=3200`), so every resolved value is byte-identical to the literal it
replaces. Env overrides (`DROPBOX_PORT`) are unchanged — `registry` supplies only
the *default* the override falls back to.

**External precondition (assume satisfied; do NOT build it here).** The repo-root
`go.work` carries `use ./registry` and the `registry` module exists and is green.
Both are owned outside `dropbox/`. No step in this phase edits `../go.work`,
`../registry/`, `../bin/`, or any sibling module — the executor runs from
`dropbox/` and cannot reach outside it.

**What gets changed (all inside `dropbox/`):**

- **`dropbox/go.mod`** — add `require registry v0.0.0` and a committed
  `replace registry => ../registry`, mirroring the existing `appkit` /
  `eventplane` in-repo replace-siblings. This is the only build-graph change.
- **`dropbox/cmd/dropbox/main.go`** — import `registry` and replace two own-port
  int literals per D9:
  - the appkit `Spec.Port` value `3200` → `registry.MustPort("dropbox")`;
  - in the `Handlers` hook, the `DROPBOX_PORT` default in
    `config.EnvOrInt(os.Getenv, "DROPBOX_PORT", 3200)` →
    `registry.MustPort("dropbox")`. Leave the `DROPBOX_IP` read, the content-base
    composition, and every other line unchanged — only the two `3200` defaults
    become a `registry` call.
- **`dropbox/internal/dropbox/events.go`** — import `registry` and replace the
  reflection Sample's hardcoded origin: `contentURL("http://127.0.0.1:3200", …)`
  → `contentURL(registry.BaseURL("dropbox"), …)` (== the same bytes). Reword the
  `NewOutboxProducer` doc comment that spells `"http://127.0.0.1:3200"` in prose
  so it names the loopback `/content` origin **without** the literal address (so
  the D10 source-scan guard has no comment needle to trip on). Do not change the
  runtime `buildFilePayload` path — it already composes from the injected
  `contentBase`.
- **`dropbox/internal/dropbox/events_test.go`** (or the existing events test
  file) — add a genuinely-asserting test tagged `// R-QKGB-OPNE`: over the
  package `Events` registry, assert each entry's sampled payload `content_url`
  begins with `registry.BaseURL("dropbox") + "/content?path="` (and still equals
  the old `http://127.0.0.1:3200/content?path=%2Fnotes%2Fmeeting.md` byte-for-byte).
- **`R-QJ8F-AXWP`** — the composition root's port is `registry.MustPort("dropbox")`.
  The Spec is not directly inspectable at runtime, so this id is satisfied by the
  manifest drift guard in phase 08 (`manifest.Emit` with
  `registry.MustPort("dropbox")` byte-matches the committed `etc/manifest.env`);
  it is tagged there and delegated here.
- Touch nothing else. **No schema change — no migration.** Do not edit
  `etc/manifest.env` or `etc/nginx.conf` (phase 08 re-points their *tests* at
  `registry`; the files' literals stay). Do not delete or move `internal/web`
  (phase 09).

**Done when:**

- R-QKGB-OPNE — a test proves each `file.*` reflection Sample's `content_url`
  origin is `registry.BaseURL("dropbox")` (byte-equal to the retired literal).
- R-QJ8F-AXWP — the composition root's listen port and the content-base default
  are `registry.MustPort("dropbox")`, not `3200` literals (delegated to phase
  08's manifest guard for the executable assertion).
- `dropbox/go.mod` requires `registry` with a committed
  `replace registry => ../registry`.
- The suite is green: `cd dropbox && go build ./...`, `cd dropbox && go vet ./...`,
  `cd dropbox && gofmt -l .` (prints nothing), `cd dropbox && go test ./...`, and
  `bin/check-migrations dropbox`.
