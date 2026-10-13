package drafts_test

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

func operation(f *draftFixture, which string, id string) (store.Draft, error) {
	switch which {
	case "get":
		return f.svc.Get(f.ctx, f.caller, id)
	case "send":
		return f.svc.Send(f.ctx, f.caller, id)
	case "discard":
		return f.svc.Discard(f.ctx, f.caller, id)
	default:
		return f.svc.Attachable(f.ctx, f.caller, f.token, id, store.Attachment{Name: "part", Bytes: 1})
	}
}

func TestLookupAndClosedDrafts(t *testing.T) {
	// R-K8K1-673R R-K9RX-JYUG R-JP1N-1V8N
	for _, which := range []string{"get", "send", "discard", "attach"} {
		t.Run(which, func(t *testing.T) {
			f := fixture(t)
			d := f.add(t)
			f.caller.UserID = "other"
			got, e := operation(f, which, d.ID)
			refused(t, got, e, transfers.RefusalNotFound, "")
			f.caller.UserID = "owner"
			got, e = operation(f, which, "unknown")
			refused(t, got, e, transfers.RefusalNotFound, "")
			f.db.SetFailing(true)
			got, e = operation(f, which, d.ID)
			var r *transfers.Refusal
			if !reflect.DeepEqual(got, store.Draft{}) || !errors.Is(e, store.ErrUnreachable) || errors.As(e, &r) {
				t.Fatalf("db error %#v %v", got, e)
			}
			f.db.SetFailing(false)
			ended, e := f.st.MarkDraftDiscarded(f.ctx, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			before := f.tokenCalls
			got, e = operation(f, which, d.ID)
			if which == "get" {
				if e != nil || !reflect.DeepEqual(got, ended) || f.tokenCalls != before {
					t.Fatalf("closed get %#v %v", got, e)
				}
			} else {
				refused(t, got, e, transfers.RefusalNotOpen, "")
			}
			if len(f.snapshot()) != 0 {
				t.Fatal(f.snapshot())
			}
			f.noEvents(t)
		})
	}
}

func TestProbeRefreshAndErrors(t *testing.T) {
	// R-5XFM-CC7G R-676T-EI50
	for _, which := range []string{"get", "send", "discard", "attach"} {
		for _, status := range []int{200, 401, 500} {
			t.Run(which+http.StatusText(status), func(t *testing.T) {
				f := fixture(t)
				d := f.add(t)
				f.setHandler(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						if status == 200 {
							writeJSON(w, 200, map[string]any{"id": "provider-draft", "message": map[string]any{"id": "edited-message", "threadId": "edited-thread"}})
						} else {
							w.WriteHeader(status)
						}
					} else {
						w.WriteHeader(500)
					}
				})
				got, e := operation(f, which, d.ID)
				if status != 200 {
					word, url := transfers.RefusalProvider, ""
					if status == 401 {
						word, url = transfers.RefusalReconnect, f.cfg.ConnectURL
					}
					refused(t, got, e, word, url)
				} else if which == "get" || which == "attach" {
					if e != nil || got.Message != "edited-message" || got.Thread != "edited-thread" || got.Modified != d.Modified || got.Link == d.Link {
						t.Fatalf("refresh %#v %v", got, e)
					}
				}
				held, e := f.st.GetDraft(f.ctx, f.caller.UserID, d.ID)
				if e != nil {
					t.Fatal(e)
				}
				if status != 200 && !reflect.DeepEqual(held, d) {
					t.Fatalf("changed %#v", held)
				}
				f.noEvents(t)
			})
		}
	}
}

func threadAnswer(w http.ResponseWriter, status int, values ...map[string]any) {
	if status != 200 {
		w.WriteHeader(status)
		return
	}
	if values == nil {
		values = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"id": "draft-thread", "messages": values})
}
func stat(id string, ms string, sent bool) map[string]any {
	labels := []string{}
	if sent {
		labels = append(labels, "SENT")
	}
	return map[string]any{"id": id, "threadId": "draft-thread", "internalDate": ms, "labelIds": labels}
}

