# candidates

An app of the Ikigenba platform: candidate businesses ingested from the
rows scrapes takes from public records, gathered into prospect lists,
served at `candidates.<host>`. One Go binary serves on the listening socket
it is passed as descriptor 3 (`/run/ikigenba/candidates.sock` on a host),
behind the host's nginx. candidates is stages two to four of the funnel,
ingest, enrich and judge: it ingests scrapes' rows into candidates, one per
real business with every row that describes it attached as evidence,
enriches each and judges each. Each candidate is judged twice, under the
rules version `D13-judging` names: an operating
confidence, how sure candidates is that the business operates today, with
the evidence behind it, and a revenue range. A prospect list is a saved
search the owner creates and grooms: filters, a sort order, a size and a
reject list, its members worked out each time it is read; nothing is
removed automatically, and a candidate leaves every prospect list only by
the owner's exclusion. A source is Go code in the service
that knows how one scraper's rows become match keys, tags, a size estimate,
a filing date, operating evidence and a category, and the fixed registry
holds four, `fmcsa`, `ppp`, `form5500` and `osha_ita`; the scraper
`mn_parcels`, whose rows parks are derived from, is not one. A pass is one
ingest, judge, enrich, targeted enrich, parks pass or walk, recorded at once and worked in the background: an ingest
is started by a `scrape.finished` event of a `done` run, delivered on the
bus, and ingests that run's rows without judging, or by the `ingest` tool,
which ingests one named scraper's current run the same way or, named none,
every source's current run, then records every derived kind of enrichment
for every candidate and then judges; a judge is started by the daily
`cron.judge_candidates.fired` event, delivered on the bus, or by the `judge`
tool, and judges every candidate, reading nothing from scrapes; an enrich,
started by an
ingest or a walk that ends `done` or by the `enrich` tool, walks the registry of
enrichment kinds `D16-enrichers` names, one kind or every kind, derived
kinds first, and records a result for each candidate whose inputs for a
kind differ from those of its newest result, fetching from outside only for
the website check, and reads nothing from scrapes; a targeted enrich, of
kind `enrich_target`, started by the `enrich` tool naming a prospect list
or a candidate, walks one kind over that target, and for `website_search`,
`site_text` or `brief` runs the owner's prompt named for the kind,
`website-search`, `site-text` or `brief`, for each due candidate, and for
`drive_route` asks Google's Geocoding and Routes APIs for the drive from the
origin to each due candidate's street address; a parks pass reads the
parcel rows of a `mn_parcels` run and derives from them industrial parks,
places the owner can name for a later walk and never candidates, and is
started by a `scrape.finished` of `mn_parcels` delivered on the bus, by the
`edit_park_rules` tool, or at start, when scrapes' current `mn_parcels` run
is not the one the parks were last derived from. A walk, started only by the
`walk` tool naming parks, searches them with Google Places Nearby Search in
circles up to the walk ceiling and matches or creates a candidate for each
place found. An ingest or a parks pass
reads the run's record and rows from
scrapes over its socket through appkit's `scrapes` client, as the caller
for the `ingest` or `edit_park_rules` tool and as the service identity for a
delivery or a start, and the start itself asks scrapes once, as the service
identity and in the background, for the current `mn_parcels` run; a targeted pass of a prompts kind runs its prompts
through appkit's `prompts` client over the socket the services file's
`prompts` entry names, as the caller of the `enrich` that started it;
candidates calls no other sibling. It names campaigns only to link to a
campaign's page, built on the services file's optional entry `campaigns`. Outside the suite it fetches nothing but
the websites the website check visits, the pages `add_page` keeps and the
URL a website search's run names, which it confirms with one `Checker.Get`,
over outbound HTTP through `cli.Process.HTTP`, and the geocoding and routing
requests of a targeted `drive_route` pass and the Nearby Search requests of a
walk, over HTTPS to Google through the same client; when `IKIGENBA_SANDBOX` is
present the website fetches go only to an in-process stub on loopback and a
walk's requests to an in-process fake, while a `drive_route` pass still
reaches Google, so nothing a sandbox runs reaches outside but a route a
developer asked for. It holds two secrets, which its manifest lists and reads from its
environment at start: `GOOGLE_MAPS_API_KEY`, the key every request to Google
carries, and `CANDIDATES_ROUTE_ORIGIN`, the address a drive route is measured
from, an address rather than a credential, listed as a secret so the owner
can move the base on a space without a release. Unset or empty, the key
leaves candidates with no routes client and a targeted enrich of
`drive_route` refused and no Places client, a walk refused; the origin defaults to Sandstone, MN. The key is
never written to its database, its trail, a page or a tool's answer. It keeps its candidates, links, estimates,
judgments, enrichment results, facts and kept pages, the owner's rulings,
prospect lists, rejects and opens, its
campaign uses, its passes, its parks with their parcels, the places its walks
found and each park's walks, the park rules and the ZIP drive-time table in its own
SQLite database, `state/candidates.db` resolved against the working
directory and opened through appkit's `db` package; it is the only writer of
it. It emits an event on the bus for each pass that begins and each that
ends, and records every scrapes read in the trail. It takes campaigns'
`campaign.member.added`, `campaign.member.rejected` and
`campaign.piece.delivered`, delivered on the bus, and records each as a
campaign use of the candidate it names, followed through a merge, starting
no pass and reading no sibling. Its manifest sets
`guests = false`: nginx lets no request without a credential through, to the
pages and to `/mcp` alike, and its nginx fragment answers the bus's two paths
404 at the public name. At `/mcp` it offers the twenty-three MCP tools
`D10-tools` catalogs, which agents reach through the MCP gateway, and,
unlisted, the legacy `cohort` tool a campaigns of the previous release still
calls. At `/` it serves a page of the prospect lists and the passes, a page
per prospect list at `/prospect-lists/<id>`, a page per candidate at
`/candidates/<id>`, a page per kept page at `/candidates/<id>/pages/<n>`, a
page per pass at `/passes/<id>`, a page of the parks at `/parks`, a page
per park at `/parks/<id>`, a tools page at `/tools`, an about screen
at `/about` and a not-found page, and `/cohorts/<id>` redirects a prospect
list's id of the previous release to its page. On a host it runs as
`bin/candidates` of its release with
`/var/opt/ikigenba/candidates` as its working directory and its environment
from `/etc/opt/ikigenba/candidates/env`; a developer runs the same binary
from the checkout. The module path is
`github.com/ikigenba/ikigenba/candidates`. It requires appkit
(`github.com/ikigenba/ikigenba/appkit`) and uses appkit's packages `page`
(the banner and footer, and the shared static files under `/_appkit/`),
`identity` (the caller nginx authenticated, required on every route but the
event bus's two), `mcp` (the server mounted at `/mcp`, and the client the
tests drive it with), `telemetry` (the event contract, the request
middleware and the writer candidates' events go through), `db` (the
database's handle and its migrations), `services` (the services file),
`version` (the identity and its display string), `events` (the event bus)
`scrapes` (the client of scrapes' socket) and `prompts` (the client of
prompts' socket). It uses neither `artifacts` nor `connections`. It also
requires `golang.org/x/net`, whose `html` package the website check parses a
page's title with and `add_page` strips a page to its text with.
The contract is the documents in `specs/design/`. This file restates none of
it. candidates is an app, so it has no stories: its intent is the decisions
document that delivered it, or what was agreed in conversation, together with
its templates under `assets/`, and the design is the record.

This sub-project is spec-driven: `specs/design/` defines the contract, and the
build run writes the Go source, the tests, `go.mod`'s requirements and
`go.sum`, the migrations under `migrations/` (`0001_candidates.sql`,
`0002_links.sql`, `0003_estimates.sql`, `0004_assessments.sql`,
`0005_decisions.sql`, `0006_passes.sql`, `0007_zip_drive.sql`,
`0008_windows.sql`, `0009_judgments.sql`, `0010_cohorts.sql`,
`0011_enrichments.sql`, `0012_facts.sql`, `0013_site_text.sql`,
`0014_brief.sql`, `0015_website_search.sql`, `0016_campaign_uses.sql`,
`0017_prospect_lists.sql`, `0018_drive_route.sql`, `0019_opens.sql`, `0020_headcounts.sql`, `0021_link_keys.sql`, `0022_headcount_trend.sql`, `0023_parks.sql`, `0024_walk.sql` and `0025_search_cases.sql`), which the root package embeds, and everything under `etc/` (`etc/manifest.toml`
and the nginx fragment `etc/nginx.conf`). The
delivery that created candidates seeded `go.mod` on appkit; the build run
owns it and `go.sum` from then on and changes them as design says. It never
writes `assets/`, `share/`, `tools/`, this `AGENTS.md`, the `Makefile` or
`.golangci.yml`. `state/` is where a running candidates keeps
`candidates.db`; it is created at run time and never committed. See the
`spec` and `build-spec` skills. Everything below is what the build run
computes the gap and runs the gates against; it is written outside the run
and read-only to it.

## Assets

`assets/` holds candidates' markup: Go `html/template` files, each opening
with a comment naming the data it receives: `landing.html`,
`prospect_list.html`, `candidate.html`, `page.html`, `pass.html`, `parks.html`,
`park.html`, `about.html`, `tools.html` and `notfound.html`. Every page is shown only to a signed-in user. They are
written by the user or the delivering agent, following the repository's
`design/`, and are inputs to the spec: the build run reads them and never
writes them. The code parses them into the set appkit's `page.Templates`
returns and executes them by template name; it never writes markup of its
own, not even a fragment or an error page. The one departure is design's:
the website check's loopback stub answers with bodies of its own, fixture
bytes standing in for a foreign site, which candidates' routes never serve
and no person sees (`D15-website-check`). Go's `embed` reaches only files at
or below the embedding package's directory, so the module's root package (the
directory holding `go.mod`) embeds `assets/`, and design names what it
exports.

Every word a person or an agent reads, and every class, id and attribute,
lives in exactly one place, a template under `assets/` or a named copy constant
in the source; a test and a requirement never spell one. Design names each
template and the data it receives, never its text, hooks, markup or styles. A
test proves a page by executing the named template with the data the design
says and comparing, or by checking that a value the test supplied appears in
the body; it never looks for a word or a tag. A change to copy or markup is an
edit to the asset alone. A needed template that is missing or wrong, or a
template that cannot show a state the design names, is an issue: the run files
it in `specs/issues/` and never edits the asset to close it.

candidates holds no copy of the stylesheet, fonts, or licences; appkit's
`page` package embeds and serves them. `share/icon.svg` is candidates' icon,
shown in the banner's trail and on home: Tabler's `filter` outline from
`design/ikigenba/icons/tabler/`, without its class, width, height, or
invisible bounding path, as `design/README.md` asks of a service icon. The
build run never writes it. `devctl build` packs it beside `bin/` and `etc/`.

`share/zip_drive.csv` is the ZIP drive-time table (`zip,minutes`, drive
minutes from Sandstone, MN), which the root package embeds and the start
loads into the database whole. It is built by `tools/zipdrive/build.sh`,
whose README names its inputs and the date it ran; both are human-authored,
and the build run never writes either.

## Toolchain

- Go 1.26 (`go version` must report 1.26+)
- a C compiler `cgo` can use (`gcc`, say): `go test -race` needs it, and
  without one gate 4 fails with `go: -race requires cgo`. The release build
  itself is cgo-free, which gate 3 proves.
- the modules `go.mod` requires, in the Go module cache; `go.sum` is
  committed, and the gates themselves run offline. `go.mod` starts with the
  one requirement below, set by the delivery that created candidates; the
  build run sets any other and its `go.sum` lines, and moves to another
  release only when this file names one:
  - appkit `v0.30.0`, set with
    `go get github.com/ikigenba/ikigenba/appkit@v0.30.0`: a release that
    exports the `db`, `paging`, `version`, `scrapes` and `prompts` packages
    and whose
    `page.Banner` carries the trail, the home link and the tools flag the
    templates use, with `Release` and `Commit`. What it adds,
    `candidates.Client.ProspectList` and `scrapes.Row.Scraped`, candidates
    does not need; it moves with campaigns, which does. See Adopting appkit
    below.
  - `golang.org/x/net`, for its `html` package, which the website check
    parses a page's title with and `add_page` strips a page to its text
    with: approved by the delivery of the website
    check, and required by the build run at a release it chooses.

  These two and the modules they pull in are candidates' only
  dependencies.
- `golangci-lint` v2 (config: `.golangci.yml` in this directory)
- a POSIX shell at `/bin/sh`: the one exec'ing test starts the binary through
  it (see Test discipline)
- GNU `make`: the developer targets in the `Makefile` (see Build); no gate
  runs through it

**The SQLite driver is appkit's.** candidates stores its state in SQLite
through appkit's `db` package, which brings `modernc.org/sqlite`: pure Go, so
the release build stays cgo-free (gate 3), and already approved for the
repository. candidates neither requires nor imports it directly. Adding any
other external dependency needs approval first, the user's or a delivery's.

### Adopting appkit

appkit is required only at a published release, here `v0.30.0`, fetched
through the ordinary module proxy and checked against the checksum database
(see the root `AGENTS.md`); it is set with
`go get github.com/ikigenba/ikigenba/appkit@v0.30.0`. No `replace` directive,
no `go.work`, no local module cache stands in for it.

## Test files

The sub-project's tests are all `*_test.go` files in the module: the root
package, `cmd/candidates` and everything under `internal/` (`internal/cli`,
`internal/settings`, `internal/server`, `internal/web`, `internal/store`,
`internal/sources`, `internal/fmcsa`, `internal/ppp`, `internal/form5500`, `internal/oshaita`, `internal/judging`,
`internal/parks`, `internal/places`, `internal/passes`, `internal/website`, `internal/routes`, `internal/enrichers`,
`internal/trail`, `internal/urls`,
`internal/tools` and `internal/pages`).
This is the file set the canonical gap greps for requirement ids:

```
grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | sort -u
```

`specs/`, `assets/`, `share/` and `tools/` hold no `*_test.go`, so the
design documents never enter the test side of the grep.

**No id-shaped literal in a fixture.** The grep cannot tell a requirement tag
from any other string of that shape: such a literal anywhere in a `*_test.go`
file counts as a covered id. candidates echoes back what callers and its
siblings give it — scrapes' rows and every field of them (DOT numbers,
loan amounts and dates, EINs, establishment ids and names, filing years, employee counts, hours worked, business names, addresses, phones, emails, counties,
people, facts, and of parcel rows PINs, owner and taxpayer names, site
addresses, towns, counties and use classes), source ids, scraper names, run ids, user ids and emails,
request ids, bus event ids, the services file's URLs, ruling text,
prospect list names, reject reasons, the park rules' counties and labels,
park names, campaigns' event payloads (campaign and piece ids,
`camp_…` and `piece_…`, and a campaign reject's reason), and the `cand_`,
`pass_`, `pl_` and `park_` ids candidates mints and the `coh_` ids of the previous
release it maps to them — and tests echo source ids above all, since every link,
split and candidate page names one, so a test that sends an id-shaped value
lands one in the test file. No fixture carries one: not a header value, a
query parameter, a tool argument, a fake scrapes' or fake prompts' answer,
a fake prompts' standard output (a brief's summary, reasons and labels, a
website search's URL, reason and sites set aside), a fake Google answer (a
formatted address, a status), an API key, a route origin or a street
address,
a row's field, a
source id, a run id, a candidate id, a pass id, a prospect list id, old or new, a park id, a Place ID, a place's name, address, website or phone, a parcel's PIN, a park rule's county or label, a URL, a services file entry,
an event's payload, a campaign or piece id, a ruling's, reject's or campaign
reject's reason, a prospect list's name, or an expected body.

