package protocol

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// R-V6XY-NDT1.
func TestTemplateWireObjectsAndConstants(t *testing.T) {
	_ = struct {
		GID    int64
		Commit string
	}(Seed{})
	_ = struct {
		UID    int64
		Commit string
	}(Home{})
	type count uint16
	type text string
	if got, want := [...]count{MaxSeeds, MaxHomes}, [...]count{50, 200}; got != want {
		t.Errorf("template report limits = %v, want %v", got, want)
	}
	if got, want := [...]text{DefaultTemplateURL, DefaultTemplateRef}, [...]text{"https://github.com/ikigenba/team-template", "main"}; got != want {
		t.Errorf("template defaults = %v, want %v", got, want)
	}
	for _, tc := range []struct {
		entry any
		want  string
	}{
		{Seed{7, "commit"}, `{"gid":7,"commit":"commit"}`},
		{Home{8, "commit"}, `{"uid":8,"commit":"commit"}`},
	} {
		got, err := json.Marshal(tc.entry)
		if err != nil || string(got) != tc.want {
			t.Errorf("marshal %T = %s, %v, want %s", tc.entry, got, err, tc.want)
		}
	}
}

// R-V9DR-EXAF.
func TestDecodeReportWithoutTemplateEntries(t *testing.T) {
	raw := `{"protocol":1,"attempted":3,"applied":3,"restores":[]}`
	r, err := DecodeReport(strings.NewReader(raw))
	if err != nil || r.Applied != 3 || len(r.Seeds) != 0 || len(r.Homes) != 0 {
		t.Fatalf("DecodeReport = %+v, %v", r, err)
	}
}

// R-VALN-SP14.
func TestDecodeStateWithoutTemplateSettings(t *testing.T) {
	raw := `{"protocol":1,"host":"host01","version":5,"daemon":"","data_volume":"","root_gb":20,"data_gb":50,"accounts":[],"users":[],"allowlist":[],"unban":[],"domain":"hosts.example.com","backup":null}`
	state, err := DecodeState(strings.NewReader(raw))
	if err != nil || state.Version != 5 || state.TemplateURL != "" || state.TemplateRef != "" {
		t.Fatalf("DecodeState = %+v, %v", state, err)
	}
}

// R-3K8V-84FW, R-V85V-15JQ.
func TestBoundedTemplateEntries(t *testing.T) {
	a := strings.Repeat("a", 40)
	b := strings.Repeat("b", 40)
	c := strings.Repeat("0f", 32)
	invalid := strings.Repeat("a", 39) + "F"
	original := Report{
		Seeds: []Seed{{3, a}, {1, "XYZ"}, {3, b}, {0, a}, {2, c}, {4, invalid}, {-1, b}},
		Homes: []Home{{3, a}, {1, strings.Repeat("g", 64)}, {3, b}, {0, a}, {2, c}, {4, invalid}, {-1, b}},
	}
	// An invalid entry cannot displace a later valid entry for the same ID.
	original.Seeds = append(original.Seeds, Seed{4, b})
	original.Homes = append(original.Homes, Home{4, b})
	wantSeeds := []Seed{{2, c}, {3, a}, {4, b}}
	wantHomes := []Home{{2, c}, {3, a}, {4, b}}
	seedsBefore, homesBefore := slices.Clone(original.Seeds), slices.Clone(original.Homes)
	got := original.Bounded()
	if !slices.Equal(got.Seeds, wantSeeds) || !slices.Equal(got.Homes, wantHomes) {
		t.Errorf("bounded entries = %v, %v, want %v, %v", got.Seeds, got.Homes, wantSeeds, wantHomes)
	}
	if !slices.Equal(original.Seeds, seedsBefore) || !slices.Equal(original.Homes, homesBefore) {
		t.Error("Bounded mutated its receiver's template lists")
	}
	for _, size := range []int{0, 39, 41, 63, 65} {
		commit := strings.Repeat("a", size)
		got := (Report{Seeds: []Seed{{1, commit}}, Homes: []Home{{1, commit}}}).Bounded()
		if len(got.Seeds) != 0 || len(got.Homes) != 0 {
			t.Errorf("bounded entries for commit length %d = %v, %v, want both empty", size, got.Seeds, got.Homes)
		}
	}
}

// R-V85V-15JQ.
func TestBoundedTemplateEntryCaps(t *testing.T) {
	commit := strings.Repeat("a", 40)
	var original Report
	for id := int64(60); id >= 1; id-- {
		original.Seeds = append(original.Seeds, Seed{id, commit})
	}
	for id := int64(250); id >= 1; id-- {
		original.Homes = append(original.Homes, Home{id, commit})
	}
	wantSeeds := make([]Seed, MaxSeeds)
	for i := range wantSeeds {
		wantSeeds[i] = Seed{int64(i + 1), commit}
	}
	wantHomes := make([]Home, MaxHomes)
	for i := range wantHomes {
		wantHomes[i] = Home{int64(i + 1), commit}
	}
	got := original.Bounded()
	if !slices.Equal(got.Seeds, wantSeeds) {
		t.Errorf("bounded seeds did not keep the lowest %d IDs in order", MaxSeeds)
	}
	if !slices.Equal(got.Homes, wantHomes) {
		t.Errorf("bounded homes did not keep the lowest %d IDs in order", MaxHomes)
	}
}
