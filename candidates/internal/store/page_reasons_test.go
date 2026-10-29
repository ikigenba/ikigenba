package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-ZYYT-KILO
func TestPageReasonsMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0027_page_reasons.sql")
	contractOK(t, err)
	contractEqual(t, string(b), "ALTER TABLE pages ADD COLUMN reason TEXT;\nCREATE TABLE page_refetches (seq INTEGER PRIMARY KEY, outcome TEXT);\nINSERT INTO page_refetches (seq) SELECT seq FROM pages WHERE size = 262144 AND seq IN (SELECT MAX(seq) FROM pages GROUP BY candidate, url);\n")
}

func priorPageCandidate(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated,name_source,name_record,address_source,address_record,phone_source,phone_record,email_source,email_record,other_names,other_addresses,other_phones,other_emails) VALUES (?,'active','','old','["old"]','old','','','','','','','','[]','','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','','','','','','','','','[]','[]','[]','[]')`, id)
	return err
}
func priorPage(ctx context.Context, tx *sql.Tx, p store.Page) (int64, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO pages(candidate,url,title,text,size,fetched,user_id,user_email) VALUES(?,?,?,?,?,?,?,?)`, p.Candidate, p.URL, p.Title, p.Text, p.Size, p.Fetched.Format(time.RFC3339), p.UserID, p.UserEmail)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func pageRefetchFixture(t *testing.T) (*db.DB, *store.Store, string, []store.Page) {
	t.Helper()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "state.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0026_fact_sources.sql"), Now: func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }})
	contractOK(t, err)
	pages := []store.Page{}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{"cand_1111111111111111", "cand_2222222222222222"} {
			if e := priorPageCandidate(ctx, tx, id); e != nil {
				return e
			}
		}
		for i, spec := range []struct {
			candidate, url string
			size           int64
		}{
			{"cand_1111111111111111", "a", 262144}, {"cand_1111111111111111", "a", 12},
			{"cand_1111111111111111", "b", 262144}, {"cand_1111111111111111", "b", 262144},
			{"cand_2222222222222222", "a", 262144}, {"cand_2222222222222222", "A", 262144},
			{"cand_2222222222222222", "small", 262143},
		} {
			p := keptPage(spec.candidate, spec.url)
			p.Size = spec.size
			p.Title = string(rune('a' + i))
			p.Fetched = time.Date(2026, 10, 1, 2, 3, i, 0, time.UTC)
			number, e := priorPage(ctx, tx, p)
			if e != nil {
				return e
			}
			p.Number = number
			pages = append(pages, p)
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }})
	contractOK(t, err)
	s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }})
	t.Cleanup(func() { contractOK(t, d.Close()) })
	return d, s, path, pages
}

// R-E0CR-S781 R-01EM-C232 R-ZSVB-NNW7
func TestPageReasonsUpgradeAndRefetchCursor(t *testing.T) {
	_, s, _, pages := pageRefetchFixture(t)
	ctx := t.Context()
	for _, want := range pages {
		got, err := s.GetPage(ctx, want.Candidate, want.Number)
		contractOK(t, err)
		contractEqual(t, got, want)
	}
	want := []store.Page{pages[3], pages[4], pages[5]}
	got, err := s.PendingRefetches(ctx, 0, 100)
	contractOK(t, err)
	contractEqual(t, got, want)
	for _, id := range []string{pages[0].Candidate, pages[4].Candidate} {
		listed, e := s.Pages(ctx, id)
		contractOK(t, e)
		for _, p := range listed {
			contractEqual(t, p.Reason, "")
		}
		subject, e := s.Subject(ctx, id)
		contractOK(t, e)
		for _, p := range subject.Pages {
			contractEqual(t, p.Reason, "")
		}
	}
	walked := []store.Page{}
	var after int64
	for {
		part, e := s.PendingRefetches(ctx, after, 1)
		contractOK(t, e)
		if len(part) == 0 {
			contractEqual(t, part, []store.Page{})
			break
		}
		walked = append(walked, part...)
		after = part[0].Number
	}
	contractEqual(t, walked, want)
	for _, n := range []int{0, -1} {
		part, e := s.PendingRefetches(ctx, 0, n)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, part, []store.Page(nil))
	}
	// A merge follows the page rather than freezing its original candidate id.
	_, _, err = s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: pages[0].Candidate, Other: pages[4].Candidate, UserID: "owner", Why: "same"})
	contractOK(t, err)
	got, err = s.PendingRefetches(ctx, 0, 100)
	contractOK(t, err)
	for i := range want {
		want[i].Candidate = pages[0].Candidate
	}
	contractEqual(t, got, want)
}

