# Phase 88 — Inline links in the web answer and subject prose (`internal/web`)

*Realizes design Decision 44 (answer prose inline links) and Decision 45 (subject prose inline links), consuming Decision 58. Depends on Phase 85 (`Service.LinkifyMentions`) and Phase 87 (`webBase`).*

Route the web prose through the D58 linkifier before the D48 `md` render, using `webBase`:

- **Answer page** (D44) — inject a `Linkifier` seam beside `Asker`/`Mentioner`; the ask path runs `LinkifyMentions(ctx, answer.Text, webBase, "")` and the template renders the linkified text through `md`. Honest-empty names no subject, so linkify is a no-op there.
- **Subject page** (D45) — the `PageByPath` adapter sets `SubjectView.Body = LinkifyMentions(ctx, page.Body, webBase, subject.ID)` (own subject excluded) before `md`.

goldmark renders the injected `[…](https://…)` as `<a>`; bluemonday (D48) passes the absolute https href. Footers (Phase 87) are unchanged; a subject appears both inline at first mention and in the footer, at the identical absolute URL.

**Done when** the suite is green (`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`, `bin/check-migrations wiki`) and each id below is covered:

- R-8FQU-M1J4 — the answer page's **prose** contains `<a href="https://…/srv/wiki/subject/entity/acme-corp">Acme Corp</a>` at the first mention, driven through the **real** `LinkifyMentions` over a real temp SQLite — proving the ask path linkifies `Answer.Text` before rendering. *(integration)*
- R-8GYQ-ZT9T — a subject page's prose inline-links the first mention of a *referenced* subject (absolute href) and does **not** wrap the page's own subject name (self-exclusion), driven through the **real** `LinkifyMentions` + `ResolveByPath` over a real temp SQLite. *(integration)*

(R-NPVU-26CX / R-NONX-OEM8 — markdown-render wiring — stay green; the linkified text still round-trips through `md`.)
