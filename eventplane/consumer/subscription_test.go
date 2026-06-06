package consumer

import "testing"

func TestSubscriptionMatch(t *testing.T) {
	cases := []struct {
		name      string
		filter    string
		eventType string
		want      bool
	}{
		{"exact match", "contact.created", "contact.created", true},
		{"exact non-match", "contact.created", "contact.updated", false},
		{"prefix glob match", "contact.*", "contact.created", true},
		{"prefix glob spans dots", "contact.*", "contact.created", true},
		{"prefix glob non-match", "contact.*", "deal.created", false},
		{"star matches all", "*", "contact.created", true},
		{"star matches dotted", "*", "ledger.entry.posted", true},
		{"empty filter non-match", "", "contact.created", false},
		{"malformed pattern never matches", "[", "contact.created", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Subscription{Filter: tc.filter}
			if got := s.Match(tc.eventType); got != tc.want {
				t.Errorf("Subscription{Filter:%q}.Match(%q) = %v, want %v",
					tc.filter, tc.eventType, got, tc.want)
			}
		})
	}
}
