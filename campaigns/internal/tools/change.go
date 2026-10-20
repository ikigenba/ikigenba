package tools

import (
	"context"
	"errors"
	"slices"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/trail"
)

func validPiece(id string) bool    { return store.ValidPieceID(id) }
func validMember(c, m string) bool { return store.ValidCampaignID(c) && store.ValidCandidateID(m) }
func (h handlers) live(ctx context.Context, id string) (store.Campaign, error) {
	if !store.ValidCampaignID(id) {
		return store.Campaign{}, missing()
	}
	c, e := h.Store.GetCampaign(ctx, id)
	return c, storeError(e, false)
}
func (h handlers) channel(ctx context.Context, c, ch string) error {
	if !store.ValidChannel(ch) {
		return invalid()
	}
	k, e := h.live(ctx, c)
	if e != nil {
		return e
	}
	if !slices.Contains(k.Channels, ch) {
		return invalid()
	}
	return nil
}
func (h handlers) createCampaign(ctx context.Context, c identity.Caller, in CreateArgs) (Campaign, error) {
	if !store.ValidText(in.Name) || !store.ValidChannels(in.Channels) || !store.ValidDestination(in.Destination) {
		return Campaign{}, invalid()
	}
	k, e := h.Store.CreateCampaign(ctx, store.Campaign{Name: in.Name, Description: in.Description, Channels: in.Channels, Destination: in.Destination, CreatedUserID: c.UserID, CreatedUserEmail: c.Email})
	return campaignAnswer(k), storeError(e, false)
}
func (h handlers) editCampaign(ctx context.Context, _ identity.Caller, in EditArgs) (CampaignChange, error) {
	if in.Name != nil && !store.ValidText(*in.Name) || in.Destination != nil && !store.ValidDestination(*in.Destination) {
		return CampaignChange{}, invalid()
	}
	if !store.ValidCampaignID(in.Campaign) {
		return CampaignChange{}, missing()
	}
	k, ch, e := h.Store.EditCampaign(ctx, store.CampaignEdit{ID: in.Campaign, Name: in.Name, Description: in.Description, Destination: in.Destination})
	return CampaignChange{campaignAnswer(k), ch}, storeError(e, false)
}
func (h handlers) deleteCampaign(ctx context.Context, _ identity.Caller, in DeleteArgs) (CampaignChange, error) {
	if !store.ValidCampaignID(in.Campaign) {
		return CampaignChange{}, missing()
	}
	k, ch, e := h.Store.DeleteCampaign(ctx, in.Campaign)
	return CampaignChange{campaignAnswer(k), ch}, storeError(e, true)
}
func (h handlers) addMember(ctx context.Context, c identity.Caller, in MemberArgs) (AddedMember, error) {
	if !store.ValidCampaignID(in.Campaign) {
		return AddedMember{}, missing()
	}
	if !store.ValidCandidateID(in.Candidate) {
		return AddedMember{}, refusal(RefusalNotFound, RefuseNoCandidate)
	}
	k, e := h.live(ctx, in.Campaign)
	if e != nil {
		return AddedMember{}, e
	}
	r, e := h.Store.GetMember(ctx, in.Campaign, in.Candidate)
	if e == nil {
		return AddedMember{Member: memberAnswer(r), Skipped: []Skip{}}, nil
	}
	if !errors.Is(e, store.ErrNotFound) {
		return AddedMember{}, storeError(e, false)
	}
	if h.Candidates == nil {
		return AddedMember{}, candidateFailure()
	}
	b, e := h.Candidates.Candidate(ctx, in.Candidate)
	if errors.Is(e, candidates.ErrNotFound) {
		return AddedMember{}, refusal(RefusalNotFound, RefuseNoCandidate)
	}
	if e != nil {
		return AddedMember{}, candidateFailure()
	}
	return h.add(ctx, c, k, in.Candidate, "", b)
}
func (h handlers) add(ctx context.Context, c identity.Caller, k store.Campaign, id, prospectList string, b candidates.Business) (AddedMember, error) {
	ch, skip := channels(k, b)
	r, a, e := h.Store.AddMember(ctx, store.MemberInput{Campaign: k.ID, Candidate: id, ProspectList: prospectList, Channels: ch, UserID: c.UserID, UserEmail: c.Email})
	if e != nil {
		return AddedMember{}, storeError(e, false)
	}
	if a {
		trail.MemberAdded(ctx, h.Telemetry, h.Events, k.ID, id, prospectList)
	} else {
		skip = []Skip{}
	}
	return AddedMember{memberAnswer(r), a, skip}, nil
}
func (h handlers) addProspectList(ctx context.Context, c identity.Caller, in ProspectListArgs) (out ProspectListAdd, err error) {
	if !store.ValidCampaignID(in.Campaign) {
		return out, missing()
	}
	if !store.ValidProspectListID(in.ProspectList) {
		return out, refusal(RefusalNotFound, RefuseNoProspectList)
	}
	k, e := h.live(ctx, in.Campaign)
	if e != nil {
		return out, e
	}
	if h.Candidates == nil {
		return out, candidateFailure()
	}
	ms, e := h.Candidates.ProspectList(ctx, in.ProspectList)
	if errors.Is(e, candidates.ErrNotFound) {
		return out, refusal(RefusalNotFound, RefuseNoProspectList)
	}
	if e != nil {
		return out, candidateFailure()
	}
	for _, m := range ms {
		if !store.ValidCandidateID(m.Business.ID) {
			return out, candidateFailure()
		}
	}
	out.Added = []AddedMember{}
	for _, m := range ms {
		if m.Excluded {
			out.Excluded++
			continue
		}
		a, e := h.add(ctx, c, k, m.Business.ID, in.ProspectList, m.Business)
		if e != nil {
			return ProspectListAdd{}, e
		}
		if a.Added {
			out.Added = append(out.Added, a)
		} else {
			out.Present++
		}
	}
	return out, nil
}
func (h handlers) reject(ctx context.Context, c identity.Caller, in RejectArgs) (MemberChange, error) {
	if !store.ValidText(in.Why) {
		return MemberChange{}, invalid()
	}
	if !validMember(in.Campaign, in.Candidate) {
		return MemberChange{}, missing()
	}
	r, ch, e := h.Store.RejectMember(ctx, store.Rejection{Campaign: in.Campaign, Candidate: in.Candidate, Why: in.Why, UserID: c.UserID, UserEmail: c.Email})
	if e != nil {
		return MemberChange{}, storeError(e, false)
	}
	if ch {
		trail.MemberRejected(ctx, h.Telemetry, h.Events, r.Member.Campaign, r.Member.Candidate, r.Member.Why)
	}
	return MemberChange{memberAnswer(r), ch}, nil
}
func (h handlers) drop(ctx context.Context, c identity.Caller, in PieceArgs) (PieceChange, error) {
	if !validPiece(in.Piece) {
		return PieceChange{}, missing()
	}
	p, ch, e := h.Store.DropPiece(ctx, store.PieceAction{Piece: in.Piece, UserID: c.UserID, UserEmail: c.Email})
	return PieceChange{record(p), ch}, storeError(e, true)
}
func (h handlers) deliver(ctx context.Context, c identity.Caller, in PieceArgs) (PieceChange, error) {
	if !validPiece(in.Piece) {
		return PieceChange{}, missing()
	}
	p, ch, e := h.Store.DeliverPiece(ctx, store.PieceAction{Piece: in.Piece, UserID: c.UserID, UserEmail: c.Email})
	if e != nil {
		return PieceChange{}, storeError(e, true)
	}
	if ch {
		trail.PieceDelivered(ctx, h.Telemetry, h.Events, p.Campaign, p.Candidate, p.ID, p.Channel, p.How)
	}
	return PieceChange{record(p), ch}, nil
}
func (h handlers) deliverChannel(ctx context.Context, c identity.Caller, in ChannelArgs) (out Delivered, err error) {
	if e := h.channel(ctx, in.Campaign, in.Channel); e != nil {
		return out, e
	}
	ps, e := h.Store.DeliverChannel(ctx, store.ChannelDelivery{Campaign: in.Campaign, Channel: in.Channel, UserID: c.UserID, UserEmail: c.Email})
	if e != nil {
		return out, storeError(e, false)
	}
	out.Pieces = []PieceRecord{}
	for _, p := range ps {
		out.Pieces = append(out.Pieces, record(p))
		trail.PieceDelivered(ctx, h.Telemetry, h.Events, p.Campaign, p.Candidate, p.ID, p.Channel, p.How)
	}
	return out, nil
}
func (h handlers) addMaterial(ctx context.Context, c identity.Caller, in MaterialArgs) (MaterialChange, error) {
	if in.Path == "" || !store.ValidText(in.Label) {
		return MaterialChange{}, invalid()
	}
	if _, e := h.live(ctx, in.Campaign); e != nil {
		return MaterialChange{}, e
	}
	if h.Artifacts == nil {
		return MaterialChange{}, artifactFailure()
	}
	_, e := h.Artifacts.Head(ctx, in.Path)
	if errors.Is(e, artifacts.ErrBadPath) {
		return MaterialChange{}, invalid()
	}
	if errors.Is(e, artifacts.ErrNotFound) {
		return MaterialChange{}, refusal(RefusalNotFound, RefuseNoArtifact)
	}
	if e != nil {
		return MaterialChange{}, artifactFailure()
	}
	md, e := h.Artifacts.Show(ctx, in.Path)
	if e != nil || md.ID == "" {
		return MaterialChange{}, artifactFailure()
	}
	m, ch, e := h.Store.AddMaterial(ctx, store.Material{Campaign: in.Campaign, Path: in.Path, Label: in.Label, Artifact: md.ID, AddedUserID: c.UserID, AddedUserEmail: c.Email})
	return MaterialChange{materialAnswer(m), ch}, storeError(e, false)
}
func (h handlers) removeMaterial(ctx context.Context, _ identity.Caller, in RemoveArgs) (Removed, error) {
	if in.Path == "" {
		return Removed{}, invalid()
	}
	if !store.ValidCampaignID(in.Campaign) {
		return Removed{}, missing()
	}
	_, ch, e := h.Store.RemoveMaterial(ctx, in.Campaign, in.Path)
	return Removed{in.Path, ch}, storeError(e, false)
}
