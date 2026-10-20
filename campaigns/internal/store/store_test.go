package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	campaigns "github.com/ikigenba/ikigenba/campaigns"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
)

var ctx = context.Background()

type random struct{ n byte }

func (r *random) Read(p []byte) (int, error) {
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

type fixture struct {
	t    *testing.T
	d    *db.DB
	s    *store.Store
	now  time.Time
	path string
	rand io.Reader
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, now: time.Date(2030, 2, 3, 4, 5, 6, 987, time.FixedZone("offset", 7200)), path: filepath.Join(t.TempDir(), "campaigns.db"), rand: &random{}}
	f.open()
	t.Cleanup(func() {
		if e := f.d.Close(); e != nil {
			t.Error(e)
		}
	})
	return f
}
func (f *fixture) open() {
	f.t.Helper()
	d, e := db.Open(ctx, db.Config{Path: f.path, Migrations: campaigns.Migrations(), Now: func() time.Time { return f.now }, Service: "campaigns", Stderr: io.Discard})
	if e != nil {
		f.t.Fatal(e)
	}
	f.d = d
	f.s = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rand})
}
func (f *fixture) reopen() {
	f.t.Helper()
	if e := f.d.Close(); e != nil {
		f.t.Fatal(e)
	}
	f.open()
}
func (f *fixture) campaign(ch ...string) store.Campaign {
	f.t.Helper()
	if len(ch) == 0 {
		ch = []string{store.ChannelMail, store.ChannelEmail}
	}
	c, e := f.s.CreateCampaign(ctx, store.Campaign{Name: "Owner campaign", Description: "description", Channels: ch, Destination: "https://destination.invalid/x", CreatedUserID: "owner", CreatedUserEmail: "mgreenly+fake00@gmail.com"})
	if e != nil {
		f.t.Fatal(e)
	}
	return c
}
func (f *fixture) member(c store.Campaign, i int, ch ...string) store.MemberRow {
	f.t.Helper()
	m, b, e := f.s.AddMember(ctx, store.MemberInput{Campaign: c.ID, Candidate: fmt.Sprintf("cand_%016x", i), Channels: ch, UserID: "adder", UserEmail: "mgreenly+fake01@gmail.com"})
	if e != nil || !b {
		f.t.Fatalf("add: %+v %v %v", m, b, e)
	}
	return m
}
func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func errIs(t *testing.T, e, want error) {
	t.Helper()
	if !errors.Is(e, want) {
		t.Fatalf("got error %v; want %v", e, want)
	}
}
func must[T any](t *testing.T, v T, e error) T {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func stamp(f *fixture) time.Time { return f.now.UTC().Truncate(time.Second) }

// R-60VW-LG5X R-623S-Z7WM R-63BP-CZNB R-64JL-QRE0 R-65RI-4J4P R-66ZE-IAVE R-687A-W2M3 R-69F7-9UCS R-6AN3-NM3H R-6BV0-1DU6
func TestPublicRecords(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	m := f.member(c, 1, store.ChannelMail)
	p := m.Pieces[0].Piece
	eq(t, []string{store.CampaignIDPrefix, store.PieceIDPrefix, store.ChannelMail, store.ChannelEmail, store.StatePlanned, store.StateDelivered, store.StateDropped, store.HowHand, store.HowSystem}, []string{"camp_", "piece_", "mail", "email", "planned", "delivered", "dropped", "hand", "system"})
	_ = store.Campaign{c.ID, c.Name, c.Description, c.Channels, c.Destination, c.Created, c.CreatedUserID, c.CreatedUserEmail, c.Deleted}
	_ = store.CampaignEdit{c.ID, nil, nil, nil}
	_ = store.Member{m.Member.Campaign, m.Member.Candidate, "", m.Member.Added, "adder", "mgreenly+fake01@gmail.com", false, "", "", time.Time{}, ""}
	_ = store.MemberInput{c.ID, m.Member.Candidate, "", nil, "owner", "mgreenly+fake00@gmail.com"}
	_ = store.Rejection{c.ID, m.Member.Candidate, "why", "owner", "mgreenly+fake00@gmail.com"}
	_ = store.Piece{p.ID, c.ID, m.Member.Candidate, store.ChannelMail, store.StatePlanned, p.Token, p.Created, time.Time{}, "", "", "", time.Time{}, "", ""}
	_ = store.PieceAction{p.ID, "owner", "mgreenly+fake00@gmail.com"}
	_ = store.ChannelDelivery{c.ID, store.ChannelMail, "owner", "mgreenly+fake00@gmail.com"}
	_ = store.Material{c.ID, "path", "label", "artifact", time.Time{}, "owner", "mgreenly+fake00@gmail.com"}
	_ = store.Visit{1, p.ID, time.Time{}, "agent", "ref", "addr"}
	_ = store.VisitInput{"agent", "ref", "addr"}
	_ = store.Visited{c.ID, p.ID, c.Destination}
	_ = store.PieceEngagement{0, time.Time{}, time.Time{}}
	_ = store.MemberEngagement{false, 0, time.Time{}, time.Time{}}
	_ = store.Counts{1, 0, 0}
	_ = store.CampaignRow{c, store.Counts{1, 0, 0}}
	_ = store.PieceRow{p, store.PieceEngagement{}}
	_ = store.MemberRow{m.Member, m.Pieces, m.Engagement}
	_ = store.MergeRow{p, false}
	for _, e := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrUnreachable} {
		if e == nil {
			t.Fatal("nil sentinel")
		}
	}
	for i, a := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrUnreachable} {
		for j, b := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrUnreachable} {
			eq(t, errors.Is(a, b), i == j)
		}
	}
	if store.Unreachable == "" || strings.ContainsAny(store.Unreachable, "\r\n") {
		t.Fatal("unreachable copy")
	}
}

// R-6EAS-SXBK R-6FIP-6P29 R-6GQL-KGSY
func TestValidation(t *testing.T) {
	for _, v := range []struct {
		f func(string) bool
		p string
	}{{store.ValidCampaignID, "camp_"}, {store.ValidPieceID, "piece_"}, {store.ValidCandidateID, "cand_"}, {store.ValidCohortID, "coh_"}} {
		for _, s := range []string{"0123456789abcdef", "ffffffffffffffff", "0000000000000000"} {
			eq(t, v.f(v.p+s), true)
		}
		for _, s := range []string{"", "a", "0123456789abcdeF", "0123456789abcdef0", "0123456789abcdeg"} {
			eq(t, v.f(v.p+s), false)
		}
		eq(t, v.f("other_0123456789abcdef"), false)
	}
	for _, s := range []string{"abcdefghijklmnopqrstuvwxyz", "22222222222222222222222222", "77777777777777777777777777"} {
		eq(t, store.ValidToken(s), true)
	}
	for _, s := range []string{"", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "11111111111111111111111111", "aaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		eq(t, store.ValidToken(s), false)
	}
	for _, s := range []string{"x", " x ", "\nvalue\t"} {
		eq(t, store.ValidText(s), true)
	}
	for _, s := range []string{"", " \t\n\u2003"} {
		eq(t, store.ValidText(s), false)
	}
	for _, cs := range [][]string{{"mail"}, {"email"}, {"email", "mail"}} {
		eq(t, store.ValidChannels(cs), true)
	}
	for _, cs := range [][]string{nil, {}, {"mail", "mail"}, {"sms"}, {"mail", "EMAIL"}} {
		eq(t, store.ValidChannels(cs), false)
	}
	eq(t, store.ValidChannel("mail"), true)
	eq(t, store.ValidChannel("email"), true)
	eq(t, store.ValidChannel("sms"), false)
	for _, s := range []string{"https://destination.invalid/", "http://destination.invalid/p?q=x"} {
		eq(t, store.ValidDestination(s), true)
	}
	for _, s := range []string{"/x", "https:///x", "ftp://destination.invalid/", "https://%"} {
		eq(t, store.ValidDestination(s), false)
	}
}

