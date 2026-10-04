# D11-on-a-space

What sites itself contributes when it runs on a space and in a sandbox, and what it does not. The package story (`S20-package.md`), the space stories (`S21-on-a-space.md`) and the sandbox stories (`S22-in-a-sandbox.md`) prove the whole path from a checkout to a browser, curl and an agent, and almost every step on that path belongs to someone else: devctl builds and ships the file, opsctl installs it, writes the environment, publishes the socket, bounds the unit, keeps the database and generates nginx, auth decides every credential and whether a request reaches sites at all, the host's nginx terminates TLS and challenges `/mcp`, the mcp gateway relays tool calls, repos holds the repositories, and the sandbox stands the same arrangement up on a developer's machine. sites' own share is what the earlier designs already state — the layout, the run seam and the settings (`D01-layout-and-run-seam`), the command and the manifest (`D02-cli`), the serve path (`D03-serve`), the catalog (`D04-store`), git and the trees (`D05-git-and-cache`), the pages (`D06-pages`), the site paths and the apex host (`D07-site-serving`), the visitor cookie and the trail (`D08-visitors-and-trail`), the tools (`D09-tools`) and the limits (`D10-limits`) — plus one guarantee the space and sandbox stories lean on that no single earlier document owns: the credential a caller presents never lands in anything sites records or keeps. This design states that guarantee, records the one place sites departs from a story, maps every outcome of the stories to the design that covers it, and says plainly what can be checked only on a running space or sandbox.

This design declares no name. Everything it states is behaviour of names `D01-layout-and-run-seam` declares: `cli.Process` and `cli.Run`, and the `Sink`, `Stderr`, `Dir` and `Database` fields of `cli.Process`.

## What ships

The package story lists three members of the release file: `bin/sites`, `etc/manifest.toml` and `share/icon.svg`. One of those is sites' contract and is already designed: the manifest's exact text is the constant `cli.Manifest` (`D02-cli`, R-SJ18-P0AC), and the root package's `Etc` proves that `etc/` holds exactly the file `manifest.toml` and nothing else (`D01-layout-and-run-seam`, R-XST0-U70O, R-XU0X-7YRD), with exactly the bytes of `cli.Manifest` (`D02-cli`, R-SP4Q-LUZT); that is also how "nothing under `etc/` but the manifest" and "sites ships no `etc/nginx.conf`" are proved, since `D02-cli` declares no nginx constant and an `etc/nginx.conf` would make `Etc` hold a second entry. That `sites --version` prints the version and `sites manifest` prints the manifest byte for byte is `D02-cli`'s (R-SQCM-ZMQI, R-SRKJ-DEH7). That the binary is static `linux/amd64` and cgo-free is the release build gate of `AGENTS.md`, not a requirement. That no `assets/` directory or font ships beside the binary follows from the templates being embedded in the root package (`D01-layout-and-run-seam`, R-XQD8-2NJA, R-XRL4-GF9Z) and appkit's shared files being served from appkit's `page` package (`D06-pages`). That neither the database nor any tree is in the file, and that sites creates `state/sites.db` and `cache/sites/` on its first start, is `D01-layout-and-run-seam`'s (R-2X5A-AH8Q, R-2YD6-O8ZF) and `D03-serve`'s (R-VPC5-94X4).

The rest of the file is not sites' to state. The tarball, its member list, its name carrying the version and no member's path carrying it are `devctl build`'s; `share/icon.svg` is human-authored (`AGENTS.md`), and that its presence lists sites in the launcher is opsctl's services file. No test reads the checkout, so the tarball and the icon are outside what a requirement can state; a developer checks them with the package story's own `tar` commands.

## On a space

sites on a space is the same binary serving on the same terms as under a test: a socket passed as descriptor 3, `/opt/sites` as its working directory, its settings from `/opt/sites/etc/env`, `IKIGENBA_SERVICES` naming the host's services file, the host's `git` on its `PATH`. Who supplies each of those is opsctl's (the unit, the socket `/run/ikigenba/sites.sock`, `etc/env` from the manifest's `[env]` and the space's `DRAIN_SECONDS`, the `[resources]` bounds, the services file with its `url` `https://sites.<space>`, the `git` it provisions, and the replication of the database `[database]` declares); what sites does with them is the earlier designs'. With `/opt/sites` as `Dir`, the catalog is `/opt/sites/state/sites.db`, the trees are under `/opt/sites/cache/sites/`, and `REPOS_DIR` at its default resolves to `/opt/sites/../repos/state/repos`, which is where repos keeps its bare repositories (`D01-layout-and-run-seam`, R-YIEW-VDL9).

Because the manifest says `guests = true`, the space's nginx admits a request with no credential to every path of sites but `/mcp` and passes it on with no identity headers; sites decides for itself who must sign in (`D06-pages` for `/` and `/about`, `D07-site-serving` for a private site). A request whose credential auth refuses, and every request to `/mcp` with no credential, is answered by nginx and auth and never reaches sites. Every request nginx passes carries `X-Forwarded-Proto: https`, which makes the visitor cookie `Secure` (`D08-visitors-and-trail`) and the sign-in and site addresses `https` ones (`D06-pages`), and an `X-Request-Id`, which sites records as each event's request id (`D03-serve`, R-XPM2-VTHM), so auth's check event and sites' events of one request share it.

## The credential

On a space every request sites receives has been admitted by auth, and nginx passes the client's `Authorization` header on to sites untouched: an agent's or curl's `Bearer` token, or git's `Basic` credential should one arrive on sites' host. sites never needs the header — the caller is appkit identity's `identity.Caller`, from the identity headers nginx sets — and the stories promise that the token's secret is in no record any of them leaves: not in the trail, and not in nginx's logs. The trail is sites' to keep clean, and so is everything sites keeps on disk; nginx's logs are opsctl's.

