package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

var ctx = context.Background()

type sequence struct {
	n     byte
	reads int
}

func (r *sequence) Read(p []byte) (int, error) {
	r.reads += len(p)
	for i := range p {
		p[i] = r.n
		r.n++
	}
	return len(p), nil
}

type fixture struct {
	s    *store.Store
	d    *db.DB
	path string
	now  time.Time
	rnd  *sequence
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{path: filepath.Join(t.TempDir(), "state", "gmail.db"), now: time.Date(2025, 1, 2, 3, 4, 5, 678, time.FixedZone("offset", 7200)), rnd: &sequence{}}
	f.d = open(t, f)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	t.Cleanup(func() { _ = f.d.Close() })
	return f
}
func open(t *testing.T, f *fixture) *db.DB {
	t.Helper()
	d, err := db.Open(ctx, db.Config{Path: f.path, Migrations: app.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
func requireErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v want %v", err, want)
	}
}
func transfer() store.Transfer {
	return store.Transfer{Direction: store.DirectionPull, Status: store.StatusQueued, Message: "message", Attachment: "part", Name: "name", Path: "path", ContentType: "type", UserID: "owner", UserEmail: "email", RequestID: "request"}
}
func draft() store.Draft {
	return store.Draft{GmailDraft: "google-draft", Message: "message", Thread: "thread", To: []string{"one", "two"}, Cc: []string{"cc"}, Bcc: []string{"bcc"}, Subject: "subject", Body: "body", ReplyTo: "reply", HeaderInReplyTo: "header", HeaderReferences: "references", UserID: "owner"}
}
func addT(t *testing.T, f *fixture, v store.Transfer) store.Transfer {
	t.Helper()
	v, err := f.s.AddTransfer(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func addD(t *testing.T, f *fixture, v store.Draft) store.Draft {
	t.Helper()
	v, err := f.s.AddDraft(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func checkT(t *testing.T, f *fixture, want store.Transfer) {
	t.Helper()
	got, err := f.s.GetTransfer(ctx, want.UserID, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, want)
	d := open(t, f)
	defer func() { _ = d.Close() }()
	got, err = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd}).GetTransfer(ctx, want.UserID, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, want)
}
func checkD(t *testing.T, f *fixture, want store.Draft) {
	t.Helper()
	got, err := f.s.GetDraft(ctx, want.UserID, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, want)
	d := open(t, f)
	defer func() { _ = d.Close() }()
	got, err = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd}).GetDraft(ctx, want.UserID, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, want)
}

// R-Y98X-R7OY R-YAGU-4ZFN R-YBOQ-IR6C R-YCWM-WIX1
// Struct fields in positional literals prove the published field order.
func TestTypes(t *testing.T) {
	_ = store.Transfer{"", "", "", "", "", "", "", "", "", "", 0, time.Time{}, time.Time{}, "", "", "", "", "", ""}
	_ = store.Ending{"", "", 0, "", "", "", ""}
	_ = store.Attachment{"", "", "", 0}
	_ = store.Draft{"", "", "", "", "", nil, nil, nil, "", "", "", "", "", nil, "", time.Time{}, time.Time{}, time.Time{}, ""}
	f := setup(t)
	if f.s == nil {
		t.Fatal("nil")
	}
}

// R-YE4J-AANQ R-YFCF-O2EF R-YGKC-1U54
func TestConstants(t *testing.T) {
	eq(t, []string{store.TransferIDPrefix, store.DraftIDPrefix, store.DirectionPull, store.DirectionPush, store.StatusQueued, store.StatusRunning, store.StatusDone, store.StatusFailed, store.DraftOpen, store.DraftSent, store.DraftDiscarded}, []string{"xfr_", "drf_", "pull", "push", "queued", "running", "done", "failed", "open", "sent", "discarded"})
	eq(t, []string{store.ReasonNotFound, store.ReasonForbidden, store.ReasonTooLarge, store.ReasonReconnect, store.ReasonProvider, store.ReasonArtifacts, store.ReasonInterrupted}, []string{"not_found", "forbidden", "too_large", "reconnect", "provider", "artifacts", "interrupted"})
	sentinels := []error{store.ErrNotFound, store.ErrEnded, store.ErrNotOpen, store.ErrUnreachable}
	for i, a := range sentinels {
		if a == nil {
			t.Fatal("nil")
		}
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatal("sentinel overlap")
			}
		}
	}
	if store.Unreachable == "" || strings.Contains(store.Unreachable, "\n") {
		t.Fatal("invalid copy")
	}
}

// R-ZOGP-TKU4
func TestEmpty(t *testing.T) {
	f := setup(t)
	for _, u := range []string{"owner", "other", ""} {
		a, err := f.s.ListTransfers(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, a, []store.Transfer{})
		b, err := f.s.ListDrafts(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, b, []store.Draft{})
		v, e := f.s.GetTransfer(ctx, u, "absent")
		eq(t, v, store.Transfer{})
		requireErr(t, e, store.ErrNotFound)
		d, e := f.s.GetDraft(ctx, u, "absent")
		eq(t, d, store.Draft{})
		requireErr(t, e, store.ErrNotFound)
	}
	a, e := f.s.UnfinishedTransfers(ctx)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, a, []store.Transfer{})
}