// R-BELT-8A79 R-6LM7-3JRQ R-6QHS-MMQI R-6SXL-E67W R-6Z13-B0XD
func TestCampaignPersistence(t *testing.T) {
	f := newFixture(t)
	in := store.Campaign{ID: "ignore", Name: "one", Description: "desc", Channels: []string{"email", "mail"}, Destination: "https://destination.invalid/a", Created: f.now.Add(-time.Hour), CreatedUserID: "owner", CreatedUserEmail: "mgreenly+fake00@gmail.com", Deleted: true}
	c, e := f.s.CreateCampaign(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, c.Channels, []string{"mail", "email"})
	eq(t, c.Created, stamp(f))
	eq(t, c.Name, in.Name)
	eq(t, c.Description, in.Description)
	eq(t, c.Destination, in.Destination)
	eq(t, c.CreatedUserID, in.CreatedUserID)
	eq(t, c.CreatedUserEmail, in.CreatedUserEmail)
	eq(t, c.Deleted, false)
	if !store.ValidCampaignID(c.ID) {
		t.Fatal(c.ID)
	}
	c2, e := f.s.CreateCampaign(ctx, in)
	if e != nil || c2.ID == c.ID {
		t.Fatal(c2, e)
	}
	f.reopen()
	got, e := f.s.GetCampaign(ctx, c.ID)
	eq(t, must(t, got, e), c)
	got, b, e := f.s.EditCampaign(ctx, store.CampaignEdit{ID: c.ID})
	if e != nil || b {
		t.Fatal(got, b, e)
	}
	eq(t, got, c)
	name, desc, dest := "new", "", "http://destination.invalid/b"
	got, b, e = f.s.EditCampaign(ctx, store.CampaignEdit{c.ID, &name, &desc, &dest})
	if e != nil || !b {
		t.Fatal(b, e)
	}
	c.Name = name
	c.Description = desc
	c.Destination = dest
	eq(t, got, c)
	f.reopen()
	got, e = f.s.GetCampaign(ctx, c.ID)
	eq(t, must(t, got, e), c)
	_, b, e = f.s.EditCampaign(ctx, store.CampaignEdit{c.ID, &name, &desc, &dest})
	if e != nil || b {
		t.Fatal(b, e)
	}
	got, e = f.s.GetCampaign(ctx, c2.ID)
	eq(t, must(t, got, e), c2)
}

// R-6RPP-0EH7 R-6U5H-RXYL R-BJHE-RD61 R-6Z13-B0XD
func TestCampaignRefusals(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	bad := []store.Campaign{c, c, c, c}
	bad[0].CreatedUserID = ""
	bad[1].Name = " "
	bad[2].Channels = nil
	bad[3].Destination = "relative"
	for i, v := range bad {
		r, e := f.s.CreateCampaign(ctx, v)
		want := store.ErrInvalid
		if i == 0 {
			want = store.ErrUnreachable
		}
		errIs(t, e, want)
		eq(t, r, store.Campaign{})
	}
	invalid := " "
	dest := "relative"
	for _, in := range []store.CampaignEdit{{ID: c.ID, Name: &invalid}, {ID: c.ID, Destination: &dest}} {
		r, b, e := f.s.EditCampaign(ctx, in)
		errIs(t, e, store.ErrInvalid)
		eq(t, r, store.Campaign{})
		eq(t, b, false)
	}
	for _, id := range []string{"bad", "camp_ffffffffffffffff"} {
		r, b, e := f.s.EditCampaign(ctx, store.CampaignEdit{ID: id})
		errIs(t, e, store.ErrNotFound)
		eq(t, r, store.Campaign{})
		eq(t, b, false)
		r, e = f.s.GetCampaign(ctx, id)
		errIs(t, e, store.ErrNotFound)
		eq(t, r, store.Campaign{})
		co, e := f.s.Counts(ctx, id)
		errIs(t, e, store.ErrNotFound)
		eq(t, co, store.Counts{})
		r, b, e = f.s.DeleteCampaign(ctx, id)
		errIs(t, e, store.ErrNotFound)
		eq(t, r, store.Campaign{})
		eq(t, b, false)
	}
	m := f.member(c, 1, "mail")
	_, _, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: m.Pieces[0].Piece.ID, UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: m.Member.Candidate, Why: "why", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	r, b, e := f.s.DeleteCampaign(ctx, c.ID)
	errIs(t, e, store.ErrInvalid)
	eq(t, r, store.Campaign{})
	eq(t, b, false)
}

// R-6J6E-C0AC R-7A06-QYLM R-911T-F0OD R-72OS-GC5G R-73WO-U3W5 R-7DNV-W9TP R-7HBL-1L1S R-7IJH-FCSH
func TestMemberPieceLife(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	f.now = f.now.Add(time.Minute)
	in := store.MemberInput{Campaign: c.ID, Candidate: "cand_0000000000000001", Cohort: "coh_0000000000000001", Channels: []string{"email", "mail"}, UserID: "adder", UserEmail: "mgreenly+fake01@gmail.com"}
	m, b, e := f.s.AddMember(ctx, in)
	if e != nil || !b {
		t.Fatal(m, b, e)
	}
	eq(t, m.Member, store.Member{Campaign: c.ID, Candidate: in.Candidate, Cohort: in.Cohort, Added: stamp(f), AddedUserID: in.UserID, AddedUserEmail: in.UserEmail})
	eq(t, len(m.Pieces), 2)
	for i, ch := range []string{"mail", "email"} {
		p := m.Pieces[i].Piece
		eq(t, p.Channel, ch)
		eq(t, p.State, store.StatePlanned)
		eq(t, p.Created, m.Member.Added)
		eq(t, p.Delivered, time.Time{})
		eq(t, p.Dropped, time.Time{})
		eq(t, p.How, "")
		eq(t, p.DeliveredUserID, "")
		eq(t, p.DeliveredUserEmail, "")
		eq(t, p.DroppedUserID, "")
		eq(t, p.DroppedUserEmail, "")
		if !store.ValidPieceID(p.ID) || !store.ValidToken(p.Token) {
			t.Fatal(p)
		}
		eq(t, m.Pieces[i].Engagement, store.PieceEngagement{})
	}
	eq(t, m.Engagement, store.MemberEngagement{})
	f.reopen()
	got, e := f.s.GetMember(ctx, c.ID, in.Candidate)
	eq(t, must(t, got, e), m)
	for _, p := range m.Pieces {
		v, e := f.s.GetPiece(ctx, p.Piece.ID)
		eq(t, must(t, v, e), p)
	}
	in.Cohort = "coh_0000000000000002"
	in.Channels = nil
	in.UserEmail = "mgreenly+fake02@gmail.com"
	got, b, e = f.s.AddMember(ctx, in)
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, got, m)
	f.now = f.now.Add(time.Minute)
	p, b, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: m.Pieces[0].Piece.ID, UserID: "deliverer", UserEmail: "mgreenly+fake03@gmail.com"})
	if e != nil || !b {
		t.Fatal(b, e)
	}
	want := m.Pieces[0].Piece
	want.State = store.StateDelivered
	want.Delivered = stamp(f)
	want.DeliveredUserID = "deliverer"
	want.DeliveredUserEmail = "mgreenly+fake03@gmail.com"
	want.How = store.HowHand
	eq(t, p, want)
	p, b, e = f.s.DeliverPiece(ctx, store.PieceAction{Piece: p.ID, UserID: "other"})
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, p, want)
	f.now = f.now.Add(time.Minute)
	rej := store.Rejection{Campaign: c.ID, Candidate: in.Candidate, Why: "first reason", UserID: "rejecter", UserEmail: "mgreenly+fake04@gmail.com"}
	r, b, e := f.s.RejectMember(ctx, rej)
	if e != nil || !b {
		t.Fatal(b, e)
	}
	eq(t, r.Member.Rejected, true)
	eq(t, r.Member.Why, rej.Why)
	eq(t, r.Member.RejectedAt, stamp(f))
	eq(t, r.Member.RejectedUserID, rej.UserID)
	eq(t, r.Member.RejectedUserEmail, rej.UserEmail)
	eq(t, r.Pieces[1].Piece.State, store.StatePlanned)
	rej.Why = "second reason"
	rej.UserID = "other"
	rr, b, e := f.s.RejectMember(ctx, rej)
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, rr, r)
	_, b, e = f.s.DeliverPiece(ctx, store.PieceAction{Piece: m.Pieces[1].Piece.ID, UserID: "owner"})
	errIs(t, e, store.ErrInvalid)
	eq(t, b, false)
	_, b, e = f.s.DropPiece(ctx, store.PieceAction{Piece: want.ID, UserID: "owner"})
	errIs(t, e, store.ErrInvalid)
	eq(t, b, false)
	f.now = f.now.Add(time.Minute)
	drop, b, e := f.s.DropPiece(ctx, store.PieceAction{Piece: m.Pieces[1].Piece.ID, UserID: "dropper", UserEmail: "mgreenly+fake05@gmail.com"})
	if e != nil || !b {
		t.Fatal(b, e)
	}
	dw := m.Pieces[1].Piece
	dw.State = store.StateDropped
	dw.Dropped = stamp(f)
	dw.DroppedUserID = "dropper"
	dw.DroppedUserEmail = "mgreenly+fake05@gmail.com"
	eq(t, drop, dw)
	drop, b, e = f.s.DropPiece(ctx, store.PieceAction{Piece: drop.ID, UserID: "other"})
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, drop, dw)
	_, _, e = f.s.DeliverPiece(ctx, store.PieceAction{Piece: drop.ID, UserID: "owner"})
	errIs(t, e, store.ErrInvalid)
	f.reopen()
	persisted, e := f.s.GetMember(ctx, c.ID, in.Candidate)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, persisted.Member, r.Member)
	eq(t, persisted.Pieces[0].Piece, want)
	eq(t, persisted.Pieces[1].Piece, dw)
	empty := f.member(c, 2)
	if empty.Pieces == nil || len(empty.Pieces) != 0 {
		t.Fatal(empty)
	}
	single := f.campaign("mail")
	one := f.member(single, 3)
	add := store.MemberInput{Campaign: single.ID, Candidate: one.Member.Candidate, Channels: []string{"email"}, UserID: "owner"}
	_, b, e = f.s.AddMember(ctx, add)
	if e != nil || b {
		t.Fatal(b, e)
	}
	for _, call := range []func() error{func() error { _, e := f.s.GetMember(ctx, c.ID, "cand_ffffffffffffffff"); return e }, func() error { _, e := f.s.GetMember(ctx, "bad", in.Candidate); return e }, func() error { _, e := f.s.GetPiece(ctx, "bad"); return e }} {
		errIs(t, call(), store.ErrNotFound)
	}
}

