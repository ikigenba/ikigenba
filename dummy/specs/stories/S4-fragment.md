# Stories — fragment

The panel keeps its content fresh without a page reload by re-fetching one
fragment on an interval and swapping it into the page. The fragment is HTML,
not JSON: the server re-renders the same table markup the panel page already
embeds, and the browser swaps the markup in rather than assembling content
itself. `GET /widgets/table` is that endpoint, and what it returns is the
widgets table alone — no doctype, no `html` element, no `body` element, no
page chrome. It is a fragment, never a whole HTML document, and a caller that
fetches it with `curl` sees exactly the markup the page splices in. Being a
fragment governs this endpoint's failures as much as its successes: the
panel's script splices whatever comes back into a document that is already
drawn, so an error wrapped in the panel's chrome would arrive as a whole page
pushed into a table. An error from this endpoint is therefore never drawn in
the chrome either — it is one line of plain text, whether or not the caller is
identified. The polling interval belongs to the page's script and is not
fixed by any story here; what these stories fix is the endpoint's HTTP
behavior.

Every request reaching dummy comes through the host's nginx gate, which sets
`X-User-Id` and `X-User-Email` on each upstream request, or from a sibling app
that forwards the ones it received (`S2`), so there is no unauthenticated
case; the identity the fragment requires is `X-User-Id`, and a
request without it is answered 500, the same rule the panel page follows. The
curl lines below therefore carry both headers explicitly.

The widgets are an in-memory fixture set, reset at process start, holding
exactly three widgets in this order: `alpha` count 3 status `active`; `beta`
count 0 status `paused`; `gamma` count 12 status `retired`. A widget has a
`name` (text), a `count` (integer), and a `status`, which is one of `active`,
`paused`, and `retired`. Rows appear in creation order, so the fixture widgets
come first and a newly created widget is last. Status is a word in its own
column, never colour alone, so a caller reading the fragment with `curl`
understands a row the same way a person looking at the browser does. The
fixture set changes only when someone POSTs to `/widgets`; nothing in this
group changes it.

Every response carries an `ETag`. Its value is opaque — no story fixes it, and
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
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets/table
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
ETag: "<etag>"
```

Status 200. The body is the widgets table alone: a table with a header row
and one row per widget, and nothing around it — no doctype, no `html`
element, no `body` element, no chrome. It is a fragment, not a whole HTML
document. It holds three rows, in fixture order, carrying `alpha` 3 `active`,
`beta` 0 `paused`, and `gamma` 12 `retired`; each row shows its status as a
word in its own column.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- No widget has been created since dummy started, so the fixture set holds
  the three widgets it holds at process start.

Postconditions:

- Nothing has changed.

## A page asks for the fragment's headers

A caller that wants to know whether the table has moved without paying for
the markup — a health probe, or a script checking the tag — sends `HEAD`. It
learns the same type and the same `ETag` as a `GET` of the same content, and
gets no body.

Request:

```
$ curl -sI -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets/table
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

- dummy is serving on `127.0.0.1:3000`.
- No widget has been created since dummy started.

Postconditions:

- Nothing has changed.

## A page polls again and nothing has changed

This is the ordinary case: the panel sits open, the script re-fetches on its
interval, and no one has created anything. The page quotes the `ETag` from the
response it is currently showing in `If-None-Match`; because the table's
content is unchanged, the tag still matches, and the server says so instead of
re-sending markup the page already has. The page leaves the table it is
showing in place.

Request:

```
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'If-None-Match: "<etag>"' http://127.0.0.1:3000/widgets/table
```

Response:

```
HTTP/1.1 304 Not Modified
ETag: "<etag>"
```

Status 304. The body is empty. The `ETag` returned is the one the request
quoted, so the page keeps polling with it.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
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
$ curl -si -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'If-None-Match: "<etag>"' http://127.0.0.1:3000/widgets/table
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
ETag: "<new-etag>"
```

Status 200. The body is the widgets table alone, on the same terms as an
unconditional fetch: the table and its rows, with no doctype, no `html`
element and no `body` element. It holds four rows — the three fixture widgets,
in their order, then a row for `delta` carrying the name, count, and status it
was created with. The `ETag` differs from the one the request quoted, because
the content differs.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- `"<etag>"` is the `ETag` from the response the page is currently showing.
- A widget named `delta` was created by a POST to `/widgets` after that
  response was sent.

Postconditions:

- Nothing has changed as a result of this request. `delta` existed before it
  arrived, and reading the fragment neither creates, alters, nor removes a
  widget.

## A request for the fragment arrives without the identity headers

The nginx gate sets `X-User-Id` on every upstream request, and a sibling app
forwards the one it received, so a request without it came from neither as
it should and dummy cannot say who is asking.
That is a fault on dummy's side of the boundary, not a request the caller can
correct, so it is answered 500 rather than 400 or 401 — the same rule the
panel page follows.

Request:

```
$ curl -si http://127.0.0.1:3000/widgets/table
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is one line of plain text saying the identity header is
missing. No table markup and no `ETag` are sent.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The request carries no `X-User-Id` header.

Postconditions:

- Nothing has changed.
- dummy wrote one line to stderr, `dummy: request -: X-User-Id is missing`,
  as it does for every request it answers with a 500 (`S3`).

## A caller sends the fragment a method it does not take

The fragment is read-only: it renders the table and never changes it.
Creating a widget is a POST to `/widgets`, a different route with its own
story, so a POST here is refused rather than redirected. The `Allow` header
names the two methods this route takes, and the refusal, being an error from a
fragment endpoint, is plain text rather than a page in the chrome.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' http://127.0.0.1:3000/widgets/table
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

- dummy is serving on `127.0.0.1:3000`.

Postconditions:

- Nothing has changed. No widget was created.
