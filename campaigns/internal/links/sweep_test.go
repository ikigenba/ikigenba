package links_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/campaigns/internal/links"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

var _ func(context.Context, links.Config, io.Writer) <-chan struct{} = links.StartSweep

type sweepMember struct {
	piece store.Piece
	first time.Time
}

func untoldMembers(t *testing.T, f *fixture, n int) []sweepMember {
	t.Helper()
	result := make([]sweepMember, 0, n)
	for i := range n {
		m, _, err := f.st.AddMember(context.Background(), store.MemberInput{UserID: "actor", Campaign: f.campaign.ID, Candidate: fmt.Sprintf("cand_%016x", i+1), Channels: []string{store.ChannelMail}})
		if err != nil {
			t.Fatal(err)
		}
		p := m.Pieces[0].Piece
		v, err := f.st.Visit(context.Background(), p.Token, store.VisitInput{})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, sweepMember{piece: p, first: v.First})
	}
	clearMarkers(t, f)
	return result
}

func clearMarkers(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.d.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE members SET opened_emitted = ''")
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

type writes struct{ calls [][]byte }

func (w *writes) Write(p []byte) (int, error) {
	w.calls = append(w.calls, append([]byte(nil), p...))
	return len(p), nil
}

func assertOpens(t *testing.T, f *fixture, members []sweepMember, tr []telemetry.Event, bus []events.Event) {
	t.Helper()
	want := make(map[string]string, len(members))
	for _, m := range members {
		want[m.piece.Campaign+":"+m.piece.Candidate] = m.first.UTC().Format(time.RFC3339)
	}
	if len(tr) != len(want) || len(bus) != len(want) {
		t.Fatalf("open counts: trail %d bus %d want %d", len(tr), len(bus), len(want))
	}
	seenTrail, seenBus := map[string]bool{}, map[string]bool{}
	for _, e := range tr {
		key := fmt.Sprint(e.Attrs["campaign"]) + ":" + fmt.Sprint(e.Attrs["candidate"])
		attrs := telemetry.Attrs{"campaign": e.Attrs["campaign"], "candidate": e.Attrs["candidate"], "at": want[key]}
		if e.Name != trail.EventMemberOpened || e.RequestID != "" || e.User != "" || seenTrail[key] || want[key] == "" || !reflect.DeepEqual(e.Attrs, attrs) {
			t.Fatalf("trail event: %+v", e)
		}
		seenTrail[key] = true
	}
	for _, e := range bus {
		key := fmt.Sprint(e.Attrs["campaign"]) + ":" + fmt.Sprint(e.Attrs["candidate"])
		attrs := events.Attrs{"campaign": e.Attrs["campaign"], "candidate": e.Attrs["candidate"], "at": want[key]}
		if e.Name != trail.EventMemberOpened || e.RequestID != "" || e.User != "" || seenBus[key] || want[key] == "" || !reflect.DeepEqual(e.Attrs, attrs) {
			t.Fatalf("bus event: %+v", e)
		}
		seenBus[key] = true
	}
	_, remaining, err := f.st.MarkOpened(context.Background(), store.MemberKey{}, len(members)+10)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining untold: %+v, %v", remaining, err)
	}
}

