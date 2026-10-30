package store_test

import (
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	campaigns "github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
)

func rawVisit(t *testing.T, f *fixture, p store.Piece, at time.Time) {
	t.Helper()
	if e := f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO visits(piece,at,user_agent,referrer,address)VALUES(?,?,?,?,?)", p.ID, legacyTime(at), "old agent", "old referrer", "old address")
		return e
	}); e != nil {
		t.Fatal(e)
	}
}

func writeRecords(t *testing.T, f *fixture, q string, args ...any) {
	t.Helper()
	if e := f.d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, q, args...); return e }); e != nil {
		t.Fatal(e)
	}
}

func walkOpened(t *testing.T, s *store.Store, n int) []store.Opened {
	t.Helper()
	out := []store.Opened{}
	var key store.MemberKey
	for range 100 {
		next, page, e := s.MarkOpened(ctx, key, n)
		if e != nil {
			t.Fatal(e)
		}
		if page == nil {
			t.Fatal("nil successful page")
		}
		out = append(out, page...)
		if next == (store.MemberKey{}) {
			return out
		}
		key = next
	}
	t.Fatal("walk did not finish")
	return nil
}

// R-ZQ4J-G68T R-ZSKC-7PQ7 R-ZW81-D0YA R-RTK2-JPQ2
func TestOpenedPages(t *testing.T) {
	f := newFixture(t)
	key, opened, e := f.s.MarkOpened(ctx, store.MemberKey{}, 2)
	eq(t, key, store.MemberKey{})
	eq(t, must(t, opened, e), []store.Opened{})
	a, deleted, b := f.campaign(), f.campaign(), f.campaign()
	a3 := f.member(a, 3, "mail", "email")
	a1 := f.member(a, 1, "mail")
	_ = f.member(a, 2, "mail") // An unvisited member still consumes a page position.
	d := f.member(deleted, 1, "mail")
	b1 := f.member(b, 1, "mail")
	first := stamp(f).Add(-2 * time.Hour)
	rawVisit(t, f, a3.Pieces[0].Piece, stamp(f))
	rawVisit(t, f, a3.Pieces[1].Piece, first)
	rawVisit(t, f, a1.Pieces[0].Piece, first)
	rawVisit(t, f, d.Pieces[0].Piece, first)
	rawVisit(t, f, b1.Pieces[0].Piece, first)
	_, _, e = f.s.RejectMember(ctx, store.Rejection{Campaign: a.ID, Candidate: a3.Member.Candidate, Why: "kept rejection", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = f.s.DeleteCampaign(ctx, deleted.ID)
	if e != nil {
		t.Fatal(e)
	}
	before, e := f.s.GetMember(ctx, a.ID, a3.Member.Candidate)
	if e != nil {
		t.Fatal(e)
	}
	key, opened, e = f.s.MarkOpened(ctx, store.MemberKey{}, 2)
	eq(t, key, store.MemberKey{a.ID, "cand_0000000000000002"})
	eq(t, must(t, opened, e), []store.Opened{{a.ID, a1.Member.Candidate, first}})
	key, opened, e = f.s.MarkOpened(ctx, key, 2)
	eq(t, key, store.MemberKey{deleted.ID, d.Member.Candidate})
	eq(t, must(t, opened, e), []store.Opened{{a.ID, a3.Member.Candidate, first}})
	key, opened, e = f.s.MarkOpened(ctx, key, 2)
	eq(t, key, store.MemberKey{})
	eq(t, must(t, opened, e), []store.Opened{{b.ID, b1.Member.Candidate, first}})
	after, e := f.s.GetMember(ctx, a.ID, a3.Member.Candidate)
	eq(t, must(t, after, e), before)
	eq(t, walkOpened(t, f.s, 1), []store.Opened{})
	// A full final page returns its key, then the next page ends the walk.
	key, opened, e = f.s.MarkOpened(ctx, store.MemberKey{b.ID, ""}, 1)
	eq(t, key, store.MemberKey{b.ID, b1.Member.Candidate})
	eq(t, must(t, opened, e), []store.Opened{})
	key, opened, e = f.s.MarkOpened(ctx, key, 1)
	eq(t, key, store.MemberKey{})
	eq(t, must(t, opened, e), []store.Opened{})
	// If the deleted row was marked, making it live again would hide this open.
	writeRecords(t, f, "UPDATE campaigns SET deleted=0 WHERE id=?", deleted.ID)
	eq(t, walkOpened(t, f.s, 2), []store.Opened{{deleted.ID, d.Member.Candidate, first}})
}

// R-03JF-NNEG
func TestOpenedInvalidPageDoesNotMark(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	m := f.member(c, 1, "mail")
	rawVisit(t, f, m.Pieces[0].Piece, stamp(f))
	for _, n := range []int{0, -1} {
		key, page, e := f.s.MarkOpened(ctx, store.MemberKey{c.ID, m.Member.Candidate}, n)
		eq(t, key, store.MemberKey{})
		eq(t, page, []store.Opened(nil))
		errIs(t, e, store.ErrUnreachable)
	}
	eq(t, walkOpened(t, f.s, 2), []store.Opened{{c.ID, m.Member.Candidate, stamp(f)}})
}

// R-RR49-S68O R-0774-SYMJ R-0C2Q-C1LB
func TestVisitMarksOnceWithEarliestTime(t *testing.T) {
	for _, state := range []string{store.StatePlanned, store.StateDropped, store.StateDelivered, "rejected"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			c := f.campaign()
			m := f.member(c, 1, "mail", "email")
			p := m.Pieces[0].Piece
			switch state {
			case store.StateDropped:
				_, _, e := f.s.DropPiece(ctx, store.PieceAction{Piece: p.ID, UserID: "owner"})
				if e != nil {
					t.Fatal(e)
				}
			case store.StateDelivered:
				_, _, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: p.ID, UserID: "owner"})
				if e != nil {
					t.Fatal(e)
				}
			case "rejected":
				_, _, e := f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: m.Member.Candidate, Why: "reason", UserID: "owner"})
				if e != nil {
					t.Fatal(e)
				}
			}
			first := stamp(f).Add(-time.Hour)
			rawVisit(t, f, m.Pieces[1].Piece, first)
			markedAt := stamp(f)
			v, e := f.s.Visit(ctx, p.Token, store.VisitInput{"agent", "referrer", "address"})
			eq(t, must(t, v, e), store.Visited{c.ID, m.Member.Candidate, p.ID, c.Destination, true, first})
			f.now = f.now.Add(time.Hour)
			v, e = f.s.Visit(ctx, m.Pieces[1].Piece.Token, store.VisitInput{})
			eq(t, must(t, v, e), store.Visited{c.ID, m.Member.Candidate, m.Pieces[1].Piece.ID, c.Destination, false, first})
			f.reopen()
			eq(t, walkOpened(t, f.s, 1), []store.Opened{})
			v, e = f.s.Visit(ctx, p.Token, store.VisitInput{})
			eq(t, must(t, v, e), store.Visited{c.ID, m.Member.Candidate, p.ID, c.Destination, false, first})
			visits, e := f.s.ListVisits(ctx, p.ID, 0, 10)
			eq(t, len(must(t, visits, e)), 2)
			// Only the first call's exact UTC stamp can clear this member's mark.
			writeRecords(t, f, "UPDATE members SET opened_emitted='' WHERE opened_emitted=?", stamp(f).Format(time.RFC3339))
			eq(t, walkOpened(t, f.s, 2), []store.Opened{})
			writeRecords(t, f, "UPDATE members SET opened_emitted='' WHERE opened_emitted=?", markedAt.Format(time.RFC3339))
			eq(t, walkOpened(t, f.s, 2), []store.Opened{{c.ID, m.Member.Candidate, first}})
		})
	}
}

