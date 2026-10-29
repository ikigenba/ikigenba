package pages

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-9M7V-YK4E R-9NFS-CBV3
func TestCandidateSourcedValues(t *testing.T) {
	chosen := sources.Address{Street: "9 Oak Ave", City: "Duluth", State: "MN", Zip: "55802", County: "MN-137"}
	other := sources.Address{Street: "12 Main St", City: "Duluth", State: "MN", Zip: "55802", County: "MN-137"}
	sameLine := chosen
	sameLine.County = "MN-001"
	duplicate := other
	duplicate.County = "MN-001"
	c := store.Candidate{ID: "chosen-candidate", Status: store.StatusActive, Name: "Acme Trucking", Street: chosen.Street, City: chosen.City, State: chosen.State, Zip: chosen.Zip, County: chosen.County, NameSource: places.Name, AddressSource: oshaita.Name, PhoneSource: fmcsa.Name, EmailSource: ppp.Name, OtherNames: []sources.Sourced[string]{{Value: "ACME TRUCKING LLC", Source: fmcsa.Name}, {Value: "ACME TRANSPORT", Source: form5500.Name}}, OtherAddresses: []sources.Sourced[sources.Address]{{Value: sameLine, Source: oshaita.Name}, {Value: other, Source: ppp.Name}, {Value: duplicate, Source: form5500.Name}}, OtherPhones: []sources.Sourced[string]{{Value: "2185550100", Source: fmcsa.Name}}, People: []sources.Sourced[sources.Person]{{Value: sources.Person{Name: "Ann Lee", Role: "Owner"}, Source: fmcsa.Name}}}
	got := candidateView(c)
	if got.Name != c.Name || got.NameSource != places.Name || got.AddressSource != oshaita.Name || got.PhoneSource != fmcsa.Name || got.EmailSource != ppp.Name || !reflect.DeepEqual(got.OtherNames, []OtherRow{{"ACME TRUCKING LLC", fmcsa.Name}, {"ACME TRANSPORT", form5500.Name}}) || !reflect.DeepEqual(got.OtherAddresses, []OtherRow{{"12 Main St, Duluth, MN 55802", ppp.Name}}) || !reflect.DeepEqual(got.OtherPhones, []OtherRow{{"2185550100", fmcsa.Name}}) || !reflect.DeepEqual(got.People, []PersonRow{{"Ann Lee", "Owner", fmcsa.Name}}) {
		t.Fatal(got)
	}
	c.Status = store.StatusMerged
	got = candidateView(c)
	if !reflect.DeepEqual(got, CandidateView{OtherEmails: []OtherRow{}, OwnerSet: map[string]OwnerNoteView{}, ID: c.ID, Status: store.StatusMerged, OtherNames: []OtherRow{}, OtherAddresses: []OtherRow{}, OtherPhones: []OtherRow{}, People: []PersonRow{}, Cargo: []string{}}) {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		a    sources.Address
		line string
	}{{chosen, "9 Oak Ave, Duluth, MN 55802"}, {sources.Address{City: "Duluth", State: "MN"}, "Duluth, MN"}, {sources.Address{Street: "12 Main St", Zip: "55802"}, "12 Main St, 55802"}, {sources.Address{County: "MN-137"}, ""}} {
		if got := addressLine(tc.a); got != tc.line {
			t.Fatal(got, tc)
		}
	}
	empty := candidateView(store.Candidate{Status: store.StatusActive})
	if empty.OtherNames == nil || empty.OtherAddresses == nil || empty.OtherPhones == nil || empty.People == nil || empty.Cargo == nil || empty.NameSource != "" || empty.AddressSource != "" || empty.PhoneSource != "" || empty.EmailSource != "" {
		t.Fatal(empty)
	}
}

