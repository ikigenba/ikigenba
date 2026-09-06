At the start of every session, before acting on the first request, enumerate
the project skills by printing the `name` and `description` front matter of
every `.agents/skills/*/SKILL.md` file. 

This project is spec managed.  Direct source code changes are not allowed
without direct user instruction. Agents should never start the build loop
this is strictly a human gated operation.

Branches are never pushed to the remote except `main`. Work happens on local
branches and worktrees; only `main` (and release tags) is published to origin.
Never push a working or feature branch, and never create a remote branch other
than `main`.
