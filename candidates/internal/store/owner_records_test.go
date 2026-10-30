package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func ownPtr[T any](v T) *T { return &v }
func writeOwner(t *testing.T, f *contractFixture, id string, v store.OwnerValues, clears ...string) (store.OwnerRecord, store.Candidate) {
	t.Helper()
	r, c, err := f.s.AddOwnerRecord(context.Background(), store.OwnerRecord{Candidate: id, Note: "  Owner's observation  ", UserID: "owner-user", UserEmail: "owner@example.test", Set: v, Cleared: clears})
	contractOK(t, err)
	return r, c
}

// R-MN74-UDUO R-MOF1-85LD R-MPMX-LXC2 R-MQUT-ZP2R R-MS2Q-DGTG R-MTAM-R8K5 R-MUIJ-50AU R-MWYB-WJS8 R-MY68-ABIX
func TestOwnerPublicValuesAndEncoding(t *testing.T) {
	contractEqual(t, store.OwnerRecordIDPrefix, "own_")
	for _, s := range []string{"own_0123456789abcdef"} {
		if !store.ValidOwnerRecordID(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", "own_0123456789ABCDEF", "own_0123456789abcde", "own_0123456789abcdef0", "cand_0123456789abcdef"} {
		if store.ValidOwnerRecordID(s) {
			t.Fatal(s)
		}
	}
	h := store.OwnerHeadcount{Count: 26, Year: 2024}
	contractEqual(t, store.OwnerHeadcount(struct {
		Count, Year int `json:"-"`
	}{26, 2024}), h)
	contractEqual(t, store.OwnerValues(struct {
		Name       *string
		Headcount  *store.OwnerHeadcount
		Revenue    *store.OwnerRevenue
		Category   *string
		Website    *string
		Address    *sources.Address
		Phone      *string
		Email      *string
		People     []store.OwnerPerson
		KnownSince *int
		EIN        *string
		Fleet      *int64
	}{}), store.OwnerValues{})
	v := store.OwnerValues{Name: ownPtr("North Star Hauling"), Headcount: &h}
	raw, err := json.Marshal(v)
	contractOK(t, err)
	contractEqual(t, string(raw), `{"name":"North Star Hauling","headcount":{"count":26,"year":2024}}`)
	var got store.OwnerValues
	contractOK(t, json.Unmarshal(raw, &got))
	contractEqual(t, got, v)
	v = store.OwnerValues{Revenue: &store.OwnerRevenue{High: ownPtr(int64(2000000))}, People: []store.OwnerPerson{{Name: "Jane Doe", Role: ""}}}
	raw, err = json.Marshal(v)
	contractOK(t, err)
	contractEqual(t, string(raw), `{"revenue":{"high":2000000},"people":[{"name":"Jane Doe","role":""}]}`)
	contractOK(t, json.Unmarshal(raw, &got))
	contractEqual(t, got, v)
	v = store.OwnerValues{Address: &sources.Address{Street: "12 Main St", City: "Duluth", State: "MN", Zip: "55802"}}
	raw, err = json.Marshal(v)
	contractOK(t, err)
	contractEqual(t, string(raw), `{"address":{"street":"12 Main St","city":"Duluth","state":"MN","zip":"55802"}}`)
	got = store.OwnerValues{}
	contractOK(t, json.Unmarshal(raw, &got))
	contractEqual(t, got, v)
	raw, err = json.Marshal(store.OwnerValues{})
	contractOK(t, err)
	contractEqual(t, string(raw), "{}")
	contractOK(t, json.Unmarshal([]byte(`{"name":null,"headcount":null,"people":null,"address":null}`), &got))
	contractEqual(t, got, store.OwnerValues{})
	r := store.OwnerRecord{}
	contractEqual(t, store.OwnerRecord(struct {
		ID, Candidate           string
		Founded                 bool
		Note, UserID, UserEmail string
		At                      time.Time
		Set                     store.OwnerValues
		Cleared                 []string
	}(r)), r)
	contractEqual(t, store.OwnerRevenue(struct {
		Low, High *int64 `json:"-"`
	}{nil, nil}), store.OwnerRevenue{})
	contractEqual(t, store.OwnerPerson(struct {
		Name, Role string `json:"-"`
	}{"a", "b"}), store.OwnerPerson{Name: "a", Role: "b"})
}

// R-4QTB-D0OI
func TestOwnerValuesValidation(t *testing.T) {
	valid := store.OwnerValues{Name: ownPtr("North Star Hauling"), Headcount: &store.OwnerHeadcount{Count: 26, Year: 2024}, Revenue: &store.OwnerRevenue{High: ownPtr(int64(2000000))}, Category: ownPtr(sources.CategoryTrucking), Website: ownPtr("https://northstar.test/"), Address: &sources.Address{Street: "12 Main St", City: "Duluth", State: "MN", Zip: "55802", County: "MN-137"}, Phone: ownPtr("2185550100"), Email: ownPtr("ops@northstar.test"), People: []store.OwnerPerson{{Name: "Jane Doe"}}, KnownSince: ownPtr(1988), EIN: ownPtr("411234567"), Fleet: ownPtr(int64(0))}
	if !store.ValidOwnerValues(valid) || !store.ValidOwnerValues(store.OwnerValues{}) {
		t.Fatal("valid values rejected")
	}
	invalid := []store.OwnerValues{{Name: ownPtr("  ")}, {Headcount: &store.OwnerHeadcount{Count: -1, Year: 2024}}, {Headcount: &store.OwnerHeadcount{Count: 26, Year: 0}}, {Headcount: &store.OwnerHeadcount{Count: 26, Year: 10000}}, {Revenue: &store.OwnerRevenue{}}, {Revenue: &store.OwnerRevenue{Low: ownPtr(int64(3)), High: ownPtr(int64(2))}}, {Revenue: &store.OwnerRevenue{Low: ownPtr(int64(-1))}}, {Category: ownPtr("freight")}, {Address: &sources.Address{}}, {Address: &sources.Address{City: "Duluth", State: "mn"}}, {Address: &sources.Address{City: "Duluth", Zip: "5580"}}, {Address: &sources.Address{City: "Duluth", State: "MN", County: "MN-13"}}, {Phone: ownPtr("218-555-0100")}, {Phone: ownPtr("21855501000")}, {Email: ownPtr("ops@")}, {Email: ownPtr("ops@@northstar.test")}, {Email: ownPtr("ops @northstar.test")}, {People: []store.OwnerPerson{}}, {People: []store.OwnerPerson{{Name: "  "}}}, {KnownSince: ownPtr(0)}, {KnownSince: ownPtr(10000)}, {EIN: ownPtr("41-1234567")}, {Fleet: ownPtr(int64(-1))}}
	for _, w := range []string{"northstar.test", "ftp://northstar.test/", " https://northstar.test/", "https:///home", "https://:8080/", "https://www./", "http://203.0.113.5/", "https://[2001:db8::1]:8443/", "file:///x"} {
		invalid = append(invalid, store.OwnerValues{Website: ownPtr(w)})
	}
	for i, v := range invalid {
		if store.ValidOwnerValues(v) {
			t.Fatalf("invalid values %d accepted: %+v", i, v)
		}
	}
}

// R-Y77J-0BF4 R-U68H-5RJC
func TestOwnerSetOrdersClearsWithoutChangingInput(t *testing.T) {
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := store.OwnerRecord{ID: "a", At: stamp, Set: store.OwnerValues{Name: ownPtr("a"), Phone: ownPtr("2185550100")}}
	b := store.OwnerRecord{ID: "b", At: stamp.Add(time.Second), Cleared: []string{sources.FactName}}
	c := store.OwnerRecord{ID: "c", At: stamp.Add(2 * time.Second), Set: store.OwnerValues{Phone: ownPtr("2185550111")}}
	for _, rs := range [][]store.OwnerRecord{nil, {}, {a}, {a, b}, {c, b, a}, {b, a, c}} {
		before := append([]store.OwnerRecord(nil), rs...)
		got := store.OwnerSet(rs)
		if got == nil {
			t.Fatal("nil owner set")
		}
		if len(rs) == 0 {
			contractEqual(t, len(got), 0)
		} else {
			phone := a
			if len(rs) == 3 {
				phone = c
			}
			contractEqual(t, got[sources.FactPhone], phone)
			_, name := got[sources.FactName]
			contractEqual(t, name, len(rs) == 1)
			wantLen := 1
			if name {
				wantLen++
			}
			contractEqual(t, len(got), wantLen)
		}
		if len(rs) > 0 {
			contractEqual(t, rs, before)
		}
	}
	d := store.OwnerRecord{ID: "d", At: c.At, Set: store.OwnerValues{Name: ownPtr("d")}}
	contractEqual(t, store.OwnerSet([]store.OwnerRecord{d, a, b}), map[string]store.OwnerRecord{sources.FactName: d, sources.FactPhone: a})
	same := store.OwnerRecord{ID: "z", At: d.At, Set: store.OwnerValues{Name: ownPtr("z")}}
	contractEqual(t, store.OwnerSet([]store.OwnerRecord{same, d})[sources.FactName], d)
}

// R-892I-PK2H R-87UM-BSBS R-8AAF-3BT6 R-N5HM-KXZ3 R-YAV8-5MN7 R-YC34-JEDW
func TestOwnerRecordsRefusalsMintAndPersistence(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	calls := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { calls++; return f.now.Add(time.Duration(calls-1) * time.Second) }, Rand: f.rnd})
	values := store.OwnerValues{Name: ownPtr("Owner business")}
	r, c, initialErr := f.s.AddOwnerRecord(ctx, store.OwnerRecord{ID: "caller-id", Founded: false, At: time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC), Set: values, Note: "  Owner's observation  ", UserID: "owner-user", UserEmail: "owner@example.test"})
	contractOK(t, initialErr)
	contractEqual(t, calls, 1)
	contractEqual(t, r.Set, values)
	saved, savedErr := f.s.GetCandidate(ctx, c.ID)
	contractOK(t, savedErr)
	contractEqual(t, saved, c)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	if !store.ValidOwnerRecordID(r.ID) || !store.ValidCandidateID(c.ID) || !r.Founded {
		t.Fatal(r, c)
	}
	contractEqual(t, r.At, f.now.UTC().Truncate(time.Second))
	contractEqual(t, r.Note, "  Owner's observation  ")
	contractEqual(t, r.UserID, "owner-user")
	contractEqual(t, r.UserEmail, "owner@example.test")
	contractEqual(t, r.Cleared, []string{})
	bad := []store.OwnerRecord{{Candidate: c.ID, Set: store.OwnerValues{Name: ownPtr("name")}}, {Candidate: c.ID, Set: store.OwnerValues{Phone: ownPtr("218-555-0100")}}, {Candidate: c.ID, Cleared: []string{"newest_filing"}}, {Candidate: c.ID, Cleared: []string{sources.FactPhone, sources.FactPhone}}, {Candidate: c.ID, Set: store.OwnerValues{Name: ownPtr("name")}, Cleared: []string{sources.FactName}}, {Candidate: c.ID}, {Set: store.OwnerValues{Phone: ownPtr("2185550100")}}, {Set: store.OwnerValues{Name: ownPtr("name")}, Cleared: []string{sources.FactPhone}}}
	for i, v := range bad {
		if i != 0 {
			v.Note = "note"
		}
		v.UserID = "owner"
		out, candidate, err := f.s.AddOwnerRecord(ctx, v)
		if !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("%d: %v", i, err)
		}
		contractEqual(t, out, store.OwnerRecord{})
		contractEqual(t, candidate, store.Candidate{})
	}
	_, _, err := f.s.AddOwnerRecord(ctx, store.OwnerRecord{Note: "note", Set: store.OwnerValues{Name: ownPtr("name")}})
	contractError(t, err, store.ErrUnreachable)
	_, _, err = f.s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: "cand_0000000000000000", Note: "note", UserID: "u", Set: store.OwnerValues{Name: ownPtr("name")}})
	contractError(t, err, store.ErrNotFound)
	_, err = f.s.OwnerRecords(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	f.now = f.now.Add(time.Second)
	clearedRecord, _ := writeOwner(t, f, c.ID, store.OwnerValues{}, sources.FactPhone, sources.FactName)
	contractEqual(t, clearedRecord.Cleared, []string{sources.FactName, sources.FactPhone})
	contractEqual(t, clearedRecord.Founded, false)
	before, err := f.s.OwnerRecords(ctx, c.ID)
	contractOK(t, err)
	contractEqual(t, before, []store.OwnerRecord{clearedRecord, r})
	f.reopen()
	after, err := f.s.OwnerRecords(ctx, c.ID)
	contractOK(t, err)
	contractEqual(t, after, before)
	failing := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(nil)})
	out, candidate, err := failing.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: c.ID, Note: "note", UserID: "u", Set: store.OwnerValues{Name: ownPtr("name")}})
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, out, store.OwnerRecord{})
	contractEqual(t, candidate, store.Candidate{})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = f.s.AddOwnerRecord(canceled, store.OwnerRecord{})
	if !errors.Is(err, store.ErrUnreachable) || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f.d.SetFailing(true)
	_, _, err = f.s.AddOwnerRecord(ctx, store.OwnerRecord{})
	contractError(t, err, store.ErrUnreachable)
	_, err = f.s.OwnerRecords(ctx, c.ID)
	contractError(t, err, store.ErrUnreachable)
	f.d.SetFailing(false)
	// The record id is drawn before any newly founded candidate id.
	rand := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 16))
	s := store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rand})
	x, _, err := s.AddOwnerRecord(ctx, store.OwnerRecord{Set: store.OwnerValues{Name: ownPtr("Minted")}, Note: "mint", UserID: "u"})
	contractOK(t, err)
	contractEqual(t, x.ID, "own_5a5a5a5a5a5a5a5a")
	contractEqual(t, x.Candidate, "cand_5a5a5a5a5a5a5a5a")
	random := bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))
	s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: random})
	x, _, err = s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: c.ID, Set: store.OwnerValues{Name: ownPtr("collision")}, Note: "mint", UserID: "u"})
	contractOK(t, err)
	contractEqual(t, x.ID, "own_6b6b6b6b6b6b6b6b")
	contractEqual(t, random.Len(), 0)
	random = bytes.NewReader(bytes.Repeat([]byte{0x5a}, 65))
	s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: random})
	_, _, err = s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: c.ID, Set: store.OwnerValues{Name: ownPtr("collision")}, Note: "mint", UserID: "u"})
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, random.Len(), 1)
	after, err = f.s.OwnerRecords(ctx, c.ID)
	contractOK(t, err)
	contractEqual(t, len(after), len(before)+1)
}

