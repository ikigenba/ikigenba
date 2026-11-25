package linux_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	claudeSettingsPath = "/etc/claude-code/managed-settings.json"
	claudeMarketDir    = "/usr/local/share/accountsd/claude"
	claudePluginDir    = claudeMarketDir + "/plugins/accounts"
	claudeRepoPath     = "/etc/yum.repos.d/claude-code.repo"
	claudeKeyProbe     = "rpm -q gpg-pubkey-1a7ecace"
	claudePackageProbe = "rpm -q claude-code"
	claudeDnfFlags     = " -y --setopt=claude-code.gpgkey= claude-code"
	claudeInstall      = "dnf install" + claudeDnfFlags
	claudeUpgrade      = "dnf upgrade" + claudeDnfFlags
)

var claudeDirectories = []string{
	"/etc/claude-code", "/usr/local/share/accountsd", claudeMarketDir, claudeMarketDir + "/.claude-plugin",
	claudeMarketDir + "/plugins", claudePluginDir, claudePluginDir + "/.claude-plugin",
}

var claudeFiles = []struct{ path, text string }{
	{claudeSettingsPath, `{"extraKnownMarketplaces":{"ikigenba":{"source":{"source":"directory","path":"/usr/local/share/accountsd/claude"}}},"enabledPlugins":{"accounts@ikigenba":true}}`},
	{claudeMarketDir + "/.claude-plugin/marketplace.json", `{"name":"ikigenba","owner":{"name":"ikigenba"},"plugins":[{"name":"accounts","source":"./plugins/accounts"}]}`},
	{claudePluginDir + "/.claude-plugin/plugin.json", `{"name":"accounts","description":"Manage your SSH keys and, as an admin, your account's users."}`},
	{claudePluginDir + "/.mcp.json", `{"mcpServers":{"accounts":{"command":"/usr/local/libexec/accountsd-mcp"}}}`},
}

func claudeFilePaths() []string {
	paths := make([]string, 0, len(claudeFiles))
	for _, f := range claudeFiles {
		paths = append(paths, f.path)
	}
	return paths
}

func TestMCPHelperLink(t *testing.T) {
	// R-SHMW-U4AB R-OD2V-MRI2
	if linux.MCPName != "accountsd-mcp" || linux.MCPHelper != "/usr/local/libexec/accountsd-mcp" {
		t.Fatal("unexpected MCP helper name or location")
	}
	for _, fixture := range []string{"absent", "regular file", "different link"} {
		t.Run(fixture, func(t *testing.T) {
			f := newHardeningFixture(t)
			path := f.path(linux.MCPHelper)
			if fixture != "absent" {
				f.put(linux.MCPHelper, "old helper", 0o644)
			}
			if fixture == "different link" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/elsewhere", path); err != nil {
					t.Fatal(err)
				}
			}
			var original fs.FileInfo
			chown := "chown --no-dereference root:root " + path
			for range 2 {
				f.commands = nil
				f.apply(protocol.DesiredState{})
				target, err := os.Readlink(path)
				if err != nil || target != linux.DaemonPath {
					t.Fatalf("helper target = %q, %v", target, err)
				}
				current, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if original != nil && !os.SameFile(original, current) {
					t.Fatal("same state replaced helper symlink")
				}
				original = current
				owner := snapshotHost(t, f.root)[linux.MCPHelper]
				var calls []string
				if owner.uid != 0 || owner.gid != 0 {
					calls = []string{chown}
				}
				var got []string
				for _, call := range f.commands {
					if strings.Contains(call, path) {
						got = append(got, call)
					}
				}
				if !slices.Equal(got, calls) {
					t.Fatalf("helper commands = %q, want %q", got, calls)
				}
			}
		})
	}
}

