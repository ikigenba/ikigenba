package main

import (
	"bufio"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// runtimeVersion returns the version reported by a freshly built opsctl.
// Release tests learn this value only through the binary's public contract.
func runtimeVersion(t *testing.T, project string) string {
	t.Helper()
	build := exec.Command("go", "build", "-trimpath", "-o", "opsctl-version-probe", "./cmd/opsctl")
	build.Dir = project
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOPROXY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build version probe: %v\n%s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(project, "opsctl-version-probe")) })
	report := exec.Command("./opsctl-version-probe", "version")
	report.Dir = project
	output, err := report.CombinedOutput()
	if err != nil {
		t.Fatalf("run version probe: %v\n%s", err, output)
	}
	return strings.TrimSuffix(string(output), "\n")
}

func TestReleaseBuildsAndPublishesExactAssets(t *testing.T) {
	// R-IH58-0PHG R-UL9F-YPQI R-PSAO-ADAF R-IKSX-60PJ
	project := releaseProjectRoot(t)
	version := runtimeVersion(t, project)
	tag := "opsctl/" + version
	binaryName := "opsctl-" + version + "-linux-amd64"
	dist := t.TempDir()
	capture := filepath.Join(t.TempDir(), "gh-arguments")
	fixtureBin := t.TempDir()
	writeExecutable(t, filepath.Join(fixtureBin, "gh"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\0' "$@" > "$GH_CAPTURE"
`)

	command := exec.Command("bash", "release.sh", tag) //nolint:gosec // The tag is built from the version reported by the probe binary.
	command.Dir = project
	command.Env = append(os.Environ(),
		"PATH="+fixtureBin+":"+os.Getenv("PATH"),
		"GH_CAPTURE="+capture,
		"GOPROXY=off",
		"OPSCTL_RELEASE_DIST="+dist,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("release fixture: %v\n%s", err, output)
	}

	wantNames := []string{"checksums.txt", "install.sh", binaryName}
	entries, err := os.ReadDir(dist)
	if err != nil {
		t.Fatal(err)
	}
	var gotNames []string
	for _, entry := range entries {
		gotNames = append(gotNames, entry.Name())
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("release assets = %q, want exactly %q", gotNames, wantNames)
	}

	binaryPath := filepath.Join(dist, binaryName)
	assertLinuxAMD64Static(t, binaryPath)
	report := exec.Command("./"+binaryName, "version") //nolint:gosec // The asset name is built from the version reported by the probe binary.
	report.Dir = dist
	if output, err := report.CombinedOutput(); err != nil || string(output) != version+"\n" {
		t.Fatalf("released version = %q, %v", output, err)
	}

	distRoot, err := os.OpenRoot(dist)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = distRoot.Close() })
	binary, err := distRoot.ReadFile(binaryName)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	wantChecksum := hex.EncodeToString(digest[:]) + "  " + binaryName + "\n"
	checksum, err := distRoot.ReadFile("checksums.txt")
	if err != nil || string(checksum) != wantChecksum {
		t.Fatalf("checksums.txt = %q, %v; want %q", checksum, err, wantChecksum)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}  ` + regexp.QuoteMeta(binaryName) + `\n$`).Match(checksum) {
		t.Fatalf("checksum grammar = %q", checksum)
	}
	releasedInstaller, err := distRoot.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	projectRoot, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = projectRoot.Close() })
	projectInstaller, err := projectRoot.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	installerInfo, err := os.Stat(filepath.Join(dist, "install.sh"))
	if err != nil || !reflect.DeepEqual(releasedInstaller, projectInstaller) || installerInfo.Mode().Perm() != 0o755 {
		t.Fatalf("released install.sh: bytes match = %v, mode = %v, error = %v", reflect.DeepEqual(releasedInstaller, projectInstaller), installerInfo.Mode().Perm(), err)
	}

	captureRoot, err := os.OpenRoot(filepath.Dir(capture))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = captureRoot.Close() })
	arguments, err := captureRoot.ReadFile(filepath.Base(capture))
	if err != nil {
		t.Fatal(err)
	}
	wantArguments := []string{
		"release", "create", tag,
		"--title", tag, "--verify-tag", "--generate-notes",
		binaryPath, filepath.Join(dist, "checksums.txt"), filepath.Join(dist, "install.sh"),
	}
	if got := splitNULArguments(arguments); !reflect.DeepEqual(got, wantArguments) {
		t.Fatalf("gh arguments = %q, want %q", got, wantArguments)
	}
}

