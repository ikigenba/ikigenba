package cli

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

var _ func(context.Context, []string, io.Reader, io.Writer, io.Writer, seam.Deps) int = Run

const expectedUsage = `Usage: devctl [options] <command> [arguments]

Manage the ikigenba platform from the developer's machine. Never run as root.

Commands:
  version   print the version
  space     list, create, destroy, stop, start, initialise, and inspect spaces
  secrets   push and list an app's secrets for a space
  build     build one app into its deployable file
  deploy    put a built app file on a space
  remove    take an app off a space
  restore   put a space's app back from its backups

Options:
  --help              print this help
  --version           print the version
  --account <name>    AWS shared-config profile to act in

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
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
	// R-D4F7-8UZB
	for _, args := range [][]string{
		{"version"}, {"-V"}, {"--version"}, {"-h"}, {"--help"},
		{"--account", "work", "version"}, {"--account=work", "version"},
	} {
		if got := invoke(args...).code; got != 0 {
			t.Errorf("Run(%q) = %d, want 0", args, got)
		}
	}
	for _, option := range []string{"-x", "--verbose", "--", "-version"} {
		result := invoke(option)
		if result.code != 2 || !strings.HasPrefix(result.stderr, "devctl: unknown option '") {
			t.Errorf("Run(%q) = (%d, %q), want unknown-option usage error", option, result.code, result.stderr)
		}
	}
}

func TestArgumentsAfterCommandAreCommandArguments(t *testing.T) {
	// R-9T69-QTTI
	assertResult(t, invoke("version", "--help"), 0, "Usage: devctl version\n\nPrint the version.\n", "")
	assertResult(t, invoke("version", "--account", "work"), 2, "", "devctl: version takes no arguments\n\nsee 'devctl version --help' for usage\n")
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

func TestAccountProfileReachesCloudUnchanged(t *testing.T) {
	// R-9UE6-4LK7
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"--account", "", "space"}, want: ""},
		{args: []string{"--account", " Work Profile ", "space"}, want: " Work Profile "},
		{args: []string{"--account=MiXeD Profile", "space"}, want: "MiXeD Profile"},
	} {
		var profiles []string
		result := invokeWithDeps(seam.Deps{
			EUID: 1,
			Cloud: func(_ context.Context, profile, _ string) (cloud.Clients, error) {
				profiles = append(profiles, profile)
				if len(profiles) == 1 {
					return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
				}
				return cloud.Clients{}, nil
			},
		}, test.args...)
		assertResult(t, result, 0, "", "")
		if !reflect.DeepEqual(profiles, []string{test.want, test.want}) {
			t.Errorf("Run(%q) cloud profiles = %q, want [%q %q]", test.args, profiles, test.want, test.want)
		}
	}
}

func TestLastAccountOptionWins(t *testing.T) {
	// R-9VM2-IDAW
	for _, args := range [][]string{
		{"--account", "first", "--account=second", "space"},
		{"--account=first", "--account", "second", "space"},
	} {
		var profiles []string
		result := invokeWithDeps(seam.Deps{
			EUID: 1,
			Cloud: func(_ context.Context, profile, _ string) (cloud.Clients, error) {
				profiles = append(profiles, profile)
				if len(profiles) == 1 {
					return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
				}
				return cloud.Clients{}, nil
			},
		}, args...)
		assertResult(t, result, 0, "", "")
		if !reflect.DeepEqual(profiles, []string{"second", "second"}) {
			t.Fatalf("Run(%q) cloud profiles = %q, want [second second]", args, profiles)
		}
	}
}

func TestCloudErrorsAreSingleLineOperationFailures(t *testing.T) {
	// R-ZLGA-YMQA
	for _, test := range []struct {
		cloudError *cloud.Error
		want       string
	}{
		{
			cloudError: &cloud.Error{Service: "ssm", Operation: "GetParameter", Subject: "/ikigenba/account", Code: "ParameterNotFound"},
			want:       "ssm GetParameter /ikigenba/account: ParameterNotFound",
		},
		{
			cloudError: &cloud.Error{Service: "ec2", Operation: "RunInstances", Code: "InsufficientInstanceCapacity"},
			want:       "ec2 RunInstances: InsufficientInstanceCapacity",
		},
		{
			cloudError: &cloud.Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"},
			want:       "route53 ChangeResourceRecordSets: Throttling",
		},
	} {
		var opens []string
		deps := seam.Deps{
			EUID: 1,
			Cloud: func(_ context.Context, _ string, region string) (cloud.Clients, error) {
				opens = append(opens, region)
				if len(opens) == 1 {
					return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
				}
				return cloud.Clients{EC2: &cliEC2{err: fmt.Errorf("command failed: %w", test.cloudError)}}, nil
			},
		}
		assertResult(t, invokeWithDeps(deps, "--account", "work", "space", "status", "example.test"), 1, "", "devctl: "+test.want+"\n")
		if !reflect.DeepEqual(opens, []string{"", "us-test-1"}) {
			t.Errorf("cloud regions = %q, want bootstrap and configured regions", opens)
		}
	}
}

func TestCommandsReportMissingSpace(t *testing.T) {
	// R-ZMO7-CEGZ
	domain := "missing.example.test"
	for _, args := range [][]string{
		{"space", "stop", domain},
		{"space", "start", domain},
		{"space", "status", domain},
		{"secrets", "push", domain},
		{"deploy", domain},
		{"restore", domain},
	} {
		calls := 0
		deps := seam.Deps{
			EUID: 1,
			Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				calls++
				if calls == 1 {
					return cloud.Clients{SSM: &cliSSM{value: cliPropertiesJSON}}, nil
				}
				return cloud.Clients{EC2: &cliEC2{}}, nil
			},
		}
		want := "devctl: no space at '" + domain + "'\n"
		assertResult(t, invokeWithDeps(deps, append([]string{"--account", "work"}, args...)...), 1, "", want)
	}
}

func TestAccountRequiresValue(t *testing.T) {
	// R-9WTY-W51L
	want := "devctl: option '--account' requires a value\n\nsee 'devctl --help' for usage\n"
	for _, args := range [][]string{
		{"--account"},
		{"--account", "--help"},
		{"--account="},
	} {
		assertResult(t, invoke(args...), 2, "", want)
	}
	for command := range commandSet {
		assertResult(t, invoke("--account", command), 2, "", want)
	}
}

func TestAccountDoesNotChangeVersion(t *testing.T) {
	// R-DE6E-B0WV
	want := invoke("version")
	for _, args := range [][]string{
		{"--account", "work", "version"},
		{"--account=work", "version"},
	} {
		if got := invoke(args...); got != want {
			t.Errorf("Run(%q) = %#v, want %#v", args, got, want)
		}
	}
}

func TestAccountVersionDoesNotOpenCloud(t *testing.T) {
	// R-UYZW-0UKR
	for _, args := range [][]string{
		{"--account", "work", "version"},
		{"--account=work", "version"},
	} {
		calls := 0
		result := invokeWithDeps(seam.Deps{
			EUID: 1,
			Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				calls++
				return cloud.Clients{}, nil
			},
		}, args...)
		assertResult(t, result, 0, version+"\n", "")
		if calls != 0 {
			t.Errorf("Run(%q) opened cloud %d times, want 0", args, calls)
		}
	}
}

func TestRootRefusalPrecedesEveryInvocation(t *testing.T) {
	// R-A1PK-F80D
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
	}
	for _, args := range invocations {
		assertResult(t, invokeWithDeps(seam.Deps{EUID: 0}, args...), 3, "", "devctl: must not run as root\n")
	}
}

func TestExactlyFiveCommandsRequireAccount(t *testing.T) {
	// R-C1FA-A0HT
	wantRequired := map[string]struct{}{
		"space": {}, "secrets": {}, "deploy": {}, "restore": {}, "remove": {},
	}
	if !reflect.DeepEqual(accountRequired, wantRequired) {
		t.Fatalf("account-required commands = %v, want %v", accountRequired, wantRequired)
	}

	wantBuild := invoke("build", "app")
	for _, args := range [][]string{
		{"--account", "work", "build", "app"},
		{"--account=work", "build", "app"},
	} {
		if got := invoke(args...); got != wantBuild {
			t.Errorf("Run(%q) = %#v, want %#v", args, got, wantBuild)
		}
	}
}

func TestAccountRequiredBeforeCommandArguments(t *testing.T) {
	// R-3PTI-JABB
	for _, command := range []string{"space", "secrets", "deploy", "restore", "remove"} {
		for _, arguments := range [][]string{nil, {"--definitely-invalid"}} {
			calls := 0
			deps := seam.Deps{
				EUID: 1,
				Cloud: func(context.Context, string, string) (cloud.Clients, error) {
					calls++
					return cloud.Clients{}, nil
				},
			}
			args := append([]string{command}, arguments...)
			wantStderr := "devctl: --account is required\n\nsee 'devctl " + command + " --help' for usage\n"
			assertResult(t, invokeWithDeps(deps, args...), 2, "", wantStderr)
			if calls != 0 {
				t.Errorf("Run(%q) opened cloud %d times, want 0", args, calls)
			}
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
	// R-BYZH-IH0F
	got := make([]string, 0, len(commandSet))
	for command := range commandSet {
		got = append(got, command)
	}
	sort.Strings(got)
	want := []string{"build", "deploy", "remove", "restore", "secrets", "space", "version"}
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
	// R-C3V3-1JZ7
	for _, option := range []string{"--help", "-h"} {
		assertResult(t, invoke(option), 0, expectedUsage, "")
	}
}

func TestDiagnosticStreams(t *testing.T) {
	// R-DMPO-ZF3Q
	successes := [][]string{
		{"--help"}, {"--version"}, {"version"}, {"version", "--help"}, {"build", "app"},
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

const cliPropertiesJSON = `{
	"domain":"example.test",
	"backup_bucket":"backups",
	"launch_template_id":"lt-123",
	"permissions_boundary_arn":"arn:boundary",
	"region":"us-test-1",
	"delete_secrets_on_destroy":true,
	"delete_backups_on_destroy":false,
	"backup_host_files_seconds":1,
	"backup_service_files_seconds":2,
	"backup_service_db_seconds":3,
	"backup_service_wal_seconds":4
}`

type cliSSM struct {
	cloud.SSM
	value string
	err   error
}

func (fake *cliSSM) GetParameter(context.Context, string) (string, error) {
	return fake.value, fake.err
}

type cliEC2 struct {
	cloud.EC2
	err error
}

func (fake *cliEC2) ListSpaceInstances(context.Context) ([]cloud.Instance, error) {
	return nil, fake.err
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