// R-6KPB-5GOB R-U68H-5RJC R-U50K-RZSN R-FA1W-3DCK R-FCHO-UWTY R-SMYY-JNSO R-SQMN-OZ0R R-T59G-A7X3 R-NAD8-40XV R-4ODI-LH74
func TestOwnerGivenValuesAndClearsReachDisplayFactsAndEIN(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	o := contractObservation("owner-link", "Link name", time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC))
	o.Scraper = fmcsa.Name
	o.Reading.KnownSince = time.Date(2004, 1, 15, 0, 0, 0, 0, time.UTC)
	o.Reading.PowerUnits = ownPtr(int64(12))
	o.Reading.EINKey = "411234567"
	o.Reading.Phone = "3205550100"
	o.Reading.People = []sources.Person{{Name: "Link person", Role: "officer"}}
	id := contractAttach(t, f, "owner-links", o)[0]
	_, _, err := f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
	contractOK(t, err)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "good brief", false, sources.CategoryTrucking)})
	contractOK(t, err)
	first, _ := writeOwner(t, f, id, store.OwnerValues{Name: ownPtr("First owner name"), Address: &sources.Address{Street: "7 Elm St", City: "Rush City", State: "MN", Zip: "55069"}, Phone: ownPtr("3205550177"), KnownSince: ownPtr(1990), People: []store.OwnerPerson{{Name: "Old owner"}}, EIN: ownPtr("419876543")})
	f.now = f.now.Add(time.Second)
	second, c := writeOwner(t, f, id, store.OwnerValues{Name: ownPtr("Second owner name"), Phone: ownPtr("3205550188"), KnownSince: ownPtr(2001), Fleet: ownPtr(int64(9)), People: []store.OwnerPerson{{Name: "New owner"}}, Category: ownPtr(sources.CategoryRetail), Website: ownPtr("https://owner-co.test/")})
	contractEqual(t, c.Name, "Second owner name")
	contractEqual(t, c.NameSource, sources.Owner)
	contractEqual(t, c.NameRecord, sources.SourceKey(sources.Owner, second.ID))
	contractEqual(t, c.OtherNames[0].Value, "First owner name")
	contractEqual(t, c.OtherNames[0].Key, sources.SourceKey(sources.Owner, first.ID))
	contractEqual(t, c.EIN, "419876543")
	contractEqual(t, c.EINSource, sources.Owner)
	contractEqual(t, c.People[0].Value.Name, "New owner")
	contractEqual(t, c.People[1].Value.Name, "Link person")
	contractEqual(t, len(c.People), 2)
	facts, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, facts.KnownSince, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	contractEqual(t, facts.OtherKnownSince[0].Value, time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC))
	contractEqual(t, facts.OtherKnownSince[1].Value, o.Reading.KnownSince)
	contractEqual(t, *facts.PowerUnits, int64(9))
	contractEqual(t, facts.OtherPowerUnits[0].Value, int64(12))
	contractEqual(t, facts.Categories, []sources.Category{{Name: sources.CategoryRetail, Source: sources.Owner}})
	contractEqual(t, store.ClassifiedBy(facts.Categories), sources.Owner)
	contractEqual(t, facts.WebsiteSource, store.WebsiteByOwner)
	contractEqual(t, facts.WebsiteURL, "https://owner-co.test/")
	f.now = f.now.Add(time.Second)
	_, c = writeOwner(t, f, id, store.OwnerValues{}, sources.FactName, sources.FactPhone, sources.FactAddress, sources.FactKnownSince, sources.FactEIN, sources.FactFleet)
	contractEqual(t, c.Name, o.Reading.Name)
	contractEqual(t, c.Phone, o.Reading.Phone)
	contractEqual(t, c.EIN, "")
	contractEqual(t, c.EINSource, "")
	contractEqual(t, c.EINRecord, "")
	contractEqual(t, len(c.OtherPhones), 0)
	contractEqual(t, len(c.OtherAddresses), 0)
	facts, err = f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, facts.KnownSince, o.Reading.KnownSince)
	contractEqual(t, facts.OtherKnownSince, []sources.Sourced[time.Time]{})
	contractEqual(t, facts.OtherPowerUnits, []sources.Sourced[int64]{})
	f.reopen()
	got, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, c)
}

