package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/secrets"
)

var _ func(context.Context, []string, io.Reader, io.Writer, io.Writer, seam.Deps) int = Run

const expectedUsage = `Usage: devctl [options] <command> [arguments]

Manage the platform from the developer's machine. Never run as root.

Commands:
  version   print the version
  space     list, create, destroy, stop, start, initialise, and inspect spaces
  secrets   push and list an app's secrets for a space
  build     build one app into its deployable file
  deploy    put a built app file on a space
  remove    take an app off a space
  restore   put a space's app back from its backups
  apex      point the root domain at one app on one space

Options:
  --help              print this help
  --version           print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
`

const expectedD05Usage = `Usage: devctl secrets <subcommand> <space> [<app>]

Push the values an app's manifest names from this machine's keyring to the
space's Parameter Store entry, or list which names a space holds. Values are
never printed.

Subcommands:
  push <space> [<app>]   write /<space domain>/<app> for one app, or every app
  list <space> [<app>]   print the key names held for one app, or every app

Run 'devctl secrets <subcommand> --help' for details.
`

const expectedD05PushUsage = `Usage: devctl secrets push <space> [<app>]

Write the space's Parameter Store entry for an app from this machine's keyring:
a SecureString at /<space domain>/<app> holding a JSON object whose keys are
the names the app's manifest declares. Each value comes from the environment
variable of that name, or from the login keyring. Values are never printed.

With <app> omitted, every app in the checkout is written, in name order. Every
value is gathered before anything is written, so one missing value leaves every
app's entry as it was.

Arguments:
  <space>    the space to write the entry in, by label or full domain
  <app>      one app of the checkout; omitted, every app
`

const expectedD05ListUsage = `Usage: devctl secrets list <space> [<app>]

Print the key names the space's Parameter Store entry holds for an app: the
app, then its names sorted and comma-separated, or - when the entry is empty.
Only names are printed; a value never is.

With <app> omitted, every entry under /<space domain>/ is printed, in app
order, and a space that holds none prints nothing.

Arguments:
  <space>    the space to read the entries of, by label or full domain
  <app>      one app the space holds an entry for; omitted, every app
`

const expectedD06Usage = `Usage: devctl space <subcommand> [arguments]

List, create, destroy, stop, start, initialise, and inspect spaces, and
restart, disable, enable, or read the journal of one app on one. A space is
one label under the root domain; <space> is that label or the full domain.
The cloud's tags are the only registry.

Subcommands:
  list                       one line per space
  create <space> [options]   create the space
  destroy <space> [options]  remove the space and everything it owned
  stop <space>               stop the instance; state is kept
  start <space>              start the instance; its address is unchanged
  init <space> [options]     set the host's keys again and run opsctl init
  status <space>             one line per app: version, service state, socket state, database journal mode
  restart <space> <app>      restart one app's service on the host
  disable <space> <app>      stop one app and keep it from starting until enabled
  enable <space> <app>       let a disabled app start again, and start it
  logs <space> <app>         print one app's journal from the host

Options (create):
  --acme-email <address>  where the CA sends the space's expiry warnings; required

Options (destroy):
  --no-backup             skip the final backup the host takes before it goes
  --delete-secrets        delete the space's secrets; they are kept otherwise
  --delete-backups        delete the space's backups; they are kept otherwise

Options (init):
  --opsctl <version>      move the host to this opsctl release first
  --acme-email <address>  change where the CA sends the space's expiry warnings

Options (logs):
  --follow                keep printing as the app writes, until interrupted
  --since <when>          start at this moment, as journalctl reads it: -1h, yesterday, 2026-09-11 18:00:00

Run 'devctl space <subcommand> --help' for details.
`

const expectedD06ListUsage = `Usage: devctl space list

Print one line per space: the domain, the instance state, the public address
or - when it has none, and apex on the one space that holds the root domain or
- on every other. The lines are sorted by domain, and no spaces prints nothing.

The cloud's tags are the only registry: a space is an instance tagged with the
root domain and a Space tag naming its own. The holder of the root domain is
the space whose Elastic IP the root's A record points at; no host is asked.
What a space is running is 'devctl space status'.
`

const expectedD06DestroyUsage = `Usage: devctl space destroy <space> [--no-backup] [--delete-secrets] [--delete-backups]

Delete the root domain's record first when this space holds it, have the host
take its final backup with opsctl retire, then remove the instance, Elastic IP,
records and role. Secrets and backups are kept unless an option says otherwise.
Run again to finish a partial destroy.

Options:
  --no-backup        skip the final backup the host takes before it goes
  --delete-secrets   delete the space's secrets; they are kept otherwise
  --delete-backups   delete the space's backups; they are kept otherwise
`

const expectedD06StopUsage = `Usage: devctl space stop <space>

Stop the instance and keep its disk, Elastic IP, records, secrets and backups.
If the space holds the root domain, the root keeps pointing at it.
`

const expectedD06StartUsage = `Usage: devctl space start <space>

Start the instance at its existing Elastic IP, wait for status checks and SSH,
then run certbot renew. Records are unchanged; the last line is domain and address.
`

const expectedD06StatusUsage = `Usage: devctl space status <space>

Relay opsctl status from the running host: app, version, service state, socket
state and database journal mode. A host with no apps prints nothing.
`

func TestRunReturnsWithoutTerminatingCaller(t *testing.T) {
	// R-U72C-BSYD
	file, err := parser.ParseFile(token.NewFileSet(), "run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, isIdentifier := selector.X.(*ast.Ident)
		if isIdentifier && identifier.Name == "os" && selector.Sel.Name == "Exit" {
			t.Error("Run calls os.Exit instead of returning")
		}
		return true
	})

	result := invoke("--help")
	if result.code != 0 {
		t.Fatalf("Run returned %d, want 0", result.code)
	}
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
}

