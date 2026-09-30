package session

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/quote"
)

// typed returns v as a T; a call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }

func TestAPI(t *testing.T) {
	// R-GNAW-66A5 R-GOIS-JY0U R-YNZ0-VH4Q R-GQYL-BHI8 R-YP6X-98VF R-GTEE-30ZM R-W4QW-UJZU R-GVU6-UKH0
	status := StatusWorking
	if status != "working" || StatusIdle != "idle" || StatusUnknown != "unknown" {
		t.Fatal("status declarations have wrong values")
	}
	started := time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC)
	active := started.Add(time.Minute)
	id := typed[string]("id")
	st := typed[Status](StatusIdle)
	hasStarted := typed[bool](true)
	hasLastActive := typed[bool](true)
	cwd := typed[string]("/work")
	title := typed[string]("title")
	startedAt := typed[time.Time](started)
	lastActive := typed[time.Time](active)
	s := Session{ID: id, Status: st, Started: startedAt, HasStarted: hasStarted, LastActive: lastActive, HasLastActive: hasLastActive, CWD: cwd, Title: title}
	if s.ID != id || s.Status != st || !s.Started.Equal(started) || !s.HasStarted || !s.LastActive.Equal(active) || !s.HasLastActive || s.CWD != cwd || s.Title != title {
		t.Fatalf("Session = %+v", s)
	}
	path := typed[string]("/p")
	cause := typed[error](fs.ErrPermission)
	re := ReadError{Path: path, Err: cause}
	if re.Path != path || !errors.Is(re.Err, cause) {
		t.Fatalf("ReadError = %+v", re)
	}
	if _, ok := any(ReadError{}).(error); ok {
		t.Fatal("ReadError value implements error")
	}
	if _, ok := any(&ReadError{}).(error); !ok {
		t.Fatal("*ReadError does not implement error")
	}
	errorOf := typed[func(*ReadError) string]((*ReadError).Error)
	table := typed[func([]Session) string](Table)
	lines := typed[func([]byte) [][]byte](Lines)
	_, _, _ = errorOf, table, lines
	if ErrNotJSON == nil {
		t.Fatal("ErrNotJSON is nil")
	}
}

func TestReadErrors(t *testing.T) {
	// R-GX23-8C7P R-GZHV-ZVP3
	got := (&ReadError{Path: "/home/dev/.claude/sessions", Err: fs.ErrPermission}).Error()
	if got != "cannot read /home/dev/.claude/sessions: permission denied" {
		t.Fatalf("ReadError.Error() = %q", got)
	}
	if ErrNotJSON.Error() != "not valid JSON" {
		t.Fatalf("ErrNotJSON = %q", ErrNotJSON)
	}
	if !errors.Is(ErrNotJSON, ErrNotJSON) {
		t.Fatal("ErrNotJSON is not a stable sentinel")
	}
}

func TestLines(t *testing.T) {
	// R-H1XO-RF6H
	for _, tc := range []struct {
		data string
		want []string
	}{
		{"", nil}, {`{"x":1`, nil}, {"a\n\nb\r\nc", []string{"a", "", "b\r"}}, {"\n", []string{""}}, {"x\nfragment", []string{"x"}},
	} {
		got := Lines([]byte(tc.data))
		if len(got) != len(tc.want) {
			t.Fatalf("Lines(%q) = %q", tc.data, got)
		}
		for i := range got {
			if string(got[i]) != tc.want[i] {
				t.Fatalf("Lines(%q)[%d] = %q", tc.data, i, got[i])
			}
		}
	}
}

