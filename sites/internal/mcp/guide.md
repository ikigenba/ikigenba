# Sites MCP Guide

## Model
A site is a slug plus a visibility (`public`, `private`, or `unlisted`) plus its creator. Files are served live at the site's URL. `create` is the only way a site comes into being; every other tool (`file_write`, `file_read`, `file_edit`, `file_glob`, `file_grep`, `file_list`, `mkdir`, `sync`, `set_visibility`, `delete`) requires the site to already exist and returns `not_found` otherwise. Visibility is always stated explicitly; there is no default. Public and private sites have a name you choose. An unlisted site's name is generated for you as a long random string nobody can guess, so its URL is the credential: share the secret link only with people who should see it.

## Slug Rules
Slugs are 1-63 characters, lowercase alphanumeric plus hyphen, and must start alphanumeric. Reserved names are rejected. Never supply a name when creating an unlisted site.

## Confinement
File paths are relative to the site root. Absolute paths and `..` escapes are rejected with `path_escapes_working_dir`.

## Basic - Public Page In Two Calls
Call `create(name:"launch", visibility:"public")`, then `file_write(site:"launch", file_path:"index.html", content:"...")`, then visit the returned `url`.

## Basic - Private Site
Call `create(name:"runbook", visibility:"private")`, write files, and serve them only to logged-in users. Promote later with `set_visibility(name:"runbook", visibility:"public")`.

## Basic - Secret Link
Call `create(visibility:"unlisted")` with no name, write files using the generated token as the site name, and share the returned secret `url`. Rotate a leaked link with `set_visibility(name:"<token>", visibility:"unlisted")`; the site receives a fresh URL and the old one goes dead. To leave unlisted, choose a real name with `set_visibility(name:"<token>", visibility:"public", new_name:"launch")`.

## Advanced - Import From Dropbox
Call `create(name:"marketing", visibility:"private")` first because `sync` will not create it, then `sync(source_path:"/sites/marketing")`, then `set_visibility(name:"marketing", visibility:"public")`.

## Error Self-Correction
`not_found` means call `create` first. Other stable errors include `already_exists`, `invalid_slug`, and `reserved_name`. A missing visibility, a name supplied for unlisted, or a missing `new_name` when leaving unlisted returns `validation`.
