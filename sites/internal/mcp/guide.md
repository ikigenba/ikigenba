# Sites MCP Guide

## Model
A site is a slug plus `public` or `private` visibility plus its creator. Files are served live at the site's URL. `create` is the only way a site comes into being; every other tool (`file_write`, `file_read`, `file_edit`, `file_glob`, `file_grep`, `file_list`, `mkdir`, `sync`, `set_visibility`, `delete`) requires the site to already exist and returns `not_found` otherwise.

## Slug Rules
Slugs are 1-63 characters, lowercase alphanumeric plus hyphen, and must start alphanumeric. Reserved names are rejected.

## Confinement
File paths are relative to the site root. Absolute paths and `..` escapes are rejected with `path_escapes_working_dir`.

## Basic - Public Page In Two Calls
Call `create(name:"launch", public:true)`, then `file_write(site:"launch", file_path:"index.html", content:"...")`, then visit the returned `url`.

## Basic - Private Site
Call `create(name:"runbook")` to make a private site, write files, and serve them only to logged-in users. Promote later with `set_visibility(name:"runbook", public:true)`.

## Advanced - Import From Dropbox
Call `create(name:"marketing")` first because `sync` will not create it, then `sync(source_path:"/sites/marketing")`, then `set_visibility(name:"marketing", public:true)`.

## Error Self-Correction
`not_found` means call `create` first. Other stable codes include `already_exists`, `invalid_slug`, and `reserved_name`.