// R-EXUW-9NXM R-EZ2S-NFOB R-F0AP-17F0 R-F1IL-EZ5P R-BU65-1XKS R-BVE1-FPBH
func TestOwnerKeysMatchScrapesAndNewOwnerRecords(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	record, found := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("North Star Hauling Inc"), Address: &sources.Address{Street: "12 Main St", City: "Duluth", State: "MN", Zip: "55802"}, EIN: ownPtr("411234567"), Phone: ownPtr("2185550100"), Email: ownPtr("ops@northstar.test"), Website: ownPtr("https://northstar-hauling.test/")})
	links, err := f.s.Links(ctx, found.ID)
	contractOK(t, err)
	contractEqual(t, len(links), 0)
	o := contractObservation("matched-name", "Scraped name", time.Time{})
	o.Reading.NameKey = sources.NameKey("North Star Hauling")
	o.Reading.NameKeys = []string{o.Reading.NameKey}
	o.Reading.State = "MN"
	o.Reading.Zip = "55802"
	ids := contractAttach(t, f, "name", o)
	contractEqual(t, ids, []string{found.ID})
	links, err = f.s.Links(ctx, found.ID)
	contractOK(t, err)
	contractEqual(t, links[0].Reason, store.LinkMatchKey)
	got, err := f.s.GetCandidate(ctx, found.ID)
	contractOK(t, err)
	contractEqual(t, got.Name, found.Name)
	o.SourceID = "matched-ein"
	o.Reading.NameKey = "OTHER"
	o.Reading.NameKeys = nil
	o.Reading.Zip = "55101"
	o.Reading.EINKey = "411234567"
	contractEqual(t, contractAttach(t, f, "ein", o), []string{found.ID})
	for _, v := range []store.OwnerValues{{Name: ownPtr("Different"), EIN: ownPtr("411234567")}, {Name: ownPtr("North Star Hauling LLC"), Address: &sources.Address{State: "MN", Zip: "55802"}}, {Name: ownPtr("Different"), Phone: ownPtr("2185550100")}, {Name: ownPtr("Different"), Email: ownPtr("dispatch@northstar.test")}, {Name: ownPtr("Different"), Website: ownPtr("https://www.northstar-hauling.test/")}} {
		r, _ := writeOwner(t, f, "", v)
		contractEqual(t, r.Candidate, found.ID)
		contractEqual(t, r.Founded, false)
	}
	all, err := f.s.OwnerRecords(ctx, found.ID)
	contractOK(t, err)
	if !slicesContainsRecord(all, record.ID) {
		t.Fatal("founding record lost")
	}
	// A cleared owner address supplies no name-at-place key, and a link name does not pair with an owner address.
	f.now = f.now.Add(time.Second)
	writeOwner(t, f, found.ID, store.OwnerValues{}, sources.FactAddress)
	fresh := o
	fresh.SourceID = "unmatched-place"
	fresh.Reading.EINKey = ""
	fresh.Reading.NameKey = sources.NameKey("North Star Hauling")
	fresh.Reading.NameKeys = nil
	fresh.Reading.Zip = "55999"
	newIDs := contractAttach(t, f, "fresh", fresh)
	if newIDs[0] == found.ID {
		t.Fatal("cleared address still matched")
	}
	es, err := f.s.Enrichments(ctx, found.ID)
	contractOK(t, err)
	for _, e := range es {
		if e.Kind == enrichers.KindDriveRoute || e.Kind == sources.WebsiteCheck {
			t.Fatal("owner fetched", e.Kind)
		}
	}
	ps, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	contractEqual(t, len(ps), 0)
}
func slicesContainsRecord(rs []store.OwnerRecord, id string) bool {
	for _, r := range rs {
		if r.ID == id {
			return true
		}
	}
	return false
}

// R-Y5ZM-MJOF R-YAV8-5MN7 R-YC34-JEDW R-NCT0-VKF9
func TestOwnerRecordsFollowMergeChainsAndStayThroughSplits(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	a, x := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Owner original")})
	o := contractObservation("split-link", "Linked", time.Time{})
	o.Reading.NameKey = "LINKED"
	o.Reading.Zip = "55072"
	contractAttach(t, f, "split", o)
	ids, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, err)
	var y string
	for _, c := range ids {
		if c.ID != x.ID {
			y = c.ID
		}
	}
	f.now = f.now.Add(time.Second)
	b, _ := writeOwner(t, f, y, store.OwnerValues{Phone: ownPtr("2185550100")})
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: x.ID, Other: y, Why: "same", UserID: "owner"})
	contractOK(t, err)
	got, err := f.s.OwnerRecords(ctx, x.ID)
	contractOK(t, err)
	contractEqual(t, got, []store.OwnerRecord{b, a})
	original, err := f.s.OwnerRecords(ctx, y)
	contractOK(t, err)
	contractEqual(t, original, []store.OwnerRecord{b})
	_, cs, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: x.ID, Keys: []string{sources.SourceKey(o.Scraper, o.SourceID)}, Why: "separate", UserID: "owner"})
	contractOK(t, err)
	got, err = f.s.OwnerRecords(ctx, cs[1].ID)
	contractOK(t, err)
	contractEqual(t, got, []store.OwnerRecord{})
	got, err = f.s.OwnerRecords(ctx, x.ID)
	contractOK(t, err)
	contractEqual(t, got, []store.OwnerRecord{b, a})
	_, _, err = f.s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: y, Note: "merged", UserID: "u", Set: store.OwnerValues{Phone: ownPtr("2185550111")}})
	contractError(t, err, store.ErrInvalid)
	// New records racing on one store remain unique and whole.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, _, e := f.s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: x.ID, Set: store.OwnerValues{Phone: ownPtr("2185550111")}, Note: "concurrent", UserID: "u"})
			errs <- e
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		contractOK(t, e)
	}
	got, err = f.s.OwnerRecords(ctx, x.ID)
	contractOK(t, err)
	contractEqual(t, len(got), 10)
	unique := map[string]bool{}
	for _, r := range got {
		if unique[r.ID] {
			t.Fatal("duplicate id")
		}
		unique[r.ID] = true
	}
	f.reopen()
	persisted, err := f.s.OwnerRecords(ctx, x.ID)
	contractOK(t, err)
	contractEqual(t, persisted, got)
}

