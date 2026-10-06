package store_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func TestNormalizedRunRecordRefusals(t *testing.T) {
	// R-9CAY-4JXA
	zeroFraction := time.Time{}.Add(750 * time.Millisecond)
	tests := []struct {
		name   string
		failed bool
		mutate func(*store.Run)
	}{
		{"id", false, func(r *store.Run) { r.ID = "bad" }},
		{"ref", false, func(r *store.Run) { r.Ref = "" }},
		{"user", false, func(r *store.Run) { r.User = "" }},
		{"script", false, func(r *store.Run) { r.Script = "" }},
		{"trigger", false, func(r *store.Run) { r.Trigger = "event" }},
		{"start-zero", false, func(r *store.Run) { r.Started = time.Time{} }},
		{"start-normalizes-zero", false, func(r *store.Run) { r.Started = zeroFraction }},
		{"start-offset-normalizes-zero", false, func(r *store.Run) { r.Started = zeroFraction.In(time.FixedZone("west", -3600)) }},
		{"stdout-negative", false, func(r *store.Run) { r.StdoutBytes = -1 }},
		{"stderr-negative", false, func(r *store.Run) { r.StderrBytes = -1 }},
		{"sha-short", false, func(r *store.Run) { r.SHA = strings.Repeat("a", 39) }},
		{"sha-long", false, func(r *store.Run) { r.SHA = strings.Repeat("a", 41) }},
		{"sha-uppercase", false, func(r *store.Run) { r.SHA = "A" + strings.Repeat("a", 39) }},
		{"sha-nonhex", false, func(r *store.Run) { r.SHA = "g" + strings.Repeat("a", 39) }},
		{"running-empty-sha", false, func(r *store.Run) { r.SHA = "" }},
		{"running-exit", false, func(r *store.Run) { r.ExitCode = 1 }},
		{"running-stdout", false, func(r *store.Run) { r.StdoutBytes = 1 }},
		{"running-stderr", false, func(r *store.Run) { r.StderrBytes = 1 }},
		{"running-finished", false, func(r *store.Run) { r.Finished = stamp }},
		{"running-finished-normalizes-zero", false, func(r *store.Run) { r.Finished = zeroFraction }},
		{"running-truncated", false, func(r *store.Run) { r.Truncated = true }},
		{"running-reason", false, func(r *store.Run) { r.Reason = store.ReasonStartFailed }},
		{"status-empty", false, func(r *store.Run) { r.Status = "" }},
		{"status-unknown", false, func(r *store.Run) { r.Status = "unknown" }},
		{"status-exited", false, func(r *store.Run) { r.Status = store.StatusExited }},
		{"status-killed", false, func(r *store.Run) { r.Status = store.StatusKilled }},
		{"status-timeout", false, func(r *store.Run) { r.Status = store.StatusTimedOut }},
		{"failed-reason-empty", true, func(r *store.Run) { r.Reason = "" }},
		{"failed-reason-unknown", true, func(r *store.Run) { r.Reason = "unknown" }},
		{"failed-exit", true, func(r *store.Run) { r.ExitCode = 1 }},
		{"failed-finished-zero", true, func(r *store.Run) { r.Finished = time.Time{} }},
		{"failed-finished-normalizes-zero", true, func(r *store.Run) { r.Started = time.Time{}.Add(-time.Second); r.Finished = zeroFraction }},
		{"failed-finished-offset-normalizes-zero", true, func(r *store.Run) {
			r.Started = time.Time{}.Add(-time.Second)
			r.Finished = zeroFraction.In(time.FixedZone("east", 3600))
		}},
		{"failed-finished-before-start", true, func(r *store.Run) { r.Finished = r.Started.Add(-time.Second) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			s := open(t, path)
			a := create(t, s, "alice", "alpha")
			b := create(t, s, "bob", "beta")
			add(t, s, run(b, 1))
			r := run(a, 2)
			if tc.failed {
				r.Status = store.StatusFailed
				r.Reason = store.ReasonStartFailed
				r.Finished = r.Started
			}
			tc.mutate(&r)
			before := content(t, s)
			got, err := s.AddRun(ctx, r)
			equal(t, got, store.Run{})
			catalog(t, err)
			equal(t, content(t, s), before)
			must(t, closeStore(s))
			s = open(t, path)
			equal(t, content(t, s), before)
		})
	}
}

