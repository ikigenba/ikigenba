# D09-on-a-space

What ledger contributes when it runs on a space and in a sandbox: what ships, what a space takes from the manifest, the nginx fragment `cli.NginxConf` and the file under `etc/` that copies it, the services file entries it uses, which of the contract's guarantees the delivery's sandbox walk stands on and which document owns each, the real artifacts it keeps document references against, the trust ledger places in what reaches it, and the finish line's steps on `us`. Almost every step between a checkout and an agent's tool call belongs to someone else: devctl builds and ships the file, opsctl installs it and generates nginx, auth decides every credential, the host's nginx turns away every request without one and sets the identity headers, the gateway relays tool calls, the events app takes the bus events, telemetry keeps the trail, artifacts holds the bytes a document names, and home lists the app. ledger's own share is what `D01-layout-and-run-seam` to `D08-trail` state, plus the fragment, which this document declares. Decisions cited as seam N, Sequence or Finish line are the decisions document /home/mgreenly/projects/ikigenba-ledger.md.

This document declares one name, `cli.NginxConf`, which `D01-layout-and-run-seam` and `D02-cli` assign to it, and states that `etc/nginx.conf` holds its bytes. Everything else it says of a space or a sandbox is behaviour the other documents own; the table below names the owner of each outcome.

## What ships

The release holds four members of ledger's directory: `bin/ledger`, `etc/manifest.toml`, `etc/nginx.conf` and `share/icon.svg`, the last Tabler's `book` outline, human-authored and already in the tree (seam 1). The two files under `etc/` are what the root package's `Etc` embeds (`D01-layout-and-run-seam`, R-667M-BPY7): `etc/manifest.toml` holds the bytes of `cli.Manifest` (`D02-cli`, R-V8TJ-5H53, R-VB9B-X0MH) and `etc/nginx.conf` the bytes of `cli.NginxConf`, which this document states. The templates and the migration ship inside the binary (`D01-layout-and-run-seam`, R-64ZP-XY7I, R-67FI-PHOW; the migration's bytes `D04-store`, R-WR47-0X2S), and the database is not in the file: ledger creates `state/ledger.db` under its working directory on its first start, and the migration seeds the book and the `USD` commodity (`D01-layout-and-run-seam`, R-6EQX-0452, R-6H6P-RNMG; `D04-store`, R-OEJQ-D4EL). The tarball, its member list and its name are devctl's; the icon is never opened by a test. No test reads the checkout, so the release and the icon are outside what a requirement can state; a developer checks them with `tar`.

## The manifest

The manifest is `D02-cli`'s, byte for byte. What a space takes from it: the app is `ledger`; its description is `pages.Description` (`D06-pages`, compared by `D02-cli`), which the host publishes in its services file for the gateway; it is not the default app; it is an MCP service (`mcp = true`), so the gateway offers the twenty-three tools (`D07-tools`); it does not serve guests (`guests = false`); it holds no secrets, since it calls nothing outside the suite (seam 1, seam 6); it has no `[env]`, since ledger has no setting of its own (`D01-layout-and-run-seam`); `[database]` names `state/ledger.db`, which the host keeps and replicates; `[resources]` holds only `memory_max = "128M"`; and `[home]` holds only `group = "application"`, so home lists ledger under Application. `DRAIN_SECONDS` is the space's.

**What `guests = false` means.** The host's nginx (opsctl's) and the sandbox's put every request to ledger's host through auth's `/check`, to the pages, to `/_appkit/` and to `/mcp` alike, and admit only one that carries a credential: a page asked without one is sent to sign in by nginx and auth, and `/mcp` without one is answered `401`, before anything reaches ledger. ledger's pages still send a guest to sign in themselves, through `identity.Optional` and a 302 to the sign-in address, should one reach the socket (`D03-serve` records the departure from candidates, which uses `identity.Require`; `D06-pages` draws the redirect and declares `pages.SignIn`). So every check the walk makes with a credential is answered by ledger, and every one without a credential is answered by nginx and auth.

## The nginx fragment

`cli.NginxConf` is campaigns' fragment exactly: two exact-match locations answering `/events` and `/declarations` 404 at the public name, so a bus delivery and the declarations reach ledger only over its socket (`D05-events`, `D03-serve`). Nothing else is held back: `/mcp` is guarded by nginx's own locations, and the pages are gated by nginx under `guests = false` and guard themselves besides. ledger accepts no events and declares none to accept (seam 5), so its `/events` is never a bus target; the fragment holds it back all the same, as every emitter-only app does. Earlier designs observed the two lines' behaviour in a real nginx: every method of `/events` and `/declarations`, with or without a query, answered 404, and `/events/`, `/declarations/`, `/eventsx` and `/` passed on; ledger's page handler answers those last ones (`D03-serve`, `D06-pages`).

