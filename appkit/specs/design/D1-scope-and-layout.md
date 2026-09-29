# D1-scope-and-layout

`appkit` is a Go library that gives every Ikigenba app the same banner: the
shared stylesheet, fonts, and launcher script served from one fixed path
prefix; the `banner` and `launcher` templates; and a reader for the host's
services file that fills the launcher. Module path
`github.com/ikigenba/ikigenba/appkit`, its own `go.mod`, no `go.work`, the
standard library only. Every command runs from this sub-project directory.

The module is one package, `appkit`, at the module root, beside the
human-authored `assets/` directory it embeds. `assets/` is an input to the
spec: the build run reads it and never writes it. It holds the markup
template file `banner.html`, the launcher script `launcher.js`, and copies of
the repository's design files: `theme.css`, the three fonts, and their
licences. Because the assets are embedded, an app binary carries them and
never looks for them on disk.

appkit has three consumers. The app developer writes Go against the exported
surface. A person using an app sees the banner and the launcher in a browser.
The browser fetches the shared files under the prefix. appkit knows nothing
about authentication: the app says who is signed in and where that person's
profile and sign-out live, and appkit only renders it.

## The exported surface and who owns each name

- D2 (services and the banner value): `New`, `Kit` and its method `Banner`,
  `User`, `Banner`, `Service`.
- D3 (templates): `Templates`.
- D4 (static files): `StaticPrefix`, `Static`.

Nothing else is exported. The package is small — a handful of files — and
one concern: the shared banner.

## Consumer task: an app wires the banner

An app called `dummy` does five things, using only the names above.

1. At start-up it calls `appkit.New("dummy")` once and keeps the returned
   `*appkit.Kit`. `New` reads `IKIGENBA_SERVICES` from the environment then
   and there; the app's environment file sets it.
2. It builds its page templates on top of appkit's:
   `template.Must(appkit.Templates().ParseFS(pages, "templates/*.html"))`,
   where `pages` is the app's own embedded file system. Each page links the
   stylesheet itself with a `link` element whose `href` is
   `/_appkit/theme.css` (that is, `appkit.StaticPrefix` followed by
   `theme.css`), and places the banner with `{{template "banner" .Banner}}`.
3. It mounts the shared files: `mux.Handle(appkit.StaticPrefix,
   appkit.Static())`.
4. In each page handler, after its own authentication, it calls
   `kit.Banner(appkit.User{Email: email, ProfileURL: profile, LogoutURL:
   logout})` and stores the returned `appkit.Banner` in the page data's
   `Banner` field.
5. It executes the page template with that data. The rendered page carries
   the mark, the email linked to the profile, and the sign-out form; when the
   host's services file lists usable services it also carries the launcher
   button, the launcher, and the script tag, whose `src` the handler from step
   3 serves.

When the environment variable is unset, the file is missing or malformed, or
nothing in it is usable, step 4 still returns a `Banner` (with no services)
and step 5 still renders the page, without the launcher. Nothing is reported.

## REQUIREMENTS

- R-64Q2-SLQD: The module MUST be `github.com/ikigenba/ikigenba/appkit`, with its own `go.mod` that specifies a Go version and has no `require` directive, and no `go.work` file.
- R-Z0WH-YSGD: The module MUST contain exactly one non-test Go package, named `appkit`, whose files sit at the module root; its test files MAY declare package `appkit` or `appkit_test`.
- R-675V-K57R: Package `appkit` MUST embed at build time the files `assets/banner.html`, `assets/launcher.js`, `assets/theme.css`, `assets/InterVariable.woff2`, `assets/InterVariable-Italic.woff2`, `assets/JetBrainsMono.woff2`, `assets/OFL.txt`, and `assets/TABLER-LICENSE.txt`, and MUST NOT read any of them from the file system at run time, so `Templates` and `Static` behave identically whatever the process working directory is.
- R-68DR-XWYG: Package `appkit` MUST NOT export any package-level identifier other than `New`, `Kit`, `User`, `Banner`, `Service`, `Templates`, `StaticPrefix`, and `Static`, and `Kit` MUST have no exported field and no exported method other than `Banner`.
