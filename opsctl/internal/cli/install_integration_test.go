package cli_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallPackageOwnershipAndCLIComposition(t *testing.T) {
	// R-OJB9-VRYL
	entries, err := os.ReadDir(filepath.Join("..", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"cloud": true, "config": true, "host": true}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "apps", entry.Name()), nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatal(unquoteErr)
			}
			const prefix = "github.com/ikigenba/ikigenba/opsctl/internal/"
			if strings.HasPrefix(importPath, prefix) {
				dependency := strings.Split(strings.TrimPrefix(importPath, prefix), "/")[0]
				if !allowed[dependency] {
					t.Errorf("internal/apps/%s imports forbidden internal dependency %q", entry.Name(), importPath)
				}
			}
		}
	}

	fixture := newCLIInstallFixture(t)
	stdout, stderr, code := fixture.invoke()
	if code != 0 || stderr != "" {
		t.Fatalf("install = exit %d, stderr %q", code, stderr)
	}
	wantOutput := "fetch: ok (notes-v1.tar.xz, 0.0 MiB)\n" +
		"file: ok (notes, port 4100, default)\n" +
		"secrets: ok (1 keys)\n" +
		"unpack: ok (/opt/notes)\n" +
		"unit: ok (ikigenba-notes.service)\n" +
		"nginx: ok (notes.host.example, host.example)\n" +
		"litestream: ok (state/notes.db)\n" +
		"service: ok (notes v1.2.3 active)\n"
	if stdout != wantOutput {
		t.Fatalf("stdout = %q, want %q", stdout, wantOutput)
	}
	fixture.assertOrderedCommands(t, []string{
		"xz --decompress --stdout",
		"systemctl is-active ikigenba-notes.service",
		"id --user ikigenba",
		"usermod --shell /usr/sbin/nologin ikigenba",
		"chown ikigenba " + filepath.Join(fixture.root, "opt", "notes"),
		"systemctl daemon-reload",
		"systemctl enable ikigenba-notes.service",
		"nginx -t",
		"systemctl reload nginx",
		"systemctl restart litestream.service",
		"systemctl start ikigenba-notes.service",
		"systemctl is-active ikigenba-notes.service",
		filepath.Join(fixture.root, "opt", "notes", "bin", "notes") + " --version",
	})
}

