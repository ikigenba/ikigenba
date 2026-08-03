# dashboard — Product (web surface & sign-in)

**Authority: intent.** This document owns *why* this change exists, *for whom*,
what is in and out of scope, and what we **promise** the user — in outcome terms
only. Mechanism, route tables, template structure, redirect codes, and test
assertions live in `project/design/README.md`. Where the two touch observable
behavior, product states the *promise* and design states the *exact, checkable
proof*. This product doc scopes the dashboard's web-surface reshape and its
sign-in surface: splitting the single hybrid apex page into purpose-built pages,
enriching the logged-out login page with a brief, diminished explanation of the
**ikigenba** name, adding an owner-only **metrics** page that graphs the box's
resource health over the last day, and offering a **second, permanently separate
sign-in method — "Sign in with GitHub"** — alongside the existing Google
sign-in, and making the box's front door **forensically visible**: every request
the dashboard admits or refuses on a service's behalf becomes part of the
suite's traceable record. It does not re-state the dashboard's whole product (identity, OAuth AS,
push, inventory) — only the surfaces this change reshapes.

## Problem

Today the dashboard serves **one** hybrid page at `/`. Logged out, it is a
sign-in wall. Logged in, that same page tries to be everything at once: it is the
**home** a returning owner lands on (the "connect your agent" install snippet and
the list of the box's MCP services) **and** the **account-management console**
(create/revoke personal access tokens, view/revoke the OAuth grants other agents
hold). Two unrelated jobs share one scroll.

That conflation hurts both jobs. The owner who just wants to **get connected** —
paste the install line, see what services exist, click through to one — wades
past token tables that are irrelevant to that task. The owner who wants to
**manage their account** — rotate a leaked token, revoke a client's grant — hunts
for those controls below the install instructions on a page that re-renders its
"welcome, connect your agent" framing every visit. The home page and the settings
page want different framing, different prominence, and different return cadence,
and one page can give neither.

The suite is also moving so that **every service serves its own landing page** at
`/srv/<svc>/`. The dashboard's service list still shows each service only as a row
of raw MCP text for hand-wiring — there is nowhere to *click through* to a service
now that each has a real human page.

Sign-in itself is also too narrow: the box admits only people with an account in
its Google Workspace. Collaborators who live in the **ikigenba GitHub
organization** — the people the `github` and `repos` services already serve —
have no way in at all.

## Purpose

Separate **"get connected"** from **"manage my account"** by giving each its own
page, and turn the service list into real navigation:

- The **landing page** (logged-in `/`) becomes a clean **home**: who you are, how
  to connect an agent, and a directory of the box's services you can **click into**
  (each name links to that service's own landing page). It is the first thing a
  returning owner sees and it stays focused on connecting and navigating.
- A new **profile page** (`/profile`) becomes the **account console**: personal
  access tokens (create / revoke) and OAuth grants (view / revoke) — the security
  controls that were buried on the home page — gathered in one deliberate place
  you go to when you mean to manage access, reached by clicking your email.
- The **login page** (logged-out `/`) stays functionally just sign-in, but now
  also tells a first-time visitor what **ikigenba** means — a quiet, diminished
  colophon beneath the control-plane tagline and the sign-in button. It orients;
  it adds no new control.
- A new **metrics page** (`/metrics`) becomes the owner's **at-a-glance box
  health view**: how much memory and disk the box has free, and how much memory
  and disk each service is consuming, each drawn as a graph of the **last 24
  hours**. It is reached from a tile on the landing page and is for watching the
  box, not managing it — it carries no controls, only graphs.
- **Sign-in gains a second door.** Alongside "Sign in with Google", the login
  page offers **"Sign in with GitHub"**, open to members of the ikigenba GitHub
  organization. The two sign-ins are **two permanently separate identities** —
  a person who uses both has two accounts, never one merged account. Connecting
  an agent (MCP client) offers the same choice of provider mid-flow.
- **The front door becomes traceable.** The dashboard decides, for every request
  an agent makes to any service on the box, whether it is admitted. Those
  decisions — the admissions *and* the refusals — become part of the suite's
  record, and each admitted request is tagged so everything it goes on to cause,
  in any service, can be traced back to it. It is what lets the owner ask "what
  did this agent actually do?" or "who kept getting turned away?" and get a whole
  answer instead of a fragment.

The dashboard is the **apex** app, so unlike the other services this page is not a
generic name+version card — the dashboard's logged-in landing **is** its web home.
This change is bespoke to the dashboard; the rest of the suite gets the uniform
v1 landing.

## Users

- **The owner, in a browser.** Signs in, lands on the home page, connects an agent
  by pasting one line, and clicks a service name to open that service's page. When
  they mean to manage access, they click their email to reach the profile page and
  there create or revoke a token, or revoke a client's grant. The two intents now
  live on two pages, each framed for its job.

## Scope

This change does exactly this and only this:

- **Four pages, two of them new.** Keep the logged-out `/` login page as the
  sign-in page — its only function — and add to it a **diminished name-origin
  colophon** explaining the ikigenba name, below the tagline and the sign-in
  button. Keep the logged-in `/` as the **home/landing** page. Add a **profile**
  page and a **metrics** page, each at its own new session-gated route.
- **Metrics = watch the box.** The metrics page graphs, over the **last 24
  hours**, the box's **free memory** and **free disk**, and **each service's
  memory usage and disk usage**. It refreshes about once a minute while open, is
  reached from a tile on the landing page, and is gated to the signed-in owner. It
  shows the most recent day only and starts empty after a restart; it carries no
  controls.
- **Landing = connect + navigate.** The logged-in home keeps the **install
  instructions** and the **service list**, shows the owner's email at the top, and
  carries **sign-out**. Each service in the list is a **link** to that service's
  own page at `/srv/<svc>/`. The owner's email is a **link** to the profile page.
- **Profile = manage access.** The profile page holds the **personal-access-token**
  management (create, list, revoke) and the **OAuth grant** management (view live,
  revoke) that used to sit on the home page. It is reached by clicking the email on
  the landing page and is gated to the signed-in owner.
- **Token management leaves the landing.** Personal access tokens and OAuth grants
  no longer appear on the logged-in home page — they live only on the profile page.
- **Doc truth follows the code.** The dashboard's standing "keep the apex `/` a
  single hybrid page, do not split it into a separate IAM console" rule is now
  **false** and is purged, replaced by the three-page truth.
- **Sign-in returns you to where you were headed.** When a signed-out person
  follows a link to a session-gated page and is sent to sign in, the login page
  remembers that destination and, after a successful sign-in, returns them to it
  instead of dropping them on the home page. A plain visit to the login page (no
  remembered destination) still lands on the home page as before, and the
  remembered destination is always a page on this same box — never anywhere off
  it.
- **Two sign-in methods, two separate identities.** The login page offers both
  "Sign in with Google" (unchanged: verified accounts in the box's Google
  Workspace) and "Sign in with GitHub" (members of the ikigenba GitHub
  organization in good standing; anyone else is refused). A Google identity and
  a GitHub identity are never linked, merged, or treated as the same person —
  even when they share an email address. Everything a signed-in person can do
  works identically regardless of which door they came through: web pages,
  personal access tokens, and connecting agents.
- **Connecting an agent offers the same choice.** When an MCP client connects
  and sends its human to authorize, that person picks Google or GitHub mid-flow;
  the connection then belongs to whichever identity they chose. Existing
  connections and personal access tokens are untouched.
- **Every admission decision joins the suite's record.** Each request the
  dashboard admits to a service, refuses, or turns away for exceeding its rate
  budget is recorded in the suite's forensic trail, together with who was asking,
  what they were asking for, and how it was decided. Admitted requests are tagged
  so the work they cause anywhere in the suite traces back to that one decision;
  refused attempts are each recorded separately, so a run of failed attempts
  reads as a run of attempts rather than one blur. The dashboard's own durable
  security log is unchanged and keeps everything it kept before — the new record
  is additional, never a replacement.
- **The box-health page is renamed from "Telemetry" to "Metrics".** The page,
  its address, and the tile that opens it all read *Metrics*. Nothing about what
  it shows changes. The word *telemetry* now names the suite's forensic record
  service, and one word cannot mean both.

It deliberately does **nothing else** — in particular it does not: add new
account-management capabilities (the PAT and grant features are **moved, not
changed**); change how OAuth, push, or inventory work, or change login beyond
teaching the sign-in flow to return the visitor to a remembered same-site
destination and adding the GitHub sign-in method (the dashboard's own token
mechanics are untouched); link, merge, or migrate identities between the two
sign-in methods; let a rejected GitHub visitor request access; add per-resource
authorization to the profile or metrics page beyond "signed-in owner";
introduce new MCP verbs; give the metrics page any control (it only shows
graphs); persist the metrics history across restarts or alert on it; add any web
page or control for reading the forensic record (that is the telemetry service's
job, not the dashboard's); let the record ever carry a credential, a request
body, or a response body; or let the record's availability affect whether a
request is admitted; or give the
dashboard a generic name+version landing card (its home page is its landing).

## Contractual constants

Promised values the design must honor verbatim and never re-declare:

- **The apex serves these human pages:** the **login** page (logged-out `/`), the
  **landing/home** page (logged-in `/`), the **profile** page (session-gated
  route), and the **metrics** page (session-gated route). The profile and
  metrics pages are the two session-gated routes added by this change.
- **The metrics page graphs the last 24 hours of box health and nothing else.**
  It shows free memory, free disk, and per-service memory and disk usage; it is
  gated to the signed-in owner, refreshes about once a minute, and carries no
  controls. Its history is the most recent day and does not survive a restart.
- **The login page carries exactly two sign-in controls** — "Sign in with
  Google" and "Sign in with GitHub", visually equal peers with Google first —
  keeps its control-plane tagline, and carries the name-origin colophon **only**
  in its logged-out form — the colophon never appears on the logged-in
  landing/home page. Beyond the second sign-in control it adds no new control.
- **GitHub sign-in admits only active members of the ikigenba GitHub
  organization** whose primary GitHub email is verified; everyone else is
  refused.
- **A Google identity and a GitHub identity are never unified.** Each sign-in
  method yields its own permanent identity, even for the same human and the
  same email address.
- **The profile page is gated to a signed-in owner.** A visitor without a live
  session never sees profile content.
- **Personal-access-token and OAuth-grant management live only on the profile
  page** after this change — never on the landing/home page.
- **Each service name on the landing links to that service's own page at
  `/srv/<svc>/`** — the human landing page, not the raw MCP resource URL.
- **Every gated admission decision is recorded** — admitted, refused, and
  rate-limited alike — and no record ever contains a credential, a request body,
  or a response body.
- **Recording never changes a decision.** Whether the suite's record service is
  healthy, degraded, or entirely absent, the same request gets the same answer.
- **The box-health page is named Metrics**, at `/metrics`; the name *telemetry*
  is reserved for the suite's forensic record service and never names this
  page.

## What we promise (user-facing behavior)

- **Logged out, `/` is just sign-in** — and tells you what the name means. The
  control-plane tagline sits above two sign-in buttons — "Sign in with Google"
  and "Sign in with GitHub" — and below them sits a quiet, diminished
  explanation of the **ikigenba** name (its two Japanese roots and what the word
  means together). No other control.
- **GitHub members get in; strangers do not.** Signing in with GitHub as an
  active member of the ikigenba organization lands you signed in like any owner.
  Signing in with any other GitHub account is refused — no session, no partial
  access.
- **Your two sign-ins are two accounts.** Sign in with Google and you are your
  Google identity; sign in with GitHub and you are your GitHub identity. Tokens,
  grants, and every service's data belong to whichever identity created them,
  and nothing ever crosses between the two.
- **Connecting an agent lets you pick the door.** When an MCP client sends you
  to authorize, you choose Google or GitHub on the spot; the resulting
  connection acts as the identity you chose.
- **Logged in, `/` is a focused home.** It shows who you are, how to connect an
  agent (the same paste-one-line install instructions), and the box's services —
  and nothing about token administration.
- **You navigate to a service by clicking its name.** Each service in the home
  list links to that service's own page.
- **You manage access on a page you choose to visit.** Clicking your email opens
  the profile page; there — and only there — you create and revoke personal access
  tokens and view and revoke the OAuth grants your connected clients hold.
- **The profile page is yours alone.** Reaching it requires a live session; signed
  out, you are sent back to the login page rather than shown account controls.
- **Sign-out stays on the home page** where you land, not hidden on a settings
  screen.
- **No capability is lost in the move.** Every token and grant action that worked
  on the old hybrid page works on the profile page, identically — only its
  location changed.
- **After signing in, you land where you were going.** If you clicked into a
  session-gated page while signed out and were sent to sign in, signing in takes
  you straight to that page. Sign in from the login page directly and you land on
  the home page as always. You are only ever returned to a page on this box.
- **What happened at the front door is recoverable.** Whether an agent's request
  was let through or turned away, that decision is part of the suite's record —
  with who asked, what they asked for, and the outcome — and everything an
  admitted request went on to cause elsewhere in the suite can be followed back
  to it. Refusals and rate-limited attempts are recorded just as fully as
  successes.
- **Watching the box happens on a page called Metrics.** The box-health graphs
  live at a page named *Metrics*, opened from a tile of the same name. It is the
  same page it always was, under an unambiguous name; the old *telemetry* address
  no longer exists.
- **You can watch the box's health on the metrics page.** From the landing you
  open a metrics page that graphs, over the last 24 hours, how much memory and
  disk the box has free and how much memory and disk each service is using. The
  graphs advance about once a minute while the page is open. Like the profile
  page, it is yours alone — signed out, you are sent back to the login page. It
  shows only the most recent day and begins empty after a restart.

## Success criteria (outcomes)

Each is a result the owner can confirm against the running dashboard:

- Visiting `/` while signed out shows the sign-in page — the control-plane
  tagline, the "Sign in with Google" and "Sign in with GitHub" buttons, and
  beneath them a diminished explanation of the ikigenba name — and no account
  controls.
- Signing in with GitHub as an active ikigenba-org member signs me in and lands
  me on the home page (or the page I was headed to), exactly as Google sign-in
  does.
- Signing in with GitHub from an account outside the ikigenba organization is
  refused and yields no session.
- Signing in with Google and with GitHub — even with the same email — produces
  two distinct signed-in identities: each sees its own tokens, grants, and
  service data.
- Connecting an MCP client presents a Google-or-GitHub choice during
  authorization, and completing it with either provider yields a working
  connection acting as that identity.
- The name-origin explanation is visible only signed out; once signed in, the
  home page shows no such colophon.
- Visiting `/` while signed in shows the home page: my email, the connect-your-agent
  install instructions, and the list of services — with **no** token or grant
  controls on it.
- Clicking a service's name in the home list opens that service's own page at
  `/srv/<svc>/`.
- Clicking my email on the home page opens the profile page.
- The profile page shows my personal access tokens and lets me create and revoke
  them, and shows my OAuth grants and lets me revoke them — the same actions that
  used to be on the home page.
- Visiting the profile route while signed out does not reveal account controls; I
  am returned to the login page.
- Sign-out is available from the home page.
- The home page shows a tile that opens the metrics page, labelled **Metrics**.
- Visiting `/metrics` while signed in shows graphs of the box's free memory and
  free disk over the last 24 hours, and of each service's memory and disk usage
  over the last 24 hours; the graphs advance about once a minute while the page
  stays open.
- Visiting the metrics route while signed out does not reveal the graphs; I am
  returned to the login page. The old `/telemetry` address is gone rather than
  redirected — that word now names the suite's forensic record service, not this
  page.
- The landing tile and the page it opens both read **Metrics**, and the word
  *Telemetry* appears nowhere in the signed-in web surface; visiting the old
  `/telemetry` address yields nothing rather than the page.
- After an agent makes a call to a service, the suite's forensic record shows the
  dashboard's decision to admit it, and the work that call caused in that service
  can be followed back to that decision as one connected sequence.
- After an agent is refused — a bad or expired credential, a service it has no
  grant for, or too many requests too fast — the suite's forensic record shows
  that refusal too, with the reason, and repeated failed attempts appear as
  separate attempts rather than a single lumped entry.
- After this change the dashboard's docs describe the login, landing, profile, and
  metrics pages, and no longer claim the apex is a single hybrid page that must
  not be split or capped at three pages.
- When I follow a link to a session-gated page while signed out, sign in, and
  succeed, I arrive at that page — not the home page. When I open the login page
  directly and sign in, I arrive at the home page. In neither case am I ever sent
  to an address off this box.
