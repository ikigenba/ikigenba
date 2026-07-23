# Sites MCP Guide

## Model
A site has a display **name** (the free-form label used to tell sites apart), a **slug** (its URL address), a visibility (`public`, `private`, or `unlisted`), and its creator. Files are served live at the site's URL. `create` is the only way a site comes into being; every other tool (`file_write`, `file_read`, `file_edit`, `file_glob`, `file_grep`, `file_list`, `mkdir`, `sync`, `set_visibility`, `rename`, `delete`) requires the site to already exist and returns `not_found` otherwise. Visibility is always stated explicitly; there is no default. Every site gets a name you choose. Public and private sites also get a slug you choose. An unlisted site's slug is generated as a long random string nobody can guess, so its URL is the credential: share the secret link only with people who should see it. Tools address a site by its slug; `rename` changes only its display name.

## Slug And Name Rules
Slugs are 1-63 characters, lowercase alphanumeric plus hyphen, and must start alphanumeric. Reserved names are rejected. Never supply a slug when creating an unlisted site. Display names are free-form text up to 100 characters and cannot be blank; spaces and capitals are allowed.

## Confinement
File paths are relative to the site root. Absolute paths and `..` escapes are rejected with the `validation` code and `path_escapes_working_dir` detail.

## Basic - Public Page In Two Calls
Call `create(name:"Launch", slug:"launch", visibility:"public")`, then `file_write(site:"launch", file_path:"index.html", content:"...")`, then visit the returned `url`.

## Basic - Private Site
Call `create(name:"Runbook", slug:"runbook", visibility:"private")`, write files, and serve them only to logged-in users. Promote later with `set_visibility(slug:"runbook", visibility:"public")`.

## Basic - Secret Link
Call `create(name:"Client Preview", visibility:"unlisted")` with no slug, write files using the generated token as the `site`, and share the returned secret `url`. The URL is the credential: anyone with the link can view it. The site keeps its name through every transition, so listings show "Client Preview", not the token. Rotate a leaked link with `set_visibility(slug:"<token>", visibility:"unlisted")`; the site receives a fresh URL and the old one goes dead while its name survives. To leave unlisted, choose a real slug with `set_visibility(slug:"<token>", visibility:"public", new_slug:"launch")`.

## Advanced - Import From Dropbox
Call `create(name:"Marketing", slug:"marketing", visibility:"private")` first because `sync` will not create it, then `sync(source_path:"/sites/marketing")`, then `set_visibility(slug:"marketing", visibility:"public")`.

## Error Self-Correction
`not_found` means call `create` first. `conflict` means the slug already exists. `validation` covers an invalid or reserved slug, a missing visibility, a missing or blank name, a slug supplied for unlisted, or a missing `new_slug` when leaving unlisted.
