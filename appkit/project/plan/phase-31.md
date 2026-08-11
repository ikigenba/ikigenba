# Phase 31 — Pin the `.ico` content type in `appkit/web`

*Realizes design Decision 6 (the `appkit/web` package), the `R-S0TG-Q78J` slice only.*

`appkit/web`'s `init()` registers `.ico` alongside the `.woff2` registration it
already carries, so `Site.Static()` serves a `favicon.ico` as `image/x-icon`
from the registered extension rather than from the box's MIME database or from
content sniffing. This is the substrate the suite's brand icon contract
(`root project/design/D29.md`) relies on for the thirteen services whose static
tree is served through this package.

The observable end state: a request for `/static/favicon.ico` against a `Site`
loaded over any root gets `Content-Type: image/x-icon`, and the answer does not
change when the file's bytes are not ICO magic and does not change on a host
whose `/etc/mime.types` disagrees.

No new package, no new exported surface, no signature change: one added line in
the existing `init()`, and one added test.

**Done when:**

- `R-S0TG-Q78J` is covered by a test tagged with the id verbatim in
  `appkit/web/web_test.go`: it writes a file named `favicon.ico` into a
  `t.TempDir()` root's `static/` whose bytes are deliberately **not** ICO magic
  (so `http.DetectContentType` would classify them otherwise), serves
  `GET /static/favicon.ico` through `Site.Static()`, and asserts `200` with
  `Content-Type` exactly `image/x-icon`. Deleting the `init()` registration must
  make this test fail.
- The suite is green as design's Conventions define it: from `appkit/`,
  `go build ./...`, `go vet ./...`, `gofmt -l .` printing nothing, and
  `go test ./...` all succeed with zero failures.
