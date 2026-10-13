package tools

import (
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-5QRJ-WSYK
func TestFreshnessAsScoredBounds(t *testing.T) {
	for _, tt := range []struct{ confidence, effective, want float64 }{{.8, .4, .5}, {.7, .7, 1}, {0, 0, 0}, {0, .7, 0}, {-.1, .2, 0}, {.8, -.4, 0}, {.8, 1.6, 1}} {
		got := assessmentOf(store.Assessment{Confidence: tt.confidence, Effective: tt.effective})
		if got.Freshness != tt.want {
			t.Fatalf("confidence %v effective %v freshness %v want %v", tt.confidence, tt.effective, got.Freshness, tt.want)
		}
	}
}
