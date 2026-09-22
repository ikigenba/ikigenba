The **project** is this git repository. A **sub-project** is a directory in it
that holds `specs/` beside its own `AGENTS.md`; that `AGENTS.md` declares the
sub-project's toolchain, test files, gates, and commit conventions. This file
is guidance for the whole project and never stands in for a sub-project's
`AGENTS.md`.

This project is spec managed: a sub-project's code is derived from its
`specs/`, so a hand-written file desynchronizes the tree from the design.
Agents write no file under a sub-project's tree without direct user
instruction — tests and throwaway diagnostics included. A probe that must sit
in the tree goes in a worktree that is removed afterwards. Agents never start
the build run (`build-spec`); it is strictly a human-gated operation.

Stories and designs are the working material, never a constraint on the work.
Only the build run and the audit treat them as read-only. In any proposal or
discussion, never cite a requirement as a reason something cannot be done. A
requirement is a decision we made and can unmake, so argue for keeping or
changing it on its merits: "we decided X; keep it because..." or "change it
because...". Changing a requirement's text costs one re-minted id and nothing
else.

Versions are data. No test, fixture, or requirement names a release version. A
test that needs the current version reads the value the source declares,
through the contract that declares it, and derives tags, asset names, and
expected output from that value.

Branches are never pushed to the remote except `main`. Work happens on local
branches and worktrees; only `main` (and release tags) is published to origin.
Never push a working or feature branch, and never create a remote branch other
than `main`.

Never use `git stash`. The stash stack is shared across all worktrees, so
another session may pop or drop your entry. Set work aside with a temporary WIP
commit, a dedicated local branch, or an isolated worktree instead.

Investigate outside the working tree. A reproduction, probe, or scratch script
belongs in a temporary directory outside the repository; when it must import
the code to run, it belongs in a throwaway worktree (`git worktree add`)
removed afterwards. Reach for the cheapest instrument that answers the
question first — a shell command, or asking the user what state something was
in, usually beats writing a program.

Every commit an agent makes ends with a `Co-Authored-By:` trailer naming the
agent that made it, in whatever form that agent identifies itself. This is the
only commit attribution rule in the repository; no sub-project AGENTS.md
restates it or names a specific model.

## Command-line conventions

Every command-line program in this repository behaves the same way.

- stdout carries the command's product. stderr carries diagnostics. A command
  that fails writes the reason to stderr.
- A command whose product is a report of what it found writes the whole report
  to stdout, including the parts that say something is wrong. A finding is not
  a diagnostic: it is a fact about the thing the command was asked to look at,
  not about the command. When such a command fails, stderr carries why the
  command failed, never a copy of the findings.
- A diagnostic's first line begins `<program>: `. Further detail follows after
  exactly one empty line. Another program's output is quoted there, every line
  prefixed `> `, so a reader can see at a glance which program is speaking.
  Detail the program writes itself, such as the next command to run, is
  unprefixed. The usage text is never written to stderr.
- Exit 0 on success, non-zero on failure. Which non-zero codes a program uses,
  and what each means, is that program's own design.
