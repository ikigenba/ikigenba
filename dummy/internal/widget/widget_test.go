package widget_test

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-QICF-XK9C, R-QM05-2VHF, R-QS3M-ZQ6W, R-RQ8T-PAYC.
func TestSourceDeclarations(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		files = append(files, file)
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.VAR {
				t.Errorf("package-level var at %s", fset.Position(gen.Pos()))
			}
		}
	}
	config := types.Config{Importer: importer.Default()}
	pkg, err := config.Check("github.com/ikigenba/ikigenba/dummy/internal/widget", fset, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := pkg.Scope()
	status, ok := scope.Lookup("Status").(*types.TypeName)
	if !ok || status.IsAlias() || status.Type().Underlying() != types.Typ[types.String] {
		t.Fatal("Status must be a defined string type")
	}
	wantStatuses := map[string]string{"StatusActive": `"active"`, "StatusPaused": `"paused"`, "StatusRetired": `"retired"`}
	gotStatuses := make(map[string]string)
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if c, isConst := obj.(*types.Const); isConst && types.Identical(c.Type(), status.Type()) && c.Exported() {
			gotStatuses[name] = c.Val().ExactString()
		}
	}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Errorf("Status values = %v, want %v", gotStatuses, wantStatuses)
	}
	wantConstants := map[string]string{
		"MaxNameRunes":            "40",
		"NameRequiredMessage":     strconv.Quote("a name is required"),
		"NameTooLongMessage":      strconv.Quote("the name is too long; the limit is 40 characters"),
		"NameTakenMessage":        strconv.Quote("that name is already taken"),
		"CountNotWholeMessage":    strconv.Quote("the count must be a whole number"),
		"CountNegativeMessage":    strconv.Quote("the count cannot be negative"),
		"StatusNotAllowedMessage": strconv.Quote("the status must be one of active, paused, or retired"),
	}
	for name, want := range wantConstants {
		c, isConst := scope.Lookup(name).(*types.Const)
		if !isConst || c.Val().ExactString() != want {
			t.Errorf("%s = %v, want constant %s", name, scope.Lookup(name), want)
		}
	}
}

// R-QKS8-P3QQ, R-7WR2-CK99, R-QPNU-86PI, R-QTBJ-DHXL.
func TestPublicStructFields(t *testing.T) {
	for _, tc := range []struct {
		typ    reflect.Type
		fields map[string]reflect.Type
	}{
		{reflect.TypeFor[widget.Widget](), map[string]reflect.Type{"Name": reflect.TypeFor[string](), "Count": reflect.TypeFor[int](), "Status": reflect.TypeFor[widget.Status]()}},
		{reflect.TypeFor[widget.Submission](), map[string]reflect.Type{"Name": reflect.TypeFor[string](), "Count": reflect.TypeFor[string](), "Status": reflect.TypeFor[string]()}},
		{reflect.TypeFor[widget.FieldErrors](), map[string]reflect.Type{"Name": reflect.TypeFor[string](), "Count": reflect.TypeFor[string](), "Status": reflect.TypeFor[string]()}},
		{reflect.TypeFor[widget.Store](), map[string]reflect.Type{}},
	} {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			if tc.typ.Kind() != reflect.Struct {
				t.Fatal("type must be a struct")
			}
			fields := make(map[string]reflect.Type)
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i)
				if f.IsExported() {
					fields[f.Name] = f.Type
				}
			}
			if !reflect.DeepEqual(fields, tc.fields) {
				t.Errorf("exported fields = %v, want %v", fields, tc.fields)
			}
		})
	}
}

// R-QJKC-BC01, R-QY74-WKWD, R-QZF1-ACN2.
func TestStatuses(t *testing.T) {
	statuses := widget.Statuses
	if reflect.TypeOf(statuses) != reflect.TypeFor[func() []widget.Status]() {
		t.Fatal("unexpected signature for widget.Statuses")
	}
	want := []widget.Status{widget.StatusActive, widget.StatusPaused, widget.StatusRetired}
	got := statuses()
	if !slices.Equal(got, want) {
		t.Fatalf("Statuses = %v, want %v", got, want)
	}
	got[0] = widget.StatusRetired
	got[1] = widget.StatusActive
	if !slices.Equal(statuses(), want) {
		t.Fatal("mutating a returned slice changed later statuses")
	}
}

