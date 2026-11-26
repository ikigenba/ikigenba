package linux_test

import (
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	seedFolder  = "/srv/citron"
	seedStaging = "/srv/.citron.seed"
	seedMarker  = seedFolder + "/" + linux.SeedMarker
	stageMarker = seedStaging + "/" + linux.SeedMarker
	seedPending = "pending 22000\n"
	seedUser    = "runuser -u citron -- "
)

func TestSeedMarker(t *testing.T) {
	// R-QUYZ-0COF
	const marker string = linux.SeedMarker
	if marker != ".git/ikigenba-seed" {
		t.Fatalf("seed marker = %q", marker)
	}
}

type seedFixture struct {
	*hardeningFixture
	state      protocol.DesiredState
	head       int
	fail       string
	recordOnly bool
	check      func(linux.Command)
}

func newSeedFixture(t *testing.T) *seedFixture {
	t.Helper()
	f := &seedFixture{hardeningFixture: newHardeningFixture(t), state: protocol.DesiredState{
		Host: "host04", DataVolume: "vol-12345678",
		Accounts: []protocol.Account{{Slug: "citron", GID: 22000, State: protocol.AccountActive}},
	}}
	f.observe = f.executeSeedCommand
	f.answer = func(call string) (linux.Result, bool) {
		switch call {
		case f.fail:
			return linux.Result{ExitCode: 7}, true
		case f.headQuery():
			return linux.Result{ExitCode: f.head}, true
		default:
			return linux.Result{}, false
		}
	}
	return f
}

