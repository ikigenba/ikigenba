# Stories — pages

What a visitor gets from a running dummy. There is one page, the index, and
it answers `GET` and `HEAD`. Every other path is not found and every other
method is not allowed. A response block shows the status line and the headers
the story fixes; a header it does not show, `Date` say, is not fixed. No
request here changes anything.

## A visitor asks for the index

Request:

```
$ curl -si http://127.0.0.1:3000/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is an HTML page whose visible text is `Hello from
Dummy!`.

Preconditions:

- dummy is running with `PORT=3000`.

Postconditions:

- Nothing has changed.

## A visitor asks for the index's headers

Request:

```
$ curl -sI http://127.0.0.1:3000/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is empty.

Preconditions:

- dummy is running with `PORT=3000`.

Postconditions:

- Nothing has changed.

## A visitor asks for a page that does not exist

Request:

```
$ curl -si http://127.0.0.1:3000/nope
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is one line of plain text saying the page was not found.

Preconditions:

- dummy is running with `PORT=3000`.

Postconditions:

- Nothing has changed.

## A visitor sends the index a method it does not take

Request:

```
$ curl -si -X POST http://127.0.0.1:3000/
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

- dummy is running with `PORT=3000`.

Postconditions:

- Nothing has changed.
