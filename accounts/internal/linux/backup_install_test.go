package linux_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestBackupInstallConstants(t *testing.T) {
	// R-Y0O9-WPDB R-Y1W6-AH40 R-Y342-O8UP
	if linux.MaxRedirects != 5 || linux.MaxInstallBytes != 64<<20 ||
		linux.InstallTimeout != 10*time.Minute || linux.InstallRetry != time.Hour ||
		linux.ResticPath != "/usr/local/bin/restic" || linux.LitestreamPath != "/usr/local/bin/litestream" ||
		linux.BackupDir != "/var/lib/accountsd/backup" || linux.ResticCache != "/var/cache/accountsd/restic" ||
		linux.LitestreamUnit != "accountsd-litestream@.service" || linux.CheckSubset != "10%" {
		t.Fatal("backup install constants differ from the declared values")
	}
	if linux.ReadBatch != 256 || linux.MaxDepth != 8 || linux.MaxVisit != 100000 ||
		linux.MaxDatabasesPerAccount != 100 || linux.RestoreAttempts != 3 ||
		linux.FinalSyncTimeout != 2*time.Minute || linux.ShutdownSyncTimeout != 30*time.Second ||
		linux.RestartBefore != 20*time.Minute || linux.RestoreMarker != "/data/.accountsd-restore" {
		t.Fatal("backup pass constants differ from the declared values")
	}
	for _, version := range []string{linux.ResticVersion, linux.LitestreamVersion} {
		if !regexp.MustCompile(`^[0-9.]+$`).MatchString(version) {
			t.Fatalf("invalid pinned version %q", version)
		}
	}
	for _, digest := range []string{linux.ResticDigest, linux.LitestreamDigest} {
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(digest) {
			t.Fatalf("invalid pinned digest %q", digest)
		}
	}
}

type archiveEntry struct{ name, body string }

func backupTar(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tarOut := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tarOut.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tarOut, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(tarOut.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

type backupInstaller func(context.Context, linux.Env, string, string) error

const fixtureVersion = "1.2.3"

func TestInstallBackupPrograms(t *testing.T) {
	// R-Z64U-WWKX R-CJHS-YRMB R-CLXL-QB3P R-CI9W-KZVM
	const resticELF = "\x7fELFrestic-test\n"
	const streamELF = "\x7fELFlitestream-test\n"
	const resticArchive = "QlpoOTFBWSZTWedO3sMAAAfVgAAQAAIDBAogHACgADEDQNAgAMgs5fUG4ISx8XckU4UJDnTt7DA="
	restic, err := base64.StdEncoding.DecodeString(resticArchive)
	if err != nil {
		t.Fatal(err)
	}
	stream := backupTar(t, archiveEntry{"etc/litestream.yml", "skip"}, archiveEntry{"litestream", streamELF})
	for _, tc := range []struct {
		path, stamp, url, executable string
		fetchBytes                   []byte
		install                      backupInstaller
	}{
		{linux.ResticPath, "restic.digest", "https://github.com/restic/restic/releases/download/v" + fixtureVersion +
			"/restic_" + fixtureVersion + "_linux_arm64.bz2", resticELF, restic, linux.InstallRestic},
		{linux.LitestreamPath, "litestream.digest", "https://github.com/benbjohnson/litestream/releases/download/v" + fixtureVersion +
			"/litestream-" + fixtureVersion + "-linux-arm64.tar.gz", streamELF, stream, linux.InstallLitestream},
	} {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			f := newHardeningFixture(t)
			stamp := filepath.Join(linux.BackupDir, tc.stamp)
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(tc.fetchBytes))
			fetches := 0
			f.env.Fetch = func(ctx context.Context, url string) (io.ReadCloser, error) {
				fetches++
				deadline, ok := ctx.Deadline()
				if url != tc.url || !ok || deadline.After(time.Now().Add(linux.InstallTimeout)) {
					t.Fatalf("fetch called with %q, deadline %v (set %t)", url, deadline, ok)
				}
				return io.NopCloser(bytes.NewReader(tc.fetchBytes)), nil
			}
			wrong := digest[:len(digest)-1] + string('0'+(digest[len(digest)-1]-'0'+1)%10)
			if err := tc.install(t.Context(), f.env, fixtureVersion, wrong); !errors.Is(err, machine.ErrDigestMismatch) {
				t.Fatalf("install with wrong digest = %v", err)
			}
			f.expectAbsent(tc.path)
			f.expectAbsent(stamp)
			if fetches != 1 {
				t.Fatalf("fetches after failed install = %d", fetches)
			}
			if err := tc.install(t.Context(), f.env, fixtureVersion, digest); err != nil {
				t.Fatal(err)
			}
			f.expectFile(tc.path, tc.executable, 0o755)
			f.expectFile(stamp, digest+"\n", 0o600)
			f.put(tc.path, "old executable", 0o755)
			f.put(stamp, "old digest\n", 0o600)
			watch := watchBackupProgramDirectory(t, f.path(filepath.Dir(tc.path)))
			if err := tc.install(t.Context(), f.env, fixtureVersion, digest); err != nil {
				t.Fatal(err)
			}
			assertBackupProgramDirectoryWrites(t, watch, filepath.Base(tc.path))
			f.expectFile(tc.path, tc.executable, 0o755)
			f.expectFile(stamp, digest+"\n", 0o600)
			f.expectCalls()
			before := snapshotHost(t, f.root)
			if err := tc.install(t.Context(), f.env, fixtureVersion, digest); err != nil {
				t.Fatal(err)
			}
			if fetches != 3 {
				t.Fatalf("fetches after checking pinned install = %d", fetches)
			}
			if after := snapshotHost(t, f.root); !reflect.DeepEqual(after, before) {
				t.Fatal("checking the installed pin changed a host path")
			}
			backupDirs := map[string]string{linux.BackupDir: tc.stamp, filepath.Dir(tc.path): filepath.Base(tc.path)}
			for dir, want := range backupDirs {
				entries, err := os.ReadDir(f.path(dir))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.Name() != want && entry.Name() != filepath.Base(linux.DaemonPath) {
						t.Fatalf("temporary file remained in %s: %v", dir, entry)
					}
				}
			}
		})
	}
}

