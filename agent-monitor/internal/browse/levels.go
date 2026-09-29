package browse

import (
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/claude"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/codex"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/harness/grok"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type harnessAPI struct {
	name, arg, padding string
	list               func(fs.FS, string) ([]session.Session, error)
	tree               func(fs.FS, string, string) (tree.Tree, error)
	chat               func(fs.FS, string, string, string) (*chat.Transcript, []chat.Entry, error)
}

var browserHarnesses = [...]harnessAPI{
	{"Claude Code", "claude", "  ", claude.List, claude.Tree, claude.Chat},
	{"Codex", "codex", "        ", codex.List, codex.Tree, codex.Chat},
	{"Grok", "grok", "         ", grok.List, grok.Tree, grok.Chat},
}

type browserState struct {
	cfg            Config
	root           fs.FS
	level, harness int
	session        session.Session
	node           tree.Node
	menus          [3]*menuState
	openedKeys     [3]menuKey
	openedRows     [3][]menuKey
	chat           *chatState
	failure        string
	backRead       bool
}

func newBrowserState(cfg Config, root fs.FS) *browserState {
	b := &browserState{cfg: cfg, root: root}
	b.menus[0] = newMenu()
	return b
}
func (b *browserState) read() {
	if b.backRead {
		b.backRead = false
		defer b.menus[b.level].restore(b.openedKeys[b.level], b.openedRows[b.level])
	}
	switch b.level {
	case 0:
		rows := make([]menuRow, 3)
		for i, h := range browserHarnesses {
			s, err := h.list(b.root, b.cfg.Home)
			count := "-"
			if err == nil {
				count = strconv.Itoa(len(s))
			}
			rows[i] = menuRow{line: h.name + h.padding + count, key: menuKey{id: h.name}, harness: i}
		}
		b.menus[0].replace(nil, rows, nil)
	case 1:
		h := browserHarnesses[b.harness]
		s, err := h.list(b.root, b.cfg.Home)
		if err != nil {
			b.fail(err)
			return
		}
		s = session.OrderByStart(s)
		header, lines := session.TableRows(s)
		top := []string{header}
		if len(s) == 0 {
			top = append(top, "no live sessions")
		}
		rows := make([]menuRow, len(s))
		for i, item := range s {
			rows[i] = menuRow{line: lines[i], key: menuKey{id: item.ID}, session: item}
		}
		b.menus[1].replace(top, rows, nil)
	case 2:
		h := browserHarnesses[b.harness]
		t, err := h.tree(b.root, b.cfg.Home, b.session.ID)
		if err != nil {
			b.fail(err)
			return
		}
		lines := strings.Split(strings.TrimSuffix(tree.Draw(t, b.cfg.Color), "\n"), "\n")
		nodes := drawnNodes(t)
		rows := make([]menuRow, len(nodes))
		for i, node := range nodes {
			rows[i] = menuRow{line: lines[i], key: menuKey{id: node.ID}, node: node}
		}
		b.menus[2].tree = true
		b.menus[2].replace(nil, rows, lines[len(nodes):])
	case 3:
		h := browserHarnesses[b.harness]
		err := b.chat.read(b.root, func(root fs.FS) (*chat.Transcript, []chat.Entry, error) {
			return h.chat(root, b.cfg.Home, b.session.ID, b.node.ID)
		})
		if err != nil {
			b.fail(err)
		}
	}
}
func (b *browserState) fail(err error) {
	line := failureLine(err, browserHarnesses[b.harness].arg, b.session.ID, b.node.ID)
	if b.level == 3 {
		if !b.chat.hasContent() {
			b.failure = line
		}
	} else if !b.menus[b.level].content {
		b.menus[b.level].failure = line
	}
}
func failureLine(err error, h, sid, aid string) string {
	var read *session.ReadError
	switch {
	case errors.As(err, &read):
		return "cannot read " + quote.Field(read.Path) + ": " + read.Err.Error()
	case errors.Is(err, tree.ErrNotFound):
		return "no " + h + " session '" + quote.Arg(sid) + "'"
	case errors.Is(err, chat.ErrAgentNotFound):
		return "no " + h + " agent '" + quote.Arg(aid) + "' in session '" + quote.Arg(sid) + "'"
	default:
		return err.Error()
	}
}
func (b *browserState) apply(k key, cols, rows int) bool {
	if k == keyBack {
		if b.level == 0 {
			return false
		}
		b.level--
		b.backRead = true
		b.menus[b.level].restore(b.openedKeys[b.level], b.openedRows[b.level])
		return true
	}
	if b.level == 3 {
		if b.chat.hasContent() {
			b.chat.scroll(k, cols, rows)
		}
		return false
	}
	m := b.menus[b.level]
	if k != keyOpen {
		m.move(k, menuRoom(b.level, rows))
		return false
	}
	if m.highlighted < 0 {
		return false
	}
	row := m.rows[m.highlighted]
	b.openedKeys[b.level] = row.key
	b.openedRows[b.level] = rowKeys(m.rows)
	switch b.level {
	case 0:
		b.harness = row.harness
	case 1:
		b.session = row.session
	case 2:
		b.node = row.node
	}
	b.level++
	if b.level == 3 {
		b.chat = newChatState()
		b.failure = ""
	} else {
		b.menus[b.level] = newMenu()
	}
	return true
}
func (b *browserState) view(cols, rows int) screenView {
	crumb := "agent-monitor"
	if b.level > 0 {
		crumb += " › " + browserHarnesses[b.harness].name
	}
	if b.level > 1 {
		crumb += " › " + quote.Field(b.session.ID)
	}
	if b.level > 2 {
		crumb += " › " + quote.Field(b.node.ID)
	}
	view := screenView{breadcrumb: crumb}
	if b.level == 3 {
		if b.chat.hasContent() {
			view.body = b.chat.body(cols, rows)
		} else {
			lines := wrapLine(b.failure, cols)
			for _, line := range lines[:min(len(lines), rows-3)] {
				view.body = append(view.body, renderLine{text: line})
			}
		}
		status := "following"
		if !b.chat.following() {
			status = "paused"
		}
		view.hint = "↑↓ scroll  pgup/pgdn page  g/G top/bottom  ← back  q quit   [" + status + "]"
	} else {
		view.body = b.menus[b.level].body(b.level, rows)
		view.hint = "↑↓ move  → open  "
		if b.level > 0 {
			view.hint += "← back  "
		}
		view.hint += "q quit"
	}
	return view
}

