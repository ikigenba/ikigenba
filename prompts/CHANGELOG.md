# Changelog

## v0.39.0 — 2026-08-11

- Completions for sibling services are now a durable per-consumer work queue (`POST/GET/DELETE /completions`): submit returns immediately, results wait until acknowledged (plus a 7-day safety TTL), and interrupted items requeue and re-execute on boot.
- Every completion result is guaranteed valid JSON: prompts enforces a fixed reply envelope with up to 3 internal corrective round trips, and hands consumers only the unwrapped result or an honest error.
- The queue's `context` field is an arbitrary JSON value, echoed back byte-for-byte; malformed context is rejected at submission.
- The synchronous `POST /complete` endpoint is removed — it silently severed every response slower than ~15 s on the chassis write deadline.

## v0.38.0 — 2026-08-11

- The service's web pages now carry the suite's brand icon: browsers show the ikigenba mark on the tab, in bookmarks and history, and as the icon if the page is saved to an iOS home screen. Nothing else about the pages changed.
- All five prompts pages carry it, not just the index.

## v0.37.0 — 2026-08-09

- baseline; changes before this version are recorded only in git history
