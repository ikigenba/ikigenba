package cloud

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type inventoryEC2 struct {
	EC2
	instances               []Instance
	addresses               []Address
	instanceErr, addressErr error
	calls                   []string
}

func (f *inventoryEC2) ListSpaceInstances(_ context.Context, domain string) ([]Instance, error) {
	f.calls = append(f.calls, "instances "+domain)
	return f.instances, f.instanceErr
}
func (f *inventoryEC2) ListSpaceAddresses(_ context.Context, domain string) ([]Address, error) {
	f.calls = append(f.calls, "addresses "+domain)
	return f.addresses, f.addressErr
}

func TestRegistryValueErrors(t *testing.T) {
	// R-FF0D-KZ45 R-NDXK-52OA R-NF5G-IUEZ
	_ = DuplicateSpaceError(struct {
		Domain string
		IDs    []string
	}{})
	_ = DuplicateAddressError(struct {
		Domain string
		IPs    []string
	}{})
	_ = NoInstanceError(struct{ Domain string }{})
	d := &DuplicateSpaceError{Domain: "sbx1.ikigenba.dev", IDs: []string{"i-0c9e94542d98846a8", "i-0f1e2d3c4b5a69788"}}
	if d.Error() != "'sbx1.ikigenba.dev' has more than one active instance (i-0c9e94542d98846a8, i-0f1e2d3c4b5a69788)" || d.ExitCode() != 2 {
		t.Fatal(d)
	}
	a := &DuplicateAddressError{Domain: "sbx1.ikigenba.dev", IPs: []string{"18.118.7.42", "18.190.33.61"}}
	if a.Error() != "'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)" || a.ExitCode() != 2 {
		t.Fatal(a)
	}
	if (&NoInstanceError{Domain: "sbx1.ikigenba.dev"}).Error() != "'sbx1.ikigenba.dev' has no instance" {
		t.Fatal("no instance message")
	}
}

func TestLookupRegistryResources(t *testing.T) {
	// R-NIT5-O5N2 R-Y156-VX9O R-NHL9-ADWD R-NL8Y-FP4G R-NMGU-TGV5
	const domain = "sbx1.ikigenba.dev"
	host := Instance{ID: "i-0e5d4c3b2a1f09876", Space: domain, State: StateRunning, Address: "3.22.140.61"}
	ip := Address{AllocationID: "eipalloc-01", AssociationID: "eipassoc-01", InstanceID: host.ID, IP: "18.118.7.42", Space: domain}
	for _, tc := range []struct {
		name      string
		instances []Instance
		addresses []Address
		want      Space
		missing   bool
	}{
		{"both", []Instance{host}, []Address{ip}, Space{Domain: domain, ID: host.ID, State: StateRunning, Address: ip.IP, ElasticIP: ip}, false},
		{"address only", nil, []Address{ip}, Space{Domain: domain, Address: ip.IP, ElasticIP: ip}, false},
		{"instance only", []Instance{host}, nil, Space{Domain: domain, ID: host.ID, State: StateRunning, Address: host.Address}, false},
		{"terminated", []Instance{{ID: "i-dead", Space: domain, State: StateTerminated}}, nil, Space{}, true},
		{"terminated with address", []Instance{{ID: "i-dead", Space: domain, State: StateTerminated}}, []Address{ip}, Space{Domain: domain, Address: ip.IP, ElasticIP: ip}, false},
		{"stopped", []Instance{{ID: "i-stopped", Space: domain, State: StateStopped}}, nil, Space{Domain: domain, ID: "i-stopped", State: StateStopped}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &inventoryEC2{instances: tc.instances, addresses: tc.addresses}
			got, err := LookupSpace(t.Context(), f, "ikigenba.dev", domain)
			var absent *NoSpaceError
			if got != tc.want || tc.missing != errors.As(err, &absent) || (!tc.missing && err != nil) {
				t.Fatalf("lookup %v %v want %v", got, err, tc.want)
			}
			if !reflect.DeepEqual(f.calls, []string{"instances ikigenba.dev", "addresses ikigenba.dev"}) {
				t.Fatal(f.calls)
			}
			f.calls = nil
			got, err = LookupInstance(t.Context(), f, "ikigenba.dev", domain)
			if tc.want.ID != "" {
				if got != tc.want || err != nil {
					t.Fatalf("instance %v %v", got, err)
				}
			} else if !tc.missing {
				var noHost *NoInstanceError
				if got != (Space{}) || !errors.As(err, &noHost) || noHost.Domain != domain {
					t.Fatalf("no host %v %v", got, err)
				}
			}
			if !reflect.DeepEqual(f.calls, []string{"instances ikigenba.dev", "addresses ikigenba.dev"}) {
				t.Fatal(f.calls)
			}
		})
	}
	// An unassociated address overrides the instance's transient public address.
	ip.AssociationID, ip.InstanceID, ip.IP = "", "", "18.220.10.5"
	f := &inventoryEC2{instances: []Instance{host}, addresses: []Address{ip}}
	got, err := LookupSpace(t.Context(), f, "ikigenba.dev", domain)
	if err != nil || got.Address != ip.IP || got.ElasticIP != ip {
		t.Fatalf("unassociated %v %v", got, err)
	}
}

