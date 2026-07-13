# Dropbox filesystem API

This is the loopback filesystem API for services that share the Dropbox-backed
mirror. It documents filesystem interaction only; `/feed`, `/health`, `/mcp`,
the protected-resource metadata endpoint, and the landing page are not part of
this API.

All paths are absolute, slash-separated paths in the shared mirror namespace.
The namespace is shared and is not partitioned by service. Every mutation path
is resolved through `Mirror.resolve`: a path that escapes the mirror root (for
example, `../outside`) is rejected as a validation error and writes nothing.

For mutations, `X-Client-Id` identifies the origin. Nginx injects it for
external callers; a loopback caller sets it itself. The value is carried to an
emitted event's `origin`; changes pulled from Dropbox use `"dropbox"` instead.

## Read a file

### `GET /content`

Query parameters:

- `path` (required): file path.
- `rev` (optional): require this revision before serving bytes.

Returns the file as a streaming HTTP response, including normal range,
content-type, and conditional-request behavior. With no `rev`, it returns the
current bytes. A stale `rev` is a `conflict` (HTTP 409); an absent, unservable,
or confined path is `not_found` (HTTP 404).

## Write a file

### `PUT /content`

Query parameters:

- `path` (required): destination file path.

The request body is streamed to the local mirror. On success it returns HTTP
200 JSON: `{ "path": string, "size": number, "content_hash": string,
"rev": string }`. The bytes are durable in the mirror and the index/event
transaction is committed before the response; uploading those bytes to Dropbox
is queued and happens asynchronously, so the caller never waits for the
network.

An invalid, missing, escaping, or incompatible path is `validation` (HTTP 400).
The plain-text response body carries the underlying domain error text rather
than a fixed placeholder.

## Delete a path

### `DELETE /content`

Query parameters:

- `path` (required): file or directory path to remove.

Removes a file or directory recursively and returns HTTP 204. It is idempotent:
deleting an absent path is still successful. A successful local deletion commits
before the asynchronous Dropbox deletion is queued. Invalid or escaping paths
are `validation` (HTTP 400); the absent-path case is the idempotent success
described above, not `not_found`.

## Create a directory

### `POST /mkdir`

Query parameters:

- `path` (required): directory path.

Creates an empty directory and returns HTTP 204. The local mirror and index are
committed before the asynchronous Dropbox directory-create operation is queued.
Invalid or escaping paths are `validation` (HTTP 400).

## Move a path

### `POST /move`

Query parameters:

- `from` (required): current file or directory path.
- `to` (required): destination path.

Renames or moves a file or directory and returns HTTP 204. The local move and
index transaction commit before a single asynchronous Dropbox move upload is
queued; it does not re-upload the bytes. A cross-path move emits
`file.deleted(from)` and `file.created(to)` so path-keyed consumers see both
states. Invalid, missing, or escaping source/destination paths are
`validation` (HTTP 400), except that an absent `from` path is `not_found`
(HTTP 404). This distinguishes a stale source from an invalid request.

## List entries

### `GET /list`

Query parameters:

- `path` (optional): path prefix; empty or `/` lists the whole mirror.
- `cursor` (optional): opaque path cursor from `next_cursor`.
- `limit` (optional): page size, defaulting to 1000 and clamped to 1000.

Returns HTTP 200 JSON with `files`, an array of
`{path, kind, size, hash, rev, updated_at}`, and `next_cursor` only when the
page is full. The listing includes both files and directories. Internal lookup
failures return HTTP 500.

## Stat an entry

### `GET /stat`

Query parameters:

- `path` (required): file or directory path.

Returns HTTP 200 JSON metadata for one entry: `path`, `kind`, `size`, `rev`,
`content_hash`, and `updated_at` (directory-only fields may be empty). A missing
entry is `not_found` (HTTP 404); an internal lookup failure is HTTP 500.

## Errors and delivery contract

The service vocabulary is `not_found`, `conflict`, `validation`, and
`too_large`, conventionally represented by `{ "error": { "code": string,
"message": string } }` in structured callers. The loopback HTTP handlers above
use their documented HTTP status and plain error responses for their current
route-specific mappings. Mutation `ErrValidation` and `ErrPathEscape` failures
return HTTP 400; mutation `ErrNotFound` failures return HTTP 404. Every mutation
4xx response is a single plain-text line containing the underlying domain error
text, rather than a fixed placeholder. `DELETE /content` remains idempotent, so
deleting an absent path returns success rather than HTTP 404. `too_large` applies
where a caller or boundary rejects an oversized payload.

Each successful mutation is local-commit-then-async-push: mirror bytes and the
database transaction are committed before returning success, while Dropbox
network synchronization is performed later. This makes filesystem operations
near-native and avoids coupling their latency to Dropbox availability.
