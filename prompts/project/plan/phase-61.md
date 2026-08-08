# Phase 61 — `internal/version`: the version-plane client

*Realizes design Decision 52 (version-plane client seam).*

A new package `prompts/internal/version` holding the `Client` interface, the
`File`/`Definition`/`Credential` types, the typed errors
(`ErrNotFound`/`ErrConflict`/`ErrValidation`/`ErrUnavailable`), and one HTTP
implementation constructed from a base URL and a client-id function. The
`prompts` kind is a package constant; every method addresses the plane by name
key. Requests carry `X-Client-Id: prompts:<prompt id>` and no nginx identity
headers. `Commit` sends one request per batch.

Read the repos spec's loopback surface for the concrete routes and payload
shapes; this design deliberately names none of them.

Nothing else is wired yet: the composition root does not construct the client in
this phase and no domain code calls it.

**Done when:**

- `R-RH3E-NZW4` — a test drives all seven methods against an `httptest` server
  and asserts every request carries `X-Client-Id: prompts:<prompt id>`, no
  `X-Owner-Email`, no `X-Owner-Id`, no `X-Forwarded-Proto`, and addresses the
  `prompts` kind (including for a name key containing `../sites/blog`).
- `R-RIBB-1RMT` — 404/409/400/500 and a connection-refused base URL map to
  `ErrNotFound`/`ErrConflict`/`ErrValidation`/`ErrUnavailable` respectively,
  with the 400 body's detail in the message.
- `R-RJJ7-FJDI` — `Commit` with two writes and one delete issues exactly one
  request carrying all three paths and returns the server's sha.
- `R-RKR3-TB47` — a source scan finds `"repos"` and `registry.BaseURL("repos")`
  in no non-test Go source outside `internal/version` and `cmd/prompts`
  (excluding `project/`).
- `go build ./...` and `go test ./...` from `prompts/` are green;
  `gofmt -l .` is empty.
