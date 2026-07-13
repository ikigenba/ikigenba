# Phase 30 — File-share tools, streaming pair: the share client seam, `FileGet`, `FilePut`

*Realizes design Decision 26 (slice: `ShareConfig` plumbing, `FileGet`,
`FilePut`, client identity, failure mapping — R-F74Y-B8X1, R-F8CU-P0NQ,
R-F9KR-2SEF, R-FD8G-83MI) and Decision 5 (slice: the `ShareConfig` type and
the widened `tools.All` signature, without yet asserting the final count).
Depends on Phase 26 (the `Fetch` tool's streaming/confinement helpers this
phase reuses). ⛔ Operator-sequenced behind dropbox plan phase 25 (the refined
loopback mutation error contract).*

Observable end state:

- `prompts/internal/tools/` gains `ShareConfig{BaseURL, ClientID}` and the
  `tools.All(sandboxRoot, sourcePortAllowed, share)` signature (D5); the
  runner's `execute` threads the registry-defaulted `DROPBOX_BASE_URL` and
  `"prompts:" + <prompt id>` into it at the composition root.
- `FileGet(share_path, dest_path)` streams `GET /content` disk-to-disk into
  the sandbox (temp + SHA-256 on the same pass + rename, the D21 machinery),
  returning `{path, size, content_hash}`; `FilePut(source_path, share_path)`
  streams the sandbox file as a `PUT /content` body, returning the share's
  `{path, size, content_hash, rev}` verbatim.
- Sandbox-side paths (`dest_path`, `source_path`) resolve through D5's shared
  confinement; every request carries `X-Client-Id` and no identity headers;
  failures map by HTTP status to the pinned `validation:` / `not_found:` /
  `conflict:` / `source_unavailable:` prefixes with the 400 body's detail
  included.
- The two new tools appear in the toolset; `Fetch` and the six file/shell
  tools are unchanged (the full 13-tool table and the framing prompt land in
  Phase 31).

**Done when:** the suite is green (design Conventions commands, from
`prompts/`) and:

- R-F74Y-B8X1 is covered by a test recording, on a real local `httptest`
  share stand-in, that every request `FileGet`/`FilePut` issue carries
  `X-Client-Id` exactly `prompts:<prompt id>` and no `X-Owner-Email` /
  `X-Forwarded-Proto` (the remaining four tools extend this assertion in
  Phase 31 against the same seam).
- R-F8CU-P0NQ is covered by a test streaming known served bytes to
  `dest_path` (bytes equal, exactly one recorded `GET /content`, URL-escaped
  `path` query, `{path, size, content_hash}` with the lowercase-hex SHA-256),
  with an escaping `dest_path` rejected `validation:` at zero requests and no
  file created.
- R-F9KR-2SEF is covered by a test asserting exactly one recorded
  `PUT /content` whose body equals the sandbox file's bytes, the server's
  `{path, size, content_hash, rev}` returned verbatim, an escaping
  `source_path` → `validation:` and an absent in-sandbox `source_path` →
  `not_found:`, each at zero requests.
- R-FD8G-83MI is covered by a test driving the mapping against the real local
  server: 400 + detail body → `validation:` containing the detail; 404 →
  `not_found:`; 409 → `conflict:`; a connection-refused base URL →
  `source_unavailable:`; after a failed `FileGet` no file exists at
  `dest_path`.
