# The story format

See `../SKILL.md` for the layout. Stories live in `specs/stories/`; they are
the input to `specs/design/` and the `draft-stories` skill authors them.

A story is the intent the sub-project is built to serve, written before any
design. It says who wants what, the preconditions in the system, the exact
interaction, what each option does, and the postconditions once the
interaction has run. It is concrete: literal command lines, literal output,
literal exit codes. A reader who has only the story can sit at a terminal and
check whether the sub-project does what it says.

Stories carry no requirement ids. A design realises a group of stories and
mints the ids there. Stories are never deleted, designed or not, and when a
design changes its stories are updated to match, so `specs/stories/` always
describes the current intent.

## Layout

- `specs/stories/S<int>-<slug>.md` — one group of related stories. A group
  is one coherent seam a single design would realise: one command and its
  subcommands, one lifecycle, one store.

The number orders the groups: it is the order they are meant to be
designed, and a group comes after the groups it builds on. The rules match
the design filename:

- `<int>` is the next group number: the max existing number in
  `specs/stories/` plus 1 (start at 1 if none).
- Zero-pad `<int>` so every file in the folder is the same width. If a new
  number needs more digits, re-pad the others to match. Padding is
  cosmetic; the number is the identity.
- `<slug>` is a short kebab-case name. It is not part of the identity.

There is no index file; the directory listing is the index.

## A group file

```
# Stories — <group>

<One paragraph: what this group covers, and the facts every story in it
shares — the shape of a resource, the one writer of some state, the
frame the group adds to.>

## <A story>

## <Another story>
```

The opening paragraph carries what the stories share so each story does not
repeat it. It states facts, not rationale; a story's own prose motivates it.

## A story

Each story is one `##` section. The heading is a sentence naming the actor
and what they do: `## A developer asks which devctl they have`,
`## An operator adds a record and takes it away again`, `## certbot asks for
a challenge record`. The actor is a role, never a person. Failure paths are
stories too, one per distinct failure the actor can cause or meet: `## A
developer gives the account option no value`, `## A developer builds an app
that is not in the checkout`.

Under the heading, in this order:

1. **Prose.** Why the actor does this and the facts the story fixes: what a
   name means, where a thing is read from, what is deliberately not looked
   at. Short; often one paragraph, sometimes none for a plain failure case.
   A fixture the story depends on (a manifest, a config file) is shown here
   in a fenced block.

2. **`Command:`** then one fenced block per form, each a `$ ` line. Several
   blocks list alternate spellings that behave identically (`devctl version`
   and `devctl --version`). Placeholders are `<name>`.

3. **`Options:`** when the command has options this story exercises: a bullet
   per option saying what it does. When a group's command has several
   subcommands with their own options, label the block: `Options (init):`.

4. **`Output:`** then one fenced block holding the exact text. For output
   that varies, the prose or a placeholder says how. Where a story's output is
   too long to quote whole, a line beside the label says what it consists of.

5. **The exit line.** `Exits <n>. The text is on stdout; stderr is empty.`
   Always says the code and which stream carries what. Variants: `The line is
   on stderr; stdout is empty.`, `The `ok` lines are on stdout; the last line
   is on stderr.`, `Nothing is on stdout; stderr is empty.`

6. **`Preconditions:`** bullets. The state of the system before the command:
   what exists, what is configured, who the effective user is. Every story
   has at least one.

7. **`Postconditions:`** bullets. What has changed once the command has run.
   A read-only story says `Nothing has changed.`; a story that deliberately
   does not touch something says so (`No AWS call was made; the profile name
   was accepted, not checked.`).

A story states the interaction from the outside only. It never names a
package, a function, a file the code is in, or how the behavior is
implemented or tested; those belong to the design. It does name the things the actor
can see: paths on disk the command reads or writes, environment it consults,
a resource's identifier scheme.

## For a package

The format above assumes a CLI. A sub-project whose
consumer is another program, not a person at a terminal, keeps the same
sections with the interaction as consumer code: the `Command:` block is the
call as the consumer writes it, `Output:` is what it returns or the error it
surfaces, and the exit line is replaced by a sentence naming the return.
Preconditions and postconditions are unchanged.

## For a web app

A sub-project whose consumer is an HTTP client keeps the same sections with
the interaction as a request. `Command:` becomes `Request:`, one fenced block
per form, each a `$ curl -si` line so the verb, path, and any header the story
depends on are explicit and the line can be run as written. `Output:` becomes
`Response:`, a fenced block holding the status line and only the headers the
story fixes. The exit line becomes a status line: `Status 200.` followed by
what the body must satisfy, stated as a fact (`The body is an HTML page whose
visible text is ...`), never quoted whole. A header or body the story does not
mention is not fixed. Preconditions and postconditions are unchanged; a
read-only request says `Nothing has changed.`

## Example

`specs/stories/S01-bootstrap.md`:

````
# Stories — bootstrap

Running the tool at all: help, version, exit codes. Every later group adds a
command to this frame.

## A developer asks which version they have

The version is a `var` in the source with a `v<major>.<minor>.<patch>` shape,
never injected at build time, so a developer's build and a release report the
same string.

Command:

```
$ simple-go version
```

```
$ simple-go --version
```

Output:

```
v0.1.0
```

Exits 0. The line is on stdout; stderr is empty.

Preconditions:

- `bin/simple-go` exists, built from the checkout with `make`.

Postconditions:

- Nothing has changed.

## A developer mistypes a command

Command:

```
$ simple-go bogus
```

Output:

```
simple-go: unknown command 'bogus'

see 'simple-go --help' for usage
```

Exits 2. The text is on stderr; stdout is empty.

Preconditions:

- `bin/simple-go` exists.

Postconditions:

- Nothing has changed.
````
