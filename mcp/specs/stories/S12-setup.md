# Stories — setup

Connecting an agent's client to the gateway without the token ever passing through the chat. mcp serves two files to anyone, signed in or not: `/setup.txt`, plain-text instructions an agent reads and follows, and `/setup.sh`, a script a user runs in their own terminal to store a token. The connect page's `Automatic Install` section points at the first (`S03`); the first has the agent ask the user to run the second, and then has the agent configure its own client from what the second stored. Both files are the same for every caller and carry this space's values, filled in when they are asked for, so neither the agent nor the script works any of them out:

- `<mcp>` is `<scheme>://<host>`, where `<host>` and `<scheme>` are read from the request as the connect page reads them for its endpoint (`S03`); the endpoint is `<mcp>/mcp`.
- `<auth>` is `<auth-profile>` as the connect page derives it (`S03`), auth's profile page, where a user creates a token.
- `<space>` is the request's `Host` with a trailing port dropped, then its leading `mcp.` label dropped; the gateway's host is always `mcp.<space>`.
- `<variable>` is `IKIGENBA_TOKEN_` followed by the space with every character that is not an ASCII letter or digit replaced by `_`, in upper case.
- `<server>` is `ikigenba-` followed by the space with every character that is not an ASCII letter or digit replaced by `-`, in lower case.
- `<dir>` is `${XDG_CONFIG_HOME:-~/.config}/ikigenba/<space>/`, the space's own directory on the user's machine, so tokens for different spaces never meet.

For the suite's services file and `Host: mcp.sbx.ikigenba.dev` over `https`, `<mcp>` is `https://mcp.sbx.ikigenba.dev`, `<auth>` is `https://auth.sbx.ikigenba.dev/`, `<space>` is `sbx.ikigenba.dev`, `<variable>` is `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV`, `<server>` is `ikigenba-sbx-ikigenba-dev`, and `<dir>` is `${XDG_CONFIG_HOME:-~/.config}/ikigenba/sbx.ikigenba.dev/`. For `Host: mcp.wip-mcp.localhost:7403` over `http` they are `http://mcp.wip-mcp.localhost:7403`, `wip-mcp.localhost`, `IKIGENBA_TOKEN_WIP_MCP_LOCALHOST`, and `ikigenba-wip-mcp-localhost`. mcp reads the services file afresh for each request, as for the page. mcp serves guests (`S01`), so the host's nginx passes a request for either path to mcp whether or not it carries a credential, and a guest's arrives with no `X-User-Id` (`S03`); mcp answers the same either way, and records the request in its trail with the user it arrived with, empty for none (`S02`). `/setup.txt` and `/setup.sh` take `GET` and `HEAD`; a path beneath either, or either with a trailing `/`, is a path that does not exist (`S03`).

The script is a Bash program for Linux, whose diagnostics begin `ikigenba-setup: `. It is run as `curl -fsSL <mcp>/setup.sh | bash` and takes no options. Its standard input is the pipe, so it reads the token from the terminal, `/dev/tty`, and writes its lines, its prompt and the token's asterisks there; every diagnostic goes to stderr, and nothing to stdout. It checks the token with the gateway before it writes anything, and writes nothing when it stops before or at that check. When the token is good it writes two files in `<dir>`, creating the directory with mode `0700` when there is none, and replacing each file whole, never leaving one half-written: `token`, mode `0600`, holding the token and a newline, which no agent reads; and `config`, mode `0644`, which the agent reads, holding one `key=value` line each for the space, the endpoint, auth's profile page, the variable, and the server, in this order:

```
space=sbx.ikigenba.dev
mcp=https://mcp.sbx.ikigenba.dev/mcp
auth=https://auth.sbx.ikigenba.dev/
variable=IKIGENBA_TOKEN_SBX_IKIGENBA_DEV
server=ikigenba-sbx-ikigenba-dev
```

The script changes no environment and no client's configuration; that is the agent's work. It exits 0 when the token is stored and 1 when it stopped short.

## An agent reads the setup instructions

