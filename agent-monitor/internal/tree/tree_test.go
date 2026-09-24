package tree

import (
	"reflect"
	"testing"
	"time"
)

func checkDraw(t *testing.T, input Tree, want string) {
	t.Helper()
	if got := Draw(input); got != want {
		t.Errorf("Draw() = %q, want %q", got, want)
	}
}

func acceptDraw(func(Tree) string) {}

func isNilError(err error) bool { return err == nil }

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

// R-2YTG-2W4H
func TestDrawSignature(t *testing.T) {
	t.Helper()
	acceptDraw(Draw)
}

// R-301C-GNV6
func TestErrNotFound(t *testing.T) {
	if isNilError(ErrNotFound) {
		t.Fatal("ErrNotFound is nil")
	}
}

// R-32H5-87CK
func TestFirstDistinctSubagent(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "x", Label: "first"}, {ID: "x", Label: "second"}, {ID: "r", Label: "duplicate root"},
	}}, "r  \n└── first  \n")
}

// R-33P1-LZ39
func TestDepthFirstPreorder(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "b"}, {ID: "a"}, {ID: "b1", Parent: "b"},
		{ID: "a1", Parent: "a"}, {ID: "a1a", Parent: "a1"},
	}}, "r  \n├── a  \n│   └── a1  \n│       └── a1a  \n└── b  \n    └── b1  \n")
}

// R-34WX-ZQTY
func TestParentResolution(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "p"}, {ID: "child", Parent: "p"}, {ID: "empty"},
		{ID: "root", Parent: "r"}, {ID: "missing", Parent: "absent"},
	}}, "r  \n├── empty  \n├── missing  \n├── p  \n│   └── child  \n└── root  \n")
}

// R-364U-DIKN
func TestParentCycles(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "p", Parent: "q", Status: StatusDone},
		{ID: "q", Parent: "p", Status: StatusDone},
		{ID: "c", Parent: "p", Status: StatusDone},
		{ID: "s", Parent: "s", Status: StatusDone},
	}}, "r  \n├── p  done\n│   └── c  done\n├── q  done\n└── s  done\n")
}

// R-37CQ-RABC
func TestSiblingOrder(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "z", Started: now.Add(-time.Hour)},
		{ID: "b", Started: now, HasStarted: true},
		{ID: "c"},
		{ID: "a", Started: now, HasStarted: true},
		{ID: "d", Started: now.Add(-time.Second), HasStarted: true},
	}}, "r  \n├── d  \n├── a  \n├── b  \n├── c  \n└── z  \n")
}

// R-38KN-5221
func TestConnectors(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r"}, Subagents: []Node{
		{ID: "a"}, {ID: "b"},
	}}, "r  \n├── a  \n└── b  \n")
}

// R-39SJ-ITSQ
func TestAncestorPrefixes(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ids := []struct{ id, parent string }{
		{"A", ""}, {"A1", "A"}, {"A1a", "A1"}, {"B", ""},
		{"B1", "B"}, {"B1a", "B1"}, {"B1b", "B1"},
	}
	input := Tree{Root: Node{ID: "r", Status: StatusWorking}}
	for i, item := range ids {
		input.Subagents = append(input.Subagents, Node{
			ID: item.id, Parent: item.parent, Status: StatusDone,
			Started: now.Add(time.Duration(i) * time.Second), HasStarted: true,
		})
	}
	checkDraw(t, input, "r  working\n├── A  done\n│   └── A1  done\n│       └── A1a  done\n└── B  done\n    └── B1  done\n        ├── B1a  done\n        └── B1b  done\n")
}

// R-3B0F-WLJF
func TestLineFormatAndArbitraryStatus(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r", Status: Status("stalled")}, Subagents: []Node{
		{ID: "x", Status: Status("")},
	}}, "r  stalled\n└── x  \n")
}

// R-3DG8-O50T
func TestLabelsAndEscaping(t *testing.T) {
	checkDraw(t, Tree{Root: Node{ID: "r", Label: "fix Bob's checkout"}, Subagents: []Node{
		{ID: "a0a3b6c9d1f4e7528"}, {ID: "x", Label: "a\tb"},
	}}, "fix Bob's checkout  \n├── a0a3b6c9d1f4e7528  \n└── a\\tb  \n")
}

// R-3EO5-1WRI
func TestDrawPreservesInput(t *testing.T) {
	input := Tree{Root: Node{ID: "r", Label: "root"}, Subagents: []Node{
		{ID: "b", Parent: "a"}, {ID: "a", Started: time.Now(), HasStarted: true},
	}}
	want := Tree{Root: input.Root, Subagents: append([]Node(nil), input.Subagents...)}
	Draw(input)
	if !reflect.DeepEqual(input, want) {
		t.Errorf("Draw modified input: %#v", input)
	}
}

// R-3FW1-FOI7
func TestRootOnly(t *testing.T) {
	root := Node{ID: "b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14", Status: StatusIdle}
	want := "b41f0c77-9e2a-4d18-8c3b-6a5e2f9d0c14  idle\n"
	checkDraw(t, Tree{Root: root}, want)
	checkDraw(t, Tree{Root: root, Subagents: []Node{}}, want)
}

// R-3H3X-TG8W
func TestCheckoutExample(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	input := Tree{Root: Node{ID: "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", Label: "fix Bob's checkout", Status: StatusWorking}, Subagents: []Node{
		{ID: "a5b8c1f4e6d372905", Label: "Draft the refund fix", Status: StatusWorking, Started: now.Add(4 * time.Second), HasStarted: true},
		{ID: "a3e6f9d2b4c150783", Parent: "a2d5f8e1c3b049672", Label: "Run the payment suite", Status: StatusFailed, Started: now.Add(2 * time.Second), HasStarted: true},
		{ID: "a1c4e7f09b2d38561", Label: "Find the checkout handler", Status: StatusDone, Started: now, HasStarted: true},
		{ID: "a4f7e0c3d5a261894", Label: "Profile the cart query", Status: StatusKilled, Started: now.Add(3 * time.Second), HasStarted: true},
		{ID: "a2d5f8e1c3b049672", Label: "Review the payment tests", Status: StatusWorking, Started: now.Add(time.Second), HasStarted: true},
	}}
	checkDraw(t, input, "fix Bob's checkout  working\n├── Find the checkout handler  done\n├── Review the payment tests  working\n│   └── Run the payment suite  failed\n├── Profile the cart query  killed\n└── Draft the refund fix  working\n")
}

// R-3IBU-77ZL
func TestDocsExample(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	base := Tree{Root: Node{ID: "e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54", Status: StatusIdle}, Subagents: []Node{
		{ID: "a9f2a5b8c0e3d6417", Parent: "a8e1f4a7b9d2c5306", Label: "List the page templates", Status: StatusDone, Started: now.Add(time.Second), HasStarted: true},
		{ID: "a0a3b6c9d1f4e7528", Status: StatusUnknown, Started: now.Add(2 * time.Second), HasStarted: true},
		{ID: "a8e1f4a7b9d2c5306", Label: "Map the docs routes", Status: StatusUnknown, Started: now, HasStarted: true},
	}}
	want := "e82a5c90-4f17-4b3d-9d06-1c7b3e8a2f54  idle\n├── Map the docs routes  unknown\n│   └── List the page templates  done\n└── a0a3b6c9d1f4e7528  unknown\n"
	checkDraw(t, base, want)
	base.Subagents[1].HasStarted = false
	checkDraw(t, base, want)
}