// R-0774-SYMJ R-0EIJ-3L2P
func TestSweepUsesOneStampAndHonorsAnyMark(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	a := f.member(c, 1, "mail")
	b := f.member(c, 2, "mail")
	fixed := f.member(c, 3, "mail")
	for _, m := range []store.MemberRow{a, b, fixed} {
		rawVisit(t, f, m.Pieces[0].Piece, stamp(f))
	}
	writeRecords(t, f, "UPDATE members SET opened_emitted=? WHERE campaign=? AND candidate=?", "arbitrary non-empty text", c.ID, fixed.Member.Candidate)
	reads := 0
	at := stamp(f).Add(time.Hour)
	rand := &failedReader{}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { reads++; return at.Add(time.Duration(reads-1) * time.Hour) }, Rand: rand})
	want := []store.Opened{{c.ID, a.Member.Candidate, stamp(f)}, {c.ID, b.Member.Candidate, stamp(f)}}
	eq(t, walkOpened(t, f.s, 10), want)
	eq(t, reads, 1)
	eq(t, rand.reads, 0)
	v, e := f.s.Visit(ctx, fixed.Pieces[0].Piece.Token, store.VisitInput{})
	eq(t, must(t, v, e).Opened, false)
	writeRecords(t, f, "UPDATE members SET opened_emitted='' WHERE opened_emitted=?", at.Format(time.RFC3339))
	eq(t, walkOpened(t, f.s, 10), want)
}

