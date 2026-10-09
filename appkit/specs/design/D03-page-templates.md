# D03-page-templates

The chrome's markup is a human-written asset, `banner.html`, a Go `html/template` file embedded from `page/assets/` that defines four templates, `banner`, `launcher`, `footer`, and `preload`. `page.Templates` parses it from the embedded copy and hands the app a fresh template set to parse its own pages into. The code writes no markup of its own. This document names the data each template receives and what it shows in each state, never its words or its markup; the asset holds those, and its opening comment documents the hooks the stylesheet and the script rely on. A test executes the named template with the data a requirement states and checks that the values it supplied appear in the output. A template that cannot show a state this design names is an issue for a human.

The markup follows the repository's banner, app, and launcher mock-ups in `design/`. The banner and footer appear only on pages shown to a signed-in user (D02).

The set's own root template is named `appkit`, after the library.

## `banner`

`banner` receives a `page.Banner` (D02) and renders the page's header. First comes the mark: the suite's favicon as decoration and the product name, meant to link to the suite's home when the `Banner` carries a `Home` and not to be a link when it does not. Then comes the trail. Its root is the service: the app's own icon, when the `Banner` carries one, followed by the app's name, meant to link to the app's landing page, `/`; with no icon, the name alone. Beside the root sits the chevron that opens the page menu. Each element of `Trail` follows the root in order, its name meant to link to its URL, except the last, the current page, which shows its name and no link. An empty `Trail` is the landing page itself. Then follow the launcher button, a link to the user's profile that identifies the signed-in user by email, and sign out, a button posting to the logout URL. Their words, order and markup are the asset's.

The **page menu** is drawn on every execution, root pages and notice pages included, and lists the app's own pages: the landing page, the tools page at `/tools` only when `Tools` is true, and the about page at `/about`. Those two paths are the routes the apps serve, so they are contract, not copy. The row for the page being shown is meant to be marked as current: the landing row when `Trail` is empty (the asset's to keep, as below), the tools row when `Trail` is exactly one level whose URL is `/tools`, the about row when it is exactly one level whose URL is `/about`. A trail of more than one level marks no row, nor does a single level whose URL is none of `/`, `/tools`, and `/about`; a single level at `/` is not a page an app shows, and the contract leaves its marking to the asset. The list never grows beyond those rows.

A test reaches most of this through values and comparisons. It checks that a `Home`, a level's name, and each level's URL but the last's appear, in order, and that the last level's URL does not, choosing plain values, letters, digits, `/`, `-` and `_`, that the output does not already hold for other reasons; that `/about` is always emitted and `/tools` exactly when `Tools` is true; and, for the marks, it compares executions that differ only in the last level's URL, since that URL is never itself emitted: a difference can only be a row's mark. The requirements prove that values are emitted and in what order, not that any of them is a link. What no value can reach is the asset's alone, and the contract does not guarantee it: that a non-empty `Home` is a link and an empty one leaves the mark unlinked, that the root links to `/`, that each level but the last is a link to its URL, that the chevron is drawn, that the landing row is listed, that the landing row is marked when `Trail` is empty, and which of the rows carries a mark when one does.

The **launcher is shown** exactly when the `Banner`'s `Services` is not empty. Then `banner` also renders the launcher button, the launcher itself (by invoking the `launcher` template with the same `Banner`), and loads `launcher.js` from under `page.StaticPrefix` (D04). When the launcher is not shown, none of those three appear, and the page still renders. `banner` never links the stylesheet: each page links `/_appkit/theme.css` itself, in its own head. Nor does it load `feedback.js`; a page that wants button feedback links that itself too (D04). Nor does it link the favicon; each page links `/_appkit/favicon.svg` as its icon in its own head (D04), and the mark's image shows the same file.

## `launcher`

`launcher` also receives a `page.Banner` and renders the services popover: a search box, one tile per service in order, and a hidden line that the script fills when no tile matches. A tile holds the service's icon, then its name. An enabled service's tile links to its URL. The app's own service is marked as the current page and stays linked. A disabled service stays in its place, unlinked and marked disabled. A service that is both current and disabled carries both marks and no link; the rules compose.

## `footer`

`footer` also receives a `page.Banner` and renders the page's footer: the app's service name and its version. The version is shown exactly as the app gave it to `page.New`. Each page places the footer itself as the last child of its `body`, with `{{template "footer" .Banner}}`, just as it places the banner at the top with `{{template "banner" .Banner}}`.

## `preload`

`preload` takes no data and renders the one link that has the browser fetch upright Inter before the stylesheet asks for it, at the font's hashed URL, `page.PreloadURL` (D04). Every page places it in its own `head` with `{{template "preload"}}`, beside its stylesheet link. The asset cannot know the hash, so the set carries a template function, `preloadURL`, that returns the same URL; `preload` calls it, and so may a consumer's own template. A page appkit does not parse renders the same link itself from `page.PreloadURL`.

