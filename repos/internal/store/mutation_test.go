package store_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func TestCreationIdsCollisionFailureAndConfiguration(t *testing.T) {
	// R-MWEK-8EDP R-MXMG-M64E R-MYUC-ZXV3
	f := setup(t)
	template := filepath.Join(f.base, "template")
	must(t, os.MkdirAll(filepath.Join(template, "hooks"), 0700))
	write(t, filepath.Join(template, "hooks", "post-receive"), "must not be copied")
	f.env = append(f.env, "GIT_TEMPLATE_DIR="+template)
	// Config.Git samples the isolated environment supplied by setup; add the template explicitly.
	g, err := newGit(f)
	must(t, err)
	f.cfg.Git = g
	random := []byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 0, 0, 9, 0, 0, 0, 0, 0, 0, 0, 10}
	f.cfg.Rand = bytes.NewReader(random)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	same(t, r.ID, "rep_0102030405060708")
	// Both a catalog collision and an uncatalogued path are passed over.
	write(t, s.Dir(id(9)), "foreign")
	second := create(t, s, "owner", "other")
	same(t, second.ID, id(10))
	same(t, stringRead(t, s.Dir(id(9))), "foreign")
	before := snapshot(t, f.cfg.Root)
	catalog := all(t, s)
	got, err := s.Create(testContext(t), "owner", "exhausted")
	same(t, got, store.Repo{})
	if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
		t.Fatal(err)
	}
	same(t, snapshot(t, f.cfg.Root), before)
	same(t, all(t, s), catalog)
	dir := s.Dir(r.ID)
	same(t, f.git(t, f.base, "--git-dir="+dir, "symbolic-ref", "HEAD"), "refs/heads/"+store.DefaultBranch+"\n")
	same(t, f.git(t, f.base, "--git-dir="+dir, "for-each-ref"), "")
	same(t, f.git(t, f.base, "--git-dir="+dir, "rev-parse", "--show-object-format"), "sha1\n")
	same(t, f.git(t, f.base, "--git-dir="+dir, "rev-parse", "--is-bare-repository"), "true\n")
	absent(t, filepath.Join(dir, "hooks"))
	values := [][2]string{{"pack.windowMemory", "64m"}, {"pack.threads", "1"}, {"core.bigFileThreshold", "16m"}, {"core.bare", "true"}, {"ikigenba.id", r.ID}, {"ikigenba.name", r.Name}, {"ikigenba.owner", r.Owner}, {"ikigenba.created", r.Created.Format("2006-01-02T15:04:05Z")}}
	for _, p := range values {
		same(t, f.git(t, f.base, "config", "--file", filepath.Join(dir, "config"), "--get", p[0]), p[1]+"\n")
	}
	var expected strings.Builder
	for _, p := range values[4:] {
		expected.WriteString(p[0] + " " + p[1] + "\n")
	}
	same(t, f.git(t, f.base, "config", "--file", filepath.Join(dir, "config"), "--get-regexp", `^ikigenba\.`), expected.String())
}

func TestCreateRefusalsPreserveStateAndNamesArePerOwner(t *testing.T) {
	// R-N029-DPLS R-N1A5-RHCH
	f := setup(t)
	s := f.open(t)
	create(t, s, "owner", "notes")
	before := snapshot(t, f.cfg.Root)
	catalog := all(t, s)
	for _, tc := range []struct {
		owner, name string
		taken       bool
	}{{"owner", "notes", true}, {"", "valid", false}, {"owner", "", false}, {"owner", "Bad", false}, {"owner", "-bad", false}, {"owner", strings.Repeat("x", 65), false}} {
		r, err := s.Create(testContext(t), tc.owner, tc.name)
		same(t, r, store.Repo{})
		if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) != tc.taken {
			t.Fatal(tc, err)
		}
		same(t, snapshot(t, f.cfg.Root), before)
		same(t, all(t, s), catalog)
	}
	other := create(t, s, "other", "notes")
	same(t, other.Owner, "other")
	same(t, len(all(t, s)), 2)
}

