package spacecreate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func TestCreateApexGrammarBeforeAnyWork(t *testing.T) {
	// R-3B9D-MSO3 R-38TK-V96P
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"sbx1", "--acme-email=x", "--apex"}, "option '--apex' requires a value"},
		{[]string{"sbx1", "--acme-email=x", "--apex="}, "option '--apex' requires a value"},
		{[]string{"sbx1", "--acme-email=x", "--apex", "crm", "--no-apex"}, "space create takes --apex or --no-apex, not both"},
		{[]string{"sbx1", "--no-apex", "--acme-email=x", "--apex=crm"}, "space create takes --apex or --no-apex, not both"},
	} {
		var out bytes.Buffer
		err := Run(t.Context(), tc.args, "test-version", &out, seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) { t.Fatal("Exec"); return seam.Result{}, nil }, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			t.Fatal("Cloud")
			return cloud.Clients{}, nil
		}})
		var usage *space.UsageError
		if !errors.As(err, &usage) || usage.Message != tc.want || out.Len() != 0 {
			t.Fatalf("%v out=%q err=%v", tc.args, out.String(), err)
		}
	}
	inv, err := parseInvocation([]string{"--apex", "crm", "sbx1", "--acme-email=x"})
	if err != nil || inv.apex != "crm" || inv.operand != "sbx1" {
		t.Fatalf("%#v %v", inv, err)
	}
}

func TestCreateRefusesAddressOnlyOrDuplicateSpace(t *testing.T) {
	// R-3CHA-0KES
	for _, tc := range []struct {
		name      string
		instances []cloud.Instance
		addresses []cloud.Address
		want      string
	}{
		{name: "instance", instances: []cloud.Instance{{ID: "i-old", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning}}, want: "a space at 'sbx1.ikigenba.dev' already exists (i-old)"},
		{name: "address", instances: []cloud.Instance{{ID: "old", Space: "sbx1.ikigenba.dev", State: cloud.StateTerminated}}, addresses: []cloud.Address{{AllocationID: "alloc", Space: "sbx1.ikigenba.dev", IP: "18.118.7.42"}}, want: "a space at 'sbx1.ikigenba.dev' already exists (elastic ip 18.118.7.42)"},
		{name: "duplicate", addresses: []cloud.Address{{AllocationID: "a", Space: "sbx1.ikigenba.dev", IP: "18.190.33.61"}, {AllocationID: "b", Space: "sbx1.ikigenba.dev", IP: "18.118.7.42"}}, want: "'sbx1.ikigenba.dev' has more than one Elastic IP (18.118.7.42, 18.190.33.61)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCreateFake(t)
			f.instances = tc.instances
			f.addresses = tc.addresses
			var out bytes.Buffer
			err := Run(t.Context(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, f.deps())
			if err == nil || err.Error() != tc.want || out.Len() != 0 || contains(f.events, "build") {
				t.Fatalf("out=%q err=%v events=%v", out.String(), err, f.events)
			}
		})
	}
}