## On a space

ledger on a space is the same binary on the same terms as under a test: a socket passed as descriptor 3 (`/run/ikigenba/ledger.sock`), `/var/opt/ikigenba/ledger` as its working directory, its environment from `/etc/opt/ikigenba/ledger/env`, and `IKIGENBA_SERVICES` naming the host's services file. Who supplies each is opsctl's; what ledger does with them is `D01-layout-and-run-seam`'s and `D03-serve`'s. Of the services file it uses `artifacts`, through appkit's `artifacts` client, for `add_document` (`D07-tools`, the client `D03-serve`); `auth`, for the banner's links and a guest's sign-in address (`D06-pages`); and `events` and `telemetry`, appkit's sinks'. Among its siblings only artifacts is reached, and nothing outside the suite (`D01-layout-and-run-seam`, R-6NA7-OIBX; seam 1, seam 6). A missing entry never refuses the start: without `artifacts`, `add_document` refuses `unavailable` (`D03-serve`, R-8SBY-JVMS, R-B7NL-L9F4; `D07-tools`, R-K33N-3L2K), and the bus and trail writers behave as appkit's do. The host must run artifacts, events, telemetry and auth.

## In a sandbox

The sandbox builds ledger from the worktree, gives it `DRAIN_SECONDS`, includes `etc/nginx.conf` in ledger's server, and, the manifest refusing guests, routes every request through auth's `/check`, as the host does. ledger's state is under the sandbox's data directory and survives `down` and `up`. An agent is curl on the developer's machine: the pages at `http://ledger.<sandbox>.localhost:<port>/` with `Authorization: Bearer $(sandbox token)`, and the tools through the gateway or at ledger's `/mcp` with the same token (Sequence 2). Every step of the walk signs in; a step without the token is answered by nginx and auth, never by ledger.

The sandbox runs a real artifacts, and the walk keeps document references against it, not a stand-in: the delivery puts one small file into the sandbox's artifacts through artifacts' published `import` tool and the upload it plans, and reads its metadata with artifacts' `show`. No stand-in sibling is needed, since artifacts is ledger's only sibling call and the real one can be made to hold a chosen file.

### The walk

With the sandbox up from the worktree, the delivery walks, as the developer, with the stored token, through the gateway's tools or ledger's `/mcp` and with curl, in Sequence 2's order. Every record it makes is dated on or before today, so `check` sees the walk's failing assertion; the walk never names a time zone.

1. Before any record: home, `GET /`, `/tools` and `/about` with the token and without it, `POST /mcp` without a credential, `/events` and `/declarations` at the public name and `/declarations` on ledger's socket, and the gateway's `describe` of ledger.
2. `book`: `USD` at its precision, no lock date, fiscal year start January 1.
3. `open_account` for `Assets:Bank:Checking`, `Equity:Opening-Balances`, `Expenses:Office` and `Income:Sales`; one of them again.
4. `post` an opening balance between checking and opening balances.
5. `post` an office expense with a `key`, the expense posting's amount omitted; `transaction` reads it back filled in.
6. `post` it again with the same `key`.
7. `annotate` it `cleared`; `history` of it.
8. `reverse` it; `transaction` of the original and of the reversal.
9. `assert_balance` on checking for the right amount and for a wrong one; `check`; `retract_assertion` of the wrong one; `check` again.
10. `add_document` on checking with the path the sandbox's artifacts holds, and with a well-formed path it does not hold; `account` of checking.
11. Refusals: a `post` to an undeclared account, a `post` that does not balance, and a `post` with an amount of three decimals.
12. `journal`, `register` of checking, `balances`, `balance_sheet` as of today, and `income_statement` from the fiscal year start through today.
13. `set_book` a lock date; a `post` dated on it; `book`.
14. `GET /` again, `/tools`, `/about` and an unknown path, with the token.
15. events' `catalog` and `search` for ledger, and telemetry's `catalog` and `search` for ledger.

Sequence 2 names steps 2 to 14; steps 1 and 15 add what `D09-on-a-space`, `D05-events` and `D08-trail` contribute, observed as campaigns' walk observes them. Step 11 comes before step 13 so that each refusal is the rule it names and not the lock date's.

## Verification