// R-7CFZ-II30 R-7EVS-A1KE R-7JRD-T4J6 R-7M76-KO0K R-7OMZ-C7HY
func TestMemberRefusals(t *testing.T) {
	f := newFixture(t)
	c := f.campaign("mail")
	valid := store.MemberInput{Campaign: c.ID, Candidate: "cand_0000000000000001", Channels: []string{"mail"}, UserID: "owner"}
	cases := []struct {
		in   store.MemberInput
		want error
	}{}
	for _, kind := range []string{"user", "candidate", "cohort", "duplicate", "channel", "absent", "unused"} {
		v := valid
		want := store.ErrInvalid
		switch kind {
		case "user":
			v.UserID = ""
			want = store.ErrUnreachable
		case "candidate":
			v.Candidate = "bad"
		case "cohort":
			v.Cohort = "bad"
		case "duplicate":
			v.Channels = []string{"mail", "mail"}
		case "channel":
			v.Channels = []string{"sms"}
		case "absent":
			v.Campaign = "bad"
			want = store.ErrNotFound
		case "unused":
			v.Channels = []string{"email"}
		}
		cases = append(cases, struct {
			in   store.MemberInput
			want error
		}{v, want})
	}
	for _, tc := range cases {
		r, b, e := f.s.AddMember(ctx, tc.in)
		errIs(t, e, tc.want)
		eq(t, r, store.MemberRow{})
		eq(t, b, false)
	}
	m := f.member(c, 1, "mail")
	for _, tc := range []struct {
		in   store.Rejection
		want error
	}{{store.Rejection{Campaign: c.ID, Candidate: m.Member.Candidate, Why: "why"}, store.ErrUnreachable}, {store.Rejection{Campaign: c.ID, Candidate: m.Member.Candidate, Why: " ", UserID: "owner"}, store.ErrInvalid}, {store.Rejection{Campaign: c.ID, Candidate: "bad", Why: "why", UserID: "owner"}, store.ErrNotFound}, {store.Rejection{Campaign: "bad", Candidate: m.Member.Candidate, Why: "why", UserID: "owner"}, store.ErrNotFound}} {
		r, b, e := f.s.RejectMember(ctx, tc.in)
		errIs(t, e, tc.want)
		eq(t, r, store.MemberRow{})
		eq(t, b, false)
	}
	for _, action := range []func(context.Context, store.PieceAction) (store.Piece, bool, error){f.s.DropPiece, f.s.DeliverPiece} {
		for _, tc := range []struct {
			in   store.PieceAction
			want error
		}{{store.PieceAction{Piece: m.Pieces[0].Piece.ID}, store.ErrUnreachable}, {store.PieceAction{Piece: "bad", UserID: "owner"}, store.ErrNotFound}} {
			r, b, e := action(ctx, tc.in)
			errIs(t, e, tc.want)
			eq(t, r, store.Piece{})
			eq(t, b, false)
		}
	}
	for _, tc := range []struct {
		in   store.ChannelDelivery
		want error
	}{{store.ChannelDelivery{Campaign: c.ID, Channel: "mail"}, store.ErrUnreachable}, {store.ChannelDelivery{Campaign: c.ID, Channel: "sms", UserID: "owner"}, store.ErrInvalid}, {store.ChannelDelivery{Campaign: "bad", Channel: "mail", UserID: "owner"}, store.ErrNotFound}, {store.ChannelDelivery{Campaign: c.ID, Channel: "email", UserID: "owner"}, store.ErrInvalid}} {
		r, e := f.s.DeliverChannel(ctx, tc.in)
		errIs(t, e, tc.want)
		eq(t, r, []store.Piece(nil))
	}
	for _, ch := range []string{"sms", "email"} {
		r, e := f.s.MergeList(ctx, c.ID, ch, false, "", "", 5)
		errIs(t, e, store.ErrInvalid)
		eq(t, r, []store.MergeRow(nil))
	}
	r, e := f.s.GetMember(ctx, c.ID, m.Member.Candidate)
	eq(t, must(t, r, e), m)
}

