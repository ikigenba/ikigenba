# D3-role-config

Every choice a user makes is a `-c key=value` string, in agent-repl's grammar
but scoped to a **role**: the key is `<role>.<name>`, where `<role>` is
`supervisor` or `worker`. There are two models in a pass and no reason to
pretend otherwise; an unscoped key is an error rather than a guess about which
role it meant. Within a role, six names are **interpreted** because they pick
*which* conversation to build; `max_context` sets the role's agentkit
`Limits`; every other name is **passed through** to agentkit as a
`Settings.Options` entry for that role, unchanged and unchecked, exactly as in
agent-repl.

| name          | meaning                                                          | default                                   |
|---------------|------------------------------------------------------------------|-------------------------------------------|
| `provider`    | agentkit host: `anthropic` `gemini` `openai` `openrouter` `xai`  | derived from `model`                      |
| `model`       | model name; the catalog's name, or any string with `provider`    | `gpt-5.6-sol`                             |
| `wire`        | agentkit wire name                                               | the offering's own                        |
| `auth`        | `api_key` or `oauth`                                             | `oauth` when offered and the token file exists, else `api_key` |
| `auth_file`   | OAuth token file path                                            | `~/.dory/<provider>-auth.json`            |
| `base_url`    | request URL prefix                                               | the offering's endpoint for the chosen `auth` |
| `max_context` | agentkit `Limits.MaxContextTokens` for every agent of the role   | the offering's `Context`; `0` (no limit) for an off-catalog model |

```
$ dory -c supervisor.model=claude-opus-4-8 -c worker.model=gpt-5.6-sol \
       -c worker.max_context=150000 -c worker.reasoning_effort=high < prompt
```

**Validation without I/O** (`Flags.Validate`, D2) produces `Options`, one
`Role` per role:

```go
package options

type Role struct {
    Provider   string            // "" when it must be derived from Model
    Model      string
    Wire       string            // "" for the offering's own
    Auth       string            // "" when decided at open (file existence)
    AuthFile   string            // "" for the default path
    BaseURL    string            // "" for the offering's own
    MaxContext int64             // -1 when absent: use the offering's Context
    Settings   map[string]string // every non-interpreted name, verbatim
}

type Options struct {
    Supervisor Role
    Worker     Role
    Resume     string            // "" for a new session
    Version    bool
}
```

`Validate` folds the pairs last-wins, then per role checks what agent-repl
checks: `provider`, when given, names an agentkit host; `auth`, when given, is
an agentkit auth mode; `wire`, when given, is an agentkit wire name; `model`
is non-empty; a `model` given *without* `provider` is in the catalog, and the
provider is derived with `agentkit.Lookup(model, "", "")`. `max_context`,
when given, must parse as a non-negative integer. A key with no `.`, or with a
prefix that is neither role, is an error naming the key.

**Opening a role** (`internal/model`) is where the I/O lives. Because a pass
builds a fresh conversation for every agent, the package resolves credentials
once per role and returns a factory:

```go
package model

type Config struct {
    Provider   string            // an agentkit host name, always set by the caller
    Model      string
    Wire       string            // "" for the offering's own
    Auth       string            // "" to decide by token-file existence
    AuthFile   string            // "" for the default path
    BaseURL    string            // "" for the offering's own
    MaxContext int64             // -1 for the offering's Context
    Settings   map[string]string // agentkit Settings.Options, verbatim
    Home       string            // ~/.dory lives here
    Getenv     func(string) string
}

// Plan is everything Open decided before touching the network.
type Plan struct {
    Offering   agentkit.Offering // the transport in use (WireModel replaced for an off-catalog model)
    Model      string            // the model string sent on the wire
    AuthMode   agentkit.AuthMode
    EnvVar     string            // the API-key variable consulted, when AuthMode is api_key
    AuthFile   string            // the token file consulted, when AuthMode is oauth
    BaseURL    string            // the endpoint actually used
    MaxContext int64             // the Limits.MaxContextTokens every conversation gets
}

func Resolve(cfg Config) (Plan, error)
func Open(cfg Config) (*Factory, error)
func (f *Factory) Plan() Plan

// New builds a fresh conversation over the role's fixed transport with the
// given tools and log, and Limits{MaxContextTokens: Plan().MaxContext}.
func (f *Factory) New(tools []agentkit.Tool, log *agentkit.Log) (*agentkit.Conversation, error)
```

