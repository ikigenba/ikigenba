# Phase 29 — Custody: bare repositories, the `Git` seam, the ref choke point, and the events registry

*Realizes design Decision 17 (custody) and the registry/archive slice of
Decision 23 (R-JDHK-5ND7, R-JH59-AYLA). Depends on Phase 28.*

`internal/repos/` gains the `Git` seam over the real `git` binary
(`REPOS_GIT_BIN`, scrubbed environment, streaming form), `Custody` over the
resolved state root (`Init`, `Path`, `Exists`, `Rename`, `Archive`,
`EnsureHooks`, `Refs`), the committed `pre-receive` hook body, and
`Service.ApplyRefUpdate` — the single in-process ref choke point that refuses a
rewrite or deletion of `main`, moves the ref with a compare-and-swap, and
appends the `push` outbox row. The `Events` registry (`push`, `archived`) and
`Service.Archive` land here too, so archival publishes.

There is no `Git` fake: every test drives the real binary against bare
repositories under `t.TempDir()`.

**Done when:** the suite is green (`go build`, `go vet`, `go test` exit 0;
`gofmt -l .` empty) and these ids are each covered by a clearly-named test —

- R-J4Y9-H96C — `Init` produces a real bare repository whose `HEAD` is
  `refs/heads/main` before any commit, with zero refs.
- R-J665-V0X1 — invalid kinds and names are `ErrValidation` and create nothing
  anywhere under the state root.
- R-J7E2-8SNQ — `Rename` preserves history (verified by cloning the new path);
  conflict and not-found cases move nothing.
- R-J8LY-MKEF — `Archive` moves the repository, keeps every commit clonable from
  the archived path, and permits re-creating and re-archiving the same key.
- R-J9TV-0C54 — `Init` leaves an executable `pre-receive`; `EnsureHooks`
  restores a deleted one and the boot sweep repairs every live repository.
- R-JB1R-E3VT — `ApplyRefUpdate` refuses a `main` delete and a `main` rewrite
  with `ErrForcePush`, allows the same rewrite on another ref, and allows a
  `main` fast-forward.
- R-JC9N-RVMI — an accepted update appends exactly one `push` outbox row; a
  rejected one appends none and never reaches `git update-ref`.
- R-JDHK-5ND7 — the `Events` registry declares exactly `push` and `archived`,
  and `reflection` advertises both and no subscriptions.
- R-JH59-AYLA — archiving publishes exactly one `archived` event carrying the
  stored `archived_path`, with no accompanying `push`.