// R-BLX7-IWNF R-7NF2-YFR9
func TestBatchAndMerge(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	ms := []store.MemberRow{}
	for i := 5; i > 0; i-- {
		ms = append(ms, f.member(c, i, "email", "mail"))
	}
	_, _, e := f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: ms[0].Member.Candidate, Why: "why", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = f.s.DropPiece(ctx, store.PieceAction{Piece: ms[1].Pieces[0].Piece.ID, UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = f.s.DeliverPiece(ctx, store.PieceAction{Piece: ms[2].Pieces[0].Piece.ID, UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	planned, e := f.s.MergeList(ctx, c.ID, "mail", false, "", "", 10)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(planned), 2)
	eq(t, planned[0].Piece.Candidate, ms[4].Member.Candidate)
	eq(t, planned[1].Piece.Candidate, ms[3].Member.Candidate)
	all, e := f.s.MergeList(ctx, c.ID, "mail", true, "", "", 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(all), 2)
	next, e := f.s.MergeList(ctx, c.ID, "mail", true, all[1].Piece.Created.Format(time.RFC3339), all[1].Piece.Candidate, 10)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(next), 3)
	eq(t, next[2].Rejected, true)
	eq(t, next[0].Piece.State, store.StateDelivered)
	eq(t, next[1].Piece.State, store.StateDropped)
	f.now = f.now.Add(time.Hour)
	del, e := f.s.DeliverChannel(ctx, store.ChannelDelivery{Campaign: c.ID, Channel: "mail", UserID: "batch", UserEmail: "mgreenly+fake06@gmail.com"})
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(del), 2)
	for i, p := range del {
		w := planned[i].Piece
		w.State = store.StateDelivered
		w.Delivered = stamp(f)
		w.DeliveredUserID = "batch"
		w.DeliveredUserEmail = "mgreenly+fake06@gmail.com"
		w.How = store.HowHand
		eq(t, p, w)
	}
	again, e := f.s.DeliverChannel(ctx, store.ChannelDelivery{Campaign: c.ID, Channel: "mail", UserID: "batch"})
	if e != nil || again == nil || len(again) != 0 {
		t.Fatal(again, e)
	}
	for _, m := range ms {
		p, e := f.s.GetPiece(ctx, m.Pieces[1].Piece.ID)
		if e != nil {
			t.Fatal(e)
		}
		eq(t, p.Piece, m.Pieces[1].Piece)
	}
}

// R-6KEA-PS11 R-7PUV-PZ8N R-7R2S-3QZC R-7SAO-HIQ1
func TestMaterials(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	initial, e := f.s.Materials(ctx, c.ID)
	eq(t, must(t, initial, e), []store.Material{})
	ms := []store.Material{}
	for i := range 3 {
		f.now = f.now.Add(time.Minute)
		in := store.Material{Campaign: c.ID, Path: fmt.Sprintf("folder/%d", i), Label: fmt.Sprintf("label %d", i), Artifact: fmt.Sprintf("artifact%d", i), AddedUserID: "owner", AddedUserEmail: "mgreenly+fake00@gmail.com"}
		v, b, e := f.s.AddMaterial(ctx, in)
		if e != nil || !b {
			t.Fatal(v, b, e)
		}
		in.Added = stamp(f)
		eq(t, v, in)
		ms = append(ms, v)
	}
	f.reopen()
	got, e := f.s.Materials(ctx, c.ID)
	eq(t, must(t, got, e), ms)
	in := ms[0]
	in.AddedUserID = "other"
	in.AddedUserEmail = "mgreenly+fake01@gmail.com"
	in.Added = time.Time{}
	v, b, e := f.s.AddMaterial(ctx, in)
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, v, ms[0])
	in.Label = "replacement"
	in.Artifact = "new artifact"
	v, b, e = f.s.AddMaterial(ctx, in)
	if e != nil || !b {
		t.Fatal(b, e)
	}
	ms[0].Label = in.Label
	ms[0].Artifact = in.Artifact
	eq(t, v, ms[0])
	got, e = f.s.Materials(ctx, c.ID)
	eq(t, must(t, got, e), ms)
	v, b, e = f.s.RemoveMaterial(ctx, c.ID, ms[0].Path)
	if e != nil || !b {
		t.Fatal(b, e)
	}
	eq(t, v, ms[0])
	v, b, e = f.s.RemoveMaterial(ctx, c.ID, ms[0].Path)
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, v, store.Material{})
	v, b, e = f.s.AddMaterial(ctx, in)
	if e != nil || !b {
		t.Fatal(b, e)
	}
	got, e = f.s.Materials(ctx, c.ID)
	eq(t, must(t, got, e), []store.Material{ms[1], ms[2], v})
	f.reopen()
	got, e = f.s.Materials(ctx, c.ID)
	eq(t, must(t, got, e), []store.Material{ms[1], ms[2], v})
	for _, kind := range []string{"user", "artifact", "path", "label", "campaign"} {
		bad := in
		want := store.ErrInvalid
		switch kind {
		case "user":
			bad.AddedUserID = ""
			want = store.ErrUnreachable
		case "artifact":
			bad.Artifact = ""
			want = store.ErrUnreachable
		case "path":
			bad.Path = ""
		case "label":
			bad.Label = " "
		case "campaign":
			bad.Campaign = "bad"
			want = store.ErrNotFound
		}
		v, b, e = f.s.AddMaterial(ctx, bad)
		errIs(t, e, want)
		eq(t, v, store.Material{})
		eq(t, b, false)
	}
	_, e = f.s.Materials(ctx, "bad")
	errIs(t, e, store.ErrNotFound)
	v, b, e = f.s.RemoveMaterial(ctx, "bad", "x")
	errIs(t, e, store.ErrNotFound)
	eq(t, v, store.Material{})
	eq(t, b, false)
}

// R-708Z-OSO2 R-71GW-2KER R-7TIK-VAGQ R-7UQH-927F R-7VYD-MTY4
func TestVisitsEngagement(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	a := f.member(c, 1, "mail", "email")
	b := f.member(c, 2, "mail")
	_ = f.member(c, 3)
	for _, p := range []store.Piece{a.Pieces[0].Piece, b.Pieces[0].Piece} {
		_, _, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: p.ID, UserID: "owner"})
		if e != nil {
			t.Fatal(e)
		}
	}
	_, _, e := f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: a.Member.Candidate, Why: "why", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = f.s.DropPiece(ctx, store.PieceAction{Piece: a.Pieces[1].Piece.ID, UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	in := store.VisitInput{UserAgent: "whole user agent", Referrer: "https://referrer.invalid/path", Address: "198.51.100.3"}
	times := []time.Time{}
	for i := range 5 {
		f.now = f.now.Add(-time.Hour)
		times = append(times, stamp(f))
		p := a.Pieces[i%2].Piece
		v, e := f.s.Visit(ctx, p.Token, in)
		eq(t, must(t, v, e), store.Visited{Campaign: c.ID, Piece: p.ID, Destination: c.Destination})
	}
	co, e := f.s.Counts(ctx, c.ID)
	eq(t, must(t, co, e), store.Counts{Members: 3, Delivered: 2, Opened: 1})
	m, e := f.s.GetMember(ctx, c.ID, a.Member.Candidate)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, m.Engagement, store.MemberEngagement{Opened: true, Opens: 5, First: times[4], Last: times[0]})
	eq(t, m.Pieces[0].Engagement, store.PieceEngagement{Opens: 3, First: times[4], Last: times[0]})
	eq(t, m.Pieces[1].Engagement, store.PieceEngagement{Opens: 2, First: times[3], Last: times[1]})
	p, e := f.s.GetPiece(ctx, a.Pieces[0].Piece.ID)
	eq(t, must(t, p, e), m.Pieces[0])
	unopened, e := f.s.GetMember(ctx, c.ID, b.Member.Candidate)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, unopened.Engagement, store.MemberEngagement{})
	visits, e := f.s.ListVisits(ctx, a.Pieces[0].Piece.ID, 0, 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(visits), 2)
	eq(t, visits[0].At, times[4])
	eq(t, visits[1].At, times[2])
	for _, v := range visits {
		eq(t, v.UserAgent, in.UserAgent)
		eq(t, v.Referrer, in.Referrer)
		eq(t, v.Address, in.Address)
		eq(t, v.Piece, a.Pieces[0].Piece.ID)
	}
	if visits[0].Seq <= visits[1].Seq {
		t.Fatal(visits)
	}
	last, e := f.s.ListVisits(ctx, a.Pieces[0].Piece.ID, visits[1].Seq, 10)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(last), 1)
	eq(t, last[0].At, times[0])
	empty, e := f.s.ListVisits(ctx, a.Pieces[0].Piece.ID, last[0].Seq, 10)
	eq(t, must(t, empty, e), []store.Visit{})
	none, e := f.s.ListVisits(ctx, b.Pieces[0].Piece.ID, -1, 10)
	eq(t, must(t, none, e), []store.Visit{})
	_, e = f.s.ListVisits(ctx, "bad", 0, 10)
	errIs(t, e, store.ErrNotFound)
	for _, token := range []string{"unknown", strings.ToUpper(a.Pieces[0].Piece.Token)} {
		v, e := f.s.Visit(ctx, token, in)
		errIs(t, e, store.ErrNotFound)
		eq(t, v, store.Visited{})
	}
	dest := "http://destination.invalid/new"
	_, _, e = f.s.EditCampaign(ctx, store.CampaignEdit{ID: c.ID, Destination: &dest})
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Visit(ctx, a.Pieces[0].Piece.Token, in)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, v.Destination, dest)
	f.reopen()
	after, e := f.s.ListVisits(ctx, a.Pieces[0].Piece.ID, 0, 10)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(after), 4)
	eq(t, after[1:], append(visits, last...))
}

