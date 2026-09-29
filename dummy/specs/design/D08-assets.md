# D08-assets

dummy's pages take the platform's visual style from files dummy carries
itself: the stylesheet every page links, the fonts that stylesheet loads, and
the fonts' licence. They live in the checkout's hand-maintained `assets/`
directory and reach the binary through `Assets`, the embedded file system the
root package `dummy` exports (`D01-layout-and-run-seam`). This design is how
dummy serves them. It says nothing about what the files contain, which files
there are, or what they are called: the tests compare what dummy serves with
what `Assets` holds, so a restyle that only replaces files touches no
requirement here.

Serving is part of dummy's one HTTP surface, so it lives in `internal/panel`
and is done by `panel.Handler`, which keeps the signature `D04-panel` declares
and reads `Assets` directly; `internal/panel` is the root package's only
importer. Nothing here declares a new exported name. Everything `Handler` does
before it looks at a path — the identity check that answers a request without
`X-User-Id` with the plain 500 — is `D04-panel`'s, and applies to these routes
as to every other, so it is not repeated here. The same holds for `HEAD`:
`D04-panel` requires a `HEAD` to be answered with the status and headers of
the matching `GET` and an empty body on every path, and that covers a
conditional `HEAD` too, since it is compared with the `GET` carrying the same
`If-None-Match`. The requirements below speak of `GET` and leave `HEAD` to that
rule. Because every asset path is a path other than `/`, `/widgets` and
`/widgets/table`, `D04-panel`'s store-neutrality requirement for such paths
already guarantees that no asset request changes the widgets.

## Paths

The namespace is flat. An **asset path** is `/assets/` followed by the name
of a file directly in `Assets`' `assets` directory, and nothing else is: not
`/assets/` itself, not a path with a further `/`, not a name `Assets` does not
hold, not a name that differs from a held one only in letter case. The path
compared is the request's `URL.Path`, which `net/http` stores decoded (the
`net/url` documentation: "the Path field is stored in decoded form: /%47%6f%2f
becomes /Go/"), so a percent-encoded spelling of an asset name is that asset,
and an encoded slash is a slash and so never names one. Any other path
beginning `/assets/` is a path that does not exist, whatever the method: it
gets the same chrome-drawn not-found page as any unknown path (`D04-panel`),
never a 405. `D04-panel` routes an asset path here rather than to its
catch-all 404.

## Responses

A `GET` of an asset path answers 200 with the file's bytes, unchanged. The
`Content-Type` comes from a fixed table keyed on the file name's extension,
because leaving it unset would let `net/http` guess: the `ResponseWriter.Write`
documentation says that when the header "does not contain a Content-Type
line, Write adds a Content-Type set to the result of passing the initial 512
bytes of written data to DetectContentType".

Every 200 and 304 carries an `ETag` and `Cache-Control: no-cache`. RFC 9111
section 5.2.2.4 gives unqualified `no-cache` the meaning the stories want: the
response "MUST NOT be used to satisfy any other request without forwarding it
for validation". The `ETag` is fixed as the quoted lowercase hexadecimal
SHA-256 digest of the file's bytes. RFC 9110 section 8.8.3.1 names "a
collision-resistant hash of representation content" as a way to generate an
entity tag, and fixing it makes the stories' relation testable within one
build: the same file always yields the same value because the value is a
function of the bytes, and different content yields a different value because
SHA-256 is collision-resistant. A relation stated only as "different content
gives a different tag" could not be tested without building two binaries; a
declared computation is checked against `Assets` directly. The digest's
characters are all within RFC 9110's `etagc`, and there is no `W/` prefix, so
the tag is strong by the grammar of RFC 9110 section 8.8.3; since it changes
whenever a byte changes, it also meets the strong validator's meaning.

