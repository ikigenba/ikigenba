# Phase 48 — Raise the apex fragment's `variables_hash_max_size` to 4096

*Realizes design Decision 39 (front-door tuning), as amended.*

Phase 47 landed the directive at `2048`; live deployment then proved that value
insufficient on the box's `nginx-core` build (its compiled-in modules
contribute more built-in variables than a dev-machine nginx, and the box's
`nginx -t` still warned). D39 now records the box-verified value `4096`. Two
edits, both to files phase 47 already shaped:

- `dashboard/etc/nginx.conf`: the top-of-file directive becomes
  `variables_hash_max_size 4096;`. Nothing else in the fragment changes.
- The `R-6MX7-JDFX` file-content test in `cmd/dashboard/main_test.go` asserts
  the new value `4096` (presence, value, and before-first-`server {` placement,
  exactly as before).

**Done when:**

- `R-6MX7-JDFX` is covered by the file-content test asserting
  `variables_hash_max_size 4096;` before the first `server {` in
  `etc/nginx.conf`, and `grep -c 'variables_hash_max_size' etc/nginx.conf`
  prints `1`.
- The suite is green per design's Conventions: `cd dashboard &&
  go build ./...`, `go vet ./...`, `gofmt -l .` silent, and `go test ./...`
  all succeed.