// R-LXL8-T7A3 R-T59G-A7X3 R-KRB2-I81N
func TestOwnerWebsiteDecisionAndInputs(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	q := "https://owner-co.test/path"
	_, c := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Owner site"), Website: &q, Email: ownPtr("ops@mail.test")})
	for _, w := range []website.Result{{State: sources.WebsiteHTTP, From: q, URL: "http://redirect.test/", Title: "redirected"}, {State: sources.WebsiteHTTPS, From: "mail.test", URL: "https://mail.test/", Misses: []website.Miss{{From: q, Error: "failed"}}}} {
		raw, err := json.Marshal(w)
		contractOK(t, err)
		f.now = f.now.Add(time.Second)
		es, err := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: c.ID, Kind: sources.WebsiteCheck, Inputs: website.OwnerInputs(q, "", "ops@mail.test"), Found: raw}})
		contractOK(t, err)
		source, state, url, title, why, checked, at := store.WebsiteDecisionOf(es, "", q)
		contractEqual(t, source, store.WebsiteByOwner)
		contractEqual(t, url, q)
		contractEqual(t, why, "")
		contractEqual(t, checked, "")
		contractEqual(t, at, es[0].At)
		if w.From == q {
			contractEqual(t, state, w.State)
			contractEqual(t, title, w.Title)
		} else {
			contractEqual(t, state, sources.WebsiteHTTPS)
			contractEqual(t, title, "")
		}
		facts, err := f.s.GetFacts(ctx, c.ID)
		contractOK(t, err)
		contractEqual(t, facts.WebsiteURL, q)
		contractEqual(t, facts.WebsiteSource, store.WebsiteByOwner)
	}
	subject, err := f.s.Subject(ctx, c.ID)
	contractOK(t, err)
	contractEqual(t, subject.Website, q)
	contractEqual(t, subject.WebsiteSource, sources.Owner)
	contractEqual(t, (enrichers.WebsiteSearch{}).Inputs(subject), "")
	contractEqual(t, (enrichers.WebsiteCheck{}).Inputs(subject), website.OwnerInputs(q, "", "ops@mail.test"))
}

// R-4PLE-Z8XT
func TestOwnerMigrationBytes(t *testing.T) {
	raw, err := fs.ReadFile(candidates.Migrations(), "0028_owner_records.sql")
	contractOK(t, err)
	want := "CREATE TABLE owner_records (id TEXT PRIMARY KEY, candidate TEXT NOT NULL, founded INTEGER NOT NULL, note TEXT NOT NULL, user_id TEXT NOT NULL, user_email TEXT NOT NULL, at TEXT NOT NULL, name TEXT, headcount INTEGER, headcount_year INTEGER, revenue_low INTEGER, revenue_high INTEGER, category TEXT, website TEXT, street TEXT, city TEXT, state TEXT, zip TEXT, county TEXT, phone TEXT, email TEXT, people TEXT, known_since INTEGER, ein TEXT, fleet INTEGER, cleared TEXT NOT NULL) WITHOUT ROWID;\nCREATE INDEX owner_records_candidate ON owner_records(candidate,at,id);\nCREATE INDEX owner_records_ein ON owner_records(ein);\nCREATE INDEX owner_records_place ON owner_records(state,zip);\n"
	contractEqual(t, string(raw), want)
}

// R-NGGQ-0VNC R-NHOM-ENE1
func TestOwnerMigrationUpgradeAndPreviousWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0027_page_reasons.sql")})
	contractOK(t, err)
	ids := []string{"cand_1111111111111111", "cand_2222222222222222"}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if e := ownerPriorInsertCandidate(ctx, tx, id); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, "UPDATE candidates SET status='merged',merged_into=? WHERE id=?", ids[0], ids[1])
		return e
	}))
	before := []store.Candidate{}
	// Read the previous row shape directly: the previous schema has no owner table.
	contractOK(t, d.Read(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			var c store.Candidate
			var names, people, cargo, namesLower, otherNames, otherAddresses, otherPhones, otherEmails string
			var created, updated string
			err := tx.QueryRowContext(ctx, "SELECT "+ownerPriorCandidateColumns+" FROM candidates WHERE id=?", id).Scan(&c.ID, &c.Status, &c.MergedInto, &c.Name, &names, &namesLower, &c.Street, &c.City, &c.State, &c.Zip, &c.County, &c.Phone, &c.Email, &people, &c.Class, &cargo, &created, &updated, &c.NameSource, &c.NameRecord, &c.AddressSource, &c.AddressRecord, &c.PhoneSource, &c.PhoneRecord, &c.EmailSource, &c.EmailRecord, &otherNames, &otherAddresses, &otherPhones, &otherEmails)
			if err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(names), &c.Names); err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(people), &c.People); err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(cargo), &c.Cargo); err != nil {
				return err
			}
			c.Created, _ = time.Parse(time.RFC3339, created)
			c.Updated, _ = time.Parse(time.RFC3339, updated)
			c.OtherNames = []sources.Sourced[string]{}
			c.OtherAddresses = []sources.Sourced[sources.Address]{}
			c.OtherPhones = []sources.Sourced[string]{}
			c.OtherEmails = []sources.Sourced[string]{}
			before = append(before, c)
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	f := &contractFixture{t: t, path: path, d: d, now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), rnd: &contractRandom{next: 30}}
	f.s = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	for i, id := range ids {
		rs, e := f.s.OwnerRecords(ctx, id)
		contractOK(t, e)
		contractEqual(t, rs, []store.OwnerRecord{})
		c, e := f.s.GetCandidate(ctx, id)
		contractOK(t, e)
		contractEqual(t, c, before[i])
	}
	c := contractAttach(t, f, "rollback-c", contractObservation("rollback-c", "Scraped c", time.Time{}))[0]
	o := contractObservation("rollback-d", "Scraped d", time.Time{})
	o.Reading.NameKey = "SCRAPEDD"
	o.Reading.Zip = "55101"
	dd := contractAttach(t, f, "rollback-d", o)[0]
	a, _ := writeOwner(t, f, c, store.OwnerValues{Name: ownPtr("Owner c")}, sources.FactPhone)
	b, n := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Founded on rollback")})
	added := "cand_3333333333333333"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if e := ownerPriorInsertCandidate(ctx, tx, added); e != nil {
			return e
		}
		if e := ownerPriorWriteFacts(ctx, tx, added); e != nil {
			return e
		}
		// Every candidate column before 0028 remains writable by the previous release.
		_, e := tx.ExecContext(ctx, `UPDATE candidates SET status='active',merged_into='',name='previous display',names='["previous display"]',names_lower='previous display',street='',city='',state='',zip='',county='',phone='',email='',people='[]',class='',cargo='[]',created='2026-01-01T00:00:00Z',updated='2026-01-02T00:00:00Z',name_source='',name_record='',address_source='',address_record='',phone_source='',phone_record='',email_source='',email_record='',other_names='[]',other_addresses='[]',other_phones='[]',other_emails='[]' WHERE id=?`, c)
		if e != nil {
			return e
		}
		if e = ownerPriorWriteFacts(ctx, tx, c); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE candidates SET status='merged',merged_into=? WHERE id=?", dd, c)
		return e
	}))
	contractOK(t, d.Read(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, "SELECT "+ownerPriorCandidateColumns+" FROM candidates WHERE id=?", n.ID)
		if e != nil {
			return e
		}
		defer func() { contractOK(t, rows.Close()) }()
		if !rows.Next() {
			return errors.New("founded candidate not readable")
		}
		values := make([]any, 30)
		for i := range values {
			values[i] = new(any)
		}
		if e = rows.Scan(values...); e != nil {
			return e
		}
		contractEqual(t, *(values[0].(*any)), any(n.ID))
		return rows.Err()
	}))
	for id, want := range map[string][]store.OwnerRecord{c: {a}, n.ID: {b}, dd: {a}, added: {}} {
		rs, e := f.s.OwnerRecords(ctx, id)
		contractOK(t, e)
		contractEqual(t, rs, want)
	}
}