func TestTopLevelGrammar(t *testing.T) {
	// R-OFH7-TLNF
	for _, args := range [][]string{
		{"version"}, {"-V"}, {"--version"}, {"-h"}, {"--help"},
	} {
		if got := invoke(args...).code; got != 0 {
			t.Errorf("Run(%q) = %d, want 0", args, got)
		}
	}
	for _, option := range []string{"-x", "--verbose", "--", "-version", "--account"} {
		result := invoke(option)
		if result.code != 2 || !strings.HasPrefix(result.stderr, "devctl: unknown option '") {
			t.Errorf("Run(%q) = (%d, %q), want unknown-option usage error", option, result.code, result.stderr)
		}
	}
	assertResult(t, invoke("--account", "ikigenba.dev", "version"), 2, "", "devctl: unknown option '--account'\n\nsee 'devctl --help' for usage\n")
}

func TestArgumentsAfterCommandAreCommandArguments(t *testing.T) {
	// R-OHX0-L54T
	assertResult(t, invoke("version", "--help"), 0, "Usage: devctl version\n\nPrint the version.\n", "")
	assertResult(t, invoke("version", "--bogus"), 2, "", "devctl: version takes no arguments\n\nsee 'devctl version --help' for usage\n")
}

func TestNoCommand(t *testing.T) {
	// R-D82W-E67E
	assertResult(t, invoke(), 2, "", "devctl: no command given\n\nsee 'devctl --help' for usage\n")
}

func TestUnknownCommand(t *testing.T) {
	// R-D9AS-RXY3
	assertResult(t, invoke("frobnicate"), 2, "", "devctl: unknown command 'frobnicate'\n\nsee 'devctl --help' for usage\n")
}

func TestUnknownTopLevelOption(t *testing.T) {
	// R-DAIP-5POS
	assertResult(t, invoke("--frobnicate"), 2, "", "devctl: unknown option '--frobnicate'\n\nsee 'devctl --help' for usage\n")
}

func TestRootFileSelectsCloudProfileAndRegion(t *testing.T) {
	// R-OO0I-HZUA R-N1LD-IX1I
	tests := []struct {
		name    string
		args    []string
		prepare func(*testing.T, seam.Deps) seam.Deps
	}{
		{name: "space", args: []string{"space", "list"}},
		{name: "secrets", args: []string{"secrets", "list", "sbx1"}},
		{name: "deploy", args: []string{"deploy", "sbx1", "crm-v1.2.3.tar.xz"}, prepare: prepareCLIArchive},
		{name: "restore", args: []string{"restore", "sbx1", "crm"}},
		{name: "remove", args: []string{"remove", "sbx1", "crm"}},
		{name: "apex", args: []string{"apex", "show"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := checkoutDeps(t, `{"domain":"example.test","region":"eu-west-1"}`)
			if test.prepare != nil {
				deps = test.prepare(t, deps)
			}
			var calls []cloudOpenCall
			deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
				calls = append(calls, cloudOpenCall{profile: profile, region: region})
				return cliClients(&cliEC2{}, &cliRoute53{err: errors.New("cloud progress complete")}), nil
			}
			result := invokeWithDeps(deps, test.args...)
			if result.code != 1 {
				t.Fatalf("Run(%q) = %#v, want operation failure after cloud connection", test.args, result)
			}
			if len(calls) != 1 || calls[0] != (cloudOpenCall{profile: "example.test", region: "eu-west-1"}) {
				t.Fatalf("cloud calls = %#v, want one root-file profile and region", calls)
			}
		})
	}

	failures := []struct {
		name       string
		args       []string
		prepare    func(*testing.T, seam.Deps) seam.Deps
		rootFile   string
		wantStdout string
		wantStderr string
	}{
		{name: "space missing", args: []string{"space", "list"}, wantStderr: "devctl: no infra/terraform.tfvars.json in the checkout\n"},
		{name: "space invalid", args: []string{"space", "list"}, rootFile: `{}`, wantStderr: "devctl: infra/terraform.tfvars.json: missing 'domain'\n"},
		{name: "secrets missing", args: []string{"secrets", "list", "sbx1"}, wantStderr: "devctl: no infra/terraform.tfvars.json in the checkout\n"},
		{name: "secrets invalid", args: []string{"secrets", "list", "sbx1"}, rootFile: `{}`, wantStderr: "devctl: infra/terraform.tfvars.json: missing 'domain'\n"},
		{name: "deploy missing", args: []string{"deploy", "sbx1", "crm-v1.2.3.tar.xz"}, prepare: prepareCLIArchive, wantStdout: "file: ok (crm v1.2.3)\n", wantStderr: "devctl: no infra/terraform.tfvars.json in the checkout\n"},
		{name: "deploy invalid", args: []string{"deploy", "sbx1", "crm-v1.2.3.tar.xz"}, prepare: prepareCLIArchive, rootFile: `{}`, wantStdout: "file: ok (crm v1.2.3)\n", wantStderr: "devctl: infra/terraform.tfvars.json: missing 'domain'\n"},
		{name: "restore missing", args: []string{"restore", "sbx1", "crm"}, wantStderr: "devctl: no infra/terraform.tfvars.json in the checkout\n"},
		{name: "restore invalid", args: []string{"restore", "sbx1", "crm"}, rootFile: `{}`, wantStderr: "devctl: infra/terraform.tfvars.json: missing 'domain'\n"},
		{name: "remove missing", args: []string{"remove", "sbx1", "crm"}, wantStderr: "devctl: no infra/terraform.tfvars.json in the checkout\n"},
		{name: "remove invalid", args: []string{"remove", "sbx1", "crm"}, rootFile: `{}`, wantStderr: "devctl: infra/terraform.tfvars.json: missing 'domain'\n"},
		{name: "apex missing", args: []string{"apex", "show"}, wantStderr: "devctl: no infra/terraform.tfvars.json in the checkout\n"},
		{name: "apex invalid", args: []string{"apex", "show"}, rootFile: `{}`, wantStderr: "devctl: infra/terraform.tfvars.json: missing 'domain'\n"},
	}
	for _, test := range failures {
		t.Run(test.name, func(t *testing.T) {
			var deps seam.Deps
			if test.rootFile == "" {
				root := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "work"), 0o700); err != nil {
					t.Fatal(err)
				}
				deps = checkoutDepsAt(root)
			} else {
				deps = checkoutDeps(t, test.rootFile)
			}
			if test.prepare != nil {
				deps = test.prepare(t, deps)
			}
			cloudCalls := 0
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				cloudCalls++
				return cloud.Clients{}, errors.New("unexpected cloud call")
			}
			assertResult(t, invokeWithDeps(deps, test.args...), 2, test.wantStdout, test.wantStderr)
			if cloudCalls != 0 {
				t.Fatalf("root-file failure made %d cloud calls", cloudCalls)
			}
		})
	}
}