## Test discipline

These rules govern the unit tests: everything `go test ./...` runs.

- Offline: no network beyond loopback and Unix sockets, no real credentials.
- Deterministic: time, randomness, and environment are injected; no test
  sleeps to wait for something. Clocks and timers are injected: a test hands
  in a `Now`, a `Sleep`, an `After` and a `Rand` it controls. The one
  exception is the database's own busy timeout: appkit's `db.Open` sets a
  fixed five-second `busy_timeout` that no seam shortens, so a test that
  proves the store's retry of a busy write, by holding a write transaction
  open on a second handle, waits that timeout out on every busy attempt
  while the injected `Sleep` records the pauses. The suite holds one such
  test that exhausts `store.WriteRetries`, about thirty seconds; every other
  busy-write test releases the lock after the first attempt, about five
  seconds each.
- No fixed ports: a test binds `127.0.0.1:0` or a Unix socket in a
  temporary directory.
- Isolated: a test touches only its own temporary directory, never the
  developer's home, config, or real state.

**The run seam carries the process.** candidates binds nothing itself; it
serves on the listener it is passed. Arguments, environment lookup and
removal, the pid, the inherited listener, the output streams, the clock, the
pause between delivery attempts, the sweeper's, the website check's and
the routes client's and the Places client's timer, the random source, the
working directory and the HTTP client the website check, the routes client and
the Places client fetch through come in through the run seam design declares
(`cli.Run` with `cli.Process`), and tests inject them. A `Run`-level test
sets the version string to one of its own and `Dir` to a temporary directory
of its own, so the database lands there and never in the checkout. A test
never reads or changes the real environment, clock, or randomness, and never
leaves the inherited-listener step unset, since that would take the test
process's real descriptor 3. The one exception is the working directory: a
test that proves what an empty `Dir`, or the root package's `Assets`, `Etc`,
`Migrations` or ZIP table does whatever the working directory is may
`t.Chdir` into a temporary directory of its own, and such a test does not
call `t.Parallel`. A test learns that the server is ready the way systemd
does: it binds a Unix datagram socket in a short temporary directory
(`os.MkdirTemp("", ...)`, since a Unix socket path is limited to 108 bytes
and `t.TempDir()` can exceed it), names it in `NOTIFY_SOCKET`, and waits for
`READY=1` with a deadline that fails the test. A test waits on the clock in
two places only, because there the deadline is the behavior: a drain test,
which keeps that wait to a few seconds, and a test that a pass reaches `done`
within the deadline a requirement states; a busy-write test's wait on the
database's busy timeout is the exception the test discipline above names.
The gates run offline as an ordinary user, with no systemd.

