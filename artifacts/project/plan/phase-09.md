# Phase 9 — The landing page: a sortable, filterable inventory

*Realizes design Decision 9 (landing page). Depends on Phase 8.*

The `internal/web` surface: the cron-canonical Carbon template with the
inventory table (linked filenames, visibility, size, created-at, creator,
download count, empty state), the JSON data island, the island-fed client
controller (text + visibility filter, four sort columns with direction
flip, `textContent`-only rebuild), embedded `/static/` assets, and the
headless-Chrome wiring proof. End state: a logged-in user sees, sorts,
filters, and downloads from the full inventory at the mount root.

**Done when:** the suite is green and each of R-53EO-JVJ9, R-54MK-XN9Y,
R-55UH-BF0N, R-572D-P6RC, R-59I6-GQ8Q, R-5AQ2-UHZF is covered by a test
tagged with its id.
