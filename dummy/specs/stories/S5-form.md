# Stories — form

Creating a widget: the one interaction in dummy that changes state. The form
sits on the panel page at `/widgets`, below the table, and it is an ordinary
HTML form — it POSTs to `/widgets` with
`application/x-www-form-urlencoded`, and nothing about the submission is
assembled by JavaScript, so a caller with `curl` submits exactly what a
browser submits. A widget has three fields, and the form has one field for
each: `name`, text, required, 1 to 40 characters and unique across widgets;
`count`, an integer, required, zero or more; and `status`, one of exactly
`active`, `paused`, or `retired`. The widgets are an in-memory fixture set,
reset at process start, holding exactly three widgets in this order: `alpha`
count 3 status `active`; `beta` count 0 status `paused`; `gamma` count 12
status `retired`. Rows appear in creation order, so a newly created widget is
last in the table (`S4`).

A submission dummy accepts is answered `303 See Other` with `Location:
/widgets` and an empty body: the browser then re-fetches the panel, where the
new row is visible. A submission dummy reads and rejects is answered `422`
whose body is the panel page re-rendered in the same chrome as a `GET
/widgets` — the table exactly as it was, and the form still carrying the
values the caller submitted, with an error message beside each field that was
rejected. A submission dummy does not read at all, because its media type
is not `application/x-www-form-urlencoded`, is answered `415` instead, and
that answer carries no form and no field errors, there being no submitted
values to carry. Nothing is created when a submission is rejected either
way: the fixture set is left as it was, down to its order.

An nginx gate in front of dummy sets `X-User-Id` and `X-User-Email` on every
upstream request, and a sibling app forwards the ones it received (`S2`), so
there is no unauthenticated case and a request without
`X-User-Id` is answered 500, the same rule the panel page and the fragment
follow. The curl lines below therefore carry both headers explicitly, and only
the missing-header story carries neither.

A response block shows the status line and the headers the story fixes; a
header it does not show, `Date` say, is not fixed.

## A user adds a widget

The ordinary case, and the one the form exists for. The answer is a redirect
rather than a page because the browser must not be left showing the result of
a POST: after a `303` the browser fetches `/widgets` with a `GET`, so the
address the user ends on is the panel, and refreshing it re-reads the panel
instead of submitting the form a second time. That is the whole reason the
success answer carries no body.

There is no flash message and no confirmation banner. The redirect lands on
the panel, where the new row is in the table, and the row is the
confirmation; dummy sets no cookie and puts nothing in the URL to carry a
message across the redirect.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=delta' -d 'count=7' -d 'status=active' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 303 See Other
Location: /widgets
```

Status 303. The body is empty.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it, so no widget is
  named `delta`.

Postconditions:

- A widget named `delta`, count 7, status `active`, now exists.
- The fixture set holds four widgets. `delta` is last, after `gamma`, because
  rows appear in creation order.

## A user submits the form with no name

A name is required, so an empty one is a rejection and not a widget with a
blank name. The caller is identified, so dummy answers in the chrome with the
panel they were on rather than with bare text.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=' -d 'count=7' -d 'status=active' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets` — the service name, `mg@example.com`, and the sign-out link — with
the table holding the three fixture widgets in fixture order. The form carries
the values the caller submitted: the name field empty, the count field 7, the
status field `active`. An error message sits beside the name field saying a
name is required. No error sits beside the count or the status field.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A user submits a name that is already taken

Names are unique across widgets, so `alpha` can be created once and not
twice. This is the only rejection that depends on stored state rather than on
the submitted field alone: the value is a perfectly good name, 1 to 40
characters of text, and what makes it wrong is that a widget already carries
it. Reading the field cannot decide this; only asking the widgets can.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=alpha' -d 'count=5' -d 'status=paused' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets`, with the table holding the three fixture widgets in fixture order.
The form carries the values the caller submitted: the name field `alpha`, the
count field 5, the status field `paused`. An error message sits beside the
name field saying that name is already taken.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it, so a widget named
  `alpha` exists.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with. In
  particular the existing `alpha` still has count 3 and status `active`; a
  duplicate name is refused, never merged into the widget that holds it.

## A user submits a name longer than 40 characters

40 characters is the limit, so 41 is a rejection. The name below is 41
characters.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=a-widget-name-that-is-far-too-long-to-fit' -d 'count=7' -d 'status=active' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets`, with the table holding the three fixture widgets in fixture order.
The form carries the values the caller submitted: the name field holding the
41-character name in full, unshortened, the count field 7, the status field
`active`. An error message sits beside the name field saying the name is too
long.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A user submits a count that is not a number

The count is an integer, and text that is not one is a rejection rather than
a count of zero. A widget is never created with a count the caller did not
give.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=delta' -d 'count=three' -d 'status=active' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets`, with the table holding the three fixture widgets in fixture order.
The form carries the values the caller submitted: the name field `delta`, the
count field holding the text `three` as it was typed, the status field
`active`. An error message sits beside the count field saying the count must
be a whole number.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A user submits a negative count

