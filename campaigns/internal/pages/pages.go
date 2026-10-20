package pages

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/campaigns/internal/store"
	"github.com/ikigenba/ikigenba/campaigns/internal/urls"
)

// Handler returns the page routes; its caller wraps it in identity.Optional.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-Id") == "" {
			redirect(w, http.StatusFound, urls.SignIn(r, cfg.ServicesPath))
			return
		}
		path := r.URL.EscapedPath()
		id := strings.TrimPrefix(path, "/campaigns/")
		campaign := strings.HasPrefix(path, "/campaigns/") && store.ValidCampaignID(id)
		notfound := func() {
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: banner(cfg, r, nil)})
		}
		if path != "/" && path != "/tools" && path != "/about" && !campaign {
			notfound()
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch path {
		case "/tools":
			rows := make([]ToolRow, 0)
			for _, t := range cfg.MCP.Tools() {
				description, _, _ := strings.Cut(t.Description, "\n")
				rows = append(rows, ToolRow{Name: t.Name, Description: description})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", ToolsData{Banner: banner(cfg, r, []page.Level{ToolsLevel}), Tools: rows})
			return
		case "/about":
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: banner(cfg, r, []page.Level{AboutLevel}), Description: Description})
			return
		}
		name := "campaigns"
		if campaign {
			name = "campaign"
		}
		p := r.URL.Query().Get("page")
		pos, ok := decode(name, p)
		if !ok {
			notfound()
			return
		}
		var err error
		if campaign {
			err = campaignPage(cfg, w, r, id, p, pos)
		} else {
			err = landing(cfg, w, r, p, pos)
		}
		if errors.Is(err, store.ErrNotFound) {
			notfound()
			return
		}
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(store.Unreachable + "\n"))
			}
		}
	})
}

func banner(cfg Config, r *http.Request, trail []page.Level) page.Banner {
	auth := urls.Auth(r, cfg.ServicesPath)
	b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
	b.Trail = trail
	return b
}
func redirect(w http.ResponseWriter, status int, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(status)
}
func stamp(t time.Time, layout string) (string, string) {
	if t.IsZero() {
		return "", ""
	}
	return t.UTC().Format(time.RFC3339), t.UTC().Format(layout)
}

type position struct {
	key, id       string
	reverse, last bool
}

