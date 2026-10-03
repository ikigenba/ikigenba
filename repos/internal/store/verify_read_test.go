package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func TestVerificationSoundnessDamageRecoveryAndEvents(t *testing.T) {
	// R-V54X-SGJG R-QBMJ-0G2N R-V6CU-68A5 R-NQW1-SNX2 R-MP35-XRXJ
	cases := []struct {
		part   string
		mode   os.FileMode
		remove bool
	}{
		{"", 0, true}, {"HEAD", 0, true}, {"config", 0, true}, {"objects", 0, true}, {"refs", 0, true},
		{"HEAD", 0, false}, {"config", 0, false}, {"objects", 0, false}, {"refs", 0, false},
		{"objects", 0300, false}, {"refs", 0300, false},
	}
	for _, tc := range cases {
		t.Run(tc.part+map[bool]string{true: "-remove", false: "-chmod"}[tc.remove]+tc.mode.String(), func(t *testing.T) {
			f := setup(t)
			s := f.open(t)
			r := create(t, s, "owner", "broken")
			sound := create(t, s, "owner", "sound")
			target := filepath.Join(s.Dir(r.ID), tc.part)
			old := snapshot(t, s.Dir(r.ID))
			backup := filepath.Join(f.base, "backup")
			if tc.remove {
				must(t, os.Rename(target, backup))
			} else {
				must(t, os.Chmod(target, tc.mode))
				t.Cleanup(func() { must(t, os.Chmod(target, old[tc.part].Mode.Perm())) })
			}
			w, c := writer(t)
			ctx := identity.NewContext(testContext(t), identity.Caller{UserID: "caller", RequestID: "request"})
			must(t, s.Verify(ctx, w))
			got, err := s.Find(testContext(t), r.Owner, r.ID)
			must(t, err)
			same(t, got.Available, false)
			list, err := s.List(testContext(t), r.Owner)
			must(t, err)
			same(t, list[0].Available, false)
			same(t, list[1], sound)
			repos := all(t, s)
			same(t, repos[0].Available, false)
			same(t, repos[1], sound)
			es := events(t, w, c)
			same(t, len(es), 1)
			same(t, es[0].Name, "repo.unavailable")
			same(t, es[0].Attrs["repo"], r.ID)
			same(t, es[0].RequestID, "")
			same(t, es[0].User, "")
			if tc.remove {
				absent(t, target)
				must(t, os.Rename(backup, target))
			} else {
				info, err := os.Stat(target)
				must(t, err)
				same(t, info.Mode().Perm(), tc.mode)
				must(t, os.Chmod(target, old[tc.part].Mode.Perm()))
			}
			same(t, snapshot(t, s.Dir(r.ID)), old)
			before := snapshot(t, f.cfg.Root)
			must(t, s.Verify(ctx, w))
			same(t, snapshot(t, f.cfg.Root), before)
			got, err = s.Find(testContext(t), r.Owner, r.ID)
			must(t, err)
			same(t, got, r)
			same(t, len(events(t, w, c)), 1)
		})
	}
	// A regular file in place of a catalogued repository is damage, not a Verify failure.
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	must(t, os.RemoveAll(s.Dir(r.ID)))
	write(t, s.Dir(r.ID), "damaged")
	w, c := writer(t)
	before := snapshot(t, f.cfg.Root)
	must(t, s.Verify(testContext(t), w))
	same(t, snapshot(t, f.cfg.Root), before)
	same(t, len(events(t, w, c)), 1)
	got, err := s.Find(testContext(t), r.Owner, r.ID)
	must(t, err)
	same(t, got.Available, false)
	// An empty catalog/root is equally successful and emits no events.
	f = setup(t)
	s = f.open(t)
	w, c = writer(t)
	must(t, s.Verify(testContext(t), w))
	same(t, len(events(t, w, c)), 0)
}

func TestAvailabilityChangesOnlyWithVerifyAndSurvivesReopen(t *testing.T) {
	// R-MQB2-BJO8
	f := setup(t)
	s, err := store.Open(testContext(t), f.cfg)
	must(t, err)
	r := create(t, s, "owner", "notes")
	damaged(t, s, r)
	got, err := s.Find(testContext(t), r.Owner, r.ID)
	must(t, err)
	same(t, got.Available, true)
	w, _ := writer(t)
	must(t, s.Verify(testContext(t), w))
	r.Available = false
	same(t, all(t, s), []store.Repo{r})
	must(t, s.Close())
	s = f.open(t)
	same(t, all(t, s), []store.Repo{r})
	write(t, filepath.Join(s.Dir(r.ID), "HEAD"), "ref: refs/heads/main\n")
	got, err = s.Find(testContext(t), r.Owner, r.Name)
	must(t, err)
	same(t, got, r)
	list, err := s.List(testContext(t), r.Owner)
	must(t, err)
	same(t, list, []store.Repo{r})
	same(t, all(t, s), []store.Repo{r})
	must(t, s.Verify(testContext(t), w))
	r.Available = true
	same(t, all(t, s), []store.Repo{r})
}