func TestEveryCloudCommandStopsAtMissingRootFile(t *testing.T) {
	// R-N1LD-IX1I
	tests := []struct {
		name    string
		args    []string
		prepare func(*testing.T, seam.Deps) seam.Deps
	}{
		{name: "space list", args: []string{"space", "list"}},
		{name: "space create", args: []string{"space", "create", "sbx1", "--acme-email", "ops@example.test"}},
		{name: "space destroy", args: []string{"space", "destroy", "sbx1"}},
		{name: "space stop", args: []string{"space", "stop", "sbx1"}},
		{name: "space start", args: []string{"space", "start", "sbx1"}},
		{name: "space init", args: []string{"space", "init", "sbx1"}},
		{name: "space status", args: []string{"space", "status", "sbx1"}},
		{name: "space restart", args: []string{"space", "restart", "sbx1", "crm"}},
		{name: "space logs", args: []string{"space", "logs", "sbx1", "crm"}},
		{name: "secrets push", args: []string{"secrets", "push", "sbx1"}},
		{name: "secrets list", args: []string{"secrets", "list", "sbx1"}},
		{name: "deploy", args: []string{"deploy", "sbx1", "crm-v1.2.3.tar.xz"}, prepare: prepareCLIArchive},
		{name: "restore", args: []string{"restore", "sbx1", "crm"}},
		{name: "remove", args: []string{"remove", "sbx1", "crm"}},
		{name: "apex set", args: []string{"apex", "set", "crm.sbx1"}},
		{name: "apex show", args: []string{"apex", "show"}},
		{name: "apex clear", args: []string{"apex", "clear"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "work"), 0o700); err != nil {
				t.Fatal(err)
			}
			deps := checkoutDepsAt(root)
			if test.prepare != nil {
				deps = test.prepare(t, deps)
			}
			cloudCalls := 0
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				cloudCalls++
				return cloud.Clients{}, errors.New("unexpected cloud call")
			}
			result := invokeWithDeps(deps, test.args...)
			if result.code != 2 || !strings.Contains(result.stderr, "no infra/terraform.tfvars.json in the checkout") || cloudCalls != 0 {
				t.Fatalf("Run(%q) = %#v, cloud calls %d", test.args, result, cloudCalls)
			}
		})
	}
}

func TestEverySpaceOperandUsesSharedGrammarBeforeCloud(t *testing.T) {
	// R-QW0J-KUFF
	tests := []struct {
		name    string
		args    []string
		prepare func(*testing.T, seam.Deps) seam.Deps
	}{
		{name: "space create", args: []string{"space", "create", "crm.sbx1", "--acme-email", "ops@example.test"}},
		{name: "space destroy", args: []string{"space", "destroy", "crm.sbx1"}},
		{name: "space stop", args: []string{"space", "stop", "crm.sbx1"}},
		{name: "space start", args: []string{"space", "start", "crm.sbx1"}},
		{name: "space init", args: []string{"space", "init", "crm.sbx1"}},
		{name: "space status", args: []string{"space", "status", "crm.sbx1"}},
		{name: "space restart", args: []string{"space", "restart", "crm.sbx1", "crm"}},
		{name: "space logs", args: []string{"space", "logs", "crm.sbx1", "crm"}},
		{name: "secrets push", args: []string{"secrets", "push", "crm.sbx1"}},
		{name: "secrets list", args: []string{"secrets", "list", "crm.sbx1"}},
		{name: "deploy", args: []string{"deploy", "crm.sbx1", "crm-v1.2.3.tar.xz"}, prepare: prepareCLIArchive},
		{name: "restore", args: []string{"restore", "crm.sbx1", "crm"}},
		{name: "remove", args: []string{"remove", "crm.sbx1", "crm"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
			if test.prepare != nil {
				deps = test.prepare(t, deps)
			}
			cloudCalls := 0
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				cloudCalls++
				return cloud.Clients{}, errors.New("unexpected cloud call")
			}
			result := invokeWithDeps(deps, test.args...)
			want := "devctl: 'crm.sbx1' is not a space: a space is one label under 'ikigenba.dev'\n"
			wantStdout := ""
			if test.name == "deploy" {
				wantStdout = "file: ok (crm v1.2.3)\n"
			}
			if result.code != 2 || result.stdout != wantStdout || result.stderr != want || cloudCalls != 0 {
				t.Fatalf("Run(%q) = %#v, cloud calls %d", test.args, result, cloudCalls)
			}
		})
	}
}

