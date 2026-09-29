package browse

import (
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type menuKey struct {
	id         string
	occurrence int
}
type menuRow struct {
	line    string
	key     menuKey
	session session.Session
	node    tree.Node
	harness int
}
type menuState struct {
	topLines, bottomLines []string
	rows                  []menuRow
	highlighted, top      int
	content               bool
	failure               string
	tree                  bool
}

func newMenu() *menuState { return &menuState{highlighted: -1} }
func rowKeys(rows []menuRow) []menuKey {
	keys := make([]menuKey, len(rows))
	for i := range rows {
		keys[i] = rows[i].key
	}
	return keys
}
func keyIndex(rows []menuRow, key menuKey) int {
	for i := range rows {
		if rows[i].key == key {
			return i
		}
	}
	return -1
}
func assignKeys(rows []menuRow) {
	counts := map[string]int{}
	for i := range rows {
		id := rows[i].key.id
		rows[i].key.occurrence = counts[id]
		counts[id]++
	}
}
func (m *menuState) replace(top []string, rows []menuRow, bottom []string) {
	assignKeys(rows)
	next := -1
	if len(rows) > 0 {
		next = 0
		if m.highlighted >= 0 && m.highlighted < len(m.rows) {
			next = keyIndex(rows, m.rows[m.highlighted].key)
			if next < 0 {
				below := make(map[menuKey]bool)
				for _, old := range m.rows[m.highlighted+1:] {
					below[old.key] = true
				}
				for i, row := range rows {
					if below[row.key] {
						next = i
						break
					}
				}
				if next < 0 {
					next = len(rows) - 1
				}
			}
		}
	}
	m.topLines, m.rows, m.bottomLines = top, rows, bottom
	m.highlighted = next
	m.content = true
}
func (m *menuState) restore(key menuKey, previous []menuKey) {
	m.highlighted = keyIndex(m.rows, key)
	if m.highlighted >= 0 {
		return
	}
	position := -1
	for i, k := range previous {
		if k == key {
			position = i
			break
		}
	}
	for i := position - 1; i >= 0; i-- {
		if index := keyIndex(m.rows, previous[i]); index >= 0 {
			m.highlighted = index
			return
		}
	}
	if len(m.rows) > 0 {
		m.highlighted = 0
	}
}
func (m *menuState) move(k key, room int) {
	if m.highlighted < 0 {
		return
	}
	switch k {
	case keyUp:
		m.highlighted = max(0, m.highlighted-1)
	case keyDown:
		m.highlighted = min(len(m.rows)-1, m.highlighted+1)
	case keyPageUp:
		m.highlighted = max(0, m.highlighted-room)
	case keyPageDown:
		m.highlighted = min(len(m.rows)-1, m.highlighted+room)
	case keyFirst:
		m.highlighted = 0
	case keyLast:
		m.highlighted = len(m.rows) - 1
	}
}
func menuRoom(level, rows int) int { return max(1, rows-3-level) }
func (m *menuState) body(level, rows int) []renderLine {
	if !m.content {
		return []renderLine{{text: m.failure}}
	}
	room := menuRoom(level, rows)
	limit := max(0, len(m.rows)-room)
	if m.highlighted >= 0 {
		m.top = min(max(m.top, m.highlighted-room+1), m.highlighted, limit)
	} else {
		m.top = min(m.top, limit)
	}
	var body []renderLine
	for _, line := range m.topLines {
		body = append(body, renderLine{text: line})
	}
	for i := m.top; i < min(m.top+room, len(m.rows)); i++ {
		body = append(body, renderLine{text: m.rows[i].line, tree: m.tree, highlighted: i == m.highlighted})
	}
	bottom := m.bottomLines
	if len(body)+len(bottom) > rows-3 && len(bottom) > 0 && bottom[0] == "" {
		bottom = bottom[1:]
	}
	for _, line := range bottom {
		body = append(body, renderLine{text: line, tree: m.tree})
	}
	return body[:min(len(body), rows-3)]
}
