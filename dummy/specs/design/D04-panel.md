# D04-panel

What a visitor gets from a running dummy. dummy's whole HTTP surface lives in
one package, `internal/panel`: the one `http.Handler` the process serves, the
identity precondition every request passes through, routing, the page frame
the platform's banner is drawn in and the head every page carries, the icon
the widget form's button carries, the two failure shapes, and the panel page
itself. `internal/cli` builds the widget store, hands it to `panel.Handler`
together with the banner source and the writer the handler's diagnostics go
to, and gives the result to `server.Serve` (`D01-layout-and-run-seam`,
`D03-serve`). This design says nothing about the socket, nginx, TLS or the
host's name, because none of those reach the handler.

The banner is no longer dummy's markup. It is the platform's, the same for
every app: appkit (`github.com/ikigenba/ikigenba/appkit`,
`D01-layout-and-run-seam`) ships it as the template named `banner` in the set
`appkit.Templates()` returns, together with the service launcher it opens.
dummy decides what goes into it — the caller's email and the two auth URLs it
derives from the request — and where it goes on the page; appkit decides how
it is drawn. The banner data comes from the **banner source**, the function
`Handler` is built over: in the running binary the `Banner` method of the kit
`main` made with `appkit.New(ServiceName)`, which adds the service's name and
the launcher's services, read afresh from the host's services file on every
call; in a test, a closure returning whatever the case needs.

Four sibling designs finish the surface. `D05-widgets` owns the domain — the
widget, the store, and the rules a submission is judged by. `D06-table` owns
the widgets table's markup, its anchor, and the whole of the
`GET`/`HEAD /widgets/table` route. `D07-form` owns the widget form's markup,
the card it sits in, and the whole of `POST /widgets`. `D08-assets` owns the
platform's shared files dummy serves through appkit — the stylesheet, the
launcher's script, the fonts and the licences — and every answer a request
under `/_appkit/` gets: it defines an **appkit path**, and this document uses
that term to leave those paths out of its catch-all and out of the rules it
states over what dummy writes, and says nothing else about them. This
document states **nothing**
route-specific about `/widgets/table` — not its successes, not its failures,
not the validator its responses carry: every requirement about that route
belongs to `D06-table` and to no one else, and nothing here names what
`D06-table` names. Two authors minting the same 405 is exactly what this split
prevents. Where this document has to name that path at all, it says only what
it must: that the route exists and is not an unknown path, so that routing
stays total, and that whatever it answers with is not one of the HTML
documents whose shape is fixed here.

A declaration, though, is about a package, not about a route. Every exported
name of `internal/panel` is declared here, `MissingIdentityBody`,
`MethodNotAllowedBody`, `UnsupportedMediaTypeMessage` and `PlusIcon`
included, even though the behavior that names each of those four is stated by
`D06-table` or by `D07-form`. Those documents name the constants; they do not
re-declare them. `ServiceName` is declared here too, and `main` hands it to
`appkit.New` (`D01-layout-and-run-seam`), so the name the page's title
carries and the name appkit's banner marks are one declaration.

## Identity comes first

An nginx gate in front of dummy authenticates every request and sets
`X-User-Id` and `X-User-Email` on what it passes upstream, and a sibling app
that calls dummy forwards the ones it received (`D03-serve` states the terms).
Only nginx and the suite's own apps can reach dummy's socket. So a request
arriving without `X-User-Id` says the gate or a sibling is misconfigured —
dummy's fault to report, not the caller's to fix, hence a 500 and not a 400 or
a 401. `X-User-Id` alone gates the request: a present-but-empty value counts
as missing. `X-User-Email` is not a precondition at all; dummy hands the
banner source whatever arrived, which may be nothing.

