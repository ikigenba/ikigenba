# D08-assets

auth's pages take the platform's visual style from files auth carries itself:
the stylesheet every page links, the fonts that stylesheet loads, and the
licences of what they carry. They live in the checkout's hand-maintained
`assets/` directory and reach the binary through `Assets`, the embedded file
system the root package `auth` exports (`D01-layout-and-run-seam`). This
design is how auth serves them. It says nothing about what the files contain,
which files there are beyond the one every page links, or what they are
called: the tests compare what auth serves with what `Assets` holds, so a
restyle that only replaces files touches no requirement here.

Serving is part of auth's one HTTP surface, so it is done by the `*Server`
that `server.New` returns (`D03-serve`), which reads `Assets` directly;
`internal/server` is the root package's only importer (D01). Nothing here
declares a new exported name. The files replace the embedded HTML,
JavaScript, and CSS auth used to serve at `/assets/index.html`,
`/assets/app.js`, and `/assets/style.css`; those names are not in `assets/`,
so they fall under the not-found rule below like any other missing name.

An asset is the same for everyone. A visitor drawing the sign-in page has no
session yet, so no request under `/assets/` needs a credential, and none is
answered differently for carrying one: a session cookie or a bearer token
changes nothing, and no answer sets a cookie. Serving an asset touches
nothing auth keeps — it never calls the store — so a request under `/assets/`
changes no state, and its answer is the same when the store has failed. The
bytes come from the binary: auth reads nothing from disk to answer for an
asset, and a host holds no `assets/` directory, so the answer does not depend
on the working directory or on any file in it.

## Paths

The namespace is flat. An **asset path** is `/assets/` followed by the name
of a file directly in `Assets`' `assets` directory, and nothing else is: not
`/assets/` itself, not a path with a further `/`, not a name `Assets` does not
hold, not a name that differs from a held one only in letter case. The path
compared is the request's `URL.Path`, which `net/http` stores decoded (the
`net/url` documentation: "Note that the Path field is stored in decoded form:
/%47%6f%2f becomes /Go/."), so a percent-encoded spelling of an asset name is
that asset, and an encoded slash is a slash and so never names one. Any other
path beginning `/assets/` is a path that does not exist, whatever the method:
it answers 404 with one line of plain text (a `HEAD` gets the same status and
headers with no body), never a 405 and never a page in the chrome, since the
caller may be signed out. That includes paths `net/http`'s `ServeMux` would
otherwise redirect to a cleaned form, such as `/assets//theme.css` or
`/assets/./x`: each begins with `/assets/` and has a further `/`, so it is not
an asset path and answers 404.

## Responses

A `GET` of an asset path answers 200 with the file's bytes, unchanged. The
`Content-Type` comes from a fixed table keyed on the file name's extension,
because leaving it unset would let `net/http` guess: the `ResponseWriter.Write`
documentation says that if the header "does not contain a Content-Type line,
Write adds a Content-Type set to the result of passing the initial 512 bytes
of written data to DetectContentType".

Every 200 and 304 carries an `ETag` and `Cache-Control: no-cache`. RFC 9111
section 5.2.2.4 gives unqualified `no-cache` the meaning the stories want: the
response "MUST NOT be used to satisfy any other request without forwarding it
for validation". The story leaves the tag's value opaque to a visitor and
fixes only a relation: the same file always yields the same value, and an auth
carrying different content for that file yields a different one. A relation
between two builds cannot be tested inside one, so the design fixes the
computation instead: the tag is the quoted lowercase hexadecimal SHA-256
digest of the file's bytes. RFC 9110 section 8.8.3.1 names "a
collision-resistant hash of representation content" among the ways to
generate an entity tag. The same bytes give the same digest because the
digest is a function of the bytes; different bytes give a different digest
because SHA-256 is collision-resistant — NIST SP 800-107 Rev. 1 defines
collision resistance as "it is computationally infeasible to find two
different inputs to the hash function that have the same hash value" and
states that "SHA-256 provides an expected collision resistance of 128 bits".
A test therefore decides the relation deterministically, within one build, by
computing each file's digest from `Assets` and comparing it with the tag
served. The digest's characters all fall within RFC 9110's `etagc`
(`%x21 / %x23-7E / obs-text`) and the tag has no `W/` prefix, so it is strong
by the grammar of RFC 9110 section 8.8.3 (`entity-tag = [ weak ] opaque-tag`);
since it changes whenever a byte changes, it also has a strong validator's
meaning.

A request whose `If-None-Match` names the current tag gets a 304 with an
empty body, and that 304 carries the same `ETag` and `Cache-Control` the 200
would, as RFC 9110 section 15.4.5 requires ("The server generating a 304
response MUST generate any of the following header fields that would have
been sent in a 200 (OK) response to the same request", a list naming `ETag`
and `Cache-Control`); the same section says a 304 "cannot contain content".
The field is read as RFC 9110 section 13.1.2 defines it: `If-None-Match = "*"
/ #entity-tag`, a comma-separated list of entity tags, or `*`, which matches
because "the origin server has a current representation for the target
resource"; and the comparison is the weak one that section requires ("A
recipient MUST use the weak comparison function when comparing entity tags
for If-None-Match"), under which two tags are equivalent "if their
opaque-tags match character-by-character, regardless of either or both being
tagged as 'weak'" (section 8.8.3.2), so an entry of `W/` followed by the tag
matches too. A field that matches nothing is ignored.

A `HEAD` is answered as the `GET` would be, with no body: RFC 9110 section
9.3.2 says "The HEAD method is identical to GET except that the server MUST
NOT send content in the response", and the story fixes headers and status
alike, so every header field but `Date` is the `GET`'s.

