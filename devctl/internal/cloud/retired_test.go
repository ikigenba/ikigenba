package cloud

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

var _ func(context.Context, EC2, string, string) ([]Instance, error) = SpaceInstances
var _ func(context.Context, EC2, string, string) ([]Address, error) = SpaceAddresses

type inventoryEC2 struct {
	EC2
	instances []Instance
	addresses []Address
	err       error
	domains   []string
}

func (f *inventoryEC2) ListSpaceInstances(_ context.Context, domain string) ([]Instance, error) {
	f.domains = append(f.domains, domain)
	return f.instances, f.err
}
func (f *inventoryEC2) ListSpaceAddresses(_ context.Context, domain string) ([]Address, error) {
	f.domains = append(f.domains, domain)
	return f.addresses, f.err
}

func TestRetirementContract(t *testing.T) {
	// R-FG89-YQUU R-FHG6-CILJ R-FF0D-KZ45
	const tag string = RetiredTag
	if tag != "Retired" {
		t.Fatal(tag)
	}
	format := RetiredValue
	got := format(time.Date(2026, 11, 8, 12, 30, 0, 750000000, time.FixedZone("west", -6*3600)))
	if got != "2026-11-08T18:30:00Z" {
		t.Fatal(got)
	}
	_ = DuplicateSpaceError(struct {
		Domain string
		IDs    []string
	}{})
	e := &DuplicateSpaceError{Domain: "sbx1.ikigenba.dev", IDs: []string{"i-0c9e94542d98846a8", "i-0f1e2d3c4b5a69788"}}
	if e.Error() != "'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)" || e.ExitCode() != 2 {
		t.Fatal(e)
	}
}

func TestRetiredInventoryAndActiveRegistry(t *testing.T) {
	// R-FIO2-QAC8 R-FJVZ-422X R-FCKK-TFMR R-FDSH-77DG
	const domain = "sbx1.ikigenba.dev"
	retired := Instance{ID: "i-0c9e94542d98846a8", Space: domain, State: StateStopped, Retired: "2026-11-08T18:30:00Z"}
	active := Instance{ID: "i-0e5d4c3b2a1f09876", Space: domain, State: StateRunning, Address: "3.141.27.86"}
	dead := Instance{ID: "i-0aaaaaaaaaaaaaaa1", Space: domain, State: StateTerminated}
	f := &inventoryEC2{instances: []Instance{retired, dead, {ID: "i-other", Space: "sbx2.ikigenba.dev", State: StateRunning}, active}}
	got, err := SpaceInstances(t.Context(), f, "ikigenba.dev", domain)
	if err != nil || !reflect.DeepEqual(got, []Instance{active, retired}) {
		t.Fatalf("inventory %v %v", got, err)
	}
	spaces, err := Spaces(t.Context(), f, "ikigenba.dev")
	want := []Space{{Domain: domain, ID: active.ID, State: active.State, Address: active.Address}, {Domain: "sbx2.ikigenba.dev", ID: "i-other", State: StateRunning}}
	if err != nil || !reflect.DeepEqual(spaces, want) {
		t.Fatalf("spaces %v %v", spaces, err)
	}
	found, err := LookupSpace(t.Context(), f, "ikigenba.dev", domain)
	if err != nil || found != want[0] {
		t.Fatalf("lookup %v %v", found, err)
	}
	f.instances = []Instance{retired, dead}
	spaces, err = Spaces(t.Context(), f, "ikigenba.dev")
	if err != nil || len(spaces) != 0 {
		t.Fatalf("spaces %v %v", spaces, err)
	}
	_, err = LookupSpace(t.Context(), f, "ikigenba.dev", domain)
	var missing *NoSpaceError
	if !errors.As(err, &missing) || missing.Domain != domain {
		t.Fatal(err)
	}
	a := Address{AllocationID: "eipalloc-01", Space: domain, IP: "18.118.7.42", Retired: retired.Retired}
	b := Address{AllocationID: "eipalloc-02", Space: domain, IP: active.Address}
	c := Address{AllocationID: "eipalloc-03", Space: domain, IP: "1.2.3.4"}
	d := Address{AllocationID: "eipalloc-00", Space: domain, Retired: "older"}
	f.addresses = []Address{a, c, {Space: "other"}, d, b}
	addresses, err := SpaceAddresses(t.Context(), f, "ikigenba.dev", domain)
	if err != nil || !reflect.DeepEqual(addresses, []Address{b, c, d, a}) {
		t.Fatalf("addresses %v %v", addresses, err)
	}
	// Sort each partition independently, even with several active instances.
	a1 := active
	a1.ID = "i-000"
	r1 := retired
	r1.ID = "i-0"
	f.instances = []Instance{retired, active, r1, a1}
	got, err = SpaceInstances(t.Context(), f, "ikigenba.dev", domain)
	if err != nil || !reflect.DeepEqual(got, []Instance{a1, active, r1, retired}) {
		t.Fatalf("instances %v %v", got, err)
	}
	for _, root := range f.domains {
		if root != "ikigenba.dev" {
			t.Fatal(root)
		}
	}
	for _, failure := range []bool{false, true} {
		f = &inventoryEC2{}
		if failure {
			f.err = errors.New("inventory sentinel")
		}
		got, err = SpaceInstances(t.Context(), f, "ikigenba.dev", domain)
		if len(got) != 0 || reflect.ValueOf(err) != reflect.ValueOf(f.err) {
			t.Fatalf("instances %v %v", got, err)
		}
		addresses, err = SpaceAddresses(t.Context(), f, "ikigenba.dev", domain)
		if len(addresses) != 0 || reflect.ValueOf(err) != reflect.ValueOf(f.err) {
			t.Fatalf("addresses %v %v", addresses, err)
		}
		spaces, err = Spaces(t.Context(), f, "ikigenba.dev")
		if len(spaces) != 0 || reflect.ValueOf(err) != reflect.ValueOf(f.err) {
			t.Fatalf("spaces %v %v", spaces, err)
		}
	}
}

func TestDuplicateRegistryIsScopedAndDeterministic(t *testing.T) {
	// R-FCKK-TFMR R-FDSH-77DG
	f := &inventoryEC2{instances: []Instance{
		{ID: "i-z", Space: "zulu", State: StateRunning}, {ID: "i-y", Space: "zulu", State: StateRunning},
		{ID: "i-0f1e2d3c4b5a69788", Space: "sbx1.ikigenba.dev", State: StateRunning},
		{ID: "i-0c9e94542d98846a8", Space: "sbx1.ikigenba.dev", State: StateStopped},
		{ID: "i-alone", Space: "sbx2.ikigenba.dev", State: StateStopped}}}
	_, err := Spaces(t.Context(), f, "ikigenba.dev")
	var duplicate *DuplicateSpaceError
	if !errors.As(err, &duplicate) || duplicate.Domain != "sbx1.ikigenba.dev" || !reflect.DeepEqual(duplicate.IDs, []string{"i-0c9e94542d98846a8", "i-0f1e2d3c4b5a69788"}) {
		t.Fatal(err)
	}
	_, err = LookupSpace(t.Context(), f, "ikigenba.dev", "sbx1.ikigenba.dev")
	if !errors.As(err, &duplicate) || duplicate.Domain != "sbx1.ikigenba.dev" {
		t.Fatal(err)
	}
	got, err := LookupSpace(t.Context(), f, "ikigenba.dev", "sbx2.ikigenba.dev")
	if err != nil || got.ID != "i-alone" {
		t.Fatalf("%v %v", got, err)
	}
}
