# Phase 51 — `run_delete`

*Realizes design Decision 41 (`run_delete`). Depends on Phase 50.*

**End state.**

`Service.RunDelete(ctx, ownerID, runID)` removes one run: its `calls` rows, then its `runs` row, then `<stateDir>/runs/<run_id>/` — rows before files, so a crash leaves reclaimable bytes rather than dangling rows. Ownership resolves through `runForOwner` on the run's own `owner_id`, so a run whose prompt was deleted is still deletable; a run owned by someone else reports not found. Deleting an absent run is not an error.

An in-flight run is not deleted: `RunDelete` signals cancellation and returns an error saying the run is being cancelled and to retry.

The MCP tool `run_delete(run_id)` exposes it, with no bulk form. The `delete` (prompt) tool's description and `describe`'s prose state that deleting a prompt **does not cascade** to its runs; the word "tombstone" appears nowhere in the service's code, comments, tests, or tool descriptions.

**Done when:**

- `go build ./...` and `go test ./...` are green from `prompts/`, and `gofmt -l .` is silent.
- `grep -rni 'tombstone' --exclude-dir=project .` returns nothing.
- These ids are covered by clearly-named tests:
  - R-ZTUJ-HCV2 — deleting a completed run with `calls` rows removes its row, its `calls` rows, and its directory, while a second run alongside it keeps all three.
  - R-ZV2F-V4LR — a mismatched owner id returns not-found and leaves the row and directory present; the true owner then deletes it successfully.
  - R-ZWAC-8WCG — after deleting a prompt with a completed run, that run's row, `calls` rows, and directory are all present and `run_delete` on it succeeds.