**The siblings are faked on loopback.** No test reaches scrapes. A
`Run`-level test writes a services file whose `scrapes` entry names a Unix
socket in a short temporary directory on which a loopback fake serves
scrapes' three read paths (`/scrapers/<name>/current`, `/runs/<id>/record`
and `/runs/<id>/rows`) with the runs and rows the test chooses, refuses or
fails on demand, holds an answer open when a test needs it, and records each
request it receives, identity headers included; since a `Run` with a
`scrapes` entry makes the start read of the current `mn_parcels` run, the
fake answers that read too, with 404 where the test wants no parks pass; a
test of the passes or the tools hands them appkit's `scrapes` client built over such an entry, as
design names. A test proves a read by what the fake received. No test reaches
prompts either. A test that drives a targeted pass of a prompts kind,
`website_search`, `site_text` or `brief`, writes a services file whose `prompts` entry names a
Unix socket in a short temporary directory on which a loopback fake serves
prompts' MCP tools `show`, `run` and `result`: `show` answers the prompt
named for the kind, `run` the starts and `result` the run records the test
chooses, with the standard output the test chooses; it refuses or fails on
demand, holds a run `running` when a test needs it, and
records each call it receives, arguments and identity headers included; a
test of the passes or the tools hands them appkit's `prompts` client built
over such an entry, as design names. The bus and telemetry are captures.
The exec'ing test never lets the binary reach scrapes or prompts, since the
one pass it starts is an enrich of `website_check`, which reads nothing from
either.