// R-0CCH-S231
func TestFactsKeepDateSourcesOnlyBesideDates(t *testing.T) {
	for _, known := range []time.Time{{}, time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC)} {
		for _, newest := range []time.Time{{}, time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC)} {
			got := factsView(store.Facts{KnownSince: known, KnownSinceSource: form5500.Name, NewestFiling: newest, NewestFilingSource: fmcsa.Name}, "")
			ks, nf := "", ""
			if !known.IsZero() {
				ks = form5500.Name
			}
			if !newest.IsZero() {
				nf = fmcsa.Name
			}
			if got.KnownSinceSource != ks || got.NewestFilingSource != nf {
				t.Fatal(got)
			}
		}
	}
}

// R-02LA-PW5H
func TestWebsiteCheckedURLAndPlacesFields(t *testing.T) {
	at := fixtureTime()
	for _, tc := range []struct{ offered, inputs, from, landed, checked string }{{"https://acme.test/", website.Inputs("https://acme.test/", "ops@acme-mail.test"), "https://acme.test/", "https://www.acme.test/", "https://acme.test/"}, {"https://acme.test/", website.Inputs("https://acme.test/", "ops@acme-mail.test"), "https://acme.test/", "https://acme.test/", ""}, {"", "acme-mail.test", "acme-mail.test", "https://acme-mail.test/", ""}, {"", "acme-mail.test", "acme-mail.test", "https://acme-mail.test", ""}, {"", "acme-mail.test", "acme-mail.test", "https://www.acme-mail.test/", "https://acme-mail.test"}, {"", website.Inputs("http://www.acme.test/", ""), "http://www.acme.test/", "https://acme.test/", "http://www.acme.test/"}} {
		es := []store.Enrichment{{Kind: sources.WebsiteCheck, Inputs: tc.inputs, At: at, Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: tc.landed, From: tc.from})}}
		var got CandidateView
		setWebsite(&got, es, tc.offered, "")
		if got.WebsiteSource != store.WebsiteByCheck || got.WebsiteURL != tc.landed || got.WebsiteCheckedURL != tc.checked {
			t.Fatal(got, tc)
		}
	}
	var got CandidateView
	setWebsite(&got, nil, "https://acme.test/", "")
	if got.WebsiteSource != store.WebsiteByPlaces || got.Website != sources.WebsiteUnknown || got.WebsiteURL != "" || got.WebsiteCheckedURL != "" || got.WebsiteChecked != "" {
		t.Fatal(got)
	}
	setWebsite(&got, []store.Enrichment{{Kind: sources.WebsiteCheck, Inputs: "acme-mail.test", At: at, Found: encodedJSON(website.Result{State: sources.WebsiteNone, Title: "chosen-title"})}}, "https://acme.test/", "")
	if got.WebsiteSource != store.WebsiteByPlaces || got.Website != sources.WebsiteUnknown || got.WebsiteURL != "" || got.WebsiteCheckedURL != "" || got.WebsiteTitle != "chosen-title" || got.WebsiteChecked != at.Format(time.RFC3339) {
		t.Fatal(got)
	}
}

