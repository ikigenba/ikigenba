# Phase 21 — `suite.fetch` and `suite.files`: the content-plane and file-share clients

*Realizes design Decisions 23 (`suite.fetch`) and 24 (`suite.files`). Depends
on Phase 20.*

Implement the byte-moving half of `suite.py`. `suite.fetch(content_url, dest)`:
the D23 URL confinement (scheme `http`, literal loopback host, explicit port
from the injected origins; violations → `ToolError("validation")`, zero
requests), a single no-redirect `GET` streamed to a temp file with SHA-256 on
the same pass then `os.replace` (torn stream leaves no partial file), the
`{path, size, content_hash}` result, and the pinned failure mapping. The
`suite.files` namespace: all seven verbs over `SUITE_FILES_BASE_URL`
(`list`/`stat`/`get`/`put`/`delete`/`move`/`mkdir`), service-agnostic wording,
`X-Client-Id: scripts:$SUITE_SCRIPT_ID` with no `X-Owner-Email`/
`X-Forwarded-Proto`, `get` reusing the fetch streaming core, `put` streaming
the local file as the request body, verbatim return shapes (`None` for the 204
verbs), and the status-derived failure mapping (400 body detail →
`validation`). Tests ride the Phase 19 probe harness against recording
`httptest` stand-ins.

**Done when:**

- R-I7RP-9G6B — a named test proves `fetch`'s happy path: one recorded `GET`,
  byte-identical file at `dest`, and the `{path, size, content_hash}` result
  with the correct lowercase-hex SHA-256.
- R-I8ZL-N7X0 — a named test proves URL-confinement rejections (non-loopback
  host, hostname, unregistered port, non-http scheme) each raise
  `ToolError("validation")` with zero requests, while the byte-equivalent
  allowed URL proceeds.
- R-IA7I-0ZNP — a named test proves the fetch failure mapping (404 →
  `not_found`, 409 → `conflict`, refused → `source_unavailable`, 302 not
  followed → `source_unavailable`) and that no file exists at `dest` after any
  failure, including a torn body.
- R-IBFE-EREE — a named test proves all seven `files` verbs send `X-Client-Id`
  exactly `scripts:$SUITE_SCRIPT_ID` and never `X-Owner-Email` or
  `X-Forwarded-Proto`.
- R-ICNA-SJ53 — a named test proves `files.get` streams the served bytes to
  `dest` via one `GET /content` and returns the hash triple.
- R-IDV7-6AVS — a named test proves `files.put` streams the local file's bytes
  as the `PUT /content` body and returns the server's JSON verbatim, while a
  missing local source raises `FileNotFoundError` with zero requests.
- R-IF33-K2MH — a named test proves `files.list`/`files.stat` pass their query
  params through and return the server's JSON verbatim.
- R-IGAZ-XUD6 — a named test proves `delete`/`mkdir`/`move` hit their exact
  routes with URL-escaped params and return `None` on 204.
- R-IHIW-BM3V — a named test proves the files failure mapping (400 + detail →
  `validation` carrying the detail, 404 → `not_found`, 409 → `conflict`,
  refused → `source_unavailable`) and no file at `dest` after a failed `get`.
- The scripts suite is green per design Conventions.