func TestInstallCLIReportsEveryStageAndStopsAtFailure(t *testing.T) {
	// R-P0DV-8KCB, R-P2TO-03TP, R-EM4J-XPDA, R-EOKC-P8UO, R-UBWT-KXSF
	fixture := newCLIInstallFixture(t)
	fixture.failCommand = "nginx -t"
	stdout, stderr, code := fixture.invoke()
	wantOutput := "fetch: ok (notes-v1.tar.xz, 0.0 MiB)\n" +
		"file: ok (notes, port 4100, default)\n" +
		"secrets: ok (1 keys)\n" +
		"unpack: ok (/opt/notes)\n" +
		"unit: ok (ikigenba-notes.service)\n" +
		"nginx: failed: nginx -t: exit status 7\n"
	if code != 1 || stdout != wantOutput || stderr != "opsctl: install failed\n\n> nginx stdout\n> nginx stderr\n" {
		t.Fatalf("install failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	fixture.assertOrderedCommands(t, []string{
		"xz --decompress --stdout",
		"systemctl is-active ikigenba-notes.service",
		"id --user ikigenba",
		"usermod --shell /usr/sbin/nologin ikigenba",
		"chown ikigenba " + filepath.Join(fixture.root, "opt", "notes"),
		"systemctl daemon-reload",
		"systemctl enable ikigenba-notes.service",
		"nginx -t",
	})
	if _, err := os.Stat(filepath.Join(fixture.root, "etc", "litestream.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("litestream configuration exists after nginx failure: %v", err)
	}
}

func TestInstallCLIStopsWhenLitestreamRegenerationFails(t *testing.T) {
	// R-P0DV-8KCB, R-UBWT-KXSF
	fixture := newCLIInstallFixture(t)
	store := config.Store{Root: fixture.root}
	if err := store.Set("backup.s3_uri", "not-an-s3-uri"); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := fixture.invoke()
	wantOutput := "fetch: ok (notes-v1.tar.xz, 0.0 MiB)\n" +
		"file: ok (notes, port 4100, default)\n" +
		"secrets: ok (1 keys)\n" +
		"unpack: ok (/opt/notes)\n" +
		"unit: ok (ikigenba-notes.service)\n" +
		"nginx: ok (notes.host.example, host.example)\n" +
		"litestream: failed: backup.s3_uri: must be an absolute s3:// URI with a nonempty bucket and no query or fragment\n"
	if code != 1 || stdout != wantOutput || stderr != "opsctl: install failed\n" {
		t.Fatalf("regeneration failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	fixture.assertOrderedCommands(t, installCommandsThroughNginx(fixture.root))
}

func TestInstallCLIStopsWhenLitestreamRestartFails(t *testing.T) {
	// R-P0DV-8KCB, R-UBWT-KXSF
	fixture := newCLIInstallFixture(t)
	fixture.failCommand = "systemctl restart litestream.service"

	stdout, stderr, code := fixture.invoke()
	wantOutput := "fetch: ok (notes-v1.tar.xz, 0.0 MiB)\n" +
		"file: ok (notes, port 4100, default)\n" +
		"secrets: ok (1 keys)\n" +
		"unpack: ok (/opt/notes)\n" +
		"unit: ok (ikigenba-notes.service)\n" +
		"nginx: ok (notes.host.example, host.example)\n" +
		"litestream: failed: restart litestream.service: exit status 7\n"
	if code != 1 || stdout != wantOutput || stderr != "opsctl: install failed\n\n> command rejected\n" {
		t.Fatalf("restart failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	wantCommands := append(installCommandsThroughNginx(fixture.root), "systemctl restart litestream.service")
	fixture.assertOrderedCommands(t, wantCommands)
}

func TestInstallCLIStopsWhenLitestreamRestartTransportFails(t *testing.T) {
	// R-P0DV-8KCB, R-UBWT-KXSF
	for _, test := range []struct {
		name       string
		failure    error
		result     host.Result
		wantReport string
		wantStderr string
	}{
		{
			name:       "transport",
			failure:    errors.New("connection lost"),
			result:     host.Result{Stderr: []byte("partial transport detail\n")},
			wantReport: "restart litestream.service: connection lost",
			wantStderr: "opsctl: install failed\n\n> partial transport detail\n",
		},
		{
			name: "preexisting command error",
			failure: &host.CommandError{
				Label:  "remote litestream restart",
				Result: host.Result{Stdout: []byte("remote stdout\n"), Stderr: []byte("remote stderr\n")},
				Err:    errors.New("connection lost"),
			},
			wantReport: "remote litestream restart: connection lost",
			wantStderr: "opsctl: install failed\n\n> remote stdout\n> remote stderr\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLIInstallFixture(t)
			fixture.failCommand = "systemctl restart litestream.service"
			fixture.commandFailure = test.failure
			fixture.failureResult = test.result

			stdout, stderr, code := fixture.invoke()
			wantOutput := installReportThroughNginx() + "litestream: failed: " + test.wantReport + "\n"
			if code != 1 || stdout != wantOutput || stderr != test.wantStderr {
				t.Fatalf("transport failure = exit %d stdout %q stderr %q", code, stdout, stderr)
			}
			wantCommands := append(installCommandsThroughNginx(fixture.root), "systemctl restart litestream.service")
			fixture.assertOrderedCommands(t, wantCommands)
			if fixture.commandCount("systemctl start ikigenba-notes.service") != 0 {
				t.Fatalf("app start followed Litestream failure: %#v", fixture.commands)
			}
		})
	}
}

func TestInstallCLIDatabaseRemovalReportsUpdatedLitestream(t *testing.T) {
	// R-P2TO-03TP
	fixture := newCLIInstallFixture(t)
	if stdout, stderr, code := fixture.invoke(); code != 0 || stderr != "" || stdout != installReportPrefix("state/notes.db")+"service: ok (notes v1.2.3 active)\n" {
		t.Fatalf("initial install = exit %d stdout %q stderr %q", code, stdout, stderr)
	}

	fixture.commands = nil
	fixture.manifest = strings.ReplaceAll(fixture.manifest, "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n", "")
	stdout, stderr, code := fixture.invoke()
	wantOutput := installReportPrefix("updated") + "service: ok (notes v1.2.3 active)\n"
	if code != 0 || stdout != wantOutput || stderr != "" {
		t.Fatalf("database-removal install = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if fixture.commandCount("systemctl restart litestream.service") != 1 {
		t.Fatalf("database-removal commands = %#v", fixture.commands)
	}
}

func TestInstallCLIQuotesUnsafeReportDetail(t *testing.T) {
	// R-EM4J-XPDA
	fixture := newCLIInstallFixture(t)
	fixture.openFailure = errors.New("\x1b[31m")
	stdout, stderr, code := fixture.invoke()
	if code != 1 || stdout != "fetch: failed: \"\\x1b[31m\"\n" || stderr != "opsctl: artifact download failed\n" {
		t.Fatalf("unsafe detail = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestInstallCLIStartupFailureKeepsEffectsAndRetrySucceeds(t *testing.T) {
	// R-P8X5-WYJ6, R-PA52-AQ9V
	fixture := newCLIInstallFixture(t)
	const litestreamUnitPath = "etc/systemd/system/litestream.service"
	litestreamUnit := filepath.Join(fixture.root, filepath.FromSlash(litestreamUnitPath))
	writeCLIInstallFile(t, litestreamUnit, "shared Litestream unit\n")
	rootFS, err := os.OpenRoot(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	fixture.failStart = true
	stdout, stderr, code := fixture.invoke()
	if code != 1 || !strings.HasSuffix(stdout, "service: failed: notes: service failed to start\n") ||
		stderr != "opsctl: install failed\n\n> app journal one\n> app journal two\n" {
		t.Fatalf("failed install = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, name := range []string{
		filepath.Join(fixture.root, "opt", "notes", "bin", "notes"),
		filepath.Join(fixture.root, "etc", "systemd", "system", "ikigenba-notes.service"),
		filepath.Join(fixture.root, "etc", "nginx", "conf.d", "ikigenba.conf"),
		filepath.Join(fixture.root, "etc", "litestream.yml"),
	} {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("completed effect %s was rolled back: %v", name, err)
		}
	}
	configurationBefore, err := rootFS.ReadFile("etc/litestream.yml")
	if err != nil {
		t.Fatal(err)
	}
	unitBefore, err := rootFS.ReadFile(litestreamUnitPath)
	if err != nil {
		t.Fatal(err)
	}
	unitInfoBefore, err := rootFS.Stat(litestreamUnitPath)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(fixture.root, "opt", "notes", "state", "keep")
	cache := filepath.Join(fixture.root, "opt", "notes", "cache", "keep")
	writeCLIInstallFile(t, state, "state")
	writeCLIInstallFile(t, cache, "cache")

	fixture.commands = nil
	fixture.failStart = false
	stdout, stderr, code = fixture.invoke()
	wantRetryOutput := installReportPrefix("unchanged") + "service: ok (notes v1.2.3 active)\n"
	if code != 0 || stderr != "" || stdout != wantRetryOutput {
		t.Fatalf("retry = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	configurationAfter, err := rootFS.ReadFile("etc/litestream.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(configurationAfter, configurationBefore) {
		t.Fatal("retry changed byte-identical Litestream configuration")
	}
	unitAfter, err := rootFS.ReadFile(litestreamUnitPath)
	if err != nil {
		t.Fatal(err)
	}
	unitInfoAfter, err := rootFS.Stat(litestreamUnitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unitAfter, unitBefore) || !os.SameFile(unitInfoBefore, unitInfoAfter) ||
		unitInfoAfter.Mode() != unitInfoBefore.Mode() || !unitInfoAfter.ModTime().Equal(unitInfoBefore.ModTime()) {
		t.Fatal("retry changed the shared Litestream unit")
	}
	if fixture.commandCount("systemctl restart litestream.service") != 0 {
		t.Fatalf("retry commands = %v; Litestream was restarted for unchanged configuration", fixture.commands)
	}
	stateContents, stateErr := rootFS.ReadFile("opt/notes/state/keep")
	cacheContents, cacheErr := rootFS.ReadFile("opt/notes/cache/keep")
	if stateErr != nil || string(stateContents) != "state" {
		t.Fatalf("preserved state = %q, %v", stateContents, stateErr)
	}
	if cacheErr != nil || string(cacheContents) != "cache" {
		t.Fatalf("preserved cache = %q, %v", cacheContents, cacheErr)
	}
}

func installReportPrefix(litestreamDetail string) string {
	return installReportThroughNginx() +
		"litestream: ok (" + litestreamDetail + ")\n"
}

func installReportThroughNginx() string {
	return "fetch: ok (notes-v1.tar.xz, 0.0 MiB)\n" +
		"file: ok (notes, port 4100, default)\n" +
		"secrets: ok (1 keys)\n" +
		"unpack: ok (/opt/notes)\n" +
		"unit: ok (ikigenba-notes.service)\n" +
		"nginx: ok (notes.host.example, host.example)\n"
}

func installCommandsThroughNginx(root string) []string {
	return []string{
		"xz --decompress --stdout",
		"systemctl is-active ikigenba-notes.service",
		"id --user ikigenba",
		"usermod --shell /usr/sbin/nologin ikigenba",
		"chown ikigenba " + filepath.Join(root, "opt", "notes"),
		"systemctl daemon-reload",
		"systemctl enable ikigenba-notes.service",
		"nginx -t",
		"systemctl reload nginx",
	}
}

func TestInstallCLINeverSerializesEnvironmentValues(t *testing.T) {
	// R-OL1Q-ZER1
	fixture := newCLIInstallFixture(t)
	fixture.secretValue = "secret-value-never-report"
	fixture.plainValue = "plain-value-never-report"
	fixture.failCommand = "systemctl enable ikigenba-notes.service"
	stdout, stderr, code := fixture.invoke()
	if code != 1 {
		t.Fatalf("install exit = %d, want 1", code)
	}
	all := stdout + stderr
	for _, value := range []string{fixture.secretValue, fixture.plainValue} {
		if strings.Contains(all, value) {
			t.Fatalf("output exposed environment value %q: stdout %q stderr %q", value, stdout, stderr)
		}
		for _, command := range fixture.commands {
			if strings.Contains(strings.Join(append(append([]string{}, command.Args...), command.Env...), "\x00"), value) {
				t.Fatalf("command exposed environment value %q: %#v", value, command)
			}
		}
	}
	if !strings.Contains(stdout, "secrets: ok (1 keys)\n") || !strings.Contains(stdout, "unit: failed:") {
		t.Fatalf("safe metadata missing from stdout %q", stdout)
	}
}

func TestInstallCLIQuotesJournalEnvironmentValuesWithoutRedaction(t *testing.T) {
	// R-OL1Q-ZER1, R-P8X5-WYJ6
	fixture := newCLIInstallFixture(t)
	fixture.secretValue = "actual-secret-value"
	fixture.plainValue = "actual-plain-value"
	fixture.failStart = true
	fixture.journalOutput = []byte("TOKEN=" + fixture.secretValue + "\nPLAIN=" + fixture.plainValue + "\n")

	stdout, stderr, code := fixture.invoke()
	wantStderr := "opsctl: install failed\n\n> TOKEN=actual-secret-value\n> PLAIN=actual-plain-value\n"
	if code != 1 || !strings.HasSuffix(stdout, "service: failed: notes: service failed to start\n") || stderr != wantStderr {
		t.Fatalf("journal exception = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestInstallCLIIdentifiesStartupAndJournalFailures(t *testing.T) {
	// R-P8X5-WYJ6
	for _, test := range []struct {
		name       string
		failure    error
		result     host.Result
		wantStderr string
	}{
		{
			name:    "journal transport",
			failure: errors.New("journal connection lost"),
			wantStderr: "opsctl: install failed\n\n" +
				"start ikigenba-notes.service: exit status 9\n> start rejected\n" +
				"obtain ikigenba-notes.service journal: journal connection lost\n",
		},
		{
			name:   "journal nonzero exit",
			result: host.Result{ExitCode: 8, Stdout: []byte("journal stdout\n"), Stderr: []byte("journal stderr\n")},
			wantStderr: "opsctl: install failed\n\n" +
				"start ikigenba-notes.service: exit status 9\n> start rejected\n" +
				"obtain ikigenba-notes.service journal: exit status 8\n> journal stdout\n> journal stderr\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCLIInstallFixture(t)
			fixture.failStart = true
			fixture.journalFailure = test.failure
			fixture.journalResult = &test.result

			stdout, stderr, code := fixture.invoke()
			if code != 1 || !strings.HasSuffix(stdout, "service: failed: notes: service failed to start\n") || stderr != test.wantStderr {
				t.Fatalf("combined failure = exit %d stdout %q stderr %q", code, stdout, stderr)
			}
			if fixture.commands[len(fixture.commands)-1].Name != "journalctl" {
				t.Fatalf("last command = %#v, want journalctl", fixture.commands[len(fixture.commands)-1])
			}
		})
	}
}

type cliInstallFixture struct {
	t              *testing.T
	root           string
	manifest       string
	secretValue    string
	plainValue     string
	commands       []host.Command
	active         bool
	failStart      bool
	failCommand    string
	commandFailure error
	failureResult  host.Result
	journalFailure error
	journalResult  *host.Result
	journalOutput  []byte
	openFailure    error
	cloud          *cliInstallCloud
}

func newCLIInstallFixture(t *testing.T) *cliInstallFixture {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, "etc", "nginx", "conf.d"),
		filepath.Join(root, "etc", "systemd", "system"),
		filepath.Join(root, "opt"),
	} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	store := config.Store{Root: root}
	for key, value := range map[string]string{
		"host.name":                  "host.example",
		"aws.region":                 "us-east-2",
		"backup.s3_uri":              "s3://backups/services",
		"backup.service_db_seconds":  "3600",
		"backup.service_wal_seconds": "1",
	} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	manifest := "app = \"notes\"\nport = 4100\ndefault = true\nsecrets = [\"TOKEN\"]\n[env]\nPLAIN = \"PLAIN_VALUE\"\n"
	manifest += "[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n"
	fixture := &cliInstallFixture{
		t: t, root: root, manifest: manifest,
		secretValue: "secret", plainValue: "PLAIN_VALUE",
	}
	fixture.cloud = &cliInstallCloud{fixture: fixture}
	return fixture
}

func (fixture *cliInstallFixture) invoke() (string, string, int) {
	fixture.t.Helper()
	fixture.manifest = strings.ReplaceAll(fixture.manifest, "PLAIN_VALUE", fixture.plainValue)
	return invoke([]string{"install", "s3://artifacts/releases/notes-v1.tar.xz"}, cli.Deps{
		Root:    fixture.root,
		EUID:    0,
		Execute: fixture.execute,
		Cloud: cloud.Env{Open: func(_ context.Context, region string) (cloud.Client, error) {
			if region != "us-east-2" {
				return nil, errors.New("wrong region")
			}
			if fixture.openFailure != nil {
				return nil, fixture.openFailure
			}
			return fixture.cloud, nil
		}},
	})
}

func (fixture *cliInstallFixture) execute(_ context.Context, command host.Command) (host.Result, error) {
	fixture.commands = append(fixture.commands, command)
	key := command.Name
	if len(command.Args) > 0 {
		key += " " + strings.Join(command.Args, " ")
	}
	if key == fixture.failCommand {
		if fixture.commandFailure != nil {
			return fixture.failureResult, fixture.commandFailure
		}
		if key == "nginx -t" {
			return host.Result{ExitCode: 7, Stdout: []byte("nginx stdout\n"), Stderr: []byte("nginx stderr\n")}, nil
		}
		return host.Result{ExitCode: 7, Stderr: []byte("command rejected\n")}, nil
	}
	switch {
	case command.Name == "xz":
		return host.Result{Stdout: cliInstallTar(fixture.t, fixture.manifest)}, nil
	case key == "systemctl is-active ikigenba-notes.service":
		if fixture.active {
			return host.Result{Stdout: []byte("active\n")}, nil
		}
		return host.Result{ExitCode: 3, Stdout: []byte("inactive\n")}, nil
	case key == "id --user ikigenba":
		return host.Result{Stdout: []byte("998\n")}, nil
	case key == "systemctl start ikigenba-notes.service" || key == "systemctl restart ikigenba-notes.service":
		if fixture.failStart {
			return host.Result{ExitCode: 9, Stderr: []byte("start rejected\n")}, nil
		}
		fixture.active = true
		return host.Result{}, nil
	case command.Name == "journalctl":
		if fixture.journalFailure != nil {
			return host.Result{}, fixture.journalFailure
		}
		if fixture.journalResult != nil {
			return *fixture.journalResult, nil
		}
		output := fixture.journalOutput
		if output == nil {
			output = []byte("app journal one\napp journal two\n")
		}
		return host.Result{Stdout: output}, nil
	case command.Name == filepath.Join(fixture.root, "opt", "notes", "bin", "notes"):
		return host.Result{Stdout: []byte("v1.2.3\n")}, nil
	default:
		return host.Result{}, nil
	}
}

func (fixture *cliInstallFixture) assertOrderedCommands(t *testing.T, want []string) {
	t.Helper()
	got := make([]string, 0, len(fixture.commands))
	for _, command := range fixture.commands {
		line := command.Name
		if len(command.Args) > 0 {
			line += " " + strings.Join(command.Args, " ")
		}
		got = append(got, line)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func (fixture *cliInstallFixture) commandCount(want string) int {
	count := 0
	for _, command := range fixture.commands {
		line := command.Name
		if len(command.Args) > 0 {
			line += " " + strings.Join(command.Args, " ")
		}
		if line == want {
			count++
		}
	}
	return count
}

type cliInstallCloud struct{ fixture *cliInstallFixture }

func (client *cliInstallCloud) GetObject(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("compressed artifact")), nil
}

func (*cliInstallCloud) PutObject(context.Context, string, io.Reader) error { return nil }

func (*cliInstallCloud) ListObjects(context.Context, string) ([]cloud.Object, error) { return nil, nil }

func (client *cliInstallCloud) ReadSecrets(_ context.Context, parameter string) (map[string]string, error) {
	if parameter != "/ikigenba/host.example/notes" {
		return nil, errors.New("wrong secret parameter")
	}
	return map[string]string{"TOKEN": client.fixture.secretValue, "UNREQUESTED": "not installed"}, nil
}

func cliInstallTar(t *testing.T, manifest string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, entry := range []struct {
		name string
		mode int64
		body string
	}{
		{name: "bin/notes", mode: 0o755, body: "installed binary"},
		{name: "etc/manifest.toml", mode: 0o600, body: manifest},
	} {
		if err := writer.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writeCLIInstallFile(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
