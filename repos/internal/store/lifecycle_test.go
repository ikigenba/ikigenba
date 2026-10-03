package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func calls(ctx context.Context, s *store.Store, w *telemetry.Writer, r store.Repo) []func() error {
	return []func() error{
		func() error { return s.Verify(ctx, w) },
		func() error { _, err := s.Find(ctx, r.Owner, r.ID); return err },
		func() error { _, err := s.List(ctx, r.Owner); return err },
		func() error { _, err := s.All(ctx); return err },
		func() error { _, err := s.Size(ctx, r.ID); return err },
		func() error { _, err := s.Create(ctx, r.Owner, "new"); return err },
		func() error { _, err := s.Rename(ctx, r.ID, "new"); return err },
		func() error { return s.Delete(ctx, r.ID) },
		func() error { _, err := s.Head(ctx, r.ID); return err },
	}
}

func TestClosedAndCanceledMethodsRefuseWithoutEffects(t *testing.T) {
	// R-NKSJ-VT7L R-NM0G-9KYA
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{true: "closed", false: "canceled"}[closed], func(t *testing.T) {
			f := setup(t)
			s := f.open(t)
			r := create(t, s, "owner", "notes")
			before := snapshot(t, f.cfg.Root)
			catalog := all(t, s)
			w, c := writer(t)
			ctx := testContext(t)
			if closed {
				must(t, s.Close())
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			for i, call := range calls(ctx, s, w, r) {
				err := call()
				if err == nil {
					t.Fatalf("call %d succeeded", i)
				}
				if closed {
					if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
						t.Fatal(err)
					}
				} else if !errors.Is(err, ctx.Err()) {
					t.Fatalf("call %d: %v", i, err)
				}
				same(t, snapshot(t, f.cfg.Root), before)
			}
			same(t, len(events(t, w, c)), 0)
			if !closed {
				same(t, all(t, s), catalog)
			}
		})
	}
}

func TestConcurrentAllMethods(t *testing.T) {
	// R-NN8C-NCOZ
	f := setup(t)
	s := f.open(t)
	anchor := create(t, s, "anchor", "stable")
	w, c := writer(t)
	start := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			<-start
			owner := fmt.Sprintf("owner%d", i)
			r, err := s.Create(testContext(t), owner, "original")
			if err != nil {
				errs <- err
				return
			}
			if !store.ValidID(r.ID) || !r.Available || r.Owner != owner {
				errs <- fmt.Errorf("bad create: %+v", r)
				return
			}
			renamed, err := s.Rename(testContext(t), r.ID, "renamed")
			if err != nil {
				errs <- err
				return
			}
			found, err := s.Find(testContext(t), owner, renamed.Name)
			if err != nil {
				errs <- err
				return
			}
			if found != renamed {
				errs <- fmt.Errorf("find differs: %+v", found)
				return
			}
			list, err := s.List(testContext(t), owner)
			if err != nil {
				errs <- err
				return
			}
			if len(list) != 1 || list[0] != renamed {
				errs <- fmt.Errorf("list differs: %+v", list)
				return
			}
			repos, err := s.All(testContext(t))
			if err != nil {
				errs <- err
				return
			}
			for j := 1; j < len(repos); j++ {
				if repos[j-1].ID >= repos[j].ID {
					errs <- errors.New("all unordered")
					return
				}
			}
			head, err := s.Head(testContext(t), r.ID)
			if err != nil {
				errs <- err
				return
			}
			if head != "" {
				errs <- errors.New("unexpected head")
				return
			}
			size, err := s.Size(testContext(t), r.ID)
			if err != nil {
				errs <- err
				return
			}
			if size <= 0 {
				errs <- errors.New("empty size")
				return
			}
			if err = s.Verify(testContext(t), w); err != nil {
				errs <- err
				return
			}
			// Dir is immutable and remains safe during the other methods' work.
			if _, err = os.Stat(s.Dir(r.ID)); err != nil {
				errs <- err
				return
			}
			if err = s.Delete(testContext(t), r.ID); err != nil {
				errs <- err
				return
			}
			_, err = s.Find(testContext(t), owner, r.ID)
			if !errors.Is(err, store.ErrNotFound) {
				errs <- fmt.Errorf("deleted find: %w", err)
			}
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	same(t, all(t, s), []store.Repo{anchor})
	same(t, len(events(t, w, c)), 0)
	entries, err := os.ReadDir(f.cfg.Root)
	must(t, err)
	same(t, len(entries), 1)
	same(t, entries[0].Name(), anchor.ID+".git")
}
