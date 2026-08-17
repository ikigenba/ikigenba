package opsctl

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-CIUC-KW66
func TestSetupRejectsDefaultWithFragmentBeforeProvisioning(t *testing.T) {
	const app = "dashboard"
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := &Opsctl{
		Root:    root,
		SysRoot: sysRoot,
		System:  sys,
		Out:     io.Discard,
		Err:     io.Discard,
	}
	l := NewLayoutSys(root, sysRoot, app)

	err := o.Setup(context.Background(), SetupOptions{
		App:       app,
		Fragment:  "location / { proxy_pass http://127.0.0.1:3000; }\n",
		IsDefault: true,
	})
	if err == nil {
		t.Fatal("setup accepted --default with --fragment, want refusal")
	}
	if got := err.Error(); !strings.Contains(got, "--default") || !strings.Contains(got, "--fragment") {
		t.Fatalf("error = %q, want both --default and --fragment named", got)
	}
	if got := sys.opSeq(); len(got) != 0 {
		t.Fatalf("setup performed privileged ops before refusing: %v", got)
	}
	for _, path := range []string{l.AppDir(), l.UnitPath(), l.FragmentPath(), l.ApexBlockPath()} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s exists after refused setup, want absent (err=%v)", path, statErr)
		}
	}
}

func newSetupTestOpsctl(t *testing.T, app string) (*Opsctl, *stubSystem, Layout) {
	t.Helper()

	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	o := &Opsctl{
		Root:    root,
		SysRoot: sysRoot,
		System:  sys,
		Out:     io.Discard,
		Err:     io.Discard,
	}
	l := NewLayoutSys(root, sysRoot, app)
	if err := os.MkdirAll(l.LocationsDir(), 0o755); err != nil {
		t.Fatalf("create locations dir: %v", err)
	}
	return o, sys, l
}

// R-CK28-YNWV
func TestSetupDefaultCreatesAppTreeAndEnabledUnitWithoutWorkerWebGroup(t *testing.T) {
	const app = "dashboard"
	o, sys, l := newSetupTestOpsctl(t, app)

	if err := o.Setup(context.Background(), SetupOptions{App: app, IsDefault: true}); err != nil {
		t.Fatalf("setup default: %v", err)
	}

	for _, dir := range []string{
		l.AppDir(),
		l.BinDir(),
		l.EtcDir(),
		l.LibexecDir(),
		l.CacheDir(),
		l.BackupsDir(),
		l.StateDir(),
	} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("default tree dir %s not materialized: %v", dir, err)
		}
	}
	assertMode(t, l.StateDir(), 0o750)
	if _, err := os.Stat(l.WWWDir()); !os.IsNotExist(err) {
		t.Fatalf("default setup created worker www dir %s, want absent (err=%v)", l.WWWDir(), err)
	}
	if _, err := os.Stat(l.DBPath()); !os.IsNotExist(err) {
		t.Fatalf("default setup created worker db %s, want absent (err=%v)", l.DBPath(), err)
	}
	if got := readRepoFile(t, l.UnitPath()); got != expectedUnit(app) {
		t.Fatalf("default unit mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, expectedUnit(app))
	}

	for _, op := range sys.opSeq() {
		if op == "ensure-group:web" {
			t.Fatalf("default setup requested web group: %v", sys.opSeq())
		}
		if strings.HasPrefix(op, "chown:"+app+":web:") {
			t.Fatalf("default setup requested worker web ownership: %v", sys.opSeq())
		}
	}
	wantOps := []string{
		"ensure-user:" + app + ":" + l.AppDir(),
		"chown:" + app + ":" + app + ":" + l.CacheDir(),
		"daemon-reload",
		"enable:" + app + ".service",
	}
	if got := sys.opSeq(); strings.Join(got, "|") != strings.Join(wantOps, "|") {
		t.Fatalf("default setup ops = %v, want %v", got, wantOps)
	}
}

func TestSetupMakesCacheOwnedByAppInEveryTreeBranch(t *testing.T) {
	// R-4ZI0-4CH5
	tests := []struct {
		name string
		opts SetupOptions
	}{
		{name: "default", opts: SetupOptions{App: "dashboard", IsDefault: true}},
		{name: "worker", opts: SetupOptions{App: "worker"}},
		{name: "fragment", opts: SetupOptions{App: "api", Fragment: "location /api/ { proxy_pass http://127.0.0.1:3000; }\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, sys, l := newSetupTestOpsctl(t, tt.opts.App)
			if err := o.Setup(context.Background(), tt.opts); err != nil {
				t.Fatalf("setup %s: %v", tt.name, err)
			}

			want := "chown:" + tt.opts.App + ":" + tt.opts.App + ":" + l.CacheDir()
			if got := countEvents(sys.opSeq(), want); got != 1 {
				t.Fatalf("cache ownership call count = %d, want 1 for %q; ops=%v", got, want, sys.opSeq())
			}
		})
	}
}

