# D3-config-and-session

Every choice a user makes is a `-c key=value` string. Six keys are
**interpreted** by agent-repl because they pick *which* conversation to build
rather than *how* the model generates; every other key is **passed through** to
agentkit as a `Settings.Options` entry, unchanged and unchecked. agentkit
rejects a key its wire does not know at `Send`, and the provider rejects a
value the model does not support; both surface as a rendered turn error (D6).
agent-repl validates neither, on purpose: the catalog is for showing the user
their choices (D4), not for second-guessing them.

| key         | meaning                                              | default                                   |
|-------------|------------------------------------------------------|-------------------------------------------|
| `provider`  | agentkit host: `anthropic` `gemini` `openai` `openrouter` `xai` | derived from `model`           |
| `model`     | model name; the catalog's name, or any string with `provider` | `gpt-5.6-sol`                    |
| `wire`      | agentkit wire name: `messages` `generate-content` `chat` `responses` | the offering's own        |
| `auth`      | `api_key` or `oauth`                                 | `oauth` when offered and the token file exists, else `api_key` |
| `auth_file` | OAuth token file path                                | `~/.agent-repl/<provider>-auth.json`      |
| `base_url`  | request URL prefix                                   | the offering's `BaseURL`                  |

**Validation without I/O** (`Flags.Validate`, D2) produces `Options`:

```go
package options

type Options struct {
    Provider string            // "" when it must be derived from Model
    Model    string
    Wire     string            // "" for the offering's own
    Auth     string            // "" when decided at open (file existence)
    AuthFile string            // "" for the default path
    BaseURL  string            // "" for the offering's own
    Settings map[string]string // every non-interpreted key, verbatim
    Raw      bool
    Version  bool
}
```

`Validate` folds the pairs last-wins, then checks: `provider`, when given,
names an agentkit host; `auth`, when given, is an agentkit auth mode; `wire`,
when given, is an agentkit wire name; `model` is non-empty (the default applies
only when the key is absent); and a `model` given *without* `provider` is in
the catalog, because there is nothing else to derive the provider from. A
`provider` plus an unknown `model` is legal: the model passes to the wire
verbatim and is priced at zero, per agentkit's own off-catalog rule. Provider
derivation is `agentkit.Lookup(model, "", "")`, whose first offering is the
vendor's own host when cataloged, else OpenRouter.

**Opening the session** (`internal/session`) is where the I/O lives:

```go
package session

type Config struct {
    Provider string            // an agentkit host name, always set by the caller
    Model    string
    Wire     string            // "" for the offering's own
    Auth     string            // "" to decide by token-file existence
    AuthFile string            // "" for the default path
    BaseURL  string            // "" for the offering's own
    Settings map[string]string // agentkit Settings.Options, verbatim
    Home     string            // ~/.agent-repl lives here
    Getenv   func(string) string
    Root     string            // tool root
    Log      *agentkit.Log     // may be nil
}

// Plan is everything Open decided before touching the network. Exported so a
// test can check the decision without a conversation.
type Plan struct {
    Offering agentkit.Offering // the transport in use (WireModel replaced for an off-catalog model)
    Model    string            // the model string sent on the wire
    AuthMode agentkit.AuthMode
    EnvVar   string            // the API-key variable consulted, when AuthMode is api_key
    AuthFile string            // the token file consulted, when AuthMode is oauth
    BaseURL  string            // the endpoint actually used
}

func Resolve(cfg Config) (Plan, error)
func Open(cfg Config) (*Session, error)
func (s *Session) Plan() Plan
func (s *Session) Send(ctx context.Context, prompt string) *agentkit.Stream
```

