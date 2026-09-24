// Package session defines the shared session data and table used by harnesses.
package session

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
)

// Status is the observed state of a session.
type Status string

// The three statuses a harness can report.
const (
	StatusWorking Status = "working"
	StatusIdle    Status = "idle"
	StatusUnknown Status = "unknown"
)

// Session contains the facts displayed for one session.
type Session struct {
	ID            string
	Status        Status
	LastActive    time.Time
	HasLastActive bool
	CWD           string
	Title         string
}

// ReadError reports a registry read failure with its absolute path and cause.
type ReadError struct {
	Path string
	Err  error
}

func (e *ReadError) Error() string {
	return "cannot read " + e.Path + ": " + e.Err.Error()
}

// ErrNotJSON is the cause of an invalid JSON registry file.
var ErrNotJSON = func() error { return errors.New("not valid JSON") }()

// Lines returns newline-terminated lines, without their newline bytes.
func Lines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	return lines
}

// Table renders sessions in descending last-active order.
func Table(sessions []Session) string {
	rows := make([]Session, len(sessions))
	copy(rows, sessions)
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.HasLastActive != b.HasLastActive {
			return a.HasLastActive
		}
		if a.HasLastActive {
			if c := a.LastActive.Compare(b.LastActive); c != 0 {
				return c > 0
			}
		}
		return a.ID < b.ID
	})

	const columns = 5
	header := [columns]string{"SESSION", "STATUS", "LAST ACTIVE", "CWD", "TITLE"}
	widths := [columns]int{}
	for i, cell := range header {
		widths[i] = utf8.RuneCountInString(cell)
	}
	data := make([][columns]string, len(rows))
	for i, s := range rows {
		lastActive := "-"
		if s.HasLastActive {
			lastActive = s.LastActive.UTC().Format("2006-01-02T15:04:05Z")
		}
		data[i] = [columns]string{quote.Field(s.ID), string(s.Status), lastActive, quote.Field(s.CWD), quote.Field(s.Title)}
		for j, cell := range data[i] {
			widths[j] = max(widths[j], utf8.RuneCountInString(cell))
		}
	}
	var out strings.Builder
	writeRow := func(cells [columns]string) {
		last := columns - 1
		for last > 0 && cells[last] == "" {
			last--
		}
		for j := 0; j <= last; j++ {
			out.WriteString(cells[j])
			if j < last {
				for padding := widths[j] - utf8.RuneCountInString(cells[j]) + 2; padding > 0; padding-- {
					out.WriteByte(' ')
				}
			}
		}
		out.WriteByte('\n')
	}
	writeRow(header)
	for _, cells := range data {
		writeRow(cells)
	}
	return out.String()
}
