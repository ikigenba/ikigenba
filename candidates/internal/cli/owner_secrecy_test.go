package cli_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-UJV3-I48L R-UL2Z-VVZA
func exerciseRunOwnerSecrecy(t *testing.T, h *runHarness, markers []string, phone, matched string, forbidden *[]string, call func(string, any, bool) map[string]json.RawMessage) {
	t.Helper()
	// Nine digits beginning with eight, distinct from each other and from all
	// phone markers. The remaining fixtures carry no nine-digit numeral.
	eins := []string{"811111111", "822222222", "833333333", "844444444", "855555555", "866666666", "877777777", "888888888", "899999999"}
	newPhone := "8098765432"
	for _, ein := range eins {
		if len(ein) != 9 || ein[0] != '8' || strings.Contains(phone, ein) || strings.Contains(newPhone, ein) {
			t.Fatal("invalid EIN marker fixture")
		}
		for _, other := range eins {
			if ein != other && strings.Contains(other, ein) {
				t.Fatal("overlapping EIN markers")
			}
		}
		for _, c := range ein {
			if c < '0' || c > '9' {
				t.Fatal("non-numeric EIN marker")
			}
		}
	}
	*forbidden = append(*forbidden, eins...)
	*forbidden = append(*forbidden, newPhone)
	marked := strings.Join(append(append([]string{}, markers...), phone, newPhone), " ")
	number := func(i int) int64 {
		n, err := strconv.ParseInt(eins[i], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	values := func(phoneValue string) map[string]any {
		return map[string]any{
			"name": markers[0], "note": marked,
			"address": map[string]any{"street": markers[1], "city": markers[2], "state": "MN", "zip": "55072"},
			"phone":   phoneValue, "email": markers[3] + "@" + markers[4] + ".example", "website": "https://" + markers[5] + ".example/",
			"people": []map[string]any{{"name": markers[6], "role": markers[7]}},
			"ein":    eins[0], "headcount": map[string]any{"count": number(1), "year": 2024}, "revenue": map[string]any{"low": number(2), "high": number(3)}, "fleet": number(4),
		}
	}
	readIDs := []string{matched}
	read := func() {
		t.Helper()
		for _, id := range readIDs {
			call("candidate", map[string]any{"candidate": id}, false)
			if status, _ := h.request("GET", "/candidates/"+id, "owner-secrecy-page", nil); status != 200 {
				t.Fatal("owner candidate page", status)
			}
		}
	}
	fresh := values(newPhone)
	fresh["name"] = markers[8]
	result := call("add_business", fresh, false)
	var record struct {
		Candidate string
		Founded   bool
	}
	if err := json.Unmarshal(result["record"], &record); err != nil || !record.Founded || record.Candidate == matched {
		t.Fatal(record, err)
	}
	readIDs = append(readIDs, record.Candidate)
	read()
	matchedValues := values(phone)
	matchedValues["ein"] = eins[7]
	result = call("add_business", matchedValues, false)
	if err := json.Unmarshal(result["record"], &record); err != nil || record.Founded || record.Candidate != matched {
		t.Fatal(record, err)
	}
	read()
	set := values(phone)
	set["candidate"] = matched
	call("set_facts", set, false)
	read()
	call("set_facts", map[string]any{"candidate": matched, "name": markers[9], "address": map[string]any{"street": markers[8], "city": markers[7], "state": "MN", "zip": "55072"}, "phone": newPhone, "people": []map[string]any{{"name": markers[5], "role": markers[4]}}, "headcount": map[string]any{"count": number(5), "year": 2024}, "fleet": number(6), "note": marked}, false)
	read()
	call("set_facts", map[string]any{"candidate": matched, "clear": []string{"email"}, "note": marked}, false)
	read()
	for _, change := range []map[string]any{
		{"note": " "}, {"category": markers[0]}, {"phone": markers[1]}, {"website": markers[2]}, {"email": markers[3]}, {"ein": eins[7] + "0"},
		{"people": []map[string]any{{"name": " ", "role": markers[4]}}}, {"address": map[string]any{"street": markers[5], "state": markers[6]}},
		{"revenue": map[string]any{"low": number(8), "high": number(7)}}, {"headcount": map[string]any{"count": number(1), "year": 0}},
	} {
		args := values(phone)
		for k, v := range change {
			args[k] = v
		}
		call("add_business", args, true)
		read()
	}
	call("set_facts", map[string]any{"candidate": "cand_0000000000000000", "name": markers[0], "note": marked}, true)
	read()
	call("set_facts", map[string]any{"candidate": matched, "email": markers[3] + "@" + markers[4] + ".example", "clear": []string{"email"}, "note": marked}, true)
	read()
	call("set_facts", map[string]any{"candidate": matched, "clear": []string{markers[5]}, "note": marked}, true)
	read()
	// Keep this merge's loser separate from the candidates later tests use.
	merged := call("add_business", map[string]any{"name": markers[6], "note": marked}, false)
	if err := json.Unmarshal(merged["record"], &record); err != nil || !record.Founded {
		t.Fatal(record, err)
	}
	readIDs = append(readIDs, record.Candidate)
	read()
	call("decide", map[string]any{"kind": store.KindMerge, "candidate": matched, "other": record.Candidate, "why": marked}, false)
	call("set_facts", map[string]any{"candidate": record.Candidate, "name": markers[0], "note": marked}, true)
	read()
}
