package linux_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const fixtureCommit = "0123456789abcdef0123456789abcdef01234567"

func templateStep(c linux.Command) string {
	if c.Name != "runuser" || len(c.Args) < 5 || c.Args[1] != linux.CloneUser {
		return ""
	}
	if c.Args[3] == "prlimit" {
		if c.Args[9] == "-C" {
			return "checkout"
		}
		return "clone"
	}
	return c.Args[6]
}

func (f *hardeningFixture) playTemplate(step string, c linux.Command) linux.Result {
	f.t.Helper()
	switch step {
	case "clone":
		for _, dir := range []string{"seed", "home", ".git"} {
			if err := os.MkdirAll(f.path(filepath.Join(linux.CloneDir, dir)), 0o700); err != nil {
				f.t.Fatal(err)
			}
		}
		for path, contents := range f.template {
			f.put(filepath.Join(linux.CloneDir, path), contents, 0o600)
		}
	case "rev-parse":
		return hostAnswer(fixtureCommit + "\n")
	case "ls-tree":
		for path, contents := range f.template {
			if _, err := fmt.Fprintf(c.Stdout, "100644 blob %s %8d\t%s\x00", fixtureCommit, len(contents), path); err != nil {
				f.t.Fatal(err)
			}
		}
	}
	return linux.Result{}
}

func TestTemplateContract(t *testing.T) {
	// R-WS98-MLNO R-WTH5-0DED
	const (
		dir     string        = linux.TemplateDir
		clone   string        = linux.CloneDir
		user    string        = linux.CloneUser
		timeout time.Duration = linux.CloneTimeout
	)
	var maxEntries uint16 = linux.MaxTemplateEntries
	var maxBytes uint32 = linux.MaxTemplateBytes
	if dir != "/var/lib/accountsd/template" || clone != "/var/lib/accountsd/template/clone" ||
		user != "nobody" || timeout != 2*time.Minute || maxEntries != 1000 || maxBytes != 16<<20 {
		t.Fatalf("template constants: %q %q %q %v %d %d", dir, clone, user, timeout, maxEntries, maxBytes)
	}
	if linux.ErrTemplate == nil {
		t.Fatal("nil template error")
	}
	for _, other := range []error{machine.ErrNoDataVolume, machine.ErrDigestMismatch, machine.ErrNotExecutable,
		machine.ErrTooLarge, machine.ErrIdentity, machine.ErrInvalidState, machine.ErrNotSynced, machine.ErrNoSnapshot, linux.ErrClaudeKey} {
		if errors.Is(other, linux.ErrTemplate) {
			t.Fatalf("template error aliases %v", other)
		}
	}
	for _, line := range []string{"", "diagnostic line"} {
		err := &linux.TemplateError{URL: "https://example.test/team", Ref: "test-ref", Line: line, Err: linux.ErrTemplate}
		if !strings.Contains(err.Error(), err.URL) || !strings.Contains(err.Error(), err.Ref) ||
			line != "" && !strings.Contains(err.Error(), line) || !errors.Is(err.Unwrap(), linux.ErrTemplate) {
			t.Fatalf("template error lost fields: %v", err)
		}
	}
}

