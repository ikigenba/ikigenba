# D03-page-templates

The banner's markup is a human-written asset, `banner.html`, a Go `html/template` file embedded from `page/assets/` that defines three templates, `banner`, `launcher`, and `footer`. `page.Templates` parses it from the embedded copy and hands the app a fresh template set to parse its own pages into. The code writes no markup of its own. This document names the data each template receives and the hooks it emits in each state; the asset author makes the markup carry them, and the tests assert on these hooks and on visible text, never on layout. A hook this design names that the asset lacks is an issue for a human.

The hooks follow the repository's app and launcher mock-ups in `design/`. The banner and footer appear only on pages shown to a signed-in user (D02).

The set's own root template is named `appkit`, after the library.

## `banner`

`banner` receives a `page.Banner` (D02) and renders the page's header: the `ikigenba` mark naming the app's service, a profile icon linking to the user's profile, and a sign-out form posting to the logout URL. The profile link carries no text: it is labelled `Profile` for assistive technology and titled with the user's email, so hovering it shows who is signed in. The **launcher is shown** exactly when the `Banner`'s `Services` is not empty. Then `banner` also renders the launcher button, the launcher itself (by invoking the `launcher` template with the same `Banner`), and the script tag that loads `launcher.js` from under `page.StaticPrefix` (D04). When the launcher is not shown, none of those three appear and the page still renders. `banner` never links the stylesheet: each page links `/_appkit/theme.css` itself, in its own head.

## `launcher`

`launcher` also receives a `page.Banner` and renders the services popover: a search box, one tile per service in order, and a hidden "No service matches" line that the script fills. A tile is a link holding the service's icon, then its name. An enabled service's tile links to its URL. The app's own service is marked as the current page and stays linked. A disabled service stays in its place, unlinked, marked disabled, with a title saying it is unavailable. A service that is both current and disabled carries both marks and no link; the rules compose.

## `footer`

`footer` also receives a `page.Banner` and renders the page's footer: the app's service name and its version, separated by one space (`<service> <version>`). The version is shown exactly as the app gave it to `page.New`. Each page places the footer itself as the last child of its `body`, with `{{template "footer" .Banner}}`, just as it places the banner at the top with `{{template "banner" .Banner}}`.

## Escaping

Every string the templates emit — the service name, version, email, URLs, service names — goes through `html/template`'s contextual autoescaping, as the standard library documents it: markup characters become character references, and a URL with a scheme `html/template` does not allow is replaced by `#ZgotmplZ`. The icon is the one exception. It is `template.HTML` and is inserted verbatim, a trust D02 explains.

## Browser behaviour (the script asset, not testable here)

`launcher.js` is human-written and static. Its intended behaviour: the popover does not open on page load; typing in the search box filters the tiles to names containing the trimmed query, case-insensitively; with no tile matching, the hidden line shows `No service matches “<query>”.`; clearing the query shows every tile again; Enter opens the first visible tile that is a link, skipping disabled tiles; Enter with no such tile does nothing.

None of that is a requirement. The toolchain is the Go standard library with no browser, so no gate can run the script, and a requirement no gate can test does not belong in the list. The build's part is limited to what it can check: the templates emit the hooks the script relies on (the `services` id, the search input, the tiles, the hidden line with its `q`), and D04 serves the script's bytes unaltered.

## Notes for the asset author

These are decisions about the assets, recorded here so the human writing them has them in one place; the build run neither writes nor tests them. In `theme.css`, the launcher's icon selectors are `nav.services a > svg` (changed in the repository's `design/ikigenba/theme.css` first, then copied), and app icons carry no `svg.ico` class. App icons carry no `aria-hidden`: the link's text is the service name. The stylesheet and fonts are copied from `design/` with a header naming the `design/` commit they came from.

## REQUIREMENTS