func TestNormalizedAddRunPersistence(t *testing.T) {
	// R-9CAY-4JXA R-Y4FI-1CFI
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := open(t, path)
	sc := create(t, s, "alice", "alpha")
	inputs := []store.Run{run(sc, 1)}
	running := run(sc, 2)
	running.Finished = time.Time{}.In(time.FixedZone("zero-offset", 3600))
	running.RequestID = ""
	inputs = append(inputs, running)
	for _, started := range []time.Time{time.Time{}.Add(time.Second + 800*time.Millisecond), time.Time{}.Add(-200 * time.Millisecond)} {
		r := run(sc, len(inputs)+1)
		r.Started = started.In(time.FixedZone("offset", 7200))
		inputs = append(inputs, r)
	}
	for _, reason := range []string{store.ReasonRepositoryMissing, store.ReasonCommitMissing, store.ReasonTooLarge, store.ReasonGitFailed, store.ReasonTimedOut, store.ReasonStartFailed} {
		for _, sha := range []string{"", "0123456789abcdef0123456789abcdef01234567"} {
			for _, truncated := range []bool{false, true} {
				r := run(sc, len(inputs)+1)
				r.Status, r.Reason, r.SHA, r.Truncated = store.StatusFailed, reason, sha, truncated
				r.StdoutBytes, r.StderrBytes = 7, 9
				// Earlier fractions within the same UTC second are valid.
				r.Finished = r.Started.Add(-500 * time.Millisecond).In(time.FixedZone("west", -7200))
				r.RequestID = ""
				inputs = append(inputs, r)
			}
		}
	}
	// Failed records also accept zero output sizes and nonzero normalized times adjacent to year one.
	for _, started := range []time.Time{time.Time{}.Add(time.Second + 800*time.Millisecond), time.Time{}.Add(-200 * time.Millisecond)} {
		r := run(sc, len(inputs)+1)
		r.Status, r.Reason, r.SHA = store.StatusFailed, store.ReasonGitFailed, ""
		r.Started, r.Finished = started, started.Add(-100*time.Millisecond)
		inputs = append(inputs, r)
	}
	want := make([]store.Run, 0, len(inputs))
	for _, r := range inputs {
		got, err := s.AddRun(ctx, r)
		must(t, err)
		r.Started = r.Started.UTC().Truncate(time.Second)
		if !r.Finished.IsZero() {
			r.Finished = r.Finished.UTC().Truncate(time.Second)
		} else {
			r.Finished = time.Time{}
		}
		equal(t, got, r)
		got, err = s.RunByID(ctx, r.ID)
		must(t, err)
		equal(t, got, r)
		want = append(want, r)
	}
	must(t, closeStore(s))
	s = open(t, path)
	for _, r := range want {
		got, err := s.RunByID(ctx, r.ID)
		must(t, err)
		equal(t, got, r)
	}
}