// R-QQVQ-LYG7, R-RIXF-EOI6.
func TestFieldErrorsAny(t *testing.T) {
	anyError := widget.FieldErrors.Any
	if reflect.TypeOf(anyError) != reflect.TypeFor[func(widget.FieldErrors) bool]() {
		t.Fatal("unexpected signature for widget.FieldErrors.Any")
	}
	for bits := range 8 {
		e := widget.FieldErrors{}
		if bits&1 != 0 {
			e.Name = "name error"
		}
		if bits&2 != 0 {
			e.Count = "count error"
		}
		if bits&4 != 0 {
			e.Status = "status error"
		}
		if got := anyError(e); got != (bits != 0) {
			t.Errorf("%+v.Any() = %v", e, got)
		}
	}
}

func initialWidgets() []widget.Widget {
	return []widget.Widget{
		{Name: "alpha", Count: 3, Status: widget.StatusActive},
		{Name: "beta", Count: 0, Status: widget.StatusPaused},
		{Name: "gamma", Count: 12, Status: widget.StatusRetired},
	}
}

// R-QUJF-R9OA, R-QVRC-51EZ, R-R1UU-1W4G, R-RRGQ-32P1.
func TestStoreInitialStateAndIndependence(t *testing.T) {
	newStore := widget.NewStore
	if reflect.TypeOf(newStore) != reflect.TypeFor[func() *widget.Store]() {
		t.Fatal("unexpected signature for widget.NewStore")
	}
	all := (*widget.Store).All
	if reflect.TypeOf(all) != reflect.TypeFor[func(*widget.Store) []widget.Widget]() {
		t.Fatal("unexpected signature for (*widget.Store).All")
	}
	first, second := newStore(), newStore()
	want := initialWidgets()
	if !slices.Equal(all(first), want) || !slices.Equal(all(second), want) {
		t.Fatal("new stores do not contain the exact starting widgets")
	}
	created, errs := first.Create(widget.Submission{Name: "delta", Count: "5", Status: "active"})
	if errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(second.All(), want) || !slices.Equal(newStore().All(), want) {
		t.Fatal("a write changed another store or later store's starting widgets")
	}
	if _, errs := second.Create(widget.Submission{Name: "epsilon", Count: "6", Status: "paused"}); errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(first.All(), append(want, created)) {
		t.Fatal("second store's write changed first store")
	}
}

// R-R32Q-FNV5.
func TestAllReturnsIndependentSnapshots(t *testing.T) {
	s := widget.NewStore()
	want := initialWidgets()
	snapshot := s.All()
	snapshot[0] = widget.Widget{Name: "changed", Count: 900, Status: widget.StatusRetired}
	snapshot = append(snapshot, widget.Widget{Name: "appended"})
	if len(snapshot) != 4 || !slices.Equal(s.All(), want) {
		t.Fatal("modifying or appending to a snapshot changed the store")
	}
	snapshot = nil
	if snapshot != nil || !slices.Equal(s.All(), want) {
		t.Fatal("discard changed store")
	}
	old := s.All()
	if _, errs := s.Create(widget.Submission{Name: "delta", Count: "1", Status: "active"}); errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(old, want) {
		t.Fatal("create modified an earlier snapshot")
	}
}

// R-QWZ8-IT5O, R-R4AM-TFLU, R-R5IJ-77CJ, R-RLD8-67ZK.
func TestAcceptedCreationOrderAndTrimming(t *testing.T) {
	create := (*widget.Store).Create
	if reflect.TypeOf(create) != reflect.TypeFor[func(*widget.Store, widget.Submission) (widget.Widget, widget.FieldErrors)]() {
		t.Fatal("unexpected signature for (*widget.Store).Create")
	}
	s := widget.NewStore()
	want := initialWidgets()
	for _, tc := range []struct {
		sub  widget.Submission
		want widget.Widget
	}{
		{widget.Submission{Name: "\u2003 delta \t", Count: "\u00a0+007\n", Status: "\ractive\u2002"}, widget.Widget{Name: "delta", Count: 7, Status: widget.StatusActive}},
		{widget.Submission{Name: "omega", Count: "0", Status: "paused"}, widget.Widget{Name: "omega", Count: 0, Status: widget.StatusPaused}},
		{widget.Submission{Name: "epsilon", Count: "12", Status: "retired"}, widget.Widget{Name: "epsilon", Count: 12, Status: widget.StatusRetired}},
	} {
		before := tc.sub
		got, errs := create(s, tc.sub)
		if errs != (widget.FieldErrors{}) || got != tc.want {
			t.Fatalf("Create(%+v) = %+v, %+v", tc.sub, got, errs)
		}
		if tc.sub != before {
			t.Fatal("Create changed the raw submission")
		}
		want = append(want, tc.want)
		if !slices.Equal(s.All(), want) {
			t.Fatalf("All = %v, want %v", s.All(), want)
		}
	}
}

