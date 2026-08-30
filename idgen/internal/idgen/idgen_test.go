package idgen

import (
	"testing"
	"time"
)

func TestMintAtEpochGoldenVector(t *testing.T) {
	// R-SKFL-OD3U
	want := "R-" + "0007-J3LA"
	if got := MintAt("R", Epoch); got != want {
		t.Fatalf("MintAt(R, Epoch) = %q, want %q", got, want)
	}
}

func TestMintAtAbsoluteInstantGoldenVector(t *testing.T) {
	// R-SLNI-24UJ
	instant := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	want := "R-" + "OBCA-0VLA"
	if got := MintAt("R", instant); got != want {
		t.Fatalf("MintAt(R, instant) = %q, want %q", got, want)
	}
}

func TestMintAtZeroPadsBody(t *testing.T) {
	// R-SMVE-FWL8
	instant := Epoch.Add(1143449080754 * time.Millisecond)
	want := "P-" + "0000-0000"
	if got := MintAt("P", instant); got != want {
		t.Fatalf("MintAt(P, instant) = %q, want %q", got, want)
	}
}

func TestMintAtClampsBeforeEpoch(t *testing.T) {
	// R-SO3A-TOBX
	beforeEpoch := Epoch.Add(-time.Nanosecond)
	if got, want := MintAt("R", beforeEpoch), MintAt("R", Epoch); got != want {
		t.Fatalf("MintAt before Epoch = %q, want epoch encoding %q", got, want)
	}
}

func TestMintAtTimeOfRoundTrip(t *testing.T) {
	// R-SJ7P-ALD5
	const seed = uint64(1729)
	state := seed
	nextOffset := func() int64 {
		// xorshift64 is a deterministic PRNG suitable for reproducible sampling.
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return int64(state % uint64(modulus))
	}
	prefixes := []string{"R", "abc123", "Z9", "Requirement"}
	for i := 0; i < 1000; i++ {
		ms := nextOffset()
		instant := Epoch.Add(time.Duration(ms) * time.Millisecond)
		for _, prefix := range prefixes {
			id := MintAt(prefix, instant)
			got, err := TimeOf(id)
			if err != nil {
				t.Fatalf("TimeOf(MintAt(%q, offset %dms)) returned error: %v", prefix, ms, err)
			}
			want := instant.Truncate(time.Millisecond).UTC()
			if !got.Equal(want) {
				t.Fatalf("TimeOf(MintAt(%q, offset %dms)) = %s, want %s", prefix, ms, got, want)
			}
		}
	}
}
