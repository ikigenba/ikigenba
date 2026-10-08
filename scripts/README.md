> [!WARNING]
> This is unsupported AI slop.

# scripts

`scripts` is the Ikigenba suite's script runner, served at `scripts.<host>`.
A script names a git repository that `repos` holds and the ref it runs (by
default `main`); a run unpacks that ref's commit and runs its `main.py`
with Python 3.12, keeping its input, output and files. Agents create, list,
show, update, delete and run scripts, and read and cancel their runs, through
nine MCP tools, `list`, `show`, `create`, `update`, `delete`, `run`, `runs`,
`result` and `cancel`, reached through the MCP gateway. Its home page lists
the user's scripts and their last runs, with a page for each script and each
run.

## Installing it

`scripts` runs on an Ikigenba host beside `repos`, whose repositories it reads
with the `git` the host provides, and runs scripts with the host's
`python3.12`. From a checkout, build the release and deploy it to a space with
[`devctl`](../devctl):

```sh
devctl build <sha|tag>
devctl deploy <space> <sha|tag>
```

## Using it

```sh
scripts --help
```
