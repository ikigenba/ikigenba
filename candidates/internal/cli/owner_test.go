package cli_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-UHFA-QKR7 R-UIN7-4CHW R-UG7E-CT0I
func TestRunOwnerWritesEmitOneExactTrailEventPerSavedRecord(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	h.start()
	caller := identity.Caller{UserID: "owner-writer", Email: "owner-writer@example.test"}
	requests := map[string]string{}
	var candidates []string
	write := func(name string, args map[string]any, request string) (store.OwnerRecord, store.Candidate) {
		t.Helper()
		caller.RequestID = request
		result, value := h.call(caller, name, args)
		if result.IsError() {
			t.Fatal(name, result)
		}
		var record store.OwnerRecord
		// The tool's record is its JSON view, whose member names are lowercase.
		var view struct {
			ID, Candidate string
			Founded       bool
		}
		if err := json.Unmarshal(value["record"], &view); err != nil {
			t.Fatal(err)
		}
		record.ID, record.Candidate, record.Founded = view.ID, view.Candidate, view.Founded
		if record.ID == "" || record.Candidate == "" {
			t.Fatal(value)
		}
		requests[record.ID] = request
		if !slices.Contains(candidates, record.Candidate) {
			candidates = append(candidates, record.Candidate)
		}
		return record, store.Candidate{ID: record.Candidate}
	}
	founded, candidate := write("add_business", map[string]any{"name": "Owner business", "note": "Founded by the owner", "email": "ops@owner.test", "website": "https://owner.test/"}, "owner-founded")
	if !founded.Founded {
		t.Fatal(founded)
	}
	changed, _ := write("set_facts", map[string]any{"candidate": candidate.ID, "name": "Owner renamed business", "phone": "2185550100", "clear": []string{"email", "website"}, "note": "Owner corrected the contact"}, "owner-corrected")
	if changed.Founded {
		t.Fatal(changed)
	}
	attached, _ := write("add_business", map[string]any{"name": "Owner additional record", "phone": "2185550100", "note": "Matched by phone"}, "owner-matched")
	if attached.Founded || attached.Candidate != candidate.ID {
		t.Fatal(attached)
	}
	_, merged := write("add_business", map[string]any{"name": "Owner merged business", "note": "Merge fixture"}, "owner-merged")
	if result, _ := h.call(caller, "decide", map[string]any{"kind": store.KindMerge, "candidate": candidate.ID, "other": merged.ID, "why": "Owner joined records"}); result.IsError() {
		t.Fatal(result)
	}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"add_business", map[string]any{"name": "Invalid note", "note": " "}},
		{"add_business", map[string]any{"name": "Invalid phone", "note": "Phone fixture", "phone": "218-555-0100"}},
		{"set_facts", map[string]any{"candidate": "cand_0000000000000000", "name": "Missing candidate", "note": "Missing fixture"}},
		{"set_facts", map[string]any{"candidate": merged.ID, "name": "Merged candidate", "note": "Merged fixture"}},
	} {
		result, _ := h.call(caller, tc.tool, tc.args)
		if !result.IsError() {
			t.Fatal(tc.tool, result)
		}
	}
	for _, id := range candidates {
		if result, _ := h.call(caller, "candidate", map[string]any{"candidate": id}); result.IsError() {
			t.Fatal(result)
		}
		h.request("GET", "/candidates/"+id, "owner-page", nil)
	}
	h.request("GET", "/", "owner-landing", nil)
	h.request("GET", "/declarations", "owner-declarations", nil)
	h.campaignDelivery("campaign.member.added", "campaign_owner_fixture", candidate.ID, "pl_owner_fixture", "", h.p.Now(), "owner-campaign-delivery")
	h.deliver("evt_5656565656565656", "owner-skipped-delivery", "failed", 0)
	if h.stop() != 0 {
		t.Fatal(h.err.String())
	}
	_, persisted := runStore(t, h.p.Dir, h.p.Now)
	saved := map[string]store.OwnerRecord{}
	for _, id := range candidates {
		records, err := persisted.OwnerRecords(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			saved[record.ID] = record
		}
	}
	if len(saved) != len(requests) {
		t.Fatal("unexpected saved owner records", saved, requests)
	}
	seen := map[string]int{}
	allowed := []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "event.lost", "sibling.called", "pass.started", "pass.finished", "owner_record.written"}
	for _, event := range h.telemetry.Events() {
		if !slices.Contains(allowed, event.Name) {
			t.Fatal("unexpected trail name", event.Name)
		}
		if event.Name != "owner_record.written" {
			continue
		}
		id, _ := event.Attrs["record"].(string)
		record, ok := saved[id]
		if !ok {
			t.Fatal("event without a saved record", event)
		}
		encoded, err := json.Marshal(record.Set)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		set := []string{}
		for _, fact := range sources.SettableFacts() {
			if _, ok := fields[fact]; ok {
				set = append(set, fact)
			}
		}
		want := telemetry.Attrs{"record": record.ID, "candidate": record.Candidate, "founded": record.Founded, "set": strings.Join(set, ","), "cleared": strings.Join(record.Cleared, ",")}
		if event.RequestID != requests[id] || event.User != record.UserID || !reflect.DeepEqual(event.Attrs, want) {
			t.Fatal(event, want)
		}
		if id == changed.ID && (event.Attrs["set"] != "name,phone" || event.Attrs["cleared"] != "website,email") {
			t.Fatal(event)
		}
		seen[id]++
	}
	for id := range saved {
		if seen[id] != 1 {
			t.Fatal("owner event count", id, seen[id])
		}
	}
	for _, event := range h.bus.Events() {
		if event.Name == "owner_record.written" {
			t.Fatal("owner write emitted a bus event", event)
		}
	}
}