An agent the user is talking to has been asked to follow the instructions at `<mcp>/setup.txt`, as the connect page tells the user to ask (`S03`). The file is written for the agent, not the user. It states outcomes for the agent to make true rather than steps to follow, leaving the how to what the agent knows of its own client and the user's machine, and an agent that follows it is the actor of the stories below that begin `An agent`. A signed-in caller gets the same file, and so does a caller on a host whose services file names no `auth`, whose `<auth>` is read from the `Host` as on the connect page (`S03`).

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

Status 200. The body is plain text, the same for both requests, naming the space `sbx.ikigenba.dev`. It tells the agent:

- that it must be running on the user's own machine, and, when it runs anywhere else, to tell the user it cannot configure their machine from there and stop; and that an agent whose client has its own way to add a remote MCP server with a secret typed outside the chat uses that way instead;
- first, to ask the user to create a token at `https://auth.sbx.ikigenba.dev/` and run `curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash` in a terminal on their machine, and to wait until they say it is done;
- that the script saves `config` and `token` in `${XDG_CONFIG_HOME:-~/.config}/ikigenba/sbx.ikigenba.dev/`, written out in full; that the agent reads `config` and uses its values, deriving none itself; and that it never reads, prints or copies `token`, into the chat or into a command it shows;
- then, to make four outcomes true: the variable `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV` holds the token in the environment the client gets when it is restarted the way it is running now, a terminal session for a command-line client and the desktop session for a desktop app, and no command that moves the token prints it; the client has an MCP server named `ikigenba-sbx-ikigenba-dev`, at `https://mcp.sbx.ikigenba.dev/mcp`, that sends `Authorization: Bearer <token>` by referring to the variable, never holding the token itself unless the client cannot read a variable or a file and the user agrees; nothing else in the user's configuration is lost or changed, other servers, other spaces' entries and other variables included, and another ikigenba entry or token variable found is left as it is and mentioned to the user; and, when the client has both a user scope and a project scope, the user has chosen which;
- then, to tell the user what it configured: the server's name, the file and scope it is in, the variable's name and where it is set; and exactly what to restart, and whether a new terminal or a fresh login is needed; and to wait; it does not ask the user how they launch the client;
- after the restart, to call the gateway's `services` tool and then one read-only `call` on a service it lists, both of which must succeed, since some clients connect without a token when the variable is missing; and, when either fails, to find out why and fix it.

Last, the body holds a reference of facts, not steps, about Codex, Claude Code, the Grok CLI, and where a Linux terminal and a Linux desktop session take their environment from.

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

## A user's terminal fetches the script

The command the agent gives the user fetches the script and runs it. The file is the script the stories below run, with this space's values in it, so it needs nothing from the user but the token.

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

Status 200. The body is a Bash script, beginning `#!/usr/bin/env bash`, whose space is `sbx.ikigenba.dev`, whose endpoint is `https://mcp.sbx.ikigenba.dev/mcp`, whose token page is `https://auth.sbx.ikigenba.dev/`, whose variable is `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV`, and whose server is `ikigenba-sbx-ikigenba-dev`. With `Host: mcp.sbx.ikigenba.dev:443` its endpoint is `https://mcp.sbx.ikigenba.dev:443/mcp` and its space, variable and server are unchanged; with `X-Forwarded-Proto: http`, its endpoint is `http://mcp.sbx.ikigenba.dev/mcp`, and its token page `http://auth.sbx.ikigenba.dev/` when the services file names no `auth`.

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

## An agent has the user store a token

The agent is a client on the user's own Linux machine; it has read `https://mcp.sbx.ikigenba.dev/setup.txt`. It tells the user, in its own words, to create a token at `https://auth.sbx.ikigenba.dev/` and to run this in a terminal on their machine, then waits for them to say it is done:

```
curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

The wording of the agent's reply is its own; the token page and the command are as above. The token never passes through the chat.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The user asked the agent to follow `https://mcp.sbx.ikigenba.dev/setup.txt`, and it read it.
- The agent runs on the user's own machine.

