package space

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
)

const (
	testRoot   = "ikigenba.dev"
	testRegion = "eu-west-1"
	testDomain = "sbx1.ikigenba.dev"
)

func TestRunListCurrentRootAndApex(t *testing.T) {
	// R-RJ9Z-1OB0 R-V57V-SDRH R-V6FS-65I6 R-XO8Q-YVRD R-V8VK-XOZK R-8YVJ-0IZI
	if got, want := reflect.TypeOf(Run), reflect.TypeFor[func(context.Context, []string, io.Writer, seam.Deps) error](); got != want {
		t.Fatalf("Run has type %v, want %v", got, want)
	}
	f := newOperationFake(t)
	f.instances = []cloud.Instance{
		{ID: "i-new", Space: "new." + testRoot, State: cloud.StateRunning, Address: "18.220.10.5"},
		{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.118.7.42"},
		{ID: "i-two", Space: "sbx2." + testRoot, State: cloud.StateStopped},
	}
	f.records = []cloud.Record{{Name: testRoot, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}}
	f.addresses = []cloud.Address{{IP: "18.118.7.42", Space: testDomain}}
	stdout, err := runOperation(t, f, "list")
	if err != nil {
		t.Fatal(err)
	}
	want := "new.ikigenba.dev running 18.220.10.5 -\n" +
		"sbx1.ikigenba.dev running 18.118.7.42 apex\n" +
		"sbx2.ikigenba.dev stopped - -\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "zone " + testRoot, "spaces " + testRoot, "find " + testRoot, "addresses " + testRoot}
	if !reflect.DeepEqual(f.ops, wantOps) {
		t.Fatalf("operations = %#v, want %#v", f.ops, wantOps)
	}
	if f.streamCalls != 0 {
		t.Fatalf("Stream calls = %d", f.streamCalls)
	}
	if want := []seam.Cmd{checkoutCommand(f.root)}; !reflect.DeepEqual(f.execCalls, want) {
		t.Fatalf("Exec commands = %#v, want %#v", f.execCalls, want)
	}

	empty := newOperationFake(t)
	stdout, err = runOperation(t, empty, "list")
	if err != nil || stdout != "" || containsOp(empty.ops, "find ") {
		t.Fatalf("empty list: stdout=%q err=%v ops=%v", stdout, err, empty.ops)
	}
	if !reflect.DeepEqual(empty.execCalls, []seam.Cmd{checkoutCommand(empty.root)}) || empty.streamCalls != 0 {
		t.Fatalf("empty list: Exec=%#v Stream=%d", empty.execCalls, empty.streamCalls)
	}
	for _, values := range [][]string{nil, {"203.0.113.9"}} {
		unheld := newOperationFake(t)
		unheld.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.118.7.42"}}
		if values != nil {
			unheld.records = []cloud.Record{{Name: testRoot, Type: "A", Values: values}}
		}
		stdout, err = runOperation(t, unheld, "list")
		if err != nil || stdout != testDomain+" running 18.118.7.42 -\n" {
			t.Fatalf("unheld apex values=%v stdout=%q err=%v", values, stdout, err)
		}
		if !reflect.DeepEqual(unheld.execCalls, []seam.Cmd{checkoutCommand(unheld.root)}) || unheld.streamCalls != 0 {
			t.Fatalf("unheld apex values=%v Exec=%#v Stream=%d", values, unheld.execCalls, unheld.streamCalls)
		}
	}

	invalid := newOperationFake(t)
	stdout, err = runOperation(t, invalid, "status", "crm.sbx1")
	if err == nil || stdout != "" || containsOp(invalid.ops, "open ") {
		t.Fatalf("parse failure: stdout=%q err=%v ops=%v", stdout, err, invalid.ops)
	}
}

func TestRunActionsLookupAndOutput(t *testing.T) {
	// R-V7NO-JX8V R-VBBD-P8GY R-VCJA-307N R-VDR6-GRYC R-S6SW-5NHI
	t.Run("status relays exact bytes through exact host command", func(t *testing.T) {
		// R-S9WX-LSSA R-8YVJ-0IZI
		for _, output := range []string{"one\ntwo\n", "without trailing newline", ""} {
			t.Run(strings.ReplaceAll(output, "\n", "_"), func(t *testing.T) {
				f := newOperationFake(t)
				f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.118.7.42"}}
				f.execOutput = output
				stdout, err := runOperation(t, f, "status", "sbx1")
				if err != nil || stdout != output {
					t.Fatalf("stdout=%q err=%v, want byte-for-byte %q", stdout, err, output)
				}
				wantExec := []seam.Cmd{
					checkoutCommand(f.root),
					hostCommand(f.root, "18.118.7.42", "'sudo' 'opsctl' 'status'"),
				}
				if !reflect.DeepEqual(f.execCalls, wantExec) {
					t.Fatalf("Exec commands = %#v, want %#v", f.execCalls, wantExec)
				}
				wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot, "ssh 'sudo' 'opsctl' 'status'"}
				if !reflect.DeepEqual(f.ops, wantOps) || f.streamCalls != 0 {
					t.Fatalf("operations=%#v Stream calls=%d, want %#v and zero", f.ops, f.streamCalls, wantOps)
				}
			})
		}
	})
	t.Run("status refuses stopped before host", func(t *testing.T) {
		f := newOperationFake(t)
		f.instances = []cloud.Instance{{ID: "i-two", Space: testDomain, State: cloud.StateStopped}}
		stdout, err := runOperation(t, f, "status", "sbx1.ikigenba.dev")
		var target *NotRunningError
		if stdout != "" || !errors.As(err, &target) || reflect.TypeOf(err) != reflect.TypeOf(target) ||
			target.Domain != testDomain || target.State != cloud.StateStopped || err.Error() != "'"+testDomain+"' is stopped" {
			t.Fatalf("stdout=%q err=%#v ops=%v", stdout, err, f.ops)
		}
		wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot}
		if !reflect.DeepEqual(f.ops, wantOps) || !reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
			t.Fatalf("operations=%#v Exec=%#v Stream=%d", f.ops, f.execCalls, f.streamCalls)
		}
	})
	t.Run("stop and already stopped", func(t *testing.T) {
		// R-8XNM-MR8T
		for _, tc := range []struct {
			name       string
			state      cloud.InstanceState
			wantOut    string
			wantAction []string
		}{
			{name: "running", state: cloud.StateRunning, wantOut: "instance: ok (i-one stopped)\n", wantAction: []string{"stop i-one", "describe i-one"}},
			{name: "already stopped", state: cloud.StateStopped, wantOut: "instance: ok (already stopped)\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newOperationFake(t)
				f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: tc.state}}
				f.described = cloud.Instance{ID: "i-one", Space: testDomain, State: cloud.StateStopped}
				stdout, err := runOperation(t, f, "stop", "sbx1")
				wantOps := append([]string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot}, tc.wantAction...)
				if err != nil || stdout != tc.wantOut || !reflect.DeepEqual(f.ops, wantOps) {
					t.Fatalf("stdout=%q err=%v operations=%#v, want %#v", stdout, err, f.ops, wantOps)
				}
				if !reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
					t.Fatalf("Exec=%#v Stream=%d, want only checkout Exec and no Stream", f.execCalls, f.streamCalls)
				}
			})
		}
	})
	t.Run("start ordered and repeats checks", func(t *testing.T) {
		// R-DP6D-0RTU R-B1XP-NI4K
		for _, state := range []cloud.InstanceState{cloud.StateStopped, cloud.StateRunning} {
			f := newOperationFake(t)
			address := ""
			if state == cloud.StateRunning {
				address = "18.220.10.5"
			}
			f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: state, Address: address}}
			f.described = cloud.Instance{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.220.10.5"}
			f.checks = true
			stdout, err := runOperation(t, f, "start", "sbx1")
			want := "instance: ok (i-one running, 18.220.10.5)\nhost: ok (status checks passed)\ncertificate: ok (certbot renew)\n" + testDomain + " 18.220.10.5\n"
			wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot}
			if state == cloud.StateStopped {
				wantOps = append(wantOps, "start i-one", "describe i-one")
			}
			wantOps = append(wantOps, "checks i-one", "ssh 'true'", "ssh 'sudo' 'certbot' 'renew'")
			wantExec := []seam.Cmd{
				checkoutCommand(f.root),
				hostCommand(f.root, "18.220.10.5", "'true'"),
				hostCommand(f.root, "18.220.10.5", "'sudo' 'certbot' 'renew'"),
			}
			if err != nil || stdout != want || !reflect.DeepEqual(f.ops, wantOps) || !reflect.DeepEqual(f.execCalls, wantExec) || f.streamCalls != 0 {
				t.Fatalf("state=%s stdout=%q err=%v ops=%#v Exec=%#v Stream=%d", state, stdout, err, f.ops, f.execCalls, f.streamCalls)
			}
		}
	})
	t.Run("start failure preserves completed steps", func(t *testing.T) {
		// R-B1XP-NI4K
		f := newOperationFake(t)
		f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.220.10.5"}}
		f.checksErr = errors.New("checks failed")
		stdout, err := runOperation(t, f, "start", "sbx1")
		wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot, "checks i-one"}
		if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(f.checksErr).Pointer() || stdout != "instance: ok (i-one running, 18.220.10.5)\n" || !reflect.DeepEqual(f.ops, wantOps) ||
			!reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
			t.Fatalf("stdout=%q err=%v ops=%v", stdout, err, f.ops)
		}
	})
}

