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

// R-ZMK6-SZTW.
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

// R-W66F-SE72.
func TestMaxNameRunesConstant(t *testing.T) {
	if externalMaxNameRunesInt8 != 40 || externalMaxNameRunesFloat != 40 {
		t.Errorf("MaxNameRunes = %d, %v; want 40", externalMaxNameRunesInt8, externalMaxNameRunesFloat)
	}
}

// R-XBN0-SLEO.
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

// R-I2W8-NTQ6, R-ISI4-P0AR, R-EBQ5-GVM3.
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

// R-VLG5-AAL9, R-XV5E-WX9S, R-YH3L-SSMA.
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

// R-WPOT-WQ26, R-Z1TW-AW83.
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

// R-EFDU-M6U6, R-EJ1J-RI29, R-EYW8-QIPA, R-GV6M-5O86.
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
	created, errs := first.Create(widget.Draft{Name: "delta", Count: 5, Status: widget.StatusActive})
	if errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(second.All(), want) || !slices.Equal(widget.NewStore().All(), want) {
		t.Fatal("a write changed another store or later store's starting widgets")
	}
	if _, errs := second.Create(widget.Draft{Name: "epsilon", Count: 6, Status: widget.StatusPaused}); errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(first.All(), append(want, created)) {
		t.Fatal("second store's write changed first store")
	}
}

// R-F1C1-I26O.
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
	if _, errs := s.Create(widget.Draft{Name: "delta", Count: 1, Status: widget.StatusActive}); errs.Any() {
		t.Fatal(errs)
	}
	if !slices.Equal(old, want) {
		t.Fatal("create modified an earlier snapshot")
	}
}

// R-APP9-2RBW.
func TestDraftFields(t *testing.T) {
	name, count, status := " raw ", -7, widget.StatusPaused
	d := widget.Draft{Name: name, Count: count, Status: status}
	if d.Name != name || d.Count != count || d.Status != status {
		t.Errorf("Draft = %+v", d)
	}
}

// R-AN9G-B7UI, R-EU0N-7FQI, R-EWGF-YZ7W.
func TestStatusEnum(t *testing.T) {
	enum := struct {
		call func(widget.Status) []string
	}{call: widget.Status.Enum}.call
	statuses := widget.Statuses()
	want := make([]string, len(statuses))
	for i, status := range statuses {
		want[i] = string(status)
	}
	for _, receiver := range []widget.Status{"", "unknown", widget.StatusActive, widget.StatusPaused, widget.StatusRetired} {
		got := enum(receiver)
		if !slices.Equal(got, want) {
			t.Fatalf("Enum(%q) = %v, want %v", receiver, got, want)
		}
		got[0] = "changed"
		if !slices.Equal(enum(receiver), want) || !slices.Equal(widget.Statuses(), statuses) {
			t.Fatal("Enum slice mutation changed allowed values")
		}
	}
}

// R-EMP8-WTAC, R-F7FJ-EWW5.
func TestParseSubmissionCopiesNameWithoutJudgingIt(t *testing.T) {
	parse := struct {
		call func(widget.Submission) (widget.Draft, widget.FieldErrors)
	}{call: widget.ParseSubmission}.call
	for _, name := range []string{"", " \t", " alpha\u2003", strings.Repeat("é", widget.MaxNameRunes+1), "\xff"} {
		sub := widget.Submission{Name: name, Count: "0", Status: "active"}
		d, errs := parse(sub)
		if d.Name != name || errs.Name != "" {
			t.Errorf("ParseSubmission(%q) = %+v, %+v", name, d, errs)
		}
	}
}

// R-F9VC-6GDJ, R-FCB4-XZUX.
func TestParseCount(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		raw  string
		want int
		err  string
	}{
		{"", 0, widget.CountNotWholeMessage}, {" \u2003\t", 0, widget.CountNotWholeMessage},
		{"1.0", 0, widget.CountNotWholeMessage}, {"1e2", 0, widget.CountNotWholeMessage},
		{"0x10", 0, widget.CountNotWholeMessage}, {"1_000", 0, widget.CountNotWholeMessage},
		{"1 2", 0, widget.CountNotWholeMessage}, {"text", 0, widget.CountNotWholeMessage},
		{strings.Repeat("9", 100), 0, widget.CountNotWholeMessage},
		{"-" + strings.Repeat("9", 100), 0, widget.CountNotWholeMessage},
		{strconv.FormatUint(uint64(maxInt)+1, 10), 0, widget.CountNotWholeMessage},
		{"-1", -1, ""}, {"\t-12\u2003", -12, ""}, {strconv.Itoa(-maxInt - 1), -maxInt - 1, ""},
		{"0", 0, ""}, {"-0", 0, ""}, {"+7", 7, ""}, {"007", 7, ""},
		{" \u00a042\n", 42, ""}, {strconv.Itoa(maxInt), maxInt, ""},
	} {
		t.Run(strconv.Quote(tc.raw), func(t *testing.T) {
			d, errs := widget.ParseSubmission(widget.Submission{Count: tc.raw, Status: "active"})
			if d.Count != tc.want || errs.Count != tc.err {
				t.Errorf("ParseSubmission count %q = %d, %q; want %d, %q", tc.raw, d.Count, errs.Count, tc.want, tc.err)
			}
		})
	}
}

