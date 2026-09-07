package store_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/dory/internal/store"
)

func TestPublicConstantsAndShapes(t *testing.T) {
	// R-IFMK-HS5B
	if got := []store.Kind{store.KindNote, store.KindPrompt, store.KindReport, store.KindTranscript}; !slices.Equal(got, []store.Kind{"note", "prompt", "report", "transcript"}) {
		t.Fatalf("kinds = %v", got)
	}
	if store.PageSize != 20 || store.PreviewRunes != 160 {
		t.Fatalf("PageSize = %d, PreviewRunes = %d", store.PageSize, store.PreviewRunes)
	}

	// R-IGUG-VJW0
	assertFields(t, store.Entry{}, []field{
		{"ID", reflect.TypeFor[int64]()},
		{"Address", reflect.TypeFor[string]()},
		{"Kind", reflect.TypeFor[store.Kind]()},
		{"Text", reflect.TypeFor[string]()},
		{"Raw", reflect.TypeFor[json.RawMessage]()},
		{"Created", reflect.TypeFor[time.Time]()},
	})
	assertFields(t, store.Hit{}, []field{
		{"ID", reflect.TypeFor[int64]()},
		{"Address", reflect.TypeFor[string]()},
		{"Kind", reflect.TypeFor[store.Kind]()},
		{"Preview", reflect.TypeFor[string]()},
	})
	assertFields(t, store.Page{}, []field{
		{"Hits", reflect.TypeFor[[]store.Hit]()},
		{"Page", reflect.TypeFor[int]()},
		{"Pages", reflect.TypeFor[int]()},
		{"Total", reflect.TypeFor[int]()},
	})
	assertFields(t, store.Filter{}, []field{
		{"Kinds", reflect.TypeFor[[]store.Kind]()},
		{"Exclude", reflect.TypeFor[[]store.Kind]()},
		{"Address", reflect.TypeFor[string]()},
		{"Page", reflect.TypeFor[int]()},
	})
}

func TestStorePublicAPIIsOpaqueAndAppendOnly(t *testing.T) {
	// R-IJA9-N3DE
	assertCreateSignature := func(_ func(string, string, string, func() time.Time) (*store.Store, error)) {}
	assertOpenSignature := func(_ func(string, func() time.Time) (*store.Store, error)) {}
	assertCreateSignature(store.Create)
	assertOpenSignature(store.Open)
	var _ interface {
		Close() error
		ID() string
		Root() string
		NextPass() (int, error)
		Add(string, store.Kind, string, json.RawMessage) (int64, error)
		Search(string, store.Filter) (store.Page, error)
		Fetch(int64) (store.Entry, error)
	} = (*store.Store)(nil)
	if store.ErrNotFound == nil {
		t.Fatal("ErrNotFound is nil")
	}

	typ := reflect.TypeFor[store.Store]()
	for i := range typ.NumField() {
		if typ.Field(i).IsExported() {
			t.Errorf("Store field %q is exported", typ.Field(i).Name)
		}
	}
	wantMethods := []string{"Add", "Close", "Fetch", "ID", "NextPass", "Root", "Search"}
	gotMethods := make([]string, reflect.TypeFor[*store.Store]().NumMethod())
	for i := range gotMethods {
		gotMethods[i] = reflect.TypeFor[*store.Store]().Method(i).Name
	}
	if !slices.Equal(gotMethods, wantMethods) {
		t.Fatalf("exported methods = %v, want %v", gotMethods, wantMethods)
	}

	wantPackageExports := []string{
		"Create", "Entry", "ErrNotFound", "Filter", "Hit", "Kind",
		"KindNote", "KindPrompt", "KindReport", "KindTranscript", "Open",
		"Page", "PageSize", "PreviewRunes", "Store",
	}
	gotPackageExports := packageExports(t)
	if !slices.Equal(gotPackageExports, wantPackageExports) {
		t.Fatalf("package exports = %v, want %v", gotPackageExports, wantPackageExports)
	}
}

func TestCreateOpenAndMetadataPersistence(t *testing.T) {
	// R-IKI6-0V43
	path := filepath.Join(t.TempDir(), "session.sqlite")
	createdAt := time.Date(2026, 9, 6, 1, 2, 3, 4, time.UTC)
	now := func() time.Time { return createdAt }
	s, err := store.Create(path, "session-id", "/tool/root", now)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID() != "session-id" || s.Root() != "/tool/root" {
		t.Fatalf("metadata = (%q, %q)", s.ID(), s.Root())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(path, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reopened.ID() != "session-id" || reopened.Root() != "/tool/root" {
		t.Fatalf("reopened metadata = (%q, %q)", reopened.ID(), reopened.Root())
	}

	independent, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = independent.Close() })
	var storedCreated []byte
	if err := independent.QueryRow("SELECT created FROM metadata WHERE singleton = 1").Scan(&storedCreated); err != nil {
		t.Fatal(err)
	}
	var decodedCreated time.Time
	if err := decodedCreated.UnmarshalBinary(storedCreated); err != nil {
		t.Fatal(err)
	}
	if decodedCreated != createdAt {
		t.Fatalf("persisted creation time = %v, want %v", decodedCreated, createdAt)
	}
	if _, err := store.Create(path, "other", "other", now); err == nil {
		t.Fatal("Create accepted an existing file")
	}
	if _, err := store.Open(filepath.Join(t.TempDir(), "absent.sqlite"), now); err == nil {
		t.Fatal("Open accepted an absent file")
	}
}

