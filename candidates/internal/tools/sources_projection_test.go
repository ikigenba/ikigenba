package tools

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-3063-RGEW R-2KBE-SFRV R-31E0-585L
func TestSourceAnswerShapesAndProjection(t *testing.T) {
	_ = Person{"name", "role", "source"}
	_ = Sourced{"value", "source"}
	_ = SourcedAddress{"street", "city", "state", "zip", "county", "source"}
	_ = Sources{"name", "address", "phone", "email", nil, nil, nil, nil}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{Person{"n", "r", "s"}, `{"name":"n","role":"r","source":"s"}`},
		{Person{"n", "r", ""}, `{"name":"n","role":"r"}`},
		{Sourced{}, `{"value":"","source":""}`},
		{SourcedAddress{}, `{"street":"","city":"","state":"","zip":"","county":"","source":""}`},
		{Sources{}, `{"name":"","address":"","phone":"","email":"","other_names":[],"other_addresses":[],"other_phones":[],"other_emails":[]}`},
		{Sources{"n", "a", "p", "e", []Sourced{{"alias", "ns"}}, []SourcedAddress{{"street", "city", "state", "zip", "county", "as"}}, []Sourced{{"phone", "ps"}}, []Sourced{{"email", "es"}}}, `{"name":"n","address":"a","phone":"p","email":"e","other_names":[{"value":"alias","source":"ns"}],"other_addresses":[{"street":"street","city":"city","state":"state","zip":"zip","county":"county","source":"as"}],"other_phones":[{"value":"phone","source":"ps"}],"other_emails":[{"value":"email","source":"es"}]}`},
	} {
		b, err := json.Marshal(tc.value)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%T: %s, %v; want %s", tc.value, b, err, tc.want)
		}
	}
	c := store.Candidate{People: []sources.Sourced[sources.Person]{{Value: sources.Person{Name: "first", Role: "owner"}, Source: "form5500", Key: "private-record"}, {Value: sources.Person{Name: "legacy", Role: "manager"}}}, NameSource: "places", AddressSource: "osha_ita", PhoneSource: "places", EmailSource: "fmcsa", OtherNames: []sources.Sourced[string]{{Value: "alias", Source: "fmcsa", Key: "hidden-key"}}, OtherAddresses: []sources.Sourced[sources.Address]{{Value: sources.Address{Street: "alternate", City: "town", State: "MN", Zip: "55001", County: "county"}, Source: "ppp"}}, OtherPhones: []sources.Sourced[string]{{Value: "other-phone", Source: "phone-source"}}, OtherEmails: []sources.Sourced[string]{{Value: "other-email", Source: "email-source"}}}
	want := Sources{"places", "osha_ita", "places", "fmcsa", []Sourced{{"alias", "fmcsa"}}, []SourcedAddress{{"alternate", "town", "MN", "55001", "county", "ppp"}}, []Sourced{{"other-phone", "phone-source"}}, []Sourced{{"other-email", "email-source"}}}
	if got := sourcesOf(c); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	if got := candidateOf(c).People; !reflect.DeepEqual(got, []Person{{"first", "owner", "form5500"}, {"legacy", "manager", ""}}) {
		t.Fatal(got)
	}
	if got := linkOf(store.Link{Reading: sources.Reading{People: []sources.Person{{Name: "first", Role: "owner"}}}}).People; !reflect.DeepEqual(got, []Person{{"first", "owner", ""}}) {
		t.Fatal(got)
	}
}

// R-32LW-IZWA R-2LJB-67IK
func TestFactsSourceDatesAndCheckedURL(t *testing.T) {
	d := time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ checked, landed, want string }{
		{"", "https://acme.test/", ""},
		{"https://acme.test/", "https://www.acme.test/", "https://acme.test/"},
		{"https://acme.test/", "https://acme.test/", ""},
		{"acme-mail.test", "https://www.acme-mail.test/", "https://acme-mail.test"},
		{"acme-mail.test", "https://acme-mail.test/", ""},
		{"acme-mail.test", "https://acme-mail.test", ""},
		{"http://www.acme.test/", "https://acme.test/", "http://www.acme.test/"},
	} {
		f := store.Facts{KnownSince: d, KnownSinceSource: "fmcsa", NewestFiling: d, NewestFilingSource: "ppp", WebsiteURL: tc.landed, WebsiteCheckedURL: tc.checked}
		got := factsOf(f, "")
		if got.KnownSinceSource != "fmcsa" || got.NewestFilingSource != "ppp" || got.WebsiteCheckedURL != tc.want {
			t.Fatal(got, tc)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(b, &obj); err != nil {
			t.Fatal(err)
		}
		if (obj["website_checked_url"] != nil) != (tc.want != "") {
			t.Fatal(string(b))
		}
	}
	for _, known := range []time.Time{{}, d} {
		got := factsOf(store.Facts{KnownSince: known, KnownSinceSource: "fmcsa", NewestFilingSource: "ppp"}, "")
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if err = json.Unmarshal(b, &obj); err != nil {
			t.Fatal(err)
		}
		if (obj["known_since_source"] != nil) != (!known.IsZero()) || obj["newest_filing"] != nil || obj["newest_filing_source"] != nil {
			t.Fatal(string(b))
		}
	}
}

// R-2LJB-67IK R-2MR7-JZ99
func TestPopulatedFactsAndDetailSourceEncoding(t *testing.T) {
	facts := Facts{KnownSince: "known", KnownSinceSource: "known-source", NewestFiling: "filed", NewestFilingSource: "filed-source", WebsiteCheckedURL: "https://checked.test/"}
	b, err := json.Marshal(facts)
	want := `{"known_since":"known","known_since_source":"known-source","newest_filing":"filed","newest_filing_source":"filed-source","categories":[],"classified_by":"","records_differ":false,"record_categories":[],"fleet_verdict":"","website":"","website_source":"","website_checked_url":"https://checked.test/","minutes_source":"","written":[],"headcounts":[],"brief_labels":[]}`
	if err != nil || string(b) != want {
		t.Fatal(string(b), want, err)
	}
	candidate, err := json.Marshal(Candidate{})
	if err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(Detail{Sources: &Sources{Name: "name-source", Address: "address-source", Phone: "phone-source", Email: "email-source"}})
	want = `{"candidate":` + string(candidate) + `,"sources":{"name":"name-source","address":"address-source","phone":"phone-source","email":"email-source","other_names":[],"other_addresses":[],"other_phones":[],"other_emails":[]},"estimates":[],"links":[],"rulings":[],"pages":[],"enrichment_results":[],"rejects":[],"campaigns":[]}`
	if err != nil || string(b) != want {
		t.Fatal(string(b), want, err)
	}
}