// R-SMYY-JNSO
func TestOwnerEINHydratesEveryReadShape(t *testing.T) {
	for _, scraper := range []string{form5500.Name, oshaita.Name} {
		t.Run(scraper, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			o := contractObservation("form-ein", "Benefit company", time.Time{})
			o.Scraper = scraper
			o.Reading.EINKey = "411234567"
			id := contractAttach(t, f, "form", o)[0]
			check := func(value, source, key string) {
				t.Helper()
				c, e := f.s.GetCandidate(ctx, id)
				contractOK(t, e)
				contractEqual(t, []string{c.EIN, c.EINSource, c.EINRecord}, []string{value, source, key})
				list, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
				contractOK(t, e)
				contractEqual(t, list[0].EIN, value)
				k := prospectListCreate(t, f, "EIN values")
				read := prospectListRead(t, f, k)
				contractEqual(t, read.Members[0].Candidate, c)
			}
			_, err := f.s.Judge(ctx, []string{id})
			contractOK(t, err)
			check(o.Reading.EINKey, scraper, sources.SourceKey(scraper, o.SourceID))
			r, _ := writeOwner(t, f, id, store.OwnerValues{EIN: ownPtr("419876543")})
			check("419876543", sources.Owner, sources.SourceKey(sources.Owner, r.ID))
			f.now = f.now.Add(time.Second)
			writeOwner(t, f, id, store.OwnerValues{}, sources.FactEIN)
			check(o.Reading.EINKey, scraper, sources.SourceKey(scraper, o.SourceID))
			live := o
			live.Scraper = fmcsa.Name
			live.SourceID = "live-without-ein"
			live.Reading.EINKey = ""
			contractEqual(t, contractAttach(t, f, "live-run", live), []string{id})
			_, _, err = f.s.MarkGone(ctx, scraper, "absent-run", "", 100)
			contractOK(t, err)
			check("", "", "")

		})
	}
}

// R-BVE1-FPBH R-T59G-A7X3
func TestOwnerDerivedChangesOnlyChangedInputsAndRouteFallsBack(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55069": 30, "55802": 90}))
	o := contractObservation("derived", "Derived", time.Time{})
	o.Reading.Zip = "55069"
	id := contractAttach(t, f, "derive", o)[0]
	other := enrichmentCandidate(t, f, "untouched", "ops@untouched.test")
	var err error
	for _, kind := range []string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
		_, _, err = f.s.Derive(ctx, kind, "", false, 100)
		contractOK(t, err)
	}
	beforeC, e := f.s.GetCandidate(ctx, other)
	contractOK(t, e)
	beforeL, e := f.s.Links(ctx, other)
	contractOK(t, e)
	beforeE, e := f.s.Enrichments(ctx, other)
	contractOK(t, e)
	beforeF, e := f.s.GetFacts(ctx, other)
	contractOK(t, e)
	before, e := f.s.Enrichments(ctx, id)
	contractOK(t, e)
	f.s = routeStore(f, "base")
	sub, e := f.s.Subject(ctx, id)
	contractOK(t, e)
	appendRoute(t, f.s, id, (enrichers.DriveRoute{}).Inputs(sub), enrichers.RouteFound{Minutes: ptrInt(12), Precision: routes.PrecisionBuilding, Address: "route matched"})
	f.now = f.now.Add(time.Second)
	_, c := writeOwner(t, f, id, store.OwnerValues{Address: &sources.Address{Street: "12 Main St", City: "Duluth", State: "MN", Zip: "55802"}})
	after, e := f.s.Enrichments(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(after), len(before)+2)
	last := after[0]
	contractEqual(t, last.Kind, enrichers.KindDriveMinutes)
	contractEqual(t, last.Inputs, "55802")
	contractEqual(t, last.At, c.Updated)
	var minutes enrichers.MinutesFound
	contractOK(t, json.Unmarshal(last.Found, &minutes))
	contractEqual(t, *minutes.Minutes, int64(90))
	facts, e := f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, *facts.Minutes, int64(90))
	contractEqual(t, facts.MinutesSource, store.MinutesByZip)
	f.now = f.now.Add(time.Second)
	writeOwner(t, f, id, store.OwnerValues{Phone: ownPtr("2185550111")})
	same, e := f.s.Enrichments(ctx, id)
	contractOK(t, e)
	contractEqual(t, same, after)
	cc, e := f.s.GetCandidate(ctx, other)
	contractOK(t, e)
	contractEqual(t, cc, beforeC)
	ll, e := f.s.Links(ctx, other)
	contractOK(t, e)
	contractEqual(t, ll, beforeL)
	ee, e := f.s.Enrichments(ctx, other)
	contractOK(t, e)
	contractEqual(t, ee, beforeE)
	ff, e := f.s.GetFacts(ctx, other)
	contractOK(t, e)
	contractEqual(t, ff, beforeF)
	_, n := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("New derived"), Address: &sources.Address{Street: "22 Main St", City: "Duluth", State: "MN", Zip: "55802"}})
	es, e := f.s.Enrichments(ctx, n.ID)
	contractOK(t, e)
	contractEqual(t, len(es), 1)
	contractEqual(t, es[0].Kind, enrichers.KindDriveMinutes)
	ps, e := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, e)
	contractEqual(t, len(ps), 0)
	// Fill and rebuild see the same current owner inputs and repair projections.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "DELETE FROM facts WHERE candidate=?", id)
		return e
	}))
	_, e = f.s.FillFacts(ctx, "", 100)
	contractOK(t, e)
	ff, e = f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, *ff.Minutes, int64(90))
	_, e = f.s.Rebuild(ctx, "", 100)
	contractOK(t, e)
	cc, e = f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, cc.Zip, "55802")
}