// R-CLA5-CFNK
func TestSetupDefaultWritesNoNginxConfDArtifacts(t *testing.T) {
	const app = "dashboard"
	root := t.TempDir()
	sysRoot := t.TempDir()
	sys := &stubSystem{}
	var out strings.Builder
	o := &Opsctl{
		Root:    root,
		SysRoot: sysRoot,
		System:  sys,
		Out:     &out,
		Err:     io.Discard,
	}
	l := NewLayoutSys(root, sysRoot, app)

	if err := o.Setup(context.Background(), SetupOptions{App: app, IsDefault: true}); err != nil {
		t.Fatalf("setup default: %v", err)
	}

	for _, path := range []string{l.FragmentPath(), l.ApexBlockPath()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("default setup wrote nginx artifact %s, want absent (err=%v)", path, err)
		}
	}
	if _, err := os.Lstat(l.NginxConfDir()); !os.IsNotExist(err) {
		t.Fatalf("default setup created nginx conf.d %s, want absent (err=%v)", l.NginxConfDir(), err)
	}
	log := out.String()
	if !strings.Contains(log, "apex block") || !strings.Contains(log, "init-box/deploy") {
		t.Fatalf("default setup log = %q, want apex block ownership by init-box/deploy", log)
	}
	if hasOp(sys.opSeq(), "nginx-test") || hasOp(sys.opSeq(), "nginx-reload") {
		t.Fatalf("default setup touched nginx through seam: %v", sys.opSeq())
	}
}

// R-3SAU-8T9F
func TestSetupMaterializesInstallTreeWithPermissionsAndOwnership(t *testing.T) {
	const app = "svc"
	o, sys, l := newSetupTestOpsctl(t, app)

	if err := o.Setup(context.Background(), SetupOptions{App: app}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for _, dir := range []string{
		l.AppDir(),
		l.StateDir(),
		l.CacheDir(),
		l.LibexecDir(),
		l.BinDir(),
		l.EtcDir(),
		l.shareDir(),
	} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("directory %s not materialized: %v", dir, err)
		}
	}
	if _, err := os.Stat(l.BackupsDir()); !os.IsNotExist(err) {
		t.Fatalf("setup created backups dir %s, want absent (err=%v)", l.BackupsDir(), err)
	}

	assertMode(t, l.StateDir(), 0o711)
	assertMode(t, l.DBPath(), 0o640)
	if _, err := os.Stat(l.WWWDir()); !os.IsNotExist(err) {
		t.Fatalf("worker setup created www dir %s, want absent (err=%v)", l.WWWDir(), err)
	}

	if err := os.WriteFile(filepath.Join(l.CacheDir(), "probe"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("cache dir is not writable: %v", err)
	}

	wantOps := []string{
		"chown:" + app + ":" + app + ":" + l.StateDir(),
		"chown:" + app + ":" + app + ":" + l.DBPath(),
	}
	for _, want := range wantOps {
		if !hasOp(sys.opSeq(), want) {
			t.Fatalf("setup ownership ops = %v, missing %q", sys.opSeq(), want)
		}
	}
	for _, op := range sys.opSeq() {
		if op == "ensure-group:web" || strings.HasPrefix(op, "chown:"+app+":web:") || strings.HasPrefix(op, "chmod:") {
			t.Fatalf("worker setup requested served-tree op %q; ops = %v", op, sys.opSeq())
		}
	}

	owners := ownershipPlan(sys.opSeq())
	for _, rootOwned := range []string{l.EtcDir(), l.shareDir()} {
		if got := ownerForPath(owners, rootOwned); got != (ownerGroup{}) {
			t.Fatalf("%s was handed to %s:%s through Owner seam, want root-owned", rootOwned, got.owner, got.group)
		}
	}
}

// R-CMI1-Q7E9
func TestSetupWorkerNoFragmentStillCreatesFragmentSymlinkWithoutWebGroup(t *testing.T) {
	const app = "worker"
	o, sys, l := newSetupTestOpsctl(t, app)

	if err := o.Setup(context.Background(), SetupOptions{App: app}); err != nil {
		t.Fatalf("setup worker: %v", err)
	}

	fi, err := os.Lstat(l.FragmentPath())
	if err != nil {
		t.Fatalf("lstat worker fragment symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s mode = %v, want symlink", l.FragmentPath(), fi.Mode())
	}
	if target, err := os.Readlink(l.FragmentPath()); err != nil || target != l.ActiveNginxConf() {
		t.Fatalf("worker fragment symlink target = %q, err=%v; want %q", target, err, l.ActiveNginxConf())
	}
	// R-AUAI-EX87
	if _, err := os.Stat(l.WWWDir()); !os.IsNotExist(err) {
		t.Fatalf("worker setup created www dir %s, want absent (err=%v)", l.WWWDir(), err)
	}
	for _, op := range sys.opSeq() {
		if op == "ensure-group:web" || strings.HasPrefix(op, "chown:"+app+":web:") || strings.HasPrefix(op, "chmod:") {
			t.Fatalf("worker setup requested served-tree op %q; ops = %v", op, sys.opSeq())
		}
	}
}