`Resolve` and `Open` follow agent-repl's D3 rule for rule: the offering is
`agentkit.Lookup(model, provider, wire)`; an off-catalog model borrows the
transport of the first cataloged offering on that host matching `wire`, with
`WireModel` replaced by the user's string and `Context` zeroed, since the
borrowed `Context` describes another model; the API-key variable is the host
upper-cased with `_API_KEY`; the default token file is
`<Home>/.dory/<host>-auth.json`; the mode defaults to `oauth` when an `oauth`
spec exists and the file exists; `base_url` overrides the chosen spec's URL;
the OAuth rotator is read once up front so a bad file fails at open.
`Plan.MaxContext` is `cfg.MaxContext` when non-negative, else
`Plan.Offering.Context`. The one addition is `New`: `Open` holds the
authenticator and endpoint, and each `New` hands agentkit a `Config` with the
caller's tools and log, the role's `Settings.Options`, and the role's
`Limits`.

There is no system-file key. The system prompt of every agent is its role's
fixed prompt (D5), and the pass's prompt is the root's first user message.

## REQUIREMENTS

- R-HUW9-ZOJI: Package `internal/options` MUST export a `Role` struct whose fields are exactly `Provider string`, `Model string`, `Wire string`, `Auth string`, `AuthFile string`, `BaseURL string`, `MaxContext int64`, and `Settings map[string]string`, and an `Options` struct whose fields are exactly `Supervisor Role`, `Worker Role`, `Resume string`, and `Version bool`.
- R-HW46-DGA7: `Flags.Validate` MUST fold `Flags.Config` so that for a repeated key the last `Pair` wins, MUST route a key of the form `supervisor.<name>` to `Options.Supervisor` and `worker.<name>` to `Options.Worker`, placing `provider`, `model`, `wire`, `auth`, `auth_file`, `base_url` in the corresponding `Role` fields and every other `<name>` except `max_context` verbatim in `Role.Settings`, and MUST return an error naming the key when a key has no `.` or its prefix is neither `supervisor` nor `worker`.
- R-HXC2-R80W: `Flags.Validate` MUST set `Role.MaxContext` to the parsed value of `max_context` when present and to `-1` when absent, and MUST return an error naming the key when the value is not a non-negative decimal integer.
- R-HYJZ-4ZRL: For each role, `Flags.Validate` MUST set `Role.Model` to `gpt-5.6-sol` when no `model` pair is present, MUST return an error naming the key when a `model` pair is present with an empty value, and MUST return an error naming the key and the value when `provider` is given and is not one of `anthropic`, `gemini`, `openai`, `openrouter`, `xai`, when `auth` is given and is not `api_key` or `oauth`, or when `wire` is given and is not one of `messages`, `generate-content`, `chat`, `responses`.
- R-I0ZR-WJ8Z: For each role, when `model` is set (explicitly or by default) and `provider` is absent, `Flags.Validate` MUST set `Role.Provider` to the `Host` of the first offering `agentkit.Lookup(model, "", "")` returns and MUST return an error naming the key when that lookup fails; when `provider` is given it MUST accept any non-empty `model` and leave `Role.Provider` as given.
- R-I27O-AAZO: `Flags.Validate` MUST NOT read the environment or any file, verified by validating options whose `auth_file` names a path that does not exist and whose API-key variables are unset.
- R-I3FK-O2QD: Package `internal/model` MUST export a `Config` struct whose fields are exactly `Provider string`, `Model string`, `Wire string`, `Auth string`, `AuthFile string`, `BaseURL string`, `MaxContext int64`, `Settings map[string]string`, `Home string`, and `Getenv func(string) string`, and a `Plan` struct whose fields are exactly `Offering agentkit.Offering`, `Model string`, `AuthMode agentkit.AuthMode`, `EnvVar string`, `AuthFile string`, `BaseURL string`, and `MaxContext int64`.
- R-I4NH-1UH2: Package `internal/model` MUST export `Resolve(cfg Config) (Plan, error)`, `Open(cfg Config) (*Factory, error)`, and on `*Factory` the methods `Plan() Plan` and `New(tools []agentkit.Tool, log *agentkit.Log) (*agentkit.Conversation, error)`.
- R-I5VD-FM7R: For a cataloged model, `Resolve` MUST set `Plan.Offering` to the offering `agentkit.Lookup(cfg.Model, agentkit.Host(cfg.Provider), agentkit.WireName(cfg.Wire))` returns and `Plan.Model` to that offering's `WireModel`; for a model not in the catalog it MUST set `Plan.Offering` to the first cataloged offering whose `Host` equals `cfg.Provider` and, when `cfg.Wire` is non-empty, whose `WireName` equals it, with `WireModel` replaced by `cfg.Model` and `Context` replaced by `0`, and MUST set `Plan.Model` to `cfg.Model`.
- R-I739-TDYG: `Resolve` MUST set `Plan.EnvVar` to `cfg.Provider` upper-cased followed by `_API_KEY`, and `Plan.AuthFile` to `cfg.AuthFile` when non-empty, else `<cfg.Home>/.dory/<cfg.Provider>-auth.json`.
- R-I8B6-75P5: When `cfg.Auth` is empty, `Resolve` MUST set `Plan.AuthMode` to `oauth` iff `Plan.Offering.Endpoints` contains a spec whose `AuthMode` is `agentkit.AuthModeOAuth` and the file at `Plan.AuthFile` exists, and to `api_key` otherwise; when `cfg.Auth` is non-empty it MUST use it, returning an error naming `auth` when no spec in `Plan.Offering.Endpoints` has that `AuthMode`.
- R-I9J2-KXFU: `Resolve` MUST set `Plan.BaseURL` to `cfg.BaseURL` when non-empty, else to the `BaseURL` of the spec in `Plan.Offering.Endpoints` whose `AuthMode` equals `Plan.AuthMode`.
- R-IAQY-YP6J: `Resolve` MUST set `Plan.MaxContext` to `cfg.MaxContext` when it is non-negative and to `Plan.Offering.Context` when it is negative.
- R-IBYV-CGX8: With `Plan.AuthMode` `api_key`, `Open` MUST authenticate with `agentkit.APIKeyRotator` of `cfg.Getenv(Plan.EnvVar)` and MUST return an error naming the variable when that value is empty; with `Plan.AuthMode` `oauth`, `Open` MUST authenticate with `agentkit.OAuthRotator` over `agentkit.FileTokenStore(Plan.AuthFile)` and MUST return an error naming the file when the file is missing, unreadable, or yields no token, rather than deferring that failure to the first `Send`.
- R-ID6R-Q8NX: Every conversation `Factory.New` returns MUST send requests to `Plan.BaseURL`, MUST advertise exactly the `tools` given to that `New` call, MUST carry `cfg.Settings` as `agentkit.Settings.Options` unchanged, MUST write its records to the `log` given to that `New` call, and MUST have `Limits.MaxContextTokens` equal to `Plan.MaxContext`, verified through an `httptest` provider and a log writer.
- R-IEEO-40EM: Two conversations from successive `Factory.New` calls MUST have independent histories, verified by a turn on the first leaving the second's first request without the first's messages.
