package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-ZYWL-UYRQ
func TestUnchangedAttachPreservesCompleteCandidate(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			name := "unforced"
			if force {
				name = "forced"
			}
			if reverse {
				name += "/reverse"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				f := contractDB(t)
				var obs []store.Observation
				for i, id := range []string{"c", "a", "b"} {
					o := contractObservation(id, []string{"Zulu", "Alpha", "Middle"}[i], f.now)
					o.Reading.EINKey = "shared-ein"
					o.Reading.Street = id + " Main St"
					o.Reading.Phone = "320555010" + id
					o.Reading.Email = id + "@business.test"
					obs = append(obs, o)
				}
				id := contractAttach(t, f, "initial", obs...)[0]
				before, err := f.s.GetCandidate(ctx, id)
				contractOK(t, err)
				order := []int{0, 1, 2}
				if reverse {
					order = []int{2, 1, 0}
				}
				for _, offset := range []time.Duration{time.Second, -time.Hour, 24 * time.Hour} {
					for _, i := range order {
						f.now = f.now.Add(offset)
						o := obs[i]
						if force {
							o.Digest = "new-digest"
						}
						_, _, err = f.s.Attach(ctx, "next", force, []store.Observation{o})
						contractOK(t, err)
						after, err := f.s.GetCandidate(ctx, id)
						contractOK(t, err)
						contractEqual(t, after, before)
					}
				}
				f.reopen()
				after, err := f.s.GetCandidate(ctx, id)
				contractOK(t, err)
				contractEqual(t, after, before)
			})
		}
	}
}
