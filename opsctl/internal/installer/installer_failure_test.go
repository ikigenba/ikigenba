package installer_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerOperandAndRootRefusalsHaveNoEffects(t *testing.T) {
	// R-UTSQ-N3XD R-IZFP-R9LV R-2G25-5WET
	for _, test := range []struct {
		name       string
		uid        int
		arguments  []string
		wantCode   int
		wantStderr string
	}{
		{name: "missing version", uid: 0, wantCode: 2, wantStderr: "opsctl-install: needs a version\n"},
		{name: "non-root", uid: 1000, arguments: []string{"v1.2.3"}, wantCode: 3, wantStderr: "opsctl-install: must run as root\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newSandbox(t)
			s.addRelease("v1.2.3")
			before := snapshotTree(t, s.usrLocal, s.etc, s.opt, s.tmp)
			result := s.run(test.uid, "/fixture/install.sh", test.arguments...)
			if result.code != test.wantCode || result.stdout != "" || result.stderr != test.wantStderr {
				t.Fatalf("result = code %d stdout %q stderr %q", result.code, result.stdout, result.stderr)
			}
			assertProtectedTree(t, before, snapshotTree(t, s.usrLocal, s.etc, s.opt, s.tmp))
			assertEvents(t, s.curlEventLines(), nil)
		})
	}
}

func TestInstallerMissingReleaseReports404WithoutPublishing(t *testing.T) {
	// R-2CEG-0L6Q R-2G25-5WET
	for _, existing := range []bool{false, true} {
		name := "fresh"
		if existing {
			name = "existing installation"
		}
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			installer := "/fixture/install.sh"
			if existing {
				s.addRelease("v1.2.3")
				assertSuccess(t, s.run(0, installer, "v1.2.3"), "v1.2.3")
				installer = "/usr/local/share/ikigenba/opsctl-install.sh"
				s.resetEvents()
			}
			before := snapshotTree(t, s.usrLocal, s.etc, s.opt)
			result := s.run(0, installer, "v4.5.6")
			url := releasePrefix + "opsctl/v4.5.6/checksums.txt"
			assertFailureResult(t, result,
				"install: failed: no release for v4.5.6\n",
				"opsctl-install: installation failed\n\n"+url+": 404\n")
			assertProtectedTree(t, before, snapshotTree(t, s.usrLocal, s.etc, s.opt))
			assertEvents(t, s.curlEventLines(), []string{url})
			assertNoInstallerTemporaries(t, s)
		})
	}
}

func TestInstallerChecksumMismatchPreservesInstalledFiles(t *testing.T) {
	// R-2DMC-ECXF R-2G25-5WET
	s := installedSandbox(t)
	s.addRelease("v2.0.0")
	asset := "opsctl-v2.0.0-linux-amd64"
	assetPath := filepath.Join(s.fixture, "releases", "opsctl", "v2.0.0", asset)
	corrupt := []byte("corrupt candidate\n")
	if err := os.WriteFile(assetPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, s.usrLocal, s.etc, s.opt)
	expectedLine := strings.TrimSpace(string(readFile(t, filepath.Join(filepath.Dir(assetPath), "checksums.txt"))))
	expected := strings.Fields(expectedLine)[0]
	actual := fmt.Sprintf("%x", sha256.Sum256(corrupt))
	s.resetEvents()

	result := s.run(0, "/usr/local/share/ikigenba/opsctl-install.sh", "v2.0.0")
	assertFailureResult(t, result,
		"install: failed: checksum mismatch for "+asset+"\n",
		"opsctl-install: installation failed\n\nexpected "+expected+"\n     got "+actual+"\n")
	assertProtectedTree(t, before, snapshotTree(t, s.usrLocal, s.etc, s.opt))
	if got := s.eventText("candidate"); got != "" {
		t.Fatalf("checksum mismatch executed candidate: %q", got)
	}
	if got := s.eventText("mv"); got != "" {
		t.Fatalf("checksum mismatch published files: %q", got)
	}
	assertNoInstallerTemporaries(t, s)
}