**Websites are faked, never fetched.** No test reaches a website outside
loopback. A test of the check, or of the website search's confirmation
fetch, hands `website.Checker` a client over an `http.RoundTripper` of its own, which answers by URL, holds or fails a
request on demand and records each it receives, and an `After` whose
channels it holds, as design names; a test of the stub checks through the
stub's own client, which dials only its loopback listeners. A `Run`-level
test leaves `Process.HTTP` nil, so it has no checker, hands in a client over
such a transport, or names `IKIGENBA_SANDBOX` in the lookup it gives `Run`
to check against the stub. The real fetcher's refusal of inward addresses is
proved with addresses it refuses before any connection, never by fetching.

**Google is faked, never reached.** No test reaches Google's Geocoding,
Routes or Places API, and no test holds a real key. A test of the routes client, or of
a pass, a tool or a `Run` that routes, hands `routes.Client` an `*http.Client`
over an `http.RoundTripper` of its own, which answers each request with the
body the test chooses, fails or holds it on demand and records each it
receives, a key of the test's own and an `After` whose channels it holds, as
design names; a `Run`-level test sets `GOOGLE_MAPS_API_KEY` and
`CANDIDATES_ROUTE_ORIGIN` only through the `LookupEnv` it gives `Run`, and
hands in `Process.HTTP` over such a transport when it drives a route. A test
of the Places client, or of a walk, hands `places.Client` the same kind of
client, key and `After`, as design names. There is no loopback stub for
Google's routing: `IKIGENBA_SANDBOX` selects the website stub and the walk's
in-process Places fake (`places.Fake`).

