package session_test

import (
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/session"
)

// R-E90Y-BSQO
func TestToolValues(t *testing.T) {
	const (
		claude = session.ToolClaude
		codex  = session.ToolCodex
		grok   = session.ToolGrok
	)
	claudeValue, codexValue, grokValue := claude, codex, grok
	values := []struct {
		got  *session.Tool
		want string
	}{
		{&claudeValue, "claude"},
		{&codexValue, "codex"},
		{&grokValue, "grok"},
	}
	for _, value := range values {
		if string(*value.got) != value.want {
			t.Errorf("tool = %q, want %q", *value.got, value.want)
		}
	}
}

// R-EA8U-PKHD
func TestToolsSignature(_ *testing.T) {
	tools := []func() []session.Tool{session.Tools}
	_ = tools[0]()
}

// R-EBGR-3C82
func TestToolsOrder(t *testing.T) {
	want := []session.Tool{session.ToolClaude, session.ToolCodex, session.ToolGrok}
	if got := session.Tools(); !slices.Equal(got, want) {
		t.Errorf("Tools() = %v, want %v", got, want)
	}
}

// R-ECON-H3YR
func TestToolsFreshSlice(t *testing.T) {
	want := []session.Tool{session.ToolClaude, session.ToolCodex, session.ToolGrok}
	for index := range want {
		got := session.Tools()
		got[index] = session.Tool("changed")
		if next := session.Tools(); !slices.Equal(next, want) {
			t.Errorf("after changing index %d, Tools() = %v, want %v", index, next, want)
		}
	}
}

// R-EDWJ-UVPG
func TestWhoseValues(t *testing.T) {
	const (
		person = session.WhosePerson
		agent  = session.WhoseAgent
		tool   = session.WhoseTool
	)
	personValue, agentValue, toolValue := person, agent, tool
	values := []struct {
		got  *session.Whose
		want string
	}{
		{&personValue, "person"},
		{&agentValue, "agent"},
		{&toolValue, "tool"},
	}
	for _, value := range values {
		if string(*value.got) != value.want {
			t.Errorf("whose = %q, want %q", *value.got, value.want)
		}
	}
}
