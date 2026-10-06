package store_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func TestExportedMutationSurface(t *testing.T) {
	// R-YKVO-6VRZ R-M5KR-TG2F R-M6SO-77T4
	if store.ErrNameTaken == nil || errors.Is(store.ErrNameTaken, store.ErrNotFound) || errors.Is(store.ErrNameTaken, store.ErrRoot) {
		t.Fatal("non-distinct sentinel")
	}

	surface := struct {
		create func(*store.Store, context.Context, string, string) (store.Repo, error)
		rename func(*store.Store, context.Context, string, string) (store.Repo, error)
		delete func(*store.Store, context.Context, string) error
		head   func(*store.Store, context.Context, string) (string, error)
	}{(*store.Store).Create, (*store.Store).Rename, (*store.Store).Delete, (*store.Store).Head}
	f := setup(t)
	s := f.open(t)
	r, err := surface.create(s, testContext(t), "owner", "notes")
	must(t, err)
	same(t, store.DefaultBranch, "main")
	h, err := surface.head(s, testContext(t), r.ID)
	must(t, err)
	same(t, h, "")
	r, err = surface.rename(s, testContext(t), r.ID, "new")
	must(t, err)
	same(t, r.Name, "new")
	must(t, surface.delete(s, testContext(t), r.ID))
}

func TestOpenCreatesParentsAndRetainsModes(t *testing.T) {
	// R-YM3K-KNIO
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "existing"}[existing], func(t *testing.T) {
			f := setup(t)
			f.cfg.Root = filepath.Join(f.base, "repositories", "nested", "root")
			want := os.FileMode(0700)
			if existing {
				must(t, os.MkdirAll(f.cfg.Root, 0750))
				want = 0750
			}
			s := f.open(t)
			same(t, all(t, s), []store.Repo{})
			for _, path := range []string{filepath.Join(f.base, "repositories"), filepath.Join(f.base, "repositories", "nested")} {
				info, err := os.Stat(path)
				must(t, err)
				same(t, info.Mode().Perm(), want)
			}
			info, err := os.Stat(f.cfg.Root)
			must(t, err)
			same(t, info.Mode().Perm(), want)

		})
	}
}

func TestReopenPreservesCatalogBeforeVerification(t *testing.T) {
	// R-UPDA-W7MZ R-URT3-NR4D
	f := setup(t)
	s, err := store.Open(testContext(t), f.handle(t), f.cfg)
	must(t, err)
	r := create(t, s, "owner", "notes")
	same(t, r.Created, fixedNow.UTC().Truncate(1e9))
	same(t, r.Available, true)
	for _, ref := range []string{r.Name, r.ID} {
		found, err := s.Find(testContext(t), r.Owner, ref)
		must(t, err)
		same(t, found, r)
	}
	f.close(t)
	must(t, os.RemoveAll(s.Dir(r.ID)))
	s = f.open(t)
	same(t, all(t, s), []store.Repo{r})
	for _, ref := range []string{r.Name, r.ID} {
		found, err := s.Find(testContext(t), r.Owner, ref)
		must(t, err)
		same(t, found, r)
	}
}

func stringRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	return string(body)
}

func TestOpenRootFailureDiscardsNewCatalog(t *testing.T) {
	// R-YOJD-C702 R-YPR9-PYQR R-UO5E-IFWA
	for _, kind := range []string{"file", "unlistable", "parent-file"} {
		for _, zero := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "-absent", true: "-zero"}[zero], func(t *testing.T) {
				f := setup(t)
				if zero {
					must(t, os.MkdirAll(filepath.Dir(f.source), 0700))
					write(t, f.source, "")
				}
				var expected error
				switch kind {
				case "file":
					write(t, f.cfg.Root, "root")
					expected = os.MkdirAll(f.cfg.Root, 0700)
				case "parent-file":
					write(t, f.cfg.Root, "parent")
					f.cfg.Root = filepath.Join(f.cfg.Root, "child")
					expected = os.MkdirAll(f.cfg.Root, 0700)
				case "unlistable":
					must(t, os.Mkdir(f.cfg.Root, 0000))
					parentInfo, err := os.Stat(f.base)
					must(t, err)
					t.Cleanup(func() { must(t, os.Chmod(f.cfg.Root, parentInfo.Mode().Perm())) })
					_, expected = os.ReadDir(f.cfg.Root)
				}
				s, err := store.Open(testContext(t), f.handle(t), f.cfg)
				if s != nil || !errors.Is(err, store.ErrRoot) {
					t.Fatalf("store=%v error=%v", s, err)
				}
				same(t, err.Error(), expected.Error())

			})
		}
	}
	f := setup(t)
	f.identity(t, id(1), "notes", "owner", "2024-01-01T00:00:00Z")
	before := snapshot(t, f.cfg.Root)
	ctx, cancel := context.WithCancel(testContext(t))
	cancel()
	s, err := store.Open(ctx, f.handle(t), f.cfg)
	if s != nil || !errors.Is(err, ctx.Err()) {
		t.Fatal(s, err)
	}
	same(t, len(all(t, f.open(t))), 1)
	same(t, snapshot(t, f.cfg.Root), before)
}