func watchBackupProgramDirectory(t *testing.T, path string) int {
	t.Helper()
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(watch) })
	if _, err := syscall.InotifyAddWatch(watch, path, syscall.IN_CREATE|syscall.IN_DELETE|syscall.IN_MOVED_FROM|syscall.IN_MOVED_TO|syscall.IN_ATTRIB|syscall.IN_MODIFY); err != nil {
		t.Fatal(err)
	}
	return watch
}

func assertBackupProgramDirectoryWrites(t *testing.T, watch int, program string) {
	t.Helper()
	temp := "." + program + ".new"
	var buffer [4096]byte
	var sawTemp, sawProgram bool
	for {
		n, err := syscall.Read(watch, buffer[:])
		if errors.Is(err, syscall.EAGAIN) {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || n < syscall.SizeofInotifyEvent {
			t.Fatalf("read executable directory events: n=%d err=%v", n, err)
		}
		for events := buffer[:n]; len(events) >= syscall.SizeofInotifyEvent; {
			end := syscall.SizeofInotifyEvent + int(binary.NativeEndian.Uint32(events[12:16]))
			if end > len(events) {
				t.Fatal("truncated executable directory event")
			}
			name := string(bytes.TrimRight(events[syscall.SizeofInotifyEvent:end], "\x00"))
			switch name {
			case temp:
				sawTemp = true
			case program:
				sawProgram = true
			default:
				t.Errorf("unexpected temporary entry %q next to %s", name, program)
			}
			events = events[end:]
		}
	}
	if !sawTemp || !sawProgram {
		t.Errorf("expected filesystem events at %s and %s", temp, program)
	}
}

func TestInstallLitestreamFailurePreservesInstalledFiles(t *testing.T) {
	// R-CLXL-QB3P
	for _, tc := range []struct {
		name, archive string
		member        archiveEntry
		want          error
	}{
		{name: "wrong directory", member: archiveEntry{"bin/litestream", "\x7fELFbinary"}, want: machine.ErrNotExecutable},
		{name: "wrong magic", member: archiveEntry{"litestream", "plain file"}, want: machine.ErrNotExecutable},
		{name: "invalid archive", archive: "not gzip"},
		{name: "stamp collision", member: archiveEntry{"litestream", "\x7fELFbinary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put(linux.LitestreamPath, "old program", 0o755)
			stamp := linux.BackupDir + "/litestream.digest"
			if tc.name == "stamp collision" {
				if err := os.MkdirAll(f.path(stamp), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				f.put(stamp, "old digest\n", 0o600)
			}
			before := snapshotHost(t, f.root)
			archive := []byte(tc.archive)
			if tc.member.name != "" {
				archive = backupTar(t, tc.member)
			}
			f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(archive)), nil
			}
			err := linux.InstallLitestream(t.Context(), f.env, fixtureVersion, fmt.Sprintf("sha256:%x", sha256.Sum256(archive)))
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("install error = %v, want %v", err, tc.want)
			}
			if after := snapshotHost(t, f.root); !reflect.DeepEqual(after, before) {
				t.Fatal("failed install changed a host path")
			}
		})
	}
}