func TestCreatePermissionFailureRecoveryAndConcurrentSameName(t *testing.T) {
	// R-0HQZ-2R03 R-WWSQ-NF65
	for _, where := range []string{"root", "database"} {
		t.Run(where, func(t *testing.T) {
			f := setup(t)
			s := f.open(t)
			create(t, s, "owner", "existing")
			before := snapshot(t, f.cfg.Root)
			catalog := all(t, s)
			path := f.cfg.Root
			if where == "database" {
				(*f.d).SetFailing(true)
			}
			info, err := os.Stat(path)
			must(t, err)
			must(t, os.Chmod(path, info.Mode().Perm()&^0222))
			t.Cleanup(func() { must(t, os.Chmod(path, info.Mode().Perm())) })
			r, err := s.Create(testContext(t), "owner", "new")
			same(t, r, store.Repo{})
			if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
				t.Fatal(err)
			}
			must(t, os.Chmod(path, info.Mode().Perm()))
			(*f.d).SetFailing(false)
			same(t, snapshot(t, f.cfg.Root), before)
			same(t, all(t, s), catalog)
			r = create(t, s, "owner", "new")
			entries, err := os.ReadDir(f.cfg.Root)
			must(t, err)
			same(t, len(entries), 2)
			same(t, r.Name, "new")
		})
	}
	f := setup(t)
	s := f.open(t)
	start := make(chan struct{})
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() { <-start; _, err := s.Create(testContext(t), "owner", "same"); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, store.ErrNameTaken) {
			t.Fatal(err)
		}
	}
	same(t, successes, 1)
	repos := all(t, s)
	same(t, len(repos), 1)
	entries, err := os.ReadDir(f.cfg.Root)
	must(t, err)
	same(t, len(entries), 1)
	same(t, entries[0].Name(), repos[0].ID+".git")
}

func TestRenameChangesOnlyNameConfigAndCatalog(t *testing.T) {
	// R-N65R-AKB9 R-N7DN-OC1Y
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	f.commit(t, s.Dir(r.ID), "refs/heads/main")
	create(t, s, "other", "renamed")
	before := snapshot(t, f.cfg.Root)
	configPath := filepath.Join(r.ID+".git", "config")
	config := before[configPath]
	config.Bytes = strings.Replace(config.Bytes, "name = notes", "name = renamed", 1)
	before[configPath] = config
	renamed, err := s.Rename(testContext(t), r.ID, "renamed")
	must(t, err)
	r.Name = "renamed"
	same(t, renamed, r)
	same(t, snapshot(t, f.cfg.Root), before)
	got, err := s.Find(testContext(t), r.Owner, r.Name)
	must(t, err)
	same(t, got, r)
	_, err = s.Find(testContext(t), r.Owner, "notes")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	same(t, f.git(t, f.base, "config", "--file", filepath.Join(s.Dir(r.ID), "config"), "--get", "ikigenba.name"), "renamed\n")
}

func TestUnavailableRenameAndNoopWriteNothing(t *testing.T) {
	// R-N8LK-23SN R-N9TG-FVJC
	for _, kind := range []string{"damaged", "missing"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			s := f.open(t)
			r := create(t, s, "owner", "notes")
			if kind == "damaged" {
				damaged(t, s, r)
			} else {
				must(t, os.RemoveAll(s.Dir(r.ID)))
			}
			w, _ := writer(t)
			must(t, s.Verify(testContext(t), w))
			before := snapshot(t, f.cfg.Root)
			renamed, err := s.Rename(testContext(t), r.ID, "new")
			must(t, err)
			r.Name = "new"
			r.Available = false
			same(t, renamed, r)
			same(t, snapshot(t, f.cfg.Root), before)
			got, err := s.Find(testContext(t), r.Owner, r.Name)
			must(t, err)
			same(t, got, r)
		})
	}
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	f.git(t, f.base, "config", "--file", filepath.Join(s.Dir(r.ID), "config"), "ikigenba.name", "disk-name")
	before := snapshot(t, f.cfg.Root)
	catalog := all(t, s)
	renamed, err := s.Rename(testContext(t), r.ID, r.Name)
	must(t, err)
	same(t, renamed, r)
	same(t, all(t, s), catalog)
	same(t, snapshot(t, f.cfg.Root), before)
}

