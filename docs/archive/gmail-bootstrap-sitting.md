# Gmail Connector — Bootstrap Sitting Runbook

The **one-time, attended** credential bootstrap for the `gmail` connector. This
is the single human-in-the-loop gate in the whole effort (the "P0b sitting" from
the design review): it runs **after P1** (which produced every tool this runbook
consumes) and **before P2**. It mints all three `GMAIL_*` credentials and seeds
them everywhere they are consumed. Everything after this sitting — the Gmail
client (P2), the producer/poll daemon (P3), the MCP surface (P4), and the deploy
(P5) — runs unattended.

The steps are in **strict order**; each feeds the next. Authoritative context:
`docs/gmail-connector-decisions.md` §2 and `docs/gmail-connector-plan.md`
(the "★ BOOTSTRAP SITTING" section).

## Prerequisites

- A Google account that owns the mailbox the connector will operate.
- `aws sso` access to the `int` account (for the SSM seeding step).
- The P1 scaffold committed: the consent CLI (`gmail/cmd/consent`), `gmail/.envrc`,
  `gmail/bin/secrets`, and `gmail/etc/manifest.env` all exist.
- `direnv` installed and hooked into your shell.

## Steps

1. **Create a dedicated GCP project** for gmail. The separate project is the
   isolation boundary that matters: the two project-wide changes below (adding a
   restricted scope, flipping publishing status) must not touch the dashboard's
   production "Sign in with Google" client, which lives in its own project.

2. **Configure the OAuth consent screen** in that new project:
   - Set the app name and a support email.
   - **Add the `https://mail.google.com/` scope** (Google's restricted tier:
     read + send + permanent delete).
   - Set the publishing status to **"In production"** and leave the app
     **unverified**.
   - *Why production-unverified, not Testing:* apps in **Testing** status with
     External user type have their refresh tokens **revoked after 7 days**, which
     would break the connector weekly. Production-status tokens are durable.
     Leaving the app unverified still requires **no CASA assessment** —
     verification is only needed to remove the one-time "unverified app" warning
     or to serve other users, neither of which a single-owner box needs.

3. **Create a Desktop-type ("installed app") OAuth client** in that project. Note
   the generated `GMAIL_CLIENT_ID` and `GMAIL_CLIENT_SECRET`. Desktop clients get
   implicit loopback-redirect support, so there is no redirect URI to register —
   the consent CLI binds an ephemeral `127.0.0.1` port at runtime.

4. **Write the two client credentials into `~/.secrets/`:**
   ```sh
   printf %s '<CLIENT_ID>'     > ~/.secrets/GMAIL_CLIENT_ID
   printf %s '<CLIENT_SECRET>' > ~/.secrets/GMAIL_CLIENT_SECRET
   chmod 600 ~/.secrets/GMAIL_CLIENT_ID ~/.secrets/GMAIL_CLIENT_SECRET
   ```

5. **`direnv allow`** in `gmail/`. The committed `.envrc` exports the three
   `GMAIL_*` vars from `~/.secrets/`. The `GMAIL_REFRESH_TOKEN` `cat` is
   harmlessly empty at this point — it gets populated in step 6.

6. **Run the consent CLI** from `gmail/`:
   ```sh
   ! go run ./cmd/consent
   ```
   (or run the built binary). Your browser opens to Google's consent screen.
   Click through the **one-time "unverified app"** warning
   (*Advanced → proceed to <app>*) and grant the mailbox scope. The CLI captures
   the authorization code on its loopback port, exchanges it, and **self-writes**
   `~/.secrets/GMAIL_REFRESH_TOKEN` (mode `0600`). It prints **only a masked
   confirmation** like `wrote GMAIL_REFRESH_TOKEN (1a2b…wxyz)` — it never emits
   the token value, so it is safe under `!` whose stdout enters the agent
   transcript.

7. **`direnv reload`** in `gmail/` so the now-populated `GMAIL_REFRESH_TOKEN` is
   exported into the environment.

8. **Seed SSM.** Authenticate and run the seeder:
   ```sh
   aws sso login --profile int
   gmail/bin/secrets
   ```
   This does a non-destructive read-modify-write of the `gmail` key in
   `/ikigenba/int/app-config`, seeding `GMAIL_CLIENT_ID`, `GMAIL_CLIENT_SECRET`,
   and `GMAIL_REFRESH_TOKEN` (masked in the summary, never printed; sibling app
   keys preserved). Type `yes` at the confirmation prompt.

After step 8 succeeds, the credentials exist in `~/.secrets/` (for local
live-verification) and in SSM (for the box). The rest of the build proceeds
unattended.

## Contingency

If **step 6** fails to mint a working token — i.e. Google blocks the
restricted-scope consent on an unverified production app — **stop and surface it.
Do not loop.** The heavyweight fallback is CASA verification, which is a separate
discussion. A successful step 6 prints the masked `wrote GMAIL_REFRESH_TOKEN`
line; anything else (an `access_denied`, a missing `refresh_token`, a blocked
consent) is the signal to halt.