// R-9TJA-96KK
func TestSourcedCandidateTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	identifiers := []string{fmcsa.Name, ppp.Name, form5500.Name, oshaita.Name, places.Name, store.WebsiteByCheck, store.WebsiteBySearch, sources.Owner, sources.Brief, sources.Route, "chosen-unknown-source", ""}
	for _, source := range identifiers {
		for _, street := range []string{"chosen-street", ""} {
			for count := 0; count <= 2; count++ {
				for _, rowSource := range []string{source, ""} {
					for _, dated := range []bool{false, true} {
						for _, checked := range []bool{false, true} {
							d := CandidateData{Candidate: CandidateView{Status: store.StatusActive, Name: "chosen-name", Street: street, City: "chosen-city", Phone: "chosen-phone", Email: "chosen-email", NameSource: source, AddressSource: source, PhoneSource: source, EmailSource: source, WebsiteSource: store.WebsiteByCheck, Website: sources.WebsiteHTTPS}, Enrichments: &EnrichmentsView{Facts: &FactsView{}}}
							values := []string{}
							for i := 0; i < count; i++ {
								suffix := string(rune('a' + i))
								n, a, p := "chosen-other-name-"+suffix, "chosen-other-address-"+suffix, "chosen-other-phone-"+suffix
								d.Candidate.OtherNames = append(d.Candidate.OtherNames, OtherRow{n, rowSource})
								d.Candidate.OtherAddresses = append(d.Candidate.OtherAddresses, OtherRow{a, rowSource})
								d.Candidate.OtherPhones = append(d.Candidate.OtherPhones, OtherRow{p, rowSource})
								values = append(values, n, a, p)
							}
							d.Candidate.People = []PersonRow{{"chosen-person", "chosen-role", rowSource}, {"chosen-other-person", "", ""}}
							values = append(values, "chosen-person", "chosen-other-person")
							if dated {
								d.Enrichments.Facts.KnownSince = "chosen-known"
								d.Enrichments.Facts.NewestFiling = "chosen-newest"
								d.Enrichments.Facts.KnownSinceSource = source
								d.Enrichments.Facts.NewestFilingSource = source
							}
							if checked {
								d.Candidate.WebsiteCheckedURL = "https://chosen-checked.test/"
								values = append(values, d.Candidate.WebsiteCheckedURL)
							}
							var body bytes.Buffer
							if err := set.templates.ExecuteTemplate(&body, "candidate", d); err != nil {
								t.Fatal(err)
							}
							for _, value := range values {
								if !strings.Contains(body.String(), value) {
									t.Fatal("missing supplied value", value)
								}
							}
							if source == "chosen-unknown-source" && !strings.Contains(body.String(), source) {
								t.Fatal("missing supplied source")
							}
						}
					}
				}
			}
		}
	}
	for _, source := range []string{store.WebsiteByPlaces, store.WebsiteByOwner} {
		for _, checked := range []string{"", "chosen-time"} {
			d := CandidateData{Candidate: CandidateView{Status: store.StatusActive, WebsiteSource: source, Website: sources.WebsiteUnknown, WebsiteChecked: checked}}
			if source == store.WebsiteByOwner {
				d.Candidate.Website = sources.WebsiteHTTPS
				d.Candidate.WebsiteURL = "https://chosen-owner.test/"
			}
			if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", d); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// R-9TJA-96KK
func TestSourcedTemplatePreservesEachUnknownSource(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	d := CandidateData{Candidate: CandidateView{Status: store.StatusActive, Name: "name-value", Street: "street-value", Phone: "phone-value", Email: "email-value", NameSource: "name-source-value", AddressSource: "address-source-value", PhoneSource: "phone-source-value", EmailSource: "email-source-value", OtherNames: []OtherRow{{"other-name-value", "other-name-source-value"}}, OtherAddresses: []OtherRow{{"other-address-value", "other-address-source-value"}}, OtherPhones: []OtherRow{{"other-phone-value", "other-phone-source-value"}}, People: []PersonRow{{"person-value", "role-value", "person-source-value"}}}, Enrichments: &EnrichmentsView{Facts: &FactsView{KnownSince: "known-date-value", KnownSinceText: "known-date-text-value", KnownSinceSource: "known-source-value", NewestFiling: "newest-date-value", NewestFilingText: "newest-date-text-value", NewestFilingSource: "newest-source-value"}}}
	var body bytes.Buffer
	if err := set.templates.ExecuteTemplate(&body, "candidate", d); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{d.Candidate.NameSource, d.Candidate.AddressSource, d.Candidate.PhoneSource, d.Candidate.EmailSource, d.Candidate.OtherNames[0].Source, d.Candidate.OtherAddresses[0].Source, d.Candidate.OtherPhones[0].Source, d.Candidate.People[0].Source, d.Enrichments.Facts.KnownSinceSource, d.Enrichments.Facts.NewestFilingSource} {
		if !strings.Contains(body.String(), value) {
			t.Fatal("missing supplied source", value)
		}
	}
}
