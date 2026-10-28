package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// WalkArgs selects parks and whether to repeat a previous walk.
type WalkArgs struct {
	Parks []string `json:"parks" mcp:"required"`
	Again bool     `json:"again"`
}

// ParkWalk describes one recorded walk of a park.
type ParkWalk struct {
	Pass           string `json:"pass"`
	Walked         string `json:"walked"`
	Circles        int64  `json:"circles"`
	Requests       int64  `json:"requests"`
	Found          int64  `json:"found"`
	NewCandidates  int64  `json:"new_candidates"`
	Cost           int64  `json:"cost"`
	CeilingReached bool   `json:"ceiling_reached"`
}

// Place is a place found in a park.
type Place struct {
	PlaceID     string `json:"place_id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	PrimaryType string `json:"primary_type"`
	Website     string `json:"website"`
	Phone       string `json:"phone"`
	Walk        string `json:"walk"`
	Candidate   string `json:"candidate"`
	New         bool   `json:"new"`
}

// FoundCandidate identifies a business found in a park.
type FoundCandidate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	New    bool   `json:"new"`
}

// ParkRef identifies a candidate's founding park.
type ParkRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ParkWalkRef describes a park covered by a walk pass.
type ParkWalkRef struct {
	Park           string `json:"park"`
	Name           string `json:"name"`
	Found          int64  `json:"found"`
	NewCandidates  int64  `json:"new_candidates"`
	Requests       int64  `json:"requests"`
	Cost           int64  `json:"cost"`
	CeilingReached bool   `json:"ceiling_reached"`
}

// PassDetail extends a walk pass with its parks.
type PassDetail struct {
	Pass  passes.Record
	Parks []ParkWalkRef
}

// MarshalJSON appends parks only for walk passes.
func (v PassDetail) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(v.Pass)
	if err != nil || v.Parks == nil {
		return b, err
	}
	parks, err := json.Marshal(v.Parks)
	if err != nil {
		return nil, err
	}
	b = append(b[:len(b)-1], []byte(`,"parks":`)...)
	b = append(b, parks...)
	return append(b, '}'), nil
}

func registerWalk(srv *mcp.Server, cfg Config) {
	add(srv, "walk", DescribeWalk, mcp.Additive, func(ctx context.Context, c identity.Caller, a WalkArgs) mcp.Result {
		p, err := cfg.Passes.Walk(ctx, c, a.Parks, a.Again)
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return engineRefusal(r)
			}
			return refuse(passes.RefusalUnavailable)
		}
		return answer(passes.RecordOf(p))
	})
}

func passAnswer(ctx context.Context, st *store.Store, p store.Pass) mcp.Result {
	out := PassDetail{Pass: passes.RecordOf(p)}
	if p.Kind == store.KindWalk {
		walks, err := st.PassWalks(ctx, p.ID)
		if err != nil {
			return failure(err)
		}
		out.Parks = []ParkWalkRef{}
		for _, w := range walks {
			out.Parks = append(out.Parks, ParkWalkRef{w.Park, w.Name, w.Found, w.NewCandidates, w.Requests, w.Cost, w.CeilingReached})
		}
	}
	return answer(out)
}

func parkDetailAnswer(ctx context.Context, st *store.Store, out ParkDetail) mcp.Result {
	walks, err := st.ParkWalks(ctx, out.Park.ID)
	if err != nil {
		return failure(err)
	}
	out.Walks = []ParkWalk{}
	for _, w := range walks {
		out.Walks = append(out.Walks, ParkWalk{w.Pass, stamp(w.Walked), w.Circles, w.Requests, w.Found, w.NewCandidates, w.Cost, w.CeilingReached})
	}
	places, err := st.ParkPlaces(ctx, out.Park.ID)
	if err != nil {
		return failure(err)
	}
	out.Places = []Place{}
	for _, x := range places {
		out.Places = append(out.Places, Place{x.PlaceID, x.Name, x.Address, x.PrimaryType, x.Website, x.Phone, x.Walk, x.Candidate, x.New})
	}
	found, err := st.ParkCandidates(ctx, out.Park.ID)
	if err != nil {
		return failure(err)
	}
	out.FoundCandidates = []FoundCandidate{}
	for _, f := range found {
		out.FoundCandidates = append(out.FoundCandidates, FoundCandidate{f.ID, f.Name, f.Status, f.New})
	}
	return answer(out)
}
