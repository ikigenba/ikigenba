package settings_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/ledger/internal/settings"
)

const _ = settings.WantSeconds

// R-6OI4-2A2M R-6PQ0-G1TB R-6QXW-TTK0 R-6TDP-LD1E R-6ULL-Z4S3 R-6VTI-CWIS R-6Y9B-4G06 R-734W-NIYY
func TestAccepted(t *testing.T) {
	for range 2 {
		if settings.Defaults() != (settings.Settings{5}) {
			t.Fatal("defaults")
		}
	}
	if settings.WantSeconds == "" || strings.Contains(settings.WantSeconds, "\n") {
		t.Fatal("copy")
	}
	for _, tc := range []struct {
		v   string
		set bool
		n   int64
	}{{"", false, 5}, {"", true, 5}, {"30", true, 30}, {"1", true, 1}, {"99999999999999999999", true, math.MaxInt64}} {
		s, e := settings.Read(func(k string) (string, bool) {
			if k != "DRAIN_SECONDS" {
				t.Fatalf("unexpected key %s", k)
			}
			return tc.v, tc.set
		})
		if e != nil || s.DrainSeconds != tc.n {
			t.Fatal(s, e)
		}
	}
	for _, n := range []int64{1, 5, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		want := time.Duration(math.MaxInt64)
		if n <= math.MaxInt64/int64(time.Second) {
			want = time.Duration(n) * time.Second
		}
		if (settings.Settings{n}).Drain() != want {
			t.Fatal(n)
		}
	}
}

// R-6S5T-7LAP R-6X1E-QO9H R-70P3-VZHK R-71X0-9R89
func TestRefused(t *testing.T) {
	for _, v := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1e9", "05", " 5", "5 ", "abc", "١", "1\n"} {
		_, err := settings.Read(func(_ string) (string, bool) { return v, true })
		var e *settings.Error
		if !errors.As(err, &e) || *e != (settings.Error{"DRAIN_SECONDS", v, settings.WantSeconds}) || e.Error() != e.Name+" is '"+v+"', not "+e.Want {
			t.Fatal(v, err)
		}
	}
}
