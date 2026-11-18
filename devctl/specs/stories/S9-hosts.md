# Stories — hosts

The accounts service runs hosts, EC2 machines of their own, from the one space
whose role AWS lets do so: the hosts operator. `hosts` is the command that
makes a space the hosts operator and stops it being one, from the developer's
machine. The hosts operator's role, `<space domain>`, carries the
hosts-operator tag, `HostsOperator=true`, and has the managed policy
`ikigenba.dev-hosts-operator` attached, the policy Terraform made for it,
named after the root as the permissions boundary is. At most one space is the
hosts operator. The tag and the attachment change only through `hosts set`,
`hosts clear`, and `space destroy` of the hosts operator, which detaches the
policy before it deletes the role and its tag with it. `space create`, `space
replace`, and the `apex` commands leave both as they find them: a new space's
role is new and untagged, so a new space is never the hosts operator; replace
keeps the space's role, so a replaced space stays the hosts operator if it
was; and `apex` puts and narrows only the role's inline policy `space`. A
space destroyed and created again at the same label is not the hosts operator
until `hosts set` names it. Nothing here touches a space's host: no ssh
connection is opened, and the space may be running, stopped, or left with no
instance by a failed replace. `hosts set` needs the space; `hosts clear` needs
only its role, so it also clears a role that a destroy which failed part-way
left with no instance and no address.

## A developer asks what `hosts` can do

The top-level usage gains the line `  hosts     make one space the hosts
operator, or stop it being one` under `Commands:`.

Command:

```
$ devctl hosts --help
```

Output:

```
Usage: devctl hosts <subcommand> [arguments]

Make one space the hosts operator, or stop it being one. The hosts operator's
role has the managed policy <root>-hosts-operator, which lets the accounts
service on that space run hosts; at most one space is the hosts operator.

Subcommands:
  set <space>     make <space> the hosts operator
  clear <space>   stop <space> being the hosts operator

Run 'devctl hosts <subcommand> --help' for details.
```

Exits 0. The text is on stdout; stderr is empty.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed.

## A developer makes a space the hosts operator

A developer wants the accounts service on `us` to run hosts. Each line of
output is one step; the last line is the space. The `tag` step tags the
space's role as the hosts operator first, so a set interrupted before the
policy is attached already stands in the way of a set on another space. The
`role` step then attaches the managed policy to the space's role.

Every step runs every time, whether or not the space is already the hosts
operator: re-tagging and re-attaching change nothing, and the output has one
shape. Setting the space that is already the hosts operator prints the same
lines and succeeds.

Command:

```
$ devctl hosts set us
```

```
$ devctl hosts set us.ikigenba.dev
```

Output:

```
tag: ok (us.ikigenba.dev tagged hosts operator)
role: ok (ikigenba.dev-hosts-operator attached to us.ikigenba.dev)
us.ikigenba.dev
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev`, and a live SSO session for the profile `ikigenba.dev`.
- The space `us.ikigenba.dev` exists, and its role `us.ikigenba.dev`
  exists.
- Terraform has been applied: the account holds the managed policy
  `ikigenba.dev-hosts-operator`.
- No other space is the hosts operator.

Postconditions:

- The role `us.ikigenba.dev` carries the tag `HostsOperator=true`, and no
  other space's role does.
- The managed policy `ikigenba.dev-hosts-operator` is attached to the role
  `us.ikigenba.dev`. Its inline policy `space`, its permissions boundary,
  and its instance profile are as they were.
- No other role, record, or host has changed. No ssh connection was opened.

## A developer makes a second space the hosts operator

Unlike `apex set`, which moves the apex from its old holder, `hosts set`
does not take the role from another space: a set aimed at the wrong space
would strip the hosts operator's rights from production. The developer
clears the old one first, on purpose, and then sets the new one.

Command:

```
$ devctl hosts set sbx1
```

Output:

```
devctl: 'us.ikigenba.dev' is the hosts operator

run 'devctl hosts clear us' first
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space `sbx1.ikigenba.dev` exists.
- The role `us.ikigenba.dev` carries the tag `HostsOperator=true`.

Postconditions:

- Nothing has changed. Neither role's tags nor either role's policies were
  touched.

## A developer makes a space that does not exist the hosts operator

Command:

```
$ devctl hosts set gone
```

Output:

```
devctl: no space at 'gone.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance that is not terminated and no Elastic IP is tagged
  `Space=gone.ikigenba.dev`.

Postconditions:

- Nothing has changed.

## A developer makes a space the hosts operator before Terraform has been applied

The managed policy is looked up by its name before any step runs, as create
looks up the launch template and the permissions boundary.

Command:

```
$ devctl hosts set us
```

Output:

