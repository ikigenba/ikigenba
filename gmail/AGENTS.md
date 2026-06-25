# gmail

gmail is a loopback-only Gmail connector. nginx routes `/srv/gmail/` to the
service and remains the sole trust boundary for both served surfaces: a
bearer-gated MCP surface for agents and a session-cookie-gated human web landing page.

The service accepts nginx-provided identity headers as trusted input and runs no
token logic. Its domain work stays in the normal-mailbox MCP tool surface, the
Gmail History API poll daemon, and the `mail.*` event-plane producer.

## Local Work

Run package checks from this service directory:

```sh
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

Run migration checks from the suite root:

```sh
bin/check-migrations gmail
```
