Ikigenba is a platform for one business or workgroup that unifies the development, deployment and operation of a suite of services built primarily for agents to use. Its services are not designed to run alone: each runs behind the host's nginx, takes its caller's identity from auth, sends its trail of events to telemetry, and offers its tools to agents through the MCP gateway, and the cross-dependencies only grow from there. The point of the architecture is consistency: every service is built on appkit from the same patterns, so an agent that has developed or operated one already knows the shape of the next. Around the services sit the tools to develop, deploy and operate it, listed below.

The **project** is this git repository. A **sub-project** is a directory that holds `specs/` beside its own `AGENTS.md`, which declares its toolchain, test files, gates and commit conventions. This file is guidance for the whole project and never stands in for a sub-project's `AGENTS.md`.

## Sub-projects

Platform apps, each one Go binary behind the host's nginx:

- `auth` signs users in and answers nginx's identity subrequest for every other app.
- `telemetry` keeps the suite's trail of events; every service posts to it and agents query it.
- `mcp` is the MCP gateway: one endpoint through which agents reach every service's tools.
- `repos` is the suite's home for git repositories, served over smart HTTP and MCP.
- `sites` is the suite's static site host: it publishes a repository that `repos` holds at one commit, public or private.
- `scripts` runs Python scripts kept in `repos` on behalf of agents and keeps each run's input, output and files.
- `events` is the suite's internal event bus: services emit events to it and it delivers them to the services that accept them.
- `cron` keeps triggers, each a slug and a cron schedule, and emits an event on the bus each time one fires; agents subscribe scripts to those events.
- `home` is the suite's front door: a grid of every service on the space, served at the space host.
- `webhooks` keeps webhooks, each a slug with a minted secret, and turns each authenticated delivery from outside into an event on the bus; agents subscribe scripts to those events and fetch the body.
- `dummy` is the reference app: a control panel over in-memory widgets that shows the patterns.

Shared library:

- `appkit` holds what every app shares: page chrome, identity, the MCP server and client, telemetry, and the database open path.

Develop, deploy, operate:

- `devctl` is the developer's CLI; it creates and destroys spaces on the substrate.
- `sandbox` stands up the whole suite from the current worktree on the developer's machine.
- `infra` is the Terraform for the one AWS account: root domain, hosted zone and substrate.
- `opsctl` is the operator's CLI on a host; it bootstraps and manages that one deployment.
- `idgen` mints the `R-XXXX-XXXX` requirement ids the specs use.

Agent runtime, used by no service yet; a future `prompts` service will link it:

- `agentkit` is a Go library over LLM chat APIs with an agentic tool loop.
- `toolkit` is agentkit's standard local tools: Bash, Read, Write, Edit, Glob and Grep.
- `agent-repl` is a REPL for exercising agentkit by hand.
- `oauth` is a CLI that runs the OAuth login flow agent-repl's providers need.

Personal tools:

- `agent-monitor` watches the coding agents running on the developer's machine. Nothing depends on it.

Not sub-projects: `design/` holds the visual style every page follows, and `.agents/skills/` holds the spec workflow, which is shared across projects.

## Working rules

Each sub-project's code is derived from its `specs/`; a hand-written file desynchronizes the tree from the design. An agent writes under a sub-project's tree only inside work the user started, and each kind of work has its own exclusions: the build run (`build-spec`) writes source and tests and nothing it treats as read-only; a delivery (`deliver`) writes what the build run cannot; a direct instruction writes what it names. Only the user starts the build run, directly or through a delivery.

Stories and designs are working material, not a constraint. Only the build run and the audit treat them as read-only. Never cite a requirement as a reason something cannot be done: it is a decision we made and can unmake, so argue for or against it on its merits. Changing one costs a re-minted id.

Versions are data. No test, fixture or requirement names a release version; a test that needs it reads the value the source declares and derives the rest from that.

Interfaces between releases change by expand then contract: the database schema, the APIs between apps, and event payloads, which are persisted and may be delivered across the release boundary. The release before the current one must run on the current one's schema, so an old and new mix, during a rolling restart or after a rollback, works in both directions. A provider adds in one release and its callers use the addition in the next; callers stop using something in one release and the provider removes it in the next. Data rewrites follow the same rule as DDL. A migration is frozen once it has run on a production-grade space; before that it may be edited, and dev databases are reset.

Adding an external dependency is a decision: ask the user, or in a delivery decide it and record it.

A library in this repository (appkit, agentkit, toolkit) is consumed only as a published release: tag it `<lib>/vX.Y.Z` on `main`, push the tag, then require that version through the ordinary module proxy and checksum database. Never build against the local tree: no `replace` directives, no `go.work`, no locally tagged or seeded module cache, no `GONOSUMDB` or `GOPROXY=direct` workarounds. A change that needs a new library feature lands and releases the library first.

Push only `main` and release tags to origin, and only when asked. Commit only to the branch checked out where you started; unless asked, move no other branch and create no branch or worktree for the work you were given. Land work, when asked, with `git fetch . <branch>:main`. Never `git stash`: the stack is shared across worktrees. Set work aside with a WIP commit or a local branch.

Investigate outside the tree: a probe or scratch script goes in a temporary directory, or in a throwaway worktree when it must import the code, removed afterwards and never committed. Try the cheapest instrument first; a shell command or a question usually beats a program.

In `rm` commands use literal absolute paths or `"${VAR:?}"` guards, never globs, bare variables or command substitution; the harness stops for approval on those.

Every agent commit ends with a `Co-Authored-By:` trailer naming the agent. No sub-project AGENTS.md restates this or names a model.

## Sandbox

The apps are not designed to run alone; `sandbox` (built from `sandbox/` with `make install`) stands up the whole suite from the current worktree, edits included, as user systemd units behind a local nginx. `sandbox up` builds and starts or redeploys it and prints each app's URL; `sandbox --help` has the rest. Apps behind auth take `Authorization: Bearer $(sandbox token)`; when none is stored, ask the human to create one at the auth app and store it with `sandbox token set`.

## Web assets

`design/ikigenba/theme.css` is the source of truth for every page's style and `design/README.md` records the decisions; UI work conforms to it or changes it there first.

A sub-project's `assets/` (markup, styles, fonts, icons) follows `design/` and is an input to the spec: the build run never writes it; the user or the delivering agent does, before design is drafted. Markup assets are Go `html/template` files the code embeds; code never writes markup of its own. Every word a person or an agent reads, and every class, id and attribute, lives in the asset and nowhere else; a test, a requirement and the source never spell one. Design names each template and the data it receives, never its text, hooks, markup or styles. A test proves a page by executing the named template with the data the design says and comparing, or by checking that a value the test supplied appears in the body; it never looks for a word or a tag. A change to copy or markup is an edit to the asset alone. An asset that is missing or wrong, or a template that cannot show a state the design names, is raised as an issue, not fixed.

## Command-line conventions

Every command-line program here behaves the same way.

- stdout carries the product; stderr carries diagnostics, including why a command failed.
- Findings are product, not diagnostics: a report goes to stdout whole, bad news included, and on failure stderr says why the command failed without repeating the findings.
- A diagnostic's first line begins `<program>: `. Detail follows one empty line later; another program's output is quoted there, every line prefixed `> `, and the program's own detail is unprefixed. Usage text never goes to stderr.
- Exit 0 on success, non-zero on failure; what each non-zero code means is the program's own design.