func TestReconciliation(t *testing.T) {
	// R-5YNI-Q3Y5 R-5ZVF-3VOU R-613B-HNFJ R-62B7-VF68
	for _, which := range []string{"get", "send", "discard", "attach"} {
		for _, state := range []string{"sent", "same-time", "unsent", "missing", "unauthorized", "failure"} {
			t.Run(which+state, func(t *testing.T) {
				f := fixture(t)
				d := f.add(t)
				f.setHandler(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/drafts/") {
						w.WriteHeader(404)
						return
					}
					switch state {
					case "sent":
						threadAnswer(w, 200, stat("earlier", "1700000001000", true), stat("tie-first", "1700000002000", true), stat("tie-last", "1700000002000", true), stat("newer-unsent", "1700000003000", false))
					case "same-time":
						threadAnswer(w, 200, stat("past", "1699999999000", true), stat("same", "1700000000000", true))
					case "unsent":
						threadAnswer(w, 200, stat("unsent", "1700000002000", false))
					case "missing":
						threadAnswer(w, 404)
					case "unauthorized":
						threadAnswer(w, 401)
					default:
						threadAnswer(w, 500)
					}
				})
				got, e := operation(f, which, d.ID)
				held, he := f.st.GetDraft(f.ctx, f.caller.UserID, d.ID)
				if he != nil {
					t.Fatal(he)
				}
				if state == "unauthorized" || state == "failure" {
					word, url := transfers.RefusalProvider, ""
					if state == "unauthorized" {
						word, url = transfers.RefusalReconnect, f.cfg.ConnectURL
					}
					refused(t, got, e, word, url)
					if !reflect.DeepEqual(held, d) {
						t.Fatal("failed probe changed draft")
					}
					f.noEvents(t)
					return
				}
				wantStatus, name := store.DraftDiscarded, trail.DraftDiscarded
				if state == "sent" {
					wantStatus, name = store.DraftSent, trail.DraftSent
					if held.Message != "tie-last" || !held.Sent.Equal(time.Unix(1700000002, 0)) {
						t.Fatal(held)
					}
				} else if held.Message != d.Message {
					t.Fatal(held)
				}
				if held.Status != wantStatus {
					t.Fatal(held)
				}
				if which == "get" || which == "discard" && wantStatus == store.DraftDiscarded {
					if e != nil || !reflect.DeepEqual(got, held) {
						t.Fatalf("answer %#v %v", got, e)
					}
				} else {
					refused(t, got, e, transfers.RefusalNotOpen, "")
				}
				requests := f.snapshot()
				if len(requests) != 2 || requests[0].Method != "GET" || requests[1].Method != "GET" || !strings.HasSuffix(requests[1].Path, "/threads/draft-thread") {
					t.Fatal(requests)
				}
				f.events(t, name, held)
				again, e := f.svc.Get(f.ctx, f.caller, d.ID)
				if e != nil || !reflect.DeepEqual(again, held) {
					t.Fatalf("again %#v %v", again, e)
				}
				f.events(t, name, held)
			})
		}
	}
}

func TestBusyAndFinish(t *testing.T) {
	// R-L7ZX-HUWJ R-69MM-61ME R-63J4-96WX R-KN6T-RG03 R-6DAB-BCUH R-676T-EI50
	for _, which := range []string{"send", "discard"} {
		t.Run(which, func(t *testing.T) {
			f := fixture(t)
			d := f.add(t)
			tr, e := f.st.AddTransfer(f.ctx, store.Transfer{Status: store.StatusQueued, Direction: store.DirectionPush, Draft: d.ID, UserID: f.caller.UserID, Message: d.Message, Path: "part", Name: "part"})
			if e != nil {
				t.Fatal(e)
			}
			got, e := operation(f, which, d.ID)
			refused(t, got, e, transfers.RefusalBusy, "")
			f.noEvents(t)
			if len(f.snapshot()) != 1 {
				t.Fatal(f.snapshot())
			}
			_, e = f.st.StartTransfer(f.ctx, tr.ID)
			if e != nil {
				t.Fatal(e)
			}
			got, e = operation(f, which, d.ID)
			refused(t, got, e, transfers.RefusalBusy, "")
			_, e = f.st.FinishTransfer(f.ctx, tr.ID, store.Ending{Status: store.StatusDone, Bytes: 1})
			if e != nil {
				t.Fatal(e)
			}
			got, e = operation(f, which, d.ID)
			if e != nil {
				t.Fatal(e)
			}
			held, e := f.st.GetDraft(f.ctx, f.caller.UserID, d.ID)
			if e != nil || !reflect.DeepEqual(got, held) {
				t.Fatalf("stored %#v %v", held, e)
			}
			name := trail.DraftDiscarded
			if which == "send" {
				name = trail.DraftSent
				if got.Status != store.DraftSent || got.Message != "sent-message" || got.Thread != "sent-thread" || got.GmailDraft != "" || got.Link == "" {
					t.Fatal(got)
				}
			} else if got.Status != store.DraftDiscarded || got.Message != d.Message || got.GmailDraft != "" || got.Link != "" {
				t.Fatal(got)
			}
			f.events(t, name, got)
			requests := f.snapshot()
			count := 0
			for _, r := range requests {
				if r.Method != "GET" {
					count++
				}
			}
			if count != 1 {
				t.Fatal(requests)
			}
		})
	}
}

