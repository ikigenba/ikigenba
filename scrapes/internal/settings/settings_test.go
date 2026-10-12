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

// R-YOZQ-93AU R-YQ7M-MV1J R-YV38-5Y0B R-YYQX-B98E
func TestDefaultsAndValues(t *testing.T) {
	want := settings.Settings{5, 2, []string{"MN", "WI"}, 5, "A"}
	a := settings.Defaults()
	if !reflect.DeepEqual(a, want) {
		t.Fatal(a)
	}
	a.FMCSAStates[0] = "XX"
	if !reflect.DeepEqual(settings.Defaults(), want) {
		t.Fatal("shared defaults")
	}
	s, e := settings.Read(func(string) (string, bool) { return "", false })
	if e != nil || !reflect.DeepEqual(s, want) {
		t.Fatal(s, e)
	}
	vals := map[string]string{"DRAIN_SECONDS": "99999999999999999999999", "SCRAPE_KEEP_RUNS": "99999999999999999999999", "FMCSA_STATES": "WI,MN,WI", "FMCSA_MIN_POWER_UNITS": "0", "FMCSA_STATUS": "I"}
	s, e = settings.Read(func(k string) (string, bool) { v, ok := vals[k]; return v, ok })
	if e != nil || s.DrainSeconds != math.MaxInt64 || s.KeepRuns != math.MaxInt64 || s.FMCSAMinPowerUnits != 0 || s.FMCSAStatus != "I" || !reflect.DeepEqual(s.FMCSAStates, []string{"WI", "MN", "WI"}) || s.Drain() != time.Duration(math.MaxInt64) {
		t.Fatal(s, e)
	}
	if settings.Defaults().Drain() != 5*time.Second {
		t.Fatal("drain")
	}
}

// R-YRFJ-0MS8 R-YSNF-EEIX R-6PQV-UNW0 R-6QYS-8FMP R-6TEK-ZZ43
func TestValidation(t *testing.T) {
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS"}
	wants := []string{settings.WantSeconds, settings.WantRuns, settings.WantStates, settings.WantPowerUnits, settings.WantStatus}
	bad := [][]string{{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc"}, {"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc"}, {"mn", "MN, WI", "MN,", ",MN", "MN,,WI", "MNW", "M", "MN;WI", "MN'", "M'N"}, {"00", "05", "-1"}, {"a", "AA", "A ", "'"}}
	good := [][]string{{"1", "12", "99999999999999999999999"}, {"1", "12"}, {"MN", "MN,WI", "MN,WI,IA"}, {"0", "1"}, {"A", "I"}}
	seen := map[string]bool{}
	for i, k := range keys {
		if wants[i] == "" || strings.Contains(wants[i], "\n") || seen[wants[i]] {
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