func TestRunActionLookupFailureStopsBeforeEffects(t *testing.T) {
	wantErr := errors.New("lookup sentinel")
	for _, command := range []string{"status", "stop", "start", "destroy"} {
		t.Run(command, func(t *testing.T) {
			f := newOperationFake(t)
			f.spacesErr = wantErr
			args := []string{command, "sbx1"}
			if command == "destroy" {
				args = append(args, "--no-backup")
			}
			stdout, err := runOperation(t, f, args...)
			wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot}
			if err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(wantErr).Pointer() || stdout != "" || !reflect.DeepEqual(f.ops, wantOps) {
				t.Fatalf("stdout=%q error=%#v operations=%#v, want identical error and %#v", stdout, err, f.ops, wantOps)
			}
			if !reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
				t.Fatalf("Exec=%#v Stream=%d, want only checkout Exec and no Stream", f.execCalls, f.streamCalls)
			}
		})
	}

	t.Run("missing lookup returns typed error", func(t *testing.T) {
		for _, command := range []string{"status", "stop", "start"} {
			t.Run(command, func(t *testing.T) {
				f := newOperationFake(t)
				stdout, err := runOperation(t, f, command, "gone")
				var target *cloud.NoSpaceError
				wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot}
				if stdout != "" || !errors.As(err, &target) || reflect.TypeOf(err) != reflect.TypeOf(target) ||
					target.Domain != "gone."+testRoot || !reflect.DeepEqual(f.ops, wantOps) {
					t.Fatalf("stdout=%q error=%#v operations=%#v", stdout, err, f.ops)
				}
				if !reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
					t.Fatalf("Exec=%#v Stream=%d, want only checkout Exec and no Stream", f.execCalls, f.streamCalls)
				}
			})
		}
	})
}