func TestClaudeSeams(t *testing.T) {
	// R-SK2P-LNRP R-SMII-D793
	if linux.ClaudeKeyURL != "https://downloads.claude.ai/keys/claude-code.asc" ||
		linux.ClaudeKeyFingerprint != "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE" ||
		linux.ClaudeKeyFile != "/var/lib/accountsd/claude-code.asc" || linux.GnupgHome != "/var/lib/accountsd/gnupg" ||
		linux.ClaudeStamp != "/var/lib/accountsd/claude-code-upgraded" || linux.MaxKeyBytes != 64<<10 ||
		linux.FetchTimeout != time.Minute || linux.ClaudeUpgradeInterval != 24*time.Hour || linux.ClaudeRetry != time.Hour {
		t.Fatal("unexpected Claude Code constants")
	}
	if linux.DiagClaudeCode == "" || linux.ErrClaudeKey == nil {
		t.Fatal("Claude Code diagnostics were not set")
	}
	for _, err := range []error{machine.ErrNoDataVolume, machine.ErrDigestMismatch, machine.ErrNotExecutable,
		machine.ErrTooLarge, machine.ErrIdentity, machine.ErrInvalidState} {
		if errors.Is(err, linux.ErrClaudeKey) {
			t.Fatalf("ErrClaudeKey matches %v", err)
		}
	}
}

func TestGet(t *testing.T) {
	// R-SLAL-ZFIE R-SNQE-QYZS
	get := linux.Get
	const payload = "download\n\x00"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q", r.Method)
		}
		switch r.URL.Path {
		case "/key":
			_, _ = io.WriteString(w, payload)
		case "/redirect":
			http.Redirect(w, r, "/unexpected", http.StatusFound)
		case "/unexpected":
			t.Error("Get followed redirect")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	body, err := get(t.Context(), server.URL+"/key")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(body)
	if closeErr := body.Close(); string(got) != payload || readErr != nil || closeErr != nil {
		t.Fatalf("body = %q, read error = %v, close error = %v", got, readErr, closeErr)
	}
	expectFailure := func(ctx context.Context, url string) {
		t.Helper()
		body, err := get(ctx, url)
		if body != nil || err == nil {
			t.Fatalf("Get returned (%v, %v) for %s", body, err, url)
		}
	}
	expectFailure(t.Context(), server.URL+"/missing")
	expectFailure(t.Context(), server.URL+"/redirect")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	expectFailure(ctx, server.URL+"/key")
	server.Close()
	expectFailure(t.Context(), server.URL+"/key")
}

func TestGetReturnsUnreadStream(t *testing.T) {
	// R-SNQE-QYZS
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush response headers: %v", err)
			return
		}
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		_, _ = io.WriteString(w, "sent after Get returned")
	}))
	t.Cleanup(server.Close)
	body, err := linux.Get(ctx, server.URL)
	if err != nil {
		t.Fatalf("Get waited for the body: %v", err)
	}
	close(release)
	got, readErr := io.ReadAll(body)
	if closeErr := body.Close(); string(got) != "sent after Get returned" || readErr != nil || closeErr != nil {
		t.Fatalf("stream = %q, read error = %v, close error = %v", got, readErr, closeErr)
	}
}

func TestGetClosesRejectedStream(t *testing.T) {
	// R-SNQE-QYZS
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	disconnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush response headers: %v", err)
			return
		}
		<-r.Context().Done()
		close(disconnected)
	}))
	t.Cleanup(server.Close)
	body, err := linux.Get(ctx, server.URL)
	if body != nil || err == nil {
		if body != nil {
			_ = body.Close()
		}
		t.Fatalf("Get returned (%v, %v) for 404", body, err)
	}
	select {
	case <-disconnected:
		if ctx.Err() != nil {
			t.Fatal("response stream stayed open until the request timed out")
		}
	case <-ctx.Done():
		t.Fatal("response stream stayed open after Get returned")
	}
}

type claudeFixture struct {
	*hardeningFixture
	stderr bytes.Buffer
	now    time.Time
}

func newClaudeFixture(t *testing.T) *claudeFixture {
	t.Helper()
	f := &claudeFixture{hardeningFixture: newHardeningFixture(t), now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	f.env.Now = func() time.Time { return f.now }
	f.env.Stderr = &f.stderr
	f.env.Fetch = func(_ context.Context, url string) (io.ReadCloser, error) {
		t.Fatalf("unexpected fetch of %s", url)
		return nil, nil
	}
	f.host = linux.New(f.env)
	return f
}

func (f *claudeFixture) cycle() {
	f.t.Helper()
	f.commands = nil
	f.stderr.Reset()
	f.apply(protocol.DesiredState{DataVolume: "vol-12345678"})
}

func (f *claudeFixture) expectDiag(err error) {
	f.t.Helper()
	want := "accountsd: " + linux.DiagClaudeCode + ": " + strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error()) + "\n"
	if f.stderr.String() != want {
		f.t.Fatalf("stderr = %q, want %q", f.stderr.String(), want)
	}
}

