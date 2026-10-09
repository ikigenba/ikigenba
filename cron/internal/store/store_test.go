package store_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/store"
)

var instant = time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.FixedZone("offset", 3600))

func open(t *testing.T, path string, random io.Reader) (*db.DB, *store.Store) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: cron.Migrations(), Now: func() time.Time { return instant }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	// R-CAJF-PI3J R-CBRC-39U8
	return d, store.New(d, store.Config{Now: func() time.Time { return instant }, Rand: random})
}
func fixture(t *testing.T) (*db.DB, *store.Store) {
	t.Helper()
	data := make([]byte, 8*128)
	for i := 0; i < 128; i++ {
		data[i*8+7] = byte(i + 1)
	}
	return open(t, filepath.Join(t.TempDir(), "state", "cron.db"), bytes.NewReader(data))
}
func draft(slug string) store.Draft {
	// R-9OFG-SIBL
	return store.Draft{Slug: slug, When: "*/15 * * * *", OwnerID: "owner", OwnerEmail: "Mixed+tag@example.test"}
}
func create(t *testing.T, s *store.Store, slug string) store.Trigger {
	t.Helper()
	result, err := s.Create(context.Background(), draft(slug))
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func content(t *testing.T, s *store.Store) []store.Trigger {
	t.Helper()
	result, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func same(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
func failure(t *testing.T, result store.Trigger, err, want error) {
	t.Helper()
	same(t, result, store.Trigger{})
	if !errors.Is(err, want) {
		t.Fatalf("error %v, want %v", err, want)
	}
}

func TestValidation(t *testing.T) {
	// R-CHUU-04JP R-CJ2Q-DWAE R-4051-Y1M9
	same(t, store.IDPrefix, "crn_")
	same(t, store.Active, "active")
	same(t, store.Paused, "paused")
	const unreachable string = store.Unreachable
	if unreachable == "" {
		t.Fatal("empty unreachable response")
	}
	// R-CE74-UTBM R-9Y6N-UO95
	for _, s := range []string{"crn_0123456789abcdef", "crn_0000000000000000"} {
		if !store.ValidID(s) {
			t.Error(s)
		}
	}
	for _, s := range []string{"", "crn_0123456789abcde", "crn_0123456789abcdef0", "CRN_0123456789abcdef", "crn_0123456789abcdeF", "crn_0123456789abcdeg", "crn_0123456789abcdeé"} {
		if store.ValidID(s) {
			t.Error(s)
		}
	}
	// R-CFF1-8L2B R-9ZEK-8FZU
	for _, s := range []string{"crm_sync", "a", "nightly_backup", "a1_b2", strings.Repeat("a", 64)} {
		if !store.ValidSlug(s) {
			t.Error(s)
		}
	}
	for _, s := range []string{"crm-sync", "CRM_Sync", "9am_report", "_crm", "crm__sync", "crm_", "crm.sync", "crm sync", " crm_sync", "", strings.Repeat("a", 65), "aé"} {
		if store.ValidSlug(s) {
			t.Error(s)
		}
	}
	// R-CGMX-MCT0 R-A0MG-M7QJ
	for _, s := range []string{"@hourly", "@daily", "@weekly", "@monthly", "@yearly", "*/15 * * * *", "0 9 * * MON-FRI", "30 2 * * *", "0 8 * * 1", "0 0 30 2 *"} {
		if !store.ValidWhen(s) {
			t.Error(s)
		}
	}
	for _, s := range []string{"0 */15 * * * *", "@every 5m", "@reboot", "@annually", "@midnight", "@DAILY", "bogus", "", " */15 * * * *", "*/15 * * * * ", "*/15  * * * *", "*/15 * * *\t*", "*/15 * * *\n*", "*/15 * * *\r*", "*/15 * * *\v*", "*/15 * * *\f*", "0 0 * * 7", "60 * * * *", "TZ=UTC * * * * *"} {
		if store.ValidWhen(s) {
			t.Error(s)
		}
	}
	// R-9TB2-BLAD
	errs := []error{store.ErrNotFound, store.ErrSlugTaken, store.ErrInvalid, store.ErrUnreachable}
	for i, a := range errs {
		if a == nil {
			t.Fatal("nil error")
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Fatalf("errors overlap: %v %v", a, b)
			}
		}
	}
}
func TestRecordsAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state", "cron.db")
	d, s := open(t, path, bytes.NewReader([]byte{0x3f, 0x9a, 0x1c, 0x7e, 0x5b, 0x2d, 0x80, 0x46, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2}))
	// R-9UIY-PD12 R-A1UC-ZZH8
	empty := content(t, s)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("not empty: %v", empty)
	}
	for _, slug := range []string{"missing", "", "bad slug"} {
		got, err := s.Get(ctx, slug)
		failure(t, got, err, store.ErrNotFound)
	}
	// R-9VQV-34RR R-9N7K-EQKW R-VEJZ-ZR1Z R-V5SE-48L0
	x := create(t, s, "crm_sync")
	want := store.Trigger{ID: "crn_3f9a1c7e5b2d8046", Slug: "crm_sync", When: draft("crm_sync").When, OwnerID: "owner", OwnerEmail: draft("").OwnerEmail, Status: store.Active, Created: instant.UTC().Truncate(time.Second)}
	same(t, x, want)
	// R-AADN-ODO3
	got, err := s.Get(ctx, x.Slug)
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, x)
	for _, slug := range []string{"", x.ID, "CRM_SYNC"} {
		got, err = s.Get(ctx, slug)
		failure(t, got, err, store.ErrNotFound)
	}
	other := create(t, s, "nightly_backup")
	// R-AE1C-TOW6
	for _, when := range []string{"@daily", "@daily", "0 0 30 2 *"} {
		x, err = s.SetWhen(ctx, x.ID, when)
		if err != nil {
			t.Fatal(err)
		}
		want.When = when
		same(t, x, want)
		got, err = s.Get(ctx, x.Slug)
		if err != nil {
			t.Fatal(err)
		}
		same(t, got, want)
		same(t, content(t, s), []store.Trigger{want, other})
	}
	// R-AIWY-CRUY R-ATW1-SPJ7
	for _, slot := range []time.Time{instant.Add(time.Hour), instant.Add(-time.Hour)} {
		x, err = s.SetLastFired(ctx, x.ID, slot)
		if err != nil {
			t.Fatal(err)
		}
		want.LastFired = slot.UTC().Truncate(time.Second)
		same(t, x, want)
		same(t, content(t, s), []store.Trigger{want, other})
		got, err = s.Get(ctx, x.Slug)
		if err != nil {
			t.Fatal(err)
		}
		same(t, got, want)
		if x.Created.Location() != time.UTC || x.LastFired.Location() != time.UTC || x.Created.Nanosecond() != 0 || x.LastFired.Nanosecond() != 0 {
			t.Fatal("time normalization")
		}
	}
	// R-AGH5-L8DK
	for _, status := range []string{store.Paused, store.Paused, store.Active} {
		x, err = s.SetStatus(ctx, x.ID, status)
		if err != nil {
			t.Fatal(err)
		}
		want.Status = status
		got, err = s.Get(ctx, x.Slug)
		if err != nil {
			t.Fatal(err)
		}
		same(t, got, want)
		same(t, x, want)
		same(t, content(t, s), []store.Trigger{want, other})
	}
	// R-VGZS-RAJD
	deleted, err := s.Delete(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	same(t, deleted, other)
	same(t, content(t, s), []store.Trigger{want})
	got, err = s.Get(ctx, other.Slug)
	failure(t, got, err, store.ErrNotFound)
	// R-A329-DR7X
	before := content(t, s)
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	_, s = open(t, path, bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 2}))
	same(t, content(t, s), before)
	got, err = s.Get(ctx, want.Slug)
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, want)
	got, err = s.Get(ctx, other.Slug)
	failure(t, got, err, store.ErrNotFound)
	replacement := draft(other.Slug)
	replacement.OwnerID = "another"
	got, err = s.Create(ctx, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == want.ID || !got.LastFired.IsZero() || got.OwnerID != "another" {
		t.Fatalf("replacement: %v", got)
	}
	same(t, content(t, s)[0], want)
}