func TestRunPrerequisiteFailuresStopUnchanged(t *testing.T) {
	want := errors.New("sentinel")
	for _, tc := range []struct {
		name string
		args []string
		set  func(*operationFake)
	}{
		{name: "connect", args: []string{"stop", "sbx1"}, set: func(f *operationFake) { f.openErr = want }},
		{name: "identity", args: []string{"stop", "sbx1"}, set: func(f *operationFake) { f.stsErr = want }},
		{name: "lookup", args: []string{"status", "sbx1"}, set: func(f *operationFake) { f.spacesErr = want }},
		{name: "list zone", args: []string{"list"}, set: func(f *operationFake) { f.zoneErr = want }},
		{name: "destroy zone", args: []string{"destroy", "sbx1", "--no-backup"}, set: func(f *operationFake) { f.zoneErr = want }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOperationFake(t)
			tc.set(f)
			stdout, err := runOperation(t, f, tc.args...)
			if stdout != "" || !errors.Is(err, want) || containsOp(f.ops, "ssh ") || containsOp(f.ops, "start ") || containsOp(f.ops, "stop ") || containsOp(f.ops, "terminate ") || containsOp(f.ops, "change ") {
				t.Fatalf("stdout=%q err=%v ops=%v", stdout, err, f.ops)
			}
		})
	}
}

func TestDestroyOrderedOptionsAndCleanup(t *testing.T) {
	// R-VIMR-ZUX4 R-JCCP-CNWX R-VMAH-5657 R-UL0J-793W R-VNID-IXVW R-S80S-JF87 R-S98O-X6YW R-VR62-O93Z
	f := newOperationFake(t)
	f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.118.7.42"}}
	f.described = cloud.Instance{ID: "i-one", State: cloud.StateTerminated}
	f.addresses = []cloud.Address{{AllocationID: "alloc", AssociationID: "assoc", IP: "18.118.7.42", Space: testDomain}}
	f.records = []cloud.Record{
		{Name: testRoot, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}},
		{Name: testDomain, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}},
		{Name: `\052.` + testDomain, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}},
	}
	f.parameters = []cloud.Parameter{{Name: "/a"}, {Name: "/b"}, {Name: "/c"}}
	f.objects = []cloud.Object{{Key: "sbx1/a"}, {Key: "sbx1/b"}}
	f.roleExists = true
	f.execOutput = "opaque retire output that must not affect the step detail\n"
	stdout, err := runOperation(t, f, "destroy", "--delete-backups", "sbx1", "--delete-secrets")
	if err != nil {
		t.Fatal(err)
	}
	want := "apex: ok (ikigenba.dev record deleted)\n" +
		"retire: ok (opsctl retire)\ninstance: ok (i-one terminated)\n" +
		"address: ok (elastic ip 18.118.7.42 released)\n" +
		"records: ok (deleted sbx1.ikigenba.dev, *.sbx1.ikigenba.dev)\n" +
		"secrets: ok (3 parameters deleted)\nbackups: ok (2 objects deleted)\n" +
		"role: ok (sbx1.ikigenba.dev deleted)\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	wantOps := []string{
		"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot, "zone " + testRoot,
		"find " + testRoot, "addresses " + testRoot, "change apex", "ssh 'sudo' 'opsctl' 'retire'",
		"terminate i-one", "describe i-one", "addresses " + testRoot, "disassociate assoc", "release alloc",
		"list records", "change records", "parameters " + secrets.Prefix(testDomain),
		"delete parameters /a", "delete parameters /b", "delete parameters /c",
		"objects " + testRoot + " " + BackupPrefix("sbx1"),
		"delete objects " + testRoot + " sbx1/a,sbx1/b",
		"role " + testDomain, "profile " + testDomain, "delete policy " + testDomain,
		"delete profile " + testDomain, "delete role " + testDomain,
	}
	if !reflect.DeepEqual(f.ops, wantOps) {
		t.Fatalf("operations = %#v, want %#v", f.ops, wantOps)
	}
	wantExec := []seam.Cmd{
		checkoutCommand(f.root),
		hostCommand(f.root, "18.118.7.42", "'sudo' 'opsctl' 'retire'"),
	}
	if !reflect.DeepEqual(f.execCalls, wantExec) || f.streamCalls != 0 {
		t.Fatalf("Exec=%#v Stream=%d, want %#v and zero", f.execCalls, f.streamCalls, wantExec)
	}
	wantApex := cloud.RecordChange{Action: cloud.ChangeDelete, Record: f.records[0]}
	wantRecords := []cloud.RecordChange{
		{Action: cloud.ChangeDelete, Record: f.records[1]},
		{Action: cloud.ChangeDelete, Record: f.records[2]},
	}
	wantChanges := []recordChangeCall{
		{zoneID: "zone", changes: []cloud.RecordChange{wantApex}},
		{zoneID: "zone", changes: wantRecords},
	}
	if !reflect.DeepEqual(f.changeCalls, wantChanges) {
		t.Fatalf("ChangeRecords calls = %#v, want %#v", f.changeCalls, wantChanges)
	}
	if !reflect.DeepEqual(f.findCalls, []string{"zone|" + testRoot + "|A"}) || !reflect.DeepEqual(f.listZones, []string{"zone"}) {
		t.Fatalf("FindRecord=%#v ListRecords zones=%#v", f.findCalls, f.listZones)
	}
	wantDeletes := []deleteObjectsCall{{bucket: testRoot, keys: []string{"sbx1/a", "sbx1/b"}}}
	if !reflect.DeepEqual(f.deleteKeys, wantDeletes) {
		t.Fatalf("DeleteObjects calls = %#v, want %#v", f.deleteKeys, wantDeletes)
	}
	if strings.Contains(stdout, "account:") || strings.Contains(stdout, "domain:") {
		t.Fatalf("obsolete step in %q", stdout)
	}
}

