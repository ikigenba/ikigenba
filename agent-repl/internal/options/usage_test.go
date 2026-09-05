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

const fixedUsageTrailer = `a bare -c model=NAME must name a cataloged model (its provider is derived).
any other model needs -c provider=... too and is priced at 0.
every other -c key is passed to the wire as an option; unknown keys fail at send.
-c base_url=URL sends requests to URL instead of the provider's endpoint.
`

const fixedUsageCatalog = `  anthropic    auth=api_key  (ANTHROPIC_API_KEY)
  gemini       auth=api_key  (GEMINI_API_KEY)
  openai       auth=api_key  (OPENAI_API_KEY)
               auth=oauth    (auth_file=~/.agent-repl/openai-auth.json)
  openrouter   auth=api_key  (OPENROUTER_API_KEY)
  xai          auth=api_key  (XAI_API_KEY)
               auth=oauth    (auth_file=~/.agent-repl/xai-auth.json)

anthropic
  claude-fable-5            effort={low|*medium|high|xhigh|max}
  claude-haiku-4-5          thinking_budget={*off|1024–4096}
  claude-opus-4-8           effort={low|medium|*high|xhigh|max|off}
  claude-opus-5             effort={low|*medium|high|xhigh|max|off}
  claude-sonnet-4-6         effort={low|medium|*high|max|off}
  claude-sonnet-5           effort={low|*medium|high|xhigh|max|off}

gemini
  gemini-2.5-flash          thinking_budget={*dynamic|off|0–24576}
  gemini-2.5-pro            thinking_budget={*dynamic|128–32768}
  gemini-3.1-flash-lite     thinking_level={minimal|low|*medium|high}
  gemini-3.1-pro-preview    thinking_level={low|medium|*high}
  gemini-3.5-flash          thinking_level={minimal|low|*medium|high}
  gemini-3.7-flash          thinking_level={low|*medium|high}

openai
  gpt-5.4                   effort={*none|low|medium|high|xhigh|off}
  gpt-5.4-mini              effort={*none|low|medium|high|xhigh|off}
  gpt-5.4-nano              effort={*none|low|medium|high|xhigh|off}
  gpt-5.5                   effort={none|low|*medium|high|xhigh|off}
  gpt-5.5-pro               effort={*high|xhigh}
  gpt-5.6-luna              effort={none|low|*medium|high|xhigh|off}
  gpt-5.6-sol               effort={none|low|*medium|high|xhigh|off}
  gpt-5.6-terra             effort={none|low|*medium|high|xhigh|off}

openrouter
  claude-fable-5            effort={low|*medium|high|xhigh|max}
  claude-haiku-4-5          thinking_budget={*off|1024–4096}
  claude-opus-4-8           effort={low|medium|*high|xhigh|max|off}
  claude-opus-5             effort={low|*medium|high|xhigh|max|off}
  claude-sonnet-4-6         effort={low|medium|*high|max|off}
  claude-sonnet-5           effort={low|*medium|high|xhigh|max|off}
  deepseek-v4-flash         thinking={*dynamic|on|off}
  deepseek-v4-pro           thinking={*on|off}
  gemini-2.5-flash          thinking_budget={*dynamic|off|0–24576}
  gemini-2.5-pro            thinking_budget={*dynamic|128–32768}
  gemini-3.1-flash-lite     thinking_level={minimal|low|*medium|high|off}
  gemini-3.1-pro-preview    thinking_level={low|medium|*high}
  gemini-3.5-flash          thinking_level={minimal|low|*medium|high}
  gemini-3.7-flash          thinking_level={low|*medium|high}
  glm-4.6                   thinking={*on|off}
  glm-4.7                   thinking={*on|off}
  glm-5.1                   thinking={*on|off}
  glm-5.2                   effort={*high|xhigh|off}
  gpt-5.4                   effort={*none|low|medium|high|xhigh|off}
  gpt-5.4-mini              effort={*none|low|medium|high|xhigh|off}
  gpt-5.4-nano              effort={*none|low|medium|high|xhigh|off}
  gpt-5.5                   effort={none|low|*medium|high|xhigh|off}
  gpt-5.5-pro               effort={*high|xhigh}
  gpt-5.6-luna              effort={none|low|*medium|high|xhigh|off}
  gpt-5.6-sol               effort={none|low|*medium|high|xhigh|off}
  gpt-5.6-terra             effort={none|low|*medium|high|xhigh|off}
  grok-4.20                 thinking={on|*off}
  grok-4.20-multi-agent     effort={low|medium|*high|xhigh}
  grok-4.3                  thinking={*on|off}
  grok-4.5                  effort={low|medium|*high}
  grok-4.6                  effort={low|medium|*high|xhigh}
  kimi-k2.6                 thinking={*on|off}
  kimi-k2.7-code            thinking={*on}
  kimi-k3                   thinking={*on|off}
  nemotron-3.5-lightning    thinking={*on|off}
  qwen3.8-27b               effort={low|medium|*xhigh|off}
  qwen3.8-max               effort={low|medium|*xhigh}

xai
  grok-4.20                 thinking={*on}
  grok-4.20-multi-agent     effort={low|medium|*high|xhigh}
  grok-4.3                  effort={*low|medium|high}
  grok-4.5                  effort={low|medium|*high}
  grok-4.6                  effort={low|medium|*high|xhigh}

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

// R-VHPC-DM72
func TestUsageHasExactOrderedComposition(t *testing.T) {
	want := fixedUsageHead + fixedUsageCatalog + fixedUsageTrailer
	if got := options.Usage(); got != want {
		t.Fatalf("Usage() = %q, want %q", got, want)
	}
}
