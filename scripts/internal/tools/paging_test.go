package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

func TestPagingMemberDescriptions(t *testing.T) {
	// R-GZ3V-0SF0 R-H2RK-63N3 R-H57C-XN4H R-H6F9-BEV6 R-H7N5-P6LV R-HA2Y-GQ39 R-HBAU-UHTY R-HCIR-89KN
	for _, tt := range []struct {
		v                  any
		field, description string
	}{
		{tools.ListArgs{}, "Page", paging.PageDescription}, {tools.ListArgs{}, "Limit", paging.LimitDescription},
		{tools.RunsArgs{}, "Page", paging.PageDescription}, {tools.RunsArgs{}, "Limit", paging.LimitDescription},
		{tools.ScriptList{}, "Next", paging.NextDescription}, {tools.RunList{}, "Next", paging.NextDescription},
	} {
		f, ok := reflect.TypeOf(tt.v).FieldByName(tt.field)
		if !ok || f.Tag.Get("description") != tt.description {
			t.Fatalf("%T.%s description", tt.v, tt.field)
		}
	}
	h := setup(t, "print(1)\n")
	infos, err := h.client.ListTools(context.Background(), caller())
	must(t, err)
	for _, info := range infos {
		if info.Name != "list" && info.Name != "runs" {
			continue
		}
		for _, schema := range []struct {
			raw  json.RawMessage
			want map[string]string
		}{
			{info.InputSchema, map[string]string{"page": paging.PageDescription, "limit": paging.LimitDescription}},
			{info.OutputSchema, map[string]string{"next": paging.NextDescription}},
		} {
			var value struct {
				Properties map[string]struct{ Description string }
			}
			must(t, json.Unmarshal(schema.raw, &value))
			for name, want := range schema.want {
				if value.Properties[name].Description != want {
					t.Fatal(info.Name, name, value)
				}
			}
		}
	}
}

func TestPagingRefusalsBeforeCatalogAndName(t *testing.T) {
	// R-HDQN-M1BC R-HEYJ-ZT21 R-A2H0-0SGP
	h := setup(t, "print(1)\n")
	h.create("nightly-report")
	before := snapshot(t, h)
	listToken, runsToken := paging.Token{Tool: "list"}, paging.Token{Tool: "runs"}
	cases := []struct{ tool, args, want string }{}
	for _, tool := range []string{"list", "runs"} {
		for _, n := range []int{-1, 0, paging.MaxLimit + 1} {
			_, err := paging.Window(&n)
			args := fmt.Sprintf(`{"page":"bogus","limit":%d}`, n)
			if tool == "runs" {
				args = fmt.Sprintf(`{"name":"Nightly Report","page":"bogus","limit":%d}`, n)
			}
			cases = append(cases, struct{ tool, args, want string }{tool, args, err.Error()})
		}
	}
	for _, p := range []string{"bogus", listToken.Encode(int64(7)), listToken.Encode("a", "b"), runsToken.Encode("nightly-report")} {
		cases = append(cases, struct{ tool, args, want string }{"list", fmt.Sprintf(`{"page":%q}`, p), paging.BadPage})
	}
	for _, p := range []string{"bogus", runsToken.Encode("2026-10-05T09:00:07Z"), runsToken.Encode(int64(1759654807), "run_5a5a5a5a5a5a5a5a"), listToken.Encode("2026-10-05T09:00:07Z", "run_5a5a5a5a5a5a5a5a")} {
		cases = append(cases, struct{ tool, args, want string }{"runs", fmt.Sprintf(`{"name":"nope","page":%q}`, p), paging.BadPage})
	}
	for _, failing := range []bool{false, true} {
		h.db.SetFailing(failing)
		for _, c := range cases {
			refusal(t, h.raw(c.tool, c.args, caller()), c.want)
		}
		h.db.SetFailing(false)
		unchanged(t, h, before)
	}
	for _, args := range []string{`{}`, `{"page":null,"limit":null}`, `{"page":""}`} {
		assertWire(t, h.raw("list", args, caller()), `{"scripts":[`+expectedScript(t, before.Scripts[0], false)+`]}`)
	}
	for _, args := range []string{`{"name":"nightly-report"}`, `{"name":"nightly-report","page":null,"limit":null}`, `{"name":"nightly-report","page":""}`} {
		assertWire(t, h.raw("runs", args, caller()), `{"runs":[]}`)
	}
}

