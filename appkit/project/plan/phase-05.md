# Phase 05 — WWW-root resolution in `appkit/config`

*Realizes design Decision 5 (WWW-root resolution). Depends on no earlier phase
(pure extension of the existing `config.Resolve` composition).*

`appkit/config` gains the `WWWPath` field on `Config` and a `composeWWWPath`
helper beside `composeDataPaths`. Observable end state: `Resolve(app, …)`
always returns a composed `WWWPath` — `<IKIGENBA_ROOT>/<app>/share/current/www`
when `IKIGENBA_ROOT` is set, `./share/www` when it is not, with an
`<APP>_WWW_PATH` env value winning verbatim over either. Nothing checks the
path's existence at resolve time, and no other `Config` field or existing
behavior changes.

**Done when:** the suite is green — `cd appkit && go build ./...`,
`go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
zero failures — and R-LWOU-OWWQ, R-LXWR-2ONF, and R-LZ4N-GGE4 are each covered
by a clearly-named table test in `appkit/config` that genuinely asserts the
behavior its D5 Verification line describes.
