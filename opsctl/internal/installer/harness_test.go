package installer_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const releasePrefix = "https://github.com/ikigenba/ikigenba/releases/download/"

type sandbox struct {
	t           *testing.T
	root        string
	fixture     string
	usrLocal    string
	etc         string
	opt         string
	tmp         string
	events      string
	installer   []byte
	hostNetNS   string
	releaseData map[string][]byte
}

type commandResult struct {
	stdout string
	stderr string
	code   int
}

type treeEntry struct {
	mode fs.FileMode
	data string
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()

	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Fatalf("bubblewrap is required: %v", err)
	}

	installerPath, err := filepath.Abs(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	installer, err := os.ReadFile(installerPath) //nolint:gosec // The path is a fixed repository-relative test input.
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}

	root := t.TempDir()
	s := &sandbox{
		t:           t,
		root:        root,
		fixture:     filepath.Join(root, "fixture"),
		usrLocal:    filepath.Join(root, "deployment", "usr-local"),
		etc:         filepath.Join(root, "deployment", "etc"),
		opt:         filepath.Join(root, "deployment", "opt"),
		tmp:         filepath.Join(root, "deployment", "tmp"),
		events:      filepath.Join(root, "events"),
		installer:   installer,
		releaseData: make(map[string][]byte),
	}
	for _, dir := range []string{s.fixture, s.usrLocal, s.etc, s.opt, s.tmp, s.events} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	hostNetNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatalf("read host network namespace: %v", err)
	}
	s.hostNetNS = hostNetNS
	s.writeFixture("install.sh", installer, 0o755)
	s.writeCurlFixture()
	s.writeChecksumFixture()
	s.writeInstallFixture()
	s.writeMoveFixture()
	s.seedDeploymentState()

	return s
}

func (s *sandbox) addRelease(version string) {
	s.addReleaseReporting(version, version)
}

func (s *sandbox) addReleaseReporting(version string, reportedVersion string) {
	s.t.Helper()

	asset := "opsctl-" + version + "-linux-amd64"
	binary := []byte(fmt.Sprintf(`#!/usr/bin/env bash
set -u
/usr/bin/printf 'invoke %s uid=%%s argc=%%s' "$EUID" "$#" >>"$INSTALL_TEST_EVENTS/candidate"
for argument in "$@"; do
  /usr/bin/printf ' arg=%%s' "$argument" >>"$INSTALL_TEST_EVENTS/candidate"
done
/usr/bin/printf '\n' >>"$INSTALL_TEST_EVENTS/candidate"
if [[ $# -ne 1 || $1 != version ]]; then
  exit 64
fi
if [[ -e /home || -e /root || -e /run || -w /usr ]]; then
  exit 65
fi
if [[ -n ${HOME+x} || -n ${AWS_SHARED_CREDENTIALS_FILE+x} ]]; then
  exit 66
fi
if [[ $(/usr/bin/readlink /proc/self/ns/net) == %q ]]; then
  exit 67
fi
/usr/bin/printf '%s\n'
`, version, s.hostNetNS, reportedVersion))
	digest := sha256.Sum256(binary)
	checksums := []byte(fmt.Sprintf("%x  %s\n", digest, asset))
	releaseInstaller := append(bytes.Clone(s.installer), []byte("\n# release fixture "+version+"\n")...)

	base := filepath.Join("releases", "opsctl", version)
	s.writeFixture(filepath.Join(base, asset), binary, 0o755)
	s.writeFixture(filepath.Join(base, "checksums.txt"), checksums, 0o644)
	s.writeFixture(filepath.Join(base, "install.sh"), releaseInstaller, 0o755)
	s.releaseData[version+"/binary"] = binary
	s.releaseData[version+"/installer"] = releaseInstaller
}