func TestListPagingTraversalAndPositions(t *testing.T) {
	// R-HHEC-RCJF R-HIM9-54A4 R-HJU5-IW0T
	h := setup(t, "print(1)\n")
	ctx := context.Background()
	names := []string{"a-b", "a1", "aa"}
	for i := len(names); i < paging.DefaultLimit; i++ {
		names = append(names, fmt.Sprintf("job-%03d", i))
	}
	for _, name := range names {
		_, err := h.st.Create(ctx, store.Draft{Owner: "alice", Name: name, Repo: "rep_1111111111111111", Ref: "main"})
		must(t, err)
	}
	foreign, err := h.st.Create(ctx, store.Draft{Owner: "bob", Name: "foreign", Repo: "rep_1111111111111111", Ref: "main"})
	must(t, err)
	_ = foreign
	must(t, os.RemoveAll(h.repo))
	assertWire(t, h.raw("list", `{}`, identity.Caller{UserID: "nobody"}), `{"scripts":[]}`)
	first := decode[tools.ScriptList](t, h.call("list", tools.ListArgs{}))
	if len(first.Scripts) != paging.DefaultLimit || first.Next != nil {
		t.Fatal(first)
	}
	names = append(names, "zulu")
	_, err = h.st.Create(ctx, store.Draft{Owner: "alice", Name: "zulu", Repo: "rep_1111111111111111", Ref: "main"})
	must(t, err)
	defaultPage := decode[tools.ScriptList](t, h.call("list", tools.ListArgs{}))
	if len(defaultPage.Scripts) != paging.DefaultLimit || defaultPage.Next == nil {
		t.Fatal("default first page", defaultPage)
	}
	for i, entry := range defaultPage.Scripts {
		if entry.Name != names[i] {
			t.Fatal("default page name", entry.Name, names[i])
		}
	}
	expectedNext := (paging.Token{Tool: "list"}).Encode(names[paging.DefaultLimit-1])
	if *defaultPage.Next != expectedNext {
		t.Fatal("default position", *defaultPage.Next, expectedNext)
	}
	defaultLastResult := h.call("list", tools.ListArgs{Page: defaultPage.Next})
	defaultLast := decode[tools.ScriptList](t, defaultLastResult)
	if len(defaultLast.Scripts) != 1 || defaultLast.Scripts[0].Name != names[paging.DefaultLimit] || defaultLast.Next != nil {
		t.Fatal("default last page", defaultLast)
	}
	var defaultLastMembers map[string]json.RawMessage
	must(t, json.Unmarshal(object(t, defaultLastResult), &defaultLastMembers))
	if _, ok := defaultLastMembers["next"]; ok {
		t.Fatal("default terminal next present")
	}
	for _, n := range []int{1, 2, paging.DefaultLimit, paging.MaxLimit} {
		var page *string
		var got []string
		for count := 0; ; count++ {
			if count > len(names) {
				t.Fatal("nonterminating pages")
			}
			result := h.call("list", tools.ListArgs{Page: page, Limit: &n})
			v := decode[tools.ScriptList](t, result)
			remainder := names[len(got):]
			pageSize := min(n, len(remainder))
			if len(v.Scripts) != pageSize || (v.Next != nil) != (len(remainder) > n) {
				t.Fatalf("limit %d: page length %d, next %v; remainder %d", n, len(v.Scripts), v.Next, len(remainder))
			}
			for i, entry := range v.Scripts {
				if entry.Name != remainder[i] {
					t.Fatal("page name", entry.Name, remainder[i])
				}
			}
			for _, s := range v.Scripts {
				got = append(got, s.Name)
				stored, e := h.st.Find(ctx, "alice", s.Name)
				must(t, e)
				b, _ := json.Marshal(s)
				var want any
				must(t, json.Unmarshal([]byte(expectedScript(t, stored, false)), &want))
				var actual any
				must(t, json.Unmarshal(b, &actual))
				delete(actual.(map[string]any), "last_run")
				if !reflect.DeepEqual(actual, want) {
					t.Fatal(actual, want)
				}
			}
			if v.Next == nil {
				var members map[string]json.RawMessage
				must(t, json.Unmarshal(object(t, result), &members))
				if _, ok := members["next"]; ok {
					t.Fatal("terminal next present")
				}
				break
			}
			last := v.Scripts[len(v.Scripts)-1]
			if *v.Next != (paging.Token{Tool: "list"}).Encode(last.Name) {
				t.Fatal("wrong position")
			}
			page = v.Next
		}
		if !reflect.DeepEqual(got, names) {
			t.Fatal(got, names)
		}
	}
	for _, pos := range []string{"-", "\xff", "a0"} {
		p := (paging.Token{Tool: "list"}).Encode(pos)
		v := decode[tools.ScriptList](t, h.call("list", tools.ListArgs{Page: &p}))
		var want []string
		for _, n := range names {
			if n > pos {
				want = append(want, n)
			}
		}
		if len(want) > paging.DefaultLimit {
			want = want[:paging.DefaultLimit]
		}
		var got []string
		for _, s := range v.Scripts {
			got = append(got, s.Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(pos, got, want)
		}
	}
}

func TestRunsPagingTraversalAndPositions(t *testing.T) {
	// R-HHEC-RCJF R-HL21-WNRI R-HM9Y-AFI7 R-HNHU-O78W
	h := setup(t, "print(1)\n")
	ctx := context.Background()
	create := func(owner, name string) store.Script {
		s, e := h.st.Create(ctx, store.Draft{Owner: owner, Name: name, Repo: "rep_1111111111111111", Ref: "main"})
		must(t, e)
		return s
	}
	sc := create("alice", "nightly-report")
	other := create("alice", "other")
	foreign := create("bob", "foreign")
	add := func(sc store.Script, n int) []store.Run {
		rr := []store.Run{}
		for i := 0; i < n; i++ {
			started := time.Date(2026, 10, 5, 9, 0, i/3, 0, time.UTC)
			r, e := h.st.AddRun(ctx, store.Run{ID: fmt.Sprintf("run_%016x", 1000+n*100+i*7%n), Script: sc.ID, Ref: "main", User: sc.Owner, Trigger: store.TriggerManual, Status: store.StatusFailed, Reason: store.ReasonCommitMissing, Started: started, Finished: started})
			must(t, e)
			rr = append(rr, r)
		}
		sort.Slice(rr, func(i, j int) bool {
			return rr[i].Started.After(rr[j].Started) || rr[i].Started.Equal(rr[j].Started) && rr[i].ID > rr[j].ID
		})
		return rr
	}
	empty := decode[tools.RunList](t, h.call("runs", tools.RunsArgs{Name: sc.Name}))
	if len(empty.Runs) != 0 || empty.Next != nil {
		t.Fatal(empty)
	}
	want := add(sc, 30)
	add(other, 5)
	add(foreign, 1)
	refusal(t, h.call("runs", tools.RunsArgs{Name: foreign.Name}), fmt.Sprintf(tools.MissingScript, foreign.Name))
	refusal(t, h.call("runs", tools.RunsArgs{Name: "absent"}), fmt.Sprintf(tools.MissingScript, "absent"))
	n := 4
	var page *string
	var got []string
	answers := 0
	for {
		answers++
		if answers > 30 {
			t.Fatal("nonterminating pages")
		}
		result := h.call("runs", tools.RunsArgs{Name: sc.Name, Page: page, Limit: &n})
		v := decode[tools.RunList](t, result)
		remainder := want[len(got):]
		pageSize := min(n, len(remainder))
		if len(v.Runs) != pageSize || (v.Next != nil) != (len(remainder) > n) {
			t.Fatalf("limit %d: page length %d, next %v; remainder %d", n, len(v.Runs), v.Next, len(remainder))
		}
		for i, entry := range v.Runs {
			if entry.ID != remainder[i].ID {
				t.Fatal("page run", entry.ID, remainder[i].ID)
			}
		}
		for _, r := range v.Runs {
			stored, e := h.st.RunByID(ctx, r.ID)
			must(t, e)
			var expected tools.RunEntry
			must(t, json.Unmarshal([]byte(expectedEntry(t, stored)), &expected))
			if !reflect.DeepEqual(r, expected) {
				t.Fatal(r, expected)
			}
			got = append(got, r.ID)
		}
		if v.Next == nil {
			var members map[string]json.RawMessage
			must(t, json.Unmarshal(object(t, result), &members))
			if _, ok := members["next"]; ok {
				t.Fatal("terminal next present")
			}
			break
		}
		last := v.Runs[len(v.Runs)-1]
		if *v.Next != (paging.Token{Tool: "runs"}).Encode(last.Started, last.ID) {
			t.Fatal("wrong position")
		}
		page = v.Next
	}
	var ids []string
	for _, r := range want {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(got, ids) || answers != 8 {
		t.Fatal(got, ids, answers)
	}
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 1} {
		s := create("alice", fmt.Sprintf("default-%d", count))
		rr := add(s, count)
		v := decode[tools.RunList](t, h.call("runs", tools.RunsArgs{Name: s.Name}))
		if len(v.Runs) != paging.DefaultLimit {
			t.Fatal(v)
		}
		if count == paging.DefaultLimit {
			if v.Next != nil {
				t.Fatal(v)
			}
		} else {
			if v.Next == nil {
				t.Fatal(v)
			}
			last := decode[tools.RunList](t, h.call("runs", tools.RunsArgs{Name: s.Name, Page: v.Next}))
			if len(last.Runs) != 1 || last.Runs[0].ID != rr[count-1].ID || last.Next != nil {
				t.Fatal(last)
			}
		}
	}
	for _, position := range [][2]string{{want[10].Started.UTC().Format(time.RFC3339), want[10].ID}, {"2000-01-01T00:00:00Z", ""}, {"9999-01-01T00:00:00Z", "z"}} {
		p := (paging.Token{Tool: "runs"}).Encode(position[0], position[1])
		for _, name := range []string{sc.Name, other.Name} {
			s, e := h.st.Find(ctx, "alice", name)
			must(t, e)
			rr, e := h.st.Runs(ctx, s.ID, position[0], position[1], paging.DefaultLimit)
			must(t, e)
			v := decode[tools.RunList](t, h.call("runs", tools.RunsArgs{Name: name, Page: &p}))
			if len(v.Runs) != len(rr) {
				t.Fatal(name, v, rr)
			}
			for i, r := range rr {
				if v.Runs[i].ID != r.ID {
					t.Fatal(name, v, rr)
				}
			}
		}
	}
}