// R-08F1-6QD8
func TestConcurrentOpened(t *testing.T) {
	for _, sweep := range []bool{false, true} {
		t.Run(fmt.Sprint(sweep), func(t *testing.T) {
			f := newFixture(t)
			c := f.campaign()
			m := f.member(c, 1, "mail", "email")
			if sweep {
				rawVisit(t, f, m.Pieces[0].Piece, stamp(f).Add(-time.Hour))
			}
			start := make(chan struct{})
			changed := make(chan int, 17)
			errs := make(chan error, 17)
			var wg sync.WaitGroup
			for i := range 16 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					v, e := f.s.Visit(ctx, m.Pieces[i%2].Piece.Token, store.VisitInput{})
					errs <- e
					if v.Opened {
						changed <- 1
					}
				}()
			}
			if sweep {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, v, e := f.s.MarkOpened(ctx, store.MemberKey{}, 2)
					errs <- e
					changed <- len(v)
				}()
			}
			close(start)
			wg.Wait()
			close(changed)
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatal(e)
				}
			}
			total := 0
			for n := range changed {
				total += n
			}
			eq(t, total, 1)
			eq(t, walkOpened(t, f.s, 1), []store.Opened{})
			m, e := f.s.GetMember(ctx, c.ID, m.Member.Candidate)
			expected := 16
			if sweep {
				expected++
			}
			eq(t, must(t, m, e).Engagement.Opens, expected)
		})
	}
}

// R-0KM1-0FS6
func TestOpenedMigrationBytes(t *testing.T) {
	b, e := fs.ReadFile(campaigns.Migrations(), "0003_opened.sql")
	eq(t, string(must(t, b, e)), "ALTER TABLE members ADD COLUMN opened_emitted TEXT NOT NULL DEFAULT '';\n")
}