func TestCloudErrorsAreSingleLineOperationFailures(t *testing.T) {
	// R-QZSL-ZMJL
	tests := []struct {
		name       string
		args       []string
		clients    cloud.Clients
		wantStdout string
		wantStderr string
	}{
		{
			name: "STS Connect", args: []string{"space", "list"},
			clients: cliClientsWithSTS(&cliSTS{err: &cloud.Error{
				Service: "sts", Operation: "GetCallerIdentity", Err: errors.New("<m>"),
			}}),
			wantStderr: "devctl: sts GetCallerIdentity: <m>\n",
		},
		{
			name: "EC2 RunInstances", args: []string{"space", "create", "sbx1", "--acme-email", "ops@ikigenba.dev"},
			clients: createCLIClients(&cliEC2{runErr: &cloud.Error{
				Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity",
			}}, &cliRoute53{}),
			wantStdout: "account: ok (ikigenba.dev, us-east-2, 123456789012)\n" +
				"domain: ok (zone ikigenba.dev ZROOT)\n" +
				"secrets: ok (0 apps)\n" +
				"role: ok (sbx1.ikigenba.dev)\n",
			wantStderr: "devctl: ec2 RunInstances: InsufficientInstanceCapacity\n",
		},
		{
			name: "Route53 ChangeResourceRecordSets", args: []string{"apex", "set", "crm.sbx1"},
			clients: cliClients(
				&cliEC2{instances: []cloud.Instance{{ID: "i-one", Space: "sbx1.ikigenba.dev", State: cloud.StateRunning, Address: "192.0.2.10"}}},
				&cliRoute53{changeErr: &cloud.Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"}},
			),
			wantStdout: "space: ok (sbx1.ikigenba.dev running, 192.0.2.10)\n" +
				"role: ok (sbx1.ikigenba.dev may prove ikigenba.dev)\n" +
				"host: ok (host.apex=crm, certificate obtained, nginx applied)\n",
			wantStderr: "devctl: route53 ChangeResourceRecordSets: Throttling\n",
		},
		{
			name: "launch template NotFound", args: []string{"space", "create", "sbx1", "--acme-email", "ops@ikigenba.dev"},
			clients: createCLIClients(&cliEC2{launchErr: &cloud.NotFoundError{
				Kind: "launch template", Name: "ikigenba.dev",
			}}, &cliRoute53{}),
			wantStderr: "devctl: no launch template 'ikigenba.dev'\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
			deps.Exec = cliCheckoutAndHostExec(t, deps.Exec)
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) { return test.clients, nil }
			assertResult(t, invokeWithDeps(deps, test.args...), 1, test.wantStdout, test.wantStderr)
		})
	}
}

func TestCommandsReportMissingSpace(t *testing.T) {
	// R-R10I-DEAA
	tests := []struct {
		name    string
		args    []string
		prepare func(*testing.T, seam.Deps) seam.Deps
	}{
		{name: "space stop", args: []string{"space", "stop", "gone"}},
		{name: "space start", args: []string{"space", "start", "gone"}},
		{name: "space status", args: []string{"space", "status", "gone"}},
		{name: "space init", args: []string{"space", "init", "gone"}},
		{name: "space restart", args: []string{"space", "restart", "gone", "crm"}},
		{name: "space logs", args: []string{"space", "logs", "gone", "crm"}},
		{name: "secrets push", args: []string{"secrets", "push", "gone", "crm"}, prepare: prepareCLIApp},
		{name: "deploy", args: []string{"deploy", "gone", "crm-v1.2.3.tar.xz"}, prepare: prepareCLIArchive},
		{name: "remove", args: []string{"remove", "gone", "crm"}},
		{name: "restore", args: []string{"restore", "gone", "crm"}},
		{name: "apex set", args: []string{"apex", "set", "crm.gone"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := checkoutDeps(t, `{"domain":"example.test","region":"eu-west-1"}`)
			if test.prepare != nil {
				deps = test.prepare(t, deps)
			}
			ec2 := &cliEC2{}
			deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
				return cliClients(ec2, &cliRoute53{}), nil
			}
			wantStdout := ""
			if test.name == "deploy" {
				wantStdout = "file: ok (crm v1.2.3)\n"
			}
			assertResult(t, invokeWithDeps(deps, test.args...), 1, wantStdout, "devctl: no space at 'gone.example.test'\n")
			if !reflect.DeepEqual(ec2.listDomains, []string{"example.test"}) {
				t.Fatalf("EC2 ListSpaceInstances domains = %q, want LookupSpace through example.test", ec2.listDomains)
			}
		})
	}
}

func TestRootRefusalPrecedesEveryInvocation(t *testing.T) {
	// R-OJ4W-YWVI
	invocations := [][]string{
		nil,
		{"--help"}, {"-h"}, {"--version"}, {"-V"}, {"version"},
		{"unknown"}, {"--unknown"}, {"--account"},
		{"version", "--help"},
		{"space", "--help"},
		{"secrets", "--help"},
		{"build", "--help"},
		{"deploy", "--help"},
		{"restore", "--help"},
		{"remove", "--help"},
		{"apex", "--help"},
		{"space", "list"},
		{"secrets", "list", "sbx1"},
		{"build", "crm"},
		{"deploy", "sbx1", "crm/dist/crm-v1.0.0.tar.xz"},
		{"restore", "sbx1", "crm"},
		{"remove", "sbx1", "crm"},
		{"apex", "show"},
	}
	for _, args := range invocations {
		external := 0
		deps := noExternalDeps(&external)
		deps.EUID = 0
		deps.Dir = filepath.Join(t.TempDir(), "outside")
		assertResult(t, invokeWithDeps(deps, args...), 3, "", "devctl: must not run as root\n")
		if external != 0 {
			t.Errorf("Run(%q) made %d external calls", args, external)
		}
	}
}

func TestTopLevelNonCommandsDoNotTouchExternalDependencies(t *testing.T) {
	// R-OP8E-VRKZ
	invocations := [][]string{
		{"--help"}, {"-h"}, {"--version"}, {"-V"}, {"version"},
		nil, {"unknown"}, {"--unknown"},
	}
	for _, args := range invocations {
		external := 0
		deps := noExternalDeps(&external)
		deps.Dir = filepath.Join(t.TempDir(), "outside")
		_ = invokeWithDeps(deps, args...)
		if external != 0 {
			t.Errorf("Run(%q) made %d external calls", args, external)
		}
	}
}

