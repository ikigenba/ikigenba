package widget_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/dummy"
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

// R-G7RO-91KZ.
func TestMessageConstants(t *testing.T) {
	for _, message := range []string{externalNameRequiredMessage, externalNameTooLongMessage, externalNameTakenMessage, externalCountNotWholeMessage, externalCountNegativeMessage, externalStatusNotAllowedMessage} {
		if message == "" {
			t.Fatal("empty validation message")
		}
	}
}

// R-KVZI-S6MW, R-ISI4-P0AR, R-EBQ5-GVM3.
func TestPublicStructsConstructWithDeclaredFields(t *testing.T) {
	// Each literal names its fields with values of the declared types, and each
	// read compares a field against a value of the declared type, so this
	// compiles only if every declared field exists with its declared type.
	id, name, count, status := "known-id", "delta", 7, widget.StatusPaused
	w := widget.Widget{ID: id, Name: name, Count: count, Status: status}
	if w.ID != id || w.Name != name || w.Count != count || w.Status != status {
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

func knownSource() io.Reader {
	data := make([]byte, 8*256)
	for i := range 256 {
		data[i*8] = byte(i)
	}
	return bytes.NewReader(data)
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

var ctx = context.Background()

func openDB(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: dummy.Migrations(), Now: func() time.Time { return time.Unix(123, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d
}
func newStore(t *testing.T, source io.Reader) (*widget.Store, *db.DB) {
	t.Helper()
	d := openDB(t, filepath.Join(t.TempDir(), "widgets.db"))
	return widget.NewStore(d, source), d
}
func all(t *testing.T, s *widget.Store) []widget.Widget {
	t.Helper()
	got, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func create(t *testing.T, s *widget.Store, d widget.Draft) widget.Widget {
	t.Helper()
	w, e, err := s.Create(ctx, d)
	if err != nil || e.Any() {
		t.Fatalf("Create: %+v %v", e, err)
	}
	return w
}
func draft(name string) widget.Draft {
	return widget.Draft{Name: name, Count: 7, Status: widget.StatusActive}
}

// R-EXKM-XZU8.
func TestUnreachableConstant(_ *testing.T) {
	const value string = widget.Unreachable
	_ = value
}

// R-E8TO-J993, R-EA1K-X0ZS, R-ENGH-4I5F, R-F1C1-I26O, R-EPW9-W1MT, R-ER46-9TDI.
func TestStoreEmptySnapshotsAndReopen(t *testing.T) {
	constructor := struct {
		call func(*db.DB, io.Reader) *widget.Store
	}{widget.NewStore}.call
	list := struct {
		call func(*widget.Store, context.Context) ([]widget.Widget, error)
	}{(*widget.Store).All}.call
	path := filepath.Join(t.TempDir(), "widgets.db")
	d := openDB(t, path)
	s := constructor(d, knownSource())
	empty, err := list(s, ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %v", empty, err)
	}
	var want []widget.Widget
	for _, name := range []string{"zeta", "alpha", "mu"} {
		want = append(want, create(t, s, draft(name)))
	}
	snapshot := all(t, s)
	snapshot[0].Name = "changed"
	snapshot = append(snapshot, widget.Widget{Name: "extra"})
	if len(snapshot) != len(want)+1 {
		t.Fatal("snapshot append failed")
	}
	if !slices.Equal(all(t, s), want) {
		t.Fatal("snapshot mutation changed store")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	later := widget.NewStore(openDB(t, path), knownSource())
	if !slices.Equal(all(t, later), want) {
		t.Fatal("reopen lost values or order")
	}
	want = append(want, create(t, later, draft("omega")))
	if !slices.Equal(all(t, later), want) {
		t.Fatal("later creation not appended")
	}
}

// R-EL0O-CYO1, R-EM8K-QQEQ.
func TestMigrationSchema(t *testing.T) {
	_, d := newStore(t, knownSource())
	err := d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return err
		}
		defer func() {
			if err := rows.Close(); err != nil {
				t.Error(err)
			}
		}()
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !slices.Equal(names, []string{"schema_migrations", "widgets"}) {
			t.Errorf("tables: %v", names)
		}
		columns, err := tx.QueryContext(ctx, "PRAGMA table_info(widgets)")
		if err != nil {
			return err
		}
		defer func() {
			if err := columns.Close(); err != nil {
				t.Error(err)
			}
		}()
		type column struct {
			name, kind  string
			notnull, pk int
		}
		var got []column
		for columns.Next() {
			var cid int
			var c column
			var defaultValue any
			if err := columns.Scan(&cid, &c.name, &c.kind, &c.notnull, &defaultValue, &c.pk); err != nil {
				return err
			}
			got = append(got, c)
		}
		want := []column{{"seq", "INTEGER", 0, 1}, {"id", "TEXT", 1, 0}, {"name", "TEXT", 1, 0}, {"count", "INTEGER", 1, 0}, {"status", "TEXT", 1, 0}}
		if len(got) != len(want) {
			t.Errorf("columns: %+v", got)
		} else {
			got[0].notnull = want[0].notnull // The seq column has no declared nullability requirement.
			if !slices.Equal(got, want) {
				t.Errorf("columns: %+v", got)
			}
		}
		return columns.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-EOOD-I9W4.
func TestAdoptExistingWidgets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widgets.db")
	migration := fstest.MapFS{"0001_existing.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE widgets (seq INTEGER PRIMARY KEY, id TEXT NOT NULL, name TEXT NOT NULL, count INTEGER NOT NULL, status TEXT NOT NULL);`)}}
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: migration, Now: func() time.Time { return time.Unix(123, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	want := []widget.Widget{{ID: "wgt_1111111111111111", Name: "first", Count: 0, Status: widget.StatusRetired}, {ID: "wgt_2222222222222222", Name: "second", Count: 9, Status: widget.StatusPaused}}
	err = d.Write(ctx, func(tx *sql.Tx) error {
		for _, i := range []int{1, 0} {
			w := want[i]
			if _, err := tx.ExecContext(ctx, "INSERT INTO widgets VALUES (?, ?, ?, ?, ?)", i+5, w.ID, w.Name, w.Count, w.Status); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "DROP TABLE schema_migrations")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	s := widget.NewStore(openDB(t, path), knownSource())
	if !slices.Equal(all(t, s), want) {
		t.Fatal("adoption changed rows or order")
	}
}

// R-EB9H-ASQH, R-ECHD-OKH6, R-FIEM-UUKE, R-EURV-F4LL, R-EVZR-SWCA, R-EYFK-KFTO, R-EZNG-Y7KD, R-F0VD-BZB2, R-F239-PR1R, R-F3B6-3ISG, R-F4J2-HAJ5, R-F5QY-V29U, R-F6YV-8U0J, R-F86R-MLR8, R-F9EO-0DHX, R-FAMK-E58M.
func TestCheckAndCreateRules(t *testing.T) {
	check := struct {
		call func(*widget.Store, context.Context, widget.Draft) (widget.FieldErrors, error)
	}{(*widget.Store).Check}.call
	add := struct {
		call func(*widget.Store, context.Context, widget.Draft) (widget.Widget, widget.FieldErrors, error)
	}{(*widget.Store).Create}.call
	maxInt := int(^uint(0) >> 1)
	cases := []struct {
		d widget.Draft
		e widget.FieldErrors
	}{
		{draft(""), widget.FieldErrors{Name: widget.NameRequiredMessage}},
		{draft(" \t\u2003"), widget.FieldErrors{Name: widget.NameRequiredMessage}},
		{draft(strings.Repeat("x", 41)), widget.FieldErrors{Name: widget.NameTooLongMessage}},
		{draft(strings.Repeat("é", 41)), widget.FieldErrors{Name: widget.NameTooLongMessage}},
		{draft(" alpha\t"), widget.FieldErrors{Name: widget.NameTakenMessage}},
		{draft("Alpha"), widget.FieldErrors{}},
		{draft("ALPHA"), widget.FieldErrors{}},
		{draft("  " + strings.Repeat("é", 40) + "\u2003"), widget.FieldErrors{}},
		{draft(strings.Repeat("x", 40)), widget.FieldErrors{}},
		{draft("\xff"), widget.FieldErrors{}},
	}
	for _, count := range []int{-maxInt - 1, -1, 0, 1, maxInt} {
		d := draft("new")
		d.Count = count
		e := widget.FieldErrors{}
		if count < 0 {
			e.Count = widget.CountNegativeMessage
		}
		cases = append(cases, struct {
			d widget.Draft
			e widget.FieldErrors
		}{d, e})
	}
	for _, status := range []widget.Status{"", "Active", " active", "active ", "pending", widget.StatusActive, widget.StatusPaused, widget.StatusRetired} {
		d := draft("new")
		d.Status = status
		e := widget.FieldErrors{}
		if !slices.Contains(widget.Statuses(), status) {
			e.Status = widget.StatusNotAllowedMessage
		}
		cases = append(cases, struct {
			d widget.Draft
			e widget.FieldErrors
		}{d, e})
	}
	for bits := range 8 {
		d := draft(" new ")
		e := widget.FieldErrors{}
		if bits&1 != 0 {
			d.Name = " "
			e.Name = widget.NameRequiredMessage
		}
		if bits&2 != 0 {
			d.Count = -1
			e.Count = widget.CountNegativeMessage
		}
		if bits&4 != 0 {
			d.Status = "bad"
			e.Status = widget.StatusNotAllowedMessage
		}
		cases = append(cases, struct {
			d widget.Draft
			e widget.FieldErrors
		}{d, e})
	}
	for i, tc := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s, _ := newStore(t, knownSource())
			create(t, s, draft("alpha"))
			before := all(t, s)
			checked, err := check(s, ctx, tc.d)
			if err != nil || checked != tc.e {
				t.Fatalf("Check: %+v %v want %+v", checked, err, tc.e)
			}
			if !slices.Equal(all(t, s), before) {
				t.Fatal("Check changed widgets")
			}
			w, e, err := add(s, ctx, tc.d)
			if err != nil || e != checked {
				t.Fatalf("Create: %+v %v", e, err)
			}
			if e.Any() {
				if w != (widget.Widget{}) || !slices.Equal(all(t, s), before) {
					t.Fatal("refusal changed store")
				}
			} else {
				if w.Name != strings.TrimSpace(tc.d.Name) || w.Count != tc.d.Count || w.Status != tc.d.Status || !slices.Equal(all(t, s), append(before, w)) {
					t.Fatalf("creation: %+v", w)
				}
			}
		})
	}
}

type countedSource struct {
	source  io.Reader
	lengths []int
}

func (r *countedSource) Read(p []byte) (int, error) {
	r.lengths = append(r.lengths, len(p))
	return r.source.Read(p)
}

// R-4I59-DCKR, R-ESC2-NL47, R-ETJZ-1CUW, R-4GXC-ZKU2.
func TestFailureRecoveryAndNoDraw(t *testing.T) {
	source := &countedSource{source: knownSource()}
	s, d := newStore(t, source)
	create(t, s, draft("kept"))
	before := all(t, s)
	draws := slices.Clone(source.lengths)
	for _, value := range []widget.Draft{draft("new"), {Name: "", Count: -1, Status: "bad"}, draft("kept")} {
		d.SetFailing(true)
		if _, err := s.All(ctx); err == nil {
			t.Error("All succeeded while failing")
		}
		if _, err := s.Check(ctx, value); err == nil {
			t.Error("Check succeeded while failing")
		}
		if _, _, err := s.Create(ctx, value); err == nil {
			t.Error("Create succeeded while failing")
		}
		if !slices.Equal(source.lengths, draws) {
			t.Fatal("failed call drew ID")
		}
		d.SetFailing(false)
		if !slices.Equal(all(t, s), before) {
			t.Fatal("failure changed widgets")
		}
		if _, err := s.Check(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []widget.Draft{{Name: "", Count: -1, Status: "bad"}, draft("kept")} {
		if _, _, err := s.Create(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(source.lengths, draws) {
		t.Fatal("refusal or Check or All drew ID")
	}
	create(t, s, draft("recovered"))
}

// R-EG52-TVP9, R-4GXC-ZKU2, R-L0V4-B9LO.
func TestDrawsAndPersistedCollisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "widgets.db")
	d := openDB(t, path)
	source := &countedSource{source: knownSource()}
	s := widget.NewStore(d, source)
	if len(source.lengths) != 0 {
		t.Fatal("constructor drew")
	}
	all(t, s)
	if _, err := s.Check(ctx, draft("first")); err != nil {
		t.Fatal(err)
	}
	if len(source.lengths) != 0 {
		t.Fatal("observation drew")
	}
	w := create(t, s, draft("first"))
	if w.ID != "wgt_0000000000000000" || !slices.Equal(source.lengths, []int{8}) {
		t.Fatalf("first: %+v %v", w, source.lengths)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	source = &countedSource{source: knownSource()}
	s = widget.NewStore(openDB(t, path), source)
	w = create(t, s, draft("second"))
	if w.ID != "wgt_0100000000000000" || !slices.Equal(source.lengths, []int{8, 8}) {
		t.Fatalf("collision: %+v %v", w, source.lengths)
	}
	w = create(t, s, draft("third"))
	if w.ID != "wgt_0200000000000000" || !slices.Equal(source.lengths, []int{8, 8, 8}) {
		t.Fatalf("draw: %+v %v", w, source.lengths)
	}
}

type chunkSource struct{ source io.Reader }

func (r chunkSource) Read(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}
	return r.source.Read(p)
}

// R-EG52-TVP9, R-4GXC-ZKU2.
func TestReadFullAccumulatesShortReads(t *testing.T) {
	source := &countedSource{source: chunkSource{source: bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8})}}
	s, _ := newStore(t, source)
	w := create(t, s, draft("one"))
	if w.ID != "wgt_0102030405060708" || !slices.Equal(source.lengths, []int{8, 6, 4, 2}) {
		t.Fatalf("draw: %+v %v", w, source.lengths)
	}
}
func assertIDs(t *testing.T, widgets []widget.Widget) {
	t.Helper()
	seen := make(map[string]bool)
	for _, w := range widgets {
		if len(w.ID) != 20 || !strings.HasPrefix(w.ID, "wgt_") {
			t.Fatal(w.ID)
		}
		data, err := hex.DecodeString(w.ID[4:])
		if err != nil || len(data) != 8 || hex.EncodeToString(data) != w.ID[4:] || seen[w.ID] {
			t.Fatal(w.ID)
		}
		seen[w.ID] = true
	}
}

type partialFailureSource []byte

func (r partialFailureSource) Read(p []byte) (int, error) { return copy(p, r), io.ErrUnexpectedEOF }

// R-EIKV-LF6N.
func TestFailedDrawDiscardsPartialBytes(t *testing.T) {
	original := rand.Reader
	t.Cleanup(func() { rand.Reader = original })
	var baseline string
	for _, marker := range [][]byte{nil, {0x13, 0x47, 0x82, 0xac, 0x59, 0xd1, 0xe7}, {0xf3, 0xb7, 0x62, 0x0c, 0xa9, 0x31, 0x17}} {
		rand.Reader = knownSource()
		s, _ := newStore(t, partialFailureSource(marker))
		var created []widget.Widget
		for _, d := range []widget.Draft{draft("first"), {Name: " second ", Count: 0, Status: widget.StatusPaused}} {
			before := all(t, s)
			w := create(t, s, d)
			assertIDs(t, []widget.Widget{w})
			if w.Name != strings.TrimSpace(d.Name) || w.Count != d.Count || w.Status != d.Status {
				t.Fatalf("fallback changed fields: %+v", w)
			}
			if !slices.Equal(all(t, s), append(before, w)) {
				t.Fatal("fallback did not append accepted widget")
			}
			created = append(created, w)
		}
		first := created[0]
		if baseline == "" {
			baseline = first.ID
		} else if first.ID != baseline {
			t.Fatal("partial bytes affected fallback ID")
		}
		assertIDs(t, all(t, s))
		if len(marker) > 0 && strings.HasPrefix(first.ID, "wgt_"+hex.EncodeToString(marker)) {
			t.Fatal("partial bytes retained")
		}
	}
}

// R-EJSR-Z6XC.
func TestNilSource(t *testing.T) {
	original := rand.Reader
	rand.Reader = knownSource()
	t.Cleanup(func() { rand.Reader = original })
	first, _ := newStore(t, nil)
	second, _ := newStore(t, nil)
	a, b := create(t, first, draft("first")), create(t, second, draft("second"))
	assertIDs(t, []widget.Widget{a, b})
}

// R-L3AX-2T32, R-GMNB-HA1B, R-FBUG-RWZB.
func TestConcurrentAcceptedCalls(t *testing.T) {
	s, _ := newStore(t, knownSource())
	const n = 32
	start := make(chan struct{})
	results := make(chan widget.Widget, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			w, e, err := s.Create(ctx, draft(fmt.Sprintf("item-%d", i)))
			if err != nil || e.Any() {
				t.Errorf("create: %+v %v", e, err)
				return
			}
			results <- w
		})
		wg.Go(func() {
			<-start
			for range 4 {
				rows, err := s.All(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				if len(rows) > 0 {
					rows[0].Name = "mutation"
				}
				if _, err := s.Check(ctx, draft("unused")); err != nil {
					t.Error(err)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	got := all(t, s)
	if len(got) != n {
		t.Fatalf("count: %d", len(got))
	}
	assertIDs(t, got)
	for w := range results {
		if !slices.Contains(got, w) {
			t.Errorf("lost %+v", w)
		}
	}
}

// R-FD2D-5OQ0, R-GMNB-HA1B.
func TestConcurrentDuplicateCreation(t *testing.T) {
	s, _ := newStore(t, knownSource())
	const n = 32
	start := make(chan struct{})
	results := make(chan bool, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			name := "same"
			if i%2 == 0 {
				name = " \u2003same\t"
			}
			w, e, err := s.Create(ctx, draft(name))
			if err != nil {
				t.Error(err)
				return
			}
			if e.Any() {
				if e != (widget.FieldErrors{Name: widget.NameTakenMessage}) || w != (widget.Widget{}) {
					t.Errorf("rejection: %+v %+v", w, e)
				}
			}
			results <- !e.Any()
		})
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for ok := range results {
		if ok {
			accepted++
		}
	}
	if accepted != 1 || len(all(t, s)) != 1 {
		t.Fatalf("accepted: %d rows: %v", accepted, all(t, s))
	}
}