// R-BI9I-DLFC
func TestSoftDelete(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	m := f.member(c, 1, "mail")
	_, e := f.s.Visit(ctx, m.Pieces[0].Piece.Token, store.VisitInput{})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = f.s.AddMaterial(ctx, store.Material{Campaign: c.ID, Path: "path", Label: "label", Artifact: "artifact", AddedUserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	c.Deleted = true
	got, b, e := f.s.DeleteCampaign(ctx, c.ID)
	if e != nil || !b {
		t.Fatal(b, e)
	}
	eq(t, got, c)
	f.reopen()
	got, b, e = f.s.DeleteCampaign(ctx, c.ID)
	if e != nil || b {
		t.Fatal(b, e)
	}
	eq(t, got, c)
	for _, call := range calls(f, c, m) {
		if strings.HasPrefix(call.name, "campaigns") {
			if e := call.fn(ctx); e != nil {
				t.Fatal(e)
			}
		} else {
			errIs(t, call.fn(ctx), store.ErrNotFound)
		}
	}
	for _, rev := range []bool{false, true} {
		var rs []store.CampaignRow
		if rev {
			rs, e = f.s.ListCampaignsReversed(ctx, "", "", 10)
		} else {
			rs, e = f.s.ListCampaigns(ctx, "", "", 10)
		}
		eq(t, must(t, rs, e), []store.CampaignRow{})
	}
}

type call struct {
	name string
	fn   func(context.Context) error
}

func calls(f *fixture, c store.Campaign, m store.MemberRow) []call {
	p := m.Pieces[0].Piece
	return []call{
		{"edit", func(ctx context.Context) error {
			v, b, e := f.s.EditCampaign(ctx, store.CampaignEdit{ID: c.ID})
			if e != nil {
				eq(f.t, v, store.Campaign{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"get", func(ctx context.Context) error {
			v, e := f.s.GetCampaign(ctx, c.ID)
			if e != nil {
				eq(f.t, v, store.Campaign{})
			}
			return e
		}},
		{"counts", func(ctx context.Context) error {
			v, e := f.s.Counts(ctx, c.ID)
			if e != nil {
				eq(f.t, v, store.Counts{})
			}
			return e
		}},
		{"campaigns", func(ctx context.Context) error {
			v, e := f.s.ListCampaigns(ctx, "", "", 2)
			if e != nil {
				eq(f.t, v, []store.CampaignRow(nil))
			}
			return e
		}},
		{"campaigns reversed", func(ctx context.Context) error {
			v, e := f.s.ListCampaignsReversed(ctx, "", "", 2)
			if e != nil {
				eq(f.t, v, []store.CampaignRow(nil))
			}
			return e
		}},
		{"add member", func(ctx context.Context) error {
			v, b, e := f.s.AddMember(ctx, store.MemberInput{Campaign: c.ID, Candidate: m.Member.Candidate, UserID: "owner"})
			if e != nil {
				eq(f.t, v, store.MemberRow{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"get member", func(ctx context.Context) error {
			v, e := f.s.GetMember(ctx, c.ID, m.Member.Candidate)
			if e != nil {
				eq(f.t, v, store.MemberRow{})
			}
			return e
		}},
		{"members", func(ctx context.Context) error {
			v, e := f.s.ListMembers(ctx, c.ID, "", "", 2)
			if e != nil {
				eq(f.t, v, []store.MemberRow(nil))
			}
			return e
		}},
		{"members reversed", func(ctx context.Context) error {
			v, e := f.s.ListMembersReversed(ctx, c.ID, "", "", 2)
			if e != nil {
				eq(f.t, v, []store.MemberRow(nil))
			}
			return e
		}},
		{"reject", func(ctx context.Context) error {
			v, b, e := f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: m.Member.Candidate, Why: "why", UserID: "owner"})
			if e != nil {
				eq(f.t, v, store.MemberRow{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"piece", func(ctx context.Context) error {
			v, e := f.s.GetPiece(ctx, p.ID)
			if e != nil {
				eq(f.t, v, store.PieceRow{})
			}
			return e
		}},
		{"drop", func(ctx context.Context) error {
			v, b, e := f.s.DropPiece(ctx, store.PieceAction{Piece: p.ID, UserID: "owner"})
			if e != nil {
				eq(f.t, v, store.Piece{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"deliver", func(ctx context.Context) error {
			v, b, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: p.ID, UserID: "owner"})
			if e != nil {
				eq(f.t, v, store.Piece{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"batch", func(ctx context.Context) error {
			v, e := f.s.DeliverChannel(ctx, store.ChannelDelivery{Campaign: c.ID, Channel: "mail", UserID: "owner"})
			if e != nil {
				eq(f.t, v, []store.Piece(nil))
			}
			return e
		}},
		{"merge", func(ctx context.Context) error {
			v, e := f.s.MergeList(ctx, c.ID, "mail", true, "", "", 2)
			if e != nil {
				eq(f.t, v, []store.MergeRow(nil))
			}
			return e
		}},
		{"material", func(ctx context.Context) error {
			v, b, e := f.s.AddMaterial(ctx, store.Material{Campaign: c.ID, Path: "path", Label: "label", Artifact: "artifact", AddedUserID: "owner"})
			if e != nil {
				eq(f.t, v, store.Material{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"remove", func(ctx context.Context) error {
			v, b, e := f.s.RemoveMaterial(ctx, c.ID, "path")
			if e != nil {
				eq(f.t, v, store.Material{})
				eq(f.t, b, false)
			}
			return e
		}},
		{"materials", func(ctx context.Context) error {
			v, e := f.s.Materials(ctx, c.ID)
			if e != nil {
				eq(f.t, v, []store.Material(nil))
			}
			return e
		}},
		{"visit", func(ctx context.Context) error {
			v, e := f.s.Visit(ctx, p.Token, store.VisitInput{})
			if e != nil {
				eq(f.t, v, store.Visited{})
			}
			return e
		}},
		{"visits", func(ctx context.Context) error {
			v, e := f.s.ListVisits(ctx, p.ID, 0, 2)
			if e != nil {
				eq(f.t, v, []store.Visit(nil))
			}
			return e
		}},
	}
}

// R-754L-7VMU R-76CH-LNDJ R-77KD-ZF48 R-78SA-D6UX R-84HO-B84Z
func TestWindows(t *testing.T) {
	f := newFixture(t)
	rs, e := f.s.ListCampaigns(ctx, "", "", 10)
	eq(t, must(t, rs, e), []store.CampaignRow{})
	rs, e = f.s.ListCampaignsReversed(ctx, "", "", 10)
	eq(t, must(t, rs, e), []store.CampaignRow{})
	dummy := store.Campaign{ID: "camp_ffffffffffffffff"}
	dm := store.MemberRow{Member: store.Member{Candidate: "cand_ffffffffffffffff"}, Pieces: []store.PieceRow{{Piece: store.Piece{ID: "piece_ffffffffffffffff", Token: strings.Repeat("a", 26)}}}}
	for _, c := range calls(f, dummy, dm) {
		if strings.HasPrefix(c.name, "campaigns") {
			continue
		}
		errIs(t, c.fn(ctx), store.ErrNotFound)
	}
	cs := []store.Campaign{}
	for i := range 6 {
		if i == 1 || i == 4 {
			f.now = f.now.Add(time.Minute)
		}
		cs = append(cs, f.campaign())
	}
	_, _, e = f.s.DeleteCampaign(ctx, cs[2].ID)
	if e != nil {
		t.Fatal(e)
	}
	want := []store.Campaign{cs[5], cs[4], cs[3], cs[1], cs[0]}
	got := []store.Campaign{}
	created, id := "ignored", ""
	for {
		page, e := f.s.ListCampaigns(ctx, created, id, 2)
		if e != nil {
			t.Fatal(e)
		}
		if len(page) == 0 {
			break
		}
		for _, v := range page {
			got = append(got, v.Campaign)
			co, e := f.s.Counts(ctx, v.Campaign.ID)
			eq(t, v.Counts, must(t, co, e))
		}
		last := page[len(page)-1].Campaign
		created, id = last.Created.Format(time.RFC3339), last.ID
	}
	eq(t, got, want)
	rev, e := f.s.ListCampaignsReversed(ctx, "", "", 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, []store.Campaign{rev[0].Campaign, rev[1].Campaign}, []store.Campaign{cs[0], cs[1]})
	prev, e := f.s.ListCampaignsReversed(ctx, rev[1].Campaign.Created.Format(time.RFC3339), rev[1].Campaign.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, []store.Campaign{prev[0].Campaign, prev[1].Campaign}, []store.Campaign{cs[3], cs[4]})
	empty, e := f.s.ListCampaignsReversed(ctx, cs[5].Created.Format(time.RFC3339), cs[5].ID, 2)
	eq(t, must(t, empty, e), []store.CampaignRow{})
	c := cs[0]
	ms := []store.MemberRow{}
	for _, i := range []int{3, 1, 2} {
		ms = append(ms, f.member(c, i, "mail"))
	}
	f.now = f.now.Add(time.Minute)
	ms = append(ms, f.member(c, 4, "mail"))
	_, _, e = f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: ms[2].Member.Candidate, Why: "why", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	members, e := f.s.ListMembers(ctx, c.ID, "ignored", "", 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, []string{members[0].Member.Candidate, members[1].Member.Candidate}, []string{ms[1].Member.Candidate, ms[2].Member.Candidate})
	eq(t, members[1].Member.Rejected, true)
	next, e := f.s.ListMembers(ctx, c.ID, members[1].Member.Added.Format(time.RFC3339), members[1].Member.Candidate, 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, []string{next[0].Member.Candidate, next[1].Member.Candidate}, []string{ms[0].Member.Candidate, ms[3].Member.Candidate})
	back, e := f.s.ListMembersReversed(ctx, c.ID, "", "", 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, back[0], next[1])
	eq(t, back[1], next[0])
	before, e := f.s.ListMembersReversed(ctx, c.ID, back[1].Member.Added.Format(time.RFC3339), back[1].Member.Candidate, 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, before[0], members[1])
	eq(t, before[1], members[0])
	for _, fn := range []func() error{func() error { _, e := f.s.ListCampaigns(ctx, "", "", 0); return e }, func() error { _, e := f.s.ListCampaignsReversed(ctx, "", "", 0); return e }, func() error { _, e := f.s.ListMembers(ctx, c.ID, "", "", 0); return e }, func() error { _, e := f.s.ListMembersReversed(ctx, c.ID, "", "", 0); return e }, func() error { _, e := f.s.ListVisits(ctx, ms[0].Pieces[0].Piece.ID, 0, 0); return e }, func() error { _, e := f.s.MergeList(ctx, c.ID, "mail", false, "", "", 0); return e }} {
		errIs(t, fn(), store.ErrUnreachable)
	}
}

// R-7X6A-0LOT R-7YE6-EDFI R-7ZM2-S567 R-821V-JONL
func TestWindowStatements(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	_ = f.campaign()
	a := f.member(c, 1, "mail")
	_ = f.member(c, 2, "mail")
	for range 3 {
		_, e := f.s.Visit(ctx, a.Pieces[0].Piece.Token, store.VisitInput{})
		if e != nil {
			t.Fatal(e)
		}
	}
	created := c.Created.Format(time.RFC3339)
	added := a.Member.Added.Format(time.RFC3339)
	p := a.Pieces[0].Piece
	type statement struct {
		q        string
		args     []any
		index    string
		expected func() []string
	}
	campaignIDs := func(v []store.CampaignRow, e error) []string {
		if e != nil {
			t.Fatal(e)
		}
		out := []string{}
		for _, r := range v {
			out = append(out, r.Campaign.ID)
		}
		return out
	}
	memberIDs := func(v []store.MemberRow, e error) []string {
		if e != nil {
			t.Fatal(e)
		}
		out := []string{}
		for _, r := range v {
			out = append(out, r.Member.Candidate)
		}
		return out
	}
	visitIDs := func(v []store.Visit, e error) []string {
		if e != nil {
			t.Fatal(e)
		}
		out := []string{}
		for _, r := range v {
			out = append(out, fmt.Sprint(r.Seq))
		}
		return out
	}
	mergeIDs := func(v []store.MergeRow, e error) []string {
		if e != nil {
			t.Fatal(e)
		}
		out := []string{}
		for _, r := range v {
			out = append(out, r.Piece.ID)
		}
		return out
	}
	ss := []statement{
		{store.CampaignsFirst, []any{10}, " INDEX campaigns_window (deleted=?)", func() []string { return campaignIDs(f.s.ListCampaigns(ctx, "", "", 10)) }},
		{store.CampaignsAfter, []any{created, c.ID, 10}, " INDEX campaigns_window (deleted=? AND (created,id)<(?,?))", func() []string { return campaignIDs(f.s.ListCampaigns(ctx, created, c.ID, 10)) }},
		{store.CampaignsLast, []any{10}, " INDEX campaigns_window (deleted=?)", func() []string { return campaignIDs(f.s.ListCampaignsReversed(ctx, "", "", 10)) }},
		{store.CampaignsPreceding, []any{created, c.ID, 10}, " INDEX campaigns_window (deleted=? AND (created,id)>(?,?))", func() []string { return campaignIDs(f.s.ListCampaignsReversed(ctx, created, c.ID, 10)) }},
		{store.MembersFirst, []any{c.ID, 10}, " INDEX members_window (campaign=?)", func() []string { return memberIDs(f.s.ListMembers(ctx, c.ID, "", "", 10)) }},
		{store.MembersAfter, []any{c.ID, added, a.Member.Candidate, 10}, " INDEX members_window (campaign=? AND (added,candidate)>(?,?))", func() []string { return memberIDs(f.s.ListMembers(ctx, c.ID, added, a.Member.Candidate, 10)) }},
		{store.MembersLast, []any{c.ID, 10}, " INDEX members_window (campaign=?)", func() []string { return memberIDs(f.s.ListMembersReversed(ctx, c.ID, "", "", 10)) }},
		{store.MembersPreceding, []any{c.ID, added, a.Member.Candidate, 10}, " INDEX members_window (campaign=? AND (added,candidate)<(?,?))", func() []string { return memberIDs(f.s.ListMembersReversed(ctx, c.ID, added, a.Member.Candidate, 10)) }},
		{store.VisitsFirst, []any{p.ID, 10}, " INDEX visits_window (piece=?)", func() []string { return visitIDs(f.s.ListVisits(ctx, p.ID, 0, 10)) }},
		{store.VisitsAfter, []any{p.ID, int64(3), 10}, " INDEX visits_window (piece=? AND seq<?)", func() []string { return visitIDs(f.s.ListVisits(ctx, p.ID, 3, 10)) }},
		{store.MergeFirst, []any{c.ID, "mail", 0, 10}, " INDEX pieces_merge (campaign=? AND channel=?)", func() []string { return mergeIDs(f.s.MergeList(ctx, c.ID, "mail", false, "", "", 10)) }},
		{store.MergeAfter, []any{c.ID, "mail", 1, created, a.Member.Candidate, 10}, " INDEX pieces_merge (campaign=? AND channel=? AND (created,candidate)>(?,?))", func() []string {
			return mergeIDs(f.s.MergeList(ctx, c.ID, "mail", true, created, a.Member.Candidate, 10))
		}},
	}
	for _, st := range ss {
		expected := st.expected()
		e := f.d.Read(ctx, func(tx *sql.Tx) error {
			rs, e := tx.QueryContext(ctx, st.q, st.args...)
			if e != nil {
				return e
			}
			out := []string{}
			for rs.Next() {
				var v any
				if e = rs.Scan(&v); e != nil {
					_ = rs.Close()
					return e
				}
				out = append(out, fmt.Sprint(v))
			}
			e = rs.Err()
			_ = rs.Close()
			if e != nil {
				return e
			}
			eq(t, out, expected)
			rs, e = tx.QueryContext(ctx, "EXPLAIN QUERY PLAN "+st.q, st.args...)
			if e != nil {
				return e
			}
			defer func() { _ = rs.Close() }()
			first := true
			for rs.Next() {
				var id, parent, unused int
				var detail string
				if e = rs.Scan(&id, &parent, &unused, &detail); e != nil {
					return e
				}
				if strings.HasPrefix(detail, "SCAN ") || strings.Contains(detail, "USE TEMP B-TREE") {
					t.Fatalf("%s plan: %s", st.q, detail)
				}
				if first {
					if !strings.HasPrefix(detail, "SEARCH ") || !strings.HasSuffix(detail, st.index) {
						t.Fatalf("%s plan: %s; suffix %s", st.q, detail, st.index)
					}
					first = false
				}
			}
			if first {
				t.Fatal("empty plan")
			}
			return rs.Err()
		})
		if e != nil {
			t.Fatal(e)
		}
	}
}

// R-839R-XGEA
func TestMigration(t *testing.T) {
	entries, e := fs.ReadDir(campaigns.Migrations(), ".")
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(entries), 1)
	eq(t, entries[0].Name(), "0001_campaigns.sql")
	b, e := fs.ReadFile(campaigns.Migrations(), entries[0].Name())
	if e != nil {
		t.Fatal(e)
	}
	eq(t, string(b), `CREATE TABLE campaigns (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL, channels TEXT NOT NULL, destination TEXT NOT NULL, created TEXT NOT NULL, created_user_id TEXT NOT NULL, created_user_email TEXT NOT NULL, deleted INTEGER NOT NULL DEFAULT 0);
CREATE INDEX campaigns_window ON campaigns(deleted,created,id);
CREATE TABLE members (campaign TEXT NOT NULL, candidate TEXT NOT NULL, cohort TEXT NOT NULL, added TEXT NOT NULL, added_user_id TEXT NOT NULL, added_user_email TEXT NOT NULL, rejected INTEGER NOT NULL DEFAULT 0, rejected_user_id TEXT NOT NULL DEFAULT '', rejected_user_email TEXT NOT NULL DEFAULT '', rejected_at TEXT NOT NULL DEFAULT '', why TEXT NOT NULL DEFAULT '', PRIMARY KEY (campaign,candidate)) WITHOUT ROWID;
CREATE INDEX members_window ON members(campaign,added,candidate);
CREATE TABLE pieces (id TEXT PRIMARY KEY, campaign TEXT NOT NULL, candidate TEXT NOT NULL, channel TEXT NOT NULL, state TEXT NOT NULL, token TEXT NOT NULL, created TEXT NOT NULL, delivered TEXT NOT NULL DEFAULT '', delivered_user_id TEXT NOT NULL DEFAULT '', delivered_user_email TEXT NOT NULL DEFAULT '', how TEXT NOT NULL DEFAULT '', dropped TEXT NOT NULL DEFAULT '', dropped_user_id TEXT NOT NULL DEFAULT '', dropped_user_email TEXT NOT NULL DEFAULT '');
CREATE UNIQUE INDEX pieces_token ON pieces(token);
CREATE UNIQUE INDEX pieces_member ON pieces(campaign,candidate,channel);
CREATE INDEX pieces_merge ON pieces(campaign,channel,created,candidate);
CREATE TABLE materials (seq INTEGER PRIMARY KEY, campaign TEXT NOT NULL, path TEXT NOT NULL, label TEXT NOT NULL, artifact TEXT NOT NULL, added TEXT NOT NULL, added_user_id TEXT NOT NULL, added_user_email TEXT NOT NULL);
CREATE UNIQUE INDEX materials_path ON materials(campaign,path);
CREATE TABLE visits (seq INTEGER PRIMARY KEY, piece TEXT NOT NULL, at TEXT NOT NULL, user_agent TEXT NOT NULL, referrer TEXT NOT NULL, address TEXT NOT NULL);
CREATE INDEX visits_window ON visits(piece,seq);
`)
}

// R-6MU3-HBIF R-BFTP-M1XY R-BH1L-ZTON
func TestMintingAndRollback(t *testing.T) {
	f := newFixture(t)
	f.rand = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rand})
	c := f.campaign("mail")
	eq(t, c.ID, "camp_5a5a5a5a5a5a5a5a")
	_, _, e := f.s.DeleteCampaign(ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.rand = bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rand})
	c = f.campaign()
	eq(t, c.ID, "camp_6b6b6b6b6b6b6b6b")
	data := []byte{}
	for _, b := range []struct {
		n int
		v byte
	}{{8, 0x5a}, {16, 0x6b}, {8, 0x7c}, {16, 0x8d}} {
		data = append(data, bytes.Repeat([]byte{b.v}, b.n)...)
	}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(data)})
	m := f.member(c, 1, "email", "mail")
	eq(t, m.Pieces[0].Piece.ID, "piece_5a5a5a5a5a5a5a5a")
	eq(t, m.Pieces[0].Piece.Token, "nnvww23lnnvww23lnnvww23lnm")
	eq(t, m.Pieces[1].Piece.ID, "piece_7c7c7c7c7c7c7c7c")
	eq(t, m.Pieces[1].Piece.Token, "rwgy3dmnrwgy3dmnrwgy3dmnru")
	data = append(bytes.Repeat([]byte{0x11}, 8), bytes.Repeat([]byte{0x6b}, 16)...)
	data = append(data, bytes.Repeat([]byte{0x7c}, 16)...)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(data)})
	m2 := f.member(c, 2, "mail")
	eq(t, m2.Pieces[0].Piece.ID, "piece_1111111111111111")
	eq(t, m2.Pieces[0].Piece.Token, "pr6hy7d4pr6hy7d4pr6hy7d4pq")
	for _, data := range [][]byte{nil, bytes.Repeat([]byte{0x22}, 7), bytes.Repeat([]byte{0x5a}, 64), append(bytes.Repeat([]byte{0x33}, 8), bytes.Repeat([]byte{0x6b}, 128)...), append(bytes.Repeat([]byte{0x44}, 8), bytes.Repeat([]byte{0x55}, 16)...)} {
		f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(data)})
		row, b, e := f.s.AddMember(ctx, store.MemberInput{Campaign: c.ID, Candidate: "cand_0000000000000003", Channels: []string{"mail", "email"}, UserID: "owner"})
		errIs(t, e, store.ErrUnreachable)
		eq(t, row, store.MemberRow{})
		eq(t, b, false)
		_, e = f.s.GetMember(ctx, c.ID, "cand_0000000000000003")
		errIs(t, e, store.ErrNotFound)
		co, e := f.s.Counts(ctx, c.ID)
		eq(t, must(t, co, e), store.Counts{Members: 2})
	}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x6b}, 64))})
	v, e := f.s.CreateCampaign(ctx, store.Campaign{Name: "collision", Channels: []string{"mail"}, Destination: c.Destination, CreatedUserID: "owner"})
	errIs(t, e, store.ErrUnreachable)
	eq(t, v, store.Campaign{})
	// Id collision retries draw only ids, then the token bytes, including collisions inside one add.
	data = append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x66}, 8)...)
	data = append(data, bytes.Repeat([]byte{0x77}, 16)...)
	data = append(data, bytes.Repeat([]byte{0x66}, 8)...)
	data = append(data, bytes.Repeat([]byte{0x88}, 8)...)
	data = append(data, bytes.Repeat([]byte{0x77}, 16)...)
	data = append(data, bytes.Repeat([]byte{0x99}, 16)...)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(data)})
	third := f.member(c, 3, "email", "mail")
	eq(t, third.Pieces[0].Piece.ID, "piece_6666666666666666")
	eq(t, third.Pieces[1].Piece.ID, "piece_8888888888888888")
	if third.Pieces[0].Piece.Token == third.Pieces[1].Piece.Token {
		t.Fatal("token reused")
	}
	fresh := newFixture(t)
	c = fresh.campaign("mail")
	fresh.s = store.New(fresh.d, store.Config{Now: func() time.Time { return fresh.now }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 24))})
	first := fresh.member(c, 1, "mail")
	eq(t, first.Pieces[0].Piece.ID, "piece_5a5a5a5a5a5a5a5a")
	eq(t, first.Pieces[0].Piece.Token, "ljnfuws2ljnfuws2ljnfuws2li")
}

