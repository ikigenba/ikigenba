package tools

import (
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
)

func refusal(word, text string) error { return errors.New(word + ": " + text) }
func invalid() error                  { return refusal(RefusalInvalid, RefuseInvalid) }
func missing() error                  { return refusal(RefusalNotFound, RefuseNotFound) }
func candidateFailure() error         { return refusal(RefusalUnavailable, RefuseCandidates) }
func artifactFailure() error          { return refusal(RefusalUnavailable, RefuseArtifacts) }
func storeError(err error, state bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return missing()
	}
	if errors.Is(err, store.ErrInvalid) {
		if state {
			return refusal(RefusalInvalid, RefuseState)
		}
		return invalid()
	}
	return refusal(RefusalUnavailable, store.Unreachable)
}
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
func campaignAnswer(c store.Campaign) Campaign {
	return Campaign{c.ID, c.Name, c.Description, c.Channels, c.Destination, stamp(c.Created), c.CreatedUserID, c.CreatedUserEmail, c.Deleted}
}
func record(p store.Piece) PieceRecord {
	return PieceRecord{p.ID, p.Campaign, p.Candidate, p.Channel, p.State, stamp(p.Created), stamp(p.Delivered), p.DeliveredUserID, p.DeliveredUserEmail, p.How, stamp(p.Dropped), p.DroppedUserID, p.DroppedUserEmail}
}
func pieceAnswer(r store.PieceRow) Piece {
	p := record(r.Piece)
	return Piece{p.ID, p.Campaign, p.Candidate, p.Channel, p.State, p.Created, p.Delivered, p.DeliveredBy, p.DeliveredEmail, p.How, p.Dropped, p.DroppedBy, p.DroppedEmail, r.Engagement.Opens, stamp(r.Engagement.First), stamp(r.Engagement.Last)}
}
func memberAnswer(r store.MemberRow) Member {
	m := r.Member
	e := r.Engagement
	p := make([]Piece, 0, len(r.Pieces))
	for _, x := range r.Pieces {
		p = append(p, pieceAnswer(x))
	}
	return Member{m.Campaign, m.Candidate, m.Cohort, stamp(m.Added), m.AddedUserID, m.AddedUserEmail, m.Rejected, m.RejectedUserID, m.RejectedUserEmail, stamp(m.RejectedAt), m.Why, e.Opened, e.Opens, stamp(e.First), stamp(e.Last), p}
}
func materialAnswer(m store.Material) Material {
	return Material{m.Path, m.Label, m.Artifact, stamp(m.Added), m.AddedUserID, m.AddedUserEmail}
}
func businessAnswer(b candidates.Business) Business {
	ps := make([]Person, 0, len(b.People))
	for _, p := range b.People {
		ps = append(ps, Person{p.Name, p.Role})
	}
	return Business{b.Name, b.Names, b.Street, b.City, b.State, b.Zip, b.County, b.Phone, b.Email, ps}
}
func channels(k store.Campaign, b candidates.Business) ([]string, []Skip) {
	ch := []string{}
	skip := []Skip{}
	for _, c := range k.Channels {
		switch {
		case c == store.ChannelMail && (b.Street == "" || b.City == "" || b.State == "" || b.Zip == ""):
			skip = append(skip, Skip{c, SkipNoAddress})
		case c == store.ChannelEmail && b.Email == "":
			skip = append(skip, Skip{c, SkipNoEmail})
		default:
			ch = append(ch, c)
		}
	}
	return ch, skip
}
func window(tool string, page *string, limit *int, into ...any) (int, error) {
	z, err := paging.Window(limit)
	if err != nil {
		return 0, err
	}
	if page != nil && *page != "" {
		if err = (paging.Token{Tool: tool}).Decode(*page, into...); err != nil {
			return 0, errors.New(paging.BadPage)
		}
	}
	return z, nil
}
func next(tool string, keys ...any) *string {
	v := (paging.Token{Tool: tool}).Encode(keys...)
	return &v
}