func TestDestroyRetireSudo(t *testing.T) {
	// R-DDFT-AXS8
	for _, failed := range []bool{false, true} {
		name := "success"
		if failed {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newOperationFake(t)
			f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateRunning, Address: "18.220.10.5"}}
			f.described = cloud.Instance{ID: "i-one", State: cloud.StateTerminated}
			if failed {
				f.execStatus = 1
			}
			_, err := runOperation(t, f, "destroy", "sbx1")
			if failed {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) || reflect.ValueOf(err).Pointer() != reflect.ValueOf(commandErr).Pointer() {
					t.Fatalf("error = %#v, want unchanged Sudo error", err)
				}
				if commandErr.Step != "retire" {
					t.Fatalf("Sudo step = %q, want retire", commandErr.Step)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			wantHost := hostCommand(f.root, "18.220.10.5", "'sudo' 'opsctl' 'retire'")
			wantExec := []seam.Cmd{checkoutCommand(f.root), wantHost}
			if !reflect.DeepEqual(f.execCalls, wantExec) {
				t.Fatalf("Exec calls = %#v, want %#v", f.execCalls, wantExec)
			}
			retireAt := -1
			for i, op := range f.ops {
				if op == "ssh 'sudo' 'opsctl' 'retire'" {
					retireAt = i
					break
				}
			}
			if retireAt < 0 {
				t.Fatalf("retire Sudo absent from operations: %#v", f.ops)
			}
			for _, op := range f.ops[:retireAt] {
				for _, prefix := range []string{"start ", "stop ", "terminate ", "release ", "disassociate ", "change records", "delete ", "remove role "} {
					if strings.HasPrefix(op, prefix) {
						t.Fatalf("mutation %q preceded retire Sudo: %#v", op, f.ops)
					}
				}
			}
			if failed && len(f.ops) != retireAt+1 {
				t.Fatalf("operations after failed retire Sudo: %#v", f.ops[retireAt+1:])
			}
		})
	}
}

func TestDestroyRefusalAndResumptionBranches(t *testing.T) {
	// R-VHEV-M36F R-VL2K-REEI R-UL0J-793W
	for _, operand := range []string{"sbx1", testDomain} {
		t.Run("non-running refuses unchanged "+operand, func(t *testing.T) {
			f := newOperationFake(t)
			f.instances = []cloud.Instance{{ID: "i-refuse", Space: testDomain, State: cloud.StateStopped, Address: "18.118.7.42"}}
			f.records = []cloud.Record{{Name: testRoot, Type: "A", TTL: 60, Values: []string{"18.118.7.42"}}}
			f.addresses = []cloud.Address{{AllocationID: "alloc", IP: "18.118.7.42", Space: testDomain}}
			stdout, err := runOperation(t, f, "destroy", operand)
			var target *RetireStateError
			wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot, "zone " + testRoot}
			if stdout != "" || !errors.As(err, &target) || reflect.TypeOf(err) != reflect.TypeOf(target) ||
				target.ID != "i-refuse" || target.State != cloud.StateStopped || target.Label != "sbx1" ||
				!reflect.DeepEqual(f.ops, wantOps) || len(f.changeCalls) != 0 ||
				!reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
				t.Fatalf("stdout=%q err=%#v operations=%#v Exec=%#v changes=%#v", stdout, err, f.ops, f.execCalls, f.changeCalls)
			}
		})
	}
	t.Run("zone error is unchanged before refusal and mutation", func(t *testing.T) {
		f := newOperationFake(t)
		f.instances = []cloud.Instance{{ID: "i-refuse", Space: testDomain, State: cloud.StateStopped}}
		f.zoneErr = errors.New("zone sentinel")
		stdout, err := runOperation(t, f, "destroy", "sbx1")
		wantOps := []string{"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot, "zone " + testRoot}
		if stdout != "" || err == nil || reflect.ValueOf(err).Pointer() != reflect.ValueOf(f.zoneErr).Pointer() || !reflect.DeepEqual(f.ops, wantOps) || len(f.changeCalls) != 0 ||
			!reflect.DeepEqual(f.execCalls, []seam.Cmd{checkoutCommand(f.root)}) || f.streamCalls != 0 {
			t.Fatalf("stdout=%q err=%#v operations=%#v Exec=%#v", stdout, err, f.ops, f.execCalls)
		}
	})
	t.Run("no backup skips host and keeps independent data", func(t *testing.T) {
		f := newOperationFake(t)
		f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateStopped}}
		f.described = cloud.Instance{ID: "i-one", State: cloud.StateTerminated}
		stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup")
		want := "retire: skipped (--no-backup)\ninstance: ok (i-one terminated)\naddress: ok (no elastic ip)\n" +
			"records: ok (already gone)\nsecrets: ok (kept)\nbackups: ok (kept)\nrole: ok (already gone)\n"
		if err != nil || stdout != want || containsOp(f.ops, "ssh ") || containsOp(f.ops, "parameters ") || containsOp(f.ops, "objects ") {
			t.Fatalf("stdout=%q err=%v ops=%v", stdout, err, f.ops)
		}
	})
	t.Run("already gone", func(t *testing.T) {
		f := newOperationFake(t)
		stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup", "--delete-secrets", "--delete-backups")
		want := "retire: ok (already gone)\ninstance: ok (already gone)\naddress: ok (already gone)\n" +
			"records: ok (already gone)\nsecrets: ok (already gone)\nbackups: ok (already gone)\nrole: ok (already gone)\n"
		if err != nil || stdout != want || containsOp(f.ops, "terminate ") || containsOp(f.ops, "release ") || containsOp(f.ops, "ssh ") {
			t.Fatalf("stdout=%q err=%v ops=%v", stdout, err, f.ops)
		}
	})
}

