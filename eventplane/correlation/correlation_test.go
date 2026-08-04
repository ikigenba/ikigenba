package correlation

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewUsesCrockfordAlphabet(t *testing.T) {
	const required = "0189"
	seen := make(map[byte]bool)
	for range 1000 {
		id := New()
		// R-UBWK-3IAS
		if len(id) != 26 {
			t.Fatalf("New() length = %d, want 26", len(id))
		}
		for i := range id {
			if !strings.ContainsRune(alphabet, rune(id[i])) {
				t.Fatalf("New() returned %q containing non-Crockford character %q", id, id[i])
			}
			if strings.ContainsRune("ILOU", rune(id[i])) {
				t.Fatalf("New() returned %q containing excluded character %q", id, id[i])
			}
			seen[id[i]] = true
		}
	}
	for i := range required {
		if !seen[required[i]] {
			t.Errorf("character %q did not appear across 1000 ids", required[i])
		}
	}
}

func TestNewIsUniqueAndTimeOrdered(t *testing.T) {
	ids := make(map[string]struct{}, 1000)
	for range 1000 {
		ids[New()] = struct{}{}
	}
	// R-UD4G-HA1H
	if len(ids) != 1000 {
		t.Fatalf("1000 New() calls produced %d distinct ids", len(ids))
	}
	for range 20 {
		before := New()
		time.Sleep(2 * time.Millisecond)
		after := New()
		if after <= before {
			t.Fatalf("later id %q does not sort after earlier id %q", after, before)
		}
	}
}

func TestValidAcceptsMintedAndRejectsMalformedIDs(t *testing.T) {
	if id := New(); !Valid(id) {
		t.Fatalf("Valid(New()) = false for %q", id)
	}

	malformed := []string{
		"",
		strings.Repeat("0", 25),
		strings.Repeat("0", 27),
		strings.Repeat("a", 26),
		"I" + strings.Repeat("0", 25),
		"L" + strings.Repeat("0", 25),
		"O" + strings.Repeat("0", 25),
		"U" + strings.Repeat("0", 25),
	}
	for _, id := range malformed {
		// R-UECC-V1S6
		if Valid(id) {
			t.Errorf("Valid(%q) = true, want false", id)
		}
	}
}

func TestContextRoundTripAndMalformedID(t *testing.T) {
	ctx := context.Background()
	// R-UFK9-8TIV
	if got := FromContext(ctx); got != "" {
		t.Fatalf("FromContext(background) = %q, want empty", got)
	}
	id := New()
	if got := FromContext(WithContext(ctx, id)); got != id {
		t.Fatalf("round trip = %q, want %q", got, id)
	}
	if got := FromContext(WithContext(ctx, "not-a-correlation-id")); got != "" {
		t.Fatalf("malformed id was stored as %q", got)
	}
}

func TestEnsurePreservesExistingAndMintsForBareContext(t *testing.T) {
	existing := New()
	existingContext := WithContext(context.Background(), existing)
	returnedContext, returnedID := Ensure(existingContext)
	// R-UGS5-ML9K
	if returnedID != existing || FromContext(returnedContext) != existing {
		t.Fatalf("Ensure(existing) returned id %q and context id %q, want %q", returnedID, FromContext(returnedContext), existing)
	}

	firstContext, first := Ensure(context.Background())
	_, second := Ensure(context.Background())
	if !Valid(first) || FromContext(firstContext) != first {
		t.Fatalf("Ensure(bare) returned id %q and context id %q", first, FromContext(firstContext))
	}
	if first == second {
		t.Fatalf("Ensure on two bare contexts returned duplicate id %q", first)
	}
}

func TestHeaderContract(t *testing.T) {
	// R-UI02-0D09
	if Header != "X-Correlation-Id" {
		t.Fatalf("Header = %q, want %q", Header, "X-Correlation-Id")
	}
}
