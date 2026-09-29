# D3-templates

The banner's markup is a human-written asset, `assets/banner.html`, a Go
`html/template` file that defines two templates, `banner` and `launcher`.
`Templates` parses it from the embedded copy and hands the app a fresh
template set to parse its own pages into. The code writes no markup of its
own. This document names the data each template receives and the hooks each
emits in each state; the asset author makes the markup carry them, and the
tests assert on these hooks and on visible text, never on layout. A hook this
design names that the asset lacks is an issue for a human.

The hooks follow the repository's launcher mock-up in `design/`.

## `banner`

`banner` receives an `appkit.Banner` (D2) and renders the page's header: the
`ikigenba` mark naming the app's service, the user's email linked to their
profile, and a sign-out form posting to the logout URL. The **launcher is
shown** exactly when the `Banner`'s `Services` is not empty. Then `banner`
also renders the launcher button, the launcher itself (by invoking the
`launcher` template with the same `Banner`), and the script tag that loads
`launcher.js` from under `StaticPrefix` (D4). When the launcher is not shown,
none of those three appear and the page still renders. `banner` never links
the stylesheet: each page links `/_appkit/theme.css` itself, in its own head.

## `launcher`

`launcher` also receives an `appkit.Banner` and renders the services
popover: a search box, one tile per service in order, and a hidden
"No service matches" line that the script fills. A tile is a link holding the
service's icon, then its name. An enabled service's tile links to its URL. The
app's own service is marked as the current page and stays linked. A disabled
service stays in its place, unlinked, marked disabled, with a title saying it
is unavailable. A service that is both current and disabled carries both
marks and no link; the rules compose.

## Escaping

Every string the templates emit — the service name, email, URLs, service
names — goes through `html/template`'s contextual autoescaping, as the
standard library documents it: markup characters become character
references, and a URL with a scheme `html/template` does not allow is replaced
by `#ZgotmplZ`. The icon is the one exception. It is `template.HTML` and is
inserted verbatim, a trust D2 explains.

## Browser behaviour (the script asset, not testable here)

`assets/launcher.js` is human-written and static. Its intended behaviour:
the popover does not open on page load; typing in the search box filters the
tiles to names containing the trimmed query, case-insensitively; with no
tile matching, the hidden line shows `No service matches “<query>”.`; clearing
the query shows every tile again; Enter opens the first visible tile that is
a link, skipping disabled tiles; Enter with no such tile does nothing.

None of that is a requirement. The toolchain is the Go standard library with
no browser, so no gate can run the script, and a requirement no gate can test
does not belong in the list. The build's part is limited to what it can
check: the templates emit the hooks the script relies on (the `services` id,
the search input, the tiles, the hidden line with its `q`), and D4 serves the
script's bytes unaltered.

## Notes for the asset author