func TestOpsctlTagWorkflowPublishesThroughReleaseBuilder(t *testing.T) {
	// R-UL9F-YPQI
	project := releaseProjectRoot(t)
	repository, err := os.OpenRoot(filepath.Join(project, ".."))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	workflow, err := repository.ReadFile(".github/workflows/release-opsctl.yml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseReleaseWorkflow(workflow)
	if err != nil {
		t.Fatal(err)
	}
	want := []releaseWorkflowRecord{
		{indent: 0, key: "name", value: "Release opsctl"},
		{indent: 0, key: "on"},
		{indent: 2, key: "push"},
		{indent: 4, key: "tags", value: `["opsctl/v*"]`},
		{indent: 0, key: "permissions"},
		{indent: 2, key: "contents", value: "write"},
		{indent: 0, key: "jobs"},
		{indent: 2, key: "release"},
		{indent: 4, key: "runs-on", value: "ubuntu-latest"},
		{indent: 4, key: "steps"},
		{indent: 6, sequence: true, key: "name", value: "Check out repository"},
		{indent: 8, key: "uses", value: "actions/checkout@v5"},
		{indent: 6, sequence: true, key: "name", value: "Set up Go"},
		{indent: 8, key: "uses", value: "actions/setup-go@v6"},
		{indent: 8, key: "with"},
		{indent: 10, key: "go-version", value: `"1.26"`},
		{indent: 6, sequence: true, key: "name", value: "Build and publish release assets"},
		{indent: 8, key: "env"},
		{indent: 10, key: "GH_TOKEN", value: "${{ github.token }}"},
		{indent: 8, key: "run", value: `opsctl/release.sh "$GITHUB_REF_NAME"`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workflow structure = %#v, want %#v", got, want)
	}
	for _, mutation := range []struct {
		name string
		old  string
		new  string
	}{
		{name: "commented run", old: `        run: opsctl/release.sh "$GITHUB_REF_NAME"`, new: `        # run: opsctl/release.sh "$GITHUB_REF_NAME"`},
		{name: "misindented token", old: "          GH_TOKEN: ${{ github.token }}", new: "        GH_TOKEN: ${{ github.token }}"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			changed := strings.Replace(string(workflow), mutation.old, mutation.new, 1)
			if changed == string(workflow) {
				t.Fatalf("mutation target %q absent", mutation.old)
			}
			mutated, parseErr := parseReleaseWorkflow([]byte(changed))
			if parseErr == nil && reflect.DeepEqual(mutated, want) {
				t.Fatal("workflow mutation retained the required structure")
			}
		})
	}
}

func TestReleaseRejectsTagsThatCannotNameTheRuntimeVersion(t *testing.T) {
	// R-IH58-0PHG R-PSAO-ADAF
	project := releaseProjectRoot(t)
	version := runtimeVersion(t, project)
	dist := t.TempDir()
	fixtureBin := t.TempDir()
	called := filepath.Join(t.TempDir(), "gh-called")
	writeExecutable(t, filepath.Join(fixtureBin, "gh"), "#!/usr/bin/env bash\nprintf called > \"$GH_CALLED\"\nexit 0\n")
	command := exec.Command("bash", "release.sh", "opsctl/v9.8.7")
	command.Dir = project
	command.Env = append(os.Environ(), "PATH="+fixtureBin+":"+os.Getenv("PATH"), "GH_CALLED="+called, "GOPROXY=off", "OPSCTL_RELEASE_DIST="+dist)
	output, err := command.CombinedOutput()
	if exitCode(err) != 1 || string(output) != "tag opsctl/v9.8.7 names v9.8.7 but the binary reports "+version+"\n" {
		t.Fatalf("runtime mismatch = exit %d, output %q", exitCode(err), output)
	}
	assertReleaseNotInvokedAndDistEmpty(t, called, dist)
	malformedDist := t.TempDir()
	malformed := exec.Command("bash", "release.sh", "opsctl/v01.1.0")
	malformed.Dir = project
	malformed.Env = append(os.Environ(), "PATH="+fixtureBin+":"+os.Getenv("PATH"), "GH_CALLED="+called, "GOPROXY=off", "OPSCTL_RELEASE_DIST="+malformedDist)
	output, err = malformed.CombinedOutput()
	if exitCode(err) != 2 || string(output) != "invalid opsctl release tag: opsctl/v01.1.0\n" {
		t.Fatalf("malformed tag = exit %d, output %q", exitCode(err), output)
	}
	assertReleaseNotInvokedAndDistEmpty(t, called, malformedDist)
}

func TestReleasePropagatesBuildChecksumInstallAndPublishFailures(t *testing.T) {
	// R-UL9F-YPQI R-IKSX-60PJ
	project := releaseProjectRoot(t)
	version := runtimeVersion(t, project)
	binaryName := "opsctl-" + version + "-linux-amd64"
	for _, test := range []struct {
		name       string
		tool       string
		exit       int
		wantAssets []string
		wantGHCall bool
	}{
		{name: "build", tool: "go", exit: 41},
		{name: "checksum", tool: "sha256sum", exit: 42, wantAssets: []string{"checksums.txt", binaryName}},
		{name: "installer copy", tool: "install", exit: 43, wantAssets: []string{"checksums.txt", binaryName}},
		{name: "GitHub release", tool: "gh", exit: 44, wantAssets: []string{"checksums.txt", "install.sh", binaryName}, wantGHCall: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dist := t.TempDir()
			fixtureBin := t.TempDir()
			called := filepath.Join(t.TempDir(), "gh-called")
			writeExecutable(t, filepath.Join(fixtureBin, "gh"), "#!/usr/bin/env bash\nprintf called > \"$GH_CALLED\"\nexit 0\n")
			if test.tool == "gh" {
				writeExecutable(t, filepath.Join(fixtureBin, "gh"), "#!/usr/bin/env bash\nprintf called > \"$GH_CALLED\"\nexit 44\n")
			} else {
				writeExecutable(t, filepath.Join(fixtureBin, test.tool), "#!/usr/bin/env bash\nexit "+strconv.Itoa(test.exit)+"\n")
			}
			command := exec.Command("bash", "release.sh", "opsctl/"+version) //nolint:gosec // The tag is built from the version the source declares.
			command.Dir = project
			command.Env = append(os.Environ(), "PATH="+fixtureBin+":"+os.Getenv("PATH"), "GH_CALLED="+called, "GOPROXY=off", "OPSCTL_RELEASE_DIST="+dist)
			output, err := command.CombinedOutput()
			if exitCode(err) != test.exit || len(output) != 0 {
				t.Fatalf("release failure = exit %d output %q, want exit %d and empty output", exitCode(err), output, test.exit)
			}
			_, callErr := os.Stat(called)
			if got := callErr == nil; got != test.wantGHCall {
				t.Fatalf("gh invoked = %v, want %v (stat error %v)", got, test.wantGHCall, callErr)
			}
			entries, readErr := os.ReadDir(dist)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, test.wantAssets) {
				t.Fatalf("staged assets = %v, want %v", names, test.wantAssets)
			}
		})
	}
}

