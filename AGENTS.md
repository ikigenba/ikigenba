At the start of every session, before acting on the first request, enumerate
the project skills by printing the `name` and `description` front matter of
every `.agents/skills/*/SKILL.md` file. 

This project is spec managed. Direct source code changes are not allowed
without direct user instruction. Agents never start the build run
(`build-spec`); it is strictly a human-gated operation.

Branches are never pushed to the remote except `main`. Work happens on local
branches and worktrees; only `main` (and release tags) is published to origin.
Never push a working or feature branch, and never create a remote branch other
than `main`.

Never use `git stash`. The stash stack is shared across all worktrees, so
another session may pop or drop your entry. Set work aside with a temporary WIP
commit, a dedicated local branch, or an isolated worktree instead.

## Command-line conventions

Every command-line program in this repository behaves the same way.

- stdout carries the command's product. stderr carries diagnostics. A command
  that fails writes the reason to stderr.
- A command whose product is a report of what it found writes the whole report
  to stdout, including the parts that say something is wrong. A finding is not
  a diagnostic: it is a fact about the thing the command was asked to look at,
  not about the command. When such a command fails, stderr carries why the
  command failed, never a copy of the findings.
- A diagnostic's first line begins `<program>: `. Further detail — another
  program's output, or the next command to run — follows after exactly one
  empty line, unprefixed. The usage text is never written to stderr.
- Exit 0 on success, non-zero on failure. Which non-zero codes a program uses,
  and what each means, is that program's own design.