func TestRenameRefusalsAndPermissionFailureRollback(t *testing.T) {
	// R-NB1C-TNA1 R-Z365-XFWE
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	create(t, s, "owner", "taken")
	before := snapshot(t, f.cfg.Root)
	catalog := all(t, s)
	for _, tc := range []struct {
		id, name string
		sentinel error
	}{{id(99), "new", store.ErrNotFound}, {r.ID, "taken", store.ErrNameTaken}, {r.ID, "Bad", nil}} {
		renamed, err := s.Rename(testContext(t), tc.id, tc.name)
		same(t, renamed, store.Repo{})
		if err == nil {
			t.Fatal("accepted refusal")
		}
		if tc.sentinel != nil {
			if !errors.Is(err, tc.sentinel) {
				t.Fatal(err)
			}
		} else if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
			t.Fatal(err)
		}
		same(t, snapshot(t, f.cfg.Root), before)
		same(t, all(t, s), catalog)
	}
	for _, path := range []string{s.Dir(r.ID)} {
		info, err := os.Stat(path)
		must(t, err)
		must(t, os.Chmod(path, info.Mode().Perm()&^0222))
		renamed, err := s.Rename(testContext(t), r.ID, "new")
		must(t, os.Chmod(path, info.Mode().Perm()))
		same(t, renamed, store.Repo{})
		if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
			t.Fatal(err)
		}
		same(t, snapshot(t, f.cfg.Root), before)
		same(t, all(t, s), catalog)
	}
	renamed, err := s.Rename(testContext(t), r.ID, "new")
	must(t, err)
	same(t, renamed.Name, "new")
}

func TestDeleteDirectoryFileMissingAndReopen(t *testing.T) {
	// R-UT10-1IV2
	for _, kind := range []string{"directory", "file", "missing"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			s, err := store.Open(testContext(t), f.handle(t), f.cfg)
			must(t, err)
			r := create(t, s, "owner", "notes")
			other := create(t, s, "other", "other")
			f.commit(t, s.Dir(other.ID), "refs/heads/main")
			if kind != "directory" {
				must(t, os.RemoveAll(s.Dir(r.ID)))
			}
			if kind == "file" {
				write(t, s.Dir(r.ID), "damaged")
			}
			otherBefore := snapshot(t, s.Dir(other.ID))
			must(t, s.Delete(testContext(t), r.ID))
			absent(t, s.Dir(r.ID))
			same(t, all(t, s), []store.Repo{other})
			same(t, snapshot(t, s.Dir(other.ID)), otherBefore)
			entries, err := os.ReadDir(f.cfg.Root)
			must(t, err)
			same(t, len(entries), 1)
			same(t, entries[0].Name(), other.ID+".git")
			for _, ref := range []string{r.ID, r.Name} {
				_, err := s.Find(testContext(t), r.Owner, ref)
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatal(err)
				}
			}
			f.close(t)
			s = f.open(t)
			same(t, all(t, s), []store.Repo{other})
			for _, ref := range []string{r.ID, r.Name} {
				_, err := s.Find(testContext(t), r.Owner, ref)
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDeleteRefusalsAndPermissionFailuresPreserveState(t *testing.T) {
	// R-NEP1-YYI4 R-Z5LY-OZDS
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	f.commit(t, s.Dir(r.ID), "refs/heads/main")
	write(t, s.Dir(id(99)), "foreign")
	before := snapshot(t, f.cfg.Root)
	catalog := all(t, s)
	err := s.Delete(testContext(t), id(99))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	same(t, snapshot(t, f.cfg.Root), before)
	same(t, all(t, s), catalog)
	for _, path := range []string{f.cfg.Root} {
		info, err := os.Stat(path)
		must(t, err)
		must(t, os.Chmod(path, info.Mode().Perm()&^0222))
		err = s.Delete(testContext(t), r.ID)
		must(t, os.Chmod(path, info.Mode().Perm()))
		if err == nil || errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
		same(t, snapshot(t, f.cfg.Root), before)
		same(t, all(t, s), catalog)
	}
	must(t, s.Delete(testContext(t), r.ID))
	absent(t, s.Dir(r.ID))
	same(t, stringRead(t, s.Dir(id(99))), "foreign")
}

func newGit(f fixture) (*git.Git, error) {
	return git.Find(filepath.Dir(f.path), func() []string { return append([]string(nil), f.env...) })
}

type failingRandom struct{ err error }

func (r failingRandom) Read([]byte) (int, error) { return 0, r.err }

func TestRandomFailuresNeverBecomeRepositoryRuleErrors(t *testing.T) {
	// R-MWEK-8EDP
	for i, reader := range []io.Reader{
		failingRandom{store.ErrNotFound}, failingRandom{store.ErrNameTaken},
		failingRandom{fmt.Errorf("random: %w", store.ErrNotFound)},
		failingRandom{fmt.Errorf("random: %w", store.ErrNameTaken)},
		bytes.NewReader([]byte{1, 2, 3}),
	} {
		t.Run(fmt.Sprintf("reader%d", i), func(t *testing.T) {
			f := setup(t)
			f.cfg.Rand = io.MultiReader(bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 1}), reader)
			s := f.open(t)
			create(t, s, "owner", "existing")
			// Existing, uncatalogued bytes must not be changed by a failing id draw.
			write(t, filepath.Join(f.cfg.Root, "untouched"), "same bytes")
			before := snapshot(t, f.cfg.Root)
			catalog := all(t, s)
			r, err := s.Create(testContext(t), "owner", "notes")
			same(t, r, store.Repo{})
			if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
				t.Fatal(err)
			}
			same(t, all(t, s), catalog)
			same(t, snapshot(t, f.cfg.Root), before)
		})
	}
}

