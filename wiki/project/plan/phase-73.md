# Phase 73 — docs purge: the web surface is a read UI, not a name+version card

*Realizes design Decision 39 (the stale-doc consequence of the web surface's
expansion). **Structural / docs-only phase — no R-ids.** Edits only
`wiki/AGENTS.md` (and its `CLAUDE.md` symlink); touches no Go code, no migration.
Depends on Phases 69–71 (the read surface the docs must now describe).*

Phase 65 corrected the "no UI" line and stated the **landing-page** truth (a
session-gated human page showing **service name + version**). The surface has
since become a **read UI** — a search box, an `ask` result, per-subject pages —
so that description is now stale. Per suite doctrine (docs state current truth),
rewrite the web-surface sentence in `wiki/AGENTS.md` to describe the read surface:
a session-gated human **web read surface** (search/`ask` + per-subject pages,
Carbon-styled) at the mount root. The "no token logic" truth stays (nginx is
still the trust boundary; the web handlers read no token).

**Done when:** the suite stays green (no Go change — `cd wiki && go build ./... &&
go vet ./... && gofmt -l . && go test ./...` and `bin/check-migrations wiki`
unaffected) **and** the named docs check passes (scoped to `wiki/AGENTS.md`, never
`project/`):

- `grep -qiE 'search|read surface|subject page' wiki/AGENTS.md` — the
  web-surface description now names the read UI (exit 0).
- `! grep -qi 'service name + version' wiki/AGENTS.md` — the stale name+version-
  only framing is gone (exit 0; reachable because the phrase is removed from this
  one file).

(Structural phase: "Ids to cover" is **(none — structural phase)**; the build
loop verifies the green suite plus the docs check above, not an `R-id` test.)