// R-R5IJ-77CJ, R-R6QF-KZ38, R-R7YB-YQTX, R-R968-CIKM, R-RAE4-QABB.
func TestNameValidation(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"", widget.NameRequiredMessage}, {" \t\r\n\u2003", widget.NameRequiredMessage},
		{strings.Repeat("x", widget.MaxNameRunes+1), widget.NameTooLongMessage},
		{strings.Repeat("é", widget.MaxNameRunes+1), widget.NameTooLongMessage},
		{"  " + strings.Repeat("é", widget.MaxNameRunes) + " \u00a0", ""},
		{strings.Repeat("x", widget.MaxNameRunes), ""},
		{" alpha\t", widget.NameTakenMessage}, {"beta", widget.NameTakenMessage}, {"gamma", widget.NameTakenMessage},
		{"Alpha", ""}, {"ALPHA", ""}, {"distinct", ""}, {"\xff", ""},
	} {
		t.Run(strconv.Quote(tc.name), func(t *testing.T) {
			s := widget.NewStore()
			_, errs := s.Create(widget.Submission{Name: tc.name, Count: "0", Status: "active"})
			if errs != (widget.FieldErrors{Name: tc.want}) {
				t.Errorf("errors = %+v, want name %q only", errs, tc.want)
			}
		})
	}
	s := widget.NewStore()
	if _, errs := s.Create(widget.Submission{Name: " new name ", Count: "0", Status: "active"}); errs.Any() {
		t.Fatal(errs)
	}
	if _, errs := s.Create(widget.Submission{Name: "new name", Count: "0", Status: "active"}); errs.Name != widget.NameTakenMessage {
		t.Fatalf("new name was not reserved: %+v", errs)
	}
}

// R-RBM1-4220, R-RCTX-HTSP, R-RE1T-VLJE, R-R5IJ-77CJ.
func TestCountValidation(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct{ count, want string }{
		{"", widget.CountNotWholeMessage}, {" \u2003\t", widget.CountNotWholeMessage},
		{"1.0", widget.CountNotWholeMessage}, {"1e2", widget.CountNotWholeMessage},
		{"0x10", widget.CountNotWholeMessage}, {"1_000", widget.CountNotWholeMessage},
		{"1 2", widget.CountNotWholeMessage}, {"text", widget.CountNotWholeMessage},
		{strings.Repeat("9", 100), widget.CountNotWholeMessage}, {"-" + strings.Repeat("9", 100), widget.CountNotWholeMessage},
		{strconv.FormatUint(uint64(maxInt)+1, 10), widget.CountNotWholeMessage},
		{"-1", widget.CountNegativeMessage}, {"\t-12\u2003", widget.CountNegativeMessage},
		{strconv.Itoa(-maxInt - 1), widget.CountNegativeMessage},
		{"0", ""}, {"-0", ""}, {"+7", ""}, {"007", ""}, {" \u00a042\n", ""}, {strconv.Itoa(maxInt), ""},
	} {
		t.Run(strconv.Quote(tc.count), func(t *testing.T) {
			s := widget.NewStore()
			got, errs := s.Create(widget.Submission{Name: "delta", Count: tc.count, Status: "active"})
			if errs != (widget.FieldErrors{Count: tc.want}) {
				t.Fatalf("errors = %+v, want count %q only", errs, tc.want)
			}
			if tc.want == "" {
				want, err := strconv.Atoi(strings.TrimSpace(tc.count))
				if err != nil {
					t.Fatal(err)
				}
				if got.Count != want {
					t.Errorf("stored count = %d, want %d", got.Count, want)
				}
			}
		})
	}
}

// R-RF9Q-9DA3, R-RGHM-N50S, R-R5IJ-77CJ.
func TestStatusValidation(t *testing.T) {
	for _, tc := range []struct{ status, want string }{
		{"", widget.StatusNotAllowedMessage}, {" \t", widget.StatusNotAllowedMessage},
		{"Active", widget.StatusNotAllowedMessage}, {"ACTIVE", widget.StatusNotAllowedMessage},
		{"pending", widget.StatusNotAllowedMessage}, {"active paused", widget.StatusNotAllowedMessage},
		{"active", ""}, {"paused", ""}, {"retired", ""}, {"\u2003active\n", ""},
	} {
		t.Run(strconv.Quote(tc.status), func(t *testing.T) {
			got, errs := widget.NewStore().Create(widget.Submission{Name: "delta", Count: "1", Status: tc.status})
			if errs != (widget.FieldErrors{Status: tc.want}) {
				t.Fatalf("errors = %+v, want status %q only", errs, tc.want)
			}
			if tc.want == "" && got.Status != widget.Status(strings.TrimSpace(tc.status)) {
				t.Errorf("stored status = %q", got.Status)
			}
		})
	}
}