// R-FFYU-3B30.
func TestParseStatus(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want widget.Status
		err  string
	}{
		{"", "", widget.StatusNotAllowedMessage}, {" \t", "", widget.StatusNotAllowedMessage},
		{"Active", "", widget.StatusNotAllowedMessage}, {"ACTIVE", "", widget.StatusNotAllowedMessage},
		{"pending", "", widget.StatusNotAllowedMessage}, {"active paused", "", widget.StatusNotAllowedMessage},
		{"active", widget.StatusActive, ""}, {"paused", widget.StatusPaused, ""},
		{"retired", widget.StatusRetired, ""}, {"\u2003active\n", widget.StatusActive, ""},
	} {
		d, errs := widget.ParseSubmission(widget.Submission{Count: "0", Status: tc.raw})
		if d.Status != tc.want || errs.Status != tc.err {
			t.Errorf("ParseSubmission status %q = %q, %q", tc.raw, d.Status, errs.Status)
		}
	}
}

// R-ENX5-AL11, R-FIEM-UUKE, R-FM2C-05SH, R-FOI4-RP9V, R-FQXX-J8R9, R-FTDQ-AS8N, R-GAGB-NKMD.
func TestCheckNames(t *testing.T) {
	check := struct {
		call func(*widget.Store, widget.Draft) widget.FieldErrors
	}{call: (*widget.Store).Check}.call
	s := widget.NewStore()
	if _, errs := s.Create(widget.Draft{Name: " new name ", Count: 0, Status: widget.StatusActive}); errs.Any() {
		t.Fatal(errs)
	}
	before := s.All()
	for _, tc := range []struct{ name, want string }{
		{"", widget.NameRequiredMessage}, {" \t\r\n\u2003", widget.NameRequiredMessage},
		{strings.Repeat("x", widget.MaxNameRunes+1), widget.NameTooLongMessage},
		{strings.Repeat("é", widget.MaxNameRunes+1), widget.NameTooLongMessage},
		{"  " + strings.Repeat("é", widget.MaxNameRunes) + " \u00a0", ""},
		{strings.Repeat("x", widget.MaxNameRunes), ""},
		{" alpha\t", widget.NameTakenMessage}, {"beta", widget.NameTakenMessage}, {"gamma", widget.NameTakenMessage},
		{" \u2003new name\t", widget.NameTakenMessage},
		{"Alpha", ""}, {"ALPHA", ""}, {"distinct", ""}, {"\xff", ""},
	} {
		errs := check(s, widget.Draft{Name: tc.name, Count: 0, Status: widget.StatusActive})
		if errs.Name != tc.want {
			t.Errorf("Check(%q).Name = %q; want %q", tc.name, errs.Name, tc.want)
		}
		if !slices.Equal(s.All(), before) {
			t.Fatal("Check changed widgets or order")
		}
	}
}

// R-FVTJ-2BQ1, R-FZH8-7MY4.
func TestCheckCounts(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, count := range []int{-maxInt - 1, -12, -1, 0, 1, maxInt} {
		want := ""
		if count < 0 {
			want = widget.CountNegativeMessage
		}
		errs := widget.NewStore().Check(widget.Draft{Name: "delta", Count: count, Status: widget.StatusActive})
		if errs.Count != want {
			t.Errorf("Check count %d = %q; want %q", count, errs.Count, want)
		}
	}
}

// R-G1X0-Z6FI, R-G4CT-QPWW.
func TestCheckStatuses(t *testing.T) {
	for _, status := range []widget.Status{"", "Active", "active ", " active", "pending", widget.StatusActive, widget.StatusPaused, widget.StatusRetired} {
		want := widget.StatusNotAllowedMessage
		if slices.Contains(widget.Statuses(), status) {
			want = ""
		}
		errs := widget.NewStore().Check(widget.Draft{Name: "delta", Count: 0, Status: status})
		if errs.Status != want {
			t.Errorf("Check status %q = %q; want %q", status, errs.Status, want)
		}
	}
}

