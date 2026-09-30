package secrets

import "testing"

func TestEntryHasExactFields(_ *testing.T) {
	// R-FQGR-F925
	_ = Entry(struct {
		App  string
		Keys []string
	}{})
}

func TestParameterPaths(t *testing.T) {
	// R-0ATG-O3LP
	if got, want := Parameter("sbx1.ikigenba.dev", "crm"), "/sbx1.ikigenba.dev/crm"; got != want {
		t.Fatalf("Parameter() = %q, want %q", got, want)
	}
	if got, want := Prefix("sbx1.ikigenba.dev"), "/sbx1.ikigenba.dev"; got != want {
		t.Fatalf("Prefix() = %q, want %q", got, want)
	}
}

func TestObjectErrorHasExactFieldsAndMessage(t *testing.T) {
	// R-FWK9-C3RM
	_ = ObjectError(struct {
		Parameter string
		Reason    string
	}{})

	err := ObjectError{Parameter: "/sbx1.ikigenba.dev/crm", Reason: "invalid object"}
	if got, want := err.Error(), "/sbx1.ikigenba.dev/crm: invalid object"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
