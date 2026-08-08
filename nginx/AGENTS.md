# nginx

`nginx/` is the local-development front door on `:8080`. Its `./run` script
assembles routing that mirrors production's `/srv/<svc>/` layout, while the
committed files under `parked/` provide the `default_server` for live non-apex
domains.

This tree is spec-governed by `nginx/project/`; its configuration and `run`
script are not hand-edited. The tree is not versioned.

## Tests

This tree adopts the testing-language contract at `root project/design/D23.md`
structurally. It has no test suite and therefore no test command. Its committed
testing declaration is:

- check commands: `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` and
  `bash -n nginx/run`
- layers: manual only — no hermetic, composed, or live layer, and no
  `//go:build live` file
- preconditions: an `nginx` binary on `PATH` for the configuration check; no Go
  toolchain is required because no Go code lives here
- GOWORK: not applicable — this is not a Go module, and the repository-root
  `go.work` must not name this tree

The two check commands are configuration and syntax checks, not tests or
testing layers. Manual verification lives in the repository-root `deploy.md`
live-box checklist and in the suite-level bring-the-stack-up health check.