func TestApplyTemplateCommandSequence(t *testing.T) {
	// R-WUP1-E552 R-WX4U-5OMG R-XP6I-YEOF R-XCZJ-4P9H
	for _, setting := range []struct{ url, ref string }{
		{}, {"https://example.test/my/team", "refs/branch"}, {"", "custom"}, {"https://example.test/other", ""},
	} {
		t.Run(setting.url+" "+setting.ref, func(t *testing.T) {
			f := newSeedFixture(t)
			f.state.TemplateURL, f.state.TemplateRef = setting.url, setting.ref
			url, ref := setting.url, setting.ref
			if url == "" {
				url = protocol.DefaultTemplateURL
			}
			if ref == "" {
				ref = protocol.DefaultTemplateRef
			}
			f.put("/srv/existing", "fixture", 0o600)
			f.put(linux.CloneDir+"/old", "residue", 0o600)
			f.setMode(linux.TemplateDir, 0o755)
			dir := f.path(linux.CloneDir)
			limit := strconv.Itoa(linux.MaxTemplateBytes)
			prefix := []string{"-u", linux.CloneUser, "--", "prlimit", "--fsize=" + limit, "--", "git", "-c", "core.hooksPath=/dev/null"}
			expected := [][]string{
				append(slices.Clone(prefix), "-c", "credential.helper=", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
					"-c", "advice.detachedHead=false", "clone", "--quiet", "--no-checkout", "--depth", "1", "--single-branch", "--no-tags", "--no-recurse-submodules", "--branch", ref, "--", url, dir),
				{"-u", linux.CloneUser, "--", "git", "-C", dir, "rev-parse", "HEAD"},
				{"-u", linux.CloneUser, "--", "git", "-C", dir, "ls-tree", "-r", "-l", "-z", "HEAD"},
				append(slices.Clone(prefix), "-C", dir, "checkout", "--quiet", "--force", "HEAD"),
			}
			env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
			var deadline time.Time
			var count int
			f.clone = func(ctx context.Context, step string, c linux.Command) (linux.Result, error) {
				now := time.Now()
				limit, ok := ctx.Deadline()
				if count == 0 {
					deadline = limit
					f.expectAbsent(linux.CloneDir + "/old")
					f.expectAbsent(seedStaging)
					f.expectMode(linux.TemplateDir, 0o711)
					f.expectMode(linux.CloneDir, 0o700)
				}
				if !ok || !limit.Equal(deadline) || limit.After(now.Add(linux.CloneTimeout)) {
					t.Fatalf("clone step %s deadline = %v, want shared deadline within CloneTimeout", step, limit)
				}
				if count >= len(expected) || !slices.Equal(c.Args, expected[count]) || !slices.Equal(c.Env, env) {
					t.Fatalf("unexpected clone step %s, %q %q", step, c.Args, c.Env)
				}
				if step == "ls-tree" && c.Stdout == nil {
					t.Fatal("ls-tree was captured instead of streamed")
				}
				count++
				return f.playTemplate(step, c), nil
			}
			f.env.Fetch = func(_ context.Context, url string) (io.ReadCloser, error) {
				t.Fatalf("unexpected Fetch of %q", url)
				return nil, nil
			}
			f.host = linux.New(f.env)
			f.apply(f.state)
			if count != len(expected) {
				t.Fatalf("clone steps = %d, want %d", count, len(expected))
			}
			var calls []string
			for _, call := range f.commands {
				if strings.Contains(call, f.path(linux.TemplateDir)) {
					calls = append(calls, call)
				}
			}
			want := []string{
				"chown --no-dereference root:root " + f.path(linux.TemplateDir),
				"chown nobody:nobody " + dir,
			}
			for _, args := range expected {
				want = append(want, "runuser "+strings.Join(args, " "))
			}
			want = append(want, "chown -R --no-dereference root:root "+dir)
			if !slices.Equal(calls, want) {
				t.Fatalf("clone calls = %q, want %q", calls, want)
			}
			f.expectAbsent(linux.CloneDir)
		})
	}
}

func checkTemplateFailure(t *testing.T, err error, state protocol.DesiredState, line string) *linux.TemplateError {
	t.Helper()
	var failure *linux.TemplateError
	if !errors.As(err, &failure) || failure.URL != state.TemplateURL || failure.Ref != state.TemplateRef || failure.Line != line {
		t.Fatalf("Apply error = %v, want TemplateError for %q at %q with line %q", err, state.TemplateURL, state.TemplateRef, line)
	}
	return failure
}

func TestApplyRefusesInvalidTemplateSettingsWhenNeeded(t *testing.T) {
	// R-WYCQ-JGD5
	for _, setting := range []struct{ url, ref string }{
		{"http://example.test/team", "main"},
		{"https://example.test/team", "-c"},
	} {
		f, state := newHomeFixture(t)
		state.TemplateURL, state.TemplateRef = setting.url, setting.ref
		err := f.host.Apply(t.Context(), state)
		failure := checkTemplateFailure(t, err, state, "")
		if !errors.Is(failure, linux.ErrTemplate) {
			t.Fatalf("Apply error = %v, want ErrTemplate", err)
		}
		f.forbidCalls("runuser -u "+linux.CloneUser+" ", "chown nobody:nobody ", "chown --no-dereference root:root "+f.path(linux.TemplateDir))
		f.expectAbsent(linux.TemplateDir)
		f.expectAbsent(homeKeys)
		f.put("/etc/passwd", homeCreated, 0o644)
		f.apply(state)
	}
}

