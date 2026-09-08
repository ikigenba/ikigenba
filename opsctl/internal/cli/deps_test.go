package cli

import "testing"

func TestNilGetenvReadsAsEmpty(t *testing.T) {
	// R-LXGR-SR2H
	if got := (Deps{}).getenv("OPSCTL_TEST"); got != "" {
		t.Fatalf("nil Getenv read = %q, want empty", got)
	}

	deps := Deps{Getenv: func(key string) string { return "value for " + key }}
	if got := deps.getenv("OPSCTL_TEST"); got != "value for OPSCTL_TEST" {
		t.Fatalf("injected Getenv read = %q", got)
	}
}
