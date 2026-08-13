# Suite contracts — Design Conventions

Shared facts every Decision leans on:

- **No toolchain of its own.** This project builds, compiles, and tests nothing:
  there is no build command, no test command, and no test-file glob belonging to
  it. Each implementing tree's own design Conventions state those, and a contract's
  ids are proven under *that* tree's gate. A contract whose only artifacts are
  committed prose documents mints no ids at all.
- **The suite-wide gate, for reference.** Every Go tree in the suite is green under
  `go test ./...` and tags requirement ids in `*_test.go` files; `bin/` shell
  tooling is proven through the `bin/bintest` Go module (governed by
  `bin/project/`, not by this workspace) or verified once manually, per that
  tree's spec. The **repo-root aggregate gate** is `suite_test.go` in the
  repo-root module: it enumerates every `go.work` module (skipping its own) and
  runs `go test ./...` in each, so "the repo is green" is one command from the
  root. The testing vocabulary those gates share — the hermetic / composed /
  live / manual layers and what each may touch — is D23's contract. All of this
  is context for reading the markers, not a gate this project runs.
- **Coverage is checked per marker, not by a tree-local grep.** The ordinary
  tree-local coverage grep does not apply here, since this tree holds no tests. For
  each id, check the tree its marker names:

  ```
  # for an id marked [proof: <tree>]
  grep -rl 'R-XXXX-XXXX' --include='*_test.go' <tree>/

  # every marked id at once, listing any with no proof in its named tree
  grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4} \[proof: [a-z-]+\]' project/design/D*.md | sort -u
  ```

  An id marked `[proof: <tree>]` passes when it appears as a tag in that tree's
  test files. An id marked `[proof: per-service]` is checked in **each adopting
  service's** tree instead, and enters that service's own coverage denominator when
  its design cites the id.
- **Contracts are cited, never copied.** An implementing tree's spec references a
  contract by its path here and owns none of it. A local restatement is drift by
  construction.
- **Citations carry the `root` qualifier, always.** Every subproject has its own
  `project/design/DNN.md` files, so the bare path is ambiguous by construction —
  it names a *local* Decision in any subproject context. A citation of a suite
  contract is therefore written in the exact form **`root project/design/DNN.md`**,
  never the bare path and never a prose qualifier ("at the repo root"): the fixed
  form makes every contract citation in the repo findable with one grep
  (`grep -rn 'root project/design/'`) and makes a bare `project/design/DNN.md`
  string unambiguously local.
