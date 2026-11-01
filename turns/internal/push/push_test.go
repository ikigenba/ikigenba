package push_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/push"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

func pending(id string, n int) push.Pending {
	turns := make([]session.Turn, n)
	for i := range turns {
		turns[i] = session.Turn{Moment: time.Unix(int64(i+1), 0).UTC(), Whose: session.WhoseAgent, Model: fmt.Sprint(i), Tokens: session.Tokens{Input: int64(i)}}
	}
	return push.Pending{Session: session.Session{Ref: session.Ref{Tool: session.ToolClaude, ID: id}, Parent: &session.Ref{Tool: session.ToolCodex, ID: "parent"}, Directory: "/work/" + id, Headless: true, Command: &session.Command{Name: "/task", Argument: id}, Turns: turns}, Latest: time.Unix(1, 0).UTC()}
}

// R-OI6W-XT0U R-OLUM-348X R-OPIB-8FH0 R-OQQ7-M77P R-ORY3-ZYYE R-OT60-DQP3 R-9AMO-8YVT R-9BUK-MQMI
func TestSelect(t *testing.T) {
	l := time.Unix(100, 0).UTC()
	a := pending("a", 4).Session
	a.Turns = []session.Turn{{Moment: l.Add(-time.Millisecond)}, {Moment: l, Whose: session.WhosePerson}, {Moment: l, Whose: session.WhoseTool}, {Moment: l.Add(time.Millisecond)}}
	b := pending("b", 2).Session
	c := pending("c", 1).Session
	d := pending("d", 1).Session
	input := []session.Session{a, b, c, d}
	held := []space.Held{{Ref: d.Ref, Latest: l}, {Ref: a.Ref, Latest: l}, {Ref: b.Ref, Latest: time.Time{}}}
	before := make([]session.Session, len(input))
	copy(before, input)
	for i := range before {
		before[i].Turns = append([]session.Turn(nil), before[i].Turns...)
	}
	heldBefore := append([]space.Held(nil), held...)
	api := struct {
		Select func([]session.Session, []space.Held) []push.Pending
	}{Select: push.Select}
	got := api.Select(input, held)
	want := []push.Pending{{Session: a, Latest: l}, {Session: b}, {Session: c}}
	want[0].Session.Turns = a.Turns[1:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(input, before) || !reflect.DeepEqual(held, heldBefore) {
		t.Fatal("selection mutated input")
	}
}

// R-ON2I-GVZM R-OVLT-5A6H R-OWTP-J1X6 R-OY1L-WTNV R-OZ9I-ALEK R-P0HE-OD59 R-P1PB-24VY R-P453-TODC R-P5D0-7G41
func TestBatches(t *testing.T) {
	cases := []struct {
		name  string
		input []push.Pending
		sizes [][]int
	}{
		{"nil", nil, nil}, {"empty", []push.Pending{}, nil},
		{"1000", []push.Pending{pending("a", 1000)}, [][]int{{1000}}},
		{"1001", []push.Pending{pending("a", 1001)}, [][]int{{1000}, {1}}},
		{"2500", []push.Pending{pending("a", 2500)}, [][]int{{1000}, {1000}, {500}}},
		{"mixed", []push.Pending{pending("a", 600), pending("b", 600), pending("c", 1)}, [][]int{{600, 400}, {200, 1}}},
	}
	for _, n := range []int{500, 501} {
		values := make([]push.Pending, n)
		sizes := make([]int, min(n, 500))
		for i := range values {
			turns := 1
			if n == 500 {
				turns = 2
			}
			values[i] = pending(fmt.Sprint(i), turns)
			if i < len(sizes) {
				sizes[i] = turns
			}
		}
		batches := [][]int{sizes}
		if n == 501 {
			batches = append(batches, []int{1})
		}
		cases = append(cases, struct {
			name  string
			input []push.Pending
			sizes [][]int
		}{fmt.Sprint(n), values, batches})
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			api := struct {
				Batches func([]push.Pending) [][]push.Pending
			}{Batches: push.Batches}
			got := api.Batches(tt.input)
			var sizes [][]int
			var joined []push.Pending
			for bi, batch := range got {
				if len(batch) == 0 || len(batch) > space.MaxPushSessions {
					t.Fatalf("invalid entries: %d", len(batch))
				}
				turns := 0
				seen := map[session.Ref]bool{}
				partSizes := []int{}
				for _, part := range batch {
					originalFound := false
					for _, original := range tt.input {
						if original.Session.Ref == part.Session.Ref {
							originalFound = true
							a, b := original, part
							a.Session.Turns = nil
							b.Session.Turns = nil
							if !reflect.DeepEqual(a, b) {
								t.Fatal("split entry lost session metadata or Latest")
							}
							break
						}
					}
					if !originalFound {
						t.Fatal("unknown session in batch")
					}
					n := len(part.Session.Turns)
					if n == 0 || seen[part.Session.Ref] {
						t.Fatal("empty or duplicate entry")
					}
					seen[part.Session.Ref] = true
					turns += n
					partSizes = append(partSizes, n)
					if len(joined) > 0 && joined[len(joined)-1].Session.Ref == part.Session.Ref {
						last := &joined[len(joined)-1]
						last.Session.Turns = append(last.Session.Turns, part.Session.Turns...)
					} else {
						copyPart := part
						copyPart.Session.Turns = append([]session.Turn(nil), part.Session.Turns...)
						joined = append(joined, copyPart)
					}
				}
				if turns > space.MaxPushTurns {
					t.Fatal("too many turns")
				}
				if bi < len(got)-1 && len(batch) != space.MaxPushSessions && turns != space.MaxPushTurns {
					t.Fatal("non-final batch not full")
				}
				if bi > 0 && batch[0].Session.Ref == got[bi-1][len(got[bi-1])-1].Session.Ref {
					previous := 0
					for _, part := range got[bi-1] {
						previous += len(part.Session.Turns)
					}
					if previous != space.MaxPushTurns {
						t.Fatal("split before turn limit")
					}
				}
				sizes = append(sizes, partSizes)
			}
			if !reflect.DeepEqual(sizes, tt.sizes) {
				t.Fatalf("sizes %v, want %v", sizes, tt.sizes)
			}
			if len(tt.input) > 0 && !reflect.DeepEqual(joined, tt.input) {
				t.Fatal("batches did not preserve all session data")
			}
		})
	}
}

// R-OJET-BKRJ R-OOAE-UNQB R-P6KW-L7UQ R-P7SS-YZLF R-P90P-CRC4
func TestCount(t *testing.T) {
	api := struct {
		Count func([][]push.Pending) map[session.Tool]push.Tally
	}{Count: push.Count}
	zero := api.Count(nil)
	if len(zero) != len(session.Tools()) {
		t.Fatal(zero)
	}
	for _, tool := range session.Tools() {
		if tally, ok := zero[tool]; !ok || tally != (push.Tally{Sessions: 0, Turns: 0}) {
			t.Fatal(zero)
		}
	}
	a := pending("a", 1001)
	a.Latest = time.Unix(500, 0).UTC()
	b := pending("b", 1)
	b.Latest = b.Session.Turns[0].Moment
	c := pending("c", 2)
	c.Session.Ref.Tool = session.ToolCodex
	c.Latest = time.Time{}
	c.Session.Turns[0].Moment = time.Time{}
	got := api.Count(push.Batches([]push.Pending{a, b, c}))
	want := map[session.Tool]push.Tally{session.ToolClaude: {Sessions: 1, Turns: 501}, session.ToolCodex: {Sessions: 1, Turns: 2}, session.ToolGrok: {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts %v, want %v", got, want)
	}
}