func TestIDCollisionAndNoDrawOnRejection(t *testing.T) {
	// R-V70A-I0BP R-A6PY-J2G0 R-A7XU-WU6P R-9WYR-GWIG
	data := []byte{0x3f, 0x9a, 0x1c, 0x7e, 0x5b, 0x2d, 0x80, 0x46, 0x3f, 0x9a, 0x1c, 0x7e, 0x5b, 0x2d, 0x80, 0x46, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2}
	random := bytes.NewReader(data)
	_, s := open(t, filepath.Join(t.TempDir(), "state", "cron.db"), random)
	first := create(t, s, "first")
	same(t, random.Len(), 24)
	second := create(t, s, "second")
	same(t, second.ID, "crn_0000000000000001")
	same(t, random.Len(), 8)
	same(t, content(t, s)[0], first)
	before := content(t, s)
	bad := []store.Draft{draft("bad slug"), {Slug: "new", When: "bogus", OwnerID: "owner"}, {Slug: "new", When: "@daily"}}
	for _, d := range bad {
		got, err := s.Create(context.Background(), d)
		failure(t, got, err, store.ErrInvalid)
		same(t, content(t, s), before)
		same(t, random.Len(), 8)
	}
	duplicate := draft("first")
	duplicate.OwnerID = "another"
	got, err := s.Create(context.Background(), duplicate)
	failure(t, got, err, store.ErrSlugTaken)
	same(t, random.Len(), 8)
	same(t, content(t, s), before)
}
func TestInvalidAndMissingChanges(t *testing.T) {
	// R-AF99-7GMV R-AHP1-Z049 R-AK4U-QJLN R-AMKN-I331
	_, s := fixture(t)
	x := create(t, s, "first")
	before := content(t, s)
	ctx := context.Background()
	calls := []struct {
		call func() (store.Trigger, error)
		want error
	}{
		{func() (store.Trigger, error) { return s.SetWhen(ctx, x.ID, "bad") }, store.ErrInvalid},
		{func() (store.Trigger, error) { return s.SetWhen(ctx, "missing", "@daily") }, store.ErrNotFound},
		{func() (store.Trigger, error) { return s.SetStatus(ctx, x.ID, "stopped") }, store.ErrInvalid},
		{func() (store.Trigger, error) { return s.SetStatus(ctx, "missing", store.Active) }, store.ErrNotFound},
		{func() (store.Trigger, error) { return s.SetLastFired(ctx, x.ID, time.Time{}) }, store.ErrInvalid},
		{func() (store.Trigger, error) { return s.SetLastFired(ctx, "missing", instant) }, store.ErrNotFound},
		{func() (store.Trigger, error) { return s.Delete(ctx, "missing") }, store.ErrNotFound},
	}
	for _, tc := range calls {
		got, err := tc.call()
		failure(t, got, err, tc.want)
		same(t, content(t, s), before)
	}
}

