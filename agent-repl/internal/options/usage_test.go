package options_test

import (
	"strings"
	"testing"

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
