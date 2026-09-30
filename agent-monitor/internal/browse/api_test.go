package browse

import (
	"context"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
)

// typed returns v as a T; a call compiles only when v is assignable to T.
func typed[T any](v T) T { return v }

type apiWatcher struct{ changes chan struct{} }

func (apiWatcher) Watch([]string)             {}
func (w apiWatcher) Changes() <-chan struct{} { return w.changes }

type apiTerminal struct {
	keys    chan []byte
	resized chan struct{}
}

func (t apiTerminal) Keys() <-chan []byte      { return t.keys }
func (apiTerminal) Size() (int, int)           { return 80, 24 }
func (t apiTerminal) Resized() <-chan struct{} { return t.resized }

func TestPublicRunSignature(_ *testing.T) {
	// R-L6YX-30R6
	run := typed[func(context.Context, Config, io.Writer) error](Run)
	_ = run
}

func TestConfigFields(t *testing.T) {
	// R-YGNM-KUOK
	home := typed[string]("/home/dev")
	root := typed[fs.FS](fstest.MapFS{})
	color := typed[bool](true)
	watcher := typed[Watcher](apiWatcher{})
	terminal := typed[Terminal](apiTerminal{})
	cfg := Config{Home: home, Root: root, Color: color, Watcher: watcher, Terminal: terminal}
	if cfg.Home != home || cfg.Color != color || cfg.Watcher != watcher || cfg.Terminal != terminal {
		t.Fatalf("Config = %+v", cfg)
	}
	if _, ok := cfg.Root.(fstest.MapFS); !ok {
		t.Fatalf("Root = %T", cfg.Root)
	}
}

func TestWatcherMethods(t *testing.T) {
	// R-YHVI-YMF9
	changes := make(chan struct{})
	w := typed[Watcher](apiWatcher{changes: changes})
	watch := typed[func([]string)](w.Watch)
	changesOf := typed[func() <-chan struct{}](w.Changes)
	watch([]string{"name"})
	if changesOf() != changes {
		t.Fatal("Changes channel lost")
	}
}

func TestTerminalMethods(t *testing.T) {
	// R-YJ3F-CE5Y
	term := apiTerminal{keys: make(chan []byte), resized: make(chan struct{})}
	tm := typed[Terminal](term)
	keys := typed[func() <-chan []byte](tm.Keys)
	size := typed[func() (int, int)](tm.Size)
	resized := typed[func() <-chan struct{}](tm.Resized)
	if cols, rows := size(); keys() != term.keys || resized() != term.resized || cols != 80 || rows != 24 {
		t.Fatal("Terminal methods lost values")
	}
}

func TestMinimumColumns(t *testing.T) {
	// R-0EO5-CU68
	const fromUntyped float64 = MinCols
	if _, ok := any(MinCols).(int); fromUntyped != 40 || !ok {
		t.Fatalf("MinCols: %v", fromUntyped)
	}
}

func TestMinimumRows(t *testing.T) {
	// R-0FW1-QLWX
	const fromUntyped float64 = MinRows
	if _, ok := any(MinRows).(int); fromUntyped != 5 || !ok {
		t.Fatalf("MinRows: %v", fromUntyped)
	}
}
