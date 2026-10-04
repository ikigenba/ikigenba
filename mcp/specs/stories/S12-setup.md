# Stories — setup

Connecting an agent's client to the gateway without the token ever passing through the chat. mcp serves two files to anyone, signed in or not: `/setup.txt`, plain-text instructions an agent reads and follows, and `/setup.sh`, the installer a user runs in their own terminal. The connect page points at the first (`S03`); the first tells the agent to have the user run the second. Both are the same for every caller and carry this deployment's addresses, filled in when they are asked for: `<mcp>` is `<scheme>://<host>`, where `<host>` and `<scheme>` are read from the request as the connect page reads them for its endpoint (`S03`), so the endpoint is `<mcp>/mcp`; and `<auth>` is `<auth-profile>` as the connect page derives it (`S03`), auth's profile page, where a user creates a token. For the suite's services file and `Host: mcp.sbx.ikigenba.dev` over `https`, `<mcp>` is `https://mcp.sbx.ikigenba.dev` and `<auth>` is `https://auth.sbx.ikigenba.dev/`. mcp reads the services file afresh for each request, as for the page. mcp serves guests (`S01`), so the host's nginx passes a request for either path to mcp whether or not it carries a credential, and a guest's arrives with no `X-User-Id` (`S03`); mcp answers the same either way, and records the request in its trail with the user it arrived with, empty for none (`S02`). `/setup.txt` and `/setup.sh` take `GET` and `HEAD`; a path beneath either, or either with a trailing `/`, is a path that does not exist (`S03`).

The installer is a Bash program for Linux, whose diagnostics begin `ikigenba-setup: `. It is run as `curl -fsSL <mcp>/setup.sh | bash -s -- --client <client> --scope <scope>`, so its standard input is the pipe; it reads every answer from the user, the token included, from the terminal, `/dev/tty`, and writes its prompts and the token's asterisks there. Its other lines go to stdout, and every diagnostic to stderr. `<client>` is one of `codex-cli`, `codex-desktop`, `claude-cli`, `claude-desktop`, and `grok-cli`, the clients' names in the installer's text being `Codex CLI`, `Codex desktop`, `Claude CLI`, `Claude desktop`, and `Grok CLI`. `<scope>` is `user`, the user's own configuration, or `project`, the configuration of the project in the current working directory. Each pair shares one configuration: the Codex CLI and Codex desktop read `config.toml` under `${CODEX_HOME:-~/.codex}` for `user` and `.codex/config.toml` in the project for `project`; the Claude CLI and the Code tab of Claude desktop read `~/.claude.json` for `user` and `.mcp.json` in the project for `project`; the Grok CLI reads `config.toml` under `${GROK_HOME:-~/.grok}` for `user` and `.grok/config.toml` in the project for `project`. The installer writes one entry, named `ikigenba`, which reaches `<mcp>/mcp` with the header `Authorization: Bearer <token>`, the token taken from the environment variable `IKIGENBA_TOKEN` and never written into the client's configuration:

```toml
[mcp_servers.ikigenba]
url = "https://mcp.sbx.ikigenba.dev/mcp"
bearer_token_env_var = "IKIGENBA_TOKEN"
```

```json
{ "type": "http", "url": "https://mcp.sbx.ikigenba.dev/mcp", "headers": { "Authorization": "Bearer ${IKIGENBA_TOKEN}" } }
```

```toml
[mcp_servers.ikigenba]
url = "https://mcp.sbx.ikigenba.dev/mcp"
enabled = true
headers = { "Authorization" = "Bearer ${IKIGENBA_TOKEN}" }
```

The first is Codex's, the second Claude's (under `mcpServers` in the file), the third Grok's. When the client's own command, `codex`, `claude`, or `grok`, is on the `PATH`, the installer has it write the entry with its own `mcp add`; otherwise it edits the file itself, adding or replacing the `ikigenba` entry and leaving everything else in the file as it was, and creating the file, and its directory, when there is none. The token is kept in one place, `~/.config/environment.d/ikigenba.conf`, mode `0600`, as the one line `IKIGENBA_TOKEN=<token>`, which the user's systemd manager reads at every login, so terminal and desktop apps alike see it; it is the same variable the connect page's `Git` section names (`S03`). The installer checks the token with the gateway before it writes anything, and writes nothing at all when it stops for any reason. It exits 0 when the client is set up, 1 when it stopped short, and 2 when it was run wrongly.

