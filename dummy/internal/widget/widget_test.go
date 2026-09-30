package widget_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// Declaring these from another package proves each name is an exported
// constant: a const declaration accepts nothing else.
const (
	externalStatusActive  widget.Status = widget.StatusActive
	externalStatusPaused  widget.Status = widget.StatusPaused
	externalStatusRetired widget.Status = widget.StatusRetired

	// Both declarations compile only if MaxNameRunes is an untyped constant.
	externalMaxNameRunesInt8  int8    = widget.MaxNameRunes
	externalMaxNameRunesFloat float64 = widget.MaxNameRunes

	externalNameRequiredMessage     string = widget.NameRequiredMessage
	externalNameTooLongMessage      string = widget.NameTooLongMessage
	externalNameTakenMessage        string = widget.NameTakenMessage
	externalCountNotWholeMessage    string = widget.CountNotWholeMessage
	externalCountNegativeMessage    string = widget.CountNegativeMessage
	externalStatusNotAllowedMessage string = widget.StatusNotAllowedMessage
)

// R-RKRT-ZN8J.
func TestStatusTypeAndConstants(t *testing.T) {
	// An untyped string constant converts to Status, and a Status converts to
	// string, only if Status is a type whose underlying type is string.
	custom := widget.Status("custom")
	if string(custom) != "custom" {
		t.Errorf("Status round trip = %q", custom)
	}
	// Each short declaration takes the constant's own type; assigning it to a
	// Status variable compiles only if that type is Status.
	active, paused, retired := widget.StatusActive, widget.StatusPaused, widget.StatusRetired
	var statuses [3]widget.Status
	statuses[0], statuses[1], statuses[2] = active, paused, retired
	if statuses != [3]widget.Status{"active", "paused", "retired"} {
		t.Errorf("status constants = %q", statuses)
	}
	if string(externalStatusActive) != "active" || string(externalStatusPaused) != "paused" || string(externalStatusRetired) != "retired" {
		t.Errorf("status constants = %q, %q, %q", externalStatusActive, externalStatusPaused, externalStatusRetired)
	}
}

// R-QM05-2VHF.
func TestMaxNameRunesConstant(t *testing.T) {
	if externalMaxNameRunesInt8 != 40 || externalMaxNameRunesFloat != 40 {
		t.Errorf("MaxNameRunes = %d, %v; want 40", externalMaxNameRunesInt8, externalMaxNameRunesFloat)
	}
}

// R-QS3M-ZQ6W.
func TestMessageConstants(t *testing.T) {
	for _, tc := range []struct{ name, got, want string }{
		{"NameRequiredMessage", externalNameRequiredMessage, "a name is required"},
		{"NameTooLongMessage", externalNameTooLongMessage, "the name is too long; the limit is 40 characters"},
		{"NameTakenMessage", externalNameTakenMessage, "that name is already taken"},
		{"CountNotWholeMessage", externalCountNotWholeMessage, "the count must be a whole number"},
		{"CountNegativeMessage", externalCountNegativeMessage, "the count cannot be negative"},
		{"StatusNotAllowedMessage", externalStatusNotAllowedMessage, "the status must be one of active, paused, or retired"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// R-4F49-SZRQ, R-4GC6-6RIF, R-4IRY-YAZT.
func TestPublicStructsConstructWithDeclaredFields(t *testing.T) {
	// Each literal names its fields with values of the declared types, and each
	// read compares a field against a value of the declared type, so this
	// compiles only if every declared field exists with its declared type.
	name, count, status := "delta", 7, widget.StatusPaused
	w := widget.Widget{Name: name, Count: count, Status: status}
	if w.Name != name || w.Count != count || w.Status != status {
		t.Errorf("Widget = %+v", w)
	}
	rawName, rawCount, rawStatus := " delta ", "7", "paused"
	sub := widget.Submission{Name: rawName, Count: rawCount, Status: rawStatus}
	if sub.Name != rawName || sub.Count != rawCount || sub.Status != rawStatus {
		t.Errorf("Submission = %+v", sub)
	}
	nameErr, countErr, statusErr := "name error", "count error", "status error"
	errs := widget.FieldErrors{Name: nameErr, Count: countErr, Status: statusErr}
	if errs.Name != nameErr || errs.Count != countErr || errs.Status != statusErr {
		t.Errorf("FieldErrors = %+v", errs)
	}
}

// R-QJKC-BC01, R-QY74-WKWD, R-QZF1-ACN2.
func TestStatuses(t *testing.T) {
	declared := struct {
		statuses func() []widget.Status
	}{widget.Statuses}
	want := []widget.Status{widget.StatusActive, widget.StatusPaused, widget.StatusRetired}
	got := declared.statuses()
	if !slices.Equal(got, want) {
		t.Fatalf("Statuses = %v, want %v", got, want)
	}
	got[0] = widget.StatusRetired
	got[1] = widget.StatusActive
	if !slices.Equal(declared.statuses(), want) {
		t.Fatal("mutating a returned slice changed later statuses")
	}
}

// R-QQVQ-LYG7, R-RIXF-EOI6.
func TestFieldErrorsAny(t *testing.T) {
	declared := struct {
		anyError func(widget.FieldErrors) bool
	}{widget.FieldErrors.Any}
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
		if got := declared.anyError(e); got != (bits != 0) {
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
	declared := struct {
		newStore func() *widget.Store
		all      func(*widget.Store) []widget.Widget
	}{widget.NewStore, (*widget.Store).All}
	first, second := declared.newStore(), declared.newStore()
	want := initialWidgets()
	if !slices.Equal(declared.all(first), want) || !slices.Equal(declared.all(second), want) {
		t.Fatal("new stores do not contain the exact starting widgets")
	}
	created, errs := first.Create(widget.Submission{Name: "delta", Count: "5", Status: "active"})
	if errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(second.All(), want) || !slices.Equal(widget.NewStore().All(), want) {
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
	declared := struct {
		create func(*widget.Store, widget.Submission) (widget.Widget, widget.FieldErrors)
	}{(*widget.Store).Create}
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
		got, errs := declared.create(s, tc.sub)
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

// R-RHPJ-0WRH, R-RML4-JZQ9, R-4IRY-YAZT, R-R5IJ-77CJ.
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