func TestRebuildIdentificationAndDuplicateSelection(t *testing.T) {
	// R-V3X1-EOSR R-UJ9S-ZCXI R-UKHP-D4O7 R-UMXI-4O5L R-ULPL-QWEW
	for _, source := range []string{"missing", "zero"} {
		t.Run(source, func(t *testing.T) {
			f := setup(t)
			f.identity(t, id(1), "duplicate", "owner", "2024-02-01T00:00:00Z")
			f.identity(t, id(3), "duplicate", "owner", "2024-01-01T00:00:00Z")
			f.identity(t, id(2), "duplicate", "owner", "2024-01-01T00:00:00Z")
			broken := f.identity(t, id(4), "broken", "other", "2024-01-01T00:00:00Z")
			must(t, os.Remove(filepath.Join(broken, "HEAD")))
			// Each identity condition is independently absent or invalid.
			for i, change := range [][2]string{{"id", id(99)}, {"name", "Bad"}, {"owner", ""}, {"created", "2024-01-01T00:00:00+00:00"}, {"created", "invalid"}} {
				dir := f.identity(t, id(10+i), "invalid", "owner", "2024-01-01T00:00:00Z")
				f.git(t, f.base, "config", "--file", filepath.Join(dir, "config"), "ikigenba."+change[0], change[1])
			}
			dir := f.identity(t, id(20), "last", "owner", "2024-01-01T00:00:00Z")
			f.git(t, f.base, "config", "--file", filepath.Join(dir, "config"), "--add", "ikigenba.name", "last-value")
			f.identity(t, "bad-id", "invalid", "owner", "2024-01-01T00:00:00Z")
			write(t, filepath.Join(f.cfg.Root, id(30)+".git"), "not directory")
			must(t, os.Symlink(dir, filepath.Join(f.cfg.Root, id(31)+".git")))
			must(t, os.Mkdir(filepath.Join(f.cfg.Root, id(32)+".git"), 0700))
			if source == "zero" {
				must(t, os.MkdirAll(filepath.Dir(f.source), 0700))
				write(t, f.source, "")
			}
			before := snapshot(t, f.cfg.Root)
			s := f.open(t)
			repos := all(t, s)
			same(t, len(repos), 3)
			same(t, []string{repos[0].ID, repos[1].ID, repos[2].ID}, []string{id(2), id(4), id(20)})
			same(t, repos[0].Name, "duplicate")
			same(t, repos[0].Owner, "owner")
			same(t, repos[0].Created.Format("2006-01-02T15:04:05Z"), "2024-01-01T00:00:00Z")
			same(t, repos[2].Name, "last-value")
			for _, r := range repos {
				same(t, r.Available, true)
			}
			same(t, snapshot(t, f.cfg.Root), before)
			w, c := writer(t)
			must(t, s.Verify(testContext(t), w))
			es := events(t, w, c)
			same(t, len(es), 3)
			for _, e := range es {
				same(t, e.Name, "repo.unavailable")
				same(t, e.RequestID, "")
				same(t, e.User, "")
			}
			same(t, []any{es[0].Attrs["repo"], es[1].Attrs["repo"], es[2].Attrs["repo"]}, []any{id(1), id(3), id(4)})
			must(t, s.Verify(testContext(t), w))
			same(t, len(events(t, w, c)), 4)
			same(t, len(all(t, s)), 3)
			same(t, snapshot(t, f.cfg.Root), before)
		})
	}
}

func TestExistingCatalogDoesNotAdoptForeignDirectories(t *testing.T) {
	// R-UMXI-4O5L
	f := setup(t)
	s, err := store.Open(testContext(t), f.handle(t), f.cfg)
	must(t, err)
	r := create(t, s, "owner", "notes")
	f.close(t)
	f.identity(t, id(99), "foreign", "owner", "2024-01-01T00:00:00Z")
	before := snapshot(t, f.cfg.Root)
	s = f.open(t)
	same(t, all(t, s), []store.Repo{r})
	w, _ := writer(t)
	must(t, s.Verify(testContext(t), w))
	same(t, all(t, s), []store.Repo{r})
	same(t, snapshot(t, f.cfg.Root), before)
	// Losing the database causes a rebuild rather than generating fresh ids.
	f.close(t)
	must(t, os.Remove(f.source))
	f.cfg.Rand = bytes.NewReader(nil)
	s = f.open(t)
	same(t, len(all(t, s)), 2)
}