A request whose `If-None-Match` names the current tag gets a 304 with an
empty body, and that 304 carries the same `ETag` and `Cache-Control` the 200
would, as RFC 9110 section 15.4.5 requires of a 304 ("MUST generate any of
the following header fields that would have been sent in a 200 (OK) response
... ETag ... Cache-Control"). The field is read as RFC 9110 section 13.1.2
defines it: a comma-separated list of entity tags, or `*`, which matches
because the asset has a current representation; and the comparison is the
weak comparison that section requires ("A recipient MUST use the weak
comparison function"), so an entry of `W/` followed by the tag matches too. A
field that matches nothing is ignored.

Any method other than `GET` and `HEAD` on an asset path answers 405 with
`Allow: GET, HEAD` (RFC 9110 section 15.5.6: a 405 "MUST generate an Allow
header field") and `D04-panel`'s chrome failure page. A 405 and a 404 ignore
`If-None-Match`, as RFC 9110 section 13.2.1 directs: a server "MUST ignore all
received preconditions if its response to the same request without those
conditions, prior to processing the request content, would have been a status
code other than a 2xx (Successful) or 412 (Precondition Failed)", and a 405 or
404 is neither. No answer on these paths other than a 200 or a 304 carries an
`ETag`, the identity 500 included.

## No other origin

The page needs nothing from any other host: no font service and no
third-party request of any kind. The page's own markup is `D04-panel`'s. The
hand-maintained stylesheet is expected to reference only the files beside it
and `data:` URLs, and the copying rule in `dummy/AGENTS.md` (`## Assets`) keeps
it so. That is an authoring rule for the hand copy, not part of this design,
and no requirement here checks it.

## REQUIREMENTS

- R-5JK4-98EF: dummy's design defines an **asset path** as a request path — the value of the request's `URL.Path` field, which `net/http` stores decoded — that consists of `/assets/` followed by a non-empty `<name>` containing no `/`, where `Assets` holds a regular file at path `assets/<name>`, `<name>` being compared byte for byte and so case-sensitively; it calls that file the asset path's **asset file**; and every requirement in dummy's design that names an asset path or an asset file MUST denote that path or that file.
- R-5KS0-N054: `Handler` MUST answer a `GET` request carrying a non-empty `X-User-Id` header, whose path is an asset path, and which carries no `If-None-Match` header, with status 200 and a body byte-identical to that path's asset file.
- R-5LZX-0RVT: A response `Handler` sends with status 200 to a request whose path is an asset path MUST carry a `Content-Type` header whose value is fixed by the extension of the asset file's name — the characters from the last `.` in `<name>` through its end, compared byte for byte, and no extension when `<name>` contains no `.` — as exactly `text/css; charset=utf-8` for `.css`, exactly `font/woff2` for `.woff2`, exactly `text/plain; charset=utf-8` for `.txt`, and exactly `application/octet-stream` for any other extension or none.
- R-5N7T-EJMI: A response `Handler` sends with status 200 or 304 to a request whose path is an asset path MUST carry an `ETag` header whose value is exactly a `"`, then the SHA-256 digest of the bytes of that path's asset file written as 64 lowercase hexadecimal digits, then a `"`, with nothing before or after.
- R-5OFP-SBD7: A response `Handler` sends with status 200 or 304 to a request whose path is an asset path MUST carry the header `Cache-Control` exactly once, with the value exactly `no-cache`.
- R-5PNM-633W: `Handler` MUST answer a `GET` request carrying a non-empty `X-User-Id` header, whose path is an asset path, and which carries an `If-None-Match` field at least one of whose entries — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is exactly `*`, is byte-identical to the `ETag` value the same request would be answered with were the field absent, or is `W/` followed by that value, with status 304 and an empty body.
- R-5QVI-JUUL: `Handler` MUST answer a `GET` request carrying a non-empty `X-User-Id` header, whose path is an asset path, and which carries an `If-None-Match` field no entry of which — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is `*`, the `ETag` value the same request would be answered with were the field absent, or `W/` followed by that value, exactly as it would answer that request were the field absent: with the same status, the same value for every header it sets, and the same body.
- R-5S3E-XMLA: `Handler` MUST answer a request carrying a non-empty `X-User-Id` header whose path is an asset path and whose method is neither `GET` nor `HEAD`, whatever `If-None-Match` field it carries, with status 405, the header `Allow: GET, HEAD`, and a response in the chrome failure shape for `MethodNotAllowedMessage`.
- R-5TBB-BEBZ: `Handler` MUST answer a request carrying a non-empty `X-User-Id` header whose path begins with `/assets/` and is not an asset path — `/assets/` itself, a path with a further `/` after `/assets/`, and a name `Assets` holds no regular file for included — whatever its method and whatever `If-None-Match` field it carries, with status 404 and a response in the chrome failure shape for `NotFoundMessage`.
- R-5UJ7-P62O: A response `Handler` sends to a request whose path begins with `/assets/` MUST carry no `ETag` header when its status is neither 200 nor 304.
