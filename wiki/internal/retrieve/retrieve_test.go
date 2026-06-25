package retrieve

import "testing"

func TestSearchLimitsResolveClampsLimitToContract(t *testing.T) {
	tests := map[string]struct {
		in   int
		want int
	}{
		"default":  {in: 0, want: DefaultLimit},
		"one":      {in: 1, want: 1},
		"negative": {in: -3, want: 1},
		"cap":      {in: LimitCap, want: LimitCap},
		"over cap": {in: LimitCap + 1, want: LimitCap},
	}

	for name, tt := range tests {
		if got := (SearchLimits{Limit: tt.in}).Resolve().Limit; got != tt.want {
			t.Fatalf("%s: Resolve().Limit = %d, want %d", name, got, tt.want)
		}
	}
}