func (f *claudeFixture) expectPackageCalls(want ...string) {
	f.t.Helper()
	var got []string
	for _, call := range f.commands {
		switch call {
		case claudeKeyProbe, claudePackageProbe, claudeInstall, claudeUpgrade:
			got = append(got, call)
		default:
			if strings.HasPrefix(call, "dnf ") {
				f.t.Fatalf("unexpected dnf command %q", call)
			}
		}
	}
	if !slices.Equal(got, want) {
		f.t.Fatalf("Claude Code package commands = %v, want %v", got, want)
	}
}

func TestApplyClaudeConfigurationAndInstall(t *testing.T) {
	// R-SOYB-4QQH R-MH7T-0G2G
	f := newClaudeFixture(t)
	for _, file := range claudeFiles {
		f.put(file.path, "incorrect bytes", 0o600)
	}
	for _, dir := range claudeDirectories {
		f.setMode(dir, 0o700)
	}
	f.put("/home/existing/.claude/keep", "home settings", 0o600)
	f.put(linux.SkelDir+"/keep", "skeleton data", 0o600)
	f.answer = func(call string) (linux.Result, bool) {
		if call == claudeKeyProbe {
			for _, file := range claudeFiles {
				f.expectFile(file.path, file.text, 0o644)
			}
			for _, dir := range claudeDirectories {
				f.expectMode(dir, 0o755)
			}
		}
		if call == claudePackageProbe {
			f.expectFile(claudeRepoPath, "[claude-code]\nname=Claude Code\nbaseurl=https://downloads.claude.ai/claude-code/rpm/stable\nenabled=1\ngpgcheck=1\ngpgkey=https://downloads.claude.ai/keys/claude-code.asc\n", 0o644)
			return linux.Result{ExitCode: 1}, true
		}
		return linux.Result{}, false
	}
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeInstall)
	f.forbidCalls("gpg ", "rpmkeys ")
	f.expectFile(linux.ClaudeStamp, f.now.Format(time.RFC3339)+"\n", 0o644)
	snapshot := snapshotApplyHost(t, f.root)
	f.answer = nil
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe)
	assertHostChanges(t, snapshot, snapshotApplyHost(t, f.root), nil)
	f.expectFile("/home/existing/.claude/keep", "home settings", 0o600)
	f.expectFile(linux.SkelDir+"/keep", "skeleton data", 0o600)
}