Each row is an outcome the walk checks and the requirements that own it, cited by id from `D01-layout-and-run-seam` to `D08-trail`. A row naming nginx, auth, the sandbox, home, events, telemetry, artifacts or the gateway is that service's behaviour, observed and not required here. Where an outcome depends on "today", today is `D04-store`'s (R-NV1C-8SJH).

| Outcome the checks rely on | Owner |
|---|---|
| home's page lists ledger under Application, with its icon and description | `[home] group = "application"` and the description in the manifest (`D02-cli`, R-V8TJ-5H53, R-VA1F-J8VS; `pages.Description` `D06-pages`, R-PYS6-ERES); the icon in `share/` (this document, What ships); the listing: home and the sandbox's or opsctl's services file |
| `GET /`, `/tools` and `/about` with the token answer 200 with the page drawn for the developer, the release in the footer; an unknown path answers ledger's not-found page | `D06-pages` (R-Z1NE-I3QT, R-QUHK-CSOU, R-QVPG-QKFJ, R-QOE2-FXZD, R-QLY9-OEHZ); routing `D03-serve` (R-8IKR-HPP8); the footer's release `D01-layout-and-run-seam` (R-7BO7-BX5T) |
| the same paths without a credential are sent to sign in by nginx and auth, and nothing reaches ledger; ledger's own redirect for a guest stands behind it | `guests = false` (`D02-cli`, R-V8TJ-5H53); nginx and auth's `/check`; the page's redirect `D06-pages` (R-Z0FI-4C04, R-QEMV-DS1T), the departure recorded in `D03-serve` |
| `POST /mcp` without a credential answers 401 and nothing reaches ledger | nginx and auth (sandbox's or opsctl's); were it to reach ledger, `D03-serve` (R-8EX2-CEH5) |
| `/events` and `/declarations` at the public name answer 404, with or without the token | the fragment (this document, R-6VVJ-1UX2, R-6X3F-FMNR), included by the sandbox's and opsctl's nginx |
| `/declarations` on ledger's socket answers `transaction.posted` with its attributes and accepts nothing; events' `catalog` names ledger as its emitter and no accepter | `D05-events` (R-E5QP-ZFPW, R-EJ5M-6WVJ); the route `D03-serve` (R-8DP5-YMQG); `/events` `D05-events` (R-EKDI-KOM8); the catalog: events |
| the gateway's `describe` lists the twenty-three tools in catalog order with their effects, and a call's result carries ledger's `serverInfo` | `D07-tools` (R-HCBS-460B, R-HERK-VPHP); mounting `D03-serve` (R-8G4Y-Q67U); `D01-layout-and-run-seam` (R-7AGA-Y5F4, R-76SL-SU71); the relay: the gateway |
| `book` answers `USD` at precision 2, an empty lock date, fiscal year start `01-01` and zero counts | `D07-tools` (R-JKT5-D0Y5); the seeded book and commodity `D04-store` (R-OEJQ-D4EL, R-OI7F-IFMO, R-OFRM-QW5A) |
| `open_account` opens each of the four with the developer as opener; again answers it as it stands and false | `D07-tools` (R-JPOQ-W3WX); `D04-store` (R-OLV4-NQUR, R-ON31-1ILG, R-O4SJ-AYH1) |
| `post` of the opening balance answers a `txn_` id, its postings at precision and true | `D07-tools` (R-RY8T-9SBP, R-JDHR-2EHZ); `D04-store` (R-OY24-HG9P, R-O9O4-U1FT, R-NZWX-RVI9) |
| `post` with an omitted amount answers, and `transaction` reads back, that posting filled in with the remainder | `D04-store` (R-OY24-HG9P, R-P1PT-MRHS); `D07-tools` (R-RY8T-9SBP, R-JS4J-NNEB) |
| `post` again with the same `key` answers the first transaction and false, and nothing is recorded or emitted | `D04-store` (R-OZA0-V80E, R-P0HX-8ZR3); `D07-tools` (R-RY8T-9SBP, R-S1WI-F3JS, R-S5K7-KERV) |
| `annotate` `cleared` answers true; `history` answers two versions, newest first, paged | `D04-store` (R-P2XQ-0J8H, R-P917-XDXY); `D07-tools` (R-RZGP-NK2E, R-JVS8-SYME, R-JH5G-7PQ2) |
| `reverse` answers a new transaction with every amount negated in order, dated today by default, true; the reversal's `reverses` names the original and the original's `reversed_by` names the reversal | `D04-store` (R-P5DI-S2PV, R-NV1C-8SJH); `D07-tools` (R-S0OM-1BT3, R-JS4J-NNEB) |
| `assert_balance` for the right amount holds and for a wrong one fails; `check` lists the failing one with expected, actual and difference; after `retract_assertion` `check` is empty | `D04-store` (R-PK0B-DBM7, R-PISE-ZJVI, R-POVW-WEKZ, R-WOOE-9DLE); `D07-tools` (R-JX05-6QD3, R-JFXJ-TXZD, R-K0NU-C1L6, R-JY81-KI3S) |
| `add_document` with the held path answers the document with the id artifacts' `show` answers; with an absent path it is refused `not_found`; `account` of checking lists the document | `D07-tools` (R-K33N-3L2K, R-K4BJ-HCT9, R-JOGU-IC68); `D04-store` (R-WNGH-VLUP, R-PTRI-FHJR, R-OVMB-PWSB); appkit/artifacts Head and Show |
| `add_document`'s requests reach the real artifacts with the developer's identity, one `sibling.called` with target `artifacts` each | `D03-serve` (R-B8VH-Z15T); `D08-trail` (R-7S95-BCYG) |
| a posting to an undeclared account is refused `not_found`; a transaction that does not balance and an amount with three decimals are each refused `invalid`; each refusal is `<word>: <sentence>` | `D04-store` (R-P0HX-8ZR3, R-O14U-5N8Y); `D07-tools` (R-J9U1-X39W, R-J7E9-5JSI) |
| `journal`, `register`, `balances`, `balance_sheet` and `income_statement` answer consistent numbers: `register`'s running balance ends at checking's balance, `balances`' roots sum to zero, the balance sheet's `Equity:Earnings:Current` is the income statement's `net` negated, the walk's records all falling in the current fiscal year | `D04-store` (R-PCOX-2P61, R-PGCM-80E4, R-SR26-1FBX, R-PXF7-KSRU, R-PYN3-YKIJ); `D07-tools` (R-K6RC-8WAN, R-K7Z8-MO1C, R-K975-0FS1, R-KAF1-E7IQ, R-KBMX-RZ9F) |
| `set_book` a lock date answers it and true; a `post` dated on it is refused `invalid`; `book` shows the lock date | `D04-store` (R-OJFB-W7DD, R-NYP1-E3RK, R-P0HX-8ZR3); `D07-tools` (R-JM11-QSOU, R-J9U1-X39W, R-JKT5-D0Y5) |
| `GET /` shows the currency, the lock date, the fiscal year start, the open account count, the transaction count with the reversal included, the latest date and the failing count | `D06-pages` (R-Z1NE-I3QT); `D04-store` (R-OI7F-IFMO) |
| events' `search` finds one `transaction.posted` per transaction posted, the reversal's naming the original as `reverses`, each under the developer and the tool call's request id; none for the repeated key | `D05-events` (R-10QZ-OJNQ, R-RI03-NQ98, R-E86I-QZ7A); which call emits `D07-tools` (R-S1WI-F3JS, R-S34E-SVAH); storing and searching: events. No sibling accepts it yet (seam 5), so delivery is observed at the bus |
| telemetry's `catalog` and `search` for ledger show only the names `D08-trail` lists, one trail event per mutation, the reversal's `transaction.reversed` naming both ids and no trail `transaction.posted` for it; no narration, payee, note text, metadata value or email in any attribute | `D08-trail` (R-7KXR-0QIA, R-OKZX-ES5E, R-ONFQ-6BMS, R-7M5N-EI8Z, R-OR3F-BMUV); `D05-events` (R-10QZ-OJNQ, R-S7NT-E6C7); which call records `D07-tools` (R-S5K7-KERV) |
| ledger reaches nothing but artifacts: telemetry shows `sibling.called` only to `artifacts` | `D01-layout-and-run-seam` (R-6NA7-OIBX, R-6IEM-5FD5); `D08-trail` (R-7KXR-0QIA, R-7S95-BCYG); `D07-tools` (R-KP1T-ZGF2) |
| `sandbox logs ledger` shows nothing from ledger | `D03-serve` (R-90V9-89TN) |

