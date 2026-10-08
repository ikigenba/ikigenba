package release_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func put(t *testing.T, root, name, body string) string {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}
func link(t *testing.T, root, name, target string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filename); err != nil {
		t.Fatal(err)
	}
}
func shaFixture() string { return strings.Repeat("a", 40) }

// R-9OPF-VAST R-9TL1-EDRL R-9USX-S5IA R-9R58-MUA7 R-9SD5-0M0W
func TestReleaseValuesAndNames(t *testing.T) {
	actual := []string{release.Dir, release.ReleasesDir, release.CurrentLink, release.PreviousLink, release.OpsctlLink, release.CurrentOpsctl, release.OpsctlPath, release.MetadataName, release.LabelName}
	expected := []string{"/opt/ikigenba", "/opt/ikigenba/releases", "/opt/ikigenba/current", "/opt/ikigenba/previous", "/usr/local/bin/opsctl", "/opt/ikigenba/current/opsctl/bin/opsctl", "opsctl/bin/opsctl", "release.json", "label"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal(actual)
	}
	for _, s := range []string{strings.Repeat("0", 40), strings.Repeat("f", 40), "0123456789abcdef0123456789abcdef01234567"} {
		if !release.ValidSHA(s) {
			t.Errorf("valid sha rejected %q", s)
		}
	}
	for _, s := range []string{"", strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("A", 40), strings.Repeat("g", 40), strings.Repeat("é", 20)} {
		if release.ValidSHA(s) {
			t.Errorf("invalid sha accepted %q", s)
		}
	}
	for _, s := range []string{"r142", "r142-rc1", "v1.2_3", "rc/142", "A", "0", "._/-"} {
		if !release.ValidLabel(s) {
			t.Errorf("valid label rejected %q", s)
		}
	}
	for _, s := range []string{"", "r 142", "r:142", "é", "label\n", "label\\x"} {
		if release.ValidLabel(s) {
			t.Errorf("invalid label accepted %q", s)
		}
	}
	for _, fixture := range []struct {
		r              release.Release
		short, display string
	}{{release.Release{SHA: shaFixture()}, "aaaaaaa", "aaaaaaa"}, {release.Release{SHA: shaFixture(), Label: "rc/142"}, "aaaaaaa", "rc/142 (aaaaaaa)"}, {release.Release{SHA: "abc", Label: "one"}, "abc", "one (abc)"}, {release.Release{}, "", ""}} {
		if fixture.r.Short() != fixture.short || fixture.r.Display() != fixture.display {
			t.Fatal(fixture.r)
		}
	}
}

// R-9W0U-5X8Z R-9X8Q-JOZO R-9YGM-XGQD
func TestReadMetadataAndOptionalLabel(t *testing.T) {
	root := t.TempDir()
	sha := shaFixture()
	folder := release.ReleasesDir + "/" + sha
	for _, body := range []string{"", "null", "[]", "true", "{}", `{"sha":null}`, `{"sha":1}`, `{"sha":"x"} {}`, `{broken`} {
		put(t, root, folder+"/"+release.MetadataName, body)
		if _, err := release.Read(root, sha); !errors.Is(err, release.ErrNoMetadata) {
			t.Fatalf("%q: %v", body, err)
		}
	}
	if _, err := release.Read(root, strings.Repeat("b", 40)); !errors.Is(err, release.ErrNoMetadata) {
		t.Fatal(err)
	}
	put(t, root, folder+"/"+release.MetadataName, `{"sha":"another","future":{"ignored":true}}`)
	r, err := release.Read(root, sha)
	if err != nil || r.SHA != "another" || r.Label != "" {
		t.Fatalf("%#v %v", r, err)
	}
	for _, label := range []struct{ file, want string }{{"rc/142\n", "rc/142"}, {"label", "label"}, {"label\n\n", ""}, {"label\r\n", ""}, {" label", ""}, {"", ""}, {"é", ""}} {
		put(t, root, folder+"/"+release.LabelName, label.file)
		r, err = release.Read(root, sha)
		if err != nil || r.Label != label.want {
			t.Fatalf("label %q => %#v %v", label.file, r, err)
		}
	}
}