func TestInstallBackupProgramsCapDownload(t *testing.T) {
	// R-CJHS-YRMB R-CLXL-QB3P
	for _, install := range []backupInstaller{linux.InstallRestic, linux.InstallLitestream} {
		f := newHardeningFixture(t)
		over := new(endlessBytes)
		f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(over), nil
		}
		err := install(t.Context(), f.env, fixtureVersion, strings.Repeat("x", 64))
		if !errors.Is(err, machine.ErrTooLarge) || over.read != linux.MaxInstallBytes+1 {
			t.Fatalf("install error = %v after %d bytes", err, over.read)
		}
		f.expectAbsent(linux.ResticPath)
		f.expectAbsent(linux.LitestreamPath)
		entries, err := os.ReadDir(f.path(linux.BackupDir))
		if err != nil || len(entries) != 0 {
			t.Fatalf("install leftovers = %v, %v", entries, err)
		}
	}
}

func TestInstallResticRejectsMalformedOutput(t *testing.T) {
	// R-CJHS-YRMB
	badRead := errors.New("fixture read failure")
	for _, tc := range []struct {
		name, bzip string
		want       error
	}{
		{name: "not bzip2", bzip: "bm90IGJ6aXAy"},
		{name: "wrong magic", bzip: "QlpoOTFBWSZTWVZOeZEAAAWRgEAAOgWGQCAAMQDTTQQDTRppiaxbZAPJX4u5IpwoSCsnPMiA", want: machine.ErrNotExecutable},
		// The ELF header followed by zeros, totaling MaxInstallBytes + 1 bytes.
		{name: "expanded size", bzip: "QlpoOTFBWSZTWbsEMF4FfjnEgMAAAwQAAIAIIABQYAJqoZO6RUBLtJJBVW7uZQqAl8xQVkmU1m6SxevAKJ0QADAAAAIIAAwgCkYSohNBKiE4u5IpwoSGYhu4kA==", want: machine.ErrTooLarge},
		{name: "failed read", want: badRead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive, err := base64.StdEncoding.DecodeString(tc.bzip)
			if err != nil {
				t.Fatal(err)
			}
			var input io.Reader = bytes.NewReader(archive)
			if errors.Is(tc.want, badRead) {
				input = io.MultiReader(input, failingKeyReader{err: badRead})
			}
			assertRejectedBackupInstall(t, linux.InstallRestic, linux.ResticPath, linux.BackupDir+"/restic.digest", input, archive, tc.want)
		})
	}
}

func TestInstallLitestreamValidatesExpandedSizeAndGzipFooter(t *testing.T) {
	// R-CLXL-QB3P
	archive := backupTar(t, archiveEntry{"litestream", "\x7fELFbinary"}, archiveEntry{"after", "more archive entries"})
	archive[len(archive)-8] ^= 0xff
	assertRejectedBackupInstall(t, linux.InstallLitestream, linux.LitestreamPath, linux.BackupDir+"/litestream.digest",
		bytes.NewReader(archive), archive, gzip.ErrChecksum)

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tarOut := tar.NewWriter(gz)
	const tooLarge = linux.MaxInstallBytes + 1
	if err := tarOut.WriteHeader(&tar.Header{Name: "litestream", Mode: 0o755, Size: tooLarge}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tarOut, "\x7fELF"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(tarOut, new(endlessBytes), tooLarge-4); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(tarOut.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	assertRejectedBackupInstall(t, linux.InstallLitestream, linux.LitestreamPath, linux.BackupDir+"/litestream.digest",
		bytes.NewReader(compressed.Bytes()), compressed.Bytes(), machine.ErrTooLarge)
}

func assertRejectedBackupInstall(t *testing.T, install backupInstaller, path, stamp string, body io.Reader, archive []byte, want error) {
	t.Helper()
	f := newHardeningFixture(t)
	f.put(path, "previous executable", 0o755)
	f.put(stamp, "previous stamp\n", 0o600)
	before := snapshotHost(t, f.root)
	f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(body), nil
	}
	err := install(t.Context(), f.env, fixtureVersion, fmt.Sprintf("sha256:%x", sha256.Sum256(archive)))
	if err == nil || (want != nil && !errors.Is(err, want)) {
		t.Fatalf("install error = %v, want %v", err, want)
	}
	if after := snapshotHost(t, f.root); !reflect.DeepEqual(after, before) {
		t.Fatal("failed install changed a host path")
	}
}

func TestInstallBackupProgramsRecoverInterruptedPublication(t *testing.T) {
	// R-CJHS-YRMB R-CLXL-QB3P
	for _, tc := range []struct {
		path    string
		install backupInstaller
	}{
		{linux.ResticPath, linux.InstallRestic},
		{linux.LitestreamPath, linux.InstallLitestream},
	} {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			testBackupPublishRecovery(t, tc.path, tc.install)
		})
	}
}

