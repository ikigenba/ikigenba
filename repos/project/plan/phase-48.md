# Phase 48 — Composition-root wire proof for the git-door push event

*Realizes design Decision 19 (git smart-HTTP door).*

The git door's `push` emission is currently proven only at the outbox table
(R-K4BC-KLOH) and on the real `/feed` only for the loopback-commit door
(R-JID5-OQBZ). This phase adds the missing live proof for the **git-door
`receive-pack` path**, driving it through the real composition root so a wiring
regression (the door mounted on a producer-less service, or an outbox row that
never reaches `/feed`) is caught by the suite.

Add one composed-layer test in `cmd/repos` (the assembled-service layer,
alongside `TestInstalledLayoutBootsBuiltService`) that stands up the real
`cmd/repos` spec through the composition root — `spec.Handlers` mounted with the
eventplane producer injected via `spec.Producer` — on a real
`httptest.NewServer` socket, connects a subscriber to that same assembly's
`/feed`, then uses the real `git` binary to clone the git door and push a new
branch to a `sites/<name>` repository. The observable end state: the subscriber
receives the `push` envelope for the pushed branch off the real feed.

**Done when:**
- R-27UP-QL4F — a real `git` clone-and-push of a new branch through the git door
  of the **assembled `cmd/repos` service** (composition root, real producer via
  `spec.Producer`, real `httptest.NewServer` socket) causes a subscriber on that
  assembly's `/feed` to receive the `push` envelope for that branch: `source`
  `repos`, event/routing key `repos:push/sites/<name>`, `subject`
  `/sites/<name>`, a non-empty `correlation_id`, and a payload whose `sha`
  equals the pushed head and whose `actor` is the authenticated owner — covered
  by a test tagged `R-27UP-QL4F` in `cmd/repos/*_test.go`.
- The suite is green: `go test ./...`, `go build ./...`, and `go vet ./...`
  succeed and `gofmt -l .` prints nothing.
