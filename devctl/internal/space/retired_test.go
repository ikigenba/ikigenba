package space

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestRetiredAddressesHoldNeitherSpaceNorApex(t *testing.T) {
	// R-FNJO-9DB0 R-FORK-N51P
	active := cloud.Address{Space: testDomain, IP: "3.141.27.86", AllocationID: "active"}
	retired := cloud.Address{Space: testDomain, IP: "18.118.7.42", AllocationID: "old", Retired: "2026-11-08T18:30:00Z"}
	var addresses []cloud.Address
	e := &helperEC2{addresses: func(context.Context, string) ([]cloud.Address, error) { return addresses, nil }}
	for _, items := range [][]cloud.Address{{active, retired}, {retired}} {
		addresses = items
		got, found, err := ElasticIP(t.Context(), e, testRoot, testDomain)
		if err != nil || found != (len(items) == 2) || found && got != active {
			t.Fatalf("%v %v %v", got, found, err)
		}
	}
	for _, tc := range []struct {
		ip, holder string
		items      []cloud.Address
	}{
		{retired.IP, "", []cloud.Address{retired}},
		{active.IP, testDomain, []cloud.Address{retired, active}},
		{retired.IP, testDomain, []cloud.Address{{Space: testDomain, IP: retired.IP}}},
		{"203.0.113.9", "", []cloud.Address{active, retired}},
	} {
		addresses = tc.items
		record := cloud.Record{Name: testRoot, Type: "A", TTL: 123, Values: []string{tc.ip}}
		r := &helperRoute53{findRecord: func(context.Context, string, string, string) (cloud.Record, bool, error) { return record, true, nil }}
		got, err := FindApex(t.Context(), r, e, "zone", testRoot)
		if err != nil || !got.Found || got.Holder != tc.holder || !reflect.DeepEqual(got.Record, record) {
			t.Fatalf("%#v %v", got, err)
		}
	}
}
func TestDestroyIncludesRetiredResources(t *testing.T) {
	// R-FPZH-0WSE R-FR7D-EOJ3
	for _, active := range []bool{true, false} {
		f := newOperationFake(t)
		f.instances = []cloud.Instance{{ID: "i-0c9e94542d98846a8", Space: testDomain, State: cloud.StateStopped, Retired: "2026-11-08T18:30:00Z"}}
		f.addresses = []cloud.Address{{AllocationID: "alloc-old", IP: "18.118.7.42", Space: testDomain, Retired: "2026-11-08T18:30:00Z"}}
		if active {
			f.instances = append(f.instances, cloud.Instance{ID: "i-0e5d4c3b2a1f09876", Space: testDomain, State: cloud.StateRunning, Address: "3.141.27.86"})
			f.addresses = append(f.addresses, cloud.Address{AllocationID: "alloc-new", Space: testDomain, IP: "3.141.27.86"})
		}
		f.described = cloud.Instance{State: cloud.StateTerminated}
		stdout, err := runOperation(t, f, "destroy", "sbx1")
		if err != nil {
			t.Fatal(err)
		}
		instance := "instance: ok (i-0c9e94542d98846a8 terminated)\n"
		address := "address: ok (elastic ip 18.118.7.42 released)\n"
		retire := "retire: ok (already gone)\n"
		if active {
			instance = "instance: ok (i-0e5d4c3b2a1f09876 terminated, i-0c9e94542d98846a8 terminated)\n"
			address = "address: ok (elastic ip 3.141.27.86 released, elastic ip 18.118.7.42 released)\n"
			retire = "retire: ok (opsctl retire)\n"
		}
		if !strings.HasPrefix(stdout, retire+instance+address) {
			t.Fatal(stdout)
		}
		if countExact(f.ops, "terminate i-0c9e94542d98846a8") != 1 || countExact(f.ops, "release alloc-old") != 1 || containsOp(f.ops, "start ") {
			t.Fatal(f.ops)
		}
		if active && (countExact(f.ops, "terminate i-0e5d4c3b2a1f09876") != 1 || countExact(f.ops, "release alloc-new") != 1) {
			t.Fatal(f.ops)
		}
		ssh := 0
		for _, cmd := range f.execCalls {
			if cmd.Path == "ssh" {
				ssh++
				if !strings.Contains(strings.Join(cmd.Args, " "), "ec2-user@3.141.27.86") {
					t.Fatal(cmd)
				}
			}
		}
		if active && ssh != 1 || !active && ssh != 0 {
			t.Fatal(ssh)
		}
	}
}
