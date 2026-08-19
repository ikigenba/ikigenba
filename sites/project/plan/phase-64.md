# Phase 64 — Interactive write refuses a wrong-typed path

*Realizes design Decision 33 (the write path).*

The `file_write` and `file_edit` handlers refuse, with a `validation` error
raised **before any plane call**, a write whose confined path resolves onto an
existing **directory** in the served copy, or whose path descends through an
existing **regular file** as if it were a directory. No commit is issued and the
served copy is untouched: the interactive single-write tools never silently
destroy a subtree. Replacing a directory with a file is the explicit `rmdir`
(Phase 65) then `file_write` sequence; this phase only adds the refusal.

**Done when:**

- R-FNMC-959X — on a site whose copy holds `qr/index.html`, `file_write("qr")`
  and `file_edit("qr")` each return the `validation` envelope with **zero**
  recorded repos requests and `qr/` byte-identical to before; and a
  `file_write("a/b.css")` where `a` is a regular file returns `validation` with
  zero repos calls and `a` unchanged.
- `cd sites && go build ./... && go vet ./... && gofmt -l . && go test ./...` is
  green.