func TestApplyTemplateGitFailureReportsDiagnostic(t *testing.T) {
	// R-WZKM-X83U
	for _, step := range []string{"clone", "rev-parse", "ls-tree", "checkout"} {
		t.Run(step, func(t *testing.T) {
			f, state := newHomeFixture(t)
			state.TemplateURL, state.TemplateRef = "https://example.test/team", "feature"
			result := linux.Result{ExitCode: 128, Stderr: []byte("one\ntwo three\r\n \t\r\n")}
			var commands []string
			f.clone = func(_ context.Context, name string, c linux.Command) (linux.Result, error) {
				commands = append(commands, name)
				if name == step {
					return result, nil
				}
				return f.playTemplate(name, c), nil
			}
			err := f.host.Apply(t.Context(), state)
			failure := checkTemplateFailure(t, err, state, "two three")
			var command *linux.CommandError
			if !errors.As(failure.Err, &command) || command.Label != "runuser" ||
				command.Result.ExitCode != result.ExitCode || string(command.Result.Stderr) != string(result.Stderr) ||
				!strings.Contains(err.Error(), "two three") {
				t.Fatalf("unexpected wrapped command error: %v", err)
			}
			if commands[len(commands)-1] != step || len(commands) != slices.Index([]string{"clone", "rev-parse", "ls-tree", "checkout"}, step)+1 {
				t.Fatalf("clone command sequence = %v", commands)
			}
			f.expectAbsent(linux.CloneDir)
			f.forbidCalls("chown -R --no-dereference root:root " + f.path(linux.CloneDir))
		})
	}
}

func TestApplyTemplateRevParseRequiresExactCommit(t *testing.T) {
	// R-WZKM-X83U
	for _, output := range []string{
		strings.ToUpper(fixtureCommit) + "\n", fixtureCommit, fixtureCommit + "a\n",
		fixtureCommit + "\r\n", strings.Repeat("a", 64) + "\n",
	} {
		f, state := newHomeFixture(t)
		state.TemplateURL, state.TemplateRef = "https://example.test/team", "feature"
		var steps []string
		f.clone = func(_ context.Context, name string, c linux.Command) (linux.Result, error) {
			steps = append(steps, name)
			if name == "rev-parse" {
				return hostAnswer(output), nil
			}
			return f.playTemplate(name, c), nil
		}
		err := f.host.Apply(t.Context(), state)
		if len(output) == 65 {
			report, inspectErr := f.host.Inspect(t.Context(), state)
			if err != nil || inspectErr != nil || len(report.Homes) != 1 || report.Homes[0].Commit != output[:64] {
				t.Fatalf("SHA-256 Apply = %v, report %v, %v", err, report.Homes, inspectErr)
			}
			continue
		}
		failure := checkTemplateFailure(t, err, state, "")
		if !errors.Is(failure, linux.ErrTemplate) || !slices.Equal(steps, []string{"clone", "rev-parse"}) {
			t.Fatalf("rev-parse output %q: error %v, clone steps %v", output, err, steps)
		}
	}
}

func listingRecord(mode, size, path string) string {
	return mode + " blob " + fixtureCommit + " " + size + "\t" + path + "\x00"
}