func TestDestroyApexAndCleanupBranches(t *testing.T) {
	t.Run("apex held elsewhere is unchanged", func(t *testing.T) {
		f := newOperationFake(t)
		apex := cloud.Record{Name: testRoot, Type: "A", TTL: 300, Values: []string{"203.0.113.8"}}
		f.records = []cloud.Record{apex}
		f.addresses = []cloud.Address{{AllocationID: "other", IP: "203.0.113.8", Space: "other." + testRoot}}
		stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup")
		if err != nil || strings.Contains(stdout, "apex:") || len(f.changeCalls) != 0 ||
			!reflect.DeepEqual(f.findCalls, []string{"zone|" + testRoot + "|A"}) {
			t.Fatalf("stdout=%q err=%v changes=%#v find=%#v", stdout, err, f.changeCalls, f.findCalls)
		}
	})

	t.Run("bare record is the only record deleted", func(t *testing.T) {
		f := newOperationFake(t)
		bare := cloud.Record{Name: testDomain, Type: "A", TTL: 123, Values: []string{"192.0.2.4"}}
		f.records = []cloud.Record{bare, {Name: testDomain, Type: "TXT", TTL: 9, Values: []string{"keep"}}}
		stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup")
		wantChange := []recordChangeCall{{zoneID: "zone", changes: []cloud.RecordChange{{Action: cloud.ChangeDelete, Record: bare}}}}
		if err != nil || !strings.Contains(stdout, "records: ok (deleted "+testDomain+")\n") ||
			!reflect.DeepEqual(f.changeCalls, wantChange) || !reflect.DeepEqual(f.listZones, []string{"zone"}) {
			t.Fatalf("stdout=%q err=%v ChangeRecords=%#v ListRecords zones=%#v", stdout, err, f.changeCalls, f.listZones)
		}
	})

	t.Run("secrets empty and deleted use exact prefix", func(t *testing.T) {
		for _, parameters := range [][]cloud.Parameter{nil, {{Name: "/one"}, {Name: "/two"}, {Name: "/three"}}} {
			f := newOperationFake(t)
			f.parameters = parameters
			stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup", "--delete-secrets")
			wantDetail := "already gone"
			wantOps := []string{"parameters " + secrets.Prefix(testDomain)}
			if len(parameters) != 0 {
				wantDetail = "3 parameters deleted"
				wantOps = append(wantOps, "delete parameters /one", "delete parameters /two", "delete parameters /three")
			}
			if err != nil || !strings.Contains(stdout, "secrets: ok ("+wantDetail+")\n") || !orderedSubset(f.ops, wantOps) {
				t.Fatalf("parameters=%#v stdout=%q err=%v ops=%#v", parameters, stdout, err, f.ops)
			}
			for _, op := range wantOps {
				if countExact(f.ops, op) != 1 {
					t.Fatalf("operation %q count=%d in %#v", op, countExact(f.ops, op), f.ops)
				}
			}
		}
	})

	t.Run("backups empty and deleted use exact bucket prefix and keys", func(t *testing.T) {
		for _, objects := range [][]cloud.Object{nil, {{Key: "sbx1/one"}, {Key: "sbx1/two"}}} {
			f := newOperationFake(t)
			f.objects = objects
			stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup", "--delete-backups")
			wantDetail := "already gone"
			var wantDeletes []deleteObjectsCall
			if len(objects) != 0 {
				wantDetail = "2 objects deleted"
				wantDeletes = []deleteObjectsCall{{bucket: testRoot, keys: []string{"sbx1/one", "sbx1/two"}}}
			}
			listOp := "objects " + testRoot + " " + BackupPrefix("sbx1")
			if err != nil || !strings.Contains(stdout, "backups: ok ("+wantDetail+")\n") || countExact(f.ops, listOp) != 1 ||
				!reflect.DeepEqual(f.deleteKeys, wantDeletes) {
				t.Fatalf("objects=%#v stdout=%q err=%v ops=%#v deletes=%#v", objects, stdout, err, f.ops, f.deleteKeys)
			}
		}
	})

	t.Run("role absent and present", func(t *testing.T) {
		for _, exists := range []bool{false, true} {
			f := newOperationFake(t)
			f.roleExists = exists
			stdout, err := runOperation(t, f, "destroy", "sbx1", "--no-backup")
			wantDetail := "already gone"
			wantRoleOps := []string{"role " + testDomain, "profile " + testDomain}
			if exists {
				wantDetail = testDomain + " deleted"
				wantRoleOps = append(wantRoleOps, "delete policy "+testDomain, "delete profile "+testDomain, "delete role "+testDomain)
			}
			if err != nil || !strings.Contains(stdout, "role: ok ("+wantDetail+")\n") || !orderedSubset(f.ops, wantRoleOps) {
				t.Fatalf("exists=%t stdout=%q err=%v ops=%#v", exists, stdout, err, f.ops)
			}
			for _, op := range wantRoleOps {
				if countExact(f.ops, op) != 1 {
					t.Fatalf("operation %q count=%d in %#v", op, countExact(f.ops, op), f.ops)
				}
			}
		}
	})
}