**The ZIP table is the embedded one or the test's own.** A test that needs
the real drive minutes reads the table the root package embeds, through the
root package, never from the checkout's `share/`; a test may instead load a
ZIP table of its own into its own store with `ReplaceZipDrive`, as design
names.

**The state is an isolated directory.** Each test gives candidates a working
directory inside a test-owned temporary directory, so the database is the
test's own. A test that needs a store opens its own database with appkit's
`db.Open` at a path in its own temporary directory, with the root package's
`Migrations()` and a clock it controls, closes the handle when it ends, and
builds the store over that handle with `store.New`. Tests may create
filesystem fixtures inside that temporary tree (a regular file named
`state`, a file at `state/candidates.db` that is not a database, a database
a test's own `db.Open` made and then changed through `DB.Write`, candidates,
links, rulings, prospect lists, rejects, opens, enrichments, pages, ZIP tables and passes a test's
own store recorded, passes left
`running`, `done` or `failed`); nothing touches
`/var/opt/ikigenba/candidates`, the checkout's `state/` or a shared file. A
database failure is provoked with `SetFailing(true)` on the handle the store
the test handed the server or engine was built over, before a call or from a
hook design names, never by corrupting the file, removing permissions or
closing the store. Tests prove candidates' use of the database, its schema,
its store and its wiring, never SQLite's own guarantees (atomicity,
durability, locking) and never appkit's `db` contract (opening, migrations,
transactions, `db status`), which appkit's own tests prove; they prove what
a store holds by reopening its path, never by reading tables. The one
exception is a design's rollback-compatibility requirement that names
`DB.Read` or `DB.Write` and "the previous release's store": it is proved
by running that release's SQL through those calls, the only seam a
rollback observes.

