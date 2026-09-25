package tree

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func checkDraw(t *testing.T, input Tree, color bool, want string) {
	t.Helper()
	if got := Draw(input, color); got != want {
		t.Errorf("Draw() = %q, want %q", got, want)
	}
}

func acceptDraw(func(Tree, bool) string) {}
func isNilError(err error) bool          { return err == nil }

// R-2TXU-JT5P
func TestStatusType(t *testing.T) {
	var status Status = "custom"
	if reflect.TypeOf(status).Kind() != reflect.String {
		t.Fatal("Status is not string based")
	}
}

// R-2V5Q-XKWE
func TestStatusConstants(t *testing.T) {
	values := []struct {
		status Status
		want   string
	}{
		{StatusWorking, "working"}, {StatusIdle, "idle"}, {StatusUnknown, "unknown"},
		{StatusEnded, "ended"}, {StatusDone, "done"}, {StatusFailed, "failed"},
		{StatusKilled, "killed"},
	}
	for _, item := range values {
		if string(item.status) != item.want || reflect.TypeOf(item.status) != reflect.TypeOf(Status("")) {
			t.Errorf("status %q has wrong value or type", item.want)
		}
	}
}

// R-2WDN-BCN3
func TestNodeFields(t *testing.T) {
	typ := reflect.TypeOf(Node{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{"ID", reflect.TypeOf("")}, {"Parent", reflect.TypeOf("")},
		{"Label", reflect.TypeOf("")}, {"Status", reflect.TypeOf(Status(""))},
		{"Started", reflect.TypeOf(time.Time{})}, {"HasStarted", reflect.TypeOf(false)},
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Node has %d fields, want %d", typ.NumField(), len(want))
	}
	for i, field := range want {
		got := typ.Field(i)
		if got.Name != field.name || got.Type != field.typ || !got.IsExported() {
			t.Errorf("Node field %d = %s %s", i, got.Name, got.Type)
		}
	}
}

// R-2XLJ-P4DS
func TestTreeFields(t *testing.T) {
	typ := reflect.TypeOf(Tree{})
	if typ.NumField() != 2 || typ.Field(0).Name != "Root" || typ.Field(0).Type != reflect.TypeOf(Node{}) ||
		typ.Field(1).Name != "Subagents" || typ.Field(1).Type != reflect.TypeOf([]Node{}) ||
		!typ.Field(0).IsExported() || !typ.Field(1).IsExported() {
		t.Errorf("Tree fields = %v", typ)
	}
}

// R-VZAE-KSAJ
func TestDrawSignature(_ *testing.T) { acceptDraw(Draw) }

// R-301C-GNV6
func TestErrNotFound(t *testing.T) {
	if isNilError(ErrNotFound) {
		t.Fatal("ErrNotFound is nil")
	}
}

// R-W91L-MY83
func TestFirstDistinctSubagent(t *testing.T) {
	input := Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "x", Label: "first"}, {ID: "x", Label: "second"}, {ID: "r", Label: "duplicate root"},
	}}
	for _, color := range []bool{false, true} {
		got := Draw(input, color)
		if strings.Count(got, "[x] first") != 1 || strings.Contains(got, "second") || strings.Contains(got, "duplicate root") || strings.Count(got, "\n├── ")+strings.Count(got, "\n└── ") != 1 {
			t.Errorf("Draw(%v) drew wrong subagents: %q", color, got)
		}
	}
}

// R-W7TP-96HE
func TestDepthFirstPreorder(t *testing.T) {
	input := Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "b"}, {ID: "a"}, {ID: "b1", Parent: "b"},
		{ID: "a1", Parent: "a"}, {ID: "a1a", Parent: "a1"},
	}}
	for _, color := range []bool{false, true} {
		got := Draw(input, color)
		lines, _, ok := strings.Cut(got, "\n\n")
		if !ok {
			t.Fatalf("Draw(%v) has no key separator: %q", color, got)
		}
		var ids []string
		for _, line := range strings.Split(strings.TrimSuffix(lines, "\n"), "\n") {
			start := strings.LastIndex(line, " [") + 1
			end := strings.IndexByte(line, ']')
			ids = append(ids, line[start+1:end])
		}
		if !reflect.DeepEqual(ids, []string{"r", "a", "a1", "a1a", "b", "b1"}) || strings.Count(got, "\n\n") != 1 || !strings.HasSuffix(got, "\n") {
			t.Errorf("Draw(%v) has wrong order or framing: %q", color, got)
		}
	}
}

