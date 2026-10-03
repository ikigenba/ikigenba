package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func TestCoordinatedRollbackPreservesLastSuccessfulVerify(t *testing.T) {
	// R-MQB2-BJO8
	for _, memory := range []bool{false, true} {
		for _, available := range []bool{false, true} {
			faults := []string{"writable"}
			if !memory {
				faults = append(faults, "source-parent", "source-file")
			}
			for _, fault := range faults {
				name := map[bool]string{false: "file", true: "memory"}[memory] + "/" + map[bool]string{false: "damage", true: "repair"}[available] + "/" + fault
				t.Run(name, func(t *testing.T) {
					f := setup(t)
					if memory {
						f.cfg.Source = ":memory:"
					}
					s := f.open(t)
					r := create(t, s, "owner", "notes")
					w, _ := writer(t)
					if available {
						damaged(t, s, r)
						must(t, s.Verify(testContext(t), w))
						r.Available = false
					}
					assertRepo := func(want store.Repo) {
						got, err := s.Find(testContext(t), want.Owner, want.ID)
						must(t, err)
						same(t, got, want)
						got, err = s.Find(testContext(t), want.Owner, want.Name)
						must(t, err)
						same(t, got, want)
						list, err := s.List(testContext(t), want.Owner)
						must(t, err)
						same(t, list, []store.Repo{want})
						same(t, all(t, s), []store.Repo{want})
					}
					assertRepo(r)
					originalConfig, err := os.ReadFile(filepath.Join(s.Dir(r.ID), "config"))
					must(t, err)
					var permissionTarget string
					var originalMode os.FileMode
					if fault != "writable" {
						permissionTarget = f.cfg.Source
						if fault == "source-parent" {
							permissionTarget = filepath.Dir(permissionTarget)
						}
						info, err := os.Stat(permissionTarget)
						must(t, err)
						originalMode = info.Mode().Perm()
						t.Cleanup(func() { must(t, os.Chmod(permissionTarget, originalMode)) })
					}
					failure := errors.New("late answer failure")
					err = s.Coordinate(testContext(t), func(ctx context.Context) error {
						renamed, err := s.Rename(ctx, r.ID, "journal")
						if err != nil {
							return err
						}
						assertRepo(renamed)
						if available {
							write(t, filepath.Join(s.Dir(r.ID), "HEAD"), "ref: refs/heads/main\n")
						} else {
							damaged(t, s, r)
						}
						if err = s.Verify(ctx, w); err != nil {
							return err
						}
						renamed.Available = available
						assertRepo(renamed)
						if permissionTarget != "" {
							must(t, os.Chmod(permissionTarget, originalMode&^0222))
						}
						return failure
					})
					if permissionTarget != "" {
						must(t, os.Chmod(permissionTarget, originalMode))
					}
					if !errors.Is(err, failure) {
						t.Fatal(err)
					}
					r.Available = available
					assertRepo(r)
					config, err := os.ReadFile(filepath.Join(s.Dir(r.ID), "config"))
					must(t, err)
					same(t, config, originalConfig)
					if !memory {
						must(t, s.Close())
						s = f.open(t)
						assertRepo(r)
					}
				})
			}
		}
	}
}

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
