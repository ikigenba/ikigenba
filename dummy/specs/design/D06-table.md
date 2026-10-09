# D06-table

The widgets table, and the one route that serves it on its own. Two things
live here and nowhere else: what the table's template receives, and the whole
of `GET`/`HEAD /widgets/table`.

The table's markup is not dummy's code. It is the `table` template in
`assets/table.html`, written and approved by a human following the
repository's `design/` and embedded by the module's root package, whose
`Assets` (`D01-layout-and-run-seam`) hands it to the panel, which parses it
into dummy's one template set (`D04-panel`). The code executes that template
by name and writes no markup of its own — not a row, not a cell, not an
escape. What it owes the template is its data, and the data is the plainest
thing that could be: the widgets of the store (`D04-panel`), the slice
`D05-widgets`'s `Store.All` returns, the widgets in creation order, each
with its `Name`, `Count` and `Status`. The page passes the same slice as its
data's `Table`. Escaping is `html/template`'s contextual autoescaping and
nothing else; no template function is named, so the template set carries none
of dummy's. One requirement ties the code to the asset by use: the fragment's
body is exactly what executing that template on that slice writes. What the
template writes for that slice — its words, its markup, its hooks — is the
asset's, and no requirement here names it; a template that cannot show what
this design hands it is an issue for the human who owns it, never something
the build writes around.

The table is one thing rendered twice. The panel page (`D04-panel`) embeds it
in the document it sends, and `/widgets/table` re-renders it alone, so that
the page's inline polling script can replace the table with whatever the
fragment returns. That replacement is why the two renderings must be the same
markup: one template drawing the table in both places, given the same slice,
is what makes them so, and both the page's body and the fragment's body are
stated as template output over the widgets of the store. The fragment is a
fragment, with no banner — a caller fetching it with `curl` sees exactly the
markup the page splices in. That the fragment holds nothing the page must keep
— the poller, the heading, the count — is the template author's obligation.

Every failure from this endpoint is plain text with no banner, whether or not
the caller is identified. No template is executed for either: a failure here
is a fixed line, not a drawing. This is the deliberate exception to the rule
that a failure dummy can name to an identified caller is itself a page with
the banner. The reason is the swap again: the script splices whatever comes
back into a document that is already drawn, so an error drawn as a page with
the banner would arrive as a whole page pushed into a table. So this document
says which shape each of this route's failures takes, and `D04-panel` keeps
the shapes themselves. The missing-identity 500 is the one place the two
documents meet: appkit's `identity.Require` answers it for every route at
once, wrapped around the whole handler as `D04-panel` states, and this
document fixes it for this route, from the fragment side, because being a
fragment is what makes it plain. The two failures are the missing-identity 500
and the 405; their bodies are appkit's `identity.MissingBody` and
`MethodNotAllowedBody`, which `D04-panel` declares; this document only names
them. The 405 names `Allow: GET, HEAD` and refuses a `POST` rather than
redirecting it, because creating a widget is a different route.

The route has one more failure, and it is plain too. While the store cannot
reach the widgets (`D04-panel` defines the store being unreachable), there is
no true table to send, and an empty one would tell the page every widget was
gone; so a `GET` or `HEAD` is answered with the unreachable answer `D04-panel`
defines, the 503 whose body is the constant `widget.Unreachable`, whatever
`If-None-Match` it carries. A 304 there would vouch for a table nobody could
read, so the conditional read is a reachable store's only, and the 503 carries
no `ETag`, which the rule below for every status but 200 and 304 already says.
The page's poll simply tries again on its next tick.

This route's `ETag` is stated here, whole: every rule about the table's
validator is in this document. It is not the only `ETag` dummy sends — every
shared file under `/_appkit/` carries appkit's own, on appkit's terms
(`D08-assets`) — and nothing here reaches beyond `/widgets/table`. It is a
strong validator, an opaque quoted string determined by the rendered
fragment's bytes. The quoted characters exclude the comma and whitespace:
`If-None-Match` carries a comma-separated list, so a tag containing a comma
would be split into pieces that match nothing, and the 304 would be
unreachable while every requirement here stayed satisfied. Excluding the comma
is narrower than RFC 9110's own grammar allows, and that is deliberate — it
costs an implementation nothing and makes the list split sound. The contract
fixes the *relation* — equal content yields an equal tag, and the tag changes
when a widget is created — and never the value; a requirement that did would
be fixing an implementation. It is carried by the 200 and the 304 and by
nothing else. The missing-identity 500 is the gate's, not this route's, and
appkit promises nothing about its validators, so this document states none for
it. The 405 carries no validator because it does not represent table content
for a caller to validate.

