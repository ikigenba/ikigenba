package cli

import (
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
)

type chatFollowRenderer struct {
	command    parsedCommand
	sys        System
	transcript *chat.Transcript
	footer     string
}

func newChatFollowRenderer(command parsedCommand, sys System) followRenderer {
	return &chatFollowRenderer{command: command, sys: sys}
}

func (r *chatFollowRenderer) render(first bool) (string, error) {
	var entries []chat.Entry
	if first || r.transcript.Path() == "" {
		result, err := renderCommand(r.command, r.sys)
		if err != nil {
			return "", err
		}
		r.transcript = result.transcript
		entries = result.entries
	} else {
		var err error
		entries, _, err = r.transcript.Read(r.sys.Root)
		if err != nil {
			return "", err
		}
	}
	var product strings.Builder
	for _, entry := range entries {
		product.WriteString(chat.Format(entry))
	}
	if !r.sys.Terminal {
		return product.String(), nil
	}
	footer := strings.TrimSuffix(chat.TotalsLine(r.transcript.Usage(), r.transcript.Recorded()), "\n")
	if first {
		r.footer = footer
		return "\x1b[?25l" + product.String() + footer, nil
	}
	if len(entries) == 0 && footer == r.footer {
		return "", nil
	}
	r.footer = footer
	return "\r\x1b[2K" + product.String() + footer, nil
}

func (r *chatFollowRenderer) interrupt() string {
	if r.sys.Terminal {
		return "\x1b[?25h\n"
	}
	return ""
}