// R-34WX-ZQTY
func TestParentResolution(t *testing.T) {
	input := Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "p"}, {ID: "child", Parent: "p"}, {ID: "empty"},
		{ID: "root", Parent: "r"}, {ID: "missing", Parent: "absent"},
	}}
	checkDraw(t, input, false, "● [r]  unknown\n├── ● [empty]  unknown\n├── ● [missing]  unknown\n├── ● [p]  unknown\n│   └── ● [child]  unknown\n└── ● [root]  unknown\n\n● working (0)  ● idle (0)  ● done (0)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (6)\n")
}

// R-WF53-JSXK
func TestParentCycles(t *testing.T) {
	input := Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "p", Parent: "q", Status: StatusDone},
		{ID: "q", Parent: "p", Status: StatusDone},
		{ID: "c", Parent: "p", Status: StatusDone},
		{ID: "s", Parent: "s", Status: StatusDone},
	}}
	want := "● [r]  unknown\n├── ● [p]  done\n│   └── ● [c]  done\n├── ● [q]  done\n└── ● [s]  done\n\n● working (0)  ● idle (0)  ● done (4)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (1)\n"
	checkDraw(t, input, false, want)
}

// R-37CQ-RABC
func TestSiblingOrder(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	input := Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "z", Started: now.Add(-time.Hour)},
		{ID: "b", Started: now, HasStarted: true},
		{ID: "c"},
		{ID: "a", Started: now, HasStarted: true},
		{ID: "d", Started: now.Add(-time.Second), HasStarted: true},
	}}
	checkDraw(t, input, false, "● [r]  unknown\n├── ● [d]  unknown\n├── ● [a]  unknown\n├── ● [b]  unknown\n├── ● [c]  unknown\n└── ● [z]  unknown\n\n● working (0)  ● idle (0)  ● done (0)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (6)\n")
}

// R-38KN-5221
func TestConnectors(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{{ID: "a"}, {ID: "b"}}}, false, "● [r]  unknown\n├── ● [a]  unknown\n└── ● [b]  unknown\n\n● working (0)  ● idle (0)  ● done (0)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (3)\n")
}

// R-WGCZ-XKO9
func TestAncestorPrefixes(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ids := []struct{ id, parent string }{
		{"A", ""}, {"A1", "A"}, {"A1a", "A1"}, {"B", ""},
		{"B1", "B"}, {"B1a", "B1"}, {"B1b", "B1"},
	}
	input := Tree{Root: Node{ID: "r", Status: StatusWorking}}
	for i, item := range ids {
		input.Subagents = append(input.Subagents, Node{ID: item.id, Parent: item.parent, Status: StatusDone, Started: now.Add(time.Duration(i) * time.Second), HasStarted: true})
	}
	checkDraw(t, input, false, "● [r]  working\n├── ● [A]  done\n│   └── ● [A1]  done\n│       └── ● [A1a]  done\n└── ● [B]  done\n    └── ● [B1]  done\n        ├── ● [B1a]  done\n        └── ● [B1b]  done\n\n● working (1)  ● idle (0)  ● done (7)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (0)\n")
}

// R-W0IA-YK18
func TestEffectiveStatus(t *testing.T) {
	input := Tree{Root: Node{ID: "r", Status: "stalled"}, Subagents: []Node{{ID: "x", Status: ""}}}
	got := Draw(input, false)
	if !strings.HasPrefix(got, "● [r]  unknown\n└── ● [x]  unknown\n") || !strings.HasSuffix(got, "● unknown (2)\n") {
		t.Errorf("wrong effective status: %q", got)
	}
}

