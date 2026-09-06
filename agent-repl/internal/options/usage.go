package options

import "github.com/ikigenba/ikigenba/agent-repl/internal/help"

const usageHead = `usage: agent-repl [-c key=value ...] [-raw] [-V] [-h]

flags:
  -c key=value   set a config value (repeatable, last wins)
  -raw           emit the raw, undecorated message stream
  -V, --version  show the version and exit
  -h, --help     show this catalog and exit

defaults:
  provider=openai   model=gpt-5.6-sol   auth=oauth

providers:
`

const usageTrailer = `a bare -c model=NAME must name a cataloged model (its provider is derived).
any other model needs -c provider=... too and is priced at 0.
every other -c key is passed to the wire as an option; unknown keys fail at send.
-c base_url=URL sends requests to URL instead of the provider's endpoint.
-c system_file=PATH sends the file's contents as the system prompt before the first message.
`

// Usage returns the complete command-line usage catalog.
func Usage() string {
	return usageHead + help.Providers() + "\n\n" + help.Models() + "\n\n" + usageTrailer
}
