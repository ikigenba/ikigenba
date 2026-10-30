package settings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

// R-CZ10-4RU5
func TestWantSecondsCopy(t *testing.T) {
	const want string = settings.WantSeconds
	if want == "" || strings.Contains(want, "\n") {
		t.Fatalf("invalid WantSeconds %q", want)
	}
	_, err := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": "invalid"}))
	var refused *settings.Error
	if !errors.As(err, &refused) || refused.Want != want || !strings.HasSuffix(err.Error(), "not "+want) {
		t.Fatalf("drain refusal does not use WantSeconds: %v", err)
	}
}
