package page

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain lowercase", "acme corp", "acme corp"},
		{"casefold", "ACME Corp", "acme corp"},
		{"trim", "  Acme Corp  ", "acme corp"},
		{"collapse internal whitespace", "Acme    Corp\tInc", "acme corp inc"},
		{"strip diacritics", "Café Münster", "cafe munster"},
		{"NFKC ligature fold", "ﬁle", "file"},
		{"NFKC fullwidth fold", "ＡＣＭＥ", "acme"},
		{"newlines collapse", "Acme\nCorp", "acme corp"},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"combined", "  ＤÉJÀ   Vu\n", "deja vu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.in); got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeIsDeterministic(t *testing.T) {
	in := "São Paulo — Brazil"
	first := Normalize(in)
	for i := 0; i < 100; i++ {
		if got := Normalize(in); got != first {
			t.Fatalf("Normalize not deterministic: iteration %d gave %q, first was %q", i, got, first)
		}
	}
}

func TestKeySet(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		aliases []string
		want    []string
	}{
		{
			name:    "name plus aliases",
			subject: "Acme Corp",
			aliases: []string{"Acme LLC", "ACME"},
			want:    []string{"acme corp", "acme llc", "acme"},
		},
		{
			name:    "dedup name and alias that normalize equal",
			subject: "Acme Corp",
			aliases: []string{"acme corp", "ACME  CORP"},
			want:    []string{"acme corp"},
		},
		{
			name:    "empty aliases dropped",
			subject: "Acme",
			aliases: []string{"", "   ", "Other"},
			want:    []string{"acme", "other"},
		},
		{
			name:    "empty name with aliases",
			subject: "",
			aliases: []string{"Acme"},
			want:    []string{"acme"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := KeySet(tc.subject, tc.aliases)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("KeySet(%q, %v) = %v, want %v", tc.subject, tc.aliases, got, tc.want)
			}
		})
	}
}