// R-A0WF-P07R R-A24C-2RYG R-9YGM-XGQD
func TestReleaseLinksIdentifyEvenMissingMetadata(t *testing.T) {
	for _, fixture := range []struct {
		name string
		read func(string) (release.Release, bool, error)
	}{{release.CurrentLink, release.Current}, {release.PreviousLink, release.Previous}} {
		root := t.TempDir()
		r, ok, err := fixture.read(root)
		if err != nil || ok || r != (release.Release{}) {
			t.Fatal(r, ok, err)
		}
		link(t, root, fixture.name, "releases/"+shaFixture())
		r, ok, err = fixture.read(root)
		if err != nil || !ok || r.SHA != shaFixture() {
			t.Fatal(r, ok, err)
		}
		put(t, root, release.ReleasesDir+"/"+shaFixture()+"/label", "r1\n")
		r, ok, err = fixture.read(root)
		if err != nil || !ok || r.Label != "r1" {
			t.Fatal(r, ok, err)
		}
		for _, target := range []string{"", "releases/" + strings.Repeat("a", 39), "releases/" + strings.Repeat("A", 40), release.ReleasesDir + "/" + shaFixture(), "./releases/" + shaFixture(), "releases/" + shaFixture() + "/"} {
			if target == "" {
				continue
			}
			bad := t.TempDir()
			link(t, bad, fixture.name, target)
			_, ok, err = fixture.read(bad)
			if ok || err == nil || err.Error() != fixture.name+" does not name a release" {
				t.Fatal(target, ok, err)
			}
		}
		for _, directory := range []bool{false, true} {
			bad := t.TempDir()
			if directory {
				if err := os.MkdirAll(filepath.Join(bad, fixture.name), 0o750); err != nil {
					t.Fatal(err)
				}
			} else {
				put(t, bad, fixture.name, "regular")
			}
			_, ok, err = fixture.read(bad)
			if ok || err == nil || err.Error() != fixture.name+" does not name a release" {
				t.Fatal(ok, err)
			}
		}
	}
}

// R-A3C8-GJP5 R-SAHR-MESY R-S99V-8N29 R-9YGM-XGQD
func TestExecutableBelongsOnlyToItsResolvedRelease(t *testing.T) {
	root := t.TempDir()
	sha := shaFixture()
	folder := release.ReleasesDir + "/" + sha
	executable := put(t, root, folder+"/"+release.OpsctlPath, "binary")
	put(t, root, folder+"/release.json", `{"sha":"`+sha+`"}`)
	put(t, root, folder+"/label", "rc/one\n")
	link(t, root, release.CurrentLink, "releases/"+sha)
	link(t, root, release.PreviousLink, "releases/"+sha)
	link(t, root, release.OpsctlLink, release.CurrentOpsctl)
	for _, name := range []string{executable, filepath.Join(root, release.CurrentOpsctl), filepath.Join(root, release.PreviousLink, release.OpsctlPath), filepath.Join(root, release.OpsctlLink)} {
		r, ok := release.Of(root, name)
		if !ok || r != (release.Release{SHA: sha, Label: "rc/one"}) {
			t.Fatalf("Of(%q)=%#v %v", name, r, ok)
		}
	}
	for _, name := range []string{filepath.Join(root, "missing"), put(t, root, "opt/unreleased/opsctl", "x"), put(t, t.TempDir(), "outside", "x")} {
		if r, ok := release.Of(root, name); ok || r != (release.Release{}) {
			t.Fatal(name, r, ok)
		}
	}
	link(t, root, "loop", "loop")
	if _, ok := release.Of(root, filepath.Join(root, "loop")); ok {
		t.Fatal("loop accepted")
	}
	for _, body := range []string{`{}`, `{"sha":"wrong"}`} {
		put(t, root, folder+"/release.json", body)
		if _, ok := release.Of(root, executable); ok {
			t.Fatal("invalid metadata accepted")
		}
	}
}