`Resolve` picks the offering with `agentkit.Lookup(model, provider, wire)`.
For an off-catalog model it borrows the transport — wire format, base URL,
auth modes, OAuth client — of the first cataloged offering on that host that
matches `wire` (or the host's first offering when `wire` is empty), and sends
the user's model string as the wire model. The API-key variable is the host
name upper-cased with `_API_KEY` appended; the default token file is
`<Home>/.agent-repl/<host>-auth.json`. When `Auth` is empty, the mode is
`oauth` if the offering lists it *and* the token file exists, else `api_key`.

`Open` then builds the credential — `agentkit.APIKey` from the variable, or
`agentkit.OAuth` over `agentkit.FileTokenStore(AuthFile)` through
`Offering.TokenSource`, which reads the file once and refreshes into it — the
endpoint from `BaseURL` (the offering's, unless overridden), the six toolkit
tools rooted at `Root` with `.git` skipped for `Glob` and `Grep`, and the
conversation with `Settings.Options` set to `Settings` and the supplied log.
An empty API-key variable, a missing or unreadable token file, an `auth` the
offering does not list, or a tool that cannot be constructed is an `Open`
error; the composition root turns every `Open` error into exit 1 (D2).

## REQUIREMENTS

- R-ULZY-FKX0: Package `internal/options` MUST export an `Options` struct whose fields are exactly `Provider string`, `Model string`, `Wire string`, `Auth string`, `AuthFile string`, `BaseURL string`, `Settings map[string]string`, `Raw bool`, and `Version bool`.
- R-UN7U-TCNP: `Flags.Validate` MUST fold `Flags.Config` so that for a repeated key the last `Pair` wins, MUST place the values of `provider`, `model`, `wire`, `auth`, `auth_file`, and `base_url` in the corresponding `Options` fields, and MUST place every other key verbatim in `Options.Settings`.
- R-UOFR-74EE: `Flags.Validate` MUST set `Options.Model` to `gpt-5.6-sol` when no `model` pair is present, and MUST return an error naming `model` when a `model` pair is present with an empty value.
- R-UPNN-KW53: `Flags.Validate` MUST return an error naming the key and the value when `provider` is given and is not one of `anthropic`, `gemini`, `openai`, `openrouter`, `xai`; when `auth` is given and is not `api_key` or `oauth`; or when `wire` is given and is not one of `messages`, `generate-content`, `chat`, `responses`.
- R-UQVJ-YNVS: When `model` is set (explicitly or by default) and `provider` is absent, `Flags.Validate` MUST set `Options.Provider` to the `Host` of the first offering `agentkit.Lookup(model, "", "")` returns, and MUST return an error naming `model` when that lookup fails.
- R-US3G-CFMH: When `provider` is given, `Flags.Validate` MUST accept any non-empty `model`, including one not in the catalog, and leave `Options.Provider` as given.
- R-UTBC-Q7D6: `Flags.Validate` MUST NOT read the environment or any file, verified by validating options whose `auth_file` names a path that does not exist and whose API-key variable is unset.
- R-UUJ9-3Z3V: Package `internal/session` MUST export a `Config` struct whose fields are exactly `Provider string`, `Model string`, `Wire string`, `Auth string`, `AuthFile string`, `BaseURL string`, `Settings map[string]string`, `Home string`, `Getenv func(string) string`, `Root string`, and `Log *agentkit.Log`.
- R-UVR5-HQUK: Package `internal/session` MUST export a `Plan` struct whose fields are exactly `Offering agentkit.Offering`, `Model string`, `AuthMode agentkit.AuthMode`, `EnvVar string`, `AuthFile string`, and `BaseURL string`.
- R-UWZ1-VIL9: Package `internal/session` MUST export `Resolve(cfg Config) (Plan, error)`, `Open(cfg Config) (*Session, error)`, and on `*Session` the methods `Plan() Plan` and `Send(ctx context.Context, prompt string) *agentkit.Stream`.
- R-UY6Y-9ABY: For a cataloged model, `Resolve` MUST set `Plan.Offering` to the offering `agentkit.Lookup(cfg.Model, agentkit.Host(cfg.Provider), agentkit.WireName(cfg.Wire))` returns and `Plan.Model` to that offering's `WireModel`.
- R-UZEU-N22N: For a model not in the catalog, `Resolve` MUST set `Plan.Offering` to the first cataloged offering whose `Host` equals `cfg.Provider` and, when `cfg.Wire` is non-empty, whose `WireName` equals it, with `WireModel` replaced by `cfg.Model`, and MUST set `Plan.Model` to `cfg.Model`.
- R-V1UN-ELK1: `Resolve` MUST set `Plan.EnvVar` to `cfg.Provider` upper-cased followed by `_API_KEY`, and `Plan.AuthFile` to `cfg.AuthFile` when non-empty, else `<cfg.Home>/.agent-repl/<cfg.Provider>-auth.json`.
- R-V32J-SDAQ: When `cfg.Auth` is empty, `Resolve` MUST set `Plan.AuthMode` to `oauth` iff the offering's `AuthModes` contains `agentkit.AuthModeOAuth` and the file at `Plan.AuthFile` exists, and to `api_key` otherwise; when `cfg.Auth` is non-empty it MUST use it, returning an error naming `auth` when the offering's `AuthModes` does not contain it.
- R-V4AG-651F: `Resolve` MUST set `Plan.BaseURL` to `cfg.BaseURL` when non-empty, else to the offering's `BaseURL`.
- R-V5IC-JWS4: With `Plan.AuthMode` `api_key`, `Open` MUST authenticate with `agentkit.APIKey` of `cfg.Getenv(Plan.EnvVar)` and MUST return an error naming the variable when that value is empty.
- R-V6Q8-XOIT: With `Plan.AuthMode` `oauth`, `Open` MUST authenticate with `agentkit.OAuth` over the token source `Plan.Offering.TokenSource(agentkit.FileTokenStore(Plan.AuthFile))` returns, and MUST return an error naming the file when the source cannot be built.
- R-V7Y5-BG9I: `Open` MUST send requests to `Plan.BaseURL`, verified by an `httptest` server named through `cfg.BaseURL` receiving the request.
- R-V961-P807: `Open` MUST register exactly the six toolkit tools `Bash`, `Read`, `Write`, `Edit`, `Glob`, and `Grep` rooted at `cfg.Root`, with `Glob` and `Grep` skipping `.git`, verified by the tool declarations the `httptest` provider receives and by a `Read` of a file under `cfg.Root`.
- R-VADY-2ZQW: `Open` MUST pass `cfg.Settings` to the conversation as `agentkit.Settings.Options` unchanged, verified by an unknown key producing an `agentkit.ErrInvalidConfig` from `Send` rather than an `Open` error.
- R-VBLU-GRHL: `Open` MUST pass `cfg.Log` to the conversation, verified by a turn producing records on the log's writer.
- R-VCTQ-UJ8A: `Session.Send` MUST send `prompt` as a single `agentkit.Text` block and return the resulting stream.