## An agent reads the setup instructions

An agent the user is talking to has been asked to read `<mcp>/setup.txt`, which the connect page tells the user to ask (`S03`). The file is written for the agent, not the user: it tells the agent what to work out, what to tell the user, and how to check the result, and an agent that follows it is the actor of the stories below that begin `An agent`. A signed-in caller gets the same file, and so does a caller on a host whose services file names no `auth`, whose `<auth>` is read from the `Host` as on the connect page (`S03`).

Request:

```
GET /setup.txt HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

```
GET /setup.txt HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
```

Status 200. The body is plain text, the same for both requests. It tells the agent: to set up only the client the user is talking to it through, and, when the agent runs on a machine other than the user's, to say so and stop; to work out the client and the OS from its own instructions, confirming the OS with a command where it can, and to ask the user only what it cannot tell, and which scope, `user` or `project`, when the user has not said; that the installer runs on Linux only, and on any other OS to say so and stop; the five client names the installer takes; to give the user the three-step message of `An agent on the user's machine sets up its own client`, with `<auth>` as the token page and the command `curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client <client> --scope <scope>`; never to ask for the token in the chat; once the user says the client is back, to check the connection with the gateway's `services` tool and one read-only `call`; and, for an agent with its own way to add a remote MCP server and take a secret outside the chat, as grokbot has, to use that way with the endpoint `https://mcp.sbx.ikigenba.dev/mcp` instead of the installer. The endpoint `https://mcp.sbx.ikigenba.dev/mcp` and the token page `https://auth.sbx.ikigenba.dev/` are written out in full.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file (`S03`).

Postconditions:

- Nothing has changed. No backend was contacted, and mcp set no cookie and wrote nothing to stderr.
- The trail holds two events for each request, the first under request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` and an empty user, the second under user `u_7f3a9c21`:

  ```
  request.started method=GET path=/setup.txt
  request.finished status=200
  ```

## A user's terminal fetches the installer

The command the agent gives the user fetches the installer and runs it. The file is the installer the stories below run, with this deployment's addresses in it, so it needs nothing from the user but the client, the scope, and the token.

Request:

```
GET /setup.sh HTTP/1.1
Host: mcp.sbx.ikigenba.dev
X-Forwarded-Proto: https
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
```

Status 200. The body is a Bash script, beginning `#!/usr/bin/env bash`, whose endpoint is `https://mcp.sbx.ikigenba.dev/mcp` and whose token page is `https://auth.sbx.ikigenba.dev/`. With `Host: mcp.sbx.ikigenba.dev:443` its endpoint is `https://mcp.sbx.ikigenba.dev:443/mcp`; with `X-Forwarded-Proto: http`, `http://mcp.sbx.ikigenba.dev/mcp`, and its token page `http://auth.sbx.ikigenba.dev/` when the services file names no `auth`.

Preconditions:

- mcp is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed. No backend was contacted.
- The trail holds two events for the request, under an empty user:

  ```
  request.started method=GET path=/setup.sh
  request.finished status=200
  ```

## A client asks for a setup file's headers

A `HEAD` is answered exactly as the `GET` would be, headers and status alike, with no body.

Request:

```
HEAD /setup.txt HTTP/1.1
Host: mcp.sbx.ikigenba.dev
```

```
HEAD /setup.sh HTTP/1.1
Host: mcp.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
```

Status 200. The body is empty.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.

## A caller sends a setup file a method it does not take

The setup files only read, like the connect page.

Request:

```
POST /setup.sh HTTP/1.1
Host: mcp.sbx.ikigenba.dev
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
```

Status 405. The body is empty. Every method but `GET` and `HEAD`, on either file, is refused the same way, with or without an identity.

Preconditions:

- mcp is serving.

Postconditions:

- Nothing has changed.
- mcp wrote nothing to stderr.

## An agent on the user's machine sets up its own client

The agent is the Codex desktop app on the user's Linux machine. Its instructions name the client and that it is the desktop app, and `uname -s` prints `Linux`; the user asked for their own account, so the scope is `user`. It tells the user, in one message, everything they need, the token last so it can sit on the clipboard while they run the command. The token never passes through the chat.

Output:

```
I'll set up Codex desktop for your user account.

1. Create a token at https://auth.sbx.ikigenba.dev/ and copy it.
2. Open a terminal and run:

   curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user

   It will ask for the token. Paste it; it shows as asterisks.
3. Restart Codex and tell me when it's back.
```

