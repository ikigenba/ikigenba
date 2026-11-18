package valid_test

import (
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// R-N78A-E943.
func TestAddressErrorsAreDistinct(t *testing.T) {
	sentinels := []error{valid.ErrName, valid.ErrSlug, valid.ErrReserved, valid.ErrKey, valid.ErrType, valid.ErrSize, valid.ErrAddress, valid.ErrPrefix}
	for i, err := range sentinels {
		if err == nil {
			t.Fatalf("sentinel %d is nil", i)
		}
		for j, other := range sentinels {
			if i != j && errors.Is(err, other) {
				t.Fatalf("sentinels %d and %d overlap", i, j)
			}
		}
	}
}

// R-NAVZ-JKC6.
func TestAddressLimits(t *testing.T) {
	const (
		unbans    uint16 = valid.MaxUnbansPerHost
		allowlist uint16 = valid.MaxAllowlistPerHost
		ipv4      uint16 = valid.MinPrefixIPv4
		ipv6      uint16 = valid.MinPrefixIPv6
	)
	if unbans != 50 || allowlist != 50 || ipv4 != 24 || ipv6 != 48 {
		t.Fatalf("address limits = %d, %d, %d, %d", unbans, allowlist, ipv4, ipv6)
	}
}

// R-N9O3-5SLH, R-NC3V-XC2V, R-NDBS-B3TK, R-NFRL-2NAY.
func TestAddressAndAllowEntry(t *testing.T) {
	type example struct {
		input, want string
		err         error
	}
	addresses := []example{
		{"192.0.2.7", "192.0.2.7", nil},
		{"2001:DB8::1", "2001:db8::1", nil},
		{"2001:db8:0:0:0:0:0:1", "2001:db8::1", nil},
		{"::ffff:192.0.2.7", "192.0.2.7", nil},
		{"fe80::1%eth0", "", valid.ErrAddress},
		{" 192.0.2.7", "", valid.ErrAddress},
		{"192.0.2.07", "", valid.ErrAddress},
		{"1.2.3", "", valid.ErrAddress},
		{"", "", valid.ErrAddress},
	}
	prefixes := []example{
		{"192.0.2.77/24", "192.0.2.0/24", nil},
		{"192.0.2.7/32", "192.0.2.7", nil},
		{"::ffff:192.0.2.7/128", "192.0.2.7", nil},
		{"::ffff:192.0.2.0/120", "192.0.2.0/24", nil},
		{"::ffff:192.0.2.0/95", "::fffe:0:0/95", nil},
		{"2001:db8::/48", "2001:db8::/48", nil},
		{"2001:DB8:0:1::5/48", "2001:db8::/48", nil},
		{"2001:db8::1/128", "2001:db8::1", nil},
		{"192.0.2.0/23", "", valid.ErrPrefix},
		{"10.0.0.0/8", "", valid.ErrPrefix},
		{"0.0.0.0/0", "", valid.ErrPrefix},
		{"::ffff:192.0.2.0/112", "", valid.ErrPrefix},
		{"2001:db8::/47", "", valid.ErrPrefix},
		{"192.0.2.0/33", "", valid.ErrAddress},
		{"x/24", "", valid.ErrAddress},
		{"192.0.2.0/024", "", valid.ErrAddress},
		{"192.0.2.7/", "", valid.ErrAddress},
		{"fe80::%eth0/64", "", valid.ErrAddress},
		{"192.0.2.07/24", "", valid.ErrAddress},
	}
	for _, test := range []struct {
		name     string
		parse    func(string) (string, error)
		examples []example
	}{
		{"Address", valid.Address, append(addresses, example{"192.0.2.7/32", "", valid.ErrAddress})},
		{"AllowEntry", valid.AllowEntry, append(addresses, prefixes...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, tc := range test.examples {
				got, err := test.parse(tc.input)
				if got != tc.want || !errors.Is(err, tc.err) {
					t.Errorf("parse %q = %q, %v; want %q, %v", tc.input, got, err, tc.want, tc.err)
					continue
				}
				if err == nil {
					if again, err := test.parse(got); again != got || err != nil {
						t.Errorf("reparse %q = %q, %v", got, again, err)
					}
				}
			}
		})
	}
}

// R-N9O3-5SLH, R-NEJO-OVK9.
func TestCoversCanonicalAddresses(t *testing.T) {
	_ = struct{ covers func(string, string) bool }{valid.Covers}
	for _, tc := range []struct {
		entry, address string
		want           bool
	}{
		{"192.0.2.0/24", "192.0.2.77", true},
		{"192.0.2.0/24", "192.0.3.1", false},
		{"192.0.2.0/24", "2001:db8::1", false},
		{"192.0.2.7", "192.0.2.7", true},
		{"192.0.2.7", "192.0.2.8", false},
		{"2001:db8::/48", "2001:db8:0:ffff::1", true},
		{"2001:db8::/48", "2001:db9::1", false},
		{"::fffe:0:0/95", "::fffe:0:1", true},
		{"::fffe:0:0/95", "192.0.2.7", false},
		{"192.0.2.77/24", "192.0.2.77", false},
		{"192.0.2.0/24", "::ffff:192.0.2.77", false},
		{"2001:db8::/48", "2001:DB8::1", false},
		{"10.0.0.0/8", "10.1.2.3", false},
		{"192.0.2.7/32", "192.0.2.7", false},
		{"", "", false},
	} {
		if got := valid.Covers(tc.entry, tc.address); got != tc.want {
			t.Errorf("Covers(%q, %q) = %t, want %t", tc.entry, tc.address, got, tc.want)
		}
	}
}