// R-W1Q7-CBRX
func TestDots(t *testing.T) {
	cases := []struct {
		status Status
		code   string
	}{
		{StatusWorking, "36"}, {StatusIdle, "34"}, {StatusDone, "32"},
		{StatusKilled, "33"}, {StatusFailed, "31"}, {StatusEnded, "35"},
		{StatusUnknown, "90"},
	}
	for _, tc := range cases {
		got := Draw(Tree{Root: Node{ID: "r", Status: tc.status}}, true)
		if !strings.HasPrefix(got, "\x1b["+tc.code+"m●\x1b[0m [r]\n") || strings.Count(got, "\x1b[") != 16 {
			t.Errorf("wrong dot for %s: %q", tc.status, got)
		}
	}
}

// R-W2Y3-Q3IM
func TestLineFormat(t *testing.T) {
	input := Tree{Root: Node{ID: "a\tb", Label: "x\ty", Status: "stalled"}}
	if got := Draw(input, false); !strings.HasPrefix(got, "● [a\\tb] x\\ty  unknown\n") {
		t.Errorf("wrong plain line: %q", got)
	}
	if got := Draw(Tree{Root: Node{ID: "r", Status: StatusIdle}}, true); !strings.HasPrefix(got, "\x1b[34m●\x1b[0m [r]\n") {
		t.Errorf("wrong colored line: %q", got)
	}
}

// R-W460-3V9B
func TestKeyFormat(t *testing.T) {
	input := Tree{Root: Node{ID: "r", Status: StatusWorking}, Subagents: []Node{{ID: "a", Status: StatusDone}, {ID: "b", Status: StatusKilled}, {ID: "c", Status: StatusFailed}}}
	got := Draw(input, false)
	want := "● working (1)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)\n"
	if !strings.HasSuffix(got, "\n"+want) {
		t.Errorf("wrong key: %q", got)
	}
}

// R-W6LS-VEQP
func TestCountsExcludeDuplicates(t *testing.T) {
	input := Tree{Root: Node{ID: "r", Status: StatusIdle}, Subagents: []Node{
		{ID: "x", Status: StatusDone}, {ID: "x", Status: StatusKilled},
		{ID: "y", Status: "stalled"}, {ID: "r", Status: StatusFailed},
	}}
	for _, color := range []bool{false, true} {
		got := Draw(input, color)
		key := got[strings.Index(got, "\n\n")+2:]
		for _, entry := range []string{"idle (1)", "done (1)", "unknown (1)", "working (0)", "killed (0)", "failed (0)", "ended (0)"} {
			if !strings.Contains(key, entry) {
				t.Errorf("Draw(%v) key lacks %q: %q", color, entry, key)
			}
		}
	}
}

// R-3EO5-1WRI
func TestDrawPreservesInput(t *testing.T) {
	input := Tree{Root: Node{ID: "r", Label: "root"}, Subagents: []Node{
		{ID: "b", Parent: "a"}, {ID: "a", Started: time.Now(), HasStarted: true},
	}}
	want := Tree{Root: input.Root, Subagents: append([]Node(nil), input.Subagents...)}
	Draw(input, false)
	if !reflect.DeepEqual(input, want) {
		t.Errorf("Draw modified input: %#v", input)
	}
}

// R-WA9I-0PYS
func TestRootOnly(t *testing.T) {
	root := Node{ID: "b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14", Status: StatusIdle}
	want := "● [b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14]  idle\n\n● working (0)  ● idle (1)  ● done (0)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (0)\n"
	checkDraw(t, Tree{Root: root}, false, want)
	checkDraw(t, Tree{Root: root, Subagents: []Node{}}, false, want)
}

