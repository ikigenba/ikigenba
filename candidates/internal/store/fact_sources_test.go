package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func sourcedPeople(ps []sources.Person, source, id string) []sources.Sourced[sources.Person] {
	out := make([]sources.Sourced[sources.Person], 0, len(ps))
	for _, p := range ps {
		out = append(out, sources.Sourced[sources.Person]{Value: p, Source: source, Key: sources.SourceKey(source, id)})
	}
	return out
}

func withProvenance(c store.Candidate, obs ...store.Observation) store.Candidate {
	rs := []sources.Record{}
	for _, o := range obs {
		rs = append(rs, sources.Record{Source: o.Scraper, SourceID: o.SourceID, Reading: o.Reading})
	}
	p := sources.Pick(sources.Orders(), rs, nil)
	c.NameSource, c.NameRecord = p.Name.Source, p.Name.Key
	c.AddressSource, c.AddressRecord = p.Address.Source, p.Address.Key
	c.PhoneSource, c.PhoneRecord = p.Phone.Source, p.Phone.Key
	c.EmailSource, c.EmailRecord = p.Email.Source, p.Email.Key
	c.OtherNames, c.OtherAddresses, c.OtherPhones, c.OtherEmails = p.OtherNames, p.OtherAddresses, p.OtherPhones, p.OtherEmails
	c.People = p.People
	return c
}

