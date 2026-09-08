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

Always preserve the `main` branch. Worktree cleanup applies to `main` like any
other worktree: the `main` worktree may be removed if it's no longer wanted, but
never delete the `main` branch itself.