func TestApplyClaudeCreatesOnlyMachineWideConfiguration(t *testing.T) {
	// R-SOYB-4QQH
	f := newClaudeFixture(t)
	f.put("/home/existing/.claude/keep", "home settings", 0o600)
	if err := os.Mkdir(f.path("/home/fresh"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.put("/srv/citron/.git/config", "repo", 0o600)
	f.put("/etc/group", "citron:x:21000:existing,fresh\nshell:x:990:existing,fresh\nexisting:x:21001:\nfresh:x:21002:\n", 0o644)
	f.put("/etc/passwd", "citron:x:21000:21000::/srv/citron:/usr/sbin/nologin\n"+
		"existing:x:21001:21001::/home/existing:/bin/bash\nfresh:x:21002:21002::/home/fresh:/bin/bash\n", 0o644)
	protectedSnapshot := func() map[string]applyHostFile {
		files := snapshotApplyHost(t, f.root)
		for path := range files {
			if path != "/home" && !strings.HasPrefix(path, "/home/") && !strings.HasPrefix(path, linux.SkelDir+"/") {
				delete(files, path)
			}
		}
		return files
	}
	before := protectedSnapshot()
	f.apply(protocol.DesiredState{
		DataVolume: "vol-12345678",
		Accounts:   []protocol.Account{{Slug: "citron", GID: 21000, State: protocol.AccountActive}},
		Users: []protocol.User{
			{Name: "existing", Account: "citron", UID: 21001, State: protocol.UserActive},
			{Name: "fresh", Account: "citron", UID: 21002, State: protocol.UserActive},
		},
	})
	for _, file := range claudeFiles {
		f.expectFile(file.path, file.text, 0o644)
	}
	for _, dir := range claudeDirectories {
		f.expectMode(dir, 0o755)
	}
	f.expectMode(linux.SkelDir, 0o755)
	assertHostChanges(t, before, protectedSnapshot(), nil)
}

type keyBody struct {
	io.Reader
	bytesRead int
	closes    int
}

func (b *keyBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytesRead += n
	return n, err
}

func (b *keyBody) Close() error {
	b.closes++
	return nil
}

type failingKeyReader struct{ err error }

func (r failingKeyReader) Read([]byte) (int, error) { return 0, r.err }

func TestApplyClaudeDownloadsKey(t *testing.T) {
	// R-MFZW-MOBR R-CSTR-M40L R-SSM0-A1YK
	const pinnedListing = "pub:fixture\nfpr:::::::::" + linux.ClaudeKeyFingerprint + ":\nsub:fixture\nfpr:::::::::SUBKEY:\n"
	for _, tc := range []struct {
		name, listing string
		imported      bool
	}{
		{"pinned primary", pinnedListing, true},
		{"unrelated fingerprint", strings.Replace(pinnedListing, linux.ClaudeKeyFingerprint, "UNRELATED", 1), false},
		{"two primaries", pinnedListing + "pub:another\n", false},
		{"missing fingerprint", "pub:something\n", false},
		{"no primary", "fpr:::::::::" + linux.ClaudeKeyFingerprint + ":\n", false},
		{"first fingerprint wrong", "pub:fixture\nfpr:::::::::UNRELATED:\nfpr:::::::::" + linux.ClaudeKeyFingerprint + ":\n", false},
		{"capture at byte limit", pinnedListing + strings.Repeat("x", linux.MaxCapture-len(pinnedListing)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClaudeFixture(t)
			key := strings.Repeat("k", linux.MaxKeyBytes)
			body := &keyBody{Reader: strings.NewReader(key)}
			fetches := 0
			f.env.Fetch = func(ctx context.Context, url string) (io.ReadCloser, error) {
				fetches++
				deadline, ok := ctx.Deadline()
				if url != linux.ClaudeKeyURL || !ok || deadline.After(time.Now().Add(linux.FetchTimeout)) {
					t.Fatalf("fetch called with %q, deadline %v (set %t)", url, deadline, ok)
				}
				f.expectMode(linux.GnupgHome, 0o700)
				f.expectAbsent(claudeRepoPath)
				return body, nil
			}
			f.host = linux.New(f.env)
			gpg := "gpg --homedir " + f.path(linux.GnupgHome) + " --batch --show-keys --with-colons " + f.path(linux.ClaudeKeyFile)
			importKey := "rpmkeys --import " + f.path(linux.ClaudeKeyFile)
			var keyCommands []string
			f.answer = func(call string) (linux.Result, bool) {
				switch call {
				case claudeKeyProbe:
					keyCommands = append(keyCommands, call)
					return linux.Result{ExitCode: 1}, true
				case gpg:
					keyCommands = append(keyCommands, call)
					f.expectFile(linux.ClaudeKeyFile, key, 0o600)
					if body.closes != 1 {
						t.Fatalf("body closed %d times before gpg", body.closes)
					}
					return hostAnswer(tc.listing), true
				case importKey:
					keyCommands = append(keyCommands, call)
					f.expectFile(linux.ClaudeKeyFile, key, 0o600)
				case claudePackageProbe:
					f.expectAbsent(linux.ClaudeKeyFile)
				}
				return linux.Result{}, false
			}
			f.cycle()
			f.expectAbsent(linux.ClaudeKeyFile)
			if fetches != 1 || body.closes != 1 || body.bytesRead != len(key) {
				t.Fatalf("fetches = %d, closes = %d, bytes = %d", fetches, body.closes, body.bytesRead)
			}
			want := []string{claudeKeyProbe, gpg}
			if tc.imported {
				want = append(want, importKey)
				f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeUpgrade)
				if f.stderr.Len() != 0 {
					t.Fatalf("unexpected diagnostic %q", f.stderr.String())
				}
			} else {
				f.expectPackageCalls(claudeKeyProbe)
				f.expectAbsent(claudeRepoPath)
				f.expectDiag(linux.ErrClaudeKey)
			}
			if !slices.Equal(keyCommands, want) {
				t.Fatalf("key commands = %v, want %v", keyCommands, want)
			}
		})
	}
}

func TestApplyClaudeRefusesFailedAndOversizeDownloads(t *testing.T) {
	// R-SSM0-A1YK
	broken := errors.New("read error")
	fetchErr := errors.New("fetch error")
	for _, tc := range []struct {
		name   string
		reader io.Reader
		fetch  error
		err    error
		read   int
	}{
		{"large", strings.NewReader(strings.Repeat("x", 2*linux.MaxKeyBytes)), nil, machine.ErrTooLarge, linux.MaxKeyBytes + 1},
		{"read failed", io.MultiReader(strings.NewReader("a"), failingKeyReader{broken}), nil, broken, 1},
		{"fetch failed", strings.NewReader(""), fetchErr, fetchErr, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClaudeFixture(t)
			f.put(linux.ClaudeKeyFile, "stale key", 0o600)
			body := &keyBody{Reader: tc.reader}
			f.env.Fetch = func(context.Context, string) (io.ReadCloser, error) { return body, tc.fetch }
			f.host = linux.New(f.env)
			f.answer = func(call string) (linux.Result, bool) {
				return linux.Result{ExitCode: 1}, call == claudeKeyProbe
			}
			f.cycle()
			wantError := tc.err
			if tc.fetch == nil {
				wantError = fmt.Errorf("%s: %w", linux.ClaudeKeyFile, tc.err)
			}
			f.expectDiag(wantError)
			f.expectPackageCalls(claudeKeyProbe)
			f.forbidCalls("gpg ", "rpmkeys ")
			f.expectAbsent(linux.ClaudeKeyFile)
			f.expectAbsent(claudeRepoPath)
			if body.bytesRead != tc.read || body.closes != 1 {
				t.Fatalf("body read %d, closed %d times", body.bytesRead, body.closes)
			}
		})
	}
}

