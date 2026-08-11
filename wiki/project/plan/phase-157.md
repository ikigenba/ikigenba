# Phase 157 — `internal/llm` becomes the completion-queue client

*Realizes design Decision 5 (the prompts completion-queue client).*

Rewrites `internal/llm` to the D5 shape: the four queue verbs (`Ensure`/`Get`/`Inbox`/`Ack`) over `POST|GET|DELETE /completions…` with `Consumer = "service:wiki"`, the live `Do` (ensure + injectable-interval poll + ack), and `JSON[T]` over `Do` with semantic corrective re-ensures (`/a<attempt>` key suffixes, full stateless replay). `ExtractJSON`, the old `/complete` transport, and the retired D5 ids' tests (R-J8QP-BETB, R-4BCC-0EHJ, R-J9YL-P6K0, R-JCEE-GQ1E, R-0X4N-U0XB, R-8H1B-9CCI, R-0ZKG-LKEP, R-10SC-ZC5E, R-1209-D3W3) are deleted. The D18 truncation ids (R-MSKH-GPX5, R-MTSD-UHNU, R-MV0A-89EJ) move their tests to the new seam in this same package rework. Compilation of dependent packages may be kept green with mechanical call-site adaptation; their behavioral rework is phases 158/159.

**Done when:**

- These Verification ids are covered by clearly-named tests tagged verbatim, driven against an httptest server playing the prompts queue: R-JW4G-367U, R-JXCC-GXYJ, R-JYK8-UPP8, R-JZS5-8HFX, R-K101-M96M, R-K27Y-00XB, R-K3FU-DSO0, R-K4NQ-RKEP.
- R-MSKH-GPX5, R-MTSD-UHNU, and R-MV0A-89EJ remain tagged by tests exercising the new seam (payload `max_tokens`, usage-at-ceiling → `ErrTruncated`, terminal-no-corrective).
- `grep -rn 'R-J8QP-BETB\|R-4BCC-0EHJ\|R-J9YL-P6K0\|R-JCEE-GQ1E\|R-0X4N-U0XB\|R-8H1B-9CCI\|R-0ZKG-LKEP\|R-10SC-ZC5E\|R-1209-D3W3' --include='*_test.go' .` from `wiki/` returns nothing.
- `grep -rn 'ExtractJSON' --include='*.go' internal cmd` from `wiki/` returns nothing.
- `go test ./...` from `wiki/` is green.
