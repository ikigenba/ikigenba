# Stories — fragment

The panel keeps its content fresh without a page reload by re-fetching one
fragment on an interval and swapping it into the page. The fragment is HTML,
not JSON: the server re-renders the same table markup the panel page already
embeds, and the browser swaps the markup in rather than assembling content
itself. `GET /widgets/table` is that endpoint, and what it returns is the
widgets table alone — no doctype, no `html` element, no `body` element, no
banner, no footer. It is a fragment, never a whole HTML document, and a caller that
fetches it with `curl` sees exactly the markup the page splices in. Being a
fragment governs this endpoint's failures as much as its successes: the
panel's script splices whatever comes back into a document that is already
drawn, so an error drawn as a page with the panel's banner would arrive as a
whole page pushed into a table. An error from this endpoint therefore never
carries the banner or the footer either — it is one line of plain text, whether or not the caller is
identified. The page's script re-fetches the fragment every 5 seconds, on the
panel page and on the 422 redraw alike (`S3`, `S5`), which is what the
panel's subtitle, `refreshes every 5 seconds`, tells the reader. The subtitle
is drawn with the page and is not part of the fragment, so a poll never
changes it. The stories below fix the endpoint's HTTP behavior.

Every request reaching dummy comes through the host's nginx gate, which sets
`X-User-Id` and `X-User-Email` on each upstream request, or from a sibling app
that forwards the ones it received (`S2`), so there is no unauthenticated
case; the identity the fragment requires is `X-User-Id`, and a
request without it is answered 500, the same rule the panel page follows. The
requests below therefore carry both headers explicitly.

The widgets are kept in dummy's database, `state/dummy.db` under its working
directory, and every request shares them. A database dummy creates holds no
widgets; every widget created since is kept across restarts and deploys, with
the id it was given. A widget has a `name` (text), a `count` (integer), and a
`status`, which is one of `active`, `paused`, and `retired`, and an id the
table does not show (`S3`). Rows appear in creation order, so a newly created
widget is last. Status is a word in its own
column, never colour alone, so a caller reading the fragment with `curl`
understands a row the same way a person looking at the browser does. The
table carries the same header row and marking as the panel's (`S3`): its
header row holds exactly three cells, in order, `<th>Name</th>`,
`<th class="num">Count</th>`, and `<th>Status</th>`; the count column's
header cell is marked numeric, `<th class="num">Count</th>`, as is each
row's count cell, `<td class="num">3</td>`, and each status word sits inside a
status marker naming its value,
`<span class="status" data-status="active">active</span>`. The fragment and
the table the panel page embeds are the same markup, byte for byte. The
widgets change only when a widget is created, by a POST to `/widgets`
(`S5`) or by the MCP tool `create_widget` (`S9-mcp.md`); nothing in this group
changes them.

Every 200 and 304 carries an `ETag`. Its value is opaque — no story fixes it, and
`"<etag>"` below stands for whatever the server sent. What is fixed is the
relation: the same table content always yields the same value, and different
table content yields a different one. That is what makes a poll cheap at rest
and truthful after a change.

A response block shows the status line and the headers the story fixes; a
header it does not show, `Date` say, is not fixed.

## A page asks for the current table

The first fetch of the fragment, and the one the page's script repeats. The
response is the whole product: the markup, the type it is to be read as, and
the tag the next poll will quote back.

Request:

```
GET /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
ETag: "<etag>"
```

Status 200. The body is the widgets table alone: a table with a header row
and one row per widget, and nothing around it — no doctype, no `html`
element, no `body` element, no banner, no footer. It is a fragment, not a whole HTML
document. It holds three rows, in creation order, carrying `alpha` 3 `active`,
`beta` 0 `paused`, and `gamma` 12 `retired`; each row shows its status as a
word in its own column, inside a status marker naming that status, and its
count in a cell marked numeric; the header row's `Count` cell is marked
numeric too, and the header row's cells read `Name`, `Count`, and `Status`, in
that order. The body is byte for byte the table a `GET
/widgets` embeds for the same widgets.

Preconditions:

- dummy is serving.
- The database holds exactly these widgets, in creation order: `alpha`
  count 3 status `active`, `beta` count 0 status `paused`, `gamma` count 12
  status `retired`.

Postconditions:

- Nothing has changed.

## A page asks for the fragment's headers

A caller that wants to know whether the table has moved without paying for
the markup — a health probe, or a script checking the tag — sends `HEAD`. It
learns the same type and the same `ETag` as a `GET` of the same content, and
gets no body.

Request:

```
HEAD /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
ETag: "<etag>"
```

Status 200. The body is empty. The `ETag` is the one a `GET` of the same
table returns.

Preconditions:

- dummy is serving.
- The database holds the widgets of `A page asks for the current table`.

Postconditions:

- Nothing has changed.

## A page polls again and nothing has changed

This is the ordinary case: the panel sits open, the script re-fetches every 5
seconds, and no one has created anything. The page quotes the `ETag` from the
response it is currently showing in `If-None-Match`; because the table's
content is unchanged, the tag still matches, and the server says so instead of
re-sending markup the page already has. The page leaves the table it is
showing in place.

Request:

```
GET /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "<etag>"
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "<etag>"
```

Status 304. The body is empty. The `ETag` returned is the one the request
quoted, so the page keeps polling with it.

Preconditions:

- dummy is serving.
- `"<etag>"` is the `ETag` from the response the page is currently showing.
- Nothing has been created since that response, so the table's content is
  unchanged.

Postconditions:

- Nothing has changed.

## A page polls after a widget was created

The other half of the polling pair. Someone has POSTed to `/widgets`, so the
table's content differs from what the polling page is showing, and the tag
the page quotes is no longer the current one. The server answers with the
whole fragment, which the page swaps in, and with a new `ETag` for the page to
poll with from now on. The creation happened before this request; this request
only reads.

Request:

```
GET /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
If-None-Match: "<etag>"
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
ETag: "<new-etag>"
```

Status 200. The body is the widgets table alone, on the same terms as an
unconditional fetch: the table and its rows, with no doctype, no `html`
element and no `body` element. It holds four rows — the three widgets of
`A page asks for the current table`, in their order, then a row for `delta` carrying the name, count, and status it
was created with, marked the same way as the others. The `ETag` differs from
the one the request quoted, because the content differs.

Preconditions:

- dummy is serving.
- `"<etag>"` is the `ETag` from the response the page is currently showing.
- When that response was sent, the database held the widgets of
  `A page asks for the current table`.
- A widget named `delta` was created by a POST to `/widgets` after that
  response was sent.

Postconditions:

- Nothing has changed as a result of this request. `delta` existed before it
  arrived, and reading the fragment neither creates, alters, nor removes a
  widget.

## A page asks for the table while dummy cannot reach the widgets

The table is the widgets, so when dummy cannot read them there is no true
table to send, and an empty one would tell the page every widget was gone.
dummy says plainly that it cannot reach them, quoting nothing of the
database's own error, in one line of plain text like every error from this
endpoint, and the page may ask again on its next poll.

Request:

```
GET /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
```

Status 503. The body is exactly the one line
`cannot reach the widgets; try again later`, ending in a newline, as on the
panel (`S3`). No table markup is sent, and no `ETag`. A poll that quotes an
`ETag` in `If-None-Match` is answered the same way. A `HEAD` is answered with
the same status and headers and an empty body.

Preconditions:

- dummy is serving, and telemetry takes every event.
- dummy's database cannot be read: `state/dummy.db` has become unreadable
  since dummy opened it, the filesystem holding it failing, say.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr. Its trail records the request as it records
  every request (`S3`): a `request.started` with the `method` `GET` and the
  `path` `/widgets/table`, and a `request.finished` with the `status` 503,
  both under user `u_7f3a9c21`.
- dummy is still serving.

## A request for the fragment arrives without the identity headers

The nginx gate sets `X-User-Id` on every upstream request, and a sibling app
forwards the one it received, so a request without it came from neither as
it should and dummy cannot say who is asking.
That is a fault on dummy's side of the boundary, not a request the caller can
correct, so it is answered 500 rather than 400 or 401 — the same rule the
panel page follows.

Request:

```
GET /widgets/table HTTP/1.1
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the one line `identity header missing`,
ending in a newline, as on every route (`S3`). No table markup is sent.

Preconditions:

- dummy is serving.
- The request carries no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- dummy wrote nothing to stderr about the 500. Its trail records the
  request as it records every request (`S3`): a `request.started` with the
  `method` `GET` and the `path` `/widgets/table`, and a `request.finished` with
  the `status` 500, both with an empty user, under the id dummy gave the
  request (`S2`).

## A caller sends the fragment a method it does not take

The fragment is read-only: it renders the table and never changes it.
Creating a widget is a POST to `/widgets`, a different route with its own
story, so a POST here is refused rather than redirected. The `Allow` header
names the two methods this route takes, and the refusal, being an error from a
fragment endpoint, is plain text rather than a page with the banner.

Request:

```
POST /widgets/table HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
Content-Type: text/plain; charset=utf-8
```

Status 405. The body is one line of plain text saying the method is not
allowed. `PUT`, `DELETE`, and `PATCH` are refused the same way.

Preconditions:

- dummy is serving.

Postconditions:

- Nothing has changed. No widget was created.