func TestApplyClaudeFailureIsNonfatalAndPaced(t *testing.T) {
	// R-MIFP-E7T5 R-MM3E-JJ18 R-MKVI-5RAJ
	f := newClaudeFixture(t)
	key := fixtureKey("new user")
	const name = "alice"
	state := protocol.DesiredState{DataVolume: "vol-12345678",
		Accounts: []protocol.Account{{Slug: "citron", GID: 21000, State: protocol.AccountActive}},
		Users:    []protocol.User{{Name: name, Account: "citron", UID: 21001, State: protocol.UserActive, Keys: []string{key}}},
	}
	f.put("/srv/citron/.git/config", "fixture repo", 0o600)
	outcome := linux.Result{ExitCode: 1, Stderr: []byte("failed\n")}
	f.answer = func(call string) (linux.Result, bool) {
		return outcome, call == claudePackageProbe || call == claudeInstall
	}
	f.apply(state)
	f.expectDiag(&linux.CommandError{Label: "dnf install", Result: outcome})
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeInstall)
	f.expectFile(linux.KeysDir+"/"+name, key+"\n", 0o644)
	f.expectFile("/etc/systemd/system/"+linux.PersonSlice("citron", name), "[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n", 0o644)
	if !slices.Contains(f.commands, "useradd --uid 21001 --gid alice --groups shell,citron --create-home --home-dir "+f.path("/home/alice")+" --skel "+f.path(linux.SkelDir)+" --key HOME_MODE=0700 --shell /bin/bash alice") {
		t.Fatal("user was not applied before the failed install")
	}
	f.expectAbsent(linux.ClaudeStamp)
	f.now = f.now.Add(linux.ClaudeRetry - time.Second)
	f.put(claudeSettingsPath, "repair during pacing", 0o600)
	f.cycle()
	f.expectPackageCalls()
	f.forbidCalls("gpg ", "rpmkeys ")
	f.expectFile(claudeSettingsPath, claudeFiles[0].text, 0o644)
	if f.stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostic %q during pacing", f.stderr.String())
	}
	f.now = f.now.Add(time.Second)
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeInstall)
	f.expectDiag(&linux.CommandError{Label: "dnf install", Result: outcome})
	f.now = f.now.Add(-time.Second)
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeInstall)
}

