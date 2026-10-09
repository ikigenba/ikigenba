# D07-form

Creating a widget from the panel is one of the two ways dummy's state changes
(the other is the MCP tool `create_widget`, `D09-mcp`), and this document owns
all of it: the data the form's template receives and the whole of
`POST /widgets`.

The form's markup is not dummy's code. It is the `form` template in
`assets/form.html`, written and approved by a human following the repository's
`design/`, embedded by the module's root package and handed out by its
`Assets` (`D01-layout-and-run-seam`), and parsed into dummy's one template set
(`D04-panel`), where the page invokes it with its data's `Form`. The code
executes that template by name and writes no markup of its own. What the code
owes it is its data, a `FormView`: the `Submission` to echo, the `FieldErrors`
to place beside the fields, the statuses to offer, and the status to select.
On a `GET /widgets` page all but the statuses are zero, so the same template
draws an empty form; on a 422 they carry the rejected submission. Escaping is
`html/template`'s contextual autoescaping and nothing else, and no template
function is named, so the template set carries none of dummy's. The
requirements tie the code to the asset by use: a panel page, the 422 redraw
included, is exactly what executing the `page` template writes for panel page
data whose `Form` is the form view this document defines. What the template
writes for that view — its words, its markup, the card it sits in, the button,
where each message sits beside its field, and the hooks the stylesheet reads —
is the asset's, and no requirement names it; a template that cannot show a
state this design hands it is an issue for the human who owns it, never
something the build writes around.

The form is an ordinary HTML form. It POSTs to `/widgets` with
`application/x-www-form-urlencoded`, it has one control per widget field —
`name`, `count`, `status` — and nothing about a submission is assembled by
JavaScript, so a caller with `curl` submits exactly what a browser submits and
dummy validates one submission shape the same way whoever sent it. That the
markup keeps that promise — the keys, the method, the encoding, one submit
control that overrides none of them — is the template author's obligation,
not a requirement: stating it would mean reading markup. The keys the route
reads, `name`, `count` and `status`, are contract, and R-MLL7-BZZT states them.

**The route has five answers and no sixth.** A request without identity is a
500, decided before anything else by appkit's `identity.Require`, which
`D04-panel` wraps around the whole handler; that answer is not restated here,
but the *ordering* is stated here, extended from path and
method to the submitted fields: a body that would have been accepted and a
body that would have been rejected are answered identically, and neither is
read. A submission whose media type is not `application/x-www-form-urlencoded`
is a 415: the objection is to the format, not to the values, and no retry that
keeps the media type can succeed, so a 422 would be an invitation to send
better values that could never be taken up. The body is not read at all, no
widget form and no field errors come back, and there is no JSON way in through
this route. The 415 page is drawn with the banner like the 404 and the 405, in
the banner failure shape `D04-panel` defines, with no form. A submission dummy reads and accepts is a 303 to
`/widgets` with an empty body. A submission dummy reads and rejects is a 422
whose body is a panel page. And while the store cannot reach the widgets
(`D04-panel` defines the store being unreachable), every form-encoded
submission is the unreachable answer `D04-panel` defines, the plain-text 503
whose body is the constant `widget.Unreachable`: an acceptable one, because
nothing can be stored and a 303 would send the user to a panel that lacks the
widget; and a rejected one too, because the 422 redraws the table, which
cannot be read. Nothing is stored and nothing is recorded but the request's
own two events. A body that is not form-encoded still gets its 415, which
needs no widget.

**Why the success is a redirect.** The browser must not be left showing the
result of a POST: after a 303 it fetches `/widgets` with a GET, so the address
the user ends on is the panel and refreshing re-reads the panel instead of
submitting a second time. There is no flash message and no confirmation
notice. dummy sets no cookie and puts nothing in the URL, so nothing carries a
message across the redirect — the new row in the table is the confirmation,
and the redirect's target is exactly `/widgets`, bare. No requirement below
says "there is no flash message": "a message" is not something a check can
recognise in a page. The carriers are fixed instead, every one a message could
ride: the 303's `Location` is exactly `/widgets` with no query string and no
fragment and its body is empty (stated below), and no answer carries a
`Set-Cookie` header, which `D04-panel` states once for every response and
this document does not repeat. Nothing else survives a redirect, and the
panel page that follows is the `page` template's output for data that carries
no message. The absence of a notice is therefore a consequence of decidable
requirements rather than a requirement of its own.

