> [!WARNING]
> This is unsupported AI slop.

# repos

`repos` is the Ikigenba suite's home for git repositories, served at
`repos.<host>`. Each user's repositories are bare git repositories on the
host, which the user clones from and pushes to with ordinary git over HTTPS,
giving git their personal access token as the password. Agents create, list,
show, rename and delete them, and see how busy git is, through six MCP tools,
`list`, `show`, `status`, `create`, `rename` and `delete`, reached through the
MCP gateway. The suite's other apps on the same host read a repository
straight from its directory. Its home page says what it is and how to clone.

## Installing it

`repos` runs on an Ikigenba host, which provides the `git` it runs. From a
checkout at a release tag, build it and deploy it to a space with
[`devctl`](../devctl):

```sh
devctl build repos
devctl deploy <space> repos/dist/repos-vX.Y.Z.tar.xz
```

## Using it

```sh
repos --help
```