## Escaping

Every string the templates emit — the service name, version, email, URLs, the home URL, the trail's names and URLs, service names — goes through `html/template`'s contextual autoescaping, as the standard library documents it: markup characters become character references, and a URL with a scheme `html/template` does not allow is replaced by `#ZgotmplZ`. The icons are the one exception. The app's own icon in the trail and the page menu and each service's icon in the launcher are `template.HTML` and are inserted verbatim, a trust D02 explains.

## Browser behaviour (the script asset, not testable here)

`launcher.js` is human-written and static. Its intended behaviour: the popover does not open on page load; typing in the search box filters the tiles to names containing the trimmed query, case-insensitively; with no tile matching, the hidden line shows the query; clearing the query shows every tile again; Enter opens the first visible tile that is a link, skipping disabled tiles; Enter with no such tile does nothing.

None of that is a requirement. The toolchain is the Go standard library with no browser, so no gate can run the script, and a requirement no gate can test does not belong in the list. The build's part is limited to what it can check: D04 serves the script's bytes unaltered. The hooks the script relies on are the asset's, documented in `banner.html`'s opening comment.

## Notes for the asset author

These are decisions about the assets, recorded here so the human writing them has them in one place; the build run neither writes nor tests them. The hooks and the stylesheet's selectors for them are documented in `banner.html`'s opening comment and in `theme.css`, changed in the repository's `design/ikigenba/theme.css` first, then copied. The stylesheet and fonts are copied from `design/` with a header naming the `design/` commit they came from. `preload` is defined in `banner.html` beside the other three and takes its URL from `{{preloadURL}}`, never a written font name or hash.

## REQUIREMENTS