type failedReader struct{ reads int }

func (r *failedReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("random unavailable")
}

// R-86XH-2RMD
func TestRandomOnlyForNewRecords(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	m := f.member(c, 1, "mail")
	r := &failedReader{}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	for _, call := range calls(f, c, m) {
		_ = call.fn(ctx)
	}
	_, _, e := f.s.DeleteCampaign(ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, r.reads, 0)
	bad := store.Campaign{Name: " ", Channels: []string{"mail"}, Destination: c.Destination, CreatedUserID: "owner"}
	_, e = f.s.CreateCampaign(ctx, bad)
	errIs(t, e, store.ErrInvalid)
	eq(t, r.reads, 0)
}

// R-BN53-WOE4
func TestConcurrentChanges(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	in := store.MemberInput{Campaign: c.ID, Candidate: "cand_0000000000000001", Channels: []string{"mail", "email"}, UserID: "owner"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	changed := 0
	rows := []store.MemberRow{}
	for range 12 {
		wg.Go(func() {
			m, b, e := f.s.AddMember(ctx, in)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				t.Error(e)
			}
			if b {
				changed++
			}
			rows = append(rows, m)
		})
	}
	wg.Wait()
	eq(t, changed, 1)
	for _, r := range rows {
		eq(t, r, rows[0])
	}
	m := rows[0]
	for _, operation := range []func() (bool, error){func() (bool, error) {
		_, b, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: m.Pieces[0].Piece.ID, UserID: "owner"})
		return b, e
	}, func() (bool, error) {
		_, b, e := f.s.DropPiece(ctx, store.PieceAction{Piece: m.Pieces[1].Piece.ID, UserID: "owner"})
		return b, e
	}, func() (bool, error) {
		_, b, e := f.s.RejectMember(ctx, store.Rejection{Campaign: c.ID, Candidate: m.Member.Candidate, Why: "why", UserID: "owner"})
		return b, e
	}} {
		changed = 0
		for range 12 {
			wg.Go(func() {
				b, e := operation()
				mu.Lock()
				defer mu.Unlock()
				if e != nil {
					t.Error(e)
				}
				if b {
					changed++
				}
			})
		}
		wg.Wait()
		eq(t, changed, 1)
	}
	other := f.member(c, 2, "mail")
	changed = 0
	wg.Go(func() {
		_, b, e := f.s.DeliverPiece(ctx, store.PieceAction{Piece: other.Pieces[0].Piece.ID, UserID: "owner"})
		mu.Lock()
		defer mu.Unlock()
		if e != nil {
			t.Error(e)
		}
		if b {
			changed++
		}
	})
	wg.Go(func() {
		ps, e := f.s.DeliverChannel(ctx, store.ChannelDelivery{Campaign: c.ID, Channel: "mail", UserID: "owner"})
		mu.Lock()
		defer mu.Unlock()
		if e != nil {
			t.Error(e)
		}
		changed += len(ps)
	})
	wg.Wait()
	eq(t, changed, 1)
}