func testBackupPublishRecovery(t *testing.T, program string, install backupInstaller) {
	t.Helper()
	staged := filepath.Join(filepath.Dir(program), "."+filepath.Base(program)+".new")
	stamp := filepath.Join(linux.BackupDir, filepath.Base(program)+".digest")
	const newStamp = linux.BackupDir + "/.fixture-digest"
	const exchange = linux.BackupDir + "/.fixture-exchange"
	record := stamp + ".install"
	const oldDigest, newDigest = "old digest", "new digest"
	for _, outcome := range []string{"prepared", "rollback", "committed", "unexpected program", "unexpected stamp", "stamp edited in place"} {
		t.Run(outcome, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.put(program, "previous executable", 0o755)
			f.put(stamp, oldDigest+"\n", 0o600)
			f.put(staged, "checked executable", 0o755)
			f.put(newStamp, newDigest+"\n", 0o600)

			stampTime := time.Unix(1_000_000, 0)
			setStampTime := func() {
				t.Helper()
				if err := os.Chtimes(f.path(stamp), stampTime, stampTime); err != nil {
					t.Fatal(err)
				}
			}
			setStampTime()
			type entry struct {
				Device, Inode uint64
				Size          int64
				Modified      syscall.Timespec
				Mode          uint32
				Fingerprint   string
			}
			info := func(path string, stamp bool) entry {
				t.Helper()
				stat, ok := f.stat(path).Sys().(*syscall.Stat_t)
				if !ok {
					t.Fatalf("missing file identity for %s", path)
				}
				got := entry{Device: stat.Dev, Inode: stat.Ino, Size: stat.Size, Modified: stat.Mtim, Mode: stat.Mode}
				if stamp {
					data, err := os.ReadFile(f.path(path))
					if err != nil {
						t.Fatal(err)
					}
					got.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(data))
				}
				return got
			}
			journal, err := json.Marshal(struct{ OldProgram, NewProgram, OldStamp, NewStamp entry }{
				info(program, false), info(staged, false), info(stamp, true), info(newStamp, true),
			})
			if err != nil {
				t.Fatal(err)
			}
			f.put(record, string(journal), 0o600)
			rename := func(source, dest string) {
				t.Helper()
				if err := os.Rename(f.path(source), f.path(dest)); err != nil {
					t.Fatal(err)
				}
			}
			if outcome != "prepared" {
				rename(program, exchange)
				rename(staged, program)
				rename(exchange, staged)
			}
			fetches := 0
			f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
				fetches++
				return nil, errors.New("recovery must precede downloads")
			}
			wantDigest, wantProgram := oldDigest, "previous executable"
			switch outcome {
			case "committed":
				rename(newStamp, stamp)
				wantDigest, wantProgram = newDigest, "checked executable"
			case "unexpected program":
				rename(program, exchange)
				f.put(program, "external replacement", 0o755)
				if err := install(t.Context(), f.env, fixtureVersion, oldDigest); err == nil {
					t.Fatal("installed over an unexpected program")
				}
				f.expectFile(staged, "previous executable", 0o755)
				f.expectFile(program, "external replacement", 0o755)
				rename(exchange, program)
			case "unexpected stamp":
				saved := stamp + ".saved"
				rename(stamp, saved)
				f.put(stamp, "external stamp", 0o600)
				if err := install(t.Context(), f.env, fixtureVersion, oldDigest); err == nil {
					t.Fatal("installed over an unexpected stamp")
				}
				f.expectFile(staged, "previous executable", 0o755)
				f.expectFile(stamp, "external stamp", 0o600)
				rename(saved, stamp)
			case "stamp edited in place":
				before := f.stat(stamp)
				f.put(stamp, newDigest+"\n", 0o600)
				setStampTime()
				after := f.stat(stamp)
				if !os.SameFile(before, after) || before.Size() != after.Size() ||
					!before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
					t.Fatal("stamp fixture changed file metadata")
				}
				if err := install(t.Context(), f.env, fixtureVersion, newDigest); err == nil {
					t.Fatal("accepted an edit to the stamp")
				}
				f.expectFile(staged, "previous executable", 0o755)
				f.expectFile(program, "checked executable", 0o755)
				f.expectFile(stamp, newDigest+"\n", 0o600)
				f.put(stamp, oldDigest+"\n", 0o600)
				setStampTime()
			}
			if err := install(t.Context(), f.env, fixtureVersion, wantDigest); err != nil {
				t.Fatal(err)
			}
			if fetches != 0 {
				t.Fatalf("recovery fetched %d times", fetches)
			}
			f.expectFile(program, wantProgram, 0o755)
			f.expectFile(stamp, wantDigest+"\n", 0o600)
			f.expectAbsent(staged)
			f.expectAbsent(record)
		})
	}
}