func checkoutTree() Tree {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return Tree{Root: Node{ID: "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", Label: "fix Bob's checkout", Status: StatusWorking}, Subagents: []Node{
		{ID: "a5b8c1f4e6d372905", Label: "Draft the refund fix", Status: StatusWorking, Started: now.Add(4 * time.Second), HasStarted: true},
		{ID: "a3e6f9d2b4c150783", Parent: "a2d5f8e1c3b049672", Label: "Run the payment suite", Status: StatusFailed, Started: now.Add(2 * time.Second), HasStarted: true},
		{ID: "a1c4e7f09b2d38561", Label: "Find the checkout handler", Status: StatusDone, Started: now, HasStarted: true},
		{ID: "a4f7e0c3d5a261894", Label: "Profile the cart query", Status: StatusKilled, Started: now.Add(3 * time.Second), HasStarted: true},
		{ID: "a2d5f8e1c3b049672", Label: "Review the payment tests", Status: StatusWorking, Started: now.Add(time.Second), HasStarted: true},
	}}
}

// R-WBHE-EHPH
func TestCheckoutExample(t *testing.T) {
	checkDraw(t, checkoutTree(), false, "● [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout  working\n├── ● [a1c4e7f09b2d38561] Find the checkout handler  done\n├── ● [a2d5f8e1c3b049672] Review the payment tests  working\n│   └── ● [a3e6f9d2b4c150783] Run the payment suite  failed\n├── ● [a4f7e0c3d5a261894] Profile the cart query  killed\n└── ● [a5b8c1f4e6d372905] Draft the refund fix  working\n\n● working (3)  ● idle (0)  ● done (1)  ● killed (1)  ● failed (1)  ● ended (0)  ● unknown (0)\n")
}

// R-WCPA-S9G6
func TestCheckoutColorExample(t *testing.T) {
	checkDraw(t, checkoutTree(), true, "\x1b[36m●\x1b[0m [7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93] fix Bob's checkout\n├── \x1b[32m●\x1b[0m [a1c4e7f09b2d38561] Find the checkout handler\n├── \x1b[36m●\x1b[0m [a2d5f8e1c3b049672] Review the payment tests\n│   └── \x1b[31m●\x1b[0m [a3e6f9d2b4c150783] Run the payment suite\n├── \x1b[33m●\x1b[0m [a4f7e0c3d5a261894] Profile the cart query\n└── \x1b[36m●\x1b[0m [a5b8c1f4e6d372905] Draft the refund fix\n\n\x1b[36m●\x1b[0m working (3)  \x1b[34m●\x1b[0m idle (0)  \x1b[32m●\x1b[0m done (1)  \x1b[33m●\x1b[0m killed (1)  \x1b[31m●\x1b[0m failed (1)  \x1b[35m●\x1b[0m ended (0)  \x1b[90m●\x1b[0m unknown (0)\n")
}

// R-WDX7-616V
func TestDocsExample(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := Tree{Root: Node{ID: "e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54", Status: StatusIdle}, Subagents: []Node{
		{ID: "a9f2a5b8c0e3d6417", Parent: "a8e1f4a7b9d2c5306", Label: "List the page templates", Status: StatusDone, Started: now.Add(time.Second), HasStarted: true},
		{ID: "a0a3b6c9d1f4e7528", Status: StatusUnknown},
		{ID: "a8e1f4a7b9d2c5306", Label: "Map the docs routes", Status: StatusUnknown, Started: now, HasStarted: true},
	}}
	want := "● [e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54]  idle\n├── ● [a8e1f4a7b9d2c5306] Map the docs routes  unknown\n│   └── ● [a9f2a5b8c0e3d6417] List the page templates  done\n└── ● [a0a3b6c9d1f4e7528]  unknown\n\n● working (0)  ● idle (1)  ● done (1)  ● killed (0)  ● failed (0)  ● ended (0)  ● unknown (2)\n"
	checkDraw(t, base, false, want)
	base.Subagents[1].Started = now.Add(2 * time.Second)
	base.Subagents[1].HasStarted = true
	checkDraw(t, base, false, want)
}