func TestRenameRebuiltRepeatedNamesPreservesOtherConfigBytes(t *testing.T) {
	// R-N65R-AKB9 R-N7DN-OC1Y R-Z365-XFWE
	for _, variant := range []string{"different", "same", "quoted", "continued", "comment", "mixed", "whitespace", "bom"} {
		t.Run(variant, func(t *testing.T) {
			f := setup(t)
			dir := f.identity(t, id(1), "earlier", "owner", "2024-01-01T00:00:00Z")
			config := filepath.Join(dir, "config")
			lastValue := "last-value"
			suffix := "\tname = last-value\n"
			replacement := "\tname = renamed\n"
			switch variant {
			case "same":
				lastValue = "earlier"
				suffix = "\tname = earlier\n"
			case "quoted":
				suffix = "[IKIGENBA]\n  NAME = \"last-value\"  # retained comment\n"
				replacement = "[IKIGENBA]\n  NAME = \"renamed\"  # retained comment\n"
			case "comment":
				suffix = "\tname = last-value\n# comment with a backslash \\\n[other]\n\tname = untouched\n"
				replacement = "\tname = renamed\n# comment with a backslash \\\n[other]\n\tname = untouched\n"
			case "mixed":
				suffix = "\tname = \"last\"-value ; retained comment\n"
				replacement = "\tname = renamed ; retained comment\n"
			case "whitespace":
				suffix = "[IKIGENBA]\r\n  \tNAME  =\t last-value \t; retained comment\r\n"
				replacement = "[IKIGENBA]\r\n  \tNAME  =\t renamed \t; retained comment\r\n"
			case "continued":
				suffix = "[other]\n\tname = untouched\n[ikigenba]\n\tname = last\\\n-value\n"
				replacement = "[other]\n\tname = untouched\n[ikigenba]\n\tname = renamed\n"
			}
			original := stringRead(t, config)
			if variant == "bom" {
				original = "\xef\xbb\xbf" + original
			}
			write(t, config, original+suffix)
			// The fixture is admitted through the real identity and soundness contracts.
			same(t, f.git(t, f.base, "config", "--file", config, "--get", "ikigenba.name"), lastValue+"\n")
			s := f.open(t)
			repos := all(t, s)
			same(t, len(repos), 1)
			r := repos[0]
			same(t, r.Name, lastValue)
			same(t, r.Available, true)
			f.commit(t, dir, "refs/heads/main")
			before := snapshot(t, f.cfg.Root)
			// A refused write leaves even repeated assignments unchanged.
			info, err := os.Stat(dir)
			must(t, err)
			must(t, os.Chmod(dir, info.Mode().Perm()&^0222))
			renamed, err := s.Rename(testContext(t), r.ID, "renamed")
			must(t, os.Chmod(dir, info.Mode().Perm()))
			same(t, renamed, store.Repo{})
			if err == nil || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrNameTaken) {
				t.Fatal(err)
			}
			same(t, snapshot(t, f.cfg.Root), before)
			same(t, all(t, s), repos)
			expected := before[filepath.Join(r.ID+".git", "config")]
			expected.Bytes = original + replacement
			before[filepath.Join(r.ID+".git", "config")] = expected
			renamed, err = s.Rename(testContext(t), r.ID, "renamed")
			must(t, err)
			r.Name = "renamed"
			same(t, renamed, r)
			same(t, snapshot(t, f.cfg.Root), before)
			same(t, all(t, s), []store.Repo{r})
			found, err := s.Find(testContext(t), r.Owner, "renamed")
			must(t, err)
			same(t, found, r)
			_, err = s.Find(testContext(t), r.Owner, lastValue)
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatal(err)
			}
			same(t, f.git(t, f.base, "config", "--file", config, "--get", "ikigenba.name"), "renamed\n")
		})
	}
}
