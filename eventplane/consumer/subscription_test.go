package consumer

import (
	"reflect"
	"testing"
)

func TestSubscriptionIsCanonicalKeyDeclarationWithoutMatchMethod(t *testing.T) {
	s := Subscription{Source: "dropbox", Filter: "dropbox:create/bills/**/*.pdf"}
	if s.Filter != "dropbox:create/bills/**/*.pdf" {
		t.Fatalf("Filter = %q", s.Filter)
	}
	// R-95KP-1QIO: Subscription is declaration-only; matching uses routing.Match.
	if _, ok := reflect.TypeOf(s).MethodByName("Match"); ok {
		t.Fatal("Subscription unexpectedly exposes Match")
	}
}
