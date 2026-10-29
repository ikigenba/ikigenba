package passes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-6OKQ-7I6S R-6PSM-L9XH R-M015-H2RX R-M191-UUIM R-YJKZ-96BR R-YOGK-S9AJ
func TestWebsitePassDeduplicatesFullInputs(t *testing.T) {
	t.Parallel()
	for _, targeted := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "targeted"}[targeted], func(t *testing.T) {
			f := newFixture(t, nil, false)
			ctx := context.Background()
			emails := []string{"owner@email.test", "owner@other-email.test", "owner@email.test"}
			observations := make([]store.Observation, len(emails))
			foundPlaces := make([]places.Found, len(emails))
			reportedURL := "https://reported.test/about"
			for i, email := range emails {
				name, phone := fmt.Sprintf("Reported business %d", i), fmt.Sprintf("320555012%d", i)
				observations[i] = store.Observation{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: name, Phone: phone, PhoneKey: phone, Email: email}}
				// Phone matching keeps these candidates distinct despite sharing a
				// reported URL. Two full inputs match and the third differs.
				foundPlaces[i] = places.Found{Place: places.Place{ID: fmt.Sprintf("reported-place-%d", i), Name: name, Website: reportedURL, Phone: phone}}
			}
			_, ids, err := f.st.Attach(ctx, "seed", false, observations)
			if err != nil || len(ids) != len(emails) {
				t.Fatal(ids, err)
			}
			ks := seedWalkParks(t, f, 1)
			walk, err := f.st.AddPass(ctx, store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.st.RecordSweep(ctx, store.Sweep{Pass: walk.ID, Park: ks[0].ID, Found: foundPlaces}); err != nil {
				t.Fatal(err)
			}
			if _, err = f.st.FinishWalk(ctx, walk.ID, store.Counts{}, false); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			requests := map[string]int{}
			f.checker(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				requests[r.URL.String()]++
				mu.Unlock()
				return pageResponse(r), nil
			})
			var p store.Pass
			if targeted {
				if _, _, err = f.st.JudgeAll(ctx, "", 100, walk.Started); err != nil {
					t.Fatal(err)
				}
				list, err := f.st.CreateProspectList(ctx, store.ProspectList{Name: "Full input targets", Size: int64(len(ids)), CreatedUserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				p, err = f.eng.EnrichTarget(ctx, identity.Caller{UserID: "owner"}, sources.WebsiteCheck, list.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				p = f.ended(p.ID)
			} else {
				p = f.enrich(sources.WebsiteCheck)
			}
			if p.Status != store.StatusDone || p.Due != 3 || p.Fetched != 2 || p.HTTPS != 3 || p.HTTP+p.None != 0 {
				t.Fatal(p)
			}
			for i, id := range ids {
				es, err := f.st.Enrichments(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				wantInput := website.Inputs(reportedURL, emails[i])
				if len(es) != 1 || es[0].Inputs != wantInput || es[0].Source != wantInput || es[0].Cost != 0 || es[0].At != p.Started {
					t.Fatal(es, wantInput)
				}
				var found website.Result
				if err := json.Unmarshal(es[0].Found, &found); err != nil || found.URL != reportedURL || found.State != sources.WebsiteHTTPS {
					t.Fatal(found, err)
				}
			}
			waitFollow(t, f, p.ID)
			wantRequests := map[string]int{reportedURL: 2}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(requests, wantRequests) {
				t.Fatal(requests, wantRequests)
			}
		})
	}
}
