# Wiki usage guide

Wiki turns source text into a cited knowledge base. Its subjects have exactly three types: `entity`, `event`, and `concept`.

## Scopes

Wiki content is partitioned into explicit scopes. Every content verb — `ingest`, `ask`, `subjects`, `claims`, `page`, `merge`, `merges`, `jobs`, and `jobs_count` — requires a `scope` argument; there is no assumed scope. Use `scopes` to list them, `scope_create` to create a private scope, `scope_set_visibility` to switch its human-web visibility between `private` and `public`, and `scope_delete` to irreversibly delete a non-default scope and all its content. MCP remains bearer-gated regardless of visibility.

## Subject paths

Tools that identify one subject use a `type/norm_name` path, such as `entity/acme-robotics`, not a display name or internal ID. The first segment is one of the three subject types. The second is the normalized name (slug): lowercase the name, replace punctuation and whitespace runs with `-`, and trim surrounding `-` characters. For example, `Acme Robotics, Inc.` becomes `acme-robotics-inc`.

Use `subjects` to discover canonical paths. Pass a path as `subject` to `claims` or `page`, and pass paths as `from` and `to` to `merge`.

## Jobs

`ingest` and `merge` return a `job_id` immediately while the background pipeline does the work. Poll `status` with that ID.

The lifecycle statuses are:

- `pending`: queued, non-terminal
- `working`: processing, non-terminal
- `done`: completed, terminal
- `failed`: stopped with an error, terminal
- `aborted`: stopped by request, terminal

All five values can filter `jobs` and `jobs_count`. `abort` accepts only pending or working jobs. `rerun` accepts only done, failed, or aborted jobs.

## Claims and pages

A claim is returned as an object with `id`, `text`, and `job`. The text is the extracted factual statement, and job identifies the source-processing job that supports it. A compiled page has `subject`, `title`, and a cited Markdown `body`.

## Pagination

`jobs`, `merges`, `subjects`, and `claims` are cursor-paginated. Send optional `limit` and `cursor` fields. Each response contains its item array and `next_cursor`; pass a non-empty `next_cursor` unchanged into the next call, and stop when it is empty.

## Basic flow: ingest, poll, ask

1. Call `scopes`, or create a workspace with `scope_create {"name":"robotics"}`.
2. Call `ingest` with `{"scope":"robotics","text":"Acme Robotics launched Atlas in 2025.","title":"Launch notes","tags":["robotics"]}` and save the returned `job_id`.
3. Call `status` with `{"job_id":"..."}` until the status is terminal.
4. Call `ask` with `{"scope":"robotics","question":"What did Acme Robotics launch?"}` for a grounded answer and cited wiki page URLs.

## Read compiled knowledge

1. Call `subjects` with `{"scope":"robotics","type":"entity","name":"Acme","limit":20}`.
2. Take the returned path, such as `entity/acme-robotics`, and call `page` with `{"scope":"robotics","subject":"entity/acme-robotics"}`.
3. Call `claims` with the same subject path when you need the underlying extracted statements.

## Merge duplicates

Choose the canonical survivor before merging. To fold `entity/acme-robotics-inc` into `entity/acme-robotics`, call `merge` with `{"scope":"robotics","from":"entity/acme-robotics-inc","to":"entity/acme-robotics"}`. Save the returned job ID and poll `status`. The `from` subject is the folded duplicate; the `to` subject survives. This operation is irreversible.

## Inspect pipeline calls

Inspect inference through the prompts service's `calls` and `usage` tools, grouped by wiki job ID.
