# Phase 22 — Adopt the MCP self-discovery convention (routing `instructions` + a `guide` tool)

*Realizes design Decision 21 (Tier-0 routing instructions, the `guide` tool replacing `describe`, keyword-forward descriptions, the two-channel guide reference). Depends on Phase 21 (the `create(name, public?)` and `sync`-requires-existing behaviors the guide's worked examples and the `sync` description describe).*

Make the sites MCP surface self-describing per
`docs/mcp-discovery-convention.md`. All in `internal/mcp`; no store, schema,
nginx, or landing change; every domain verb's behavior is unchanged (surface
wording only).

- **`sites/internal/mcp/mcp.go`** — rewrite the `Instructions` const to name the
  domain in user vocabulary (website, landing/marketing/docs page, HTML/CSS/JS,
  public/private, host), state the create → write → set-visibility flow, and
  point at the `guide` tool; it no longer mentions `describe`.
- **`sites/internal/mcp/guide.md`** — a new embedded document (the seven sections
  in D21): the model with the create-first rule, slug rules, the confinement
  rule (`path_escapes_working_dir`), the public-in-one-call example, the private
  example, the Dropbox `create`-then-`sync` example, and the error self-correction
  map.
- **`sites/internal/mcp/tools.go`** — delete the `describe` tool and
  `toolDescribe`; add a `guide` tool via `//go:embed guide.md` → `var guideDoc
  string`, declared flat (object schema, no `required`), read-only, input-free,
  returning `appkitmcp.TextResult(guideDoc)`; its description is the only tool
  description that references the guide. Keyword-forward the `create` and `sync`
  descriptions (lead with domain vocabulary; `sync` stays free of "publish"/
  "deploy"). Update the `tools_test.go` want-set (`describe` → `guide`) — this
  keeps R-0UUY-N97T's fifteen-tool count (Phase 12's id) true through the rename.

**Done when:** the sites suite is green (`cd sites && go build ./...`, `go vet
./...`, `gofmt -l .` prints nothing, `go test ./...`, `bin/check-migrations sites`),
AND R-57KJ-V5SQ: a test asserts `tools/list` contains a tool named `guide` and
**no** tool named `describe`, that `guide`'s input schema is an object with no
`required`, and that `tools/call guide` (no arguments) returns a single non-empty
text content block (not an error envelope);
AND R-58SG-8XJF: a test over the embedded `guideDoc` asserts it contains the
create-first rule, a public-in-one-call example invoking `create` with `public`,
the Dropbox flow showing `create` before `sync`, and the token
`path_escapes_working_dir`;
AND R-5A0C-MPA4: a test asserts the `Instructions` string contains `website` and
`guide` and does **not** contain `describe`;
AND R-5B89-0H0T: a test asserts `Instructions` contains `guide`, the `guide`
tool's `Description` is non-empty, and for every **other** tool in the table its
`Description` does not contain `guide`;
AND R-5CG5-E8RI: a test asserts the `sync` tool's `Description` contains neither
`publish` nor `deploy` and does contain `Dropbox`.
