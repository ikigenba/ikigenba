# Phase 4 — Raise the dev front door's `variables_hash_max_size` to 4096

*Realizes design Decision 1 (dev front door), as amended.*

One edit: the `http`-context directive in `nginx.conf` becomes
`variables_hash_max_size 4096;`, keeping the dev mirror value-identical to the
production line the dashboard owns (its `project/design/D39.md` records why
`2048` proved insufficient on the box's nginx build). Nothing else changes.

**Done when:**

- `grep -c 'variables_hash_max_size 4096;' nginx.conf` prints `1`, and no other
  `variables_hash` directive exists in the file
  (`grep -c 'variables_hash' nginx.conf` prints `1`).
- From the tree root, after populating `locations/` with the sibling fragments
  as `run` does: `mkdir -p tmp && nginx -p . -c nginx.conf -t` exits 0 **and**
  its output contains no `[warn]` line.
- `bash -n run` exits 0 (the tree's green bar; `run` is not edited).