Postconditions:

- Nothing has changed. The agent asked for no token and was given none.

## An agent configures its client once the token is stored

The user says the script is done. The agent reads `config` in `${XDG_CONFIG_HOME:-~/.config}/ikigenba/sbx.ikigenba.dev/` and works from its values. How it gets the token into the client's environment, and how it adds the server, is up to the agent and what it knows of its client, so the commands it runs are not fixed; what holds afterwards is. It then tells the user, in its own words, the server's name `ikigenba-sbx-ikigenba-dev`, the file and scope the server is in, the variable's name `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV` and where it is set, since the user may have other ikigenba spaces set up and needs the names to tell them apart; and exactly what to restart, and whether a new terminal or a fresh login is needed for the variable. It waits for the user to say the client is back. It does not ask how the user launches the client: it is running in it.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The user ran `curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash` and it exited 0, so `config` and `token` are in the space's directory.
- The client has one scope, or the user has chosen one (`An agent asks which scope to use`).

Postconditions:

- When the client is restarted the way it is running now, its environment holds `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV`, whose value is the token in `token`.
- The client's configuration, in the chosen scope, holds a server named `ikigenba-sbx-ikigenba-dev` at `https://mcp.sbx.ikigenba.dev/mcp` that sends `Authorization: Bearer <token>` by referring to `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV`; the token itself is in no client configuration file.
- Everything else in the user's configuration is as it was.
- The agent never read `token`, and no output it showed or wrote to the chat holds the token.

## An agent asks which scope to use

The agent is the Claude CLI, which has both a user scope, for all of the user's projects, and a project scope, for the project at hand. The user has not said which they want, so the agent asks, in its own words, and configures the client in the scope the user names. A client with one scope is configured in that scope without the question.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent has read `https://mcp.sbx.ikigenba.dev/setup.txt`.
- The user has not said which scope they want.

Postconditions:

- Nothing has changed.

## An agent finds another space already set up

The user has connected the client before to another space, `wip-mcp.localhost`, so the client's configuration holds a server `ikigenba-wip-mcp-localhost` and the environment a variable `IKIGENBA_TOKEN_WIP_MCP_LOCALHOST`. The agent adds `ikigenba-sbx-ikigenba-dev` and `IKIGENBA_TOKEN_SBX_IKIGENBA_DEV` beside them, and tells the user it found the other entry and left it alone.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The token for `sbx.ikigenba.dev` is stored, as in `An agent configures its client once the token is stored`.
- The client's configuration holds the server `ikigenba-wip-mcp-localhost`, and the variable `IKIGENBA_TOKEN_WIP_MCP_LOCALHOST` is set.

Postconditions:

- The server `ikigenba-wip-mcp-localhost` and the variable `IKIGENBA_TOKEN_WIP_MCP_LOCALHOST` are as they were.
- The client also holds `ikigenba-sbx-ikigenba-dev`, as in `An agent configures its client once the token is stored`.

## An agent whose client cannot refer to a variable asks first

Some client can read neither a variable nor a file for a server's header. The only way to send the token is then to write it into the client's configuration, which the agent does only when the user agrees. When the user does not agree, the agent writes no token into the configuration.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The token is stored, as in `An agent configures its client once the token is stored`.
- The client has no way to read a server's header from a variable or a file.

Postconditions:

- With the user's agreement, the client's configuration holds the server `ikigenba-sbx-ikigenba-dev` with the token written into its header; without it, nothing has changed.

## An agent on another machine stops

An agent may run somewhere other than the user's machine, in a hosted sandbox, say. Configuring a client there would configure the wrong machine, so the agent tells the user it cannot configure their machine from where it runs and stops, giving no command.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent has read `https://mcp.sbx.ikigenba.dev/setup.txt`.
- The agent runs on a machine that is not the user's.

Postconditions:

- Nothing has changed. The agent gave the user no command to run.

## An agent with its own secrets flow adds the gateway itself