func (f *seedFixture) executeSeedCommand(c linux.Command) {
	f.t.Helper()
	if f.check != nil {
		f.check(c)
	}
	if c.Name != "runuser" || len(c.Args) < 5 || !slices.Equal(c.Args[:3], []string{"-u", "citron", "--"}) {
		return
	}
	if f.commands[len(f.commands)-1] == f.fail {
		return
	}
	switch {
	case len(c.Args) == 7 && slices.Equal(c.Args[3:6], []string{"git", "init", "--shared=group"}):
		if err := os.Mkdir(filepath.Join(f.seedTarget(c.Args[6]), ".git"), 0o750); err != nil {
			f.t.Fatal(err)
		}
	case len(c.Args) == 6 && slices.Equal(c.Args[3:5], []string{"mkdir", "-p"}):
		if !f.recordOnly {
			if err := os.MkdirAll(f.seedTarget(c.Args[5]), 0o750); err != nil {
				f.t.Fatal(err)
			}
		}
	case len(c.Args) == 5 && c.Args[3] == "tee":
		if c.Stdin == nil || c.Stdout != io.Discard {
			f.t.Fatalf("tee %q did not pipe input and discard output", c.Args[4])
		}
		data, err := io.ReadAll(c.Stdin)
		if err != nil {
			f.t.Fatal(err)
		}
		if f.recordOnly {
			return
		}
		if err := os.WriteFile(f.seedTarget(c.Args[4]), data, 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *seedFixture) seedTarget(full string) string {
	f.t.Helper()
	for _, path := range []string{seedFolder, seedStaging} {
		base := f.path(path)
		if full == base || strings.HasPrefix(full, base+"/") {
			return full
		}
	}
	f.t.Fatalf("unexpected seed path %q", full)
	return ""
}

func (f *seedFixture) expectSeedCommands(want ...string) {
	f.t.Helper()
	var got []string
	for _, call := range f.commands {
		if strings.HasPrefix(call, seedUser) ||
			strings.HasPrefix(call, "chown citron:citron ") || strings.HasPrefix(call, "setfacl -d -m g::rwx,o::- ") {
			got = append(got, call)
		}
		if strings.HasPrefix(call, "chown --no-dereference root:root "+f.path(seedFolder)) {
			f.t.Fatalf("isolation touched the seed: %s", call)
		}
	}
	if !slices.Equal(got, want) {
		f.t.Fatalf("account folder commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func (f *seedFixture) chown(path string) string { return "chown citron:citron " + f.path(path) }
func (f *seedFixture) acl(path string) string   { return "setfacl -d -m g::rwx,o::- " + f.path(path) }
func (f *seedFixture) initGit(path string) string {
	return seedUser + "git init --shared=group " + f.path(path)
}
func (f *seedFixture) tee(path string) string { return seedUser + "tee " + f.path(path) }
func (f *seedFixture) headQuery() string {
	return seedUser + "git -C " + f.path(seedFolder) + " rev-parse --verify --quiet HEAD"
}

func (f *seedFixture) seedCommit(hostname string) []string {
	return []string{"-u", "citron", "--", "git", "-C", f.path(seedFolder),
		"-c", "user.name=citron", "-c", "user.email=citron@" + hostname, "commit", "--quiet", "-m", machine.SeedMessage}
}

func (f *seedFixture) expectSetgid(path string) {
	f.t.Helper()
	if got := f.stat(path).Mode() & (fs.ModePerm | fs.ModeSetgid); got != fs.ModeSetgid|0o770 {
		f.t.Fatalf("%s mode = %v, want setgid and 0770", path, got)
	}
}

func TestApplyStagesAndRetriesAccountFolder(t *testing.T) {
	// R-288Q-42K0
	f := newSeedFixture(t)
	f.put(seedStaging+"/abandoned/old", "stale staging", 0o600)
	f.put("/outside/keep", "outside staging", 0o600)
	linkInHost(t, f, "/outside", seedStaging+"/link")

	var staged fs.FileInfo
	f.check = func(_ linux.Command) {
		switch f.commands[len(f.commands)-1] {
		case f.chown(seedStaging):
			f.expectAbsent(seedFolder)
			f.expectAbsent(seedStaging + "/abandoned")
			f.expectAbsent(seedStaging + "/link")
			f.expectMode(seedStaging, 0o700)
		case f.acl(seedStaging):
			f.expectAbsent(seedFolder)
			f.expectSetgid(seedStaging)
		case f.tee(stageMarker):
			f.expectAbsent(seedFolder)
			f.expectAbsent(stageMarker)
			staged = f.stat(seedStaging)
		case f.headQuery():
			f.expectAbsent(seedStaging)
			f.expectFile(seedMarker, seedPending, 0o600)
			if staged == nil || !os.SameFile(staged, f.stat(seedFolder)) {
				t.Fatal("account folder was not renamed from staging")
			}
		}
	}
	f.fail = f.tee(stageMarker)
	f.applyFails(f.state, f.fail, "runuser")
	f.expectSeedCommands(f.chown(seedStaging), f.acl(seedStaging), f.initGit(seedStaging), f.tee(stageMarker))
	f.expectAbsent(seedFolder)
	f.expectFile("/outside/keep", "outside staging", 0o600)
	f.put(seedStaging+"/abandoned/old", "failed staging", 0o600)
	linkInHost(t, f, "/outside", seedStaging+"/link")

	f.fail = ""
	f.commands = nil
	f.apply(f.state)
	f.expectSeedCommands(f.chown(seedStaging), f.acl(seedStaging), f.initGit(seedStaging), f.tee(stageMarker), f.headQuery(), f.tee(seedMarker))
	f.expectAbsent(seedStaging)
	f.expectAbsent(seedFolder + "/abandoned")
	f.expectFile("/outside/keep", "outside staging", 0o600)
}

func TestApplyRepairsExistingAccountFolder(t *testing.T) {
	// R-288Q-42K0 R-5WTB-RRWI
	f := newSeedFixture(t)
	f.put(seedFolder+"/project", "retained", 0o640)
	f.put("/outside/keep", "outside staging", 0o600)
	linkInHost(t, f, "/outside", seedStaging)
	prior := f.stat(seedFolder + "/project")
	dir := f.stat(seedFolder)
	f.check = func(_ linux.Command) {
		switch f.commands[len(f.commands)-1] {
		case f.chown(seedFolder):
			f.expectAbsent(seedStaging)
			f.expectMode(seedFolder, 0o700)
		case f.acl(seedFolder):
			f.expectSetgid(seedFolder)
		}
	}
	f.apply(f.state)
	f.expectSeedCommands(f.chown(seedFolder), f.acl(seedFolder), f.initGit(seedFolder))
	f.expectFile(seedFolder+"/project", "retained", 0o640)
	assertUnchanged(t, f.path(seedFolder)+"/project", prior)
	assertUnchanged(t, f.path(seedFolder), dir)
	f.expectAbsent(seedMarker)
	f.expectFile("/outside/keep", "outside staging", 0o600)
}

func linkInHost(t *testing.T, f *seedFixture, target, link string) {
	t.Helper()
	if err := os.Symlink(f.path(target), f.path(link)); err != nil {
		t.Fatal(err)
	}
}

func TestApplyIgnoresNonpendingSeedMarkers(t *testing.T) {
	// R-29GM-HUAP
	for _, tc := range []struct {
		name    string
		prepare func(*seedFixture)
	}{
		{"absent", func(f *seedFixture) { f.put(seedFolder+"/.git/config", "original", 0o600) }},
		{"done", func(f *seedFixture) { f.put(seedMarker, "done\n", 0o600) }},
		{"other gid", func(f *seedFixture) { f.put(seedMarker, "pending 22001\n", 0o600) }},
		{"trailing bytes", func(f *seedFixture) { f.put(seedMarker, seedPending+"\n", 0o600) }},
		{"too big", func(f *seedFixture) { f.put(seedMarker, seedPending+strings.Repeat("x", linux.MaxCapture), 0o600) }},
		{"directory", func(f *seedFixture) { f.put(seedMarker+"/content", seedPending, 0o600) }},
		{"marker link", func(f *seedFixture) {
			f.put(seedFolder+"/.git/config", "original", 0o600)
			f.put("/outside/marker", seedPending, 0o600)
			linkInHost(f.t, f, "/outside/marker", seedMarker)
		}},
		{"git link", func(f *seedFixture) {
			f.put(seedFolder+"/original", "original", 0o600)
			f.put("/outside/"+filepath.Base(seedMarker), seedPending, 0o600)
			linkInHost(f.t, f, "/outside", seedFolder+"/.git")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSeedFixture(t)
			tc.prepare(f)
			before := snapshotApplyHost(t, f.path(seedFolder))
			f.apply(f.state)
			f.forbidCalls(seedUser)
			delete(before, "")
			after := snapshotApplyHost(t, f.path(seedFolder))
			delete(after, "")
			assertHostChanges(t, before, after, nil)
		})
	}
}

func TestMarkerSocket(t *testing.T) {
	// R-29GM-HUAP
	f := newSeedFixture(t)
	f.put(seedFolder+"/.git/config", "original", 0o600)
	listener, err := net.Listen("unix", f.path(seedMarker))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	f.apply(f.state)
	f.forbidCalls(seedUser)
	if f.stat(seedMarker).Mode()&fs.ModeSocket == 0 {
		t.Fatal("seed marker socket was replaced")
	}
}

func TestApplyStopsWhenHeadQueryFails(t *testing.T) {
	// R-29GM-HUAP R-S739-S0BW
	f := newSeedFixture(t)
	f.put(seedMarker, seedPending, 0o600)
	f.fail = f.headQuery()
	f.applyFails(f.state, f.fail, "runuser")
	f.expectFile(seedMarker, seedPending, 0o600)
}

func TestApplyLeavesSeedWritesToRunuser(t *testing.T) {
	// R-KR34-ANBD R-QYMO-5NWI
	f := newSeedFixture(t)
	f.recordOnly = true
	f.head = 1
	f.put(seedMarker, seedPending, 0o600)
	before := snapshotApplyHost(t, f.path(seedFolder))
	f.apply(f.state)
	want, _, _ := f.seedPlan()
	for _, call := range append(want, f.tee(seedMarker)) {
		if !slices.Contains(f.commands, call) {
			t.Fatalf("missing seed command %q", call)
		}
	}
	f.expectFile(seedMarker, seedPending, 0o600)
	delete(before, "")
	after := snapshotApplyHost(t, f.path(seedFolder))
	delete(after, "")
	assertHostChanges(t, before, after, nil)
}

func TestApplyLeavesMarkerPendingOnCommitFailure(t *testing.T) {
	// R-QYMO-5NWI R-S739-S0BW
	f := newSeedFixture(t)
	f.head = 1
	f.put(seedMarker, seedPending, 0o600)
	f.fail = "runuser " + strings.Join(f.seedCommit(f.state.Host), " ")
	f.applyFails(f.state, f.fail, "runuser")
	f.forbidCalls(f.tee(seedMarker))
	f.expectFile(seedMarker, seedPending, 0o600)

	f.commands = nil
	f.fail = ""
	f.head = 0 // Git now reports a commit despite the previous command's failure.
	f.apply(f.state)
	f.expectSeedCommands(f.chown(seedFolder), f.acl(seedFolder), f.headQuery(), f.tee(seedMarker))
	f.expectFile(seedMarker, "done\n", 0o600)
}

func TestApplySeedsAndCommitsOnce(t *testing.T) {
	// R-29GM-HUAP R-KR34-ANBD R-QXER-RW5T R-QYMO-5NWI R-H6HF-3CTF
	for _, tc := range []struct {
		name, domain, hostname string
		retryDone              bool
	}{
		{"host only", "", "host04", false},
		{"qualified with failed done write", "example.test", "host04.example.test", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSeedFixture(t)
			f.state.Domain = tc.domain
			f.put(seedMarker, seedPending, 0o600)
			f.head = 1
			want, files, names := f.seedPlan()
			addPrefix := []string{"-u", "citron", "--", "git", "-C", f.path(seedFolder), "add", "--force", "--"}
			commit := f.seedCommit(tc.hostname)
			var added, addCalls []string
			f.check = func(c linux.Command) {
				if c.Name != "runuser" {
					return
				}
				if len(c.Args) == 6 && slices.Equal(c.Args[3:5], []string{"mkdir", "-p"}) {
					f.expectAbsent(strings.TrimPrefix(c.Args[5], f.root))
				}
				if len(c.Args) == 5 && c.Args[3] == "tee" && c.Args[4] != f.path(seedMarker) {
					f.expectAbsent(strings.TrimPrefix(c.Args[4], f.root))
				}
				if len(c.Args) >= len(addPrefix) && slices.Equal(c.Args[:len(addPrefix)], addPrefix) {
					batch := c.Args[len(addPrefix):]
					if len(batch) == 0 || len(batch) > linux.ChownBatch {
						t.Fatalf("git add batch contains %d files", len(batch))
					}
					added = append(added, batch...)
					addCalls = append(addCalls, "runuser "+strings.Join(c.Args, " "))
				}
				if slices.Contains(c.Args, "commit") && !slices.Equal(c.Args, commit) {
					t.Fatalf("git commit arguments: %q, want %q", c.Args, commit)
				}
			}
			if tc.retryDone {
				f.fail = f.tee(seedMarker)
				f.applyFails(f.state, f.fail, "runuser")
			} else {
				f.apply(f.state)
			}
			slices.Sort(added)
			if !slices.Equal(added, names) {
				t.Fatalf("git add files: %q, want %q", added, names)
			}
			want = append(want, addCalls...)
			want = append(want, "runuser "+strings.Join(commit, " "), f.tee(seedMarker))
			f.expectSeedCommands(want...)
			for path, contents := range files {
				f.expectFile(path, contents, 0o600)
			}
			for path := range files {
				f.put(path, "team's edits", 0o600)
			}
			if tc.retryDone {
				f.expectFile(seedMarker, seedPending, 0o600)
				f.fail = ""
				f.commands = nil
				f.head = 0
				f.apply(f.state)
				f.expectSeedCommands(f.chown(seedFolder), f.acl(seedFolder), f.headQuery(), f.tee(seedMarker))
			}
			f.expectFile(seedMarker, "done\n", 0o600)
			f.commands = nil
			f.apply(f.state)
			f.forbidCalls(seedUser)
			for path := range files {
				f.expectFile(path, "team's edits", 0o600)
			}
		})
	}
}

func (f *seedFixture) seedPlan() ([]string, map[string]string, []string) {
	f.t.Helper()
	source := accounts.Seed()
	want := []string{f.chown(seedFolder), f.acl(seedFolder), f.headQuery()}
	files := map[string]string{}
	var names []string
	if err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		path := filepath.Join(seedFolder, name)
		if entry.IsDir() {
			want = append(want, seedUser+"mkdir -p "+f.path(path))
			return nil
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		files[path] = string(data)
		want = append(want, f.tee(path))
		names = append(names, name)
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
	slices.Sort(names)
	return want, files, names
}
