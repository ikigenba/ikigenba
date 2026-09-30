# D2-services

A `Kit` is what an app holds to build its banner and footer. The app makes
one with `New`, naming its own service and its release version, and asks it
for a `Banner` on every page, passing a `User`: the signed-in person's email
and the URLs of their profile and of sign-out. The returned `Banner` is the
data the `banner`, `launcher`, and `footer` templates (D3) render: the app's
service name and version, the user's three values, and the services the
launcher offers. The version is whatever string the app declares (its
`--version` output, say); appkit carries it unaltered and never parses it.

## The services file

The host's installer, opsctl, publishes a services file and gives every app
its path in the environment variable `IKIGENBA_SERVICES` (normally
`/var/lib/ikigenba/services.json`). appkit reads it as a published external
format; the requirements below are appkit's reader contract for it, and
appkit relies on nothing else about opsctl.

The file is a JSON object whose member `services` is an array. Each element
describes one service with the members `name` (string), `url` (string),
`icon` (string, the SVG text of the service's icon), and `enabled` (boolean;
`false` for a service switched off). The format only grows: a reader ignores
members it does not know, at the top level and in an entry. The file's order
is the launcher's order.

`New` reads the variable once, when it is called, and keeps the path; it has
no default path. `Banner` reads the file on every call, so a rewrite of the
file shows on the next page without a restart. An element is usable only when
it is an object whose four members have the types above and whose `name` is
not empty; an unusable element is skipped and the rest are still shown. When
there is no variable, no readable file, no object with a `services` array,
or no usable element, the `Banner` has no services and the page renders
without a launcher. None of this is reported: the reader has no error to
return and writes no log. A broken launcher must never break a page.

The icon is carried as `template.HTML`, so the templates insert it verbatim.
That trust is deliberate: opsctl validates every icon when it installs a
service, and the file is written only by opsctl.

## Tests and the environment

The tests reach the variable the way an app does, by setting it in the test
process's environment (`testing.T.Setenv`) before calling `New` and pointing
it at a file in the test's temporary directory. This is the whole injection
seam: because `New` reads the variable exactly once and keeps only the path,
nothing further is needed, and the exported surface carries no test-only
option.

## REQUIREMENTS

- R-69LO-BOP5: Package `appkit` MUST export `type User struct { Email, ProfileURL, LogoutURL string }`, with exactly these fields in this order.
- R-6ATK-PGFU: Package `appkit` MUST export `type Service struct { Name, URL string; Icon template.HTML; Enabled, Current bool }`, with exactly these fields in this order, where `template` is the standard library's `html/template`.
- R-AYMG-TUX2: Package `appkit` MUST export `type Banner struct { Service, Version, Email, ProfileURL, LogoutURL string; Services []Service }`, with exactly these fields in this order.
- R-AZUD-7MNR: Package `appkit` MUST export type `Kit`, `func New(service, version string) *Kit`, and the method `func (k *Kit) Banner(u User) Banner`.
- R-6FP6-8JEM: `New` MUST read the environment variable `IKIGENBA_SERVICES` exactly once, during the call, and the returned `Kit` MUST use the value read then as the services file path, so setting, changing, or unsetting the variable after `New` returns does not change what that `Kit`'s `Banner` returns.
- R-B129-LEEG: `Kit.Banner` MUST return a `Banner` whose `Service` and `Version` are the `service` and `version` passed to `New` and whose `Email`, `ProfileURL`, and `LogoutURL` are `u`'s fields of the same names, each unaltered.
- R-6I4Z-02W0: Every `Kit.Banner` call MUST read the services file anew at the path `New` read, as given (a relative path resolves against the process working directory at the time of the call), so a change to the file's content, or its appearance or removal, between two calls is reflected in the second call's `Services`.
- R-Z24E-CK72: `Kit.Banner` MUST return `Services` of length zero when `IKIGENBA_SERVICES` was unset or empty when `New` read it; when the path names nothing, or something that cannot be read as a file (a directory included); when the file's content is not valid UTF-8, begins with a byte order mark, or is not exactly one JSON text (RFC 8259); when that JSON value is not an object; when the object has no `services` member or that member is not an array; or when the array has no usable element.
- R-6KKR-RMDE: An element of the `services` array MUST be usable exactly when it is a JSON object whose member `name` is a string other than the empty string, whose member `url` is a string, whose member `icon` is a string, and whose member `enabled` is `true` or `false`; an element that is not usable MUST contribute nothing to `Services` and MUST NOT prevent the usable elements from contributing.
- R-Z4K7-43OG: Members that R-Z24E-CK72 and R-6KKR-RMDE do not name, in the top-level object or in an element, MUST be ignored, so adding any such member of any type to a file does not change the `Services` that `Kit.Banner` returns for it; when one object, at any level, holds a member name more than once, only the last occurrence of that name MUST count.
- R-6N0K-J5US: `Kit.Banner` MUST return one `Service` per usable element, in the order the elements appear in the array, duplicates by name included, with `Name` the decoded `name`, `URL` the decoded `url`, `Icon` the decoded `icon` string unaltered, `Enabled` the value of `enabled`, and `Current` true exactly when `Name` equals the `service` passed to `New`.
- R-Z5S3-HVF5: `New` and `Kit.Banner` MUST NOT panic and MUST NOT write to standard output, to standard error, or through the standard library `log` package's default logger, in any of the cases R-Z24E-CK72 and R-6KKR-RMDE describe.
- R-B2A5-Z655: The zero value of `Kit` MUST behave as the `Kit` that `New("", "")` returns when `IKIGENBA_SERVICES` is unset: its `Banner` returns `Service` and `Version` empty and `Services` of length zero.
- R-6QO9-OH2V: `Kit.Banner` MUST be safe to call concurrently from multiple goroutines on the same `Kit`.