That 500 is trouble — every 5xx is, and it is dummy's only one — and it is
the one answer dummy reports on stderr: one line,
`dummy: request <id>: X-User-Id is missing`, naming the request by its
`X-Request-Id` so an operator can find the same request in nginx's log, and
`-` when the request carries none (a developer's hand-made request). An empty
`X-Request-Id` is read as none, the same reading `X-User-Id` gets. The id is
written as it arrived: nginx sets it, overwriting whatever a client sent, and
dummy trusts the suite. Every other answer — a 303, a 404, a 405, a 415, a
422 — is the caller's business or a success and writes nothing, so a healthy
dummy stays silent. The handler writes its line through the `io.Writer` it was
built with, never a real stream, one `Write` per line, and never from two
requests at once, so a writer that is not safe for concurrent use, such as a
test's buffer, is safe to hand it.

The check runs **before** dummy looks at the path or the method, so a request
without identity is never a 303, a 404, a 405 or a 415, on any route — the
root and the fragment route included. That ordering is a requirement rather
than an implementation note because the alternative is indistinguishable from
the outside only until the first unknown path arrives without headers.

There is no unauthenticated case anywhere: every path is one of the three
routes, an appkit path, or an unknown path answered with a 404, all of them
behind the same check, and so there is no sign-in page and no signed-out
banner for a request to reach. The appkit paths are no exception — a
stylesheet is never sent to a request without identity, and the 500 that
answers it carries no validator for a cache to keep. A request without
identity never reaches the banner source either, so it never causes a read of
the services file.

## The space, the scheme, the email link and the sign-out form

The banner offers sign-out as a form, not a link: a one-button `POST` to auth's
`/logout` on the same space. That is possible because auth accepts a sign-out
`POST` whose `Origin` is on its space — over `https`, `https://<space>` or any
`https://<host>.<space>` at any depth, and run locally `http://localhost` on any
port — as auth's own stories state (its `S3-sign-in` group, "A user signs out
from an app on the space"). The browser sends auth's `SameSite=Lax` session
cookie with that `POST` because dummy and auth are same-site, and auth's answer
ends the session and sends the browser to auth's sign-in page. All of that is
auth's behavior, stated in auth's stories and not restated here. dummy's part
is to name the form's target and nothing more — appkit's banner template draws
the form: dummy serves no logout route — `/logout` is an unknown path like any
other and takes a 404 with the banner — and it sets no cookie on any answer.

The banner's one link is the caller's email, which leads to their profile in
auth. auth's stories put the profile at auth's `/`: its `S3-sign-in` group
opens with "the sign-in page and profile at `/`", and its story "A user asks
for the profile" fetches the profile as `http://localhost:3001/`. On a space
auth is served as `auth.<space>`, the host that same group names as auth's own
origin, `https://auth.<space>`. So the link's `href` is auth's root on the same
space, and what auth shows there — the profile, or its sign-in page to a
visitor whose session has ended — is auth's behavior, not restated here. The
link sits between the mark and the sign-out form, and sign-out stays a form,
so the email link is the only `a` in the banner. The launcher's entries are
links too, and one of them may lead to auth, but they sit in the list of
services that follows the banner, not in it; with no launcher, the email is
the page's only link to auth.

dummy derives the space per request, from the request's own `Host`: strip a
trailing port if there is one, then strip a single leading `dummy.` label; what
remains is the space. The form's `action` is the scheme, `://auth.`, the
space, and `/logout`; the email link's `href` is the same with `/` in place of
`/logout`. When `Host` carries no `dummy.` label to strip — a developer on
`127.0.0.1:3000` or `localhost:3000` — both name auth's local origin instead:
the `action` is `http://localhost:3001/logout` and the `href` is
`http://localhost:3001/`. Those two values are facts about a sibling project
taken from auth's stories — auth, run bare, is served on `127.0.0.1:3001`
(`S2-serve`) at `http://localhost:3001`, its profile is fetched as
`http://localhost:3001/`, and its story of a sign-out from an app on the space
posts to `http://localhost:3001/logout` with the `Origin`
`http://localhost:3000` — not from reading auth's tree; nothing here names a
path inside auth, builds auth, or parses auth's output. A developer who wants
the button to work locally opens dummy as `http://localhost:3000`: auth's
stories accept `localhost` origins, not `127.0.0.1` ones, and that is auth's
rule to keep.

The scheme reading is deliberately strict and stays strict. It is
`X-Forwarded-Proto` only when that header is exactly `http` or exactly
`https`; anything else — `HTTPS`, a `https, http` list from a second proxy, a
stray space, an empty value, an absent header — yields `https`. Two reasons:
the header is attacker-influenced unless the gate rewrites it, so its bytes
must never reach a rendered `action` unvalidated; and a request whose `Host`
carries a space is by construction a deployed one, where `https` is the truth.
This must not be "simplified" into using the header whenever it is non-empty.
A request that names a space and says `http` gets an `http://auth.<space>`
action and `href`, as the stories fix; auth's stories refuse an `http` origin on a space,
which is harmless because a space is never served over `http`.

`LogoutURL` and `ProfileURL` are exported so each derivation is testable
directly, as a table of host and forwarded-proto against the resulting URL,
rather than only through a rendered page. The names follow what auth serves at
each address: the form posts to auth's `/logout`, and `LocalLogoutURL` is
auth's local one; the email link leads to auth's profile, and
`LocalProfileURL` is auth's local one. The two functions share the space and
the scheme and differ only in the path, and each is stated whole rather than
one defined through the other, so either can be read, and tested, on its own.
The button's label, `Sign out`, and its `logout` icon are appkit's markup
now, so dummy declares neither.

## Icons

The widget form's button carries the `plus` icon before its text
(`D07-form`). It is a Tabler outline icon as the platform style draws it
(`design/`, the Icons entry of its README): inline `svg` markup with the class
`ico` and `aria-hidden="true"`, so the icon is hidden from assistive
technology and the button's accessible text is its words alone. dummy emits it
as one fixed string, and `internal/panel` exports it as a constant,
`PlusIcon`, whose exact bytes are the contract. `D07-form` refers to
`PlusIcon` and restates none of it. The banner's icons — the launcher button's
`grid-dots` and the sign-out button's `logout` — are drawn by appkit's
template, and each service's icon in the launcher comes verbatim from the
services file; none of them is dummy's.

Pinning the bytes rather than the shape is deliberate. No Tabler file is in
dummy's tree, so a test can compare only against what the design states, and
exact bytes are the one statement a test decides with no reference outside
the sub-project. It is also what lets the foreign-content rule stay closed: an
`svg` is allowed in dummy's own markup only as this string, whose every
attribute is known to load nothing. The string follows the platform's own
inline form: Tabler's path data unchanged; the `class` Tabler ships replaced by
`ico`; no `width` or `height`, because the stylesheet sizes `svg.ico` (18
pixels, 16 inside a button); and without Tabler's invisible bounding-box path,
which the platform's markup omits too.

## The text procedures

dummy renders HTML, and most of its markup is not contract. The stories fix
what a reader sees, so the design fixes **visible text** and defines it as a
procedure the Go standard library can run. Where the design fixes markup — the
page's title, its stylesheet link and viewport, its heading and the wrapper
the stylesheet lays out, and the hooks of appkit's banner a test looks for —
it fixes exactly the tags and attributes it names, read by the same rules, and
nothing around them. It has to be the standard library: appkit is dummy's one
module dependency (`D01-layout-and-run-seam`) and carries no HTML parser, so
there is none available, here or in a test.

The procedures are stated **once, here**, for every document in this design.
`D06-table` reads a cell's text and `D07-form` reads a field error's text; each
of them refers to the statements below by name and restates no step of them. A
restatement would be a second contract, and the two would drift.

Two reading rules come first, because a procedure that says "the `form` start
tag" has to say what one is. A **start tag** for an element is a `<`, the
element's name, then a character that is neither an ASCII letter nor an ASCII
digit, then everything through the first `>` that follows; an **end tag** is
the same with a `/` after the `<`. The delimiter is the whole of it: without it
`<a` matches `<abbr` and `<form` matches `<formx`, so "exactly one `form` start
tag" would be a count of something else. The other rule is how an attribute is
read — its name preceded by whitespace and followed by `=`, its value in double
quotes, at most one occurrence in a tag, and character references unescaped in
the value the way the procedures below unescape them. The whitespace before the
name matters for the same reason the delimiter does: it keeps a scan for
`name=` out of `data-name=`. An attribute value is unescaped but not otherwise
normalised, because whitespace inside a raw echo is part of what was submitted.
Both rules are stated once here, and `D06-table` and `D07-form` cite them
rather than restating them.

The attribute rule's two clauses — at most one occurrence of each attribute a
requirement names, and every such attribute written with its value in double
quotes — are invariants over the bytes dummy sends, for every input dummy can
be given. They are not authoring guidance for the template's own literal
markup, and the difference is worth stating because the obvious implementation
gets it wrong. The standard library's `html/template` escapes `<`, `>`, `&` and
`"` inside a double-quoted attribute value but leaves `=` and whitespace alone
— observed, not recalled. So a raw echo rendered the obvious way puts a second
`value` occurrence on the name control the moment a caller submits a name like
`x" value="zzz`, and the same avenue reaches `name`, `id`, `type` and
`aria-describedby`, with any of space, tab, newline, carriage return or form
feed serving as the whitespace. The document's tag structure survives, because
the `"` is escaped and so no tag opens or closes — but "at most one occurrence"
does not survive, and the binary is non-conforming. Escaping `=` as well is the
simple fix; numeric-escaping the whitespace does the same job.

The banner's two URLs are that trap's mirror image, and they are no longer
dummy's to settle. The sign-out form's `action` and the email link's `href`
are URL contexts in appkit's template, and there `html/template` percent-encodes
rather than writing character references — observed against the published
module: a `ProfileURL` of `https://auth.a" id="x/` is drawn as
`https://auth.a%22%20id=%22x/`. So for a `Host` outside the ordinary alphabet
of host names the read value is not what `ProfileURL` or `LogoutURL` returns,
though no attacker got anything out of the page. dummy's contract is therefore
the data it hands the banner source, which is exact for every `Host`, and the
observation of the drawn banner is stated for a `Host` of ASCII letters,
digits, `.`, `-` and `:`, which is every host a space or a laptop presents.

One mechanical detail is worth recording, because it is easy to re-derive the
hard way and easy to get backwards. The second occurrence an injected value
creates is real, not an artefact: a read value's span is cut on literal `"`
bytes and only unescaped afterwards, so an escaped `&#34;` inside a value never
terminates that span, and the second occurrence reads on into the markup that
follows it. That is why nothing about `D07-form`'s counts of the form's
controls and its error anchors changes here, and why what forbids the rendering
is the attribute rule rather than the invariant that a caller's bytes
contribute no tag. The two guard different things: one guards a document's tag
structure, the other guards what can be read out of a single tag.

The procedures themselves are built from three definitions applied in a fixed
order. First the **script-stripped form**: every `script` and `style` element
is removed whole, from its start tag through its matching end tag. Then
**normalisation**: remove the remaining tags, unescape HTML character
references, and take the **whitespace collapse** — every run of whitespace down
to a single space, then trim. The whitespace collapse is named on its own
because the banner's email comparison needs that step and no other one. The
**visible text** of a document is the normalisation of what lies between its
`body` start tag and its `</body>` end tag.

Script-stripping runs first, and that ordering is the whole point of it. The
panel page carries an inline script (below). Script source is not a tag, so
without stripping it survives tag removal into the "visible text" — and worse,
a `<` inside it makes "remove each `<`…`>` span" swallow an
unpredictable run of real text up to the next `>`, so a page's visible text
becomes a function of its script's punctuation. The same step is what makes
tag counting sound: a script mentioning the literal `<table`, or a
`#widgets-table` selector, would otherwise be counted as markup.

Stripping is exact rather than best-effort because of two small constraints
dummy accepts: a script element's source contains no `</script` sequence, and
dummy's pages and its table fragment carry no `style` element at all — the
look comes from the linked stylesheet. The first is already required for the
page to parse as HTML; stating it makes "the first following matching end tag"
provably the element's real end.

Stripping also has to leave nothing behind that a second strip would find, and
that is required outright: no page's or fragment's script-stripped form
contains a `script` or a `style` start tag. So the stripping normalisation performs again,
inside the visible-text procedure, provably removes nothing — the redundancy is
an idempotent step rather than a second reading of the same body.

Unescaping is there because the banner renders an email address and the form
echoes an arbitrary submitted string, both of which a template escapes.
Without unescaping, a value containing `&` or `<` fails a comparison it must
pass — and the raw echo of attacker-chosen input is exactly where that shows
up.

Requirements over pages say the visible text **contains** a constant rather
than **equals** it, because a page's text also carries its link back to the
panel, and a whole body read with its banner carries the mark's text, the
caller's email and the sign-out button's label as well.

These body rules — the `style` ban, the `</script` ban — are stated over what
dummy writes, never over what appkit writes. Every answer to an appkit path is
appkit's (`D08-assets`), and a stylesheet or a font may hold any bytes at all,
`<style` included, so each rule leaves every such answer out. Inside a page
the same line is drawn around the appkit banner (below): its markup is
appkit's, and each launcher entry's icon is the services file's SVG text
inserted verbatim, which may use single-quoted attributes or anything else SVG
allows. So each rule is read over a page's **written markup**, the page with
its appkit banner taken out. The attribute rule's "markup dummy sends" has the
same reach: it means the written markup of the HTML documents dummy sends and
the table fragment, never appkit's bytes.

The frame a count is taken over is the script-stripped form by default, and any
requirement may name the frame it counts over instead, in which case the frame
it names governs. One exception sits inside the default: a requirement that
counts `script` start tags without naming a frame means the raw body, because
that is what such a count is always for. The exception is scoped to the default
branch rather than applied globally, and that scoping is load-bearing — a
global one would reach the requirement below that bans a `script` start tag in
the *stripped* form, send that count to the raw body instead, and contradict
the panel page's exactly-one-`script`-in-the-raw-body.

Naming a frame earns its keep on the `style` and `script` elements, because
stripping removes those two elements whole. A ban read over the stripped form
is therefore not a ban on dummy sending such an element at all — stripping has
already taken away any the raw body really held — it is a ban on one the
stripping itself *synthesized*, by joining what lay on either side of a
removal, the way a `<sty` before a script element joins an `le>` after it. That
is a real obligation and a different one, not a vacuous restatement, which is
why dummy states both: the raw-body ban says no page or fragment carries a
`style` element,
and the stripped-form ban says a second strip finds nothing left to remove.
Read either over the other's frame and one of the two is lost.

## The banner, the two failure shapes and the panel page

Everything in this section is said about **an HTML document dummy sends**, and
that class is named rather than left implicit because it has to exclude two
things. `D06-table`'s table fragment travels with exactly the `Content-Type` a
page does, and that document forbids it a doctype, an `html` element and a
`body` element. A rule quantified over "every `text/html` body" would therefore
demand of the fragment precisely what the fragment is defined not to have, and
no implementation could satisfy both — the contract would be unsatisfiable.
And every answer to an appkit path is appkit's (`D08-assets`), whatever type
it carries. So the class is every non-empty `text/html; charset=utf-8` body
`Handler` sends **except** the ones answering `/widgets/table` or an appkit
path. Naming those paths here says nothing about how they are answered — that
is `D06-table`'s and `D08-assets`'s — only that whatever they send is not one
of these documents. A path under `/assets/` is no longer special in any way:
it is an unknown path, and its 404 is an ordinary HTML document.

Every HTML document dummy sends is **drawn with the banner**: right after the
body's start tag it holds the **appkit banner** for the request it answers —
exactly what appkit's `banner` template writes for the banner data the banner
source returned while that request was answered. `Handler` calls the banner
source once per page, with the **banner user** for the request: the email
`X-User-Email` carried and the two URLs `ProfileURL` and `LogoutURL` derive
from its `Host` and `X-Forwarded-Proto`. It calls the source afresh for every
page and never keeps what an earlier call returned, so when the kit's services
file changes the next page shows the change; and it never calls the source for
an answer that draws no banner — the missing-identity 500, the fragment, the
shared files — so those never cause a read of the services file. This is the
whole of dummy's side of the banner, and it is stated exactly: the data in,
the one call, and the placement of appkit's output byte for byte. How appkit
draws that data is appkit's; which template dummy's pages are parsed with to
get there is the build run's.

What appkit draws, as `appkit.Templates()` in the published module draws it,
is the `header` element that is the body's first child, its WAI-ARIA banner
landmark: the **mark**, a `strong` of class `mark` reading `ikigenba` whose
`data-service` carries the banner data's service name, from which the
stylesheet draws `ikigenba │ dummy`; then, when the banner data lists
services, the **launcher button**, a `button` of class `launcher` labelled
`Services`; then the **email link**, an `a` whose `href` is the profile URL
and whose text is the email; then the **sign-out form**, a `form` POSTing to
the logout URL around a submit `button` reading `Sign out` behind an
`aria-hidden` `logout` icon. When there are services, the header is followed
by the **list of services**, a `nav` with id `services`, labelled `Services`
and carrying the `popover` attribute, so a browser keeps it closed until the
launcher button opens it; it holds the search field, an `input` of type
`search` labelled and placeholdered `Find a service`, one `a` per service in
the data's order — an enabled one's `href` is its URL; a switched-off one has
no `href`, is `aria-disabled` and titled `<name> is unavailable`; the current
one is `aria-current="page"`; each holds the service's icon verbatim and then
its name — and the no-match line, a `p` carrying `hidden`, reading
`No service matches` and then an empty quotation the script fills. Last comes
the one `script` element loading `/_appkit/launcher.js`. With no services
there is no launcher button, no list and no script. These are the hooks a
test looks for, and the requirements name them; they restate no more of the
markup than that.

dummy's tests cannot build the kit — `appkit.New` reads the real environment,
which no test may touch — so they observe the banner through an **echoing
banner source**: a function returning the banner user's three values with
`ServiceName` and a list of services the test chose. That is exactly what the
kit's `Banner` method returns, with the services its file lists and the one
named like the service marked current (observed against the published module:
an entry named `dummy` came back with `Current` true, an entry lacking a field
was left out, and a missing file or an unset variable gave no services). So
the requirements over an echoing source are the stories' outcomes as dummy's
handler can show them, and they hold through any appkit release only while
that release still draws these hooks — the point of stating them, as
`D08-assets` does for the shared files, is that dummy's tests fail if one
stops. The observation of the drawn entries is stated for **plain services**,
names of the alphabet a services file actually uses, URLs that are empty or
plain `http://` or `https://` addresses, and icons that are a bare `svg` with
no text, like the stories' fixture, so that nothing a test chooses can read as
a second entry or a stray tag. The scheme matters: `html/template` replaces a
URL whose scheme is not `http`, `https` or `mailto` — `javascript:x`, say, or
`ftp://h/` — with `#ZgotmplZ` in an `href`, so the entry would not link where
the data said.

The banner is found without a parser by position: it is the `header` start
tag immediately after the `body` start tag, through the first `</header>`
after it. That is exact because the header holds no second `header` — appkit
draws none — and it is what makes "the first `</header>`" the right end: the
form card (`D07-form`) has a `header` of its own, and it sits later in the
page, inside the wrapper. The banner holds exactly one `a` and one `form`, so
the one link in the banner leads to auth's profile and sign-out is never a
link. The banner is a defined term rather than a passing phrase because the
observations are stated over it, and `D07-form` places the widget form
outside it.

A page's **written markup** is the page with its appkit banner taken out: the
one stretch of the body that dummy's own markup did not produce. Every rule
this design states over a page's markup — the head, the frame, the counts, the
attribute and tag rules, the script rules — is read out of the written markup,
because appkit's bytes are not dummy's to guarantee and a service's icon is
the services file's. The appkit banner is the first thing in the body; the
written markup's body then holds exactly one thing, one `main` element, and
everything that is the page's own — the panel's heading block, table and form
card, or a failure page's message and its link back — is inside it, so the
content sits in the stylesheet's centred column under the banner rather than
running the width of the window. What lies between the `main` start tag and
its end tag is the page's **page content**, a defined term so that `D07-form`
can place things in it without restating the frame. The frame is stated once,
over every HTML document dummy sends, so the 404, the 405s, the 415 and the
422 redraw all have it; the plain-text 500 is not an HTML document and has
neither the banner nor the `main`.

`ServiceName` is lowercase, `dummy`, and so is every other place the service's
name appears: the page's title is `ServiceName` too, the kit `main` builds is
named with it, and no document carries the name in any other casing. The only
way a differently cased `dummy` can reach a page is inside a value a caller
sent or a widget's name, so the rule is stated over requests and stores that
carry none; dummy does not rewrite a caller's bytes.

The email link's text is compared against the **whitespace collapse** of what
`X-User-Email` carried and not against its raw bytes: normalisation collapses runs of
whitespace, so an address carrying two spaces would otherwise fail against a
page that rendered it perfectly. It is not compared against the full
normalisation either, which would be worse than the raw value — normalisation
unescapes, so a header carrying the four characters `&lt;` would normalise to
`<`, while the page renders those four characters and the email's normalised
text hands them back unchanged, and `&lt;` is not `<`, so a comparison against
the normalisation would fail on a page that rendered the address perfectly.
Collapsing whitespace is exactly the difference the comparison needs, and
nothing else is.

Because the banner is drawn from the caller's identity, a failure dummy can
name to an identified caller is itself a page with that same banner, with a way
back to the panel; only the missing-identity fault, where there is no identity
to draw with, is bare text. So there are two failure shapes, and they are
defined here as shapes. Which shape a given route uses is stated by whoever
owns that route: the 404 and the two 405s below, `D07-form` for the 415, and
`D06-table` for its own route, which is plain always — the deliberate
exception, because the panel's script splices that response into a live
document and a whole page with the banner pushed into a table element is exactly what
must not happen.

A **panel page** is a document shape, not a route, and it is a shape a
requirement *assigns*: a body is a panel page because some requirement says
that body is one — the 200 on `GET /widgets` here, the 422 body in `D07-form` —
never merely because it happens to look like one. What this document owns of
the shape is that a panel page is an HTML document dummy sends and is drawn
with the banner, and how its parts are composed (below); what those parts hold
belongs to the documents that own them, `D06-table` for the widgets table and
`D07-form` for the widget form and its card, each stating its obligation over
every panel page. That is why
membership is assigned rather than tested: were it decided by the banner and
content type alone, the 404 page would qualify and would then be required to
carry a table and a form. Naming the shape is what lets `D07-form` say that its
422 body is a panel page without either document leaving the 422's table
unstated.

## The head every page carries

Every HTML document dummy sends — the panel page, the 404, both 405s, the 415
and the 422 redraw — carries the same three things before its body: a `title`
whose text is `ServiceName`, one stylesheet link whose `href` is
`/_appkit/theme.css`, the platform stylesheet appkit serves through dummy
(`D08-assets`), and the phone-width viewport declaration,
`width=device-width, initial-scale=1`. The head is dummy's markup, not
appkit's: appkit's template draws only the banner and the launcher. They are stated once, over the class, so no page can be the one that
forgot. The table fragment is not in the class and carries none of them: it
is spliced into a page that already has them. The plain-text 500 carries
none either, which is the point of it — a request without identity is sent
no stylesheet.

The link is root-relative, so it resolves against whichever host served the
page: `127.0.0.1:3000` on a laptop, `dummy.<space>` on a host, where it is the
`https://dummy.<space>/_appkit/theme.css` of `S7-on-a-space`; the launcher's
script is root-relative in appkit's markup the same way. That is also
the one part of "a page makes no request to any third party" that a page's
own markup can decide, and the design closes it route by route rather than
claiming more than a standard-library test can see. The common attributes a
plain HTML element loads a resource from — `href` and `xlink:href` on anything
but an `a`, `src`, `poster`, `data`, `background`, `manifest`, and every
candidate of a `srcset` or an `imagesrcset` — name a path on dummy's own host.
That list is not every attribute some browser fetches from, and no rule here
tests for one outside it: what keeps such an attribute off dummy's pages is
that dummy's markup is written with none, and the escaping of an echoed value
(below) that keeps a caller from adding one. A path on
dummy's own host means `/` alone or `/` followed by anything but a second
slash or a backslash, read after tabs and newlines are dropped: a browser's
URL parser deletes those characters and treats `\` as `/` in an `http` or
`https` URL, so `/\host` and a `/` split from a `/` by a tab both name another
host. An `iframe`, an `object` or an `embed` loads from those same
attributes, so the path rule already keeps each of them on dummy's host; the
one way an `iframe` gets a document without loading one is closed below.

Further routes are closed outright rather than checked, because dummy's markup
needs none of them and the stylesheet does all the styling. No start tag
carries a `style` attribute, whose `url(...)` would fetch from wherever it
points, and none carries `ping`, which reports a followed link to any host it
lists. None carries an event-handler attribute — a name beginning `on` —
whose value is script run in the page; none carries `srcdoc`, which writes a
whole document into an inline frame where no rule here reads it; and none
carries `http-equiv`, whose refresh form sends the page to whatever URL its
`content` names. The viewport declaration is a `meta` with `name`, not
`http-equiv`, and a page's encoding travels in its `Content-Type` header, so
no page needs one. And dummy's own markup carries no foreign content beyond
the one icon: foreign content can name a resource through presentation
attributes and animation that no attribute list here reads, so a `math`
element is banned outright and an `svg` is allowed only as `PlusIcon`, the
fixed string above, whose attributes load nothing. A `script` start tag in
dummy's own markup carries no `src`, `href` or `xlink:href` — the last two are
how an SVG script names its source — which is the inline poller's no-`src`
rule (below) stated for every page, the 404, the 405s and the 415 included.
The launcher's `script`, which does carry a `src`, is appkit's and sits in the
appkit banner.

Every one of these rules is read over the **raw** written markup, not its
script-stripped form, and that choice is what makes them hold. Stripping cuts
from anything that looks like a `<script` start tag to the next `</script>`,
and a `<script` inside a comment, inside an attribute value, or at the front
of a custom element's name such as `script-x` looks like one while opening no
script at all; read over the stripped form, a rule would never see the real
markup such a fake cuts away. Over the raw body nothing is cut, so nothing can
hide. The cost is that a script's source is scanned too, which is harmless:
at worst a stray `<` in the source reads as a tag and trips a rule on a page
that fetches nothing, so the poller is written with no `<` followed by a
letter, and the literals its requirement asks for contain no `<` at all.

Reading raw also needs each tag read the way a browser reads it, and the
occurrence rule alone does not do that: a browser also splits attributes at a
`/`, allows whitespace around `=`, and keeps a `>` inside a quoted value as
part of the value, so `<script/href=...>`, `<img src ="...">` and
`<img alt="a>b" onerror="...">` would each carry an attribute the occurrence
scan never sees. So every tag dummy sends is **well formed**: every `<`
followed by a letter opens a tag of a name, then attributes each preceded by
whitespace, each either bare or written `="..."` with no `"`, `<` or `>`
inside the value, then an optional `/`, then `>`. On such a tag the first `>`
is the browser's end of it, and every attribute the browser reads is one the
occurrence rule reads. A bare attribute carries no URL and no script, so the
rules above need not look at it. The requirement is over what dummy writes,
and it is also what the standard library's escaping already produces for a
caller's value inside a double-quoted attribute: `<`, `>` and `"` all come out
as character references.

What no rule here reads is the inside of a script element. The poller's source
could fetch from any host and no gate would notice, because the gates have no
JavaScript engine; its requirement (below) fixes only what a standard-library
test can see of it. That the poller fetches nothing but `/widgets/table` is
the author's obligation, not a tested claim.

Only `a` is exempt from the path rule, because following a link is navigation,
not a load; the one link dummy's own markup carries, back to the panel, is
`/widgets` anyway. A form's `action` is outside the list because it is a
submission. The banner's absolute links to auth, the launcher's links to the
other services and the launcher's `script` with its `src` are all inside the
appkit banner, which these rules do not read: they are read over the written
markup, and appkit's markup loads nothing from another host but what the
services file's icons name, which is the host's to decide. Whether the
stylesheet itself loads anything from elsewhere is a question about the
file's contents, which no design states; it is not decided here.

These rules scan whole start tags, and a caller's bytes can sit inside one:
the 422 echoes a submitted name into an attribute value. A raw echo of
`x style=y` or `x onclick=y` would put a banned attribute into that tag, the
same way `x" value="zzz` puts a second `value` there, and the escaping that
settles the one — `=` written as a character reference — settles the others.

## The panel page's composition

The panel page has three parts in a fixed order: the banner, then, as
the whole of its page content, the page's **heading block** and the **panel
wrapper**, a `div` of class `panel` holding the widgets table (`D06-table`) and
then the form card (`D07-form`) and nothing else. The platform's stylesheet lays the
wrapper's two children side by side on a wide screen and stacks them on a
narrow one, table first either way. Which of those happens at 960 pixels is a
fact about a layout engine, which the gates do not have, so the contract is
the markup the stylesheet reads: the wrapper, its class, and its two children
in order. The narrow layout's "the form below the table" is document order,
which holds in both layouts and needs no rule of its own: the wrapper holds
the table span and then the form card, and the single table and the widget
form each sit inside their own part. (The page's other form, the sign-out form,
is in the banner, outside the page content.)

The wrapper is found without a parser by counting: from its `div` start tag,
its end is the first `</div>` at which the `div` end tags seen balance the
`div` start tags seen. That is exact whatever the table and the card hold.

The heading block is the platform's section head: a `div` of class
`section-head` holding a `div` that holds the `h1` reading `Widgets` and a `p`
beneath it, the **panel subtitle**. The subtitle counts the widgets and says
how often the table refreshes — `3 widgets · refreshes every 5 seconds`,
`1 widget · …` for exactly one, `0 widgets · …` for none — and the count is the
number of rows the page's own table shows, so the two can never disagree on a
page, whatever a concurrent creation does. The block is fixed byte for byte,
because nothing in it comes from a caller: the only variable is a number. It is
drawn when the page is rendered and sits outside the table, so the fragment
(`D06-table`) never carries it and a poll never changes it; it catches up on
the next load. Because it is stated over every panel page, the 422 redraw
carries it too, counting the widgets as they are, and `D07-form` restates
nothing of it.

The page content holds the heading block and the wrapper and nothing else:
nothing can sit before the heading, between the heading and the wrapper, or
after the wrapper, so a footer, a skip link or a notice has nowhere to go. The
stories show no other text on the page, and that is meant to be decidable.

## A caller's bytes never become markup

Three kinds of caller-supplied value reach a page: the email from
`X-User-Email`; the email link's `href` and the sign-out form's `action`,
which `ProfileURL` and `LogoutURL` derive from `Host` and `X-Forwarded-Proto`;
and — on a 422 — the name, the count and the status exactly as they were
submitted. The first two reach it through the banner user and appkit's
template, the last through dummy's own. The two URLs belong in that list on
their own account: a `Host` of `dummy.a<table` reaches the rendered link and
form, and leaving them out would mean that value was policed by nobody. What becomes of a `<` in one of them has to be contract:
an implementation that escaped `&` and `"` but not `<` would meet every other
requirement in this design while a submitted name of `<table id=x></table>`
produced a page carrying two tables. Such a page breaks the table identity `D06-table`
requires, the single-`body` count here and the `form` count in
`D07-form`, and any caller who can submit the form can reach it. It is a
correctness hole and a stored-scripting hole at once.

What is contract is the invariant and not the mechanism: no value a caller
sends may contribute a tag, so a document's tag structure does not depend on
anything a caller sends. Escaping while rendering is how an implementation gets
there — dummy's templates for the echo, appkit's `html/template` for the
banner — and the design fixes the observable end over the whole page, appkit
banner included, drawn from an echoing banner source, and leaves the means
alone. It is decidable with the standard library: submit a name full of markup, submit an
innocuous name that fails validation the same way, and compare the two pages'
tag-name sequences and their counts of `>`. Both halves are needed: the
sequence catches a `<` that opens a tag nobody meant, and the count catches an
unescaped `>` inside a quoted value, which ends a start tag early and would let
the document's tag extents follow the caller too. The comparison holds the
widget set fixed as well, since a page renders the store and a creation
between the two reads would move the tag sequence with no caller value doing
it. The value itself still has to arrive — the banner's email in the visible
text, the echoed name in an attribute — and the unescape step in normalisation
is what makes those comparisons come out right.

## The inline polling script

The panel keeps itself fresh by re-fetching the table fragment every 5 seconds
and swapping it into the page. The script that does this is **inline** in the
panel page, with no `src`. The files under `/_appkit/` are appkit's
(`D08-assets`), the same for every app, and the launcher's script there is the
platform's; the poller is dummy's own behavior, and inline keeps it in the
page this document describes. The page's scripts only act on what the server
sent: the poller replaces the table with the fragment the server renders, and
the launcher's script filters the list of services the page already holds, so
no script adds content of its own. That the launcher's script does no more is
appkit's to keep; no gate here runs either script.

It sits **outside** the table element. If it were inside, the first swap would
delete it and the page would poll exactly once. That placement is read over the
**raw** written markup, and it has to be: over the script-stripped form the script is
gone, and the clause would be vacuously true of a page whose poller sits
squarely inside the table. Reading it raw is sound because a script element's
start tag always precedes whatever its source mentions, so a `<table` written
inside the script can never make a conforming page fail.

The polling interval is contract: 5 seconds, which is what the panel subtitle
tells the reader, on the panel page and the 422 redraw alike. No gate runs the
script, so the interval is fixed as what a standard-library test can see — the
script's source carries the literal `5000`, the interval in milliseconds.

The requirement over the script is **deliberately weaker than the behavior**,
and saying so here is part of the design rather than an apology for it.
"Re-fetches and swaps" cannot be decided without a JavaScript engine, and there
is none available: appkit is dummy's one module dependency
(`D01-layout-and-run-seam`), and neither it nor the standard library carries
an HTML parser or a JavaScript engine. So what is fixed is
what a standard-library test can see: exactly one script element in dummy's
written markup (the launcher's is appkit's, in the appkit banner), no `src`
attribute, and a source carrying the literal `/widgets/table`, the literal
`widgets-table` and the literal `5000` — the path it polls, the anchor
`D06-table` puts on the table it replaces, and the interval. Anything weaker does not say the page polls at all; anything
stronger is not testable here. A reader who mistakes this for an oversight will
try to "fix" it and will end up writing a requirement no gate can run.

## Routing

Identity first, then path, then method. Path matching is **exact** on the URL's
path component and nothing else — not the query, not the host, not a header.
`/widgets/` and `/widgets/table/` are unknown paths and take the 404; dummy
never answers a trailing-slash variant with a 301. That has to be a
requirement rather than an assumption, because Go's `http.ServeMux` would
otherwise supply a redirect nobody designed and a test that never asks would
never notice.

The table is total. `/` answers `GET` and `HEAD` with a 303 to `/widgets` and
every other method with a 405 naming `GET, HEAD` — no story covers a bad
method on the root, and leaving the cell empty is how a 404 nobody meant
appears there. `/widgets` answers `GET` and `HEAD` with the panel page,
`POST` as `D07-form` defines, and every other method with a 405 naming
`GET, HEAD, POST`. `/widgets/table` is answered as `D06-table` defines; all
this document says about it is that it is not an unknown path, which is
exactly enough to keep routing total without owning a fragment-route fact. An
appkit path, as `D08-assets` defines it, is answered by appkit as that
document says — including appkit's own 404 for a name appkit does not serve,
such as `/_appkit/banner.html`, and its 405 — and this document says nothing
more of it. Every path that is neither one of the three routes nor an appkit
path is a 404 with the banner, whatever the method — `/assets/theme.css`,
`/assets/` and anything else under `/assets/` included, since dummy serves
nothing there any more. There are two 404s, then, and they do not overlap: an
appkit path takes appkit's, every other unknown path takes this one.

A `HEAD` is answered exactly as the corresponding `GET` would be — same
status, same headers the handler sets, empty body — on every route, the root's
303 included. It is stated once rather than per route.

Reading never mutates. Every read route's postcondition in the stories asserts
it, so it is a fixed outcome and not an assumption; `D06-table` states the same
invariant for its route and `D07-form` for its two refusals.

## What the handler does not depend on

The handler's responses do not depend on the process's working directory or on
any file outside the binary — dummy's templates are embedded, and appkit
embeds its own and the shared files (`D01-layout-and-run-seam`,
`D08-assets`). The one file a page does depend on is the host's services
file, and the handler never reads it: the banner source does, in `main`'s kit.
So the rule is stated for a banner source that reads no file, and tested by
driving two handlers over the same such source with the working directory set
to an empty temporary directory and to the checkout.

An earlier iteration of the design required that `internal/server` declare no
package-level `var`; that rule does not survive: its purpose was to prove the handler held no state, and the handler
now deliberately holds a store. `internal/panel` is free to keep the idiomatic
parsed-template package variable. Nothing here replaces it: that two stores do
not share widgets is a property of the store type, so `D05-widgets` states it
over `internal/widget`, where it is decided by calling `All` rather than read
back out of a rendered page.

## The launcher stories: dummy's part and appkit's

`S3-panel`'s launcher stories rest on two halves. dummy's half is stated
here and tested with an echoing banner source: every page calls the source
afresh with the banner user; the page carries appkit's rendering of what came
back, right after `<body>`; with services the banner holds the launcher
button, the list of services follows it with the search field, one entry per
service in order — enabled a link, switched off titled unavailable and not a
link, current marked `aria-current` — and the hidden no-match line, and the
page loads `/_appkit/launcher.js`; with none there is no button, no list and
no script; and none of it writes to stderr, since a 200 writes nothing.

appkit's half is covered by the wiring and not by dummy's handler tests,
because a test may not read the real environment `appkit.New` reads:
`main` builds the kit with `appkit.New(ServiceName)` and hands its `Banner`
method down unchanged (`D01-layout-and-run-seam`, `D03-serve`). Reading
`IKIGENBA_SERVICES` once at start; reading the file afresh on every call, so
that a rewritten file shows on the next page without a restart; answering an
unset variable, a missing, unreadable or malformed file, or no usable entry
with no services and no output; leaving out an entry that lacks a member; and
marking the entry named `dummy` current are the kit's `Banner`, observed
against the published module. That the list is closed on load and opens from
the button is the browser's reading of appkit's `popover` markup; filtering
as the user types, the no-match line's text, and Enter opening the first
entry are `/_appkit/launcher.js`, which no gate runs. `S7-on-a-space`'s
launcher story is those same outcomes on a live space, where the host sets
`IKIGENBA_SERVICES` and lists dummy because its package ships
`share/icon.svg` (`D01-layout-and-run-seam`); like the rest of `S7`, it is an
acceptance of a deployment and adds no requirement here.

## REQUIREMENTS

- R-ARNQ-LC87: The `internal/panel` package MUST export `func Handler(s *widget.Store, banner func(u appkit.User) appkit.Banner, stderr io.Writer) http.Handler`, where `appkit` is the package `github.com/ikigenba/ikigenba/appkit`; the function passed as `banner` is the handler's **banner source**.
- R-XH4O-AY7C: The `internal/panel` package MUST export `const ServiceName = "dummy"`.
- R-LBS0-ECOZ: The `internal/panel` package MUST export `const MissingIdentityBody = "identity header missing\n"` and `const MethodNotAllowedBody = "method not allowed\n"`.
- R-LCZW-S4FO: The `internal/panel` package MUST export `const NotFoundMessage = "That page was not found."`, `const MethodNotAllowedMessage = "That method is not allowed here."` and `const UnsupportedMediaTypeMessage = "That media type is not supported."`.
- R-V4W1-QTA8: The `internal/panel` package MUST export `const LocalLogoutURL = "http://localhost:3001/logout"`.
- R-V63Y-4L0X: The `internal/panel` package MUST export `func LogoutURL(host, forwardedProto string) string`.
- R-V7BU-ICRM: `LogoutURL` MUST return, for arguments `host` and `forwardedProto`: let `h` be `host` when `host` contains no `:`, and otherwise `host` with its last `:` and every character following that `:` removed; when `h` begins with the six characters `dummy.` and at least one character follows them, the result is `<scheme>` then `://auth.` then those following characters then `/logout`; otherwise the result is exactly `LocalLogoutURL`; where `<scheme>` is `forwardedProto` when `forwardedProto` is exactly `http` or exactly `https`, and is `https` in every other case, the empty string included.
- R-1A0D-766L: The `internal/panel` package MUST export `const LocalProfileURL = "http://localhost:3001/"`.
- R-1B89-KXXA: The `internal/panel` package MUST export `func ProfileURL(host, forwardedProto string) string`.
- R-1DO2-CHEO: `ProfileURL` MUST return, for arguments `host` and `forwardedProto`: let `h` be `host` when `host` contains no `:`, and otherwise `host` with its last `:` and every character following that `:` removed; when `h` begins with the six characters `dummy.` and at least one character follows them, the result is `<scheme>` then `://auth.` then those following characters then `/`; otherwise the result is exactly `LocalProfileURL`; where `<scheme>` is `forwardedProto` when `forwardedProto` is exactly `http` or exactly `https`, and is `https` in every other case, the empty string included.
- R-ASVM-Z3YW: The `internal/panel` package MUST export `const PlusIcon`, an untyped string constant whose value is exactly `<svg class="ico" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5l0 14"/><path d="M5 12l14 0"/></svg>`, the Tabler outline `plus` icon as the platform style draws it inline.
- R-KDGH-2SDG: dummy's design defines, for an element name `x` and a string `s`, an **`x` start tag** in `s` — written `<x` start tag where that reads better, and meaning the same span — as a span beginning with a `<`, then `x` compared case-insensitively, then a character that is neither an ASCII letter nor an ASCII digit, and running through the first `>` that follows that `<`; and an **`</x>` end tag** in `s` as a span beginning with a `<`, then a `/`, then `x` compared case-insensitively, then a character that is neither an ASCII letter nor an ASCII digit, and running through the first `>` that follows that `<`; a `<` that no `>` follows begins neither; and every requirement in dummy's design that names a start tag or an end tag of an element MUST denote such a span.
- R-LX4G-PAXO: dummy's design defines an **occurrence** of a named attribute in a start tag as that attribute's name compared case-insensitively, immediately preceded within that start tag by an ASCII whitespace character and immediately followed by `=`, and the **read value** of that occurrence as the characters between the double quote that follows that `=` and the next double quote, with HTML character references unescaped by `html.UnescapeString` from the Go standard library and with nothing else altered; in the markup dummy sends every attribute a requirement in dummy's design names MUST be written that way with its value enclosed in double quotes, and a start tag MUST carry at most one occurrence of each attribute a requirement in dummy's design names; and every requirement in dummy's design that names an attribute's read value MUST denote that result.
- R-KFW9-UBUU: dummy's design defines the **whitespace collapse** of a string `s` as `s` with every run of one or more whitespace characters replaced by a single space and with leading and trailing whitespace then removed, and every requirement in dummy's design that names the whitespace collapse of a string MUST denote that result.
- R-KH46-83LJ: dummy's design defines the **script-stripped form** of a string `s` as the result of this procedure, and every requirement in dummy's design that names the script-stripped form MUST denote that result: scanning `s` from the left, repeatedly find the earliest `script` start tag or `style` start tag and delete every character from that start tag's `<` through the last character of the first `</script>` end tag or `</style>` end tag respectively that follows that start tag; when no such end tag follows, delete every character from that `<` through the end of `s`.
- R-KIC2-LVC8: dummy's design defines the **normalisation** of a string `s` as the result of these steps applied in this order, and every requirement in dummy's design that names the normalisation of a string MUST denote that result: take the script-stripped form of `s`; remove every tag, meaning that, scanning from the left, the span from the earliest remaining `<` through the first `>` that follows that `<` is removed, and the span from a `<` that no `>` follows through the end of the string is removed, until no `<` remains; unescape HTML character references with `html.UnescapeString` from the Go standard library; take the whitespace collapse of the result.
- R-AU3J-CVPL: dummy's design defines **an HTML document dummy sends** to be a non-empty response body `Handler` sends with the header `Content-Type: text/html; charset=utf-8` in answer to a request whose path is neither `/widgets/table` nor an appkit path (`D08-assets`), and the **written markup** of such a body to be that body with the occurrence of the appkit banner that its being drawn with the banner places in it removed, or that body unchanged when it is not drawn with the banner; every requirement in dummy's design that names an HTML document dummy sends, or a body that is one, a panel page included, MUST denote such a body; and wherever such a requirement reads anything out of that body — its raw body, its script-stripped form, its visible text, its page content, a tag, a count of tags or an attribute — other than whether it is drawn with the banner, and other than where the requirement says it reads the whole body, it MUST read it out of that body's written markup; and every requirement in dummy's design that names the markup dummy sends MUST denote the written markup of every HTML document dummy sends together with every other response body `Handler` sends in answer to a request whose path is not an appkit path.
- R-LYCD-32OD: Every HTML document dummy sends MUST, after optional leading whitespace, begin with `<!doctype html>` compared case-insensitively, and its script-stripped form MUST contain exactly one `body` start tag and exactly one `</body>` end tag.
- R-KLZR-R6KB: dummy's design defines the **visible text** of a string `s` as the normalisation of the characters lying between the `>` of the first `body` start tag in the script-stripped form of `s` and the `<` of the first `</body>` end tag following that start tag in that same form, and as the empty string when either of those tags is absent; and every requirement in dummy's design that names a document's visible text MUST denote that result.
- R-RMP2-ZITR: Wherever a requirement in dummy's design fixes how many start tags or end tags of a named element a document contains and does not itself name the form of that document the count is taken over, the count MUST be taken over that document's script-stripped form, except where the requirement names the `script` element itself, whose count MUST be taken over the raw body; and wherever such a requirement does name the form the count is taken over, the count MUST be taken over the form it names.
- R-B8QB-Y4LX: The source of every `script` element — the characters between that element's start tag and its end tag — in every response body `Handler` sends in answer to a request whose path is not an appkit path (`D08-assets`), read out of that body's written markup when that body is an HTML document dummy sends, MUST NOT contain the sequence `</script` compared case-insensitively.
- R-B9Y8-BWCM: Let a **written body** be any response body `Handler` sends in answer to a request whose path is not an appkit path (`D08-assets`), taken to be that body's written markup when that body is an HTML document dummy sends; the raw text of every written body MUST NOT contain a `style` start tag, so dummy writes no `style` element in any page or fragment; and the script-stripped form of every written body MUST NOT contain a `script` start tag and MUST NOT contain a `style` start tag, so that taking the script-stripped form of that form again removes nothing from it; neither half implies the other, because stripping removes a `style` element the raw body really held, and because a removal can join a `<sty` preceding a `script` element to an `le>` following it and so put a `style` start tag in the form that the raw body never held.
- R-1EVY-Q95D: dummy's design defines the **banner** of a string `s` as the span of the script-stripped form of `s` running from the `<` of the `header` start tag whose `<` follows the `>` of the first `body` start tag in that form with nothing but ASCII whitespace between them, through the `>` of the first `</header>` end tag following that `header` start tag; a string has no banner when no `header` start tag so follows or no `</header>` end tag follows it; and every requirement in dummy's design that names a banner MUST denote that span.
- R-AVBF-QNGA: dummy's design defines the **banner user** for a request as the `appkit.User` whose `Email` is the value `r.Header.Get("X-User-Email")` returns for that request, the empty string when it carries none, whose `ProfileURL` is the value `ProfileURL` returns for that request's `Host` header and its `X-Forwarded-Proto` header, and whose `LogoutURL` is the value `LogoutURL` returns for those same two headers; and every requirement in dummy's design that names the banner user MUST denote that value.
- R-AWJC-4F6Z: dummy's design defines the **appkit banner** for a request `Handler` answers as the text that executing the template named `banner` in the set `appkit.Templates()` returns writes when its data is the `appkit.Banner` value returned by the call to its banner source that `Handler` made while answering that request; and every requirement in dummy's design that names the appkit banner MUST denote that text.
- R-AXR8-I6XO: dummy's design defines a response body to be **drawn with the banner** for the request it answers when, read as sent, it contains the appkit banner for that request beginning immediately after the `>` of its first `body` start tag, with nothing but ASCII whitespace between them; that occurrence is the one its written markup leaves out; and every requirement in dummy's design that names being drawn with the banner MUST denote that property.
- R-AYZ4-VYOD: `Handler` MUST call its banner source exactly once while answering each request it answers with an HTML document dummy sends, with the banner user for that request as its argument, and MUST NOT call it while answering a request it answers with status 500 because its `X-User-Id` header is absent or empty, a request whose path is `/widgets/table`, or a request whose path is an appkit path (`D08-assets`); so that every page is drawn from banner data fetched for that page and never from data fetched for another request.
- R-85Q6-Z7GW: dummy's design defines, for a slice `svc` of `appkit.Service` values, the **echoing banner source** for `svc` as a function of type `func(u appkit.User) appkit.Banner` that, called with any `u`, returns `appkit.Banner{Service: ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Services: svc}`; and a **plain service** as an `appkit.Service` whose `Name` is non-empty and consists only of ASCII letters, ASCII digits, `.`, `-` and `_`, whose `URL` is empty or is `http://` or `https://` followed by characters each of which is an ASCII letter, an ASCII digit or one of `:/.-_~`, and whose `Icon` is empty or is one `svg` element — an `svg` start tag through an `</svg>` end tag that ends the string — every `<` of which begins a start tag or an end tag of `svg`, `g`, `path`, `circle`, `rect` or `line` and whose normalisation is empty; and every requirement in dummy's design that names an echoing banner source or a plain service MUST denote such a function or such a value.
- R-B1EX-NI5R: dummy's design defines a **bare occurrence** of a named attribute in a start tag as that attribute's name compared case-insensitively, immediately preceded within that start tag by an ASCII whitespace character and immediately followed by an ASCII whitespace character, a `/`, or the `>` that ends that start tag; and every requirement in dummy's design that names a bare occurrence MUST denote that.
- R-B2MU-19WG: For a `Handler` built over an echoing banner source, every HTML document dummy sends in answer to a request whose `Host` header consists only of ASCII letters, ASCII digits, `.`, `-` and `:` MUST, read as a whole body, have a banner that contains exactly one `a` start tag and exactly one `form` start tag and contains, in this order: a `strong` start tag carrying an occurrence of the attribute `class` whose read value is exactly `mark` and an occurrence of the attribute `data-service` whose read value is exactly `ServiceName`, the normalisation of whose element content — the characters from that start tag's `>` up to the `<` of the first `</strong>` end tag following it — is exactly `ikigenba`; then the **email link**, that `a` start tag, carrying an occurrence of the attribute `href` whose read value is exactly the `ProfileURL` of the banner user for that request, the normalisation of whose element content up to the first `</a>` end tag following it is exactly the whitespace collapse of that banner user's `Email`; then the **sign-out form**, that `form` start tag, carrying an occurrence of the attribute `method` whose read value is exactly `post` and an occurrence of the attribute `action` whose read value is exactly the `LogoutURL` of that banner user, whose element content up to the first `</form>` end tag following it contains exactly one `button` start tag, carrying an occurrence of the attribute `type` whose read value is exactly `submit`, the normalisation of whose element content up to the first `</button>` end tag following it is exactly `Sign out`; and every `svg` start tag in that banner MUST carry an occurrence of the attribute `aria-hidden` whose read value is exactly `true`.
- R-86Y3-CZ7L: For a `Handler` built over the echoing banner source for an empty or nil slice, every HTML document dummy sends MUST, read as a whole body, contain no `nav` start tag, no `button` start tag carrying an occurrence of the attribute `class` whose read value is exactly `launcher`, no `input` start tag carrying an occurrence of the attribute `type` whose read value is exactly `search`, no occurrence of the string `No service matches`, and no occurrence of the string `/_appkit/launcher.js`, so that a page drawn with no services carries no launcher button, no list of services, no search field and no no-match line, and loads no launcher script.
- R-B52M-STDU: For a `Handler` built over the echoing banner source for a slice of one or more plain services, every HTML document dummy sends MUST, read as a whole body: have a banner containing a `button` start tag carrying an occurrence of the attribute `class` whose read value is exactly `launcher`, an occurrence of the attribute `type` whose read value is exactly `button` and an occurrence of the attribute `aria-label` whose read value is exactly `Services`, lying before the banner's `a` start tag; contain exactly one `nav` start tag, lying after the end of that banner and before the first `main` start tag, carrying an occurrence of the attribute `id` whose read value is exactly `services`, an occurrence of the attribute `aria-label` whose read value is exactly `Services` and a bare occurrence of the attribute `popover`, whose element — the characters from that start tag through the first `</nav>` end tag following it — contains exactly one `input` start tag, carrying an occurrence of the attribute `type` whose read value is exactly `search`, an occurrence of the attribute `placeholder` whose read value is exactly `Find a service` and an occurrence of the attribute `aria-label` whose read value is exactly `Find a service`, and exactly one `p` start tag, carrying a bare occurrence of the attribute `hidden`, the normalisation of whose element content up to the first `</p>` end tag following it begins with `No service matches`; and contain exactly one `script` start tag carrying an occurrence of the attribute `src` whose read value is exactly `/_appkit/launcher.js`, lying after that `</nav>` end tag and before the first `main` start tag.
- R-B6AJ-6L4J: For a `Handler` built over the echoing banner source for a slice `svc` of one or more plain services, in every HTML document dummy sends, read as a whole body, the element of the `nav` start tag R-B52M-STDU describes MUST contain exactly as many `a` start tags as `svc` has elements, and for each index `i` the `i`-th of those `a` start tags in document order MUST: when `svc[i].Enabled` is true, carry an occurrence of the attribute `href` whose read value is exactly `svc[i].URL` and no occurrence of the attribute `aria-disabled`; when it is false, carry no occurrence of the attribute `href`, an occurrence of the attribute `aria-disabled` whose read value is exactly `true`, and an occurrence of the attribute `title` whose read value is exactly `svc[i].Name` followed by ` is unavailable`; when `svc[i].Current` is true, carry an occurrence of the attribute `aria-current` whose read value is exactly `page`, and otherwise no occurrence of that attribute; and be followed by element content — the characters from that start tag's `>` up to the `<` of the first `</a>` end tag following it — that begins with exactly `svc[i].Icon` and whose normalisation is exactly `svc[i].Name`.
- R-LZK9-GUF2: Every HTML document dummy sends MUST be drawn with the banner for the request it answers.
- R-VDFC-F7H3: `Handler` MUST NOT send a `Set-Cookie` header in any response, whatever the request's path, method and headers, so that dummy sets no cookie and leaves the session to auth.
- R-VIAX-YAFV: dummy's design defines the **page content** of a string `s` as the characters of the script-stripped form of `s` lying between the `>` of the first `main` start tag in that form and the `<` of the first `</main>` end tag following that start tag; a string has no page content when either tag is absent; and every requirement in dummy's design that names page content MUST denote those characters.
- R-BB64-PO3B: The script-stripped form of every HTML document dummy sends MUST contain exactly one `main` start tag and exactly one `</main>` end tag, and the characters of that form between the `>` of its first `body` start tag and the `<` of the first `</body>` end tag following it MUST consist of exactly these, in this order: optional ASCII whitespace, that `main` start tag, the page content, that `</main>` end tag, optional ASCII whitespace; so that, with the appkit banner at the top of the body, everything the page itself holds is inside the one `main` element.
- R-M0S5-UM5R: Every response in the banner failure shape for a named message constant whose body is non-empty MUST have page content the normalisation of which contains that constant and which contains an `a` start tag carrying an occurrence of the attribute `href` whose read value is exactly `/widgets`, so that a failure page's message and its way back sit inside its `main` element.
- R-M202-8DWG: The script-stripped form of every HTML document dummy sends MUST contain exactly one `title` start tag and exactly one `</title>` end tag, both before the first `body` start tag and the start tag first, and the normalisation of the characters between that start tag's `>` and that end tag's `<` MUST be exactly `ServiceName`.
- R-BCE1-3FU0: The script-stripped form of every HTML document dummy sends MUST contain exactly one `link` start tag carrying an occurrence of the attribute `rel` whose read value is exactly `stylesheet`, and that start tag MUST lie before the first `body` start tag and MUST carry an occurrence of the attribute `href` whose read value is exactly `/_appkit/theme.css`.
- R-M37Y-M5N5: The script-stripped form of every HTML document dummy sends MUST contain exactly one `meta` start tag carrying an occurrence of the attribute `name` whose read value is exactly `viewport`, and that start tag MUST lie before the first `body` start tag and MUST carry an occurrence of the attribute `content` whose read value is exactly `width=device-width, initial-scale=1`.
- R-M4FU-ZXDU: For a request such that neither its path, nor its `Host` header, nor any other header value it carries, nor its body contains a sequence of five characters that equals `dummy` compared case-insensitively and is not exactly `dummy`, and immediately before which no widget in the slice `s.All()` returns has a `Name` containing such a sequence, an HTML document dummy sends in answer to that request MUST NOT contain such a sequence, so that the service's name appears in it only as `dummy` and the text `Dummy` appears nowhere in it.
- R-M5NR-DP4J: In the raw body of every HTML document dummy sends, in every start tag that is not an `a` start tag, every occurrence of an attribute named `href`, `xlink:href`, `src`, `poster`, `data`, `background` or `manifest` MUST have a read value that, once every ASCII tab, line feed and carriage return has been removed from it, is exactly `/` or begins with `/` followed by a character that is neither `/` nor `\`, and every occurrence of an attribute named `srcset` or `imagesrcset` MUST have a read value every comma-separated candidate of which, once its leading ASCII whitespace is removed, meets that same condition, so that a page names no resource on another host.
- R-M6VN-RGV8: In the raw body of every HTML document dummy sends, every start tag MUST carry no occurrence of the attribute `style` and no occurrence of the attribute `ping`, so that no inline declaration can name a `url(...)` on another host and no link a reader follows reports the follow to another host.
- R-M83K-58LX: In the raw body of every HTML document dummy sends, every `script` start tag MUST carry no occurrence of the attribute `src`, no occurrence of the attribute `href` and no occurrence of the attribute `xlink:href`, so that no script element, an SVG one included, loads its source from anywhere, dummy's own host included.
- R-M9BG-J0CM: In the raw body of every HTML document dummy sends, every `<` immediately followed by an ASCII letter MUST begin a **well-formed start tag**, meaning a span consisting of exactly these, in this order: that `<`; a tag name of one or more characters each of which is an ASCII letter, an ASCII digit or `-`; zero or more attributes, each being one or more ASCII whitespace characters, then an attribute name of one or more characters none of which is ASCII whitespace, `"`, `'`, `<`, `>`, `/` or `=`, then optionally `=` immediately followed by a double quote, zero or more characters none of which is `"`, `<` or `>`, and a double quote; zero or more ASCII whitespace characters; an optional `/`; and `>`; so that the first `>` following that `<` ends the tag a browser reads there and every attribute a browser reads in that tag is preceded by ASCII whitespace and, when it has a value, followed immediately by `=` and a double-quoted value.
- R-MAJC-WS3B: In the raw body of every HTML document dummy sends, every start tag MUST contain no ASCII whitespace character immediately followed by the two characters `on` compared case-insensitively, then one or more ASCII letters, then `=`, and MUST carry no occurrence of the attribute `srcdoc` and no occurrence of the attribute `http-equiv`, so that no event-handler attribute carries script, no inline frame is handed a document written in place, and no `meta` element refreshes the page or sends it elsewhere.
- R-BDLX-H7KP: The raw body of every HTML document dummy sends MUST contain no `math` start tag, and every `svg` start tag in it MUST begin an occurrence of the value of `PlusIcon`, meaning that the raw body, read from that start tag's `<`, begins with the whole of that value; so that the only foreign content dummy's own markup carries is one icon whose every byte dummy's design fixes, and no presentation attribute or animation element names a resource.
- R-BETT-UZBE: For a `Handler` built over an echoing banner source, the tag structure of an HTML document dummy sends, read as a whole body, MUST NOT depend on any value `Handler` draws into it from the request — the value of the request's `X-User-Email` header, the values of its `Host` and `X-Forwarded-Proto` headers, which reach it through the banner user, and the `Name`, `Count` and `Status` fields of the `Submission` (`D05-widgets`) a 422 answer echoes: for two requests that differ only in one of those values, that are answered with the same status, immediately before each of which the slice `s.All()` returns is equal element for element and in the same order, and for which, where `Store.Create` (`D05-widgets`) was called at all, it returned equal `FieldErrors` values, the **tag-name sequence** of the two bodies MUST be equal and the two bodies MUST contain equally many `>` characters, so that such a value can neither open a tag nor end one, where the tag-name sequence of a body is obtained by scanning the whole body from the left and taking, for each `<` in turn, the characters following that `<` — following the `/` when a `/` immediately follows it — up to but not including the first character that is neither an ASCII letter nor an ASCII digit.
- R-LU2I-4WTE: dummy's design defines a response to be in the **plain failure shape** for a named body constant when its `Content-Type` header is exactly `text/plain; charset=utf-8` and its body is exactly that constant, or is empty when the request's method is `HEAD`.
- R-MBR9-AJU0: dummy's design defines a response to be in the **banner failure shape** for a named message constant when its `Content-Type` header is exactly `text/html; charset=utf-8` and its body is empty when the request's method is `HEAD` and is otherwise a body whose visible text contains that constant and which contains an `a` start tag whose `href` attribute has read value exactly `/widgets`.
- R-ME72-23BE: dummy's design uses **panel page** for a document shape and not for a route: a response body is a panel page exactly when a requirement in dummy's design requires that body to be one, and no body is a panel page merely by meeting the obligations stated here; every panel page MUST be an HTML document dummy sends and MUST be drawn with the banner for the request it answers; and every further obligation a panel page carries is stated by the requirement that states it.
- R-LXQ7-A81H: `Handler` MUST answer a request whose `X-User-Id` header is absent, or present with an empty value, with status 500, the header `Content-Type: text/plain; charset=utf-8`, no `Allow` header, and a response in the plain failure shape for `MissingIdentityBody`, whatever the request's path and method.
- R-Y5IN-YD18: `Handler` MUST NOT set an `ETag` header on a response it answers with status 500 because the request's `X-User-Id` header is absent or present with an empty value, whatever the request's path and method.
- R-LYY3-NZS6: `Handler` MUST decide that a request carries no identity before it examines the request's path or method, so that a request whose `X-User-Id` header is absent or empty MUST NOT be answered with status 303, 404, 405 or 415 for any path, `/`, `/widgets` and `/widgets/table` included.
- R-IVCG-647F: `Handler` MUST NOT treat the `X-User-Email` header as a precondition: for two requests that carry a non-empty `X-User-Id`, that differ only in that one carries no `X-User-Email` header while the other carries an empty one, and immediately before each of which the slice `s.All()` returns is equal element for element and in the same order, `Handler` MUST answer both with the same status and the same value for every header it sets, and MUST answer neither with status 500.
- R-M1DW-FJ9K: `MissingIdentityBody` and `MethodNotAllowedBody` MUST each be one line: exactly one newline, at the end.
- R-ISWN-EKQ1: `Handler` MUST decide a request's route from the path component of its URL alone, compared for exact equality and nothing else — not the query string, not the `Host` header, not any other header — so that a request whose path is `/widgets/` or `/widgets/table/` is answered as a request whose path is none of `/`, `/widgets` and `/widgets/table`; `Handler` MUST NOT answer any request with status 301; and `Handler` MUST NOT answer a request whose path is `/widgets/` or `/widgets/table/` with any status in the 3xx range or with a `Location` header, so that no trailing-slash variant is redirected to the path it resembles.
- R-M3TP-72QY: `Handler` MUST answer a request carrying identity whose path is `/` and whose method is `GET` or `HEAD` with status 303, the header `Location: /widgets`, and an empty body.
- R-1PV2-66TM: `Handler` MUST answer a request carrying identity whose path is `/` and whose method is neither `GET` nor `HEAD` with status 405, the header `Allow: GET, HEAD`, and a response in the banner failure shape for `MethodNotAllowedMessage`.
- R-M69H-YM8C: `Handler` MUST answer a request carrying identity whose path is `/widgets` and whose method is `GET` with status 200, the header `Content-Type: text/html; charset=utf-8`, and a body that is a panel page.
- R-1R2Y-JYKB: `Handler` MUST answer a request carrying identity whose path is `/widgets` and whose method is none of `GET`, `HEAD` and `POST` with status 405, the header `Allow: GET, HEAD, POST`, and a response in the banner failure shape for `MethodNotAllowedMessage`.
- R-BG1Q-8R23: `Handler` MUST answer a request carrying identity whose path is none of `/`, `/widgets` and `/widgets/table` and is not an appkit path (`D08-assets`), whatever its method, with status 404 and a response in the banner failure shape for `NotFoundMessage`.
- R-MB53-HP74: `Handler` MUST NOT answer a request whose path is `/widgets/table` as a path it does not know: such a request, carrying identity, MUST NOT be answered with status 404.
- R-JJDV-KL7M: For two requests that differ only in that one's method is `HEAD` and the other's is `GET`, and immediately before each of which the slice `s.All()` returns is equal element for element and in the same order, `Handler` MUST answer the `HEAD` request with the same status and the same value for every header it sets as it answers the `GET` request, and with an empty body; this holds for every path, `/` included.
- R-MFEY-FV23: The script-stripped form of a panel page MUST contain exactly one `h1` start tag and exactly one `</h1>` end tag, the start tag first, and the normalisation of the characters between that start tag's `>` and that end tag's `<` MUST be exactly `Widgets`.
- R-XTBO-4NMA: dummy's design defines the **panel wrapper** of a string `s` as the span of the script-stripped form of `s` running from the `<` of the first `div` start tag in that form that carries an occurrence of the attribute `class` whose read value is exactly `panel`, through the `>` of the earliest `</div>` end tag following that start tag such that the span from that start tag through that end tag contains exactly as many `</div>` end tags as `div` start tags; a string has no panel wrapper when no such start tag or no such end tag exists; and every requirement in dummy's design that names a panel wrapper MUST denote that span.
- R-MGMU-TMSS: Every panel page MUST have a panel wrapper, and its script-stripped form MUST contain exactly one start tag carrying an occurrence of the attribute `class` whose read value is exactly `panel`.
- R-MHUR-7EJH: In a panel page, the characters of the panel wrapper between the `>` ending the `div` start tag that begins it and the `<` beginning the `</div>` end tag that ends it MUST consist of exactly these, in this order: optional ASCII whitespace, the page's table span (`D06-table`), optional ASCII whitespace, the page's form card (`D07-form`), optional ASCII whitespace.
- R-VLYN-3LNY: dummy's design defines the **panel subtitle** for a non-negative integer `n` as the decimal digits of `n` with no sign and no leading zero, `0` when `n` is zero, then ` widget` when `n` is 1 and ` widgets` otherwise, then a space, the character U+00B7 MIDDLE DOT encoded in UTF-8, a space, and `refreshes every 5 seconds`; and the **heading block** for `n` as exactly the string `<div class="section-head"><div><h1>Widgets</h1><p>`, then the panel subtitle for `n`, then the string `</p></div></div>`; and every requirement in dummy's design that names a panel subtitle or a heading block MUST denote that string.
- R-MJ2N-L6A6: The page content of every panel page MUST consist of exactly these, in this order: optional ASCII whitespace, the heading block for `n`, optional ASCII whitespace, the panel wrapper, optional ASCII whitespace; where `n` is the number of data rows (`D06-table`) in that page's table span (`D06-table` R-MLIG-CPRK); so that every rendering of the panel page, the 422 redraw included, carries the heading and a subtitle counting the widgets its table shows, and nothing else sits in the page's content.
- R-MKAJ-YY0V: A panel page MUST contain exactly one `script` start tag and exactly one `</script>` end tag, counted over the raw body; that `script` start tag MUST carry no `src` attribute; the source of that element — the characters between that start tag's `>` and the `<` of that end tag — MUST contain the literal `/widgets/table`, the literal `widgets-table` and the literal `5000`, the interval in milliseconds at which the script re-fetches the table; and in the raw body that `script` start tag MUST NOT lie between the first `table` start tag and the first `</table>` end tag following that `table` start tag.
- R-0AAB-3WJP: A request whose `X-User-Id` header is present with a non-empty value, whose path is `/` and whose method is `GET`, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-0BI7-HOAE: A request whose `X-User-Id` header is present with a non-empty value, whose path is `/` and whose method is `HEAD`, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-0CQ3-VG13: A request whose `X-User-Id` header is present with a non-empty value, whose path is `/widgets` and whose method is `GET`, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-0DY0-97RS: A request whose `X-User-Id` header is present with a non-empty value, whose path is `/widgets` and whose method is `HEAD`, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-A2RR-3ZJU: A request whose `X-User-Id` header is present with a non-empty value, whose path is `/` and whose method is neither `GET` nor `HEAD`, a `POST` request carrying the header `Content-Type: application/x-www-form-urlencoded` and a body in that encoding whose values for `name`, `count` and `status` `Store.Create` (`D05-widgets`) would accept on `s` immediately before the request — a `Submission` of those values for which `Create` on `s` would return a `FieldErrors` whose `Any` is false — included, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-A3ZN-HRAJ: A request whose `X-User-Id` header is present with a non-empty value, whose path is `/widgets` and whose method is none of `GET`, `HEAD` and `POST`, a `PUT` request carrying the header `Content-Type: application/x-www-form-urlencoded` and a body in that encoding whose values for `name`, `count` and `status` `Store.Create` (`D05-widgets`) would accept on `s` immediately before the request — a `Submission` of those values for which `Create` on `s` would return a `FieldErrors` whose `Any` is false — included, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-A57J-VJ18: A request whose `X-User-Id` header is present with a non-empty value and whose path is none of `/`, `/widgets` and `/widgets/table`, whatever its method, `/widgets/` and `/widgets/table/` included, and a `POST` request to `/widgets/` carrying the header `Content-Type: application/x-www-form-urlencoded` and a body in that encoding whose values for `name`, `count` and `status` `Store.Create` (`D05-widgets`) would accept on `s` immediately before the request — a `Submission` of those values for which `Create` on `s` would return a `FieldErrors` whose `Any` is false — included, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-KX9D-3SPX: A request that carries no `X-User-Id` header, whatever its path and method, a `POST /widgets` request carrying a form-encoded submission (`D07-form`) whose values for `name`, `count` and `status` `Store.Create` (`D05-widgets`) would accept on `s` — a `Submission` of those values for which `Create` on `s` would return a `FieldErrors` whose `Any` is false — included, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-KYH9-HKGM: A request whose `X-User-Id` header is present with an empty value, whatever its path and method, a `POST /widgets` request carrying a form-encoded submission (`D07-form`) whose values for `name`, `count` and `status` `Store.Create` (`D05-widgets`) would accept on `s` — a `Submission` of those values for which `Create` on `s` would return a `FieldErrors` whose `Any` is false — included, MUST leave the store `s` that `Handler` was built over unchanged, whatever widgets `s` holds: the slice `s.All()` returns before the request and the slice it returns after the request MUST be equal element for element and in the same order.
- R-BH9M-MISS: The response `Handler` produces for a request MUST NOT depend on the process's working directory or on any file outside the binary other than one its banner source reads: for two identical requests, one answered by a handler driven with the working directory set to an empty temporary directory and one answered by a handler driven with the working directory set to the checkout, both handlers built over the same banner source, one that reads no file and returns equal values for equal arguments, and immediately before each of which the slice `s.All()` returns for that handler's own store is equal element for element and in the same order, the two answers MUST have the same status, the same value for every header `Handler` sets, and the same body.
- R-MM92-HZAL: For every request `Handler` answers with status 500 because its `X-User-Id` header is absent or empty, `Handler` MUST write exactly `"dummy: request " + id + ": X-User-Id is missing\n"` to `stderr` in a single call to `stderr.Write`, where `id` is the value `r.Header.Get("X-Request-Id")` returns when that value is non-empty and `-` when it is empty.
- R-Y1D9-4V24: `Handler` MUST write nothing to `stderr` for a request it answers with a status outside 500 through 599, and MUST write exactly one line to `stderr` for each request it answers with a status from 500 through 599.
- R-MOOV-9IRZ: `Handler` MUST NOT let two calls to `stderr.Write` be in progress at the same time, whatever requests it is handling concurrently.