The conditional read follows RFC 9110 §13.1.2 (`If-None-Match`, HTTP Semantics,
RFC 9110, June 2022): the field carries a list, the condition succeeds when any
member matches, and `*` matches whenever a representation exists — which on
this route it always does while the store is reachable, since the table
renders for every widget set, the empty one included. The comparison is the
weak one that section requires ("A recipient MUST use the weak comparison
function when comparing entity tags for If-None-Match"), so an entry of `W/`
followed by the current tag matches too. A request may carry the field on
several header lines; RFC 9110 §5.3 reads them as one list joined with commas,
and so does this route: every entry of every line counts, and the field is
never refused as malformed — an entry that is not `*`, the current tag, or
`W/` followed by it simply matches nothing. That is this route's own rule,
stated whole below; the shared files follow appkit's, which `D08-assets`
states for them, and neither document leans on the other. RFC 9110 is
published documentation of a well-known protocol, so it is the proof for this
behavior and no live observation is needed.

Reading the fragment never creates, alters or removes a widget, and the
testable form is that the store's widgets are equal, element for element and
in order, immediately before and immediately after any request this route
handles, whatever its method and whatever it is answered with.

A requirement that instead compares two requests to each other — the `HEAD`
against the `GET` it mirrors — has to say what is held equal between them. A
successful creation landing in between changes the fragment's bytes and so, by
this document's own rule, changes the `ETag`, which would make a conforming
implementation falsify the comparison. So that requirement names the store:
the two answers are required to agree only when the store is reachable and
its widgets are the same, in the same order, immediately before each request.
On this route the `ETag` is exactly what makes the omission observable rather
than theoretical.

The polling interval is not this document's: `D04-panel` owns the script that
re-fetches the fragment. The fragment carries no trace of it.

One thing deliberately gets no requirement. That the whole of a page's
content arrives in the response body, and that script never adds content of
its own, is owned here only in its decidable half — that the panel page and
the fragment are both the same template's output over the same widgets —
because the other half, what a script does once it runs, is not decidable by
any procedure the standard library can run.

This document declares no exported name. The template's data is a type
`D05-widgets` already declares, `internal/panel`'s names are `D04-panel`'s
but `FormView`, which `D07-form` declares, and nothing that computes the `ETag` is exported.

## REQUIREMENTS

- R-HDMU-CCPX: The body of a 200 response to a `GET` request whose path is `/widgets/table` MUST be byte-identical to the text that executing the template named `table` in dummy's template set (`D04-panel` R-Y5H2-05Q9) writes when given as its data the **table data** for the widget set current when the response was rendered, where the **widget set current** when a response was rendered is the widgets of the store `Handler` was built over (`D04-panel`) at that moment and the table data for a widget set is that slice of `widget.Widget` values (`D05-widgets`) itself; the table data is the data this document names for the template `table`, which a panel page's page data carries as its `Table` (`D04-panel` R-YACN-J8P1).
- R-HIIF-VFOP: A `GET` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries no `If-None-Match` field whose condition succeeds, MUST, while the store `Handler` was built over is reachable (`D04-panel`), be answered with status 200 and the header `Content-Type: text/html; charset=utf-8`.
- R-HZFA-S3P3: A 200 response to a request whose path is `/widgets/table` MUST carry an `ETag` header whose value is a strong validator: a `"`, then at least one character, none of which is a `"`, a comma or an ASCII whitespace character, then a `"`, with no `W/` prefix and nothing before or after the quoted string.
- R-I1V3-JN6H: Two 200 responses to `GET` requests whose path is `/widgets/table` whose bodies are byte-identical MUST carry `ETag` values that are byte-identical.
- R-I4AW-B6NV: A 200 response to a request whose path is `/widgets/table` sent after a widget has been created MUST carry an `ETag` value differing from the `ETag` value carried by the last such response sent before that creation.
- R-HG2N-3W7B: For a `HEAD` request whose path is `/widgets/table` and the otherwise identical `GET` request, immediately before each of which the store `Handler` was built over is reachable and its widgets (`D04-panel`) are equal element for element and in the same order, the `HEAD` request MUST be answered with the status, the `ETag` value and every other header the `GET` request is answered with, and with an empty body.
- R-HJQC-97FE: A `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries an `If-None-Match` field at least one of whose entries — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is exactly `*`, is byte-identical to the `ETag` value the same request would be answered with were the field absent, or is `W/` followed by that value, MUST, while the store `Handler` was built over is reachable (`D04-panel`), be answered with status 304, that same `ETag` value, and an empty body.
- R-HKY8-MZ63: While the store `Handler` was built over is unreachable (`D04-panel`), a `GET` or `HEAD` request carrying a non-empty `X-User-Id` header whose path is `/widgets/table` MUST be answered with the unreachable answer (`D04-panel`), never in the banner failure shape, whatever `If-None-Match` fields it carries, one with the entry `*` and one carrying the `ETag` value of a 200 response to a request whose path is `/widgets/table` sent while that store was reachable included.
- R-ICU6-ZKUQ: A `GET` or `HEAD` request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table`, and which carries an `If-None-Match` field no entry of which — the values of all its `If-None-Match` header lines joined with commas, split on commas, each entry trimmed of leading and trailing whitespace — is `*`, the `ETag` value the same request would be answered with were the field absent, or `W/` followed by that value, MUST be answered exactly as it would be were the field absent, and the `ETag` value it is answered with MUST be such that no entry of that field is that value or `W/` followed by it.
- R-IF9Z-R4C4: A request whose path is `/widgets/table` and whose `X-User-Id` header is absent or present with an empty value MUST be answered with status 500 and a response in the plain failure shape (`D04-panel`) for appkit's `identity.MissingBody`, never in the banner failure shape.
- R-IIXO-WFK7: A request carrying a non-empty `X-User-Id` header, whose path is `/widgets/table` and whose method is neither `GET` nor `HEAD`, MUST be answered with status 405, the header `Allow: GET, HEAD`, no `Location` header, and a response in the plain failure shape (`D04-panel`) for `MethodNotAllowedBody`, never in the banner failure shape.
- R-0142-4LXJ: A response to a request carrying a non-empty `X-User-Id` header whose path is `/widgets/table` and whose status is neither 200 nor 304 MUST carry no `ETag` header.
- R-HHAJ-HNY0: Handling a request whose path is `/widgets/table` MUST leave the store `Handler` was built over unchanged, whatever the request's method and whatever status it is answered with, the unreachable answer (`D04-panel`) included: the widgets of that store (`D04-panel`) when it is last reachable before the request and when it is first reachable after it, no other request being answered in between, are equal element for element and in the same order.
