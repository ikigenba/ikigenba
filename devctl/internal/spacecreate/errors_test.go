package spacecreate

import "testing"

func TestRefusedError(t *testing.T) {
	// R-XS6W-S2VB
	_ = RefusedError(struct{ Message string }{})
	err := &RefusedError{Message: "cannot proceed"}
	if got := err.Error(); got != err.Message {
		t.Fatalf("Error() = %q, want %q", got, err.Message)
	}
	if got := err.ExitCode(); got != 2 {
		t.Fatalf("ExitCode() = %d, want 2", got)
	}
}