// R-2BTS-XFVR R-HTD3-0YUG R-HUKZ-EQL5 R-DCSP-YFTV R-2GVW-PRK8
func TestRecordChosenAddressAndRoutedDisplay(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	f.s = routeStore(f, "origin")
	a := contractObservation("a", "Carrier", f.now)
	a.Reading.Street = "5 Main St"
	a.Reading.City = "Rush City"
	a.Reading.Zip = "55069"
	a.Reading.County = "MN-025"
	b := a
	b.SourceID = "b"
	b.Digest = "b"
	b.Reading.Street = "9 Older St"
	id := contractAttach(t, f, "one", a, b)[0]
	// Reverse timestamps: the record identity still chooses the address.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE links SET first_seen=CASE source_id WHEN 'a' THEN ? ELSE ? END,last_seen=CASE source_id WHEN 'a' THEN ? ELSE ? END`, f.now.Add(time.Hour).UTC().Format(time.RFC3339), f.now.UTC().Format(time.RFC3339), f.now.UTC().Format(time.RFC3339), f.now.Add(time.Hour).UTC().Format(time.RFC3339))
		return e
	}))
	su, e := f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, su.RecordAddress, sources.Sourced[sources.Address]{Value: sources.Address{Street: "5 Main St", City: "Rush City", State: "MN", Zip: "55069", County: "MN-025"}, Source: "fmcsa", Key: "fmcsa:a"})
	for _, l := range su.Links {
		want := f.now
		if l.SourceID == "b" {
			want = want.Add(time.Hour)
		}
		contractEqual(t, l.Seen, want.UTC().Truncate(time.Second))
	}
	// Gone identity is preserved when converting links; older live a defeats newer gone b.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE links SET gone=1 WHERE source_id='b'`)
		return e
	}))
	su, e = f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, su.RecordAddress.Key, "fmcsa:a")
	contractEqual(t, su.RecordAddress.Value.Street, "5 Main St")
	for _, l := range su.Links {
		if l.SourceID == "b" {
			contractEqual(t, l.Gone, true)
		}
	}
	in := (enrichers.DriveRoute{}).Inputs(su)
	get := func() store.Candidate { t.Helper(); c, e := f.s.GetCandidate(ctx, id); contractOK(t, e); return c }
	for _, tc := range []struct{ address, precision, street, county string }{
		{"7 Main St S, Rush City, MN 55069, USA", routes.PrecisionBuilding, "7 Main St S", "MN-025"},
		{"7 Main St S, Harris, MN 55032, USA", routes.PrecisionBuilding, "7 Main St S", ""},
		{"7 Main St S, Rush City, MN 55067, USA", routes.PrecisionBuilding, "7 Main St S", ""},
		{"Rush City, MN 55069, USA", routes.PrecisionBuilding, "5 Main St", "MN-025"},
		{"7 Main St S, Rush City, MN 55069", routes.PrecisionBuilding, "5 Main St", "MN-025"},
		{"7 Main St S, Rush City, MN 55069, USA", routes.PrecisionStreet, "5 Main St", "MN-025"},
	} {
		before := get()
		f.now = f.now.Add(time.Second)
		appendRoute(t, f.s, id, in, enrichers.RouteFound{Minutes: ptrInt(40), Precision: tc.precision, Address: tc.address})
		c := get()
		contractEqual(t, c.Street, tc.street)
		contractEqual(t, c.County, tc.county)
		oldUpdated := before.Updated
		before.Updated = c.Updated
		if !reflect.DeepEqual(c, before) {
			contractEqual(t, c.Updated, f.now.UTC().Truncate(time.Second))
		} else {
			contractEqual(t, c.Updated, oldUpdated)
		}
	}
	f.now = f.now.Add(time.Second)
	appendRoute(t, f.s, id, in, enrichers.RouteFound{Minutes: ptrInt(40), Precision: routes.PrecisionBuilding, Address: "7 Main St S, Rush City, MN 55069, USA"})
	routed := get()
	contractEqual(t, routed.AddressSource, sources.Route)
	contractEqual(t, routed.AddressRecord, "route:"+in)
	contractEqual(t, routed.OtherAddresses[0].Key, "fmcsa:a")
	for _, g := range []enrichers.RouteFound{{Error: "failure"}, {Why: routes.WhyNoRoute, Precision: routes.PrecisionBuilding}, {Why: routes.WhyTooCoarse, Precision: routes.PrecisionTown}} {
		f.now = f.now.Add(time.Second)
		appendRoute(t, f.s, id, in, g)
		contractEqual(t, get(), routed)
	}
	su, e = f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, su.RecordAddress.Key, "fmcsa:a")
	contractEqual(t, su.RecordAddress.Value.Street, "5 Main St")
	contractEqual(t, su.AddressSource, sources.Route)
	contractEqual(t, (enrichers.DriveRoute{}).Inputs(su), in)
	contractEqual(t, su.People, routed.People)
	due, e := f.s.DueAmong(ctx, enrichers.KindDriveRoute, []string{id}, false)
	contractOK(t, e)
	contractEqual(t, len(due), 0)
	appendRoute(t, f.s, id, "different origin", enrichers.RouteFound{Minutes: ptrInt(8), Precision: routes.PrecisionBuilding, Address: "8 Foreign St, Rush City, MN 55069, USA"})
	contractEqual(t, get(), routed)
	// FillFacts must repair at the current origin, but leave a non-route candidate alone.
	other := contractObservation("other", "Untouched", f.now)
	other.Reading.PhoneKey = ""
	other.Reading.DomainKey = ""
	other.Reading.NameKey = "other"
	otherID := contractAttach(t, f, "other", other)[0]
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE candidates SET name='corrupt' WHERE id=?`, otherID)
		return e
	}))
	f.reopen()
	f.s = routeStore(f, "other origin")
	f.now = f.now.Add(time.Second)
	contractOK(t, fillFactsWalk(ctx, f.s))
	unrouted := get()
	contractEqual(t, unrouted.Street, "5 Main St")
	contractEqual(t, unrouted.AddressSource, "fmcsa")
	contractEqual(t, unrouted.Updated, f.now.UTC().Truncate(time.Second))
	c, e := f.s.GetCandidate(ctx, otherID)
	contractOK(t, e)
	contractEqual(t, c.Name, "corrupt")
	f.now = f.now.Add(time.Second)
	contractOK(t, fillFactsWalk(ctx, f.s))
	contractEqual(t, get(), unrouted)
	f.s = routeStore(f, "origin")
	contractOK(t, fillFactsWalk(ctx, f.s))
	contractEqual(t, get().Street, "7 Main St S")
	// Route inputs ignore a corrupt displayed row; a route write repairs it, other kinds do not.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE candidates SET name='corrupt',street='wrong' WHERE id=?`, id)
		return e
	}))
	su, e = f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, (enrichers.DriveRoute{}).Inputs(su), in)
	_, e = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteNone)})
	contractOK(t, e)
	contractEqual(t, get().Name, "corrupt")
	appendRoute(t, f.s, id, "irrelevant", enrichers.RouteFound{Error: "failed"})
	contractEqual(t, get().Name, "Carrier")
	contractEqual(t, get().Street, "7 Main St S")
	f.s = routeStore(f, "")
	contractOK(t, fillFactsWalk(ctx, f.s))
	contractEqual(t, get().AddressSource, "fmcsa")
}