// R-A5S1-836J R-A6ZX-LUX8
func TestLinkOpsctlReplacesEntryAndLeavesOnlyLink(t *testing.T) {
	for _, existing := range []string{"absent", "regular", "symlink"} {
		root := t.TempDir()
		switch existing {
		case "regular":
			put(t, root, release.OpsctlLink, "old")
		case "symlink":
			put(t, root, "old-target", "untouched")
			link(t, root, release.OpsctlLink, "/old-target")
		}
		if err := release.LinkOpsctl(root, release.CurrentOpsctl); err != nil {
			t.Fatal(err)
		}
		target, err := os.Readlink(filepath.Join(root, release.OpsctlLink))
		if err != nil || target != release.CurrentOpsctl {
			t.Fatal(target, err)
		}
		entries, err := os.ReadDir(filepath.Join(root, "usr/local/bin"))
		if err != nil || len(entries) != 1 || entries[0].Name() != "opsctl" {
			t.Fatal(entries, err)
		}
		info, err := os.Stat(filepath.Join(root, "usr/local/bin"))
		if err != nil || info.Mode().Perm() != map[bool]os.FileMode{true: 0o755, false: 0o750}[existing == "absent"] {
			t.Fatal(info, err)
		}
		if existing == "symlink" {
			data, err := rootRead(t, root, "old-target")
			if err != nil || string(data) != "untouched" {
				t.Fatal(string(data), err)
			}
		}
	}
}

// R-A87T-ZMNX
func TestReleaseReadersLeaveFilesystemUnchanged(t *testing.T) {
	root := t.TempDir()
	sha := shaFixture()
	folder := release.ReleasesDir + "/" + sha
	executable := put(t, root, folder+"/"+release.OpsctlPath, "binary")
	put(t, root, folder+"/release.json", `{"sha":"`+sha+`"}`)
	link(t, root, release.CurrentLink, "releases/"+sha)
	before := tree(t, root)
	if _, err := release.Read(root, sha); err != nil {
		t.Fatal(err)
	}
	if _, _, err := release.Current(root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := release.Previous(root); err != nil {
		t.Fatal(err)
	}
	if _, ok := release.Of(root, executable); !ok {
		t.Fatal("Of")
	}
	if after := tree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal(before, after)
	}
}
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, e := os.Readlink(name)
			result[name] = target
			return e
		}
		if !entry.IsDir() {
			data, e := rootRead(t, root, strings.TrimPrefix(name, root+string(filepath.Separator)))
			result[name] = string(data)
			return e
		}
		result[name] = "directory"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func rootRead(t *testing.T, root, name string) ([]byte, error) {
	t.Helper()
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	return filesystem.ReadFile(name)
}

