package store_test

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-LZFF-71TK R-M0NB-KTK9 R-M1V7-YLAY
func TestBuildSpoolFiles(t *testing.T) {
	s, _, runs := configured(t, 1, 2)
	r := add(t, s, "a")
	calls := 0
	b := build(t, s, r, func(context.Context, int64, time.Duration) error { calls++; return nil })
	var spool source.Spool = b.Spool
	if _, e := os.Stat(runs); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	f, e := spool()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = f.Close() }()
	for _, dir := range []string{runs, filepath.Dir(runs)} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal(info, err)
		}
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != 0 || filepath.Dir(f.Name()) != runs || !strings.HasPrefix(filepath.Base(f.Name()), r.ID+".") || filepath.Base(f.Name()) == r.ID+".db" {
		t.Fatal(info, f.Name(), e)
	}
	offset, e := f.Seek(0, io.SeekCurrent)
	if e != nil || offset != 0 {
		t.Fatal(offset, e)
	}
	if _, e = f.WriteString("download bytes"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(f)
	if e != nil || string(data) != "download bytes" {
		t.Fatal(string(data), e)
	}
	before := snapshot(t, runs)
	second, e := spool()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = second.Close() }()
	if _, exists := before[filepath.Base(second.Name())]; exists || second.Name() == f.Name() || calls != 0 {
		t.Fatal(second.Name(), calls)
	}
	if e = b.Add(ctx, []source.Row{{SourceID: "one"}}); e != nil {
		t.Fatal(e)
	}
	if entries := snapshot(t, runs); len(entries) != 3 || entries[filepath.Base(f.Name())] != "download bytes" || calls != 1 {
		t.Fatal(entries, calls)
	}
}

// R-M4B0-Q4SC
func TestSpoolCleanup(t *testing.T) {
	for _, outcome := range []string{"seal", "discard", "removed", "removed discard", "canceled", "failed step"} {
		t.Run(outcome, func(t *testing.T) {
			s, _, runs := configured(t, store.DefaultChunkBytes, 2)
			r := add(t, s, "a")
			var path string
			calls := 0
			b := build(t, s, r, func(context.Context, int64, time.Duration) error {
				calls++
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("spool present at step: %v", err)
				}
				if outcome == "failed step" {
					return errors.New("step failure")
				}
				return nil
			})
			f, e := b.Spool()
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = f.Close() }()
			path = f.Name()
			if _, e = f.WriteString("zip bytes"); e != nil {
				t.Fatal(e)
			}
			if outcome == "removed" || outcome == "removed discard" {
				if e = os.Remove(path); e != nil {
					t.Fatal(e)
				}
			}
			if outcome == "discard" || outcome == "removed discard" {
				if e = b.Discard(); e != nil {
					t.Fatal(e)
				}
			} else {
				if e = b.Add(ctx, []source.Row{{SourceID: "one"}}); e != nil {
					t.Fatal(e)
				}
				sealctx := ctx
				if outcome == "canceled" {
					var cancel context.CancelFunc
					sealctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				e = b.Seal(sealctx)
				wantError := outcome == "canceled" || outcome == "failed step"
				if (e != nil) != wantError {
					t.Fatal(e)
				}
				if !wantError && calls != 1 {
					t.Fatal(calls)
				}
			}
			if _, e = os.Stat(path); !errors.Is(e, os.ErrNotExist) {
				t.Fatal(e)
			}
			entries := snapshot(t, runs)
			if outcome == "discard" || outcome == "removed discard" || outcome == "canceled" {
				if len(entries) != 0 {
					t.Fatal(entries)
				}
			} else {
				if len(entries) != 1 {
					t.Fatal(entries)
				}
				for name := range entries {
					if !strings.HasPrefix(name, r.ID+".") {
						t.Fatal(name)
					}
					chunk, err := os.Open(filepath.Clean(filepath.Join(runs, name)))
					if err != nil {
						t.Fatal(err)
					}
					z, err := gzip.NewReader(chunk)
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(z)
					if err != nil || !strings.Contains(string(data), `"source_id":"one"`) {
						t.Fatal(string(data), err)
					}
					if err = z.Close(); err != nil {
						t.Fatal(err)
					}
					if err = chunk.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

// R-M334-CD1N
func TestSpoolRefusals(t *testing.T) {
	for _, state := range []string{"sealed", "loaded", "discarded", "add failed", "seal failed", "load failed"} {
		t.Run(state, func(t *testing.T) {
			s, _, runs := configured(t, 1, 2)
			r := add(t, s, "a")
			fail := func(context.Context, int64, time.Duration) error { return errors.New("write refused") }
			var step store.Step
			if state == "add failed" {
				step = fail
			}
			b := build(t, s, r, step)
			switch state {
			case "discarded":
				if e := b.Discard(); e != nil {
					t.Fatal(e)
				}
			case "add failed":
				if e := b.Add(ctx, []source.Row{{SourceID: "one"}}); e == nil {
					t.Fatal("expected add failure")
				}
			case "seal failed":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				if e := b.Seal(canceled); e == nil {
					t.Fatal("expected seal failure")
				}
			default:
				if e := b.Seal(ctx); e != nil {
					t.Fatal(e)
				}
				if state == "loaded" || state == "load failed" {
					var loadStep store.Step
					if state == "load failed" {
						loadStep = fail
					}
					_, e := b.Load(ctx, loadStep)
					if (e != nil) != (state == "load failed") {
						t.Fatal(e)
					}
				}
			}
			before := snapshot(t, runs)
			record, e := s.GetRun(ctx, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			f, e := b.Spool()
			if f != nil || !errors.Is(e, store.ErrUnreachable) {
				t.Fatal(f, e)
			}
			after, e := s.GetRun(ctx, r.ID)
			if e != nil || after != record || !reflect.DeepEqual(before, snapshot(t, runs)) {
				t.Fatal(after, e)
			}
		})
	}
}

// R-M1V7-YLAY R-M334-CD1N
func TestSpoolFailureDoesNotPoisonBuild(t *testing.T) {
	for _, next := range []string{"spool", "add"} {
		t.Run(next, func(t *testing.T) {
			s, _, runs := configured(t, 1, 2)
			r := add(t, s, "a")
			b := build(t, s, r, nil)
			if e := os.MkdirAll(filepath.Dir(runs), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(runs, []byte("sentinel"), 0600); e != nil {
				t.Fatal(e)
			}
			f, e := b.Spool()
			if f != nil || !errors.Is(e, store.ErrUnreachable) {
				t.Fatal(f, e)
			}
			data, e := os.ReadFile(filepath.Clean(runs))
			if e != nil || string(data) != "sentinel" {
				t.Fatal(string(data), e)
			}
			if e = os.Remove(runs); e != nil {
				t.Fatal(e)
			}
			if next == "spool" {
				f, e = b.Spool()
				if e != nil {
					t.Fatal(e)
				}
				if e = f.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if e = b.Add(ctx, []source.Row{{SourceID: "one"}}); e != nil {
				t.Fatal(e)
			}
			if e = b.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			counts, e := b.Load(ctx, nil)
			if e != nil || counts.Rows != 1 {
				t.Fatal(counts, e)
			}
		})
	}
}
