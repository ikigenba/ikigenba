// Package render turns agent events and log records into user-facing output.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/agentkit"
)

// Decorated writes a human-readable transcript.
type Decorated struct {
	stdout    io.Writer
	stderr    io.Writer
	stdoutErr error
	stderrErr error
	toolName  map[string]string
}

// NewDecorated returns a transcript renderer writing to stdout and stderr.
func NewDecorated(stdout, stderr io.Writer) *Decorated {
	return &Decorated{
		stdout:   stdout,
		stderr:   stderr,
		toolName: make(map[string]string),
	}
}

// Prompt writes the input prompt.
func (d *Decorated) Prompt() {
	d.writeStdout("you › ")
}

// Begin separates the terminal-echoed input from the transcript.
func (d *Decorated) Begin() {
	d.writeStdout("\n")
}

// Event writes the transcript representation of ev.
func (d *Decorated) Event(ev agentkit.Event) {
	switch event := ev.(type) {
	case agentkit.MessageDone:
		d.message(event.Message)
	case *agentkit.MessageDone:
		if event != nil {
			d.message(event.Message)
		}
	case agentkit.ToolCall:
		d.toolCall(event.Use)
	case *agentkit.ToolCall:
		if event != nil {
			d.toolCall(event.Use)
		}
	case agentkit.ToolReturn:
		d.toolReturn(event.Result)
	case *agentkit.ToolReturn:
		if event != nil {
			d.toolReturn(event.Result)
		}
	case agentkit.OutputDone:
		return
	case *agentkit.OutputDone:
		return
	}
}

// Error writes a terminal turn error to stderr.
func (d *Decorated) Error(err error) {
	d.writeStderr("error › " + OneLine(err.Error()) + "\n\n")
}

// End terminates the final prompt line.
func (d *Decorated) End() {
	d.writeStdout("\n")
}

// Summary writes the final usage and cost summary.
func (d *Decorated) Summary(usage agentkit.Usage, cost agentkit.Cost) {
	cacheWrite := usage.CacheWrite5mTokens + usage.CacheWrite1hTokens
	total := usage.InputTokens + usage.CachedTokens + cacheWrite + usage.OutputTokens + usage.ReasoningTokens
	d.writeStdout(fmt.Sprintf(
		"summary\n· tokens  in=%d cache(r=%d w=%d) out=%d reasoning=%d total=%d\n· cost     $%.6f session\n",
		usage.InputTokens,
		usage.CachedTokens,
		cacheWrite,
		usage.OutputTokens,
		usage.ReasoningTokens,
		total,
		float64(cost)/1_000_000_000,
	))
}

// OneLine replaces line terminators with spaces while preserving all other text.
func OneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

func (d *Decorated) message(message agentkit.Message) {
	var text []string
	for _, block := range message.Blocks {
		if value, ok := block.(agentkit.Text); ok {
			text = append(text, value.Text)
		}
	}
	if len(text) == 0 {
		return
	}
	d.writeStdout("assistant › " + strings.Join(text, "\n") + "\n\n")
}

func (d *Decorated) toolCall(use agentkit.ToolUse) {
	name := OneLine(use.Name)
	d.toolName[use.ID] = name
	input := use.Input
	var compact bytes.Buffer
	if err := json.Compact(&compact, input); err == nil {
		input = compact.Bytes()
	} else {
		input = []byte(OneLine(string(input)))
	}
	d.writeStdout("tool › " + name + " " + string(input) + "\n\n")
}

func (d *Decorated) toolReturn(result agentkit.ToolResult) {
	name, ok := d.toolName[result.ToolUseID]
	if !ok {
		name = OneLine(result.ToolUseID)
	}
	errorPrefix := ""
	if result.IsError {
		errorPrefix = "error: "
	}
	d.writeStdout("result › " + name + " " + errorPrefix + OneLine(result.Content) + "\n\n")
}

func (d *Decorated) writeStdout(text string) {
	if d.stdoutErr != nil {
		return
	}
	_, d.stdoutErr = io.WriteString(d.stdout, text)
}

func (d *Decorated) writeStderr(text string) {
	if d.stderrErr != nil {
		return
	}
	_, d.stderrErr = io.WriteString(d.stderr, text)
}