func installTestState() protocol.DesiredState {
	return protocol.DesiredState{
		Host:     "host01",
		Accounts: []protocol.Account{{Slug: "acme", GID: 20001, State: protocol.AccountActive}},
		Backup: &protocol.Backup{
			Bucket: "fixture-bucket", Region: "us-test-2",
			Settings: protocol.Settings{EveryMinutes: 60},
			Host: protocol.Credentials{
				AccessKeyID:     "AKIDfixtureKEY",
				SecretAccessKey: "FixtureSecret123456",
				SessionToken:    "FixtureToken345678",
			},
			HostPrefix: "hosts/host01/7/",
		},
	}
}

func TestBackupPassResticInstallRetry(t *testing.T) {
	// R-NRU5-E21I
	f := newHardeningFixture(t)
	f.put(dataPrivateKey, "host private", 0o600)
	f.put(dataPublicKey, "host public", 0o644)
	start := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	now := start
	f.env.Now = func() time.Time { return now }
	s := installTestState()
	s.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: s.Backup.Host}}
	var fetched []string
	f.env.Fetch = func(_ context.Context, url string) (io.ReadCloser, error) {
		fetched = append(fetched, url)
		return io.NopCloser(strings.NewReader("bad archive")), nil
	}
	f.host = linux.New(f.env)
	for i, offset := range []time.Duration{0, linux.InstallRetry - time.Second, linux.InstallRetry} {
		now = start.Add(offset)
		_ = f.host.Backup(t.Context(), s)
		want := 1
		if i == 2 {
			want = 2
		}
		if len(fetched) != want {
			t.Fatalf("pass %d fetched %v, want %d fetches", i, fetched, want)
		}
		report, err := f.host.Inspect(t.Context(), s)
		if err != nil || len(report.Backups) != 1 || report.Backups[0].Error != machine.ErrDigestMismatch.Error() {
			t.Fatalf("pass %d inspect backups = %+v, %v", i, report.Backups, err)
		}
		if len(fetched) != want {
			t.Fatal("Inspect fetched an install asset")
		}
		f.forbidCalls(f.path(linux.ResticPath), "restic ")
	}
	version := linux.ResticVersion
	wantURL := fmt.Sprintf("https://github.com/restic/restic/releases/download/v%s/restic_%s_linux_arm64.bz2", version, version)
	for _, url := range fetched {
		if url != wantURL {
			t.Fatalf("fetched %q, want %q", url, wantURL)
		}
	}
}