func TestInstallerVersionMismatchPreservesInstalledFiles(t *testing.T) {
	// R-2EU8-S4O4 R-2G25-5WET
	s := installedSandbox(t)
	s.addReleaseReporting("v2.0.0", "v9.9.9")
	before := snapshotTree(t, s.usrLocal, s.etc, s.opt)
	s.resetEvents()

	result := s.run(0, "/usr/local/share/ikigenba/opsctl-install.sh", "v2.0.0")
	assertFailureResult(t, result,
		"install: failed: asked for v2.0.0 but the binary reports v9.9.9\n",
		"opsctl-install: installation failed\n")
	assertProtectedTree(t, before, snapshotTree(t, s.usrLocal, s.etc, s.opt))
	if got := s.eventText("candidate"); got != "invoke v2.0.0 uid=0 argc=1 arg=version\n" {
		t.Fatalf("candidate invocation = %q", got)
	}
	if got := s.eventText("mv"); got != "" {
		t.Fatalf("version mismatch published files: %q", got)
	}
	assertNoInstallerTemporaries(t, s)
}

func TestInstallerEscapesCRAndLFInFailureOutcome(t *testing.T) {
	// R-2G25-5WET
	s := newSandbox(t)
	s.addReleaseReporting("v2.0.0", "bad\r\nvalue")
	result := s.run(0, "/fixture/install.sh", "v2.0.0")
	assertFailureResult(t, result,
		"install: failed: asked for v2.0.0 but the binary reports bad\\r\\nvalue\n",
		"opsctl-install: installation failed\n")
	if got := s.eventText("mv"); got != "" {
		t.Fatalf("multiline failure published files: %q", got)
	}
	assertNoInstallerTemporaries(t, s)
}

