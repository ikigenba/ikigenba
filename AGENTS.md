Ikigenba is a platform for one business or workgroup that unifies the development, deployment and operation of a suite of services built primarily for agents to use. Its services are not designed to run alone: each runs behind the host's nginx, takes its caller's identity from auth, sends its trail of events to telemetry, and offers its tools to agents through the MCP gateway, and the cross-dependencies only grow from there. The point of the architecture is consistency: every service is built on appkit from the same patterns, so an agent that has developed or operated one already knows the shape of the next. Around the services sit the tools to develop, deploy and operate it, listed below.

The **project** is this git repository. A **sub-project** is a directory that holds `specs/` beside its own `AGENTS.md`, which declares its toolchain, test files, gates and commit conventions. This file is guidance for the whole project and never stands in for a sub-project's `AGENTS.md`.

## Sub-projects

Platform apps, each one Go binary behind the host's nginx:

- `auth` signs users in and answers nginx's identity subrequest for every other app.
- `telemetry` keeps the suite's trail of events; every service posts to it and agents query it.
- `mcp` is the MCP gateway: one endpoint through which agents reach every service's tools.
- `repos` is the suite's home for git repositories, served over smart HTTP and MCP.
- `dummy` is the reference app: a control panel over in-memory widgets that shows the patterns.

Shared library:

- `appkit` holds what every app shares: page chrome, identity, the MCP server and client, and telemetry.

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

Each sub-project's code is derived from its `specs/`; a hand-written file desynchronizes the tree from the design. Without explicit, direct instruction from a human, an agent writes no file under a sub-project's tree, tests and diagnostics included, and never starts the build run (`build-spec`).

Stories and designs are working material, not a constraint. Only the build run and the audit treat them as read-only. Never cite a requirement as a reason something cannot be done: it is a decision we made and can unmake, so argue for or against it on its merits. Changing one costs a re-minted id.

Versions are data. No test, fixture or requirement names a release version; a test that needs it reads the value the source declares and derives the rest from that.

Adding an external dependency needs human approval. Ask first.

Push only `main` and release tags to origin, and only when asked. Commit only to the branch checked out where you started; unless asked, move no other branch and create no branch or worktree for the work you were given. Land work, when asked, with `git fetch . <branch>:main`. Never `git stash`: the stack is shared across worktrees. Set work aside with a WIP commit or a local branch.

Investigate outside the tree: a probe or scratch script goes in a temporary directory, or in a throwaway worktree when it must import the code, removed afterwards and never committed. Try the cheapest instrument first; a shell command or a question usually beats a program.

In `rm` commands use literal absolute paths or `"${VAR:?}"` guards, never globs, bare variables or command substitution; the harness stops for approval on those.

Every agent commit ends with a `Co-Authored-By:` trailer naming the agent. No sub-project AGENTS.md restates this or names a model.

## Sandbox

The apps are not designed to run alone; `sandbox` (built from `sandbox/` with `make install`) stands up the whole suite from the current worktree, edits included, as user systemd units behind a local nginx. `sandbox up` builds and starts or redeploys it and prints each app's URL; `sandbox --help` has the rest. Apps behind auth take `Authorization: Bearer $(sandbox token)`; when none is stored, ask the human to create one at the auth app and store it with `sandbox token set`.

## Web assets

`design/ikigenba/theme.css` is the source of truth for every page's style and `design/README.md` records the decisions; UI work conforms to it or changes it there first.

A sub-project's `assets/` (markup, styles, fonts, icons) follows `design/` and is an input to the spec: the build run never writes it, and an agent changes it only on explicit, direct instruction from a human. Markup assets are Go `html/template` files the code embeds; code never writes markup of its own. Stories say what a user does and sees, never appearance or structure. Design names each template, its data, and the hooks (classes, ids, attributes, labels, text) each state produces, never markup or styles. Tests assert on hooks and visible text, never layout. An asset that is missing or wrong, a state a story names that it can't show or a hook design names that it lacks, is raised as an issue, not fixed.

## Command-line conventions

Every command-line program here behaves the same way.

- stdout carries the product; stderr carries diagnostics, including why a command failed.
- Findings are product, not diagnostics: a report goes to stdout whole, bad news included, and on failure stderr says why the command failed without repeating the findings.
- A diagnostic's first line begins `<program>: `. Detail follows one empty line later; another program's output is quoted there, every line prefixed `> `, and the program's own detail is unprefixed. Usage text never goes to stderr.
- Exit 0 on success, non-zero on failure; what each non-zero code means is the program's own design.