func TestApplyClaudeReportsOtherFailures(t *testing.T) {
	// R-MIFP-E7T5 R-MFZW-MOBR
	f := newClaudeFixture(t)
	cmdErr := errors.New("cannot execute\r\ncommand")
	execute := f.env.Execute
	f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
		res, err := execute(ctx, c)
		if c.Name == "rpm" && slices.Equal(c.Args, []string{"-q", "gpg-pubkey-1a7ecace"}) {
			return linux.Result{}, cmdErr
		}
		return res, err
	}
	f.host = linux.New(f.env)
	f.cycle()
	f.expectDiag(&linux.CommandError{Label: "rpm", Err: cmdErr})
	f.expectPackageCalls(claudeKeyProbe)
	f.expectAbsent(claudeRepoPath)

	f.host = linux.New(f.env)
	if err := os.Remove(f.path(claudeSettingsPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.path(claudeSettingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	f.cycle()
	f.expectDiag(errors.New(claudeSettingsPath + ": " + fs.ErrInvalid.Error()))
	f.expectPackageCalls()
	f.expectAbsent(claudeRepoPath)

	f.env.Stderr = nil
	f.host = linux.New(f.env)
	f.cycle()
	if f.stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostic %q without stderr", f.stderr.String())
	}
}

func TestApplyClaudeUpgradePersistsAcrossRestart(t *testing.T) {
	// R-MES0-8WL2 R-MH7T-0G2G
	f := newClaudeFixture(t)
	f.answer = func(call string) (linux.Result, bool) {
		return linux.Result{ExitCode: 1}, call == claudeUpgrade
	}
	f.now = f.now.In(time.FixedZone("fixture", 3600)).Add(555 * time.Millisecond)
	f.cycle()
	f.expectDiag(&linux.CommandError{Label: "dnf upgrade", Result: linux.Result{ExitCode: 1}})
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeUpgrade)
	if last := f.commands[len(f.commands)-1]; last != claudeUpgrade {
		t.Fatalf("ran %q after dnf upgrade failed", last)
	}
	f.expectFile(linux.ClaudeStamp, "2026-11-24T12:00:00Z\n", 0o644)
	f.host = linux.New(f.env)
	f.now = f.now.Add(linux.ClaudeUpgradeInterval - time.Second)
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe)
	f.now = f.now.Add(time.Second)
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeUpgrade)
	f.expectFile(linux.ClaudeStamp, "2026-11-25T12:00:00Z\n", 0o644)

	f.host = linux.New(f.env)
	f.now = f.now.Add(-linux.ClaudeUpgradeInterval)
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeUpgrade)
	f.expectFile(linux.ClaudeStamp, "2026-11-24T12:00:00Z\n", 0o644)
}

func stagedStampPath(f *hardeningFixture, command linux.Command) string {
	if command.Name != "chown" || len(command.Args) != 3 ||
		command.Args[0] != "--no-dereference" || command.Args[1] != "root:root" {
		return ""
	}
	path := command.Args[2]
	if filepath.Dir(path) == filepath.Dir(f.path(linux.ClaudeStamp)) && strings.HasPrefix(filepath.Base(path), ".accountsd-") {
		return path
	}
	return ""
}