The message is the agent's reply to the user; the client's name in its first and third lines, `--client`, and `--scope` follow what the agent worked out. For the `project` scope the first line ends `for this project` and the command ends `--scope project`.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The user asked the agent to read `https://mcp.sbx.ikigenba.dev/setup.txt`, and it read it.
- The agent runs on the user's own Linux machine.

Postconditions:

- Nothing has changed. The agent asked for no token and was given none.

## An agent asks what it cannot tell

The agent is the Claude CLI on the user's machine. Its instructions name the client and the OS, but the user has not said whether ikigenba is for them or for the project at hand, and the Claude CLI has both. It asks that one question, and gives the three-step message once the user answers. An agent whose instructions do not name its client, or that cannot tell its OS, asks that as well, and nothing more.

Output:

```
Should I set up ikigenba for your user account, or just for this project?
```

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent has read `https://mcp.sbx.ikigenba.dev/setup.txt`.
- The user has not said which scope they want.

Postconditions:

- Nothing has changed.

## An agent on another machine stops

An agent may run somewhere other than the user's machine, in a hosted sandbox, say. Setting up a client there would set up the wrong machine, so the agent says where it is running and stops, giving no command. An agent on the user's machine whose OS is not Linux stops the same way, saying the installer supports Linux only.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent has read `https://mcp.sbx.ikigenba.dev/setup.txt`.
- The agent runs on a machine that is not the user's.

Postconditions:

- Nothing has changed. The agent gave the user no command to run.

## An agent with its own secrets flow adds the gateway itself

grokbot is hosted, keeps its MCP servers for the user's account, and adds a remote server through an approval card in which the user types the token masked, out of the transcript. It needs no installer: it adds the server `ikigenba` at `https://mcp.sbx.ikigenba.dev/mcp` with the header `Authorization: Bearer <token>` through that card, tells the user to create a token at `https://auth.sbx.ikigenba.dev/` and enter it there, and checks the connection as below once the user has approved.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent is grokbot and has read `https://mcp.sbx.ikigenba.dev/setup.txt`.

Postconditions:

- The user's grokbot account holds the server `ikigenba`. No token was written in the chat.

## An agent checks the connection after the restart

The user tells the agent the client is back. The agent calls the gateway's `services` tool, then `call` with one read tool of a service it lists as available, and tells the user it is connected, naming the services it reaches. Codex connects without a token when the variable is unset, so a tool list alone does not prove the token worked; the call does. When either step fails, the agent says the connection did not work, quotes the gateway's error, and points the user back to the installer.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The user ran the installer and it exited 0, then restarted the client.

Postconditions:

- Nothing has changed. The agent's call ran a read tool, never `mutate`.

## A user sets up Codex desktop for their account

The user runs the command the agent gave. The installer says what it was told and what it found, and asks before doing anything. It then asks for the token from the terminal, showing one `*` for each character, with backspace taking the last one away, and checks the token with the gateway before writing it anywhere. `codex` is on the `PATH`, so Codex writes its own entry.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user
```

Options:

- `--client <client>` names the client to set up, one of the five above.
- `--scope <scope>` names whose configuration to change, `user` or `project`.

Output:

```
ikigenba setup

  Client:   Codex desktop
  OS:       Linux
  Scope:    user, /home/me/.codex/config.toml
  Endpoint: https://mcp.sbx.ikigenba.dev/mcp

Continue? [y/N] y
Token from https://auth.sbx.ikigenba.dev/: ********************************
Checking the token with the gateway... ok
Saved IKIGENBA_TOKEN in /home/me/.config/environment.d/ikigenba.conf
Set IKIGENBA_TOKEN for this login session
Added ikigenba to /home/me/.codex/config.toml

Restart Codex desktop, then tell your agent it's back.
If it can't reach ikigenba, sign out of your desktop, sign back in, and restart it again.
```

Exits 0. The two prompts and what the user typed in answer, as asterisks, are on the terminal; the other lines are on stdout; stderr is empty. A desktop app may not see a variable set for the session until the user signs in again, hence the last line. For `--scope project` with a Codex client, a last line reads `Codex reads a project's configuration only in a folder you trust.`

Preconditions:

- The user is `me`, with home `/home/me`, on Linux with a systemd user manager, in a terminal; `CODEX_HOME` is unset.
- `codex` is on the `PATH`. `/home/me/.codex/config.toml` holds no `ikigenba` entry.
- The user typed `y` at the first prompt and pasted a token the gateway accepts at the second.

Postconditions:

- `/home/me/.config/environment.d/ikigenba.conf` holds the one line `IKIGENBA_TOKEN=<token>`, owned by `me` with mode `0600`.
- `systemctl --user show-environment` lists `IKIGENBA_TOKEN=<token>`, so an app started from now on in this login session sees it.
- `/home/me/.codex/config.toml` holds the Codex entry for `https://mcp.sbx.ikigenba.dev/mcp`, written by `codex mcp add`, and everything it held before.
- The token is in no client configuration file.

## A user sets up a client whose command is not installed

The Code tab of Claude desktop shares the Claude CLI's configuration, but a user with only the desktop app has no `claude` on the `PATH`. The installer edits the file itself. Everything already in `~/.claude.json`, other servers and settings alike, is kept.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client claude-desktop --scope user
```

Output:

```
ikigenba setup

  Client:   Claude desktop
  OS:       Linux
  Scope:    user, /home/me/.claude.json
  Endpoint: https://mcp.sbx.ikigenba.dev/mcp

Continue? [y/N] y
Token from https://auth.sbx.ikigenba.dev/: ********************************
Checking the token with the gateway... ok
Saved IKIGENBA_TOKEN in /home/me/.config/environment.d/ikigenba.conf
Set IKIGENBA_TOKEN for this login session
Added ikigenba to /home/me/.claude.json

Restart Claude desktop, then tell your agent it's back.
If it can't reach ikigenba, sign out of your desktop, sign back in, and restart it again.
```

Exits 0. The prompts and the asterisks are on the terminal; the other lines are on stdout; stderr is empty.

Preconditions:

- As in `A user sets up Codex desktop for their account`, except that no `claude` is on the `PATH`, and `/home/me/.claude.json` exists, holding other members and an `mcpServers` object with a server `other`.

Postconditions:

- `/home/me/.claude.json` holds, under `mcpServers`, the server `other` as it was and `ikigenba`, the Claude entry; every other member is as it was.
- The token file and the session are as in `A user sets up Codex desktop for their account`.

## A user sets up a client for a project

The project is the current working directory. The token is still the user's own and goes where every other token does; only the client's entry is the project's. The Codex and Grok CLIs read a project's configuration only in a folder the user trusts; the installer grants no trust, and its last lines say so. The Claude clients ask the user to approve a project's server on first use.

Command:

```
$ cd /home/me/src/app
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client grok-cli --scope project
```

Output:

```
ikigenba setup

  Client:   Grok CLI
  OS:       Linux
  Scope:    project, /home/me/src/app/.grok/config.toml
  Endpoint: https://mcp.sbx.ikigenba.dev/mcp

Continue? [y/N] y
Token from https://auth.sbx.ikigenba.dev/: ********************************
Checking the token with the gateway... ok
Saved IKIGENBA_TOKEN in /home/me/.config/environment.d/ikigenba.conf
Set IKIGENBA_TOKEN for this login session
Added ikigenba to /home/me/src/app/.grok/config.toml

Restart Grok CLI, then tell your agent it's back.
If it can't reach ikigenba, sign out of your desktop, sign back in, and restart it again.
Grok CLI reads a project's configuration only in a folder you trust.
```

Exits 0. The prompts and the asterisks are on the terminal; the other lines are on stdout; stderr is empty.

Preconditions:

- As in `A user sets up Codex desktop for their account`, with the working directory `/home/me/src/app`, and `grok` on the `PATH`.

Postconditions:

- `/home/me/src/app/.grok/config.toml` holds the Grok entry, written by `grok mcp add` for the project scope. The user's own `~/.grok/config.toml` and `~/.grok/trusted_folders.toml` are untouched.
- The token file and the session are as in `A user sets up Codex desktop for their account`.

## A user runs the installer again

A user who rotates their token runs the same command again. The installer always asks for a token, and replaces the stored token and the client's `ikigenba` entry with new ones; there is no keeping the old token. The output is that of the first run.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user
```

Output: as in `A user sets up Codex desktop for their account`.

Exits 0. The prompts and the asterisks are on the terminal; the other lines are on stdout; stderr is empty.

Preconditions:

