package source_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// R-LUJT-NYUS R-LVRQ-1QLH
func TestSpoolContext(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "source.")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sentinel := errors.New("spool refused")
	calls := 0
	var s source.Spool = func() (*os.File, error) { calls++; return f, sentinel }
	base := context.Background()
	if source.SpoolFrom(base) != nil {
		t.Fatal("unexpected spool")
	}
	parent := source.WithSpool(base, s)
	canceled, cancel := context.WithCancel(parent)
	cancel()
	type key struct{}
	for _, ctx := range []context.Context{parent, canceled, context.WithoutCancel(canceled), context.WithValue(parent, key{}, "value")} {
		spool := source.SpoolFrom(ctx)
		if spool == nil {
			t.Fatal("missing spool")
		}
		before := calls
		got, e := spool()
		if calls != before+1 || got != f || !errors.Is(e, sentinel) || !errors.Is(sentinel, e) {
			t.Fatal(calls, got, e)
		}
	}
	if source.SpoolFrom(source.WithSpool(parent, nil)) != nil {
		t.Fatal("nil override lost")
	}
	innerCalls := 0
	inner := source.WithSpool(parent, func() (*os.File, error) { innerCalls++; return nil, nil })
	got, e := source.SpoolFrom(inner)()
	if innerCalls != 1 || got != nil || e != nil || calls != 4 {
		t.Fatal(innerCalls, calls, got, e)
	}
	var makeFile source.Spool = func() (*os.File, error) { return os.CreateTemp(dir, "new.") }
	made, e := source.SpoolFrom(source.WithSpool(base, makeFile))()
	if e != nil {
		t.Fatal(e)
	}
	info, e := made.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatal(info, e)
	}
	if _, e = made.WriteString("source bytes"); e != nil {
		t.Fatal(e)
	}
	if e = made.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(made.Name()); e != nil {
		t.Fatal(e)
	}
}