func TestVersionDeclaration(t *testing.T) {
	// R-DFEA-OSNK
	if !regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(version) {
		t.Fatalf("version = %q, want a stable semantic version", version)
	}
	declaration := findVersionDeclaration(t)
	if declaration == nil {
		t.Fatal("run.go has no package-level var version")
	}
}

func TestVersionOutput(t *testing.T) {
	// R-DGM7-2KE9
	for _, args := range [][]string{{"version"}, {"-V"}, {"--version"}} {
		assertResult(t, invoke(args...), 0, version+"\n", "")
	}
}

func TestVersionHelp(t *testing.T) {
	// R-T9YB-VTNZ
	for _, option := range []string{"--help", "-h"} {
		assertResult(t, invoke("version", option), 0, "Usage: devctl version\n\nPrint the version.\n", "")
	}
}

func TestVersionRejectsArguments(t *testing.T) {
	// R-DJ1Z-U3VN
	for _, args := range [][]string{{"extra"}, {"--account", "work"}, {"-V"}} {
		assertResult(t, invoke(append([]string{"version"}, args...)...), 2, "", "devctl: version takes no arguments\n\nsee 'devctl version --help' for usage\n")
	}
}

func TestTopLevelCommandSet(t *testing.T) {
	// R-OKCT-COM7
	got := make([]string, 0, len(commandSet))
	for command := range commandSet {
		got = append(got, command)
	}
	sort.Strings(got)
	want := []string{"apex", "build", "deploy", "remove", "restore", "secrets", "space", "version"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level commands = %q, want %q", got, want)
	}
	for _, command := range got {
		if result := invoke(command); strings.Contains(result.stderr, "unknown command") {
			t.Errorf("Run(%q) rejected a command in the command set", command)
		}
	}
}

func TestTopLevelHelp(t *testing.T) {
	// R-OMSM-483L
	for _, option := range []string{"--help", "-h"} {
		assertResult(t, invoke(option), 0, expectedUsage, "")
	}
}

func TestSecretsHelpThroughCLI(t *testing.T) {
	for _, args := range [][]string{{"secrets", "--help"}, {"secrets", "-h"}} {
		assertResult(t, invoke(args...), 0, expectedD05Usage, "")
	}
	// R-0EH5-TETS

	for _, option := range []string{"--help", "-h"} {
		assertResult(t, invoke("secrets", "push", option), 0, expectedD05PushUsage, "")
	}
	// R-0FP2-76KH

	for _, option := range []string{"--help", "-h"} {
		assertResult(t, invoke("secrets", "list", option), 0, expectedD05ListUsage, "")
	}
	// R-0GWY-KYB6
}

func TestSpaceHelpThroughCLIWithoutCheckoutOrEffects(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    string
	}{
		// R-JBGS-JDPU
		{name: "space", command: []string{"space"}, want: expectedD06Usage},
		// R-UPD6-TD4G
		{name: "list", command: []string{"space", "list"}, want: expectedD06ListUsage},
		// R-UQL3-74V5
		{name: "destroy", command: []string{"space", "destroy"}, want: expectedD06DestroyUsage},
		// R-URSZ-KWLU
		{name: "stop", command: []string{"space", "stop"}, want: expectedD06StopUsage},
		// R-UT0V-YOCJ
		{name: "start", command: []string{"space", "start"}, want: expectedD06StartUsage},
		// R-JCOO-X5GJ
		{name: "status", command: []string{"space", "status"}, want: expectedD06StatusUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, option := range []string{"--help", "-h"} {
				t.Run(option, func(t *testing.T) {
					cloudCalls := 0
					execCalls := 0
					args := append(append([]string{}, test.command...), option)
					result := invokeWithDeps(seam.Deps{
						EUID: 1,
						Dir:  filepath.Join(t.TempDir(), "outside-checkout"),
						Cloud: func(context.Context, string, string) (cloud.Clients, error) {
							cloudCalls++
							return cloud.Clients{}, nil
						},
						Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
							execCalls++
							return seam.Result{}, errors.New("unexpected exec")
						},
					}, args...)
					assertResult(t, result, 0, test.want, "")
					if cloudCalls != 0 || execCalls != 0 {
						t.Fatalf("Run(%q) effects: Cloud=%d Exec=%d", args, cloudCalls, execCalls)
					}
				})
			}
		})
	}
}

func TestSpaceNeedsSubcommandBeforeEffects(t *testing.T) {
	// R-UVGO-Q7TX
	assertSpaceUsageBeforeEffects(t, []string{"space"}, "space needs <subcommand>")
}

func TestSpaceCommandsNeedSpaceBeforeEffects(t *testing.T) {
	// R-UWOL-3ZKM
	for _, subcommand := range []string{"destroy", "stop", "start", "status"} {
		t.Run(subcommand, func(t *testing.T) {
			assertSpaceUsageBeforeEffects(t, []string{"space", subcommand}, "space "+subcommand+" needs <space>")
		})
	}
}

func TestSpaceRejectsExtraOperandsBeforeEffects(t *testing.T) {
	// R-UZ4D-VJ20
	for _, subcommand := range []string{"destroy", "stop", "start", "status"} {
		t.Run(subcommand, func(t *testing.T) {
			assertSpaceUsageBeforeEffects(t, []string{"space", subcommand, "sbx1", "extra"}, "space "+subcommand+" takes only <space>")
		})
	}
	assertSpaceUsageBeforeEffects(t, []string{"space", "list", "extra"}, "space list takes no arguments")
}

func TestSpaceRejectsUnknownOptionsBeforeEffects(t *testing.T) {
	// R-JHKA-G8FB
	tests := [][]string{
		{"space", "--wat"},
		{"space", "--help", "--wat"},
		{"space", "list", "--wat"},
		{"space", "stop", "sbx1", "--no-backup"},
		{"space", "stop", "sbx1", "--no-backup", "--help"},
		{"space", "destroy", "sbx1", "--no-backup=true"},
		{"space", "destroy", "--delete-secrets=yes", "sbx1"},
	}
	for _, args := range tests {
		option := args[len(args)-1]
		for _, argument := range args {
			if strings.HasPrefix(argument, "-") && argument != "--help" && argument != "-h" {
				option = argument
				break
			}
		}
		t.Run(strings.Join(args[1:], "_"), func(t *testing.T) {
			assertSpaceUsageBeforeEffects(t, args, "unknown option '"+option+"'")
		})
	}
}

