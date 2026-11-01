package session

import (
	"testing"
	"time"
)

// Positional literals prove each complete field list, while field access proves names.

// R-EGCC-MF6U
func TestRefShape(t *testing.T) {
	tool := ToolCodex
	id := "conversation"
	ref := Ref{tool, id}
	if ref.Tool != tool || ref.ID != id {
		t.Errorf("Ref = %#v", ref)
	}
}

// R-EHK9-06XJ
func TestCommandShape(t *testing.T) {
	name, argument := "/review", "changes"
	command := Command{name, argument}
	if command.Name != name || command.Argument != argument {
		t.Errorf("Command = %#v", command)
	}
}

// R-EIS5-DYO8
func TestTokensShape(t *testing.T) {
	var input, cached, output, reasoning int64 = 11, 7, 5, 3
	tokens := Tokens{input, cached, output, reasoning}
	if tokens.Input != input || tokens.Cached != cached || tokens.Output != output || tokens.Reasoning != reasoning {
		t.Errorf("Tokens = %#v", tokens)
	}
}

// R-EK01-RQEX
func TestTurnShape(t *testing.T) {
	moment := time.Date(2025, time.June, 12, 9, 30, 0, 0, time.UTC)
	whose := WhoseAgent
	model := "sample-model"
	tokens := Tokens{Input: 19, Cached: 13, Output: 11, Reasoning: 7}
	turn := Turn{moment, whose, model, tokens}
	if turn.Moment != moment || turn.Whose != whose || turn.Model != model || turn.Tokens != tokens {
		t.Errorf("Turn = %#v", turn)
	}
}

// R-EL7Y-5I5M
func TestSessionShape(t *testing.T) {
	ref := Ref{Tool: ToolClaude, ID: "child"}
	parent := &Ref{Tool: ToolClaude, ID: "parent"}
	directory := "/workspace"
	headless := true
	command := &Command{Name: "/review", Argument: "changes"}
	turns := []Turn{{Whose: WhosePerson, Model: "sample-model"}}
	session := Session{ref, parent, directory, headless, command, turns}
	if session.Ref != ref || session.Parent != parent || session.Directory != directory || session.Headless != headless || session.Command != command || len(session.Turns) != 1 || session.Turns[0] != turns[0] {
		t.Errorf("Session = %#v", session)
	}
}

// R-EMFU-J9WB
func TestFoundShape(t *testing.T) {
	sessions := []Session{{Ref: Ref{Tool: ToolGrok, ID: "conversation"}}}
	skipped := 2
	found := Found{sessions, skipped}
	if len(found.Sessions) != 1 || found.Sessions[0].Ref != sessions[0].Ref || found.Skipped != skipped {
		t.Errorf("Found = %#v", found)
	}
}

// R-ENNQ-X1N0
func TestMentionShape(t *testing.T) {
	path := "/dispatch/job"
	by := Ref{Tool: ToolClaude, ID: "conversation"}
	moment := time.Date(2025, time.June, 12, 9, 30, 0, 0, time.UTC)
	mention := Mention{path, by, moment}
	if mention.Path != path || mention.By != by || mention.Moment != moment {
		t.Errorf("Mention = %#v", mention)
	}
}
