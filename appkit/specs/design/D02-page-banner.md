# D02-page-banner

A `Kit` is what an app holds to build its banner and footer. The app makes one with `page.New`, naming its own service and its release version, and asks it for a `Banner` on every page, passing a `User`: the signed-in person's email and the URLs of their profile and of sign-out. The returned `Banner` is the data the `banner`, `launcher`, and `footer` templates (D03) render: the app's service name, its own icon, and its version, the user's three values, the services the launcher offers, the suite's home URL that the product mark links to, whether the app has MCP tools, and the page's trail. The version is whatever string the app declares (its `--version` output, say); `page` carries it unaltered and never parses it. `page` knows nothing about authentication: the app says who is signed in, typically from `identity.FromContext` (D06), and `page` only renders it.

The banner and the footer appear only on pages shown to a signed-in user. A page with no signed-in user, such as auth's sign-in page, carries neither, so `page` never has to render either one without a `User`.

## Where the launcher's services come from

The launcher lists the host's services from opsctl's services file, read through `services.Read` (D05), which owns the format and its reader contract. `page` adds one rule of its own: the launcher shows only the entries that carry an icon. That is opsctl's launcher opt-in — a service whose package ships an icon is offered in the launcher, and one without is still listed in the file (for the MCP gateway, say) but not in the launcher. The file's order is the launcher's order.

The app's own icon, which the banner's trail shows beside its name at the root (D03), comes from the same list: it is the `Icon` of the service the list marks `Current`, so it is the app's icon exactly when the services file lists the app with an icon. There is no second source and no argument to `New`; opsctl already validates every icon it installs. When the list has no current service, because the file does not name the app, names it without an icon, or cannot be read, the `Banner`'s `Icon` is empty and the trail's root shows the name alone. Should the launcher's services hold more than one marked `Current`, the icon is that of the first of them in the launcher's order, which skips every entry without an icon.

`New` reads the variable `services.Variable` once, when it is called, and keeps the path; there is no default path. `Banner` reads the file on every call, so a rewrite of the file shows on the next page without a restart. When the variable was unset, `services.Read` fails for any reason, or no entry carries an icon, the `Banner` has no services and no icon, and the page renders without a launcher. None of this is reported: `Banner` has no error to return and writes nothing. A broken launcher must never break a page.

The icon is carried as `template.HTML`, so the templates insert it verbatim. That trust is deliberate: opsctl validates every icon when it installs a service, and the file is written only by opsctl. `services.Entry` already types its icon `template.HTML` (D05), so `page` copies the value and never converts a string to trusted markup itself.

## Home, tools and the trail

The banner's product mark links to the suite's home: the URL of the services file's entry named `home`, the front door every space carries. It comes from the same `services.Read` call that fills the launcher, so it costs nothing more and needs no configuration. When the file has no `home` entry, or the entry is disabled, or the file cannot be read, `Home` is empty and the mark is not a link. Should the file name `home` more than once, the first entry so named decides, as `services.List.Find` (D05) picks the first.

`Tools` says whether the app has MCP tools, so the banner's page menu offers its tools page. It is the `MCP` flag of the services file's entry named as the app's service, read on the same call, whether or not that entry carries an icon or is enabled; no app passes it. With no such entry, or no readable file, it is false. Again the first entry so named decides.

The trail is the page's place in the app's hierarchy, below the app's landing page. Each `Level` is one step, a name and a URL; the last is the page being shown. The root, the landing page, is never a level: it is implied by `Service` and `Icon`. `Kit.Banner` cannot know which page is being rendered, so it always returns an empty `Trail`, and the app sets the levels on the returned `Banner` before rendering. A landing page, and a notice page such as not found, leaves it empty.

## Tests and the environment

The tests reach the variable the way an app does, by setting it in the test process's environment (`testing.T.Setenv`) before calling `New` and pointing it at a file in the test's temporary directory. Because `New` reads the variable exactly once and keeps only the path, nothing further is needed, and the exported surface carries no test-only option.

## REQUIREMENTS