// R-BFUH-DS25 R-D0LQ-4QEX R-RYYW-L4WX R-D6P8-1L4E R-CZDT-QYO8 R-DCSP-YFTV R-QZ2H-X82S R-QGS0-6NYD
func TestPlacesURLCurrentRecordAndSubjectChecks(t *testing.T) {
	var _ interface {
		PlacesURL(context.Context, string) (string, error)
	} = (*store.Store)(nil)
	contractEqual(t, []string{store.WebsiteByPlaces, store.WebsiteByOwner}, []string{"places", "owner"})
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	x := walkFound("place", "Offered", "3205550101", " https://acme.test/ ")
	walkSweep(t, f, p, ks[0], x)
	ps, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	id := ps[0].Candidate
	assertURL := func(want string) {
		t.Helper()
		u, e := f.s.PlacesURL(ctx, id)
		contractOK(t, e)
		contractEqual(t, u, want)
	}
	assertURL("https://acme.test/")
	facts, e := f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.Website, sources.WebsiteUnknown)
	contractEqual(t, facts.WebsiteSource, store.WebsiteByPlaces)
	contractEqual(t, facts.WebsiteURL, "")
	o := contractObservation("record", "Offered", f.now)
	o.Reading.Email = "ops@acme.test"
	o.Reading.PhoneKey = "3205550101"
	o.Reading.Street = "15 Main St"
	o.Reading.City = "Example"
	o.Reading.Zip = "55056"
	contractEqual(t, contractAttach(t, f, "record", o)[0], id)
	su, e := f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, su.PlacesURL, "https://acme.test/")
	contractEqual(t, (enrichers.WebsiteCheck{}).Inputs(su), "https://acme.test/\nacme.test")
	check := func(in string, w website.Result) {
		t.Helper()
		raw, e := json.Marshal(w)
		contractOK(t, e)
		_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: in, Found: raw}})
		contractOK(t, e)
	}
	old := website.Result{State: sources.WebsiteNone, Title: "email failure"}
	newer := website.Result{State: sources.WebsiteHTTPS, URL: "https://www.acme.test/", From: "https://acme.test/", Title: "places success"}
	check("acme.test", old)
	check("https://acme.test/", newer)
	// A line in malformed inputs is not a URL token under website.URLs.
	check("invalid-first-line\nhttps://acme.test/\nacme.test", website.Result{State: sources.WebsiteNone, Title: "must not match"})
	su, e = f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, *su.Check, newer)
	contractEqual(t, *su.EmailCheck, old)
	c, e := f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, []string{su.NameSource, su.NameRecord, su.AddressSource, su.AddressRecord, su.PhoneSource, su.PhoneRecord, su.EmailSource, su.EmailRecord}, []string{c.NameSource, c.NameRecord, c.AddressSource, c.AddressRecord, c.PhoneSource, c.PhoneRecord, c.EmailSource, c.EmailRecord})
	contractEqual(t, su.People, c.People)
	check(website.Inputs(su.PlacesURL, c.Email), newer)
	su, e = f.s.Subject(ctx, id)
	contractOK(t, e)
	contractEqual(t, *su.Check, newer)
	contractEqual(t, *su.EmailCheck, newer)
	check("https://acme.test/", website.Result{State: sources.WebsiteNone, Title: "site failure"})
	facts, e = f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.WebsiteSource, "")
	// Each later completed walk updates the current place record; old websites do not leak.
	for _, w := range []string{"https://new-acme.test/", "", "acme.test", "ftp://acme.test/", "https://10.0.0.1/", " https://last-acme.test/ "} {
		_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
		contractOK(t, e)
		p = walkPass(t, f)
		x.Place.Website = w
		walkSweep(t, f, p, ks[0], x)
		want := ""
		if w == "https://new-acme.test/" {
			want = w
		}
		if w == " https://last-acme.test/ " {
			want = "https://last-acme.test/"
		}
		assertURL(want)
		facts, e = f.s.GetFacts(ctx, id)
		contractOK(t, e)
		if want != "" {
			contractEqual(t, facts.WebsiteSource, store.WebsiteByPlaces)
		} else {
			contractEqual(t, facts.WebsiteSource, "")
		}
	}
	f.reopen()
	assertURL("https://last-acme.test/")
	facts, e = f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.WebsiteSource, store.WebsiteByPlaces)
	no := contractObservation("no-place", "No place", f.now)
	no.Reading.PhoneKey = ""
	no.Reading.DomainKey = ""
	other := contractAttach(t, f, "other", no)[0]
	u, e := f.s.PlacesURL(ctx, other)
	contractOK(t, e)
	contractEqual(t, u, "")
	u, e = f.s.PlacesURL(ctx, "missing")
	contractError(t, e, store.ErrNotFound)
	contractEqual(t, u, "")
	// Merge moves links; both statuses remain readable and read calls leave records unchanged.
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: id, Other: other, Why: "same", UserID: "owner"})
	contractOK(t, e)
	before, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, e)
	u, e = f.s.PlacesURL(ctx, other)
	contractOK(t, e)
	contractEqual(t, u, "")
	assertURL("https://last-acme.test/")
	after, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, e)
	contractEqual(t, after, before)
	for _, candidate := range []string{id, other} {
		c, e := f.s.GetCandidate(ctx, candidate)
		contractOK(t, e)
		if c.OtherNames == nil || c.OtherAddresses == nil || c.OtherPhones == nil || c.OtherEmails == nil {
			t.Fatal("nil alternatives")
		}
	}
}

