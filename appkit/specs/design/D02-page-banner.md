# D02-page-banner

A `Kit` is what an app holds to build its banner and footer. The app makes one with `page.New`, naming its own service and its release version, and asks it for a `Banner` on every page, passing a `User`: the signed-in person's email and the URLs of their profile and of sign-out. The returned `Banner` is the data the `banner`, `launcher`, and `footer` templates (D03) render: the app's service name and version, the user's three values, and the services the launcher offers. The version is whatever string the app declares (its `--version` output, say); `page` carries it unaltered and never parses it. `page` knows nothing about authentication: the app says who is signed in, typically from `identity.FromContext` (D06), and `page` only renders it.

The banner and the footer appear only on pages shown to a signed-in user. A page with no signed-in user, such as auth's sign-in page, carries neither, so `page` never has to render either one without a `User`.

## Where the launcher's services come from

The launcher lists the host's services from opsctl's services file, read through `services.Read` (D05), which owns the format and its reader contract. `page` adds one rule of its own: the launcher shows only the entries that carry an icon. That is opsctl's launcher opt-in — a service whose package ships an icon is offered in the launcher, and one without is still listed in the file (for the MCP gateway, say) but not in the launcher. The file's order is the launcher's order.

`New` reads the variable `services.Variable` once, when it is called, and keeps the path; there is no default path. `Banner` reads the file on every call, so a rewrite of the file shows on the next page without a restart. When the variable was unset, `services.Read` fails for any reason, or no entry carries an icon, the `Banner` has no services and the page renders without a launcher. None of this is reported: `Banner` has no error to return and writes nothing. A broken launcher must never break a page.

The icon is carried as `template.HTML`, so the templates insert it verbatim. That trust is deliberate: opsctl validates every icon when it installs a service, and the file is written only by opsctl.

## Tests and the environment

The tests reach the variable the way an app does, by setting it in the test process's environment (`testing.T.Setenv`) before calling `New` and pointing it at a file in the test's temporary directory. Because `New` reads the variable exactly once and keeps only the path, nothing further is needed, and the exported surface carries no test-only option.

## REQUIREMENTS

- R-HT5B-WEOW: Package `page` MUST export `type User struct { Email, ProfileURL, LogoutURL string }`, with exactly these fields in this order.
- R-HUD8-A6FL: Package `page` MUST export `type Service struct { Name, URL string; Icon template.HTML; Enabled, Current bool }`, with exactly these fields in this order, where `template` is the standard library's `html/template`.
- R-HVL4-NY6A: Package `page` MUST export `type Banner struct { Service, Version, Email, ProfileURL, LogoutURL string; Services []Service }`, with exactly these fields in this order.
- R-HY0X-FHNO: Package `page` MUST export type `Kit`, `func New(service, version string) *Kit`, and the method `func (k *Kit) Banner(u User) Banner`.
- R-HZ8T-T9ED: `page.New` MUST read the environment variable that `services.Variable` names exactly once, during the call, and the returned `Kit` MUST use the value read then as the services file path, so setting, changing, or unsetting the variable after `New` returns does not change what that `Kit`'s `Banner` returns.
- R-I0GQ-7152: `Kit.Banner` MUST return a `Banner` whose `Service` and `Version` are the `service` and `version` passed to `page.New` and whose `Email`, `ProfileURL`, and `LogoutURL` are `u`'s fields of the same names, each unaltered.
- R-I1OM-KSVR: Every `Kit.Banner` call MUST read the services file anew, as `services.Read` reads it, at the path `page.New` read, so a change to the file's content, or its appearance or removal, between two calls is reflected in the second call's `Services`.
- R-I2WI-YKMG: `Kit.Banner` MUST return in `Services` one `Service` for each `services.Entry` that `services.Read` returns for the path whose `HasIcon` is true, in the order `Read` returns them, duplicates by name included, with `Name`, `URL`, and `Enabled` the entry's fields of the same names, `Icon` the entry's `Icon` string unaltered, and `Current` true exactly when `Name` equals the `service` passed to `page.New`; an entry whose `HasIcon` is false MUST contribute nothing.
- R-I44F-CCD5: `Kit.Banner` MUST return `Services` of length zero when the variable was unset or empty when `page.New` read it, when `services.Read` returns an error for the path, or when no entry it returns has `HasIcon` true.
- R-I5CB-Q43U: `page.New` and `Kit.Banner` MUST NOT panic, whatever the variable's value and whatever the services file holds or whether it exists.
- R-I6K8-3VUJ: The zero value of `Kit` MUST behave as the `Kit` that `page.New("", "")` returns when the variable is unset: its `Banner` returns `Service` and `Version` empty and `Services` of length zero.
- R-I7S4-HNL8: `Kit.Banner` MUST be safe to call concurrently from multiple goroutines on the same `Kit`.
