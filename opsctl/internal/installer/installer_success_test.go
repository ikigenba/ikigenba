package installer_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstallerFirstUpgradeAndRepeat(t *testing.T) {
	// R-UNP8-Q97W R-UQ51-HSPA R-URCX-VKFZ R-IOGM-BBXM
	// R-IPOI-P3OB R-IQWF-2VF0 R-2B6J-MTG1 R-USKU-9C6O
	// R-IUK4-86N3 R-IVS0-LYDS R-0I5P-90PU
	s := newSandbox(t)
	s.addRelease("v1.2.3")
	s.addRelease("v2.0.0")
	protectedBefore := snapshotTree(t, s.etc, s.opt)

	first := s.run(0, "/fixture/install.sh", "v1.2.3")
	assertSuccess(t, first, "v1.2.3")
	assertBytes(t, filepath.Join(s.usrLocal, "bin", "opsctl"), s.releaseData["v1.2.3/binary"])
	assertBytes(t, filepath.Join(s.usrLocal, "share", "ikigenba", "opsctl-install.sh"), s.installer)
	s.assertRootOwnershipAndExecutableMode("/usr/local/bin/opsctl")
	s.assertRootOwnershipAndExecutableMode("/usr/local/share/ikigenba/opsctl-install.sh")
	assertProtectedTree(t, protectedBefore, snapshotTree(t, s.etc, s.opt))
	assertEvents(t, s.curlEventLines(), releaseURLs("v1.2.3", false))
	assertInstalledSurface(t, s)

	upgrade := s.run(0, "/usr/local/share/ikigenba/opsctl-install.sh", "v2.0.0")
	assertSuccess(t, upgrade, "v2.0.0")
	assertBytes(t, filepath.Join(s.usrLocal, "bin", "opsctl"), s.releaseData["v2.0.0/binary"])
	assertBytes(t, filepath.Join(s.usrLocal, "share", "ikigenba", "opsctl-install.sh"), s.releaseData["v2.0.0/installer"])
	s.assertRootOwnershipAndExecutableMode("/usr/local/bin/opsctl")
	s.assertRootOwnershipAndExecutableMode("/usr/local/share/ikigenba/opsctl-install.sh")
	assertProtectedTree(t, protectedBefore, snapshotTree(t, s.etc, s.opt))
	assertEvents(t, s.curlEventLines(), append(releaseURLs("v1.2.3", false), releaseURLs("v2.0.0", true)...))

	binaryBefore := readFile(t, filepath.Join(s.usrLocal, "bin", "opsctl"))
	installerBefore := readFile(t, filepath.Join(s.usrLocal, "share", "ikigenba", "opsctl-install.sh"))
	repeat := s.run(0, "/usr/local/share/ikigenba/opsctl-install.sh", "v2.0.0")
	assertSuccess(t, repeat, "v2.0.0")
	assertBytes(t, filepath.Join(s.usrLocal, "bin", "opsctl"), binaryBefore)
	assertBytes(t, filepath.Join(s.usrLocal, "share", "ikigenba", "opsctl-install.sh"), installerBefore)
	assertProtectedTree(t, protectedBefore, snapshotTree(t, s.etc, s.opt))
	wantURLs := append(releaseURLs("v1.2.3", false), releaseURLs("v2.0.0", true)...)
	wantURLs = append(wantURLs, releaseURLs("v2.0.0", false)...)
	assertEvents(t, s.curlEventLines(), wantURLs)
	assertCandidateInvocations(t, s.eventText("candidate"))
	assertPublishRenames(t, s, s.eventText("mv"))
}

func TestInstallerValidatesBeforePublishing(t *testing.T) {
	t.Run("checksum", func(t *testing.T) {
		s := newSandbox(t)
		s.addRelease("v1.2.3")
		asset := filepath.Join(s.fixture, "releases", "opsctl", "v1.2.3", "opsctl-v1.2.3-linux-amd64")
		tampered := append(bytes.Clone(s.releaseData["v1.2.3/binary"]), []byte("\n# checksum tamper\n")...)
		if err := os.WriteFile(asset, tampered, 0o600); err != nil {
			t.Fatal(err)
		}

		result := s.run(0, "/fixture/install.sh", "v1.2.3")
		assertRejectedBeforePublish(t, s, result)
		assetName := "opsctl-v1.2.3-linux-amd64"
		expected := sha256.Sum256(s.releaseData["v1.2.3/binary"])
		actual := sha256.Sum256(tampered)
		wantStdout := "install: failed: checksum mismatch for " + assetName + "\n"
		wantStderr := fmt.Sprintf("opsctl-install: installation failed\n\nexpected %x\n     got %x\n", expected, actual)
		if result.stdout != wantStdout || result.stderr != wantStderr {
			t.Fatalf("checksum mismatch = stdout %q stderr %q, want stdout %q stderr %q", result.stdout, result.stderr, wantStdout, wantStderr)
		}
		if got := s.eventText("candidate"); got != "" {
			t.Fatalf("checksum mismatch executed candidate: %q", got)
		}
	})

	t.Run("version", func(t *testing.T) {
		s := newSandbox(t)
		s.addReleaseReporting("v1.2.3", "v9.9.9")

		result := s.run(0, "/fixture/install.sh", "v1.2.3")
		assertRejectedBeforePublish(t, s, result)
	})
}

