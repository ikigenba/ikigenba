package tools

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

func schemaCases(t *testing.T, list []mcp.ToolInfo) {
	t.Helper()
	cases := []struct {
		name             string
		fields, required []string
	}{
		// R-F5HA-RF7O
		{"campaigns", []string{"page", "limit"}, []string{}},
		// R-F6P7-56YD
		{"campaign", []string{"campaign", "page", "limit"}, []string{"campaign"}},
		// R-FACW-AI6G
		{"create_campaign", []string{"name", "description", "channels", "destination"}, []string{"name", "channels", "destination"}},
		// R-FBKS-O9X5
		{"edit_campaign", []string{"campaign", "name", "description", "destination"}, []string{"campaign"}},
		// R-FCSP-21NU
		{"delete_campaign", []string{"campaign"}, []string{"campaign"}},
		// R-2S7G-J8J6
		{"add_prospect_list", []string{"campaign", "prospect_list"}, []string{"campaign", "prospect_list"}},
		// R-FGGE-7CVX
		{"add_member", []string{"campaign", "candidate"}, []string{"campaign", "candidate"}},
		// R-FHOA-L4MM
		{"reject", []string{"campaign", "candidate", "why"}, []string{"campaign", "candidate", "why"}},
		// R-FIW6-YWDB
		{"drop", []string{"piece"}, []string{"piece"}},
		// R-FIW6-YWDB
		{"deliver", []string{"piece"}, []string{"piece"}},
		// R-FK43-CO40
		{"deliver_channel", []string{"campaign", "channel"}, []string{"campaign", "channel"}},
		// R-FLBZ-QFUP
		{"add_material", []string{"campaign", "path", "label"}, []string{"campaign", "path", "label"}},
		// R-FMJW-47LE
		{"remove_material", []string{"campaign", "path"}, []string{"campaign", "path"}},
		// R-F7X3-IYP2
		{"merge_list", []string{"campaign", "channel", "all", "page", "limit"}, []string{"campaign", "channel"}},
		// R-F94Z-WQFR
		{"engagement", []string{"piece", "campaign", "candidate", "page", "limit"}, []string{}},
	}
	for i, c := range cases {
		var s struct {
			Properties map[string]struct{ Type, Description string }
			Required   []string
		}
		if e := json.Unmarshal(list[i].InputSchema, &s); e != nil {
			t.Fatal(e)
		}
		if c.name != list[i].Name || len(s.Properties) != len(c.fields) {
			t.Fatal(c.name)
		}
		if len(s.Required) != len(c.required) || len(c.required) > 0 && !reflect.DeepEqual(s.Required, c.required) {
			t.Fatalf("%s required %v", c.name, s.Required)
		}
		last := -1
		for _, k := range c.fields {
			p, ok := s.Properties[k]
			if !ok {
				t.Fatal(k)
			}
			at := strings.Index(string(list[i].InputSchema), "\""+k+"\":")
			if at <= last {
				t.Fatal("property order")
			}
			last = at
			if k == "page" && p.Description != paging.PageDescription || k == "limit" && p.Description != paging.LimitDescription {
				t.Fatal("paging descriptions")
			}
		}
	}
}
func outputValue[T any](t *testing.T, v T, keys []string) {
	t.Helper()
	f := setup(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "campaigns", Telemetry: f.cfg.Telemetry})
	mcp.AddTool(srv, mcp.Tool[CampaignsArgs, T]{Name: "sample", Description: DescribeCampaigns, Effect: mcp.Read, Handler: func(context.Context, identity.Caller, CampaignsArgs) (T, error) { return v, nil }})
	hs := httptest.NewServer(events.Middleware(identity.Require(srv)))
	defer hs.Close()
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: hs.URL})
	m := f.ok("sample", CampaignsArgs{})
	if len(m) != len(keys) {
		t.Fatalf("keys %v want %v", m, keys)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Fatal(k)
		}
		if string(m[k]) == "null" {
			t.Fatalf("null %s", k)
		}
	}
	list, e := f.client.ListTools(context.Background(), testCaller)
	if e != nil {
		t.Fatal(e)
	}
	var schema struct {
		Properties map[string]struct{ Description string }
	}
	if e = json.Unmarshal(list[0].OutputSchema, &schema); e != nil {
		t.Fatal(e)
	}
	ordered := append([]string{}, keys...)
	if _, ok := schema.Properties["visits"]; ok {
		ordered = append(ordered, "visits")
	}
	if p, ok := schema.Properties["next"]; ok {
		ordered = append(ordered, "next")
		if p.Description != paging.NextDescription {
			t.Fatal("next description")
		}
	}
	var shape map[string]json.RawMessage
	if e = json.Unmarshal(list[0].OutputSchema, &shape); e != nil {
		t.Fatal(e)
	}
	if got := objectKeys(t, shape["properties"]); !reflect.DeepEqual(got, ordered) {
		t.Fatalf("output property order %v want %v", got, ordered)
	}
	r, e := f.client.CallTool(context.Background(), testCaller, "sample", json.RawMessage(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := r.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var answer struct{ StructuredContent json.RawMessage }
	if e = json.Unmarshal(raw, &answer); e != nil {
		t.Fatal(e)
	}
	if got := objectKeys(t, answer.StructuredContent); !reflect.DeepEqual(got, keys) {
		t.Fatalf("result property order %v want %v", got, keys)
	}
}

// R-FNRS-HZC3
func TestOutputCampaign(t *testing.T) {
	outputValue(t, Campaign{}, []string{"id", "name", "description", "channels", "destination", "created", "created_by", "created_email", "deleted"})
}

// R-FOZO-VR2S
func TestOutputCounts(t *testing.T) {
	outputValue(t, Counts{}, []string{"members", "delivered", "opened"})
}

// R-FQ7L-9ITH
func TestOutputCampaignSummary(t *testing.T) {
	outputValue(t, CampaignSummary{}, []string{"id", "name", "channels", "created", "members", "delivered", "opened"})
}

// R-FRFH-NAK6
func TestOutputPiece(t *testing.T) {
	outputValue(t, Piece{}, []string{"id", "campaign", "candidate", "channel", "state", "created", "delivered", "delivered_by", "delivered_email", "how", "dropped", "dropped_by", "dropped_email", "opens", "first", "last"})
}

// R-2TFC-X09V
func TestOutputMember(t *testing.T) {
	outputValue(t, Member{}, []string{"campaign", "candidate", "prospect_list", "added", "added_by", "added_email", "rejected", "rejected_by", "rejected_email", "rejected_at", "why", "opened", "opens", "first", "last", "pieces"})
}

// R-FTVA-EU1K
func TestOutputMaterial(t *testing.T) {
	outputValue(t, Material{}, []string{"path", "label", "artifact", "added", "added_by", "added_email"})
}

// R-FV36-SLS9
func TestOutputCampaignList(t *testing.T) { outputValue(t, CampaignList{}, []string{"campaigns"}) }

// R-FXIZ-K59N
func TestOutputCampaignRead(t *testing.T) {
	outputValue(t, CampaignRead{}, []string{"campaign", "counts", "material", "members"})
}

// R-FYQV-XX0C
func TestOutputCampaignChange(t *testing.T) {
	outputValue(t, CampaignChange{}, []string{"campaign", "changed"})
}

// R-FZYS-BOR1
func TestOutputSkip(t *testing.T) { outputValue(t, Skip{}, []string{"channel", "reason"}) }

// R-G16O-PGHQ
func TestOutputAddedMember(t *testing.T) {
	outputValue(t, AddedMember{}, []string{"member", "added", "skipped"})
}

// R-2UN9-AS0K
func TestOutputProspectListAdd(t *testing.T) {
	outputValue(t, ProspectListAdd{}, []string{"added", "present", "excluded"})
}

// R-G3MH-GZZ4
func TestOutputMemberChange(t *testing.T) {
	outputValue(t, MemberChange{}, []string{"member", "changed"})
}

// R-G4UD-URPT
func TestOutputPieceRecord(t *testing.T) {
	outputValue(t, PieceRecord{}, []string{"id", "campaign", "candidate", "channel", "state", "created", "delivered", "delivered_by", "delivered_email", "how", "dropped", "dropped_by", "dropped_email"})
}

// R-G62A-8JGI
func TestOutputPieceChange(t *testing.T) { outputValue(t, PieceChange{}, []string{"piece", "changed"}) }

// R-G7A6-MB77
func TestOutputDelivered(t *testing.T) { outputValue(t, Delivered{}, []string{"pieces"}) }

// R-G8I3-02XW
func TestOutputMaterialChange(t *testing.T) {
	outputValue(t, MaterialChange{}, []string{"material", "changed"})
}

// R-G9PZ-DUOL
func TestOutputRemoved(t *testing.T) { outputValue(t, Removed{}, []string{"path", "removed"}) }

// R-GAXV-RMFA
func TestOutputPerson(t *testing.T) { outputValue(t, Person{}, []string{"name", "role"}) }

// R-GC5S-5E5Z
func TestOutputBusiness(t *testing.T) {
	outputValue(t, Business{}, []string{"name", "names", "street", "city", "state", "zip", "county", "phone", "email", "people"})
}

// R-GDDO-J5WO
func TestOutputMergeRow(t *testing.T) {
	outputValue(t, MergeRow{}, []string{"piece", "candidate", "channel", "state", "rejected", "link", "business"})
}

// R-GELK-WXND
func TestOutputMergeList(t *testing.T) { outputValue(t, MergeList{}, []string{"rows"}) }

// R-GH1D-OH4R
func TestOutputVisit(t *testing.T) {
	outputValue(t, Visit{}, []string{"at", "user_agent", "referrer", "address"})
}

// R-GI9A-28VG
func TestOutputEngagement(t *testing.T) {
	outputValue(t, Engagement{}, []string{"campaign", "candidate", "piece", "opened", "opens", "first", "last"})
}

func objectKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(string(raw)))
	if _, e := d.Token(); e != nil {
		t.Fatal(e)
	}
	keys := []string{}
	for d.More() {
		v, e := d.Token()
		if e != nil {
			t.Fatal(e)
		}
		key, ok := v.(string)
		if !ok {
			t.Fatal("object key")
		}
		keys = append(keys, key)
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			t.Fatal(e)
		}
	}
	return keys
}