func packageExports(t *testing.T) []string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate store package test")
	}
	directory := filepath.Dir(filename)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	fileSet := token.NewFileSet()
	var exports []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(directory, entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Recv == nil && declaration.Name.IsExported() {
					exports = append(exports, declaration.Name.Name)
				}
			case *ast.GenDecl:
				for _, specification := range declaration.Specs {
					switch specification := specification.(type) {
					case *ast.TypeSpec:
						if specification.Name.IsExported() {
							exports = append(exports, specification.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range specification.Names {
							if name.IsExported() {
								exports = append(exports, name.Name)
							}
						}
					}
				}
			}
		}
	}
	slices.Sort(exports)
	return exports
}

func TestAddAndFetchRoundTrip(t *testing.T) {
	// R-ILQ2-EMUS
	times := []time.Time{
		time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 6, 1, 0, 0, 1, time.UTC),
		time.Date(2026, 9, 6, 2, 0, 0, 2, time.FixedZone("test", -5*60*60)),
		time.Date(2026, 9, 6, 3, 0, 0, 3, time.UTC),
		time.Date(2026, 9, 6, 4, 0, 0, 4, time.UTC),
	}
	clockCalls := 0
	s := createStore(t, func() time.Time {
		value := times[clockCalls]
		clockCalls++
		return value
	})
	kinds := []store.Kind{store.KindNote, store.KindPrompt, store.KindReport, store.KindTranscript}
	raws := []json.RawMessage{nil, json.RawMessage(`{"prompt":true}`), nil, json.RawMessage(`{"event":"done"}`)}
	var previous int64
	for i, kind := range kinds {
		id, err := s.Add("root.child", kind, "text "+string(kind), raws[i])
		if err != nil {
			t.Fatal(err)
		}
		if id != int64(i+1) {
			t.Fatalf("id = %d, want %d", id, i+1)
		}
		previous = id
		entry, err := s.Fetch(id)
		if err != nil {
			t.Fatal(err)
		}
		if entry.ID != id || entry.Address != "root.child" || entry.Kind != kind || entry.Text != "text "+string(kind) {
			t.Fatalf("entry = %#v", entry)
		}
		if !slices.Equal(entry.Raw, raws[i]) {
			t.Fatalf("raw = %s, want %s", entry.Raw, raws[i])
		}
		if !entry.Created.Equal(times[i+1]) {
			t.Fatalf("created = %v, want %v", entry.Created, times[i+1])
		}
	}
	if clockCalls != len(times) {
		t.Fatalf("clock calls = %d, want %d", clockCalls, len(times))
	}
	if _, err := s.Fetch(previous + 100); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing Fetch error = %v", err)
	}
}

func TestNextPassCountsOnlyRootPrompts(t *testing.T) {
	// R-IMXY-SELH
	now := func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) }
	path := filepath.Join(t.TempDir(), "session.sqlite")
	s, err := store.Create(path, "id", "root", now)
	if err != nil {
		t.Fatal(err)
	}
	assertNextPass(t, s, 1)
	if _, err := s.Add("root", store.KindNote, "text", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("root.child", store.KindPrompt, "text", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("root", store.KindReport, "text", nil); err != nil {
		t.Fatal(err)
	}
	assertNextPass(t, s, 1)
	if _, err := s.Add("first-root", store.KindPrompt, "text", nil); err != nil {
		t.Fatal(err)
	}
	assertNextPass(t, s, 2)
	if _, err := s.Add("another-root", store.KindPrompt, "text", nil); err != nil {
		t.Fatal(err)
	}
	assertNextPass(t, s, 3)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(path, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	assertNextPass(t, s, 3)
}

type field struct {
	name string
	typ  reflect.Type
}

func assertFields(t *testing.T, value any, want []field) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ, typ.NumField(), len(want))
	}
	for i, expected := range want {
		actual := typ.Field(i)
		if actual.Name != expected.name || actual.Type != expected.typ {
			t.Errorf("%s field %d = %s %s, want %s %s", typ, i, actual.Name, actual.Type, expected.name, expected.typ)
		}
	}
}

func createStore(t *testing.T, now func() time.Time) *store.Store {
	t.Helper()
	s, err := store.Create(filepath.Join(t.TempDir(), "session.sqlite"), "id", "root", now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func assertNextPass(t *testing.T, s *store.Store, want int) {
	t.Helper()
	got, err := s.NextPass()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("NextPass = %d, want %d", got, want)
	}
}