grokbot is hosted, keeps its MCP servers for the user's account, and adds a remote server through an approval card in which the user types the token masked, out of the transcript. It uses that way instead of the script and the rest of the instructions: it adds the server `ikigenba-sbx-ikigenba-dev` at `https://mcp.sbx.ikigenba.dev/mcp` with the header `Authorization: Bearer <token>` through that card.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent is grokbot and has read `https://mcp.sbx.ikigenba.dev/setup.txt`.

Postconditions:

- The user's grokbot account holds the server `ikigenba-sbx-ikigenba-dev`. No token was written in the chat.

## An agent checks the connection after the restart

The user tells the agent the client is back. The agent calls the gateway's `services` tool, then `call` with one read tool of a service it lists as available, and tells the user it is connected, naming the services it reaches. Codex connects without a token when the variable is unset, so a connection or a tool list alone does not prove the token worked; the call does. When either step fails, the agent finds out why, whether the variable is set in its own process, whether it is the right one, whether the configuration refers to it, and fixes it.

No command is run and nothing exits; the agent acts as `/setup.txt` directs it, and what it says is its reply to the user in the chat.

Preconditions:

- The agent configured its client as in `An agent configures its client once the token is stored`, and the user restarted the client as the agent said.

Postconditions:

- Nothing has changed. The agent's call ran a read tool, never `mutate`.

## A user stores a token for the space

The user runs the command the agent gave. The script says which space it is for, which gateway it checks with, and where it saves; asks for the token from the terminal, showing one `*` for each character, with backspace taking the last one away and Ctrl-U clearing them all; trims white space from both ends of what was typed; and checks the token with the gateway before writing anything.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
Space:   sbx.ikigenba.dev
Gateway: https://mcp.sbx.ikigenba.dev/mcp
Saves:   /home/me/.config/ikigenba/sbx.ikigenba.dev/{config,token}

Create a token at https://auth.sbx.ikigenba.dev/ and paste it here.
Token: ****************************************************************
Checking the token with the gateway... ok

Saved. Tell your agent setup is done; it will take it from here.
```

Exits 0. Every line, and what the user typed, as asterisks, is on the terminal; stdout and stderr are empty. With `XDG_CONFIG_HOME` set, `Saves:` names the directory under it.

Preconditions:

- The user is `me`, with home `/home/me`, on Linux, in a terminal; `XDG_CONFIG_HOME` is unset.
- `/home/me/.config/ikigenba/sbx.ikigenba.dev/` does not exist.
- The user pasted a token the gateway accepts.

Postconditions:

- `/home/me/.config/ikigenba/sbx.ikigenba.dev/` exists, owned by `me` with mode `0700`.
- In it, `token` holds the token and a newline, mode `0600`, and `config` holds the five lines shown above, mode `0644`.
- No environment, shell profile, or client configuration was changed.
- The token was never in a command line: the script sent it to the gateway only in the request's `Authorization` header.

## A user runs the script again

A user who rotates their token, or set it up wrong, runs the same command again. The script always asks for a token, and a token the gateway accepts replaces the stored one; there is no keeping the old token. The output is that of the first run.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output: as in `A user stores a token for the space`.

Exits 0. Every line is on the terminal; stdout and stderr are empty.

Preconditions:

- The script was run before for `sbx.ikigenba.dev` with another token.
- The user pasted a new token the gateway accepts.

Postconditions:

- `token` holds the new token, and `config` the same five lines.
- Every other space's directory under `/home/me/.config/ikigenba/` is as it was.

## A user gives a token the gateway refuses

A token mistyped, revoked, or expired is refused by the gateway's gate. The script stops before writing anything, so a bad token never replaces a good one.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
Space:   sbx.ikigenba.dev
...
Checking the token with the gateway... failed
```

```
ikigenba-setup: the gateway refused the token; nothing was changed

Create a token at https://auth.sbx.ikigenba.dev/ and run this again.
```

Exits 1. The lines up to the check are on the terminal, as in `A user stores a token for the space`; the diagnostic is on stderr; stdout is empty.

Preconditions:

- The user pasted a token the gateway answers 401 or 403.