// drawnNodes mirrors the drawing contract's node selection and traversal.
func drawnNodes(t tree.Tree) []tree.Node {
	nodes := map[string]tree.Node{}
	for _, n := range t.Subagents {
		if n.ID != t.Root.ID {
			if _, ok := nodes[n.ID]; !ok {
				nodes[n.ID] = n
			}
		}
	}
	children := map[string][]tree.Node{}
	for _, n := range nodes {
		parent := n.Parent
		cycle := false
		seen := map[string]bool{n.ID: true}
		current := n
		for current.Parent != "" && current.Parent != t.Root.ID {
			if current.Parent == n.ID {
				cycle = true
				break
			}
			if seen[current.Parent] {
				break
			}
			next, ok := nodes[current.Parent]
			if !ok {
				break
			}
			seen[current.Parent] = true
			current = next
		}
		_, exists := nodes[parent]
		if parent == "" || parent == t.Root.ID || cycle || !exists {
			parent = t.Root.ID
		}
		children[parent] = append(children[parent], n)
	}
	for p := range children {
		sort.Slice(children[p], func(i, j int) bool {
			a, z := children[p][i], children[p][j]
			if a.HasStarted != z.HasStarted {
				return a.HasStarted
			}
			if a.HasStarted {
				if c := a.Started.Compare(z.Started); c != 0 {
					return c < 0
				}
			}
			return a.ID < z.ID
		})
	}
	result := []tree.Node{t.Root}
	var walk func(string)
	walk = func(p string) {
		for _, n := range children[p] {
			result = append(result, n)
			walk(n.ID)
		}
	}
	walk(t.Root.ID)
	return result
}
