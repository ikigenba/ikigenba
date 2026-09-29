package browse

import (
	"io/fs"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
)

type chatAnchor struct{ line, offset int }
type chatState struct {
	transcript *chat.Transcript
	entries    []chat.Entry
	follows    bool
	anchor     chatAnchor
}

func newChatState() *chatState        { return &chatState{follows: true} }
func (c *chatState) hasContent() bool { return c.transcript != nil }
func (c *chatState) following() bool  { return c.follows }
func (c *chatState) read(root fs.FS, open func(fs.FS) (*chat.Transcript, []chat.Entry, error)) error {
	var entries []chat.Entry
	if c.transcript == nil || c.transcript.Path() == "" {
		transcript, gained, err := open(root)
		if err != nil {
			return err
		}
		c.transcript = transcript
		entries = gained
	} else {
		gained, _, err := c.transcript.Read(root)
		if err != nil {
			return err
		}
		entries = gained
	}
	c.entries = append(c.entries, entries...)
	return nil
}

type chatLayout struct {
	rows, totals []string
	anchors      []chatAnchor
	page, end    int
}

func (c *chatState) layout(cols, rows int) chatLayout {
	var l chatLayout
	var text strings.Builder
	for _, entry := range c.entries {
		text.WriteString(chat.Format(entry))
	}
	if text.Len() > 0 {
		lines := strings.Split(strings.TrimSuffix(text.String(), "\n"), "\n")
		for a, line := range lines {
			wrapped := wrapLine(line, cols)
			for i, row := range wrapped {
				l.rows = append(l.rows, row)
				l.anchors = append(l.anchors, chatAnchor{line: a, offset: i * cols})
			}
		}
	}
	totals := strings.TrimSuffix(chat.TotalsLine(c.transcript.Usage(), c.transcript.Recorded()), "\n")
	l.totals = wrapLine(totals, cols)
	if len(l.totals) > rows-4 {
		l.totals = []string{totals}
	}
	l.page = max(0, rows-3-len(l.totals))
	l.end = max(0, len(l.rows)-l.page)
	return l
}
func (c *chatState) top(l chatLayout, cols int) int {
	if c.follows {
		return l.end
	}
	r := 0
	for i, anchor := range l.anchors {
		if anchor.line == c.anchor.line && anchor.offset/cols == c.anchor.offset/cols {
			r = i
			break
		}
	}
	if r >= l.end {
		c.follows = true
		return l.end
	}
	return r
}
func (c *chatState) body(cols, rows int) []renderLine {
	if !c.hasContent() {
		return nil
	}
	l := c.layout(cols, rows)
	top := c.top(l, cols)
	body := make([]renderLine, 0, rows-3)
	for _, row := range l.rows[top:min(top+l.page, len(l.rows))] {
		body = append(body, renderLine{text: row})
	}
	for _, row := range l.totals {
		body = append(body, renderLine{text: row})
	}
	return body[:min(len(body), rows-3)]
}
func (c *chatState) scroll(k key, cols, rows int) {
	if !c.hasContent() || cols < MinCols || rows < MinRows {
		return
	}
	switch k {
	case keyUp, keyDown, keyPageUp, keyPageDown, keyFirst, keyLast:
	default:
		return
	}
	l := c.layout(cols, rows)
	target := c.top(l, cols)
	switch k {
	case keyUp:
		target--
	case keyDown:
		target++
	case keyPageUp:
		target -= l.page
	case keyPageDown:
		target += l.page
	case keyFirst:
		target = 0
	case keyLast:
		target = l.end
	}
	target = max(0, min(target, l.end))
	c.follows = target == l.end
	if !c.follows {
		c.anchor = l.anchors[target]
	}
}