```
devctl: no managed policy 'ikigenba.dev-hosts-operator'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The space `us.ikigenba.dev` exists, and the account has no managed policy
  named `ikigenba.dev-hosts-operator`.

Postconditions:

- Nothing has changed.

## A developer's `hosts set` fails part-way

The space's role is tagged and the policy is not attached. The tag already
keeps any other space from being set, so the developer runs the same command
again, which repeats both steps.

Command:

```
$ devctl hosts set us
```

Output:

```
tag: ok (us.ikigenba.dev tagged hosts operator)
devctl: iam AttachRolePolicy: Throttling
```

Exits 1. The `ok` line is on stdout; the last line is on stderr.

Preconditions:

- As for a set that succeeds, and IAM throttles the attachment.

Postconditions:

- The role `us.ikigenba.dev` carries the tag `HostsOperator=true`; the managed
  policy is not attached to its role.
- `hosts set` on another space is refused naming `us.ikigenba.dev`, and
  `hosts clear us` clears it.

## A developer stops a space being the hosts operator

The reverse of `set`, in the reverse order: the policy is detached first, so
the role loses its rights before the space stops being marked as holding
them.

Command:

```
$ devctl hosts clear us
```

```
$ devctl hosts clear us.ikigenba.dev
```

Output:

```
role: ok (ikigenba.dev-hosts-operator detached from us.ikigenba.dev)
tag: ok (us.ikigenba.dev untagged)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The role `us.ikigenba.dev` exists, carries the tag `HostsOperator=true`, and
  has the managed policy `ikigenba.dev-hosts-operator` attached.

Postconditions:

- The managed policy `ikigenba.dev-hosts-operator` is not attached to the
  role `us.ikigenba.dev`; its inline policy `space`, its permissions
  boundary, and its instance profile are as they were.
- The role `us.ikigenba.dev` does not carry the tag `HostsOperator=true`. No
  space is the hosts operator, and `hosts set` on any space is no longer
  refused.
- No other role, record, or host has changed. No ssh connection was opened.

## A developer clears a space that is not the hosts operator

Each step finds nothing to undo and says so; the command succeeds.

Command:

```
$ devctl hosts clear sbx1
```

Output:

```
role: ok (already detached)
tag: ok (already gone)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- The role `sbx1.ikigenba.dev` exists, and it neither carries the tag
  `HostsOperator=true` nor has the managed policy
  `ikigenba.dev-hosts-operator` attached.

Postconditions:

- Nothing has changed. Whichever space is the hosts operator, if any, still
  is.

## A developer clears the hosts operator a failed destroy left as a role

`space destroy us` failed after its `address` step (see "A developer's
destroy fails part-way" in `S2-space-lifecycle.md`): the instance is
terminated and the address released, so `us` is no longer a space, but its
role is still there, tagged and with the policy attached. `hosts set` on any
other space is refused naming `us.ikigenba.dev` until this is cleared, and
`hosts clear us` clears it as it clears a space.

Command:

```
$ devctl hosts clear us
```

Output:

```
role: ok (ikigenba.dev-hosts-operator detached from us.ikigenba.dev)
tag: ok (us.ikigenba.dev untagged)
```

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No instance that is not terminated and no Elastic IP is tagged
  `Space=us.ikigenba.dev`. The role `us.ikigenba.dev` exists, carries the tag
  `HostsOperator=true`, and has the managed policy
  `ikigenba.dev-hosts-operator` attached.

Postconditions:

- As for clearing a space: the role carries no tag `HostsOperator=true` and
  has no managed policy attached, and no space is the hosts operator.
- The role itself is still there; `space destroy us` run again deletes it,
  with no `hosts` line.

## A developer clears a space that has no role

`hosts clear` acts on the role named after the space, and with no such role
there is nothing to clear.

Command:

```
$ devctl hosts clear gone
```

Output:

```
devctl: no role for 'gone.ikigenba.dev'
```

Exits 1. The line is on stderr; stdout is empty.

Preconditions:

- The working directory is inside the checkout, and a live SSO session for
  the profile `ikigenba.dev`.
- No role named `gone.ikigenba.dev` exists.

Postconditions:

- Nothing has changed.

## A developer names something that is not a space

The operand is `<space>`, read as every command that takes one reads it (see
`S2-space-lifecycle.md`).

Command:

```
$ devctl hosts set crm.us
```

Output:

```
devctl: 'crm.us' is not a space: a space is one label under 'ikigenba.dev'
```

Exits 2. The line is on stderr, naming the operand as typed; stdout is empty.
`devctl: 'Us_1' is not a valid label` refuses a bad label, and `hosts clear`
refuses the same operands the same way.

Preconditions:

- The working directory is inside the checkout, whose root file names
  `ikigenba.dev`.

Postconditions:

- Nothing has changed. No AWS call was made.

## A developer runs `hosts set` or `hosts clear` without a space

Command:

```
$ devctl hosts set
```

Output:

```
devctl: hosts set needs <space>

see 'devctl hosts --help' for usage
```

Exits 2. The text is on stderr; stdout is empty. `hosts clear` says
`devctl: hosts clear needs <space>`.

Preconditions:

- `bin/devctl` exists.

Postconditions:

- Nothing has changed. No AWS call was made.
