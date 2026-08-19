# bug: a push to a site's repos git door does not update the live site

**Observed.** Cloning a site's repository through the repos clone door
(`/srv/repos/git/sites/<slug>.git`), committing, and `git push origin main`
updated the repository HEAD but the live served site never changed, across many
minutes. Success criterion 13 and Decision 35 both promise that a `main` push
re-materializes the served tree via the `repos:push/sites/**` event consumer.

**Why this is not a sites `project/` fix.** The sites-side wiring was read
directly and is correct: the consumer subscription is declared
(`cmd/sites/main.go`), genuinely wired into the composition root (it runs in the
deployed binary), resolves the repos feed with defaulted env
(`SITES_REPOS_FEED_URL` → `registry.BaseURL("repos")+"/feed"`,
`SITES_REPOS_FROM` → `tail`), matches the right kind/slug, and its handler
(`internal/sites/push.go`) branch-gates on `main`, echo-gates on the recorded
`repo_sha`, and re-materializes. D35's design already specifies the correct
behavior, so there is no sites Decision to add or change — a spec edit here would
only restate D35.

**Where the real cause almost certainly lives (other trees / deploy).**
- **repos** may not emit a `push` event for a git-door (`receive-pack`) push, or
  emits it on a subject/branch the consumer does not see.
- The repos **`/feed`** may be unreachable at the resolved URL on the box, or the
  durable consumer cursor (`from: "tail"`) advanced past the event.
- The **deployed sites binary** may predate D35 (version skew), so the consumer
  is simply not present in what is running.

**Suggested next step (outside this session).** Reproduce with a git-door push
and query telemetry: confirm whether a `repos:push/sites/<slug>` publish record
exists, whether sites recorded a matching `consume`, and whether the deployed
sites version includes the D35 consumer. Fix belongs in repos / eventplane / the
deploy, not in `sites/project/`.