func TestDestroyStopsAtEveryFirstFailure(t *testing.T) {
	// R-VEZ2-UJP1
	initialOps := []string{
		"git", "open " + testRoot + " " + testRegion, "sts", "spaces " + testRoot,
		"zone " + testRoot, "find " + testRoot,
	}
	normalOps := append(append([]string{}, initialOps...),
		"terminate i-one", "describe i-one", "addresses "+testRoot,
		"disassociate assoc", "release alloc", "list records", "change records",
		"parameters "+secrets.Prefix(testDomain), "delete parameters /one", "delete parameters /two",
		"objects "+testRoot+" "+BackupPrefix("sbx1"), "delete objects "+testRoot+" sbx1/one,sbx1/two",
		"role "+testDomain, "profile "+testDomain, "delete policy "+testDomain,
		"remove role "+testDomain+" "+testDomain, "delete profile "+testDomain, "delete role "+testDomain,
	)
	through := func(op string) []string {
		for index, candidate := range normalOps {
			if candidate == op {
				return append([]string(nil), normalOps[:index+1]...)
			}
		}
		t.Fatalf("failure operation %q is not in the normal destroy path", op)
		return nil
	}
	retireSkipped := "retire: skipped (--no-backup)\n"
	instanceDone := retireSkipped + "instance: ok (i-one terminated)\n"
	addressDone := instanceDone + "address: ok (elastic ip 18.118.7.42 released)\n"
	recordsDone := addressDone + "records: ok (deleted " + testDomain + ")\n"
	secretsDone := recordsDone + "secrets: ok (2 parameters deleted)\n"
	backupsDone := secretsDone + "backups: ok (2 objects deleted)\n"

	cases := []struct {
		name        string
		args        []string
		failOp      string
		execFailure bool
		setup       func(*operationFake)
		wantStdout  string
		wantOps     []string
	}{
		{name: "apex lookup", failOp: "find " + testRoot, wantOps: append([]string(nil), initialOps...)},
		{name: "apex address lookup", failOp: "addresses " + testRoot, setup: addHeldApex,
			wantOps: append(append([]string{}, initialOps...), "addresses "+testRoot)},
		{name: "apex delete", failOp: "change apex", setup: addHeldApex,
			wantOps: append(append([]string{}, initialOps...), "addresses "+testRoot, "change apex")},
		{name: "retire", args: []string{"destroy", "sbx1"}, failOp: "ssh 'sudo' 'opsctl' 'retire'", execFailure: true,
			setup: func(f *operationFake) {
				f.instances[0].State = cloud.StateRunning
				f.instances[0].Address = "18.118.7.42"
			},
			wantOps: append(append([]string{}, initialOps...), "ssh 'sudo' 'opsctl' 'retire'")},
		{name: "instance terminate", failOp: "terminate i-one", wantStdout: retireSkipped},
		{name: "instance wait", failOp: "describe i-one", wantStdout: retireSkipped},
		{name: "address lookup", failOp: "addresses " + testRoot, wantStdout: instanceDone},
		{name: "address disassociate", failOp: "disassociate assoc", wantStdout: instanceDone},
		{name: "address release", failOp: "release alloc", wantStdout: instanceDone},
		{name: "records list", failOp: "list records", wantStdout: addressDone},
		{name: "records delete", failOp: "change records", wantStdout: addressDone},
		{name: "secrets list", failOp: "parameters " + secrets.Prefix(testDomain), wantStdout: recordsDone},
		{name: "secrets delete", failOp: "delete parameters /one", wantStdout: recordsDone},
		{name: "backups list", failOp: "objects " + testRoot + " " + BackupPrefix("sbx1"), wantStdout: secretsDone},
		{name: "backups delete", failOp: "delete objects " + testRoot + " sbx1/one,sbx1/two", wantStdout: secretsDone},
		{name: "role lookup", failOp: "role " + testDomain, wantStdout: backupsDone},
		{name: "role profile lookup", failOp: "profile " + testDomain, wantStdout: backupsDone},
		{name: "role policy delete", failOp: "delete policy " + testDomain, wantStdout: backupsDone},
		{name: "role profile detach", failOp: "remove role " + testDomain + " " + testDomain, wantStdout: backupsDone},
		{name: "role profile delete", failOp: "delete profile " + testDomain, wantStdout: backupsDone},
		{name: "role delete", failOp: "delete role " + testDomain, wantStdout: backupsDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOperationFake(t)
			f.instances = []cloud.Instance{{ID: "i-one", Space: testDomain, State: cloud.StateStopped}}
			f.described = cloud.Instance{ID: "i-one", State: cloud.StateTerminated}
			f.addresses = []cloud.Address{{AllocationID: "alloc", AssociationID: "assoc", IP: "18.118.7.42", Space: testDomain}}
			f.records = []cloud.Record{{Name: testDomain, Type: "A", Values: []string{"18.118.7.42"}}}
			f.parameters = []cloud.Parameter{{Name: "/one"}, {Name: "/two"}}
			f.objects = []cloud.Object{{Key: "sbx1/one"}, {Key: "sbx1/two"}}
			f.roleExists = true
			f.profileRoles = []string{testDomain}
			if tc.setup != nil {
				tc.setup(f)
			}
			wantErr := errors.New(tc.name + " sentinel")
			if tc.execFailure {
				f.execErr = wantErr
			} else {
				f.opErrors[tc.failOp] = wantErr
			}
			args := tc.args
			if args == nil {
				args = []string{"destroy", "sbx1", "--no-backup", "--delete-secrets", "--delete-backups"}
			}
			wantOps := tc.wantOps
			if wantOps == nil {
				wantOps = through(tc.failOp)
			}
			stdout, err := runOperation(t, f, args...)
			if !errors.Is(err, wantErr) || stdout != tc.wantStdout || !reflect.DeepEqual(f.ops, wantOps) {
				t.Fatalf("stdout=%q, want %q; err=%#v; operations=%#v, want %#v", stdout, tc.wantStdout, err, f.ops, wantOps)
			}
			if last(f.ops) != tc.failOp || countExact(f.ops, tc.failOp) != 1 {
				t.Fatalf("failed operation %q was not the single final call in %#v", tc.failOp, f.ops)
			}
		})
	}
}