// R-6KPB-5GOB R-U68H-5RJC R-U50K-RZSN R-FA1W-3DCK
func TestOwnerEqualTimeClearsAndInForceHistory(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	o := contractObservation("history-link", "BROWN HAWK LLC", time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC))
	o.Scraper = fmcsa.Name
	o.Reading.Names = nil
	o.Reading.Street = "5 Main St"
	o.Reading.City = "Rush City"
	o.Reading.State = "MN"
	o.Reading.Zip = "55069"
	o.Reading.Phone = "3205550100"
	o.Reading.People = []sources.Person{{Name: "JANE DOE", Role: "company officer"}}
	o.Reading.KnownSince = time.Date(2004, 1, 15, 0, 0, 0, 0, time.UTC)
	o.Reading.PowerUnits = ownPtr(int64(12))
	id := contractAttach(t, f, "history", o)[0]
	_, _, e := f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
	contractOK(t, e)
	put := func(n byte, month, day int, v store.OwnerValues, clears ...string) store.OwnerRecord {
		t.Helper()
		f.now = time.Date(2026, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: &contractRandom{next: n}})
		r, _ := writeOwner(t, f, id, v, clears...)
		return r
	}
	a := put(11, 1, 10, store.OwnerValues{Name: ownPtr("Brown Hawk Steel"), Address: &sources.Address{Street: "7 Elm St", City: "Rush City", State: "MN", Zip: "55069"}, Phone: ownPtr("3205550177"), People: []store.OwnerPerson{{Name: "JOHN ROE", Role: "owner"}}, KnownSince: ownPtr(1990)})
	b := put(12, 2, 10, store.OwnerValues{Name: ownPtr("Northern Lights Steel"), Address: &sources.Address{Street: "9 Oak Ave", City: "Rush City", State: "MN", Zip: "55069"}, Phone: ownPtr("3205550188"), People: []store.OwnerPerson{{Name: "MARY MAJOR", Role: "president"}}, KnownSince: ownPtr(2001)})
	put(13, 3, 10, store.OwnerValues{Fleet: ownPtr(int64(9))}, sources.FactPhone)
	put(19, 3, 10, store.OwnerValues{Phone: ownPtr("3205550199")})
	d := put(14, 4, 1, store.OwnerValues{Name: ownPtr("NL Steel Fab")})
	e5 := put(15, 4, 1, store.OwnerValues{Name: ownPtr("NLS")}, sources.FactAddress)
	got, e := f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, got.Name, "NL Steel Fab")
	contractEqual(t, got.NameRecord, sources.SourceKey(sources.Owner, d.ID))
	contractEqual(t, got.OtherNames, []sources.Sourced[string]{{Value: "NLS", Source: sources.Owner, Key: sources.SourceKey(sources.Owner, e5.ID)}, {Value: "Northern Lights Steel", Source: sources.Owner, Key: sources.SourceKey(sources.Owner, b.ID)}, {Value: "Brown Hawk Steel", Source: sources.Owner, Key: sources.SourceKey(sources.Owner, a.ID)}, {Value: o.Reading.Name, Source: fmcsa.Name, Key: sources.SourceKey(fmcsa.Name, o.SourceID)}})
	contractEqual(t, got.Street, "5 Main St")
	contractEqual(t, got.OtherAddresses, []sources.Sourced[sources.Address]{})
	contractEqual(t, got.Phone, o.Reading.Phone)
	contractEqual(t, got.OtherPhones, []sources.Sourced[string]{})
	contractEqual(t, got.People, []sources.Sourced[sources.Person]{{Value: sources.Person{Name: "MARY MAJOR", Role: "president"}, Source: sources.Owner, Key: sources.SourceKey(sources.Owner, b.ID)}, {Value: o.Reading.People[0], Source: fmcsa.Name, Key: sources.SourceKey(fmcsa.Name, o.SourceID)}})
	facts, e := f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.KnownSince, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	contractEqual(t, facts.OtherKnownSince, []sources.Sourced[time.Time]{{Value: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Source: sources.Owner, Key: sources.SourceKey(sources.Owner, a.ID)}, {Value: o.Reading.KnownSince, Source: fmcsa.Name, Key: sources.SourceKey(fmcsa.Name, o.SourceID)}})
	contractEqual(t, *facts.PowerUnits, int64(9))
	contractEqual(t, facts.OtherPowerUnits, []sources.Sourced[int64]{{Value: 12, Source: fmcsa.Name, Key: sources.SourceKey(fmcsa.Name, o.SourceID)}})
	later := put(16, 5, 1, store.OwnerValues{Phone: ownPtr("3205550166")})
	got, e = f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, got.Phone, "3205550166")
	contractEqual(t, got.PhoneRecord, sources.SourceKey(sources.Owner, later.ID))
	contractEqual(t, got.OtherPhones, []sources.Sourced[string]{{Value: o.Reading.Phone, Source: fmcsa.Name, Key: sources.SourceKey(fmcsa.Name, o.SourceID)}})
	put(17, 6, 1, store.OwnerValues{}, sources.FactPhone)
	// The lesser id at a clear's time remains ahead of the clear; the greater id does not.
	put(9, 6, 1, store.OwnerValues{Phone: ownPtr("3205550155")})
	got, e = f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, got.Phone, "3205550155")
	contractEqual(t, got.OtherPhones, []sources.Sourced[string]{{Value: o.Reading.Phone, Source: fmcsa.Name, Key: sources.SourceKey(fmcsa.Name, o.SourceID)}})
}

// R-EZ2S-NFOB R-BU65-1XKS
func TestOwnerAddressClearActuallyRemovesThePreviouslyMatchingKey(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	_, c := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Clearable Works"), Address: &sources.Address{State: "MN", Zip: "55802"}})
	same, _ := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Clearable Works LLC"), Address: &sources.Address{State: "MN", Zip: "55802"}})
	contractEqual(t, same.Candidate, c.ID)
	f.now = f.now.Add(time.Second)
	writeOwner(t, f, c.ID, store.OwnerValues{}, sources.FactAddress)
	o := contractObservation("cleared-key", "Clearable Works", time.Time{})
	o.Reading.NameKey = sources.NameKey("Clearable Works")
	o.Reading.NameKeys = nil
	o.Reading.State = "MN"
	o.Reading.Zip = "55802"
	ids := contractAttach(t, f, "cleared-key", o)
	if ids[0] == c.ID {
		t.Fatal("cleared owner key still matched")
	}
	rs, e := f.s.OwnerRecords(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, len(rs), 3)
}

// R-Y5ZM-MJOF R-NCT0-VKF9
func TestOwnerRecordsReadDuringWritesAndMergeThroughTwoHops(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	a, x := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("First")})
	b, y := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Second")})
	_, z := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Third")})
	var concurrent store.OwnerRecord
	var concurrentErr error
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	wg.Go(func() {
		<-start
		r, _, e := f.s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: x.ID, Set: store.OwnerValues{Phone: ownPtr("2185550100")}, Note: "concurrent", UserID: "u"})
		concurrent, concurrentErr = r, e
		if errors.Is(e, store.ErrInvalid) {
			e = nil
		}
		errs <- e
	})
	wg.Go(func() {
		<-start
		_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: y.ID, Other: x.ID, Why: "same", UserID: "u"})
		errs <- e
	})
	wg.Go(func() {
		<-start
		for range 5 {
			rs, e := f.s.OwnerRecords(ctx, y.ID)
			if e != nil {
				errs <- e
				return
			}
			for _, record := range rs {
				if record.ID == b.ID && (record.Candidate != y.ID || record.Set.Name == nil || *record.Set.Name != "Second" || record.Note != b.Note || record.UserID != b.UserID) {
					errs <- errors.New("reader saw partial record")
					return
				}
			}
			if !slicesContainsRecord(rs, b.ID) {
				errs <- errors.New("reader saw incomplete owner family")
				return
			}
		}
		errs <- nil
	})
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		contractOK(t, e)
	}
	_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: z.ID, Other: y.ID, Why: "same", UserID: "u"})
	contractOK(t, e)
	rs, e := f.s.OwnerRecords(ctx, z.ID)
	contractOK(t, e)
	if !slicesContainsRecord(rs, a.ID) || !slicesContainsRecord(rs, b.ID) {
		t.Fatal("two-hop records lost")
	}
	if concurrentErr == nil {
		if !slicesContainsRecord(rs, concurrent.ID) {
			t.Fatal("concurrent owner record lost through merge")
		}
		for _, r := range rs {
			if r.ID == concurrent.ID {
				contractEqual(t, r, concurrent)
			}
		}
	} else {
		contractError(t, concurrentErr, store.ErrInvalid)
		contractEqual(t, concurrent, store.OwnerRecord{})
	}
	for _, r := range rs {
		if r.ID == a.ID {
			contractEqual(t, r.Candidate, x.ID)
		}
	}
}

// R-LXL8-T7A3 R-T59G-A7X3
func TestOwnerWebsiteNewestRelevantCheckPreservesNewestWritten(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	q := "https://owner.test/"
	_, c := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Website history"), Website: &q, Phone: ownPtr("2185550111")})
	k := seedParks(t, f)[0]
	baseSweep(t, f, k.ID, places.Place{ID: "website-place", Name: "Website history", Address: "1 Main St, Duluth, MN 55802, USA", Phone: "2185550111", Website: "https://places.test/"})
	p, e := f.s.PlacesURL(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, p, "https://places.test/")
	add := func(inputs, from, state string) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		raw, e := json.Marshal(website.Result{From: from, URL: from, State: state})
		contractOK(t, e)
		_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: c.ID, Kind: sources.WebsiteCheck, Inputs: inputs, Found: raw}})
		contractOK(t, e)
	}
	add("https://places.test/", "https://places.test/", sources.WebsiteHTTP)
	add(q, q, sources.WebsiteHTTPS)
	facts, e := f.s.GetFacts(ctx, c.ID)
	contractOK(t, e)
	for _, w := range facts.Written {
		if w.Kind == sources.WebsiteCheck {
			contractEqual(t, w.At, f.now.UTC().Truncate(time.Second))
		}
	}
	add(q+"\nhttps://places.test/", q, sources.WebsiteHTTP)
	add(q, q, sources.WebsiteHTTPS)
	add("https://other.test/", "https://other.test/", sources.WebsiteHTTP)
	facts, e = f.s.GetFacts(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, facts.Website, sources.WebsiteHTTPS)
	for _, w := range facts.Written {
		if w.Kind == sources.WebsiteCheck {
			contractEqual(t, w.At, f.now.UTC().Truncate(time.Second))
		}
	}
	f.now = f.now.Add(time.Second)
	writeOwner(t, f, c.ID, store.OwnerValues{}, sources.FactWebsite)
	facts, e = f.s.GetFacts(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, facts.WebsiteSource, store.WebsiteByCheck)
	contractEqual(t, facts.WebsiteURL, "https://other.test/")

}