func TestNormalizedEndingRefusals(t *testing.T) {
	// R-9CAY-4JXA
	zeroFraction := time.Time{}.Add(750 * time.Millisecond)
	for i, e := range []store.Ending{
		{Status: store.StatusRunning, Finished: stamp},
		{Status: store.StatusFailed, Finished: stamp},
		{Status: "unknown", Finished: stamp},
		{Status: "", Finished: stamp},
		{Status: store.StatusExited, ExitCode: -1, Finished: stamp},
		{Status: store.StatusExited, ExitCode: 256, Finished: stamp},
		{Status: store.StatusKilled, ExitCode: -1, Finished: stamp},
		{Status: store.StatusKilled, ExitCode: 1, Finished: stamp},
		{Status: store.StatusTimedOut, ExitCode: -1, Finished: stamp},
		{Status: store.StatusTimedOut, ExitCode: 1, Finished: stamp},
		{Status: store.StatusExited},
		{Status: store.StatusExited, Finished: zeroFraction},
		{Status: store.StatusKilled, Finished: zeroFraction.In(time.FixedZone("east", 3600))},
		{Status: store.StatusTimedOut, Finished: zeroFraction.In(time.FixedZone("west", -3600))},
		{Status: store.StatusExited, Finished: stamp, StdoutBytes: -1},
		{Status: store.StatusExited, Finished: stamp, StderrBytes: -1},
	} {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			s := open(t, path)
			a := create(t, s, "alice", "alpha")
			b := create(t, s, "bob", "beta")
			r := run(a, 1)
			// A nonzero time before year one isolates zero normalization from the before-start refusal.
			r.Started = time.Time{}.Add(-time.Second)
			add(t, s, r)
			add(t, s, run(b, 2))
			before := content(t, s)
			got, err := s.FinishRun(ctx, r.ID, e)
			equal(t, got, store.Run{})
			catalog(t, err)
			equal(t, content(t, s), before)
			must(t, closeStore(s))
			s = open(t, path)
			equal(t, content(t, s), before)
		})
	}
}

func TestNormalizedFinishRun(t *testing.T) {
	// R-9CAY-4JXA R-LQMJ-7DR4
	endings := []store.Ending{}
	for _, code := range []int{0, 1, 254, 255} {
		endings = append(endings, store.Ending{Status: store.StatusExited, ExitCode: code})
	}
	endings = append(endings, store.Ending{Status: store.StatusKilled}, store.Ending{Status: store.StatusTimedOut})
	for _, ending := range endings {
		for _, truncated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-%d-%t", ending.Status, ending.ExitCode, truncated), func(t *testing.T) {
				s := open(t, "")
				sc := create(t, s, "alice", "alpha")
				r := add(t, s, run(sc, 1))
				e := ending
				e.Finished = stamp.Add(-500 * time.Millisecond).In(time.FixedZone("west", -3600))
				e.Truncated = truncated
				if truncated {
					e.StdoutBytes, e.StderrBytes = 13, 21
				}
				got, err := s.FinishRun(ctx, r.ID, e)
				must(t, err)
				r.Status, r.ExitCode, r.StdoutBytes, r.StderrBytes, r.Truncated = e.Status, e.ExitCode, e.StdoutBytes, e.StderrBytes, e.Truncated
				r.Finished = e.Finished.UTC().Truncate(time.Second)
				equal(t, got, r)
				got, err = s.RunByID(ctx, r.ID)
				must(t, err)
				equal(t, got, r)
				got, err = s.FindRun(ctx, sc.Owner, r.ID)
				must(t, err)
				equal(t, got, r)
				rr, err := s.Runs(ctx, sc.ID)
				must(t, err)
				equal(t, rr, []store.Run{r})
			})
		}
	}
	for _, finished := range []time.Time{time.Time{}.Add(time.Second + 400*time.Millisecond), time.Time{}.Add(-200 * time.Millisecond)} {
		s := open(t, "")
		sc := create(t, s, "alice", "alpha")
		r := run(sc, 1)
		r.Started = finished.Add(-2 * time.Second)
		r = add(t, s, r)
		got, err := s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: finished})
		must(t, err)
		r.Status, r.Finished = store.StatusExited, finished.UTC().Truncate(time.Second)
		equal(t, got, r)
	}
}

func assertReturnedRun(t *testing.T, r store.Run) {
	t.Helper()
	switch r.Status {
	case store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed:
	default:
		t.Fatalf("unknown status %q", r.Status)
	}
	equal(t, r.Finished.IsZero(), r.Status == store.StatusRunning)
	if r.Status != store.StatusExited {
		equal(t, r.ExitCode, 0)
	}
	if r.Status == store.StatusFailed {
		switch r.Reason {
		case store.ReasonRepositoryMissing, store.ReasonCommitMissing, store.ReasonTooLarge, store.ReasonGitFailed, store.ReasonTimedOut, store.ReasonStartFailed:
		default:
			t.Fatalf("unknown failure reason %q", r.Reason)
		}
	} else {
		equal(t, r.Reason, "")
	}
	equal(t, r.Started.Location(), time.UTC)
	equal(t, r.Started.Nanosecond(), 0)
	if !r.Finished.IsZero() {
		equal(t, r.Finished.Location(), time.UTC)
		equal(t, r.Finished.Nanosecond(), 0)
	}
}

