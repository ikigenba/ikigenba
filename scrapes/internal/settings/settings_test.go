package settings_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
)

// R-7G1S-SO6P R-7H9P-6FXE R-7KXE-BR5H R-YYQX-B98E
func TestDefaultsAndValues(t *testing.T) {
	want := settings.Settings{5, 2, 4096000, 10, []string{"MN", "WI"}, 5, "A", []string{"public_150k_plus_240930.csv"}, []string{"MN", "WI"}, []string{"Carlton", "Chisago", "Isanti", "Mille Lacs", "Aitkin", "St. Louis", "Anoka", "Washington", "Sherburne", "Stearns", "Benton", "Morrison", "Crow Wing"}, []string{"2025", "2024", "2023", "2022", "2021", "2020", "2019"}, []string{"MN", "WI"}, []string{"largefiles/ITA_300A_Summary_Data_2025_through_08-31-2026.csv", "files/ITA_300A_Summary_Data_2024_through_12-31-2025.zip", "files/ITA_300A_Summary_Data_2023_through_12-31-2024.zip", "files/ITA-data-cy2022.zip", "files/ITA-data-cy2021.zip", "files/ITA-Data-CY-2020.zip", "files/ITA Data CY 2019.zip", "files/ITA Data CY 2018.zip", "files/ITA Data CY 2017.zip", "files/ITA Data CY 2016.zip"}, []string{"MN", "WI"}}
	a := settings.Defaults()
	if !reflect.DeepEqual(a, want) {
		t.Fatal(a)
	}
	a.FMCSAStates[0] = "XX"
	if a.PPPStates[0] != "MN" || a.Form5500States[0] != "MN" {
		t.Fatal("shared state slices")
	}
	a.PPPStates[0] = "XX"
	if a.Form5500States[0] != "MN" {
		t.Fatal("shared state slices")
	}
	a.Form5500States[0] = "XX"
	if a.OSHAITAStates[0] != "MN" {
		t.Fatal("shared state slices")
	}
	a.OSHAITAStates[0] = "XX"
	a.OSHAITAFiles[0] = "files/changed.zip"
	a.Form5500Years[0] = "1999"
	a.PPPFiles[0] = "changed.csv"
	a.MNParcelsCounties[0] = "Changed"
	if !reflect.DeepEqual(settings.Defaults(), want) {
		t.Fatal("shared defaults")
	}
	s, e := settings.Read(func(string) (string, bool) { return "", false })
	if e != nil || !reflect.DeepEqual(s, want) {
		t.Fatal(s, e)
	}
	vals := map[string]string{"DRAIN_SECONDS": "99999999999999999999999", "SCRAPE_KEEP_RUNS": "99999999999999999999999", "SCRAPE_WRITE_BUDGET": "99999999999999999999999", "SCRAPE_PRESSURE_LIMIT": "100", "FMCSA_STATES": "WI,MN,WI", "FMCSA_MIN_POWER_UNITS": "0", "FMCSA_STATUS": "I", "PPP_FILES": "b.csv,a.csv,b.csv", "PPP_STATES": "IA", "MN_PARCELS_COUNTIES": "St. Louis,O'Brien,St. Louis", "FORM5500_YEARS": "2019,2024,2019", "FORM5500_STATES": "IA", "OSHA_ITA_FILES": "files/b c.zip,largefiles/a.csv,files/b c.zip", "OSHA_ITA_STATES": "IA,WI,IA"}
	s, e = settings.Read(func(k string) (string, bool) { v, ok := vals[k]; return v, ok })
	if e != nil || s.DrainSeconds != math.MaxInt64 || s.KeepRuns != math.MaxInt64 || s.WriteBudget != math.MaxInt64 || s.PressureLimit != 100 || s.FMCSAMinPowerUnits != 0 || s.FMCSAStatus != "I" || !reflect.DeepEqual(s.PPPFiles, []string{"b.csv", "a.csv", "b.csv"}) || !reflect.DeepEqual(s.PPPStates, []string{"IA"}) || !reflect.DeepEqual(s.FMCSAStates, []string{"WI", "MN", "WI"}) || !reflect.DeepEqual(s.MNParcelsCounties, []string{"St. Louis", "O'Brien", "St. Louis"}) || !reflect.DeepEqual(s.Form5500Years, []string{"2019", "2024", "2019"}) || !reflect.DeepEqual(s.Form5500States, []string{"IA"}) || !reflect.DeepEqual(s.OSHAITAFiles, []string{"files/b c.zip", "largefiles/a.csv", "files/b c.zip"}) || !reflect.DeepEqual(s.OSHAITAStates, []string{"IA", "WI", "IA"}) || s.Drain() != time.Duration(math.MaxInt64) {
		t.Fatal(s, e)
	}
	if settings.Defaults().Drain() != 5*time.Second {
		t.Fatal("drain")
	}
}

