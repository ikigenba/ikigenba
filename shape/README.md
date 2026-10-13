# shape

This directory is where an agent building the next app finds the suite's conventions: the shapes every service shares, so that one learned is all learned. Each entry gives the rule, the wire shape once, the shared code with the appkit design page that is its contract, and the precedent app; it cites contracts and never restates a requirement. A sub-project design that departs from an entry records the departure and why.

## Listing

**Rule.** A tool or page that lists data an agent or a person can add pages it, with one shape. A list that configuration bounds (providers, services, subscribers, a thread's labels) is listed whole.

**Wire shape.** The input takes an optional `page`, an opaque string, and an optional `limit`, an integer from 1 to 500, 25 when absent. The answer is the collection member (`widgets`, `runs`, ...) then `next`, an opaque string present exactly when at least one entry follows. An absent or empty `page` is the first page, and `next` is handed back as `page` unchanged. A `page` the tool did not issue, or a `limit` out of range, is refused before the store is read, each with its shared sentence. The token is a keyset position, the sort key of the last entry, never an offset. The members are declared on each tool's own input struct with the shared descriptions; nothing is embedded. `cursor` and page numbers are not used. A page that lists navigates the same window: it takes the tool's `page` token as `?page=<token>` on its own URL (25 entries, no `limit`), answers a token it cannot decode, one issued for a different list included, with 404 and the app's existing not-found page (a URL carrying a token the page never issued names no page; the tools' own refusal sentence stays the agents' interface), and below the list draws a `nav.paging` with First, always, and Next, when more follow; `design/README.md` has the markup and the two-list form.

**Shared code.** `appkit/paging`: `Token`, `Window`, `DefaultLimit`, `MaxLimit`, `BadPage`, `BadLimit`, `PageDescription`, `LimitDescription`, `NextDescription`. Contract: `appkit/specs/design/D25-paging.md`.

**Precedent.** `dummy`: `list_widgets` and the widgets page. Provider-backed lists (`gdrive`, `gmail` search) pass the provider's own token through `page` and `next` and map `limit` onto the provider's page size; their designs say so.

## Storage

**Rule.** A big list is filtered, ordered and windowed by the database: the query carries `WHERE`, `ORDER BY` and `LIMIT`, an index backs the order, and the service never holds more than one page plus one row.

**Wire shape.** None of its own; this is how Listing is served. The keyset condition compares a multi-column key as a row value, such as `(started, id) < (?, ?)`. The usual keys: catalogs by name or slug within the owner; runs and transfers by `started` descending then `id` descending; artifacts by path; log records by sequence, or by time then id.

**Shared code.** `appkit/db` for the open path and `appkit/paging` for the token. Contract: `appkit/specs/design/D25-paging.md`, and the app's own design, which names each list's key.

**Precedent.** `dummy`: a SQL window keyed by `seq`.

Later entries, each its own discussion and not written yet: retention by count and age; per-owner against space-wide lists; refusal words.