// R-0N1T-RZ9K R-0PHM-JIQY
func TestOpenedUpgradeAndRollbackRecords(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			f := newFixture(t)
			if e := f.d.Close(); e != nil {
				t.Fatal(e)
			}
			f.path = filepath.Join(t.TempDir(), "previous.db")
			migrations := campaigns.Migrations()
			if upgrade {
				previous := fstest.MapFS{}
				for _, name := range []string{"0001_campaigns.sql", "0002_prospect_lists.sql"} {
					data, e := fs.ReadFile(migrations, name)
					if e != nil {
						t.Fatal(e)
					}
					previous[name] = &fstest.MapFile{Data: data}
				}
				migrations = previous
			}
			d, e := db.Open(ctx, db.Config{Path: f.path, Migrations: migrations, Now: func() time.Time { return f.now }, Service: "campaigns", Stderr: io.Discard})
			if e != nil {
				t.Fatal(e)
			}
			f.d = d
			c := store.Campaign{ID: "camp_0101010101010101", Name: "earlier campaign", Description: "description", Channels: []string{"mail", "email"}, Destination: "https://earlier.invalid/", Created: stamp(f), CreatedUserID: "creator", CreatedUserEmail: "creator@example.invalid"}
			material := store.Material{Campaign: c.ID, Path: "old/material", Label: "label", Artifact: "artifact", Added: stamp(f), AddedUserID: "adder", AddedUserEmail: "adder@example.invalid"}
			rows := []store.MemberRow{}
			visits := map[string][]store.Visit{}
			wantOpened := []store.Opened{}
			e = d.Write(ctx, func(tx *sql.Tx) error {
				for _, deleted := range []bool{false, true} {
					id := c.ID
					if deleted {
						id = "camp_0202020202020202"
					}
					if _, e := tx.ExecContext(ctx, "INSERT INTO campaigns(id,name,description,channels,destination,created,created_user_id,created_user_email,deleted)VALUES(?,?,?,?,?,?,?,?,?)", id, c.Name, c.Description, "mail,email", c.Destination, c.Created.Format(time.RFC3339), c.CreatedUserID, c.CreatedUserEmail, deleted); e != nil {
						return e
					}
					count := 30
					if deleted {
						count = 1
					}
					for i := range count {
						member := store.Member{Campaign: id, Candidate: fmt.Sprintf("cand_%016x", i), ProspectList: "pl_0303030303030303", Added: stamp(f), AddedUserID: "adder", AddedUserEmail: "adder@example.invalid"}
						if i == 5 {
							member.Rejected = true
							member.RejectedUserID = "rejecter"
							member.RejectedUserEmail = "rejecter@example.invalid"
							member.RejectedAt = stamp(f)
							member.Why = "old reason"
						}
						if _, e := tx.ExecContext(ctx, "INSERT INTO members(campaign,candidate,cohort,prospect_list,added,added_user_id,added_user_email,rejected,rejected_user_id,rejected_user_email,rejected_at,why)VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, member.Candidate, "", member.ProspectList, member.Added.Format(time.RFC3339), member.AddedUserID, member.AddedUserEmail, member.Rejected, member.RejectedUserID, member.RejectedUserEmail, legacyTime(member.RejectedAt), member.Why); e != nil {
							return e
						}
						row := store.MemberRow{Member: member, Pieces: []store.PieceRow{}}
						visited := i < 12
						first := stamp(f).Add(-time.Duration(i+1) * time.Hour)
						// Previous Visit encoded a zero injected clock as empty text.
						if i == 0 {
							first = time.Time{}
						}
						if visited && !deleted {
							wantOpened = append(wantOpened, store.Opened{id, member.Candidate, first})
						}
						for j, ch := range c.Channels {
							n := i*2 + j + 1
							if deleted {
								n += 100
							}
							p := store.Piece{ID: fmt.Sprintf("piece_%016x", n), Campaign: id, Candidate: member.Candidate, Channel: ch, State: store.StatePlanned, Token: fmt.Sprintf("old-token-%d", n), Created: stamp(f)}
							if i%2 == 0 {
								p.State = store.StateDelivered
								p.Delivered = stamp(f)
								p.DeliveredUserID = "deliverer"
								p.DeliveredUserEmail = "deliverer@example.invalid"
								p.How = store.HowHand
							}
							if _, e := tx.ExecContext(ctx, "INSERT INTO pieces(id,campaign,candidate,channel,state,token,created,delivered,delivered_user_id,delivered_user_email,how)VALUES(?,?,?,?,?,?,?,?,?,?,?)", p.ID, id, p.Candidate, p.Channel, p.State, p.Token, p.Created.Format(time.RFC3339), legacyTime(p.Delivered), p.DeliveredUserID, p.DeliveredUserEmail, p.How); e != nil {
								return e
							}
							g := store.PieceEngagement{}
							visits[p.ID] = []store.Visit{}
							if visited {
								at := first.Add(time.Duration(j) * time.Hour)
								result, e := tx.ExecContext(ctx, "INSERT INTO visits(piece,at,user_agent,referrer,address)VALUES(?,?,?,?,?)", p.ID, legacyTime(at), "old agent", "old referrer", "old address")
								if e != nil {
									return e
								}
								seq, e := result.LastInsertId()
								if e != nil {
									return e
								}
								visits[p.ID] = []store.Visit{{Seq: seq, Piece: p.ID, At: at, UserAgent: "old agent", Referrer: "old referrer", Address: "old address"}}
								g = store.PieceEngagement{Opens: 1, First: at, Last: at}
							}
							row.Pieces = append(row.Pieces, store.PieceRow{Piece: p, Engagement: g})
						}
						if visited {
							row.Engagement = store.MemberEngagement{Opened: true, Opens: 2, First: first, Last: first.Add(time.Hour)}
						}
						if !deleted {
							rows = append(rows, row)
						}
					}
				}
				_, e := tx.ExecContext(ctx, "INSERT INTO materials(campaign,path,label,artifact,added,added_user_id,added_user_email)VALUES(?,?,?,?,?,?,?)", material.Campaign, material.Path, material.Label, material.Artifact, material.Added.Format(time.RFC3339), material.AddedUserID, material.AddedUserEmail)
				return e
			})
			if e != nil {
				t.Fatal(e)
			}
			f.reopen()
			gotCampaign, e := f.s.GetCampaign(ctx, c.ID)
			eq(t, must(t, gotCampaign, e), c)
			counts, e := f.s.Counts(ctx, c.ID)
			eq(t, must(t, counts, e), store.Counts{Members: 30, Delivered: 30, Opened: 12})
			mats, e := f.s.Materials(ctx, c.ID)
			eq(t, must(t, mats, e), []store.Material{material})
			all, e := f.s.ListMembers(ctx, c.ID, "", "", 100)
			eq(t, must(t, all, e), rows)
			for _, row := range rows {
				got, e := f.s.GetMember(ctx, c.ID, row.Member.Candidate)
				eq(t, must(t, got, e), row)
				for _, p := range row.Pieces {
					got, e := f.s.GetPiece(ctx, p.Piece.ID)
					eq(t, must(t, got, e), p)
					gotVisits, e := f.s.ListVisits(ctx, p.Piece.ID, 0, 100)
					eq(t, must(t, gotVisits, e), visits[p.Piece.ID])
				}
			}
			for j, ch := range c.Channels {
				merge, e := f.s.MergeList(ctx, c.ID, ch, true, "", "", 100)
				want := []store.MergeRow{}
				for _, row := range rows {
					want = append(want, store.MergeRow{Piece: row.Pieces[j].Piece, Rejected: row.Member.Rejected})
				}
				eq(t, must(t, merge, e), want)
			}
			eq(t, walkOpened(t, f.s, 7), wantOpened)
			eq(t, walkOpened(t, f.s, 7), []store.Opened{})
			for i := range 12 {
				v, e := f.s.Visit(ctx, rows[i].Pieces[0].Piece.Token, store.VisitInput{})
				eq(t, must(t, v, e).Opened, false)
				eq(t, v.First, wantOpened[i].First)
			}
			if !upgrade {
				// A rollback writes fresh visits for both a marked member and a new one.
				rawVisit(t, f, rows[0].Pieces[0].Piece, stamp(f))
				member := store.Member{Campaign: c.ID, Candidate: "cand_ffffffffffffffff", ProspectList: "pl_0404040404040404", Added: stamp(f), AddedUserID: "rollback", AddedUserEmail: "rollback@example.invalid"}
				p := store.Piece{ID: "piece_ffffffffffffffff", Campaign: c.ID, Candidate: member.Candidate, Channel: store.ChannelMail, State: store.StatePlanned, Token: "rollback-token", Created: stamp(f)}
				writeRecords(t, f, "INSERT INTO members(campaign,candidate,cohort,prospect_list,added,added_user_id,added_user_email)VALUES(?,?,?,?,?,?,?)", member.Campaign, member.Candidate, "", member.ProspectList, member.Added.Format(time.RFC3339), member.AddedUserID, member.AddedUserEmail)
				writeRecords(t, f, "INSERT INTO pieces(id,campaign,candidate,channel,state,token,created)VALUES(?,?,?,?,?,?,?)", p.ID, p.Campaign, p.Candidate, p.Channel, p.State, p.Token, p.Created.Format(time.RFC3339))
				rawVisit(t, f, p, stamp(f))
				got, e := f.s.GetMember(ctx, c.ID, member.Candidate)
				eq(t, must(t, got, e), store.MemberRow{Member: member, Pieces: []store.PieceRow{{Piece: p, Engagement: store.PieceEngagement{Opens: 1, First: stamp(f), Last: stamp(f)}}}, Engagement: store.MemberEngagement{Opened: true, Opens: 1, First: stamp(f), Last: stamp(f)}})
				eq(t, walkOpened(t, f.s, 7), []store.Opened{{c.ID, member.Candidate, stamp(f)}})
				eq(t, walkOpened(t, f.s, 7), []store.Opened{})
			}
		})
	}
}