**Time and the sweep are driven, not waited out.** A pass's started and
finished times, a link's first and last seen, a ruling's time, a judge's
weighing of a filing's age and the prune's ordering are proved through the
injected `Now`: a test advances the clock it handed in, never waits real
seconds. The sweeper waits on `Process.After`; a test hands in an `After`
whose channels it holds and delivers on one to make the next prune happen.
The website check's attempt timeouts and its pace between checks wait on the
same `After`, told apart by duration, and a test delivers on or holds those
channels the same way. The Places client's request timeout waits on the same
`After`, with `places.Timeout`. A test that sleeps to age a pass or a filing or to let a sweep come round is a
bug. The judgment formulas live once, in Go: no pass and no start rewrites
the stored judgments in SQL, so no test compares SQL with Go. A test waits
for something it started to finish (a pass reaching `done` or `failed`, a
fake receiving a request) with a deadline that fails the test.

**Events come from the test's own sink.** Where a test observes the events
candidates records or emits, it hands candidates' writer appkit's
`telemetry.Capture`, or a services file whose `telemetry` or `events` entry
names a Unix socket the test holds, as design names; it hands the emitter
appkit's `events.Capture` and sets an event cause with `events.NewContext`.
Every test builds its own database; no test depends on state another test
made or on the order the tests run in.

**Environment variables, set by the test.** appkit's constructors read
`IKIGENBA_SERVICES` (`services.Variable`) from the process environment; it is
the one environment read candidates cannot route through the run seam, and in
the binary only `main` makes it. A test that reaches such a read, directly or
through a candidates constructor, first sets that variable with
`testing.T.Setenv` to a services file it wrote or to the empty string, so the
developer's environment never decides a result. It is the only variable an
in-process test sets for candidates' own code; `DRAIN_SECONDS`,
`CANDIDATES_KEEP_PASSES`, `CANDIDATES_ROUTE_ORIGIN` and
`GOOGLE_MAPS_API_KEY` reach `Run` through the `LookupEnv` the test gives
it. The exec'ing test also sets `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE` with
`testing.T.Setenv`, so it may call appkit's `version.Read()` and the
identity's `String()` under the same two values it composes into the child's
environment; the commit it sets is longer than seven characters, so a `main`
that skipped shortening would fail. Any test that sets a variable with
`testing.T.Setenv` does not call `t.Parallel`.

**Process-wide state is not shared.** A test that changes the standard
`log` package's output, as the proof that the stub writes nothing through
it does with `log.SetOutput`, restores it when it ends and does not call
`t.Parallel`, so no other test's line lands in its buffer. Nor does a test
that measures the heap with `runtime.ReadMemStats`, since another test's
allocations would land in its reading.