// R-U2I1-FFA5 R-UB1C-3TH0 R-UC98-HL7P R-UH4U-0O6H
func TestSweepCompletesEveryPageAndThenIsSilent(t *testing.T) {
	f := setup(t)
	var untyped float64 = links.SweepBatchSize
	if untyped != 500 {
		t.Fatalf("batch size = %v", untyped)
	}
	start := links.StartSweep
	members := untoldMembers(t, f, 2*links.SweepBatchSize+1)
	// A second visit does not replace the earlier first open.
	f.now = f.now.Add(time.Hour)
	if _, err := f.st.Visit(context.Background(), members[0].piece.Token, store.VisitInput{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.RejectMember(context.Background(), store.Rejection{UserID: "actor", Campaign: f.campaign.ID, Candidate: members[1].piece.Candidate, Why: "fixture rejection"}); err != nil {
		t.Fatal(err)
	}
	deleted, err := f.st.CreateCampaign(context.Background(), store.Campaign{Name: "deleted fixture", CreatedUserID: "actor", Channels: []string{store.ChannelMail}, Destination: "https://deleted.example/"})
	if err != nil {
		t.Fatal(err)
	}
	dm, _, err := f.st.AddMember(context.Background(), store.MemberInput{UserID: "actor", Campaign: deleted.ID, Candidate: "cand_2222222222222222", Channels: []string{store.ChannelMail}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.Visit(context.Background(), dm.Pieces[0].Piece.Token, store.VisitInput{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.DeleteCampaign(context.Background(), deleted.ID); err != nil {
		t.Fatal(err)
	}
	clearMarkers(t, f)
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "owner", RequestID: "sweep-inherited-request"})
	var out writes
	done := start(ctx, f.config(), &out)
	await(t, done)
	await(t, done) // Closing broadcasts completion, including to later receivers.
	if !reflect.DeepEqual(out.calls, [][]byte{[]byte("campaigns: opened events sent\n")}) {
		t.Fatalf("completion writes = %q", out.calls)
	}
	assertOpens(t, f, members, f.capture.Events(), f.bus.Events())
	var silent writes
	await(t, start(ctx, f.config(), &silent))
	if len(silent.calls) != 0 || len(f.capture.Events()) != len(members) || len(f.bus.Events()) != len(members) {
		t.Fatalf("caught-up sweep wrote %q or emitted more", silent.calls)
	}
	await(t, start(ctx, f.config(), nil))
}

// R-UH4U-0O6H
func TestSweepDoesNotRetellLiveFirstOpen(t *testing.T) {
	f := setup(t)
	answer(t, serve(f.h, "/t/"+f.piece.Token, identity.Caller{RequestID: "live-request"}), f.campaign.Destination)
	flushBoth(t, f)
	var out writes
	await(t, links.StartSweep(context.Background(), f.config(), &out))
	if len(out.calls) != 0 || len(f.capture.Events()) != 2 || len(f.bus.Events()) != 1 {
		t.Fatalf("live open retold: %q %+v %+v", out.calls, f.capture.Events(), f.bus.Events())
	}
}

type heldWrite struct {
	entered chan struct{}
	release chan struct{}
	calls   [][]byte
}

func (w *heldWrite) Write(p []byte) (int, error) {
	w.calls = append(w.calls, append([]byte(nil), p...))
	close(w.entered)
	<-w.release
	return len(p), nil
}

// R-UC98-HL7P
func TestSweepClosesOnlyAfterCompletionWriteReturns(t *testing.T) {
	f := setup(t)
	members := untoldMembers(t, f, 1)
	out := &heldWrite{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-out.release:
		default:
			close(out.release)
		}
	})
	done := links.StartSweep(context.Background(), f.config(), out)
	await(t, out.entered)
	assertOpens(t, f, members, f.capture.Events(), f.bus.Events())
	select {
	case <-done:
		t.Fatal("closed before Write returned")
	default:
	}
	if !reflect.DeepEqual(out.calls, [][]byte{[]byte("campaigns: opened events sent\n")}) {
		t.Fatalf("writes %q", out.calls)
	}
	close(out.release)
	await(t, done)
}

// R-UDH4-VCYE
func TestSweepFailureIsOneWriteAndCloses(t *testing.T) {
	for _, nilOutput := range []bool{false, true} {
		t.Run(fmt.Sprint(nilOutput), func(t *testing.T) {
			f := setup(t)
			untoldMembers(t, f, 1)
			f.d.SetFailing(true)
			_, _, expected := f.st.MarkOpened(context.Background(), store.MemberKey{}, links.SweepBatchSize)
			if expected == nil {
				t.Fatal("store did not fail")
			}
			var out writes
			var stderr io.Writer = &out
			if nilOutput {
				stderr = nil
			}
			await(t, links.StartSweep(context.Background(), f.config(), stderr))
			flushBoth(t, f)
			if len(f.capture.Events()) != 0 || len(f.bus.Events()) != 0 {
				t.Fatal("failed page emitted an event")
			}
			if !nilOutput {
				want := "campaigns: opened events failed\n\n" + strings.ReplaceAll(expected.Error(), "\n", " ") + "\n"
				if !reflect.DeepEqual(out.calls, [][]byte{[]byte(want)}) {
					t.Fatalf("failure writes = %q want %q", out.calls, want)
				}
			}
		})
	}
}

// R-UEP1-94P3
func TestSweepAlreadyCancelledChangesNothing(t *testing.T) {
	f := setup(t)
	members := untoldMembers(t, f, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out writes
	await(t, links.StartSweep(ctx, f.config(), &out))
	flushBoth(t, f)
	if len(out.calls) != 0 || len(f.capture.Events()) != 0 || len(f.bus.Events()) != 0 {
		t.Fatalf("cancelled sweep wrote or emitted: %q", out.calls)
	}
	_, remaining, err := f.st.MarkOpened(context.Background(), store.MemberKey{}, links.SweepBatchSize)
	if err != nil || len(remaining) != 1 || remaining[0].Candidate != members[0].piece.Candidate {
		t.Fatalf("cancelled sweep marked: %+v %v", remaining, err)
	}
}

// R-UB1C-3TH0 R-UFWX-MWFS
func TestSweepFlushesBothSinksBeforeMarkingNextPage(t *testing.T) {
	for _, blockTrail := range []bool{true, false} {
		t.Run(fmt.Sprint(blockTrail), func(t *testing.T) {
			f := setup(t)
			members := untoldMembers(t, f, 2*links.SweepBatchSize+1)
			tr, bus := blockedSinks(t, f, blockTrail, !blockTrail)
			var out writes
			done := links.StartSweep(context.Background(), f.config(), &out)
			if blockTrail {
				await(t, tr.entered)
			} else {
				await(t, bus.entered)
			}
			select {
			case <-done:
				t.Fatal("sweep returned closed channel while sink blocks")
			default:
			}
			p := members[links.SweepBatchSize].piece
			answer(t, serve(f.h, "/t/"+p.Token, identity.Caller{UserID: "owner", RequestID: "between-pages"}), f.campaign.Destination)
			if blockTrail {
				close(tr.release)
			} else {
				close(bus.release)
			}
			await(t, done)
			trEvents, busEvents := tr.capture.Events(), bus.capture.Events()
			checkPageRace(t, members, trEvents, busEvents, p.Candidate)
			if !reflect.DeepEqual(out.calls, [][]byte{[]byte("campaigns: opened events sent\n")}) {
				t.Fatalf("writes = %q", out.calls)
			}
		})
	}
}

func checkPageRace(t *testing.T, members []sweepMember, tr []telemetry.Event, bus []events.Event, live string) {
	t.Helper()
	opened := make([]telemetry.Event, 0, len(members))
	for _, e := range tr {
		if e.Name == trail.EventMemberOpened {
			opened = append(opened, e)
		}
	}
	if len(opened) != len(members) || len(bus) != len(members) {
		t.Fatalf("opens: trail %d bus %d want %d", len(opened), len(bus), len(members))
	}
	seen := make(map[string]int)
	for _, e := range opened {
		candidate := fmt.Sprint(e.Attrs["candidate"])
		seen[candidate]++
		q := ""
		if candidate == live {
			q = "between-pages"
		}
		if e.RequestID != q {
			t.Fatalf("candidate %s request = %q want %q", candidate, e.RequestID, q)
		}
	}
	busSeen := make(map[string]int)
	for _, e := range bus {
		candidate := fmt.Sprint(e.Attrs["candidate"])
		busSeen[candidate]++
		q := ""
		if candidate == live {
			q = "between-pages"
		}
		if e.RequestID != q {
			t.Fatalf("bus candidate %s request = %q want %q", candidate, e.RequestID, q)
		}
	}
	for _, m := range members {
		if seen[m.piece.Candidate] != 1 || busSeen[m.piece.Candidate] != 1 {
			t.Fatalf("candidate %s counts %d %d", m.piece.Candidate, seen[m.piece.Candidate], busSeen[m.piece.Candidate])
		}
	}
}

// R-UEP1-94P3
func TestSweepCancelledDuringFlushLeavesLaterPageUntold(t *testing.T) {
	for _, size := range []int{1, 2*links.SweepBatchSize + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f := setup(t)
			members := untoldMembers(t, f, size)
			tr, bus := blockedSinks(t, f, false, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out writes
			done := links.StartSweep(ctx, f.config(), &out)
			await(t, bus.entered)
			cancel()
			await(t, done)
			if len(out.calls) != 0 {
				t.Fatalf("cut-off sweep wrote %q", out.calls)
			}
			if size > links.SweepBatchSize {
				answer(t, serve(f.h, "/t/"+members[len(members)-1].piece.Token, identity.Caller{RequestID: "last-page-live"}), f.campaign.Destination)
				flush(t, f)
				found := false
				for _, e := range tr.capture.Events() {
					if e.Name == trail.EventMemberOpened && e.Attrs["candidate"] == members[len(members)-1].piece.Candidate && e.RequestID == "last-page-live" {
						found = true
					}
				}
				if !found {
					t.Fatal("later page was already marked")
				}
			}
			close(bus.release)
		})
	}
}

// R-UICQ-EFX6
func TestTwoSweepsAndVisitsShareOneMarker(t *testing.T) {
	f := setup(t)
	members := untoldMembers(t, f, 30)
	members = append(members, sweepMember{piece: f.piece, first: f.now.UTC().Truncate(time.Second)})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { <-start; await(t, links.StartSweep(context.Background(), f.config(), nil)) })
	}
	wg.Go(func() {
		<-start
		answer(t, serve(f.h, "/t/"+f.piece.Token, identity.Caller{RequestID: "new-member-race"}), f.campaign.Destination)
	})
	for i := range 12 {
		wg.Go(func() {
			<-start
			answer(t, serve(f.h, "/t/"+members[i].piece.Token, identity.Caller{RequestID: fmt.Sprintf("race-%d", i)}), f.campaign.Destination)
		})
	}
	close(start)
	wg.Wait()
	flushBoth(t, f)
	tr, bus := f.capture.Events(), f.bus.Events()
	seenTrail, seenBus := map[string]int{}, map[string]int{}
	for _, e := range tr {
		if e.Name == trail.EventMemberOpened {
			seenTrail[fmt.Sprint(e.Attrs["candidate"])]++
		}
	}
	for _, e := range bus {
		if e.Name != trail.EventMemberOpened {
			t.Fatalf("unexpected bus event %+v", e)
		}
		seenBus[fmt.Sprint(e.Attrs["candidate"])]++
	}
	if len(seenTrail) != len(members) || len(seenBus) != len(members) {
		t.Fatalf("unique opens = %d %d want %d", len(seenTrail), len(seenBus), len(members))
	}
	for _, m := range members {
		if seenTrail[m.piece.Candidate] != 1 || seenBus[m.piece.Candidate] != 1 {
			t.Fatalf("candidate %s opens %d %d", m.piece.Candidate, seenTrail[m.piece.Candidate], seenBus[m.piece.Candidate])
		}
	}
}