- The installer was run before for `codex-desktop` and `user` with another token.
- The user pasted a new token the gateway accepts.

Postconditions:

- `/home/me/.config/environment.d/ikigenba.conf` holds the one line `IKIGENBA_TOKEN=<new-token>`, and the session holds the new token.
- `/home/me/.codex/config.toml` holds one `ikigenba` entry, for `https://mcp.sbx.ikigenba.dev/mcp`.

## A user declines

The summary is the user's chance to catch a wrong client or scope. Any answer but `y` or `yes`, in either case, an empty one included, stops the installer before the token is asked for.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user
```

Output:

```
ikigenba setup

  Client:   Codex desktop
  OS:       Linux
  Scope:    user, /home/me/.codex/config.toml
  Endpoint: https://mcp.sbx.ikigenba.dev/mcp

Continue? [y/N] n
```

```
ikigenba-setup: cancelled; nothing was changed
```

Exits 1. The summary is on stdout and the prompt on the terminal; the last line is on stderr.

Preconditions:

- As in `A user sets up Codex desktop for their account`; the user typed `n`.

Postconditions:

- Nothing has changed.

## A user gives a token the gateway refuses

A token mistyped, revoked, or expired is refused by the gateway's gate. The installer stops before writing anything, so a bad token never replaces a good one.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user
```

Output:

```
ikigenba setup
...
Checking the token with the gateway... failed
```

```
ikigenba-setup: the gateway refused the token

Create a token at https://auth.sbx.ikigenba.dev/ and run this command again.
```

Exits 1. The lines up to the check are as in `A user sets up Codex desktop for their account`, on the terminal and stdout; the diagnostic is on stderr.

Preconditions:

- The user pasted a token the gateway answers 401 or 403.

Postconditions:

- Nothing has changed. The token file, the session, and the client's configuration are as they were.

## A user's installer cannot reach the gateway

The check needs the gateway. When there is no answer from it, the installer cannot tell a good token from a bad one, so it stops, writing nothing.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user
```

Output:

```
ikigenba setup
...
Checking the token with the gateway... failed
```

```
ikigenba-setup: cannot reach https://mcp.sbx.ikigenba.dev/mcp
```

Exits 1. The lines up to the check are on the terminal and stdout, as in `A user sets up Codex desktop for their account`; the diagnostic is on stderr.

Preconditions:

- The gateway does not answer, or answers with anything but success, 401, or 403.

Postconditions:

- Nothing has changed.

## A user gives no token

Pressing Enter at the token prompt without typing stops the installer.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user
```

Output:

```
ikigenba-setup: no token given; nothing was changed
```

Exits 1. The summary is on stdout and the prompts on the terminal; the last line is on stderr.

Preconditions:

- The user answered `y`, then pressed Enter at the token prompt.

Postconditions:

- Nothing has changed. The gateway was not asked.

## A user runs the installer on another OS

The installer supports Linux only. On any other system it stops before the summary, naming what it found.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client claude-cli --scope user
```

Output:

```
ikigenba-setup: unsupported operating system: Darwin

ikigenba-setup supports Linux only.
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- `uname -s` prints `Darwin`.

Postconditions:

- Nothing has changed.

## A user runs the installer without a terminal

The installer reads the token from the terminal, never from its input. With no terminal, from a job or a pipeline with no controlling terminal, it cannot ask, and stops.

Command:

```
$ setsid curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client codex-desktop --scope user </dev/null
```

Output:

```
ikigenba-setup: no terminal to read from

Run this command in a terminal.
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The installer has no `/dev/tty` to open.

Postconditions:

- Nothing has changed.

## A user names a client or scope the installer does not know

The client and scope are the installer's whole input, so a missing or unknown one stops it before anything else, listing what it takes.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --client cursor --scope user
```

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash -s -- --scope user
```

Output:

```
ikigenba-setup: unknown client 'cursor'

The client is one of codex-cli, codex-desktop, claude-cli, claude-desktop, grok-cli.
```

Exits 2. The text is on stderr; stdout is empty. With no `--client` the first line is `ikigenba-setup: missing --client`; a missing or unknown `--scope` is the same with `scope`, its detail `The scope is user or project.`, and an option the installer does not take is `ikigenba-setup: unknown option '<option>'`.

Preconditions:

- Linux, as in `A user sets up Codex desktop for their account`.

Postconditions:

- Nothing has changed.
