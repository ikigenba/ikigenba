package candidates_test

import (
	"io/fs"
	"maps"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/candidates"
)

// R-0RI3-7F5X
func TestAssets(t *testing.T) {
	t.Chdir(t.TempDir())
	checkFiles(t, candidates.Assets, []string{
		"about.html", "candidate.html", "landing.html", "notfound.html", "pass.html", "tools.html",
	})
}

// R-0SPZ-L6WM
func TestEtc(t *testing.T) {
	t.Chdir(t.TempDir())
	checkFiles(t, candidates.Etc, []string{"manifest.toml", "nginx.conf"})
}

// R-0TXV-YYNB
func TestMigrations(t *testing.T) {
	t.Chdir(t.TempDir())
	checkFiles(t, candidates.Migrations, []string{
		"0001_candidates.sql", "0002_links.sql", "0003_estimates.sql", "0004_assessments.sql",
		"0005_decisions.sql", "0006_passes.sql", "0007_zip_drive.sql",
	})
}

func checkFiles(t *testing.T, get func() fs.FS, want []string) {
	t.Helper()
	for range 2 {
		files := get()
		entries, err := fs.ReadDir(files, ".")
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() {
				t.Errorf("%s is not a regular file", entry.Name())
			}
			if _, err := fs.ReadFile(files, entry.Name()); err != nil {
				t.Errorf("read %s: %v", entry.Name(), err)
			}
			names = append(names, entry.Name())
		}
		if !slices.Equal(names, want) {
			t.Errorf("files = %v, want %v", names, want)
		}
	}
}

// R-0V5S-CQE0 R-5936-7IMA
func TestZipDrive(t *testing.T) {
	t.Chdir(t.TempDir())
	first := candidates.ZipDrive()
	second := candidates.ZipDrive()
	if first == nil || len(first) != 1664 {
		t.Fatalf("ZIP table is nil or has %d entries, want 1664", len(first))
	}
	if !maps.Equal(first, second) {
		t.Fatal("successive calls differ")
	}
	for zip, minutes := range first {
		if len(zip) != 5 {
			t.Errorf("ZIP %q has length %d", zip, len(zip))
			continue
		}
		for i := range len(zip) {
			if zip[i] < '0' || zip[i] > '9' {
				t.Errorf("ZIP %q has a non-digit", zip)
			}
		}
		if zip[:2] != "53" && zip[:2] != "54" && zip[:2] != "55" && zip[:2] != "56" {
			t.Errorf("ZIP %q has an unexpected prefix", zip)
		}
		if minutes < 0 {
			t.Errorf("ZIP %q has negative minutes %d", zip, minutes)
		}
	}
	for zip, want := range map[string]int64{"55072": 31, "55802": 80, "55401": 104, "54880": 78, "54701": 195} {
		if got, exists := first[zip]; !exists || got != want {
			t.Errorf("ZIP %s = %d (exists %t), want %d", zip, got, exists, want)
		}
	}
	first["55072"] = -1
	delete(first, "55802")
	first["00000"] = 0
	if second["55072"] != 31 || second["55802"] != 80 || len(second) != 1664 {
		t.Fatal("modification changed an earlier returned map")
	}
	if !maps.Equal(second, candidates.ZipDrive()) {
		t.Fatal("modification changed a subsequently returned map")
	}
}