func decode(name, p string) (position, bool) {
	var pos position
	if p == "" {
		return pos, true
	}
	if (paging.Token{Tool: name}).Decode(p, &pos.key, &pos.id) == nil {
		return pos, true
	}
	if (paging.Token{Tool: name + "-before"}).Decode(p, &pos.key, &pos.id) == nil {
		pos.reverse = true
		return pos, true
	}
	if p == "last" {
		return position{reverse: true, last: true}, true
	}
	return position{}, false
}
func window[T any](rows []T, pos position) ([]T, bool) {
	more := len(rows) > paging.DefaultLimit
	if more {
		rows = rows[:paging.DefaultLimit]
	}
	if pos.reverse {
		slices.Reverse(rows)
	}
	return rows, more
}
func navigation(name, p string, pos position, more bool, firstTime, lastTime time.Time, firstID, lastID string) (prev, next string) {
	if firstID == "" {
		return "", ""
	}
	if p != "" {
		prev = (paging.Token{Tool: name + "-before"}).Encode(firstTime.UTC().Format(time.RFC3339), firstID)
	}
	if !pos.last && (pos.reverse || more) {
		next = (paging.Token{Tool: name}).Encode(lastTime.UTC().Format(time.RFC3339), lastID)
	}
	return prev, next
}
func landing(cfg Config, w http.ResponseWriter, r *http.Request, p string, pos position) error {
	var rows []store.CampaignRow
	var err error
	if pos.reverse {
		rows, err = cfg.Store.ListCampaignsReversed(r.Context(), pos.key, pos.id, paging.DefaultLimit+1)
	} else {
		rows, err = cfg.Store.ListCampaigns(r.Context(), pos.key, pos.id, paging.DefaultLimit+1)
	}
	if err != nil {
		return err
	}
	if pos.reverse && len(rows) <= paging.DefaultLimit {
		redirect(w, http.StatusSeeOther, "/")
		return nil
	}
	rows, more := window(rows, pos)
	data := LandingData{Banner: banner(cfg, r, nil), Campaigns: make([]CampaignRow, 0, len(rows)), Page: p}
	for _, row := range rows {
		c := row.Campaign
		n := row.Counts
		a, b := stamp(c.Created, MinuteLayout)
		data.Campaigns = append(data.Campaigns, CampaignRow{ID: c.ID, Name: c.Name, Channels: c.Channels, Created: a, CreatedText: b, Members: n.Members, Delivered: n.Delivered, Opened: n.Opened})
	}
	if len(rows) > 0 {
		first, last := rows[0].Campaign, rows[len(rows)-1].Campaign
		data.Prev, data.Next = navigation("campaigns", p, pos, more, first.Created, last.Created, first.ID, last.ID)
	}
	cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
	return nil
}
func campaignPage(cfg Config, w http.ResponseWriter, r *http.Request, id, p string, pos position) error {
	c, err := cfg.Store.GetCampaign(r.Context(), id)
	if err != nil {
		return err
	}
	n, err := cfg.Store.Counts(r.Context(), id)
	if err != nil {
		return err
	}
	ms, err := cfg.Store.Materials(r.Context(), id)
	if err != nil {
		return err
	}
	var rows []store.MemberRow
	if pos.reverse {
		rows, err = cfg.Store.ListMembersReversed(r.Context(), id, pos.key, pos.id, paging.DefaultLimit+1)
	} else {
		rows, err = cfg.Store.ListMembers(r.Context(), id, pos.key, pos.id, paging.DefaultLimit+1)
	}
	if err != nil {
		return err
	}
	if pos.reverse && len(rows) <= paging.DefaultLimit {
		redirect(w, http.StatusSeeOther, "/campaigns/"+id)
		return nil
	}
	rows, more := window(rows, pos)
	created, createdText := stamp(c.Created, SecondLayout)
	data := CampaignData{Banner: banner(cfg, r, []page.Level{{Name: id, URL: "/campaigns/" + id}}), Campaign: CampaignView{ID: c.ID, Name: c.Name, Description: c.Description, Channels: c.Channels, Destination: c.Destination, CreatedBy: c.CreatedUserEmail, CreatedUser: c.CreatedUserID, Created: created, CreatedText: createdText, Members: n.Members, Delivered: n.Delivered, Opened: n.Opened}, Material: make([]MaterialRow, 0, len(ms)), Members: make([]MemberRow, 0, len(rows)), Page: p}
	for _, m := range ms {
		data.Material = append(data.Material, MaterialRow{Label: m.Label, Path: m.Path, ArtifactPage: urls.ArtifactPage(urls.Sibling(cfg.ServicesPath, "artifacts"), m.Artifact)})
	}
	for _, m := range rows {
		data.Members = append(data.Members, memberRow(cfg, r, c.Channels, m))
	}
	if len(rows) > 0 {
		first, last := rows[0].Member, rows[len(rows)-1].Member
		data.Prev, data.Next = navigation("campaign", p, pos, more, first.Added, last.Added, first.Candidate, last.Candidate)
	}
	cfg.Pages.Write(w, r, http.StatusOK, "campaign", data)
	return nil
}
func memberRow(cfg Config, r *http.Request, channels []string, m store.MemberRow) MemberRow {
	x := m.Member
	a, b := stamp(x.Added, MinuteLayout)
	base := urls.Sibling(cfg.ServicesPath, "candidates")
	out := MemberRow{Candidate: x.Candidate, CandidatePage: urls.CandidatePage(base, x.Candidate), Cohort: x.Cohort, CohortPage: urls.CohortPage(base, x.Cohort), Added: a, AddedText: b, Opened: m.Engagement.Opened, Rejected: x.Rejected, Pieces: make([]PieceRow, 0, len(channels))}
	if cfg.Candidates != nil {
		business, err := cfg.Candidates.Candidate(r.Context(), x.Candidate)
		if err == nil {
			out.Name = business.Name
		}
	}
	if x.Rejected {
		out.RejectedBy = x.RejectedUserEmail
		out.RejectedUser = x.RejectedUserID
		out.Why = x.Why
		out.RejectedAt, out.RejectedAtText = stamp(x.RejectedAt, MinuteLayout)
	}
	for _, ch := range channels {
		piece := PieceRow{Channel: ch}
		for _, p := range m.Pieces {
			if p.Piece.Channel == ch {
				piece = pieceRow(p)
				break
			}
		}
		out.Pieces = append(out.Pieces, piece)
	}
	return out
}
func pieceRow(p store.PieceRow) PieceRow {
	x := p.Piece
	out := PieceRow{Channel: x.Channel, State: x.State, Opens: p.Engagement.Opens}
	if x.State == store.StateDelivered {
		out.Delivered, out.DeliveredText = stamp(x.Delivered, MinuteLayout)
		out.DeliveredBy = x.DeliveredUserEmail
		out.DeliveredUser = x.DeliveredUserID
		out.How = x.How
	}
	if out.Opens > 0 {
		out.First, out.FirstText = stamp(p.Engagement.First, MinuteLayout)
		out.Last, out.LastText = stamp(p.Engagement.Last, MinuteLayout)
	}
	return out
}
