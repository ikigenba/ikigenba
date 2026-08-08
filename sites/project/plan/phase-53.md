# Phase 53 — The `repos:push` materializer

*Realizes design Decision 35 (the `repos:push` consumer and re-materialization). Depends on Phase 49 and Phase 48.*

`internal/sites` gains the event handler and the materializer: the branch gate
(non-`main` → `nil`), the slug lookup (`ErrNotFound` → `consumer.ErrSkip`), the
echo gate (event sha == the row's `repo_sha` → `nil`), the whole-tree
`version.Export`, the path guard, the in-place reconcile into
`SiteDir(site.Visibility, slug)`, and `Store.SetRepoSha`. Export or filesystem
failures are returned so the engine stalls.

The path guard is the safety-critical part: an export entry whose
slash-separated path is absolute, contains a `..` segment, or contains a `.git`
segment fails the **whole** materialization before any file is written — no
filtering, no partial application.

This phase builds the handler and its tests only; declaring the subscription in
the `appkit.Spec` is Phase 55's.

**Done when:**

- R-F3GA-3K1N — a `main` push with a new sha replaces changed files, adds new
  ones, deletes locally-present files absent from the export, and records the
  pushed sha.
- R-F4O6-HBSC — a `feature/x` push returns `nil`, makes zero export calls, and
  changes no byte and no sha.
- R-OEVK-U243 — a `main` push whose sha equals the recorded `repo_sha` returns
  `nil`, makes zero export calls, and writes nothing; the next event with a
  different sha does export and rewrite.
- R-F5W2-V3J1 — an event naming a slug with no row returns an error satisfying
  `errors.Is(err, consumer.ErrSkip)` and writes nothing.
- R-F73Z-8V9Q — with `Export` made to fail, the handler returns a non-`ErrSkip`
  error, the sha is unchanged, and the files are byte-identical.
- R-F8BV-MN0F — an export containing `index.html` plus `.git/config` (and
  separately `nested/.git/HEAD`) fails whole: a non-`ErrSkip` error, no `.git`
  anywhere under `SITES_ROOT`, and `index.html` **not** applied either.
- R-F9JS-0ER4 — export entries at `../../escape.html` and `/etc/escape.html`
  fail the same way, with no `escape.html` anywhere outside
  `SiteDir(v, slug)` (checked against the temp root's parent).
- The suite is green.
