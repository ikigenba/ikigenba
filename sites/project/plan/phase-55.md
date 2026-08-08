# Phase 55 — Compose the plane at the root: client, consumer, seeding, docs

*Realizes design Decision 13 (the reflection graph: R-FJAZ-2KOO) and Decision 32 (the composition slice: R-ER9A-9UMP). Depends on Phases 50, 52, 53, 54.*

`cmd/sites/main.go` wires everything the earlier phases built:

- the `VersionClient`, constructed inside `Spec.Handlers` over
  `config.EnvOr(os.Getenv, "REPOS_BASE_URL", registry.BaseURL("repos"))` and
  `rt.HTTPClient(30*time.Second)`, and passed into `mcp.NewHandler`;
- one `Spec.Consumers` entry for the `repos` upstream carrying the
  `repos:push/sites/**` subscription and the Phase 53 handler;
- the background seeding pass: `sites.Seed` started in a goroutine once the
  server is listening, retrying with backoff while it errors — never on the boot
  path.

`AGENTS.md` is trued up: sites is an event-plane **consumer** (it was declared as
neither), and the Tests section keeps its D31 declaration accurate. `etc/manifest.env`
picks up the `CONSUMES` key the chassis derives.

D13's **retired reflection id** — named in `project/design/INDEX.md`'s
retired-ids note, and deliberately not written out here so it cannot be mistaken
for work this phase realizes — goes away with its tagged test: the existing
reflection test is rewritten under `R-FJAZ-2KOO` against the new expectation.

**Done when:**

- R-FJAZ-2KOO — `tools/call reflection` through the assembled handler returns an
  empty `publishes` array and a `subscribes` containing exactly one entry with
  source `repos` and filter `repos:push/sites/**`.
- R-ER9A-9UMP — the composed boot smoke starts sites' real built binary with
  `REPOS_BASE_URL` pointed at a conforming stub on loopback, performs one
  `file_write` through the running service's MCP endpoint, and the stub records
  the resulting commit call. **Substrate: composed.**
- The suite is green.
- Deterministic structural checks, both returning no match (exit 1). The first
  assembles the retired id from parts so this phase file never contains it:
  ```sh
  ID="R-P21E"; ID="$ID-0285"
  grep -rn "$ID" --include='*_test.go' --exclude-dir=project sites/
  grep -rn 'http.DefaultClient' --include='*.go' --exclude='*_test.go' --exclude-dir=project sites/
  ```
