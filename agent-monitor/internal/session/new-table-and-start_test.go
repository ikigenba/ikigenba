package session

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewTableAndStartSignatures(_ *testing.T) {
	// R-MTJZ-93J5 R-KFTE-HPU0
	orderByStart := typed[func([]Session) []Session](OrderByStart)
	tableRows := typed[func([]Session) (string, []string)](TableRows)
	_, _ = orderByStart, tableRows
}

func TestNewTableRows(t *testing.T) {
	// R-KH1A-VHKP R-KI97-99BE
	when := time.Date(2026, 9, 23, 1, 2, 3, 999999999, time.FixedZone("offset", -18000))
	input := []Session{
		{ID: "z", Status: StatusIdle, CWD: "é\npath", Title: "tail\tend"},
		{ID: "a", Status: StatusWorking, LastActive: when, HasLastActive: true},
		{ID: "a", Status: StatusWorking, LastActive: when, HasLastActive: true, Title: "duplicate"},
		{ID: string([]byte{0xff}), Status: StatusUnknown},
	}
	before := append([]Session(nil), input...)
	header, rows := TableRows(input)
	table := strings.Split(strings.TrimSuffix(Table(input), "\n"), "\n")
	want := []string{table[3], table[1], table[2], table[4]}
	if header != table[0] || !reflect.DeepEqual(rows, want) {
		t.Fatalf("header %q rows %q; table %q", header, rows, table)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("TableRows changed input")
	}
	for _, empty := range [][]Session{nil, {}} {
		h, r := TableRows(empty)
		if h != "SESSION  STATUS  LAST ACTIVE  CWD  TITLE" || len(r) != 0 {
			t.Fatalf("empty: %q %q", h, r)
		}
	}
}

func TestNewOrderByStartCopy(t *testing.T) {
	// R-MURV-MV9U
	input := []Session{{ID: "b"}, {ID: "a"}, {ID: "b"}}
	before := append([]Session(nil), input...)
	got := OrderByStart(input)
	counts := map[Session]int{}
	for _, s := range input {
		counts[s]++
	}
	for _, s := range got {
		counts[s]--
	}
	if len(got) != len(input) || !reflect.DeepEqual(input, before) {
		t.Fatal("length or input changed")
	}
	for _, n := range counts {
		if n != 0 {
			t.Fatal("multiplicity changed")
		}
	}
	got[0] = Session{ID: "changed"}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("returned backing array shared")
	}
	for _, empty := range [][]Session{nil, {}} {
		if len(OrderByStart(empty)) != 0 {
			t.Fatal("nonempty result")
		}
	}
}

func TestNewOrderByStartOrdering(t *testing.T) {
	// R-MVZS-0N0J R-MX7O-EER8
	base := time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC)
	input := []Session{
		{ID: "z", Started: base.Add(-time.Hour)},
		{ID: "b", Started: base, HasStarted: true},
		{ID: "c", Started: base.Add(time.Hour)},
		{ID: "a", Started: base.In(time.FixedZone("offset", -18000)), HasStarted: true, Title: "first"},
		{ID: "d", Started: base.Add(-time.Second), HasStarted: true},
		{ID: "a", Started: base, HasStarted: true, Title: "second"},
		{ID: "early", Started: base.Add(-time.Nanosecond), HasStarted: true},
		{ID: "é"}, {ID: string([]byte{0xff})},
		{ID: "z", Started: base.Add(-2 * time.Hour), Title: "second unknown"},
	}
	want := []Session{input[4], input[6], input[3], input[5], input[1], input[2], input[0], input[9], input[7], input[8]}
	if got := OrderByStart(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %+v", got)
	}
}
