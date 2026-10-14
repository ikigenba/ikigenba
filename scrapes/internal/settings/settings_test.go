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

// R-650S-YVU7 R-67GL-QFBL R-6CC7-9IAD R-YYQX-B98E
func TestDefaultsAndValues(t *testing.T) {
	want := settings.Settings{5, 2, []string{"MN", "WI"}, 5, "A", []string{"public_150k_plus_240930.csv"}, []string{"MN", "WI"}}
	a := settings.Defaults()
	if !reflect.DeepEqual(a, want) {
		t.Fatal(a)
	}
	a.FMCSAStates[0] = "XX"
	if a.PPPStates[0] != "MN" {
		t.Fatal("shared state slices")
	}
	a.PPPStates[0] = "XX"
	a.PPPFiles[0] = "changed.csv"
	if !reflect.DeepEqual(settings.Defaults(), want) {
		t.Fatal("shared defaults")
	}
	s, e := settings.Read(func(string) (string, bool) { return "", false })
	if e != nil || !reflect.DeepEqual(s, want) {
		t.Fatal(s, e)
	}
	vals := map[string]string{"DRAIN_SECONDS": "99999999999999999999999", "SCRAPE_KEEP_RUNS": "99999999999999999999999", "FMCSA_STATES": "WI,MN,WI", "FMCSA_MIN_POWER_UNITS": "0", "FMCSA_STATUS": "I", "PPP_FILES": "b.csv,a.csv,b.csv", "PPP_STATES": "IA"}
	s, e = settings.Read(func(k string) (string, bool) { v, ok := vals[k]; return v, ok })
	if e != nil || s.DrainSeconds != math.MaxInt64 || s.KeepRuns != math.MaxInt64 || s.FMCSAMinPowerUnits != 0 || s.FMCSAStatus != "I" || !reflect.DeepEqual(s.PPPFiles, []string{"b.csv", "a.csv", "b.csv"}) || !reflect.DeepEqual(s.PPPStates, []string{"IA"}) || !reflect.DeepEqual(s.FMCSAStates, []string{"WI", "MN", "WI"}) || s.Drain() != time.Duration(math.MaxInt64) {
		t.Fatal(s, e)
	}
	if settings.Defaults().Drain() != 5*time.Second {
		t.Fatal("drain")
	}
}

// R-YRFJ-0MS8 R-YSNF-EEIX R-6FZW-ETIG R-6QYS-8FMP R-6IFP-6CZU R-69WE-HYSZ
func TestValidation(t *testing.T) {
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS", "PPP_FILES", "PPP_STATES"}
	wants := []string{settings.WantSeconds, settings.WantRuns, settings.WantStates, settings.WantPowerUnits, settings.WantStatus, settings.WantFiles, settings.WantStates}
	bad := [][]string{{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc"}, {"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc"}, {"mn", "MN, WI", "MN,", ",MN", "MN,,WI", "MNW", "M", "MN;WI", "MN'", "M'N"}, {"00", "05", "-1"}, {"a", "AA", "A ", "'"}, {"a.csv,", ",a.csv", "a.csv,,b.csv", "a.csv, b.csv", " a.csv", "a/b.csv", "../a.csv", "..", ".x", "a.csv,.x", "a%2Fb.csv", "a.csv?x=1", "a.csv#x", "a+b.csv", `a\b.csv`, "é.csv", "a.csv;b"}, {"mn", "MN, WI", "MN,", ",MN", "MN,,WI", "MNW", "M", "MN'"}}
	good := [][]string{{"1", "12", "99999999999999999999999"}, {"1", "12"}, {"MN", "MN,WI", "MN,WI,IA"}, {"0", "1"}, {"A", "I"}, {"public_150k_plus_240930.csv", "a.csv,b.csv", "a", "a..b.csv", "A-1_b.CSV"}, {"MN", "MN,WI", "MN,WI,IA"}}
	seen := map[string]bool{}
	for i, k := range keys {
		if wants[i] == "" || strings.Contains(wants[i], "\n") || (seen[wants[i]] && k != "PPP_STATES") {
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

// R-6FZW-ETIG
func TestFirstRefusal(t *testing.T) {
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS", "PPP_FILES", "PPP_STATES"}
	values := []string{"0", "0", "mn", "-1", "a", "a/b.csv", "mn"}
	wants := []string{settings.WantSeconds, settings.WantRuns, settings.WantStates, settings.WantPowerUnits, settings.WantStatus, settings.WantFiles, settings.WantStates}
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

// R-6CC7-9IAD
func TestEmptyAndIndependentSettings(t *testing.T) {
	want := settings.Defaults()
	got, err := settings.Read(func(string) (string, bool) { return "", true })
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	values := map[string]string{"DRAIN_SECONDS": "12", "SCRAPE_KEEP_RUNS": "3", "FMCSA_MIN_POWER_UNITS": "99999999999999999999999", "PPP_STATES": "IA,IA", "unknown": "invalid"}
	got, err = settings.Read(func(k string) (string, bool) {
		if _, known := map[string]bool{"DRAIN_SECONDS": true, "SCRAPE_KEEP_RUNS": true, "FMCSA_STATES": true, "FMCSA_MIN_POWER_UNITS": true, "FMCSA_STATUS": true, "PPP_FILES": true, "PPP_STATES": true}[k]; !known {
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

// R-6IFP-6CZU
func TestCopyConstants(t *testing.T) {
	const a, b, c, d, e, f = settings.WantSeconds, settings.WantRuns, settings.WantStates, settings.WantPowerUnits, settings.WantStatus, settings.WantFiles
	seen := map[string]bool{}
	for _, value := range []string{a, b, c, d, e, f} {
		if value == "" || strings.Contains(value, "\n") || seen[value] {
			t.Fatal("copy constants", value)
		}
		seen[value] = true
	}
}
