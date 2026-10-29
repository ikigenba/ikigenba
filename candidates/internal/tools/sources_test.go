package tools_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

func sourceAnswer(c store.Candidate) *tools.Sources {
	out := &tools.Sources{Name: c.NameSource, Address: c.AddressSource, Phone: c.PhoneSource, Email: c.EmailSource, OtherNames: []tools.Sourced{}, OtherAddresses: []tools.SourcedAddress{}, OtherPhones: []tools.Sourced{}, OtherEmails: []tools.Sourced{}}
	for _, v := range c.OtherNames {
		out.OtherNames = append(out.OtherNames, tools.Sourced{Value: v.Value, Source: v.Source})
	}
	for _, v := range c.OtherAddresses {
		a := v.Value
		out.OtherAddresses = append(out.OtherAddresses, tools.SourcedAddress{Street: a.Street, City: a.City, State: a.State, Zip: a.Zip, County: a.County, Source: v.Source})
	}
	for _, v := range c.OtherPhones {
		out.OtherPhones = append(out.OtherPhones, tools.Sourced{Value: v.Value, Source: v.Source})
	}
	for _, v := range c.OtherEmails {
		out.OtherEmails = append(out.OtherEmails, tools.Sourced{Value: v.Value, Source: v.Source})
	}
	return out
}

// R-FHME-1ZJM R-31E0-585L R-2MR7-JZ99
func TestCandidateSourcesAndMemberPeopleThroughMCP(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(2)
	c, err := f.st.GetCandidate(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	raw := f.call("candidate", tools.CandidateArgs{Candidate: ids[0]})
	got := decodeList[tools.Detail](t, raw)
	if !reflect.DeepEqual(got.Sources, sourceAnswer(c)) || got.Sources.Name != "fmcsa" || got.Sources.Address != "fmcsa" || got.Sources.Phone != "fmcsa" || got.Sources.Email != "fmcsa" {
		t.Fatal(got.Sources, c)
	}
	if len(got.Candidate.People) != 1 || got.Candidate.People[0].Name != "Person" || got.Candidate.People[0].Source != "fmcsa" || len(got.Links) != 1 || got.Links[0].People[0].Source != "" {
		t.Fatal(got)
	}
	list := f.create("People sources")
	members := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: list.ID, Peek: true})).Members
	if len(members) != 2 {
		t.Fatal(members)
	}
	for _, m := range members {
		if len(m.Candidate.People) != 1 || m.Candidate.People[0].Source != "fmcsa" {
			t.Fatal(m)
		}
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(decode(t, raw).Structured, &obj); err != nil {
		t.Fatal(err)
	}
	var candidate map[string]json.RawMessage
	if err = json.Unmarshal(obj["candidate"], &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate["sources"] != nil || candidate["other_names"] != nil || candidate["name_source"] != nil {
		t.Fatal(string(obj["candidate"]))
	}
	merged := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "one business"})
	if merged.IsError() {
		t.Fatal(decode(t, merged))
	}
	raw = f.call("candidate", tools.CandidateArgs{Candidate: ids[1]})
	got = decodeList[tools.Detail](t, raw)
	if got.Sources != nil {
		t.Fatal(got)
	}
	// Decode into a fresh map so an omitted field cannot retain its previous value.
	obj = nil
	if err = json.Unmarshal(decode(t, raw).Structured, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["sources"] != nil {
		t.Fatal(obj)
	}
}
