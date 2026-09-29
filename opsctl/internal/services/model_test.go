package services

import "testing"

// R-86XF-542L
func TestChangeValues(t *testing.T) {
	values := []struct {
		change Change
		want   string
	}{
		{Unchanged, "unchanged"},
		{Added, "added"},
		{Removed, "removed"},
		{Disabled, "disabled"},
		{Enabled, "enabled"},
		{Updated, "updated"},
	}
	for _, value := range values {
		if got := string(value.change); got != value.want {
			t.Errorf("Change = %q, want %q", got, value.want)
		}
	}
}

// R-885B-IVTA
func TestChangesFor(t *testing.T) {
	var nilChanges Changes
	if got := nilChanges.For("alpha"); got != Unchanged {
		t.Errorf("nil Changes.For(alpha) = %q, want %q", got, Unchanged)
	}

	changes := Changes{"alpha": Added, "beta": ""}
	if got := changes.For("alpha"); got != Added {
		t.Errorf("Changes.For(alpha) = %q, want %q", got, Added)
	}
	if got := changes.For("beta"); got != "" {
		t.Errorf("Changes.For(beta) = %q, want empty mapped value", got)
	}
	if got := changes.For("missing"); got != Unchanged {
		t.Errorf("Changes.For(missing) = %q, want %q", got, Unchanged)
	}
}
