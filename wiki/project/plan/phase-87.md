# Phase 87 — Absolute front-door URLs for every web subject link (`internal/web` + composition root)

*Realizes design Decision 59 (fully-qualified web subject URLs). Depends on Phase 70/71 (the web answer/subject pages + footers, the `Ref` adapter) and Phase 82/84 (the `AuthServer + Mount + "subject/"` base shape).*

Make every web subject link fully-qualified, matching the MCP `ask` citations:

- Derive `webBase := strings.TrimRight(rt.AuthServer(), "/") + wiki.Mount + "subject/"` in `cmd/wiki/main.go` where the web handler is assembled.
- Change the `wiki.Ref{Path,Name}` → web `Ref{Href,Name}` mapping from `Href:"subject/"+Path` to `Href: webBase + Path`, for the outbound footer, the inbound footer, and the `MentionsIn` (D44) result.
- Leave the navigation affordances relative: the "ask another question" control (`.`) and the D41 Home link are unchanged.

This corrects the collateral URL assertions on ids owned by frozen phases (the Phase 84 pattern): R-AU31-XI76 and R-PIAB-HZC0 (footer hrefs → absolute), and the end-to-end URL checks in R-AXQR-2TF9 (D44) and R-PODT-EU1H (D45) now expect the absolute `…/srv/wiki/subject/…` form.

**Done when** the suite is green (`go build ./...`, `go vet ./...`, `gofmt -l .` empty, `go test ./...`, `bin/check-migrations wiki`) and each id below is covered:

- R-8I6N-DL0I — a subject `Ref` renders as the absolute `<origin>/srv/wiki/subject/<type>/<slug>` for both `rt.AuthServer() == "https://acct.ikigenba.com"` and `"http://localhost:8080"` (full `/srv/wiki/subject/` prefix, exactly one `/` between parts; a relative href fails). *(httptest, both origins)*
- R-8JEJ-RCR7 — on both the answer page and a subject page, the "ask another question" control and the D41 Home link stay relative (resolve to the base / dashboard, not an absolute `https://…/srv/wiki/` URL) while subject links on the same page are absolute. *(httptest)*

(The corrected assertions on R-AU31-XI76, R-PIAB-HZC0, R-AXQR-2TF9, R-PODT-EU1H — all owned by frozen Phases 70/71 — are updated here to the absolute form and must stay green; those ids are not re-listed as newly realized.)