func addHeldApex(f *operationFake) {
	f.records = append([]cloud.Record{{Name: testRoot, Type: "A", Values: []string{"18.118.7.42"}}}, f.records...)
}

func runOperation(t *testing.T, f *operationFake, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "infra", "terraform.tfvars.json"), []byte(`{"domain":"`+testRoot+`","region":"`+testRegion+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.root = dir
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, f.deps(dir))
	return stdout.String(), err
}

type operationFake struct {
	t            *testing.T
	root         string
	ops          []string
	instances    []cloud.Instance
	described    cloud.Instance
	addresses    []cloud.Address
	records      []cloud.Record
	parameters   []cloud.Parameter
	objects      []cloud.Object
	profileRoles []string
	roleExists   bool
	checks       bool
	checksErr    error
	execOutput   string
	execStderr   string
	execStatus   int
	execErr      error
	execCalls    []seam.Cmd
	changeErr    error
	opErrors     map[string]error
	changeCalls  []recordChangeCall
	deleteKeys   []deleteObjectsCall
	findCalls    []string
	listZones    []string
	streamCalls  int
	openErr      error
	stsErr       error
	zoneErr      error
	spacesErr    error
}

type recordChangeCall struct {
	zoneID  string
	changes []cloud.RecordChange
}

type deleteObjectsCall struct {
	bucket string
	keys   []string
}

func newOperationFake(t *testing.T) *operationFake {
	return &operationFake{t: t, checks: true, opErrors: make(map[string]error)}
}

func (f *operationFake) deps(dir string) seam.Deps {
	return seam.Deps{
		Dir: dir,
		Cloud: func(_ context.Context, profile, region string) (cloud.Clients, error) {
			f.ops = append(f.ops, "open "+profile+" "+region)
			if f.openErr != nil {
				return cloud.Clients{}, f.openErr
			}
			return cloud.Clients{EC2: f, SSM: f, Route53: f, S3: f, IAM: f, STS: f}, nil
		},
		Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			f.execCalls = append(f.execCalls, cmd)
			if cmd.Path == "git" {
				f.ops = append(f.ops, "git")
				return seam.Result{Stdout: []byte(f.root + "\n")}, nil
			}
			f.ops = append(f.ops, "ssh "+cmd.Args[len(cmd.Args)-1])
			return seam.Result{Stdout: []byte(f.execOutput), Stderr: []byte(f.execStderr), ExitCode: f.execStatus}, f.execErr
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			f.streamCalls++
			return seam.Result{}, nil
		},
		After: func(time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- time.Time{}; return ch },
	}
}