// R-85PK-OZVO R-7UQH-927F
func TestFailuresTakePrecedence(t *testing.T) {
	f := newFixture(t)
	c := f.campaign()
	m := f.member(c, 1, "mail")
	before, e := f.s.GetMember(ctx, c.ID, m.Member.Candidate)
	if e != nil {
		t.Fatal(e)
	}
	all := calls(f, c, m)
	all = append(all, call{"create", func(ctx context.Context) error {
		v, e := f.s.CreateCampaign(ctx, store.Campaign{})
		eq(t, v, store.Campaign{})
		return e
	}}, call{"delete", func(ctx context.Context) error {
		v, b, e := f.s.DeleteCampaign(ctx, c.ID)
		eq(t, v, store.Campaign{})
		eq(t, b, false)
		return e
	}})
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, call := range all {
		e := call.fn(done)
		errIs(t, e, store.ErrUnreachable)
		errIs(t, e, context.Canceled)
		if errors.Is(e, store.ErrInvalid) || errors.Is(e, store.ErrNotFound) {
			t.Fatal(call.name, e)
		}
	}
	f.d.SetFailing(true)
	for _, call := range all {
		e := call.fn(ctx)
		errIs(t, e, store.ErrUnreachable)
		if errors.Is(e, store.ErrInvalid) || errors.Is(e, store.ErrNotFound) {
			t.Fatal(call.name, e)
		}
	}
	f.d.SetFailing(false)
	after, e := f.s.GetMember(ctx, c.ID, m.Member.Candidate)
	eq(t, must(t, after, e), before)
	co, e := f.s.Counts(ctx, c.ID)
	eq(t, must(t, co, e), store.Counts{Members: 1})
	if e = f.d.Close(); e != nil {
		t.Fatal(e)
	}
	for _, call := range all {
		errIs(t, call.fn(ctx), store.ErrUnreachable)
	}
	f.open()
	after, e = f.s.GetMember(ctx, c.ID, m.Member.Candidate)
	eq(t, must(t, after, e), before)
}

