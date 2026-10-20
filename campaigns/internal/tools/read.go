package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (h handlers) campaigns(ctx context.Context, _ identity.Caller, in CampaignsArgs) (out CampaignList, err error) {
	var k, i string
	z, e := window("campaigns", in.Page, in.Limit, &k, &i)
	if e != nil {
		return out, e
	}
	w, e := h.Store.ListCampaigns(ctx, k, i, z+1)
	if e != nil {
		return out, storeError(e, false)
	}
	out.Campaigns = []CampaignSummary{}
	for j, r := range w {
		if j == z {
			p := w[j-1].Campaign
			out.Next = next("campaigns", stamp(p.Created), p.ID)
			break
		}
		c := r.Campaign
		n := r.Counts
		out.Campaigns = append(out.Campaigns, CampaignSummary{c.ID, c.Name, c.Channels, stamp(c.Created), n.Members, n.Delivered, n.Opened})
	}
	return out, nil
}
func (h handlers) campaign(ctx context.Context, _ identity.Caller, in CampaignArgs) (out CampaignRead, err error) {
	var k, i string
	z, e := window("campaign", in.Page, in.Limit, &k, &i)
	if e != nil {
		return out, e
	}
	c, e := h.live(ctx, in.Campaign)
	if e != nil {
		return out, e
	}
	out.Campaign = campaignAnswer(c)
	n, e := h.Store.Counts(ctx, in.Campaign)
	if e != nil {
		return out, storeError(e, false)
	}
	out.Counts = Counts{n.Members, n.Delivered, n.Opened}
	ms, e := h.Store.Materials(ctx, in.Campaign)
	if e != nil {
		return out, storeError(e, false)
	}
	out.Material = []Material{}
	for _, m := range ms {
		out.Material = append(out.Material, materialAnswer(m))
	}
	w, e := h.Store.ListMembers(ctx, in.Campaign, k, i, z+1)
	if e != nil {
		return out, storeError(e, false)
	}
	out.Members = []Member{}
	for j, r := range w {
		if j == z {
			m := w[j-1].Member
			out.Next = next("campaign", stamp(m.Added), m.Candidate)
			break
		}
		out.Members = append(out.Members, memberAnswer(r))
	}
	return out, nil
}
func (h handlers) mergeList(ctx context.Context, _ identity.Caller, in MergeArgs) (out MergeList, err error) {
	var k, i string
	z, e := window("merge_list", in.Page, in.Limit, &k, &i)
	if e != nil {
		return out, e
	}
	if e = h.channel(ctx, in.Campaign, in.Channel); e != nil {
		return out, e
	}
	w, e := h.Store.MergeList(ctx, in.Campaign, in.Channel, in.All, k, i, z+1)
	if e != nil {
		return out, storeError(e, false)
	}
	if h.Candidates == nil {
		return out, candidateFailure()
	}
	out.Rows = []MergeRow{}
	for j, x := range w {
		if j == z {
			p := w[j-1].Piece
			out.Next = next("merge_list", stamp(p.Created), p.Candidate)
			break
		}
		b, e := h.Candidates.Candidate(ctx, x.Piece.Candidate)
		if e != nil && !errors.Is(e, candidates.ErrNotFound) {
			return MergeList{}, candidateFailure()
		}
		business := Business{}
		if e == nil {
			business = businessAnswer(b)
		}
		p := x.Piece
		out.Rows = append(out.Rows, MergeRow{p.ID, p.Candidate, p.Channel, p.State, x.Rejected, h.LinkPrefix + p.Token, business})
	}
	return out, nil
}
func (h handlers) engagement(ctx context.Context, _ identity.Caller, in EngagementArgs) (out Engagement, err error) {
	var seq int64
	z, e := window("engagement", in.Page, in.Limit, &seq)
	if e != nil {
		return out, e
	}
	piece := in.Piece != "" && in.Campaign == "" && in.Candidate == ""
	member := in.Piece == "" && in.Campaign != "" && in.Candidate != ""
	if !piece && !member {
		return out, invalid()
	}
	if member {
		if !validMember(in.Campaign, in.Candidate) {
			return out, missing()
		}
		r, e := h.Store.GetMember(ctx, in.Campaign, in.Candidate)
		if e != nil {
			return out, storeError(e, false)
		}
		a := r.Engagement
		return Engagement{Campaign: r.Member.Campaign, Candidate: r.Member.Candidate, Opened: a.Opened, Opens: a.Opens, First: stamp(a.First), Last: stamp(a.Last)}, nil
	}
	if !validPiece(in.Piece) {
		return out, missing()
	}
	r, e := h.Store.GetPiece(ctx, in.Piece)
	if e != nil {
		return out, storeError(e, false)
	}
	w, e := h.Store.ListVisits(ctx, in.Piece, seq, z+1)
	if e != nil {
		return out, storeError(e, false)
	}
	a := r.Engagement
	out = Engagement{Campaign: r.Piece.Campaign, Candidate: r.Piece.Candidate, Piece: r.Piece.ID, Opened: a.Opens > 0, Opens: a.Opens, First: stamp(a.First), Last: stamp(a.Last)}
	vs := []Visit{}
	for j, v := range w {
		if j == z {
			out.Next = next("engagement", w[j-1].Seq)
			break
		}
		vs = append(vs, Visit{stamp(v.At), v.UserAgent, v.Referrer, v.Address})
	}
	out.Visits = &vs
	return out, nil
}