**Identity comes from headers the test sets.** appkit's `identity.Require`
wraps every route but the event bus's `/events` and `/declarations`, so a
request reaches the pages or `/mcp` only with an `X-User-Id` header. A test
sets the identity headers to be a signed-in user, or omits `X-User-Id` to
exercise the missing-identity answer, and sets `Host` and
`X-Forwarded-Proto` the way nginx does. Where it matters it acts as two
users, and it proves that a pass or a ruling is recorded as made by its
caller by its record. What appkit's middleware and writer do is appkit's
contract; candidates' tests prove only that its handlers are wired to them,
by use.

**MCP through appkit's client.** A test drives `/mcp` the way a client would:
it serves the handler it built on loopback or a Unix socket and calls it with
appkit's `mcp.Client`, passing the caller whose identity headers the client
forwards. Raw HTTP to `/mcp` is for what the client cannot send (a missing
identity header, `server/discover`), never a substitute for the client.
Assertions are on the `mcp.Result` and `mcp.ToolInfo` the client returns.
A test reaches the unlisted legacy `cohort` tool the same way, through the
client on the handler that serves `/mcp`, as design names.
candidates' tests never re-prove appkit's transport.

**Pages are raw HTTP.** A test drives `/`, `/prospect-lists/<id>`,
`/cohorts/<id>`, `/candidates/<id>`, `/candidates/<id>/pages/<n>`,
`/passes/<id>`, `/parks`, `/parks/<id>`, `/tools`, `/about`, the event bus's two paths and any other
path as a browser or the bus would: raw HTTP requests on the handler it
built, served with `httptest` or on a Unix socket, with the identity headers
where the route takes them, and never follows a redirect.

**The addresses are the test's own.** The banner's profile and sign-out
addresses are built on the services file's entry `auth`, and the links to
scrapes' row pages on its entry `scrapes`, as design names, or, with none,
on the request's scheme and host; the links to campaigns' campaign pages
are built on its entry `campaigns`, and with none there is no link. A test chooses them: it writes a services
file into its own temporary directory whose entry names a URL, or none, and
hands its path in the config or names it in `IKIGENBA_SERVICES` through the
`LookupEnv` it gives `Run`. The tools page's `tools.Catalog()` takes no base.

**No test runs the page's scripts.** Every page carries appkit's feedback
script.
The gates have no browser and no JavaScript engine, and adding one is an
external dependency no one has approved, so a test asserts what a response
body carries and never what a script would do with it.

**No test reads the checkout.** A test opens no file of this directory — no
`.go` file, not `go.mod` or `go.sum`, nothing under `etc/`, `share/`,
`tools/` or `assets/` — and never parses or inspects source. It proves what
the design declares by using it. Handing `cmd/candidates` to `go build` is
not the test reading it.

**One exec'ing test of the binary, and only one.** No test starts a process:
tests under `internal/` never start candidates. The wiring in
`cmd/candidates` can be proved no other way, so exactly one test that execs
the binary is admissible, and it lives in `cmd/candidates`. It builds the
binary into a temporary directory and runs it with `--version`, with
`manifest`, with `bogus`, and bare with no socket passed in. For the serve
case it stands in for systemd: it makes a Unix socket in a short temporary
directory, passes it as `exec.Cmd.ExtraFiles[0]` (descriptor 3 in the
child), and starts the child through
`/bin/sh -c 'LISTEN_PID=$$ LISTEN_FDS=1 exec "$0"' <binary>`, because
`LISTEN_PID` must be the child's own pid and `exec` keeps the shell's. The
child runs in a test-owned temporary working directory, where it creates
`state/candidates.db`. Its environment is one the test composes, never the
developer's, with non-empty `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`; the
test computes the release and commit it expects with `version.Read()` and the
display string with its `String()` after setting the same two values with
`t.Setenv`, so no test spells a version. Separately it runs `--version` with
neither variable in the child's environment and expects exactly one empty
line. The test waits for `READY=1`, makes the requests design names for the
binary, over the socket, then stops the child with `SIGTERM`, and in a second
run with `SIGINT`, asserting what design states. It may make exactly one
`enrich` call, with kind `website_check`, on the empty store the child
started on and with `IKIGENBA_SANDBOX` absent from the child's
environment, expecting a `running` pass record, to prove the binary wires a
real HTTP client; it waits for that pass to end before it stops the child,
starts no other pass and, with nothing due, fetches nothing. The
binary's `--version` output, the MCP `serverInfo` and `service.started` all
carry that display string, and the pages' footer the release and commit. Any
other test that builds, execs, waits on, or signals a process is a bug.