func TestStreamPassLitestreamInstallRetry(t *testing.T) {
	// R-NRU5-E21I
	f := newHardeningFixture(t)
	start := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	now := start
	f.env.Now = func() time.Time { return now }
	f.put("/srv/acme/data/test.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	s := installTestState()
	s.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: s.Backup.Host}}
	version := linux.LitestreamVersion
	wantURL := fmt.Sprintf("https://github.com/benbjohnson/litestream/releases/download/v%s/litestream-%s-linux-arm64.tar.gz", version, version)
	fetches := 0
	f.env.Fetch = func(_ context.Context, url string) (io.ReadCloser, error) {
		if url != wantURL {
			t.Fatalf("fetched %q, want %q", url, wantURL)
		}
		fetches++
		return io.NopCloser(strings.NewReader("bad archive")), nil
	}
	f.host = linux.New(f.env)
	for i, offset := range []time.Duration{0, linux.InstallRetry - time.Second, linux.InstallRetry} {
		now = start.Add(offset)
		_ = f.host.Stream(t.Context(), s)
		report, err := f.host.Inspect(t.Context(), s)
		if err != nil || len(report.Backups) != 1 || len(report.Backups[0].Databases) != 1 ||
			report.Backups[0].Databases[0].Error != machine.ErrDigestMismatch.Error() {
			t.Fatalf("pass %d inspect databases = %+v, %v", i, report.Backups, err)
		}
		if want := 1 + i/2; fetches != want {
			t.Fatalf("pass %d fetched %d times, want %d", i, fetches, want)
		}
		f.forbidCalls(f.path(linux.LitestreamPath), "litestream ", "systemctl restart accountsd-litestream@")
	}
}

func (f *hardeningFixture) putBackupProgramStamps() {
	f.put(linux.ResticPath, "installed restic", 0o755)
	f.put(linux.BackupDir+"/restic.digest", linux.ResticDigest+"\n", 0o600)
	f.put(linux.LitestreamPath, "installed litestream", 0o755)
	f.put(linux.BackupDir+"/litestream.digest", linux.LitestreamDigest+"\n", 0o600)
}

func TestHostBackupConcurrentCalls(t *testing.T) {
	// R-Y4BZ-20LE
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put(dataPrivateKey, "host private", 0o600)
	f.put(dataPublicKey, "host public", 0o644)
	f.put("/srv/acme/data/test.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	s := installTestState()
	s.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: s.Backup.Host}}

	backupStarted, streamStarted := make(chan struct{}, 1), make(chan struct{}, 1)
	releaseBackup, releaseStream := make(chan struct{}), make(chan struct{})
	block := func(ctx context.Context, started chan<- struct{}, release <-chan struct{}) error {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var commandMu sync.Mutex
	base := f.env.Execute
	f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
		switch c.Name {
		case f.path(linux.ResticPath):
			return linux.Result{ExitCode: 7}, block(ctx, backupStarted, releaseBackup)
		case f.path(linux.LitestreamPath):
			if err := block(ctx, streamStarted, releaseStream); err != nil {
				return linux.Result{}, err
			}
			_, err := io.WriteString(c.Stdout, "[]")
			return linux.Result{}, err
		default:
			commandMu.Lock()
			defer commandMu.Unlock()
			return base(ctx, c)
		}
	}
	f.host = linux.New(f.env)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	run := func(call func() error) <-chan error {
		done := make(chan error, 1)
		go func() { done <- call() }()
		return done
	}
	backupDone := run(func() error { return f.host.Backup(ctx, s) })
	select {
	case <-backupStarted:
	case <-ctx.Done():
		t.Fatal("Backup did not start restic")
	}
	streamDone := run(func() error { return f.host.Stream(ctx, s) })
	select {
	case <-streamStarted:
	case <-ctx.Done():
		t.Fatal("Stream did not start Litestream while Backup ran")
	}
	applyDone := run(func() error { return f.host.Apply(ctx, protocol.DesiredState{}) })
	inspectDone := run(func() error { _, err := f.host.Inspect(ctx, s); return err })
	wait := func(name string, done <-chan error, wantError bool) {
		t.Helper()
		select {
		case err := <-done:
			if (err != nil) != wantError {
				t.Fatalf("%s: %v", name, err)
			}
		case <-ctx.Done():
			t.Fatalf("%s waited for a blocked operation", name)
		}
	}
	wait("Apply", applyDone, false)
	wait("Inspect", inspectDone, false)
	close(releaseStream)
	wait("Stream", streamDone, false)
	close(releaseBackup)
	wait("Backup", backupDone, true)
}

