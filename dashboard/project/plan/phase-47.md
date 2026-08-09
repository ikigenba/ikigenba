# Phase 47 — The apex fragment sets the http-level `variables_hash_max_size`

*Realizes design Decision 39 (front-door tuning).*

Two small artifacts, following the D20/D33 fragment-content pattern exactly:

- `dashboard/etc/nginx.conf` gains `variables_hash_max_size 2048;` at the top
  of the file, above both `server` blocks (D39 states why: the box's `conf.d`
  include places it at `http` level, where it enlarges nginx's variables hash
  past the suite's ~150 `auth_request_set` variables and silences the
  `could not build optimal variables_hash` warning on every reload).
- A file-content test beside the existing fragment guards in
  `cmd/dashboard/main_test.go`, tagged `R-6MX7-JDFX`, that reads
  `etc/nginx.conf` from disk and asserts the directive is present with the
  value `2048` **and** appears before the first `server {` — placement is the
  load-bearing property, since inside a `server` block nginx would reject the
  directive at load time and the gate never runs nginx.

No other file changes; the fragment's server blocks, locations, and headers are
untouched.

**Done when:**

- `R-6MX7-JDFX` is covered by the clearly-named file-content test above,
  asserting presence, value, and before-first-`server {` placement.
- The suite is green per design's Conventions: `cd dashboard &&
  go build ./...`, `go vet ./...`, `gofmt -l .` silent, and `go test ./...`
  all succeed.