func (s *sandbox) run(uid int, installer string, arguments ...string) commandResult {
	s.t.Helper()

	args := []string{
		"--die-with-parent", "--new-session",
		"--unshare-user", "--uid", fmt.Sprint(uid), "--gid", fmt.Sprint(uid),
		"--unshare-pid", "--unshare-net",
		"--clearenv", "--setenv", "PATH", "/fixture/bin:/usr/bin",
		"--setenv", "INSTALL_TEST_EVENTS", "/events",
		"--setenv", "HOST_NET_NS", s.hostNetNS,
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib64", "/lib64",
		"--proc", "/proc", "--dev", "/dev", "--bind", s.tmp, "/tmp",
		"--dir", "/fixture", "--ro-bind", s.fixture, "/fixture",
		"--dir", "/events", "--bind", s.events, "/events",
		"--dir", "/etc", "--bind", s.etc, "/etc",
		"--dir", "/opt", "--bind", s.opt, "/opt",
		"--bind", s.usrLocal, "/usr/local",
		"--chdir", "/tmp",
		"/bin/bash", "-c", boundaryCommand, "sandbox-boundary", fmt.Sprint(uid), installer,
	}
	args = append(args, arguments...)
	cmd := exec.Command("bwrap", args...) //nolint:gosec // Arguments name only test-owned paths and fixed sandbox commands.
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			s.t.Fatalf("run bubblewrap: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

const boundaryCommand = `
expected_uid=$1
installer=$2
shift 2
if [[ $EUID -ne $expected_uid ]]; then
  printf 'sandbox: effective uid mismatch\n' >&2
  exit 125
fi
if [[ -e /home || -e /root || -e /run ]]; then
  printf 'sandbox: host path exposed\n' >&2
  exit 125
fi
if [[ -w /usr ]]; then
  printf 'sandbox: host tools are writable\n' >&2
  exit 125
fi
if [[ $(/usr/bin/readlink /proc/self/ns/net) == "$HOST_NET_NS" ]]; then
  printf 'sandbox: network namespace was not separated\n' >&2
  exit 125
fi
if [[ -f /fixture/control/stdout-full ]]; then
  exec /bin/bash "$installer" "$@" >/dev/full
fi
exec /bin/bash "$installer" "$@"
`

func (s *sandbox) assertRootOwnershipAndExecutableMode(path string) {
	s.t.Helper()
	command := `/usr/bin/stat -c '%u:%g %a' "$1"`
	args := s.inspectArgs(command, path)
	output, err := exec.Command("bwrap", args...).CombinedOutput() //nolint:gosec // Arguments are fixed except the test-owned path.
	if err != nil {
		s.t.Fatalf("inspect %s: %v\n%s", path, err, output)
	}
	if got, want := string(output), "0:0 755\n"; got != want {
		s.t.Errorf("metadata for %s = %q, want %q", path, got, want)
	}
}

func (s *sandbox) inspectArgs(command string, path string) []string {
	return []string{
		"--die-with-parent", "--unshare-user", "--uid", "0", "--gid", "0",
		"--unshare-pid", "--unshare-net", "--clearenv",
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib64", "/lib64",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--bind", s.usrLocal, "/usr/local",
		"/bin/bash", "-c", command, "inspect", path,
	}
}

func (s *sandbox) writeCurlFixture() {
	s.t.Helper()
	content := []byte(`#!/usr/bin/env bash
set -u
output=
url=
write_out=
while [[ $# -gt 0 ]]; do
  case "$1" in
    --fail|--location|--silent|--show-error) shift ;;
    --output) output=$2; shift 2 ;;
    --write-out) write_out=$2; shift 2 ;;
    *) url=$1; shift ;;
  esac
done
if [[ -z $output || $url != ` + releasePrefix + `* ]]; then
  exit 70
fi
relative=${url#` + releasePrefix + `}
/usr/bin/printf '%s\n' "$url" >>"$INSTALL_TEST_EVENTS/curl"
source=/fixture/releases/$relative
if [[ ! -f $source ]]; then
  /usr/bin/printf '404'
  /usr/bin/printf 'curl: (22) The requested URL returned error: 404\n' >&2
  exit 22
fi
/usr/bin/cp "$source" "$output"
/usr/bin/printf '200'
`)
	s.writeFixture(filepath.Join("bin", "curl"), content, 0o755)
}

func (s *sandbox) writeChecksumFixture() {
	s.t.Helper()
	content := []byte(`#!/usr/bin/env bash
set -u
if [[ -f /fixture/control/sha256sum-fail ]]; then
  /usr/bin/printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' "$1"
  /usr/bin/printf 'checksum fixture failed\nsecond line\n' >&2
  exit 71
fi
exec /usr/bin/sha256sum "$@"
`)
	s.writeFixture(filepath.Join("bin", "sha256sum"), content, 0o755)
}

func (s *sandbox) writeInstallFixture() {
	s.t.Helper()
	content := []byte(`#!/usr/bin/env bash
set -u
/usr/bin/printf 'install' >>"$INSTALL_TEST_EVENTS/install"
for argument in "$@"; do
  /usr/bin/printf ' %s' "$argument" >>"$INSTALL_TEST_EVENTS/install"
done
/usr/bin/printf '\n' >>"$INSTALL_TEST_EVENTS/install"
if [[ -f /fixture/control/install-fail ]] && [[ "$*" == *"$(/usr/bin/cat /fixture/control/install-fail)"* ]]; then
  /usr/bin/printf 'install fixture failed\nsecond line\n' >&2
  exit 71
fi
exec /usr/bin/install "$@"
`)
	s.writeFixture(filepath.Join("bin", "install"), content, 0o755)
}

func (s *sandbox) writeMoveFixture() {
	s.t.Helper()
	content := []byte(`#!/usr/bin/env bash
set -u
if [[ $# -ne 4 || $1 != -f || $2 != -- ]]; then
  exit 71
fi
source=$3
destination=$4
source_sum=$(/usr/bin/sha256sum "$source")
source_sum=${source_sum%% *}
destination_sum=absent
if [[ -e $destination ]]; then
  destination_sum=$(/usr/bin/sha256sum "$destination")
  destination_sum=${destination_sum%% *}
fi
/usr/bin/printf 'mv source=%s destination=%s source-sha256=%s destination-sha256=%s\n' \
  "$source" "$destination" "$source_sum" "$destination_sum" >>"$INSTALL_TEST_EVENTS/mv"
exec /usr/bin/mv "$@"
`)
	s.writeFixture(filepath.Join("bin", "mv"), content, 0o755)
}

func (s *sandbox) resetEvents() {
	s.t.Helper()
	entries, err := os.ReadDir(s.events)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(s.events, entry.Name())); err != nil {
			s.t.Fatal(err)
		}
	}
}