func TestApplyTemplateRejectsInvalidTreeBeforeCheckout(t *testing.T) {
	// R-X0SJ-AZUJ
	var entries strings.Builder
	for i := range linux.MaxTemplateEntries + 1 {
		entries.WriteString(listingRecord("100644", "1", fmt.Sprintf("seed/file%04d", i)))
	}
	rec := func(size, path string) string { return listingRecord("100644", size, path) }
	for _, tc := range []struct{ name, listing string }{
		{"symlink in home", listingRecord("120000", "16", "home/AGENTS.md")},
		{"nonregular seed", listingRecord("160000", "-", "seed/submodule")},
		{"too many records", entries.String()},
		{"oversize record", rec("3", "seed/"+strings.Repeat("x", linux.MaxCapture))},
		{"oversize aggregate", rec("1", "seed/earlier") + rec(strconv.Itoa(linux.MaxTemplateBytes), "seed/later")},
		{"attributes anywhere", rec("3", "outside/nested/.gitattributes")},
		{"unknown size", rec("unknown", "seed/sample")},
		{"missing object", "100644 blob \tseed/sample\x00"},
		{"unterminated record", strings.TrimSuffix(rec("3", "seed/sample"), "\x00")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, state := newHomeFixture(t)
			state.TemplateURL, state.TemplateRef = "https://example.test/team", "feature"
			var commands []string
			f.clone = func(_ context.Context, step string, c linux.Command) (linux.Result, error) {
				commands = append(commands, step)
				if step == "ls-tree" {
					for len(tc.listing) > 0 {
						n := min(29, len(tc.listing))
						if _, err := io.WriteString(c.Stdout, tc.listing[:n]); err != nil {
							t.Fatal(err)
						}
						tc.listing = tc.listing[n:]
					}
					return linux.Result{}, nil
				}
				return f.playTemplate(step, c), nil
			}
			err := f.host.Apply(t.Context(), state)
			failure := checkTemplateFailure(t, err, state, "")
			if !errors.Is(failure, linux.ErrTemplate) || !slices.Equal(commands, []string{"clone", "rev-parse", "ls-tree"}) {
				t.Fatalf("Apply error %v, clone steps %v", err, commands)
			}
			f.forbidCalls("useradd ", "groupadd --gid 20001", "chown -R --no-dereference root:root "+f.path(linux.CloneDir))
			f.expectAbsent(linux.CloneDir)
		})
	}
}

