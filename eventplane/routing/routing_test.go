package routing

import "testing"

func TestKey(t *testing.T) {
	// R-3FIX-KJG7
	for _, tc := range []struct{ source, kind, subject, want string }{
		{"ledger", "recorded", "", "ledger:recorded"},
		{"dropbox", "create", "/bills/aws/2026-06.pdf", "dropbox:create/bills/aws/2026-06.pdf"},
	} {
		if got := Key(tc.source, tc.kind, tc.subject); got != tc.want {
			t.Errorf("Key(%q, %q, %q) = %q, want %q", tc.source, tc.kind, tc.subject, got, tc.want)
		}
	}
}

func TestMatchDialect(t *testing.T) {
	tests := []struct {
		name, pattern, key string
		want               bool
	}{
		// R-3GQT-YB6W
		{"star cannot cross slash", "dropbox:create/bills/*.pdf", "dropbox:create/bills/aws/2026-06.pdf", false},
		{"star within segment", "dropbox:create/bills/*.pdf", "dropbox:create/bills/x.pdf", true},
		// R-3HYQ-C2XL
		{"double star crosses segments", "dropbox:create/bills/**/*.pdf", "dropbox:create/bills/aws/2026-06.pdf", true},
		{"double star crosses many segments", "dropbox:create/bills/**/*.pdf", "dropbox:create/bills/a/b/c.pdf", true},
		{"double star still respects suffix", "dropbox:create/bills/**/*.pdf", "dropbox:create/bills/aws/readme.txt", false},
		{"double star matches zero segments", "dropbox:create/bills/**/*.pdf", "dropbox:create/bills/x.pdf", true},
		// R-3J6M-PUOA
		{"no prefix elision", "bills/**", "dropbox:create/bills/x.pdf", false},
		{"no suffix elision", "dropbox:create", "dropbox:create/bills/x.pdf", false},
		// R-3KEJ-3MEZ
		{"question one character", "cron:tick/a?c", "cron:tick/abc", true},
		{"question requires character", "cron:tick/a?c", "cron:tick/ac", false},
		{"question cannot cross slash", "cron:tick/a?c", "cron:tick/a/c", false},
		{"class matches", "dropbox:create/bills/[ab]*.pdf", "dropbox:create/bills/a1.pdf", true},
		{"class rejects", "dropbox:create/bills/[ab]*.pdf", "dropbox:create/bills/c1.pdf", false},
		{"negated class", "cron:tick/[^ab]", "cron:tick/c", true},
		{"class range", "cron:tick/[a-c]", "cron:tick/b", true},
		{"braces literal", "dropbox:{a,b}", "dropbox:a", false},
		{"literal braces match", "dropbox:{a,b}", "dropbox:{a,b}", true},
		// R-3MUB-V5WD
		{"literal exact", "cron:tick/bill-sweep", "cron:tick/bill-sweep", true},
		{"literal rejects suffix", "cron:tick/bill-sweep", "cron:tick/bill-sweep2", false},
		{"subjectless star", "dropbox:*", "dropbox:create", true},
		{"subjectful star rejected", "dropbox:*", "dropbox:create/bills/x.pdf", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Match(tc.pattern, tc.key)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.key, got, tc.want)
			}
		})
	}
}

func TestMatchMalformedPattern(t *testing.T) {
	// R-3LMF-HE5O
	if got, err := Match("dropbox:[", "dropbox:create"); got || err == nil {
		t.Fatalf("Match malformed pattern = (%v, %v), want (false, non-nil error)", got, err)
	}
}

func TestCouldMatchSubjectUsesFixedPrefixAndOpenRootedSuffix(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		prefix  string
		want    bool
	}{
		{"dropbox:create/bills/**", "dropbox:create", true},
		{"dropbox:*", "dropbox:create", true},
		{"dropbox:delete/**", "dropbox:create", false},
		{"cron:tick/specific", "cron:tick", true},
	} {
		got, err := CouldMatchSubject(tc.pattern, tc.prefix)
		if err != nil || got != tc.want {
			t.Errorf("CouldMatchSubject(%q, %q) = %v, %v; want %v", tc.pattern, tc.prefix, got, err, tc.want)
		}
	}
	if _, err := CouldMatchSubject("dropbox:[", "dropbox:create"); err == nil {
		t.Error("malformed pattern returned nil error")
	}
}

func TestAddressValidity(t *testing.T) {
	// R-41H4-GESP
	for _, tc := range []struct {
		kind string
		want bool
	}{{"create", true}, {"run.succeeded", true}, {"tick-2", true}, {"", false}, {"Create", false}, {"a b", false}, {"a/b", false}} {
		if got := ValidKind(tc.kind); got != tc.want {
			t.Errorf("ValidKind(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
	for _, tc := range []struct {
		subject string
		want    bool
	}{{"", true}, {"/bills/aws", true}, {"bills", false}, {"/bills\n/aws", false}, {"/bills\r/aws", false}} {
		if got := ValidSubject(tc.subject); got != tc.want {
			t.Errorf("ValidSubject(%q) = %v, want %v", tc.subject, got, tc.want)
		}
	}
}