- R-I900-VFBX: Package `page` MUST export `func Templates() *template.Template`, where `template` is the standard library's `html/template`.
- R-55GA-E3MI: `page.Templates` MUST return a non-nil template set that defines the templates `banner`, `launcher`, `footer`, and `preload`, parsed from the embedded asset `banner.html`, embedded from `page/assets/`.
- R-YHYW-CTC1: The template `page.Templates` returns MUST have the name `appkit`.
- R-56O6-RVD7: The name of every template in the set `page.Templates` returns MUST be `appkit`, `banner`, `launcher`, `footer`, or `preload`.
- R-ICNQ-0QK0: Each `page.Templates` call MUST return a new set independent of every other call's, so parsing templates into, redefining templates in, or executing one returned set does not change the templates or output of another.
- R-57W3-5N3W: Templates a consumer parses into the set `page.Templates` returns, with `ParseFS` or `Parse`, MUST be able to invoke `banner`, `launcher`, `footer`, and `preload`, and executing such a template MUST render them as the other requirements in this document describe.
- R-5ABV-X6LA: The set `page.Templates` returns MUST provide a template function named `preloadURL`, taking no arguments, that a template a consumer parses into the set can call and that returns the string `page.PreloadURL` returns.
- R-CHNB-5CO9: `banner`, executed with a `page.Banner`, MUST emit the `Banner`'s `Service`, escaped as R-CSME-LACI states.
- R-CIV7-J4EY: `banner`, executed with a `page.Banner`, MUST emit the `Banner`'s `Icon` byte-for-byte unaltered, markup characters included.
- R-CK33-WW5N: `banner`, executed with a `page.Banner`, MUST emit the `Banner`'s `ProfileURL` and its `Email`, each escaped as R-CSME-LACI states.
- R-CLB0-ANWC: `banner`, executed with a `page.Banner`, MUST emit the `Banner`'s `LogoutURL`, escaped as R-CSME-LACI states.
- R-IL70-P4QV: When the `page.Banner`'s `Services` is not empty, `banner` MUST emit the output of `launcher` executed with the same `Banner`, exactly once.
- R-CMIW-OFN1: When the `page.Banner`'s `Services` is not empty, `banner` MUST emit `page.StaticPrefix` followed by `launcher.js` exactly once.
- R-CNQT-27DQ: When the `page.Banner`'s `Services` is empty, `banner`'s output MUST NOT contain the output of `launcher` executed with the same `Banner`, MUST NOT contain `page.StaticPrefix` followed by `launcher.js`, and MUST still emit what R-CHNB-5CO9, R-CIV7-J4EY, R-CK33-WW5N, and R-CLB0-ANWC state.
- R-COYP-FZ4F: `launcher`, executed with a `page.Banner`, MUST emit, for each element of the `Banner`'s `Services` and in that order, that `page.Service`'s `Icon` followed by its `Name`, escaped as R-CSME-LACI states.
- R-CQ6L-TQV4: For each element of the `page.Banner`'s `Services` whose `Enabled` is true, `launcher` MUST emit its `URL`, escaped as R-CSME-LACI states.
- R-CREI-7ILT: For each element of the `page.Banner`'s `Services` whose `Enabled` is false, `launcher` MUST NOT emit its `URL`.
- R-CSME-LACI: Every string field of `page.Banner` and `page.Service` that `banner`, `launcher`, or `footer` emits MUST be emitted through `html/template`'s contextual autoescaping for its position, and a URL field (`ProfileURL`, `LogoutURL`, or a `page.Service`'s `URL`) whose scheme `html/template` does not allow (for example `javascript:`) MUST appear as `#ZgotmplZ`.
- R-IZTT-ADN7: The `Icon` of each `page.Service` MUST appear in `launcher`'s output byte-for-byte unaltered, markup characters included.
- R-CTUA-Z237: `footer`, executed with a `page.Banner`, MUST emit the `Banner`'s `Service` and then its `Version`, each escaped as R-CSME-LACI states.
- R-CV27-CTTW: `preload`, executed with no data, as `{{template "preload"}}` invokes it, MUST emit the string `page.PreloadURL` returns.
- R-FCTS-Z475: `Home`, and the `Name` and `URL` of each `page.Level` in `Trail`, wherever `banner` emits them, MUST be emitted through `html/template`'s contextual autoescaping for its position, and a `Home` or a `page.Level`'s `URL` whose scheme `html/template` does not allow (for example `javascript:`) MUST appear as `#ZgotmplZ`.
- R-FE1P-CVXU: When the `page.Banner`'s `Home` is not empty, `banner` MUST emit it, escaped as R-FCTS-Z475 states.
- R-KEI6-W1CP: `banner`, executed with a `page.Banner`, MUST emit each element of the `Banner`'s `Trail` as its `Name` and, for every element but the last, its `URL`, each escaped as R-FCTS-Z475 states.
- R-KFQ3-9T3E: When the `Name` of each element of the `page.Banner`'s `Trail` and the `URL` of each element but the last are each made only of ASCII letters, digits, `/`, `-`, and `_`, none occurring in the output of `banner` executed with the same `Banner` but an empty `Trail`, and none occurring within another of them, the first occurrence of each element's `Name` in `banner`'s output MUST follow the first occurrence of the previous element's `Name`.
- R-KGXZ-NKU3: When the `Name` of each element of the `page.Banner`'s `Trail` and the `URL` of each element but the last are each made only of ASCII letters, digits, `/`, `-`, and `_`, none occurring in the output of `banner` executed with the same `Banner` but an empty `Trail`, and none occurring within another of them, the first occurrence of each such `URL` in `banner`'s output MUST precede the first occurrence of its own element's `Name` and follow the first occurrence of the previous element's `Name`, if there is a previous element.
- R-NNXD-MOSC: When the `URL` of the last element of the `page.Banner`'s `Trail` is made only of ASCII letters, digits, `/`, `-`, and `_`, and does not occur in the output of `banner` executed with the same `Banner` but that `URL` replaced by another value made only of those characters, `banner`'s output MUST NOT contain that `URL`.
- R-FIXA-VYWM: `banner`, executed with a `page.Banner`, MUST emit `/about`, whatever the `Banner`'s `Trail`, `Tools`, `Home`, and `Services`.
- R-FLD3-NIE0: When the `page.Banner`'s `Tools` is true, `banner` MUST emit `/tools`, whatever the `Banner`'s `Trail`.
- R-FML0-1A4P: When the `page.Banner`'s `Tools` is false and no value of the `Banner`, of its `Services`, or of its `Trail` contains `/tools`, `banner`'s output MUST NOT contain `/tools`.
- R-FNSW-F1VE: When the `page.Banner`'s `Trail` has exactly one element and its `URL` is `/about`, `banner`'s output MUST differ from its output for the same `Banner` with that `URL` replaced by a URL other than `/`, `/tools`, and `/about`, whether `Tools` is true or false.
- R-FP0S-STM3: When the `page.Banner`'s `Tools` is true and its `Trail` has exactly one element whose `URL` is `/tools`, `banner`'s output MUST differ from its output for the same `Banner` with that `URL` replaced by a URL other than `/`, `/tools`, and `/about`.
- R-FQ8P-6LCS: When the `page.Banner`'s `Tools` is true and its `Trail` has exactly one element, `banner`'s output when that element's `URL` is `/tools` MUST differ from its output when that `URL` is `/about`, all else equal.
- R-FRGL-KD3H: When the `page.Banner`'s `Tools` is false and its `Trail` has exactly one element whose `URL` is `/tools`, `banner`'s output MUST equal its output for the same `Banner` with that `URL` replaced by a URL other than `/`, `/tools`, and `/about`.
- R-FSOH-Y4U6: When the `page.Banner`'s `Trail` has exactly one element whose `URL` is none of `/`, `/tools`, and `/about`, `banner`'s output MUST equal its output for the same `Banner` with that `URL` replaced by any other URL that is none of those three.
- R-WP9A-D906: When the `page.Banner`'s `Trail` has more than one element, `banner`'s output MUST be the same whatever the `URL` of the last element of that `Trail`.