// R-QJ7S-Y7FR
func TestWebsiteDecisionPlacesAndCheckedURL(t *testing.T) {
	p := "https://acme.test/"
	at := time.Date(2031, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		in              string
		w               website.Result
		source, checked string
	}{
		{"acme-mail.test", website.Result{State: sources.WebsiteNone, Title: "none"}, store.WebsiteByPlaces, ""},
		{p, website.Result{State: sources.WebsiteNone, Title: "none"}, "", ""},
		{website.Inputs(p, "ops@acme-mail.test"), website.Result{State: sources.WebsiteNone, Title: "none"}, "", ""},
		{website.Inputs(p, "ops@acme-mail.test"), website.Result{State: sources.WebsiteHTTPS, URL: "https://www.acme.test/", From: p, Title: "works"}, store.WebsiteByCheck, p},
		{"acme-mail.test", website.Result{State: sources.WebsiteHTTPS, URL: "https://acme-mail.test/", From: "acme-mail.test", Title: "works"}, store.WebsiteByCheck, "acme-mail.test"},
		{"acme-mail.test", website.Result{State: sources.WebsiteHTTPS, URL: "https://acme-mail.test/", Title: "legacy"}, store.WebsiteByCheck, ""},
	} {
		raw, e := json.Marshal(tc.w)
		contractOK(t, e)
		src, state, u, title, why, checked, tm := store.WebsiteDecisionOf([]store.Enrichment{{Kind: sources.WebsiteCheck, Inputs: tc.in, Found: raw, At: at}}, p)
		contractEqual(t, src, tc.source)
		contractEqual(t, checked, tc.checked)
		contractEqual(t, title, tc.w.Title)
		contractEqual(t, why, "")
		contractEqual(t, tm, at)
		if tc.w.State == sources.WebsiteNone {
			contractEqual(t, state, sources.WebsiteUnknown)
			contractEqual(t, u, "")
		} else {
			contractEqual(t, state, tc.w.State)
			contractEqual(t, u, tc.w.URL)
		}
	}
	src, state, u, title, why, checked, tm := store.WebsiteDecisionOf(nil, p)
	contractEqual(t, src, store.WebsiteByPlaces)
	contractEqual(t, state, sources.WebsiteUnknown)
	contractEqual(t, []string{u, title, why, checked}, []string{"", "", "", ""})
	contractEqual(t, tm, time.Time{})
}

// R-QEC7-F4GZ
func TestCandidateFactSourcesDeclaration(t *testing.T) {
	c := store.Candidate{}
	contractEqual(t, store.Candidate(struct {
		ID, Status, MergedInto, Name                                                                             string
		Names                                                                                                    []string
		Street, City, State, Zip, County, Phone, Email                                                           string
		People                                                                                                   []sources.Sourced[sources.Person]
		Class                                                                                                    string
		Cargo                                                                                                    []string
		Created, Updated                                                                                         time.Time
		NameSource, NameRecord, AddressSource, AddressRecord, PhoneSource, PhoneRecord, EmailSource, EmailRecord string
		OtherNames                                                                                               []sources.Sourced[string]
		OtherAddresses                                                                                           []sources.Sourced[sources.Address]
		OtherPhones, OtherEmails                                                                                 []sources.Sourced[string]
	}(c)), c)
}