**The 422 body is a panel page**, and "panel page" is a document shape
`D04-panel` owns rather than a route. The 422 body is exactly the `page`
template's output for panel page data built from the banner the banner source
returned, the widgets as they are — nothing was created — and the form view of
that answer. So it is drawn with the same banner and the same table as a
`GET /widgets`, and what it adds is all in the form view: the values the
caller submitted, the field errors, and the status to select.

**The echo is raw wherever the control can hold bytes.** All three values are
trimmed before they are judged, but the 422 hands the template the two
free-text fields exactly as the caller submitted them — a 41-character name
comes back in full, unshortened, and the count field holding the text `three`
as it was typed. So the form view carries the `Submission`
(`D05-widgets` R-ISI4-P0AR), which holds the raw strings, never anything
parsing or the rules produced. Echoing arbitrary caller bytes into markup is
exactly where a design can hand a caller a tag; `html/template`'s contextual
autoescaping, which writes the echo as a value and never as structure, is what
closes it (`D04-panel`).

The status control is the one field whose echo cannot be literal, and the
difference is the control rather than an inconsistency. A select cannot hold
arbitrary bytes at all: it selects one of its three options, or none. There is
therefore nothing of the caller's to give back byte for byte, and the faithful
rendering is instead the option the submission would actually have used — the
one equal to the *trimmed* status, the value `ParseSubmission` parses
(`D05-widgets`), which is exactly the `Status` of the `Draft` it returns and so
exactly what the form view selects; the zero `Status`, selecting none, when
the trimmed status is not one of the three words. Selecting on the raw value
instead would answer a submission of `status=%20active` — perfectly acceptable
everywhere else, since trimming happens before validation — with nothing
selected whenever some other field failed.

**Every wrong field is reported at once.** A submission is judged in the two
layers `D05-widgets` defines. `ParseSubmission` turns the three strings into a
`Draft`, reporting a count that is not a whole number and a status that is not
one of the three; then the rules for a widget, the same ones the MCP tool
`create_widget` applies (`D09-mcp`), judge the `Draft`. A submission's field
errors combine the two, field by field: the parse message where parsing
failed, and otherwise the rules' message. So
`name=&count=three&status=archived` earns all three messages from one answer —
the name from the rules, the count and status from parsing — and
`name=alpha&count=-1&status=active` earns the taken name and the negative
count, both from the rules. When parsing found nothing, the handler creates
through `Store.Create`, which decides the rules and adds the widget in one
step, so two submissions racing for one name cannot both win; the field
errors are then what `Create` reported, so the answer follows what `Create`
actually did — a 303 only for the submission that created the widget, a 422
with the taken-name message for the one that lost; when parsing
found something, nothing is created, and `Store.Check` supplies the rules'
messages for the fields that did parse. The form view carries the field errors
rather than anything about inputs, so one message beside each rejected field
and none beside any other is one rule that holds for one bad field and for
three. `D05-widgets` owns the parsing, the rules and the six
message constants — `NameRequiredMessage`, `NameTooLongMessage`,
`NameTakenMessage`, `CountNotWholeMessage`, `CountNegativeMessage` and
`StatusNotAllowedMessage` — and this document names them only through the
field of the field errors that carries them. No rule of theirs is restated
here; a second statement of a rule is a second contract that will drift. The
same applies to the definitions `D04-panel` states once for the whole design:
an HTML document dummy sends, a panel page and the banner failure shape. The phrase "carries identity" is the one this
document defines for itself, as appkit's `identity.Require` decides it: a
first `X-User-Id` value that is present and not empty. This document cites
each where it uses it and restates no step of any of them.

That creating a widget is the *only* interaction that changes state is not one
document's to state. `D04-panel` fixes that its page and error routes leave
the store alone, `D06-table` fixes it for the fragment, and this document
fixes it for the three refusals and for the request that never gets as far as
the fields. Those together are the claim; none of them repeats another.

Two things are deliberately absent. That nothing is assembled by JavaScript
is not decidable with the standard library, and no requirement pretends
otherwise; what a `curl` caller depends on — the keys the route reads, the
method and the encoding it accepts — is fixed below as the route's behavior.
And no requirement here names a validation outcome for a particular input:
which message a value earns is `D05-widgets`'s, and the rejections are covered
here as HTTP behavior over whatever the field errors report.