- R-HT5B-WEOW: Package `page` MUST export `type User struct { Email, ProfileURL, LogoutURL string }`, with exactly these fields in this order.
- R-HUD8-A6FL: Package `page` MUST export `type Service struct { Name, URL string; Icon template.HTML; Enabled, Current bool }`, with exactly these fields in this order, where `template` is the standard library's `html/template`.
- R-F4AI-AQ0A: Package `page` MUST export `type Banner struct { Service string; Icon template.HTML; Version, Email, ProfileURL, LogoutURL string; Services []Service; Home string; Tools bool; Trail []Level }`, with exactly these fields in this order, where `template` is the standard library's `html/template`.
- R-F5IE-OHQZ: Package `page` MUST export `type Level struct { Name, URL string }`, with exactly these fields in this order.
- R-HY0X-FHNO: Package `page` MUST export type `Kit`, `func New(service, version string) *Kit`, and the method `func (k *Kit) Banner(u User) Banner`.
- R-HZ8T-T9ED: `page.New` MUST read the environment variable that `services.Variable` names exactly once, during the call, and the returned `Kit` MUST use the value read then as the services file path, so setting, changing, or unsetting the variable after `New` returns does not change what that `Kit`'s `Banner` returns.
- R-I0GQ-7152: `Kit.Banner` MUST return a `Banner` whose `Service` and `Version` are the `service` and `version` passed to `page.New` and whose `Email`, `ProfileURL`, and `LogoutURL` are `u`'s fields of the same names, each unaltered.
- R-I1OM-KSVR: Every `Kit.Banner` call MUST read the services file anew, as `services.Read` reads it, at the path `page.New` read, so a change to the file's content, or its appearance or removal, between two calls is reflected in the second call's `Services`.
- R-6XQZ-7ZFT: `Kit.Banner` MUST return in `Services` one `Service` for each `services.Entry` that `services.Read` returns for the path whose `HasIcon` is true, in the order `Read` returns them, duplicates by name included, with `Name`, `URL`, and `Enabled` the entry's fields of the same names, `Icon` the entry's `Icon` unaltered, and `Current` true exactly when `Name` equals the `service` passed to `page.New`; an entry whose `HasIcon` is false MUST contribute nothing.
- R-SHIJ-UIIT: `Kit.Banner` MUST return a `Banner` whose `Icon` is the `Icon`, unaltered, of the first element of the `Services` it returns whose `Current` is true, and whose `Icon` is empty when no element of those `Services` has `Current` true.
- R-I44F-CCD5: `Kit.Banner` MUST return `Services` of length zero when the variable was unset or empty when `page.New` read it, when `services.Read` returns an error for the path, or when no entry it returns has `HasIcon` true.
- R-I5CB-Q43U: `page.New` and `Kit.Banner` MUST NOT panic, whatever the variable's value and whatever the services file holds or whether it exists.
- R-I6K8-3VUJ: The zero value of `Kit` MUST behave as the `Kit` that `page.New("", "")` returns when the variable is unset: its `Banner` returns `Service` and `Version` empty and `Services` of length zero.
- R-I7S4-HNL8: `Kit.Banner` MUST be safe to call concurrently from multiple goroutines on the same `Kit`.
- R-F6QB-29HO: `Kit.Banner` MUST return a `Banner` whose `Home` is the `URL`, unaltered, of the first `services.Entry` whose `Name` is `home` among those `services.Read` returns for the path on that call, when that entry's `Enabled` is true, and empty when that entry's `Enabled` is false or no entry's `Name` is `home`, whatever any entry's `HasIcon`.
- R-F7Y7-G18D: `Kit.Banner` MUST return a `Banner` whose `Tools` is the `MCP` of the first `services.Entry` whose `Name` equals the `service` passed to `page.New` among those `services.Read` returns for the path on that call, whatever that entry's `Enabled` and `HasIcon`, and false when no entry's `Name` equals it.
- R-F963-TSZ2: `Kit.Banner` MUST return `Home` empty and `Tools` false when the variable was unset or empty when `page.New` read it, when `services.Read` returns an error for the path, and when called on the zero value of `Kit`.
- R-FAE0-7KPR: A change to the services file's content, or its appearance or removal, between two `Kit.Banner` calls MUST be reflected in the second call's `Home` and `Tools`.
- R-FBLW-LCGG: `Kit.Banner` MUST return a `Banner` whose `Trail` has length zero, whatever the services file holds.
