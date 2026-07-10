# Phase 19 — MCP write tools (put / mkdir / delete / move)

*Realizes design Decision 19. Depends on phase 18 (the `Service` write methods
these tools declare over) and on the phase 10 `internal/mcp` tool-table shape.*

Observable end state:

- `internal/mcp` declares four new domain tools over the same `Service` write
  methods, added to `Tools(svc)`: `put(path, content_base64)` → `Service.Write`
  (base64 decoded, **25 MiB** cap → `too_large`, like `get`), `mkdir(path)` →
  `Service.Mkdir`, `delete(path)` → `Service.Delete` (file or dir, recursive),
  `move(from, to)` → `Service.Move`. Each reuses the existing sentinel→code error
  envelope (`not_found`/`conflict`/`validation`/`too_large`/`internal`). An MCP
  write's `origin` is the authenticated caller's `client_id`.
- The advertised surface is now **eight** tools: `list`, `get`, `put`, `mkdir`,
  `delete`, `move` (dropbox-declared) plus chassis `health`, `reflection`.

**Done when:** the suite is green (design Conventions commands, from `dropbox/`)
and:

- R-KRXK-IQE2 is covered by a test asserting `tools/list` returns exactly those
  eight tools (the six declared + the two chassis) — the reserved-name and count
  partition.
- R-KT5G-WI4R is covered by a test asserting MCP `put` of a small base64 body
  writes the file (a following `get` returns the bytes) and enqueues an upload,
  and that a decoded body over 25 MiB is rejected `too_large`.
- R-KUDD-A9VG is covered by a test asserting MCP `mkdir`/`delete`/`move` invoke
  the same `Service` methods as the loopback routes (empty dir listable, recursive
  delete, one-op move) and that a failure surfaces through the sentinel→code
  envelope.