func TestOwnerReadsAndOrdering(t *testing.T) {
	// R-MRIY-PBEX R-MSQV-335M R-MTYR-GUWB
	f := setup(t)
	s := f.open(t)
	z := create(t, s, "owner", "zeta")
	a := create(t, s, "owner", "alpha")
	other := create(t, s, "other", "other")
	list, err := s.List(testContext(t), "owner")
	must(t, err)
	same(t, list, []store.Repo{a, z})
	empty, err := s.List(testContext(t), "missing")
	must(t, err)
	same(t, empty, []store.Repo{})
	same(t, all(t, s), []store.Repo{z, a, other})
	for _, r := range []store.Repo{z, a, other} {
		for _, ref := range []string{r.ID, r.Name} {
			got, err := s.Find(testContext(t), r.Owner, ref)
			must(t, err)
			same(t, got, r)
		}
	}
	for _, ref := range []string{other.ID, other.Name, "", "-invalid", "rep_invalid", "Missing", "none"} {
		got, err := s.Find(testContext(t), "owner", ref)
		same(t, got, store.Repo{})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatal(ref, err)
		}
	}
	f = setup(t)
	s = f.open(t)
	same(t, all(t, s), []store.Repo{})
}

func TestHeadExactLoosePackedAndDamaged(t *testing.T) {
	// R-UTJ6-MT24 R-DE5Q-8J77
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	head, err := s.Head(testContext(t), r.ID)
	must(t, err)
	same(t, head, "")
	sha := f.commit(t, s.Dir(r.ID), "refs/heads/main/x")
	head, err = s.Head(testContext(t), r.ID)
	must(t, err)
	same(t, head, "")
	f.git(t, f.base, "--git-dir="+s.Dir(r.ID), "update-ref", "-d", "refs/heads/main/x")
	f.git(t, f.base, "--git-dir="+s.Dir(r.ID), "update-ref", "refs/heads/main", sha)
	for _, packed := range []bool{false, true} {
		if packed {
			f.git(t, f.base, "--git-dir="+s.Dir(r.ID), "pack-refs", "--all", "--prune")
		}
		head, err = s.Head(testContext(t), r.ID)
		must(t, err)
		same(t, head, sha)
	}
	// Every soundness failure makes Head empty even while persisted Available remains true.
	for _, part := range []string{"HEAD", "config", "objects", "refs"} {
		path := filepath.Join(s.Dir(r.ID), part)
		backup := filepath.Join(f.base, "backup")
		must(t, os.Rename(path, backup))
		head, err = s.Head(testContext(t), r.ID)
		must(t, err)
		same(t, head, "")
		must(t, os.Rename(backup, path))
	}
	damaged(t, s, r)
	w, _ := writer(t)
	must(t, s.Verify(testContext(t), w))
	write(t, filepath.Join(s.Dir(r.ID), "HEAD"), "ref: refs/heads/main\n")
	head, err = s.Head(testContext(t), r.ID)
	must(t, err)
	same(t, head, "")
	must(t, os.RemoveAll(s.Dir(r.ID)))
	head, err = s.Head(testContext(t), r.ID)
	must(t, err)
	same(t, head, "")
	head, err = s.Head(testContext(t), id(99))
	same(t, head, "")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSizeApparentBytesUnreadableAndLinks(t *testing.T) {
	// R-NJKN-I1GW
	f := setup(t)
	s := f.open(t)
	r := create(t, s, "owner", "notes")
	write(t, filepath.Join(f.base, "outside"), "outside ignored")
	must(t, os.Symlink(filepath.Join(f.base, "outside"), filepath.Join(s.Dir(r.ID), "link")))
	must(t, os.Symlink(s.Dir(r.ID), filepath.Join(s.Dir(r.ID), "cycle")))
	sparse, err := os.Create(filepath.Join(s.Dir(r.ID), "sparse"))
	must(t, err)
	must(t, sparse.Truncate(4097))
	must(t, sparse.Close())
	expected := int64(0)
	for _, e := range snapshot(t, s.Dir(r.ID)) {
		if e.Mode.IsRegular() {
			expected += int64(len(e.Bytes))
		}
	}
	size, err := s.Size(testContext(t), r.ID)
	must(t, err)
	same(t, size, expected)
	unreadable := filepath.Join(s.Dir(r.ID), "unreadable")
	write(t, unreadable, "ignored")
	must(t, os.Chmod(unreadable, 0000))
	t.Cleanup(func() {
		if err := os.Chmod(unreadable, 0600); !os.IsNotExist(err) {
			must(t, err)
		}
	})
	hidden := filepath.Join(s.Dir(r.ID), "hidden")
	must(t, os.Mkdir(hidden, 0700))
	write(t, filepath.Join(hidden, "file"), "ignored")
	hiddenInfo, err := os.Stat(hidden)
	must(t, err)
	must(t, os.Chmod(hidden, 0000))
	t.Cleanup(func() {
		if err := os.Chmod(hidden, hiddenInfo.Mode().Perm()); !os.IsNotExist(err) {
			must(t, err)
		}
	})
	size, err = s.Size(testContext(t), r.ID)
	must(t, err)
	same(t, size, expected)
	must(t, os.Chmod(hidden, hiddenInfo.Mode().Perm()))
	must(t, os.Chmod(unreadable, 0600))
	damaged(t, s, r)
	w, _ := writer(t)
	must(t, s.Verify(testContext(t), w))
	expected = 0
	for _, e := range snapshot(t, s.Dir(r.ID)) {
		if e.Mode.IsRegular() {
			expected += int64(len(e.Bytes))
		}
	}
	size, err = s.Size(testContext(t), r.ID)
	must(t, err)
	same(t, size, expected)
	// A regular file at the path contributes its own apparent size, and absence contributes zero.
	must(t, os.RemoveAll(s.Dir(r.ID)))
	write(t, s.Dir(r.ID), "file")
	size, err = s.Size(testContext(t), r.ID)
	must(t, err)
	same(t, size, int64(4))
	must(t, os.Remove(s.Dir(r.ID)))
	size, err = s.Size(testContext(t), r.ID)
	must(t, err)
	same(t, size, int64(0))
	size, err = s.Size(testContext(t), id(99))
	same(t, size, int64(0))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}
