package mcp

// describeText is the on-demand deep overview returned by the
// describe tool. It is intentionally NOT loaded into the
// initialize `instructions` field (which every client pays on every connection)
// — callers load it only when they choose to call describe.
//
// Source of truth for the concepts below is README.md / ARCHITECTURE.md in this
// module; this is a concise restatement, not a substitute.
const describeText = `Scripts runs Python scripts on your behalf — manually or on an event trigger.

WHAT IT IS
- A *script* is a git repository whose main tip is live.
  Its root main.py is the entrypoint, and supporting Python modules beside it
  are importable. create and update commit main.py; add other files by git push
  from a laptop or from a run.
- A *run* executes a fresh clone pinned to one commit, reported as the run's
  repo_sha, by python3. This makes each run exactly reproducible. Runs are
  unbounded (no single-flight): many runs of one script can be in flight at once.
  Each run has its own persistent dir (the checkout, config.json, stdout.log,
  stderr.log, and any files the script wrote).
- A *trigger* binds a script to a canonical upstream routing key (e.g.
  dropbox:create/bills/**/*.pdf):
  when a matching event fires, scripts starts a run with the trigger envelope
  {source, kind, subject, event_id, payload} on stdin and in $EVENT_JSON; the
  producer payload is preserved under payload.

RUNTIME CONTRACT
- python3 >= 3.11, bash >= 5.0, and network access. The Python standard library
  and the preinstalled suite module are available; no other third-party packages
  are installed day-one.
- A run works in the fresh checkout pinned to its repo_sha. Its environment has
  SUITE_REPO_KEY, SUITE_REPO_SHA, and a scoped SUITE_GIT_TOKEN, so ordinary git
  commands work in the checkout, including pushing a branch.
- The suite module is importable in every run with: import suite
- suite.event() returns {source, kind, subject, event_id, payload} as a dict,
  with the producer payload verbatim under payload, or {} for a manual run.
    event = suite.event()
- suite.mcp(service, tool, args) calls any suite service's MCP tool and returns
  its structured result as a dict, or text for a prose tool.
    contact = suite.mcp("crm", "contact_get", {"contact_id": contact_id})
- suite.fetch(content_url, dest) writes the bytes behind a content URL from an
  event payload or tool result to a local file.
    suite.fetch(invoice["content_url"], "invoice.pdf")
- suite.files.* accesses the account's file share: list, stat, get, put, delete,
  move, and mkdir. This is the durable, shared file store: its files persist and
  sync, and are what the owner and other workflows see; the run dir is private
  working space. Put a product in the file share to publish it durably and let
  watching workflows trigger.
  Share paths are absolute and /-rooted; relative spellings are accepted and treated as rooted.
    suite.files.put("summary.pdf", "/reports/summary.pdf")
- Suite-service failures raise suite.ToolError; its .code is one of validation,
  not_found, conflict, too_large, source_unavailable, or internal. Catch it and
  branch on .code, or let it crash the run: the
  failure is written to stderr.log and the run is marked failed.
    try: suite.mcp(service, tool, args)
    except suite.ToolError as err: print(err.code)
- Products travel by reference. Non-directory run_fs_list entries carry a
  content_url that other services can fetch after the run, so hand results
  onward by writing a file instead of printing its bytes.

LIFECYCLE
  1. create {name, body}        -> commits main.py, returns {script_id}
  2. update {script_id, body}   -> commits a new main.py
  3. git push                   -> adds or changes any repository files
  4. run {script_id}            -> starts a run, returns {run_id, repo_sha}
  5. poll run_get {run_id}      -> status until succeeded|failed|cancelled
  6. run_output {run_id}        -> stdout/stderr logs
  7. run_fs_list / run_fs_read -> files the run wrote

TRIGGERS
  set_trigger {script_id, filter} binds the script to a canonical
  source:kind<subject> glob (for example cron, crm, ledger, dropbox, prompts, or
  repos); ** matches across subject paths. On a matching event a
  run starts automatically. Completion emits succeeded / failed
  on this service's own /feed for other services (e.g. prompts) to consume.

Scripts is also the suite's workflow runner. Trigger on a repository push with a
filter such as repos:push/<kind>/<name>, work in the pinned checkout, and report
with suite.mcp("repos", "status_set", args). Nothing merges automatically: merge
only by explicitly calling suite.mcp("repos", "merge", args) yourself.

health proves the auth chain and reports the runtime contract.`

// toolDescribe returns the on-demand overview. Takes no inputs.
func toolDescribe() (map[string]any, error) {
	return toolResultText(describeText), nil
}