func (f *operationFake) CallerAccountID(context.Context) (string, error) {
	f.ops = append(f.ops, "sts")
	return "123", f.stsErr
}
func (f *operationFake) ListSpaceInstances(_ context.Context, root string) ([]cloud.Instance, error) {
	f.ops = append(f.ops, "spaces "+root)
	return f.instances, f.spacesErr
}
func (f *operationFake) DescribeInstance(_ context.Context, id string) (cloud.Instance, error) {
	op := "describe " + id
	f.ops = append(f.ops, op)
	return f.described, f.opErrors[op]
}
func (f *operationFake) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	panic("unexpected")
}
func (f *operationFake) LaunchTemplate(context.Context, string) (string, error) { panic("unexpected") }
func (f *operationFake) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) {
	panic("unexpected")
}
func (f *operationFake) StartInstance(_ context.Context, id string) error {
	f.ops = append(f.ops, "start "+id)
	return nil
}
func (f *operationFake) StopInstance(_ context.Context, id string) error {
	f.ops = append(f.ops, "stop "+id)
	return nil
}
func (f *operationFake) TerminateInstance(_ context.Context, id string) error {
	op := "terminate " + id
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) InstanceChecksPassed(_ context.Context, id string) (bool, error) {
	f.ops = append(f.ops, "checks "+id)
	return f.checks, f.checksErr
}
func (f *operationFake) ListSpaceAddresses(_ context.Context, root string) ([]cloud.Address, error) {
	op := "addresses " + root
	f.ops = append(f.ops, op)
	return f.addresses, f.opErrors[op]
}
func (f *operationFake) AllocateAddress(context.Context, string, string) (cloud.Address, error) {
	panic("unexpected")
}
func (f *operationFake) AssociateAddress(context.Context, string, string) error { panic("unexpected") }
func (f *operationFake) DisassociateAddress(_ context.Context, id string) error {
	op := "disassociate " + id
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) ReleaseAddress(_ context.Context, id string) error {
	op := "release " + id
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) Zone(_ context.Context, root string) (cloud.Zone, error) {
	f.ops = append(f.ops, "zone "+root)
	return cloud.Zone{ID: "zone", Name: root}, f.zoneErr
}
func (f *operationFake) ListRecords(_ context.Context, zoneID string) ([]cloud.Record, error) {
	op := "list records"
	f.ops = append(f.ops, op)
	f.listZones = append(f.listZones, zoneID)
	return f.records, f.opErrors[op]
}
func (f *operationFake) FindRecord(_ context.Context, zoneID, name, recordType string) (cloud.Record, bool, error) {
	op := "find " + name
	f.ops = append(f.ops, op)
	f.findCalls = append(f.findCalls, zoneID+"|"+name+"|"+recordType)
	if err := f.opErrors[op]; err != nil {
		return cloud.Record{}, false, err
	}
	for _, r := range f.records {
		if r.Name == name && r.Type == "A" {
			return r, true, nil
		}
	}
	return cloud.Record{}, false, nil
}
func (f *operationFake) ChangeRecords(_ context.Context, zoneID string, changes []cloud.RecordChange) (string, error) {
	f.changeCalls = append(f.changeCalls, recordChangeCall{zoneID: zoneID, changes: append([]cloud.RecordChange(nil), changes...)})
	op := "change records"
	if len(changes) == 1 && changes[0].Record.Name == testRoot {
		op = "change apex"
	}
	f.ops = append(f.ops, op)
	if err := f.opErrors[op]; err != nil {
		return "", err
	}
	return "change", f.changeErr
}
func (f *operationFake) ChangeStatus(context.Context, string) (cloud.ChangeStatus, error) {
	return cloud.ChangeInsync, nil
}
func (f *operationFake) GetParameter(context.Context, string) (string, error) { panic("unexpected") }
func (f *operationFake) PutSecureParameter(context.Context, string, string) error {
	panic("unexpected")
}
func (f *operationFake) ListParameters(_ context.Context, prefix string) ([]cloud.Parameter, error) {
	op := "parameters " + prefix
	f.ops = append(f.ops, op)
	return f.parameters, f.opErrors[op]
}
func (f *operationFake) DeleteParameter(_ context.Context, name string) error {
	op := "delete parameters " + name
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	op := "objects " + bucket + " " + prefix
	f.ops = append(f.ops, op)
	return f.objects, f.opErrors[op]
}
func (f *operationFake) PutObject(context.Context, string, string, io.Reader, int64) error {
	panic("unexpected")
}
func (f *operationFake) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	f.deleteKeys = append(f.deleteKeys, deleteObjectsCall{bucket: bucket, keys: append([]string(nil), keys...)})
	op := "delete objects " + bucket + " " + strings.Join(keys, ",")
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) PermissionsBoundary(context.Context, string) (string, error) {
	panic("unexpected")
}
func (f *operationFake) RoleExists(_ context.Context, name string) (bool, error) {
	op := "role " + name
	f.ops = append(f.ops, op)
	return f.roleExists, f.opErrors[op]
}
func (f *operationFake) CreateRole(context.Context, cloud.RoleSpec) error { panic("unexpected") }
func (f *operationFake) PutRolePolicy(context.Context, string, string, string) error {
	panic("unexpected")
}
func (f *operationFake) DeleteRolePolicy(_ context.Context, role, _ string) error {
	op := "delete policy " + role
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) InstanceProfileRoles(_ context.Context, name string) ([]string, bool, error) {
	op := "profile " + name
	f.ops = append(f.ops, op)
	return append([]string(nil), f.profileRoles...), f.roleExists, f.opErrors[op]
}
func (f *operationFake) CreateInstanceProfile(context.Context, string) error { panic("unexpected") }
func (f *operationFake) AddRoleToInstanceProfile(context.Context, string, string) error {
	panic("unexpected")
}
func (f *operationFake) RemoveRoleFromInstanceProfile(_ context.Context, profile, role string) error {
	op := "remove role " + profile + " " + role
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) DeleteInstanceProfile(_ context.Context, name string) error {
	op := "delete profile " + name
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}
func (f *operationFake) DeleteRole(_ context.Context, name string) error {
	op := "delete role " + name
	f.ops = append(f.ops, op)
	return f.opErrors[op]
}

func containsOp(ops []string, prefix string) bool {
	for _, op := range ops {
		if strings.HasPrefix(op, prefix) {
			return true
		}
	}
	return false
}
func countExact(ops []string, want string) int {
	count := 0
	for _, op := range ops {
		if op == want {
			count++
		}
	}
	return count
}

func orderedSubset(ops, want []string) bool {
	index := 0
	for _, op := range ops {
		if index < len(want) && op == want[index] {
			index++
		}
	}
	return index == len(want)
}
func last(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func checkoutCommand(dir string) seam.Cmd {
	return seam.Cmd{Path: "git", Args: []string{"rev-parse", "--show-toplevel"}, Dir: dir}
}

func hostCommand(dir, address, remote string) seam.Cmd {
	return seam.Cmd{
		Path: "ssh",
		Args: []string{
			"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new",
			"ec2-user@" + address, remote,
		},
		Dir: dir,
	}
}

var _ = secrets.Prefix
