package space

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestAddressOnlySpaceLifecycle(t *testing.T) {
	// R-9IAV-GV18 R-CI07-EYVE R-CJ83-SQM3 R-CLNW-KA3H
	f := newOperationFake(t)
	f.instances = []cloud.Instance{{ID: "old", Space: testDomain, State: cloud.StateTerminated}}
	f.addresses = []cloud.Address{{AllocationID: "alloc", IP: "18.118.7.42", Space: testDomain}}
	out, err := runOperation(t, f, "list")
	if err != nil || out != testDomain+" - 18.118.7.42 -\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	f.ops = nil
	out, err = runOperation(t, f, "destroy", "sbx1")
	if err != nil || !strings.HasPrefix(out, "retire: ok (already gone)\ninstance: ok (already gone)\naddress: ok (elastic ip 18.118.7.42 released)\n") || containsOp(f.ops, "terminate ") || containsOp(f.ops, "disassociate ") {
		t.Fatalf("out=%q err=%v ops=%v", out, err, f.ops)
	}
}