func TestFailedFinishAndSecondProbe(t *testing.T) {
	// R-XFR4-5N5K R-64R0-MYNM
	for _, which := range []string{"send", "discard"} {
		for _, status := range []int{401, 500, 404} {
			for _, second := range []int{200, 404, 401, 500} {
				t.Run(which+http.StatusText(status)+http.StatusText(second), func(t *testing.T) {
					f := fixture(t)
					d := f.add(t)
					probes := 0
					f.setHandler(func(w http.ResponseWriter, r *http.Request) {
						if strings.Contains(r.URL.Path, "/threads/") {
							threadAnswer(w, 200)
							return
						}
						if r.Method == "GET" {
							probes++
							p := 200
							if probes > 1 {
								p = second
							}
							if p == 200 {
								writeJSON(w, p, map[string]any{"id": "provider-draft", "message": map[string]any{"id": "refreshed-message", "threadId": "refreshed-thread"}})
							} else {
								w.WriteHeader(p)
							}
							return
						}
						w.WriteHeader(status)
					})
					got, e := operation(f, which, d.ID)
					held, he := f.st.GetDraft(f.ctx, f.caller.UserID, d.ID)
					if he != nil {
						t.Fatal(he)
					}
					if status == 404 && second == 404 {
						if which == "discard" {
							if e != nil || got.Status != store.DraftDiscarded {
								t.Fatalf("discard %#v %v", got, e)
							}
						} else {
							refused(t, got, e, transfers.RefusalNotOpen, "")
						}
						f.events(t, trail.DraftDiscarded, held)
						return
					}
					word, url := transfers.RefusalProvider, ""
					if status == 401 || status == 404 && second == 401 {
						word, url = transfers.RefusalReconnect, f.cfg.ConnectURL
					}
					refused(t, got, e, word, url)
					if held.Status != store.DraftOpen || held.Message != "refreshed-message" || held.Modified != d.Modified {
						t.Fatal(held)
					}
					if status != 404 && probes != 1 || status == 404 && probes != 2 {
						t.Fatalf("probes %d", probes)
					}
					f.noEvents(t)
				})
			}
		}
	}
}

func TestAttachableCapAndNoToken(t *testing.T) {
	// R-XJET-AYDN R-6AUI-JTD3 R-KTAB-OAPK
	f := fixture(t)
	d := f.add(t)
	a := store.Attachment{Name: "part", ContentType: "application/octet-stream", Bytes: gmail.UploadCap}
	got, e := f.svc.Attachable(f.ctx, f.caller, f.token, d.ID, a)
	refused(t, got, e, transfers.RefusalTooLarge, "")
	if f.tokenCalls != 0 || len(f.snapshot()) != 0 {
		t.Fatal("cap made requests")
	}
	_, e = f.st.MarkDraftDiscarded(f.ctx, d.ID)
	if e != nil {
		t.Fatal(e)
	}
	got, e = f.svc.Attachable(f.ctx, f.caller, f.token, d.ID, a)
	refused(t, got, e, transfers.RefusalTooLarge, "")
	got, e = f.svc.Attachable(f.ctx, f.caller, f.token, "unknown", a)
	refused(t, got, e, transfers.RefusalNotFound, "")
	f = fixture(t)
	d = f.add(t)
	a.Bytes = 1
	got, e = f.svc.Attachable(f.ctx, f.caller, connections.Token{AccessToken: f.token.AccessToken, Account: f.token.Account}, d.ID, a)
	if e != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("attach %#v %v", got, e)
	}
	if f.tokenCalls != 0 {
		t.Fatal("attach asked token")
	}
	f.noEvents(t)
}

