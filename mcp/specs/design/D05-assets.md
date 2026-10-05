# D05-assets

The connect page takes the platform's visual style and its service launcher from files mcp does not author: the stylesheet the page links, the fonts that stylesheet loads, the launcher's script, the button-feedback script the page links, and the licences of the fonts and of the Tabler icons the style draws. They are the platform's shared web files, the same for every app, and they come from appkit's `page` package (`github.com/ikigenba/ikigenba/appkit/page`), which embeds them and serves them through the handler `page.Static()` returns, under the prefix `page.StaticPrefix`, `/_appkit/`. This design is how the gateway's handler mounts that handler and what a caller observes of it through mcp.

mcp holds no copy of any of these files. Its own `assets/` directory holds only its templates, the connect page's (`D04-connect`) and the setup files' (`D09-setup`), none of which is served as a file; mcp has no `/assets/` route and no extension-to-`Content-Type` table of its own, and a path under `/assets/` is an ordinary path that does not exist, answered with the gateway's 404 that `D04-connect` defines.

## Delegation, not restatement

Everything `page.Static` answers is appkit's contract (its `D04-page-static`). mcp restates none of it. Its one obligation is to hand every request under the prefix to appkit's handler unchanged — the whole `URL.Path`, prefix included, since appkit's handler matches the full path itself — and to send back exactly what that handler answers. The requirement below states that as an equality a test can check without knowing any of appkit's rules: for each request the assets stories show, and for paths a careless mount would mishandle, the test sends the same request to the gateway's handler and to a handler `page.Static()` returned, and compares the status, every header field and the body.

The careless mounts are the ones Go's `http.ServeMux` would supply: a pattern for the prefix answers the prefix without its trailing slash, `/_appkit`, with a redirect, and a path holding `.` or `..` segments, such as `/_appkit/../theme.css`, with a redirect to its cleaned form. The first is not an appkit path, so it is the gateway's 404 (`D04-connect`); the second is an appkit path, so it is answered as appkit's handler answers it, which is never a redirect. The test sends both.

## Served to guests

The shared files hold nothing of anyone's, and any page of the platform may ask for them, a guest's included. Every path but the MCP paths is under appkit's `identity.Optional` (`D03-serve`), so a request under `/_appkit/` is answered by appkit's static handler whether or not it carries an `X-User-Id`, byte for byte the same: a guest's `GET`, `HEAD`, revalidation, refused method and missing file are answered as a user's. Each is recorded in the trail by its `request.started` and `request.finished` (R-O1E7-TCYD), under an empty user for a guest. A request under `/_appkit/` contacts no backend and sets no cookie, which `D04-connect` states for every path outside the MCP endpoint.

## REQUIREMENTS

- R-S0LT-03HX: mcp's design defines an **appkit path** as a request's URL path, as `net/http` decodes it into `URL.Path`, that begins with the value of `page.StaticPrefix`, where `page` is the package `github.com/ikigenba/ikigenba/appkit/page`; and every requirement of mcp's design that names an appkit path MUST denote such a path.
- R-YTQ0-53GB: A handler `gateway.Handler(cfg)` returns MUST answer every request whose URL path is an appkit path, whatever its method, its identity headers, its other headers and its body, a request whose `X-User-Id` header is absent or whose first `X-User-Id` value is empty included, exactly as the `http.Handler` that `page.Static()` returns answers the same request: with the same status, the same set of header fields with the same values, and the same body.
