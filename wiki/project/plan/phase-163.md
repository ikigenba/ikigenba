# Phase 163 — Stage plans by name; mint subject ids inside the integrate commit

*Realizes design Decision 4 (ingest pipeline), the identity slice. Depends on Phase 162.*

The staged extract plan stops carrying minted subject ids. `stagedIntegration`
and its staged claims key subjects by `norm_name` (with `name` and `type`
alongside), and the ids of newly created subjects are assigned inside the
integrate transaction. `SubjectStore.Save` becomes an upsert on
`(scope, norm_name)` that adopts the existing row's id rather than returning a
constraint error.

At commit, every staged norm_name is re-resolved through the alias-aware
`Resolver` — subjects, then aliases — so a `merge` committed between staging and
integrate lands the claims on the survivor. A name that resolves to nothing and
is not among the plan's new subjects ends the job `failed` naming the stale
plan, writing nothing partial.

This removes the duplicate-subject-id collision as a representable state rather
than serializing around it; Phase 162's lease and this phase are independent
guards on the same invariant.

**Done when:** these ids are covered by clearly-named tests and the suite is
green:

- R-ORRV-XUTW — a staged plan carries norm_names and no minted subject id, and
  two plans staged concurrently against the same absent name integrate to one
  subject with both jobs' claims attached
- R-OU7O-PEBA — saving an already-present `(scope, norm_name)` returns the
  existing id and writes no duplicate
- R-OSZS-BMKL — a staged name merged away integrates onto the survivor; a name
  resolving to nothing and absent from the plan's new subjects fails the job
