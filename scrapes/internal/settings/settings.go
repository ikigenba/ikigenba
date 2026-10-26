// Package settings reads the service's startup configuration.
package settings

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Settings holds startup settings in read order.
type Settings struct {
	DrainSeconds, KeepRuns, WriteBudget, PressureLimit int64
	FMCSAStates                                        []string
	FMCSAMinPowerUnits                                 int64
	FMCSAStatus                                        string
	PPPFiles                                           []string
	PPPStates                                          []string
	MNParcelsCounties                                  []string
	Form5500Years                                      []string
	Form5500States                                     []string
	OSHAITAFiles                                       []string
	OSHAITAStates                                      []string
}

// Error identifies a refused setting.
type Error struct{ Name, Value, Want string }

func (e *Error) Error() string { return e.Name + " is '" + e.Value + "', not " + e.Want }

// Configuration refusal copy.
const (
	WantSeconds    string = "a positive whole number of seconds"
	WantBudget     string = "a positive whole number of bytes per second"
	WantPressure   string = "a whole percentage from 1 to 100"
	WantRuns       string = "a positive whole number of runs"
	WantStates     string = "comma-separated two-letter uppercase state codes"
	WantPowerUnits string = "a non-negative whole number of power units"
	WantStatus     string = "one uppercase status letter"
	WantFiles      string = "comma-separated file names of letters, digits, underscore, hyphen and dot, not starting with dot"
	WantPaths      string = "comma-separated relative paths of letters, digits, underscore, hyphen, dot and spaces, with no segment starting with dot or space or ending with space"
	WantYears      string = "comma-separated four-digit years starting with a non-zero digit"
	WantCounties   string = "comma-separated county names starting with a letter, containing letters, spaces, dots and apostrophes, and not ending with a space"
)

// Defaults returns fresh default settings.
func Defaults() Settings {
	return Settings{5, 2, 4096000, 10, []string{"MN", "WI"}, 5, "A", []string{"public_150k_plus_240930.csv"}, []string{"MN", "WI"}, []string{"Carlton", "Chisago", "Isanti", "Mille Lacs", "Aitkin", "St. Louis", "Anoka", "Washington", "Sherburne", "Stearns", "Benton", "Morrison", "Crow Wing"}, []string{"2025", "2024", "2023", "2022", "2021", "2020", "2019"}, []string{"MN", "WI"}, []string{"largefiles/ITA_300A_Summary_Data_2025_through_08-31-2026.csv", "files/ITA_300A_Summary_Data_2024_through_12-31-2025.zip", "files/ITA_300A_Summary_Data_2023_through_12-31-2024.zip", "files/ITA-data-cy2022.zip", "files/ITA-data-cy2021.zip", "files/ITA-Data-CY-2020.zip", "files/ITA Data CY 2019.zip", "files/ITA Data CY 2018.zip", "files/ITA Data CY 2017.zip", "files/ITA Data CY 2016.zip"}, []string{"MN", "WI"}}
}

// Drain returns the bounded drain duration.
func (s Settings) Drain() time.Duration {
	if s.DrainSeconds > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s.DrainSeconds) * time.Second
}
func number(v string, zero bool) bool {
	if v == "0" {
		return zero
	}
	if v == "" || v[0] == '0' {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func capital(v string, n int) bool {
	if len(v) != n {
		return false
	}
	for _, c := range v {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// Read validates and reads the fourteen settings, stopping at the first refusal.
func Read(lookup func(string) (string, bool)) (Settings, error) {
	s := Defaults()
	keys := []string{"DRAIN_SECONDS", "SCRAPE_KEEP_RUNS", "SCRAPE_WRITE_BUDGET", "SCRAPE_PRESSURE_LIMIT", "FMCSA_STATES", "FMCSA_MIN_POWER_UNITS", "FMCSA_STATUS", "PPP_FILES", "PPP_STATES", "MN_PARCELS_COUNTIES", "FORM5500_YEARS", "FORM5500_STATES", "OSHA_ITA_FILES", "OSHA_ITA_STATES"}
	wants := []string{WantSeconds, WantRuns, WantBudget, WantPressure, WantStates, WantPowerUnits, WantStatus, WantFiles, WantStates, WantCounties, WantYears, WantStates, WantPaths, WantStates}
	for i, k := range keys {
		v, ok := lookup(k)
		if !ok || v == "" {
			continue
		}
		valid := false
		switch i {
		case 0, 1, 2, 3, 5:
			valid = number(v, i == 5)
			if i == 3 {
				n, err := strconv.ParseInt(v, 10, 64)
				valid = valid && err == nil && n <= 100
			}
		case 4, 8, 11, 13:
			valid = true
			for _, code := range strings.Split(v, ",") {
				if !capital(code, 2) {
					valid = false
				}
			}
		case 6:
			valid = capital(v, 1)
		case 7:
			valid = fileNames(v)
		case 12:
			valid = filePaths(v)
		case 9:
			valid = countyNames(v)
		case 10:
			valid = true
			for _, year := range strings.Split(v, ",") {
				if len(year) != 4 || !number(year, false) {
					valid = false
				}
			}
		}
		if !valid {
			return Settings{}, &Error{k, v, wants[i]}
		}
		switch i {
		case 0, 1, 2, 3, 5:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				n = math.MaxInt64
			}
			switch i {
			case 0:
				s.DrainSeconds = n
			case 1:
				s.KeepRuns = n
			case 2:
				s.WriteBudget = n
			case 3:
				s.PressureLimit = n
			case 5:
				s.FMCSAMinPowerUnits = n
			}
		case 4:
			s.FMCSAStates = strings.Split(v, ",")
		case 6:
			s.FMCSAStatus = v
		case 7:
			s.PPPFiles = strings.Split(v, ",")
		case 8:
			s.PPPStates = strings.Split(v, ",")
		case 9:
			s.MNParcelsCounties = strings.Split(v, ",")
		case 10:
			s.Form5500Years = strings.Split(v, ",")
		case 11:
			s.Form5500States = strings.Split(v, ",")
		case 12:
			s.OSHAITAFiles = strings.Split(v, ",")
		case 13:
			s.OSHAITAStates = strings.Split(v, ",")
		}
	}
	return s, nil
}

func countyNames(v string) bool {
	for _, name := range strings.Split(v, ",") {
		if name == "" || !letter(name[0]) || name[len(name)-1] == ' ' {
			return false
		}
		for _, c := range []byte(name) {
			if !letter(c) && c != ' ' && c != '.' && c != '\'' {
				return false
			}
		}
	}
	return true
}

func letter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

func fileNames(v string) bool {
	for _, name := range strings.Split(v, ",") {
		if name == "" || name[0] == '.' {
			return false
		}
		for _, c := range []byte(name) {
			switch {
			case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
			default:
				return false
			}
		}
	}
	return true
}

func filePaths(v string) bool {
	for _, path := range strings.Split(v, ",") {
		for _, segment := range strings.Split(path, "/") {
			if segment == "" || segment[0] == '.' || segment[0] == ' ' || segment[len(segment)-1] == ' ' {
				return false
			}
			for _, c := range []byte(segment) {
				if !letter(c) && (c < '0' || c > '9') && c != '_' && c != '-' && c != '.' && c != ' ' {
					return false
				}
			}
		}
	}
	return true
}