// R-ZNZQ-4KXF R-ZVB4-F7DL R-ZWJ0-SZ4A R-ZXQX-6QUZ R-ZU38-1FMW
func TestRefetchOutcomesPersistAndOnlySettle(t *testing.T) {
	type word string
	const read word = store.RefetchRead
	const unreadable word = store.RefetchUnreadable
	const failed word = store.RefetchFailed
	contractEqual(t, []word{read, unreadable, failed}, []word{"read", "unreadable", "failed"})
	d, s, path, pages := pageRefetchFixture(t)
	ctx := t.Context()
	pending, err := s.PendingRefetches(ctx, 0, 100)
	contractOK(t, err)
	before, err := s.Pages(ctx, pages[0].Candidate)
	contractOK(t, err)
	for _, bad := range []string{"", "unknown", "READ"} {
		contractError(t, s.SettleRefetch(ctx, -1, bad), store.ErrUnreachable)
	}
	for _, number := range []int64{-1, 0, pages[0].Number, pages[1].Number, 9999} {
		contractError(t, s.SettleRefetch(ctx, number, store.RefetchRead), store.ErrNotFound)
	}
	for i, p := range pending {
		outcome := []string{store.RefetchRead, store.RefetchUnreadable, store.RefetchFailed}[i]
		contractOK(t, s.SettleRefetch(ctx, p.Number, outcome))
		contractOK(t, s.SettleRefetch(ctx, p.Number, store.RefetchFailed))
		contractOK(t, s.SettleRefetch(ctx, p.Number, store.RefetchRead))
		var got string
		contractOK(t, d.Read(ctx, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "SELECT outcome FROM page_refetches WHERE seq=?", p.Number).Scan(&got)
		}))
		contractEqual(t, got, outcome)
		page, e := s.GetPage(ctx, p.Candidate, p.Number)
		contractOK(t, e)
		contractEqual(t, page, p)
	}
	after, err := s.Pages(ctx, pages[0].Candidate)
	contractOK(t, err)
	contractEqual(t, after, before)
	p := keptPage(pages[0].Candidate, "new")
	p.Size = 262144
	_, err = s.AddPage(ctx, p)
	contractOK(t, err)
	contractOK(t, s.ReplaceZipDrive(ctx, map[string]int64{}))
	_, err = s.FillFacts(ctx, "", 500)
	contractOK(t, err)
	got, err := s.PendingRefetches(ctx, -1, 100)
	contractOK(t, err)
	contractEqual(t, got, []store.Page{})
	// A later handle observes settled outcomes and the unchanged original copies.
	later, err := db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }})
	contractOK(t, err)
	defer func() { contractOK(t, later.Close()) }()
	for i, p := range pending {
		var outcome string
		contractOK(t, later.Read(ctx, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, "SELECT outcome FROM page_refetches WHERE seq=?", p.Number).Scan(&outcome)
		}))
		contractEqual(t, outcome, []string{store.RefetchRead, store.RefetchUnreadable, store.RefetchFailed}[i])
	}
	other := store.New(later, store.Config{})
	got, err = other.PendingRefetches(ctx, 0, 100)
	contractOK(t, err)
	contractEqual(t, got, []store.Page{})
	for _, p := range pending {
		got, e := other.GetPage(ctx, p.Candidate, p.Number)
		contractOK(t, e)
		contractEqual(t, got, p)
	}
}