That a test proves each owner's requirement over fakes on loopback sockets, and never over the real artifacts, events, telemetry, nginx or the gateway, is the run's; the walk above is the delivery's, and no requirement states what the real services answer.

## Trust

ledger believes the identity it is sent, as every sibling does: `X-User-Id`, `X-User-Email` and `X-Request-Id` as nginx or the gateway set them on a request to its socket (`D03-serve`, R-8CH9-KUZR). It asks auth nothing; every account, transaction, annotation version, reversal, assertion, note, document and book change is recorded as made by the user id the request names, and no email is kept or answered (`D04-store`). The calls it makes to artifacts forward that identity (`D03-serve`, R-B8VH-Z15T), so artifacts decides what the caller may see. Every signed-in user of the space reads and writes the whole book (seam 1). A process that can reach ledger's socket can therefore name any user and do anything a tool does as that user; every service on the host is trusted, and nginx and the gateway are the only doors from outside, nginx admitting no request without a credential and holding back `/events` and `/declarations`.

What artifacts answers for a path is believed: the artifact id it gives is kept as the document's (seam 6). The bytes stay in artifacts, and a document reference grants nothing artifacts does not grant the reader. The bus event `transaction.posted` carries the money, never the words, and the trail never carries a narration, payee, note text, metadata value or email (seam 5; `D08-trail`, R-OR3F-BMUV), so a subscriber or a reader of the trail learns amounts and account names, which every signed-in user may read anyway. ledger holds no secret; its database holds the books and no credential.