// R-RR49-S68O R-RTK2-JPQ2 R-0774-SYMJ
func TestOpenedWithZeroClock(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	a := f.member(c, 1, "mail")
	b := f.member(c, 2, "mail")
	f.now = time.Time{}
	v, e := f.s.Visit(ctx, a.Pieces[0].Piece.Token, store.VisitInput{})
	eq(t, must(t, v, e), store.Visited{c.ID, a.Member.Candidate, a.Pieces[0].Piece.ID, c.Destination, true, time.Time{}})
	v, e = f.s.Visit(ctx, a.Pieces[0].Piece.Token, store.VisitInput{})
	eq(t, must(t, v, e).Opened, false)
	rawVisit(t, f, b.Pieces[0].Piece, time.Time{})
	eq(t, walkOpened(t, f.s, 10), []store.Opened{{c.ID, b.Member.Candidate, time.Time{}}})
	eq(t, walkOpened(t, f.s, 10), []store.Opened{})
	writeRecords(t, f, "UPDATE members SET opened_emitted='' WHERE opened_emitted=?", time.Time{}.Format(time.RFC3339))
	eq(t, walkOpened(t, f.s, 10), []store.Opened{{c.ID, a.Member.Candidate, time.Time{}}, {c.ID, b.Member.Candidate, time.Time{}}})
}