func TestCreateApexAppMustBelongToBuiltRelease(t *testing.T) {
	// R-26O1-7ZW4 R-37LO-HHG0
	for _, raw := range []bool{false, true} {
		f := newCreateFake(t)
		args := []string{"sbx1", "--acme-email=x", "--apex=missing"}
		if raw {
			args = append(args, "--release", createSHA)
		}
		var out bytes.Buffer
		err := Run(t.Context(), args, "test-version", &out, f.deps())
		var absent *NotInReleaseError
		name := "release-87"
		if raw {
			name = "dist/" + createSHA + ".tar.xz"
		}
		if !errors.As(err, &absent) || absent.App != "missing" || absent.Release != name || absent.Error() != "'missing' is not an app in "+name || absent.ExitCode() != 1 || contains(f.events, "secret") || !strings.Contains(out.String(), "build: ok ("+name) {
			t.Fatalf("out=%q err=%v", out.String(), err)
		}
		if _, err := os.Stat(filepath.Join(f.root, "dist", createSHA+".tar.xz")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreateResolveFailureStopsBeforeAnyHostOrCleanup(t *testing.T) {
	// R-3L0K-OYLN
	f := newCreateFake(t)
	deps := f.deps()
	deps.LookupHost = func(string) ([]string, error) { return []string{"198.51.100.7"}, nil }
	var out bytes.Buffer
	err := Run(t.Context(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, deps)
	var dns *space.ResolveError
	if !errors.As(err, &dns) || dns.Domain != "sbx1.ikigenba.dev" || dns.Address != "18.118.7.42" || !strings.HasSuffix(out.String(), "records: ok (created sbx1.ikigenba.dev, *.sbx1.ikigenba.dev -> 18.118.7.42, INSYNC)\n") || contains(f.events, "checks") {
		t.Fatalf("out=%q err=%v events=%v", out.String(), err, f.events)
	}
	for _, cmd := range f.commands {
		if cmd.Path == "ssh" || cmd.Path == "scp" || cmd.Path == "ssh-keygen" {
			t.Fatal(cmd)
		}
	}
}

func TestCreateApexStateControlsRoleAndRoot(t *testing.T) {
	// R-O5VN-GR82 R-NUWK-0TJT R-NW4G-ELAI R-NYK9-64RW R-49EK-CDFJ R-DYY1-CBDS
	for _, tc := range []struct {
		name       string
		backup     bool
		option     string
		getStatus  int
		holds      bool
		apexDetail string
	}{
		{name: "fresh"}, {name: "fresh-explicit", option: "--apex=crm", holds: true, apexDetail: "host.apex=crm"}, {name: "restored-kept", backup: true, holds: true}, {name: "restored-unset", backup: true, getStatus: 1}, {name: "restored-delete", backup: true, option: "--no-apex", apexDetail: "host.apex unset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCreateFake(t)
			f.apexStatus = tc.getStatus
			if tc.backup {
				f.objects = []cloud.Object{{Key: "sbx1/host/2026-09-12T14:22:51Z.tar.zst"}}
			}
			args := []string{"sbx1", "--acme-email=x"}
			if tc.option != "" {
				args = append(args, tc.option)
			}
			var out bytes.Buffer
			if err := Run(t.Context(), args, "test-version", &out, f.deps()); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			known := "known_hosts: ok (removed sbx1.ikigenba.dev)\n"
			if tc.backup {
				known = "known_hosts: ok (kept)\n"
			}
			between := known
			if tc.apexDetail != "" {
				between += "apex: ok (" + tc.apexDetail + ")\n"
			}
			if tc.holds {
				between += "role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n"
			}
			between += "init: ok\n"
			if !strings.Contains(got, between) {
				t.Fatalf("missing %q in %q", between, got)
			}
			tail := "activate output\n"
			if tc.holds {
				tail += "record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)\nprevious: ok (none)\n"
			}
			tail += "sbx1.ikigenba.dev 18.118.7.42\n"
			if !strings.HasSuffix(got, tail) {
				t.Fatal(got)
			}
			var apexCommands []string
			for _, remote := range f.remotes {
				if strings.Contains(remote, "host.apex") {
					apexCommands = append(apexCommands, remote)
				}
			}
			wantCommand := ""
			switch {
			case tc.option == "--apex=crm":
				wantCommand = "'config' 'set' '" + hostsetup.KeyHostApex + "=crm'"
			case tc.option == "--no-apex":
				wantCommand = "'config' 'del' '" + hostsetup.KeyHostApex + "'"
			case tc.backup:
				wantCommand = "'config' 'get' '" + hostsetup.KeyHostApex + "'"
			}
			if wantCommand == "" {
				if len(apexCommands) != 0 {
					t.Fatal(apexCommands)
				}
			} else if len(apexCommands) != 1 || !strings.Contains(apexCommands[0], wantCommand) {
				t.Fatal(apexCommands)
			}
			wantPolicy := 1
			if tc.holds {
				wantPolicy = 2
			}
			count := 0
			for _, event := range f.events {
				if event == "put-policy" {
					count++
				}
			}
			if count != wantPolicy {
				t.Fatal(f.events)
			}
			if !reflect.DeepEqual([]string{"account", "domain", "build", "secrets", "role", "instance", "address", "records", "resolve", "host", "copy", "unpack", "opsctl", "restore", "known_hosts"}, stepNames(got)[:15]) {
				t.Fatal(got)
			}
		})
	}
}
func stepNames(text string) []string {
	var names []string
	for _, line := range strings.Split(text, "\n") {
		if name, _, ok := strings.Cut(line, ": ok"); ok {
			names = append(names, name)
		}
	}
	return names
}
