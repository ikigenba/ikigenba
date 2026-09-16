package cli_test

import (
	"fmt"
	"testing"
)

const wantInstallUsage = `Usage: opsctl install URI

Install the app at URI, an s3:// object holding an <app>-<tag>.tar.xz built by
devctl. The app name, its port, and the secrets it needs are read from
etc/manifest.toml inside it; the secret values are read from the parameter
/ikigenba/<host.name>/<app>.

Nothing under /opt/<app>/state/ or /opt/<app>/cache/ is touched, so installing
over a running app keeps its data. Safe to re-run.

The nginx configuration and /etc/litestream.yml are regenerated from every app
on the host, so an app that declares a [database] is replicated from the
moment it is installed. litestream.service is restarted only when its
configuration changed.

Configuration keys:
  aws.region  the region this host's parameters and artifacts live in
  host.name   the fully-qualified name this host answers at
  backup.s3_uri  the prefix this host backs up to
  backup.service_db_seconds  how often a declared database is snapshotted whole
  backup.service_wal_seconds  how often a declared database's committed changes are shipped
`

func TestInstallHelpIsInert(t *testing.T) {
	// R-ZZZM-YX8L
	for _, uid := range []int{0, 1, -1, 1000} {
		for _, option := range []string{"-h", "--help"} {
			t.Run(fmt.Sprintf("%s/%d", option, uid), func(t *testing.T) {
				deps, assertNoAccess := inertDeps(t, uid)
				stdout, stderr, code := invoke([]string{"install", option}, deps)
				assertNoAccess()
				if code != 0 || stdout != wantInstallUsage || stderr != "" {
					t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
				}
			})
		}
	}
}

func TestInstallGrammarBeforeHostAccess(t *testing.T) {
	// R-OPER-SMO2
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing", []string{"install"}, "opsctl: install needs URI\n\nsee 'opsctl install --help' for usage\n"},
		{"multiple", []string{"install", "s3://bucket/key", "extra"}, "opsctl: install takes one URI\n\nsee 'opsctl install --help' for usage\n"},
		{"relative", []string{"install", "bucket/key"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"wrong scheme", []string{"install", "https://bucket/key"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"missing bucket", []string{"install", "s3:///key"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"missing key", []string{"install", "s3://bucket"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"empty key", []string{"install", "s3://bucket/"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"empty query", []string{"install", "s3://bucket/key?"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"query", []string{"install", "s3://bucket/key?version=1"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"empty fragment", []string{"install", "s3://bucket/key#"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"fragment", []string{"install", "s3://bucket/key#part"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
		{"opaque", []string{"install", "s3:bucket/key"}, "opsctl: install takes an s3:// URI\n\nsee 'opsctl install --help' for usage\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, assertNoAccess := inertDeps(t, 1000)
			stdout, stderr, code := invoke(tc.args, deps)
			assertNoAccess()
			if code != 2 || stdout != "" || stderr != tc.want {
				t.Fatalf("exit %d stdout %q stderr %q, want exit 2 empty stdout stderr %q", code, stdout, stderr, tc.want)
			}
		})
	}

	deps, assertNoAccess := inertDeps(t, 1000)
	stdout, stderr, code := invoke([]string{"install", "s3://bucket/key"}, deps)
	assertNoAccess()
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Fatalf("valid grammar root check: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}