// R-KQB5-1JDV R-YRFJ-0MS8 R-YSNF-EEIX R-7M5A-PIW6 R-6QYS-8FMP R-7ND7-3AMV R-69WE-HYSZ R-RDV8-CDEA R-PIAX-SGX1
func TestValidation(t *testing.T) {
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "SCRAPE_WRITE_BUDGET", "SCRAPE_PRESSURE_LIMIT", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS", "PPP_FILES", "PPP_STATES", "MN_PARCELS_COUNTIES", "FORM5500_YEARS", "FORM5500_STATES", "OSHA_ITA_FILES", "OSHA_ITA_STATES"}
	wants := []string{settings.WantSeconds, settings.WantRuns, settings.WantBudget, settings.WantPressure, settings.WantStates, settings.WantPowerUnits, settings.WantStatus, settings.WantFiles, settings.WantStates, settings.WantCounties, settings.WantYears, settings.WantStates, settings.WantPaths, settings.WantStates}
	// R-4EW3-QJ1M R-4G40-4ASB
	bad := [][]string{{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc"}, {"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc"}, {"0", "00", "-1", "+5", "2.5", "4M", "1e9", "05", " 5", "5 ", "abc"}, {"0", "00", "05", "010", "101", "1000", "99999999999999999999999", "-1", "+5", "10.5", "10%", " 10", "10 ", "abc"}, {"mn", "MN, WI", "MN,", ",MN", "MN,,WI", "MNW", "M", "MN;WI", "MN'", "M'N"}, {"00", "05", "-1"}, {"a", "AA", "A ", "'"}, {"a.csv,", ",a.csv", "a.csv,,b.csv", "a.csv, b.csv", " a.csv", "a/b.csv", "../a.csv", "..", ".x", "a.csv,.x", "a%2Fb.csv", "a.csv?x=1", "a.csv#x", "a+b.csv", `a\b.csv`, "é.csv", "a.csv;b"}, {"mn", "MN, WI", "MN,", ",MN", "MN,,WI", "MNW", "M", "MN'"}, {"Chisago,", ",Chisago", "Chisago,,Isanti", "Chisago, Isanti", "Chisago ,Isanti", " Chisago", ".Chisago", "'Chisago", "Chisago;Isanti", "Chisago--", "Chisago\"", "Crow_Wing", "Crow-Wing", "St. Louis2", "Chisago' OR '1'='1", "Ramsé", "A\tB", "A\nB", "A\x00B"}, {"19", "20255", "0999", "2025,", ",2025", "2025,,2024", "2025, 2024", " 2025", "2025-2024", "2025;2024", "２０２５", "2O25", "2025/", "../2025"}, {"mn", "MN, WI", "MN,", "MNW", "MN'"}, {"files/a.zip,", ",files/a.zip", "files/a.zip,,files/b.zip", "files/a.zip, files/b.zip", " files/a.zip", "files/a.zip ", "files /a.zip", "files/ a.zip", "/files/a.zip", "files/a.zip/", "files//a.zip", "..", "../a.zip", "files/../a.zip", "files/./a.zip", ".x", "files/.x", "files/a%20b.zip", "files/a.zip?x=1", "files/a.zip#x", "files/a+b.zip", `files\a.zip`, "files/é.zip", "files/a.zip;b", "https://www.osha.gov/a.zip", "files/a\tb.zip", "files/a\nb.zip", "files/a\x00b.zip"}, {"mn", "MN, WI", "MN,", ",MN", "MN,,WI", "MNW", "M", "MN'", "ＭＮ"}}
	good := [][]string{{"1", "12", "99999999999999999999999"}, {"1", "12"}, {"1", "4096000", "99999999999999999999999"}, {"1", "10", "99", "100"}, {"MN", "MN,WI", "MN,WI,IA"}, {"0", "1"}, {"A", "I"}, {"public_150k_plus_240930.csv", "a.csv,b.csv", "a", "a..b.csv", "A-1_b.CSV"}, {"MN", "MN,WI", "MN,WI,IA"}, {"Chisago", "Mille Lacs,St. Louis", "Lac qui Parle", "O'Brien", strings.Join(settings.Defaults().MNParcelsCounties, ","), "a", "Z.", "A'", "a  b", "A.'B"}, {"2025", "2025,2024,2023,2022,2021,2020,2019", "2019,2019", "9999"}, {"MN", "MN,WI", "MN,WI,IA"}, {strings.Join(settings.Defaults().OSHAITAFiles, ","), "files/ITA Data CY 2016.zip", "a b/c.zip", "a", "a/b/c.csv", "files/a..b.zip", "files/a.zip,files/a.zip", "largefiles/A-1_b.CSV", "_/1.-/a  b.zip"}, {"MN", "MN,WI", "MN,WI,IA"}}
	seen := map[string]bool{}
	for i, k := range keys {
		if wants[i] == "" || strings.Contains(wants[i], "\n") || (seen[wants[i]] && k != "PPP_STATES" && k != "FORM5500_STATES" && k != "OSHA_ITA_STATES") {
			t.Fatal(wants)
		}
		seen[wants[i]] = true
		for _, v := range append([]string{""}, good[i]...) {
			_, e := settings.Read(func(key string) (string, bool) { return v, key == k })
			if e != nil {
				t.Fatal(k, v, e)
			}
		}
		for _, v := range bad[i] {
			_, e := settings.Read(func(key string) (string, bool) {
				if key == k {
					return v, true
				}
				return "", false
			})
			var se *settings.Error
			if !errors.As(e, &se) || se.Name != k || se.Value != v || se.Want != wants[i] || e.Error() != k+" is '"+v+"', not "+wants[i] {
				t.Fatal(k, v, e)
			}
		}
	}
	_, e := settings.Read(func(k string) (string, bool) {
		if k == "SCRAPE_KEEP_RUNS" {
			return "0", true
		}
		if k == "FMCSA_STATUS" {
			return "a", true
		}
		return "", false
	})
	var se *settings.Error
	if !errors.As(e, &se) || se.Name != "SCRAPE_KEEP_RUNS" {
		t.Fatal(e)
	}
}

// R-7M5A-PIW6
func TestFirstRefusal(t *testing.T) {
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "SCRAPE_WRITE_BUDGET", "SCRAPE_PRESSURE_LIMIT", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS", "PPP_FILES", "PPP_STATES", "MN_PARCELS_COUNTIES", "FORM5500_YEARS", "FORM5500_STATES", "OSHA_ITA_FILES", "OSHA_ITA_STATES"}
	values := []string{"0", "0", "0", "101", "mn", "-1", "a", "a/b.csv", "mn", "Chisago;", "19", "mn", "/a.zip", "mn"}
	wants := []string{settings.WantSeconds, settings.WantRuns, settings.WantBudget, settings.WantPressure, settings.WantStates, settings.WantPowerUnits, settings.WantStatus, settings.WantFiles, settings.WantStates, settings.WantCounties, settings.WantYears, settings.WantStates, settings.WantPaths, settings.WantStates}
	for first := range keys {
		for last := first; last < len(keys); last++ {
			_, err := settings.Read(func(k string) (string, bool) {
				for i := first; i <= last; i++ {
					if k == keys[i] {
						return values[i], true
					}
				}
				return "", false
			})
			var refusal *settings.Error
			if !errors.As(err, &refusal) || *refusal != (settings.Error{keys[first], values[first], wants[first]}) {
				t.Fatalf("first %s last %s: %v", keys[first], keys[last], err)
			}
		}
	}
}

// R-7KXE-BR5H
func TestEmptyAndIndependentSettings(t *testing.T) {
	want := settings.Defaults()
	got, err := settings.Read(func(string) (string, bool) { return "", true })
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	got, err = settings.Read(func(string) (string, bool) { return "invalid;", false })
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	values := map[string]string{"DRAIN_SECONDS": "12", "SCRAPE_KEEP_RUNS": "3", "FMCSA_MIN_POWER_UNITS": "99999999999999999999999", "PPP_STATES": "IA,IA", "unknown": "invalid"}
	got, err = settings.Read(func(k string) (string, bool) {
		if _, known := map[string]bool{"DRAIN_SECONDS": true, "SCRAPE_KEEP_RUNS": true, "SCRAPE_WRITE_BUDGET": true, "SCRAPE_PRESSURE_LIMIT": true, "FMCSA_STATES": true, "FMCSA_MIN_POWER_UNITS": true, "FMCSA_STATUS": true, "PPP_FILES": true, "PPP_STATES": true, "MN_PARCELS_COUNTIES": true, "FORM5500_YEARS": true, "FORM5500_STATES": true, "OSHA_ITA_FILES": true, "OSHA_ITA_STATES": true}[k]; !known {
			return "invalid", true
		}
		v, ok := values[k]
		return v, ok
	})
	want.DrainSeconds, want.KeepRuns, want.FMCSAMinPowerUnits, want.PPPStates = 12, 3, math.MaxInt64, []string{"IA", "IA"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
}

// R-7KXE-BR5H
func TestOSHAVariablesPreserveOtherDefaults(t *testing.T) {
	for _, key := range []string{"OSHA_ITA_FILES", "OSHA_ITA_STATES"} {
		want := settings.Defaults()
		value := "IA,IA"
		if key == "OSHA_ITA_FILES" {
			value = "files/b c.zip,largefiles/a.csv,files/b c.zip"
			want.OSHAITAFiles = []string{"files/b c.zip", "largefiles/a.csv", "files/b c.zip"}
		} else {
			want.OSHAITAStates = []string{"IA", "IA"}
		}
		got, err := settings.Read(func(k string) (string, bool) { return value, k == key })
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %+v, error %v", key, got, err)
		}
	}
}

// R-7ND7-3AMV
func TestCopyConstants(t *testing.T) {
	const a, b, c, d, e, f, g, h, i, j, k = settings.WantSeconds, settings.WantRuns, settings.WantBudget, settings.WantPressure, settings.WantStates, settings.WantPowerUnits, settings.WantStatus, settings.WantFiles, settings.WantCounties, settings.WantYears, settings.WantPaths
	seen := map[string]bool{}
	for _, value := range []string{a, b, c, d, e, f, g, h, i, j, k} {
		if value == "" || strings.Contains(value, "\n") || seen[value] {
			t.Fatal("copy constants", value)
		}
		seen[value] = true
	}
}
