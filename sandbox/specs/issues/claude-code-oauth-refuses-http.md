# Claude Code refuses OAuth token exchange over a sandbox's plain HTTP

Filed during the MCP OAuth delivery's sandbox check on branch `wip-mcp`, after auth's MCP client authorization landed (`e1eda78`), with sandbox `wip-mcp` on port 7402 and Claude Code 2.1.293.

Requirements involved: R-0QNY-FX6R (`specs/design/D03-sandbox-and-data.md`), which makes every app origin `http://<app>.<name>.localhost:<port>`, and through it R-WMFU-SECP and R-WNNR-663E (`specs/design/D06-routing.md`), whose `resource_metadata` and, by extension, auth's issuer and token endpoint are plain HTTP on a non-loopback host name.

Friction: Claude Code's MCP OAuth client completed discovery (protected-resource and authorization-server metadata), dynamic registration, the approve page and the loopback redirect, then refused the token exchange:

```
Refusing to send credentials to non-https token endpoint 'http://auth.wip-mcp.localhost:7402/token'. OAuth token requests MUST use TLS (localhost / 127.0.0.1 / ::1 are exempt).
```

Its exemption covers only the literal `localhost`, `127.0.0.1` and `::1`, not `*.localhost`. Codex completed the same round trip over HTTP (login, tool calls, revoke answered by a prompt to log in again), so the sandbox's routing and auth's endpoints are correct; the refusal is the client's TLS policy.

Why unresolvable in-role: the sandbox serves plain HTTP on `*.localhost` by design, and no sandbox-only change reaches Claude Code's policy. The delivery's decisions document says a client refusing HTTP in the sandbox is filed as an issue and the round trip over HTTPS on `us.ikigenba.dev` stands as the proof.

Suggested resolution, any one of:

- Accept it: Claude Code's OAuth is checked on a space, never in a sandbox; delete this issue.
- Serve the sandbox over TLS (a local CA and certificates for `*.<name>.localhost`), changing R-0QNY-FX6R and the routing requirements that build on it.
- Ask upstream for Claude Code to treat `*.localhost` as loopback, as RFC 6761 and browsers do.