// R-6LM7-3JRQ R-BLX7-IWNF
func TestOneStampPerWrite(t *testing.T) {
	f := newFixture(t)
	base := f.now
	reads := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { v := base.Add(time.Duration(reads) * time.Minute); reads++; return v }, Rand: &random{}})
	c := f.campaign()
	eq(t, reads, 1)
	eq(t, c.Created, base.UTC().Truncate(time.Second))
	m := f.member(c, 1, "mail", "email")
	eq(t, reads, 2)
	eq(t, m.Member.Added, base.Add(time.Minute).UTC().Truncate(time.Second))
	for _, p := range m.Pieces {
		eq(t, p.Piece.Created, m.Member.Added)
	}
	_ = f.member(c, 2, "mail", "email")
	eq(t, reads, 3)
	ps, e := f.s.DeliverChannel(ctx, store.ChannelDelivery{Campaign: c.ID, Channel: "mail", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	eq(t, reads, 4)
	eq(t, len(ps), 2)
	for _, p := range ps {
		eq(t, p.Delivered, base.Add(3*time.Minute).UTC().Truncate(time.Second))
	}
}

// R-6MU3-HBIF R-BFTP-M1XY R-BH1L-ZTON
func TestCollisionAttemptBounds(t *testing.T) {
	f := newFixture(t)
	// The fixture reader uses one byte value per read: campaign=1, piece=2, token=3.
	c := f.campaign("mail")
	_ = f.member(c, 1, "mail")
	campaignBytes := append(bytes.Repeat([]byte{1}, 8*8), bytes.Repeat([]byte{0x7a}, 8)...)
	rnd := bytes.NewReader(campaignBytes)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rnd})
	got, e := f.s.CreateCampaign(ctx, store.Campaign{Name: "bounded", Channels: []string{"mail"}, Destination: c.Destination, CreatedUserID: "owner"})
	errIs(t, e, store.ErrUnreachable)
	eq(t, got, store.Campaign{})
	eq(t, rnd.Len(), 8)
	pieceBytes := append(bytes.Repeat([]byte{2}, 8*8), bytes.Repeat([]byte{0x7b}, 8)...)
	tokenBytes := append(bytes.Repeat([]byte{0x7c}, 8), bytes.Repeat([]byte{3}, 8*16)...)
	tokenBytes = append(tokenBytes, bytes.Repeat([]byte{0x7d}, 16)...)
	for _, tc := range []struct {
		entropy   []byte
		remaining int
	}{{pieceBytes, 8}, {tokenBytes, 16}} {
		rnd = bytes.NewReader(tc.entropy)
		f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rnd})
		row, b, e := f.s.AddMember(ctx, store.MemberInput{Campaign: c.ID, Candidate: "cand_0000000000000002", Channels: []string{"mail"}, UserID: "owner"})
		errIs(t, e, store.ErrUnreachable)
		eq(t, row, store.MemberRow{})
		eq(t, b, false)
		eq(t, rnd.Len(), tc.remaining)
		_, e = f.s.GetMember(ctx, c.ID, "cand_0000000000000002")
		errIs(t, e, store.ErrNotFound)
	}
	rows, e := f.s.ListCampaigns(ctx, "", "", 10)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(rows), 1)
	co, e := f.s.Counts(ctx, c.ID)
	eq(t, must(t, co, e), store.Counts{Members: 1})
}