// R-G6SM-I9EA, R-GAGB-NKMD, R-GCW4-F43R, R-GIZM-BYT8, R-EQCY-24IF.
func TestCheckAndCreateRejections(t *testing.T) {
	create := struct {
		call func(*widget.Store, widget.Draft) (widget.Widget, widget.FieldErrors)
	}{call: (*widget.Store).Create}.call
	for bits := range 8 {
		s := widget.NewStore()
		before := s.All()
		d := widget.Draft{Name: " delta ", Count: 0, Status: widget.StatusActive}
		want := widget.FieldErrors{}
		if bits&1 != 0 {
			d.Name = " \t"
			want.Name = widget.NameRequiredMessage
		}
		if bits&2 != 0 {
			d.Count = -1
			want.Count = widget.CountNegativeMessage
		}
		if bits&4 != 0 {
			d.Status = "invalid"
			want.Status = widget.StatusNotAllowedMessage
		}
		checked := s.Check(d)
		if checked != want {
			t.Fatalf("Check(%+v) = %+v; want %+v", d, checked, want)
		}
		if !slices.Equal(s.All(), before) {
			t.Fatal("Check changed widgets")
		}
		w, errs := create(s, d)
		if errs != checked {
			t.Fatalf("Create errors %+v differ from Check %+v", errs, checked)
		}
		if want.Any() && (w != (widget.Widget{}) || !slices.Equal(s.All(), before)) {
			t.Fatal("rejection returned widget or changed store")
		}
	}
	// All name rules must agree too, including duplicate and rune limit.
	for _, name := range []string{"alpha", " alpha ", strings.Repeat("é", widget.MaxNameRunes+1), strings.Repeat("é", widget.MaxNameRunes), "Alpha"} {
		s := widget.NewStore()
		d := widget.Draft{Name: name, Count: 2, Status: widget.StatusPaused}
		before, checked := s.All(), s.Check(d)
		w, errs := create(s, d)
		if errs != checked {
			t.Fatalf("Create(%q) errors differ from Check", name)
		}
		if errs.Any() && (w != (widget.Widget{}) || !slices.Equal(s.All(), before)) {
			t.Fatal("rejection changed store")
		}
	}
}

// R-F4ZQ-NDER, R-FIEM-UUKE, R-GFBX-6NL5.
func TestAcceptedCreationOrder(t *testing.T) {
	s := widget.NewStore()
	for _, d := range []widget.Draft{
		{Name: "\u2003 delta \t", Count: 7, Status: widget.StatusActive},
		{Name: "omega", Count: 0, Status: widget.StatusPaused},
		{Name: "epsilon", Count: 12, Status: widget.StatusRetired},
	} {
		before := s.All()
		want := widget.Widget{Name: strings.TrimSpace(d.Name), Count: d.Count, Status: d.Status}
		got, errs := s.Create(d)
		if errs.Any() || got != want || !slices.Equal(s.All(), append(before, want)) {
			t.Fatalf("Create(%+v) = %+v, %+v; All=%+v", d, got, errs, s.All())
		}
	}
}

// R-GMNB-HA1B, R-GQB0-ML9E.
func TestConcurrentCreationChecksAndSnapshots(t *testing.T) {
	s := widget.NewStore()
	const writers = 64
	start := make(chan struct{})
	results := make(chan widget.Widget, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			d := widget.Draft{Name: fmt.Sprintf("widget-%d", i), Count: i, Status: widget.StatusActive}
			created, errs := s.Create(d)
			if errs.Any() {
				t.Errorf("Create(%s): %+v", d.Name, errs)
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
				errs := s.Check(widget.Draft{Name: "alpha", Count: -1, Status: "bad"})
				if errs != (widget.FieldErrors{Name: widget.NameTakenMessage, Count: widget.CountNegativeMessage, Status: widget.StatusNotAllowedMessage}) {
					t.Errorf("concurrent Check = %+v", errs)
				}
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
		t.Errorf("accepted %d; want %d", accepted, writers)
	}
}

// R-GSQT-E4QS, R-GMNB-HA1B.
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
			w, errs := s.Create(widget.Draft{Name: name, Count: i, Status: widget.StatusPaused})
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
		t.Fatalf("accepted = %d; want 1", accepted)
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
