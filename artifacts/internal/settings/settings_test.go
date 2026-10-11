package settings_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/artifacts/internal/settings"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// R-YW55-XKE7
func TestAPI(t *testing.T) {
	var _ error = &settings.Error{Name: "n", Value: "v", Unit: "u"}
	s := settings.Settings{DrainSeconds: 2, ArtifactMaxBytes: 3, ImportSeconds: 4}
	if s.Drain() != 2*time.Second || s.Import() != 4*time.Second {
		t.Fatal(s)
	}
}

// R-YXD2-BC4W
func TestDefaults(t *testing.T) {
	if s := settings.Defaults(); s != (settings.Settings{5, 1073741824, 600}) {
		t.Fatal(s)
	}
}

// R-YYKY-P3VL
func TestGrammar(t *testing.T) {
	for _, key := range []string{"DRAIN_SECONDS", "ARTIFACT_MAX_BYTES", "IMPORT_SECONDS"} {
		for _, v := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1G", "1e9", "05", " 5", "5 ", "abc", "٥", "1\n"} {
			if _, e := settings.Read(lookup(map[string]string{key: v})); e == nil {
				t.Errorf("accepted %s=%q", key, v)
			}
		}
		for _, v := range []string{"", "1", "123", "9999999999999999999999999999"} {
			if _, e := settings.Read(lookup(map[string]string{key: v})); e != nil {
				t.Error(e)
			}
		}
	}
	if _, e := settings.Read(lookup(nil)); e != nil {
		t.Fatal(e)
	}
}

// R-YZSV-2VMA
func TestValues(t *testing.T) {
	for _, v := range []string{"9223372036854775807", "9223372036854775808", "99999999999999999999999"} {
		s, e := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": v, "ARTIFACT_MAX_BYTES": v, "IMPORT_SECONDS": v}))
		if e != nil || s != (settings.Settings{math.MaxInt64, math.MaxInt64, math.MaxInt64}) {
			t.Fatal(s, e)
		}
	}
	s, e := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": "12", "ARTIFACT_MAX_BYTES": "34", "IMPORT_SECONDS": "56"}))
	if e != nil || s != (settings.Settings{12, 34, 56}) {
		t.Fatal(s, e)
	}
	for _, m := range []map[string]string{nil, {"DRAIN_SECONDS": "", "ARTIFACT_MAX_BYTES": "", "IMPORT_SECONDS": ""}} {
		s, e = settings.Read(lookup(m))
		if e != nil || s != settings.Defaults() {
			t.Fatal(s, e)
		}
	}
}

// R-Z10R-GNCZ
func TestFirstError(t *testing.T) {
	for _, c := range []struct {
		m       map[string]string
		n, v, u string
	}{{map[string]string{"DRAIN_SECONDS": "bad", "ARTIFACT_MAX_BYTES": "0", "IMPORT_SECONDS": "bad"}, "DRAIN_SECONDS", "bad", "seconds"}, {map[string]string{"ARTIFACT_MAX_BYTES": "0", "IMPORT_SECONDS": "bad"}, "ARTIFACT_MAX_BYTES", "0", "bytes"}, {map[string]string{"IMPORT_SECONDS": "bad"}, "IMPORT_SECONDS", "bad", "seconds"}} {
		_, e := settings.Read(lookup(c.m))
		var x *settings.Error
		if !errors.As(e, &x) || *x != (settings.Error{c.n, c.v, c.u}) {
			t.Fatal(e)
		}
	}
}

// R-Z3GK-86UD
func TestErrorText(t *testing.T) {
	e := &settings.Error{Name: "custom", Value: "bad\nvalue", Unit: "unit"}
	if e.Error() != "custom is 'bad\nvalue', not a positive whole number of unit" {
		t.Fatal(e)
	}
}

// R-Z4OG-LYL2
func TestDuration(t *testing.T) {
	for _, n := range []int64{1, 23, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		want := time.Duration(math.MaxInt64)
		if n <= math.MaxInt64/int64(time.Second) {
			want = time.Duration(n) * time.Second
		}
		s := settings.Settings{DrainSeconds: n, ImportSeconds: n}
		if s.Drain() != want || s.Import() != want {
			t.Fatal(s, want)
		}
	}
}