A request carries a **credential header** when its `Authorization` header is `Bearer <p>`, or `Basic ` and the standard base64 of `<u>:<p>`, the test choosing `<u>` and `<p>` long, with an uppercase letter in every 8-character stretch of each and of their base64 value so that no id, sha, visitor id or number sites mints can match them, and occurring nowhere else in what it sends or stores. Its **secret strings** are every run of 8 or more consecutive characters of `<p>`, of `<u>` and of the base64 value, so a truncated or partial leak counts as a leak (R-DN5E-UIA0). Three guarantees follow. First, nothing sites records — no event's name, envelope or attribute — holds a secret string, whatever the request was: a page, a shared file, a site path, a rebuild, an apex redirect, a tool call (R-DODB-8A0P). Each event's exact keys are already fixed by the documents that own them (`D03-serve` for the request pair and the service events, `D08-visitors-and-trail` for every `site.*` event, appkit's MCP tools design for `tool.called`), and none of them reads the header, so this is the end-to-end statement of what those already imply, proved through `Run` with a capturing sink. Second, nothing `Run` writes to `Stderr` holds one either, including the `sites: undelivered event:` lines that are the journal's copy of the trail when telemetry is away (R-796D-SRTD). Third, nothing sites leaves on disk holds one: after a run that served such requests, no file under its working directory — the catalog and its SQLite sidecar files, and every unpacked tree — contains a secret string (R-DPL7-M1RE).

## In a sandbox

In a sandbox sites is the same binary in the same arrangement: the sandbox builds it from the worktree, runs it under its own units with a working directory of its own (`<data>/apps/sites`), gives it the environment of a host — `DRAIN_SECONDS`, `IKIGENBA_SERVICES` and the manifest's three settings — plus variables a host never sets, puts its own nginx in front, and routes through auth's `/check/open` and `/check` as a host does (sandbox's `S3-apps.md` and `S4-routing.md`). All of that is the sandbox's. sites' contributions are the ones it makes on a space:

- It serves under the sandbox like any app, at `http://sites.wip.localhost:7400`. Its catalog and trees are under its working directory (`D01-layout-and-run-seam`, R-2X5A-AH8Q, R-2YD6-O8ZF), so they survive `down` and `up` because the sandbox keeps that directory until `wipe`, and a catalog reopened is the one closed (`D04-store`, R-1FTJ-UKKK).
- `REPOS_DIR` at its default resolves against that working directory to `<data>/apps/sites/../repos/state/repos`, which is repos' own directory in the sandbox, so sites reads exactly the repositories repos holds with no setting changed (`D01-layout-and-run-seam`, R-YIEW-VDL9).
- Its site URLs are built from the sandbox's services file, whose `url` for sites is `http://sites.wip.localhost:7400` (`D06-pages`, R-CE8B-DKM7, R-CFG7-RCCW; `D09-tools`, R-N0JS-L7XX), and the requests arrive with `X-Forwarded-Proto: http`, so its visitor cookie carries no `Secure` (`D08-visitors-and-trail`, R-EVK1-Q25Q, R-EWRY-3TWF) and its sign-in redirect is to `http://auth.wip.localhost:7400/` (`D06-pages`, R-CKBT-AFBO).
- It runs the first `git` on the `PATH` it is given (`D03-serve`, R-VLOG-3TP1; `D05-git-and-cache`, R-CBY7-DQF0); that the sandbox's `PATH` holds the developer's own `git` is the sandbox's arrangement, a sandbox-level check.
- It reads nothing from its environment beyond what a host gives it, so the variables only the sandbox sets change nothing it does: the keys `Run` looks up are the closed list `D01-layout-and-run-seam` states (R-YPQB-601F), and `AGENTS.md`'s run-seam rule keeps sites' own code from reading the environment any other way. The readers of `IKIGENBA_SERVICES` that sit outside the seam are appkit's and named in `D01-layout-and-run-seam`.
- The same credential guarantees hold; `sandbox logs sites` showing no line is `D03-serve`'s quiet standard error (R-XN6A-4A08).

## The recorded deviation

One outcome of the stories is not met, by decision: the `tools/list` entry of `apex` carries no `outputSchema` (U1). `S05-mcp.md` lists `apex` with an output schema describing `apex` as either null or a site object. appkit's schema rule has no nullable form, so a typed `apex` could never answer `{"apex":null}`, which every apex story requires; `apex` is therefore registered with `mcp.AddRawTool`, which writes no `outputSchema`, and its handler builds the result by hand (`D09-tools`, prose and R-M8I3-SHVY). Every result `apex` gives is the one the stories show; only the listing differs. Keeping the results right is worth more than the schema line; the departure ends when appkit's schema rule gains a nullable form.

## What only a space can show

These outcomes are space-level checks: no test of sites can make them, and they are verified by running the stories on a space or in a sandbox. They are recorded here, not required.

- The release file holds exactly `bin/sites`, `etc/manifest.toml` and `share/icon.svg`, `bin/sites` is a static `linux/amd64` executable (`AGENTS.md`'s release build gate proves the build, devctl's release build makes the file), the icon is an SVG, the version is in the file's name and nowhere in a member's path, and `space status` reports the version the binary prints (devctl).
- `systemctl start` exits silently, a start sites refuses is reported by `systemctl` as a failed start while `ikigenba-sites.socket` stays up and keeps accepting connections into its queue, the unit runs as `ikigenba` under `[resources]`, the socket outlives a restart so that no request is refused across one, `etc/env` holds the three settings beside `DRAIN_SECONDS` and `IKIGENBA_SERVICES`, and the database is kept and replicated across releases while `cache/` is not backed up (opsctl).
- A guest reaches sites' paths with no identity headers, a refused credential is answered `403`, and a `POST /mcp` with no credential is answered `401` with `www-authenticate: Bearer realm="ikigenba"`, in each refused case nothing reaching sites' socket and sites recording no event (nginx and auth, opsctl's `S5-nginx.md`, auth's `S4-check.md`; in a sandbox, the sandbox's nginx). In a sandbox, auth's `/check/open` receives the `X-Original-*` headers for every request to sites outside `/mcp`, and no subrequest goes to `/check` for it (the sandbox's nginx, auth).
- auth's own records of each request (`check.allowed`, `check.refused`) and their sharing a request id with sites' events (auth, nginx).
- The host's nginx log holds no token: its format is opsctl's, and sites neither writes it nor affects it.
- The answers arrive over HTTP/2 and TLS on a space and over HTTP/1.1 in a sandbox (nginx).
- The apex host reaches sites at all: the root domain's record, the certificate and the server block are devctl's and opsctl's (`devctl apex set`).
- The gateway offers sites' seven tools, runs `create` and `publish` through `mutate`, and relays sites' results under its own `serverInfo` (mcp's `S09-mutate.md` and `S11-on-a-space.md`).
- In a sandbox, repos makes the repository an agent pushes to, at `<data>/apps/repos/state/repos/<rep>.git`, and records `repo.created` and `repo.pushed` (repos, the sandbox).
- The launcher a browser opens lists sites with its icon, marked as the current page, and the page's style is fetched from sites' own host and from no other origin (appkit's script; no test runs it).
- The `PATH` the sandbox gives sites holds the developer's own `git` (the sandbox).

## Story to design

Every outcome the story inventory lists, by its row label, with the design documents and requirements that cover it. A row a test of sites cannot prove in whole is marked "space-level check", naming who proves the rest; sites' own part, where it has one, is cited beside it. A requirement cited for a group is one that carries part of it; the owner document holds the rest.

| Rows | Outcome | Design and requirements |
|---|---|---|
| S01-1 | commands touch nothing; no command means serve | `D02-cli` R-SYVX-O0XD, R-SU0C-4XYL |
| S01-2 | binary path, working directory, environment file; same binary from the checkout | `D01-layout-and-run-seam` R-YVTT-2UQW, R-2X5A-AH8Q, R-2YD6-O8ZF; the paths `/opt/sites` and `etc/env`: space-level check (opsctl) |
| S01-3 | `--version` | `D02-cli` R-SQCM-ZMQI; `D01-layout-and-run-seam` R-XV8T-LQI2, R-XXOM-D9ZG |
| S01-4 | one version on footer, about, `serverInfo`, `service.started` | `D01-layout-and-run-seam` R-YZHI-85YZ, R-Z0PE-LXPO, R-Z1XA-ZPGD; `D03-serve` R-W8UJ-DGS8 |
| S01-5, S01-6 | `manifest` text; committed `etc/manifest.toml` identical; no port, no `cache/` | `D02-cli` R-SJ18-P0AC, R-KKIR-A82X, R-SRKJ-DEH7, R-SP4Q-LUZT; `D01-layout-and-run-seam` R-XST0-U70O |
| S01-7 | one description for manifest, about screen, MCP instructions | `D02-cli` R-SMOX-UBIF; `D06-pages` R-CNZI-FQJR, R-EK9V-UW2N; `D01-layout-and-run-seam` R-Z357-DH72 |
| S01-8 | `--help` | `D02-cli` R-SHTC-B8JN, R-SSSF-R67W |
| S01-9, S01-10 | unknown command and option | `D02-cli` R-SU0C-4XYL, R-SWG4-WHFZ, R-SXO1-A96O, R-5FW3-M580, R-SK95-2S11 |
| S02-0 | host units, user, socket path | space-level check (opsctl); sites' part `D03-serve` R-VI0Q-YIGY, R-W7MM-ZP1J |
| S02-1, S02-32, S02-33 | socket activation and its refusals | `D03-serve` R-VBX9-1NRH, R-VD55-FFI6, R-VFKY-6YZK, R-VI0Q-YIGY, R-VGSU-KQQ9 |
| S02-2 | `READY=1` | `D03-serve` R-VSZU-EG57, R-XLYD-QI9J |
| S02-3, S02-4, S02-25, S02-34, S02-35, S02-36 | settings, defaults, refusals and their order | `D01-layout-and-run-seam` R-Y500-NWFM, R-Y67X-1O6B, R-Y7FT-FFX0, R-Y8NP-T7NP, R-YB3I-KR53, R-YCBE-YIVS, R-YIEW-VDL9; `D03-serve` R-V9HG-A4A3, R-VAPC-NW0S, R-W0B8-P2LD; `D10-limits` R-ZZFM-ZVIU |
| S02-5, S02-17 | nothing written under `REPOS_DIR`; no sibling called | `D05-git-and-cache` R-TNVA-VCWI, R-TP37-94N7; `D09-tools` R-NBIW-15M6; `D03-serve` R-XBVB-M4IG |
| S02-6 | `IKIGENBA_SERVICES` read once, file re-read on need | `D01-layout-and-run-seam` R-YM2M-0OTC; `D03-serve` R-XMTE-HNSH; `D06-pages` R-CD0E-ZSVI |
| S02-7, S02-8 | start order; start reads no repository | `D03-serve` R-VAPC-NW0S, R-VKGJ-Q1YC, R-VMWC-HLFQ, R-VO48-VD6F, R-XTWE-ZGKT, R-VPC5-94X4 |
| S02-9 | sole writer of the catalog; `cache/` not backed up | `D02-cli` R-SJ18-P0AC (`[database]`); sole writer, backup and replication, and `cache/` not backed up: space-level check (opsctl) |
| S02-10 | event envelope | `D03-serve` R-XQ8P-U5CQ, R-XRGM-7X3F; `D08-visitors-and-trail` R-FR9F-O3FS, R-FA6U-BB22 |
| S02-11 | no names, content or credential in attributes | `D08-visitors-and-trail` R-UH9Z-UHBP, R-LON3-52D8, R-FGAC-85RJ, R-FHI8-LXI8, R-FIQ4-ZP8X, R-FJY1-DGZM, R-RFVG-0T9A; this design R-DODB-8A0P |
| S02-12, S02-31 | delivery to telemetry; journal while it is away | `D01-layout-and-run-seam` R-YULW-P307, R-Z4D3-R8XR; `D03-serve` R-WBAC-509M, R-X87M-GTAD; appkit's telemetry writer design |
| S02-13 | closed event list | `D08-visitors-and-trail` R-FQ1J-ABP3 |
| S02-14 | order of a request's events | `D03-serve` R-X244-JYKW; `D08-visitors-and-trail` R-RENJ-N1IL |
| S02-15 | what stderr and stdout carry | `D03-serve` R-XN6A-4A08, R-WEY1-ABHP, R-WG5X-O38E; `D05-git-and-cache` R-TMNE-HL5T |
| S02-16 | trusted headers; generated request id | `D03-serve` R-XPM2-VTHM, R-WPX4-Q95Y; `D07-site-serving` R-EH52-T4AD |
| S02-18 | first start on a host | `D01-layout-and-run-seam` R-YY9L-UE8A; `D03-serve` R-W8UJ-DGS8, R-XTWE-ZGKT; `systemctl` itself: space-level check (opsctl) |
| S02-19 | first start creates the catalog and `cache/sites/` | `D03-serve` R-VPC5-94X4; `D04-store` R-1ELN-GSTV |
| S02-20 | no services file | `D03-serve` R-XMTE-HNSH, R-WBAC-509M; `D06-pages` R-CE8B-DKM7; `D01-layout-and-run-seam` R-R02K-R24N |
| S02-new-1 | a guest's sign-in redirect the same with no services file | `D06-pages` R-CKBT-AFBO, R-DG17-8GLQ; `D07-site-serving` R-W6XT-ITGV |
| S02-21, S02-22, S02-23, S02-24 | start refusals: catalog, cache, git | `D03-serve` R-VMWC-HLFQ, R-VO48-VD6F, R-VKGJ-Q1YC, R-V6ZN-HU9K; `D04-store` R-1H1G-8CB9, R-1JH8-ZVSN; `D05-git-and-cache` R-CBY7-DQF0, R-CD63-RI5P |
| S02-new-2 | a failed start leaves the socket up | `D03-serve` R-W7MM-ZP1J; `systemctl` reporting the failure: space-level check (opsctl) |
| S02-26, S02-27, S02-28, S02-29 | stopping, drain, cut-off, 503 while stopping | `D03-serve` R-UZQ9-7YCJ, R-V0Y5-LQ38, R-LR2V-WLUM, R-SCP2-3J8G, R-W1J5-2UC2, R-W2R1-GM2R, R-WA2F-R8IX, R-WCI8-IS0B, R-V5RR-42IV, R-W7MM-ZP1J, R-WILQ-FMPS, R-BH93-MQG1; `D09-tools` R-XVCP-61ZC, R-LUQL-1X2P; `D01-layout-and-run-seam` R-YX1P-GMHL; `D07-site-serving` R-F6QY-UAUY, R-W5PX-51Q6; `D05-git-and-cache` R-CPD3-L7KN, R-GF0D-G2J5; `D08-visitors-and-trail` R-LM7A-DIVU; `D10-limits` R-TT42-AO7S |
| S02-30 | restart | `D03-serve` R-V0Y5-LQ38, R-W7MM-ZP1J, R-W8UJ-DGS8; `D04-store` R-1FTJ-UKKK; no request refused across the restart: space-level check (opsctl's socket unit) |
| S03-1, S03-9 | pages from templates, stylesheet, viewport, no script, footer | `D06-pages` R-D1EE-N7PE, R-D2MB-0ZG3, R-DX3S-L8ZG, R-DZJL-CSGU, R-E0RH-QK7J; `D01-layout-and-run-seam` R-YZHI-85YZ |
| S03-2 | who is a guest | `D03-serve` R-WPX4-Q95Y; `D06-pages` R-XWC7-R027; `D07-site-serving` R-EH52-T4AD |
| S03-3, S03-20 | services file read per request; a broken one is none | `D06-pages` R-CD0E-ZSVI, R-CE8B-DKM7; `D03-serve` R-XMTE-HNSH |
| S03-4, S03-5, S03-22 | banner, profile and sign-out addresses | `D06-pages` R-D7HW-K2EV, R-DJOW-DRTT, R-DX3S-L8ZG, R-CBSI-M14T, R-CHW0-IVUA, R-CJ3W-WNKZ, R-E1ZE-4BY8 |
| S03-6, S03-23 | the sites address and site URLs | `D06-pages` R-CE8B-DKM7, R-CFG7-RCCW, R-E6UZ-NEX0 |
| S03-7, S03-14, S03-15 | the landing table, its rows and badges, the empty state | `D06-pages` R-XXK4-4RSW, R-E82W-16NP, R-E9AS-EYEE, R-EAIO-SQ53, R-EBQL-6HVS, R-EE6D-Y1D6, R-EJ1Z-H4BY; `D04-store` R-1WW5-7CYA |
| S03-8, S03-19, S03-21 | the launcher | `D06-pages` R-E37A-I3OX, R-YY7J-PU0W; what the script does in a browser: space-level check (appkit's script; no test runs it) |
| S03-10 | routing | `D03-serve` R-WL1J-7676, R-WNHB-YPOK, R-WOP8-CHF9, R-WUSQ-9C4Q, R-WW0M-N3VF, R-WX8J-0VM4, R-WYGF-ENCT |
| S03-11, S03-13 | a page's events; quiet | `D03-serve` R-X244-JYKW, R-XPM2-VTHM, R-XN6A-4A08; `D08-visitors-and-trail` R-RDFN-99RW |
| S03-12 | the landing page | `D06-pages` R-XYS0-IJJL, R-DYBO-Z0Q5, R-E4F6-VVFM, R-E5N3-9N6B, R-E6UZ-NEX0, R-EFEA-BT3V, R-EGM6-PKUK, R-EHU3-3CL9 |
| S03-16 | `HEAD` of a page | `D06-pages` R-DDLE-GX4C, R-IBAF-6046 |
| S03-17 | the about screen | `D06-pages` R-DCDI-35DN, R-EK9V-UW2N, R-ELHS-8NTC; `D01-layout-and-run-seam` R-Z0PE-LXPO; `D08-visitors-and-trail` R-RDFN-99RW |
| S03-18 | a guest sent to sign in | `D06-pages` R-DG17-8GLQ, R-CKBT-AFBO; `D08-visitors-and-trail` R-RDFN-99RW |
| S03-24 | the catalog unreachable | `D03-serve` R-XP0T-GDM1, R-X0W8-66U7; `D06-pages` R-DKGW-ZMTK |
| S03-25 | other methods on a page | `D06-pages` R-DETA-UOV1; `D08-visitors-and-trail` R-RDFN-99RW |
| S04-1, S04-4, S04-6, S04-8, S04-9, S04-10, S04-11, S04-12, S04-13 | the seven shared files, their types, tags, revalidation and `HEAD` | `D06-pages` R-QGSS-CYWW, R-YUJU-KIST, R-RH3C-EKZZ, R-DMWP-R6AY, R-F04K-TWPO, R-W6K9-QDFF, R-B6BE-NEUY, R-YWZN-C2A7 |
| S04-2, S04-14, S04-15 | names not served; other methods | `D06-pages` R-F506-CZOG, R-F682-QRF5; `D03-serve` R-WNHB-YPOK, R-WW0M-N3VF; `D08-visitors-and-trail` R-RDFN-99RW |
| S04-3, S04-5, S04-7 | same to guests; no cookie; only the request pair | `D03-serve` R-WW0M-N3VF, R-X244-JYKW; `D06-pages` R-RH3C-EKZZ; `D08-visitors-and-trail` R-RDFN-99RW |
| S04-new1 | the apex host, whatever the path | `D03-serve` R-WL1J-7676, R-WOP8-CHF9 |
| S05-1, S05-23 | `/mcp` exactly, `POST` only | `D03-serve` R-WNHB-YPOK, R-WR51-40WN; the `405` and one answer per `POST`: appkit's MCP server design |
| S05-2, S05-16, S05-17, S05-22 | the seven tools listed: names, descriptions, schemas, annotations | `D09-tools` R-M8I3-SHVY, R-MWW3-FWPU, R-MAXW-K1DC, R-MC5S-XT41, R-MDDP-BKUQ, R-MELL-PCLF, R-MFTI-34C4, R-MH1E-GW2T, R-MI9A-UNTI, R-MJH7-8FK7, R-MKP3-M7AW, R-MLWZ-ZZ1L, R-MN4W-DQSA, R-MOCS-RIIZ, R-MPKP-5A9O, R-MQSL-J20D, R-MS0H-WTR2, R-MUGA-OD8G, R-MVO7-24Z5; `apex`'s `outputSchema` departs (U1, above) |
| S05-3 | site ids and names | `D04-store` R-1AXY-BHLS, R-1C5U-P9CH, R-1LX1-RFA1, R-UEZD-QS32 |
| S05-4 | a name resolves among the caller's sites | `D09-tools` R-99D9-RDYQ; `D04-store` R-1T8G-21Q7 |
| S05-5, S05-9 | the site object; success and failure shapes | `D09-tools` R-N0JS-L7XX, R-FC07-11HE |
| S05-6, S05-14 | kinds; `tool.called` | `D09-tools` R-MAXW-K1DC, R-XVCP-61ZC |
| S05-7, S05-24 | identity required on `/mcp`; `Host: sites` never apex | `D03-serve` R-WPX4-Q95Y, R-WL1J-7676 |
| S05-8, S05-21 | protocol revisions, `initialize`, `serverInfo` | appkit's MCP server design; the version `D01-layout-and-run-seam` R-Z1XA-ZPGD |
| S05-10 | argument refusals | appkit's MCP tools and schema designs; the input schemas `D09-tools` R-MKP3-M7AW to R-MQSL-J20D |
| S05-11 | sites' refusal texts | `D09-tools` R-N5FE-4AWP, R-Y1G7-2WOT, R-N2ZL-CRFB, R-N47H-QJ60 |
| S05-12, S05-26 | catalog unreachable in a tool | `D09-tools` R-QTZ2-U7F6, R-9HWK-FS5L; `D04-store` R-116R-9BO8 |
| S05-13 | a refusal changes nothing, records no `site.*` | `D09-tools` R-9FGR-O8O7; `D08-visitors-and-trail` R-FOTM-WJYE |
| S05-15 | MCP answers quiet | `D03-serve` R-XN6A-4A08 |
| S05-18 | `tools/list` reads nothing | `D09-tools` R-MWW3-FWPU; `D03-serve` R-X244-JYKW |
| S05-19, S05-20 | `server/discover` and instructions | `D01-layout-and-run-seam` R-Z357-DH72, R-R02K-R24N |
| S05-25 | unknown tool | `D09-tools` R-9BT2-IXG4 |
| S05-27 | `list` through `Host: sites` | `D09-tools` R-NIUA-BS2C; `D06-pages` R-CE8B-DKM7 |
| S06-1, S06-24, S06-25 | `create`'s input and its argument refusals | `D09-tools` R-LYQW-QBYE, R-MN4W-DQSA, R-XVCP-61ZC; appkit's MCP schema design |
| S06-2, S06-3, S06-18, S06-19 | the name rule; names taken space-wide | `D04-store` R-1C5U-P9CH, R-UEZD-QS32, R-1Y41-L4OZ; `D09-tools` R-N5FE-4AWP, R-LKDI-8SVM, R-PO7P-9OAU |
| S06-4, S06-21, S06-22 | the repository and its owner | `D05-git-and-cache` R-AXFL-X2LH, R-B4R0-7P1N, R-B5YW-LGSC; `D09-tools` R-L86I-F3GO |
| S06-5, S06-13, S06-23 | `ref` default, rule, not resolved | `D05-git-and-cache` R-AZVE-OM2V; `D09-tools` R-LVCL-OQJV, R-NRDL-0697 |
| S06-6, S06-7, S06-8, S06-9, S06-12, S06-14, S06-15 | defaults, slug, check order, the created site | `D09-tools` R-LKDI-8SVM, R-LVCL-OQJV, R-N0JS-L7XX; `D04-store` R-1KP5-DNJC, R-1LX1-RFA1; `D01-layout-and-run-seam` R-4AOP-838L; `D07-site-serving` R-ETC2-MTPB |
| S06-10 | `site.created` | `D08-visitors-and-trail` R-FGAC-85RJ |
| S06-11 | the shared fixture catalog | no requirement: every test builds its own (`AGENTS.md`); its shapes are `D04-store`'s and `D05-git-and-cache`'s |
| S06-16, S06-17, S06-20 | taken name; visibility refused | `D09-tools` R-PO7P-9OAU, R-N5FE-4AWP, R-9FGR-O8O7 |
| S07-1, S07-2, S07-3, S07-6, S07-8, S07-10, S07-11 | `list` and `show` results | `D09-tools` R-NIUA-BS2C, R-NK26-PJT1, R-9J4G-TJWA, R-M4UE-N6NV, R-M62B-0YEK, R-N0JS-L7XX, R-LVYH-FOTE; `D04-store` R-1VO8-TL7L |
| S07-4, S07-12, S07-13 | no site named | `D09-tools` R-99D9-RDYQ, R-NK26-PJT1 |
| S07-5, S07-7 | their events | `D09-tools` R-XVCP-61ZC; `D08-visitors-and-trail` R-FOTM-WJYE |
| S07-9, S07-14 | argument refusals | `D09-tools` R-MKP3-M7AW, R-MLWZ-ZZ1L, R-XVCP-61ZC; appkit's MCP schema design |
| S07-new1 | an unpublished site's URL not found until it is published | `D07-site-serving` R-Y3NM-1MID |
| S08-1, S08-6, S08-7, S08-12, S08-13, S08-14, S08-16 | `publish`: ref, result, re-publish | `D09-tools` R-LZYT-43P3, R-9KCD-7BMZ, R-LQH0-5NL3, R-XJWW-Q6D5, R-LZ0A-U1RY; `D04-store` R-1ZBX-YWFO; `D05-git-and-cache` R-B76S-Z8J1 |
| S08-2, S08-3, S08-15 | resolve, unpack, swap atomically, prune | `D09-tools` R-LFHW-PPWU, R-0ZRS-ZOAG; `D05-git-and-cache` R-TP37-94N7, R-BC2E-IBHT, R-BDAA-W38I, R-CQKZ-YZBC; `D04-store` R-21RQ-QFX2; `D07-site-serving` R-FBMK-DDTQ |
| S08-4, S08-19, S08-20 | size and time limits | `D05-git-and-cache` R-CKHI-24LV, R-CO57-7FTY; `D09-tools` R-LBU7-KEOR, R-N47H-QJ60; `D10-limits` R-ZZFM-ZVIU, R-YAZ0-C8YJ |
| S08-5 | owner not re-checked | `D09-tools` R-NXH2-X0YO |
| S08-8, S08-9, S08-17, S08-18, S08-21 | failures and their texts | `D09-tools` R-9KCD-7BMZ, R-N2ZL-CRFB, R-9FGR-O8O7, R-LBU7-KEOR, R-LO17-E43P, R-0ZRS-ZOAG; `D05-git-and-cache` R-B8EP-D09Q, R-XHH3-YMVR |
| S08-10, S08-11 | `site.published`; none on refusal | `D08-visitors-and-trail` R-FHI8-LXI8, R-FOTM-WJYE, R-ZWY1-7AE7 |
| S08-22, S08-23, S08-24 | missing site; argument refusal | `D09-tools` R-9KCD-7BMZ, R-MOCS-RIIZ, R-XVCP-61ZC, R-LVYH-FOTE |
| S08-new1 | publishing one site leaves every other site and the apex as they were | `D09-tools` R-LQH0-5NL3, R-XJWW-Q6D5 |
| S09-1, S09-2, S09-3, S09-4, S09-12, S09-13, S09-14, S09-15, S09-16, S09-17, S09-18 | `update`'s input, rules and refusals | `D09-tools` R-M16P-HVFS, R-MPKP-5A9O, R-NYOZ-ASPD, R-N5FE-4AWP, R-9FGR-O8O7; `D04-store` R-247J-HZEG, R-2DYQ-K5C0 |
| S09-5, S09-7, S09-8, S09-9, S09-10, S09-11 | what an update changes and serves | `D09-tools` R-O14S-2C6R, R-O2CO-G3XG, R-LVYH-FOTE; `D04-store` R-22ZN-47NR; `D07-site-serving` R-EJKV-KNRR, R-W6XT-ITGV |
| S09-6 | `site.updated` | `D08-visitors-and-trail` R-FIQ4-ZP8X |
| S10-1, S10-2, S10-3, S10-4, S10-5, S10-7, S10-8 | `delete` and what it frees | `D09-tools` R-LXJ0-CK7P, R-QXMR-ZIN9, R-LZ0A-U1RY, R-MAXW-K1DC, R-LVYH-FOTE; `D04-store` R-25FF-VR55, R-27V8-NAMJ; `D05-git-and-cache` R-CRSW-CR21; `D07-site-serving` R-ETC2-MTPB, R-FIXY-O09W |
| S10-6 | `site.deleted`, then `site.apex` | `D08-visitors-and-trail` R-FJY1-DGZM |
| S10-9, S10-10, S10-11, S10-12 | refusals | `D09-tools` R-O3KK-TVO5, R-9FGR-O8O7, R-XVCP-61ZC |
| S11-1, S11-2, S11-3, S11-4 | public sites, routing and order of checks | `D07-site-serving` R-EQW9-VA7X, R-ETC2-MTPB, R-Y2FP-NURO, R-Y3NM-1MID, R-EY7O-5WO3; `D03-serve` R-WX8J-0VM4; `D02-cli` R-SJ18-P0AC (`guests = true`) |
| S11-5, S11-19, S11-22, S11-23, S11-24 | path resolution, slash redirects, dotfiles, `..`, symlinks | `D07-site-serving` R-EKSR-YFIG, R-F1VD-B7W6, R-F339-OZMV; `D03-serve` R-WYGF-ENCT |
| S11-6, S11-20, S11-21, S11-29, S11-30 | missing files, unknown slug, unpublished | `D07-site-serving` R-F339-OZMV, R-F4B6-2RDK, R-ETC2-MTPB, R-Y3NM-1MID, R-EPOD-HIH8; `D08-visitors-and-trail` R-RDFN-99RW |
| S11-7, S11-13, S11-14, S11-15, S11-16, S11-17 | served files: bytes, types, length, tag | `D07-site-serving` R-EM0O-C795, R-EZFK-JOES, R-4YFR-E1U4 |
| S11-8, S11-9, S11-10, S11-31 | `Cache-Control`, cookie, `site.viewed` | `D07-site-serving` R-FAEN-ZM31, R-EJKV-KNRR; `D08-visitors-and-trail` R-1DZ9-ITG2, R-UEU7-2XUB, R-LON3-52D8 |
| S11-11 | the not-found page | `D06-pages` R-EQDD-RQS4, R-ENXL-07AQ, R-EP5H-DZ1F; `D07-site-serving` R-EPOD-HIH8 |
| S11-12 | serving reads no repository while the tree exists | `D07-site-serving` R-Y4VI-FE92, R-FFA9-IP1T |
| S11-18 | `/<slug>` redirected | `D07-site-serving` R-Y2FP-NURO |
| S11-25, S11-26 | `If-None-Match` and `304` | `D07-site-serving` R-EOGH-3QQJ, R-F0NG-XG5H, R-EZFK-JOES |
| S11-27 | `HEAD` | `D07-site-serving` R-4ZNN-RTKT |
| S11-28 | other methods | `D07-site-serving` R-EQW9-VA7X; `D08-visitors-and-trail` R-RDFN-99RW |
| S11-32 | catalog unreachable | `D03-serve` R-XP0T-GDM1; `D08-visitors-and-trail` R-RDFN-99RW |
| S11-N1 | every event's request id is nginx's `X-Request-Id` | `D03-serve` R-XPM2-VTHM, R-XRGM-7X3F, R-XQH3-MZ0K; `D08-visitors-and-trail` R-FA6U-BB22 |
| S12-1, S12-2, S12-3, S12-4, S12-7, S12-8, S12-9, S12-10, S12-11 | a guest at a private site sent to sign in | `D07-site-serving` R-W6XT-ITGV; `D06-pages` R-CKBT-AFBO, R-CHW0-IVUA, R-CBSI-M14T; `D08-visitors-and-trail` R-RDFN-99RW |
| S12-5, S12-12, S12-13, S12-14, S12-15, S12-16 | a user served a private site | `D07-site-serving` R-EJKV-KNRR, R-FAEN-ZM31, R-EY7O-5WO3, R-Y3NM-1MID; `D08-visitors-and-trail` R-LON3-52D8, R-UIHW-892E |
| S12-6 | quiet | `D03-serve` R-XN6A-4A08 |
| S13-1 | the apex setting | `D04-store` R-18I5-JY4E, R-2AB1-EU3X, R-2BIX-SLUM, R-2CQU-6DLB, R-2DYQ-K5C0 |
| S13-2, S13-28 | what an apex request is; `Host: sites` is not | `D03-serve` R-WL1J-7676, R-WOP8-CHF9 |
| S13-3, S13-25, S13-26, S13-27, S13-29, S13-30 | the apex redirect | `D07-site-serving` R-FK5V-1S0L; `D06-pages` R-CGO4-543L; `D08-visitors-and-trail` R-RDFN-99RW |
| S13-4, S13-31 | apex unset | `D07-site-serving` R-FIXY-O09W; `D06-pages` R-EQDD-RQS4; `D08-visitors-and-trail` R-RDFN-99RW |
| S13-5 | no cookie, only the request pair | `D03-serve` R-X244-JYKW; `D08-visitors-and-trail` R-RDFN-99RW |
| S13-6, S13-7, S13-8, S13-9, S13-10, S13-11, S13-12, S13-14, S13-15, S13-16, S13-18, S13-20, S13-21, S13-23, S13-24 | the `apex` tool | `D09-tools` R-M2EL-VN6H, R-MQSL-J20D, R-MAXW-K1DC, R-O60D-LF5J, R-O789-Z6W8, R-O8G6-CYMX, R-O9O2-QQDM, R-NYOZ-ASPD, R-QXMR-ZIN9, R-XVCP-61ZC |
| S13-13, S13-17, S13-19, S13-22 | `site.apex`; the sites left as they were | `D08-visitors-and-trail` R-FMDU-50H0, R-FNLQ-IS7P, R-FOTM-WJYE; `D04-store` R-AOGI-TUZD; `D09-tools` R-WE97-TFX1 |
| S13-32 | quiet | `D03-serve` R-XN6A-4A08 |
| S13-N1 | a tool request with no `X-Request-Id` gets an id sites mints | `D03-serve` R-XPM2-VTHM |
| S13-N2 | the root domain routed to sites through an open gate, with no identity headers | space-level check (devctl, opsctl, nginx) |
| S14-1, S14-4, S14-6, S14-11 | the visitor id | `D08-visitors-and-trail` R-EQOG-6Z6Y, R-ERWC-KQXN, R-EUC5-CAF1, R-LON3-52D8, R-UJPS-M0T3; `D01-layout-and-run-seam` R-XYWI-R1Q5 (`Rand`) |
| S14-2, S14-3, S14-7, S14-10, S14-12, S14-13, S14-14 | when the cookie is set | `D08-visitors-and-trail` R-1DZ9-ITG2, R-UEU7-2XUB, R-UIHW-892E |
| S14-5, S14-8, S14-9 | the cookie's attributes and `Secure` | `D08-visitors-and-trail` R-EVK1-Q25Q, R-EWRY-3TWF |
| S14-15, S14-16, S14-17, S14-18 | answers that set no cookie | `D08-visitors-and-trail` R-RDFN-99RW; `D07-site-serving` R-W6XT-ITGV |
| S14-19 | quiet | `D03-serve` R-XN6A-4A08 |
| S15-1, S15-2 | envelope; nothing named in attributes | `D08-visitors-and-trail` R-FR9F-O3FS, R-FA6U-BB22, R-UH9Z-UHBP, R-RFVG-0T9A; `D03-serve` R-XQ8P-U5CQ; this design R-DODB-8A0P |
| S15-3, S15-4, S15-5 | service events, request pair, `tool.called` | `D03-serve` R-W8UJ-DGS8, R-WA2F-R8IX, R-X244-JYKW; `D09-tools` R-XVCP-61ZC; `D08-visitors-and-trail` R-FQ1J-ABP3 |
| S15-6, S15-10, S15-11, S15-12, S15-13, S15-14, S15-15 | `site.viewed` and its referrer | `D08-visitors-and-trail` R-LON3-52D8, R-EXZU-HLN4, R-ZZDT-YTVL |
| S15-7, S15-17, S15-18, S15-19, S15-20, S15-21, S15-22, S15-23 | tool `site.*` events | `D08-visitors-and-trail` R-FGAC-85RJ, R-FHI8-LXI8, R-FIQ4-ZP8X, R-FJY1-DGZM, R-FMDU-50H0, R-FNLQ-IS7P, R-FOTM-WJYE |
| S15-8, S15-24 | `site.unavailable` | `D08-visitors-and-trail` R-LPUZ-IU3X, R-HENR-6828, R-ZWY1-7AE7; `D05-git-and-cache` R-CWOH-VU0T |
| S15-9, S15-16 | per-request order; non-site requests | `D08-visitors-and-trail` R-RENJ-N1IL, R-RDFN-99RW; `D03-serve` R-X244-JYKW |
| S16-1, S16-9 | where trees live; default `REPOS_DIR` on a host | `D01-layout-and-run-seam` R-2YD6-O8ZF, R-YIEW-VDL9; `D05-git-and-cache` R-AW7P-JAUS |
| S16-2, S16-3, S16-4, S16-7, S16-10, S16-11, S16-18 | lazy, shared, atomic rebuild with no memory of failure | `D05-git-and-cache` R-CT0S-QISQ, R-CVGL-I2A4, R-BDAA-W38I, R-XHH3-YMVR, R-UQPP-5LCD, R-UT5H-X4TR; `D07-site-serving` R-Y4VI-FE92 |
| S16-5, S16-12, S16-13, S16-14, S16-15, S16-16, S16-17 | a failed rebuild: `503`, the page, the reason | `D07-site-serving` R-W4I0-R9ZH, R-W5PX-51Q6; `D06-pages` R-ERLA-5IIT; `D05-git-and-cache` R-CWOH-VU0T, R-AIST-BTP5, R-B9ML-QS0F, R-CKHI-24LV, R-CO57-7FTY |
| S16-6 | `site.viewed` then `site.unavailable` | `D08-visitors-and-trail` R-LPUZ-IU3X, R-HENR-6828 |
| S16-8 | git's stderr discarded; quiet | `D05-git-and-cache` R-TMNE-HL5T; `D03-serve` R-XN6A-4A08 |
| S16-19 | publish then serve | `D09-tools` R-LQH0-5NL3, R-XJWW-Q6D5; `D07-site-serving` R-FBMK-DDTQ |
| S16-20 | a guest at a private site never rebuilds | `D07-site-serving` R-W6XT-ITGV; `D08-visitors-and-trail` R-RDFN-99RW |
| S17-1 | limits read once at start | `D01-layout-and-run-seam` R-Y8NP-T7NP; `D10-limits` R-X6MD-VC6F |
| S17-2, S17-3, S17-5, S17-6, S17-11 | size and time limits on unpack and git runs | `D05-git-and-cache` R-CKHI-24LV, R-CO57-7FTY, R-7PSE-YBDY; `D10-limits` R-ZWZU-8C1G, R-ZZFM-ZVIU, R-YAZ0-C8YJ; `D07-site-serving` R-Y4VI-FE92 |
| S17-4, S17-7, S17-8, S17-9, S17-10, S17-12 | `publish` over and at the limits | `D09-tools` R-LBU7-KEOR, R-N47H-QJ60, R-LQH0-5NL3, R-9FGR-O8O7; `D10-limits` R-ZZFM-ZVIU; `D07-site-serving` R-FBMK-DDTQ |
| S17-13 | quiet | `D03-serve` R-XN6A-4A08 |
| S18-1, S18-2, S18-4, S18-15 | repositories read only through git, never written | `D05-git-and-cache` R-TP37-94N7, R-TNVA-VCWI, R-AW7P-JAUS; `D09-tools` R-NBIW-15M6; `D03-serve` R-XBVB-M4IG |
| S18-3, S18-9 | `REPOS_DIR` resolution, read once | `D01-layout-and-run-seam` R-YIEW-VDL9, R-YKUP-MX2N, R-2ZL3-20Q4; `D05-git-and-cache` R-D0C7-158W |
| S18-5, S18-12 | a site names its repository by id | `D04-store` R-0WB5-Q8PG; `D09-tools` R-NXH2-X0YO |
| S18-6, S18-7, S18-16, S18-17 | what sites writes, and only under its directory | `D01-layout-and-run-seam` R-4BWL-LUZA, R-2X5A-AH8Q, R-2YD6-O8ZF; `D05-git-and-cache` R-BC2E-IBHT, R-CQKZ-YZBC, R-CRSW-CR21, R-CT0S-QISQ |
| S18-8, S18-10, S18-11, S18-13 | `create` and `publish` against the layout | `D09-tools` R-L86I-F3GO, R-LVCL-OQJV, R-9KCD-7BMZ; `D05-git-and-cache` R-B4R0-7P1N |
| S18-14 | a cached tree served with the repository gone | `D07-site-serving` R-Y4VI-FE92 |
| S18-new-1 | sites runs as the same `ikigenba` user as repos | space-level check (opsctl) |
| S19-1, S19-4 | the catalog is the only record; not rebuilt from disk | `D04-store` R-1FTJ-UKKK; restoring from the replica: space-level check (opsctl) |
| S19-2, S19-3, S19-5, S19-6 | start checks and what start leaves alone | `D03-serve` R-VKGJ-Q1YC, R-VMWC-HLFQ, R-VO48-VD6F, R-VPC5-94X4, R-XTWE-ZGKT, R-W8UJ-DGS8, R-XQH3-MZ0K |
| S19-7, S19-8, S19-9 | restored catalog, absent repositories | `D03-serve` R-XTWE-ZGKT, R-XQH3-MZ0K; `D01-layout-and-run-seam` R-2ZL3-20Q4; `D05-git-and-cache` R-CT0S-QISQ, R-UQPP-5LCD; `D07-site-serving` R-Y4VI-FE92, R-W4I0-R9ZH; `D09-tools` R-L86I-F3GO |
| S20-1 | no nginx fragment | `D02-cli` (prose: no nginx constant); `D01-layout-and-run-seam` R-XST0-U70O |
| S20-2, S20-5 | only the manifest under `etc/`; only the icon under `share/`; exactly three members | `D01-layout-and-run-seam` R-XST0-U70O; `D02-cli` R-SP4Q-LUZT; `share/` and the member list: space-level check (devctl) |
| S20-3 | templates and appkit's files inside the binary | `D01-layout-and-run-seam` R-XQD8-2NJA, R-XRL4-GF9Z; `D06-pages` R-D06I-9FYP, R-QGSS-CYWW |
| S20-4 | state made on first start; `[database]`, `[resources]`; version only in name and binary | `D03-serve` R-VPC5-94X4; `D02-cli` R-SJ18-P0AC; keeping, bounding and naming: space-level check (opsctl, devctl) |
| S20-6 | the binary's `--version` and `manifest` | `D02-cli` R-SQCM-ZMQI, R-SRKJ-DEH7, R-SJ18-P0AC |
| S20-new-1 | the binary static `linux/amd64` | the release build gate of `AGENTS.md`, not a requirement (What ships); the built file: space-level check (devctl) |
| S21-1 | the socket, the open gate, refused credentials, forwarded headers | space-level check (opsctl, nginx, auth); sites' part `D02-cli` R-SJ18-P0AC, `D03-serve` R-XPM2-VTHM, R-WX8J-0VM4 |
| S21-2 | private sites and sign-in are sites' | `D07-site-serving` R-W6XT-ITGV; `D06-pages` R-DG17-8GLQ |
| S21-3 | site URLs from the services file; tools through the gateway | `D06-pages` R-CE8B-DKM7, R-CFG7-RCCW; `D09-tools` R-N0JS-L7XX; the gateway: space-level check (mcp, opsctl) |
| S21-4 | host paths, `REPOS_DIR` on a host, replication, `etc/env` | `D01-layout-and-run-seam` R-2X5A-AH8Q, R-2YD6-O8ZF, R-YIEW-VDL9; `D02-cli` R-SJ18-P0AC; replication and `etc/env`: space-level check (opsctl) |
| S21-5 | the token in no record | this design R-DN5E-UIA0, R-DODB-8A0P, R-796D-SRTD, R-DPL7-M1RE; nginx's logs: space-level check (opsctl) |
| S21-6 | a guest at a public site on a space | `D07-site-serving` R-EZFK-JOES, R-F0NG-XG5H, R-FAEN-ZM31; `D08-visitors-and-trail` R-UEU7-2XUB, R-UIHW-892E, R-EVK1-Q25Q, R-LON3-52D8; `D05-git-and-cache` R-CT0S-QISQ; HTTP/2, TLS and auth's `check.allowed`: space-level check (nginx, auth) |
| S21-7 | a guest at a private site on a space | `D07-site-serving` R-W6XT-ITGV; `D06-pages` R-CKBT-AFBO; `D08-visitors-and-trail` R-RDFN-99RW; `D03-serve` R-X244-JYKW |
| S21-8 | a token holder at a private site | `D07-site-serving` R-EJKV-KNRR, R-FAEN-ZM31; `D08-visitors-and-trail` R-LON3-52D8, R-FA6U-BB22; auth admitting the token: space-level check (auth) |
| S21-9 | the landing page on a space | `D06-pages` R-XYS0-IJJL, R-EHU3-3CL9, R-E9AS-EYEE, R-EFEA-BT3V, R-EGM6-PKUK, R-E37A-I3OX, R-DZJL-CSGU, R-CJ3W-WNKZ, R-DX3S-L8ZG; `D01-layout-and-run-seam` R-YZHI-85YZ; `space status` and a browser's fetches: space-level check (devctl, appkit's script) |
| S21-10 | a guest at the landing page on a space | `D06-pages` R-DG17-8GLQ, R-CKBT-AFBO; `D08-visitors-and-trail` R-RDFN-99RW |
| S21-11 | the apex host on a space | `D07-site-serving` R-FK5V-1S0L, R-W85P-WL7K; `D06-pages` R-CGO4-543L; `D03-serve` R-WL1J-7676, R-WOP8-CHF9; the root domain reaching sites: space-level check (devctl, opsctl); `D08-visitors-and-trail` R-RDFN-99RW |
| S21-12 | `/mcp` with no credential | space-level check (nginx, auth): nothing reaches sites |
| S21-13 | an agent creates and publishes through the gateway | `D09-tools` R-LVCL-OQJV, R-LQH0-5NL3, R-XJWW-Q6D5; `D08-visitors-and-trail` R-FGAC-85RJ, R-FHI8-LXI8; `D06-pages` R-EBQL-6HVS, R-EE6D-Y1D6; `D03-serve` R-XN6A-4A08; `mutate` and the relay: space-level check (mcp) |
| S21-new-1 | relayed results carry the gateway's `serverInfo`, not sites' | space-level check (mcp) |
| S22-1 | sandbox paths; `REPOS_DIR` reaches repos' directory; kept until `wipe` | `D01-layout-and-run-seam` R-2X5A-AH8Q, R-2YD6-O8ZF, R-YIEW-VDL9; `D04-store` R-1FTJ-UKKK; the data directory: space-level check (the sandbox) |
| S22-2 | the sandbox's environment; extra variables ignored; git from `PATH` | `D01-layout-and-run-seam` R-YPQB-601F; `D03-serve` R-VLOG-3TP1; which `git` is on the `PATH`: space-level check (the sandbox) |
| S22-3 | sandbox site URLs | `D06-pages` R-CE8B-DKM7; `D09-tools` R-N0JS-L7XX |
| S22-4 | plain HTTP: `http` addresses, no `Secure`; strict `/mcp` | `D08-visitors-and-trail` R-EVK1-Q25Q, R-EWRY-3TWF; `D06-pages` R-CBSI-M14T; `/mcp`'s check: space-level check (the sandbox) |
| S22-5 | the token in no record | this design R-DN5E-UIA0, R-DODB-8A0P, R-796D-SRTD, R-DPL7-M1RE |
| S22-6 | the landing page in a sandbox, no sites yet | `D06-pages` R-XYS0-IJJL, R-EHU3-3CL9, R-E82W-16NP, R-E37A-I3OX |
| S22-7 | an agent publishes from a repository it pushed | `D09-tools` R-LVCL-OQJV, R-LQH0-5NL3; `D05-git-and-cache` R-BC2E-IBHT; `D08-visitors-and-trail` R-FGAC-85RJ, R-FHI8-LXI8; `D03-serve` R-XN6A-4A08; repos and the gateway: space-level check (repos, mcp, the sandbox) |
| S22-8 | a guest at the published site in a sandbox | `D07-site-serving` R-EZFK-JOES, R-Y2FP-NURO, R-FAEN-ZM31; `D08-visitors-and-trail` R-UEU7-2XUB, R-EWRY-3TWF, R-LON3-52D8 |
| S22-9 | a guest at a private site in a sandbox | `D09-tools` R-O14S-2C6R; `D07-site-serving` R-W6XT-ITGV; `D06-pages` R-CKBT-AFBO, R-CHW0-IVUA; `D08-visitors-and-trail` R-RDFN-99RW |
| S22-new-1 | auth's `/check/open` given the `X-Original-*` headers; no `/check` subrequest | space-level check (the sandbox's nginx, auth) |
| S22-new-2 | answers over HTTP/1.1 through the sandbox's nginx | space-level check (the sandbox's nginx) |

## REQUIREMENTS

- R-DN5E-UIA0: sites' design defines a request as carrying a **credential header** when it has exactly one `Authorization` header whose value is either `Bearer <p>`, or `Basic ` followed by the standard base64 encoding (RFC 4648, with padding) of `<u>:<p>`, where `<u>` and `<p>` are each at least 16 ASCII letters and digits, and every run of 8 consecutive characters of `<u>`, of `<p>`, and of that base64 value holds at least one uppercase ASCII letter; defines that request's **secret strings** as every substring of 8 or more characters of `<p>`, of `<u>`, and of the base64 value; and requires that no secret string occur anywhere else in what the test hands sites — no other header, URL path, query or body of any request in the same test, no tool argument, no value `LookupEnv` or `Environ` gives, and no content, path, ref name, commit message or author of a repository fixture, nor any part of the services file; every requirement of sites' design that names a credential header or its secret strings MUST denote that.
- R-DODB-8A0P: When `Run` serves with a non-nil `Sink` requests that each carry a credential header and a non-empty `X-User-Id` — `GET /`, `GET /about`, `GET /_appkit/theme.css`, a `GET` of `/<slug>/` for a published public site and for a published private site, the latter once more after its tree has been removed so that it is rebuilt, a `GET` whose first segment is the slug of no site, an apex request, and an MCP request to `/mcp` calling each of the tools `list`, `show`, `create`, `publish`, `update`, `apex` and `delete` — and then stops, no event handed to that `Sink`'s `Deliver` MUST contain any secret string of any of those requests in its event name, its envelope `RequestID` or `User`, or the value of any of its attributes, a string attribute's text and the decimal form of a numeric attribute alike.
- R-796D-SRTD: When `Run` serves requests that carry credential headers, as R-DODB-8A0P describes, and then stops, nothing `Run` writes to `Stderr` from its call to its return MUST contain any secret string of any of those requests, whatever the `Sink`'s `Deliver` returns, so that a `Sink` whose `Deliver` always returns an error leaves no secret string on `Stderr` either.
- R-DPL7-M1RE: When `Run` has served requests carrying credential headers, as R-DODB-8A0P describes, and has returned, no regular file anywhere under `Dir`, and no file of the catalog `Database` names (its SQLite sidecar files, the names ending `-journal`, `-wal` and `-shm`, included), MUST contain any secret string of any of those requests in its bytes.
