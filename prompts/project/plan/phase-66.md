# Phase 66 — The run token and the authenticated git door

*Realizes design Decision 56 (run token, door, push scope). Depends on
Phase 65.*

Spawn mints a run credential through `version.RunToken` before cloning, asking
for a TTL of the configured run TTL plus five minutes, and installs it in the
clone's **local** config as `http.<cloneURL>.extraHeader` — never in the remote
URL. The clone and every git command the agent runs then authenticate against
the plane's git door.

The test substrate is a real bare repository served over loopback by the real
`git http-backend` through `net/http/cgi`, wrapped by a handler that records the
`Authorization` header, refuses unauthenticated requests, and refuses updates to
`refs/heads/main`.

**Done when:**

- `R-S49H-XMZB` — `.git/config` carries the `http.<cloneURL>.extraHeader`
  Basic entry and `remote.origin.url` contains neither the username nor the
  token.
- `R-S5HE-BEQ0` — the clone succeeds with the credential and the server records
  the `Authorization` header; without it the clone fails and no workspace is
  left at the run's sandbox path.
- `R-S6PA-P6GP` — the recorded `RunToken` request asks for a TTL of at least the
  configured run TTL plus five minutes (≥ 35m at the default 30m), and a second
  run mints a second token.
- `R-RZDW-EK0J` — a real `git push` of `ikigenba/run-<run_id>` lands in the bare
  repository's refs, while a push of the same commit to `main` is refused and
  `main` still points at its previous sha.
- `go test ./...` from `prompts/` is green; `gofmt -l .` is empty.