// R-RYYW-L4WX R-QZ2H-X82S
func TestPlacesURLUsesLinksOrderAndCandidateReturnSlices(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	// All three places attach through one phone. The earliest key is invalid and is skipped.
	walkSweep(t, f, p, ks[0], walkFound("a", "Empty", "3205550991", "ftp://invalid.test/"), walkFound("b", "First URL", "3205550991", " https://first.test/ "), walkFound("c", "Last URL", "3205550991", "https://last.test/"))
	ps, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	id := ps[0].Candidate
	for _, v := range ps {
		contractEqual(t, v.Candidate, id)
	}
	ls, e := f.s.Links(ctx, id)
	contractOK(t, e)
	contractEqual(t, []string{ls[0].SourceID, ls[1].SourceID, ls[2].SourceID}, []string{"a", "b", "c"})
	u, e := f.s.PlacesURL(ctx, id)
	contractOK(t, e)
	contractEqual(t, u, "https://first.test/")
	// Refreshing c in a later walk preserves the identity order and b's offer.
	_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
	contractOK(t, e)
	f.now = f.now.Add(time.Hour)
	p = walkPass(t, f)
	walkSweep(t, f, p, ks[0], walkFound("c", "Last URL", "3205550991", "https://last.test/"))
	ls, e = f.s.Links(ctx, id)
	contractOK(t, e)
	contractEqual(t, ls[0].SourceID, "a")
	u, e = f.s.PlacesURL(ctx, id)
	contractOK(t, e)
	contractEqual(t, u, "https://first.test/")
	_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
	contractOK(t, e)
	p = walkPass(t, f)
	walkSweep(t, f, p, ks[0], walkFound("c", "Last URL", "3205550991", ""))
	u, e = f.s.PlacesURL(ctx, id)
	contractOK(t, e)
	contractEqual(t, u, "https://first.test/")
	candidates, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, e)
	_, e = f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	k := prospectListCreate(t, f, "alternatives")
	r, e := f.s.ReadProspectList(ctx, k.ID)
	contractOK(t, e)
	for _, m := range r.Members {
		candidates = append(candidates, m.Candidate)
	}
	for _, c := range candidates {
		if c.OtherNames == nil || c.OtherAddresses == nil || c.OtherPhones == nil || c.OtherEmails == nil {
			t.Fatal("nil alternative slice")
		}
	}
}

// R-HSJK-2DYU R-D1TM-II5M R-QGS0-6NYD
func TestPlacesUnknownListAndFactsProvenance(t *testing.T) {
	type protocol string
	var placesSource protocol = store.WebsiteByPlaces
	var ownerSource protocol = store.WebsiteByOwner
	contractEqual(t, placesSource, protocol("places"))
	contractEqual(t, ownerSource, protocol("owner"))
	ctx := context.Background()
	f := contractDB(t)
	ks := seedParks(t, f)
	p := walkPass(t, f)
	walkSweep(t, f, p, ks[0], walkFound("place", "Unknown site", "3205550988", "https://offered.test/"))
	ps, e := f.s.ParkPlaces(ctx, ks[0].ID)
	contractOK(t, e)
	id := ps[0].Candidate
	_, e = f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	k := prospectListCreate(t, f, "Places unknown")
	filters := store.ProspectListFilters{Website: []string{sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone}}
	k, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	r := prospectListRead(t, f, k)
	contractEqual(t, len(r.Members), 0)
	filters.WebsiteUnknown = true
	k, e = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, e)
	r = prospectListRead(t, f, k)
	contractEqual(t, len(r.Members), 1)
	contractEqual(t, r.Members[0].Facts.WebsiteSource, store.WebsiteByPlaces)
	contractEqual(t, r.Members[0].Facts.Website, sources.WebsiteUnknown)
	lf := enrichers.LinkFound{KnownSince: f.now.Add(-24 * time.Hour).Format(time.DateOnly), KnownSinceSource: "fmcsa:known", NewestFiling: f.now.Format(time.DateOnly), NewestFilingSource: "ppp:filing", FleetVerdict: sources.FleetUnknown, Categories: []enrichers.Category{}, Headcounts: []enrichers.Headcount{}}
	raw, e := json.Marshal(lf)
	contractOK(t, e)
	_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "link", Found: raw}, {Candidate: id, Kind: sources.WebsiteCheck, Inputs: "https://offered.test/", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://landed.test/", From: "https://offered.test/"})}})
	contractOK(t, e)
	f.reopen()
	facts, e := f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.KnownSinceSource, lf.KnownSinceSource)
	contractEqual(t, facts.NewestFilingSource, lf.NewestFilingSource)
	contractEqual(t, facts.WebsiteCheckedURL, "https://offered.test/")
	r = prospectListRead(t, f, k)
	contractEqual(t, r.Members[0].Facts, facts)
	// Previous-result JSON omitted provenance; a new projection clears prior source fields.
	_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "old-json", Found: json.RawMessage(`{"known_since":"2030-01-01","newest_filing":"2030-02-01","categories":[],"headcounts":[],"fleet_verdict":"unknown"}`)}})
	contractOK(t, e)
	facts, e = f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.KnownSinceSource, "")
	contractEqual(t, facts.NewestFilingSource, "")
}