func TestApplyClaudePreparesStampOwnershipBeforeFailedUpgrade(t *testing.T) {
	// R-KWQG-UW9Z R-BU0D-L2QY R-MES0-8WL2 R-MIFP-E7T5
	f := newClaudeFixture(t)
	var staged string
	var inode fs.FileInfo
	f.observe = func(c linux.Command) {
		if path := stagedStampPath(f.hardeningFixture, c); path != "" {
			if staged != "" {
				t.Fatal("staged ownership more than once")
			}
			staged = path
			var err error
			inode, err = os.Lstat(staged)
			if err != nil || !inode.Mode().IsRegular() || inode.Size() != 0 {
				t.Fatalf("before ownership command, staged stamp was not an empty file: %v, %v", inode, err)
			}
		}
	}
	f.answer = func(call string) (linux.Result, bool) {
		if call != claudeUpgrade {
			return linux.Result{}, false
		}
		f.expectAbsent(linux.ClaudeStamp)
		if staged != "" {
			current, err := os.Lstat(staged)
			if err != nil || !os.SameFile(inode, current) || current.Size() != 0 {
				t.Fatalf("before dnf, staged stamp changed: %v, %v", current, err)
			}
		}
		f.now = f.now.Add(time.Second)
		return linux.Result{ExitCode: 1}, true
	}
	f.cycle()
	f.expectDiag(&linux.CommandError{Label: "dnf upgrade", Result: linux.Result{ExitCode: 1}})
	if f.commands[len(f.commands)-1] != claudeUpgrade {
		t.Fatalf("commands ran after failed dnf: %v", f.commands)
	}
	f.expectFile(linux.ClaudeStamp, "2026-11-24T12:00:01Z\n", 0o644)
	owner := snapshotHost(t, f.root)[linux.ClaudeStamp]
	if (owner.uid != 0 || owner.gid != 0) && staged == "" {
		t.Fatal("dnf ran before ownership of the required stamp was prepared")
	}
	if staged != "" {
		assertUnchanged(t, f.path(linux.ClaudeStamp), inode)
		if _, err := os.Lstat(staged); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("staged stamp remains at %s: %v", staged, err)
		}
	}
}

func TestApplyClaudeKeepsIdenticalStampWhenClockChangesDuringUpgrade(t *testing.T) {
	// R-BU0D-L2QY R-MES0-8WL2
	f := newClaudeFixture(t)
	const existing = "2026-11-24T13:00:00Z\n"
	f.put(linux.ClaudeStamp, existing, 0o600)
	inode := f.stat(linux.ClaudeStamp)
	f.answer = func(call string) (linux.Result, bool) {
		if call != claudeUpgrade {
			return linux.Result{}, false
		}
		f.now = f.now.Add(time.Hour)
		return linux.Result{ExitCode: 1}, true
	}
	f.cycle()
	f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeUpgrade)
	f.expectFile(linux.ClaudeStamp, existing, 0o644)
	assertUnchanged(t, f.path(linux.ClaudeStamp), inode)
}

func TestApplyClaudeUpgradeFromInvalidStamp(t *testing.T) {
	// R-MES0-8WL2
	for _, tc := range []struct {
		name, text string
		mode       fs.FileMode
	}{
		{"too long", strings.Repeat("z", linux.MaxCapture+1), 0o644},
		{"wrong layout", "2026-11-24 12:00:00Z\n", 0o644},
		{"offset hours out of range", "2026-11-25T12:00:00+24:00\n", 0o644},
		{"offset minutes out of range", "2026-11-24T13:00:00+00:60\n", 0o644},
		{"missing LF", "2026-11-24T12:00:00Z", 0o644},
		{"unreadable", "2026-11-24T12:00:00Z\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClaudeFixture(t)
			f.put(linux.ClaudeStamp, tc.text, tc.mode)
			execute := f.env.Execute
			cannotRun := errors.New("no dnf")
			f.env.Execute = func(ctx context.Context, c linux.Command) (linux.Result, error) {
				res, err := execute(ctx, c)
				if c.Name == "dnf" && slices.Equal(c.Args, strings.Fields(claudeUpgrade)[1:]) {
					return linux.Result{}, cannotRun
				}
				return res, err
			}
			f.host = linux.New(f.env)
			f.cycle()
			f.expectPackageCalls(claudeKeyProbe, claudePackageProbe, claudeUpgrade)
			f.expectDiag(&linux.CommandError{Label: "dnf upgrade", Err: cannotRun})
			f.expectFile(linux.ClaudeStamp, f.now.Format(time.RFC3339)+"\n", 0o644)
		})
	}
}