// R-02MI-PTTR
func TestPageReasonsPreviousWriter(t *testing.T) {
	f := contractDB(t)
	ctx := t.Context()
	a := contractSeed(t, f)
	b := enrichmentCandidate(t, f, "other-page-owner", "other@owner.test")
	p := keptPage(a, "https://legacy.test/unreadable")
	p.Text = ""
	p.Reason = website.ReasonScripted
	q, err := f.s.AddPage(ctx, p)
	contractOK(t, err)
	inserted := keptPage(a, "https://legacy.test/after-rollback")
	inserted.Size = 262144
	inserted.Fetched = f.now.UTC().Truncate(time.Second)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		n, e := priorPage(ctx, tx, inserted)
		inserted.Number = n
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE pages SET candidate=? WHERE candidate=?", b, a)
		return e
	}))
	var text string
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		var candidate, url, title, fetched, uid, email string
		var seq, size int64
		return tx.QueryRowContext(ctx, "SELECT seq,candidate,url,title,text,size,fetched,user_id,user_email FROM pages WHERE seq=?", q.Number).Scan(&seq, &candidate, &url, &title, &text, &size, &fetched, &uid, &email)
	}))
	contractEqual(t, text, "")
	q.Candidate = b
	inserted.Candidate = b
	for _, want := range []store.Page{q, inserted} {
		got, e := f.s.GetPage(ctx, b, want.Number)
		contractOK(t, e)
		contractEqual(t, got, want)
	}
	pending, err := f.s.PendingRefetches(ctx, 0, 1)
	contractOK(t, err)
	contractEqual(t, pending, []store.Page{})
}

// R-ZP7M-ICO4 R-ZQFI-W4ET R-2ZZI-ZG6K
func TestUnreadablePageStorageAndSubject(t *testing.T) {
	f := contractDB(t)
	ctx := t.Context()
	id := contractSeed(t, f)
	for _, reason := range []string{website.ReasonScripted, website.ReasonRedirect, website.ReasonEmpty} {
		p := keptPage(id, "https://unreadable.test/"+reason)
		p.Text = ""
		p.Reason = reason
		got, err := f.s.AddPage(ctx, p)
		contractOK(t, err)
		p.Number = got.Number
		p.Fetched = f.now.UTC().Truncate(time.Second)
		contractEqual(t, got, p)
	}
	first := keptPage(id, "https://unreadable.test/shared")
	saved, err := f.s.AddPage(ctx, first)
	contractOK(t, err)
	first.Reason = website.ReasonEmpty
	first.Text = ""
	newer, err := f.s.AddPage(ctx, first)
	contractOK(t, err)
	subject, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, len(subject.Pages), 4)
	for _, p := range subject.Pages {
		got, e := f.s.GetPage(ctx, id, p.Number)
		contractOK(t, e)
		contractEqual(t, p, enrichers.Page{Number: got.Number, URL: got.URL, Title: got.Title, Text: got.Text, Reason: got.Reason, Size: got.Size})
	}
	contractEqual(t, subject.Pages[3].Number, newer.Number)
	contractEqual(t, (enrichers.Brief{}).Inputs(subject), "")
	original, err := f.s.GetPage(ctx, id, saved.Number)
	contractOK(t, err)
	contractEqual(t, original.Text, saved.Text)
	for _, reason := range []string{"SCRIPTED", "unknown"} {
		p := first
		p.Reason = reason
		got, e := f.s.AddPage(ctx, p)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, got, store.Page{})
	}
	bad := first
	bad.Text = "not empty"
	got, err := f.s.AddPage(ctx, bad)
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, got, store.Page{})
	f.reopen()
	got, err = f.s.GetPage(ctx, id, newer.Number)
	contractOK(t, err)
	contractEqual(t, got, newer)
}