## The finish line

The delivery carries ledger to `us` itself, after the sandbox walk, as the decisions document's Finish line settles it.

1. Build: ledger's build run closes the gap and the gates pass.
2. Sandbox: the walk above passes.
3. Land: the branch onto `main` with `git fetch . wip-ledger:main`.
4. Tag: the next `release-N` after the latest release tag on `main`, annotated `release-N: ledger, the suite's books`, on `main`.
5. Push: `main` and the tag to origin.
6. Deploy: `devctl deploy us release-N`. The start creates `state/ledger.db` and migrates it, seeding the book; there is no previous release of ledger, so no rollback concern beyond removing the app.
7. Check the space, with the space token: `GET /` on `ledger.us.ikigenba.dev` answers the landing page; `book` through the gateway answers the seeded book; home lists ledger among the application services.

## Recorded decisions

- **`cli.NginxConf` is declared here,** as `D01-layout-and-run-seam` and `D02-cli` assign it, and is campaigns' fragment byte for byte (seam 5, the brief): ledger is an emitter-only bus member as campaigns is.
- **The manifest stays `D02-cli`'s;** this document states no manifest requirement and explains what a space takes from it.
- **`guests = false`,** as seam 1 settles it, departing from campaigns' `guests = true`: ledger has no guest route, so nginx and auth answer every request without a credential. `D02-cli` records the departure; `D03-serve` records that ledger's pages still redirect a guest themselves, where candidates uses `identity.Require`.
- **Documents are kept against the real artifacts in the sandbox,** not a stand-in: artifacts can be made to hold a chosen file through its published tools, and the walk then exercises `Head` and `Show` against the real service (seam 6). No stand-in sibling is used, artifacts being ledger's only sibling call.
- **The walk adds two steps to Sequence 2,** the first (the public paths, `/mcp` without a credential, `/declarations` on the socket, the gateway's `describe`) and the last (the bus and the trail), as campaigns' walk has them, so the fragment, the declarations and seam 5 are observed on the real chain. Inside its steps it adds two reads: step 3 opens one account again, to show that re-opening an existing name answers it as it stands and false (`D04-store`, `D07-tools` open_account), and step 10 reads `account` of checking, to show the stored document on its account (`D04-store`, `D07-tools` account). It moves Sequence 2's lock-date step after the refusals and reports, so a later `post` in the walk is never refused by the lock date; and it dates every record on or before today, since `check` lists only assertions dated on or before today (`D04-store`).
- **Bus delivery is observed at the bus** (events' `search` and `catalog`), since no sibling accepts `transaction.posted` yet (seam 5; invoicing comes later).
- No release is named in the manifest, the fragment or any requirement; the release the finish line tags and deploys is the delivery's.

## Consumer tasks

An operator installs the release; opsctl reads `etc/manifest.toml`, writes `DRAIN_SECONDS` into `/etc/opt/ikigenba/ledger/env`, generates ledger's server with auth's `/check` in front of every path and `etc/nginx.conf` included, adds ledger's entry with its icon and group to the services file, and starts the unit; the start creates and migrates the database with no step of the operator's.

A test reads `nginx.conf` from the root package's `Etc()` with `fs.ReadFile` and compares it with `cli.NginxConf`; it imports `cli` and references the constant, so it compiles only if the constant exists.

## REQUIREMENTS

- R-6VVJ-1UX2: The `internal/cli` package MUST export `const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"`.
- R-6X3F-FMNR: The contents of the file `nginx.conf` in the file system the root package's `Etc` returns (`D01-layout-and-run-seam`) MUST be exactly the bytes of `cli.NginxConf`.
