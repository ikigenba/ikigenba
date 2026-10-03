> [!WARNING]
> This is unsupported AI slop.

# sites

`sites` is the Ikigenba suite's static site host, served at `sites.<host>`.
A site is a git repository that `repos` holds, published at one commit and
served at `sites.<host>/<slug>/`. A public site is open to anyone; a private
site asks a visitor to sign in to the space. Agents create, list, show,
publish, update and delete sites, and point the space's apex domain at one,
through seven MCP tools, `list`, `show`, `create`, `publish`, `update`,
`delete` and `apex`, reached through the MCP gateway. Its home page lists the
space's sites and says how to make one.

## Installing it

`sites` runs on an Ikigenba host beside `repos`, whose repositories it reads
with the `git` the host provides. From a checkout at a release tag, build it
and deploy it to a space with [`devctl`](../devctl):

```sh
devctl build sites
devctl deploy <space> sites/dist/sites-vX.Y.Z.tar.xz
```

## Using it

```sh
sites --help
```