func assertReturnedScript(t *testing.T, sc store.Script) {
	t.Helper()
	equal(t, sc.Created.Location(), time.UTC)
	equal(t, sc.Created.Nanosecond(), 0)
	if sc.Last != nil {
		assertReturnedRun(t, *sc.Last)
	}
}

func TestEveryStoreReturnedRecord(t *testing.T) {
	// R-KTP8-VKQD
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := open(t, path)
	scripts := []store.Script{}
	n := 0
	statuses := []string{store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed}
	reasons := []string{store.ReasonRepositoryMissing, store.ReasonCommitMissing, store.ReasonTooLarge, store.ReasonGitFailed, store.ReasonTimedOut, store.ReasonStartFailed}
	for _, status := range statuses {
		for _, reason := range reasons {
			if status != store.StatusFailed && reason != reasons[0] {
				continue
			}
			n++
			sc := create(t, s, "alice", fmt.Sprintf("script-%d", n))
			assertReturnedScript(t, sc)
			scripts = append(scripts, sc)
			r := run(sc, n)
			if status == store.StatusFailed {
				r.Status, r.Reason, r.Finished = status, reason, stamp
			}
			r = add(t, s, r)
			assertReturnedRun(t, r)
			if status != store.StatusRunning && status != store.StatusFailed {
				code := 0
				if status == store.StatusExited {
					code = 123
				}
				ended, err := s.FinishRun(ctx, r.ID, store.Ending{Status: status, ExitCode: code, Finished: stamp})
				must(t, err)
				assertReturnedRun(t, ended)
			}
		}
	}
	checkReads := func(s *store.Store, pastCount int) {
		for _, sc := range scripts {
			got, err := s.Find(ctx, sc.Owner, sc.Name)
			must(t, err)
			assertReturnedScript(t, got)
			if got.Last == nil {
				t.Fatal("missing last run")
			}
			got, _, err = s.SetRef(ctx, sc.ID, sc.Ref)
			must(t, err)
			assertReturnedScript(t, got)
			rr, err := s.Runs(ctx, sc.ID)
			must(t, err)
			for _, r := range rr {
				assertReturnedRun(t, r)
				r, err = s.RunByID(ctx, r.ID)
				must(t, err)
				assertReturnedRun(t, r)
				r, err = s.FindRun(ctx, sc.Owner, r.ID)
				must(t, err)
				assertReturnedRun(t, r)
			}
		}
		list, err := s.List(ctx, "alice")
		must(t, err)
		equal(t, len(list), len(scripts))
		for _, sc := range list {
			assertReturnedScript(t, sc)
		}
		rr, err := s.Running(ctx)
		must(t, err)
		if len(rr) == 0 {
			t.Fatal("expected running records")
		}
		for _, r := range rr {
			assertReturnedRun(t, r)
		}
		rr, err = s.PastKeeping(ctx, stamp.Add(10*24*time.Hour), 1, 1)
		must(t, err)
		equal(t, len(rr), pastCount)
		for _, r := range rr {
			assertReturnedRun(t, r)
		}
	}
	checkReads(s, 0)
	// A newer running record puts every older ended status beyond keepCount.
	for i, sc := range scripts {
		r := run(sc, 100+i)
		r.Started = stamp.Add(time.Hour)
		assertReturnedRun(t, add(t, s, r))
	}
	checkReads(s, len(scripts)-1)
	must(t, closeStore(s))
	s = open(t, path)
	checkReads(s, len(scripts)-1)
}
