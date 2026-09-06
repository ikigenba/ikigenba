# D4-help-and-version

**Usage text.** One block, produced by `options.Usage() string` and placed by
its caller: stdout for `-h`/`--help`, stderr on a usage error (D2). The text
is a catalog as much as a usage screen: it is the one place a user sees which
providers, models, and reasoning values exist, and it is generated from
`agentkit.Catalog()` so it cannot drift from the library it fronts. The fixed
head:

```
usage: agent-repl [-c key=value ...] [-raw] [-V] [-h]

flags:
  -c key=value   set a config value (repeatable, last wins)
  -raw           emit the raw, undecorated message stream
  -V, --version  show the version and exit
  -h, --help     show this catalog and exit

defaults:
  provider=openai   model=gpt-5.6-sol   auth=oauth

providers:
```

Then the **providers block**, one host per line in host-name order, from
`help.Providers()`: the host padded to 13 columns, `auth=api_key` padded to 14,
and the API-key variable in parentheses; when any of the host's offerings lists
OAuth, a second line, indented to the auth column, with `auth=oauth` and the
default token file:

```
  anthropic    auth=api_key  (ANTHROPIC_API_KEY)
  gemini       auth=api_key  (GEMINI_API_KEY)
  openai       auth=api_key  (OPENAI_API_KEY)
               auth=oauth    (auth_file=~/.agent-repl/openai-auth.json)
  openrouter   auth=api_key  (OPENROUTER_API_KEY)
  xai          auth=api_key  (XAI_API_KEY)
               auth=oauth    (auth_file=~/.agent-repl/xai-auth.json)
```

Then a blank line and the **model sections** from `help.Models()`: one section
per host in host-name order, the bare host name on its own line, then one row
per cataloged model on that host in catalog (model-name) order, sections
separated by one blank line:

```
anthropic
  claude-fable-5            effort={low|*medium|high|xhigh|max}
  claude-haiku-4-5          thinking_budget={*off|1024–4096}
  claude-opus-5             effort={low|*medium|high|xhigh|max|off}

gemini
  gemini-2.5-flash          thinking_budget={*dynamic|off|0–24576}
  gemini-3.5-flash          thinking_level={minimal|low|*medium|high}

openrouter
  deepseek-v4-flash         thinking={*dynamic|on|off}
  grok-4.20                 thinking={on|*off}
  kimi-k2.7-code            thinking={*on}
```

A row is the model name padded to 26 columns after a two-space indent, then
the offering's `Reasoning.Term`, `=`, and the vocabulary in braces separated
by `|`. The vocabulary lists, in this order: `dynamic` when the default is
the vendor's own (`ReasoningDefault`); then, by kind — effort: every level in
`Levels` order by its lowercase name, then `off` when `CanDisable`; budget:
`off` when `CanDisable`, then `MinBudget–MaxBudget` joined by an en dash; toggle:
`on` when `CanEnable`, then `off` when `CanDisable`. Exactly one entry carries
a leading `*`: the one the offering's `Default` names. A none-kind offering
renders the model name alone. When a host serves a model over two wires, the
row uses the first of that host's offerings in the entry's order.

Then a blank line and the fixed **trailer**:

```
a bare -c model=NAME must name a cataloged model (its provider is derived).
any other model needs -c provider=... too and is priced at 0.
every other -c key is passed to the wire as an option; unknown keys fail at send.
-c base_url=URL sends requests to URL instead of the provider's endpoint.
-c system_file=PATH sends the file's contents as the system prompt before the first message.
```

The examples above are illustrations; the rows in the real text come from the
catalog at build time, so a catalog release changes the help without a change
here. The two rendering operations are exported from `internal/help` so their
output can be pinned against the catalog independently of the fixed text.

**Version.** A single `var version` in `internal/cli`, carried in source and
never ldflags-injected, so a development build and a released build report
the same string. The spec fixes its shape and output form only; its value is
release data. `-V` and `--version` print it bare on its own line to stdout.