func TestHostBackupCredentialsStayInEnvironment(t *testing.T) {
	// R-BRSD-UQ99
	f := newHardeningFixture(t)
	f.putBackupProgramStamps()
	f.put(dataPrivateKey, "host private", 0o600)
	f.put(dataPublicKey, "host public", 0o644)
	f.put("/srv/acme/data/test.db", "SQLite format 3\x00\x00\x00\x02\x02", 0o600)
	s := installTestState()
	account := protocol.Credentials{
		AccessKeyID:     "XQ",
		SecretAccessKey: "redacted",
		SessionToken:    "AccountToken34",
	}
	s.Backup.Accounts = []protocol.AccountCredentials{{Account: "acme", Credentials: account}}
	secretsOf := func(c protocol.Credentials) []string {
		return []string{c.AccessKeyID, c.SecretAccessKey, c.SessionToken}
	}
	secrets := append(secretsOf(account), secretsOf(s.Backup.Host)...)
	check := func(label, text string) {
		t.Helper()
		for _, value := range secrets {
			if strings.Contains(text, value) {
				t.Errorf("%s exposed a credential", label)
			}
		}
	}
	var stderr bytes.Buffer
	f.env.Stderr = &stderr
	base := f.env.Execute
	programCalls := make(map[string]int)
	f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
		label, credentials := "", account
		switch c.Name {
		case f.path(linux.LitestreamPath):
			label = "Litestream"
		case f.path(linux.ResticPath):
			label = "account restic"
			if slices.Contains(c.Env, "AWS_ACCESS_KEY_ID="+s.Backup.Host.AccessKeyID) {
				label, credentials = "host restic", s.Backup.Host
			}
		default:
			return base(ctx, c)
		}
		programCalls[label]++
		if c.Stdin != nil {
			t.Errorf("%s received command input", label)
		}
		for i, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
			if !slices.Contains(c.Env, key+"="+secretsOf(credentials)[i]) {
				t.Errorf("%s did not receive %s in its environment", label, key)
			}
		}
		check(label+" arguments", strings.Join(c.Args, " "))
		leak := strings.Join(secrets, " ")
		return linux.Result{Stderr: []byte(leak), ExitCode: 7}, errors.New(leak)
	}
	f.host = linux.New(f.env)
	if err := f.host.Stream(t.Context(), s); err == nil {
		t.Fatal("Stream returned nil following a command failure")
	} else {
		check("Stream error", err.Error())
	}
	if err := f.host.Backup(t.Context(), s); err == nil {
		t.Fatal("Backup returned nil following a command failure")
	} else {
		check("Backup error", err.Error())
	}
	for _, label := range []string{"host restic", "account restic", "Litestream"} {
		if programCalls[label] == 0 {
			t.Errorf("no invocation for %s", label)
		}
	}
	report, err := f.host.Inspect(t.Context(), s)
	if err != nil || report.HostBackup.Error == "" || len(report.Backups) != 1 || report.Backups[0].Error == "" {
		t.Fatalf("Inspect backups = %+v / %+v, %v", report.HostBackup, report.Backups, err)
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	check("report", string(body))
	for _, path := range []string{linux.ResticPath, linux.LitestreamPath} {
		if err := os.Remove(f.path(path)); err != nil {
			t.Fatal(err)
		}
	}
	f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("installer offline: %s", strings.Join(secrets, " "))
	}
	f.host = linux.New(f.env)
	for label, pass := range map[string]func(context.Context, protocol.DesiredState) error{"Stream": f.host.Stream, "Backup": f.host.Backup} {
		err := pass(t.Context(), s)
		if err == nil || !strings.Contains(err.Error(), "installer offline") {
			t.Fatalf("%s did not report the install failure", label)
		}
		check(label+" install error", err.Error())
	}
	report, err = f.host.Inspect(t.Context(), s)
	if err != nil || len(report.Backups) != 1 || len(report.Backups[0].Databases) != 1 ||
		!strings.Contains(report.Backups[0].Error, "installer offline") || !strings.Contains(report.Backups[0].Databases[0].Error, "installer offline") {
		t.Fatalf("Inspect did not carry the installer errors, err = %v", err)
	}
	check("restic install report error", report.Backups[0].Error)
	check("Litestream install report error", report.Backups[0].Databases[0].Error)
	check("stderr", stderr.String())
	for name, file := range snapshotHost(t, f.root) {
		if name != linux.BackupDir+"/acme.env" {
			check(name, string(file.bytes))
		}
	}
}
