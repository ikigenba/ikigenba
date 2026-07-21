package eval

import "testing"

func TestAcceptanceUsesStrictNoiseFloor(t *testing.T) {
	// R-KWZR-ZFSM
	if got := Epsilon([]float64{0.81, 0.79, 0.80}); got < 0.019999 || got > 0.020001 {
		t.Fatalf("epsilon = %v", got)
	}
	if !Accept(0.821, 0.8, 0.02) {
		t.Fatal("strictly above best plus epsilon should accept")
	}
	if Accept(0.82, 0.8, 0.02) || Accept(0.819, 0.8, 0.02) {
		t.Fatal("equal or below best plus epsilon should reject")
	}
}