func assertSpaceUsageBeforeEffects(t *testing.T, args []string, message string) {
	t.Helper()
	cloudCalls := 0
	execCalls := 0
	deps := seam.Deps{
		EUID: 1,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, nil
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			execCalls++
			return seam.Result{}, errors.New("unexpected exec")
		},
	}
	wantStderr := "devctl: " + message + "\n\nsee 'devctl space --help' for usage\n"
	assertResult(t, invokeWithDeps(deps, args...), 2, "", wantStderr)
	if cloudCalls != 0 || execCalls != 0 {
		t.Fatalf("Run(%q) effects: Cloud=%d Exec=%d", args, cloudCalls, execCalls)
	}
}

func TestNoZoneErrorIsSingleLineOperationFailure(t *testing.T) {
	var stderr bytes.Buffer
	err := fmt.Errorf("wrapped: %w", &cloud.NotFoundError{Kind: "hosted zone", Name: "foo.example"})
	if got := operationError(&stderr, err); got != 1 {
		t.Fatalf("operationError exit = %d, want 1", got)
	}
	if got, want := stderr.String(), "devctl: no hosted zone 'foo.example'\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestSecretsUsageErrorsThroughCLI(t *testing.T) {
	want := func(message string) string {
		return "devctl: " + message + "\n\nsee 'devctl secrets --help' for usage\n"
	}

	assertResult(t, invoke("secrets"), 2, "", want("secrets needs <subcommand>"))
	// R-0JCR-CHSK

	assertResult(t, invoke("secrets", "frobnicate"), 2, "", want("unknown subcommand 'frobnicate'"))
	// R-G53K-0HYH

	for _, subcommand := range []string{"push", "list"} {
		assertResult(t, invoke("secrets", subcommand), 2, "", want("secrets "+subcommand+" needs <space>"))
	}
	// R-0KKN-Q9J9

	for _, subcommand := range []string{"push", "list"} {
		assertResult(t, invoke("secrets", subcommand, "sbx1", "crm", "extra"), 2, "", want("secrets "+subcommand+" takes at most <space> and <app>"))
	}
	// R-0LSK-419Y

	for _, args := range [][]string{
		{"secrets", "--verbose"},
		{"secrets", "push", "--verbose"},
		{"secrets", "push", "example.test", "--verbose"},
		{"secrets", "list", "example.test", "crm", "--verbose"},
	} {
		assertResult(t, invoke(args...), 2, "", want("unknown option '--verbose'"))
	}
	// R-G9Z5-JKX9
}

type sentinelSSM struct {
	cloud.SSM
	getValue   string
	parameters []cloud.Parameter
}

func (ssm *sentinelSSM) GetParameter(context.Context, string) (string, error) {
	return ssm.getValue, nil
}

func (*sentinelSSM) PutSecureParameter(context.Context, string, string) error { return nil }

func (ssm *sentinelSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return ssm.parameters, nil
}

type sentinelEC2 struct{ cloud.EC2 }

func (*sentinelEC2) ListSpaceInstances(context.Context, string) ([]cloud.Instance, error) {
	return []cloud.Instance{
		{Space: "foo.sbx.ikigenba.dev", State: cloud.StateRunning},
		{Space: "sbx1.ikigenba.dev", State: cloud.StateRunning},
	}, nil
}

type cliSTS struct {
	cloud.STS
	err error
}

func (fake *cliSTS) CallerAccountID(context.Context) (string, error) {
	return "123456789012", fake.err
}

