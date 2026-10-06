package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func TestCanceledCoordinationRunsFirstStoreMethodWithoutEffects(t *testing.T) {
	// R-NM0G-9KYA
	for _, busy := range []bool{false, true} {
		for _, first := range []string{"create", "find"} {
			t.Run(map[bool]string{false: "free", true: "busy"}[busy]+"/"+first, func(t *testing.T) {
				f := setup(t)
				s := f.open(t)
				before := snapshot(t, f.cfg.Root)
				ownerDone := make(chan error, 1)
				if busy {
					ownerContext := testContext(t)
					entered := make(chan struct{})
					release := make(chan struct{})
					defer func() {
						close(release)
						select {
						case err := <-ownerDone:
							must(t, err)
						case <-ownerContext.Done():
							t.Fatal("coordination owner did not finish")
						}
					}()
					go func() {
						ownerDone <- s.Coordinate(ownerContext, func(ctx context.Context) error {
							close(entered)
							select {
							case <-release:
								return nil
							case <-ctx.Done():
								return ctx.Err()
							}
						})
					}()
					select {
					case <-entered:
					case <-ownerContext.Done():
						t.Fatal("coordination owner did not enter")
					}
				}
				ctx, cancel := context.WithCancel(testContext(t))
				cancel()
				called := false
				err := s.Coordinate(ctx, func(ctx context.Context) error {
					called = true
					var r store.Repo
					var err error
					if first == "create" {
						r, err = s.Create(ctx, "owner", "new")
					} else {
						r, err = s.Find(ctx, "owner", "missing")
					}
					same(t, r, store.Repo{})
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					return err
				})
				same(t, called, true)
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				same(t, snapshot(t, f.cfg.Root), before)
				same(t, all(t, s), []store.Repo{})
			})
		}
	}
}