const ownerPriorCandidateColumns = "id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated,name_source,name_record,address_source,address_record,phone_source,phone_record,email_source,email_record,other_names,other_addresses,other_phones,other_emails"
const ownerPriorFactsColumns = "candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at,website_source,website_search_at,minutes_source,miles,route_address,drive_route_at,headcounts,brief_labels,known_since_source,newest_filing_source,website_checked_url"

func ownerPriorInsertCandidate(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO candidates("+ownerPriorCandidateColumns+") VALUES (?,'active','','old','[\"old\"]','old','','','','','','','','[]','','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','','','','','','','','','[]','[]','[]','[]')", id)
	return err
}
func ownerPriorWriteFacts(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO facts("+ownerPriorFactsColumns+") VALUES (?,'2020-01-01T00:00:00Z','pattern','2026-01-01T00:00:00Z','[]','pass',3,2,'https','https://old.test',42,'2026-01-02T00:00:00Z','','2026-01-03T00:00:00Z','','previous brief','','check','','zip',NULL,'','','[]','[]','','','') ON CONFLICT(candidate) DO UPDATE SET known_since=excluded.known_since,government_match=excluded.government_match,newest_filing=excluded.newest_filing,categories=excluded.categories,fleet_verdict=excluded.fleet_verdict,power_units=excluded.power_units,drivers=excluded.drivers,website=excluded.website,website_url=excluded.website_url,minutes=excluded.minutes,drive_minutes_at=excluded.drive_minutes_at,link_facts_at=excluded.link_facts_at,website_check_at=excluded.website_check_at,site_text_at=excluded.site_text_at,brief=excluded.brief,brief_at=excluded.brief_at,website_source=excluded.website_source,website_search_at=excluded.website_search_at,minutes_source=excluded.minutes_source,miles=excluded.miles,route_address=excluded.route_address,drive_route_at=excluded.drive_route_at,headcounts=excluded.headcounts,brief_labels=excluded.brief_labels,known_since_source=excluded.known_since_source,newest_filing_source=excluded.newest_filing_source,website_checked_url=excluded.website_checked_url", id)
	return err
}

// R-Y5ZM-MJOF
func TestOwnerMergedFoundingRecordDoesNotAllowScrapedSurvivorToSplitAllLinks(t *testing.T) {
	ctx := context.Background()
	f := contractDB(t)
	o := contractObservation("scraped-survivor", "Scraped survivor", time.Time{})
	id := contractAttach(t, f, "scraped", o)[0]
	_, founded := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Separate founded")})
	_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: id, Other: founded.ID, Why: "same", UserID: "u"})
	contractOK(t, e)
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: id, Keys: []string{sources.SourceKey(o.Scraper, o.SourceID)}, Why: "separate", UserID: "u"})
	contractError(t, e, store.ErrInvalid)
	rs, e := f.s.OwnerRecords(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(rs), 1)
	contractEqual(t, rs[0].Candidate, founded.ID)
}

// R-BU65-1XKS R-EXUW-9NXM
func TestOwnerMatchingRulesChooseFirstRuleAndOldestHolder(t *testing.T) {
	for _, tc := range []struct {
		name        string
		a, b, input store.OwnerValues
	}{
		{"EIN before name", store.OwnerValues{EIN: ownPtr("411234567")}, store.OwnerValues{Name: ownPtr("Named holder"), Address: &sources.Address{State: "MN", Zip: "55802"}}, store.OwnerValues{Name: ownPtr("Named holder"), Address: &sources.Address{State: "MN", Zip: "55802"}, EIN: ownPtr("411234567")}},
		{"name before phone", store.OwnerValues{Name: ownPtr("Named holder"), Address: &sources.Address{State: "MN", Zip: "55802"}}, store.OwnerValues{Phone: ownPtr("2185550100")}, store.OwnerValues{Name: ownPtr("Named holder"), Address: &sources.Address{State: "MN", Zip: "55802"}, Phone: ownPtr("2185550100")}},
		{"phone before email", store.OwnerValues{Phone: ownPtr("2185550100")}, store.OwnerValues{Email: ownPtr("ops@email.test")}, store.OwnerValues{Name: ownPtr("New business"), Phone: ownPtr("2185550100"), Email: ownPtr("new@email.test")}},
		{"email before website", store.OwnerValues{Email: ownPtr("ops@email.test")}, store.OwnerValues{Website: ownPtr("https://website.test/")}, store.OwnerValues{Name: ownPtr("New business"), Email: ownPtr("new@email.test"), Website: ownPtr("https://website.test/")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := contractDB(t)
			_, b := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("First seed")})
			f.now = f.now.Add(time.Second)
			_, a := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Second seed")})
			writeOwner(t, f, a.ID, tc.a)
			writeOwner(t, f, b.ID, tc.b)
			r, c := writeOwner(t, f, "", tc.input)
			contractEqual(t, r.Candidate, a.ID)
			contractEqual(t, c.ID, a.ID)
			contractEqual(t, r.Founded, false)
		})
	}
	for _, ownerFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "linked older", true: "owner older"}[ownerFirst], func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			var expected, other string
			if ownerFirst {
				_, c := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Owner first"), EIN: ownPtr("411234567")})
				expected = c.ID
				f.now = f.now.Add(time.Second)
			}
			o := contractObservation("oldest-linked", "Linked holder", time.Time{})
			if ownerFirst {
				o.Reading.EINKey = ""
			} else {
				o.Reading.EINKey = "411234567"
			}
			linked := contractAttach(t, f, "oldest", o)[0]
			if !ownerFirst {
				expected = linked
				f.now = f.now.Add(time.Second)
				_, c := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Owner later")})
				other = c.ID
			} else {
				o.Reading.EINKey = "411234567"
				o.Digest = "updated-ein"
				contractEqual(t, contractAttach(t, f, "update-ein", o), []string{linked})
			}
			if !ownerFirst {
				writeOwner(t, f, other, store.OwnerValues{EIN: ownPtr("411234567")})
			}
			r, _ := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Chosen"), EIN: ownPtr("411234567")})
			contractEqual(t, r.Candidate, expected)
			// Both the link and owner holder remain separate candidates.
			list, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
			contractOK(t, e)
			contractEqual(t, len(list), 2)
		})
	}
}

// R-EZ2S-NFOB R-EXUW-9NXM
func TestOwnerNamePlaceMatchingNeverPairsLinkNameWithOwnerAddress(t *testing.T) {
	f := contractDB(t)
	o := contractObservation("cross-name", "Link Name", time.Time{})
	o.Reading.Names = nil
	o.Reading.NameKey = sources.NameKey(o.Reading.Name)
	o.Reading.NameKeys = nil
	o.Reading.State = "MN"
	o.Reading.Zip = "55069"
	id := contractAttach(t, f, "cross", o)[0]
	writeOwner(t, f, id, store.OwnerValues{Name: ownPtr("Owner Name"), Address: &sources.Address{State: "MN", Zip: "55802"}})
	cross := o
	cross.SourceID = "cross-input"
	cross.Reading.Zip = "55802"
	ids := contractAttach(t, f, "cross-input", cross)
	if ids[0] == id {
		t.Fatal("link name paired with owner address")
	}
	cross.SourceID = "owner-pair"
	cross.Reading.NameKey = sources.NameKey("Owner Name")
	ids = contractAttach(t, f, "owner-pair", cross)
	contractEqual(t, ids, []string{id})
}