// R-YHS8-FLVT R-YMNT-YOUL R-YRJF-HRTD R-YTZ8-9BAR R-YWF1-0US5 R-ZKT0-O9M1
func TestTransferLifecycle(t *testing.T) {
	f := setup(t)
	input := transfer()
	input.ID = "ignored"
	input.Bytes = -1
	input.Reason = "ignored"
	input.URL = "ignored"
	input.Link = "ignored"
	input.ArtifactID = "ignored"
	input.Finished = f.now
	v := addT(t, f, input)
	if !store.ValidTransferID(v.ID) {
		t.Fatal(v.ID)
	}
	want := transfer()
	want.ID = v.ID
	want.Started = f.now.UTC().Truncate(time.Second)
	eq(t, v, want)
	checkT(t, f, want)
	other := addT(t, f, transfer())
	f.now = f.now.Add(time.Hour)
	v, e := f.s.StartTransfer(ctx, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	want.Status = store.StatusRunning
	eq(t, v, want)
	checkT(t, f, want)
	v, e = f.s.SetTransferBytes(ctx, v.ID, 19)
	if e != nil {
		t.Fatal(e)
	}
	want.Bytes = 19
	eq(t, v, want)
	checkT(t, f, want)
	v, e = f.s.SetTransferBytes(ctx, v.ID, 0)
	if e != nil {
		t.Fatal(e)
	}
	want.Bytes = 0
	eq(t, v, want)
	ending := store.Ending{Status: store.StatusDone, Bytes: 23, Message: "new-message", URL: "url", Link: "link", ArtifactID: "artifact"}
	v, e = f.s.FinishTransfer(ctx, v.ID, ending)
	if e != nil {
		t.Fatal(e)
	}
	want.Status = ending.Status
	want.Bytes = ending.Bytes
	want.Message = ending.Message
	want.URL = ending.URL
	want.Link = ending.Link
	want.ArtifactID = ending.ArtifactID
	want.Finished = f.now.UTC().Truncate(time.Second)
	eq(t, v, want)
	checkT(t, f, want)
	checkT(t, f, other)
	for _, reason := range []string{store.ReasonNotFound, store.ReasonForbidden, store.ReasonTooLarge, store.ReasonReconnect, store.ReasonProvider, store.ReasonArtifacts, store.ReasonInterrupted} {
		q := addT(t, f, transfer())
		v, e = f.s.FinishTransfer(ctx, q.ID, store.Ending{Status: store.StatusFailed, Reason: reason, Bytes: 3})
		if e != nil {
			t.Fatal(e)
		}
		q.Status = store.StatusFailed
		q.Reason = reason
		q.Bytes = 3
		q.Finished = f.now.UTC().Truncate(time.Second)
		eq(t, v, q)
		checkT(t, f, q)
	}
}

// R-5IST-R3B4 R-Z3QF-BH8B R-5K0Q-4V1T R-5L8M-IMSI R-Z8M0-UK73 R-5NOF-A69W R-ZM0X-21CQ
func TestDraftLifecycle(t *testing.T) {
	f := setup(t)
	input := draft()
	input.ID = "ignored"
	input.Status = "ignored"
	input.Attachments = []store.Attachment{{Path: "ignored"}}
	input.Link = "ignored"
	input.Created = f.now.Add(time.Hour)
	input.Modified = input.Created
	input.Sent = input.Created
	d := addD(t, f, input)
	want := draft()
	want.ID = d.ID
	want.Status = store.DraftOpen
	want.Attachments = []store.Attachment{}
	want.Link = urls.DraftLink(want.Message)
	want.Created = f.now.UTC().Truncate(time.Second)
	want.Modified = want.Created
	eq(t, d, want)
	if !store.ValidDraftID(d.ID) {
		t.Fatal(d.ID)
	}
	checkD(t, f, want)
	other := addD(t, f, draft())
	f.now = f.now.Add(time.Hour)
	for i := range 2 {
		a := store.Attachment{Path: fmt.Sprint("path", i), Name: "name", ContentType: "type", Bytes: int64(i)}
		th := ""
		if i == 0 {
			th = "new-thread"
		}
		d, e := f.s.AttachToDraft(ctx, want.ID, a, fmt.Sprint("message", i), th)
		if e != nil {
			t.Fatal(e)
		}
		want.Attachments = append(want.Attachments, a)
		want.Message = fmt.Sprint("message", i)
		if th != "" {
			want.Thread = th
		}
		want.Modified = f.now.UTC().Truncate(time.Second)
		want.Link = urls.DraftLink(want.Message)
		eq(t, d, want)
		checkD(t, f, want)
	}
	f.now = f.now.Add(time.Hour)
	d, e := f.s.SetDraftMessage(ctx, want.ID, "reconciled", "")
	if e != nil {
		t.Fatal(e)
	}
	want.Message = "reconciled"
	want.Link = urls.DraftLink(want.Message)
	eq(t, d, want)
	checkD(t, f, want)
	f.now = f.now.Add(time.Hour)
	d, e = f.s.SetDraftMessage(ctx, want.ID, "moved-message", "moved-thread")
	if e != nil {
		t.Fatal(e)
	}
	want.Message = "moved-message"
	want.Thread = "moved-thread"
	want.Link = urls.DraftLink(want.Message)
	eq(t, d, want)
	checkD(t, f, want)
	checkD(t, f, other)
	sent := f.now.Add(-time.Hour)
	d, e = f.s.MarkDraftSent(ctx, want.ID, "sent-message", "sent-thread", sent)
	if e != nil {
		t.Fatal(e)
	}
	want.Status = store.DraftSent
	want.GmailDraft = ""
	want.Message = "sent-message"
	want.Thread = "sent-thread"
	want.Sent = sent.UTC().Truncate(time.Second)
	want.Modified = f.now.UTC().Truncate(time.Second)
	want.Link = urls.MessageLink(want.Message)
	eq(t, d, want)
	checkD(t, f, want)
	checkD(t, f, other)
	discard := addD(t, f, draft())
	discard, e = f.s.AttachToDraft(ctx, discard.ID, store.Attachment{Path: "a"}, "m", "")
	if e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Hour)
	got, e := f.s.MarkDraftDiscarded(ctx, discard.ID)
	if e != nil {
		t.Fatal(e)
	}
	discard.Status = store.DraftDiscarded
	discard.GmailDraft = ""
	discard.Link = ""
	discard.Modified = f.now.UTC().Truncate(time.Second)
	eq(t, got, discard)
	checkD(t, f, discard)
	zero := addD(t, f, draft())
	got, e = f.s.MarkDraftSent(ctx, zero.ID, "sent", "", time.Time{})
	if e != nil {
		t.Fatal(e)
	}
	zero.Status = store.DraftSent
	zero.GmailDraft = ""
	zero.Message = "sent"
	zero.Link = urls.MessageLink("sent")
	zero.Modified = f.now.UTC().Truncate(time.Second)
	zero.Sent = zero.Modified
	eq(t, got, zero)
	checkD(t, f, zero)
}

