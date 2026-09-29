package browse

import (
	"context"
	"io"
	"io/fs"
	"reflect"
	"testing"
)

func TestPublicRunSignature(t *testing.T) {
	// R-L6YX-30R6
	if reflect.TypeOf(Run) != reflect.TypeFor[func(context.Context, Config, io.Writer) error]() {
		t.Fatal("Run signature differs")
	}
}

func TestConfigFields(t *testing.T) {
	// R-L86T-GSHV
	got := reflect.TypeFor[Config]()
	want := []struct {
		name string
		kind reflect.Type
	}{
		{"Home", reflect.TypeFor[string]()}, {"Root", reflect.TypeFor[fs.FS]()},
		{"Color", reflect.TypeFor[bool]()}, {"Watcher", reflect.TypeFor[Watcher]()},
		{"Terminal", reflect.TypeFor[Terminal]()},
	}
	if got.NumField() != len(want) {
		t.Fatalf("fields: %d", got.NumField())
	}
	for i, field := range want {
		f := got.Field(i)
		if f.Name != field.name || f.Type != field.kind || f.Anonymous || !f.IsExported() {
			t.Fatalf("field %d: %+v", i, f)
		}
	}
}

func TestWatcherMethods(t *testing.T) {
	// R-L9EP-UK8K
	kind := reflect.TypeFor[Watcher]()
	want := map[string]reflect.Type{"Watch": reflect.TypeFor[func([]string)](), "Changes": reflect.TypeFor[func() <-chan struct{}]()}
	if kind.NumMethod() != len(want) {
		t.Fatalf("methods: %d", kind.NumMethod())
	}
	for name, signature := range want {
		method, ok := kind.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("method %s: %+v", name, method)
		}
	}
}

func TestTerminalMethods(t *testing.T) {
	// R-LAMM-8BZ9
	kind := reflect.TypeFor[Terminal]()
	want := map[string]reflect.Type{"Keys": reflect.TypeFor[func() <-chan []byte](), "Size": reflect.TypeFor[func() (int, int)](), "Resized": reflect.TypeFor[func() <-chan struct{}]()}
	if kind.NumMethod() != len(want) {
		t.Fatalf("methods: %d", kind.NumMethod())
	}
	for name, signature := range want {
		method, ok := kind.MethodByName(name)
		if !ok || method.Type != signature {
			t.Fatalf("method %s: %+v", name, method)
		}
	}
}

func TestMinimumColumns(t *testing.T) {
	// R-0EO5-CU68
	const fromUntyped float64 = MinCols
	if fromUntyped != 40 || reflect.TypeOf(MinCols) != reflect.TypeFor[int]() {
		t.Fatalf("MinCols: %v", fromUntyped)
	}
}

func TestMinimumRows(t *testing.T) {
	// R-0FW1-QLWX
	const fromUntyped float64 = MinRows
	if fromUntyped != 5 || reflect.TypeOf(MinRows) != reflect.TypeFor[int]() {
		t.Fatalf("MinRows: %v", fromUntyped)
	}
}
