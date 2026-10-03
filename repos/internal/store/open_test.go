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
	// R-M34Z-1WL1 R-M5KR-TG2F R-M6SO-77T4
	if store.ErrNameTaken == nil || errors.Is(store.ErrNameTaken, store.ErrNotFound) || errors.Is(store.ErrNameTaken, store.ErrDatabase) || errors.Is(store.ErrNameTaken, store.ErrRoot) {
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

func TestOpenMemory(t *testing.T) {
	// R-M80K-KZJT
	for _, source := range []string{"", ":memory:"} {
		t.Run(source, func(t *testing.T) {
			f := setup(t)
			f.cfg.Source = source
			s := f.open(t)
			r := create(t, s, "owner", "notes")
			same(t, all(t, s), []store.Repo{r})
			renamed, err := s.Rename(testContext(t), r.ID, "new")
			must(t, err)
			same(t, all(t, s), []store.Repo{renamed})
			must(t, s.Delete(testContext(t), r.ID))
			same(t, all(t, s), []store.Repo{})
			entries, err := os.ReadDir(f.base)
			must(t, err)
			same(t, len(entries), 1)
			same(t, entries[0].Name(), "root")
		})
	}
	// A relative path uses the working directory while still staying in a test-owned tree.
	f := setup(t)
	cwd, err := os.Getwd()
	must(t, err)
	f.cfg.Source, err = filepath.Rel(cwd, f.cfg.Source)
	must(t, err)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	same(t, all(t, s), []store.Repo{r})
	info, err := os.Stat(f.cfg.Source)
	must(t, err)
	if info.Size() == 0 {
		t.Fatal("empty database")
	}
}

func TestOpenCreatesParentsAndRetainsModes(t *testing.T) {
	// R-M98G-YRAI
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "existing"}[existing], func(t *testing.T) {
			f := setup(t)
			f.cfg.Source = filepath.Join(f.base, "catalog", "nested", "repos.db")
			f.cfg.Root = filepath.Join(f.base, "repositories", "nested", "root")
			if existing {
				must(t, os.MkdirAll(filepath.Dir(f.cfg.Source), 0750))
			}
			s := f.open(t)
			same(t, all(t, s), []store.Repo{})
			want := os.FileMode(0700)
			if existing {
				want = 0750
			}
			info, err := os.Stat(filepath.Dir(f.cfg.Source))
			must(t, err)
			same(t, info.Mode().Perm(), want)
			info, err = os.Stat(filepath.Join(f.base, "catalog"))
			must(t, err)
			same(t, info.Mode().Perm(), want)
			for _, path := range []string{filepath.Join(f.base, "repositories"), filepath.Join(f.base, "repositories", "nested")} {
				info, err := os.Stat(path)
				must(t, err)
				same(t, info.Mode().Perm(), os.FileMode(0700))
			}
			info, err = os.Stat(f.cfg.Root)
			must(t, err)
			same(t, info.Mode().Perm(), os.FileMode(0700))
			info, err = os.Stat(f.cfg.Source)
			must(t, err)
			if !info.Mode().IsRegular() || info.Size() == 0 {
				t.Fatal("not a populated database")
			}
		})
	}
}

func TestReopenPreservesCatalogBeforeVerification(t *testing.T) {
	// R-MAGD-CJ17 R-MV6N-UMN0
	f := setup(t)
	s, err := store.Open(testContext(t), f.cfg)
	must(t, err)
	r := create(t, s, "owner", "notes")
	same(t, r.Created, fixedNow.UTC().Truncate(1e9))
	same(t, r.Available, true)
	for _, ref := range []string{r.Name, r.ID} {
		found, err := s.Find(testContext(t), r.Owner, ref)
		must(t, err)
		same(t, found, r)
	}
	must(t, s.Close())
	must(t, os.RemoveAll(s.Dir(r.ID)))
	s = f.open(t)
	same(t, all(t, s), []store.Repo{r})
	for _, ref := range []string{r.Name, r.ID} {
		found, err := s.Find(testContext(t), r.Owner, ref)
		must(t, err)
		same(t, found, r)
	}
}

func TestOpenDatabaseFailuresPrecedeRoot(t *testing.T) {
	// R-MBO9-QARW R-ME42-HU9A
	for _, kind := range []string{"parent-file", "not-sqlite", "file-readonly", "directory-readonly"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			must(t, os.MkdirAll(f.cfg.Root, 0700))
			write(t, filepath.Join(f.cfg.Root, "untouched"), "bytes")
			before := snapshot(t, f.cfg.Root)
			var expected error
			switch kind {
			case "parent-file":
				write(t, filepath.Dir(f.cfg.Source), "parent")
				expected = os.MkdirAll(filepath.Dir(f.cfg.Source), 0700)
			case "not-sqlite":
				must(t, os.MkdirAll(filepath.Dir(f.cfg.Source), 0700))
				write(t, f.cfg.Source, "not a database")
			case "file-readonly", "directory-readonly":
				s, err := store.Open(testContext(t), f.cfg)
				must(t, err)
				create(t, s, "owner", "notes")
				must(t, s.Close())
				before = snapshot(t, f.cfg.Root)
				path := f.cfg.Source
				mode := os.FileMode(0600)
				if kind == "directory-readonly" {
					path = filepath.Dir(path)
					mode = 0700
				}
				must(t, os.Chmod(path, 0400))
				t.Cleanup(func() { must(t, os.Chmod(path, mode)) })
			}
			s, err := store.Open(testContext(t), f.cfg)
			if s != nil || !errors.Is(err, store.ErrDatabase) || errors.Is(err, store.ErrRoot) {
				t.Fatalf("store=%v error=%v", s, err)
			}
			if expected != nil {
				same(t, err.Error(), expected.Error())
				same(t, stringRead(t, filepath.Dir(f.cfg.Source)), "parent")
			}
			same(t, snapshot(t, f.cfg.Root), before)
		})
	}
	// Database failure must not create an absent root either.
	f := setup(t)
	write(t, filepath.Dir(f.cfg.Source), "parent")
	_, err := store.Open(testContext(t), f.cfg)
	if !errors.Is(err, store.ErrDatabase) {
		t.Fatal(err)
	}
	absent(t, f.cfg.Root)
}

func stringRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	return string(body)
}

func TestOpenRootFailureDiscardsNewCatalog(t *testing.T) {
	// R-DFDM-MAXW R-VA0J-BJI8 R-ME42-HU9A
	for _, kind := range []string{"file", "unlistable", "parent-file"} {
		for _, zero := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "-absent", true: "-zero"}[zero], func(t *testing.T) {
				f := setup(t)
				if zero {
					must(t, os.MkdirAll(filepath.Dir(f.cfg.Source), 0700))
					write(t, f.cfg.Source, "")
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
				s, err := store.Open(testContext(t), f.cfg)
				if s != nil || !errors.Is(err, store.ErrRoot) || errors.Is(err, store.ErrDatabase) {
					t.Fatalf("store=%v error=%v", s, err)
				}
				same(t, err.Error(), expected.Error())
				info, err := os.Stat(f.cfg.Source)
				if err == nil {
					same(t, info.Size(), int64(0))
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
			})
		}
	}
	f := setup(t)
	f.identity(t, id(1), "notes", "owner", "2024-01-01T00:00:00Z")
	before := snapshot(t, f.cfg.Root)
	ctx, cancel := context.WithCancel(testContext(t))
	cancel()
	s, err := store.Open(ctx, f.cfg)
	if s != nil || !errors.Is(err, ctx.Err()) {
		t.Fatal(s, err)
	}
	absent(t, f.cfg.Source)
	same(t, snapshot(t, f.cfg.Root), before)
}

func TestRebuildIdentificationAndDuplicateSelection(t *testing.T) {
	// R-V3X1-EOSR R-MGJV-9DQO R-MHRR-N5HD R-NPO5-EW6D R-NQW1-SNX2
	for _, source := range []string{"missing", "zero", "memory", "empty"} {
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
			switch source {
			case "zero":
				must(t, os.MkdirAll(filepath.Dir(f.cfg.Source), 0700))
				write(t, f.cfg.Source, "")
			case "memory":
				f.cfg.Source = ":memory:"
			case "empty":
				f.cfg.Source = ""
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
	// R-NPO5-EW6D
	f := setup(t)
	s, err := store.Open(testContext(t), f.cfg)
	must(t, err)
	r := create(t, s, "owner", "notes")
	must(t, s.Close())
	f.identity(t, id(99), "foreign", "owner", "2024-01-01T00:00:00Z")
	before := snapshot(t, f.cfg.Root)
	s = f.open(t)
	same(t, all(t, s), []store.Repo{r})
	w, _ := writer(t)
	must(t, s.Verify(testContext(t), w))
	same(t, all(t, s), []store.Repo{r})
	same(t, snapshot(t, f.cfg.Root), before)
	// Losing the database causes a rebuild rather than generating fresh ids.
	must(t, s.Close())
	must(t, os.Remove(f.cfg.Source))
	f.cfg.Rand = bytes.NewReader(nil)
	s = f.open(t)
	same(t, len(all(t, s)), 2)
}

func TestOpenLiteralFilesystemSourceAndEmptyUnwritableCatalog(t *testing.T) {
	// R-M80K-KZJT R-MBO9-QARW
	f := setup(t)
	f.cfg.Source = filepath.Join(f.base, "literal?mode=memory#name.db")
	s, err := store.Open(testContext(t), f.cfg)
	must(t, err)
	r := create(t, s, "owner", "notes")
	must(t, s.Close())
	info, err := os.Stat(f.cfg.Source)
	must(t, err)
	if info.Size() == 0 {
		t.Fatal("literal database was empty")
	}
	s = f.open(t)
	same(t, all(t, s), []store.Repo{r})
	f = setup(t)
	s, err = store.Open(testContext(t), f.cfg)
	must(t, err)
	must(t, s.Close())
	parent := filepath.Dir(f.cfg.Source)
	info, err = os.Stat(parent)
	must(t, err)
	must(t, os.Chmod(parent, info.Mode().Perm()&^0222))
	t.Cleanup(func() { must(t, os.Chmod(parent, info.Mode().Perm())) })
	s, err = store.Open(testContext(t), f.cfg)
	if s != nil || !errors.Is(err, store.ErrDatabase) || errors.Is(err, store.ErrRoot) {
		t.Fatalf("empty catalog in unwritable parent: %v %v", s, err)
	}
}
