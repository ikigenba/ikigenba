package cli_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/browse"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/codex"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/proc"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

func TestPackageImportPaths(t *testing.T) {
	// R-YIMH-5RL1: this file compiles only while every package is importable at its module path.
	used := []any{
		cli.Run,
		quote.Arg,
		session.Table,
		tree.Draw,
		chat.Format,
		browse.Run,
		proc.Cwd,
		claude.List,
		codex.List,
		grok.List,
	}
	for i, v := range used {
		if v == nil {
			t.Errorf("identifier %d is nil", i)
		}
	}
}