Zero is allowed — `beta` has a count of 0 — and anything below it is not. A
negative count is a whole number, so it passes the test the previous story
fails and is caught by the range instead; the two are separate rejections with
their own messages.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=delta' -d 'count=-1' -d 'status=active' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets`, with the table holding the three fixture widgets in fixture order.
The form carries the values the caller submitted: the name field `delta`, the
count field -1, the status field `active`. An error message sits beside the
count field saying the count cannot be negative.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A caller submits a status that is not one of the three

A person using the browser cannot reach this rejection: the status field is a
`<select>` offering `active`, `paused`, and `retired`, and a browser submits
one of them or nothing. A caller with `curl` sends whatever they like, which
is why dummy checks the value against the three words rather than trusting
that the form was the thing that produced it. Every field is validated on the
same terms, for the same reason.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=delta' -d 'count=7' -d 'status=archived' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets`, with the table holding the three fixture widgets in fixture order.
The form carries the values the caller submitted: the name field `delta`, the
count field 7, and the status field, which offers the same three choices and
has none of them selected, because `archived` is not one of them. An error
message sits beside the status field saying the status must be one of
`active`, `paused`, or `retired`.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A user submits several bad fields at once

Every field is checked, and every field that is wrong is reported, so a
caller who got three things wrong learns all three from one answer instead of
discovering them one submission at a time. This is what makes the errors
per-field rather than a single banner at the top of the form: a reader sees
which field each message is about by where it sits.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -d 'name=' -d 'count=three' -d 'status=archived' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 422 Unprocessable Content
Content-Type: text/html; charset=utf-8
```

Status 422. The body is the panel page in the same chrome as a `GET
/widgets`, with the table holding the three fixture widgets in fixture order.
The form carries the values the caller submitted: the name field empty, the
count field holding the text `three`, and the status field with none of the
three choices selected. Three error messages appear, one beside the name
field, one beside the count field, and one beside the status field, each
saying what is wrong with that field.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with.

## A caller posts a body that is not form-encoded

dummy reads form submissions and nothing else. The form posts
`application/x-www-form-urlencoded`, and that is the only media type `POST
/widgets` accepts; a body sent as anything else — JSON, here — is declined on
its media type, and nothing inside it is read. The refusal is about the format
of the content rather than the values in it, so the answer is `415` and not
the `422` a submission dummy did read and found unacceptable gets: a `422`
would tell this caller their values were wrong and invite them to send better
ones, when the values are not what dummy is objecting to and no retry that
keeps the media type can succeed. There is no JSON answer either. This story
fixes that dummy does not acquire a second, JSON way in through the same
route.

Request:

```
$ curl -si -X POST -H 'X-User-Id: u_7f3a9c21' -H 'X-User-Email: mg@example.com' -H 'Content-Type: application/json' -d '{"name":"delta","count":7,"status":"active"}' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 415 Unsupported Media Type
Content-Type: text/html; charset=utf-8
```

Status 415. The body is an HTML document in the same chrome as the panel —
the service name, `mg@example.com`, and the sign-out link — whose visible text
says the media type is not supported and carries a link to `/widgets`. The
caller is identified, so this failure is a page in that chrome, as the 404 and
the 405 are (`S3`); only the missing-header 500 is bare text. The request body
is not read at all: `POST /widgets` accepts only
`application/x-www-form-urlencoded`, and the names and values inside the JSON
body are neither parsed nor reported. No form and no field errors come back,
there being no submitted values to show.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The widgets are the fixture set as the process started it.

Postconditions:

- No widget was created. The fixture set is unchanged: `alpha`, `beta`, and
  `gamma`, in that order, with the counts and statuses they started with. In
  particular no widget named `delta` exists, though the JSON body named one.

## A request to create a widget arrives without the identity headers

The gate sets `X-User-Id` on every request it forwards, a sibling app
forwards the one it received, and nothing but nginx and the suite's apps can
reach dummy's socket, so a request without it says the gate or a sibling is
misconfigured. That is dummy's fault to report rather
than the caller's to correct, so it is a 500 and not a 400 or a 401. There is
no identity to draw the chrome from, so the answer is bare text rather than a
page, as it is on every route. The identity check runs before dummy looks at
the submitted fields, so a body that would have been rejected and a body that
would have been accepted are answered the same way, and neither is examined. A
developer meets this by forgetting the headers, as here.

Request:

```
$ curl -si -X POST -d 'name=delta' -d 'count=7' -d 'status=active' http://127.0.0.1:3000/widgets
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is one line of plain text saying the identity header is
missing.

Preconditions:

- dummy is serving on `127.0.0.1:3000`.
- The request carries no `X-User-Id` header.
- The widgets are the fixture set as the process started it.

Postconditions:

- Nothing has changed. No widget was created, and the fixture set is
  unchanged.
- dummy wrote one line to stderr, `dummy: request -: X-User-Id is missing`,
  as it does for every request it answers with a 500 (`S3`).