func TestApplyTemplateRejectsInvalidCheckedOutFiles(t *testing.T) {
	// R-X0SJ-AZUJ
	for _, tc := range []struct {
		name   string
		change func(*hardeningFixture)
	}{
		{"missing home", func(f *hardeningFixture) {
			if err := os.RemoveAll(f.path(linux.CloneDir + "/home")); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"empty seed", func(f *hardeningFixture) {
			if err := os.Remove(f.path(linux.CloneDir + "/seed/project")); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"nested git", func(f *hardeningFixture) {
			f.put(linux.CloneDir+"/seed/nested/.git/file", "config fixture", 0o600)
		}},
		{"symlink below home", func(f *hardeningFixture) {
			if err := os.Symlink(f.path("/outside/keep"), f.path(linux.CloneDir+"/home/symlink")); err != nil {
				f.t.Fatal(err)
			}
		}},
		{"seed is a symbolic link", func(f *hardeningFixture) {
			if err := os.RemoveAll(f.path(linux.CloneDir + "/seed")); err != nil {
				f.t.Fatal(err)
			}
			if err := os.Symlink(f.path("/outside"), f.path(linux.CloneDir+"/seed")); err != nil {
				f.t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, state := newHomeFixture(t)
			f.put("/outside/keep", "retained", 0o600)
			state.TemplateURL, state.TemplateRef = "https://example.test/team", "feature"
			f.clone = func(_ context.Context, step string, c linux.Command) (linux.Result, error) {
				if step == "checkout" {
					tc.change(f)
				}
				return f.playTemplate(step, c), nil
			}
			err := f.host.Apply(t.Context(), state)
			failure := checkTemplateFailure(t, err, state, "")
			if !errors.Is(failure, linux.ErrTemplate) {
				t.Fatalf("Apply = %v, want ErrTemplate", err)
			}
			if !slices.Contains(f.commands, "chown -R --no-dereference root:root "+f.path(linux.CloneDir)) {
				t.Fatal("template content was rejected before root took the clone")
			}
			f.forbidCalls("useradd ", "groupadd --gid 20001")
			f.expectAbsent(linux.CloneDir)
			f.expectFile("/outside/keep", "retained", 0o600)
		})
	}
}

func TestApplyCloneFailureSkipsCreationsAndReconcilesTheRest(t *testing.T) {
	// R-XWHX-914L R-XXPT-MSVA R-XU24-HHN7 R-XYXQ-0KLZ
	f, state := newHomeFixture(t)
	state.TemplateURL, state.TemplateRef = "https://example.test/team", "feature"
	state.Accounts = append([]protocol.Account{
		{Slug: "citron", GID: 22000, State: protocol.AccountActive},
		{Slug: "birch", GID: 22001, State: protocol.AccountActive},
		{Slug: "retired", GID: 22002, State: protocol.AccountRemoved},
	}, state.Accounts...)
	state.Users = append([]protocol.User{
		{Name: "bob", Account: "acme", UID: 20002, State: protocol.UserLocked},
		{Name: "sally", Account: "acme", UID: 20004, State: protocol.UserActive},
		{Name: "old", Account: "acme", UID: 20003, State: protocol.UserDeleted},
	}, state.Users...)
	first, second := fixtureKey("retained first"), fixtureKey("retained second")
	state.Users[3].Keys = []string{first, second}
	f.put("/etc/group", "shell:x:990:daffy\nacme:x:20000:daffy\ndaffy:x:20001:\nretired:x:22002:\nwheel:x:10:bob\n", 0o644)
	f.put("/etc/passwd", homeCreated+"retired:x:22002:22002::/srv/retired:/usr/sbin/nologin\n", 0o644)
	f.put(homeFile, "existing home", 0o644)
	f.put(homeKeys, fixtureKey("removed")+"\n", 0o644)
	f.put(linux.KeysDir+"/bob", "stale key", 0o666)
	f.put(linux.KeysDir+"/old", "retired key", 0o644)
	f.put("/srv/.citron.seed/stale", "abandoned", 0o600)
	f.put("/srv/retired/retained", "keep", 0o600)
	f.setMode(homeDir, 0o750)
	f.setMode("/srv/acme", 0o750)
	var clones int
	f.clone = func(_ context.Context, step string, _ linux.Command) (linux.Result, error) {
		clones++
		if step != "clone" || clones != 1 {
			t.Fatalf("unexpected clone attempt %d: %s", clones, step)
		}
		return linux.Result{ExitCode: 128, Stderr: []byte("network failed\n")}, nil
	}
	err := f.host.Apply(t.Context(), state)
	if failure := checkTemplateFailure(t, err, state, "network failed"); failure.Err == nil {
		t.Fatal("missing clone failure cause")
	}
	if clones != 1 {
		t.Fatalf("clone attempts = %d", clones)
	}
	for _, path := range []string{"/srv/citron", "/srv/.citron.seed", "/srv/birch", "/srv/.birch.seed", linux.KeysDir + "/sally", linux.KeysDir + "/old"} {
		f.expectAbsent(path)
	}
	f.forbidCalls("useradd --uid ", "groupadd --gid 20002", "groupadd --gid 20004", "runuser -u bob ", "runuser -u sally ", "userdel --remove retired", "gpasswd --delete bob ",
		"chown --no-dereference root:root "+f.path(linux.KeysDir+"/bob"))
	f.expectFile(linux.KeysDir+"/bob", "stale key", 0o666)
	for _, call := range []string{"userdel retired", "groupdel retired",
		"groupadd --system --gid 22000 citron", "groupadd --system --gid 22001 birch"} {
		if !slices.Contains(f.commands, call) {
			t.Fatalf("Apply omitted %q after failed clone", call)
		}
	}
	f.expectFile("/srv/retired/retained", "keep", 0o600)
	f.expectFile(homeKeys, first+"\n"+second+"\n", 0o644)
	f.expectFile(homeFile, "existing home", 0o644)
	f.expectMode(homeDir, 0o700)
	if mode := f.stat("/srv/acme").Mode(); mode&(os.ModeSetgid|os.ModePerm) != os.ModeSetgid|0o770 {
		t.Fatalf("existing account folder mode = %v", mode)
	}
	f.expectAbsent(linux.CloneDir)
}

func TestApplyRemovesUnusedCloneWithoutFollowingLinks(t *testing.T) {
	// R-X20F-ORL8
	f := newHardeningFixture(t)
	f.put(linux.CloneDir+"/residue", "abandoned", 0o600)
	f.put("/outside/keep", "retained", 0o600)
	if err := os.Symlink(f.path("/outside/keep"), f.path(linux.CloneDir+"/link")); err != nil {
		t.Fatal(err)
	}
	f.apply(protocol.DesiredState{})
	f.forbidCalls("runuser -u " + linux.CloneUser + " ")
	f.expectAbsent(linux.CloneDir)
	f.expectFile("/outside/keep", "retained", 0o600)
}

func TestApplyCloneCleanupFailureReturnsFirstError(t *testing.T) {
	// R-X20F-ORL8
	for _, pending := range []bool{false, true} {
		f := newHardeningFixture(t)
		f.put("/outside/clone/keep", "retained", 0o600)
		f.put("/var/lib/accountsd/ancestor", "parent", 0o600)
		if err := os.Symlink(f.path("/outside"), f.path(linux.TemplateDir)); err != nil {
			t.Fatal(err)
		}
		if pending {
			f.answer = func(call string) (linux.Result, bool) {
				return linux.Result{ExitCode: 7}, call == "rpm -q nftables" || call == "dnf install -y nftables"
			}
		}
		err := f.host.Apply(t.Context(), protocol.DesiredState{})
		if pending {
			var command *linux.CommandError
			if !errors.As(err, &command) {
				t.Fatalf("cleanup replaced Apply's command failure: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), linux.CloneDir) {
			t.Fatalf("Apply did not return the clone cleanup failure: %v", err)
		}
		f.expectFile("/outside/clone/keep", "retained", 0o600)
	}
}

func TestApplySharesCloneAndInspectKeepsCommitsByID(t *testing.T) {
	// R-WVWX-RWVR R-X83X-LMAP R-X6W1-7UK0 R-XAJQ-D5S3
	f := newHardeningFixture(t)
	f.template["home/AGENTS.md"] = homeTemplateBody
	const rawName = "file-\xff"
	f.template["home/"+rawName] = "raw home filename"
	f.template["seed/"+rawName] = "raw seed filename"
	f.put("/srv/fixture", "fixture", 0o600)
	state := protocol.DesiredState{DataVolume: "vol-12345678", Host: "host01",
		Accounts: []protocol.Account{
			{Slug: "birch", GID: 22002, State: protocol.AccountActive},
			{Slug: "acme", GID: 22001, State: protocol.AccountActive},
		},
		Users: []protocol.User{
			{Name: "duck", Account: "birch", UID: 22005, State: protocol.UserLocked},
			{Name: "bear", Account: "acme", UID: 22004, State: protocol.UserActive},
			{Name: "bob", Account: "acme", UID: 22003, State: protocol.UserActive},
		},
	}
	var clones int
	currentCommit := fixtureCommit
	f.clone = func(_ context.Context, step string, c linux.Command) (linux.Result, error) {
		switch step {
		case "clone":
			clones++
		case "rev-parse":
			return hostAnswer(currentCommit + "\n"), nil
		}
		return f.playTemplate(step, c), nil
	}
	const firstCommitQuery = " rev-parse --verify --quiet HEAD"
	f.answer = func(call string) (linux.Result, bool) {
		return linux.Result{ExitCode: 1}, strings.HasSuffix(call, firstCommitQuery)
	}
	f.observe = func(c linux.Command) {
		if c.Name == "useradd" && c.Args[0] == "--uid" {
			name := c.Args[len(c.Args)-1]
			f.put("/home/"+name+"/created", "useradd fixture", 0o600)
			return
		}
		if c.Name != "runuser" || c.Args[1] == linux.CloneUser {
			return
		}
		program, path := c.Args[3], c.Args[len(c.Args)-1]
		switch {
		case program == "git" && c.Args[4] == "init":
			if err := os.Mkdir(filepath.Join(path, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
		case program == "tee":
			body, err := io.ReadAll(c.Stdin)
			if err != nil {
				t.Fatal(err)
			}
			f.put(strings.TrimPrefix(path, f.root), string(body), 0o600)
		}
	}
	f.apply(state)
	f.expectAbsent(linux.CloneDir)
	if clones != 1 {
		t.Fatalf("new accounts and users used %d clones", clones)
	}
	for _, a := range state.Accounts {
		f.expectFile("/srv/"+a.Slug+"/"+linux.SeedMarker, "done\n", 0o600)
		f.expectFile("/srv/"+a.Slug+"/"+rawName, "raw seed filename", 0o600)
	}
	for _, u := range state.Users {
		f.expectFile("/home/"+u.Name+"/"+rawName, "raw home filename", 0o600)
		f.expectFile("/home/"+u.Name+"/AGENTS.md", u.Name+", "+u.Account+", ("+u.Name+"), account, <nope>.\n", 0o600)
	}

	wantSeeds := []protocol.Seed{{GID: 22001, Commit: fixtureCommit}, {GID: 22002, Commit: fixtureCommit}}
	wantHomes := []protocol.Home{{UID: 22003, Commit: fixtureCommit}, {UID: 22004, Commit: fixtureCommit}, {UID: 22005, Commit: fixtureCommit}}
	check := func(host *linux.Host, s protocol.DesiredState, seeds []protocol.Seed, homes []protocol.Home) {
		t.Helper()
		report, err := host.Inspect(t.Context(), s)
		if err != nil || !slices.Equal(report.Seeds, seeds) || !slices.Equal(report.Homes, homes) {
			t.Fatalf("template report seeds %v, homes %v, error %v, want %v and %v", report.Seeds, report.Homes, err, seeds, homes)
		}
	}
	check(f.host, state, wantSeeds, wantHomes)
	check(f.host, state, wantSeeds, wantHomes)
	check(linux.New(f.env), state, nil, nil)

	var users, groups strings.Builder
	groups.WriteString("shell:x:990:duck,bear,bob\n")
	for _, a := range state.Accounts {
		fmt.Fprintf(&users, "%s:x:%d:%d::/srv/%s:/usr/sbin/nologin\n", a.Slug, a.GID, a.GID, a.Slug)
		fmt.Fprintf(&groups, "%s:x:%d:\n", a.Slug, a.GID)
	}
	for _, u := range state.Users {
		fmt.Fprintf(&users, "%s:x:%d:%d::/home/%s:/bin/bash\n", u.Name, u.UID, u.UID, u.Name)
		fmt.Fprintf(&groups, "%s:x:%d:\n", u.Name, u.UID)
	}
	f.put("/etc/passwd", users.String(), 0o644)
	f.put("/etc/group", groups.String(), 0o644)
	f.commands = nil
	f.apply(state)
	f.forbidCalls("runuser -u "+linux.CloneUser+" ", "useradd ", "groupadd --gid ",
		"chown --no-dereference root:root "+f.path(linux.TemplateDir),
		"chown nobody:nobody "+f.path(linux.CloneDir), "chown -R --no-dereference root:root "+f.path(linux.CloneDir))
	check(f.host, state, wantSeeds, wantHomes)

	// Recreate the same ids from a different clone to check replacement.
	for _, path := range []string{"/srv/birch", "/home/duck"} {
		if err := os.RemoveAll(f.path(path)); err != nil {
			t.Fatal(err)
		}
	}
	var remainingPasswd strings.Builder
	for line := range strings.SplitSeq(users.String(), "\n") {
		if line != "" && !strings.HasPrefix(line, "duck:") {
			remainingPasswd.WriteString(line + "\n")
		}
	}
	f.put("/etc/passwd", remainingPasswd.String(), 0o644)
	currentCommit = "abcdef0123456789abcdef0123456789abcdef01"
	f.apply(state)
	if clones != 2 {
		t.Fatalf("recreated account and user used %d total clones, want 2", clones)
	}
	wantSeeds[1].Commit = currentCommit
	wantHomes[2].Commit = currentCommit
	check(f.host, state, wantSeeds, wantHomes)

	other := state
	other.Accounts = slices.Clone(state.Accounts)
	other.Accounts[0].GID++
	other.Accounts[1].State = protocol.AccountRemoved
	other.Users = slices.Clone(state.Users)
	other.Users[0].UID = 22100
	other.Users[1].State = protocol.UserDeleted
	check(f.host, other, nil, wantHomes[:1])
	f.apply(protocol.DesiredState{DataVolume: state.DataVolume})
	check(f.host, state, nil, nil)
}

func TestApplyRefusesACloneAncestorSymlink(t *testing.T) {
	// R-X20F-ORL8 R-XP6I-YEOF
	for _, needed := range []bool{false, true} {
		f, state := newHomeFixture(t)
		if !needed {
			state = protocol.DesiredState{}
		}
		outside := t.TempDir()
		if err := os.MkdirAll(filepath.Join(outside, "template", "clone"), 0o700); err != nil {
			t.Fatal(err)
		}
		keep := filepath.Join(outside, "template", "clone", "keep")
		if err := os.WriteFile(keep, []byte("retained"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(f.path("/var/lib"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, f.path("/var/lib/accountsd")); err != nil {
			t.Fatal(err)
		}
		if err := f.host.Apply(t.Context(), state); err == nil {
			t.Fatal("Apply accepted a symlink to the template ancestor")
		}
		data, err := os.ReadFile(filepath.Clean(keep))
		if err != nil || string(data) != "retained" {
			t.Fatalf("outside clone file changed: %q, %v", data, err)
		}
		f.forbidCalls("runuser -u " + linux.CloneUser + " ")
	}
}

func TestApplyCloneFailureLeavesExistingPendingSeed(t *testing.T) {
	// R-WVWX-RWVR R-XWHX-914L
	f := newSeedFixture(t)
	f.state.TemplateURL, f.state.TemplateRef = "https://example.test/team", "feature"
	f.put(seedMarker, seedPending, 0o600)
	f.head = 1
	f.clone = func(_ context.Context, step string, _ linux.Command) (linux.Result, error) {
		if step != "clone" || !slices.Contains(f.commands, f.headQuery()) {
			t.Fatalf("clone step %q before the existing folder reported its pending HEAD", step)
		}
		return linux.Result{ExitCode: 128}, nil
	}
	err := f.host.Apply(t.Context(), f.state)
	if failure := checkTemplateFailure(t, err, f.state, ""); failure.Err == nil {
		t.Fatal("missing clone failure cause")
	}
	f.expectSeedCommands(f.chown(seedFolder), f.acl(seedFolder), f.headQuery())
	safeDir := "git config --file " + f.path("/etc/gitconfig") + " --add safe.directory " + seedFolder
	if !slices.Contains(f.commands, safeDir) {
		t.Fatalf("omitted safe.directory command after pending seed clone failed: %q", safeDir)
	}
	f.expectFile(seedMarker, seedPending, 0o600)
	f.expectAbsent(linux.CloneDir)
}

func TestApplyTemplateDeadlineSkipsCreationButLaunchFailureStops(t *testing.T) {
	// R-WX4U-5OMG R-XWHX-914L R-S739-S0BW
	for _, tc := range []struct {
		name     string
		cause    error
		deadline bool
	}{
		{"clone deadline", context.DeadlineExceeded, true},
		{"cannot start runuser", os.ErrNotExist, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, state := newHomeFixture(t)
			state.TemplateURL, state.TemplateRef = "https://example.test/team", "feature"
			state.Users = append(state.Users, protocol.User{Name: "alice", Account: "acme", UID: 20002, State: protocol.UserActive})
			f.put("/etc/passwd", homeAccountEntry+"alice:x:20002:20002::/home/alice:/bin/bash\n", 0o644)
			f.clone = func(_ context.Context, step string, _ linux.Command) (linux.Result, error) {
				if step != "clone" {
					t.Fatalf("ran %s after failed clone", step)
				}
				return linux.Result{ExitCode: -1}, tc.cause
			}
			err := f.host.Apply(t.Context(), state)
			failure := checkTemplateFailure(t, err, state, "")
			var command *linux.CommandError
			if !errors.As(failure, &command) || command.Label != "runuser" || !errors.Is(command, tc.cause) {
				t.Fatalf("Apply error = %v, want runuser error wrapping %v", err, tc.cause)
			}
			if tc.deadline {
				f.expectFile(linux.KeysDir+"/alice", "", 0o644)
			} else {
				f.expectAbsent(linux.KeysDir + "/alice")
				if !strings.HasPrefix(f.commands[len(f.commands)-1], "runuser -u "+linux.CloneUser+" ") {
					t.Fatal("Apply ran commands after the launch failure")
				}
			}
			f.expectAbsent(homeKeys)
			f.expectAbsent(linux.CloneDir)
		})
	}
}