func TestInstallerSandboxUsesExplicitNonRootUID(t *testing.T) {
	s := newSandbox(t)
	s.addRelease("v1.2.3")
	protectedBefore := snapshotTree(t, s.usrLocal, s.etc, s.opt)

	result := s.run(1000, "/fixture/install.sh", "v1.2.3")
	if result.code != 3 {
		t.Errorf("exit code = %d, want 3", result.code)
	}
	if result.stdout != "" {
		t.Errorf("stdout = %q, want empty", result.stdout)
	}
	if want := "opsctl-install: must run as root\n"; result.stderr != want {
		t.Errorf("stderr = %q, want %q", result.stderr, want)
	}
	assertProtectedTree(t, protectedBefore, snapshotTree(t, s.usrLocal, s.etc, s.opt))
	assertEvents(t, s.curlEventLines(), nil)
}

func assertSuccess(t *testing.T, result commandResult, version string) {
	t.Helper()
	if result.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", result.code, result.stdout, result.stderr)
	}
	if want := fmt.Sprintf("install: ok (opsctl %s -> /usr/local/bin/opsctl)\n", version); result.stdout != want {
		t.Errorf("stdout = %q, want %q", result.stdout, want)
	}
	if result.stderr != "" {
		t.Errorf("stderr = %q, want empty", result.stderr)
	}
}

func assertBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // Callers pass test-owned installation paths.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s content differs", path)
	}
}

func assertProtectedTree(t *testing.T, want map[string]treeEntry, got map[string]treeEntry) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("protected tree changed\nwant keys: %v\ngot keys:  %v", sortedKeys(want), sortedKeys(got))
	}
}

func assertEvents(t *testing.T, got []string, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func releaseURLs(version string, includeInstaller bool) []string {
	base := releasePrefix + "opsctl/" + version + "/"
	urls := []string{base + "checksums.txt", base + "opsctl-" + version + "-linux-amd64"}
	if includeInstaller {
		urls = append(urls, base+"install.sh")
	}
	return urls
}

func assertRejectedBeforePublish(t *testing.T, s *sandbox, result commandResult) {
	t.Helper()
	if result.code != 1 {
		t.Fatalf("exit code = %d, want 1\nstdout: %s\nstderr: %s", result.code, result.stdout, result.stderr)
	}
	if got := s.eventText("mv"); got != "" {
		t.Errorf("publication occurred: %q", got)
	}
	for _, path := range []string{
		filepath.Join(s.usrLocal, "bin", "opsctl"),
		filepath.Join(s.usrLocal, "share", "ikigenba", "opsctl-install.sh"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s exists before validation succeeded (stat error %v)", path, err)
		}
	}
}

func assertCandidateInvocations(t *testing.T, got string) {
	t.Helper()
	want := "" +
		"invoke v1.2.3 uid=0 argc=1 arg=version\n" +
		"invoke v2.0.0 uid=0 argc=1 arg=version\n" +
		"invoke v1.2.3 uid=0 argc=1 arg=version\n" +
		"invoke v2.0.0 uid=0 argc=1 arg=version\n" +
		"invoke v2.0.0 uid=0 argc=1 arg=version\n"
	if got != want {
		t.Errorf("candidate invocations = %q, want %q", got, want)
	}
}

func assertPublishRenames(t *testing.T, s *sandbox, got string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 5 {
		t.Fatalf("rename count = %d, want 5: %q", len(lines), got)
	}
	want := []struct {
		destination  string
		sourceDigest string
		priorDigest  string
	}{
		{destination: "/usr/local/bin/opsctl", sourceDigest: digestText(s.releaseData["v1.2.3/binary"]), priorDigest: "absent"},
		{destination: "/usr/local/share/ikigenba/opsctl-install.sh", sourceDigest: digestText(s.installer), priorDigest: "absent"},
		{destination: "/usr/local/bin/opsctl", sourceDigest: digestText(s.releaseData["v2.0.0/binary"]), priorDigest: digestText(s.releaseData["v1.2.3/binary"])},
		{destination: "/usr/local/share/ikigenba/opsctl-install.sh", sourceDigest: digestText(s.releaseData["v2.0.0/installer"]), priorDigest: digestText(s.installer)},
		{destination: "/usr/local/bin/opsctl", sourceDigest: digestText(s.releaseData["v2.0.0/binary"]), priorDigest: digestText(s.releaseData["v2.0.0/binary"])},
	}
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] != "mv" {
			t.Fatalf("rename %d malformed: %q", index, line)
		}
		source := strings.TrimPrefix(fields[1], "source=")
		destination := strings.TrimPrefix(fields[2], "destination=")
		sourceDigest := strings.TrimPrefix(fields[3], "source-sha256=")
		priorDigest := strings.TrimPrefix(fields[4], "destination-sha256=")
		if fields[1] == source || fields[2] == destination || fields[3] == sourceDigest || fields[4] == priorDigest {
			t.Fatalf("rename %d omitted labeled evidence: %q", index, line)
		}
		if destination != want[index].destination || source == destination || filepath.Dir(source) != filepath.Dir(destination) || !strings.HasPrefix(filepath.Base(source), ".opsctl-install.") {
			t.Errorf("rename %d paths = %q -> %q, want distinct hidden same-directory stage -> %q", index, source, destination, want[index].destination)
		}
		if sourceDigest != want[index].sourceDigest || priorDigest != want[index].priorDigest {
			t.Errorf("rename %d bytes = staged %q prior public %q, want %q and %q", index, sourceDigest, priorDigest, want[index].sourceDigest, want[index].priorDigest)
		}
	}
}

func digestText(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest)
}

func assertInstalledSurface(t *testing.T, s *sandbox) {
	t.Helper()
	surface := snapshotTree(t, s.usrLocal)
	wantKeys := []string{
		"usr-local/.",
		"usr-local/bin",
		"usr-local/bin/opsctl",
		"usr-local/share",
		"usr-local/share/ikigenba",
		"usr-local/share/ikigenba/opsctl-install.sh",
	}
	if got := sortedKeys(surface); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("persistent installation surface = %v, want %v", got, wantKeys)
	}
}
