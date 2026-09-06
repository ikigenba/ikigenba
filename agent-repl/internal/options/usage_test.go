package options_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-repl/internal/help"
	"github.com/ikigenba/ikigenba/agent-repl/internal/options"
)

const fixedUsageHead = `usage: agent-repl [-c key=value ...] [-raw] [-V] [-h]

flags:
  -c key=value   set a config value (repeatable, last wins)
  -raw           emit the raw, undecorated message stream
  -V, --version  show the version and exit
  -h, --help     show this catalog and exit

defaults:
  provider=openai   model=gpt-5.6-sol   auth=oauth

providers:
`

const fixedUsageTrailer = `a bare -c model=NAME must name a cataloged model (its provider is derived).
any other model needs -c provider=... too and is priced at 0.
every other -c key is passed to the wire as an option; unknown keys fail at send.
-c base_url=URL sends requests to URL instead of the provider's endpoint.
-c system_file=PATH sends the file's contents as the system prompt before the first message.
`

// R-VE1N-8AYZ
func TestUsageIsExportedAndReturnsText(t *testing.T) {
	if got := options.Usage(); got == "" {
		t.Fatal("Usage() returned an empty string")
	}
}

// R-VGHF-ZUGD
func TestUsageBeginsWithFixedHeadByteForByte(t *testing.T) {
	got := options.Usage()
	if !strings.HasPrefix(got, fixedUsageHead) {
		prefixLength := min(len(got), len(fixedUsageHead))
		t.Fatalf("Usage() head = %q, want %q", got[:prefixLength], fixedUsageHead)
	}
}

// R-NHOS-5G50
func TestUsageHasExactOrderedComposition(t *testing.T) {
	// Providers and Models have independent byte-for-byte tests in internal/help;
	// this assertion owns the fixed text and the ordering of those renderers.
	want := fixedUsageHead + help.Providers() + "\n\n" + help.Models() + "\n\n" + fixedUsageTrailer
	if got := options.Usage(); got != want {
		t.Fatalf("Usage() = %q, want %q", got, want)
	}
}
