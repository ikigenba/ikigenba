package run

import "testing"

// TestCitationGateAllSurvive — a rewrite that keeps every old citation passes.
func TestCitationGateAllSurvive(t *testing.T) {
	old := "Acme builds rockets. [01HX] It also builds anvils. [01HY]"
	new := "Acme builds rockets and anvils. [01HX] [01HY] (now corroborated [01HZ])"
	if err := checkCitationPreservation(old, new, nil); err != nil {
		t.Fatalf("all-survive should pass: %v", err)
	}
}

// TestCitationGateDeclaredSupersededPasses — a dropped citation is fine when it is
// declared in the superseded list (§6.1: "survives OR was deliberately superseded").
func TestCitationGateDeclaredSupersededPasses(t *testing.T) {
	old := "Old fact. [01HX] Newer fact. [01HY]"
	new := "Newer fact. [01HY]"
	if err := checkCitationPreservation(old, new, []string{"01HX"}); err != nil {
		t.Fatalf("declared-superseded drop should pass: %v", err)
	}
}

// TestCitationGateUndeclaredLossFails — a dropped citation NOT in the superseded
// list is the §6.1 failed call (evidence paraphrased away).
func TestCitationGateUndeclaredLossFails(t *testing.T) {
	old := "Fact A. [01HX] Fact B. [01HY]"
	new := "Fact B. [01HY]" // 01HX vanished, undeclared
	err := checkCitationPreservation(old, new, nil)
	if err == nil {
		t.Fatal("undeclared citation loss must fail the gate")
	}
	// Only the undeclared id should be named.
	if got := err.Error(); !contains(got, "01HX") || contains(got, "01HY") {
		t.Fatalf("gate error should name 01HX only: %q", got)
	}
}

// TestCitationGateNewPageDropsNothing — a created page (empty old body) can never
// fail the gate.
func TestCitationGateNewPageDropsNothing(t *testing.T) {
	if err := checkCitationPreservation("", "Fresh. [01HX]", nil); err != nil {
		t.Fatalf("new page should never trip the gate: %v", err)
	}
}

// TestCitationGateOverDeclarationTolerated — declaring a superseded id that is in
// fact still present is tolerated (the gate forbids SILENT loss, not redundant
// declarations).
func TestCitationGateOverDeclarationTolerated(t *testing.T) {
	old := "Fact. [01HX]"
	new := "Fact still here. [01HX]"
	if err := checkCitationPreservation(old, new, []string{"01HX"}); err != nil {
		t.Fatalf("over-declaration should be tolerated: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