// R-A87T-ZMNX R-9X8Q-JOZO R-9YGM-XGQD R-SAHR-MESY
func TestReleaseReadersRefuseMetadataAndLabelsOutsideReleases(t *testing.T) {
	for _, target := range []string{"/etc/metadata", "../../../../etc/metadata"} {
		root := t.TempDir()
		sha := shaFixture()
		folder := release.ReleasesDir + "/" + sha
		put(t, root, "/etc/metadata", `{"sha":"`+sha+`"}`)
		executable := put(t, root, folder+"/"+release.OpsctlPath, "binary")
		link(t, root, folder+"/release.json", target)
		if _, err := release.Read(root, sha); !errors.Is(err, release.ErrNoMetadata) {
			t.Fatalf("outside metadata %q: %v", target, err)
		}
		if r, ok := release.Of(root, executable); ok || r != (release.Release{}) {
			t.Fatalf("outside metadata executable: %#v %v", r, ok)
		}
	}
	for _, target := range []string{"/etc/label", "../../../../etc/label"} {
		root := t.TempDir()
		sha := shaFixture()
		folder := release.ReleasesDir + "/" + sha
		put(t, root, "/etc/label", "external-label\n")
		executable := put(t, root, folder+"/"+release.OpsctlPath, "binary")
		put(t, root, folder+"/release.json", `{"sha":"`+sha+`"}`)
		link(t, root, folder+"/label", target)
		link(t, root, release.CurrentLink, "releases/"+sha)
		link(t, root, release.PreviousLink, "releases/"+sha)
		r, err := release.Read(root, sha)
		if err != nil || r.Label != "" {
			t.Fatalf("outside label Read %#v %v", r, err)
		}
		for _, read := range []func(string) (release.Release, bool, error){release.Current, release.Previous} {
			r, ok, err := read(root)
			if err != nil || !ok || r.Label != "" {
				t.Fatalf("outside label link %#v %v %v", r, ok, err)
			}
		}
		if r, ok := release.Of(root, executable); !ok || r.Label != "" {
			t.Fatalf("outside label Of %#v %v", r, ok)
		}
	}
}

// R-A87T-ZMNX R-S99V-8N29
func TestReleaseReadersAcceptSymlinksWithinReleases(t *testing.T) {
	root := t.TempDir()
	sha := shaFixture()
	folder := release.ReleasesDir + "/" + sha
	put(t, root, folder+"/metadata-target", `{"sha":"`+sha+`"}`)
	put(t, root, folder+"/label-target", "inside\n")
	link(t, root, folder+"/release.json", folder+"/metadata-target")
	link(t, root, folder+"/label", "label-target")
	r, err := release.Read(root, sha)
	if err != nil || r != (release.Release{SHA: sha, Label: "inside"}) {
		t.Fatalf("inside symlinks %#v %v", r, err)
	}
}

// R-A87T-ZMNX R-9X8Q-JOZO R-9YGM-XGQD
func TestReleaseReadersRejectAncestorRedirection(t *testing.T) {
	for _, target := range []string{"/opt", "..", "../.."} {
		root := t.TempDir()
		sha := shaFixture()
		// Put valid-looking release content at each candidate outside ReleasesDir.
		for _, folder := range []string{"/opt/" + sha, "/opt/ikigenba/" + sha} {
			put(t, root, folder+"/release.json", `{"sha":"`+sha+`"}`)
			put(t, root, folder+"/label", "external\n")
		}
		link(t, root, release.ReleasesDir, target)
		link(t, root, release.CurrentLink, "releases/"+sha)
		link(t, root, release.PreviousLink, "releases/"+sha)
		if r, err := release.Read(root, sha); !errors.Is(err, release.ErrNoMetadata) {
			t.Fatalf("releases -> %q: %#v %v", target, r, err)
		}
		for _, read := range []func(string) (release.Release, bool, error){release.Current, release.Previous} {
			r, ok, err := read(root)
			if err != nil || !ok || r.SHA != sha || r.Label != "" {
				t.Fatalf("link with redirected releases -> %q: %#v %v %v", target, r, ok, err)
			}
		}
	}
	for _, name := range []string{release.MetadataName, release.LabelName} {
		root := t.TempDir()
		sha := shaFixture()
		folder := release.ReleasesDir + "/" + sha
		put(t, root, "/opt/"+sha+"/"+name, `{"sha":"`+sha+`"}`)
		if name == release.LabelName {
			put(t, root, folder+"/release.json", `{"sha":"`+sha+`"}`)
		}
		link(t, root, folder+"/"+name, "/opt")
		r, err := release.Read(root, sha)
		if name == release.MetadataName {
			if !errors.Is(err, release.ErrNoMetadata) {
				t.Fatal(r, err)
			}
		} else if err != nil || r.Label != "" {
			t.Fatal(r, err)
		}
	}
}
