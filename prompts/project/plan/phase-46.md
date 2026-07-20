# Phase 46 — Detail pages: prompt detail, the run log, and the raw-body endpoint

*Realizes design Decision 35 (browse UI), detail slice. Depends on Phase 45.*

The drill-downs land, completing the browse surface:

- **Templates** (`share/www/`): `ui-prompt.html` and `ui-run.html` on the
  shared chrome, plus the styled 404 rendering.
- **`GET /ui/prompts/{id}`**: full `user_prompt`, `system_prompt`,
  pretty-printed `config_json`, the prompt's triggers, and the link to
  `/srv/prompts/ui/runs?prompt_id=<id>`; a missing row (tombstone-deleted or
  never existed) renders the styled 404 "does not exist or was deleted" page.
- **`GET /ui/runs/{id}`** — the log: the run columns the list omits (id,
  linked prompt_id, error, usage_json, trigger_event_id, log_path), then the
  run's calls (Phase 44's `ListByGroup`) inline in `started_at` order — per
  call the metadata line and the pretty-printed bodies, with the explicit
  pruned/no-body note and the 64 KiB (`uiBodyInlineLimit`) truncation + raw
  link behavior per D35. Unknown run → styled 404.
- **`GET /ui/calls/{id}/raw`**: `?side=request|response` streamed whole as
  `text/plain; charset=utf-8`; 400 on an invalid side; 404 on an unknown id or
  a NULL body.

**Done when:** the suite is green (design Conventions) and these ids are
covered by clearly-named tagged tests:

- R-0C2E-79JM — prompt detail content and the filtered-runs link.
- R-0DAA-L1AB — missing prompt renders the styled 404 deleted notice.
- R-0EI6-YT10 — run detail renders the remaining run columns and its calls in
  order with metadata and pretty-printed bodies; other runs' calls absent.
- R-0FQ3-CKRP — NULL bodies render the explicit pruned/no-body note.
- R-0GXZ-QCIE — an over-64 KiB body truncates with a note and a raw link; an
  under-threshold body renders whole.
- R-0I5W-4493 — the raw endpoint streams the complete body verbatim as
  `text/plain`; 400 invalid side; 404 unknown id or pruned body.
- R-0JDS-HVZS — unknown run id renders the styled 404 page.