// R-RHPJ-0WRH, R-RML4-JZQ9, R-QPNU-86PI, R-R5IJ-77CJ.
func TestRejectedSubmissionReportsEveryFieldAndLeavesStoreUnchanged(t *testing.T) {
	for bits := 1; bits < 8; bits++ {
		t.Run(strconv.Itoa(bits), func(t *testing.T) {
			s := widget.NewStore()
			if _, errs := s.Create(widget.Submission{Name: "zeta", Count: "2", Status: "retired"}); errs.Any() {
				t.Fatal(errs)
			}
			before := s.All()
			sub := widget.Submission{Name: " delta ", Count: " 0 ", Status: " active "}
			want := widget.FieldErrors{}
			if bits&1 != 0 {
				sub.Name = " \t"
				want.Name = widget.NameRequiredMessage
			}
			if bits&2 != 0 {
				sub.Count = " 1.5 "
				want.Count = widget.CountNotWholeMessage
			}
			if bits&4 != 0 {
				sub.Status = " invalid "
				want.Status = widget.StatusNotAllowedMessage
			}
			raw := sub
			got, errs := s.Create(sub)
			if errs != want {
				t.Errorf("errors = %+v, want %+v", errs, want)
			}
			if got != (widget.Widget{}) {
				t.Errorf("rejected widget = %+v", got)
			}
			if !slices.Equal(s.All(), before) {
				t.Fatal("rejection changed store content or order")
			}
			if sub != raw {
				t.Fatal("rejection changed raw submission")
			}
		})
	}
}

// R-RNT0-XRGY, R-RP0X-BJ7N.
func TestConcurrentCreationAndSnapshots(t *testing.T) {
	s := widget.NewStore()
	const writers = 64
	start := make(chan struct{})
	results := make(chan widget.Widget, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			name := fmt.Sprintf("widget-%d", i)
			created, errs := s.Create(widget.Submission{Name: name, Count: strconv.Itoa(i), Status: "active"})
			if errs.Any() {
				t.Errorf("Create(%s): %+v", name, errs)
				return
			}
			results <- created
		})
		wg.Go(func() {
			<-start
			for range 8 {
				snapshot := s.All()
				if len(snapshot) < 3 {
					t.Error("snapshot lost starting widgets")
					return
				}
				snapshot[0].Name = "caller mutation"
			}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	got := s.All()
	if len(got) != len(initialWidgets())+writers {
		t.Fatalf("All length = %d", len(got))
	}
	if !slices.Equal(got[:3], initialWidgets()) {
		t.Fatal("initial widgets changed")
	}
	counts := make(map[widget.Widget]int)
	for _, w := range got[3:] {
		counts[w]++
	}
	accepted := 0
	for w := range results {
		accepted++
		if counts[w] != 1 {
			t.Errorf("accepted widget %+v appears %d times", w, counts[w])
		}
	}
	if accepted != writers {
		t.Errorf("accepted %d, want %d", accepted, writers)
	}
}

// R-7XYY-QBZY, R-RNT0-XRGY.
func TestConcurrentDuplicateNamesAreAtomic(t *testing.T) {
	s := widget.NewStore()
	const writers = 64
	start := make(chan struct{})
	type outcome struct {
		w    widget.Widget
		errs widget.FieldErrors
	}
	results := make(chan outcome, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			name := "delta"
			if i%2 == 0 {
				name = " \u2003delta\t"
			}
			w, errs := s.Create(widget.Submission{Name: name, Count: strconv.Itoa(i), Status: "paused"})
			results <- outcome{w, errs}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	var winner widget.Widget
	for result := range results {
		if result.errs == (widget.FieldErrors{}) {
			accepted++
			winner = result.w
		} else if result.errs != (widget.FieldErrors{Name: widget.NameTakenMessage}) || result.w != (widget.Widget{}) {
			t.Errorf("unexpected rejected result: %+v", result)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want 1", accepted)
	}
	if !slices.Equal(s.All(), append(initialWidgets(), winner)) {
		t.Fatalf("All = %+v", s.All())
	}
	seen := make(map[string]bool)
	for _, w := range s.All() {
		if seen[w.Name] {
			t.Errorf("duplicate name %q", w.Name)
		}
		seen[w.Name] = true
	}
}