## REQUIREMENTS

- R-VE1N-8AYZ: Package `internal/options` MUST export `Usage() string`.
- R-VF9J-M2PO: Package `internal/help` MUST export `Providers() string` and `Models() string`.
- R-VGHF-ZUGD: `options.Usage()` MUST begin with the fixed head byte-for-byte as quoted above, from the `usage:` line through the `providers:` line inclusive, followed by a newline.
- R-NHOS-5G50: `options.Usage()` MUST consist of, in order: the fixed head, `help.Providers()`, a blank line, `help.Models()`, a blank line, and the fixed trailer byte-for-byte as quoted above, with nothing else.
- R-VIX8-RDXR: `help.Providers()` MUST contain one line per agentkit host in host-name order, formatted as two spaces, the host name left-justified in 13 columns, `auth=api_key` left-justified in 14 columns, and the host name upper-cased followed by `_API_KEY` in parentheses.
- R-B9R3-FFZ7: For each host with at least one cataloged offering whose `Endpoints` contains a spec with `AuthMode` `agentkit.AuthModeOAuth`, `help.Providers()` MUST follow that host's line with a line of 15 spaces, `auth=oauth` left-justified in 14 columns, and `(auth_file=~/.agent-repl/<host>-auth.json)`, and MUST emit no such line for any other host.
- R-VMKX-WP5U: `help.Models()` MUST contain one section per agentkit host in host-name order, each beginning with the host name on its own line and followed by one row per `agentkit.Catalog()` entry that has an offering on that host, in catalog order, with consecutive sections separated by exactly one blank line.
- R-VNSU-AGWJ: Each row of `help.Models()` MUST be two spaces, the model name left-justified in 26 columns, the offering's `Reasoning.Term`, `=`, `{`, the vocabulary entries joined by `|`, and `}`, using the first offering on that host in the entry's `Offerings` order; a `ReasoningKindNone` offering MUST render as two spaces and the model name alone.
- R-VP0Q-O8N8: A row's vocabulary MUST list, in order: `dynamic` iff `Default.Mode` is `ReasoningDefault`; then for `ReasoningKindEffort` every level of `Levels` in order by its `String()` name followed by `off` iff `CanDisable`; for `ReasoningKindBudget` `off` iff `CanDisable` followed by `MinBudget`, an en dash (U+2013), and `MaxBudget`; for `ReasoningKindToggle` `on` iff `CanEnable` followed by `off` iff `CanDisable`.
- R-VQ8N-20DX: Exactly one vocabulary entry of each row MUST carry a leading `*`: `dynamic` when `Default.Mode` is `ReasoningDefault`, `off` when `ReasoningOff`, `on` when `ReasoningOn`, the level named by `Default.Effort` when `ReasoningEffort`, and the budget range when `ReasoningBudget`.
- R-VRGJ-FS4M: `help.Models()` MUST contain the rows `  claude-haiku-4-5          thinking_budget={*off|1024–4096}` and `  gemini-2.5-flash          thinking_budget={*dynamic|off|0–24576}` under the hosts `anthropic` and `gemini` respectively, and the row `  grok-4.20                 thinking={on|*off}` under `openrouter`.
- R-VSOF-TJVB: `-h` and `--help` MUST each write `options.Usage()` to stdout, write nothing to stderr, and exit 0.
- R-VTWC-7BM0: A usage error MUST write `options.Usage()` to stderr, write nothing to stdout, and exit 2.
- R-VV48-L3CP: `-V` and `--version` MUST each write the `internal/cli` version string alone on a line — exactly that string followed by a single newline, with nothing else on stdout — and exit 0, with both spellings producing identical stdout and exit code.
- R-VWC4-YV3E: The version string MUST be a `v`-prefixed semantic version of the form `vMAJOR.MINOR.PATCH`, where each of MAJOR, MINOR, and PATCH is a non-negative integer with no leading zeros (matching `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).