These are decisions about the assets, recorded here so the human writing
them has them in one place; the build run neither writes nor tests them.
In `theme.css`, the launcher's icon selectors become `nav.services a > svg`
(changed in the repository's `design/ikigenba/theme.css` first, then copied),
and app icons carry no `svg.ico` class. App icons carry no `aria-hidden`: the
link's text is the service name. The stylesheet and fonts are copied from
`design/` with a header naming the `design/` commit they came from.

## REQUIREMENTS

- R-6RW6-28TK: Package `appkit` MUST export `func Templates() *template.Template`, where `template` is the standard library's `html/template`.
- R-6T42-G0K9: `Templates` MUST return a non-nil template set that defines the templates `banner` and `launcher`, parsed from the embedded `assets/banner.html`.
- R-6UBY-TSAY: The returned template's own name, and the name of every template in its set, MUST be `appkit`, `banner`, or `launcher`.
- R-6WRR-LBSC: Each `Templates` call MUST return a new set independent of every other call's, so parsing templates into, redefining templates in, or executing one returned set does not change the templates or output of another.
- R-6XZN-Z3J1: Templates a consumer parses into the set `Templates` returns, with `ParseFS` or `Parse`, MUST be able to invoke `banner` and `launcher`, and executing such a template MUST render them as the other requirements in this document describe.
- R-1IZJ-274C: `banner`, executed with a `Banner`, MUST emit a `strong` element with class `mark`, whose `data-service` attribute value is the `Banner`'s `Service` as R-1HRM-OFDN renders it and whose text is `ikigenba`.
- R-1K7F-FYV1: `banner`, executed with a `Banner`, MUST emit an `a` element whose `href` attribute value is the `Banner`'s `ProfileURL` as R-1HRM-OFDN renders it and whose text is its `Email` as R-1HRM-OFDN renders it.
- R-1LFB-TQLQ: `banner`, executed with a `Banner`, MUST emit a `form` element with attribute `method="post"` whose `action` attribute value is the `Banner`'s `LogoutURL` as R-1HRM-OFDN renders it, containing a `button` element with attribute `type="submit"` whose visible text is `Sign out`.
- R-72V9-I6HT: When the `Banner`'s `Services` is not empty, `banner` MUST emit exactly one `button` element with class `launcher` and the attributes `popovertarget="services"`, `aria-label="Services"`, and `title="Services"`.
- R-7435-VY8I: When the `Banner`'s `Services` is not empty, `banner` MUST emit the output of `launcher` executed with the same `Banner`, exactly once.
- R-75B2-9PZ7: When the `Banner`'s `Services` is not empty, `banner` MUST emit exactly one `script` element, whose `src` attribute value is `/_appkit/launcher.js` and which carries the `defer` attribute.
- R-1QAX-CTKI: When the `Banner`'s `Services` is empty, `banner`'s output MUST contain no element with class `launcher` or class `services`, no `popovertarget` attribute, no element with id `services`, no `script` element, and no `input` element, and MUST still contain the hooks of R-1IZJ-274C, R-1K7F-FYV1, and R-1LFB-TQLQ.
- R-77QV-19GL: `banner` MUST NOT emit a `link` element.
- R-78YR-F17A: `launcher`, executed with a `Banner`, MUST emit a `nav` element with class `services`, id `services`, the attribute `popover`, and the attribute `aria-label="Services"`, which contains every other hook this document assigns to `launcher`.
- R-7A6N-SSXZ: `launcher` MUST emit an `input` element with attribute `type="search"`, `placeholder="Find a service"`, and `aria-label="Find a service"`.
- R-1P30-Z1TT: `launcher` MUST emit, for each element of the `Banner`'s `Services` and in that order, one `li` element containing one `a` element whose content, ignoring whitespace before, between, and after the two parts, is that `Service`'s `Icon` followed by its `Name` as text as R-1HRM-OFDN renders it, and no other `li` element; with `Services` empty it emits no `li` element.
- R-1MN8-7ICF: The `a` element of a tile whose `Service` has `Enabled` true MUST have an `href` attribute whose value is the `Service`'s `URL` as R-1HRM-OFDN renders it, and MUST NOT have an `aria-disabled` attribute.
- R-1NV4-LA34: The `a` element of a tile whose `Service` has `Enabled` false MUST NOT have an `href` attribute, and MUST have the attributes `aria-disabled="true"` and `title` whose value is the `Service`'s `Name` as R-1HRM-OFDN renders it followed by ` is unavailable`.
- R-7HI2-3FE5: The `a` element of a tile whose `Service` has `Current` true MUST have the attribute `aria-current="page"`, whatever its `Enabled`; the `a` element of a tile whose `Current` is false MUST NOT have an `aria-current` attribute.
- R-7IPY-H74U: `launcher` MUST emit a `p` element carrying the `hidden` attribute whose content is the text `No service matches `, then an empty `q` element, then the text `.`.
- R-1HRM-OFDN: Every string field of `Banner` and `Service` that `banner` or `launcher` emits MUST be emitted through `html/template`'s contextual autoescaping for its position, and where a requirement in this document says an attribute value or text is such a field, it means exactly the string that escaping renders for the field in that position: in element text or a non-URL attribute value, the field as `html/template` escapes it there (for example `<`, `>`, `&`, `"`, `'`, and `+` become character references, so `user+tag@example.com` appears as `user&#43;tag@example.com`, and NUL becomes U+FFFD); in an `href` or `action` attribute, the field as `html/template` renders a URL there (percent-encoding included, so a space appears as `%20`), and a URL whose scheme `html/template` does not allow (for example `javascript:`) MUST appear as `#ZgotmplZ`.
- R-7L5R-8QM8: The `Icon` of each `Service` MUST appear in `launcher`'s output byte-for-byte unaltered, markup characters included.
