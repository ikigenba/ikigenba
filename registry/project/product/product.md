# registry — Product

**Authority: intent.** This doc owns *why* `registry` exists, *for whom*, what is
in and out of scope, and what we **promise** its callers — stated once, in
**outcome terms**. It does **not** state mechanism, function signatures, type
layouts, or test assertions; those belong to `project/design/README.md`. Where the
two could overlap (behavior), product states the *promise* and design states the
*exact, checkable proof* of that promise. That boundary is load-bearing: it keeps
product, design, and plan from restating each other.

## Problem

The suite runs every service on the same host, distinguished only by a fixed
loopback port. Despite that, "which port a service answers on" is written down in
roughly five places per service: the `main.go` default, the `.envrc` override, the
`manifest.env` deploy registry, the **hardcoded peer maps** inside the services
that consume it (`notify`, `scripts`, `prompts`, `sites` each carry literal
`name → http://127.0.0.1:30xx` tables), and transitively the local nginx front
door and `bin/start`. Renumbering one service means finding and editing every one
of those in lockstep — a class of error the compiler cannot catch, and one that
today only surfaces at deploy. There is no single place that answers "what port is
`crm`?" that all of Go can call.

## Purpose

`registry` is a single, tiny, dependency-free Go library that holds the one
authoritative `name → port` table for the suite and turns a service **name** into
its loopback **address**. Code references services by name and asks the registry
where they are; the table is the only place a port is written down.

## Users

Other Go code in the mono-repo — the services (to learn their own port and their
peers'), the `appkit` chassis (to emit `PORT` when it generates a manifest), and
the `opsctl` operator CLI — all of which need to resolve a service name to a port
or a base URL without linking the service chassis or duplicating a port literal.
The registry is a leaf library: everything depends on it and it depends on
nothing, so even `opsctl` can use it without pulling in the chassis.

## Scope

`registry` owns the authoritative name→port table and the functions that resolve a
name to a port or a loopback base URL, plus the guardrail tests that keep the table
honest (unique names, unique ports, every port inside its declared block). It is
organized into number blocks by service type (core, applications, connectors) with
room to reserve a name ahead of its code.

It does **nothing else**: it performs no I/O, reads no environment, does no
runtime discovery (the table is compile-time data and the host is a constant), and
resolves only addresses — it does **not** model which service subscribes to which
(that domain coupling stays in each consumer's own code). It also does **not**
migrate the existing consumers onto itself, generate `manifest.env`, or edit
`appkit`, `opsctl`, nginx, or `bin/` — delivering the standalone library is this
project's whole job; adopting it is separate, per-consumer follow-on work.

## Contractual constants

- **`dashboard` is port `3000`.** It is the apex/`DEFAULT` app and `opsctl --port`
  already defaults to `3000`; the table must pin it there verbatim and it must
  never move.
- **The loopback host is `127.0.0.1`.** Every resolved base URL is composed
  against this host, matching the existing peer-map and `/feed` literals.

## What we promise (caller-facing behavior)

- **One lookup, by name.** A caller asks for a service by name and gets back its
  port, or a loopback base URL like `http://127.0.0.1:3100`, without knowing or
  repeating the number.
- **Unknown names fail loudly.** Asking for a name that isn't in the table is a
  programming error, surfaced immediately (a failed lookup or a panic), never a
  zero port or a silent wrong address.
- **The table cannot silently drift.** Two services can never share a port or a
  name, and every service's port sits inside the block its type declares; a change
  that violates this fails the library's own tests rather than reaching deploy.
- **Names can be reserved ahead of code.** A service name can own its number in the
  table before the service itself exists.

## Success criteria (outcomes)

- A caller can turn any known service name into the correct port and into
  `http://127.0.0.1:<port>`, with `dashboard` resolving to `3000`.
- Asking for an unknown name does not return a usable-looking wrong answer — the
  lookup reports failure, and the strict/URL forms fail loudly.
- The table's guardrails hold: no duplicate names, no duplicate ports, and every
  port within its block's range — demonstrated by the library's tests passing, and
  by a deliberate violation making them fail.
- The library builds and tests green in isolation with **no third-party
  dependencies**, so any module (including `opsctl`) can adopt it without inheriting
  the chassis's dependency graph.
