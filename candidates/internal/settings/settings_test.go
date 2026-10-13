package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/settings"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// R-1B0H-BR11 R-1C8D-PIRQ R-1EO6-H294 R-1KRO-DWYL
func TestAccepted(t *testing.T) {
	defaults := settings.Settings{DrainSeconds: 5, KeepPasses: 20}
	if settings.Defaults() != defaults {
		t.Fatal(settings.Defaults())
	}
	for _, tc := range []struct {
		m    map[string]string
		want settings.Settings
	}{{nil, defaults}, {map[string]string{"DRAIN_SECONDS": "", "CANDIDATES_KEEP_PASSES": ""}, defaults}, {map[string]string{"DRAIN_SECONDS": "7", "CANDIDATES_KEEP_PASSES": "3", "OTHER": "bad"}, settings.Settings{7, 3}}, {map[string]string{"DRAIN_SECONDS": "999999999999999999999999999", "CANDIDATES_KEEP_PASSES": "999999999999999999999999999"}, settings.Settings{math.MaxInt64, math.MaxInt64}}} {
		got, err := settings.Read(lookup(tc.m))
		if err != nil || got != tc.want {
			t.Fatalf("%v: %v %v", tc.m, got, err)
		}
	}
	for _, n := range []int64{1, 5, math.MaxInt64 / int64(time.Second), math.MaxInt64} {
		got := (settings.Settings{DrainSeconds: n}).Drain()
		want := time.Duration(math.MaxInt64)
		if n <= math.MaxInt64/int64(time.Second) {
			want = time.Duration(n) * time.Second
		}
		if got != want {
			t.Fatal(n, got, want)
		}
	}
}

// R-1DGA-3AIF R-1FW2-UTZT R-1IBV-MDH7
func TestRefused(t *testing.T) {
	for _, name := range []string{"DRAIN_SECONDS", "CANDIDATES_KEEP_PASSES"} {
		for _, v := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc", "١"} {
			_, err := settings.Read(lookup(map[string]string{name: v}))
			var e *settings.Error
			if !errors.As(err, &e) {
				t.Fatalf("%q %q: %v", name, v, err)
			}
			want := settings.WantSeconds
			if name == "CANDIDATES_KEEP_PASSES" {
				want = settings.WantPasses
			}
			if e.Name != name || e.Value != v || e.Want != want || e.Error() != name+" is '"+v+"', not "+want {
				t.Fatal(e)
			}
		}
	}
	_, err := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": "x", "CANDIDATES_KEEP_PASSES": "0"}))
	var e *settings.Error
	if !errors.As(err, &e) || e.Name != "DRAIN_SECONDS" {
		t.Fatal(err)
	}
}

// R-1JJS-057W
func TestCopy(t *testing.T) {
	if settings.WantSeconds == "" || settings.WantPasses == "" || settings.WantSeconds == settings.WantPasses || strings.ContainsAny(settings.WantSeconds+settings.WantPasses, "\n") {
		t.Fatal("invalid copy constants")
	}
}