func (s *sandbox) writeFixture(relative string, data []byte, mode fs.FileMode) {
	s.t.Helper()
	path := filepath.Join(s.fixture, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil { //nolint:gosec // path stays beneath the test's temporary fixture root.
		s.t.Fatal(err)
	}
}

func (s *sandbox) seedDeploymentState() {
	s.t.Helper()
	files := map[string]string{
		filepath.Join(s.etc, "ikigenba", "config.toml"):          "existing config\n",
		filepath.Join(s.etc, "nginx", "sites-enabled", "app"):    "existing nginx\n",
		filepath.Join(s.etc, "systemd", "system", "app.service"): "existing unit\n",
		filepath.Join(s.opt, "ikigenba", "state"):                "existing application state\n",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			s.t.Fatal(err)
		}
	}
}

func (s *sandbox) curlEventLines() []string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.events, "curl")) //nolint:gosec // s.events is a test-owned temporary directory.
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.Fields(strings.TrimSpace(string(data)))
}

func (s *sandbox) eventText(name string) string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.events, name)) //nolint:gosec // name is a controlled test event filename.
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

func snapshotTree(t *testing.T, roots ...string) map[string]treeEntry {
	t.Helper()
	result := make(map[string]treeEntry)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			key := filepath.Base(root) + "/" + relative
			item := treeEntry{mode: info.Mode()}
			if info.Mode().IsRegular() {
				data, err := os.ReadFile(path) //nolint:gosec // WalkDir supplies paths beneath test-owned roots.
				if err != nil {
					return err
				}
				item.data = string(data)
			}
			result[key] = item
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return result
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // Callers pass test-owned installation paths.
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sortedKeys(values map[string]treeEntry) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
