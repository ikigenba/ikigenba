# D04-pages

What a visitor gets from a running dummy. There is one page, the index, and
`internal/server` owns it: the package exports `Handler`, the one
`http.Handler` the process serves, and `internal/cli` hands it to `Serve`
(`D01-layout-and-run-seam`, `D03-serve`). This design fixes what that handler
answers; it says nothing about the listener, the port, nginx, TLS, or the
host's name, because none of those reach the handler. The space story
(`S5-on-a-space`) sees exactly the index response the pages story fixes, so it
is covered here and proven by a live deploy, not by a gate.

A request is decided by two things, its method and its path, where "path"
means the request URL's path component and nothing else: not the query, not
the host, not any header. The path decides first. `/` is the only page; every
other path is not found whatever the method, so `POST /nope` is a 404 and not
a 405. On `/`, `GET` returns the page and `HEAD` returns its headers with an
empty body; every other method is refused with a 405 that names the two
methods the page takes.

The stories fix the visible text of the page and the fact that the error
bodies are one line of plain text; they do not fix the bytes. The design fixes
the visible text as a constant, `IndexText`, so the handler and its tests share
it, and fixes the two error lines as constants, `NotFoundBody` and
`MethodNotAllowedBody`, so they can be checked byte for byte. The markup
around the visible text stays out of the contract: a requirement defines
"visible text" as a procedure the standard library can run, the text of the
document's `body` element (its tags matched case-insensitively, as the
doctype is) with the tags removed and the whitespace normalised, and asks
that it equal `IndexText`. Any document that passes that
check is the page. Headers the stories do not show, `Date` and
`Content-Length` among them, are not fixed and no requirement names them.

The handler has nothing to remember. The same method and path always get the
same response, and a test checks that with two identical requests, comparing
the status, the headers the handler sets, and the body; headers the server
adds on its own, `Date` say, are not compared, because they are not the
handler's. "Holds no state" is not something a finite test can decide, so the
design states it structurally instead: `internal/server` declares no
package-level `var`. Nothing in `D03-serve` or here needs one, a `go/ast`
walk over the package's non-test files decides it, and a counter, a cache, or
a session cannot grow under a package that has nowhere to keep it.

## REQUIREMENTS

- R-YDK2-FEXT: The `internal/server` package MUST export `func Handler() http.Handler`.
- R-YERY-T6OI: The `internal/server` package MUST export `const IndexText = "Hello from Dummy!"`.
- R-YFZV-6YF7: The `internal/server` package MUST export `const NotFoundBody = "not found\n"` and `const MethodNotAllowedBody = "method not allowed\n"`.
- R-YH7R-KQ5W: The handler `Handler` returns MUST answer a `GET` request whose path is `/` with status 200 and the header `Content-Type: text/html; charset=utf-8`.
- R-TVL2-8JGJ: The body of the response to a `GET` request whose path is `/` MUST be an HTML document whose visible text is exactly `IndexText`, where the body begins, after optional leading whitespace, with `<!doctype html>` compared case-insensitively, contains exactly one `body` start tag (a `<body` followed by optional attributes and `>`) and exactly one `</body>` end tag, both tag names compared case-insensitively, and the visible text is the text between that start tag and that end tag with every tag (each maximal `<`…`>` sequence) removed, every run of whitespace collapsed to one space, and leading and trailing whitespace trimmed.
- R-YJNK-C9NA: The handler `Handler` returns MUST answer a `HEAD` request whose path is `/` with status 200, the header `Content-Type: text/html; charset=utf-8`, and an empty body.
- R-YKVG-Q1DZ: The handler `Handler` returns MUST answer a request whose path is not `/`, whatever its method, with status 404, the header `Content-Type: text/plain; charset=utf-8`, and a body that is exactly `NotFoundBody`, or empty when the method is `HEAD`.
- R-YM3D-3T4O: The handler `Handler` returns MUST answer a request whose path is `/` and whose method is neither `GET` nor `HEAD` with status 405, the header `Allow: GET, HEAD`, the header `Content-Type: text/plain; charset=utf-8`, and a body that is exactly `MethodNotAllowedBody`.
- R-TT59-GZZ5: The handler `Handler` returns MUST decide a response from the request's method and the path component of its URL alone, so that two requests with the same method and path receive the same status code, the same value for every header the handler sets, and the same body.
- R-N5Q6-KDJ6: The non-test `.go` files of `internal/server` MUST declare no package-level `var`.
- R-YPR2-94CR: `NotFoundBody` and `MethodNotAllowedBody` MUST each be one line: exactly one newline, at the end.