func TestInstallerOutcomeWriteFailureReportsOnce(t *testing.T) {
	// R-2G25-5WET
	s := installedSandbox(t)
	s.addRelease("v2.0.0")
	if err := os.WriteFile(filepath.Join(s.fixture, "releases", "opsctl", "v2.0.0", "checksums.txt"), []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.writeFixture(filepath.Join("control", "stdout-full"), nil, 0o644)
	before := snapshotTree(t, s.usrLocal, s.etc, s.opt)
	s.resetEvents()

	result := s.run(0, "/usr/local/share/ikigenba/opsctl-install.sh", "v2.0.0")
	if result.code != 1 || result.stdout != "" || result.stderr != "opsctl-install: installation failed\n" {
		t.Fatalf("result = code %d stdout %q stderr %q", result.code, result.stdout, result.stderr)
	}
	assertProtectedTree(t, before, snapshotTree(t, s.usrLocal, s.etc, s.opt))
	if got := s.eventText("mv"); got != "" {
		t.Fatalf("outcome failure published files: %q", got)
	}
	if got := s.eventText("candidate"); got != "" {
		t.Fatalf("malformed checksum executed candidate: %q", got)
	}
	assertNoInstallerTemporaries(t, s)
}

func TestInstallerOperationalFailuresReportOnceAndCleanUp(t *testing.T) {
	// R-2G25-5WET
	for _, test := range []struct {
		name       string
		prepare    func(*testing.T, *sandbox)
		wantReason string
		wantDetail string
	}{
		{
			name: "binary download", wantReason: "could not download opsctl-v2.0.0-linux-amd64",
			prepare: func(t *testing.T, s *sandbox) {
				t.Helper()
				s.addRelease("v2.0.0")
				if err := os.Remove(filepath.Join(s.fixture, "releases", "opsctl", "v2.0.0", "opsctl-v2.0.0-linux-amd64")); err != nil {
					t.Fatal(err)
				}
			},
			wantDetail: "> curl: (22) The requested URL returned error: 404\n",
		},
		{
			name: "checksum entry", wantReason: "no checksum for opsctl-v2.0.0-linux-amd64",
			prepare: func(t *testing.T, s *sandbox) {
				t.Helper()
				s.addRelease("v2.0.0")
				if err := os.WriteFile(filepath.Join(s.fixture, "releases", "opsctl", "v2.0.0", "checksums.txt"), []byte("invalid\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "checksum command", wantReason: "could not checksum opsctl-v2.0.0-linux-amd64",
			prepare: func(t *testing.T, s *sandbox) {
				t.Helper()
				s.addRelease("v2.0.0")
				s.writeFixture(filepath.Join("control", "sha256sum-fail"), nil, 0o644)
			},
			wantDetail: "> checksum fixture failed\n> second line\n",
		},
		{
			name: "candidate execution", wantReason: "opsctl-v2.0.0-linux-amd64 version command failed",
			prepare: func(t *testing.T, s *sandbox) {
				t.Helper()
				s.addFailingRelease("v2.0.0")
			},
			wantDetail: "> candidate failed\n> more detail\n",
		},
		{
			name: "filesystem staging", wantReason: "could not stage opsctl",
			prepare: func(t *testing.T, s *sandbox) {
				t.Helper()
				s.addRelease("v2.0.0")
				s.writeFixture(filepath.Join("control", "install-fail"), []byte("/usr/local/bin/.opsctl-install."), 0o644)
			},
			wantDetail: "> install fixture failed\n> second line\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := installedSandbox(t)
			test.prepare(t, s)
			before := snapshotTree(t, s.usrLocal, s.etc, s.opt)
			s.resetEvents()
			result := s.run(0, "/usr/local/share/ikigenba/opsctl-install.sh", "v2.0.0")
			assertFailureResult(t, result,
				"install: failed: "+test.wantReason+"\n",
				"opsctl-install: installation failed\n"+conditionalFailureDetail(test.wantDetail))
			assertProtectedTree(t, before, snapshotTree(t, s.usrLocal, s.etc, s.opt))
			if got := s.eventText("mv"); got != "" {
				t.Fatalf("failure published files: %q", got)
			}
			switch test.name {
			case "checksum entry", "checksum command":
				if got := s.eventText("candidate"); got != "" {
					t.Fatalf("%s failure executed candidate: %q", test.name, got)
				}
			case "filesystem staging":
				want := "invoke v2.0.0 uid=0 argc=1 arg=version\n" +
					"invoke v1.2.3 uid=0 argc=1 arg=version\n"
				if got := s.eventText("candidate"); got != want {
					t.Fatalf("filesystem staging candidate validation = %q, want %q", got, want)
				}
			}
			assertNoInstallerTemporaries(t, s)
		})
	}
}

func installedSandbox(t *testing.T) *sandbox {
	t.Helper()
	s := newSandbox(t)
	s.addRelease("v1.2.3")
	result := s.run(0, "/fixture/install.sh", "v1.2.3")
	assertSuccess(t, result, "v1.2.3")
	assertNoInstallerTemporaries(t, s)
	return s
}

func (s *sandbox) addFailingRelease(version string) {
	s.t.Helper()
	asset := "opsctl-" + version + "-linux-amd64"
	binary := []byte("#!/usr/bin/env bash\nprintf 'candidate failed\\nmore detail\\n' >&2\nexit 72\n")
	digest := sha256.Sum256(binary)
	base := filepath.Join("releases", "opsctl", version)
	s.writeFixture(filepath.Join(base, asset), binary, 0o755)
	s.writeFixture(filepath.Join(base, "checksums.txt"), []byte(fmt.Sprintf("%x  %s\n", digest, asset)), 0o644)
	s.writeFixture(filepath.Join(base, "install.sh"), s.installer, 0o755)
}

func assertFailureResult(t *testing.T, result commandResult, stdout, stderr string) {
	t.Helper()
	if result.code != 1 || result.stdout != stdout || result.stderr != stderr {
		t.Fatalf("result = code %d stdout %q stderr %q; want code 1 stdout %q stderr %q", result.code, result.stdout, result.stderr, stdout, stderr)
	}
	if strings.Count(result.stdout, "install: failed:") != 1 || strings.Count(result.stderr, "opsctl-install: installation failed") != 1 {
		t.Fatalf("failure outcome was not singular: stdout %q stderr %q", result.stdout, result.stderr)
	}
}

func conditionalFailureDetail(detail string) string {
	if detail == "" {
		return ""
	}
	return "\n" + detail
}

func assertNoInstallerTemporaries(t *testing.T, s *sandbox) {
	t.Helper()
	entries, err := os.ReadDir(s.tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary directory retained entries: %v", entries)
	}
	err = filepath.WalkDir(s.usrLocal, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.HasPrefix(entry.Name(), ".opsctl-install.") {
			t.Errorf("staged installer file remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
