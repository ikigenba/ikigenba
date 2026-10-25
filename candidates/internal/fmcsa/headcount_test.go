package fmcsa_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-463S-GCA4
func TestReportedHeadcount(t *testing.T) {
	for _, tc := range []struct {
		class, units, drivers, mileage, date string
		count                                int64
	}{
		{"PRIVATE PROPERTY", "5", "20", "10000", "20250318", 20},
		{"AUTHORIZED FOR HIRE", "10", "12", "", "", 12},
		{"", "forty", "3", "", "", 3},
		{"", "1828", "1", "19390412", "", 1},
		{"", "199999", "4", "0", "", 0},
		{"", "5", "0", "", "", 0},
		{"", "5", "", "", "", 0},
		{"", "5", "12.0", "", "", 0},
	} {
		r := scrapes.Row{Facts: facts(map[string]string{"class": tc.class, "power_units": tc.units, "total_drivers": tc.drivers, "mileage": tc.mileage, "mcs150_date": tc.date})}
		got := fmcsa.New().Read(r).Headcount
		var want *sources.Headcount
		if tc.count > 0 {
			d := time.Time{}
			if tc.date != "" {
				d = time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC)
			}
			want = &sources.Headcount{Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: tc.count, AsOf: d}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(tc, got, want)
		}
	}
}
