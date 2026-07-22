# Phase 35 — Visibility-enum domain: migration, store, layout, token generator

*Realizes design Decision 15 (visibility data model), Decision 16 (filesystem
layout with `Seg`/generalized `Move`), and Decision 27 (the unlisted-name token
generator).*

The `internal/sites` + `internal/db` domain converts from the `public` boolean
to the three-value `visibility` enum, and gains the credential-token generator:

- One **new** timestamped migration (created with
  `bin/create-migration sites <name>`; every committed migration stays frozen)
  rebuilds `sites` with the CHECK-constrained `visibility` TEXT column, carrying
  existing rows across via the `1→'public'` / `0→'private'` mapping (D15).
- `internal/sites` gains the `Visibility` string type
  (`Public`/`Private`/`Unlisted`), `ParseVisibility` with the
  `ErrInvalidVisibility` sentinel, and `NewToken()` (30 chars of `a-z2-7` from
  `crypto/rand`, D27).
- `Store`: `Site.Visibility` replaces `Site.Public`;
  `Create(ctx, name, ownerID, ownerEmail, v)`;
  `SetVisibility(ctx, name, v, newName)` as one UPDATE with the optional
  rename (D15).
- `Layout`: `Seg(v)` (unlisted → the public segment), `SiteDir(v, slug)`,
  `SiteBase(v)`, and `Move(slug, from, newSlug, to)` (D16).

Dependent packages (`internal/mcp`, `cmd/sites`) are updated **mechanically**
so the suite stays green — their existing tool contracts and view models keep
their current wire shapes in this phase by mapping their boolean vocabulary
onto the enum at their own boundary (e.g. `public:true → Public`,
`site.Visibility == Public` where a bool was read); their behavioral rework to
the enum wire surface is Phases 36 and 37, not this one. Existing tests tagged
with the deleted ids R-QSLO-SAIQ, R-QTTL-629F, R-Z57J-J363, R-QV1H-JU04, and
R-QW9D-XLQT are replaced by this phase's tests (the behaviors were redefined;
their old assertions no longer describe the design); tests tagged R-Z3ZN-5BFE
and R-QYP6-P587 are updated mechanically and keep their tags.

**Done when:** each of the following ids is covered by a genuinely-asserting
test tagged with it, and the suite is green (design Conventions):

- R-H0LI-XQXI — `Create` persists each of the three visibility values verbatim.
- R-H1TF-BIO7 — `SetVisibility` updates visibility, optionally renames in the
  same UPDATE, bumps `updated_at`, `ErrNotFound` on a missing slug, `ErrExists`
  on a colliding `newName`.
- R-Z3ZN-5BFE — the owner pair is persisted exactly as passed (retagged test
  updated to the new `Create` signature).
- R-H31B-PAEW — `pragma table_info` shows `visibility` and no
  `public`/legacy columns; a `'bogus'` INSERT is rejected by the CHECK.
- R-H498-325L — the manual-application harness proves the migration maps
  seeded `public = 1`/`0` rows to `'public'`/`'private'` without dropping them.
- R-H5H4-GTWA — `Seg`/`SiteDir` cover all three visibilities with unlisted on
  the public segment.
- R-H6P0-ULMZ — `Move` relocates+renames, no-ops on same path, tolerates a
  missing source, refuses an existing destination.
- R-QYP6-P587 — the symlink/working-tree grep stays empty (existing test keeps
  passing).
- R-H7WX-8DDO — `NewToken` shape, slug validity, and distinctness.