- R-I900-VFBX: Package `page` MUST export `func Templates() *template.Template`, where `template` is the standard library's `html/template`.
- R-IA7X-972M: `page.Templates` MUST return a non-nil template set that defines the templates `banner`, `launcher`, and `footer`, parsed from the embedded asset `banner.html`, embedded from `page/assets/`.
- R-YHYW-CTC1: The template `page.Templates` returns MUST have the name `appkit`.
- R-YJ6S-QL2Q: The name of every template in the set `page.Templates` returns MUST be `appkit`, `banner`, `launcher`, or `footer`.
- R-ICNQ-0QK0: Each `page.Templates` call MUST return a new set independent of every other call's, so parsing templates into, redefining templates in, or executing one returned set does not change the templates or output of another.
- R-IDVM-EIAP: Templates a consumer parses into the set `page.Templates` returns, with `ParseFS` or `Parse`, MUST be able to invoke `banner`, `launcher`, and `footer`, and executing such a template MUST render them as the other requirements in this document describe.
- R-IF3I-SA1E: `banner`, executed with a `page.Banner`, MUST emit a `strong` element with class `mark`, whose `data-service` attribute value is the `Banner`'s `Service` as R-IYLW-WLWI renders it and whose text is `ikigenba`.
- R-IGBF-61S3: `banner`, executed with a `page.Banner`, MUST emit exactly one `a` element with class `profile`, whose `href` attribute value is the `Banner`'s `ProfileURL` as R-IYLW-WLWI renders it, which has the attribute `aria-label="Profile"` and a `title` attribute whose value is the `Banner`'s `Email` as R-IYLW-WLWI renders it, and whose content contains no text other than whitespace.
- R-IHJB-JTIS: `banner`, executed with a `page.Banner`, MUST emit a `form` element with attribute `method="post"` whose `action` attribute value is the `Banner`'s `LogoutURL` as R-IYLW-WLWI renders it, containing a `button` element with attribute `type="submit"` whose visible text is `Sign out`.
- R-IJZ4-BD06: When the `page.Banner`'s `Services` is not empty, `banner` MUST emit exactly one `button` element with class `launcher` and the attributes `popovertarget="services"`, `aria-label="Services"`, and `title="Services"`.
- R-IL70-P4QV: When the `page.Banner`'s `Services` is not empty, `banner` MUST emit the output of `launcher` executed with the same `Banner`, exactly once.
- R-IMEX-2WHK: When the `page.Banner`'s `Services` is not empty, `banner` MUST emit exactly one `script` element, whose `src` attribute value is `/_appkit/launcher.js` and which carries the `defer` attribute.
- R-INMT-GO89: When the `page.Banner`'s `Services` is empty, `banner`'s output MUST contain no element with class `launcher` or class `services`, no `popovertarget` attribute, no element with id `services`, no `script` element, and no `input` element, and MUST still contain the hooks of R-IF3I-SA1E, R-IGBF-61S3, and R-IHJB-JTIS.
- R-IOUP-UFYY: `banner` MUST NOT emit a `link` element.
- R-IQ2M-87PN: `launcher`, executed with a `page.Banner`, MUST emit a `nav` element with class `services`, id `services`, the attribute `popover`, and the attribute `aria-label="Services"`, which contains every other hook this document assigns to `launcher`.
- R-IRAI-LZGC: `launcher` MUST emit an `input` element with attribute `type="search"`, `placeholder="Find a service"`, and `aria-label="Find a service"`.
- R-ISIE-ZR71: `launcher` MUST emit, for each element of the `page.Banner`'s `Services` and in that order, one `li` element containing one `a` element whose content, ignoring whitespace before, between, and after the two parts, is that `page.Service`'s `Icon` followed by its `Name` as text as R-IYLW-WLWI renders it, and no other `li` element; with `Services` empty it emits no `li` element.
- R-ITQB-DIXQ: The `a` element of a tile whose `page.Service` has `Enabled` true MUST have an `href` attribute whose value is the `Service`'s `URL` as R-IYLW-WLWI renders it, and MUST NOT have an `aria-disabled` attribute.
- R-IUY7-RAOF: The `a` element of a tile whose `page.Service` has `Enabled` false MUST NOT have an `href` attribute, and MUST have the attributes `aria-disabled="true"` and `title` whose value is the `Service`'s `Name` as R-IYLW-WLWI renders it followed by ` is unavailable`.
- R-IW64-52F4: The `a` element of a tile whose `page.Service` has `Current` true MUST have the attribute `aria-current="page"`, whatever its `Enabled`; the `a` element of a tile whose `Current` is false MUST NOT have an `aria-current` attribute.
- R-IXE0-IU5T: `launcher` MUST emit a `p` element carrying the `hidden` attribute whose content is the text `No service matches `, then an empty `q` element, then the text `.`.
- R-IYLW-WLWI: Every string field of `page.Banner` and `page.Service` that `banner`, `launcher`, or `footer` emits MUST be emitted through `html/template`'s contextual autoescaping for its position, and where a requirement in this document says an attribute value or text is such a field, it means exactly the string that escaping renders for the field in that position: in element text or a non-URL attribute value, the field as `html/template` escapes it there (for example `<`, `>`, `&`, `"`, `'`, and `+` become character references, so `user+tag@example.com` appears as `user&#43;tag@example.com`, and NUL becomes U+FFFD); in an `href` or `action` attribute, the field as `html/template` renders a URL there (percent-encoding included, so a space appears as `%20`), and a URL whose scheme `html/template` does not allow (for example `javascript:`) MUST appear as `#ZgotmplZ`.
- R-IZTT-ADN7: The `Icon` of each `page.Service` MUST appear in `launcher`'s output byte-for-byte unaltered, markup characters included.
- R-J11P-O5DW: `footer`, executed with a `page.Banner`, MUST emit, ignoring whitespace before and after it, exactly one `footer` element and nothing else, whose content is the text of the `Banner`'s `Service` as R-IYLW-WLWI renders it, then one space, then its `Version` as R-IYLW-WLWI renders it.
