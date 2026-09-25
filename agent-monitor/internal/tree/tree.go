// Package tree formats a root session and its subagents as a text tree.
package tree

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
)

// Status describes a root session or subagent state.
type Status string

// Status values are the states reported by the harnesses.
const (
	StatusWorking Status = "working"
	StatusIdle    Status = "idle"
	StatusUnknown Status = "unknown"
	StatusEnded   Status = "ended"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusKilled  Status = "killed"
)

// Node holds one root session or subagent for drawing.
type Node struct {
	ID         string
	Parent     string
	Label      string
	Status     Status
	Started    time.Time
	HasStarted bool
}

// Tree holds a root session and a flat list of subagents.
type Tree struct {
	Root      Node
	Subagents []Node
}

// ErrNotFound reports that a requested root session does not exist.
var ErrNotFound = func() error { return errors.New("session not found") }()

// Draw returns one line for the root and one for each distinct subagent.
func Draw(t Tree, color bool) string {
	nodes := make(map[string]Node, len(t.Subagents))
	for _, node := range t.Subagents {
		if node.ID == t.Root.ID {
			continue
		}
		if _, exists := nodes[node.ID]; !exists {
			nodes[node.ID] = node
		}
	}

	children := make(map[string][]Node, len(nodes)+1)
	for _, node := range nodes {
		parent := node.Parent
		if parent == "" || parent == t.Root.ID || onParentCycle(node, nodes, t.Root.ID) {
			parent = t.Root.ID
		} else if _, exists := nodes[parent]; !exists {
			parent = t.Root.ID
		}
		children[parent] = append(children[parent], node)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			return before(children[parent][i], children[parent][j])
		})
	}

	var out strings.Builder
	counts := make(map[Status]int, len(statusOrder))
	writeLine(&out, "", t.Root, color, counts)
	var drawChildren func(string, string)
	drawChildren = func(parent, prefix string) {
		for i, node := range children[parent] {
			last := i == len(children[parent])-1
			connector, continuation := "├── ", "│   "
			if last {
				connector, continuation = "└── ", "    "
			}
			writeLine(&out, prefix+connector, node, color, counts)
			drawChildren(node.ID, prefix+continuation)
		}
	}
	drawChildren(t.Root.ID, "")
	out.WriteByte('\n')
	for i, status := range statusOrder {
		if i > 0 {
			out.WriteString("  ")
		}
		out.WriteString(dot(status, color))
		out.WriteByte(' ')
		out.WriteString(string(status))
		out.WriteString(" (")
		out.WriteString(strconv.Itoa(counts[status]))
		out.WriteByte(')')
	}
	out.WriteByte('\n')
	return out.String()
}

var statusOrder = [...]Status{
	StatusWorking, StatusIdle, StatusDone, StatusKilled,
	StatusFailed, StatusEnded, StatusUnknown,
}

func effectiveStatus(status Status) Status {
	switch status {
	case StatusWorking, StatusIdle, StatusDone, StatusKilled, StatusFailed, StatusEnded:
		return status
	default:
		return StatusUnknown
	}
}

func dot(status Status, color bool) string {
	if !color {
		return "●"
	}
	var code string
	switch status {
	case StatusWorking:
		code = "36"
	case StatusIdle:
		code = "34"
	case StatusDone:
		code = "32"
	case StatusKilled:
		code = "33"
	case StatusFailed:
		code = "31"
	case StatusEnded:
		code = "35"
	default:
		code = "90"
	}
	return "\x1b[" + code + "m●\x1b[0m"
}

func onParentCycle(start Node, nodes map[string]Node, rootID string) bool {
	current := start
	seen := map[string]bool{start.ID: true}
	for {
		parent := current.Parent
		if parent == "" || parent == rootID {
			return false
		}
		if parent == start.ID {
			return true
		}
		if seen[parent] {
			return false
		}
		next, exists := nodes[parent]
		if !exists {
			return false
		}
		seen[parent] = true
		current = next
	}
}

func before(a, b Node) bool {
	if a.HasStarted != b.HasStarted {
		return a.HasStarted
	}
	if a.HasStarted {
		if order := a.Started.Compare(b.Started); order != 0 {
			return order < 0
		}
	}
	return a.ID < b.ID
}

func writeLine(out *strings.Builder, prefix string, node Node, color bool, counts map[Status]int) {
	status := effectiveStatus(node.Status)
	counts[status]++
	out.WriteString(prefix)
	out.WriteString(dot(status, color))
	out.WriteString(" [")
	out.WriteString(quote.Field(node.ID))
	out.WriteByte(']')
	if node.Label != "" {
		out.WriteByte(' ')
		out.WriteString(quote.Field(node.Label))
	}
	if !color {
		out.WriteString("  ")
		out.WriteString(string(status))
	}
	out.WriteByte('\n')
}