func TestReleasedBinaryAddsNoCommandOrOption(t *testing.T) {
	// R-UOX5-40YL
	project := releaseProjectRoot(t)
	command := exec.Command("go", "build", "-trimpath", "-o", "opsctl-release-test", "./cmd/opsctl")
	command.Dir = project
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build released binary: %v\n%s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Join(project, "opsctl-release-test")) })
	run := exec.Command("./opsctl-release-test", "release")
	run.Dir = project
	stdout, stderr := new(strings.Builder), new(strings.Builder)
	run.Stdout, run.Stderr = stdout, stderr
	err := run.Run()
	var exit *exec.ExitError
	want := "opsctl: unknown command 'release'\n\nsee 'opsctl --help' for usage\n"
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("opsctl release = %v stdout %q stderr %q", err, stdout.String(), stderr.String())
	}
}

func releaseProjectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeExecutable(t *testing.T, name, contents string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Chmod(name, 0o700); err != nil {
		t.Fatal(err)
	}
}

func assertLinuxAMD64Static(t *testing.T, name string) {
	t.Helper()
	file, err := elf.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if file.Machine != elf.EM_X86_64 {
		t.Fatalf("ELF machine = %v, want x86-64", file.Machine)
	}
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatalf("binary has dynamic interpreter")
		}
	}
	libraries, err := file.ImportedLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) != 0 {
		t.Fatalf("binary imports dynamic libraries: %q", libraries)
	}
}

func splitNULArguments(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

type releaseWorkflowRecord struct {
	indent   int
	sequence bool
	key      string
	value    string
}

func parseReleaseWorkflow(data []byte) ([]releaseWorkflowRecord, error) {
	var records []releaseWorkflowRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "\t") {
			return nil, errors.New("workflow contains a tab indentation")
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		sequence := strings.HasPrefix(trimmed, "- ")
		if sequence {
			trimmed = strings.TrimPrefix(trimmed, "- ")
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found || strings.TrimSpace(key) == "" {
			return nil, errors.New("workflow contains an invalid mapping entry")
		}
		records = append(records, releaseWorkflowRecord{indent: indent, sequence: sequence, key: strings.TrimSpace(key), value: strings.TrimSpace(value)})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func assertReleaseNotInvokedAndDistEmpty(t *testing.T, called, dist string) {
	t.Helper()
	if _, err := os.Stat(called); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gh invocation marker = %v", err)
	}
	entries, err := os.ReadDir(dist)
	if err != nil || len(entries) != 0 {
		t.Fatalf("validation failure artifacts = %v, %v", entries, err)
	}
}