Postconditions:

- Nothing has changed. The space's directory, and the files in it, are as they were.

## A user's script cannot reach the gateway

The check needs the gateway. When the gateway does not answer, or answers with anything but success, 401, or 403, the script cannot tell a good token from a bad one, so it stops, writing nothing. The diagnostic names the status the gateway answered, or `000` when there was no answer.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
Space:   sbx.ikigenba.dev
...
Checking the token with the gateway... failed
```

```
ikigenba-setup: cannot reach https://mcp.sbx.ikigenba.dev/mcp (HTTP 000); nothing was changed
```

Exits 1. The lines up to the check are on the terminal, as in `A user stores a token for the space`; the diagnostic is on stderr; stdout is empty.

Preconditions:

- The gateway does not answer within 30 seconds.

Postconditions:

- Nothing has changed.

## A user gives no token

Pressing Enter at the token prompt with nothing typed, or only white space, stops the script.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
ikigenba-setup: no token given; nothing was changed
```

Exits 1. The lines up to the prompt are on the terminal; the diagnostic is on stderr; stdout is empty.

Preconditions:

- The user pressed Enter at the token prompt without typing a token.

Postconditions:

- Nothing has changed. The gateway was not asked.

## A user interrupts the script

Pressing Ctrl-C at the token prompt stops the script and gives the terminal back as it was, echoing again.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
ikigenba-setup: interrupted; nothing was changed
```

Exits 1. The lines up to the prompt are on the terminal; the diagnostic is on stderr; stdout is empty.

Preconditions:

- The user pressed Ctrl-C at the token prompt.

Postconditions:

- Nothing has changed. The terminal echoes what is typed again.

## A user runs the script on another OS

The script supports Linux only. On any other system it stops before anything else.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
ikigenba-setup: only Linux is supported for now
```

Exits 1. The line is on stderr; stdout is empty, and nothing is on the terminal.

Preconditions:

- `uname -s` prints something other than `Linux`, `Darwin` say.

Postconditions:

- Nothing has changed.

## A user runs the script without curl

The script checks the token with `curl`. A user who saved the script and runs it with `bash` on a machine without `curl` is stopped before anything else.

Command:

```
$ bash setup.sh
```

Output:

```
ikigenba-setup: curl is not installed
```

Exits 1. The line is on stderr; stdout is empty, and nothing is on the terminal.

Preconditions:

- `setup.sh` is the file `/setup.sh` serves, saved in the working directory.
- No `curl` is on the `PATH`.

Postconditions:

- Nothing has changed.

## A user runs the script without a terminal

The script reads the token from the terminal, never from its input. With no terminal, from a job or a pipeline with no controlling terminal, it cannot ask, and stops.

Command:

```
$ setsid bash -c 'curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash' </dev/null
```

Output:

```
ikigenba-setup: no terminal to read the token from

Run this in a terminal.
```

Exits 1. The text is on stderr; stdout is empty.

Preconditions:

- The script has no `/dev/tty` to open.

Postconditions:

- Nothing has changed.

## A user's script cannot write the space's directory

The token is good, but the space's directory cannot be created, or a file in it cannot be written, because a parent is not writable, say. The script stops, naming what it could not create or write.

Command:

```
$ curl -fsSL https://mcp.sbx.ikigenba.dev/setup.sh | bash
```

Output:

```
Space:   sbx.ikigenba.dev
...
Checking the token with the gateway... ok
```

```
ikigenba-setup: cannot create /home/me/.config/ikigenba/sbx.ikigenba.dev
```

Exits 1. The lines up to the check are on the terminal; the diagnostic is on stderr; stdout is empty. When the directory exists but a file cannot be written, the diagnostic is `ikigenba-setup: cannot write <dir>/token` or `ikigenba-setup: cannot write <dir>/config`, naming the file.

Preconditions:

- The user pasted a token the gateway accepts.
- `/home/me/.config/ikigenba/` is not writable by `me`.

Postconditions:

- No file in the space's directory holds part of a token or a config: each is as it was or wholly replaced.