Any method other than `GET` and `HEAD` on an asset path answers 405 with
`Allow: GET, HEAD` (RFC 9110 section 15.5.6: "An origin server MUST generate
an Allow header field in a 405 (Method Not Allowed) response") and one line
of plain text. A 405 and a 404 ignore `If-None-Match`, as RFC 9110 section
13.2.1 directs: "A server MUST ignore all received preconditions if its
response to the same request without those conditions, prior to processing
the request content, would have been a status code other than a 2xx
(Successful) or 412 (Precondition Failed)", and a 405 or 404 is neither. No
answer on these paths other than a 200 or a 304 carries an `ETag`.

## No other origin

A page needs nothing from any other host: no font service and no third-party
request of any kind. The page's own markup is D05's and D07's, and D05 holds
every resource reference a page carries to a path under `/assets/`. What the
stylesheet makes a browser fetch is decided by the URLs it carries, and that
is not a requirement here: `assets/` is hand-maintained, so what the copied
files reference is an authoring rule for the person who copies them, not a
property the run builds or a test checks. The copy is expected to reference only the files beside it,
by bare relative names that resolve under `/assets/`, and `data:` URLs, which
carry their resource inline and make no request.

## REQUIREMENTS

- R-23D7-LKIT: auth's design defines an **asset path** as a request path — the value of the request's `URL.Path` field, which `net/http` stores decoded — that consists of `/assets/` followed by a non-empty `<name>` containing no `/`, where `Assets` holds a regular file at path `assets/<name>`, `<name>` being compared byte for byte and so case-sensitively; it calls that file the asset path's **asset file**; and every requirement in auth's design that names an asset path or an asset file MUST denote that path or that file.
- R-25T0-D407: The `*Server` MUST answer a `GET` request whose path is an asset path and which carries no `If-None-Match` header with status 200 and a body byte-identical to that path's asset file.
- R-270W-QVQW: A response the `*Server` sends with status 200 to a request whose path is an asset path MUST carry a `Content-Type` header whose value is fixed by the extension of the asset file's name — the characters from the last `.` in `<name>` through its end, compared byte for byte, and no extension when `<name>` contains no `.` — as exactly `text/css; charset=utf-8` for `.css`, exactly `font/woff2` for `.woff2`, exactly `text/plain; charset=utf-8` for `.txt`, and exactly `application/octet-stream` for any other extension or none.
- R-288T-4NHL: A response the `*Server` sends with status 200 or 304 to a request whose path is an asset path MUST carry an `ETag` header whose value is exactly a `"`, then the SHA-256 digest of the bytes of that path's asset file written as 64 lowercase hexadecimal digits, then a `"`, with nothing before or after.
- R-29GP-IF8A: A response the `*Server` sends with status 200 or 304 to a request whose path is an asset path MUST carry the header `Cache-Control` exactly once, with the value exactly `no-cache`.
- R-2BWI-9YPO: The `*Server` MUST answer a `GET` request whose path is an asset path and which carries an `If-None-Match` field at least one of whose entries — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is exactly `*`, is byte-identical to the `ETag` value the same request would be answered with were the field absent, or is `W/` followed by that value, with status 304 and an empty body.
- R-2D4E-NQGD: The `*Server` MUST answer a `GET` request whose path is an asset path and which carries an `If-None-Match` field no entry of which — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is `*`, the `ETag` value the same request would be answered with were the field absent, or `W/` followed by that value, exactly as it would answer that request were the field absent: with the same status, the same value for every header field other than `Date`, and the same body.
- R-2ECB-1I72: The `*Server` MUST answer a `HEAD` request whose path begins with `/assets/` with the status and the value for every header field other than `Date` with which it answers a `GET` request for the same path carrying the same header fields, and with an empty body.
- R-2FK7-F9XR: The `*Server` MUST answer a request whose path is an asset path and whose method is neither `GET` nor `HEAD`, whatever `If-None-Match` field it carries, with status 405, the header `Allow` with the value exactly `GET, HEAD`, `Content-Type: text/plain; charset=utf-8`, and a body that is one line of plain text: one or more bytes none of which is `\n` or `\r`, followed by a single `\n`.
- R-UDBA-BA8O: The `*Server` MUST answer a request whose path begins with `/assets/` and is not an asset path — `/assets/` itself, a path with a further `/` after `/assets/`, and a name `Assets` holds no regular file for included — whatever its method and whatever `If-None-Match` field it carries, with status 404 and `Content-Type: text/plain; charset=utf-8`, and, when its method is not `HEAD`, with a body that is one line of plain text: one or more bytes none of which is `\n` or `\r`, followed by a single `\n`.
- R-2I00-6TF5: A response the `*Server` sends to a request whose path begins with `/assets/` MUST carry no `ETag` header when its status is neither 200 nor 304.
- R-2J7W-KL5U: The `*Server` MUST answer a request whose path begins with `/assets/` with the same status, the same value for every header field other than `Date`, and the same body as it answers the same request with its `Cookie` and `Authorization` header fields removed, whatever those fields carry, a cookie naming a live session and a bearer token that is live included.
- R-2LNP-C4N8: A response the `*Server` sends to a request whose path begins with `/assets/` MUST carry no `Set-Cookie` header.
- R-2MVL-PWDX: The `*Server` MUST answer a request whose path begins with `/assets/` without calling any method of the `*store.Store` it was built with, so that it answers such a request with the same status, the same value for every header field other than `Date`, and the same body after that `*store.Store` has been closed as before.
- R-2O3I-3O4M: The `*Server`'s answer to a request whose path begins with `/assets/` MUST NOT depend on the process's working directory or on any file outside the executable: it MUST answer with the same status, the same value for every header field other than `Date`, and the same body whatever the working directory holds, an `assets/` directory whose files differ in content from those `Assets` holds included.