// R-YK81-75D7
func TestIDs(t *testing.T) {
	for _, v := range []string{"", "xfr_0123456789ABCDEF", "drf_0123456789abcde", "drf_0123456789abcdef0", "run_0123456789abcdef", "xfr_0123456789abcdeg"} {
		if store.ValidTransferID(v) || store.ValidDraftID(v) {
			t.Fatal(v)
		}
	}
	if !store.ValidTransferID("xfr_0123456789abcdef") || store.ValidDraftID("xfr_0123456789abcdef") || !store.ValidDraftID("drf_0123456789abcdef") || store.ValidTransferID("drf_0123456789abcdef") {
		t.Fatal("invalid id validation")
	}
}

// R-YLFX-KX3W R-YP3M-Q8BZ R-Z4YB-P8Z0 R-YQBJ-402O
func TestValidationAndRandom(t *testing.T) {
	f := setup(t)
	for _, field := range []string{"direction", "status", "user", "path", "message"} {
		v := transfer()
		switch field {
		case "direction":
			v.Direction = "invalid"
		case "status":
			v.Status = store.StatusDone
		case "user":
			v.UserID = ""
		case "path":
			v.Path = ""
		case "message":
			v.Message = ""
		}
		got, e := f.s.AddTransfer(ctx, v)
		eq(t, got, store.Transfer{})
		requireErr(t, e, store.ErrUnreachable)
	}
	for _, field := range []string{"user", "gmail", "message"} {
		v := draft()
		switch field {
		case "user":
			v.UserID = ""
		case "gmail":
			v.GmailDraft = ""
		case "message":
			v.Message = ""
		}
		got, e := f.s.AddDraft(ctx, v)
		eq(t, got, store.Draft{})
		requireErr(t, e, store.ErrUnreachable)
	}
	eq(t, f.rnd.reads, 0)
	r := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))
	s := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	a, e := s.AddTransfer(ctx, transfer())
	if e != nil {
		t.Fatal(e)
	}
	eq(t, a.ID, "xfr_5a5a5a5a5a5a5a5a")
	eq(t, r.Len(), 0)
	r = bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))
	s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
	a, e = s.AddTransfer(ctx, transfer())
	if e != nil {
		t.Fatal(e)
	}
	eq(t, a.ID, "xfr_6b6b6b6b6b6b6b6b")
	eq(t, r.Len(), 0)
	for _, draftKind := range []bool{false, true} {
		r = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 8))
		s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
		if draftKind {
			d, e := s.AddDraft(ctx, draft())
			if e != nil {
				t.Fatal(e)
			}
			eq(t, d.ID, "drf_5a5a5a5a5a5a5a5a")
		}
		r = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 72))
		s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
		if draftKind {
			d, e := s.AddDraft(ctx, draft())
			eq(t, d, store.Draft{})
			requireErr(t, e, store.ErrUnreachable)
		} else {
			a, e := s.AddTransfer(ctx, transfer())
			eq(t, a, store.Transfer{})
			requireErr(t, e, store.ErrUnreachable)
		}
		eq(t, r.Len(), 8)
		s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(nil)})
		if draftKind {
			d, e := s.AddDraft(ctx, draft())
			eq(t, d, store.Draft{})
			requireErr(t, e, store.ErrUnreachable)
		} else {
			a, e := s.AddTransfer(ctx, transfer())
			eq(t, a, store.Transfer{})
			requireErr(t, e, store.ErrUnreachable)
		}
	}
	aList, e := f.s.ListTransfers(ctx, "owner")
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(aList), 2)
	dList, e := f.s.ListDrafts(ctx, "owner")
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(dList), 1)
}

// R-YSRB-VJK2 R-YV74-N31G R-YXMX-EMIU R-5OWB-NY0L R-ZJL4-AHVC
func TestWriteErrors(t *testing.T) {
	f := setup(t)
	q := addT(t, f, transfer())
	runningInput := transfer()
	runningInput.Status = store.StatusRunning
	r := addT(t, f, runningInput)
	d := addD(t, f, draft())
	ended, e := f.s.FinishTransfer(ctx, r.ID, store.Ending{Status: store.StatusDone})
	if e != nil {
		t.Fatal(e)
	}
	closed, e := f.s.MarkDraftDiscarded(ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		call func() (store.Transfer, error)
		want error
	}{{func() (store.Transfer, error) { return f.s.StartTransfer(ctx, "absent") }, store.ErrNotFound}, {func() (store.Transfer, error) { return f.s.StartTransfer(ctx, ended.ID) }, store.ErrEnded}, {func() (store.Transfer, error) { return f.s.SetTransferBytes(ctx, "absent", -1) }, store.ErrUnreachable}, {func() (store.Transfer, error) { return f.s.SetTransferBytes(ctx, "absent", 1) }, store.ErrNotFound}, {func() (store.Transfer, error) { return f.s.SetTransferBytes(ctx, q.ID, 1) }, store.ErrEnded}, {func() (store.Transfer, error) {
		return f.s.FinishTransfer(ctx, "absent", store.Ending{Status: "invalid"})
	}, store.ErrUnreachable}, {func() (store.Transfer, error) {
		return f.s.FinishTransfer(ctx, "absent", store.Ending{Status: store.StatusDone})
	}, store.ErrNotFound}, {func() (store.Transfer, error) {
		return f.s.FinishTransfer(ctx, ended.ID, store.Ending{Status: store.StatusDone})
	}, store.ErrEnded}, {func() (store.Transfer, error) {
		return f.s.FinishTransfer(ctx, q.ID, store.Ending{Status: store.StatusDone})
	}, store.ErrUnreachable}} {
		got, e := tc.call()
		eq(t, got, store.Transfer{})
		requireErr(t, e, tc.want)
	}
	for _, ending := range []store.Ending{{Status: store.StatusDone, Bytes: -1}, {Status: store.StatusDone, Reason: store.ReasonProvider}, {Status: store.StatusFailed, Reason: "invalid"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, Message: "m"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, URL: "u"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, Link: "l"}, {Status: store.StatusFailed, Reason: store.ReasonProvider, ArtifactID: "a"}} {
		got, e := f.s.FinishTransfer(ctx, q.ID, ending)
		eq(t, got, store.Transfer{})
		requireErr(t, e, store.ErrUnreachable)
	}
	for _, id := range []string{"absent", closed.ID} {
		want := store.ErrNotFound
		if id == closed.ID {
			want = store.ErrNotOpen
		}
		for _, call := range []func() (store.Draft, error){func() (store.Draft, error) { return f.s.AttachToDraft(ctx, id, store.Attachment{Path: "p"}, "m", "") }, func() (store.Draft, error) { return f.s.MarkDraftSent(ctx, id, "m", "", time.Time{}) }, func() (store.Draft, error) { return f.s.MarkDraftDiscarded(ctx, id) }, func() (store.Draft, error) { return f.s.SetDraftMessage(ctx, id, "m", "") }} {
			got, e := call()
			eq(t, got, store.Draft{})
			requireErr(t, e, want)
		}
		for _, call := range []func() (store.Draft, error){func() (store.Draft, error) { return f.s.AttachToDraft(ctx, id, store.Attachment{}, "m", "") }, func() (store.Draft, error) {
			return f.s.AttachToDraft(ctx, id, store.Attachment{Path: "p", Bytes: -1}, "m", "")
		}, func() (store.Draft, error) { return f.s.AttachToDraft(ctx, id, store.Attachment{Path: "p"}, "", "") }, func() (store.Draft, error) { return f.s.MarkDraftSent(ctx, id, "", "", time.Time{}) }, func() (store.Draft, error) { return f.s.SetDraftMessage(ctx, id, "", "") }} {
			got, e := call()
			eq(t, got, store.Draft{})
			requireErr(t, e, store.ErrUnreachable)
		}
	}
	checkT(t, f, q)
	checkT(t, f, ended)
	checkD(t, f, closed)
}