func TestConcurrentSendAndDiscard(t *testing.T) {
	// R-5V1U-CX76
	f := fixture(t)
	d := f.add(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, which := range []string{"send", "discard"} {
		wg.Go(func() { <-start; _, e := operation(f, which, d.ID); results <- e })
	}
	close(start)
	wg.Wait()
	close(results)
	successes, refusals := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else {
			var r *transfers.Refusal
			if !errors.As(e, &r) || r.Word != transfers.RefusalNotOpen {
				t.Fatal(e)
			}
			refusals++
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatalf("success %d refusal %d", successes, refusals)
	}
}

func TestStoreFailureAtWrites(t *testing.T) {
	// R-5XFM-CC7G R-613B-HNFJ R-6C2E-XL3S
	for _, which := range []string{"get", "send", "discard", "attach"} {
		for _, phase := range []string{"refresh", "reconcile", "finish"} {
			for _, failure := range []string{"failing", "ended"} {
				if phase == "finish" && (which == "get" || which == "attach") {
					continue
				}
				t.Run(which+phase+failure, func(t *testing.T) {
					f := fixture(t)
					d := f.add(t)
					mutate := func() {
						if failure == "failing" {
							f.db.SetFailing(true)
						} else {
							if _, e := f.st.MarkDraftDiscarded(f.ctx, d.ID); e != nil {
								t.Error(e)
							}
						}
					}
					f.setHandler(func(w http.ResponseWriter, r *http.Request) {
						if strings.Contains(r.URL.Path, "/threads/") {
							mutate()
							threadAnswer(w, 200)
							return
						}
						if r.Method == "GET" {
							if phase == "reconcile" {
								w.WriteHeader(404)
								return
							}
							if phase == "refresh" {
								mutate()
								writeJSON(w, 200, map[string]any{"id": "provider-draft", "message": map[string]any{"id": "changed-message", "threadId": "changed-thread"}})
							} else {
								fakeCreate(w)
							}
							return
						}
						mutate()
						if r.Method == "DELETE" {
							w.WriteHeader(204)
						} else {
							writeJSON(w, 200, map[string]any{"id": "sent-message", "threadId": "sent-thread"})
						}
					})
					got, e := operation(f, which, d.ID)
					switch {
					case failure == "failing":
						var r *transfers.Refusal
						if !reflect.DeepEqual(got, store.Draft{}) || !errors.Is(e, store.ErrUnreachable) || errors.As(e, &r) {
							t.Fatal(got, e)
						}
						f.db.SetFailing(false)
					case which == "get":
						if e != nil || got.Status != store.DraftDiscarded {
							t.Fatal(got, e)
						}
					default:
						refused(t, got, e, transfers.RefusalNotOpen, "")
					}
					f.noEvents(t)
				})
			}
		}
	}
}

func TestExactCapAccepted(t *testing.T) {
	// R-XJET-AYDN R-6AUI-JTD3
	f := fixture(t)
	d := f.add(t)
	a := store.Attachment{Name: "part", ContentType: "application/octet-stream"}
	size := func(n int64, name string) int64 {
		c := transfers.Composition(d, f.token.Account)
		c.Attachments = append(c.Attachments, gmail.Part{Name: name, ContentType: a.ContentType, Bytes: n})
		return gmail.Build(c).Size()
	}
	lo, hi := int64(0), int64(gmail.UploadCap)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if size(mid, a.Name) <= gmail.UploadCap {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	a.Bytes = lo
	a.Name += strings.Repeat("x", int(gmail.UploadCap-size(lo, a.Name)))
	if size(a.Bytes, a.Name) != gmail.UploadCap {
		t.Fatal("fixture size is not cap")
	}
	got, e := f.svc.Attachable(f.ctx, f.caller, f.token, d.ID, a)
	if e != nil || !reflect.DeepEqual(got, d) {
		t.Fatal(got, e)
	}
	a.Name += "x"
	got, e = f.svc.Attachable(f.ctx, f.caller, f.token, d.ID, a)
	refused(t, got, e, transfers.RefusalTooLarge, "")
	if len(f.snapshot()) != 1 || f.tokenCalls != 0 {
		t.Fatal("cap lookup reached dependency")
	}
	f.noEvents(t)
}

func TestConcurrentMethods(t *testing.T) {
	// R-5V1U-CX76
	f := fixture(t)
	d := f.add(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			_, _ = f.svc.Get(f.ctx, f.caller, d.ID)
			_, _ = f.svc.Attachable(f.ctx, f.caller, f.token, d.ID, store.Attachment{Name: "part", Bytes: 1})
			_, _ = f.svc.Compose(f.ctx, f.caller, drafts.ComposeRequest{To: []string{"recipient@example.invalid"}})
			_, _ = f.svc.Send(f.ctx, f.caller, d.ID)
			_, _ = f.svc.Discard(f.ctx, f.caller, d.ID)
		})
	}
	wg.Wait()
}