func TestSecretsNeverExposeValuesThroughCLIOrExports(t *testing.T) {
	// R-GS9N-A51O
	const (
		pushSentinel  = "push-value-sentinel-7fdf"
		listSentinel  = "list-value-sentinel-a9c2"
		namesSentinel = "names-value-sentinel-e431"
	)
	root := t.TempDir()
	appDir := filepath.Join(root, "crm")
	if err := os.MkdirAll(filepath.Join(appDir, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(appDir, "cmd", "crm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "cmd", "crm", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "etc", "manifest.toml"), []byte("app = 'crm'\nsecrets = ['CRM_TOKEN']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checkout.RootFilePath), []byte(`{"domain":"sbx.ikigenba.dev","region":"us-test-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	regional := &sentinelSSM{
		getValue: namesObject(namesSentinel),
		parameters: []cloud.Parameter{{
			Name:  secrets.Parameter("foo.sbx.ikigenba.dev", "crm"),
			Value: namesObject(listSentinel),
		}},
	}
	deps := secretSentinelDeps(root, pushSentinel, regional)
	pushResult := invokeWithDeps(deps, "secrets", "push", "foo", "crm")
	assertResult(t, pushResult, 0, "crm: ok (1 keys)\n", "")
	listResult := invokeWithDeps(deps, "secrets", "list", "foo")
	assertResult(t, listResult, 0, "crm CRM_TOKEN\n", "")

	visible := pushResult.stdout + pushResult.stderr + listResult.stdout + listResult.stderr
	for _, sentinel := range []string{pushSentinel, listSentinel, namesSentinel} {
		if strings.Contains(visible, sentinel) {
			t.Fatalf("secret sentinel %q exposed in %q", sentinel, visible)
		}
	}
}

func namesObject(value string) string { return `{"CRM_TOKEN":"` + value + `"}` }

func secretSentinelDeps(root, value string, regional cloud.SSM) seam.Deps {
	return seam.Deps{
		EUID:   1,
		Dir:    root,
		Getenv: func(string) string { return value },
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			return cloud.Clients{STS: &cliSTS{}, SSM: regional, EC2: &sentinelEC2{}}, nil
		},
	}
}

func TestDiagnosticStreams(t *testing.T) {
	// R-DMPO-ZF3Q
	successes := [][]string{
		{"--help"}, {"--version"}, {"version"}, {"version", "--help"}, {"build", "--help"},
	}
	for _, args := range successes {
		result := invoke(args...)
		if result.code != 0 || result.stderr != "" {
			t.Errorf("Run(%q) = code %d, stderr %q; want successful empty stderr", args, result.code, result.stderr)
		}
	}

	failures := [][]string{
		nil, {"unknown"}, {"--unknown"}, {"version", "argument"}, {"space"},
	}
	for _, args := range failures {
		result := invoke(args...)
		if !strings.HasPrefix(result.stderr, "devctl: ") {
			t.Errorf("Run(%q) stderr = %q, want devctl prefix", args, result.stderr)
		}
		if strings.Contains(result.stderr, "Usage:") {
			t.Errorf("Run(%q) wrote usage text to stderr: %q", args, result.stderr)
		}
		if result.stdout != "" {
			t.Errorf("Run(%q) duplicated diagnostic as stdout %q", args, result.stdout)
		}
	}
}

func TestCLIAloneWritesCheckoutDiagnostics(t *testing.T) {
	// R-OQGB-9JBO
	for _, commandPackage := range []string{
		"space", "spacecreate", "spaceinit", "spaceapps", "secrets",
		"build", "deploy", "restore", "remove", "apex",
	} {
		directory := filepath.Join("..", commandPackage)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read internal/%s: %v", commandPackage, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil, 0)
			if err != nil {
				t.Fatalf("parse internal/%s/%s: %v", commandPackage, entry.Name(), err)
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv != nil || function.Name.Name != "Run" {
					continue
				}
				writers := 0
				for _, field := range function.Type.Params.List {
					selector, ok := field.Type.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					qualifier, qualified := selector.X.(*ast.Ident)
					if !qualified || qualifier.Name != "io" || selector.Sel.Name != "Writer" {
						continue
					}
					writers += len(field.Names)
					if len(field.Names) != 1 || field.Names[0].Name != "stdout" {
						t.Errorf("internal/%s.Run writer parameter is not exactly stdout", commandPackage)
					}
				}
				if writers != 1 {
					t.Errorf("internal/%s.Run has %d io.Writer parameters, want 1", commandPackage, writers)
				}
			}
		}
	}

	outside := filepath.Join(t.TempDir(), "outside")
	assertResult(t, invokeWithDeps(seam.Deps{
		EUID: 1,
		Dir:  outside,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			return seam.Result{ExitCode: 128}, nil
		},
	}, "space", "list"), 2, "", "devctl: '"+outside+"' is not inside a git checkout\n")

	missingRoot := t.TempDir()
	assertResult(t, invokeWithDeps(checkoutDepsAt(missingRoot), "space", "list"), 2, "", "devctl: no infra/terraform.tfvars.json in the checkout\n")

	malformed := t.TempDir()
	if err := os.MkdirAll(filepath.Join(malformed, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformed, checkout.RootFilePath), []byte(`{"domain":"ikigenba.dev"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertResult(t, invokeWithDeps(checkoutDepsAt(malformed), "space", "list"), 2, "", "devctl: infra/terraform.tfvars.json: missing 'region'\n")
}

func TestDiagnosticDetail(t *testing.T) {
	// R-C52Z-FBPW
	var stderr bytes.Buffer
	writeDiagnostic(&stderr, "operation failed", "outer\n\n> inner\n\n", "retry with --force", false)
	want := "devctl: operation failed\n\n> outer\n> \n> > inner\nretry with --force\n"
	if got := stderr.String(); got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}

	var stdout bytes.Buffer
	stderr.Reset()
	report := "completed items\nfailed items\n"
	_, _ = io.WriteString(&stdout, report)
	writeDiagnostic(&stderr, "some items failed", report, "retry the failed items", true)
	if got := stdout.String(); got != report {
		t.Fatalf("stdout = %q, want delivered report %q", got, report)
	}
	want = "devctl: some items failed\n\nretry the failed items\n"
	if got := stderr.String(); got != want {
		t.Fatalf("diagnostic after stdout report = %q, want %q", got, want)
	}
}

func TestHostCommandDetailIsNotQuotedAgain(t *testing.T) {
	var stderr bytes.Buffer
	err := &host.CommandError{
		Step: "retire", Command: []string{"ssh", "ec2-user@18.220.10.5", "sudo", "opsctl", "retire"},
		Status: 1, Stdout: "a\nb\n", Stderr: "c\n",
	}
	if got := operationError(&stderr, err); got != 1 {
		t.Fatalf("operationError exit = %d, want 1", got)
	}
	want := "devctl: retire: ssh ec2-user@18.220.10.5 sudo opsctl retire: exit status 1\n\n> a\n> b\n> c\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestVersionIsInitializedInSource(t *testing.T) {
	// R-GV4C-IOFR
	declaration := findVersionDeclaration(t)
	if declaration == nil || len(declaration.Values) != 1 {
		t.Fatal("version has no source initializer")
	}
	literal, ok := declaration.Values[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		t.Fatal("version is not initialized directly from source text")
	}
	sourceVersion, err := strconv.Unquote(literal.Value)
	if err != nil {
		t.Fatalf("parse version initializer: %v", err)
	}
	if version != sourceVersion {
		t.Fatalf("built version = %q, source version = %q", version, sourceVersion)
	}
}

func findVersionDeclaration(t *testing.T) *ast.ValueSpec {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			if len(value.Names) == 1 && value.Names[0].Name == "version" {
				return value
			}
		}
	}
	return nil
}

type runResult struct {
	code           int
	stdout, stderr string
}

type cliSSM struct {
	cloud.SSM
	value string
	err   error
}

func (fake *cliSSM) GetParameter(context.Context, string) (string, error) {
	return fake.value, fake.err
}

func (fake *cliSSM) ListParameters(context.Context, string) ([]cloud.Parameter, error) {
	return nil, fake.err
}

type cliEC2 struct {
	cloud.EC2
	instances   []cloud.Instance
	listDomains []string
	err         error
	launchErr   error
	runErr      error
}

func (fake *cliEC2) ListSpaceInstances(_ context.Context, domain string) ([]cloud.Instance, error) {
	fake.listDomains = append(fake.listDomains, domain)
	return fake.instances, fake.err
}

func (fake *cliEC2) LaunchTemplate(context.Context, string) (string, error) {
	return "lt-root", fake.launchErr
}

func (*cliEC2) LaunchReady(context.Context, cloud.LaunchSpec) (bool, error) { return true, nil }

func (fake *cliEC2) RunInstance(context.Context, cloud.LaunchSpec) (cloud.Instance, error) {
	return cloud.Instance{}, fake.runErr
}

type cliRoute53 struct {
	cloud.Route53
	err       error
	changeErr error
}

func (fake *cliRoute53) Zone(context.Context, string) (cloud.Zone, error) {
	return cloud.Zone{ID: "ZROOT", Name: "ikigenba.dev"}, fake.err
}

func (*cliRoute53) FindRecord(context.Context, string, string, string) (cloud.Record, bool, error) {
	return cloud.Record{}, false, nil
}

func (fake *cliRoute53) ChangeRecords(context.Context, string, []cloud.RecordChange) (string, error) {
	return "", fake.changeErr
}

type cliIAM struct{ cloud.IAM }

func (*cliIAM) PermissionsBoundary(context.Context, string) (string, error) {
	return "arn:boundary", nil
}
func (*cliIAM) RoleExists(context.Context, string) (bool, error) { return false, nil }
func (*cliIAM) InstanceProfileRoles(context.Context, string) ([]string, bool, error) {
	return nil, false, nil
}
func (*cliIAM) CreateRole(context.Context, cloud.RoleSpec) error               { return nil }
func (*cliIAM) PutRolePolicy(context.Context, string, string, string) error    { return nil }
func (*cliIAM) CreateInstanceProfile(context.Context, string) error            { return nil }
func (*cliIAM) AddRoleToInstanceProfile(context.Context, string, string) error { return nil }

type cloudOpenCall struct {
	profile string
	region  string
}

func cliClients(ec2 cloud.EC2, route53 cloud.Route53) cloud.Clients {
	return cloud.Clients{
		EC2: ec2, SSM: &cliSSM{err: errors.New("cloud progress complete")}, Route53: route53,
		S3: nil, IAM: &cliIAM{}, STS: &cliSTS{},
	}
}

func cliClientsWithSTS(sts cloud.STS) cloud.Clients {
	clients := cliClients(&cliEC2{}, &cliRoute53{})
	clients.STS = sts
	return clients
}

func createCLIClients(ec2 *cliEC2, route53 *cliRoute53) cloud.Clients {
	return cliClients(ec2, route53)
}

func invoke(args ...string) runResult {
	return invokeWithDeps(seam.Deps{EUID: 1}, args...)
}

func invokeWithDeps(deps seam.Deps, args ...string) runResult {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr, deps)
	// R-A2XG-SZR2
	if code < 0 || code > 3 {
		panic(fmt.Sprintf("Run(%q) returned invalid exit code %d", args, code))
	}
	return runResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func assertResult(t *testing.T, got runResult, wantCode int, wantStdout, wantStderr string) {
	t.Helper()
	if got.code != wantCode {
		t.Errorf("exit code = %d, want %d", got.code, wantCode)
	}
	if got.stdout != wantStdout {
		t.Errorf("stdout = %q, want %q", got.stdout, wantStdout)
	}
	if got.stderr != wantStderr {
		t.Errorf("stderr = %q, want %q", got.stderr, wantStderr)
	}
}

func checkoutDeps(t *testing.T, rootFile string) seam.Deps {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, checkout.RootFilePath), []byte(rootFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	return checkoutDepsAt(root)
}

func checkoutDepsAt(root string) seam.Deps {
	return seam.Deps{
		EUID: 1,
		Dir:  filepath.Join(root, "work"),
		Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
			if command.Path != "git" || !reflect.DeepEqual(command.Args, []string{"rev-parse", "--show-toplevel"}) {
				return seam.Result{}, fmt.Errorf("unexpected command: %#v", command)
			}
			return seam.Result{Stdout: []byte(root + "\n")}, nil
		},
	}
}