// R-YYUT-SE9J R-ZDHM-DN5V R-Z02Q-6608 R-ZEPI-REWK R-Z1AM-JXQX
func TestReads(t *testing.T) {
	f := setup(t)
	a := addT(t, f, transfer())
	b := addT(t, f, transfer())
	d := addD(t, f, draft())
	e := addD(t, f, draft())
	f.now = f.now.Add(-time.Hour)
	v := transfer()
	v.UserID = "other"
	other := addT(t, f, v)
	dd := draft()
	dd.UserID = "other"
	od := addD(t, f, dd)
	got, err := f.s.ListTransfers(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, []store.Transfer{b, a})
	ds, err := f.s.ListDrafts(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{e, d})
	for _, id := range []string{a.ID, "absent"} {
		v, err := f.s.GetTransfer(ctx, "other", id)
		eq(t, v, store.Transfer{})
		requireErr(t, err, store.ErrNotFound)
	}
	for _, id := range []string{d.ID, "absent"} {
		v, err := f.s.GetDraft(ctx, "other", id)
		eq(t, v, store.Draft{})
		requireErr(t, err, store.ErrNotFound)
	}
	checkT(t, f, other)
	checkD(t, f, od)
	b, err = f.s.StartTransfer(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	unfinished, err := f.s.UnfinishedTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, unfinished, []store.Transfer{other, a, b})
	later := open(t, f)
	defer func() { _ = later.Close() }()
	ss := store.New(later, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	unfinished, err = ss.UnfinishedTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, unfinished, []store.Transfer{other, a, b})
	_, err = f.s.FinishTransfer(ctx, b.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
	if err != nil {
		t.Fatal(err)
	}
	unfinished, err = ss.UnfinishedTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, unfinished, []store.Transfer{other, a})
}

// R-Z2II-XPHM R-PUVP-9L69 R-ZH5B-IYDY
func TestPruning(t *testing.T) {
	f := setup(t)
	transfers := []store.Transfer{}
	drafts := []store.Draft{}
	for _, u := range []string{"b-user", "a-user"} {
		for i := range 4 {
			v := transfer()
			v.UserID = u
			v = addT(t, f, v)
			if i != 3 {
				var e error
				v, e = f.s.FinishTransfer(ctx, v.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
				if e != nil {
					t.Fatal(e)
				}
			}
			transfers = append(transfers, v)
			d := draft()
			d.UserID = u
			d = addD(t, f, d)
			if i != 3 {
				var e error
				if i == 1 {
					d, e = f.s.MarkDraftSent(ctx, d.ID, "sent", "", time.Time{})
				} else {
					d, e = f.s.MarkDraftDiscarded(ctx, d.ID)
				}
				if e != nil {
					t.Fatal(e)
				}
			}
			drafts = append(drafts, d)
			f.now = f.now.Add(time.Hour)
		}
	}
	now := f.now.Add(10 * 24 * time.Hour)
	for _, limits := range [][2]int64{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		a, e := f.s.PruneTransfers(ctx, now, limits[0], limits[1])
		eq(t, a, ([]store.Transfer)(nil))
		requireErr(t, e, store.ErrUnreachable)
		d, e := f.s.PruneDrafts(ctx, now, limits[0], limits[1])
		eq(t, d, ([]store.Draft)(nil))
		requireErr(t, e, store.ErrUnreachable)
	}
	a, e := f.s.PruneTransfers(ctx, now, math.MaxInt64, 1)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, a, []store.Transfer{})
	ds, e := f.s.PruneDrafts(ctx, now, math.MaxInt64, 1)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, ds, []store.Draft{})
	a, e = f.s.PruneTransfers(ctx, transfers[0].Started.Add(24*time.Hour), 1, 1)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, a, []store.Transfer{})
	ds, e = f.s.PruneDrafts(ctx, drafts[0].Modified.Add(24*time.Hour), 1, 1)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, ds, []store.Draft{})
	a, e = f.s.PruneTransfers(ctx, now, 1, 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, a, []store.Transfer{transfers[5], transfers[4], transfers[1], transfers[0]})
	ds, e = f.s.PruneDrafts(ctx, now, 1, 2)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, ds, []store.Draft{drafts[4], drafts[0]})
	later := open(t, f)
	defer func() { _ = later.Close() }()
	ss := store.New(later, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	for _, v := range transfers {
		removed := v.ID == transfers[5].ID || v.ID == transfers[4].ID || v.ID == transfers[1].ID || v.ID == transfers[0].ID
		got, e := ss.GetTransfer(ctx, v.UserID, v.ID)
		if removed {
			eq(t, got, store.Transfer{})
			requireErr(t, e, store.ErrNotFound)
		} else {
			if e != nil {
				t.Fatal(e)
			}
			eq(t, got, v)
			checkT(t, f, v)
		}
	}
	for _, v := range drafts {
		removed := v.ID == drafts[4].ID || v.ID == drafts[0].ID
		got, e := ss.GetDraft(ctx, v.UserID, v.ID)
		if removed {
			eq(t, got, store.Draft{})
			requireErr(t, e, store.ErrNotFound)
		} else {
			if e != nil {
				t.Fatal(e)
			}
			eq(t, got, v)
			checkD(t, f, v)
		}
	}
}

