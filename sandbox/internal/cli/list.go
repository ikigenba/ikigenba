package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

func (r *invocation) runLS() int {
	p, err := r.roots(false)
	if err != nil {
		return r.report(err)
	}
	reg, err := readRegistry(p)
	if err != nil {
		return r.report(err)
	}
	if len(reg.Sandboxes) == 0 {
		return 0
	}
	sort.Slice(reg.Sandboxes, func(i, j int) bool { return reg.Sandboxes[i].Name < reg.Sandboxes[j].Name })
	rows := [][4]string{{"NAME", "PORT", "STATE", "WORKTREE"}}
	widths := [3]int{4, 4, 5}
	for _, entry := range reg.Sandboxes {
		up, err := r.sandboxUp(entry)
		if err != nil {
			return r.report(err)
		}
		state := "down"
		if up {
			state = "up"
		}
		worktree := entry.Worktree
		if _, err := os.Stat(worktree); errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
			worktree += " (gone)"
		}
		row := [4]string{entry.Name, strconv.Itoa(entry.Port), state, worktree}
		rows = append(rows, row)
		for i := range widths {
			if len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}
	for _, row := range rows {
		for i, width := range widths {
			cell := row[i]
			_, _ = fmt.Fprint(r.stdout, cell)
			_, _ = fmt.Fprint(r.stdout, strings.Repeat(" ", width-len(cell)+2))
		}
		_, _ = fmt.Fprintln(r.stdout, row[3])
	}
	return 0
}