func TestRegistrySortAndAmbiguity(t *testing.T) {
	// R-44UD-VGX3 R-NK12-1XDR R-NIT5-O5N2 R-NNOR-78LU
	const root = "ikigenba.dev"
	f := &inventoryEC2{instances: []Instance{
		{ID: "i-new", Space: "new.ikigenba.dev", State: StateRunning, Address: "3.22.140.61"},
		{ID: "i-dead", Space: "gone.ikigenba.dev", State: StateTerminated},
		{ID: "i-one", Space: "sbx1.ikigenba.dev", State: StateRunning, Address: "18.118.7.42"}},
		addresses: []Address{{IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}, {IP: "3.141.27.86", Space: "sbx2.ikigenba.dev"}}}
	got, err := Spaces(t.Context(), f, root)
	if err != nil || len(got) != 3 {
		t.Fatalf("spaces %v %v", got, err)
	}
	for i, name := range []string{"new.ikigenba.dev", "sbx1.ikigenba.dev", "sbx2.ikigenba.dev"} {
		want, e := LookupSpace(t.Context(), f, root, name)
		if e != nil || got[i] != want {
			t.Fatalf("space %v want %v %v", got[i], want, e)
		}
	}
	if got[2].ID != "" || got[2].State != "" || got[2].Address != "3.141.27.86" {
		t.Fatal(got[2])
	}
	for _, twoHosts := range []bool{true, false} {
		f := &inventoryEC2{instances: []Instance{{ID: "i-z", Space: "sbx2.ikigenba.dev", State: StateRunning}, {ID: "i-a", Space: "sbx2.ikigenba.dev", State: StateStopped}}, addresses: []Address{{IP: "18.190.33.61", Space: "sbx1.ikigenba.dev"}, {IP: "18.118.7.42", Space: "sbx1.ikigenba.dev"}}}
		if twoHosts {
			f.instances = append(f.instances, Instance{ID: "i-z", Space: "sbx1.ikigenba.dev", State: StateRunning}, Instance{ID: "i-a", Space: "sbx1.ikigenba.dev", State: StateStopped})
		}
		spaces, e := Spaces(t.Context(), f, root)
		if spaces != nil {
			t.Fatal(spaces)
		}
		var dup *DuplicateSpaceError
		var ips *DuplicateAddressError
		if twoHosts {
			if !errors.As(e, &dup) || dup.Domain != "sbx1.ikigenba.dev" || !reflect.DeepEqual(dup.IDs, []string{"i-a", "i-z"}) {
				t.Fatal(e)
			}
		} else if !errors.As(e, &ips) || ips.Domain != "sbx1.ikigenba.dev" || !reflect.DeepEqual(ips.IPs, []string{"18.118.7.42", "18.190.33.61"}) {
			t.Fatal(e)
		}
		_, e = LookupInstance(t.Context(), f, root, "sbx1.ikigenba.dev")
		_, want := LookupSpace(t.Context(), f, root, "sbx1.ikigenba.dev")
		if reflect.TypeOf(e) != reflect.TypeOf(want) || e.Error() != want.Error() {
			t.Fatalf("instance %v space %v", e, want)
		}
		// Duplicates elsewhere cannot prevent looking up a distinct space.
		f.instances = append(f.instances, Instance{ID: "i-good", Space: "other.ikigenba.dev", State: StateRunning})
		other, e := LookupSpace(t.Context(), f, root, "other.ikigenba.dev")
		if e != nil || other.ID != "i-good" {
			t.Fatalf("%v %v", other, e)
		}
	}
	_, err = LookupInstance(t.Context(), &inventoryEC2{}, root, "gone.ikigenba.dev")
	var absent *NoSpaceError
	if !errors.As(err, &absent) {
		t.Fatal(err)
	}
}

func TestRegistryListingFailures(t *testing.T) {
	// R-Y2D3-9P0D R-NNOR-78LU
	sentinel := errors.New("list sentinel")
	for _, addressFails := range []bool{false, true} {
		f := &inventoryEC2{instances: []Instance{{ID: "i-one", Space: "sbx1.ikigenba.dev", State: StateRunning}}}
		if addressFails {
			f.addressErr = sentinel
		} else {
			f.instanceErr = sentinel
		}
		spaces, err := Spaces(t.Context(), f, "ikigenba.dev")
		if spaces != nil || !errors.Is(err, sentinel) {
			t.Fatalf("%v %v", spaces, err)
		}
		for _, lookup := range []func(context.Context, EC2, string, string) (Space, error){LookupSpace, LookupInstance} {
			got, err := lookup(t.Context(), f, "ikigenba.dev", "sbx1.ikigenba.dev")
			if got != (Space{}) || !errors.Is(err, sentinel) {
				t.Fatalf("%v %v", got, err)
			}
		}
	}
}
