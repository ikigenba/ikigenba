# Story-defined host summaries

The lifecycle stories remain the product contract. D06 and D07 now require:

- `retire: ok (opsctl backed up crm, dashboard, host)` for the illustrated
  successful retire, using the actual backed-up app names in sorted order.
- `restore: ok (host/2026-09-12T14:22:51Z.tar.zst, 10 keys set again)` when
  the host restore actually selected that backup and reconfiguration succeeded.
- `certificate: ok (certbot renew: renewed)` when the space certificate was
  renewed, and `certificate: ok (certbot renew: not yet due)` when it was
  unchanged because renewal was not due.

The earlier fixed completion summaries did not satisfy these stories and have
been replaced with fresh requirement IDs. No stories were changed by this
correction.

## Remaining producer evidence

The stories establish the required results but do not publish a machine-readable
opsctl result protocol. A successful process exit cannot reveal which app backups
retire created or which backup host restore selected. Listing S3 objects cannot
prove either fact. D06 and D07 therefore preserve the required summaries and
require their true results from a published opsctl interface. An installed-tool
observation identifying that interface and demonstrating these two results is
still needed before those result-producing adapters can be built. No sibling
API, parser, or source change was invented here.

Likewise, certbot exit success alone does not distinguish renewal from a
certificate not yet due. The former `(success)` substring classifier had no
recorded verification. The design now requires the actual renewal outcome;
a verified certbot result interface must supply it. These are narrow external
producer gaps, not reasons to weaken the story output.

## Lifecycle coverage review

Reviewed the original create, apex create, durable rebuild, preflight refusals,
partial-create failure, list, destroy variants, stop, start, and status stories
against D06 and D07. Existing requirements cover ordering, retained-backup
restore, configuration reapplication, the Elastic IP, exact resource cleanup,
preflight failures, and repeated start/destroy behavior. Additional corrections
restore the exact top-level space help text, explicitly preserve tags on stop,
and prohibit mutations by list/status.