// R-ZID7-WQ4N R-ZJL4-AHVC
func TestUnavailable(t *testing.T) {
	f := setup(t)
	v := addT(t, f, transfer())
	d := addD(t, f, draft())
	calls := func(c context.Context) []func() error {
		return []func() error{func() error { _, e := f.s.AddTransfer(c, store.Transfer{}); return e }, func() error { _, e := f.s.StartTransfer(c, "absent"); return e }, func() error { _, e := f.s.SetTransferBytes(c, "absent", -1); return e }, func() error { _, e := f.s.FinishTransfer(c, "absent", store.Ending{}); return e }, func() error { _, e := f.s.GetTransfer(c, "other", v.ID); return e }, func() error { _, e := f.s.ListTransfers(c, "owner"); return e }, func() error { _, e := f.s.UnfinishedTransfers(c); return e }, func() error { _, e := f.s.PruneTransfers(c, f.now, 0, 0); return e }, func() error { _, e := f.s.AddDraft(c, store.Draft{}); return e }, func() error { _, e := f.s.AttachToDraft(c, "absent", store.Attachment{}, "", ""); return e }, func() error { _, e := f.s.MarkDraftSent(c, "absent", "", "", time.Time{}); return e }, func() error { _, e := f.s.MarkDraftDiscarded(c, "absent"); return e }, func() error { _, e := f.s.SetDraftMessage(c, "absent", "", ""); return e }, func() error { _, e := f.s.GetDraft(c, "other", d.ID); return e }, func() error { _, e := f.s.ListDrafts(c, "owner"); return e }, func() error { _, e := f.s.PruneDrafts(c, f.now, 0, 0); return e }}
	}
	check := func(c context.Context) {
		for _, call := range calls(c) {
			e := call()
			requireErr(t, e, store.ErrUnreachable)
			if c.Err() != nil {
				requireErr(t, e, c.Err())
			}
			for _, forbidden := range []error{store.ErrNotFound, store.ErrEnded, store.ErrNotOpen} {
				if errors.Is(e, forbidden) {
					t.Fatal(e)
				}
			}
		}
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	check(c)
	f.d.SetFailing(true)
	check(c)
	check(ctx)
	f.d.SetFailing(false)
	checkT(t, f, v)
	checkD(t, f, d)
	eq(t, f.rnd.reads, 16)
	if e := f.d.Close(); e != nil {
		t.Fatal(e)
	}
	check(ctx)
	later := open(t, f)
	defer func() { _ = later.Close() }()
	f.s = store.New(later, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	checkT(t, f, v)
	checkD(t, f, d)
}

// R-MTSU-Y1UL R-ZN8T-FT3F
func TestConcurrent(t *testing.T) {
	f := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 16 {
		wg.Go(func() {
			v := transfer()
			v.Status = store.StatusRunning
			v.Name = fmt.Sprint(i)
			v, e := f.s.AddTransfer(ctx, v)
			if e != nil {
				errs <- e
				return
			}
			_, e = f.s.SetTransferBytes(ctx, v.ID, 3)
			if e != nil {
				errs <- e
			}
			_, e = f.s.ListTransfers(ctx, "owner")
			if e != nil {
				errs <- e
			}
			_, e = f.s.GetTransfer(ctx, "owner", v.ID)
			if e != nil {
				errs <- e
			}
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatal(<-errs)
	}
	q := transfer()
	q.Status = store.StatusRunning
	v := addT(t, f, q)
	results := make(chan error, 16)
	for range 16 {
		wg.Go(func() { _, e := f.s.FinishTransfer(ctx, v.ID, store.Ending{Status: store.StatusDone}); results <- e })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else {
			requireErr(t, e, store.ErrEnded)
		}
	}
	eq(t, success, 1)
	d := addD(t, f, draft())
	for i := range 16 {
		wg.Go(func() {
			_, e := f.s.AttachToDraft(ctx, d.ID, store.Attachment{Path: fmt.Sprint(i)}, "new", "")
			if e != nil {
				errs <- e
			}
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatal(<-errs)
	}
	d, e := f.s.GetDraft(ctx, "owner", d.ID)
	if e != nil {
		t.Fatal(e)
	}
	eq(t, len(d.Attachments), 16)
	seen := map[string]bool{}
	for _, a := range d.Attachments {
		if seen[a.Path] {
			t.Fatal("duplicate")
		}
		seen[a.Path] = true
	}
	for i := range 16 {
		if !seen[fmt.Sprint(i)] {
			t.Fatalf("missing attachment %d", i)
		}
	}
	checkD(t, f, d)
	results = make(chan error, 2)
	wg.Go(func() { _, e := f.s.MarkDraftSent(ctx, d.ID, "sent", "", time.Time{}); results <- e })
	wg.Go(func() { _, e := f.s.MarkDraftDiscarded(ctx, d.ID); results <- e })
	wg.Wait()
	close(results)
	success = 0
	for e := range results {
		if e == nil {
			success++
		} else {
			requireErr(t, e, store.ErrNotOpen)
		}
	}
	eq(t, success, 1)
	got, e := f.s.GetDraft(ctx, "owner", d.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.SetDraftMessage(ctx, d.ID, "should-not-change", "")
	requireErr(t, e, store.ErrNotOpen)
	checkD(t, f, got)
}

// R-ZKT0-O9M1 R-ZM0X-21CQ
func TestRecordInvariants(t *testing.T) {
	f := setup(t)
	v := transfer()
	v.Direction = store.DirectionPush
	v.Status = store.StatusRunning
	v = addT(t, f, v)
	if _, e := f.s.FinishTransfer(ctx, v.ID, store.Ending{Status: store.StatusDone}); e != nil {
		t.Fatal(e)
	}
	d := draft()
	d.To = nil
	d.Cc = nil
	d.Bcc = nil
	d = addD(t, f, d)
	if d.To == nil || d.Cc == nil || d.Bcc == nil || d.Attachments == nil {
		t.Fatal("nil arrays")
	}
	if _, e := f.s.MarkDraftSent(ctx, d.ID, "sent", "", time.Time{}); e != nil {
		t.Fatal(e)
	}
	all, e := f.s.ListTransfers(ctx, "owner")
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range all {
		if v.Direction != store.DirectionPull && v.Direction != store.DirectionPush {
			t.Fatal(v)
		}
		switch v.Status {
		case store.StatusQueued, store.StatusRunning:
			if !v.Finished.IsZero() {
				t.Fatal(v)
			}
		case store.StatusDone:
			if v.Finished.IsZero() || v.Reason != "" {
				t.Fatal(v)
			}
		case store.StatusFailed:
			if v.Finished.IsZero() || v.Reason == "" {
				t.Fatal(v)
			}
		default:
			t.Fatal(v)
		}
		for _, tm := range []time.Time{v.Started, v.Finished} {
			if !tm.IsZero() && (tm.Location() != time.UTC || tm.Nanosecond() != 0) {
				t.Fatal(tm)
			}
		}
		if v.Status != store.StatusDone && (v.URL != "" || v.Link != "" || v.ArtifactID != "") {
			t.Fatal(v)
		}
	}
	ds, e := f.s.ListDrafts(ctx, "owner")
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range ds {
		if d.To == nil || d.Cc == nil || d.Bcc == nil || d.Attachments == nil {
			t.Fatal(d)
		}
		switch d.Status {
		case store.DraftOpen:
			if d.GmailDraft == "" || !d.Sent.IsZero() || d.Link != urls.DraftLink(d.Message) {
				t.Fatal(d)
			}
		case store.DraftSent:
			if d.GmailDraft != "" || d.Sent.IsZero() || d.Link != urls.MessageLink(d.Message) {
				t.Fatal(d)
			}
		case store.DraftDiscarded:
			if d.GmailDraft != "" || !d.Sent.IsZero() || d.Link != "" {
				t.Fatal(d)
			}
		default:
			t.Fatal(d)
		}
		for _, tm := range []time.Time{d.Created, d.Modified, d.Sent} {
			if !tm.IsZero() && (tm.Location() != time.UTC || tm.Nanosecond() != 0) {
				t.Fatal(tm)
			}
		}
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// R-YQBJ-402O R-ZJL4-AHVC
func TestRandomRecordErrors(t *testing.T) {
	for _, cause := range []error{store.ErrNotFound, store.ErrEnded, store.ErrNotOpen, errors.New("random failure")} {
		f := setup(t)
		beforeT := addT(t, f, transfer())
		beforeD := addD(t, f, draft())
		s := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: failingReader{cause}})
		v, err := s.AddTransfer(ctx, transfer())
		eq(t, v, store.Transfer{})
		requireErr(t, err, store.ErrUnreachable)
		d, err := s.AddDraft(ctx, draft())
		eq(t, d, store.Draft{})
		requireErr(t, err, store.ErrUnreachable)
		checkT(t, f, beforeT)
		checkD(t, f, beforeD)
		ts, err := f.s.ListTransfers(ctx, "owner")
		if err != nil {
			t.Fatal(err)
		}
		eq(t, ts, []store.Transfer{beforeT})
		ds, err := f.s.ListDrafts(ctx, "owner")
		if err != nil {
			t.Fatal(err)
		}
		eq(t, ds, []store.Draft{beforeD})
	}
}

// R-Z02Q-6608 R-Z1AM-JXQX R-ZEPI-REWK
func TestReadTimestampBeforeID(t *testing.T) {
	f := setup(t)
	a := addT(t, f, transfer())
	da := addD(t, f, draft())
	f.now = f.now.Add(-time.Hour)
	b := addT(t, f, transfer())
	db := addD(t, f, draft())
	f.now = a.Started
	c := addT(t, f, transfer())
	dc := addD(t, f, draft())
	ts, err := f.s.ListTransfers(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{c, a, b})
	ds, err := f.s.ListDrafts(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{dc, da, db})
	ts, err = f.s.UnfinishedTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{b, a, c})
	for _, v := range []store.Transfer{a, b, c} {
		if _, err := f.s.FinishTransfer(ctx, v.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted}); err != nil {
			t.Fatal(err)
		}
	}
	ts, err = f.s.UnfinishedTransfers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{})
	ts, err = f.s.ListTransfers(ctx, "absent")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{})
	ds, err = f.s.ListDrafts(ctx, "absent")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{})
}

// R-Z2II-XPHM R-PUVP-9L69
func TestPruneLargeFiniteAge(t *testing.T) {
	f := setup(t)
	f.now = time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)
	a := addT(t, f, transfer())
	d := addD(t, f, draft())
	var err error
	a, err = f.s.FinishTransfer(ctx, a.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
	if err != nil {
		t.Fatal(err)
	}
	d, err = f.s.MarkDraftDiscarded(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(1001, 1, 1, 0, 0, 0, 0, time.UTC)
	keepT := addT(t, f, transfer())
	keepD := addD(t, f, draft())
	keepD, err = f.s.MarkDraftDiscarded(ctx, keepD.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	ts, err := f.s.PruneTransfers(ctx, now, math.MaxInt64, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{})
	ds, err := f.s.PruneDrafts(ctx, now, math.MaxInt64, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{})
	ts, err = f.s.PruneTransfers(ctx, now, 200000, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{a})
	ds, err = f.s.PruneDrafts(ctx, now, 200000, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{d})
	checkT(t, f, keepT)
	checkD(t, f, keepD)
	later := open(t, f)
	defer func() { _ = later.Close() }()
	s := store.New(later, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	_, err = s.GetTransfer(ctx, "owner", a.ID)
	requireErr(t, err, store.ErrNotFound)
	_, err = s.GetDraft(ctx, "owner", d.ID)
	requireErr(t, err, store.ErrNotFound)
}

// R-Z2II-XPHM R-PUVP-9L69
func TestPruneUsesStartedAndModified(t *testing.T) {
	f := setup(t)
	a := addT(t, f, transfer())
	da := addD(t, f, draft())
	f.now = f.now.Add(10 * 24 * time.Hour)
	var err error
	a, err = f.s.FinishTransfer(ctx, a.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted})
	if err != nil {
		t.Fatal(err)
	}
	da, err = f.s.MarkDraftDiscarded(ctx, da.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := addT(t, f, transfer())
	db := addD(t, f, draft())
	db, err = f.s.MarkDraftDiscarded(ctx, db.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Started ages transfers; Modified ages finished drafts.
	ts, err := f.s.PruneTransfers(ctx, f.now.Add(time.Hour), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ts, []store.Transfer{a})
	ds, err := f.s.PruneDrafts(ctx, da.Modified.Add(24*time.Hour), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{})
	ds, err = f.s.PruneDrafts(ctx, da.Modified.Add(24*time.Hour+time.Nanosecond), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, ds, []store.Draft{da})
	checkT(t, f, b)
	checkD(t, f, db)
	later := open(t, f)
	defer func() { _ = later.Close() }()
	s := store.New(later, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	_, err = s.GetTransfer(ctx, "owner", a.ID)
	requireErr(t, err, store.ErrNotFound)
	_, err = s.GetDraft(ctx, "owner", da.ID)
	requireErr(t, err, store.ErrNotFound)
}

func permutations(n int, visit func([]int)) {
	order := make([]int, n)
	used := make([]bool, n)
	var walk func(int)
	walk = func(depth int) {
		if depth == n {
			visit(order)
			return
		}
		for i := range n {
			if !used[i] {
				used[i] = true
				order[depth] = i
				walk(depth + 1)
				used[i] = false
			}
		}
	}
	walk(0)
}

type draftResult struct {
	record store.Draft
	err    error
}
type transferResult struct {
	record store.Transfer
	err    error
}

func sameError(got, want error) bool {
	if want == nil {
		return got == nil
	}
	return errors.Is(got, want)
}

// R-MTSU-Y1UL
func TestConcurrentDraftSerializable(t *testing.T) {
	for range 8 {
		f := setup(t)
		original := addD(t, f, draft())
		f.now = f.now.Add(time.Hour)
		stamp := f.now.UTC().Truncate(time.Second)
		attachment := store.Attachment{Path: "racing-path", Name: "racing-name", ContentType: "racing-type", Bytes: 17}
		calls := []func() (store.Draft, error){
			func() (store.Draft, error) {
				return f.s.AttachToDraft(ctx, original.ID, attachment, "attached-message", "attached-thread")
			},
			func() (store.Draft, error) {
				return f.s.MarkDraftSent(ctx, original.ID, "sent-message", "sent-thread", time.Time{})
			},
			func() (store.Draft, error) { return f.s.MarkDraftDiscarded(ctx, original.ID) },
			func() (store.Draft, error) {
				return f.s.SetDraftMessage(ctx, original.ID, "probed-message", "probed-thread")
			},
		}
		actual := make([]draftResult, len(calls))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Go(func() { <-start; actual[i].record, actual[i].err = call() })
		}
		close(start)
		wg.Wait()
		final, err := f.s.GetDraft(ctx, "owner", original.ID)
		if err != nil {
			t.Fatal(err)
		}
		matched := false
		permutations(len(calls), func(order []int) {
			state := original
			state.Attachments = append([]store.Attachment{}, original.Attachments...)
			expected := make([]draftResult, len(calls))
			for _, i := range order {
				if state.Status != store.DraftOpen {
					expected[i].err = store.ErrNotOpen
					continue
				}
				switch i {
				case 0:
					state.Attachments = append(state.Attachments, attachment)
					state.Message = "attached-message"
					state.Thread = "attached-thread"
					state.Modified = stamp
				case 1:
					state.Status = store.DraftSent
					state.GmailDraft = ""
					state.Message = "sent-message"
					state.Thread = "sent-thread"
					state.Modified = stamp
					state.Sent = stamp
				case 2:
					state.Status = store.DraftDiscarded
					state.GmailDraft = ""
					state.Modified = stamp
				case 3:
					state.Message = "probed-message"
					state.Thread = "probed-thread"
				}
				switch state.Status {
				case store.DraftOpen:
					state.Link = urls.DraftLink(state.Message)
				case store.DraftSent:
					state.Link = urls.MessageLink(state.Message)
				case store.DraftDiscarded:
					state.Link = ""
				}
				expected[i].record = state
			}
			if !reflect.DeepEqual(final, state) {
				return
			}
			for i := range actual {
				if !sameError(actual[i].err, expected[i].err) || !reflect.DeepEqual(actual[i].record, expected[i].record) {
					return
				}
			}
			matched = true
		})
		if !matched {
			t.Fatalf("no serial order matches results %#v and final %#v", actual, final)
		}
		checkD(t, f, final)
	}
}

// R-MTSU-Y1UL
func TestConcurrentTransferSerializable(t *testing.T) {
	for range 8 {
		f := setup(t)
		original := addT(t, f, transfer())
		f.now = f.now.Add(time.Hour)
		stamp := f.now.UTC().Truncate(time.Second)
		calls := []func() (store.Transfer, error){
			func() (store.Transfer, error) { return f.s.StartTransfer(ctx, original.ID) },
			func() (store.Transfer, error) { return f.s.SetTransferBytes(ctx, original.ID, 7) },
			func() (store.Transfer, error) {
				return f.s.FinishTransfer(ctx, original.ID, store.Ending{Status: store.StatusFailed, Reason: store.ReasonInterrupted, Bytes: 13})
			},
		}
		actual := make([]transferResult, len(calls))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Go(func() { <-start; actual[i].record, actual[i].err = call() })
		}
		close(start)
		wg.Wait()
		final, err := f.s.GetTransfer(ctx, "owner", original.ID)
		if err != nil {
			t.Fatal(err)
		}
		matched := false
		permutations(len(calls), func(order []int) {
			state := original
			expected := make([]transferResult, len(calls))
			for _, i := range order {
				switch i {
				case 0:
					if state.Status != store.StatusQueued {
						expected[i].err = store.ErrEnded
						continue
					}
					state.Status = store.StatusRunning
				case 1:
					if state.Status != store.StatusRunning {
						expected[i].err = store.ErrEnded
						continue
					}
					state.Bytes = 7
				case 2:
					state.Status = store.StatusFailed
					state.Reason = store.ReasonInterrupted
					state.Bytes = 13
					state.Finished = stamp
				}
				expected[i].record = state
			}
			if !reflect.DeepEqual(final, state) {
				return
			}
			for i := range actual {
				if !sameError(actual[i].err, expected[i].err) || !reflect.DeepEqual(actual[i].record, expected[i].record) {
					return
				}
			}
			matched = true
		})
		if !matched {
			t.Fatalf("no serial order matches %#v final %#v", actual, final)
		}
		checkT(t, f, final)
	}
}

// R-ZN8T-FT3F
func TestEveryMethodConcurrent(t *testing.T) {
	f := setup(t)
	random := &exclusiveReader{source: f.rnd}
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: random})
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 128)
	for range 8 {
		wg.Go(func() {
			<-start
			v, err := f.s.AddTransfer(ctx, transfer())
			if err != nil {
				errs <- err
				return
			}
			v, err = f.s.StartTransfer(ctx, v.ID)
			if err != nil {
				errs <- err
				return
			}
			v, err = f.s.SetTransferBytes(ctx, v.ID, 11)
			if err != nil {
				errs <- err
				return
			}
			v, err = f.s.FinishTransfer(ctx, v.ID, store.Ending{Status: store.StatusDone, Bytes: 11})
			if err != nil {
				errs <- err
				return
			}
			if _, err = f.s.GetTransfer(ctx, "owner", v.ID); err != nil {
				errs <- err
			}
			if _, err = f.s.ListTransfers(ctx, "owner"); err != nil {
				errs <- err
			}
			if _, err = f.s.UnfinishedTransfers(ctx); err != nil {
				errs <- err
			}
			if _, err = f.s.PruneTransfers(ctx, f.now, math.MaxInt64, 1); err != nil {
				errs <- err
			}
			d, err := f.s.AddDraft(ctx, draft())
			if err != nil {
				errs <- err
				return
			}
			d, err = f.s.AttachToDraft(ctx, d.ID, store.Attachment{Path: "path", Bytes: 3}, "attached", "thread")
			if err != nil {
				errs <- err
				return
			}
			d, err = f.s.SetDraftMessage(ctx, d.ID, "reconciled", "other-thread")
			if err != nil {
				errs <- err
				return
			}
			d, err = f.s.MarkDraftSent(ctx, d.ID, "sent", "sent-thread", time.Time{})
			if err != nil {
				errs <- err
				return
			}
			if _, err = f.s.GetDraft(ctx, "owner", d.ID); err != nil {
				errs <- err
			}
			if _, err = f.s.ListDrafts(ctx, "owner"); err != nil {
				errs <- err
			}
			if _, err = f.s.PruneDrafts(ctx, f.now, math.MaxInt64, 1); err != nil {
				errs <- err
			}
			d, err = f.s.AddDraft(ctx, draft())
			if err != nil {
				errs <- err
				return
			}
			if _, err = f.s.MarkDraftDiscarded(ctx, d.ID); err != nil {
				errs <- err
			}
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	ts, err := f.s.ListTransfers(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(ts), 8)
	ds, err := f.s.ListDrafts(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(ds), 16)
	for _, v := range ts {
		eq(t, v.Status, store.StatusDone)
		eq(t, v.Bytes, int64(11))
		checkT(t, f, v)
	}
	if random.overlap.Load() {
		t.Fatal("concurrent reads of random source")
	}
	for _, d := range ds {
		if d.Status != store.DraftSent && d.Status != store.DraftDiscarded {
			t.Fatal(d)
		}
		checkD(t, f, d)
	}
}

// Yield while a read is active so concurrent callers can expose overlap.
type exclusiveReader struct {
	source          *sequence
	active, overlap atomic.Bool
}

func (r *exclusiveReader) Read(p []byte) (int, error) {
	if !r.active.CompareAndSwap(false, true) {
		r.overlap.Store(true)
		return 0, errors.New("overlapping reads")
	}
	defer r.active.Store(false)
	runtime.Gosched()
	return r.source.Read(p)
}

// R-MTSU-Y1UL
func TestConcurrentProbeAndDiscard(t *testing.T) {
	for range 8 {
		f := setup(t)
		original := addD(t, f, draft())
		f.now = f.now.Add(time.Hour)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var probe, discard draftResult
		wg.Go(func() {
			<-start
			probe.record, probe.err = f.s.SetDraftMessage(ctx, original.ID, "new-message", "new-thread")
		})
		wg.Go(func() { <-start; discard.record, discard.err = f.s.MarkDraftDiscarded(ctx, original.ID) })
		close(start)
		wg.Wait()
		want := original
		if probe.err == nil {
			want.Message = "new-message"
			want.Thread = "new-thread"
			want.Link = urls.DraftLink(want.Message)
			eq(t, probe.record, want)
		} else {
			requireErr(t, probe.err, store.ErrNotOpen)
			eq(t, probe.record, store.Draft{})
		}
		if discard.err != nil {
			t.Fatal(discard.err)
		}
		want.Status = store.DraftDiscarded
		want.GmailDraft = ""
		want.Modified = f.now.UTC().Truncate(time.Second)
		want.Link = ""
		eq(t, discard.record, want)
		checkD(t, f, want)
	}
}
