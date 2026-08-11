# Phase 11 — Client-facing URLs compose from the configured base, never the request

*Realizes design Decision 3 (URL base: the R-78BZ-IXSS, R-79JV-WPJH,
R-7BZO-O90V slice) and Decision 6 (record URLs: the R-7D7L-20RK slice).*

The service stops deriving client-facing URLs from the incoming request and
composes every one — the minted upload link, the upload ingress's `201`
`url`, and every MCP record `url` — from the single configured
`Service.BaseURL` (absolute front-door base including mount and trailing
slash). The composition root (`cmd/artifacts`) sets it from the chassis:
`strings.TrimSuffix(rt.ResourceID(), "mcp")`. `NewService` requires the base
and panics on an empty one. The former `UploadOrigin` field, its
`https://localhost` default, and the request-`Host`/`X-Forwarded-Proto`
origin path in `downloadURL` are deleted; one shared
`Service.DownloadURL(id, filename, visibility)` renders the tier-correct
download URL for both the ingress response and the MCP records.
`content_url` (the content-plane reference) is untouched and stays on
artifacts' own loopback port.

**Done when:**

- R-78BZ-IXSS — with `BaseURL` = `https://example.test/srv/artifacts/`,
  `MintUpload` renders exactly
  `https://example.test/srv/artifacts/u/<token>` and the `curl` command
  embeds that URL — covered by a tagged test.
- R-79JV-WPJH — a successful `PUT /u/<token>` with `Host: 127.0.0.1:3009`
  (with and without `X-Forwarded-Proto`) returns a `201` whose `url` is on
  the configured base, tier-correct — covered by a tagged test.
- R-7BZO-O90V — `NewService` panics on an empty base; a non-empty base
  prefixes every rendered URL — covered by a tagged test.
- R-7D7L-20RK — through the real chassis MCP layer, every record-returning
  tool renders `url` (and `upload`'s `upload_url`) on the configured base,
  no `localhost`/loopback host in any returned URL, while `content_url`
  stays on artifacts' own port — covered by a tagged test.
- `grep -rn 'UploadOrigin' --include='*.go' .` from `artifacts/` exits
  non-zero (the field is gone from all Go source, tests included).
- The suite is green per design Conventions (`go test ./...`, clean
  `go build ./...`, `go vet ./...`, silent `gofmt -l .` from `artifacts/`).