// R-28X9-3XVQ R-2A55-HPMF R-2UVF-ZT88 R-2W3C-DKYX R-2XB8-RCPM R-2YJ5-54GB R-2ZR1-IW70
func TestOwnerOnlyKeysMatchPlacesWithoutFoundingCandidates(t *testing.T) {
	for _, rule := range []string{store.LinkPhone, store.LinkDomain, store.LinkMatchKey} {
		t.Run(rule, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			v := store.OwnerValues{Name: ownPtr("Owner Works"), Address: &sources.Address{Street: "12 Main St", City: "Duluth", State: "MN", Zip: "55802"}, Phone: ownPtr("2185550100"), Website: ownPtr("https://owner-works.test/")}
			_, c := writeOwner(t, f, "", v)
			x := places.Place{ID: "owner-place", Name: "Different place", Address: "8 Other St, Duluth, MN 55802, USA", Phone: "2185550100"}
			if rule != store.LinkPhone {
				x.Phone = "2185550199"
				x.Website = "https://owner-works.test/"
			}
			if rule == store.LinkMatchKey {
				x.Website = "https://other.test/"
				x.Name = "Owner Works"
			}
			k := seedParks(t, f)[0]
			p, w := baseSweep(t, f, k.ID, x)
			contractEqual(t, w.NewCandidates, int64(0))
			ls, e := f.s.Links(ctx, c.ID)
			contractOK(t, e)
			contractEqual(t, len(ls), 1)
			contractEqual(t, ls[0].Reason, rule)
			confidence := 0.85
			if rule == store.LinkMatchKey {
				confidence = 0.8
			}
			contractEqual(t, ls[0].Confidence, confidence)
			got, e := f.s.GetCandidate(ctx, c.ID)
			contractOK(t, e)
			contractEqual(t, got.Name, "Owner Works")
			contractEqual(t, got.NameSource, sources.Owner)
			contractEqual(t, got.Street, "12 Main St")
			_, e = f.s.FinishWalk(ctx, p.ID, store.Counts{Found: 1}, false)
			contractOK(t, e)
		})
	}
}

// R-8AAF-3BT6 R-87UM-BSBS
func TestOwnerCandidateMintFailureRollsBackOwnerRecord(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x61}, 8))})
	r, c, e := f.s.AddOwnerRecord(ctx, store.OwnerRecord{Set: store.OwnerValues{Name: ownPtr("Cannot mint candidate")}, Note: "observed", UserID: "u"})
	contractError(t, e, store.ErrUnreachable)
	contractEqual(t, r, store.OwnerRecord{})
	contractEqual(t, c, store.Candidate{})
	cs, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, e)
	contractEqual(t, len(cs), 0)
	f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x61}, 16))})
	r, c, e = f.s.AddOwnerRecord(ctx, store.OwnerRecord{Set: store.OwnerValues{Name: ownPtr("Retry candidate")}, Note: "observed", UserID: "u"})
	contractOK(t, e)
	contractEqual(t, r.ID, "own_6161616161616161")
	rs, e := f.s.OwnerRecords(ctx, c.ID)
	contractOK(t, e)
	contractEqual(t, rs, []store.OwnerRecord{r})
}

// R-SQMN-OZ0R R-T59G-A7X3
func TestOwnerKnownSinceYearOneFollowsTheSharedPick(t *testing.T) {
	for _, prior := range []bool{false, true} {
		t.Run(map[bool]string{false: "link fallback", true: "older owner fallback"}[prior], func(t *testing.T) {
			ctx := context.Background()
			f := contractDB(t)
			o := contractObservation("year-one", "Year One Works", time.Time{})
			o.Scraper = form5500.Name
			o.Reading.KnownSince = time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)
			id := contractAttach(t, f, "year-one", o)[0]
			_, _, err := f.s.Derive(ctx, enrichers.KindLinkFacts, "", false, 100)
			contractOK(t, err)
			if prior {
				writeOwner(t, f, id, store.OwnerValues{KnownSince: ownPtr(2000)})
			}
			f.now = f.now.Add(time.Second)
			writeOwner(t, f, id, store.OwnerValues{KnownSince: ownPtr(1)})
			facts, err := f.s.GetFacts(ctx, id)
			contractOK(t, err)
			wantDate, wantSource := o.Reading.KnownSince, form5500.Name
			if prior {
				wantDate, wantSource = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), sources.Owner
			}
			contractEqual(t, facts.KnownSince, wantDate)
			contractEqual(t, facts.KnownSinceSource, wantSource)
		})
	}
}

// R-892I-PK2H R-87UM-BSBS
func TestOwnerRefusalsPreserveAllObservedRecords(t *testing.T) {
	f := contractDB(t)
	ctx := t.Context()
	_, a := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Kept active")})
	_, b := writeOwner(t, f, "", store.OwnerValues{Name: ownPtr("Kept merged")})
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: a.ID, Other: b.ID, Why: "same", UserID: "owner"})
	contractOK(t, err)
	contractOK(t, f.s.MarkCampaignUseOpened(ctx, a.ID, "campaign", f.now))
	snapshot := func() any {
		t.Helper()
		type records struct {
			Candidate   store.Candidate
			Owners      []store.OwnerRecord
			Facts       store.Facts
			Uses        []store.CampaignUse
			Links       []store.Link
			Estimates   []sources.Estimate
			Judgments   store.Judgments
			Enrichments []store.Enrichment
			Rulings     []store.Ruling
		}
		byID := map[string]records{}
		for _, id := range []string{a.ID, b.ID} {
			c, e := f.s.GetCandidate(ctx, id)
			contractOK(t, e)
			os, e := f.s.OwnerRecords(ctx, id)
			contractOK(t, e)
			fs, e := f.s.GetFacts(ctx, id)
			if e != nil {
				contractError(t, e, store.ErrNotFound)
			}
			uses, e := f.s.CampaignUses(ctx, id)
			contractOK(t, e)
			links, e := f.s.Links(ctx, id)
			contractOK(t, e)
			estimates, e := f.s.Estimates(ctx, id)
			contractOK(t, e)
			judgments, e := f.s.GetJudgments(ctx, id)
			if e != nil {
				contractError(t, e, store.ErrNotFound)
			}
			enrichments, e := f.s.Enrichments(ctx, id)
			contractOK(t, e)
			rulings, e := f.s.Rulings(ctx, id)
			contractOK(t, e)
			byID[id] = records{c, os, fs, uses, links, estimates, judgments, enrichments, rulings}
		}
		candidates, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
		contractOK(t, e)
		passes, e := f.s.ListPasses(ctx, "", "", 100)
		contractOK(t, e)
		return struct {
			Candidates []store.Candidate
			Passes     []store.Pass
			Records    map[string]records
		}{candidates, passes, byID}
	}
	before := snapshot()
	valid := func(id string) store.OwnerRecord {
		return store.OwnerRecord{ID: "caller-id", Founded: true, At: f.now.AddDate(-1, 0, 0), Candidate: id, UserID: "owner", Note: "note", Set: store.OwnerValues{Name: ownPtr("new name")}}
	}
	for _, tc := range []struct {
		name                 string
		r                    store.OwnerRecord
		want                 error
		cancel, fail, random bool
	}{
		{name: "empty user before malformed and missing", r: store.OwnerRecord{Candidate: "missing"}, want: store.ErrUnreachable},
		{name: "malformed before missing", r: store.OwnerRecord{Candidate: "missing", UserID: "owner", Note: " "}, want: store.ErrInvalid},
		{name: "malformed before merged", r: store.OwnerRecord{Candidate: b.ID, UserID: "owner", Note: "note", Set: store.OwnerValues{Phone: ownPtr("218-555-0100")}}, want: store.ErrInvalid},
		{name: "missing before mint", r: valid("cand_0000000000000000"), want: store.ErrNotFound, random: true},
		{name: "merged before mint", r: valid(b.ID), want: store.ErrInvalid, random: true},
		{name: "mint failure", r: valid(a.ID), want: store.ErrUnreachable, random: true},
		{name: "failing before malformed", r: store.OwnerRecord{}, want: store.ErrUnreachable, fail: true},
		{name: "context before failing", r: store.OwnerRecord{}, want: store.ErrUnreachable, fail: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := f.s
			if tc.random {
				s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(nil)})
			}
			callctx := ctx
			if tc.cancel {
				var cancel context.CancelFunc
				callctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			f.d.SetFailing(tc.fail)
			r, c, e := s.AddOwnerRecord(callctx, tc.r)
			f.d.SetFailing(false)
			contractError(t, e, tc.want)
			if tc.cancel && !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			contractEqual(t, r, store.OwnerRecord{})
			contractEqual(t, c, store.Candidate{})
			contractEqual(t, snapshot(), before)
		})
	}
}
