package sites

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLayoutSiteDirAndBaseUseVisibilitySegments(t *testing.T) {
	layout := NewLayout(filepath.Join("tmp", "sites-root"))

	// R-H5H4-GTWA
	if got, want := layout.SiteBase(Public), filepath.Join(layout.root(), PublicSeg); got != want {
		t.Fatalf("public SiteBase = %q, want %q", got, want)
	}
	if got, want := layout.SiteBase(Private), filepath.Join(layout.root(), PrivateSeg); got != want {
		t.Fatalf("private SiteBase = %q, want %q", got, want)
	}
	if got, want := layout.SiteDir(Public, "blog"), filepath.Join(layout.root(), PublicSeg, "blog"); got != want {
		t.Fatalf("public SiteDir = %q, want %q", got, want)
	}
	if got, want := layout.SiteDir(Private, "blog"), filepath.Join(layout.root(), PrivateSeg, "blog"); got != want {
		t.Fatalf("private SiteDir = %q, want %q", got, want)
	}
	if got, want := layout.SiteDir(Unlisted, "token"), filepath.Join(layout.root(), PublicSeg, "token"); got != want {
		t.Fatalf("unlisted SiteDir = %q, want %q", got, want)
	}
	if Seg(Public) != PublicSeg || Seg(Private) != PrivateSeg || Seg(Unlisted) != PublicSeg {
		t.Fatalf("Seg mapping = (%q, %q, %q), want (public, private, public)", Seg(Public), Seg(Private), Seg(Unlisted))
	}
}

func TestLayoutMoveRelocatesRenamesAndProtectsDestinations(t *testing.T) {
	layout := NewLayout(t.TempDir())
	privateDir := layout.SiteDir(Private, "blog")
	publicDir := layout.SiteDir(Unlisted, "tok")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("mkdir private: %v", err)
	}
	privateFile := filepath.Join(privateDir, "index.html")
	if err := os.WriteFile(privateFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write private file: %v", err)
	}

	// R-H6P0-ULMZ
	if err := layout.Move("blog", Private, "tok", Unlisted); err != nil {
		t.Fatalf("move and rename unlisted: %v", err)
	}
	publicFile := filepath.Join(publicDir, "index.html")
	if got, err := os.ReadFile(publicFile); err != nil || string(got) != "hello" {
		t.Fatalf("public file = %q, %v; want hello, nil", got, err)
	}
	if _, err := os.Stat(privateDir); !os.IsNotExist(err) {
		t.Fatalf("private dir after public move: want missing, got %v", err)
	}

	before, err := os.Stat(publicFile)
	if err != nil {
		t.Fatalf("stat public before no-op: %v", err)
	}
	if err := layout.Move("tok", Unlisted, "tok", Public); err != nil {
		t.Fatalf("same-path move: %v", err)
	}
	after, err := os.Stat(publicFile)
	if err != nil {
		t.Fatalf("stat public after no-op: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("no-op move changed file mod time: before %v after %v", before.ModTime(), after.ModTime())
	}

	if err := layout.Move("empty", Private, "empty-token", Unlisted); err != nil {
		t.Fatalf("move missing source: %v", err)
	}
	destination := layout.SiteDir(Public, "occupied")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("mkdir destination: %v", err)
	}
	source := layout.SiteDir(Private, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := layout.Move("source", Private, "occupied", Public); err == nil {
		t.Fatal("move onto existing destination succeeded")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source changed after collision: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("destination changed after collision: %v", err)
	}
}

func TestCleanupRemovesLegacyPublishMechanics(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	packageDir := filepath.Dir(file)
	if _, err := os.Stat(filepath.Join(packageDir, "publish.go")); !os.IsNotExist(err) {
		t.Fatalf("publish.go exists or stat failed with non-missing error: %v", err)
	}

	internalDir := filepath.Dir(packageDir)
	forbidden := []string{
		"symlink" + "Target",
		"os." + "Symlink",
		"Working" + "Dir",
		"Served" + "Dir",
		"Served" + "Tier" + "Base",
		"Served" + "Base",
	}
	// R-QYP6-P587
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for _, term := range forbidden {
			if strings.Contains(src, term) {
				rel, _ := filepath.Rel(internalDir, path)
				t.Fatalf("legacy cleanup token %q remains in internal/%s", term, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan internal package tree: %v", err)
	}
}