func prepareCLIApp(t *testing.T, deps seam.Deps) seam.Deps {
	t.Helper()
	root := filepath.Dir(deps.Dir)
	app := filepath.Join(root, "crm")
	if err := os.MkdirAll(filepath.Join(app, "cmd", "crm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(app, "etc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "cmd", "crm", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, checkout.ManifestFile), []byte("app = \"crm\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return deps
}

func prepareCLIArchive(t *testing.T, deps seam.Deps) seam.Deps {
	t.Helper()
	if err := os.WriteFile(filepath.Join(deps.Dir, "crm-v1.2.3.tar.xz"), []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps.Exec = cliCheckoutAndHostExec(t, deps.Exec)
	return deps
}

func cliCheckoutAndHostExec(t *testing.T, checkoutExec seam.Runner) seam.Runner {
	t.Helper()
	return func(ctx context.Context, command seam.Cmd) (seam.Result, error) {
		switch command.Path {
		case "git":
			return checkoutExec(ctx, command)
		case "tar":
			if len(command.Args) != 0 && command.Args[0] == "-t" {
				return seam.Result{Stdout: []byte("etc/manifest.toml\nbin/crm\n")}, nil
			}
			return seam.Result{Stdout: []byte("app = \"crm\"\n")}, nil
		case "ssh":
			return seam.Result{}, nil
		default:
			t.Fatalf("unexpected command: %#v", command)
			return seam.Result{}, errors.New("unexpected command")
		}
	}
}