Two requirements below compare two requests that differ in one thing and demand
one answer: the 500 that never looks at the body, and the `Accept` header that
changes nothing. A comparison like that means something only if everything else
really is equal, and the thing most easily unequal is the store's widgets — a creation
landing between the two requests changes the table inside a panel page and
falsifies the claim without any handler misbehaving. Both therefore pin the
store's contents equal immediately before each of the two requests, which is
the form `D04-panel` settled for the whole design.

**A widget created is recorded.** Creating a widget is dummy's one domain event. When the route answers 303, it has created a widget, and the handler's telemetry writer (`D04-panel`) records `widget.created`, whose one attribute, `widget`, is the new widget's id (`D05-widgets`), between the request's `request.started` and `request.finished` and under the same request id and user. It never carries the name, count or status the user submitted: those are the store's to answer. Every other answer — a 422, a 415, the 503 or the missing-identity 500 — created nothing and records no `widget.created`; its `request.finished` carries the status.

## REQUIREMENTS

- R-IQ93-720D: The `internal/panel` package MUST export `type FormView` as a struct with the fields `Submission widget.Submission`, `Errors widget.FieldErrors`, `Statuses []widget.Status`, and `Selected widget.Status`, where `widget` is the package `internal/widget` (`D05-widgets`).
- R-MMT3-PRQI: dummy's design defines the **field errors** of a form-encoded submission, and every requirement in dummy's design that names the field errors of a submission MUST denote that value, as follows, where `d` and `p` are the `Draft` and the `FieldErrors` that `ParseSubmission` (`D05-widgets`) returns for the `Submission` that submission produces: when `p.Any()` is true, the `FieldErrors` (`D05-widgets`) value each of whose three fields is `p`'s message for the field when it is non-empty, and otherwise the message for the field in the `FieldErrors` that `Store.Check` (`D05-widgets`) returns for `d` when called on the store `Handler` was built over immediately before the request; when `p.Any()` is false, the request's creation errors (R-MLL7-BZZT), so that the field errors are always the offences that decided whether a widget was created.
- R-U955-PAW6: dummy's design defines the **form view** of a panel page, the data this document names for the template `form`, which a panel page's page data carries as its `Form` (`D04-panel` R-YACN-J8P1), as follows, and every requirement in dummy's design that names a form view, or the data of the `form` template for a page, MUST denote that value: the form view of the body of a 422 answer to a `POST /widgets` request is the `FormView` whose `Submission` is the `Submission` that request produced, whose `Errors` is that submission's field errors (R-MMT3-PRQI), whose `Statuses` is the slice `widget.Statuses()` returns, and whose `Selected` is the `Status` of the `Draft` that `ParseSubmission` returns for that `Submission`; the form view of every other panel page, the answer to `GET /widgets` included, is the `FormView` whose `Statuses` is the slice `widget.Statuses()` returns and whose other fields are zero.
- R-K5GV-9F5J: A `POST /widgets` request MUST be a form-encoded submission when the value of its `Content-Type` header, truncated at the first `;` and with leading and trailing whitespace removed, equals `application/x-www-form-urlencoded` compared case-insensitively, and MUST NOT be one otherwise, a request carrying no `Content-Type` header included.
- R-MLL7-BZZT: A request **carries identity** when its first `X-User-Id` value is present and not empty. For a `POST /widgets` request that carries identity and is a form-encoded submission, the `Submission` (`D05-widgets`) that request produces MUST be the one whose `Name`, `Count` and `Status` are the values the request body carries for the keys `name`, `count` and `status` respectively, none of them altered, taking, for each of those three keys, the first value when the body carries the key more than once and the empty string when the body carries no value for it; when `Any()` on the `FieldErrors` that `ParseSubmission` (`D05-widgets`) returns for that `Submission` is false, the request MUST have exactly the effect on the store `Handler` was built over of one call of `Store.Create` (`D05-widgets`) for the `Draft` `ParseSubmission` returns for that `Submission`, made in its place, and no other effect on that store, and this document calls the `FieldErrors` that `Create` call returns the request's **creation errors**; and when it is true, the handler MUST add no widget.
- R-HTHJ-BDCY: The handler MUST answer a `POST /widgets` request that carries identity and is a form-encoded submission, while the store it was built over is reachable (`D04-panel`), with status 303 when `Any()` on that submission's field errors (R-MMT3-PRQI) is false, which is exactly when the `Store.Create` call of R-MLL7-BZZT added the widget, and with status 422 when it is true, whether parsing or `Store.Create` reported the offences.
- R-HUPF-P53N: While the store the handler was built over is unreachable (`D04-panel`), the handler MUST answer a `POST /widgets` request that carries identity and is a form-encoded submission with the unreachable answer (`D04-panel`), whatever values the request body carries for `name`, `count` and `status`, those of an acceptable submission (`D04-panel`) on that store when it was last reachable and those `ParseSubmission` (`D05-widgets`) or the store's rules reject included.
- R-KE05-XTCE: The 303 answer to a `POST /widgets` request MUST carry a `Location` header whose value is exactly `/widgets`, with no query string and no fragment, and an empty body.
- R-HNE1-EINH: After a `POST /widgets` request the handler answered 303, the widgets of the store the handler was built over (`D04-panel`) MUST be the widgets of that store immediately before that request with exactly one element appended at the end, whose `Name` is the trimmed name (`D05-widgets` R-FIEM-UUKE) of the `Draft` `ParseSubmission` returns for the `Submission` that request produced, and whose `Count` and `Status` are that `Draft`'s `Count` and `Status`.
- R-UAD2-32MV: The body of the 422 answer to a `POST /widgets` request MUST be a panel page, as `D04-panel` defines a panel page (R-RHG9-RNM6), and MUST be exactly the text that executing the template `page` of dummy's template set (`D04-panel` R-Y5H2-05Q9) writes with the panel page data (`D04-panel` R-YACN-J8P1) for the `page.Banner` the banner source returned while answering that request, the widgets of the store the handler was built over (`D04-panel`) immediately before that request, and the form view of that answer (R-U955-PAW6).
- R-HOLX-SAE6: A `POST /widgets` request the handler answered 415, 422 or 503 MUST leave the widgets of the store the handler was built over (`D04-panel`) as they were: those widgets when the store is first reachable after that request MUST be equal to those widgets when it was last reachable before it, no other request being answered in between, element for element, in order, and with each element's `Name`, `Count` and `Status` equal, so that no widget is added, removed, reordered or altered.
- R-KNRC-ZZ9Y: A `POST /widgets` request that carries identity and is not a form-encoded submission MUST be answered with status 415 and a response in the banner failure shape for `UnsupportedMediaTypeMessage`, as `D04-panel` defines that shape.
- R-UBKY-GUDK: In answering a `POST /widgets` request that carries identity and is not a form-encoded submission, the handler MUST NOT read any byte of the request body.
- R-HPTU-624V: A `POST /widgets` request that does not carry identity MUST be answered without reading any byte of the request body and without changing the widgets of the store the handler was built over (`D04-panel`), and two such requests differing only in their bodies, immediately before each of which the widgets of that store are equal element for element, in the same order and with each element's `Name`, `Count` and `Status` equal, MUST receive the same status, the same value for every header the handler sets, and the same body.
- R-HR1Q-JTVK: The handler MUST answer every `POST /widgets` request with status 500, 415, 303, 422 or 503, and with no other status.
- R-HS9M-XLM9: No answer the handler sends to a `POST /widgets` request MUST carry a `Content-Type` header whose media type is `application/json`, and two `POST /widgets` requests differing only in their `Accept` header, immediately before each of which the widgets of the store the handler was built over (`D04-panel`) are equal element for element, in the same order and with each element's `Name`, `Count` and `Status` equal, MUST receive the same status, the same value for every header the handler sets, and the same body.
- R-HVXC-2WUC: When the handler answers a `POST /widgets` request with status 303, its telemetry writer (`D04-panel`) MUST record, between that request's `request.started` and `request.finished` events, exactly one event named `widget.created`, carrying that request's envelope request id and user (`D04-panel` R-KSBT-MVET), whose attributes are exactly `widget`, the `ID` of the element R-HNE1-EINH states that request appended to the widgets of the store the handler was built over.
- R-L6YM-84B5: When the handler answers a `POST /widgets` request with any status other than 303, its telemetry writer MUST record no event named `widget.created` for that request.
