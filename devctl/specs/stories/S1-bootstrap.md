# Stories — bootstrap

Running devctl at all: help, version, exit codes, the refusal to run as root,
and the one file every cloud command reads. Every later group adds a command
to this frame.

devctl has no configuration of its own. The platform has one root domain in
one AWS account, and the checkout states it once, in
`infra/terraform.tfvars.json`, the same file Terraform reads:

```json
{
  "domain": "ikigenba.dev",
  "region": "us-east-2"
}
```

Every command that touches AWS or a host finds the checkout it is run inside,
reads that file, and takes everything from it: `domain` is the root under
which every space lives and the name of the AWS shared-config profile devctl
acts through, and `region` is where everything is. No profile name and no
account id is written anywhere in devctl; the account is whatever that profile
reaches, and devctl learns its id by asking. `help`, `version`, and `build`
do not read the file. Nothing is kept on the developer's machine between
runs; the checkout and the cloud are the only state.

## A developer asks devctl what it can do

A developer who has just built devctl, or who has forgotten a command, asks
for the usage text. Each later group adds one line under `Commands:`; nothing
else in the text changes. The single-dash forms of the two options are
accepted and not listed.

Command:

```
$ devctl --help
```

Output:

```
Usage: devctl [options] <command> [arguments]

Manage the platform from the developer's machine. Never run as root.

Commands:
  version   print the version

Options:
  --help              print this help
  --version           print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer asks which devctl they have

The version is a `var` in the source with a `v<major>.<minor>.<patch>` shape,
never injected at build time, so a developer's build and a release report the
same string.

Command:

```
$ devctl version
```

```
$ devctl --version
```

Output:

```
v0.1.0
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer runs devctl with no command

Command:

```
$ devctl
```

Output:

```
devctl: no command given

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. The usage text itself is
never written to stderr.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ devctl bogus
```

Output:

```
devctl: unknown command 'bogus'

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer mistypes an option

Command:

```
$ devctl --bogus
```

Output:

```
devctl: unknown option '--bogus'

see 'devctl --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer runs devctl as root by mistake

devctl acts under the developer's own identity and writes nothing a root user
should own, so running it as root is a mistake it refuses before it looks at
the arguments, help and version included.

Command:

```
$ sudo devctl version
```

```
$ sudo devctl --help
```

```
$ sudo devctl bogus
```

```
$ sudo devctl
```

Output:

```
devctl: must not run as root
```

Exits 3. The line is on stderr; stdout is empty. The arguments were not
looked at.

Preconditions:

- `bin/devctl` exists.
- The effective user id is 0.

Postconditions:

- Nothing has changed.

## A developer runs a cloud command outside the checkout

Every command that touches AWS needs the root, and the root is in the
checkout. Outside one there is nothing to read, so the command stops before
it opens a connection to anything. `space list` stands here for every such
command; the refusal is the same for all of them.

Command:

```
$ cd /home/me && devctl space list
```

Output:

```
devctl: '/home/me' is not inside a git checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- `bin/devctl` exists.
- `/home/me` is not inside a git working tree.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer runs a cloud command in a checkout that has no root file

Command:

```
$ devctl space list
```

Output:

```
devctl: no infra/terraform.tfvars.json in the checkout
```

Exits 2. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside a git checkout, and
  `infra/terraform.tfvars.json` does not exist at that checkout's top level.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer runs a cloud command when the root file is malformed

The file is Terraform's and a hand edit can break it. A file that is not a
JSON object, or one missing either of its two keys, or one whose value is not
a string, is refused with what is wrong.

Command:

```
$ devctl space list
```

Output:

```
devctl: infra/terraform.tfvars.json: missing 'region'
```

Exits 2. The line is on stderr; stdout is empty. A file that is not JSON says
`devctl: infra/terraform.tfvars.json: not a JSON object`, and a key of the
wrong type says `devctl: infra/terraform.tfvars.json: 'domain' is not a
string`, also exit 2.

Preconditions:

- The working directory is inside a git checkout whose
  `infra/terraform.tfvars.json` is `{"domain": "ikigenba.dev"}`.

Postconditions:

- Nothing has changed. No AWS call was made.