func allCalls(ctx context.Context, s *store.Store, id string) []func() error {
	return []func() error{
		func() error { _, err := s.List(ctx); return err }, func() error { _, err := s.Get(ctx, "first"); return err },
		func() error { _, err := s.Create(ctx, draft("new")); return err }, func() error { _, err := s.SetWhen(ctx, id, "@yearly"); return err },
		func() error { _, err := s.SetStatus(ctx, id, store.Paused); return err }, func() error { _, err := s.SetLastFired(ctx, id, instant.Add(time.Hour)); return err },
		func() error { _, err := s.Delete(ctx, id); return err },
	}
}

// Conflicting arguments must not mask a cancelled context or failing handle.
func conflictingCalls(ctx context.Context, s *store.Store) []func() error {
	return []func() error{
		func() error { _, err := s.Get(ctx, "missing"); return err },
		func() error { _, err := s.Create(ctx, store.Draft{Slug: "first", When: "bad"}); return err },
		func() error { _, err := s.SetWhen(ctx, "missing", "bad"); return err },
		func() error { _, err := s.SetStatus(ctx, "missing", "stopped"); return err },
		func() error { _, err := s.SetLastFired(ctx, "missing", time.Time{}); return err },
		func() error { _, err := s.Delete(ctx, "missing"); return err },
	}
}
func TestFailuresAndPrecedence(t *testing.T) {
	// R-ANSJ-VUTQ R-AQ8C-NEB4 R-PLFA-QUWV R-PQAW-9XVN
	d, s := fixture(t)
	create(t, s, "first")
	before := content(t, s)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, call := range append(allCalls(cancelled, s, before[0].ID), conflictingCalls(cancelled, s)...) {
			err := call()
			if !errors.Is(err, context.Canceled) || !errors.Is(err, store.ErrUnreachable) {
				t.Fatalf("cancel precedence: %v", err)
			}
		}
	}
	for _, call := range append(allCalls(context.Background(), s, before[0].ID), conflictingCalls(context.Background(), s)...) {
		err := call()
		if !errors.Is(err, store.ErrUnreachable) {
			t.Fatalf("failing: %v", err)
		}
		for _, recordErr := range []error{store.ErrInvalid, store.ErrNotFound, store.ErrSlugTaken} {
			if errors.Is(err, recordErr) {
				t.Fatalf("overlap: %v", err)
			}
		}
	}
	_, err := s.Create(context.Background(), store.Draft{Slug: "first", When: "bad"})
	if !errors.Is(err, store.ErrUnreachable) {
		t.Fatal(err)
	}
	d.SetFailing(false)
	same(t, content(t, s), before)
	got, err := s.Create(context.Background(), store.Draft{Slug: "first", When: "bad", OwnerID: "owner"})
	failure(t, got, err, store.ErrInvalid)
	got, err = s.SetStatus(context.Background(), "missing", "stopped")
	failure(t, got, err, store.ErrInvalid)
	same(t, content(t, s), before)
}
func TestArgumentAndSlugPrecedeRandomFailure(t *testing.T) {
	// R-PQAW-9XVN
	random := bytes.NewReader(make([]byte, 8))
	_, s := open(t, filepath.Join(t.TempDir(), "state", "cron.db"), random)
	x := create(t, s, "first")
	same(t, random.Len(), 0)
	before := content(t, s)
	ctx := context.Background()
	for _, d := range []store.Draft{
		{Slug: "first", When: "bad", OwnerID: "owner"},
		{Slug: "bad slug", When: "@daily", OwnerID: "owner"},
		{Slug: "new", When: "@daily"},
	} {
		got, err := s.Create(ctx, d)
		failure(t, got, err, store.ErrInvalid)
	}
	got, err := s.Create(ctx, draft("first"))
	failure(t, got, err, store.ErrSlugTaken)
	got, err = s.Create(ctx, draft("new"))
	failure(t, got, err, store.ErrUnreachable)
	for _, call := range []func() (store.Trigger, error){
		func() (store.Trigger, error) { return s.SetWhen(ctx, "missing", "bad") },
		func() (store.Trigger, error) { return s.SetStatus(ctx, "missing", "stopped") },
		func() (store.Trigger, error) { return s.SetLastFired(ctx, "missing", time.Time{}) },
	} {
		got, err = call()
		failure(t, got, err, store.ErrInvalid)
	}
	got, err = s.SetStatus(ctx, "missing", store.Paused)
	failure(t, got, err, store.ErrNotFound)
	got, err = s.SetStatus(ctx, x.ID, store.Paused)
	if err != nil {
		t.Fatal(err)
	}
	before[0].Status = store.Paused
	same(t, got, before[0])
	same(t, content(t, s), before)
}
func TestRandomFailure(t *testing.T) {
	// R-ARG9-161T
	_, s := open(t, filepath.Join(t.TempDir(), "state", "cron.db"), bytes.NewReader([]byte{1, 2, 3}))
	before := content(t, s)
	got, err := s.Create(context.Background(), draft("first"))
	failure(t, got, err, store.ErrUnreachable)
	same(t, content(t, s), before)
}
func TestListAcrossOwners(t *testing.T) {
	// R-42KU-PL3N
	_, s := fixture(t)
	names := []string{"weekly_digest", "nightly_backup", "month_end", "hourly", "crm_sync"}
	for i, name := range names {
		d := draft(name)
		d.OwnerID = fmt.Sprintf("owner%d", i)
		if _, err := s.Create(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	rows := content(t, s)
	actual := make([]string, len(rows))
	for i, row := range rows {
		actual[i] = row.Slug
	}
	same(t, actual, []string{"crm_sync", "hourly", "month_end", "nightly_backup", "weekly_digest"})
}
func TestConcurrentStore(t *testing.T) {
	// R-CMQF-J7IH R-A95R-ALXE R-ASO5-EXSI
	_, s := fixture(t)
	ctx := context.Background()
	const count = 20
	var group sync.WaitGroup
	errorsOut := make(chan error, count)
	successes := make(chan store.Trigger, count)
	for i := 0; i < count; i++ {
		group.Go(func() {
			got, err := s.Create(ctx, draft("contested"))
			if err == nil {
				successes <- got
			} else {
				errorsOut <- err
			}
		})
	}
	group.Wait()
	close(successes)
	close(errorsOut)
	same(t, len(successes), 1)
	same(t, len(errorsOut), count-1)
	for err := range errorsOut {
		if !errors.Is(err, store.ErrSlugTaken) {
			t.Fatal(err)
		}
	}
	same(t, len(content(t, s)), 1)
	// Concurrent distinct creates consume one bytes.Reader exclusively through the writer.
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		group.Go(func() {
			slug := fmt.Sprintf("trigger%d", i)
			x, err := s.Create(ctx, draft(slug))
			if err != nil {
				errs <- err
				return
			}
			if _, err = s.Get(ctx, slug); err != nil {
				errs <- err
				return
			}
			if _, err = s.List(ctx); err != nil {
				errs <- err
				return
			}
			if _, err = s.SetWhen(ctx, x.ID, "@hourly"); err != nil {
				errs <- err
				return
			}
			if _, err = s.SetStatus(ctx, x.ID, store.Paused); err != nil {
				errs <- err
				return
			}
			if _, err = s.SetLastFired(ctx, x.ID, instant); err != nil {
				errs <- err
				return
			}
			if _, err = s.Delete(ctx, x.ID); err != nil {
				errs <- err
			}
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	same(t, len(content(t, s)), 1)
}

type failedRandom struct{ err error }

func (r failedRandom) Read([]byte) (int, error) { return 0, r.err }
func TestRandomErrorClassification(t *testing.T) {
	for _, cause := range []error{io.EOF, store.ErrInvalid, store.ErrNotFound, store.ErrSlugTaken} {
		_, s := open(t, filepath.Join(t.TempDir(), "state", "cron.db"), failedRandom{cause})
		got, err := s.Create(context.Background(), draft("first"))
		failure(t, got, err, store.ErrUnreachable)
		for _, record := range []error{store.ErrInvalid, store.ErrNotFound, store.ErrSlugTaken} {
			if errors.Is(err, record) {
				t.Fatalf("random error classified as record error: %v", err)
			}
		}
		same(t, len(content(t, s)), 0)
	}
}

func TestFourTriggers(t *testing.T) {
	// R-41CY-BTCY
	ctx := context.Background()
	want := []store.Trigger{
		{ID: "crn_3f9a1c7e5b2d8046", Slug: "hourly", When: "@hourly", OwnerID: "u_7f3a9c21", OwnerEmail: "mg@example.com", Status: store.Active, Created: stamp(t, "2026-09-20T08:00:00Z"), LastFired: stamp(t, "2026-10-05T09:00:00Z")},
		{ID: "crn_1a4f8c6e9b3d7025", Slug: "month_end", When: "@monthly", OwnerID: "u_2b8e1d04", OwnerEmail: "ann@example.com", Status: store.Active, Created: stamp(t, "2026-10-04T10:00:00Z")},
		{ID: "crn_8d2e6b4a1f7c3095", Slug: "nightly_backup", When: "30 2 * * *", OwnerID: "u_2b8e1d04", OwnerEmail: "ann@example.com", Status: store.Active, Created: stamp(t, "2026-09-28T17:15:00Z"), LastFired: stamp(t, "2026-10-05T02:30:00Z")},
		{ID: "crn_5c7b9e2f4a6d1038", Slug: "weekly_digest", When: "0 8 * * 1", OwnerID: "u_7f3a9c21", OwnerEmail: "mg@example.com", Status: store.Paused, Created: stamp(t, "2026-09-01T12:00:00Z"), LastFired: stamp(t, "2026-09-28T08:00:00Z")},
	}
	var random []byte
	for _, x := range want {
		b, err := hex.DecodeString(strings.TrimPrefix(x.ID, store.IDPrefix))
		if err != nil {
			t.Fatal(err)
		}
		random = append(random, b...)
	}
	d, _ := fixture(t)
	var now time.Time
	s := store.New(d, store.Config{Now: func() time.Time { return now }, Rand: bytes.NewReader(random)})
	for _, x := range want {
		now = x.Created
		got, err := s.Create(ctx, store.Draft{Slug: x.Slug, When: x.When, OwnerID: x.OwnerID, OwnerEmail: x.OwnerEmail})
		if err != nil {
			t.Fatal(err)
		}
		if !x.LastFired.IsZero() {
			got, err = s.SetLastFired(ctx, got.ID, x.LastFired)
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err = s.SetStatus(ctx, got.ID, x.Status)
		if err != nil {
			t.Fatal(err)
		}
		same(t, got, x)
	}
	same(t, content(t, s), want)
}

func stamp(t *testing.T, value string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