## Live tests

There are none. No test reaches outside the suite: scrapes and prompts,
the two siblings candidates calls, are faked in every unit test, so nothing
reaches a real prompts, and the website check, `add_page` and the website
search's confirmation fetch through a transport the test holds or the
loopback stub, as the routes client and the Places client request through a
transport the test holds. The sandbox fetches websites only from the stub,
walks only against the Places fake, and reaches Google only for a
`drive_route` pass a developer starts, and the sandbox check
proves the real chain, one real route included.

## Gates

Run from this directory (`candidates/`), in order; every command must exit 0.
No skipped tests, no disabled linters laundering a failure. A per-finding
suppression comment (`//nolint` and the like) is a skip: the run never adds
one; a finding it cannot fix, or believes is wrong, is filed as an issue.

1. `test -z "$(gofmt -l .)"` — fails if any file is unformatted (fix with
   `make fmt`)
2. `go build ./...`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/candidates`
   — the release build, static and cgo-free, the way `devctl build` builds
   it, which is why the SQLite driver must be pure Go
4. `go test -race ./...`
5. `GOLANGCI_LINT_CACHE="$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners`
   — the cache lives in this worktree's git directory, so worktrees never
   share it, and `--allow-parallel-runners` lets gates for several
   sub-projects lint at once; `make lint` runs this form

Gate 5 also flags an undocumented `package main` (`revive`) and an
`http.Server` without `ReadHeaderTimeout` (`gosec` G112), and a TLS
configuration that sets `InsecureSkipVerify` (`gosec` G402), which the stub's
client avoids by trusting its own certificate; all are fixed in code, never
suppressed.

## Commit conventions

```
<imperative summary of the phase, <=50 chars>

<optional: one or two lines on what changed and why>

Requirements: R-XXXX-XXXX, R-YYYY-YYYY
```

The `Requirements:` trailer lists the phase's ids so history stays greppable
by id.

## Build

`make` builds `bin/candidates` from the checkout (`make build`). `make fmt`
rewrites unformatted files, and `make test` and `make lint` run the test and
lint gates. The gates do not go through `make`: they call the Go tool
directly, and the one test that needs a binary builds its own into a
temporary directory.

## Deploy

candidates ships in the suite release: `devctl build <sha|tag>` builds it
with every other app, its part of the release holding `bin/candidates`,
`etc/` and `share/icon.svg`, and `devctl deploy <space> <sha|tag>` deploys
that release. The host must run events, which candidates emits to and takes
scrapes' `scrape.finished`, cron's `cron.judge_candidates.fired` and
campaigns' `campaign.member.added`, `campaign.member.rejected` and
`campaign.piece.delivered` from,
scrapes, which it reads over the socket, in a pass and once at start, and
prompts, which a targeted pass
of a prompts kind runs the `website-search`, `site-text` or `brief` prompt
through over the socket; its
services file names them, with telemetry and auth, and may name campaigns,
whose entry candidates uses only to link to a campaign's page. The host needs outbound HTTP and HTTPS to the
public internet, since the website check and `add_page` fetch candidates'
own domains and the website search confirms the URL its agent names, and
outbound HTTPS to Google's Geocoding and Routes APIs for `drive_route` and to
Places API (New) for a walk. The
two secrets its manifest lists, `CANDIDATES_ROUTE_ORIGIN` and
`GOOGLE_MAPS_API_KEY`, must exist on the space (`devctl secrets push`) or in
the shell that runs `sandbox up`, or the app is refused; a changed value
takes effect only at the next activation, a push followed by a deploy. The host's backup archives `state/`
whole, so `candidates.db` is taken with it; it holds candidates, their
evidence copied from public records, their enrichment results, facts and
kept pages, the owner's
rulings, prospect lists, rejects and opens, the campaign uses, the passes,
the parks derived from parcel rows with their parcels' PINs, owners, site
addresses and building values, the places each walk found in a park with
their names, addresses, websites and phones, each park's walks with what
they found and cost, and the park rules, and no secret.