func TestSetupCreatesOnlyPublicAndPrivateServedTiers(t *testing.T) {
	// R-3K9X-IPJZ
	const app = "sites"
	o, sys, l := newSetupTestOpsctl(t, app)

	if err := o.Setup(context.Background(), SetupOptions{
		App: app,
		Fragment: "location /srv/sites/ {\n" +
			"    proxy_pass http://127.0.0.1:3005;\n" +
			"}\n",
		WWWDirs: WWWDirsFor(l.Root, app),
	}); err != nil {
		t.Fatalf("setup served tree: %v", err)
	}

	// R-QFXB-VARQ
	for _, dir := range []string{l.WWWRoot(), l.WWWPublicDir(), l.WWWPrivateDir()} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("served www dir %s not created: %v", dir, err)
		}
		assertMode(t, dir, 0o750)
	}
	working := filepath.Join(l.WWWRoot(), "working")
	if _, err := os.Stat(working); !os.IsNotExist(err) {
		t.Fatalf("legacy working dir %s exists or stat failed unexpectedly: %v", working, err)
	}
	wantOps := []string{
		"ensure-user:" + app + ":" + l.AppDir(),
		"chown:" + app + ":" + app + ":" + l.CacheDir(),
		"chown:" + app + ":" + app + ":" + l.StateDir(),
		"daemon-reload",
		"enable:" + app + ".service",
		"nginx-test",
		"nginx-reload",
	}
	if got := sys.opSeq(); strings.Join(got, "|") != strings.Join(wantOps, "|") {
		t.Fatalf("served-tree setup ops = %v, want %v", got, wantOps)
	}
	for _, op := range sys.opSeq() {
		if op == "ensure-group:web" || strings.Contains(op, ":web:") || strings.HasPrefix(op, "chmod:") {
			t.Fatalf("served-tree setup requested retired permission op %q; ops = %v", op, sys.opSeq())
		}
	}
}

// R-LHY1-6IS8
func TestSetupCreatesStableNginxSymlinkToActiveConfig(t *testing.T) {
	const app = "svc"
	o, _, l := newSetupTestOpsctl(t, app)

	if err := o.Setup(context.Background(), SetupOptions{App: app}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fi, err := os.Lstat(l.FragmentPath())
	if err != nil {
		t.Fatalf("lstat fragment symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s mode = %v, want symlink", l.FragmentPath(), fi.Mode())
	}
	target, err := os.Readlink(l.FragmentPath())
	if err != nil {
		t.Fatalf("readlink fragment symlink: %v", err)
	}
	if target != l.ActiveNginxConf() {
		t.Fatalf("fragment symlink target = %q, want %q", target, l.ActiveNginxConf())
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if got := statMode(t, path).Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func statMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Mode()
}

func hasOp(ops []string, want string) bool {
	for _, op := range ops {
		if op == want {
			return true
		}
	}
	return false
}

type ownerGroup struct {
	owner string
	group string
}

func ownershipPlan(ops []string) map[string]ownerGroup {
	owners := make(map[string]ownerGroup)
	for _, op := range ops {
		parts := strings.SplitN(op, ":", 4)
		if len(parts) != 4 || parts[0] != "chown" {
			continue
		}
		owners[parts[3]] = ownerGroup{owner: parts[1], group: parts[2]}
	}
	return owners
}

func ownerForPath(owners map[string]ownerGroup, path string) ownerGroup {
	var (
		match string
		owner ownerGroup
	)
	for prefix, candidate := range owners {
		if path == prefix || strings.HasPrefix(path, prefix+string(os.PathSeparator)) {
			if len(prefix) > len(match) {
				match = prefix
				owner = candidate
			}
		}
	}
	return owner
}

type unixSubject struct {
	user   string
	groups map[string]bool
}

func (s unixSubject) canRead(mode os.FileMode, owner ownerGroup) bool {
	return s.permissionBits(mode, owner)&0o4 == 0o4
}

func (s unixSubject) canList(mode os.FileMode, owner ownerGroup) bool {
	return s.permissionBits(mode, owner)&0o5 == 0o5
}

func (s unixSubject) permissionBits(mode os.FileMode, owner ownerGroup) os.FileMode {
	perm := mode.Perm()
	switch {
	case s.user == owner.owner:
		return (perm >> 6) & 0o7
	case s.groups[owner.group]:
		return (perm >> 3) & 0o7
	default:
		return perm & 0o7
	}
}