func TestTable(t *testing.T) {
	// R-H35L-56X6 R-H4DH-IYNV R-H5LD-WQEK R-H6TA-AI59 R-H816-O9VY R-H993-21MN R-HAGZ-FTDC R-HBOV-TL41 R-HCWS-7CUQ
	const header = "SESSION  STATUS  LAST ACTIVE  CWD  TITLE\n"
	if got := Table(nil); got != header {
		t.Fatalf("empty table = %q", got)
	}
	if got := Table([]Session{}); got != header {
		t.Fatalf("non-nil empty table = %q", got)
	}
	base := time.Date(2026, 9, 23, 18, 52, 10, 0, time.FixedZone("offset", -5*3600))
	rows := []Session{
		{ID: "z", Status: StatusUnknown},
		{ID: "b", Status: StatusWorking, LastActive: base.Add(912 * time.Millisecond), HasLastActive: true, CWD: "é", Title: "Bob's\ttitle"},
		{ID: "a", Status: StatusIdle, LastActive: base.Add(999 * time.Millisecond), HasLastActive: true, CWD: "a\npath"},
		{ID: "a", Status: StatusWorking, LastActive: base.Add(999 * time.Millisecond), HasLastActive: true, CWD: "second"},
		{ID: "a", Status: StatusIdle},
	}
	before := append([]Session(nil), rows...)
	got := Table(rows)
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatalf("nonempty table must end in exactly one newline: %q", got)
	}
	if !reflect.DeepEqual(rows, before) {
		t.Fatal("Table changed caller's slice")
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != len(rows)+1 || !strings.HasPrefix(lines[0], "SESSION  ") {
		t.Fatalf("table lines = %q", got)
	}
	if strings.Contains(got, "\r") || strings.Contains(got, "\t") || strings.Contains(got, "a\npath") {
		t.Fatalf("unescaped table cell: %q", got)
	}
	for _, line := range lines {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("line has trailing spaces: %q", line)
		}
	}
	if !rowStarts(lines[1], "a", "idle") || !strings.Contains(lines[1], "2026-09-23T23:52:10Z") || !strings.Contains(lines[1], quote.Field("a\npath")) {
		t.Fatalf("newest row = %q", lines[1])
	}
	if !rowStarts(lines[2], "a", "working") || !strings.Contains(lines[2], "second") {
		t.Fatalf("stable tie row = %q", lines[2])
	}
	if !rowStarts(lines[3], "b", "working") || !strings.Contains(lines[3], quote.Field("Bob's\ttitle")) {
		t.Fatalf("older row = %q", lines[3])
	}
	if !rowStarts(lines[4], "a", "idle") || !rowStarts(lines[5], "z", "unknown") {
		t.Fatalf("unknown order = %q", got)
	}
	if !strings.HasSuffix(lines[4], "-") || !strings.HasSuffix(lines[5], "-") {
		t.Fatalf("empty trailing cells = %q", got)
	}
}

func TestTableExactCellsAndSpacing(t *testing.T) {
	// R-H35L-56X6 R-H5LD-WQEK R-H6TA-AI59 R-H816-O9VY
	when := time.Date(2026, 9, 23, 18, 52, 10, 912000000, time.FixedZone("offset", -5*3600))
	rows := []Session{
		{ID: "x", Status: StatusIdle, LastActive: when, HasLastActive: false, Title: "tail"},
		{ID: "é", Status: StatusWorking, LastActive: when, HasLastActive: true, CWD: "q\nr", Title: "Bob's"},
	}
	want := "SESSION  STATUS   LAST ACTIVE" + strings.Repeat(" ", 11) + "CWD   TITLE\n" +
		"é" + strings.Repeat(" ", 8) + "working  2026-09-23T23:52:10Z  q\\nr  Bob's\n" +
		"x" + strings.Repeat(" ", 8) + "idle" + strings.Repeat(" ", 5) + "-" + strings.Repeat(" ", 21) + strings.Repeat(" ", 6) + "tail\n"
	if got := Table(rows); got != want {
		t.Fatalf("Table = %q, want %q", got, want)
	}
}

func TestTableUsesRuneWidthsAndRawIDs(t *testing.T) {
	// R-H816-O9VY R-HAGZ-FTDC
	rows := []Session{
		{ID: string([]byte{0xff}), Status: StatusIdle},
		{ID: "é", Status: StatusIdle},
		{ID: "z", Status: StatusIdle},
	}
	want := "SESSION  STATUS  LAST ACTIVE  CWD  TITLE\n" +
		"z        idle    -\n" +
		"é        idle    -\n" +
		"\\xff     idle    -\n"
	if got := Table(rows); got != want {
		t.Fatalf("Table = %q, want %q", got, want)
	}
}

func rowStarts(line, id, status string) bool {
	fields := strings.Fields(line)
	return len(fields) >= 2 && fields[0] == id && fields[1] == status
}
