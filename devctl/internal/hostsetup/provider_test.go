package hostsetup

import "testing"

func TestDNSProvider(t *testing.T) {
	// R-RSW4-UDDM
	if DNSProvider != "route53" {
		t.Fatalf("DNSProvider = %q", DNSProvider)
	}
}
