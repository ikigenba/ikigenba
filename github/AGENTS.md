# github

github is a stateless GitHub connector. nginx routes `/srv/github/` to the
service; the MCP surface is authenticated through the shared appkit identity
gate, while loopback-only webhook twin routes remain service-owned.

The service owns no domain database state, no event-plane producer, and no
background worker. The chassis still opens SQLite for uniform `serve` and
`migrate` behavior, so this module embeds only the bootstrap migration tracking
table.

## Local Work

Run package checks from this service directory:

```sh
GOWORK=off go build ./...
GOWORK=off go vet ./...
gofmt -l .
GOWORK=off go test ./...
```
