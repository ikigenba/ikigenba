package hostsetup

import "testing"

func TestInstallerConstants(t *testing.T) {
	// R-WFMH-V6LF
	if DownloadURL != "https://github.com/ikigenba/ikigenba/releases/download" || InstallerPath != "/tmp/opsctl-install" || DNSProvider != "route53" {
		t.Fatal("installer constants")
	}
}
