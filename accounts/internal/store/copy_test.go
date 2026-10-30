package store_test

import (
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-D1GS-WBBJ
func TestUnreachableCopy(t *testing.T) {
	const unreachable string = store.Unreachable
	isNil := func(value *error) bool { return *value == nil }
	if isNil(&store.ErrUnreachable) || unreachable == "" || strings.Contains(unreachable, "\n") {
		t.Fatalf("invalid unreachable error or copy: %v, %q", store.ErrUnreachable, unreachable)
	}
}
