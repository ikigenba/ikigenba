package candidates_test

import (
	"io/fs"
	"maps"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/candidates"
)

// R-W54S-ZOP9
func TestAssets(t *testing.T) {
	t.Chdir(t.TempDir())
	checkFiles(t, candidates.Assets, []string{
		"about.html", "candidate.html", "landing.html", "notfound.html", "page.html", "pass.html", "prospect_list.html", "tools.html",
	})
}

// R-0SPZ-L6WM
func TestEtc(t *testing.T) {
	t.Chdir(t.TempDir())
	checkFiles(t, candidates.Etc, []string{"manifest.toml", "nginx.conf"})
}

// R-26Q1-RC0V
func TestMigrations(t *testing.T) {
	t.Chdir(t.TempDir())
	checkFiles(t, candidates.Migrations, []string{
		"0001_candidates.sql", "0002_links.sql", "0003_estimates.sql", "0004_assessments.sql",
		"0005_decisions.sql", "0006_passes.sql", "0007_zip_drive.sql", "0008_windows.sql", "0009_judgments.sql", "0010_cohorts.sql", "0011_enrichments.sql", "0012_facts.sql", "0013_site_text.sql", "0014_brief.sql", "0015_website_search.sql", "0016_campaign_uses.sql", "0017_prospect_lists.sql", "0018_drive_route.sql", "0019_opens.sql", "0020_headcounts.sql", "0021_link_keys.sql",
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

// R-YFY0-FLH1
func TestWindowMigrationBytes(t *testing.T) {
	want := "ALTER TABLE assessments ADD COLUMN effective REAL;\n" +
		"ALTER TABLE assessments ADD COLUMN score REAL;\n" +
		"ALTER TABLE assessments ADD COLUMN score_key INTEGER;\n" +
		"CREATE INDEX assessments_score ON assessments(score_key DESC,candidate);\n" +
		"CREATE TABLE candidate_cargo (candidate TEXT NOT NULL, cargo TEXT NOT NULL, PRIMARY KEY(candidate,cargo)) WITHOUT ROWID;\n" +
		"CREATE INDEX candidate_cargo_cargo ON candidate_cargo(cargo,candidate);\n" +
		"INSERT INTO candidate_cargo(candidate,cargo) SELECT DISTINCT candidates.id, j.value FROM candidates, json_each(candidates.cargo) AS j WHERE j.type = 'text';\n" +
		"CREATE INDEX links_phone ON links(phone_key);\n" +
		"CREATE INDEX candidates_window ON candidates(status,created,id);\n" +
		"CREATE INDEX passes_window ON passes(started,id);\n" +
		"CREATE INDEX passes_trigger ON passes(\"trigger\");\n"
	got, err := fs.ReadFile(candidates.Migrations(), "0008_windows.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("window migration differs")
	}
}

// R-35WR-U8HP
func TestJudgmentMigrationBytes(t *testing.T) {
	want := `CREATE TABLE assessments_new (candidate TEXT PRIMARY KEY, fleet INTEGER NOT NULL, low INTEGER NOT NULL DEFAULT 0, high INTEGER, confidence REAL, filed TEXT NOT NULL DEFAULT '', minutes INTEGER, exclusions TEXT NOT NULL DEFAULT '[]', excluded INTEGER NOT NULL, base REAL, rules INTEGER NOT NULL, scored TEXT NOT NULL, effective REAL, score REAL, score_key INTEGER, operating REAL, operating_key INTEGER NOT NULL DEFAULT -1, evidence TEXT, revenue_low INTEGER, revenue_high INTEGER, revenue_confidence REAL, known_since TEXT, government_match TEXT, newest_filing TEXT, categories TEXT);
DROP TABLE assessments;
ALTER TABLE assessments_new RENAME TO assessments;
CREATE INDEX assessments_base ON assessments(base);
CREATE INDEX assessments_score ON assessments(score_key DESC,candidate);
CREATE INDEX assessments_operating ON assessments(operating_key DESC,candidate);
ALTER TABLE passes ADD COLUMN kind TEXT NOT NULL DEFAULT 'ingest';
ALTER TABLE passes ADD COLUMN origin TEXT NOT NULL DEFAULT 'run';
UPDATE passes SET kind = 'rebuild', origin = 'rebuild' WHERE "trigger" = 'rebuild';
`
	got, err := fs.ReadFile(candidates.Migrations(), "0009_judgments.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("judgment migration differs")
	}
}

// R-HAM3-FEB0
func TestFactsMigrationBytes(t *testing.T) {
	want := "ALTER TABLE enrichments ADD COLUMN inputs TEXT NOT NULL DEFAULT '';\n" +
		"UPDATE enrichments SET inputs = source WHERE kind = 'website_check';\n" +
		"CREATE INDEX enrichments_inputs ON enrichments(kind,inputs,seq);\n" +
		"CREATE TABLE facts (candidate TEXT PRIMARY KEY, known_since TEXT, government_match TEXT, newest_filing TEXT, categories TEXT, fleet_verdict TEXT NOT NULL DEFAULT 'unknown', power_units INTEGER, drivers INTEGER, website TEXT NOT NULL DEFAULT 'unknown', website_url TEXT NOT NULL DEFAULT '', minutes INTEGER, drive_minutes_at TEXT NOT NULL DEFAULT '', link_facts_at TEXT NOT NULL DEFAULT '', website_check_at TEXT NOT NULL DEFAULT '');\n" +
		"ALTER TABLE passes ADD COLUMN derived INTEGER NOT NULL DEFAULT 0;\n"
	got, err := fs.ReadFile(candidates.Migrations(), "0012_facts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("facts migration differs")
	}
}

// R-XLH0-W09U
func TestWebsiteSearchMigrationBytes(t *testing.T) {
	want := "ALTER TABLE facts ADD COLUMN website_source TEXT NOT NULL DEFAULT '';\nALTER TABLE facts ADD COLUMN website_search_at TEXT NOT NULL DEFAULT '';\nUPDATE facts SET website_source = 'check' WHERE website IN ('https','http');\n"
	got, err := fs.ReadFile(candidates.Migrations(), "0015_website_search.sql")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("website search migration differs")
	}
}

// R-360R-OG83
func TestDriveRouteMigrationBytes(t *testing.T) {
	want := "ALTER TABLE facts ADD COLUMN minutes_source TEXT NOT NULL DEFAULT '';\nALTER TABLE facts ADD COLUMN miles REAL;\nALTER TABLE facts ADD COLUMN route_address TEXT NOT NULL DEFAULT '';\nALTER TABLE facts ADD COLUMN drive_route_at TEXT NOT NULL DEFAULT '';\nUPDATE facts SET minutes_source = 'zip' WHERE minutes IS NOT NULL;\n"
	got, err := fs.ReadFile(candidates.Migrations(), "0018_drive_route.sql")
	if err != nil || string(got) != want {
		t.Fatalf("drive route migration = %q, %v", got, err)
	}
}
